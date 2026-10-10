package protocolsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// oracleSandbox is disposable execution, not a second acceptance service.
// Untrusted generator/tests see only the writable validation clone and private
// build/temp state; the Git identity, retained candidate and module inputs are
// read-only. A host without this boundary is blocked, never silently degraded.
type oracleSandbox struct {
	root, candidate, work, goroot, modules string
	readonly                               []string
}

func newOracleSandbox(ctx context.Context, root, candidate string) (oracleSandbox, error) {
	if runtime.GOOS != "linux" {
		return oracleSandbox{}, errors.New("repair oracle requires the Linux sandbox")
	}
	if _, err := exec.LookPath("bwrap"); err != nil {
		return oracleSandbox{}, errors.New("repair oracle requires bubblewrap")
	}
	cmd := exec.CommandContext(ctx, "go", "env", "GOMODCACHE")
	cmd.Env = repairEnvironment()
	out, err := cmd.Output()
	if err != nil {
		return oracleSandbox{}, err
	}
	s := oracleSandbox{root: root, candidate: candidate, work: filepath.Join(filepath.Dir(root), "sandbox"), goroot: runtime.GOROOT(), modules: strings.TrimSpace(string(out))}
	for _, p := range []string{s.work, filepath.Join(s.work, "cache"), filepath.Join(s.work, "home")} {
		if err = os.MkdirAll(p, 0700); err != nil {
			return s, err
		}
	}
	// This has no candidate code, credentials, or network; it proves the actual
	// namespace policy can start before invoking a proposed generator.
	if out, err = s.command(ctx, "/bin/true").CombinedOutput(); err != nil {
		return s, fmt.Errorf("repair sandbox unavailable: %w: %s", err, boundedRepairDiagnostic(string(out)))
	}
	return s, nil
}
func (s oracleSandbox) command(ctx context.Context, program string, args ...string) *exec.Cmd {
	flags := []string{"--unshare-all", "--die-with-parent", "--new-session", "--ro-bind", "/", "/", "--proc", "/proc", "--dev", "/dev"}
	for _, p := range []string{"/tmp", "/home", "/root", "/workspace", "/run"} {
		if _, e := os.Stat(p); e == nil {
			flags = append(flags, "--tmpfs", p)
		}
	}
	flags = append(flags, "--bind", s.root, s.root, "--ro-bind", filepath.Join(s.root, ".git"), filepath.Join(s.root, ".git"), "--bind", s.work, s.work, "--ro-bind", filepath.Join(s.root, "codexsdk/.cache/openai-codex"), filepath.Join(s.root, "codexsdk/.cache/openai-codex"), "--ro-bind", s.candidate, s.candidate, "--ro-bind", s.goroot, s.goroot, "--ro-bind", s.modules, s.modules)
	for _, p := range s.readonly {
		flags = append(flags, "--ro-bind", p, p)
	}
	flags = append(flags, "--chdir", filepath.Join(s.root, "codexsdk"), "--", program)
	flags = append(flags, args...)
	cmd := exec.CommandContext(ctx, "bwrap", flags...)
	cmd.Env = []string{"PATH=" + filepath.Join(s.goroot, "bin") + ":/usr/bin:/bin", "HOME=" + filepath.Join(s.work, "home"), "GOCACHE=" + filepath.Join(s.work, "cache"), "GOMODCACHE=" + s.modules, "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "TMPDIR=/tmp", "CI=true"}
	return cmd
}
