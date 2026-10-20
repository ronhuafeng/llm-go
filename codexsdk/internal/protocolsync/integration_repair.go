package protocolsync

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
)

// IntegrationRepairRequest belongs to the trusted integration controller. Local
// binds the isolated proposal, current main, original accepted baseline and fixed
// upstream facts; none are chosen by the model. Current is a read-only guard for
// those bindings. The repair runtime has no publication or integration identity.
type IntegrationRepairRequest struct {
	Objective string
	Local     GoalVerifyRequest
	Current   func(context.Context) error
	Progress  *GoalProgress
}

// IntegrationRepairResult is local repair evidence, never CI acceptance. On
// success the caller owns Snapshot and must Close it after consuming its frozen
// bytes. Publication and all four exact-head CI checks belong to the separate
// trusted effect controller. Head and Checks remain empty here.
type IntegrationRepairResult struct {
	GoalFlowResult
	Snapshot GoalVerifyResult
}

// RunIntegrationRepair starts exactly one fresh native goal. It never resumes a
// sync goal, chooses Git conflict sides, publishes, dispatches CI or renews the
// integration deadline. The runner must use the existing isolated GoalSandbox.
func RunIntegrationRepair(ctx context.Context, req IntegrationRepairRequest, runner GoalRunner) (IntegrationRepairResult, error) {
	return runIntegrationRepair(ctx, req, runner, VerifyGoal)
}

func runIntegrationRepair(ctx context.Context, req IntegrationRepairRequest, runner GoalRunner, verify func(context.Context, GoalVerifyRequest) (GoalVerifyResult, error)) (result IntegrationRepairResult, retErr error) {
	if _, bounded := ctx.Deadline(); !bounded {
		return result, errors.New("integration repair requires the existing finite deadline")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if runner == nil || strings.TrimSpace(req.Objective) == "" || req.Current == nil || verify == nil {
		return result, errors.New("integration repair requires a fixed objective and trusted binding guard")
	}
	if req.Local.Gate != "" && req.Local.Gate != "local" {
		return result, errors.New("integration repair requires all local gates")
	}
	if !filepath.IsAbs(req.Local.RepoRoot) {
		return result, errors.New("integration repair requires an absolute isolated proposal path")
	}
	for _, path := range []string{req.Local.BaseRoot, req.Local.OriginalBaseRoot, req.Local.CandidateDir, req.Local.TrustedCacheRoot, req.Local.DiagnosticDir} {
		if !filepath.IsAbs(path) || goalWithin(req.Local.RepoRoot, path) || goalWithin(path, req.Local.RepoRoot) {
			return result, errors.New("integration repair requires fixed absolute trusted paths outside the proposal workspace")
		}
	}
	if err := req.Current(ctx); err != nil {
		result.Failure = "Integration repair binding is stale or unavailable"
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, result.Snapshot.Close())
		}
	}()

	// Only cancel the fresh runtime on a repeated failed snapshot. This preserves
	// the original controller deadline; there is no outer restart or new budget.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	var terminal error
	failed := map[string]bool{}
	acceptingTools := true
	req.Local.Progress = req.Progress
	stage := func(name string) {
		result.Stage = name
		req.Progress.group(name)
	}
	stop := func(err error) error {
		terminal = err
		cancel()
		return err
	}
	stage("Fresh integration repair")
	defer req.Progress.end()
	native, runErr := runner.RunGoal(runCtx, req.Objective, func(call context.Context) (path string, toolErr error) {
		mu.Lock()
		defer mu.Unlock()
		if !acceptingTools {
			return "", errors.New("integration repair goal already ended")
		}
		if terminal != nil {
			return result.DiagnosticPath, terminal
		}
		if err := result.Snapshot.Close(); err != nil {
			result.Failure = "Integration repair snapshot cleanup failed"
			return result.DiagnosticPath, stop(errors.New(result.Failure))
		}
		result.Snapshot = GoalVerifyResult{}
		if err := runCtx.Err(); err != nil {
			return result.DiagnosticPath, stop(err)
		}
		if err := call.Err(); err != nil {
			return result.DiagnosticPath, stop(err)
		}
		// A tool context cannot extend the controller budget; its cancellation can
		// only shorten this invocation, including during local subprocess checks.
		callCtx, cancelCall := context.WithCancel(runCtx)
		stopCall := context.AfterFunc(call, cancelCall)
		defer stopCall()
		defer cancelCall()
		result.Failure = ""
		stage("Integration repair local verification")
		defer req.Progress.end()
		if err := req.Current(callCtx); err != nil {
			result.Failure = "Integration repair binding changed"
			return result.DiagnosticPath, stop(err)
		}
		digest, err := goalSnapshot(callCtx, req.Local.RepoRoot, "")
		if err != nil {
			result.Failure = "Integration repair candidate cannot be frozen"
			return result.DiagnosticPath, stop(err)
		}
		if failed[digest] {
			result.Failure = "Integration repair repeated an unchanged failed snapshot"
			return result.DiagnosticPath, stop(errors.New(result.Failure))
		}
		defer func() {
			if toolErr != nil {
				failed[digest] = true
			}
		}()
		local, err := verify(callCtx, req.Local)
		result.DiagnosticPath = local.DiagnosticPath
		if err == nil {
			err = errors.Join(callCtx.Err(), call.Err())
		}
		if err == nil && (local.SnapshotDigest != digest || local.SnapshotRoot == "") {
			err = errors.New("verified repair snapshot changed")
		}
		if err != nil {
			result.Failure = "Integration repair local verification failed"
			if cleanupErr := local.Close(); cleanupErr != nil {
				return local.DiagnosticPath, stop(errors.Join(err, cleanupErr))
			}
			return local.DiagnosticPath, err
		}
		result.Snapshot = local
		result.SnapshotDigest = local.SnapshotDigest
		return local.DiagnosticPath, nil
	})
	mu.Lock()
	defer mu.Unlock()
	acceptingTools = false
	result.ThreadID, result.NativeStatus = native.ThreadID, native.Status
	if terminal != nil {
		if result.Failure == "" {
			result.Failure = "Integration repair verification was cancelled or blocked"
		}
		return result, terminal
	}
	if err := ctx.Err(); err != nil {
		result.Failure = "Integration repair reached its deadline or was cancelled"
		return result, err
	}
	if runErr != nil {
		result.Failure = "Fresh integration repair goal stopped without local completion"
		return result, runErr
	}
	if native.Status != "complete" || native.ThreadID == "" || result.Snapshot.SnapshotRoot == "" {
		if result.Failure == "" {
			result.Failure = "Fresh integration repair goal ended without a verified candidate"
		}
		return result, errors.New(result.Failure)
	}
	stage("Integration repair final local verification")
	if err := req.Current(ctx); err != nil {
		result.Failure = "Integration repair binding changed before local completion"
		return result, err
	}
	for _, root := range []string{req.Local.RepoRoot, result.Snapshot.SnapshotRoot} {
		if digest, err := goalSnapshot(ctx, root, ""); err != nil || digest != result.SnapshotDigest {
			result.Failure = "Integration repair candidate changed after verification"
			return result, errors.New(result.Failure)
		}
	}
	return result, ctx.Err()
}
