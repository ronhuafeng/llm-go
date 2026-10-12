package protocolupgrade

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Run the real owner against a benign generator regression, then rebuild from
// the permitted repair. A frozen control binary must accept the implementation
// change without authorizing a change to its own acceptance policy.
func TestGeneratorSameCandidateAfterRepairWithTrustedScope(t *testing.T) {
	source, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	put := func(path string, data []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{"go.mod", "go.sum"} {
		raw, err := os.ReadFile(filepath.Join(source, rel))
		if err != nil {
			t.Fatal(err)
		}
		put(filepath.Join(repo, rel), raw)
	}
	for _, pkg := range []string{"protocolupgrade", "protocolgen", "generatedcheck", "wirejson"} {
		dir := filepath.Join(source, "codexsdk/internal", pkg)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			put(filepath.Join(repo, "codexsdk/internal", pkg, entry.Name()), raw)
		}
	}
	module := filepath.Join(repo, "codexsdk")
	fix := writeCompleteApplyFixture(t)
	baseline := filepath.Join(module, filepath.FromSlash(defaultBaselineRel))
	if err := copyTree(fix.baseline, baseline); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, filepath.Join(baseline, "baseline_metadata.json"), map[string]any{"schema_version": 1, "generated_at": "2026-01-01T00:00:00Z", "source_commit": strings.Repeat("0", 40), "source_ref_name": "rust-v0.1.0", "source_ref_kind": "stable_rust_tag"})
	if err := os.MkdirAll(filepath.Join(module, "protocolv2"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"nullable.go", "json_value.go"} {
		raw, err := os.ReadFile(filepath.Join(source, "codexsdk/protocolv2", name))
		if err != nil {
			t.Fatal(err)
		}
		put(filepath.Join(module, "protocolv2", name), raw)
	}
	put(filepath.Join(module, "client.go"), []byte(`package codexsdk
import "context"
type Client struct{}
func (*Client) callProtocol(context.Context, string, any, any) error { return nil }
func (*Client) callProtocolNoParams(context.Context, string, any) error { return nil }
`))
	cache := filepath.Join(module, ".cache")
	upstream := filepath.Join(cache, "openai-codex")
	raw, err := os.ReadFile(fix.commonRS)
	if err != nil {
		t.Fatal(err)
	}
	put(filepath.Join(upstream, filepath.FromSlash(commonRSRef)), raw)
	runGit(t, upstream, "init", "-q")
	runGit(t, upstream, "config", "user.email", "fixture@example.com")
	runGit(t, upstream, "config", "user.name", "Fixture")
	runGit(t, upstream, "add", ".")
	runGit(t, upstream, "commit", "-qm", "exact source")
	sha := strings.TrimSpace(runGitOutput(t, upstream, "rev-parse", "HEAD"))
	candidate := filepath.Join(cache, "candidate")
	if err := copyTree(fix.candidate, filepath.Join(candidate, "schema")); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(fix.stable, filepath.Join(candidate, "stable-schema")); err != nil {
		t.Fatal(err)
	}
	put(filepath.Join(candidate, "common.rs"), raw)
	put(filepath.Join(candidate, "common.rs.source_sha"), []byte(sha+"\n"))
	writeJSONFile(t, filepath.Join(candidate, "reports/drift_summary.json"), map[string]any{"target": map[string]string{"source_ref_name": "rust-v1.2.3", "source_ref_kind": "stable_rust_tag", "source_commit": sha}})
	// The focused helper executes the real planning/materialization owner. The
	// fixed scope/check control is built separately from the real repository.
	helper := `package main
import("encoding/json";"os";"path/filepath";"fmt";"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolupgrade")
func main(){r,c,s:=os.Args[1],os.Args[2],os.Args[3];m:=filepath.Join(r,"codexsdk");req:=protocolupgrade.ApplyRequest{ModuleRoot:m,Baseline:filepath.Join(m,"internal/protocolschema/appserver/v2"),Candidate:filepath.Join(c,"schema"),StableCandidate:filepath.Join(c,"stable-schema"),Reports:filepath.Join(m,".cache/repair-reports"),CommonRS:filepath.Join(c,"common.rs"),CommonRSSourceSHA:s,CodexRepo:filepath.Join(m,".cache/openai-codex"),TargetRef:"rust-v1.2.3",TargetKind:"stable_rust_tag",TargetSHA:s};planned,err:=protocolupgrade.PlanRepository(req);if err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)};if len(os.Args)>4 {if planned.Status!=protocolupgrade.PlanReady{fmt.Fprintln(os.Stderr,planned.Issue);os.Exit(1)};if _,err=planned.Apply();err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}};json.NewEncoder(os.Stdout).Encode(planned)}
`
	put(filepath.Join(module, "internal/cmd/fixture/main.go"), []byte(helper))
	generator := filepath.Join(module, "internal/protocolgen/package.go")
	original, err := os.ReadFile(generator)
	if err != nil {
		t.Fatal(err)
	}
	regression := string(original)
	needle := "(ProtocolPackage, error) {"
	if !strings.Contains(regression, needle) {
		t.Fatal("fixture generator seam missing")
	}
	regression = strings.Replace(regression, needle, needle+"\nif len(manifest.Entries) > 0 { return ProtocolPackage{}, unsupportedGeneratedSchema(\"ClientRequest.json\", \"fixture unsupported representation\") }", 1)
	put(generator, []byte(regression))
	put(filepath.Join(repo, ".gitignore"), []byte("codexsdk/.cache/\n"))
	put(filepath.Join(module, "internal/protocolgen/credentials_test.go"), []byte(`package protocolgen
import("os";"testing")
func TestNoPublicationCredentials(t *testing.T){for _,key:=range []string{"GH_TOKEN","GITHUB_TOKEN"}{if os.Getenv(key)!=""{t.Fatal("proposal received publication credential")}}}
`))
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.email", "fixture@example.com")
	runGit(t, repo, "config", "user.name", "Fixture")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "accepted fixture")
	command := func(name string, args ...string) ([]byte, error) {
		cmd := exec.Command(name, args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GH_TOKEN=", "GITHUB_TOKEN=", "GITHUB_OUTPUT=")
		return cmd.CombinedOutput()
	}
	must := func(name string, args ...string) []byte {
		t.Helper()
		out, err := command(name, args...)
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
		return out
	}
	control := filepath.Join(t.TempDir(), "control")
	buildControl := exec.Command("go", "build", "-o", control, "./codexsdk/internal/cmd/protocolupgrade")
	buildControl.Dir = source
	buildControl.Env = append(os.Environ(), "GH_TOKEN=", "GITHUB_TOKEN=", "GITHUB_OUTPUT=")
	if out, e := buildControl.CombinedOutput(); e != nil {
		t.Fatalf("build frozen real control: %v\n%s", e, out)
	}
	baselineBefore, err := snapshotHashes(baseline)
	if err != nil {
		t.Fatal(err)
	}
	candidateBefore, err := snapshotHashes(candidate)
	if err != nil {
		t.Fatal(err)
	}
	initial := must("go", "run", "./codexsdk/internal/cmd/fixture", repo, candidate, sha)
	var result PlanResult
	if err = json.Unmarshal(initial, &result); err != nil {
		t.Fatalf("initial result: %v: %s", err, initial)
	}
	if result.Status != PlanSemanticUnresolved || result.Issue == nil || !strings.Contains(result.Issue.Reason, "fixture unsupported representation") {
		t.Fatalf("initial=%s", initial)
	}
	baselineAfter, err := snapshotHashes(baseline)
	if err != nil || !reflect.DeepEqual(baselineBefore, baselineAfter) {
		t.Fatalf("unresolved Plan changed accepted baseline: %v", err)
	}
	put(generator, original)
	must(control, "scope", "-repo-root", repo, "-phase", "agent")
	// A different claimed target cannot reuse the same exact common.rs checkout.
	if out, e := command("go", "run", "./codexsdk/internal/cmd/fixture", repo, candidate, strings.Repeat("f", 40)); e == nil || !strings.Contains(string(out), "common.rs") {
		t.Fatalf("wrong target accepted or unrelated failure: %v %s", e, out)
	}
	repaired := must("go", "run", "./codexsdk/internal/cmd/fixture", repo, candidate, sha, "apply")
	if err = json.Unmarshal(repaired, &result); err != nil || result.Status != PlanReady {
		t.Fatalf("repaired plan: %v %s", err, repaired)
	}
	candidateAfter, err := snapshotHashes(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(candidateBefore, candidateAfter) {
		var changed []string
		for name, hash := range candidateBefore {
			if candidateAfter[name] != hash {
				changed = append(changed, name)
			}
		}
		for name := range candidateAfter {
			if _, ok := candidateBefore[name]; !ok {
				changed = append(changed, name)
			}
		}
		t.Fatalf("repair or apply changed the bound candidate bytes: %v", changed)
	}
	must("go", "test", "./codexsdk/internal/protocolgen", "-run", "TestNoPublicationCredentials")
	must(control, "check", "-module-root", module)
	must(control, "scope", "-repo-root", repo, "-phase", "final")
	must(control, "stage", "-repo-root", repo, "-phase", "final")
	protected := filepath.Join(module, "internal/protocolsync/policy.go")
	put(protected, []byte("package protocolsync // fixture control change\n"))
	if out, err := command(control, "stage", "-repo-root", repo, "-phase", "final"); err == nil || !strings.Contains(string(out), "needs-maintainer") {
		t.Fatalf("control change accepted: %v: %s", err, out)
	}
}
