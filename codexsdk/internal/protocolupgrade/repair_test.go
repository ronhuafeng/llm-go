package protocolupgrade

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Run the real owner against a benign generator regression, then rebuild from
// the permitted repair. A frozen control binary must accept the implementation
// change without authorizing a change to its own acceptance policy.
func TestGeneratorRepairResumesSameCandidateWithTrustedScope(t *testing.T) {
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
	for _, pkg := range []string{"protocolsync", "protocolupgrade", "protocolgen", "generatedcheck", "wirejson", "cmd/protocolupgrade"} {
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
	// Acquisition is a fixture seam; Plan, construction, Apply and digest binding
	// below are the production implementations, with no injected success result.
	helper := `package main
import("encoding/json";"os";"path/filepath";"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolsync")
type lookup string
func(s lookup) LSRemote(_ string, patterns ...string)(string,error){return string(s)+"\trefs/tags/rust-v1.2.3\n",nil}
func main(){r,c,s:=os.Args[1],os.Args[2],os.Args[3]; m:=filepath.Join(r,"codexsdk"); result,err:=protocolsync.Sync(protocolsync.SyncRequest{RepoRoot:r,ModuleRoot:m,UpstreamRef:"rust-v1.2.3",Lookuper:lookup(s),Generate:func(protocolsync.GenerateRequest)(protocolsync.Candidate,error){return protocolsync.Candidate{Dir:c,SchemaDir:filepath.Join(c,"schema"),StableSchemaDir:filepath.Join(c,"stable-schema"),ReportsDir:filepath.Join(c,"reports"),CommonRS:filepath.Join(c,"common.rs"),CommonRSSourceSHA:s,CodexRepo:filepath.Join(m,".cache/openai-codex"),SourceCommit:s,DriftStatus:"review-required"},nil}});if err!=nil{panic(err)};json.NewEncoder(os.Stdout).Encode(result)}
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
	must("go", "build", "-o", control, "./codexsdk/internal/cmd/protocolupgrade")
	initial := must("go", "run", "./codexsdk/internal/cmd/fixture", repo, candidate, sha)
	var result struct{ Outcome, CandidateSHA256 string }
	if err := json.Unmarshal(initial, &result); err != nil {
		t.Fatalf("initial result: %v: %s", err, initial)
	}
	if result.Outcome != "semantic_unresolved" || result.CandidateSHA256 == "" {
		t.Fatalf("initial=%s", initial)
	}
	put(generator, original) // the permitted generator implementation repair
	must(control, "scope", "-repo-root", repo, "-phase", "agent")
	repaired := filepath.Join(t.TempDir(), "repaired")
	must("go", "build", "-o", repaired, "./codexsdk/internal/cmd/protocolupgrade")
	args := []string{"resume", "-repo-root", repo, "-module-root", module, "-candidate-dir", candidate, "-candidate-sha256", result.CandidateSHA256, "-target-ref", "rust-v1.2.3", "-target-kind", "stable_rust_tag", "-target-sha", sha}
	if out := must(repaired, args...); !strings.Contains(string(out), "re-planned successfully") {
		t.Fatalf("resume=%s", out)
	}
	must("go", "test", "./codexsdk/internal/protocolgen", "-run", "TestNoPublicationCredentials")
	must(repaired, "check", "-module-root", module)
	must(control, "scope", "-repo-root", repo, "-phase", "final")
	must(control, "stage", "-repo-root", repo, "-phase", "final")
	must(control, "verify-candidate", "-candidate-dir", candidate, "-candidate-sha256", result.CandidateSHA256, "-target-ref", "rust-v1.2.3", "-target-kind", "stable_rust_tag", "-target-sha", sha)
	protected := filepath.Join(module, "internal/protocolsync/policy.go")
	put(protected, []byte("package protocolsync // fixture control change\n"))
	if out, err := command(control, "stage", "-repo-root", repo, "-phase", "final"); err == nil || !strings.Contains(string(out), "needs-maintainer") {
		t.Fatalf("control change accepted: %v: %s", err, out)
	}
}
