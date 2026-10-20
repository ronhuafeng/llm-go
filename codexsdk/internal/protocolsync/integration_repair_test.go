package protocolsync

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/codexexec"
)

func integrationRepairFixture(t *testing.T) IntegrationRepairRequest {
	t.Helper()
	root := t.TempDir()
	goalTestGit(t, root, "init", "--quiet")
	goalTestWrite(t, root, "candidate.go", "package candidate\n")
	goalTestGit(t, root, "add", ".")
	goalTestGit(t, root, "-c", "user.name=fixture", "-c", "user.email=fixture@localhost", "commit", "--quiet", "-m", "candidate")
	return IntegrationRepairRequest{
		Objective: "Repair only the isolated candidate against original B and fixed U; local verification is not CI acceptance",
		Local:     GoalVerifyRequest{RepoRoot: root, BaseRoot: t.TempDir(), OriginalBaseRoot: t.TempDir(), CandidateDir: t.TempDir(), DiagnosticDir: t.TempDir(), TrustedCacheRoot: t.TempDir()},
		Current:   func(context.Context) error { return nil },
	}
}

func integrationRepairVerifier(t *testing.T) func(context.Context, GoalVerifyRequest) (GoalVerifyResult, error) {
	t.Helper()
	return func(ctx context.Context, req GoalVerifyRequest) (GoalVerifyResult, error) {
		work := t.TempDir()
		root := filepath.Join(work, "candidate")
		digest, err := goalSnapshot(ctx, req.RepoRoot, root)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(req.DiagnosticDir, "local.jsonl")
		if err := os.WriteFile(path, []byte("{\"kind\":\"footer\",\"complete\":true,\"passed\":true}\n"), 0600); err != nil {
			t.Fatal(err)
		}
		return GoalVerifyResult{SnapshotRoot: root, SnapshotDigest: digest, DiagnosticPath: path, cleanupRoot: work}, nil
	}
}

func TestIntegrationRepairRequiresFreshCompleteUnchangedLocalCandidate(t *testing.T) {
	for _, scenario := range []string{"pass", "never_verify", "blocked", "private_error", "local_failed", "mutate_after_tool", "mutate_frozen_snapshot", "late_failed_tool", "binding_stale", "binding_stale_final", "cancel_after_tool", "missing_thread", "cancel_during_tool"} {
		t.Run(scenario, func(t *testing.T) {
			req := integrationRepairFixture(t)
			local := integrationRepairVerifier(t)
			localCalls, nativeCalls, currentCalls := 0, 0, 0
			var frozenRoots []string
			var cancelTool context.CancelFunc
			privateErr := errors.New("private native provider response")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			req.Current = func(context.Context) error {
				currentCalls++
				if scenario == "binding_stale" || scenario == "binding_stale_final" && currentCalls == 3 {
					return errors.New("bound head changed")
				}
				return nil
			}
			verify := func(ctx context.Context, got GoalVerifyRequest) (GoalVerifyResult, error) {
				localCalls++
				if got.BaseRoot != req.Local.BaseRoot || got.OriginalBaseRoot != req.Local.OriginalBaseRoot || got.CandidateDir != req.Local.CandidateDir {
					t.Fatal("repair changed current main, original baseline or fixed upstream")
				}
				r, err := local(ctx, got)
				if scenario == "cancel_during_tool" {
					cancelTool()
				}
				frozenRoots = append(frozenRoots, r.SnapshotRoot)
				if scenario == "local_failed" || scenario == "late_failed_tool" && localCalls == 2 {
					return r, errors.New("real defect")
				}
				return r, err
			}
			runner := goalRunnerFunc(func(ctx context.Context, objective string, verify func(context.Context) (string, error)) (codexexec.GoalResult, error) {
				nativeCalls++
				if objective != req.Objective {
					t.Fatal("fixed objective changed")
				}
				if scenario != "never_verify" && scenario != "private_error" {
					call := ctx
					if scenario == "cancel_during_tool" {
						call, cancelTool = context.WithCancel(ctx)
					}
					_, _ = verify(call)
				}
				if scenario == "mutate_after_tool" || scenario == "late_failed_tool" {
					goalTestWrite(t, req.Local.RepoRoot, "candidate.go", "package changed\n")
				}
				if scenario == "mutate_frozen_snapshot" {
					goalTestWrite(t, frozenRoots[0], "candidate.go", "package changed\n")
				}
				if scenario == "late_failed_tool" {
					_, _ = verify(ctx)
				}
				if scenario == "cancel_after_tool" {
					cancel()
				}
				if scenario == "blocked" {
					return codexexec.GoalResult{ThreadID: "fresh-thread", Status: "blocked"}, nil
				}
				if scenario == "private_error" {
					return codexexec.GoalResult{ThreadID: "fresh-thread", Status: "goal_turn_failed"}, privateErr
				}
				if scenario == "missing_thread" {
					return codexexec.GoalResult{Status: "complete"}, nil
				}
				return codexexec.GoalResult{ThreadID: "fresh-thread", Status: "complete"}, nil
			})
			result, err := runIntegrationRepair(ctx, req, runner, verify)
			if (err == nil) != (scenario == "pass") {
				t.Fatalf("unexpected local completion: result=%+v error=%v", result, err)
			}
			if result.Head != "" || result.Checks.Head != "" || result.Checks.State != "" {
				t.Fatal("local repair invented published-head or CI acceptance evidence")
			}
			if scenario == "binding_stale" {
				if nativeCalls != 0 || localCalls != 0 {
					t.Fatal("stale binding launched repair")
				}
				return
			}
			if nativeCalls != 1 || (scenario != "missing_thread" && result.ThreadID != "fresh-thread") {
				t.Fatal("repair did not use exactly one fresh native invocation")
			}
			if scenario == "pass" {
				if result.Failure != "" || result.SnapshotDigest == "" || AssertClean(result.Snapshot.SnapshotRoot) != nil {
					t.Fatalf("missing final clean frozen snapshot: %+v", result)
				}
				if err := result.Snapshot.Close(); err != nil {
					t.Fatal(err)
				}
			}
			for _, root := range frozenRoots {
				if _, err := os.Stat(root); !os.IsNotExist(err) {
					t.Fatal("unaccepted or consumed snapshot was not released")
				}
			}
			if scenario == "private_error" {
				data, marshalErr := json.Marshal(result)
				if marshalErr != nil || strings.Contains(string(data), privateErr.Error()) || !errors.Is(err, privateErr) {
					t.Fatal("native private detail leaked or error identity lost")
				}
			}
		})
	}
}

func TestIntegrationRepairStopsRepeatedFailedSnapshotWithoutBudgetReset(t *testing.T) {
	for _, scenario := range []string{"unchanged", "changed_then_reverted", "changed_repairs"} {
		t.Run(scenario, func(t *testing.T) {
			req := integrationRepairFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			deadline, _ := ctx.Deadline()
			checkBudget := func(call context.Context) {
				t.Helper()
				got, ok := call.Deadline()
				if !ok || !got.Equal(deadline) {
					t.Fatal("repair reset or removed the original deadline")
				}
			}
			local := integrationRepairVerifier(t)
			localCalls, nativeCalls := 0, 0
			verify := func(call context.Context, got GoalVerifyRequest) (GoalVerifyResult, error) {
				checkBudget(call)
				localCalls++
				r, err := local(call, got)
				if localCalls == 1 || scenario == "changed_then_reverted" {
					return r, errors.New("real defect")
				}
				return r, err
			}
			runner := goalRunnerFunc(func(runCtx context.Context, _ string, call func(context.Context) (string, error)) (codexexec.GoalResult, error) {
				nativeCalls++
				checkBudget(runCtx)
				if _, err := call(context.Background()); err == nil {
					t.Fatal("first failed snapshot accepted")
				}
				if scenario == "changed_then_reverted" || scenario == "changed_repairs" {
					goalTestWrite(t, req.Local.RepoRoot, "candidate.go", "package repaired\n")
				}
				_, err := call(context.Background())
				if scenario == "changed_repairs" {
					if err != nil || runCtx.Err() != nil {
						t.Fatal("same native goal could not repair changed bytes", err)
					}
				} else if scenario == "changed_then_reverted" {
					goalTestWrite(t, req.Local.RepoRoot, "candidate.go", "package candidate\n")
					_, err = call(context.Background())
				}
				if scenario != "changed_repairs" {
					if err == nil || runCtx.Err() == nil {
						t.Fatal("unchanged failed snapshot did not stop the fresh goal")
					}
					goalTestWrite(t, req.Local.RepoRoot, "candidate.go", "package later\n")
					if _, err := call(context.Background()); err == nil {
						t.Fatal("a changed snapshot revived the stopped goal")
					}
				}
				return codexexec.GoalResult{ThreadID: "fresh", Status: "complete"}, nil
			})
			result, err := runIntegrationRepair(ctx, req, runner, verify)
			defer result.Snapshot.Close()
			if (err == nil) != (scenario == "changed_repairs") || nativeCalls != 1 {
				t.Fatalf("unexpected repair outcome %+v %v", result, err)
			}
			wantLocal := 1
			if scenario == "changed_then_reverted" || scenario == "changed_repairs" {
				wantLocal = 2
			}
			if localCalls != wantLocal || ctx.Err() != nil {
				t.Fatal("repeated failed work or cancelled the original controller context")
			}
		})
	}
}

func TestIntegrationRepairRejectsUnboundedExhaustedAndIncompleteBindings(t *testing.T) {
	for _, scenario := range []string{"unbounded", "expired", "cancelled", "one_gate", "no_original_base", "no_upstream", "no_binding_guard", "candidate_as_original_base", "nested_diagnostics"} {
		t.Run(scenario, func(t *testing.T) {
			req := integrationRepairFixture(t)
			ctx := context.Background()
			if scenario != "unbounded" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				if scenario == "cancelled" {
					cancel()
				}
			}
			switch scenario {
			case "expired":
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
			case "one_gate":
				req.Local.Gate = "source"
			case "no_original_base":
				req.Local.OriginalBaseRoot = ""
			case "no_upstream":
				req.Local.CandidateDir = ""
			case "no_binding_guard":
				req.Current = nil
			case "candidate_as_original_base":
				req.Local.OriginalBaseRoot = req.Local.RepoRoot
			case "nested_diagnostics":
				req.Local.DiagnosticDir = filepath.Join(req.Local.RepoRoot, "diagnostics")
			}
			runner := goalRunnerFunc(func(context.Context, string, func(context.Context) (string, error)) (codexexec.GoalResult, error) {
				t.Fatal("invalid binding started a fresh goal")
				return codexexec.GoalResult{}, nil
			})
			if _, err := RunIntegrationRepair(ctx, req, runner); err == nil {
				t.Fatal("invalid integration repair admitted")
			}
		})
	}
}

func TestIntegrationRepairSnapshotsManualConflictResolutionWithoutChangingIndex(t *testing.T) {
	req := integrationRepairFixture(t)
	root := req.Local.RepoRoot
	goalTestGit(t, root, "config", "user.name", "fixture")
	goalTestGit(t, root, "config", "user.email", "fixture@localhost")
	original := goalTestGit(t, root, "branch", "--show-current")
	goalTestGit(t, root, "checkout", "--quiet", "-b", "other")
	goalTestWrite(t, root, "candidate.go", "package upstream\n")
	goalTestGit(t, root, "commit", "--quiet", "-am", "upstream")
	goalTestGit(t, root, "checkout", "--quiet", original)
	goalTestWrite(t, root, "candidate.go", "package proposal\n")
	goalTestGit(t, root, "commit", "--quiet", "-am", "proposal")
	if _, err := goalGit(context.Background(), root, "merge", "other"); err == nil {
		t.Fatal("fixture must have a real unmerged index")
	}
	before := goalTestGit(t, root, "ls-files", "--unmerged")
	if before == "" {
		t.Fatal("missing conflict stages")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	runner := goalRunnerFunc(func(ctx context.Context, _ string, verify func(context.Context) (string, error)) (codexexec.GoalResult, error) {
		// A proposal chooses actual reconciled bytes. The controller never selects
		// ours/theirs and never stages or commits into the conflicted workspace.
		goalTestWrite(t, root, "candidate.go", "package reconciled\n")
		_, err := verify(ctx)
		return codexexec.GoalResult{ThreadID: "fresh", Status: "complete"}, err
	})
	result, err := runIntegrationRepair(ctx, req, runner, integrationRepairVerifier(t))
	if err != nil {
		t.Fatal(err)
	}
	defer result.Snapshot.Close()
	if after := goalTestGit(t, root, "ls-files", "--unmerged"); before != after {
		t.Fatal("repair controller changed the proposal Git index")
	}
	data, err := os.ReadFile(filepath.Join(result.Snapshot.SnapshotRoot, "candidate.go"))
	if err != nil || string(data) != "package reconciled\n" || AssertClean(result.Snapshot.SnapshotRoot) != nil {
		t.Fatal("conflict repair was not frozen as clean model-selected bytes")
	}
}
