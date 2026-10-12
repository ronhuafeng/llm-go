package protocolsync

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracle"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
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

func TestGoalContractRejectsSelfBaselineAndUnsafeArtifacts(t *testing.T) {
	root := t.TempDir()
	if err := VerifyGoalContracts(context.Background(), GoalContractRequest{RepoRoot: root, BaseRoot: root, CandidateDir: root}); err == nil {
		t.Fatal("self baseline accepted")
	}
	file := filepath.Join(root, "file")
	writeFile(t, file, "123")
	if _, err := readGoalContractArtifact(file, 2); err == nil {
		t.Fatal("oversized artifact accepted")
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readGoalContractArtifact(link, 10); err == nil {
		t.Fatal("symlink artifact accepted")
	}
}

// Uses only source-authored fixtures. Required hosted execution proves the
// candidate checker is the invoked checker, then an actual SDK assertion fails.
func TestLiveRepairSandboxGoalContractsUseCandidateChecker(t *testing.T) {
	if os.Getenv("LLMGO_REPAIR_SANDBOX_TEST") != "1" {
		t.Skip("required hosted sandbox integration")
	}
	base, candidate, upstream := t.TempDir(), t.TempDir(), t.TempDir()
	generated := "package protocolv2\ntype Payload struct { Old bool }\n"
	for _, root := range []string{base, candidate} {
		writeFile(t, filepath.Join(root, "go.mod"), "module github.com/ronhuafeng/llm-go\ngo 1.26.8\n")
		writeFile(t, filepath.Join(root, "codexsdk/runtime.go"), "package codexsdk\nfunc Stable() bool{return true}\n")
		writeFile(t, filepath.Join(root, "codexsdk/runtime_test.go"), "package codexsdk\nimport \"testing\"\nfunc TestAccepted(t *testing.T){if !Stable(){t.Fatal(\"baseline broken\")}}")
		writeFile(t, filepath.Join(root, "codexsdk/protocolv2/protocol_types.gen.go"), generated)
		writeFile(t, filepath.Join(root, "codexsdk/protocolv2/runtime.go"), "package protocolv2\nfunc Wire() string{return `{\"current\":true}`}\n")
		writeFile(t, filepath.Join(root, "codexsdk/protocolv2/runtime_test.go"), "package protocolv2\nimport \"testing\"\nfunc TestOld(t *testing.T){_ = Payload{Old:true}}")
		writeFile(t, filepath.Join(root, "codexsdk/internal/wirejson/fixture.go"), "package wirejson\n")
		writeFile(t, filepath.Join(root, "codexsdk/internal/cmd/protocolv2gen/main.go"), "package main\nimport \"os\"\nfunc main(){if e:=os.WriteFile(\"protocolv2/protocol_types.gen.go\",[]byte("+fmt.Sprintf("%q", generated)+"),0600);e!=nil{panic(e)}}")
		writeFile(t, filepath.Join(root, mechanicalPrefix, "version.json"), `{"version":"baseline"}`)
		runGitInit(t, root)
	}
	writeFile(t, filepath.Join(upstream, "schema/version.json"), `{"version":"target"}`)
	writeFile(t, filepath.Join(upstream, "common.rs"), "source")
	probe, checker := goalContractCheckerFixture()
	checkerPath := filepath.Join(candidate, "codexsdk/internal/protocolsync/fixture.go")
	writeFile(t, checkerPath, strings.Replace(checker, "if string(a)==string(b)", "if string(a)!=string(b)", 1))
	var diagnostics bytes.Buffer
	req := GoalContractRequest{RepoRoot: candidate, BaseRoot: base, CandidateDir: upstream, Output: &diagnostics}
	verify := func() error { diagnostics.Reset(); return VerifyGoalContracts(context.Background(), req) }
	if err := verify(); err == nil {
		t.Fatalf("incorrect candidate selector accepted: %s", diagnostics.String())
	}
	writeFile(t, checkerPath, strings.Replace(checker, fmt.Sprintf("%q", probe), `""`, 1))
	if err := verify(); err == nil || !strings.Contains(err.Error(), "coverage empty") {
		t.Fatalf("empty candidate selector bypassed target lane: %v\n%s", err, diagnostics.String())
	}
	writeFile(t, checkerPath, checker)
	if err := verify(); err != nil {
		t.Fatalf("candidate checker: %v\n%s", err, diagnostics.String())
	}
	writeFile(t, filepath.Join(candidate, "codexsdk/protocolv2/runtime.go"), "package protocolv2\nfunc Wire() string{return `{}`}\n")
	if err := verify(); err == nil || !strings.Contains(err.Error(), "missing target field") {
		t.Fatalf("missing field escaped assertion: %v\n%s", err, diagnostics.String())
	}
	writeFile(t, checkerPath, strings.Replace(checker, `return os.WriteFile(filepath.Join(out,"contract_test.go")`, `if len(failures)>0{return os.WriteFile(filepath.Join(out,"contract_test.go"),[]byte("changed source"),0600)};return os.WriteFile(filepath.Join(out,"contract_test.go")`, 1))
	if err := verify(); err == nil || !strings.Contains(err.Error(), "digest changed") {
		t.Fatalf("changed reconstruction accepted: %v\n%s", err, diagnostics.String())
	}
	writeFile(t, checkerPath, checker)
	writeFile(t, filepath.Join(candidate, "codexsdk/protocolv2/runtime.go"), "package protocolv2\nfunc Wire() string{return `{\"current\":true}`}\n")
	writeFile(t, filepath.Join(candidate, "codexsdk/runtime.go"), "package codexsdk\nfunc Stable() bool{return false}\n")
	if err := verify(); err == nil || !strings.Contains(err.Error(), "baseline broken") {
		t.Fatalf("baseline public assertion escaped: %v\n%s", err, diagnostics.String())
	}
}

func TestGoalContractFullCorpusTransportBudget(t *testing.T) {
	f, err := os.Open("../schemaoracle/testdata/rust-v0.162.1-full-target.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	var fixture struct {
		Schemas  map[string]json.RawMessage
		Bindings map[string]string
	}
	if err = json.NewDecoder(z).Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	spec := schemaoracle.Request{Context: context.Background(), Schemas: map[string][]byte{}, Bindings: fixture.Bindings, Package: "schemaoracleprobe", ImportPath: "github.com/ronhuafeng/llm-go/codexsdk/protocolv2", CloseObjects: true}
	for name, data := range fixture.Schemas {
		spec.Schemas[name] = data
	}
	dir := t.TempDir()
	if err = buildGoalContract(spec, dir); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 || files[0].Name() != "contract_test.go" {
		t.Fatalf("unexpected full-target transport: %v %v", files, err)
	}
	source, err := readGoalContractArtifact(filepath.Join(dir, "contract_test.go"), 12<<20)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.SplitN(strings.SplitN(string(source), "const frozenCorpus = ", 2)[1], "\n", 2)[0]
	encoded, err := strconv.Unquote(line)
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(compressed) > 8<<20 {
		t.Fatal("compressed budget changed")
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, (128<<20)+1))
	if err != nil || len(raw) > 128<<20 {
		t.Fatal("corpus budget", err)
	}
	var corpus struct{ Targets [][]json.RawMessage }
	if err = json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, v := range corpus.Targets {
		n += len(v)
	}
	if len(corpus.Targets) != 65 || n != 124376 {
		t.Fatalf("coverage changed: %d/%d", len(corpus.Targets), n)
	}
	t.Logf("full corpus: source=%d compressed=%d raw=%d probes=%d", len(source), len(compressed), len(raw), n)
}

func TestGoalContractFailedMetadataCompleteAndBound(t *testing.T) {
	location := &schemaoracle.DiagnosticLocation{Source: "full/source.json#/properties/value", Instance: "/value", Origin: "original", Attribution: "exact"}
	targets := []schemaoracle.Target{{Schema: "Record", Probes: []schemaoracle.Probe{{JSON: `{"value":"` + strings.Repeat("x", 8192) + `"}`, Valid: false, Diagnostic: "minLength exact", Location: location}}}}
	names := []string{"TestTrustedSchemaOracle/Record/probe_0", "TestTrustedSchemaOracle/Record", "TestTrustedSchemaOracle"}
	var encoded, output bytes.Buffer
	if err := writeGoalContractFailures(&encoded, targets, names); err != nil {
		t.Fatal(err)
	}
	if err := forwardGoalContractFailures(&output, encoded.Bytes(), names); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{strings.Repeat("x", 8192), location.Source, location.Instance, "minLength exact"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatal("metadata truncated", expected[:min(30, len(expected))])
		}
	}
	if err := writeGoalContractFailures(io.Discard, targets, []string{"TestTrustedSchemaOracle/Record/probe_99"}); err == nil {
		t.Fatal("unknown failure accepted")
	}
	if err := forwardGoalContractFailures(io.Discard, encoded.Bytes(), names[:1]); err == nil {
		t.Fatal("extra evidence accepted")
	}
	if err := forwardGoalContractFailures(io.Discard, encoded.Bytes()[:10], names); err == nil {
		t.Fatal("incomplete evidence accepted")
	}
	spec := schemaoracle.Request{Schemas: map[string][]byte{"R.json": []byte(`{"type":"boolean"}`)}, Bindings: map[string]string{"R.json": "R"}, Package: "probe"}
	if err := buildGoalContract(spec, t.TempDir(), "wrong-source-hash"); err == nil || !strings.Contains(err.Error(), "digest changed") {
		t.Fatalf("changed reconstruction accepted: %v", err)
	}
}

func TestGoalContractEmptySelectorRejectsRealPublicDrift(t *testing.T) {
	base, candidate, upstream := t.TempDir(), t.TempDir(), t.TempDir()
	old, next, generated := filepath.Join(base, mechanicalPrefix), filepath.Join(upstream, "schema"), filepath.Join(candidate, "codexsdk/protocolv2")
	before := `{"title":"ServerNotification","oneOf":[{"type":"object","properties":{"method":{"type":"string","enum":["notice"]},"params":{"type":"object","properties":{"old":{"type":"boolean"}},"required":["old"]}},"required":["method","params"]}]}`
	for _, view := range []struct{ root, raw string }{{old, before}, {next, strings.ReplaceAll(before, `"old"`, `"current"`)}} {
		writeFile(t, filepath.Join(view.root, "ServerNotification.json"), view.raw)
		for _, name := range []string{"ClientRequest", "ServerRequest", "ClientNotification"} {
			writeFile(t, filepath.Join(view.root, name+".json"), `{"title":"`+name+`","oneOf":[]}`)
		}
	}
	writeOracleAcceptedManifest(t, old, nil)
	for _, name := range []string{"baseline_metadata.json", "manifest.json"} {
		b, e := os.ReadFile(filepath.Join(old, name))
		if e != nil {
			t.Fatal(e)
		}
		writeFile(t, filepath.Join(old, name), strings.ReplaceAll(string(b), `"accepted"`, `"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"`))
	}
	metadata, err := os.ReadFile(filepath.Join(old, "baseline_metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(candidate, mechanicalPrefix, "baseline_metadata.json"), string(metadata))
	for _, root := range []string{base, candidate} {
		writeFile(t, filepath.Join(root, "codexsdk/protocolv2/fixture.go"), "package protocolv2")
	}
	common := filepath.Join(upstream, "common.rs")
	writeFile(t, common, "client_request_definitions! {\n}\nserver_request_definitions! {\n}\n")
	spec, err := schemaOraclePublicRequest(old, next, generated, common)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Bindings) == 0 {
		t.Fatal("fixture does not establish public drift")
	}
	output := t.TempDir()
	if err = buildGoalContract(spec, output); err != nil {
		t.Fatal(err)
	}
	req := GoalContractRequest{RepoRoot: candidate, BaseRoot: base, CandidateDir: upstream}
	source, err := readGoalContractArtifact(filepath.Join(output, "contract_test.go"), 12<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err = checkGoalContractCoverage(source, req); err != nil {
		t.Fatal(err)
	}
	spec.Bindings = nil
	if err = buildGoalContract(spec, output); err != nil {
		t.Fatal(err)
	}
	source, err = readGoalContractArtifact(filepath.Join(output, "contract_test.go"), 12<<20)
	if err != nil || len(source) != 0 {
		t.Fatalf("empty selector fixture: %v", err)
	}
	if err = checkGoalContractCoverage(source, req); err == nil || !strings.Contains(err.Error(), "schema or public protocol bytes changed") {
		t.Fatalf("empty selector accepted real public drift: %v", err)
	}
	// The exact same source, schema bytes, and complete protocol tree admit a
	// baseline-only check. SDK equality alone did not admit the previous drift.
	writeFile(t, filepath.Join(next, "ServerNotification.json"), before)
	if err = checkGoalContractCoverage(nil, req); err != nil {
		t.Fatalf("identical source falsely rejected: %v", err)
	}
	writeFile(t, filepath.Join(candidate, "codexsdk/protocolv2/fixture.go"), "package protocolv2\n// changed")
	if err = checkGoalContractCoverage(nil, req); err == nil {
		t.Fatal("changed protocol accepted")
	}
	writeFile(t, filepath.Join(candidate, "codexsdk/protocolv2/fixture.go"), "package protocolv2")
	writeFile(t, filepath.Join(candidate, mechanicalPrefix, "baseline_metadata.json"), `{"source_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
	if err = checkGoalContractCoverage(nil, req); err == nil {
		t.Fatal("different source with same schema accepted")
	}
}

func goalContractCheckerFixture() (string, string) {
	probe := `package schemaoracleprobe
import("testing";p "github.com/ronhuafeng/llm-go/codexsdk/protocolv2")
func TestTrustedSchemaOracle(t *testing.T){t.Run("Payload",func(t *testing.T){t.Run("probe_0",func(t *testing.T){if p.Wire()!=` + "`{\"current\":true}`" + `{t.Fatal("missing target field")}})})}`
	checker := `package protocolsync
import("context";"io";"os";"path/filepath";"fmt";"encoding/json")
type GoalContractRequest struct{RepoRoot,BaseRoot,CandidateDir string;Output io.Writer}
func GenerateGoalContract(_ context.Context,r GoalContractRequest,out string,failures ...string)error{
a,e:=os.ReadFile(filepath.Join(r.BaseRoot,"` + mechanicalPrefix + `/version.json"));if e!=nil{return e}; b,e:=os.ReadFile(filepath.Join(r.CandidateDir,"schema/version.json"));if e!=nil{return e};if string(a)==string(b){return fmt.Errorf("self comparison")}
if len(failures)>0{f,e:=os.Create(filepath.Join(out,"failure-metadata.jsonl"));if e!=nil{return e};defer f.Close();for _,name:=range failures[1:]{if e=json.NewEncoder(f).Encode(map[string]any{"test":name,"expectation":map[string]any{"kind":"schema_probe_expectation","input":map[string]any{"current":true}}});e!=nil{return e}}};return os.WriteFile(filepath.Join(out,"contract_test.go"),[]byte(` + fmt.Sprintf("%q", probe) + `),0600)}`
	return probe, checker
}

// Compile the exact hosted fixture locally, without executing candidate code or
// requiring namespace support. Compilation alone does not prove that the
// hosted empty-lane runtime guard executed.
func TestGoalContractCheckerFixtureCompiles(t *testing.T) {
	probe, checker := goalContractCheckerFixture()
	variants := []struct {
		name, source string
	}{
		{"normal", checker},
		{"empty", strings.Replace(checker, fmt.Sprintf("%q", probe), `""`, 1)},
		{"changed_reconstruction", strings.Replace(checker, `return os.WriteFile(filepath.Join(out,"contract_test.go")`, `if len(failures)>0{return os.WriteFile(filepath.Join(out,"contract_test.go"),[]byte("changed source"),0600)};return os.WriteFile(filepath.Join(out,"contract_test.go")`, 1)},
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, "go.mod"), "module fixture\ngo 1.26.8\n")
			writeFile(t, filepath.Join(root, "fixture.go"), v.source)
			cmd := exec.Command("go", "build", ".")
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("fixture compile: %v %s", err, out)
			}
		})
	}
}

func TestGoalContractFixedDriverCompiles(t *testing.T) {
	_, checker := goalContractCheckerFixture()
	for _, broken := range []bool{false, true} {
		t.Run(fmt.Sprintf("polluted_imports_%t", broken), func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, "go.mod"), "module github.com/ronhuafeng/llm-go\ngo 1.26.8\n")
			writeFile(t, filepath.Join(root, "codexsdk/internal/protocolsync/fixture.go"), checker)
			source := goalContractDriverSource
			if broken {
				source = strings.Replace(source, `"encoding/json"`, `"encoding/json";"encoding/hex";"io/fs";"reflect"`, 1)
			}
			writeFile(t, filepath.Join(root, "codexsdk/internal/goalcontractdriver/main.go"), source)
			runGitInit(t, root)
			cmd := exec.Command("go", "build", "-o", filepath.Join(root, "driver"), "./codexsdk/internal/goalcontractdriver")
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local")
			out, err := cmd.CombinedOutput()
			if broken {
				if err == nil || !strings.Contains(string(out), "imported and not used") {
					t.Fatalf("polluted driver did not reproduce compilation failure: %v %s", err, out)
				}
			} else if err != nil {
				t.Fatalf("actual runtime driver compile: %v %s", err, out)
			}
		})
	}
}
