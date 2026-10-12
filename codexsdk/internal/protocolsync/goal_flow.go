package protocolsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/checkacceptance"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/codexexec"
)

// GoalRunner owns native continuation. The host only supplies its fixed tool.
type GoalRunner interface {
	RunGoal(context.Context, string, func(context.Context) (string, error)) (codexexec.GoalResult, error)
}

type GoalFlowRequest struct {
	Objective   string
	Local       GoalVerifyRequest
	Publication GoalPublicationRequest
}
type GoalFlowResult struct {
	ThreadID, Head, SnapshotDigest, DiagnosticPath string
	Checks                                         checkacceptance.Observation
}

// RunUpstreamGoal performs no client-authored repair turns. Every verification
// and publication is requested by the same native goal, with immutable binding.
func RunUpstreamGoal(ctx context.Context, req GoalFlowRequest, runner GoalRunner) (GoalFlowResult, error) {
	return runUpstreamGoal(ctx, req, runner, goalFlowOperations{VerifyGoal, AppendGoalDraft, ObserveGoalCI, collectGoalCIDiagnostics})
}

type goalFlowOperations struct {
	local       func(context.Context, GoalVerifyRequest) (GoalVerifyResult, error)
	publish     func(context.Context, GoalPublicationRequest) (GoalPublication, error)
	observe     func(context.Context, GoalPublicationRequest, string, time.Duration) (checkacceptance.Observation, error)
	diagnostics func(context.Context, GoalPublicationRequest, checkacceptance.Observation, *goalDiagnostics) error
}

func runUpstreamGoal(ctx context.Context, req GoalFlowRequest, runner GoalRunner, op goalFlowOperations) (result GoalFlowResult, retErr error) {
	if _, bounded := ctx.Deadline(); !bounded {
		return result, errors.New("native goal requires a finite deadline")
	}
	if req.Local.DiagnosticDir == "" {
		return result, errors.New("native goal requires its fixed diagnostic directory")
	}
	if runner == nil || req.Objective == "" || req.Local.Gate != "" && req.Local.Gate != "local" {
		return result, errors.New("native goal requires full local verification and a fixed objective")
	}
	if err := goalBinding(req.Publication); err != nil {
		return result, err
	}
	var mu sync.Mutex
	accepted := false
	binding := req.Publication
	native, err := runner.RunGoal(ctx, req.Objective, func(call context.Context) (toolPath string, toolErr error) {
		// Serial tool invocations cannot race publication of two snapshots. This is
		// mutual exclusion, not an outer scheduler or repair state machine.
		mu.Lock()
		defer mu.Unlock()
		accepted = false
		local, e := op.local(call, req.Local)
		defer func() {
			if cleanupErr := local.Close(); cleanupErr != nil {
				accepted = false
				toolErr = errors.New("verification snapshot cleanup failed")
			}
		}()
		result.DiagnosticPath = local.DiagnosticPath
		if e != nil {
			return local.DiagnosticPath, e
		}
		log, e := newGoalDiagnostics(req.Local.DiagnosticDir)
		if e != nil {
			return "", errors.New("cannot create full goal diagnostic file")
		}
		path := log.file.Name()
		result.DiagnosticPath = path
		finish := func(failure error) (string, error) {
			log.record("goal_verification_result", "", map[string]any{"complete": log.err == nil, "passed": failure == nil, "head": result.Head, "snapshot_sha256": local.SnapshotDigest, "error": goalError(failure)})
			closeErr := log.file.Close()
			if log.err != nil || closeErr != nil {
				return "", errors.New("goal verification diagnostics incomplete")
			}
			return path, failure
		}
		if e = appendGoalDiagnosticFile(log, local.DiagnosticPath, "local_checker"); e != nil {
			return finish(e)
		}
		candidate := binding
		candidate.CandidateDir = local.SnapshotRoot
		candidate.SnapshotDigest = local.SnapshotDigest
		published, e := op.publish(call, candidate)
		// A confirmed Git push is an effect even if GitHub's PR projection is
		// temporarily stale. Retain only the controller-returned exact head;
		// it still requires fresh validation and CI before acceptance.
		if published.Head != "" && published.PreviousHead == binding.ExpectedHead && published.SnapshotDigest == local.SnapshotDigest {
			binding.ExpectedHead = published.Head
			result.Head = published.Head
		}
		if e != nil {
			log.record("publication", "", map[string]any{"passed": false, "error": goalError(e)})
			return finish(e)
		}
		binding.ExpectedHead = published.Head
		candidate.ExpectedHead = published.Head
		result.Head = published.Head
		result.SnapshotDigest = local.SnapshotDigest
		log.record("publication", "", map[string]any{"head": published.Head, "number": published.Number, "snapshot_sha256": local.SnapshotDigest})
		observation, e := op.observe(call, candidate, published.Head, 30*time.Second)
		result.Checks = observation
		log.record("ci_observation", "", map[string]any{"observation": observation})
		if observation.WorkflowRunID > 0 {
			if de := op.diagnostics(call, candidate, observation, log); de != nil {
				return finish(de)
			}
		}
		if e != nil {
			return finish(e)
		}
		if observation.State != checkacceptance.Succeeded {
			return finish(errors.New("exact-head CI has not passed"))
		}
		// The goal may have concurrent tools. Never bind their later edits to checks
		// of the earlier published bytes.
		digest, de := goalSnapshot(call, req.Local.RepoRoot, "")
		if de != nil || digest != local.SnapshotDigest {
			return finish(errors.New("candidate changed during CI verification"))
		}
		path, e = finish(nil)
		accepted = e == nil
		return path, e
	})
	result.ThreadID = native.ThreadID
	if err != nil {
		return result, err
	}
	mu.Lock()
	defer mu.Unlock()
	if !accepted || native.Status != "complete" {
		return result, errors.New("native goal completed without exact-candidate acceptance")
	}
	digest, err := goalSnapshot(ctx, req.Local.RepoRoot, "")
	if err != nil || digest != result.SnapshotDigest {
		return result, errors.New("candidate changed after accepted verification")
	}
	finalBinding := binding
	observation, err := op.observe(ctx, finalBinding, result.Head, 30*time.Second)
	if err != nil || observation.State != checkacceptance.Succeeded {
		return result, errors.New("final exact-head CI observation is not successful")
	}
	result.Checks = observation
	return result, nil
}

// Each imported record remains explicitly source-labelled. A candidate cannot
// promote its test output to a trusted controller result by choosing JSON keys.
func appendGoalDiagnosticFile(log *goalDiagnostics, path, source string) error {
	f, err := os.Open(path)
	if err != nil {
		return errors.New("diagnostic file unavailable")
	}
	defer f.Close()
	reader := io.LimitReader(f, goalDiagnosticLimit+1)
	decoder := json.NewDecoder(reader)
	var total int64
	for {
		var record json.RawMessage
		err = decoder.Decode(&record)
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.New("diagnostic JSONL is incomplete")
		}
		total += int64(len(record))
		if total > goalDiagnosticLimit {
			return errors.New("diagnostic JSONL exceeds bound")
		}
		var value any
		valueDecoder := json.NewDecoder(bytes.NewReader(record))
		valueDecoder.UseNumber()
		if valueDecoder.Decode(&value) != nil {
			return errors.New("invalid diagnostic record")
		}
		log.record("diagnostic_record", "", map[string]any{"source": source, "record": value})
		if log.err != nil {
			return errors.New("combined diagnostics exceed bound")
		}
	}
	return nil
}
