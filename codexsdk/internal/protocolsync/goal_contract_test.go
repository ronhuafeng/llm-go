package protocolsync

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracle"
)

func TestGoalContractOverlayPreservesAcceptedFixtures(t *testing.T) {
	base, candidate := t.TempDir(), t.TempDir()
	for _, root := range []string{base, candidate} {
		writeFile(t, filepath.Join(root, "codexsdk/runtime.go"), "package codexsdk\n")
		writeFile(t, filepath.Join(root, "codexsdk/runtime_test.go"), root)
		writeFile(t, filepath.Join(root, mechanicalPrefix, "Thing.json"), root)
		writeFile(t, filepath.Join(root, "codexsdk/protocolv2/protocol_types.gen.go"), root)
		runGitInit(t, root)
	}
	writeFile(t, filepath.Join(candidate, "codexsdk/runtime.go"), "package codexsdk\n// changed\n")
	if err := overlayGoalImplementation(base, candidate); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"codexsdk/runtime_test.go", mechanicalPrefix + "/Thing.json", "codexsdk/protocolv2/protocol_types.gen.go"} {
		b, e := os.ReadFile(filepath.Join(base, name))
		if e != nil || string(b) != base {
			t.Fatalf("accepted input replaced: %s %v", name, e)
		}
	}
	data, _ := os.ReadFile(filepath.Join(base, "codexsdk/runtime.go"))
	if !strings.Contains(string(data), "changed") {
		t.Fatal("candidate implementation not applied")
	}
	if err := os.Remove(filepath.Join(candidate, "codexsdk/runtime.go")); err != nil {
		t.Fatal(err)
	}
	if err := overlayGoalImplementation(base, candidate); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "codexsdk/runtime.go")); !os.IsNotExist(err) {
		t.Fatal("candidate deletion lost")
	}
}

func TestGoalContractRejectsSelfBaseline(t *testing.T) {
	root := t.TempDir()
	if err := VerifyGoalContracts(context.Background(), GoalContractRequest{RepoRoot: root, BaseRoot: root, CandidateDir: root}); err == nil {
		t.Fatal("self baseline accepted")
	}
}

// Uses only source-authored fixtures. Required hosted execution proves the
// candidate checker is the invoked checker, then an actual SDK assertion fails.
func TestLiveRepairSandboxGoalContractsUseCandidateChecker(t *testing.T) {
	if os.Getenv("LLMGO_REPAIR_SANDBOX_TEST") != "1" {
		t.Skip("required hosted sandbox integration")
	}
	t.Run("targetless forward baselines", testForwardBaselinesSandbox)
	base, candidate, upstream := t.TempDir(), t.TempDir(), t.TempDir()
	generated := "package protocolv2\ntype Payload struct { Old bool }\n"
	for _, root := range []string{base, candidate} {
		writeFile(t, filepath.Join(root, "go.mod"), "module github.com/ronhuafeng/llm-go\ngo 1.26.8\n")
		writeFile(t, filepath.Join(root, "codexsdk/runtime.go"), "package codexsdk\nfunc Stable() bool{return true}\n")
		writeFile(t, filepath.Join(root, "codexsdk/runtime_test.go"), "package codexsdk\nimport \"testing\"\nfunc TestAccepted(t *testing.T){if !Stable(){t.Fatal(\"baseline broken\")}}")
		writeFile(t, filepath.Join(root, "codexsdk/protocolv2/protocol_types.gen.go"), generated)
		writeFile(t, filepath.Join(root, "codexsdk/protocolv2/runtime.go"), "package protocolv2\nfunc Wire() string{return `{\"current\":true}`}\n")
		writeFile(t, filepath.Join(root, "codexsdk/protocolv2/decode.go"), `package protocolv2
import("encoding/json";"fmt")
func(p *Payload)UnmarshalJSON(b []byte)error{var m map[string]any;if json.Unmarshal(b,&m)!=nil||len(m)!=1||m["current"]!=true{return fmt.Errorf("current required")};return nil}
func(p Payload)MarshalJSON()([]byte,error){return []byte(Wire()),nil}
`)
		writeFile(t, filepath.Join(root, "codexsdk/protocolv2/runtime_test.go"), "package protocolv2\nimport \"testing\"\nfunc TestOld(t *testing.T){_ = Payload{Old:true}}")
		writeFile(t, filepath.Join(root, "codexsdk/internal/wirejson/fixture.go"), "package wirejson\n")
		writeFile(t, filepath.Join(root, "codexsdk/internal/cmd/protocolv2gen/main.go"), "package main\nimport \"os\"\nfunc main(){if e:=os.WriteFile(\"protocolv2/protocol_types.gen.go\",[]byte("+fmt.Sprintf("%q", generated)+"),0600);e!=nil{panic(e)}}")
		writeFile(t, filepath.Join(root, mechanicalPrefix, "version.json"), `{"version":"baseline"}`)
		runGitInit(t, root)
	}
	writeFile(t, filepath.Join(upstream, "schema/version.json"), `{"version":"target"}`)
	writeFile(t, filepath.Join(upstream, "common.rs"), "source")
	_, checker := goalContractCheckerFixture()
	checkerPath := filepath.Join(candidate, "codexsdk/internal/protocolsync/fixture.go")
	writeFile(t, checkerPath, strings.Replace(checker, "if string(a)==string(b)", "if string(a)!=string(b)", 1))
	var diagnostics bytes.Buffer
	req := GoalContractRequest{RepoRoot: candidate, BaseRoot: base, CandidateDir: upstream, Output: &diagnostics}
	verify := func() error { diagnostics.Reset(); return VerifyGoalContracts(context.Background(), req) }
	if err := verify(); err == nil {
		t.Fatalf("incorrect candidate selector accepted: %s", diagnostics.String())
	}
	writeFile(t, checkerPath, strings.Replace(checker, `bindings:=map[string]string{"version.json":"Payload"}`, `bindings:=map[string]string{}`, 1))
	if err := verify(); err == nil || !strings.Contains(err.Error(), "coverage empty") {
		t.Fatalf("empty candidate selector bypassed target lane: %v\n%s", err, diagnostics.String())
	}
	writeFile(t, checkerPath, checker)
	if err := verify(); err != nil {
		t.Fatalf("candidate checker: %v\n%s", err, diagnostics.String())
	}
	// Rebinding must retain both B's original assertion and a new assertion
	// accepted on M. Candidate-owned test edits cannot remove either obligation.
	current := filepath.Join(t.TempDir(), "current")
	if _, err := goalSnapshot(context.Background(), base, current); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(current, "codexsdk/runtime_test.go")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(current, "codexsdk/main_test.go"), "package codexsdk\nimport \"testing\"\nfunc TestCurrentMain(t *testing.T){if !NewMain(){t.Fatal(\"current main broken\")}}")
	writeFile(t, filepath.Join(current, "codexsdk/main.go"), "package codexsdk\nfunc NewMain() bool{return true}\n")
	if err := runGit(current, "add", "."); err != nil {
		t.Fatal(err)
	}
	if err := runGit(current, "-c", "user.name=Protocol test", "-c", "user.email=protocol-test@example.invalid", "commit", "-m", "new main contract"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(candidate, "codexsdk/main.go"), "package codexsdk\nfunc NewMain() bool{return true}\n")
	rebound := func() error {
		log, err := newGoalDiagnostics(t.TempDir())
		if err != nil {
			return err
		}
		defer log.file.Close()
		return goalRunContracts(context.Background(), GoalVerifyRequest{RepoRoot: candidate, BaseRoot: current, OriginalBaseRoot: base, CandidateDir: upstream}, candidate, t.TempDir(), log)
	}
	if err := rebound(); err != nil {
		t.Fatalf("legitimate rebinding: %v", err)
	}
	writeFile(t, filepath.Join(candidate, "codexsdk/main.go"), "package codexsdk\nfunc NewMain() bool{return false}\n")
	if err := rebound(); err == nil || !strings.Contains(err.Error(), "current main broken") {
		t.Fatalf("new main regression escaped: %v", err)
	}
	writeFile(t, filepath.Join(candidate, "codexsdk/main.go"), "package codexsdk\nfunc NewMain() bool{return true}\n")
	writeFile(t, filepath.Join(candidate, "codexsdk/runtime.go"), "package codexsdk\nfunc Stable() bool{return false}\n")
	if err := rebound(); err == nil || !strings.Contains(err.Error(), "baseline broken") {
		t.Fatalf("original regression escaped rebinding: %v", err)
	}
	writeFile(t, filepath.Join(candidate, "codexsdk/runtime.go"), "package codexsdk\nfunc Stable() bool{return true}\n")
	writeFile(t, filepath.Join(candidate, "codexsdk/protocolv2/runtime.go"), "package protocolv2\nfunc Wire() string{return `{}`}\n")
	if err := verify(); err == nil || !strings.Contains(err.Error(), "wire value lost") {
		t.Fatalf("missing field escaped assertion: %v\n%s", err, diagnostics.String())
	}
	writeFile(t, checkerPath, checker)
	writeFile(t, filepath.Join(candidate, "codexsdk/protocolv2/runtime.go"), "package protocolv2\nfunc Wire() string{return `{\"current\":true}`}\n")
	writeFile(t, filepath.Join(candidate, "codexsdk/runtime.go"), "package codexsdk\nfunc Stable() bool{return false}\n")
	if err := verify(); err == nil || !strings.Contains(err.Error(), "baseline broken") {
		t.Fatalf("baseline public assertion escaped: %v\n%s", err, diagnostics.String())
	}
}

func TestGoalContractCoverageRequiresCurrentWorkAndExactSource(t *testing.T) {
	upstream := t.TempDir()
	raw := []byte(`{"type":"boolean"}`)
	writeFile(t, filepath.Join(upstream, "schema/Payload.json"), string(raw))
	req := GoalContractRequest{CandidateDir: upstream}
	header := schemaoracle.Header{Bindings: map[string]string{"Payload.json": "Payload"}, Sources: schemaoracle.SourceDigests(map[string][]byte{"Payload.json": raw})}
	if err := checkGoalContractCoverage(header, req); err != nil {
		t.Fatal(err)
	}
	empty := header
	empty.Bindings = nil
	if err := checkGoalContractCoverage(empty, req); err == nil {
		t.Fatal("same-schema implementation can skip current contracts")
	}
	writeFile(t, filepath.Join(upstream, "schema/Payload.json"), `{"type":"string"}`)
	if err := checkGoalContractCoverage(header, req); err == nil {
		t.Fatal("source digest drift accepted")
	}
}

func goalContractCheckerFixture() (string, string) {
	records := `{"Target":{"Schema":"version.json","GoType":"Payload"}}
{"Probe":{"JSON":"{\"current\":true}","Valid":true}}
{"Probe":{"JSON":"{}","Valid":false,"Diagnostic":"current field required"}}
{"Done":true,"Count":2}
`
	checker := `package protocolsync
import("context";"io";"os";"path/filepath";"fmt";"encoding/json";"crypto/sha256")
type GoalContractRequest struct{RepoRoot,BaseRoot,CandidateDir string;Output io.Writer}
func GenerateGoalContract(_ context.Context,r GoalContractRequest,out io.Writer)error{
a,e:=os.ReadFile(filepath.Join(r.BaseRoot,"` + mechanicalPrefix + `/version.json"));if e!=nil{return e};b,e:=os.ReadFile(filepath.Join(r.CandidateDir,"schema/version.json"));if e!=nil{return e};if string(a)==string(b){return fmt.Errorf("self comparison")}
bindings:=map[string]string{"version.json":"Payload"}
if e=json.NewEncoder(out).Encode(map[string]any{"Bindings":bindings,"Sources":map[string]string{"version.json":fmt.Sprintf("%x",sha256.Sum256(b))}});e!=nil{return e}
_,e=io.WriteString(out,` + fmt.Sprintf("%q", records) + `);return e}
`
	return records, checker
}

// Compile the exact production driver against the sandbox checker fixture.
func TestGoalContractFixedDriverCompiles(t *testing.T) {
	_, checker := goalContractCheckerFixture()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module github.com/ronhuafeng/llm-go\ngo 1.26.8\n")
	writeFile(t, filepath.Join(root, "codexsdk/internal/protocolsync/fixture.go"), checker)
	writeFile(t, filepath.Join(root, "codexsdk/internal/goalcontractdriver/main.go"), goalContractDriverSource)
	cmd := exec.Command("go", "build", "-o", filepath.Join(root, "driver"), "./codexsdk/internal/goalcontractdriver")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("production driver compile: %v %s", err, out)
	}
}
