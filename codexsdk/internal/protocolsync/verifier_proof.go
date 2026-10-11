package protocolsync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracle"
)

// These are proposal claims, not acceptance authority. A separate fresh review
// must bind their exact digest, source contract and observed execution evidence.
// No supplied command, replacement schema or author-authored test controls lanes.
type verifierProofRequest struct {
	AcceptedRoot, CandidateDir      string
	CandidateSHA256, ProposalSHA256 string // Verified and supplied by the outer controller.
	Patch                           []byte
	Cases                           []verifierProofCase
}
type verifierProofCase struct {
	Name            string                        `json:"name"`
	Source          string                        `json:"source"` // candidate or baseline
	Schema          string                        `json:"schema"`
	Fragment        string                        `json:"fragment,omitempty"`
	Binding         string                        `json:"binding"`
	Good            string                        `json:"good"` // Fixture path under schemaoracle/testdata/verifier-proof.
	Counterexamples []verifierProofCounterexample `json:"counterexamples"`
}
type verifierProofCounterexample struct {
	Mutant    string `json:"mutant"`
	Input     string `json:"input"`
	Accept    bool   `json:"accept"`
	Canonical string `json:"canonical,omitempty"`
}

// Artifact cases are data; unknown knobs or executable commands are rejected.
// Publication stores json.Marshal(cases), exactly the bytes bound by CasesDigest.
func decodeVerifierProofCases(data []byte) ([]verifierProofCase, error) {
	if len(data) == 0 || len(data) > 1<<20 {
		return nil, fmt.Errorf("verifier proof: invalid cases artifact size")
	}
	var cases []verifierProofCase
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if e := d.Decode(&cases); e != nil {
		return nil, e
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return nil, fmt.Errorf("verifier proof: trailing cases data")
	}
	if len(cases) == 0 || len(cases) > 8 {
		return nil, fmt.Errorf("verifier proof: require 1..8 cases")
	}
	return cases, nil
}

type verifierProofEvidence struct {
	Passed          bool                `json:"passed"`
	CandidateSHA256 string              `json:"candidate_sha256"`
	ProposalSHA256  string              `json:"proposal_sha256"`
	PatchDigest     string              `json:"verifier_patch_sha256"`
	CasesDigest     string              `json:"cases_sha256"`
	SourceDigest    string              `json:"source_sha256"`
	Lanes           []verifierProofLane `json:"lanes"`
}
type verifierProofLane struct {
	Case       string `json:"case"`
	Lane       string `json:"lane"`
	Diagnostic string `json:"diagnostic,omitempty"`
	Started    bool   `json:"started"`
	Passed     bool   `json:"passed"`
	Failed     bool   `json:"failed"`
}

const verifierFixtureImport = "github.com/ronhuafeng/llm-go/codexsdk/internal/verifierfixture"

// This local error grants only another already-budgeted author turn, never acceptance.
type verifierFeedbackError struct{ cause error }

func (e *verifierFeedbackError) Error() string { return e.cause.Error() }
func (e *verifierFeedbackError) Unwrap() error { return e.cause }
func verifierFeedback(e error) error           { return &verifierFeedbackError{cause: e} }

// runVerifierProposalProof deliberately does not publish, approve review, or
// replace the running oracle. All proposed verifier and fixture code executes
// inside the existing no-network sandbox. An unavailable boundary blocks proof.
func runVerifierProposalProof(ctx context.Context, req verifierProofRequest) (verifierProofEvidence, error) {
	evidence := verifierProofEvidence{PatchDigest: repairDigest(req.Patch), CandidateSHA256: req.CandidateSHA256, ProposalSHA256: req.ProposalSHA256}
	if err := ctx.Err(); err != nil {
		return evidence, err
	}
	if err := AssertClean(req.AcceptedRoot); err != nil {
		return evidence, err
	}
	specs, sourceDigest, err := verifierProofInputs(req)
	if err != nil {
		return evidence, err
	}
	cases, _ := json.Marshal(req.Cases)
	evidence.CasesDigest, evidence.SourceDigest = repairDigest(cases), sourceDigest
	if len(req.Patch) == 0 || len(req.Patch) > 4<<20 {
		return evidence, fmt.Errorf("verifier proof: empty or oversized patch")
	}
	temp, err := os.MkdirTemp("", "verifier-proof-")
	if err != nil {
		return evidence, err
	}
	defer os.RemoveAll(temp)
	roots := map[string]string{}
	for _, version := range []string{"old", "new"} {
		root := filepath.Join(temp, version, "repo")
		if err = os.MkdirAll(filepath.Dir(root), 0700); err != nil {
			return evidence, err
		}
		cmd := exec.CommandContext(ctx, "git", "clone", "--quiet", "--no-hardlinks", req.AcceptedRoot, root)
		if out, e := boundedCommandOutput(cmd); e != nil {
			return evidence, fmt.Errorf("clone verifier proof: %w: %s", e, boundedRepairDiagnostic(string(out)))
		}
		if version == "new" {
			if err = applyProposalPatchBytes(root, req.Patch); err != nil {
				return evidence, err
			}
			if err = verifierProofScope(root); err != nil {
				return evidence, err
			}
		}
		if err = os.MkdirAll(filepath.Join(root, "codexsdk/.cache/openai-codex"), 0700); err != nil {
			return evidence, err
		}
		roots[version] = root
	}
	loaded, err := loadVerifierProofFixtures(roots["new"], req.Cases)
	if err != nil {
		return evidence, err
	}
	frozenFixtures, _ := json.Marshal(loaded)
	checkFrozen := func() error {
		_, current, e := verifierProofInputs(req)
		if e != nil {
			return e
		}
		if current != sourceDigest {
			return fmt.Errorf("verifier proof: immutable source changed")
		}
		fixtures, e := loadVerifierProofFixtures(roots["new"], req.Cases)
		if e != nil {
			return e
		}
		b, _ := json.Marshal(fixtures)
		if repairDigest(b) != repairDigest(frozenFixtures) {
			return fmt.Errorf("verifier proof: fixture changed")
		}
		return nil
	}
	for i, c := range loaded {
		generated := map[string][]byte{}
		for _, version := range []string{"old", "new"} {
			root := roots[version]
			sandbox, e := newVerifierProofSandbox(ctx, root, req.CandidateDir)
			if e != nil {
				return evidence, e
			}
			data, e := verifierProofGenerate(ctx, sandbox, specs[i])
			if frozenErr := checkFrozen(); frozenErr != nil {
				return evidence, frozenErr
			}
			if e != nil {
				if version == "new" {
					return evidence, verifierFeedback(fmt.Errorf("%s/%s generation blocked: %w", c.Name, version, e))
				}
				return evidence, fmt.Errorf("%s/%s generation blocked: %w", c.Name, version, e)
			}
			generated[version] = data
		}
		// Challenges are executed by a fixed test, separate from Generate's output.
		// This catches identical/vacuous mutants before counting their oracle result.
		for n, x := range c.Counterexamples {
			for _, fixture := range []struct {
				name, code string
				good       bool
			}{{"good", c.Good, true}, {"mutant", x.Mutant, false}} {
				lane := fmt.Sprintf("challenge-%d-%s", n, fixture.name)
				result, e := verifierProofExecute(ctx, roots["new"], req.CandidateDir, fixture.code, verifierChallengeSource(c.Binding, x, fixture.good))
				if frozenErr := checkFrozen(); frozenErr != nil {
					return evidence, frozenErr
				}
				result.Case, result.Lane = c.Name, lane
				evidence.Lanes = append(evidence.Lanes, result)
				if e != nil || !result.Passed {
					if e != nil {
						return evidence, e
					}
					return evidence, verifierFeedback(fmt.Errorf("%s/%s: challenge not established: %s", c.Name, lane, result.Diagnostic))
				}
			}
		}
		for _, version := range []string{"old", "new"} {
			result, e := verifierProofExecute(ctx, roots[version], req.CandidateDir, c.Good, generated[version])
			if frozenErr := checkFrozen(); frozenErr != nil {
				return evidence, frozenErr
			}
			result.Case, result.Lane = c.Name, version+"-good"
			evidence.Lanes = append(evidence.Lanes, result)
			if e != nil {
				return evidence, e
			}
			if version == "new" && !result.Passed {
				return evidence, verifierFeedback(fmt.Errorf("%s: proposed verifier rejected good fixture: %s", c.Name, result.Diagnostic))
			}
			if version == "old" && (!result.Failed || !verifierProofCounterexampleFailure(result.Diagnostic, c.Counterexamples)) {
				return evidence, verifierFeedback(fmt.Errorf("%s: old verifier false rejection not established: %s", c.Name, result.Diagnostic))
			}
		}
		for n, x := range c.Counterexamples {
			result, e := verifierProofExecute(ctx, roots["new"], req.CandidateDir, x.Mutant, generated["new"])
			if frozenErr := checkFrozen(); frozenErr != nil {
				return evidence, frozenErr
			}
			result.Case, result.Lane = c.Name, fmt.Sprintf("new-mutant-%d", n)
			evidence.Lanes = append(evidence.Lanes, result)
			if e != nil {
				return evidence, e
			}
			if !result.Failed || !verifierProofCounterexampleFailure(result.Diagnostic, []verifierProofCounterexample{x}) {
				return evidence, verifierFeedback(fmt.Errorf("%s: mutant rejection not established: %s", c.Name, result.Diagnostic))
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return evidence, err
	}
	evidence.Passed = true
	return evidence, nil
}

func verifierProofInputs(req verifierProofRequest) ([]schemaoracle.Request, string, error) {
	if len(req.Cases) == 0 || len(req.Cases) > 8 {
		return nil, "", fmt.Errorf("verifier proof: require 1..8 cases")
	}
	var specs []schemaoracle.Request
	var sources bytes.Buffer
	seen := map[string]bool{}
	for _, c := range req.Cases {
		if c.Name == "" || len(c.Name) > 80 || seen[c.Name] {
			return nil, "", fmt.Errorf("verifier proof: invalid or duplicate case name")
		}
		seen[c.Name] = true
		if !token.IsIdentifier(c.Binding) || token.Lookup(c.Binding).IsKeyword() {
			return nil, "", fmt.Errorf("verifier proof: invalid binding")
		}
		if c.Fragment != "" && !strings.HasPrefix(c.Fragment, "#/definitions/") && !strings.HasPrefix(c.Fragment, "#/$defs/") {
			return nil, "", fmt.Errorf("verifier proof: unsupported fragment")
		}
		base := ""
		switch c.Source {
		case "candidate":
			base = filepath.Join(req.CandidateDir, "schema")
		case "baseline":
			base = filepath.Join(req.AcceptedRoot, mechanicalPrefix)
		default:
			return nil, "", fmt.Errorf("verifier proof: unknown source")
		}
		raw, e := verifierProofReadSchema(base, c.Schema)
		if e != nil {
			return nil, "", e
		}
		fmt.Fprintf(&sources, "%s\x00%s\x00%s\x00", c.Source, c.Schema, repairDigest(raw))
		if e = verifierProofFixturePath(c.Good); e != nil {
			return nil, "", e
		}
		if len(c.Counterexamples) == 0 || len(c.Counterexamples) > 8 {
			return nil, "", fmt.Errorf("verifier proof: require 1..8 counterexamples")
		}
		for _, x := range c.Counterexamples {
			if e = verifierProofFixturePath(x.Mutant); e != nil {
				return nil, "", e
			}
			if x.Mutant == c.Good {
				return nil, "", fmt.Errorf("verifier proof: identical mutant")
			}
			if len(x.Input) > 2048 || !json.Valid([]byte(x.Input)) {
				return nil, "", fmt.Errorf("verifier proof: invalid challenge JSON")
			}
			if x.Accept && (len(x.Canonical) > 2048 || !json.Valid([]byte(x.Canonical))) {
				return nil, "", fmt.Errorf("verifier proof: invalid canonical JSON")
			}
			if !x.Accept && x.Canonical != "" {
				return nil, "", fmt.Errorf("verifier proof: rejected challenge has canonical output")
			}
		}
		specs = append(specs, schemaoracle.Request{Schemas: map[string][]byte{c.Schema: raw}, Bindings: map[string]string{c.Schema + c.Fragment: c.Binding}, Package: "schemaoracleprobe", ImportPath: verifierFixtureImport, CloseObjects: true})
	}
	return specs, repairDigest(sources.Bytes()), nil
}
func verifierProofReadSchema(base, name string) ([]byte, error) {
	if name == "" || strings.Contains(name, "\\") || filepath.IsAbs(name) || filepath.ToSlash(filepath.Clean(name)) != name || strings.HasPrefix(name, "../") || !strings.HasSuffix(name, ".json") {
		return nil, fmt.Errorf("verifier proof: invalid source path")
	}
	at := base
	for _, part := range strings.Split(name, "/") {
		at = filepath.Join(at, part)
		info, e := os.Lstat(at)
		if e != nil {
			return nil, e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("verifier proof: symlink source")
		}
	}
	f, e := os.Open(at)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, (16<<20)+1))
	if e != nil {
		return nil, e
	}
	if len(b) > 16<<20 {
		return nil, fmt.Errorf("verifier proof: source too large")
	}
	return b, nil
}
func verifierProofFixturePath(name string) error {
	if !strings.HasPrefix(name, "codexsdk/internal/schemaoracle/testdata/verifier-proof/") || !isVerifierProposalPath(name) || strings.HasSuffix(name, "_test.go") {
		return fmt.Errorf("verifier proof: invalid fixture path")
	}
	return nil
}
func loadVerifierProofFixtures(root string, cases []verifierProofCase) ([]verifierProofCase, error) {
	result := make([]verifierProofCase, len(cases))
	read := func(name string) (string, error) {
		if e := verifierProofFixturePath(name); e != nil {
			return "", e
		}
		at := root
		for _, p := range strings.Split(name, "/") {
			at = filepath.Join(at, p)
			info, e := os.Lstat(at)
			if e != nil {
				return "", e
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("verifier proof: symlink fixture")
			}
		}
		f, e := os.Open(at)
		if e != nil {
			return "", e
		}
		defer f.Close()
		b, e := io.ReadAll(io.LimitReader(f, (64<<10)+1))
		if e != nil {
			return "", e
		}
		if e = verifierProofFixture(string(b)); e != nil {
			return "", e
		}
		return string(b), nil
	}
	for i, c := range cases {
		result[i] = c
		var e error
		result[i].Good, e = read(c.Good)
		if e != nil {
			return nil, e
		}
		result[i].Counterexamples = append([]verifierProofCounterexample(nil), c.Counterexamples...)
		for j, x := range c.Counterexamples {
			result[i].Counterexamples[j].Mutant, e = read(x.Mutant)
			if e != nil {
				return nil, e
			}
			if result[i].Counterexamples[j].Mutant == result[i].Good {
				return nil, fmt.Errorf("verifier proof: identical mutant source")
			}
		}
	}
	return result, nil
}
func verifierProofFixture(source string) error {
	if len(source) == 0 || len(source) > 64<<10 {
		return fmt.Errorf("verifier proof: invalid fixture size")
	}
	f, e := parser.ParseFile(token.NewFileSet(), "fixture.go", source, 0)
	if e != nil {
		return e
	}
	if f.Name.Name != "verifierfixture" {
		return fmt.Errorf("verifier proof: fixture package must be verifierfixture")
	}
	return nil
}
func verifierProofScope(root string) error {
	paths, e := ChangedPaths(root)
	if e != nil {
		return e
	}
	if len(paths) == 0 {
		return fmt.Errorf("verifier proof: no changed verifier")
	}
	for _, p := range paths {
		if !isVerifierProposalPath(p) {
			return fmt.Errorf("verifier proof: forbidden patch path %s", p)
		}
		info, e := os.Lstat(filepath.Join(root, p))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("verifier proof: changed path must remain regular: %s", p)
		}
	}
	return nil
}
func verifierProofWrite(root, path string, data []byte) error {
	full := filepath.Join(root, "codexsdk", path)
	if e := os.MkdirAll(filepath.Dir(full), 0700); e != nil {
		return e
	}
	return os.WriteFile(full, data, 0600)
}

// Each untrusted process gets a fresh writable cache; a preceding fixture or
// generator must not poison a later lane's compiler cache or generated output.
func newVerifierProofSandbox(ctx context.Context, root, candidate string) (oracleSandbox, error) {
	s, e := newOracleSandbox(ctx, root, candidate)
	if e != nil {
		return s, e
	}
	s.work, e = os.MkdirTemp(filepath.Dir(root), "proof-work-")
	if e != nil {
		return s, e
	}
	for _, p := range []string{"cache", "home"} {
		if e = os.Mkdir(filepath.Join(s.work, p), 0700); e != nil {
			os.RemoveAll(s.work)
			return s, e
		}
	}
	return s, nil
}
func verifierProofGenerate(ctx context.Context, s oracleSandbox, req schemaoracle.Request) ([]byte, error) {
	defer os.RemoveAll(s.work)
	request, e := json.Marshal(req)
	if e != nil {
		return nil, e
	}
	output := filepath.Join(s.work, "generated.go")
	driver := `package main
import("encoding/json";"os";"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracle")
func main(){var r schemaoracle.Request;if e:=json.Unmarshal([]byte(` + strconv.Quote(string(request)) + `),&r);e!=nil{panic(e)};b,e:=schemaoracle.Generate(r);if e!=nil{panic(e)};if e=os.WriteFile(` + strconv.Quote(output) + `,b,0600);e!=nil{panic(e)}}`
	if e = verifierProofWrite(s.root, "internal/verifierproofdriver/main.go", []byte(driver)); e != nil {
		return nil, e
	}
	s.readonly = append(s.readonly, s.root)
	cmd := s.command(ctx, filepath.Join(s.goroot, "bin", "go"), "run", "./internal/verifierproofdriver")
	before, e := repairPatch(s.root)
	if e != nil {
		return nil, e
	}
	out, runErr := boundedCommandOutput(cmd)
	after, e := repairPatch(s.root)
	if e != nil {
		return nil, e
	}
	if repairDigest(before) != repairDigest(after) {
		return nil, fmt.Errorf("verifier proof: generation changed frozen inputs")
	}
	if runErr != nil {
		return nil, fmt.Errorf("generate: %w: %s", runErr, boundedRepairDiagnostic(string(out)))
	}
	info, e := os.Lstat(output)
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("verifier proof: generated output is not regular")
	}
	f, e := os.Open(output)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, (12<<20)+1))
	if e != nil {
		return nil, e
	}
	if len(b) == 0 || len(b) > 12<<20 {
		return nil, fmt.Errorf("generated proof empty or oversized")
	}
	return b, nil
}
func verifierProofExecute(ctx context.Context, root, candidate, fixture string, source []byte) (verifierProofLane, error) {
	var result verifierProofLane
	for path, data := range map[string][]byte{"internal/verifierfixture/fixture.go": []byte(fixture), "internal/schemaoracleprobe/contract_test.go": source} {
		if e := verifierProofWrite(root, path, data); e != nil {
			return result, e
		}
	}
	s, e := newVerifierProofSandbox(ctx, root, candidate)
	if e != nil {
		return result, e
	}
	defer os.RemoveAll(s.work)
	s.readonly = append(s.readonly, root)
	cmd := s.command(ctx, filepath.Join(s.goroot, "bin", "go"), "test", "-count=1", "-json", "-run", "^TestTrustedSchemaOracle$", "./internal/schemaoracleprobe")
	var events verifierProofEvents
	var stderr repairOutput
	cmd.Stdout = &events
	cmd.Stderr = &stderr
	before, e := repairPatch(root)
	if e != nil {
		return result, e
	}
	runErr := cmd.Run()
	after, e := repairPatch(root)
	if e != nil {
		return result, e
	}
	if repairDigest(before) != repairDigest(after) {
		return result, fmt.Errorf("verifier proof: execution changed frozen inputs")
	}
	_, _ = events.all.diagnostic.Write([]byte(stderr.String()))
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	return verifierProofOutcome(events, runErr)
}

// Keep proof status and assertion ownership separate from human-facing package
// diagnostics. A skipped target cannot borrow another test's failing assertion.
type verifierProofEvents struct {
	all                             oracleTestEvents
	targetDiagnostic                oracleDiagnostic
	targetFailed, skipped, abnormal bool
	buildFailed                     bool
}

func (w *verifierProofEvents) Write(p []byte) (int, error) {
	framed := w.all.pending + string(p)
	n, err := w.all.Write(p)
	for {
		line, rest, ok := strings.Cut(framed, "\n")
		if !ok {
			break
		}
		framed = rest
		var e struct{ Action, Test, Package, Output string }
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		if e.Action == "build-fail" {
			w.buildFailed = true
		}
		if e.Action == "build-fail" || strings.Contains(e.Output, "panic:") || strings.Contains(e.Output, "[setup failed]") {
			w.abnormal = true
		}
		if e.Package != "github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracleprobe" {
			continue
		}
		if e.Test == "TestTrustedSchemaOracle" {
			if e.Action == "fail" {
				w.targetFailed = true
			}
			if e.Action == "skip" {
				w.skipped = true
			}
		}
		if e.Test == "TestTrustedSchemaOracle" || strings.HasPrefix(e.Test, "TestTrustedSchemaOracle/") {
			if e.Action == "skip" {
				w.skipped = true
			}
			w.targetDiagnostic.event(e.Action, e.Package, e.Test, e.Output)
		}
	}
	return n, err
}
func verifierProofOutcome(events verifierProofEvents, runErr error) (verifierProofLane, error) {
	var result verifierProofLane
	result.Started, result.Passed, result.Failed = events.all.started, events.all.passed, events.targetFailed
	result.Diagnostic = boundedRepairDiagnostic(events.targetDiagnostic.String())
	if result.Diagnostic == "" && !events.targetFailed {
		result.Diagnostic = boundedRepairDiagnostic(events.all.diagnostic.String())
	}
	if events.buildFailed && !events.all.invalid && events.all.pending == "" {
		return result, verifierFeedback(fmt.Errorf("verifier fixture compile failed: %s", result.Diagnostic))
	}
	if events.all.invalid || events.all.pending != "" || !events.all.started || events.skipped || events.abnormal || events.all.passed == events.targetFailed {
		return result, fmt.Errorf("verifier proof: missing/unrecognized test outcome: %v: %s", runErr, result.Diagnostic)
	}
	if events.all.passed && runErr != nil {
		return result, fmt.Errorf("verifier proof: process failed after test pass: %v", runErr)
	}
	if events.targetFailed && runErr == nil {
		return result, fmt.Errorf("verifier proof: abnormal test failure: %v: %s", runErr, result.Diagnostic)
	}
	return result, nil
}
func verifierProofCounterexampleFailure(diagnostic string, examples []verifierProofCounterexample) bool {
	for _, x := range examples {
		var value any
		d := json.NewDecoder(strings.NewReader(x.Input))
		d.UseNumber()
		if d.Decode(&value) != nil {
			continue
		}
		compact, _ := json.Marshal(value)
		for _, kind := range []string{"schema-invalid input accepted: ", "schema-valid input rejected: ", "wire value lost: input="} {
			if _, tail, found := strings.Cut(diagnostic, kind+string(compact)); found && (tail == "" || strings.ContainsRune(": \n\r", rune(tail[0]))) {
				return true
			}
		}
	}
	return false
}

// Both good and mutant must be genuinely exercised on the exact same declared
// challenge. Mutant proof passes only when its observed behavior violates it.
func verifierChallengeSource(binding string, x verifierProofCounterexample, good bool) []byte {
	return []byte(`package schemaoracleprobe
import("encoding/json";"bytes";"reflect";"math/big";"testing";subject ` + strconv.Quote(verifierFixtureImport) + `)
func TestTrustedSchemaOracle(t *testing.T){var target subject.` + binding + `;err:=json.Unmarshal([]byte(` + strconv.Quote(x.Input) + `),&target);matches:=err==nil
if !` + strconv.FormatBool(x.Accept) + ` {matches=err!=nil} else if matches {actual,e:=json.Marshal(target);if e!=nil{matches=false}else{decode:=func(b []byte)any{var v any;d:=json.NewDecoder(bytes.NewReader(b));d.UseNumber();if e:=d.Decode(&v);e!=nil{t.Fatal(e)};return v};matches=proofEqual(decode(actual),decode([]byte(` + strconv.Quote(x.Canonical) + `)))}}
if matches!=` + strconv.FormatBool(good) + `{t.Fatal("fixed challenge: fixture did not demonstrate its claimed contract behavior")}}
func proofEqual(a,b any)bool{switch x:=a.(type){case json.Number:y,ok:=b.(json.Number);if !ok{return false};r,ok:=new(big.Rat).SetString(string(x));if !ok{return false};s,ok:=new(big.Rat).SetString(string(y));return ok&&r.Cmp(s)==0;case map[string]any:y,ok:=b.(map[string]any);if !ok||len(x)!=len(y){return false};for k,v:=range x{other,exists:=y[k];if !exists||!proofEqual(v,other){return false}};return true;case []any:y,ok:=b.([]any);if !ok||len(x)!=len(y){return false};for i,v:=range x{if !proofEqual(v,y[i]){return false}};return true;default:return reflect.DeepEqual(a,b)}}
`)
}
