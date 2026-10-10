package protocolsync

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build"
	"go/doc"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracle"
)

type oracleContractError struct{ error }
type oracleIntegrityError struct{ error }

func (e *oracleContractError) Unwrap() error  { return e.error }
func (e *oracleIntegrityError) Unwrap() error { return e.error }

// runTargetOracle keeps expectations out of the candidate's test package. The
// controller derives source coverage and compiles a separate immutable probe;
// candidate TestMain functions cannot skip it or supply its expected values.
func runTargetOracle(ctx context.Context, req ResumeRequest, root string, sandbox oracleSandbox) error {
	baseline := filepath.Join(req.ModuleRoot, "internal/protocolschema/appserver/v2")
	specification, err := schemaOracleRequest(baseline, filepath.Join(req.CandidateDir, "schema"), filepath.Join(root, "codexsdk/protocolv2"))
	if err != nil {
		return err
	}
	if len(specification.Bindings) == 0 {
		return nil
	} // no changed wire contracts; baseline regression still runs
	specification.Context = ctx
	source, err := schemaoracle.Generate(specification)
	if err != nil {
		return &oracleContractError{err}
	}
	dir := filepath.Join(root, "codexsdk/internal/schemaoracleprobe")
	if err = os.Mkdir(dir, 0700); err != nil {
		return fmt.Errorf("create independent probe: %w", err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "contract_test.go")
	if err = os.WriteFile(path, source, 0600); err != nil {
		return err
	}
	sandbox.readonly = append(sandbox.readonly, dir)
	before, err := repairPatch(root)
	if err != nil {
		return err
	}
	cmd := sandbox.command(ctx, filepath.Join(sandbox.goroot, "bin", "go"), "test", "-count=1", "-json", "./internal/schemaoracleprobe")
	var events oracleTestEvents
	cmd.Stdout = &events
	var stderr repairOutput
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	_, _ = events.diagnostic.Write([]byte(stderr.String()))
	after, err := repairPatch(root)
	if err != nil {
		return err
	}
	if repairDigest(before) != repairDigest(after) {
		return &oracleIntegrityError{fmt.Errorf("independent probe execution changed candidate bytes")}
	}
	if runErr != nil || !events.started || !events.passed || events.failed || events.invalid || events.pending != "" {
		return fmt.Errorf("independent schema contract: %v (started=%t passed=%t failed=%t): %s", runErr, events.started, events.passed, events.failed, boundedRepairDiagnostic(events.diagnostic.String()))
	}
	return nil
}

type oracleTestEvents struct {
	pending                          string
	started, passed, failed, invalid bool
	diagnostic                       repairOutput
}

func (w *oracleTestEvents) Write(p []byte) (int, error) {
	n := len(p)
	w.pending += string(p)
	for {
		line, rest, ok := strings.Cut(w.pending, "\n")
		if !ok {
			break
		}
		w.pending = rest
		var e struct{ Action, Test, Package, Output string }
		if json.Unmarshal([]byte(line), &e) != nil {
			w.invalid = true
			continue
		}
		if e.Action == "output" {
			_, _ = w.diagnostic.Write([]byte(e.Output))
		}
		if e.Package != "github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracleprobe" || e.Test != "TestTrustedSchemaOracle" {
			continue
		}
		switch e.Action {
		case "run":
			w.started = true
		case "pass":
			if !w.started {
				w.invalid = true
			}
			w.passed = true
		case "fail", "skip":
			w.failed = true
		}
	}
	if len(w.pending) > 4<<20 {
		w.invalid = true
		w.pending = ""
	}
	return n, nil
}

// runBaselineOracle exercises proposed handwritten implementation using the
// accepted schema and accepted public/runtime assertions. Generator internals
// may refactor: only its stable CLI and public output contract are required.
func runBaselineOracle(ctx context.Context, req ResumeRequest, targetSandbox oracleSandbox) error {
	binary := filepath.Join(targetSandbox.work, "baseline-protocolv2gen")
	cmd := targetSandbox.command(ctx, filepath.Join(targetSandbox.goroot, "bin", "go"), "build", "-o", binary, "./internal/cmd/protocolv2gen")
	if out, err := boundedCommandOutput(cmd); err != nil {
		return fmt.Errorf("build baseline generator: %w: %s", err, boundedRepairDiagnostic(string(out)))
	}
	dir, err := os.MkdirTemp("", "protocol-baseline-oracle-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	root := filepath.Join(dir, "repo")
	cmd = exec.CommandContext(ctx, "git", "clone", "--quiet", "--no-hardlinks", req.RepoRoot, root)
	if out, err := boundedCommandOutput(cmd); err != nil {
		return fmt.Errorf("clone baseline regression: %w: %s", err, boundedRepairDiagnostic(string(out)))
	}
	expected, err := acceptedBaselineTests(filepath.Join(root, "codexsdk"))
	if err != nil {
		return &oracleContractError{err}
	}
	paths, err := ChangedPaths(req.RepoRoot)
	if err != nil {
		return err
	}
	var implementation []string
	for _, p := range paths {
		if !strings.HasSuffix(p, "_test.go") {
			implementation = append(implementation, p)
		}
	}
	if len(implementation) > 0 {
		patch, err := exec.Command("git", append([]string{"-C", req.RepoRoot, "diff", "--binary", "HEAD", "--"}, implementation...)...).Output()
		if err != nil {
			return err
		}
		if err = applyProposalPatchBytes(root, patch); err != nil {
			return err
		}
	}
	if err = os.MkdirAll(filepath.Join(root, "codexsdk/.cache/openai-codex"), 0700); err != nil {
		return err
	}
	sandbox, err := newOracleSandbox(ctx, root, req.CandidateDir)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		return err
	}
	program := filepath.Join(sandbox.work, "protocolv2gen")
	if err = os.WriteFile(program, data, 0700); err != nil {
		return err
	}
	sandbox.readonly = append(sandbox.readonly, filepath.Join(root, "codexsdk/internal/protocolschema/appserver/v2"))
	before, err := baselineOracleNonGeneratedPatch(root)
	if err != nil {
		return err
	}
	cmd = sandbox.command(ctx, program, "-schema-root", "internal/protocolschema/appserver/v2", "-out", "protocolv2")
	out, runErr := boundedCommandOutput(cmd)
	after, err := baselineOracleNonGeneratedPatch(root)
	if err != nil {
		return err
	}
	if repairDigest(before) != repairDigest(after) {
		return &oracleIntegrityError{fmt.Errorf("baseline generator changed implementation or accepted fixtures")}
	}
	if runErr != nil {
		return fmt.Errorf("baseline generator: %w: %s", runErr, boundedRepairDiagnostic(string(out)))
	}
	frozen, err := repairPatch(root)
	if err != nil {
		return err
	}
	cmd = sandbox.command(ctx, filepath.Join(sandbox.goroot, "bin", "go"), "test", "-count=1", "-json", ".", "./protocolv2", "./internal/wirejson")
	events := baselineTestEvents{expected: expected, ran: map[string]bool{}, passed: map[string]bool{}}
	cmd.Stdout = &events
	var stderr repairOutput
	cmd.Stderr = &stderr
	runErr = cmd.Run()
	_, _ = events.diagnostic.Write([]byte(stderr.String()))
	after, err = repairPatch(root)
	if err != nil {
		return err
	}
	if repairDigest(frozen) != repairDigest(after) {
		return &oracleIntegrityError{fmt.Errorf("baseline test execution changed accepted fixtures")}
	}
	if proofErr := events.verify(); runErr != nil || proofErr != nil {
		return fmt.Errorf("baseline runtime assertions: process=%v evidence=%v: %s", runErr, proofErr, boundedRepairDiagnostic(events.diagnostic.String()))
	}
	return nil
}
func baselineOracleNonGeneratedPatch(root string) ([]byte, error) {
	if _, err := repairPatch(root); err != nil {
		return nil, err
	}
	return exec.Command("git", "-C", root, "diff", "--binary", "HEAD", "--", ".", ":(exclude)codexsdk/protocolv2/protocol_types.gen.go", ":(exclude)codexsdk/protocolv2/method_registry.gen.go", ":(exclude)codexsdk/protocolv2/experimental_members.gen.go").Output()
}

// Select from accepted test files before applying any candidate implementation.
// Build constraints use the controller's current platform; these tests execute
// in the same platform sandbox. Neither TestMain nor candidate output selects
// which accepted assertions must complete.
func acceptedBaselineTests(module string) (map[string]bool, error) {
	expected := map[string]bool{}
	for _, rel := range []string{".", "protocolv2", "internal/wirejson"} {
		dir := filepath.Join(module, rel)
		pkg, err := build.Default.ImportDir(dir, build.IgnoreVendor)
		if err != nil {
			return nil, fmt.Errorf("accepted baseline test selection %s: %w", rel, err)
		}
		packagePath := "github.com/ronhuafeng/llm-go/codexsdk"
		if rel != "." {
			packagePath += "/" + rel
		}
		files := append(append([]string{}, pkg.TestGoFiles...), pkg.XTestGoFiles...)
		for _, filename := range files {
			f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, filename), nil, parser.ParseComments)
			if err != nil {
				return nil, err
			}
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || (!strings.HasPrefix(fn.Name.Name, "Test") && !strings.HasPrefix(fn.Name.Name, "Fuzz")) || fn.Name.Name == "TestMain" {
					continue
				}
				prefix := "Test"
				if strings.HasPrefix(fn.Name.Name, "Fuzz") {
					prefix = "Fuzz"
				}
				suffix := strings.TrimPrefix(fn.Name.Name, prefix)
				if suffix != "" {
					r, _ := utf8.DecodeRuneInString(suffix)
					if unicode.IsLower(r) {
						continue
					}
				}
				expected[packagePath+"/"+fn.Name.Name] = true
			}
			for _, example := range doc.Examples(f) {
				if example.Output != "" || example.EmptyOutput {
					expected[packagePath+"/Example"+example.Name] = true
				}
			}
		}
	}
	if len(expected) == 0 {
		return nil, fmt.Errorf("accepted baseline contains no executable tests")
	}
	return expected, nil
}

type baselineTestEvents struct {
	expected, ran, passed map[string]bool
	pending               string
	invalid               bool
	diagnostic            repairOutput
}

func (w *baselineTestEvents) Write(p []byte) (int, error) {
	w.pending += string(p)
	for {
		line, rest, ok := strings.Cut(w.pending, "\n")
		if !ok {
			break
		}
		w.pending = rest
		var e struct{ Action, Package, Test, Output string }
		if json.Unmarshal([]byte(line), &e) != nil {
			w.invalid = true
			continue
		}
		if e.Action == "output" {
			_, _ = w.diagnostic.Write([]byte(e.Output))
		}
		key := e.Package + "/" + e.Test
		if !w.expected[key] {
			continue
		}
		switch e.Action {
		case "run":
			w.ran[key] = true
		case "pass":
			if !w.ran[key] {
				w.invalid = true
			}
			w.passed[key] = true
		case "skip", "fail":
			w.invalid = true
		}
	}
	if len(w.pending) > 4<<20 {
		w.invalid = true
		w.pending = ""
	}
	return len(p), nil
}
func (w *baselineTestEvents) verify() error {
	if w.invalid || w.pending != "" {
		return fmt.Errorf("invalid, skipped, or failed accepted test evidence")
	}
	for key := range w.expected {
		if !w.ran[key] || !w.passed[key] {
			return fmt.Errorf("accepted test did not run and pass: %s", key)
		}
	}
	return nil
}
