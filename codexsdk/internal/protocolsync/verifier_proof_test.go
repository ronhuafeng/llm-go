package protocolsync

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const proofGoodPath = "codexsdk/internal/schemaoracle/testdata/verifier-proof/good.go"
const proofMutantPath = "codexsdk/internal/schemaoracle/testdata/verifier-proof/mutant.go"
const proofGood = `package verifierfixture;type Payload bool`
const proofMutant = `package verifierfixture;type Payload bool;func(Payload)MarshalJSON()([]byte,error){return []byte("false"),nil}`

func proofInputFixture(t *testing.T) verifierProofRequest {
	t.Helper()
	root := t.TempDir()
	candidate := t.TempDir()
	writeFile(t, filepath.Join(candidate, "schema/v2/Payload.json"), `{"type":"boolean"}`)
	writeFile(t, filepath.Join(root, proofGoodPath), proofGood)
	writeFile(t, filepath.Join(root, proofMutantPath), proofMutant)
	return verifierProofRequest{AcceptedRoot: root, CandidateDir: candidate, Cases: []verifierProofCase{{Name: "boolean-preservation", Source: "candidate", Schema: "v2/Payload.json", Binding: "Payload", Good: proofGoodPath, Counterexamples: []verifierProofCounterexample{{Mutant: proofMutantPath, Input: "true", Accept: true, Canonical: "true"}}}}}
}
func TestVerifierProofInputsFailClosed(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*verifierProofRequest)
	}{
		{"unknown source", func(r *verifierProofRequest) { r.Cases[0].Source = "author" }},
		{"path escape", func(r *verifierProofRequest) { r.Cases[0].Schema = "../Payload.json" }},
		{"no negative", func(r *verifierProofRequest) { r.Cases[0].Counterexamples = nil }},
		{"identical negative", func(r *verifierProofRequest) { r.Cases[0].Counterexamples[0].Mutant = proofGoodPath }},
		{"missing expected", func(r *verifierProofRequest) { r.Cases[0].Counterexamples[0].Canonical = "" }},
		{"author test package", func(r *verifierProofRequest) { r.Cases[0].Good = "package schemaoracleprobe" }},
		{"oversized fixture", func(r *verifierProofRequest) { r.Cases[0].Good = strings.Repeat(" ", 65537) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := proofInputFixture(t)
			tt.change(&r)
			if _, _, err := verifierProofInputs(r); err == nil {
				t.Fatal("accepted invalid proof input")
			}
		})
	}
	r := proofInputFixture(t)
	a, d, e := verifierProofInputs(r)
	if e != nil {
		t.Fatal(e)
	}
	if len(a) != 1 || d == "" {
		t.Fatal("missing source binding")
	}
	writeFile(t, filepath.Join(r.CandidateDir, "schema/v2/Payload.json"), `{"type":"string"}`)
	_, other, e := verifierProofInputs(r)
	if e != nil || other == d {
		t.Fatal("source bytes not bound")
	}
}
func TestVerifierProofRejectsSymlinkSource(t *testing.T) {
	root := t.TempDir()
	if e := os.Symlink("/etc/passwd", filepath.Join(root, "schema.json")); e != nil {
		t.Fatal(e)
	}
	if _, e := verifierProofReadSchema(root, "schema.json"); e == nil {
		t.Fatal("followed source symlink")
	}
}
func TestVerifierProofRecognizesOnlyConcreteCounterexample(t *testing.T) {
	if verifierProofCounterexampleFailure("schema-invalid input accepted: 10", []verifierProofCounterexample{{Input: "1"}}) {
		t.Fatal("matched JSON prefix rather than whole value")
	}
	x := []verifierProofCounterexample{{Input: `{"x":true}`}}
	for _, s := range []string{"FAIL", "panic: example", `schema-invalid input accepted: {"x":false}`, "compile error"} {
		if verifierProofCounterexampleFailure(s, x) {
			t.Fatal("unrelated failure counted", s)
		}
	}
	if !verifierProofCounterexampleFailure(`probe 1: wire value lost: input={"x":true} output={}`, x) {
		t.Fatal("lost concrete assertion")
	}
}

// Real compiler/test events exercise the fixed challenge, including a different
// source string that is behaviorally identical (a weak negative fixture).
// These are trusted test literals. Proposed fixtures use only oracleSandbox.
func TestVerifierProofFixedChallengeRejectsWeakNegative(t *testing.T) {
	x := verifierProofCounterexample{Input: "true", Accept: true, Canonical: "true"}
	for _, tt := range []struct {
		name, fixture string
		good, pass    bool
	}{
		{"correct", proofGood, true, true}, {"actual mutant", proofMutant, false, true},
		{"weak mutant", proofGood + "\n// Different bytes, same behavior.\n", false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "go.mod"), "module github.com/ronhuafeng/llm-go/codexsdk\ngo 1.26.8\n")
			writeFile(t, filepath.Join(dir, "internal/verifierfixture/fixture.go"), tt.fixture)
			writeFile(t, filepath.Join(dir, "internal/schemaoracleprobe/contract_test.go"), string(verifierChallengeSource("Payload", x, tt.good)))
			cmd := exec.Command("go", "test", "-count=1", "-json", "./internal/schemaoracleprobe")
			cmd.Dir = dir
			var events oracleTestEvents
			cmd.Stdout = &events
			var stderr repairOutput
			cmd.Stderr = &stderr
			e := cmd.Run()
			if !events.started || events.invalid || events.pending != "" {
				t.Fatalf("no trustworthy execution: %v %s", e, stderr.String())
			}
			if tt.pass {
				if e != nil || !events.passed || events.failed {
					t.Fatalf("good challenge proof failed: %v %s", e, events.diagnostic.String())
				}
			} else if e == nil || !events.failed || events.passed {
				t.Fatal("weak fixture was accepted")
			}
		})
	}
}

// The hosted lane executes both verifier versions and every fixture within the
// real OS boundary. This tiny verifier stub tests orchestration, not a protocol
// repair answer or evidence that a real proposed repair has been accepted.
func TestLiveVerifierProofMatrix(t *testing.T) {
	if os.Getenv("LLMGO_REPAIR_SANDBOX_TEST") != "1" {
		t.Skip("required hosted sandbox integration")
	}
	for _, erased := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "erased assertions"}[erased], func(t *testing.T) {
			r := proofInputFixture(t)
			writeFile(t, filepath.Join(r.AcceptedRoot, "go.mod"), "module github.com/ronhuafeng/llm-go\ngo 1.26.8\n")
			path := filepath.Join(r.AcceptedRoot, "codexsdk/internal/schemaoracle/oracle.go")
			old := proofStubVerifier(false, false)
			writeFile(t, path, old)
			for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture"}} {
				cmd := exec.Command("git", append([]string{"-C", r.AcceptedRoot}, args...)...)
				if out, e := cmd.CombinedOutput(); e != nil {
					t.Fatalf("git: %v %s", e, out)
				}
			}
			writeFile(t, path, proofStubVerifier(true, erased))
			cmd := exec.Command("git", "-C", r.AcceptedRoot, "diff", "--binary")
			var e error
			r.Patch, e = cmd.Output()
			if e != nil {
				t.Fatal(e)
			}
			writeFile(t, path, old)
			got, e := runVerifierProposalProof(context.Background(), r)
			if erased {
				if e == nil || got.Passed {
					t.Fatal("erased assertions accepted")
				}
			} else if e != nil || !got.Passed {
				t.Fatalf("matrix failed: %v %+v", e, got)
			}
		})
	}
}
func proofStubVerifier(updated, erased bool) string {
	test := `package schemaoracleprobe;import("testing";"encoding/json";subject "github.com/ronhuafeng/llm-go/codexsdk/internal/verifierfixture");func TestTrustedSchemaOracle(t *testing.T){var p subject.Payload;if e:=json.Unmarshal([]byte("true"),&p);e!=nil{t.Fatal(e)};b,e:=json.Marshal(p);if e!=nil{t.Fatal(e)};if string(b)!="true"{t.Fatalf("wire value lost: input=true output=%s",b)}}`
	if !updated {
		test = `package schemaoracleprobe;import "testing";func TestTrustedSchemaOracle(t *testing.T){t.Fatal("schema-valid input rejected: true: stale rule")}`
	}
	if erased {
		test = `package schemaoracleprobe;import "testing";func TestTrustedSchemaOracle(t *testing.T){}`
	}
	return "package schemaoracle\ntype Request struct{}\nfunc Generate(Request)([]byte,error){return []byte(" + strconv.Quote(test) + "),nil}\n"
}

func TestVerifierProofRejectsUnknownManifestControls(t *testing.T) {
	for _, data := range []string{`[]`, `[{"name":"x","command":"echo pass"}]`, `[{}] []`, strings.Repeat(" ", 1<<20+1)} {
		if _, e := decodeVerifierProofCases([]byte(data)); e == nil {
			t.Fatal("accepted unknown/malformed artifact")
		}
	}
}
func TestVerifierProofRequiresActualAssertionOutcome(t *testing.T) {
	for _, tt := range []struct {
		name, source string
		want         string
	}{
		{"child assertion", `package schemaoracleprobe;import "testing";func TestTrustedSchemaOracle(t *testing.T){t.Run("schema/branch",func(t *testing.T){t.Fatal("schema-valid input rejected: true: stale rule")})}`, "red"},
		{"assertion", `package schemaoracleprobe;import "testing";func TestTrustedSchemaOracle(t *testing.T){t.Fatal("schema-valid input rejected: true: stale rule")}`, "red"},
		{"compile", `package schemaoracleprobe;var _ = missingIdentifier`, "blocked"},
		{"panic", `package schemaoracleprobe;import "testing";func TestTrustedSchemaOracle(t *testing.T){panic("schema-valid input rejected: true")}`, "blocked"},
		{"skip", `package schemaoracleprobe;import "testing";func TestTrustedSchemaOracle(t *testing.T){t.Skip("schema-valid input rejected: true")}`, "blocked"},
		{"skipped child", `package schemaoracleprobe;import "testing";func TestTrustedSchemaOracle(t *testing.T){t.Run("erased contract",func(t *testing.T){t.Skip("omitted")})}`, "blocked"},
		{"skip with decoy", `package schemaoracleprobe;import "testing";func TestTrustedSchemaOracle(t *testing.T){t.Skip("skip")};func TestDecoy(t *testing.T){t.Fatal("schema-invalid input accepted: true")}`, "blocked"},
		{"silent fail with decoy", `package schemaoracleprobe;import "testing";func TestTrustedSchemaOracle(t *testing.T){t.Fail()};func TestDecoy(t *testing.T){t.Fatal("schema-invalid input accepted: true")}`, "unrelated"},
		{"fail with decoy only cause", `package schemaoracleprobe;import "testing";func TestTrustedSchemaOracle(t *testing.T){t.Fatal("unrelated")};func TestDecoy(t *testing.T){t.Fatal("schema-invalid input accepted: true")}`, "unrelated"},
		{"exit before test", `package schemaoracleprobe;import("testing";"os");func TestMain(*testing.M){os.Exit(0)};func TestTrustedSchemaOracle(t *testing.T){}`, "blocked"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "go.mod"), "module github.com/ronhuafeng/llm-go/codexsdk\ngo 1.26.8\n")
			writeFile(t, filepath.Join(dir, "internal/schemaoracleprobe/contract_test.go"), tt.source)
			cmd := exec.Command("go", "test", "-count=1", "-json", "./internal/schemaoracleprobe")
			cmd.Dir = dir
			var events verifierProofEvents
			cmd.Stdout = &events
			runErr := cmd.Run()
			got, e := verifierProofOutcome(events, runErr)
			if tt.want == "red" {
				if e != nil || !got.Failed || got.Passed {
					t.Fatalf("lost assertion: %v %+v", e, got)
				}
			} else if tt.want == "unrelated" {
				if e != nil || !got.Failed {
					t.Fatalf("missing actual target failure: %v", e)
				}
				if verifierProofCounterexampleFailure(got.Diagnostic, []verifierProofCounterexample{{Input: "true"}}) {
					t.Fatal("borrowed decoy assertion")
				}
			} else if e == nil {
				t.Fatalf("counted unknown outcome: %+v", got)
			}
		})
	}
}
func TestVerifierProofFixtureLoadRejectsEqualAndMalformed(t *testing.T) {
	r := proofInputFixture(t)
	writeFile(t, filepath.Join(r.AcceptedRoot, proofMutantPath), proofGood)
	if _, e := loadVerifierProofFixtures(r.AcceptedRoot, r.Cases); e == nil {
		t.Fatal("identical code at different path accepted")
	}
	writeFile(t, filepath.Join(r.AcceptedRoot, proofMutantPath), "package schemaoracleprobe")
	if _, e := loadVerifierProofFixtures(r.AcceptedRoot, r.Cases); e == nil {
		t.Fatal("test package fixture accepted")
	}
}
func TestVerifierProofCancellationStopsBeforeExecution(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, e := runVerifierProposalProof(ctx, verifierProofRequest{}); e != context.Canceled || got.Passed {
		t.Fatalf("cancelled proof: %+v %v", got, e)
	}
}

func TestVerifierProofRetainsAbnormalFlagBeyondDiagnosticBounds(t *testing.T) {
	var events verifierProofEvents
	emit := func(action, output string) {
		b, e := json.Marshal(map[string]string{"Action": action, "Package": "github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracleprobe", "Test": "TestTrustedSchemaOracle", "Output": output})
		if e != nil {
			t.Fatal(e)
		}
		events.Write(append(b, '\n'))
	}
	emit("run", "")
	emit("output", "panic: original crash\n")
	for range 20 {
		emit("output", strings.Repeat("late context ", 500)+"\n")
	}
	emit("output", "schema-valid input rejected: true: misleading late text\n")
	emit("fail", "")
	if _, e := verifierProofOutcome(events, errors.New("exit 1")); e == nil {
		t.Fatal("truncated panic became ordinary rejection")
	}
}
