package protocolsync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/codexexec"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolupgrade"
)

// Preparing exact source is not acceptance of the candidate's Go semantics.
// An unsupported shape must reach the native goal and its verification feedback,
// even when the accepted planner would return an unclassified error.
func TestPreparedSourceReachesGoalBeforeCandidateSemantics(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	candidate := t.TempDir()
	schema := `{"type":"object","properties":{"tracestate":{"type":["object","null"],"additionalProperties":{"type":"object","additionalProperties":{"type":"string"}}}}}`
	writeFile(t, filepath.Join(candidate, "schema", "OtelRequirements.json"), schema)
	target := Target{RefName: "rust-v0.141.0", RefKind: KindStableTag, PeeledCommitSHA: newSHA}
	result, err := PrepareSource(SyncRequest{
		RepoRoot: repo, UpstreamRef: target.RefName, Lookuper: goalLookup{target},
		Generate: func(GenerateRequest) (Candidate, error) {
			return Candidate{Dir: candidate, SchemaDir: filepath.Join(candidate, "schema"), SourceCommit: newSHA}, nil
		},
		Plan: func(protocolupgrade.ApplyRequest) (protocolupgrade.PlanResult, error) {
			t.Fatal("accepted planner must not gate source preparation")
			return protocolupgrade.PlanResult{}, errors.New("unsupported schema")
		},
		Apply: func(protocolupgrade.ApplyRequest) (protocolupgrade.ApplyResult, error) {
			t.Fatal("source preparation applied candidate")
			return protocolupgrade.ApplyResult{}, nil
		},
	})
	if err != nil || result.Outcome != OutcomeSourceReady || result.Target.PeeledCommitSHA != newSHA {
		t.Fatalf("source preparation: %+v %v", result, err)
	}
	fixture := newGoalPublicationFixture(t)
	diag := t.TempDir()
	path := filepath.Join(diag, "candidate.jsonl")
	entered, received := false, false
	semantic := errors.New("candidate generator cannot represent new schema")
	runner := goalRunnerFunc(func(ctx context.Context, _ string, verify func(context.Context) (string, error)) (codexexec.GoalResult, error) {
		entered = true
		data, err := os.ReadFile(filepath.Join(result.CandidateDir, "schema", "OtelRequirements.json"))
		if err != nil || string(data) != schema {
			t.Fatalf("goal lost exact source: %s %v", data, err)
		}
		diagnostic, err := verify(ctx)
		if !errors.Is(err, semantic) {
			t.Fatalf("goal did not receive candidate failure: %v", err)
		}
		data, err = os.ReadFile(diagnostic)
		if err != nil || !strings.Contains(string(data), "unsupported schema") {
			t.Fatalf("goal lost diagnostic: %s %v", data, err)
		}
		received = true
		return codexexec.GoalResult{Status: "blocked"}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	outcome, err := runUpstreamGoal(ctx, GoalFlowRequest{Objective: "adapt exact upstream", Local: GoalVerifyRequest{DiagnosticDir: diag, CandidateDir: result.CandidateDir}, Publication: fixture.request}, runner, goalFlowOperations{
		local: func(_ context.Context, req GoalVerifyRequest) (GoalVerifyResult, error) {
			if req.CandidateDir != candidate {
				t.Fatal("verification changed exact source")
			}
			if err := os.WriteFile(path, []byte("{\"passed\":false,\"reason\":\"unsupported schema\"}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			return GoalVerifyResult{DiagnosticPath: path}, semantic
		},
		publish: func(context.Context, GoalPublicationRequest) (GoalPublication, error) {
			t.Fatal("unsupported candidate must not publish")
			return GoalPublication{}, nil
		},
	})
	if !entered || !received || err == nil || outcome.Head != "" {
		t.Fatalf("goal entry=%v diagnostic=%v outcome=%+v err=%v", entered, received, outcome, err)
	}
}

func TestPrepareSourceKeepsTrustedFailuresHard(t *testing.T) {
	for _, scenario := range []string{"build", "source identity", "accepted worktree"} {
		t.Run(scenario, func(t *testing.T) {
			repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
			target := Target{RefName: "rust-v0.141.0", RefKind: KindStableTag, PeeledCommitSHA: newSHA}
			result, err := PrepareSource(SyncRequest{RepoRoot: repo, UpstreamRef: target.RefName, Lookuper: goalLookup{target}, Generate: func(GenerateRequest) (Candidate, error) {
				if scenario == "build" {
					return Candidate{}, errors.New("source build failed")
				}
				sha := newSHA
				if scenario == "source identity" {
					sha = oldSHA
				}
				if scenario == "accepted worktree" {
					writeFile(t, filepath.Join(repo, "README.md"), "changed")
				}
				return Candidate{SourceCommit: sha}, nil
			}})
			if err == nil || result.Outcome != OutcomeFailed {
				t.Fatalf("trusted failure accepted: %+v %v", result, err)
			}
		})
	}
}
