package codexexec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk"
	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

// GoalResult reports native lifecycle facts, not acceptance of the workspace.
// Complete is only a candidate for the caller's independent verification.
type GoalResult struct{ ThreadID, Status string }

// RunGoal sets one persistent native goal. Codex Core owns all subsequent turns;
// the caller's objective owns the task, skill selection and verification criteria.
// verify is an optional fixed, no-argument host operation. It must honor its
// context and return a complete JSONL feedback path, including on check failure.
// The caller owns its authorization; no host commands or credentials are exposed.
// No exec transcript is parsed or retained by this runner.
func (e *Executor) RunGoal(ctx context.Context, objective string, verify func(context.Context) (string, error)) (GoalResult, error) {
	return e.runGoal(ctx, objective, verify, 30*time.Second)
}

func (e *Executor) runGoal(ctx context.Context, objective string, verify func(context.Context) (string, error), probeInterval time.Duration) (result GoalResult, runErr error) {
	fail := func(status string) (GoalResult, error) {
		if ctx.Err() != nil {
			status = "cancelled"
		}
		result.Status = status
		return result, errors.New(status)
	}
	// Native thread/goal/set trims the objective before storing it.
	objective = strings.TrimSpace(objective)
	if objective == "" {
		return fail("goal_objective_required")
	}
	verificationCtx, stopVerification := context.WithCancel(ctx)
	defer stopVerification()
	observation := &goalObservation{objective: objective, changed: make(chan struct{})}
	shellOverrides, err := e.goalShellOverrides()
	if err != nil {
		return fail("goal_isolation_unavailable")
	}
	args := append(e.runtimeOverrides(), shellOverrides...)
	args = append(args, "-c", "features.goals=true", "app-server")
	command, err := e.goalSandboxCommand(e.command(ctx, args...))
	if err != nil {
		return fail("goal_isolation_unavailable")
	}
	options := codexsdk.ClientOptions{
		Command: command.Args, Env: command.Env, CWD: e.config.Workspace,
		ServerNotificationHandler: observation.observe,
		ServerRequestHandler:      observation.verifier(verificationCtx, verify),
	}
	if verify != nil {
		experimental := true
		options.Initialize = protocolv2.InitializeParams{ClientInfo: protocolv2.ClientInfo{Name: "llm-go-upstream-goal", Version: "1"}, Capabilities: protocolv2.Value(protocolv2.InitializeCapabilities{ExperimentalAPI: &experimental})}
	}
	client, err := codexsdk.NewContext(ctx, options)
	if err != nil {
		return fail("goal_connect_failed")
	}
	defer func() {
		stopVerification()
		// Archive only the thread we created; never enumerate or alter other sessions.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if result.ThreadID != "" {
			if _, err := client.Threads().Archive(cleanupCtx, protocolv2.ThreadArchiveParams{ThreadID: result.ThreadID}); err != nil && runErr == nil {
				result.Status, runErr = "goal_cleanup_failed", errors.New("goal_cleanup_failed")
			}
		}
		closeCtx, closeCancel := context.WithTimeout(context.Background(), codexsdk.DefaultShutdownTimeout)
		defer closeCancel()
		shutdown, err := client.CloseContext(closeCtx)
		observation.mu.Lock()
		lateFailure := observation.failure
		stillComplete := observation.status == protocolv2.ThreadGoalStatusComplete && observation.activeTurn == "" && observation.completedTurn && !observation.verifying
		observation.mu.Unlock()
		if lateFailure != "" && runErr == nil {
			result.Status, runErr = lateFailure, errors.New(lateFailure)
		}
		if !stillComplete && runErr == nil {
			result.Status, runErr = "goal_completion_invalid", errors.New("goal_completion_invalid")
		}
		if ctx.Err() != nil && runErr == nil {
			result.Status, runErr = "cancelled", errors.New("cancelled")
		}
		if (err != nil || !shutdown.ProcessReaped || shutdown.CallbackWorkPending != 0) && runErr == nil {
			result.Status, runErr = "goal_transport_failed", errors.New("goal_transport_failed")
		}
	}()
	sandbox := protocolv2.SandboxModeWorkspaceWrite
	if e.config.ReadOnly {
		sandbox = protocolv2.SandboxModeReadOnly
	}
	params := protocolv2.ThreadStartParams{
		CWD: protocolv2.Value(e.config.Workspace), Model: protocolv2.Value(e.config.Model),
		ModelProvider:  protocolv2.Value("llm-go-exec"),
		ApprovalPolicy: protocolv2.Value(protocolv2.NewAskForApprovalNever()),
		Sandbox:        protocolv2.Value(sandbox), Ephemeral: protocolv2.Value(false),
	}
	if verify != nil {
		params.DynamicTools = protocolv2.Value([]protocolv2.DynamicToolSpec{verificationTool()})
	}
	start, err := client.Threads().Start(ctx, params)
	if err != nil {
		return fail("goal_thread_start_failed")
	}
	if !sessionPattern.MatchString(start.Thread.ID) {
		return fail("goal_thread_invalid")
	}
	result.ThreadID = start.Thread.ID
	if !e.goalThreadAdmitted(start) {
		return fail("goal_thread_rejected")
	}
	observation.mu.Lock()
	observation.threadID = result.ThreadID
	observation.mu.Unlock()
	set, err := client.Threads().GoalSet(ctx, protocolv2.ThreadGoalSetParams{
		ThreadID: result.ThreadID, Objective: protocolv2.Value(objective),
		Status: protocolv2.Value(protocolv2.ThreadGoalStatusActive),
	})
	if err != nil {
		return fail("goal_set_failed")
	}
	if set.Goal.ThreadID != result.ThreadID || set.Goal.Objective != objective || set.Goal.Status != protocolv2.ThreadGoalStatusActive {
		return fail("goal_set_invalid")
	}
	// This is a read-only connection probe, not an outer agent/repair loop.
	// Responses cannot establish completion; only the observed native lifecycle can.
	probe := time.NewTicker(probeInterval)
	defer probe.Stop()
	for {
		if ctx.Err() != nil {
			return fail("cancelled")
		}
		observation.mu.Lock()
		status := observation.failure
		changed := observation.changed
		ready := observation.status == protocolv2.ThreadGoalStatusComplete && observation.activeTurn == "" && observation.completedTurn && !observation.verifying
		observation.mu.Unlock()
		if status != "" {
			return fail(status)
		}
		if ready {
			result.Status = "complete"
			return result, nil
		}
		select {
		case <-ctx.Done():
			return fail("cancelled")
		case <-changed:
		case <-probe.C:
			probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			current, err := client.Threads().GoalGet(probeCtx, protocolv2.ThreadGoalGetParams{ThreadID: result.ThreadID})
			cancel()
			if err != nil {
				return fail("goal_transport_failed")
			}
			if current.Goal == nil || current.Goal.Value == nil || current.Goal.Value.ThreadID != result.ThreadID || current.Goal.Value.Objective != objective {
				return fail("goal_identity_invalid")
			}
		}
	}
}

func (e *Executor) goalThreadAdmitted(start protocolv2.ThreadStartResponse) bool {
	if _, ok := start.ApprovalPolicy.AsNever(); !ok {
		return false
	}
	if start.Thread.Ephemeral || len(start.Thread.Turns) != 0 || start.Thread.CWD != e.config.Workspace || start.Thread.ModelProvider != "llm-go-exec" || start.CWD != e.config.Workspace || start.Model != e.config.Model || start.ModelProvider != "llm-go-exec" {
		return false
	}
	if start.RuntimeWorkspaceRoots != nil {
		for _, root := range *start.RuntimeWorkspaceRoots {
			if filepath.Clean(root) != filepath.Clean(e.config.Workspace) {
				return false
			}
		}
	}
	if e.config.ReadOnly {
		policy, ok := start.Sandbox.AsReadOnly()
		return ok && (policy.NetworkAccess == nil || !*policy.NetworkAccess)
	}
	policy, ok := start.Sandbox.AsWorkspaceWrite()
	if !ok || (policy.NetworkAccess != nil && *policy.NetworkAccess) || policy.ExcludeSlashTmp == nil || !*policy.ExcludeSlashTmp || policy.ExcludeTmpdirEnvVar == nil || !*policy.ExcludeTmpdirEnvVar {
		return false
	}
	// The selected schema defines absent networkAccess=false and writableRoots=[].
	if policy.WritableRoots == nil {
		return true
	}
	for _, root := range *policy.WritableRoots {
		if filepath.Clean(root) != filepath.Clean(e.config.Workspace) {
			return false
		}
	}
	return true
}

// This is a bounded observation of the official goal/turn lifecycle, not an
// execution state machine: it never schedules work or responds to tool requests.
type goalObservation struct {
	mu                                       sync.Mutex
	threadID, objective, activeTurn, failure string
	status                                   protocolv2.ThreadGoalStatus
	completedTurn                            bool
	verifying                                bool
	changed                                  chan struct{}
}

func (s *goalObservation) observe(_ context.Context, event protocolv2.ServerNotification) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.signal()
	if s.failure != "" {
		return nil
	}
	if value, ok := event.AsThreadGoalUpdated(); ok {
		p := value.Params
		// Goal updates are client-wide broadcasts, including child goals.
		if p.ThreadID != s.threadID {
			return nil
		}
		if p.Goal.ThreadID != s.threadID {
			s.failure = "goal_identity_invalid"
			return nil
		}
		if p.Goal.Objective != s.objective {
			s.failure = "goal_identity_invalid"
			return nil
		}
		s.status = p.Goal.Status
		switch s.status {
		case protocolv2.ThreadGoalStatusActive, protocolv2.ThreadGoalStatusComplete:
		case protocolv2.ThreadGoalStatusPaused:
			s.failure = "goal_paused"
		case protocolv2.ThreadGoalStatusBlocked:
			s.failure = "goal_blocked"
		case protocolv2.ThreadGoalStatusUsageLimited:
			s.failure = "goal_usage_limited"
		case protocolv2.ThreadGoalStatusBudgetLimited:
			s.failure = "goal_budget_limited"
		default:
			s.failure = "goal_status_invalid"
		}
	} else if value, ok := event.AsTurnStarted(); ok {
		p := value.Params
		if p.ThreadID != s.threadID {
			s.failure = "goal_identity_invalid"
			return nil
		}
		if p.Turn.ID == "" || s.activeTurn != "" || p.Turn.Status != protocolv2.TurnStatusInProgress || s.status == protocolv2.ThreadGoalStatusComplete {
			s.failure = "goal_turn_sequence_invalid"
			return nil
		}
		s.activeTurn = p.Turn.ID
	} else if value, ok := event.AsTurnCompleted(); ok {
		p := value.Params
		if p.ThreadID != s.threadID {
			s.failure = "goal_identity_invalid"
			return nil
		}
		if p.Turn.ID == "" || p.Turn.ID != s.activeTurn {
			s.failure = "goal_turn_sequence_invalid"
			return nil
		}
		if p.Turn.Status != protocolv2.TurnStatusCompleted {
			s.failure = "goal_turn_failed"
			return nil
		}
		s.activeTurn, s.completedTurn = "", true
	} else if _, ok := event.AsThreadGoalCleared(); ok {
		s.failure = "goal_cleared"
	}
	return nil
}

// signal broadcasts an observation change without retaining event payloads.
// The caller holds mu.
func (s *goalObservation) signal() { close(s.changed); s.changed = make(chan struct{}) }

const goalVerificationTool = "verify_upstream"

func verificationTool() protocolv2.DynamicToolSpec {
	return protocolv2.NewDynamicToolSpecFunction(protocolv2.DynamicToolSpecFunction{
		Name:        goalVerificationTool,
		Description: "Run the fixed, controller-owned upstream verification route and return its complete feedback JSONL file path. A delivered path does not mean checks passed. No arguments.",
		InputSchema: protocolv2.JSONObject(map[string]protocolv2.JSONValue{
			"type":                 protocolv2.JSONString("object"),
			"properties":           protocolv2.JSONObject(map[string]protocolv2.JSONValue{}),
			"additionalProperties": protocolv2.JSONBool(false),
		}),
	})
}

// verifier grants exactly one caller-supplied operation, never arbitrary host
// commands or model-supplied arguments. The caller owns publishing/CI authority.
func (s *goalObservation) verifier(parent context.Context, verify func(context.Context) (string, error)) codexsdk.ServerRequestHandler {
	return func(handlerCtx context.Context, request protocolv2.ServerRequest) (codexsdk.ServerRequestResponse, error) {
		reject := func() (codexsdk.ServerRequestResponse, error) {
			s.mu.Lock()
			if s.failure == "" {
				s.failure = "goal_server_request_rejected"
			}
			s.signal()
			s.mu.Unlock()
			return codexsdk.ServerRequestResponse{}, errors.New("goal_server_request_rejected")
		}
		value, ok := request.AsItemToolCall()
		if !ok || verify == nil {
			return reject()
		}
		p := value.Params
		arguments, object := p.Arguments.AsObject()
		if p.Tool != goalVerificationTool || !object || len(arguments) != 0 || (p.Namespace != nil && p.Namespace.Value != nil) || p.CallID == "" || p.TurnID == "" {
			return reject()
		}
		callCtx, cancel := context.WithCancel(parent)
		stop := context.AfterFunc(handlerCtx, cancel)
		defer func() { stop(); cancel() }()
		// SDK server-request callbacks and notification callbacks run independently.
		// Wait briefly for the preceding turn/started observation, rather than invent
		// turn ownership from the action-bearing request itself.
		bindCtx, bindCancel := context.WithTimeout(callCtx, 5*time.Second)
		defer bindCancel()
		for {
			s.mu.Lock()
			if callCtx.Err() != nil || p.ThreadID != s.threadID || s.failure != "" || s.verifying {
				s.mu.Unlock()
				return reject()
			}
			if s.activeTurn == p.TurnID {
				s.verifying = true
				s.signal()
				s.mu.Unlock()
				break
			}
			changed := s.changed
			s.mu.Unlock()
			select {
			case <-changed:
			case <-bindCtx.Done():
				return reject()
			}
		}
		defer func() { s.mu.Lock(); s.verifying = false; s.signal(); s.mu.Unlock() }()
		path := invokeGoalVerification(callCtx, verify)
		// Only an existing regular absolute file path crosses back to the model.
		// Verification errors with such a path still deliver the complete feedback.
		// Raw callback errors, panic values and check output never enter the wire.
		delivered := false
		if callCtx.Err() == nil && len(path) > 0 && len(path) <= 4096 && filepath.IsAbs(path) && filepath.Clean(path) == path && strings.HasSuffix(path, ".jsonl") && !strings.ContainsAny(path, "\x00\r\n") {
			info, err := os.Lstat(path)
			delivered = err == nil && info.Mode().IsRegular()
		}
		if !delivered {
			s.mu.Lock()
			if s.failure == "" {
				s.failure = "goal_verification_failed"
			}
			s.signal()
			s.mu.Unlock()
			path = "verification_feedback_unavailable"
		}
		return codexsdk.DynamicToolResponse(protocolv2.DynamicToolCallResponse{
			Success: delivered,
			ContentItems: []protocolv2.DynamicToolCallOutputContentItem{
				protocolv2.NewDynamicToolCallOutputContentItemInputText(protocolv2.DynamicToolCallOutputContentItemInputText{Text: path}),
			},
		}), nil
	}
}

func invokeGoalVerification(ctx context.Context, verify func(context.Context) (string, error)) (path string) {
	defer func() {
		if recover() != nil {
			path = ""
		}
	}()
	if ctx.Err() != nil {
		return ""
	}
	path, _ = verify(ctx)
	return path
}
