package protocolsync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOracleSandboxPolicyBindsOnlyDisposableWrites(t *testing.T) {
	root := t.TempDir()
	s := oracleSandbox{root: root, candidate: "/tmp/candidate", work: "/tmp/private-work", goroot: "/opt/go", modules: "/tmp/modules"}
	cmd := s.command(context.Background(), "/bin/true")
	text := strings.Join(cmd.Args, " ")
	for _, want := range []string{"--unshare-all", "--die-with-parent", "--new-session", "--proc /proc", "--tmpfs /tmp", "--tmpfs /home", "--ro-bind " + filepath.Join(root, ".git"), "--ro-bind /tmp/candidate", "--ro-bind /tmp/modules"} {
		if !strings.Contains(text, want) {
			t.Fatal("missing sandbox boundary:", want)
		}
	}
	if _, err := os.Stat("/root"); err == nil && !strings.Contains(text, "--tmpfs /root") {
		t.Fatal("readable host root must be masked")
	}
	for _, e := range cmd.Env {
		if strings.HasPrefix(e, "GH_TOKEN=") || strings.HasPrefix(e, "AZURE_OPENAI_API_KEY=") {
			t.Fatal("credential entered sandbox")
		}
	}
}

// Required by the source CI job. Local environments may lack user namespaces;
// they must report this unexecuted proof, never silently run without isolation.
func TestLiveRepairSandboxRejectsExternalAndGitWrites(t *testing.T) {
	if os.Getenv("LLMGO_REPAIR_SANDBOX_TEST") != "1" {
		t.Skip("explicit Linux sandbox integration runs in required source CI")
	}
	root := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	source := filepath.Join(root, "codexsdk/.cache/openai-codex")
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	candidate := t.TempDir()
	writeFile(t, filepath.Join(candidate, "fixture"), "immutable")
	s, err := newOracleSandbox(context.Background(), root, candidate)
	if err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(root, "codexsdk/trusted-probe.txt")
	writeFile(t, probe, "trusted")
	s.readonly = append(s.readonly, probe)
	script := `set -eu
! printf tamper > "$4"
! printf tamper > "$1/.git/HEAD"
! printf tamper > "$2/fixture"
! test -e "$3"
printf safe > "$1/codexsdk/within-workspace"
`
	secret := filepath.Join(t.TempDir(), "parent-secret")
	writeFile(t, secret, "not-visible")
	cmd := s.command(context.Background(), "/bin/sh", "-c", script, "fixture", root, candidate, secret, probe)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sandbox contract: %v: %s", err, out)
	}
	if data, err := os.ReadFile(filepath.Join(candidate, "fixture")); err != nil || string(data) != "immutable" {
		t.Fatal("candidate modified")
	}
}

// The real OS boundary runs the new contract, including the old false-rejection
// case and a candidate-controlled TestMain that must not decide acceptance.
func TestLiveRepairSandboxSchemaEvolution(t *testing.T) {
	if os.Getenv("LLMGO_REPAIR_SANDBOX_TEST") != "1" {
		t.Skip("required hosted sandbox integration")
	}
	root := t.TempDir()
	module := filepath.Join(root, "codexsdk")
	writeFile(t, filepath.Join(root, "go.mod"), "module github.com/ronhuafeng/llm-go\ngo 1.26.8\n")
	writeFile(t, filepath.Join(module, "internal/protocolschema/appserver/v2/v2/Payload.json"), `{"title":"Payload","type":"object","properties":{"retired":{"type":"boolean"}},"required":["retired"]}`)
	subject := filepath.Join(module, "protocolv2/fixture.go")
	good := `package protocolv2
import("bytes";"encoding/json";"fmt")
type Payload struct { Current bool ` + "`json:\"current\"`" + ` }
func(p *Payload) UnmarshalJSON(b []byte) error { var m map[string]json.RawMessage;if e:=json.Unmarshal(b,&m);e!=nil{return e};if len(m)!=1||m["current"]==nil||bytes.Equal(m["current"],[]byte("null")){return fmt.Errorf("invalid")};type plain Payload;return json.Unmarshal(b,(*plain)(p)) }
`
	notification := `
type ServerNotification struct { Method string ` + "`json:\"method\"`" + `; Params Payload ` + "`json:\"params\"`" + ` }
func(p *ServerNotification) UnmarshalJSON(b []byte) error { var m map[string]json.RawMessage;if e:=json.Unmarshal(b,&m);e!=nil{return e};if len(m)!=2||m["method"]==nil||m["params"]==nil||bytes.Equal(m["params"],[]byte("null")){return fmt.Errorf("invalid")};type plain ServerNotification;if e:=json.Unmarshal(b,(*plain)(p));e!=nil{return e};if p.Method!="notice"{return fmt.Errorf("invalid")};return nil }
`
	writeFile(t, subject, good+notification)
	writeFile(t, filepath.Join(module, "protocolv2/fixture_test.go"), "package protocolv2\nimport(\"os\";\"testing\")\nfunc TestMain(m *testing.M){os.Exit(0)}\n")
	runGitInit(t, root)
	if e := os.MkdirAll(filepath.Join(module, ".cache/openai-codex"), 0700); e != nil {
		t.Fatal(e)
	}
	candidate := t.TempDir()
	writeFile(t, filepath.Join(candidate, "schema/v2/Payload.json"), `{"title":"Payload","type":"object","properties":{"current":{"type":"boolean"}},"required":["current"]}`)
	for _, schemaRoot := range []string{filepath.Join(module, "internal/protocolschema/appserver/v2"), filepath.Join(candidate, "schema")} {
		for _, name := range []string{"ClientRequest", "ServerRequest", "ClientNotification"} {
			writeFile(t, filepath.Join(schemaRoot, name+".json"), `{"title":"`+name+`","oneOf":[]}`)
		}
		payload, err := os.ReadFile(filepath.Join(schemaRoot, "v2/Payload.json"))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(schemaRoot, "ServerNotification.json"), `{"title":"ServerNotification","oneOf":[{"type":"object","properties":{"method":{"type":"string","enum":["notice"]},"params":{"$ref":"#/definitions/Payload"}},"required":["method","params"]}],"definitions":{"Payload":`+string(payload)+`}}`)
	}
	writeOracleAcceptedManifest(t, filepath.Join(module, "internal/protocolschema/appserver/v2"), nil)
	writeFile(t, filepath.Join(candidate, "common.rs"), "client_request_definitions! {\n}\nserver_request_definitions! {\n}\n")
	writeFile(t, filepath.Join(candidate, "schema/v2/InternalNotification.json"), `{"title":"InternalNotification","type":"object","properties":{"private":{"type":"string"}}}`)
	sandbox, e := newOracleSandbox(context.Background(), root, candidate)
	if e != nil {
		t.Fatal(e)
	}
	req := ResumeRequest{RepoRoot: root, ModuleRoot: module, CandidateDir: candidate}
	if e := runTargetOracle(context.Background(), req, root, sandbox); e != nil {
		t.Fatalf("legitimate schema evolution rejected: %v", e)
	}
	for _, tc := range []struct{ name, code string }{
		{"missing_generated_field", "package protocolv2\ntype Payload struct{}\n"},
		{"candidate_init_bypasses_test", "package protocolv2\nimport \"os\"\ntype Payload struct{Current bool `json:\"current\"`}\nfunc init(){os.Exit(0)}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeFile(t, subject, tc.code+"\ntype ServerNotification struct{ Params Payload `json:\"params\"`; Method string `json:\"method\"` }\n")
			if e := runTargetOracle(context.Background(), req, root, sandbox); e == nil {
				t.Fatal("invalid implementation acquired independent proof")
			}
		})
	}
}

func TestLiveRepairSandboxBaselineKeepsRuntimeAssertions(t *testing.T) {
	if os.Getenv("LLMGO_REPAIR_SANDBOX_TEST") != "1" {
		t.Skip("required hosted sandbox integration")
	}
	root := t.TempDir()
	module := filepath.Join(root, "codexsdk")
	writeFile(t, filepath.Join(root, "go.mod"), "module github.com/ronhuafeng/llm-go\ngo 1.26.8\n")
	writeFile(t, filepath.Join(module, "internal/protocolschema/appserver/v2/v2/Payload.json"), `{"title":"Payload","type":"object","properties":{"retired":{"type":"boolean"}},"required":["retired"]}`)
	writeFile(t, filepath.Join(module, "runtime.go"), "package codexsdk\nfunc stableRuntime() bool { return true }\n")
	writeFile(t, filepath.Join(module, "runtime_test.go"), "package codexsdk\nimport \"testing\"\nfunc TestStableRuntime(t *testing.T){if !stableRuntime(){t.Fatal(\"runtime weakened\")}}\n")
	generated := "package protocolv2\ntype Payload struct { Retired bool `json:\"retired\"` }\n"
	writeFile(t, filepath.Join(module, "protocolv2/protocol_types.gen.go"), generated)
	writeFile(t, filepath.Join(module, "protocolv2/payload_test.go"), "package protocolv2\nimport(\"testing\";\"encoding/json\")\nfunc TestAcceptedBaselineWire(t *testing.T){b,e:=json.Marshal(Payload{Retired:true});if e!=nil||string(b)!=`{\"retired\":true}`{t.Fatalf(\"wire %s %v\",b,e)}}\n")
	writeFile(t, filepath.Join(module, "internal/wirejson/fixture.go"), "package wirejson\n")
	// The fixture generator implements the stable CLI boundary; no archived code.
	writeFile(t, filepath.Join(module, "internal/cmd/protocolv2gen/main.go"), "package main\nimport \"os\"\nfunc main(){if e:=os.WriteFile(\"protocolv2/protocol_types.gen.go\",[]byte("+fmt.Sprintf("%q", generated)+"),0600);e!=nil{panic(e)}}\n")
	runGitInit(t, root)
	if e := os.MkdirAll(filepath.Join(module, ".cache/openai-codex"), 0700); e != nil {
		t.Fatal(e)
	}
	candidate := t.TempDir()
	writeFile(t, filepath.Join(candidate, "fixture"), "readonly")
	sandbox, e := newOracleSandbox(context.Background(), root, candidate)
	if e != nil {
		t.Fatal(e)
	}
	req := ResumeRequest{RepoRoot: root, ModuleRoot: module, CandidateDir: candidate}
	if e = runBaselineOracle(context.Background(), req, sandbox); e != nil {
		t.Fatalf("baseline lane: %v", e)
	}
	writeFile(t, filepath.Join(module, "runtime.go"), "package codexsdk\nfunc stableRuntime() bool { return false }\n")
	writeFile(t, filepath.Join(module, "runtime_test.go"), "package codexsdk\nimport \"testing\"\nfunc TestCandidateClaim(t *testing.T){}\n")
	if e = runBaselineOracle(context.Background(), req, sandbox); e == nil || !strings.Contains(e.Error(), "runtime weakened") {
		t.Fatalf("candidate weakened baseline evidence: %v", e)
	}
	writeFile(t, filepath.Join(module, "runtime.go"), "package codexsdk\nimport \"os\"\nfunc stableRuntime() bool { return true }\nfunc init(){os.Exit(0)}\n")
	if e = runBaselineOracle(context.Background(), req, sandbox); e == nil {
		t.Fatal("candidate init suppressed accepted runtime tests")
	}
}
