package protocolsync

import (
	"bytes"
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
	writableUpstream                       bool
	extraPath                              string
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
	// Prove the actual boundary before any candidate code executes. A successful
	// launcher exit alone is insufficient: writable positive control and unchanged
	// read-only/hidden inputs must all be observed by the trusted parent.
	if err = s.proveBoundary(ctx); err != nil {
		return s, err
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
	upstreamBind := "--ro-bind"
	if s.writableUpstream {
		upstreamBind = "--bind"
	}
	flags = append(flags, "--bind", s.root, s.root, "--ro-bind", filepath.Join(s.root, ".git"), filepath.Join(s.root, ".git"), "--bind", s.work, s.work, upstreamBind, filepath.Join(s.root, "codexsdk/.cache/openai-codex"), filepath.Join(s.root, "codexsdk/.cache/openai-codex"), "--ro-bind", s.candidate, s.candidate, "--ro-bind", s.goroot, s.goroot, "--ro-bind", s.modules, s.modules)
	for _, p := range s.readonly {
		flags = append(flags, "--ro-bind", p, p)
	}
	flags = append(flags, "--chdir", filepath.Join(s.root, "codexsdk"), "--", program)
	flags = append(flags, args...)
	cmd := exec.CommandContext(ctx, "bwrap", flags...)
	path := filepath.Join(s.goroot, "bin") + ":/usr/bin:/bin"
	if s.extraPath != "" {
		path = filepath.Join(s.goroot, "bin") + ":" + s.extraPath + ":/usr/bin:/bin"
	}
	cmd.Env = []string{"PATH=" + path, "HOME=" + filepath.Join(s.work, "home"), "GOCACHE=" + filepath.Join(s.work, "cache"), "GOMODCACHE=" + s.modules, "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "TMPDIR=/tmp", "CI=true"}
	return cmd
}

func (s oracleSandbox) proveBoundary(ctx context.Context) error {
	hidden, err := os.MkdirTemp(filepath.Dir(s.root), "sandbox-hidden-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(hidden)
	secret := filepath.Join(hidden, "parent-input")
	if err = os.WriteFile(secret, []byte("not-visible"), 0600); err != nil {
		return err
	}
	probe, err := os.CreateTemp(s.work, "trusted-boundary-")
	if err != nil {
		return err
	}
	name := probe.Name()
	defer os.Remove(name)
	if _, err = probe.WriteString("immutable"); err != nil {
		probe.Close()
		return err
	}
	if err = probe.Close(); err != nil {
		return err
	}
	head := filepath.Join(s.root, ".git/HEAD")
	before, err := os.ReadFile(head)
	if err != nil {
		return err
	}
	positive := filepath.Join(s.root, "codexsdk", filepath.Base(name)+"-positive")
	defer os.Remove(positive)
	forbidden := filepath.Join(s.candidate, filepath.Base(name)+"-forbidden")
	local := s
	local.readonly = append(append([]string{}, s.readonly...), name)
	script := `set -eu
if printf tamper > "$1"; then exit 21; fi
if printf tamper > "$2"; then exit 22; fi
if printf tamper > "$3"; then exit 23; fi
if test -e "$4"; then exit 24; fi
printf safe > "$5"
`
	cmd := local.command(ctx, "/bin/sh", "-c", script, "boundary", name, head, forbidden, secret, positive)
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		return fmt.Errorf("native sandbox boundary was not proven: %w: %s", runErr, boundedRepairDiagnostic(string(out)))
	}
	got, e := os.ReadFile(positive)
	if e != nil || string(got) != "safe" {
		return errors.New("native sandbox writable positive control failed")
	}
	got, e = os.ReadFile(name)
	if e != nil || string(got) != "immutable" {
		return errors.New("native sandbox read-only input changed")
	}
	got, e = os.ReadFile(head)
	if e != nil || !bytes.Equal(got, before) {
		return errors.New("native sandbox Git identity changed")
	}
	got, e = os.ReadFile(secret)
	if e != nil || string(got) != "not-visible" {
		return errors.New("native sandbox parent input changed")
	}
	if _, e = os.Lstat(forbidden); !os.IsNotExist(e) {
		return errors.New("native sandbox wrote retained candidate")
	}
	return nil
}
