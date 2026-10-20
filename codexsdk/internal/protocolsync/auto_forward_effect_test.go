package protocolsync

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestForwardMainAdvertisementRejectsIntermediateAncestor(t *testing.T) {
	root := t.TempDir()
	goalTestGit(t, root, "init", "-q", "-b", "main")
	goalTestGit(t, root, "config", "user.name", "Fixture")
	goalTestGit(t, root, "config", "user.email", "fixture@example.invalid")
	goalTestGit(t, root, "commit", "--allow-empty", "-qm", "M")
	main := goalTestGit(t, root, "rev-parse", "HEAD")
	goalTestGit(t, root, "commit", "--allow-empty", "-qm", "intermediate")
	intermediate := goalTestGit(t, root, "rev-parse", "HEAD")
	goalTestGit(t, root, "commit", "--allow-empty", "-qm", "H")
	head := goalTestGit(t, root, "rev-parse", "HEAD")
	remote := filepath.Join(t.TempDir(), "remote.git")
	goalTestGit(t, root, "clone", "--bare", root, remote)
	goalTestGit(t, remote, "update-ref", "refs/heads/main", intermediate)
	hooks, cleanup, e := prepareForwardMainHook(head, main)
	if e != nil {
		t.Fatal(e)
	}
	defer cleanup()
	push := func() error {
		cmd := exec.CommandContext(context.Background(), "git", "-c", "core.hooksPath="+hooks, "-C", root, "push", remote, head+":refs/heads/main")
		return cmd.Run()
	}
	if e = push(); e == nil {
		t.Fatal("accepted unproved intermediate main")
	}
	if got := goalTestGit(t, remote, "rev-parse", "refs/heads/main"); got != intermediate {
		t.Fatal("rejected proof changed main")
	}
	cleanup()
	hooks, cleanup, e = prepareForwardMainHook(head, intermediate)
	if e != nil {
		t.Fatal(e)
	}
	defer cleanup()
	if e = push(); e != nil {
		t.Fatalf("fresh exact main proof cannot normal-FF: %v", e)
	}
	if got := goalTestGit(t, remote, "rev-parse", "refs/heads/main"); got != head {
		t.Fatal("normal FF not observed")
	}
}
func TestForwardMainAdvertisementRejectsExtraOrWrongUpdate(t *testing.T) {
	hooks, cleanup, e := prepareForwardMainHook(forwardH, forwardM)
	if e != nil {
		t.Fatal(e)
	}
	defer cleanup()
	exact := forwardH + " " + forwardH + " refs/heads/main " + forwardM + "\n"
	for _, input := range []string{"", exact + exact, strings.Replace(exact, "refs/heads/main", "refs/heads/other", 1), strings.Replace(exact, forwardM, forwardOther, 1)} {
		cmd := exec.Command(filepath.Join(hooks, "pre-push"))
		cmd.Stdin = strings.NewReader(input)
		if e = cmd.Run(); e == nil {
			t.Fatal("ambiguous main update accepted")
		}
	}
}
