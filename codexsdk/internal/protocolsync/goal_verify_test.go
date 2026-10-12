package protocolsync

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func goalFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range map[string]string{"go.mod": "module fixture\ngo 1.26.8\n", "go.sum": "", "tracked.go": "package fixture\n", "codexsdk/protocolv2/manual.go": "package protocolv2\n", "codexsdk/protocolv2/test.gen.go": "package protocolv2\n", "codexsdk/internal/protocolschema/appserver/v2/schema.json": "{}", "codexsdk/internal/wirejson/wire.go": "package wirejson\n"} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=test", "-c", "user.email=test@local", "commit", "-qm", "initial"}} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatal(e, string(b))
		}
	}
	return root
}
func goalState(t *testing.T, root string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", root, "status", "--porcelain=v2", "--untracked-files=all")
	b, e := cmd.Output()
	if e != nil {
		t.Fatal(e)
	}
	head, e := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if e != nil {
		t.Fatal(e)
	}
	index, e := os.ReadFile(filepath.Join(root, ".git/index"))
	if e != nil {
		t.Fatal(e)
	}
	return string(head) + string(b) + string(index)
}
func TestGoalVerifyAllMapsNativeGatesAndPreservesDirtySource(t *testing.T) {
	root := goalFixture(t)
	os.WriteFile(filepath.Join(root, "tracked.go"), []byte("package fixture // dirty\n"), 0600)
	os.WriteFile(filepath.Join(root, "new.go"), []byte("package fixture // intended untracked\n"), 0600)
	before := goalState(t, root)
	var commands []string
	result, err := verifyGoal(context.Background(), goalContractFixtureRequest(t, root, ""), func(cmd *exec.Cmd) error {
		commands = append(commands, strings.Join(cmd.Args[1:], " "))
		if strings.Contains(strings.Join(cmd.Args, " "), "vet") {
			if b, e := os.ReadFile(filepath.Join(cmd.Dir, "new.go")); e != nil || !strings.Contains(string(b), "untracked") {
				t.Fatal("lost intended source", e)
			}
			out, e := exec.Command("git", "-C", cmd.Dir, "status", "--porcelain").Output()
			if e != nil || len(out) != 0 {
				t.Fatal("snapshot is not clean", string(out), e)
			}
		}
		if goalNativeCommand(cmd) {
			return goalTestSuccess(cmd)
		}
		native := exec.Command("sh", "-c", "printf 'native command ran\\n'")
		native.Stdout = cmd.Stdout
		native.Stderr = cmd.Stderr
		return native.Run()
	}, goalTestContract)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()
	if !reflect.DeepEqual(result.Gates, goalVerificationGates) {
		t.Fatal(result.Gates)
	}
	joined := strings.Join(commands, "\n")
	for _, want := range []string{"actionlint@v1.7.12", "mod tidy -diff", "vet ./...", "test -race ./...", "generatedcheck -module-root", "build ./codexsdk/protocolv2", "verify-provenance-offline"} {
		if !strings.Contains(joined, want) {
			t.Fatal("missing shared command", want, joined)
		}
	}
	if before != goalState(t, root) {
		t.Fatal("source/index/history mutated")
	}
	if goalWithin(root, result.DiagnosticPath) {
		t.Fatal("diagnostics exposed in candidate")
	}
}
func TestGoalVerifyStopsOnExitAndDetectsSnapshotMutation(t *testing.T) {
	for _, mutate := range []bool{false, true} {
		t.Run(map[bool]string{false: "exit", true: "mutation"}[mutate], func(t *testing.T) {
			root := goalFixture(t)
			calls := 0
			result, err := verifyGoal(context.Background(), GoalVerifyRequest{RepoRoot: root, Gate: "source", DiagnosticDir: t.TempDir()}, func(cmd *exec.Cmd) error {
				calls++
				if mutate {
					return os.WriteFile(filepath.Join(cmd.Dir, "tracked.go"), []byte("changed"), 0600)
				}
				return errors.New("native failure")
			})
			if err == nil || calls != 1 || len(result.Gates) != 0 {
				t.Fatal("false acceptance", calls, result, err)
			}
			if _, e := os.Stat(result.DiagnosticPath); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestGoalVerifyRejectsSymlinksAndInsideDiagnostics(t *testing.T) {
	root := goalFixture(t)
	if err := os.Symlink(filepath.Join(root, "tracked.go"), filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	calls := 0
	_, err := verifyGoal(context.Background(), GoalVerifyRequest{RepoRoot: root, Gate: "source", DiagnosticDir: t.TempDir()}, func(*exec.Cmd) error { calls++; return nil })
	if err == nil || calls != 0 {
		t.Fatal("symlink accepted")
	}
	_, err = VerifyGoal(context.Background(), GoalVerifyRequest{RepoRoot: root, DiagnosticDir: filepath.Join(root, "logs")})
	if err == nil {
		t.Fatal("inside diagnostics accepted")
	}
}
func TestGoalDiagnosticsSplitSecretsAndBound(t *testing.T) {
	secret := "secret-quote\"and-slash\\value"
	t.Setenv("GOAL_TEST_SECRET", secret)
	d, e := newGoalDiagnostics(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	w := &goalOutput{log: d, gate: "source"}
	io.WriteString(w, "prefix "+secret[:12])
	io.WriteString(w, secret[12:]+" suffix")
	w.flush(true)
	d.record("result", "source", map[string]any{"complete": true, "path": secret})
	d.file.Close()
	data, e := os.ReadFile(d.file.Name())
	if e != nil {
		t.Fatal(e)
	}
	var reconstructed strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var row map[string]any
		if e = json.Unmarshal([]byte(line), &row); e != nil {
			t.Fatal(e)
		}
		if s, ok := row["output"].(string); ok {
			reconstructed.WriteString(s)
		}
		if row["path"] == secret {
			t.Fatal("structured secret leaked")
		}
	}
	if strings.Contains(reconstructed.String(), secret) || !strings.Contains(reconstructed.String(), "[redacted]") {
		t.Fatal(reconstructed.String())
	}
	d, e = newGoalDiagnostics(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer d.file.Close()
	d.record("output", "source", map[string]any{"output": strings.Repeat("x", goalDiagnosticLimit)})
	if d.err == nil {
		t.Fatal("oversize evidence accepted")
	}
	info, _ := d.file.Stat()
	if info.Size() > goalDiagnosticLimit {
		t.Fatal("file bound exceeded")
	}
}
func TestGoalGateSelectorsRejectUnknown(t *testing.T) {
	_, e := VerifyGoal(context.Background(), GoalVerifyRequest{Gate: "pretend"})
	if e == nil {
		t.Fatal("unknown gate accepted")
	}
	for _, gate := range goalVerificationGates {
		root := goalFixture(t)
		r, e := verifyGoal(context.Background(), goalContractFixtureRequest(t, root, gate), goalTestSuccess, goalTestContract)
		defer r.Close()
		if e != nil || !reflect.DeepEqual(r.Gates, []string{gate}) {
			t.Fatal(gate, r, e)
		}
	}
}

func TestGoalPublicLiveAndProvenanceRemainBlocked(t *testing.T) {
	for _, gate := range []string{"all", "live"} {
		root := goalFixture(t)
		r, err := VerifyGoal(context.Background(), GoalVerifyRequest{RepoRoot: root, Gate: gate, DiagnosticDir: t.TempDir()})
		if err == nil || !strings.Contains(err.Error(), "unknown verification gate") || len(r.Gates) != 0 {
			t.Fatal("unimplemented authority reported acceptance", gate, r, err)
		}
	}
}
func TestGoalSnapshotSourceMutationAndDiagnosticsFailure(t *testing.T) {
	root := goalFixture(t)
	r, err := verifyGoal(context.Background(), GoalVerifyRequest{RepoRoot: root, Gate: "source", DiagnosticDir: t.TempDir()}, func(cmd *exec.Cmd) error {
		return os.WriteFile(filepath.Join(root, "tracked.go"), []byte("concurrent source change"), 0600)
	})
	if err == nil || !strings.Contains(err.Error(), "candidate changed") {
		t.Fatal(r, err)
	}
	root = goalFixture(t)
	r, err = verifyGoal(context.Background(), GoalVerifyRequest{RepoRoot: root, Gate: "source", DiagnosticDir: t.TempDir()}, func(cmd *exec.Cmd) error {
		_, e := io.WriteString(cmd.Stdout, strings.Repeat("failure", goalDiagnosticLimit/7+1))
		return e
	})
	var incomplete *GoalVerificationIncompleteError
	if !errors.As(err, &incomplete) || len(r.Gates) > 0 {
		t.Fatal("incomplete diagnostics accepted", r, err)
	}
}

func TestGoalDiagnosticsDoNotRewriteCommandArguments(t *testing.T) {
	secret := "credential-value"
	t.Setenv("GOAL_TEST_SECRET", secret)
	d, e := newGoalDiagnostics(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer d.file.Close()
	args := []string{"go", secret}
	d.record("command", "source", map[string]any{"argv": args})
	if args[1] != secret {
		t.Fatal("redaction changed actual command")
	}
}

func TestGoalOfflineCachePinsOfficialRefAndCommit(t *testing.T) {
	cache := goalFixture(t)
	if err := runGit(cache, "remote", "add", "origin", defaultRemote); err != nil {
		t.Fatal(err)
	}
	if err := runGit(cache, "-c", "user.name=test", "-c", "user.email=test@local", "tag", "-a", "rust-v0.140.0", "-m", "exact source"); err != nil {
		t.Fatal(err)
	}
	sha, err := gitOutput(cache, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	root := initSyncRepo(t, strings.TrimSpace(sha), "rust-v0.140.0", KindStableTag)
	if err = goalCheckUpstreamCache(root, cache); err != nil {
		t.Fatal(err)
	}
	if _, err = (goalCachedRefs{cache}).LSRemote("https://foreign.invalid"); err == nil {
		t.Fatal("foreign origin accepted")
	}
	if err = runGit(cache, "remote", "set-url", "origin", "https://foreign.invalid"); err != nil {
		t.Fatal(err)
	}
	if err = goalCheckUpstreamCache(root, cache); err == nil {
		t.Fatal("foreign cached origin accepted")
	}
	runGit(cache, "remote", "set-url", "origin", defaultRemote)
	if err = runGit(cache, "tag", "-d", "rust-v0.140.0"); err != nil {
		t.Fatal(err)
	}
	if err = goalCheckUpstreamCache(root, cache); err == nil {
		t.Fatal("commit without recorded ref accepted")
	}
}
func TestGoalPrewarmedCachesAreDisposableAndConfined(t *testing.T) {
	source := goalFixture(t)
	cache := filepath.Join(source, "codexsdk/.cache/openai-codex")
	upstream := goalFixture(t)
	if out, err := exec.Command("git", "clone", "--quiet", "--no-hardlinks", upstream, cache).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	runGit(cache, "remote", "set-url", "origin", defaultRemote)
	runGit(cache, "tag", "rust-v0.140.0")
	sha, _ := gitOutput(cache, "rev-parse", "HEAD")
	baseline := filepath.Join(source, "codexsdk", defaultBaselineRel, "baseline_metadata.json")
	writeFile(t, baseline, mustJSON(t, map[string]string{"source_commit": strings.TrimSpace(sha), "source_ref_name": "rust-v0.140.0", "source_ref_kind": KindStableTag}))
	for _, name := range []string{"rustup", "cargo-home"} {
		writeFile(t, filepath.Join(source, "codexsdk/.cache", name, "marker"), "original")
	}
	tools := t.TempDir()
	writeFile(t, filepath.Join(tools, "rustup"), "#!/bin/sh\nexit 99\n")
	os.Chmod(filepath.Join(tools, "rustup"), 0700)
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	snapshot := t.TempDir()
	writeFile(t, filepath.Join(snapshot, "codexsdk", defaultBaselineRel, "baseline_metadata.json"), mustJSON(t, map[string]string{"source_commit": strings.TrimSpace(sha), "source_ref_name": "rust-v0.140.0", "source_ref_kind": KindStableTag}))
	before := goalState(t, source)
	if err := goalPrepareOfflineCache(context.Background(), filepath.Join(source, "codexsdk/.cache"), snapshot, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"rustup", "cargo-home"} {
		os.WriteFile(filepath.Join(snapshot, "codexsdk/.cache", name, "marker"), []byte("private write"), 0600)
		b, err := os.ReadFile(filepath.Join(source, "codexsdk/.cache", name, "marker"))
		if err != nil || string(b) != "original" {
			t.Fatal("original cache changed")
		}
	}
	privateRepo := filepath.Join(snapshot, "codexsdk/.cache/openai-codex")
	if err := runGit(privateRepo, "remote", "set-url", "origin", "https://private.invalid"); err != nil {
		t.Fatal(err)
	}
	origin, err := gitOutput(cache, "remote", "get-url", "origin")
	if err != nil || strings.TrimSpace(origin) != defaultRemote {
		t.Fatal("original upstream Git cache changed")
	}
	if before != goalState(t, source) {
		t.Fatal("source Git state changed during preparation")
	}
	escape := t.TempDir()
	os.Symlink("../outside", filepath.Join(escape, "link"))
	if err := goalCopyCache(context.Background(), escape, t.TempDir()); err == nil {
		t.Fatal("escaping cache link accepted")
	}
}
func TestGoalLocalScopeAndSnapshotOwnership(t *testing.T) {
	root := goalFixture(t)
	r, err := verifyGoal(context.Background(), goalContractFixtureRequest(t, root, "local"), goalTestSuccess, goalTestContract)
	if err != nil {
		t.Fatal(err)
	}
	if r.Scope != "local" || len(r.Gates) != 3 || r.SnapshotRoot == "" {
		t.Fatal("scope or ownership", r)
	}
	frozen := r.SnapshotRoot
	digest, err := goalSnapshot(context.Background(), frozen, "")
	if err != nil || digest != r.SnapshotDigest {
		t.Fatal("published bytes not frozen")
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(frozen); !os.IsNotExist(err) {
		t.Fatal("snapshot not released")
	}
}

func TestGoalDiagnosticPathCannotExposeConfiguredSecret(t *testing.T) {
	secret := "secret-directory-value"
	t.Setenv("GOAL_TEST_SECRET", secret)
	dir := filepath.Join(t.TempDir(), secret)
	os.Mkdir(dir, 0700)
	if _, err := newGoalDiagnostics(dir); err == nil {
		t.Fatal("secret-bearing diagnostic path accepted")
	}
}

func goalTestSuccess(cmd *exec.Cmd) error {
	if goalNativeCommand(cmd) {
		name := goalNativeTest(cmd)
		_, err := io.WriteString(cmd.Stdout, "{\"Action\":\"run\",\"Test\":\""+name+"\"}\n{\"Action\":\"pass\",\"Test\":\""+name+"\"}\n")
		return err
	}
	return nil
}
func TestGoalNativeEvidenceRejectsMissingOrSkipped(t *testing.T) {
	for _, output := range []string{"", `{"Action":"skip","Test":"TestNativeGoalSandboxBoundary"}` + "\n"} {
		d, err := newGoalDiagnostics(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("go", "test", "-run", "^TestNativeGoalSandboxBoundary$")
		err = goalRunCommand(cmd, "source", d, func(cmd *exec.Cmd) error { io.WriteString(cmd.Stdout, output); return nil })
		d.file.Close()
		if err == nil {
			t.Fatal("native proof accepted without execution")
		}
	}
}
func TestGoalCIIdentityAndSeparateLivePrimitive(t *testing.T) {
	for _, gate := range []string{"source", "generated", "provenance", "live"} {
		root := goalFixture(t)
		var calls []string
		r, err := verifyCIGate(context.Background(), goalContractFixtureRequest(t, root, gate), func(cmd *exec.Cmd) error {
			calls = append(calls, strings.Join(cmd.Args, " "))
			return goalTestSuccess(cmd)
		}, goalTestContract)
		if err != nil {
			t.Fatal(gate, err)
		}
		data, err := os.ReadFile(r.DiagnosticPath)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		var header, footer map[string]any
		json.Unmarshal([]byte(lines[0]), &header)
		json.Unmarshal([]byte(lines[len(lines)-1]), &footer)
		if header["kind"] != "ci_identity" || header["head"] != r.Head || footer["kind"] != "ci_result" || footer["head"] != r.Head || footer["passed"] != true || footer["complete"] != true {
			t.Fatal("CI identity mismatch", header, footer)
		}
		if gate == "live" && !strings.Contains(strings.Join(calls, " "), "livecodex run") {
			t.Fatal("existing Live primitive lost")
		}
		if gate == "provenance" && !strings.Contains(strings.Join(calls, " "), "-force-compare -validation-only") {
			t.Fatal("existing exact provenance lost")
		}
	}
}

func TestGoalCachedLookupAllowsSelectedNewerTarget(t *testing.T) {
	root := goalFixture(t)
	cache := filepath.Join(root, "codexsdk/.cache/openai-codex")
	upstream := goalFixture(t)
	if b, e := exec.Command("git", "clone", "--quiet", "--no-hardlinks", upstream, cache).CombinedOutput(); e != nil {
		t.Fatal(e, string(b))
	}
	runGit(cache, "remote", "set-url", "origin", defaultRemote)
	runGit(cache, "tag", "rust-v0.162.1")
	lookup, err := CachedUpstreamLookuper(filepath.Join(root, "codexsdk"))
	if err != nil {
		t.Fatal(err)
	}
	target, err := ResolveUpstream(ResolveRequest{Remote: defaultRemote, UpstreamRef: "rust-v0.162.1", Lookuper: lookup})
	if err != nil || target.RefName != "rust-v0.162.1" {
		t.Fatal(target, err)
	}
}

func TestGoalLocalProvenanceRequiresExternalTrustedCache(t *testing.T) {
	root := goalFixture(t)
	for _, cache := range []string{"", root} {
		r, err := VerifyGoal(context.Background(), GoalVerifyRequest{RepoRoot: root, Gate: "provenance", DiagnosticDir: t.TempDir(), TrustedCacheRoot: cache})
		if err == nil || len(r.Gates) > 0 || r.SnapshotRoot != "" {
			t.Fatal("model workspace accepted as host cache authority", r, err)
		}
	}
}

func TestGoalDiagnosticsNestedDecodedSecrets(t *testing.T) {
	secret := "nested\"secret\\value"
	t.Setenv("GOAL_TEST_SECRET", secret)
	d, err := newGoalDiagnostics(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"values": []any{[]any{secret}, map[string]any{"key": secret}}, "number": json.Number("9007199254740993")})
	var decoded map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	d.record("nested", "source", decoded)
	if d.err != nil {
		t.Fatal(d.err)
	}
	d.file.Close()
	b, err := os.ReadFile(d.file.Name())
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	decoder = json.NewDecoder(strings.NewReader(string(b)))
	decoder.UseNumber()
	if err := decoder.Decode(&got); err != nil {
		t.Fatal(err)
	}
	arrays := got["values"].([]any)
	if arrays[0].([]any)[0] != "[redacted]" || arrays[1].(map[string]any)["key"] != "[redacted]" || got["number"] != json.Number("9007199254740993") {
		t.Fatal(got)
	}
}

func TestGoalSnapshotIgnoresGlobalCommitConfiguration(t *testing.T) {
	root := goalFixture(t)
	config := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(config, []byte("[commit]\n gpgsign = true\n[gpg]\n program = /bin/false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	destination := t.TempDir()
	if _, err := goalSnapshot(context.Background(), root, destination); err != nil {
		t.Fatal(err)
	}
	if _, err := gitOutput(destination, "rev-parse", "HEAD"); err != nil {
		t.Fatal(err)
	}
}

func TestGoalSourceRequiresBothNativeProofs(t *testing.T) {
	commands, cleanup, err := goalGateCommands(context.Background(), "source", t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	seen := map[string]bool{}
	for _, cmd := range commands {
		name := goalNativeTest(cmd)
		if name == "" {
			continue
		}
		seen[name] = true
		flag := "LLMGO_NATIVE_GOAL_SANDBOX=1"
		if name == "TestLiveRepairSandboxGoalContractsUseCandidateChecker" {
			flag = "LLMGO_REPAIR_SANDBOX_TEST=1"
		}
		if !strings.Contains(strings.Join(cmd.Env, "\n"), flag) {
			t.Fatal("missing native opt-in", name)
		}
		for _, action := range []string{"missing", "skip", "wrong-test"} {
			d, err := newGoalDiagnostics(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			err = goalRunCommand(cmd, "source", d, func(cmd *exec.Cmd) error {
				switch action {
				case "skip":
					b, _ := json.Marshal(map[string]string{"Action": "skip", "Test": name})
					_, err := cmd.Stdout.Write(append(b, '\n'))
					return err
				case "wrong-test":
					_, err := io.WriteString(cmd.Stdout, "{\"Action\":\"run\",\"Test\":\"Other\"}\n{\"Action\":\"pass\",\"Test\":\"Other\"}\n")
					return err
				}
				return nil
			})
			d.file.Close()
			if err == nil {
				t.Fatal("native proof accepted", name, action)
			}
		}
	}
	if len(seen) != 2 {
		t.Fatal("missing required proof command", seen)
	}
}

func goalContractFixtureRequest(t *testing.T, root, gate string) GoalVerifyRequest {
	t.Helper()
	facts := t.TempDir()
	if err := os.Mkdir(filepath.Join(facts, "schema"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(facts, "common.rs"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	return GoalVerifyRequest{RepoRoot: root, Gate: gate, DiagnosticDir: t.TempDir(), BaseRoot: goalFixture(t), CandidateDir: facts}
}
func goalTestContract(_ context.Context, req GoalContractRequest) error {
	_, err := io.WriteString(req.Output, "native contract test fixture\n")
	return err
}

func TestGoalProvenanceContractFailureIsRequiredAndComplete(t *testing.T) {
	root := goalFixture(t)
	req := goalContractFixtureRequest(t, root, "provenance")
	config := filepath.Join(req.BaseRoot, ".git/config")
	before, _ := os.ReadFile(config)
	os.WriteFile(config, append(before, []byte("\n[credential]\n helper = secret-helper\n")...), 0600)
	for _, ci := range []bool{false, true} {
		called := false
		verify := verifyGoal
		if ci {
			verify = verifyCIGate
		}
		result, err := verify(context.Background(), req, goalTestSuccess, func(_ context.Context, r GoalContractRequest) error {
			called = true
			if r.BaseRoot == req.BaseRoot || r.CandidateDir == req.CandidateDir {
				t.Fatal("trusted original exposed")
			}
			b, e := os.ReadFile(filepath.Join(r.BaseRoot, ".git/config"))
			if e != nil || strings.Contains(string(b), "secret-helper") {
				t.Fatal("original Git config exposed", e)
			}
			io.WriteString(r.Output, strings.Repeat("full failure\n", 9000)+"late-contract-evidence\n")
			return errors.New("contract rejected")
		})
		if err == nil || !called || len(result.Gates) != 0 {
			t.Fatal("contract failure accepted", result, err)
		}
		b, _ := os.ReadFile(result.DiagnosticPath)
		var joined strings.Builder
		for _, line := range strings.Split(string(b), "\n") {
			var record map[string]any
			if json.Unmarshal([]byte(line), &record) == nil {
				if out, ok := record["output"].(string); ok {
					joined.WriteString(out)
				}
			}
		}
		if !strings.Contains(joined.String(), "late-contract-evidence") || !strings.Contains(string(b), "contract rejected") {
			t.Fatal("contract failure lost", err)
		}
	}
	req.BaseRoot = ""
	_, err := verifyGoal(context.Background(), req, goalTestSuccess, goalTestContract)
	if err == nil {
		t.Fatal("missing accepted base accepted")
	}
}

func TestGoalFreshCIFactsMatchRecordedCommit(t *testing.T) {
	sha := strings.Repeat("a", 40)
	root := initSyncRepo(t, sha, "rust-v0.140.0", KindStableTag)
	source := filepath.Join(root, "codexsdk/.cache/codexsdk-upstream-"+sha[:12])
	os.MkdirAll(filepath.Join(source, "schema"), 0700)
	os.WriteFile(filepath.Join(source, "schema/input.json"), []byte("{}"), 0600)
	os.WriteFile(filepath.Join(source, "common.rs"), []byte("official-source"), 0600)
	for _, marker := range []string{"", strings.Repeat("b", 40), sha} {
		os.WriteFile(filepath.Join(source, "common.rs.source_sha"), []byte(marker), 0600)
		facts, err := goalFreshCIContractFacts(context.Background(), root, t.TempDir())
		if marker != sha {
			if err == nil {
				t.Fatal("wrong upstream marker accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(facts, "common.rs"))
		if err != nil || string(b) != "official-source" || goalWithin(root, facts) {
			t.Fatal("fresh facts not isolated", facts, err)
		}
	}
}
