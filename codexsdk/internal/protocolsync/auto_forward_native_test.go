package protocolsync

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The controller deliberately has M and H only as fetched objects, matching
// NativeForward.Read. A local clone must not rely on refs that the caller never
// created or silently substitute the controller checkout's old main.
func forwardNativeFixture(t *testing.T, scenario string) (root, main, head, base string) {
	t.Helper()
	origin := t.TempDir()
	goalTestGit(t, origin, "init", "--quiet", "-b", "main")
	goalTestGit(t, origin, "config", "user.name", "fixture")
	goalTestGit(t, origin, "config", "user.email", "fixture@localhost")
	goalTestWrite(t, origin, "shared.txt", "baseline\n")
	if scenario == "conflict" {
		goalTestWrite(t, origin, "delete.txt", "delete this in later proposal\n")
		goalTestWrite(t, origin, "rename-before.txt", "renamed proposal content\n")
	}
	goalTestGit(t, origin, "add", ".")
	goalTestGit(t, origin, "commit", "--quiet", "-m", "base")
	base = goalTestGit(t, origin, "rev-parse", "HEAD")
	root = filepath.Join(t.TempDir(), "controller")
	goalTestGit(t, t.TempDir(), "clone", "--quiet", "--no-local", origin, root)
	goalTestWrite(t, origin, "main.txt", "accepted main content\n")
	if scenario == "conflict" {
		goalTestWrite(t, origin, "shared.txt", "current main resolution\n")
	}
	goalTestGit(t, origin, "add", ".")
	goalTestGit(t, origin, "commit", "--quiet", "-m", "current main")
	main = goalTestGit(t, origin, "rev-parse", "HEAD")
	goalTestGit(t, origin, "checkout", "--quiet", "-b", "proposal", base)
	goalTestWrite(t, origin, "proposal.txt", "first proposal commit\n")
	if scenario == "helpers" {
		goalTestWrite(t, origin, ".gitattributes", "*.txt filter=ambient merge=ambient\n")
	}
	if scenario == "conflict" {
		goalTestWrite(t, origin, "shared.txt", "candidate resolution\n")
	}
	goalTestGit(t, origin, "add", ".")
	goalTestGit(t, origin, "commit", "--quiet", "-m", "proposal first")
	goalTestWrite(t, origin, "second.txt", "second proposal commit\n")
	if scenario == "conflict" {
		if err := os.Remove(filepath.Join(origin, "delete.txt")); err != nil {
			t.Fatal(err)
		}
		goalTestGit(t, origin, "mv", "rename-before.txt", "rename-after.txt")
		goalTestWrite(t, origin, "late-binary.bin", string([]byte{0, 1, 2, 3, 255}))
		goalTestWrite(t, origin, "shared.txt", "candidate resolution from final commit\n")
	}
	goalTestGit(t, origin, "add", ".")
	goalTestGit(t, origin, "commit", "--quiet", "-m", "proposal second")
	if scenario == "merge" {
		goalTestGit(t, origin, "checkout", "--quiet", "-b", "side", base)
		goalTestWrite(t, origin, "side.txt", "side history\n")
		goalTestGit(t, origin, "add", ".")
		goalTestGit(t, origin, "commit", "--quiet", "-m", "side")
		goalTestGit(t, origin, "checkout", "--quiet", "proposal")
		goalTestGit(t, origin, "merge", "--quiet", "--no-ff", "-m", "merge side", "side")
	}
	head = goalTestGit(t, origin, "rev-parse", "HEAD")
	goalTestGit(t, root, "fetch", "--quiet", "--no-tags", "origin", main, head)
	refs := goalTestGit(t, root, "show-ref")
	if strings.Contains(refs, main) || strings.Contains(refs, head) {
		t.Fatal("fixture accidentally gave exact fetched heads local refs")
	}
	return root, main, head, base
}

func TestForwardNativeRebaseRetainsBothBranchesAndLinearCommitChain(t *testing.T) {
	root, main, head, base := forwardNativeFixture(t, "clean")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	native := &NativeForward{proofs: map[string]bool{}, req: ForwardRequest{RepoRoot: root, Target: Target{PeeledCommitSHA: strings.Repeat("a", 40)}, VerifyCandidate: func(context.Context, string, string) error { return nil }}}
	next, err := native.Rebase(ctx, main, head)
	if err != nil {
		t.Fatal(err)
	}
	if next == head || next == main {
		t.Fatal("mechanical rebase did not produce a distinct candidate")
	}
	if descends, err := native.Ancestor(ctx, main, next); err != nil || !descends {
		t.Fatal("rebased candidate does not descend from exact M")
	}
	if got := goalTestGit(t, root, "rev-list", "--count", main+".."+next); got != "2" {
		t.Fatalf("proposal commit chain was flattened or dropped: %s", got)
	}
	if got := goalTestGit(t, root, "rev-list", "--merges", main+".."+next); got != "" {
		t.Fatal("mechanical reconciliation created a merge")
	}
	for name, want := range map[string]string{"main.txt": "accepted main content", "proposal.txt": "first proposal commit", "second.txt": "second proposal commit", "shared.txt": "baseline"} {
		if got := goalTestGit(t, root, "show", next+":"+name); got != want {
			t.Fatalf("lost content %s: got %q", name, got)
		}
	}
	if goalTestGit(t, root, "rev-parse", "HEAD") != base || AssertClean(root) != nil {
		t.Fatal("mechanical rebase changed the trusted controller checkout")
	}
}

func TestForwardNativeConflictPreservesBothSidesWithoutSelection(t *testing.T) {
	root, main, head, base := forwardNativeFixture(t, "conflict")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	candidate := filepath.Join(t.TempDir(), "candidate")
	if err := prepareForwardRebase(ctx, root, candidate, main, head); !errors.Is(err, ErrForwardConflict) {
		t.Fatalf("mechanical conflict was not retained: %v", err)
	}
	if got := goalTestGit(t, candidate, "ls-files", "--unmerged"); got == "" {
		t.Fatal("controller automatically resolved the unmerged index")
	}
	data, err := os.ReadFile(filepath.Join(candidate, "shared.txt"))
	if err != nil || !strings.Contains(string(data), "<<<<<<<") || !strings.Contains(string(data), "current main resolution") || !strings.Contains(string(data), "candidate resolution") {
		t.Fatal("controller chose a side instead of retaining the conflict", err)
	}
	if data, err := os.ReadFile(filepath.Join(candidate, "second.txt")); err != nil || string(data) != "second proposal commit\n" {
		t.Fatal("repair candidate omitted a commit after the first conflict")
	}
	if !strings.Contains(string(data), "candidate resolution from final commit") {
		t.Fatal("conflict retained only the first proposal commit's version")
	}
	for _, name := range []string{"delete.txt", "rename-before.txt"} {
		if _, err := os.Stat(filepath.Join(candidate, name)); !os.IsNotExist(err) {
			t.Fatalf("later proposal deletion or rename was omitted: %s", name)
		}
	}
	if data, err := os.ReadFile(filepath.Join(candidate, "rename-after.txt")); err != nil || string(data) != "renamed proposal content\n" {
		t.Fatal("later proposal rename destination was omitted")
	}
	if data, err := os.ReadFile(filepath.Join(candidate, "late-binary.bin")); err != nil || string(data) != string([]byte{0, 1, 2, 3, 255}) {
		t.Fatal("later binary proposal content was omitted")
	}
	if goalTestGit(t, candidate, "rev-parse", "HEAD") != main {
		t.Fatal("full conflict reconstruction did not retain exact current main")
	}
	if goalTestGit(t, root, "rev-parse", "HEAD") != base || AssertClean(root) != nil {
		t.Fatal("conflict changed the trusted controller checkout")
	}
}

func TestForwardNativeRebaseRejectsExistingMergeHistory(t *testing.T) {
	root, main, head, _ := forwardNativeFixture(t, "merge")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	candidate := filepath.Join(t.TempDir(), "candidate")
	err := prepareForwardRebase(ctx, root, candidate, main, head)
	if err == nil || errors.Is(err, ErrForwardConflict) || !strings.Contains(err.Error(), "non-linear") {
		t.Fatalf("merge history was flattened or admitted as automatic repair: %v", err)
	}
	if got := goalTestGit(t, candidate, "rev-parse", "HEAD"); got != head {
		t.Fatal("merge history was rewritten before rejection")
	}
}

func TestForwardNativeRebaseIgnoresAmbientGitHelpersAndOverrides(t *testing.T) {
	root, main, head, base := forwardNativeFixture(t, "helpers")
	work := t.TempDir()
	marker := filepath.Join(work, "helper-executed")
	helper := filepath.Join(work, "ambient-helper")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nprintf unsafe > "+marker+"\ncat\n"), 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(work, "gitconfig")
	if err := os.WriteFile(config, []byte("[filter \"ambient\"]\nsmudge = "+helper+"\nclean = cat\nrequired = true\n[merge \"ambient\"]\ndriver = "+helper+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	t.Setenv("GIT_DIR", filepath.Join(work, "not-a-repository"))
	t.Setenv("GIT_EXEC_PATH", filepath.Join(work, "no-git-programs"))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	candidate := filepath.Join(work, "candidate")
	if err := prepareForwardRebase(ctx, root, candidate, main, head); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("candidate attributes executed an ambient helper")
	}
	if got, err := forwardLocalGit(ctx, root, "rev-parse", "HEAD"); err != nil || got != base {
		t.Fatal("ambient Git overrides changed the controller checkout")
	}
	data, err := os.ReadFile(filepath.Join(candidate, "proposal.txt"))
	if err != nil || string(data) != "first proposal commit\n" {
		t.Fatal("ambient filter changed the candidate bytes")
	}
}

func TestForwardNativeCheckoutImportsExactFetchedObjectWithoutOrigin(t *testing.T) {
	root, main, head, _ := forwardNativeFixture(t, "clean")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, sha := range []string{main, head} {
		destination := filepath.Join(t.TempDir(), "candidate")
		if err := PrepareForwardCheckout(ctx, root, destination, sha); err != nil {
			t.Fatal(err)
		}
		if got := goalTestGit(t, destination, "rev-parse", "HEAD"); got != sha {
			t.Fatal("checkout substituted a different commit")
		}
		if remotes := goalTestGit(t, destination, "remote"); remotes != "" {
			t.Fatal("isolated checkout retained a host repository remote")
		}
		if branch := goalTestGit(t, destination, "branch", "--show-current"); branch != "" {
			t.Fatal("isolated checkout is not detached")
		}
		if err := AssertClean(destination); err != nil {
			t.Fatal(err)
		}
	}
}

type forwardNativeReadAPI struct {
	t            *testing.T
	head, branch string
}

func (api forwardNativeReadAPI) Request(method, path string, body, result any) error {
	api.t.Helper()
	if method != "GET" || body != nil {
		api.t.Fatalf("unexpected external action in read fixture: %s %s", method, path)
	}
	var data any
	switch path {
	case "repos/fixture/project/pulls/1":
		data = map[string]any{"number": 1, "state": "open", "draft": false,
			"user": map[string]any{"id": 42, "type": "Bot"},
			"head": map[string]any{"ref": api.branch, "sha": api.head, "repo": map[string]any{"full_name": "fixture/project"}},
			"base": map[string]any{"ref": "main", "repo": map[string]any{"full_name": "fixture/project"}},
		}
	case "repos/fixture/project/activity?ref=" + url.QueryEscape("refs/heads/"+api.branch) + "&per_page=1":
		data = []map[string]any{{"ref": "refs/heads/" + api.branch, "after": api.head, "actor": map[string]any{"id": 42, "type": "Bot"}}}
	default:
		api.t.Fatalf("unexpected external action in read fixture: %s %s", method, path)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, result)
}
func forwardNativeReadController(t *testing.T, root, main, head string) *NativeForward {
	t.Helper()
	base := goalTestGit(t, root, "merge-base", main, head)
	target := Target{RefName: "rust-v0.1.0", RefKind: KindStableTag, PeeledCommitSHA: strings.Repeat("a", 40)}
	branch := syncBranchName(target.RefName, target.PeeledCommitSHA, base) + "-goal-" + strings.Repeat("a", 16)
	origin := goalTestGit(t, root, "remote", "get-url", "origin")
	goalTestGit(t, origin, "update-ref", "refs/heads/"+branch, head)
	op, err := NewNativeForward(ForwardRequest{RepoRoot: root, Repository: "fixture/project", BaseBranch: "main", ExpectedHead: head, OriginalBase: base, VerificationBase: main, Number: 1,
		Target: target, PublicationAppBotID: 42, IntegrationAppBotID: 43,
		API: forwardNativeReadAPI{t: t, head: head, branch: branch}, Review: func(context.Context) error { return nil },
		Token: func(context.Context, bool) (string, func(), error) {
			t.Fatal("read fixture requested write authority")
			return "", nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return op
}
func TestForwardNativeRejectsRollbackAndDivergenceFromVerifiedMain(t *testing.T) {
	for _, scenario := range []string{"rollback", "divergent_descendant", "monotonic"} {
		t.Run(scenario, func(t *testing.T) {
			root, main, head, base := forwardNativeFixture(t, "clean")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			op := forwardNativeReadController(t, root, main, head)
			if _, err := op.Read(ctx, head); err != nil {
				t.Fatal(err)
			}
			origin := goalTestGit(t, root, "remote", "get-url", "origin")
			next := base
			if scenario != "rollback" {
				parent := base
				if scenario == "monotonic" {
					parent = main
				}
				next = goalTestGit(t, origin, "commit-tree", parent+"^{tree}", "-p", parent, "-m", "next fixture main "+scenario)
			}
			// This only changes the temporary on-disk fixture repository. There is
			// no network service and no controller publication token or Git push.
			goalTestGit(t, origin, "update-ref", "refs/heads/main", next)
			state, err := op.Read(ctx, head)
			if scenario == "monotonic" {
				if err != nil || state.Main != next || op.lastMain != next {
					t.Fatalf("valid main advance rejected: %+v %v", state, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "reset or diverged") || op.lastMain != main {
				t.Fatalf("accepted rollback/divergence from last verified M: %+v %v", state, err)
			}
		})
	}
}
func TestForwardNativeReadDoesNotFetchUnpublishedExpectedHead(t *testing.T) {
	root, main, head, _ := forwardNativeFixture(t, "clean")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	op := forwardNativeReadController(t, root, main, head)
	state, err := op.Read(ctx, strings.Repeat("f", 40))
	if err != nil || state.Head != head || state.Main != main {
		t.Fatalf("readback tried to fetch an unpublished candidate instead of actual refs: %+v %v", state, err)
	}
}

func TestForwardNativeCleanRebaseRepairsOnlyCompletedSemanticFailure(t *testing.T) {
	for _, scenario := range []string{"semantic", "semantic_linear", "infrastructure", "cancelled", "controls", "repair_controls", "missing_verifier", "repair_failed"} {
		t.Run(scenario, func(t *testing.T) {
			root, main, head, base := forwardNativeFixture(t, "clean")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			deadline, _ := ctx.Deadline()
			if scenario == "semantic_linear" {
				main = base
			}
			if scenario == "controls" {
				origin := goalTestGit(t, root, "remote", "get-url", "origin")
				goalTestWrite(t, origin, ".github/workflows/candidate.yml", "candidate changed gate authority\n")
				goalTestGit(t, origin, "add", ".")
				goalTestGit(t, origin, "commit", "--quiet", "-m", "change control source")
				head = goalTestGit(t, origin, "rev-parse", "HEAD")
				goalTestGit(t, root, "fetch", "--quiet", "--no-write-fetch-head", origin, head)
			}
			proofCalls, repairCalls := 0, 0
			failedHead := ""
			native := &NativeForward{proofs: map[string]bool{}, req: ForwardRequest{RepoRoot: root, Target: Target{PeeledCommitSHA: strings.Repeat("a", 40)}}}
			native.req.VerifyCandidate = func(call context.Context, gotMain, gotHead string) error {
				proofCalls++
				if gotMain != main || gotHead == main || (gotHead == head) != (scenario == "semantic_linear") {
					t.Fatal("proof did not receive the mechanically rebased candidate")
				}
				if got, ok := call.Deadline(); !ok || !got.Equal(deadline) {
					t.Fatal("proof received a fresh budget")
				}
				failedHead = gotHead
				if scenario == "infrastructure" {
					return errors.New("sandbox setup failed")
				}
				if scenario == "cancelled" {
					cancel()
				}
				return errors.Join(errors.New("complete local assertion failed"), ErrForwardSemanticProof)
			}
			if scenario == "missing_verifier" {
				native.req.VerifyCandidate = nil
			}
			native.req.RepairCandidate = func(call context.Context, candidate, gotMain, gotHead string) (GoalVerifyResult, error) {
				repairCalls++
				if got, ok := call.Deadline(); !ok || !got.Equal(deadline) || call.Err() != nil {
					t.Fatal("semantic repair reset or exhausted the original deadline")
				}
				if gotMain != main || gotHead != head || goalTestGit(t, candidate, "rev-parse", "HEAD") != failedHead {
					t.Fatal("repair changed immutable bindings or discarded the complete rebase")
				}
				if scenario == "repair_failed" {
					return GoalVerifyResult{}, errors.New("fresh local repair did not verify")
				}
				goalTestWrite(t, candidate, "proposal.txt", "locally verified semantic repair\n")
				if scenario == "repair_controls" {
					goalTestWrite(t, candidate, ".github/workflows/candidate.yml", "repair changed gate authority\n")
				}
				work := t.TempDir()
				snapshot := filepath.Join(work, "snapshot")
				digest, err := goalSnapshot(call, candidate, snapshot)
				return GoalVerifyResult{SnapshotRoot: snapshot, SnapshotDigest: digest, cleanupRoot: work}, err
			}
			if scenario == "semantic_linear" {
				if err := native.Verify(ctx, main, head); !errors.Is(err, ErrForwardSemanticProof) || proofCalls != 1 {
					t.Fatal("linear proof did not record the completed semantic failure")
				}
			}
			next, err := native.Rebase(ctx, main, head)
			if scenario == "semantic" || scenario == "semantic_linear" {
				if err != nil || proofCalls != 1 || repairCalls != 1 || next == failedHead || !native.proofs[main+":"+next] {
					t.Fatalf("eligible semantic repair did not produce a verified candidate: %s %v proofs=%d repairs=%d", next, err, proofCalls, repairCalls)
				}
				if goalTestGit(t, root, "show", next+":proposal.txt") != "locally verified semantic repair" || goalTestGit(t, root, "rev-list", "--parents", "-n", "1", next) != next+" "+main {
					t.Fatal("semantic repair was not reconstructed on exact M")
				}
				if err := native.Verify(ctx, main, next); err != nil || proofCalls != 1 {
					t.Fatal("verified repair was not cached for the exact M/H pair")
				}
			} else {
				wantRepair := 0
				if scenario == "repair_controls" || scenario == "repair_failed" {
					wantRepair = 1
				}
				if err == nil || next != "" || repairCalls != wantRepair || len(native.proofs) != 0 {
					t.Fatalf("ineligible failure escaped blocking: next=%s err=%v proofs=%v repairs=%d", next, err, native.proofs, repairCalls)
				}
				if scenario == "controls" && proofCalls != 0 {
					t.Fatal("candidate control changes reached executable verification")
				}
			}
			if goalTestGit(t, root, "rev-parse", "HEAD") != base || AssertClean(root) != nil {
				t.Fatal("semantic repair changed the trusted controller checkout")
			}
		})
	}
}

func TestForwardNativeSemanticFailureCacheCannotCrossMainOrHead(t *testing.T) {
	root, main, head, base := forwardNativeFixture(t, "clean")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	calls := map[string]int{}
	op := &NativeForward{proofs: map[string]bool{}, req: ForwardRequest{RepoRoot: root, Target: Target{PeeledCommitSHA: strings.Repeat("a", 40)}, VerifyCandidate: func(call context.Context, m, h string) error {
		calls[m+":"+h]++
		return ErrForwardSemanticProof
	}}}
	pairs := [][2]string{{main, head}, {main, head}, {base, head}, {main, main}, {main, head}}
	for _, pair := range pairs {
		if err := op.Verify(ctx, pair[0], pair[1]); !errors.Is(err, ErrForwardSemanticProof) {
			t.Fatalf("completed semantic failure classification changed: %v", err)
		}
	}
	if len(calls) != 3 || len(op.semanticFailures) != 3 || len(op.proofs) != 0 {
		t.Fatalf("semantic failure cache crossed immutable M/H bindings: calls=%v", calls)
	}
	for key, count := range calls {
		if count != 1 {
			t.Fatalf("unchanged failed proof rerun for %s: %d", key, count)
		}
	}
}
