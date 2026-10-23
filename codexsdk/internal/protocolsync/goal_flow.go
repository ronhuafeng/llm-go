package protocolsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	Progress    *GoalProgress
}
type GoalFlowResult struct {
	ThreadID, Head, SnapshotDigest, DiagnosticPath string
	NativeStatus                                   string
	// PublicationIntentHead is a write fact, never a confirmed effect or acceptance.
	PublicationIntentHead string
	VerificationBaseSHA   string
	ChangedPaths          []string
	Checks                checkacceptance.Observation
	// Reporting facts, not a second acceptance state. Failure never includes the
	// private native runtime error.
	Stage, Failure string
}

// RunUpstreamGoal performs no client-authored repair turns. Every verification
// and publication is requested by the same native goal, with immutable binding.
func RunUpstreamGoal(ctx context.Context, req GoalFlowRequest, runner GoalRunner) (GoalFlowResult, error) {
	return runUpstreamGoal(ctx, req, runner, goalFlowOperations{reconcile: ReconcileGoalPublication, local: VerifyGoal, publish: AppendGoalDraft, observe: ObserveGoalCI, diagnostics: collectGoalCIDiagnostics, refresh: refreshGoalBase, finalObserve: observeCompletedGoalCI})
}

type goalFlowOperations struct {
	reconcile    func(context.Context, GoalPublicationRequest) (GoalPublication, error)
	finalObserve func(context.Context, GoalPublicationRequest, string, time.Duration) (checkacceptance.Observation, error)
	refresh      func(context.Context, GoalVerifyRequest, GoalPublicationRequest) (GoalVerifyRequest, GoalPublicationRequest, func(), error)
	local        func(context.Context, GoalVerifyRequest) (GoalVerifyResult, error)
	publish      func(context.Context, GoalPublicationRequest) (GoalPublication, error)
	observe      func(context.Context, GoalPublicationRequest, string, time.Duration) (checkacceptance.Observation, error)
	diagnostics  func(context.Context, GoalPublicationRequest, checkacceptance.Observation, *goalDiagnostics) error
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
	stage := func(name string) {
		result.Stage = name
		req.Progress.group(name)
	}
	stage("Native goal")
	defer req.Progress.end()
	req.Local.Progress = req.Progress
	accepted := false
	binding := req.Publication
	binding.Progress = req.Progress
	native, err := runner.RunGoal(ctx, req.Objective, func(call context.Context) (toolPath string, toolErr error) {
		// Serial tool invocations cannot race publication of two snapshots. This is
		// mutual exclusion, not an outer scheduler or repair state machine.
		mu.Lock()
		defer mu.Unlock()
		accepted = false
		result.Failure = ""
		stage("Local verification")
		defer func() {
			if toolErr != nil {
				if result.Failure == "" {
					result.Failure = result.Stage + " failed; inspect retained verification diagnostics"
				}
				req.Progress.line(result.Failure)
			}
			req.Progress.end()
			req.Progress.line("Verification returned to the same native goal")
		}()
		// Reconcile a possibly completed write before spending another local check
		// or publishing. This is effect discovery, not acceptance or a retry.
		if op.reconcile != nil {
			observed, reconcileErr := op.reconcile(call, binding)
			if observed.Intent != nil {
				result.PublicationIntentHead = observed.Intent.IntendedHead
			}
			if observed.EffectState == GoalPublicationObserved && observed.Head != "" {
				binding.ExpectedHead = observed.Head
				result.Head = observed.Head
				result.SnapshotDigest = observed.SnapshotDigest
			}
			if reconcileErr != nil {
				stage("Publication reconciliation")
				result.Failure = "Publication outcome unresolved; no write was replayed"
				log, logErr := newGoalDiagnostics(req.Local.DiagnosticDir)
				if logErr != nil {
					return "", logErr
				}
				log.record("publication_reconciliation", "", map[string]any{"passed": false, "effect_state": observed.EffectState, "intent": observed.Intent, "error": goalError(reconcileErr)})
				closeErr := log.file.Close()
				if log.err != nil || closeErr != nil {
					return "", errors.New("publication reconciliation diagnostics incomplete")
				}
				result.DiagnosticPath = log.file.Name()
				return result.DiagnosticPath, reconcileErr
			}
		}
		localRequest := req.Local
		if op.refresh != nil {
			refreshedLocal, refreshedBinding, cleanup, refreshErr := op.refresh(call, localRequest, binding)
			if refreshErr != nil {
				result.Failure = "Current main cannot be rebound to the fixed goal target"
				log, logErr := newGoalDiagnostics(req.Local.DiagnosticDir)
				if logErr != nil {
					return "", logErr
				}
				log.record("accepted_base", "", map[string]any{"passed": false, "error": goalError(refreshErr)})
				closeErr := log.file.Close()
				if log.err != nil || closeErr != nil {
					return "", errors.New("accepted-base diagnostics incomplete")
				}
				return log.file.Name(), refreshErr
			}
			defer cleanup()
			localRequest, binding = refreshedLocal, refreshedBinding
		}
		local, e := op.local(call, localRequest)
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
		stage("Draft publication")
		published, e := op.publish(call, candidate)
		if published.Intent != nil {
			result.PublicationIntentHead = published.Intent.IntendedHead
		}
		// A confirmed Git push is an effect even if GitHub's PR projection is
		// temporarily stale. Retain only the controller-returned exact head;
		// it still requires fresh validation and CI before acceptance.
		if published.EffectState == GoalPublicationObserved && published.Head != "" && published.PreviousHead == binding.ExpectedHead && published.SnapshotDigest == local.SnapshotDigest {
			binding.ExpectedHead = published.Head
			result.Head = published.Head
			result.SnapshotDigest = published.SnapshotDigest
			result.ChangedPaths = published.ChangedPaths
		}
		if e != nil {
			log.record("publication", "", map[string]any{"passed": false, "effect_state": published.EffectState, "intent": published.Intent, "error": goalError(e)})
			result.Failure = "Draft publication could not be confirmed; inspect retained publication diagnostics"
			return finish(e)
		}
		binding.ExpectedHead = published.Head
		candidate.ExpectedHead = published.Head
		result.Head = published.Head
		result.SnapshotDigest = local.SnapshotDigest
		log.record("publication", "", map[string]any{"head": published.Head, "number": published.Number, "snapshot_sha256": local.SnapshotDigest, "changed_paths": published.ChangedPaths})
		req.Progress.line(fmt.Sprintf("Draft PR https://github.com/%s/pull/%d; candidate %s", candidate.Repository, candidate.Number, published.Head))
		stage("CI observation")
		observation, e := op.observe(call, candidate, published.Head, 30*time.Second)
		result.Checks = observation
		log.record("ci_observation", "", map[string]any{"observation": observation})
		if observation.WorkflowRunID > 0 {
			req.Progress.line("Collecting diagnostics for the observed CI attempt")
			if de := op.diagnostics(call, candidate, observation, log); de != nil {
				result.Failure = "CI diagnostics unavailable or incomplete"
				return finish(de)
			}
		}
		if e != nil {
			return finish(e)
		}
		if observation.State != checkacceptance.Succeeded {
			result.Failure = "Exact-head CI has not passed: " + goalCheckState(observation.State)
			return finish(errors.New("exact-head CI has not passed"))
		}
		// The goal may have concurrent tools. Never bind their later edits to checks
		// of the earlier published bytes.
		digest, de := goalSnapshot(call, req.Local.RepoRoot, "")
		if de != nil || digest != local.SnapshotDigest {
			result.Failure = "Candidate changed during CI verification"
			return finish(errors.New("candidate changed during CI verification"))
		}
		path, e = finish(nil)
		accepted = e == nil
		if accepted {
			result.VerificationBaseSHA = goalVerificationBase(binding)
			req.Progress.line("Verification passed for the published snapshot; native goal completion is still required")
		}
		return path, e
	})
	result.ThreadID = native.ThreadID
	result.NativeStatus = native.Status
	if err != nil {
		result.Failure = "Native goal stopped; inspect native status and the last verification diagnostics"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.Failure = "Native goal reached its execution deadline"
		} else if errors.Is(ctx.Err(), context.Canceled) {
			result.Failure = "Native goal was cancelled"
		}
		return result, err
	}
	mu.Lock()
	defer mu.Unlock()
	if !accepted || native.Status != "complete" {
		if result.Failure == "" {
			result.Failure = "Native goal ended without exact-candidate acceptance"
		}
		return result, errors.New("native goal completed without exact-candidate acceptance")
	}
	stage("Final acceptance")
	digest, err := goalSnapshot(ctx, req.Local.RepoRoot, "")
	if err != nil || digest != result.SnapshotDigest {
		result.Failure = "Candidate changed after accepted verification"
		return result, errors.New("candidate changed after accepted verification")
	}
	finalBinding := binding
	finalObserve := op.finalObserve
	if finalObserve == nil {
		finalObserve = op.observe
	}
	observation, err := finalObserve(ctx, finalBinding, result.Head, 30*time.Second)
	result.Checks = observation
	if err != nil || observation.State != checkacceptance.Succeeded {
		result.Failure = "Final exact-head CI observation is not successful"
		return result, errors.New("final exact-head CI observation is not successful")
	}
	req.Progress.line("Native goal complete; unchanged snapshot and final exact-head CI passed")
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
