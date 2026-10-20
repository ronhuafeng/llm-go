package protocolsync

import (
	"context"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/checkacceptance"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/codexexec"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestActiveGoalRefreshesCurrentBaseWithoutReplacingOriginalPromise(t *testing.T) {
	f := newGoalPublicationFixture(t)
	f.request.Target = Target{RefName: "rust-v0.142.0", RefKind: KindStableTag, PeeledCommitSHA: strings.Repeat("c", 40)}
	f.request.Lookuper = goalLookup{f.request.Target}
	original := f.request.BaseSHA
	goalTestWrite(t, f.request.RepoRoot, "README.md", "new main contract\n")
	goalTestGit(t, f.request.RepoRoot, "add", "README.md")
	goalTestGit(t, f.request.RepoRoot, "commit", "-m", "main moves during goal")
	current := goalTestGit(t, f.request.RepoRoot, "rev-parse", "HEAD")
	goalTestGit(t, f.request.RepoRoot, "push", "origin", "HEAD:refs/heads/main")
	goalTestGit(t, f.request.RepoRoot, "checkout", "--detach", original)
	f.pr.Base.SHA = current
	if err := goalCurrent(context.Background(), f.request, f.request.ExpectedHead); err == nil {
		t.Fatal("unverified main movement accepted")
	}
	local, binding, cleanup, err := refreshGoalBase(context.Background(), GoalVerifyRequest{RepoRoot: f.request.CandidateDir, BaseRoot: f.request.RepoRoot}, f.request)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if binding.BaseSHA != original || binding.VerificationBaseSHA != current || local.OriginalBaseRoot != f.request.RepoRoot || local.BaseRoot == local.OriginalBaseRoot {
		t.Fatal("original contract replaced", binding, local)
	}
	if got := goalTestGit(t, local.BaseRoot, "rev-parse", "HEAD"); got != current {
		t.Fatal("current contract not immutable M")
	}
	if got, _ := os.ReadFile(filepath.Join(local.BaseRoot, "README.md")); string(got) != "new main contract\n" {
		t.Fatal("wrong current contract")
	}
	if err := goalCurrent(context.Background(), binding, binding.ExpectedHead); err != nil {
		t.Fatal(err)
	}
	// A second move cannot reuse the first M's verification binding.
	goalTestGit(t, f.request.RepoRoot, "checkout", "main")
	goalTestGit(t, f.request.RepoRoot, "commit", "--allow-empty", "-m", "main moves again")
	goalTestGit(t, f.request.RepoRoot, "push", "origin", "HEAD:refs/heads/main")
	if err := goalCurrent(context.Background(), binding, binding.ExpectedHead); err == nil {
		t.Fatal("stale current-M checks reused")
	}
}

func TestActiveGoalRefusesSupersededTarget(t *testing.T) {
	f := newGoalPublicationFixture(t)
	goalTestGit(t, f.request.RepoRoot, "commit", "--allow-empty", "-m", "main already accepts target")
	goalTestGit(t, f.request.RepoRoot, "push", "origin", "HEAD:refs/heads/main")
	_, _, cleanup, err := refreshGoalBase(context.Background(), GoalVerifyRequest{BaseRoot: f.request.RepoRoot}, f.request)
	cleanup()
	if err == nil {
		t.Fatal("already accepted main silently downgraded or reverified")
	}
}

func TestNativeGoalContinuesAfterMainMovesUsingBothContracts(t *testing.T) {
	f := newGoalPublicationFixture(t)
	f.request.Target = Target{RefName: "rust-v0.142.0", RefKind: KindStableTag, PeeledCommitSHA: strings.Repeat("c", 40)}
	f.request.Lookuper = goalLookup{f.request.Target}
	goalTestWrite(t, f.request.CandidateDir, "codexsdk/internal/protocolschema/appserver/v2/baseline_metadata.json", `{"source_commit":"`+f.request.Target.PeeledCommitSHA+`","source_ref_name":"rust-v0.142.0","source_ref_kind":"stable_rust_tag"}`)
	goalTestGit(t, f.request.CandidateDir, "add", "-A")
	goalTestGit(t, f.request.CandidateDir, "-c", "user.name=Fixture", "-c", "user.email=fixture@localhost", "commit", "-m", "fixed target candidate")
	f.request.SnapshotDigest, _ = goalSnapshot(context.Background(), f.request.CandidateDir, "")
	diag := t.TempDir()
	path := filepath.Join(diag, "local.jsonl")
	if err := os.WriteFile(path, []byte("{\"passed\":true}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	req := GoalFlowRequest{Objective: "one continuing goal", Local: GoalVerifyRequest{RepoRoot: f.request.CandidateDir, BaseRoot: f.request.RepoRoot, DiagnosticDir: diag}, Publication: f.request}
	both := false
	op := goalFlowOperations{refresh: refreshGoalBase, finalObserve: observeCompletedGoalCI, publish: AppendGoalDraft, observe: ObserveGoalCI,
		local: func(_ context.Context, local GoalVerifyRequest) (GoalVerifyResult, error) {
			both = local.OriginalBaseRoot == f.request.RepoRoot && local.BaseRoot != local.OriginalBaseRoot
			return GoalVerifyResult{SnapshotRoot: f.request.CandidateDir, SnapshotDigest: f.request.SnapshotDigest, DiagnosticPath: path}, nil
		}, diagnostics: func(context.Context, GoalPublicationRequest, checkacceptance.Observation, *goalDiagnostics) error {
			return nil
		}}
	runner := goalRunnerFunc(func(ctx context.Context, _ string, verify func(context.Context) (string, error)) (codexexec.GoalResult, error) {
		goalTestWrite(t, f.request.RepoRoot, "README.md", "new accepted main obligation\n")
		goalTestGit(t, f.request.RepoRoot, "add", "README.md")
		goalTestGit(t, f.request.RepoRoot, "commit", "-m", "main advances during model work")
		f.pr.Base.SHA = goalTestGit(t, f.request.RepoRoot, "rev-parse", "HEAD")
		goalTestGit(t, f.request.RepoRoot, "push", "origin", "HEAD:refs/heads/main")
		goalTestGit(t, f.request.RepoRoot, "checkout", "--detach", f.request.BaseSHA)
		if _, err := verify(ctx); err != nil {
			return codexexec.GoalResult{}, err
		}
		// Main moves again after the last successful tool call, just as native
		// completion is handed back to the trusted launcher.
		goalTestGit(t, f.request.RepoRoot, "checkout", "main")
		goalTestGit(t, f.request.RepoRoot, "commit", "--allow-empty", "-m", "main advances after native completion")
		f.pr.Base.SHA = goalTestGit(t, f.request.RepoRoot, "rev-parse", "HEAD")
		goalTestGit(t, f.request.RepoRoot, "push", "origin", "HEAD:refs/heads/main")
		goalTestGit(t, f.request.RepoRoot, "checkout", "--detach", f.request.BaseSHA)
		return codexexec.GoalResult{Status: "complete", ThreadID: "same-native-goal"}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	result, err := runUpstreamGoal(ctx, req, runner, op)
	if err != nil || !both || result.NativeStatus != "complete" || result.Head == f.request.BaseSHA {
		t.Fatal("active goal did not safely continue", result, err, both)
	}
	if result.VerificationBaseSHA == f.pr.Base.SHA {
		t.Fatal("claimed unverified later main as accepted")
	}
	h, err := CompletedGoalHandoff(req.Publication, result, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	ready := &readyFixture{goalPublicationFixture: f}
	f.pr.URL = "https://github.com/owner/repo/pull/7"
	if err := ReadyGoal(ctx, f.request.RepoRoot, h, ready, f.request.Lookuper); err != nil {
		t.Fatal("safe post-completion main advance blocked Ready", err)
	}
	if ready.writes != 1 {
		t.Fatal("missing unique Ready handoff")
	}
}

func TestActiveGoalRejectsRollbackToOriginalBase(t *testing.T) {
	f := newGoalPublicationFixture(t)
	original := f.request.BaseSHA
	goalTestGit(t, f.request.RepoRoot, "commit", "--allow-empty", "-m", "observed main")
	observed := goalTestGit(t, f.request.RepoRoot, "rev-parse", "HEAD")
	f.request.VerificationBaseSHA = observed
	// origin still points at B: a previous observed M may never be discarded.
	_, _, cleanup, err := refreshGoalBase(context.Background(), GoalVerifyRequest{BaseRoot: f.request.RepoRoot}, f.request)
	cleanup()
	if err == nil {
		t.Fatal("previous M obligations discarded by rollback to B")
	}
	if current, _ := goalRemoteHead(context.Background(), f.request, "main"); current != original {
		t.Fatal("fixture main drift")
	}
}
