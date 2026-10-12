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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	specification, err := schemaOraclePublicRequest(baseline, filepath.Join(req.CandidateDir, "schema"), filepath.Join(root, "codexsdk/protocolv2"), filepath.Join(req.CandidateDir, "common.rs"))
	if err != nil {
		return err
	}
	if len(specification.Bindings) == 0 {
		return nil
	} // no changed wire contracts; baseline regression still runs
	specification.Context = ctx
	targets, err := schemaoracle.Build(specification)
	if err != nil {
		return &oracleContractError{err}
	}
	source, err := schemaoracle.Render(specification, targets)
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
	capture := targetFeedbackCapture(ctx, targets)
	events.diagnostic.capture = capture
	cmd.Stdout = &events
	var stderr repairOutput
	cmd.Stderr = io.MultiWriter(&stderr, feedbackStream{capture, "stderr"})
	runErr := cmd.Run()
	capture.finish(runErr != nil || !events.started || !events.passed || events.failed || events.invalid || events.pending != "", events.pending)
	if events.pending != "" {
		_, _ = events.diagnostic.Write([]byte("incomplete go test output: " + events.pending))
	}
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

func targetFeedbackCapture(ctx context.Context, targets []schemaoracle.Target) *feedbackCapture {
	capture := newFeedbackCapture(ctx, "target_schema")
	if capture != nil {
		capture.probe = func(test string) any {
			prefix, indexText, ok := strings.Cut(test, "/probe_")
			if !ok {
				return nil
			}
			index, e := strconv.Atoi(indexText)
			if e != nil {
				return nil
			}
			for _, target := range targets {
				if prefix == "TestTrustedSchemaOracle/"+target.Schema && index >= 0 && index < len(target.Probes) {
					p := target.Probes[index]
					return struct {
						Schema     string                           `json:"schema"`
						Index      int                              `json:"index"`
						Valid      bool                             `json:"expected_valid"`
						Diagnostic string                           `json:"constraint"`
						Location   *schemaoracle.DiagnosticLocation `json:"location,omitempty"`
						Input      json.RawMessage                  `json:"input"`
					}{target.Schema, index, p.Valid, p.Diagnostic, p.Location, json.RawMessage(p.JSON)}
				}
			}
			return nil
		}
	}
	return capture
}

type oracleTestEvents struct {
	pending                          string
	started, passed, failed, invalid bool
	diagnostic                       oracleDiagnostic
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
		var e struct{ Action, Test, Package, ImportPath, Output string }
		if json.Unmarshal([]byte(line), &e) != nil {
			w.invalid = true
			if w.diagnostic.capture != nil {
				w.diagnostic.capture.markIncomplete()
			}
			_, _ = w.diagnostic.Write([]byte("unparsed go test output: " + line + "\n"))
			continue
		}
		// Modern go test -json interleaves BuildEvents with TestEvents.
		// Build diagnostics have ImportPath, not Package; retain them without
		// treating any build event as evidence that a test executed.
		diagnosticPackage := e.Package
		if e.ImportPath != "" && strings.HasPrefix(e.Action, "build-") {
			diagnosticPackage = e.ImportPath
		}
		w.diagnostic.event(e.Action, diagnosticPackage, e.Test, e.Output)
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
		if w.diagnostic.capture != nil {
			w.diagnostic.capture.markIncomplete()
		}
		_, _ = w.diagnostic.Write([]byte("go test event omitted: framing bound exceeded\n"))
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
	if out, err := feedbackCommandOutput(ctx, cmd); err != nil {
		return fmt.Errorf("build baseline generator: %w: %s", err, boundedRepairDiagnostic(string(out)))
	}
	dir, err := os.MkdirTemp("", "protocol-baseline-oracle-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	root := filepath.Join(dir, "repo")
	cmd = exec.CommandContext(ctx, "git", "clone", "--quiet", "--no-hardlinks", req.RepoRoot, root)
	if out, err := feedbackCommandOutput(ctx, cmd); err != nil {
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
	out, runErr := feedbackCommandOutput(ctx, cmd)
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
	capture := newFeedbackCapture(ctx, "baseline")
	events.diagnostic.capture = capture
	cmd.Stdout = &events
	var stderr repairOutput
	cmd.Stderr = io.MultiWriter(&stderr, feedbackStream{capture, "stderr"})
	runErr = cmd.Run()
	capture.finish(runErr != nil || events.verify() != nil, events.pending)
	if events.pending != "" {
		_, _ = events.diagnostic.Write([]byte("incomplete go test output: " + events.pending))
	}
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
	return selectedExecutableTests(module, []string{".", "protocolv2", "internal/wirejson"})
}

// Selection observes current executable names, not frozen historical assertions.
func selectedExecutableTests(module string, packages []string) (map[string]bool, error) {
	expected := map[string]bool{}
	for _, rel := range packages {
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
	diagnostic            oracleDiagnostic
}

func (w *baselineTestEvents) Write(p []byte) (int, error) {
	w.pending += string(p)
	for {
		line, rest, ok := strings.Cut(w.pending, "\n")
		if !ok {
			break
		}
		w.pending = rest
		var e struct{ Action, Package, Test, ImportPath, Output string }
		if json.Unmarshal([]byte(line), &e) != nil {
			w.invalid = true
			if w.diagnostic.capture != nil {
				w.diagnostic.capture.markIncomplete()
			}
			_, _ = w.diagnostic.Write([]byte("unparsed go test output: " + line + "\n"))
			continue
		}
		diagnosticPackage := e.Package
		if e.ImportPath != "" && strings.HasPrefix(e.Action, "build-") {
			diagnosticPackage = e.ImportPath
		}
		w.diagnostic.event(e.Action, diagnosticPackage, e.Test, e.Output)
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
		if w.diagnostic.capture != nil {
			w.diagnostic.capture.markIncomplete()
		}
		_, _ = w.diagnostic.Write([]byte("go test event omitted: framing bound exceeded\n"))
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
