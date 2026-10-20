package protocolsync

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/checkacceptance"
	"strings"
	"testing"
	"time"
)

type readyFixture struct {
	*goalPublicationFixture
	writes                                   int
	lostResponse, rejectWrite, wrongBaseline bool
}

func (f *readyFixture) Request(method, path string, body, result any) error {
	if method == "POST" {
		if path != "graphql" {
			f.t.Fatalf("unexpected write %s", path)
		}
		f.writes++
		if !f.rejectWrite {
			f.pr.Draft = false
		}
		if f.lostResponse || f.rejectWrite {
			return errors.New("uncertain mutation")
		}
		return nil
	}
	if strings.Contains(path, "/contents/") {
		target := f.request.Target
		if f.wrongBaseline {
			target.PeeledCommitSHA = strings.Repeat("c", 40)
		}
		data, _ := json.Marshal(map[string]string{"source_commit": target.PeeledCommitSHA, "source_ref_name": target.RefName, "source_ref_kind": target.RefKind})
		data, _ = json.Marshal(map[string]string{"encoding": "base64", "content": base64.StdEncoding.EncodeToString(data)})
		return json.Unmarshal(data, result)
	}
	if strings.Contains(path, "/pulls/") {
		var pr goalDraftPR
		if err := f.goalPublicationFixture.Request(method, path, body, &pr); err != nil {
			return err
		}
		data, _ := json.Marshal(goalReadyPR{goalDraftPR: pr, NodeID: "PR_fixture"})
		return json.Unmarshal(data, result)
	}
	return f.goalPublicationFixture.Request(method, path, body, result)
}
func completedHandoffFixture(t *testing.T) (*readyFixture, GoalHandoff, GoalFlowResult) {
	f := &readyFixture{goalPublicationFixture: newGoalPublicationFixture(t)}
	f.pr.URL = "https://github.com/owner/repo/pull/7"
	publication, err := AppendGoalDraft(context.Background(), f.request)
	if err != nil {
		t.Fatal(err)
	}
	checks, err := checkacceptance.Observe(f, f.request.Repository, publication.Head)
	if err != nil {
		t.Fatal(err)
	}
	result := GoalFlowResult{Head: publication.Head, SnapshotDigest: publication.SnapshotDigest, NativeStatus: "complete", Stage: "Final acceptance", Checks: checks}
	h, err := CompletedGoalHandoff(f.request, result, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return f, h, result
}
func TestCompletedGoalHandoffRequiresActualFinalAcceptance(t *testing.T) {
	f, h, result := completedHandoffFixture(t)
	if h.Head != result.Head || h.OriginalBase != f.request.BaseSHA {
		t.Fatal("wrong exact binding")
	}
	for _, name := range []string{"empty_seed", "blocked", "failure", "head", "workflow", "missing_check", "expired"} {
		t.Run(name, func(t *testing.T) {
			r := result
			r.Checks.Checks = append([]checkacceptance.Check(nil), result.Checks.Checks...)
			deadline := time.Now().Add(time.Minute)
			switch name {
			case "empty_seed":
				r.Head = f.request.ExpectedHead
			case "blocked":
				r.NativeStatus = "blocked"
			case "failure":
				r.Failure = "cleanup failed"
			case "head":
				r.Checks.Head = strings.Repeat("d", 40)
			case "workflow":
				r.Checks.WorkflowState = checkacceptance.Pending
			case "missing_check":
				r.Checks.Checks = r.Checks.Checks[:3]
			case "expired":
				deadline = time.Now().Add(-time.Minute)
			}
			if _, err := CompletedGoalHandoff(f.request, r, deadline); err == nil {
				t.Fatal("invalid handoff accepted")
			}
		})
	}
}
func TestReadyGoalReconcilesSameHandoffWithoutRepeatedWrite(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "lost_response"}[lost], func(t *testing.T) {
			f, h, _ := completedHandoffFixture(t)
			f.lostResponse = lost
			for i := 0; i < 2; i++ {
				if err := ReadyGoal(context.Background(), f.request.RepoRoot, h, f, f.request.Lookuper); err != nil {
					t.Fatal(err)
				}
			}
			if f.writes != 1 || f.pr.Draft {
				t.Fatal("Ready repeated or unconfirmed")
			}
		})
	}
}
func TestReadyGoalRejectsChangedIdentityAndFailedLatestCI(t *testing.T) {
	for _, name := range []string{"actor", "creator", "head", "target", "seed", "checks", "new_attempt", "repository", "deadline", "write_rejected"} {
		t.Run(name, func(t *testing.T) {
			f, h, _ := completedHandoffFixture(t)
			switch name {
			case "actor":
				f.actor.ID++
			case "creator":
				f.pr.User.ID++
			case "head":
				h.Head = strings.Repeat("c", 40)
			case "target":
				h.TargetSHA = strings.Repeat("c", 40)
			case "seed":
				f.wrongBaseline = true
			case "checks":
				f.state = checkacceptance.Failed
			case "new_attempt":
				f.pendingAttempt = true
			case "repository":
				f.pr.Head.Repo.FullName = "foreign/repo"
			case "deadline":
				h.Deadline = time.Now().Add(-time.Minute)
			case "write_rejected":
				f.rejectWrite = true
			}
			if err := ReadyGoal(context.Background(), f.request.RepoRoot, h, f, f.request.Lookuper); err == nil {
				t.Fatal("unsafe Ready accepted")
			}
			want := 0
			if name == "write_rejected" {
				want = 1
			}
			if f.writes != want {
				t.Fatalf("writes=%d", f.writes)
			}
		})
	}
}
