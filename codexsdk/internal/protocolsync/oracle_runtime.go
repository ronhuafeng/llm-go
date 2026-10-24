package protocolsync

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/doc"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracle"
)

type oracleRequest struct {
	RepoRoot, ModuleRoot, CandidateDir string
	BaseRoot                           string
	Output                             io.Writer
	ForwardRuntime                     bool
}

func (r oracleRequest) output() io.Writer {
	if r.Output == nil {
		return io.Discard
	}
	return r.Output
}

type oracleContractError struct{ error }
type oracleIntegrityError struct{ error }

func (e *oracleContractError) Unwrap() error  { return e.error }
func (e *oracleIntegrityError) Unwrap() error { return e.error }

// runTargetOracle uses the same incremental checker as the goal-contract path.
func runTargetOracle(ctx context.Context, req oracleRequest, root string, sandbox oracleSandbox) error {
	spec, err := schemaOraclePublicRequest(filepath.Join(req.ModuleRoot, "internal/protocolschema/appserver/v2"), filepath.Join(req.CandidateDir, "schema"), filepath.Join(root, "codexsdk/protocolv2"), filepath.Join(req.CandidateDir, "common.rs"))
	if err != nil {
		return err
	}
	spec.Context = ctx
	reader, writer := io.Pipe()
	local, cancel := context.WithCancel(ctx)
	defer cancel()
	spec.Context = local
	generated := make(chan error, 1)
	go func() { err := schemaoracle.Write(spec, writer); writer.CloseWithError(err); generated <- err }()
	defer reader.Close()
	stream := bufio.NewReader(reader)
	var header schemaoracle.Header
	if err = schemaoracle.ReadFrame(stream, &header); err == nil {
		err = executeTargetOracle(local, req, root, sandbox, header, stream)
	}
	cancel()
	reader.Close()
	generationErr := <-generated
	if generationErr != nil && !errors.Is(generationErr, io.ErrClosedPipe) && !errors.Is(generationErr, context.Canceled) && !errors.Is(generationErr, context.DeadlineExceeded) {
		return &oracleContractError{generationErr}
	}
	if err != nil {
		return err
	}
	return generationErr
}

// Only the compiled decoder runs candidate runtime code. Expectations remain in
// the verifier, and every process is joined before disposing its sandbox.
func executeTargetOracle(ctx context.Context, req oracleRequest, root string, sandbox oracleSandbox, header schemaoracle.Header, expectations *bufio.Reader) error {
	req.Output = &serializedOracleOutput{writer: req.output()}
	source, err := schemaoracle.Harness(schemaoracle.Request{Bindings: header.Bindings, ImportPath: "github.com/ronhuafeng/llm-go/codexsdk/protocolv2"})
	if err != nil {
		return err
	}
	dir := filepath.Join(root, "codexsdk/internal/schemaoracleprobe")
	if err = os.Mkdir(dir, 0700); err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err = os.WriteFile(filepath.Join(dir, "main.go"), source, 0600); err != nil {
		return err
	}
	sandbox.readonly = append(sandbox.readonly, dir)
	before, err := repairPatch(root)
	if err != nil {
		return err
	}
	binary := filepath.Join(sandbox.work, "schema-decoder")
	cmd := sandbox.command(ctx, filepath.Join(sandbox.goroot, "bin/go"), "build", "-o", binary, "./internal/schemaoracleprobe")
	out, buildErr := oracleCommandOutput(req.output(), cmd)
	var checkErr error
	if buildErr != nil {
		checkErr = fmt.Errorf("compile schema decoder: %w: %s", buildErr, boundedRepairDiagnostic(string(out)))
	} else {
		local, cancel := context.WithCancel(ctx)
		defer cancel()
		sandbox.readonly = append(sandbox.readonly, binary)
		cmd = sandbox.command(local, binary)
		checkErr = checkDecoderProcess(local, cmd, header, expectations, req.output())
	}
	after, err := repairPatch(root)
	if err != nil {
		return err
	}
	if repairDigest(before) != repairDigest(after) {
		return &oracleIntegrityError{fmt.Errorf("independent probe execution changed candidate bytes")}
	}
	return checkErr
}

// The process transport is shared by production and deliberately defective
// native fixtures. A clean exit cannot replace complete observations.
func checkDecoderProcess(ctx context.Context, cmd *exec.Cmd, header schemaoracle.Header, expectations *bufio.Reader, output io.Writer) error {
	output = &serializedOracleOutput{writer: output}
	input, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	observed, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return err
	}
	var stderr repairOutput
	cmd.Stderr = io.MultiWriter(&stderr, output)
	if err = cmd.Start(); err != nil {
		input.Close()
		observed.Close()
		return err
	}
	err = schemaoracle.Check(ctx, header, expectations, input, observed, output)
	input.Close()
	observed.Close()
	if err != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	runErr := cmd.Wait()
	if err != nil || runErr != nil {
		return fmt.Errorf("independent schema contract: checking=%v process=%v: %s", err, runErr, boundedRepairDiagnostic(stderr.String()))
	}
	return nil
}

// runBaselineOracle exercises proposed handwritten implementation using the
// accepted schema and accepted public/runtime assertions. Generator internals
// may refactor: only its stable CLI and public output contract are required.
func runBaselineOracle(ctx context.Context, req oracleRequest, targetSandbox oracleSandbox) error {
	req.Output = &serializedOracleOutput{writer: req.output()}
	binary := filepath.Join(targetSandbox.work, "baseline-protocolv2gen")
	cmd := targetSandbox.command(ctx, filepath.Join(targetSandbox.goroot, "bin", "go"), "build", "-o", binary, "./internal/cmd/protocolv2gen")
	if out, err := oracleCommandOutput(req.Output, cmd); err != nil {
		return fmt.Errorf("build baseline generator: %w: %s", err, boundedRepairDiagnostic(string(out)))
	}
	dir, err := os.MkdirTemp("", "protocol-baseline-oracle-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	root := filepath.Join(dir, "repo")
	cmd = exec.CommandContext(ctx, "git", "clone", "--quiet", "--no-hardlinks", baselineRoot(req), root)
	if out, err := oracleCommandOutput(req.Output, cmd); err != nil {
		return fmt.Errorf("clone baseline regression: %w: %s", err, boundedRepairDiagnostic(string(out)))
	}
	packages := []string{".", "protocolv2", "internal/wirejson"}
	if req.ForwardRuntime {
		extra, e := forwardRuntimePackages(root)
		if e != nil {
			return e
		}
		packages = append(packages, extra...)
	}
	expected, err := selectedExecutableTests(filepath.Join(root, "codexsdk"), packages)
	if err != nil {
		return &oracleContractError{err}
	}
	if err = overlayGoalImplementation(root, req.RepoRoot); err != nil {
		return err
	}
	if req.ForwardRuntime {
		if err = overlayForwardRuntime(root, req.RepoRoot); err != nil {
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
	out, runErr := oracleCommandOutput(req.Output, cmd)
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
	args := []string{"test", "-count=1", "-json"}
	for _, p := range packages {
		args = append(args, "./"+filepath.ToSlash(p))
	}
	cmd = sandbox.command(ctx, filepath.Join(sandbox.goroot, "bin", "go"), args...)
	events := baselineTestEvents{expected: expected, ran: map[string]bool{}, passed: map[string]bool{}}
	cmd.Stdout = io.MultiWriter(&events, req.output())
	var stderr repairOutput
	cmd.Stderr = io.MultiWriter(&stderr, req.output())
	runErr = cmd.Run()
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
			packagePath = path.Join(packagePath, filepath.ToSlash(rel))
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
