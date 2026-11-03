package protocolsync

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A version-pinned go run still looks up module deprecation metadata. Exercise
// the sandbox's real environment after its original proxy has disappeared.
func TestNativeSandboxRunsPinnedToolFromPrewarmedCache(t *testing.T) {
	base := t.TempDir()
	proxy := filepath.Join(base, "original-proxy")
	versions := filepath.Join(proxy, "example.test/tool/@v")
	for _, version := range []string{"v1.0.0", "v1.1.0"} {
		mod := "module example.test/tool\n\ngo 1.26.8\n"
		writeFile(t, filepath.Join(versions, version+".mod"), mod)
		writeFile(t, filepath.Join(versions, version+".info"), fmt.Sprintf(`{"Version":%q,"Time":"2026-01-01T00:00:00Z"}`, version))
		var archive bytes.Buffer
		zw := zip.NewWriter(&archive)
		for name, content := range map[string]string{
			"go.mod":  mod,
			"main.go": fmt.Sprintf("package main\nimport \"fmt\"\nfunc main(){fmt.Println(%q)}\n", version),
		} {
			entry, err := zw.Create("example.test/tool@" + version + "/" + name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = entry.Write([]byte(content)); err != nil {
				t.Fatal(err)
			}
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(versions, version+".zip"), archive.String())
	}
	writeFile(t, filepath.Join(versions, "list"), "v1.0.0\nv1.1.0\n")
	root := filepath.Join(base, "workspace")
	writeFile(t, filepath.Join(root, "go.mod"), "module fixture\n\ngo 1.26.8\n")
	s := nativeSandbox{root: root, work: filepath.Join(base, "private"), goroot: runtime.GOROOT(), modules: filepath.Join(base, "module cache")}
	for _, dir := range []string{s.modules, filepath.Join(s.work, "home"), filepath.Join(s.work, "cache")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	run := func(env []string, version string) ([]byte, error) {
		cmd := exec.Command(filepath.Join(s.goroot, "bin", "go"), "run", "example.test/tool@"+version)
		cmd.Dir, cmd.Env = root, env
		return cmd.CombinedOutput()
	}
	warmEnv := append([]string{}, s.command(context.Background(), "/bin/true").Env...)
	warmEnv = append(warmEnv, "GOFLAGS=-modcacherw") // allow TempDir cleanup of downloaded fixtures
	for i, value := range warmEnv {
		if strings.HasPrefix(value, "GOPROXY=") {
			warmEnv[i] = "GOPROXY=" + (&url.URL{Scheme: "file", Path: filepath.ToSlash(proxy)}).String()
		}
	}
	if out, err := run(warmEnv, "v1.0.0"); err != nil || strings.TrimSpace(string(out)) == "v1.1.0" {
		t.Fatalf("warm pinned tool: %v: %s", err, out)
	}
	if err := os.Rename(proxy, proxy+"-unavailable"); err != nil {
		t.Fatal(err)
	}
	env := s.command(context.Background(), "/bin/true").Env
	if out, err := run(env, "v1.0.0"); err != nil || strings.TrimSpace(string(out)) != "v1.0.0" {
		t.Fatalf("offline pinned tool: %v: %s", err, out)
	}
	if out, err := run(env, "v1.2.0"); err == nil {
		t.Fatalf("uncached tool version succeeded: %s", out)
	}
}

func TestNativeSandboxPolicyBindsOnlyDisposableWrites(t *testing.T) {
	root := t.TempDir()
	s := nativeSandbox{root: root, candidate: "/tmp/candidate", work: "/tmp/private-work", goroot: "/opt/go", modules: "/tmp/modules"}
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
	s, err := newNativeSandbox(context.Background(), root, candidate)
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

// Source, schema and assertions travel together at H. Retired B/M fixtures are
// identity evidence only, never compilation inputs for a different version.
func TestLiveRepairSandboxCandidateVersionConsistency(t *testing.T) {
	if os.Getenv("LLMGO_REPAIR_SANDBOX_TEST") != "1" {
		t.Skip("required hosted sandbox integration")
	}
	h, b, m := goalFixture(t), goalFixture(t), goalFixture(t)
	for _, root := range []string{b, m} {
		writeFile(t, filepath.Join(root, "retired_test.go"), "deliberately incompatible retired test source")
	}
	writeFile(t, filepath.Join(h, "go.mod"), "module example.invalid/candidate\ngo 1.26.8\n")
	writeFile(t, filepath.Join(h, "tracked.go"), "package candidate\nfunc Current() int { return 2 }\n")
	writeFile(t, filepath.Join(h, "current_test.go"), "package candidate\nimport \"testing\"\nfunc TestCurrent(t *testing.T){if Current()!=2 {t.Fatal(\"candidate version regression\")}}\n")
	verify := func() error { return VerifyForwardCandidate(context.Background(), h, b, m, t.TempDir()) }
	if err := verify(); err != nil {
		t.Fatalf("same-version candidate rejected: %v", err)
	}
	writeFile(t, filepath.Join(h, "tracked.go"), "package candidate\nfunc Current() int { return 1 }\n")
	if err := verify(); err == nil {
		t.Fatal("candidate assertion failure accepted")
	}
	writeFile(t, filepath.Join(h, "tracked.go"), "package candidate\nfunc Current() int { return missing }\n")
	if err := verify(); err == nil {
		t.Fatal("candidate compile failure accepted")
	}
}
