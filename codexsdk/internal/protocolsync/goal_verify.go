package protocolsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/codexexec"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// GoalVerifyRequest selects the same native gate commands used by CI. Empty
// Gate means local source/generated/offline-provenance checks; Live belongs to CI. Diagnostics must live outside the candidate.
type GoalVerifyRequest struct {
	RepoRoot, DiagnosticDir, Gate, TrustedCacheRoot, BaseRoot, CandidateDir string
	// OriginalBaseRoot retains B when BaseRoot is rebound to current main M.
	OriginalBaseRoot string
	Progress         *GoalProgress
}
type GoalVerifyResult struct {
	DiagnosticPath, SnapshotDigest, SnapshotRoot, Scope, Head string
	cleanupRoot                                               string
	Gates                                                     []string
}

// Close releases the successful frozen candidate after publication/consumption.
func (r *GoalVerifyResult) Close() error {
	if r.cleanupRoot == "" {
		return nil
	}
	err := os.RemoveAll(r.cleanupRoot)
	if err == nil {
		r.cleanupRoot = ""
		r.SnapshotRoot = ""
	}
	return err
}

// GoalVerificationIncompleteError means the diagnostic stream cannot support a
// complete report. The command must fail even if its last child exited zero.
type GoalVerificationIncompleteError struct{ Err error }

func (e *GoalVerificationIncompleteError) Error() string {
	return "verification diagnostics incomplete: " + e.Err.Error()
}
func (e *GoalVerificationIncompleteError) Unwrap() error { return e.Err }

const goalDiagnosticLimit = 16 << 20

var goalVerificationGates = []string{"source", "generated", "provenance"}

type goalCommandRunner func(*exec.Cmd) error

func VerifyGoal(ctx context.Context, req GoalVerifyRequest) (GoalVerifyResult, error) {
	return verifyGoal(ctx, req, nil)
}
func verifyGoal(ctx context.Context, req GoalVerifyRequest, run goalCommandRunner, contract ...goalContractRunner) (result GoalVerifyResult, retErr error) {
	result.Scope = "local"
	defer func() {
		if retErr != nil {
			_ = result.Close()
		}
	}()
	gates := goalVerificationGates
	if req.Gate != "" && req.Gate != "local" {
		found := false
		for _, g := range gates {
			if g == req.Gate {
				found = true
			}
		}
		if !found {
			return result, errors.New("unknown verification gate")
		}
		gates = []string{req.Gate}
	}
	root, err := filepath.Abs(req.RepoRoot)
	if err != nil {
		return result, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return result, err
	}
	dir := req.DiagnosticDir
	if dir == "" {
		dir, err = os.MkdirTemp("", "llm-go-verification-")
		if err != nil {
			return result, err
		}
	} else {
		if err = os.MkdirAll(dir, 0700); err != nil {
			return result, err
		}
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return result, err
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil || real != dir {
		return result, errors.New("diagnostic directory must not be a symlink")
	}
	if goalWithin(root, dir) {
		return result, errors.New("diagnostic directory must be outside candidate")
	}
	log, err := newGoalDiagnostics(dir)
	if err != nil {
		return result, err
	}
	result.DiagnosticPath = log.file.Name()
	head, e := gitOutput(root, "rev-parse", "HEAD")
	if e != nil {
		log.file.Close()
		return result, e
	}
	result.Head = strings.TrimSpace(head)
	log.record("header", "", map[string]any{"head_sha": result.Head, "scope": result.Scope, "gates": gates})
	defer func() {
		log.record("footer", "", map[string]any{"head_sha": result.Head, "complete": log.err == nil, "passed": retErr == nil, "snapshot_sha256": result.SnapshotDigest, "scope": result.Scope, "gates": result.Gates, "error": goalError(retErr)})
		if err := log.file.Close(); err != nil && log.err == nil {
			log.err = err
		}
		if log.err != nil {
			retErr = &GoalVerificationIncompleteError{Err: log.err}
		}
	}()
	work, err := os.MkdirTemp("", "llm-go-verify-snapshot-")
	if err != nil {
		return result, err
	}
	result.cleanupRoot = work
	snapshot := filepath.Join(work, "candidate")
	before, err := goalSnapshot(ctx, root, snapshot)
	if err != nil {
		return result, err
	}
	result.SnapshotDigest = before
	result.SnapshotRoot = snapshot
	log.record("snapshot", "", map[string]any{"sha256": before, "gates": gates})
	if run == nil {
		// All candidate execution remains offline and credential-free. Only
		// launcher-prewarmed cache inputs may seed disposable provenance state.
		provenance := false
		for _, gate := range gates {
			if gate == "provenance" {
				provenance = true
			}
		}
		if provenance {
			if req.TrustedCacheRoot == "" {
				return result, errors.New("offline provenance blocked: trusted launcher cache is required")
			}
			cacheRoot, e := filepath.Abs(req.TrustedCacheRoot)
			if e != nil {
				return result, e
			}
			real, e := filepath.EvalSymlinks(cacheRoot)
			if e != nil || real != cacheRoot || goalWithin(root, cacheRoot) || goalWithin(cacheRoot, root) {
				return result, errors.New("trusted cache must be outside candidate without symlink indirection")
			}
			if err = goalPrepareOfflineCache(ctx, cacheRoot, snapshot, work); err != nil {
				return result, err
			}
		}

		retained := filepath.Join(work, "readonly-inputs")
		if err = os.MkdirAll(retained, 0700); err != nil {
			return result, err
		}
		if err = os.MkdirAll(filepath.Join(snapshot, "codexsdk/.cache/openai-codex"), 0700); err != nil {
			return result, err
		}
		sandbox, e := newOracleSandbox(ctx, snapshot, retained)
		if e != nil {
			return result, e
		}
		if provenance {
			tools := filepath.Join(work, "rust-tools")
			sandbox.readonly = append(sandbox.readonly, tools)
			sandbox.extraPath = tools
		}
		nativeBinary, nativeVersion := "", ""
		for _, gate := range gates {
			if gate == "source" {
				nativeVersion = os.Getenv("CODEX_CLI_VERSION")
				if nativeVersion == "" {
					return result, errors.New("local source blocked: run-selected CODEX_CLI_VERSION is required")
				}
				nativeBinary = os.Getenv("CODEX_BINARY")
				if nativeBinary == "" {
					nativeBinary = "codex"
				}
				nativeBinary, e = exec.LookPath(nativeBinary)
				if e != nil {
					return result, errors.New("local source blocked: installed Codex prerequisite unavailable")
				}
				nativeBinary, e = filepath.EvalSymlinks(nativeBinary)
				if e != nil {
					return result, e
				}
				if e = goalNativeRuntimeInputs(&sandbox, nativeBinary); e != nil {
					return result, e
				}
			}
		}
		work = sandbox.work
		run = func(cmd *exec.Cmd) error {
			local := sandbox
			if strings.Contains(strings.Join(cmd.Args, " "), "verify-provenance-offline") {
				local.writableUpstream = true
			}
			isolated := goalSandboxCommand(ctx, local, cmd, nativeBinary, nativeVersion)
			return isolated.Run()
		}
	}

	defer func() {
		after, err := goalSnapshot(ctx, root, "")
		if err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("candidate revalidation unavailable: %w", err))
		} else if after != before {
			retErr = errors.Join(retErr, errors.New("candidate changed during verification"))
		}
	}()
	for _, gate := range gates {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		commands, cleanup, err := goalGateCommands(ctx, gate, snapshot, work)
		req.Progress.line("Local " + gate + ": started")
		if err != nil {
			return result, err
		}
		for _, cmd := range commands {
			if err = goalRunCommand(cmd, gate, log, run); err != nil {
				cleanup()
				return result, err
			}

			digest, e := goalSnapshot(ctx, snapshot, "")
			if e != nil || digest != before {
				cleanup()
				return result, errors.New("verification changed frozen candidate")
			}
		}
		cleanup()
		if gate == "provenance" {
			if err = goalRunContracts(ctx, req, snapshot, work, log, contract...); err != nil {
				return result, err
			}
			digest, e := goalSnapshot(ctx, snapshot, "")
			if e != nil || digest != before {
				return result, errors.New("contract verification changed frozen candidate")
			}
		}
		result.Gates = append(result.Gates, gate)
		req.Progress.line("Local " + gate + ": passed")
	}
	return result, nil
}

// Public native runtime files may resolve into paths hidden by the outer
// sandbox (for example /etc/resolv.conf into /run). Re-admit only those exact
// targets, read-only; their containing directories and sockets remain hidden.
func goalNativeRuntimeInputs(sandbox *oracleSandbox, binary string) error {
	dirs, err := codexexec.NativeGoalRuntimePaths(binary)
	if err != nil {
		return err
	}
	sandbox.readonly = append(sandbox.readonly, dirs...)
	for _, dir := range dirs {
		if sandbox.extraPath != "" {
			sandbox.extraPath += ":"
		}
		sandbox.extraPath += dir + ":" + filepath.Join(dir, "bin")
	}
	for _, path := range []string{"/etc/ssl/certs", "/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf"} {
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("native runtime input unavailable: %s", path)
		}
		if target != path {
			sandbox.readonly = append(sandbox.readonly, target)
		}
	}
	return nil
}

func goalSandboxCommand(ctx context.Context, sandbox oracleSandbox, cmd *exec.Cmd, binary, version string) *exec.Cmd {
	dir := cmd.Dir
	if goalNativeTest(cmd) != "" {
		dir = sandbox.root
	}
	isolated := sandbox.command(ctx, filepath.Join(sandbox.goroot, "bin", "go"), append([]string{"-C", dir}, cmd.Args[1:]...)...)
	switch goalNativeTest(cmd) {
	case "TestNativeGoalSandboxBoundary":
		isolated.Env = append(isolated.Env, "LLMGO_NATIVE_GOAL_SANDBOX=1", "CODEX_BINARY="+binary, "CODEX_CLI_VERSION="+version)
	case "TestLiveRepairSandboxGoalContractsUseCandidateChecker":
		isolated.Env = append(isolated.Env, "LLMGO_REPAIR_SANDBOX_TEST=1")
	}
	isolated.Stdout, isolated.Stderr = cmd.Stdout, cmd.Stderr
	return isolated
}
func goalError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func goalWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Read-only discovery includes modified tracked and nonignored untracked files.
// Every source path and byte is checked again after copying/execution. Only the
// disposable repository gets a new index/commit; the source Git state is untouched.
func goalSnapshot(ctx context.Context, root, destination string) (string, error) {
	sourceFS, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer sourceFS.Close()
	cmd := exec.CommandContext(ctx, "git", "-c", "core.fsmonitor=false", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	data, err := cmd.Output()
	if err != nil {
		return "", errors.New("cannot enumerate candidate files")
	}
	names := strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
	sort.Strings(names)
	seen := map[string]bool{}
	hash := sha256.New()
	for _, name := range names {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if filepath.IsAbs(name) || filepath.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") {
			return "", errors.New("unsafe candidate path")
		}
		for _, part := range strings.Split(filepath.ToSlash(name), "/") {
			if part == ".git" {
				return "", errors.New("candidate contains git metadata path")
			}
		}
		path := filepath.Join(root, name)
		info, err := sourceFS.Lstat(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("candidate path is not a regular file: %s", name)
		}
		real, err := filepath.EvalSymlinks(path)
		if err != nil || real != path {
			return "", errors.New("candidate path traverses symlink")
		}
		input, err := sourceFS.Open(name)
		if err != nil {
			return "", err
		}
		fileHash := sha256.New()
		var output *os.File
		writer := io.Writer(fileHash)
		if destination != "" {
			to := filepath.Join(destination, name)
			if err = os.MkdirAll(filepath.Dir(to), 0700); err == nil {
				output, err = os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
			}
			if err != nil {
				input.Close()
				return "", err
			}
			writer = io.MultiWriter(fileHash, output)
		}
		_, err = io.Copy(writer, input)
		closeErr := input.Close()
		if output != nil {
			if e := output.Close(); err == nil {
				err = e
			}
		}
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "%s\x00%o\x00%x\n", name, info.Mode().Perm()&0111, fileHash.Sum(nil))
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if destination != "" {
		for _, args := range [][]string{{"init", "--quiet"}, {"add", "-A"}, {"-c", "user.name=verification snapshot", "-c", "user.email=verification@localhost", "-c", "core.hooksPath=/dev/null", "commit", "--quiet", "--allow-empty", "-m", "frozen verification candidate"}} {
			cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "-C", destination}, args...)...)
			for _, env := range os.Environ() {
				if !strings.HasPrefix(env, "GIT_") {
					cmd.Env = append(cmd.Env, env)
				}
			}
			cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TEMPLATE_DIR=")
			if err := cmd.Run(); err != nil {
				return "", errors.New("cannot initialize verification snapshot")
			}
		}
		after, err := goalSnapshot(ctx, root, "")
		if err != nil || after != digest {
			return "", errors.New("candidate changed while snapshotting")
		}
	}
	return digest, nil
}

func goalGateCommands(ctx context.Context, gate, root, work string) ([]*exec.Cmd, func(), error) {
	command := func(dir string, args ...string) *exec.Cmd {
		c := exec.CommandContext(ctx, "go", args...)
		c.Dir = dir
		c.Env = os.Environ()
		return c
	}
	cleanup := func() {}
	switch gate {
	case "source":
		native := command(root, "test", "./codexsdk/internal/codexexec", "-run", "^TestNativeGoalSandboxBoundary$", "-count=1", "-json")
		native.Env = append(native.Env, "LLMGO_NATIVE_GOAL_SANDBOX=1")
		contracts := command(root, "test", "./codexsdk/internal/protocolsync", "-run", "^TestLiveRepairSandboxGoalContractsUseCandidateChecker$", "-count=1", "-json")
		contracts.Env = append(contracts.Env, "LLMGO_REPAIR_SANDBOX_TEST=1")
		return []*exec.Cmd{command(root, "run", "github.com/rhysd/actionlint/cmd/actionlint@v1.7.12"), command(root, "mod", "tidy", "-diff"), native, contracts, command(root, "vet", "./..."), command(root, "test", "-race", "./...")}, cleanup, nil
	case "generated":
		isolated := filepath.Join(work, "regenerated")
		if err := goalGeneratedInputs(root, isolated); err != nil {
			return nil, cleanup, err
		}
		return []*exec.Cmd{command(filepath.Join(root, "codexsdk"), "run", "./internal/cmd/generatedcheck"), command(root, "run", "./codexsdk/internal/cmd/generatedcheck", "-module-root", filepath.Join(isolated, "codexsdk"), "-write-artifacts"), command(isolated, "build", "./codexsdk/protocolv2")}, cleanup, nil
	case "live":
		return []*exec.Cmd{command(root, "run", "./internal/tools/cmd/livecodex", "run")}, cleanup, nil
	case "provenance":
		return []*exec.Cmd{command(root, "run", "./codexsdk/internal/cmd/protocolupgrade", "verify-provenance-offline", "-repo-root", root)}, cleanup, nil
	}
	return nil, cleanup, errors.New("unknown verification gate")
}
func goalGeneratedInputs(root, destination string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		keep := rel == "go.mod" || rel == "go.sum" || strings.HasPrefix(rel, "codexsdk/internal/protocolschema/appserver/v2/") || strings.HasPrefix(rel, "codexsdk/internal/wirejson/")
		if filepath.Dir(rel) == "codexsdk/protocolv2" && strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, ".gen.go") && !strings.HasSuffix(rel, "_test.go") {
			keep = true
		}
		if !keep {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("generated input is not a regular file")
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		to := filepath.Join(destination, filepath.FromSlash(rel))
		if err = os.MkdirAll(filepath.Dir(to), 0700); err != nil {
			return err
		}
		return os.WriteFile(to, b, info.Mode().Perm())
	})
}

type goalDiagnostics struct {
	file    *os.File
	bytes   int
	err     error
	secrets []string
	keep    int
}

func newGoalDiagnostics(dir string) (*goalDiagnostics, error) {
	f, err := os.CreateTemp(dir, "verification-*.jsonl")
	if err != nil {
		return nil, err
	}
	d := &goalDiagnostics{file: f, keep: utf8.UTFMax}
	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")
		if len(value) >= 8 && (strings.Contains(name, "KEY") || strings.Contains(name, "TOKEN") || strings.Contains(name, "SECRET") || name == "CODEX_RESPONSES_API_ENDPOINT") {
			d.secrets = append(d.secrets, value)
			if len(value) > d.keep {
				d.keep = len(value)
			}
		}
	}
	sort.Slice(d.secrets, func(i, j int) bool { return len(d.secrets[i]) > len(d.secrets[j]) })
	if d.redact(f.Name()) != f.Name() {
		f.Close()
		os.Remove(f.Name())
		return nil, errors.New("diagnostic path contains configured secret")
	}
	return d, nil
}
func (d *goalDiagnostics) redact(s string) string {
	for _, secret := range d.secrets {
		s = strings.ReplaceAll(s, secret, "[redacted]")
	}
	return s
}
func (d *goalDiagnostics) record(kind, gate string, value map[string]any) {
	if d.err != nil {
		return
	}
	value["kind"] = kind
	value["gate"] = gate
	// Decode structured strings before matching secrets; JSON escaping cannot hide
	// a credential in an argument, path or diagnostic payload.
	var scrub func(any) any
	scrub = func(v any) any {
		switch x := v.(type) {
		case string:
			return d.redact(x)
		case []string:
			out := make([]string, len(x))
			for i := range x {
				out[i] = d.redact(x[i])
			}
			return out
		case []any:
			out := make([]any, len(x))
			for i := range x {
				out[i] = scrub(x[i])
			}
			return out
		case map[string]any:
			out := map[string]any{}
			for k, v := range x {
				out[d.redact(k)] = scrub(v)
			}
			return out
		default:
			return v
		}
	}
	b, err := json.Marshal(scrub(value))
	if err != nil {
		d.err = err
		return
	}
	b = append(b, '\n')
	if d.bytes+len(b) > goalDiagnosticLimit {
		d.err = errors.New("16 MiB diagnostic limit exceeded; evidence incomplete")
		return
	}
	n, err := d.file.Write(b)
	d.bytes += n
	if err != nil {
		d.err = err
	} else if n != len(b) {
		d.err = io.ErrShortWrite
	}
}

type goalOutput struct {
	log        *goalDiagnostics
	gate, tail string
}

func (w *goalOutput) Write(p []byte) (int, error) {
	w.tail += string(p)
	w.flush(false)
	return len(p), nil
}
func (w *goalOutput) flush(final bool) {
	n := len(w.tail)
	if !final {
		n -= w.log.keep
		if n <= 0 {
			return
		}
		for _, secret := range w.log.secrets {
			start := 0
			for {
				at := strings.Index(w.tail[start:], secret)
				if at < 0 {
					break
				}
				at += start
				if at < n && at+len(secret) > n {
					n = at
				}
				start = at + len(secret)
			}
		}
		for n > 0 && !utf8.RuneStart(w.tail[n]) {
			n--
		}
	}
	if n > 0 {
		w.log.record("output", w.gate, map[string]any{"trust": "untrusted_command_output", "output": w.tail[:n]})
		w.tail = w.tail[n:]
	}
}

// VerifyOfflineProvenance reuses the exact existing provenance verifier. Its
// caller owns the offline sandbox and disposable prewarmed cache. A cache ref
// is checked against the recorded upstream ref/kind/peeled commit by Sync.
func VerifyOfflineProvenance(root string) error {
	cache := filepath.Join(root, "codexsdk/.cache/openai-codex")
	if err := goalCheckUpstreamCache(root, cache); err != nil {
		return err
	}
	result, err := Sync(SyncRequest{RepoRoot: root, ModuleRoot: filepath.Join(root, "codexsdk"), UpstreamRepo: defaultRemote, ForceCompare: true, ValidationOnly: true, Lookuper: goalCachedRefs{cache}})
	if err != nil {
		return err
	}
	if result.Outcome != OutcomeExactVerified {
		return errors.New("offline provenance did not establish exact verification")
	}
	return nil
}

// CachedUpstreamLookuper resolves only locally prewarmed official upstream refs.
// It deliberately does not bind to the accepted baseline: generation may target
// a newer explicitly selected ref; final publication owns that target binding.
func CachedUpstreamLookuper(moduleRoot string) (RemoteLookuper, error) {
	cache := filepath.Join(moduleRoot, ".cache/openai-codex")
	origin, err := gitOutput(cache, "remote", "get-url", "origin")
	if err != nil || strings.TrimSpace(origin) != defaultRemote {
		return nil, errors.New("offline upstream blocked: missing official-origin cache")
	}
	return goalCachedRefs{cache}, nil
}

type goalCachedRefs struct{ root string }

func (c goalCachedRefs) LSRemote(remote string, patterns ...string) (string, error) {
	if remote != defaultRemote {
		return "", errors.New("offline provenance upstream origin mismatch")
	}
	data, err := exec.Command("git", "-C", c.root, "show-ref", "--dereference").Output()
	if err != nil {
		return "", errors.New("offline provenance cached refs unavailable")
	}
	var result strings.Builder
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		for _, pattern := range patterns {
			if fields[1] == pattern {
				result.WriteString(line + "\n")
				break
			}
		}
	}
	return result.String(), nil
}
func goalCheckUpstreamCache(root, cache string) error {
	origin, err := gitOutput(cache, "remote", "get-url", "origin")
	if err != nil || strings.TrimSpace(origin) != defaultRemote {
		return errors.New("offline provenance blocked: missing prewarmed official-origin cache")
	}
	baseline, err := loadBaselineIdentity(filepath.Join(root, "codexsdk", defaultBaselineRel, "baseline_metadata.json"))
	if err != nil {
		return err
	}
	target, err := ResolveUpstream(ResolveRequest{Remote: defaultRemote, UpstreamRef: baseline.SourceRefName, Lookuper: goalCachedRefs{cache}})
	if err != nil {
		return fmt.Errorf("offline provenance blocked: exact ref missing from cache: %w", err)
	}
	if target.PeeledCommitSHA != baseline.SourceCommit || target.RefKind != baseline.SourceRefKind {
		return errors.New("offline provenance blocked: cached ref differs from recorded upstream identity")
	}
	if err := exec.Command("git", "-C", cache, "cat-file", "-e", baseline.SourceCommit+"^{commit}").Run(); err != nil {
		return errors.New("offline provenance blocked: exact commit object missing")
	}
	return nil
}
func goalPrepareOfflineCache(ctx context.Context, from, snapshot, work string) error {
	original := filepath.Join(from, "openai-codex")
	if err := goalCheckUpstreamCache(snapshot, original); err != nil {
		return err
	}
	to := filepath.Join(snapshot, "codexsdk/.cache")
	if err := os.MkdirAll(to, 0700); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", "-c", "core.hooksPath=/dev/null", "clone", "--quiet", "--no-checkout", "--no-hardlinks", original, filepath.Join(to, "openai-codex"))
	if err := cmd.Run(); err != nil {
		return errors.New("offline provenance blocked: cannot copy prewarmed upstream")
	}
	if err := runGit(filepath.Join(to, "openai-codex"), "remote", "set-url", "origin", defaultRemote); err != nil {
		return err
	}
	if err := goalCheckUpstreamCache(snapshot, filepath.Join(to, "openai-codex")); err != nil {
		return err
	}
	for _, name := range []string{"credentials", "credentials.toml"} {
		if _, e := os.Lstat(filepath.Join(from, "cargo-home", name)); !os.IsNotExist(e) {
			return errors.New("offline provenance blocked: Cargo cache contains credentials")
		}
	}
	for _, name := range []string{"rustup", "cargo-home"} {
		if err := goalCopyCache(ctx, filepath.Join(from, name), filepath.Join(to, name)); err != nil {
			return fmt.Errorf("offline provenance blocked: prewarmed %s: %w", name, err)
		}
	}
	tool, err := exec.LookPath("rustup")
	if err != nil {
		return errors.New("offline provenance blocked: rustup prerequisite unavailable")
	}
	tool, err = filepath.EvalSymlinks(tool)
	if err != nil {
		return err
	}
	tools := filepath.Join(work, "rust-tools")
	if err = os.MkdirAll(tools, 0700); err != nil {
		return err
	}
	input, err := os.Open(tool)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(filepath.Join(tools, "rustup"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	_, err = io.Copy(output, input)
	closeErr := output.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	for _, name := range []string{"cargo", "rustc", "rustdoc"} {
		if err = os.Symlink("rustup", filepath.Join(tools, name)); err != nil {
			return err
		}
	}
	return nil
}
func goalCopyCache(ctx context.Context, source, destination string) error {
	sourceFS, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer sourceFS.Close()
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		to := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(to, 0700)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(rel), link))
			if filepath.IsAbs(link) || resolved == ".." || strings.HasPrefix(resolved, "../") {
				return errors.New("prewarmed cache symlink escapes source")
			}
			return os.Symlink(link, to)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("prewarmed cache contains special file")
		}
		input, err := sourceFS.Open(rel)
		if err != nil {
			return err
		}
		defer input.Close()
		output, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, err = io.Copy(output, input)
		closeErr := output.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}

func goalNativeCommand(cmd *exec.Cmd) bool {
	return goalNativeTest(cmd) != ""
}

func goalNativeTest(cmd *exec.Cmd) string {
	for _, arg := range cmd.Args {
		switch arg {
		case "^TestNativeGoalSandboxBoundary$":
			return "TestNativeGoalSandboxBoundary"
		case "^TestLiveRepairSandboxGoalContractsUseCandidateChecker$":
			return "TestLiveRepairSandboxGoalContractsUseCandidateChecker"
		}
	}
	return ""
}

type goalNativeEvidence struct {
	test                     string
	pending                  string
	run, pass, skip, invalid bool
}

func (p *goalNativeEvidence) Write(b []byte) (int, error) {
	p.pending += string(b)
	if len(p.pending) > goalDiagnosticLimit {
		p.invalid = true
		p.pending = ""
		return len(b), nil
	}
	for {
		line, rest, ok := strings.Cut(p.pending, "\n")
		if !ok {
			break
		}
		p.pending = rest
		var event struct{ Action, Test string }
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		if event.Test != p.test {
			continue
		}
		switch event.Action {
		case "run":
			p.run = true
		case "pass":
			p.pass = true
		case "skip", "fail":
			p.skip = true
		}
	}
	return len(b), nil
}
func goalRunCommand(cmd *exec.Cmd, gate string, log *goalDiagnostics, run goalCommandRunner) error {
	log.record("command", gate, map[string]any{"argv": cmd.Args, "trust": "controller_command"})
	output := &goalOutput{log: log, gate: gate}
	cmd.Stdout = output
	cmd.Stderr = output
	native := goalNativeEvidence{test: goalNativeTest(cmd)}
	if goalNativeCommand(cmd) {
		writer := io.MultiWriter(output, &native)
		cmd.Stdout = writer
		cmd.Stderr = writer
	}
	started := time.Now()
	err := run(cmd)
	elapsedMS := time.Since(started).Milliseconds()
	output.flush(true)
	if err == nil && goalNativeCommand(cmd) && (!native.run || !native.pass || native.skip || native.invalid || native.pending != "") {
		err = errors.New("required native test did not produce a complete executed pass")
	}
	// Observe costs without caching acceptance or repeating a provider call.
	log.record("exit", gate, map[string]any{"passed": err == nil, "error": goalError(err), "duration_ms": elapsedMS})
	if log.err != nil {
		return &GoalVerificationIncompleteError{Err: log.err}
	}
	if err != nil {
		return fmt.Errorf("%s verification failed: %w", gate, err)
	}
	return nil
}

// VerifyCIGate runs one existing gate in its established CI job trust context.
// It is intentionally separate from VerifyGoal: the fixed goal tool must only
// expose the sandboxed local function. This function neither grants credentials
// nor starts a goal; Live uses the existing CI-only livecodex primitive.
func VerifyCIGate(ctx context.Context, req GoalVerifyRequest) (GoalVerifyResult, error) {
	return verifyCIGate(ctx, req, nil)
}
func verifyCIGate(ctx context.Context, req GoalVerifyRequest, run goalCommandRunner, contract ...goalContractRunner) (result GoalVerifyResult, retErr error) {
	nativeExecution := run == nil
	if run == nil {
		run = func(cmd *exec.Cmd) error { return cmd.Run() }
	}

	result.Scope = "ci"
	if req.Gate != "source" && req.Gate != "generated" && req.Gate != "provenance" && req.Gate != "live" {
		return result, errors.New("CI verification requires one explicit gate")
	}
	root, err := filepath.Abs(req.RepoRoot)
	if err != nil {
		return result, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return result, err
	}
	if err = AssertClean(root); err != nil {
		return result, err
	}
	head, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil {
		return result, err
	}
	result.Head = strings.TrimSpace(head)
	before, err := goalSnapshot(ctx, root, "")
	if err != nil {
		return result, err
	}
	result.SnapshotDigest = before
	dir := req.DiagnosticDir
	if dir == "" {
		dir, err = os.MkdirTemp("", "llm-go-ci-diagnostics-")
	} else {
		err = os.MkdirAll(dir, 0700)
	}
	if err != nil {
		return result, err
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return result, err
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil || real != dir || goalWithin(root, dir) {
		return result, errors.New("CI diagnostics must be outside checkout without symlinks")
	}
	log, err := newGoalDiagnostics(dir)
	if err != nil {
		return result, err
	}
	result.DiagnosticPath = log.file.Name()
	log.record("ci_identity", req.Gate, map[string]any{"head": result.Head, "snapshot_sha256": before, "scope": "ci"})
	defer func() {
		log.record("ci_result", req.Gate, map[string]any{"head": result.Head, "snapshot_sha256": before, "scope": "ci", "complete": log.err == nil, "passed": retErr == nil, "error": goalError(retErr)})
		if e := log.file.Close(); e != nil && log.err == nil {
			log.err = e
		}
		if log.err != nil {
			retErr = &GoalVerificationIncompleteError{Err: log.err}
		}
	}()
	work, err := os.MkdirTemp("", "llm-go-ci-gate-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(work)
	unchanged := func() error {
		after, e := goalSnapshot(ctx, root, "")
		h, he := gitOutput(root, "rev-parse", "HEAD")
		if e != nil || he != nil {
			return errors.New("CI candidate revalidation unavailable")
		}
		if strings.TrimSpace(h) != result.Head || after != before {
			return errors.New("CI gate changed exact candidate")
		}
		return AssertClean(root)
	}
	defer func() { retErr = errors.Join(retErr, unchanged()) }()
	if req.Gate == "source" && nativeExecution {
		snapshot := filepath.Join(work, "native-boundary")
		if _, err = goalSnapshot(ctx, root, snapshot); err != nil {
			return result, err
		}
		retained := filepath.Join(work, "retained")
		if err = os.MkdirAll(retained, 0700); err != nil {
			return result, err
		}
		if err = os.MkdirAll(filepath.Join(snapshot, "codexsdk/.cache/openai-codex"), 0700); err != nil {
			return result, err
		}
		sandbox, err := newOracleSandbox(ctx, snapshot, retained)
		if err != nil {
			return result, err
		}
		binary := os.Getenv("CODEX_BINARY")
		if binary == "" {
			binary = "codex"
		}
		binary, err = exec.LookPath(binary)
		if err != nil {
			return result, errors.New("CI source requires the installed native runtime")
		}
		if err = goalNativeRuntimeInputs(&sandbox, binary); err != nil {
			return result, err
		}
		// Exercise the same composition as the real goal before starting another
		// model run. Other CI commands retain their established CI trust context.
		run = func(cmd *exec.Cmd) error {
			if goalNativeTest(cmd) != "" {
				return goalSandboxCommand(ctx, sandbox, cmd, binary, os.Getenv("CODEX_CLI_VERSION")).Run()
			}
			return cmd.Run()
		}
		log.record("native_boundary", "source", map[string]any{"passed": true, "trust": "controller_native_probe"})
	}
	commands, cleanup, err := goalGateCommands(ctx, req.Gate, root, work)
	if err != nil {
		return result, err
	}
	defer cleanup()
	if req.Gate == "provenance" {
		command := exec.CommandContext(ctx, "go", "run", "./codexsdk/internal/cmd/protocolupgrade", "sync", "-repo-root", root, "-module-root", filepath.Join(root, "codexsdk"), "-upstream-repo", defaultRemote, "-force-compare", "-validation-only")
		command.Dir = root
		commands = []*exec.Cmd{command}
	}
	for _, cmd := range commands {
		if err = goalRunCommand(cmd, req.Gate, log, run); err != nil {
			return result, err
		}
		if err = unchanged(); err != nil {
			return result, err
		}
	}
	if req.Gate == "provenance" {
		if nativeExecution {
			req.CandidateDir, err = goalFreshCIContractFacts(ctx, root, work)
			if err != nil {
				return result, err
			}
		}
		if err = goalRunContracts(ctx, req, root, work, log, contract...); err != nil {
			return result, err
		}
	}
	result.Gates = []string{req.Gate}
	return result, nil
}

type goalContractRunner func(context.Context, GoalContractRequest) error

// The host chooses the accepted baseline and rebuilt official upstream facts.
// Only sanitized disposable copies cross into candidate execution.
func goalRunContracts(ctx context.Context, req GoalVerifyRequest, candidate, work string, log *goalDiagnostics, injected ...goalContractRunner) error {
	if req.OriginalBaseRoot != "" && req.OriginalBaseRoot != req.BaseRoot {
		original := req.OriginalBaseRoot
		req.OriginalBaseRoot = ""
		var failures []error
		for i, base := range []string{req.BaseRoot, original} {
			lane := filepath.Join(work, fmt.Sprintf("baseline-%d", i))
			if err := os.MkdirAll(lane, 0700); err != nil {
				return err
			}
			req.BaseRoot = base
			if err := goalRunContracts(ctx, req, candidate, lane, log, injected...); err != nil {
				failures = append(failures, fmt.Errorf("accepted baseline lane %d: %w", i, err))
			}
		}
		return errors.Join(failures...)
	}

	root, err := filepath.Abs(req.RepoRoot)
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	for _, path := range []string{req.BaseRoot, req.CandidateDir} {
		if path == "" || !filepath.IsAbs(path) {
			return errors.New("provenance contract blocked: accepted base and rebuilt upstream directories are required")
		}
		real, err := filepath.EvalSymlinks(path)
		if err != nil || real != filepath.Clean(path) || goalWithin(root, path) || goalWithin(path, root) {
			return errors.New("provenance contract inputs must be external trusted paths without symlink indirection")
		}
	}
	if err := AssertClean(req.BaseRoot); err != nil {
		return fmt.Errorf("accepted baseline is not clean: %w", err)
	}
	base := filepath.Join(work, "accepted-contract-base")
	baseDigest, err := goalSnapshot(ctx, req.BaseRoot, base)
	if err != nil {
		return err
	}
	// Retain only the new synthetic Git metadata needed by baseline cloning;
	// the original checkout configuration, remotes and credentials are not copied.
	facts := filepath.Join(work, "rebuilt-contract-inputs")
	if err := goalCopyContractFacts(ctx, req.CandidateDir, facts); err != nil {
		return err
	}
	run := goalContractRunner(VerifyGoalContracts)
	if len(injected) != 0 {
		run = injected[0]
	}
	log.record("contract", "provenance", map[string]any{"trust": "controller_command", "base": "accepted snapshot", "base_sha256": baseDigest, "upstream": "rebuilt official facts"})
	output := &goalOutput{log: log, gate: "provenance"}
	err = run(ctx, GoalContractRequest{RepoRoot: candidate, BaseRoot: base, CandidateDir: facts, Output: output})
	output.flush(true)
	log.record("contract_exit", "provenance", map[string]any{"passed": err == nil, "error": goalError(err)})
	if log.err != nil {
		return &GoalVerificationIncompleteError{Err: log.err}
	}
	return err
}

func goalCopyContractFacts(ctx context.Context, source, destination string) error {
	if err := goalCopyCache(ctx, filepath.Join(source, "schema"), filepath.Join(destination, "schema")); err != nil {
		return err
	}
	input, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Lstat("common.rs")
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("rebuilt upstream common.rs must be a regular file")
	}
	data, err := input.ReadFile("common.rs")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(destination, "common.rs"), data, 0600)
}

// Called only after this CI gate's native force-compare build has succeeded.
func goalFreshCIContractFacts(ctx context.Context, root, work string) (string, error) {
	baseline, err := loadBaselineIdentity(filepath.Join(root, "codexsdk", defaultBaselineRel, "baseline_metadata.json"))
	if err != nil {
		return "", err
	}
	sha := baseline.SourceCommit
	if len(sha) != 40 {
		return "", errors.New("invalid rebuilt upstream commit")
	}
	if _, err := hex.DecodeString(sha); err != nil {
		return "", err
	}
	source := filepath.Join(root, "codexsdk/.cache/codexsdk-upstream-"+sha[:12])
	real, err := filepath.EvalSymlinks(source)
	if err != nil || real != source {
		return "", errors.New("rebuilt upstream facts path is not canonical")
	}
	marker, err := os.ReadFile(filepath.Join(source, "common.rs.source_sha"))
	if err != nil || strings.TrimSpace(string(marker)) != sha {
		return "", errors.New("rebuilt upstream facts commit mismatch")
	}
	destination := filepath.Join(work, "fresh-ci-upstream")
	if err := goalCopyContractFacts(ctx, source, destination); err != nil {
		return "", err
	}
	return destination, nil
}
