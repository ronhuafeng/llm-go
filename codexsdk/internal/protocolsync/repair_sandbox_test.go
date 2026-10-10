package protocolsync

import (
	"context"
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
	for _, want := range []string{"--unshare-all", "--die-with-parent", "--new-session", "--proc /proc", "--tmpfs /tmp", "--tmpfs /home", "--tmpfs /root", "--ro-bind " + filepath.Join(root, ".git"), "--ro-bind /tmp/candidate", "--ro-bind /tmp/modules"} {
		if !strings.Contains(text, want) {
			t.Fatal("missing sandbox boundary:", want)
		}
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
	script := `set -eu
! printf tamper > "$1/.git/HEAD"
! printf tamper > "$2/fixture"
! test -e "$3"
printf safe > "$1/codexsdk/within-workspace"
`
	secret := filepath.Join(t.TempDir(), "parent-secret")
	writeFile(t, secret, "not-visible")
	cmd := s.command(context.Background(), "/bin/sh", "-c", script, "fixture", root, candidate, secret)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sandbox contract: %v: %s", err, out)
	}
	if data, err := os.ReadFile(filepath.Join(candidate, "fixture")); err != nil || string(data) != "immutable" {
		t.Fatal("candidate modified")
	}
}
