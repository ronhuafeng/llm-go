package codexcaller

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk"
	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
	"github.com/ronhuafeng/llm-go/llmkit/llmadapter"
)

type fakeRunner struct {
	requests       []codexsdk.StartThreadRunRequest
	streamRequests []codexsdk.StartThreadRunRequest
	result         codexsdk.StartedThreadRun
	err            error
	streamErr      error
}

type fakeStartedRunStream struct {
	result        codexsdk.StartedThreadRun
	err           error
	notifications []protocolv2.ServerNotification
	index         int
	current       protocolv2.ServerNotification
	closed        bool
}

func (stream *fakeStartedRunStream) Next(context.Context) bool {
	if stream.index >= len(stream.notifications) {
		return false
	}
	stream.current = stream.notifications[stream.index]
	stream.index++
	return true
}

func (stream *fakeStartedRunStream) Notification() protocolv2.ServerNotification {
	return stream.current
}

func (stream *fakeStartedRunStream) Wait(context.Context) (codexsdk.StartedThreadRun, error) {
	return stream.result, stream.err
}

func (stream *fakeStartedRunStream) Result() (codexsdk.StartedThreadRun, bool) {
	return stream.result, true
}

func (stream *fakeStartedRunStream) Err() error { return stream.err }

func (stream *fakeStartedRunStream) Close() error {
	stream.closed = true
	return nil
}

func (runner *fakeRunner) Start(ctx context.Context, request codexsdk.StartThreadRunRequest) (codexsdk.StartedThreadRun, error) {
	if err := ctx.Err(); err != nil {
		return codexsdk.StartedThreadRun{}, err
	}
	runner.requests = append(runner.requests, request)
	if err := applyAdmission(request, runner.result.Start); err != nil {
		return codexsdk.StartedThreadRun{Start: runner.result.Start}, err
	}
	return runner.result, runner.err
}

func (runner *fakeRunner) StartStream(ctx context.Context, request codexsdk.StartThreadRunRequest) (*codexsdk.Stream[codexsdk.StartedThreadRun], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runner.streamRequests = append(runner.streamRequests, request)
	if err := applyAdmission(request, runner.result.Start); err != nil {
		return nil, err
	}
	return nil, runner.streamErr
}

func applyAdmission(request codexsdk.StartThreadRunRequest, start protocolv2.ThreadStartResponse) error {
	if start.Thread.ID == "" || request.AdmitTurn == nil {
		return nil
	}
	pending := request.Turn
	pending.ThreadID = start.Thread.ID
	return request.AdmitTurn(start, pending)
}

func applicationOptions(runner ThreadRunner) Options {
	return Options{
		Runner: runner,
		Defaults: codexsdk.StartThreadRunRequest{
			Thread: protocolv2.ThreadStartParams{
				ApprovalPolicy: protocolv2.Value(protocolv2.NewAskForApprovalNever()),
				Ephemeral:      protocolv2.Value(true),
				Sandbox:        protocolv2.Value(protocolv2.SandboxModeReadOnly),
			},
			Turn: protocolv2.TurnStartParams{
				ApprovalPolicy: protocolv2.Value(protocolv2.NewAskForApprovalNever()),
				SandboxPolicy:  protocolv2.Value(protocolv2.NewSandboxPolicyReadOnly(protocolv2.SandboxPolicyReadOnly{})),
			},
			AdmitTurn: applicationReadOnlyAdmission,
		},
	}
}

func applicationReadOnlyAdmission(start protocolv2.ThreadStartResponse, pending protocolv2.TurnStartParams) error {
	if !start.ApprovalPolicy.IsValid() || start.ApprovalPolicy.Kind() != protocolv2.AskForApprovalKindNever {
		return errors.New("application: approval policy rejected")
	}
	if !start.Sandbox.IsValid() || start.Sandbox.Kind() != protocolv2.SandboxPolicyKindReadOnly {
		return errors.New("application: sandbox policy rejected")
	}
	if !start.Thread.Ephemeral {
		return errors.New("application: non-ephemeral thread rejected")
	}
	if pending.ApprovalPolicy != nil && (pending.ApprovalPolicy.Value == nil || !pending.ApprovalPolicy.Value.IsValid() || pending.ApprovalPolicy.Value.Kind() != protocolv2.AskForApprovalKindNever) {
		return errors.New("application: pending approval policy rejected")
	}
	if pending.SandboxPolicy != nil && (pending.SandboxPolicy.Value == nil || !pending.SandboxPolicy.Value.IsValid() || pending.SandboxPolicy.Value.Kind() != protocolv2.SandboxPolicyKindReadOnly) {
		return errors.New("application: pending sandbox policy rejected")
	}
	return nil
}

func acceptingAdmission(protocolv2.ThreadStartResponse) error { return nil }

func newApplicationCaller(t *testing.T, runner ThreadRunner) *Caller {
	t.Helper()
	caller, err := New(applicationOptions(runner))
	if err != nil {
		t.Fatal(err)
	}
	return caller
}

func requireUnknownModel(t *testing.T, evidence llmadapter.ExecutionEvidence) {
	t.Helper()
	if got, ok := evidence.Model.Value(); ok {
		t.Fatalf("Model = (%q, true), want unknown", got)
	}
}

func requireUnknownProvider(t *testing.T, evidence llmadapter.ExecutionEvidence) {
	t.Helper()
	if got, ok := evidence.ProviderName.Value(); ok {
		t.Fatalf("ProviderName = (%q, true), want unknown", got)
	}
}

func requireObservedCount(t *testing.T, got llmadapter.Observation[int64], want int64) {
	t.Helper()
	value, ok := got.Value()
	if !ok || value != want {
		t.Fatalf("count = (%d, %t), want observed %d", value, ok, want)
	}
}

var _ ThreadRunner = (*fakeRunner)(nil)
var _ llmadapter.Caller = (*Caller)(nil)

func TestNewRequiresApplicationOwnedAdmission(t *testing.T) {
	runner := &fakeRunner{}
	if _, err := New(Options{Runner: runner}); !errors.Is(err, ErrMissingAdmission) {
		t.Fatalf("New error = %v, want ErrMissingAdmission", err)
	}
	if len(runner.requests) != 0 || len(runner.streamRequests) != 0 {
		t.Fatalf("runner invoked: starts=%d streams=%d", len(runner.requests), len(runner.streamRequests))
	}
}

func TestNewValidatesRunnerAndAdapterOwnedTurnFields(t *testing.T) {
	if _, err := New(Options{}); !errors.Is(err, ErrNilThreadRunner) {
		t.Fatalf("New error = %v, want ErrNilThreadRunner", err)
	}
	var typedNil *fakeRunner
	if _, err := New(Options{Runner: typedNil}); !errors.Is(err, ErrNilThreadRunner) {
		t.Fatalf("typed nil error = %v", err)
	}
	runner := &fakeRunner{}
	for _, defaults := range []protocolv2.TurnStartParams{
		{ThreadID: "owned"},
		{Input: []protocolv2.UserInput{}},
		{OutputSchema: outputSchemaPointer(t, `true`)},
	} {
		options := applicationOptions(runner)
		options.Defaults.Turn.ThreadID = defaults.ThreadID
		options.Defaults.Turn.Input = defaults.Input
		options.Defaults.Turn.OutputSchema = defaults.OutputSchema
		if _, err := New(options); err == nil {
			t.Fatalf("New accepted conflicting adapter-owned fields: %#v", defaults)
		}
	}
}

func TestAdapterPreservesApplicationExecutionPolicyAndAdmission(t *testing.T) {
	run := validStartedRun("ok", "gpt")
	runner := &fakeRunner{result: run}
	calls := 0
	options := Options{
		Runner: runner,
		Defaults: codexsdk.StartThreadRunRequest{
			Thread: protocolv2.ThreadStartParams{
				ApprovalPolicy: protocolv2.Value(protocolv2.NewAskForApprovalOnRequest()),
				Ephemeral:      protocolv2.Value(false),
				Sandbox:        protocolv2.Value(protocolv2.SandboxModeDangerFullAccess),
			},
			Turn: protocolv2.TurnStartParams{
				ApprovalPolicy: protocolv2.Value(protocolv2.NewAskForApprovalOnRequest()),
				SandboxPolicy:  protocolv2.Value(protocolv2.NewSandboxPolicyDangerFullAccess()),
			},
			AdmitTurn: func(start protocolv2.ThreadStartResponse, pending protocolv2.TurnStartParams) error {
				calls++
				if start.Thread.ID != "thread-1" {
					t.Fatalf("admission start = %#v", start)
				}
				if pending.ThreadID != start.Thread.ID {
					t.Fatalf("pending turn thread id = %q, want %q", pending.ThreadID, start.Thread.ID)
				}
				return nil
			},
		},
	}
	caller, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := caller.CallDetailed(context.Background(), validRequest()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("application admission calls = %d, want 1", calls)
	}
	request := runner.requests[0]
	if request.AdmitTurn == nil {
		t.Fatal("application AdmitTurn was dropped")
	}
	if request.Thread.Ephemeral == nil || request.Thread.Ephemeral.Value == nil || *request.Thread.Ephemeral.Value {
		t.Fatalf("adapter rewrote application ephemeral default: %#v", request.Thread.Ephemeral)
	}
	if request.Thread.Sandbox == nil || request.Thread.Sandbox.Value == nil || *request.Thread.Sandbox.Value != protocolv2.SandboxModeDangerFullAccess {
		t.Fatalf("adapter rewrote application sandbox default: %#v", request.Thread.Sandbox)
	}
	if request.Thread.ApprovalPolicy == nil || request.Thread.ApprovalPolicy.Value == nil || request.Thread.ApprovalPolicy.Value.Kind() != protocolv2.AskForApprovalKindOnRequest {
		t.Fatalf("adapter rewrote application approval default: %#v", request.Thread.ApprovalPolicy)
	}
}

func TestApplicationAdmissionRejectsBeforeTurnAcrossCallPaths(t *testing.T) {
	admissionErr := errors.New("application: execution not authorized")
	paths := []struct {
		name string
		call func(*Caller) error
	}{
		{name: "Call", call: func(c *Caller) error { _, err := c.Call(context.Background(), validRequest()); return err }},
		{name: "CallDetailed", call: func(c *Caller) error { _, err := c.CallDetailed(context.Background(), validRequest()); return err }},
		{name: "CallStream", call: func(c *Caller) error { _, err := c.CallStream(context.Background(), validRequest()); return err }},
	}
	for _, path := range paths {
		t.Run(path.name, func(t *testing.T) {
			runner := &fakeRunner{result: validStartedRun("must-not-execute", "gpt")}
			options := applicationOptions(runner)
			options.Defaults.AdmitTurn = func(protocolv2.ThreadStartResponse, protocolv2.TurnStartParams) error { return admissionErr }
			caller, err := New(options)
			if err != nil {
				t.Fatal(err)
			}
			if path.name == "Call" {
				response, err := caller.Call(context.Background(), validRequest())
				if !errors.Is(err, admissionErr) {
					t.Fatalf("error = %v, want application admission cause", err)
				}
				requireUnknownModel(t, response.Execution)
				requireUnknownProvider(t, response.Execution)
				details, ok := response.BackendDetails.(Details)
				if !ok || details.Run.Start.Model != "gpt" {
					t.Fatalf("admission rejection dropped exact start model: %#v", response.BackendDetails)
				}
				return
			}
			if err := path.call(caller); !errors.Is(err, admissionErr) {
				t.Fatalf("error = %v, want application admission cause", err)
			}
		})
	}
}

func TestCallerBuildsExactRequestAndProjectsEvidence(t *testing.T) {
	run := validStartedRun("final", "gpt-start")
	run.Run.Usage = &protocolv2.ThreadTokenUsage{Total: protocolv2.TokenUsageBreakdown{
		InputTokens: 11, CachedInputTokens: 3, OutputTokens: 5, ReasoningOutputTokens: 2,
	}}
	run.Run.Notifications = []protocolv2.ServerNotification{modelRerouted("gpt-start", "gpt-rerouted")}
	runner := &fakeRunner{result: run}
	options := applicationOptions(runner)
	options.Defaults.Thread.Model = protocolv2.Value("gpt-request")
	options.Defaults.Thread.RuntimeWorkspaceRoots = protocolv2.Value([]string{"/workspace"})
	options.Defaults.Turn.Effort = protocolv2.Value(protocolv2.ReasoningEffort("high"))
	caller, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	response, err := caller.Call(context.Background(), llmadapter.Request{
		Prompt:       "answer as JSON",
		OutputSchema: json.RawMessage(`{"type":"object","required":["answer"],"properties":{"answer":{"type":"boolean"}}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.FinalResponse != "final" || response.Execution.BackendName != "codex" {
		t.Fatalf("response = %#v", response)
	}
	requireUnknownProvider(t, response.Execution)
	requireUnknownModel(t, response.Execution)
	if response.Execution.Usage == nil {
		t.Fatal("missing usage")
	}
	requireObservedCount(t, response.Execution.Usage.Input, 11)
	requireObservedCount(t, response.Execution.Usage.ReasoningOutput, 2)
	details, ok := response.BackendDetails.(Details)
	if !ok || details.BackendName() != "codex" || !reflect.DeepEqual(details.Run, run) {
		t.Fatalf("details = %#v", response.BackendDetails)
	}
	request := runner.requests[0]
	if request.Turn.ThreadID != "" || len(request.Turn.Input) != 1 || request.Turn.OutputSchema == nil {
		t.Fatalf("adapter-owned turn fields = %#v", request.Turn)
	}
	text, ok := request.Turn.Input[0].AsText()
	if !ok || text.Text != "answer as JSON" {
		t.Fatalf("turn input = %#v", request.Turn.Input)
	}
	if request.Thread.Model == nil || request.Thread.Model.Value == nil || *request.Thread.Model.Value != "gpt-request" {
		t.Fatalf("exact defaults were not preserved: %#v", request.Thread)
	}
}

func TestNeutralUsageProjectsAttemptTotalNotLastProviderCall(t *testing.T) {
	run := validStartedRun("ok", "gpt-start")
	run.Run.Usage = &protocolv2.ThreadTokenUsage{
		Last: protocolv2.TokenUsageBreakdown{
			InputTokens: 1, CachedInputTokens: 7, OutputTokens: 3, ReasoningOutputTokens: 4,
		},
		Total: protocolv2.TokenUsageBreakdown{
			InputTokens: 100, CachedInputTokens: 0, OutputTokens: 50, ReasoningOutputTokens: 20,
		},
	}
	caller := newApplicationCaller(t, &fakeRunner{result: run})
	response, err := caller.Call(context.Background(), validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if response.Execution.Usage == nil {
		t.Fatal("missing attempt-scoped usage")
	}
	requireObservedCount(t, response.Execution.Usage.Input, 100)
	requireObservedCount(t, response.Execution.Usage.CachedInput, 0)
	requireObservedCount(t, response.Execution.Usage.Output, 50)
	requireObservedCount(t, response.Execution.Usage.ReasoningOutput, 20)
}

func TestCallerPreservesPartialRunAndCause(t *testing.T) {
	providerErr := errors.New("turn failed")
	run := validStartedRun("", "gpt-start")
	run.Run.Turn.Status = protocolv2.TurnStatusFailed
	run.Run.FinalResponse = "partial"
	runner := &fakeRunner{result: run, err: providerErr}
	caller := newApplicationCaller(t, runner)
	response, err := caller.Call(context.Background(), validRequest())
	if !errors.Is(err, providerErr) {
		t.Fatalf("error = %v, want provider cause", err)
	}
	if response.FinalResponse != "partial" || response.Execution.BackendName != "codex" {
		t.Fatalf("partial response = %#v", response)
	}
	requireUnknownProvider(t, response.Execution)
	details, ok := response.BackendDetails.(Details)
	if !ok || details.Run.Start.Thread.ID == "" || details.Run.Run.Turn.Status != protocolv2.TurnStatusFailed {
		t.Fatalf("partial details = %#v", details)
	}
}

func TestCallDetailedAndStreamShareRequestConstruction(t *testing.T) {
	streamErr := errors.New("stream unavailable")
	runner := &fakeRunner{result: validStartedRun("ok", "gpt"), streamErr: streamErr}
	caller := newApplicationCaller(t, runner)
	request := validRequest()
	if _, err := caller.CallDetailed(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := caller.CallStream(context.Background(), request); !errors.Is(err, streamErr) {
		t.Fatalf("CallStream error = %v", err)
	}
	if len(runner.requests) != 1 || len(runner.streamRequests) != 1 {
		t.Fatalf("run requests = %d stream requests = %d", len(runner.requests), len(runner.streamRequests))
	}
	left, _ := json.Marshal(runner.requests[0].Turn)
	right, _ := json.Marshal(runner.streamRequests[0].Turn)
	if string(left) != string(right) {
		t.Fatalf("request construction differs:\n%s\n%s", left, right)
	}
}

func TestCallIsProjectionOfDetailedResult(t *testing.T) {
	run := validStartedRun("ok", "gpt")
	runner := &fakeRunner{result: run}
	caller := newApplicationCaller(t, runner)
	detailed, err := caller.CallDetailed(context.Background(), validRequest())
	if err != nil {
		t.Fatal(err)
	}
	want := responseFromRun(detailed)
	got, err := caller.Call(context.Background(), validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || got.Execution.Usage != nil {
		t.Fatalf("Call projection = %#v, want %#v", got, want)
	}
}

func TestServedModelStaysUnknownWithoutAttemptScopedEvidence(t *testing.T) {
	cases := []struct {
		name  string
		run   func() codexsdk.StartedThreadRun
		check func(*testing.T, llmadapter.Response)
	}{
		{
			name: "thread-start model only",
			run: func() codexsdk.StartedThreadRun {
				run := validStartedRun("ok", "gpt-start")
				run.Run.Notifications = nil
				return run
			},
			check: func(t *testing.T, response llmadapter.Response) {
				t.Helper()
				requireUnknownModel(t, response.Execution)
				details := response.BackendDetails.(Details)
				if details.Run.Start.Model != "gpt-start" {
					t.Fatalf("exact start model = %q", details.Run.Start.Model)
				}
			},
		},
		{
			name: "matching no-op reroute is not served-model evidence",
			run: func() codexsdk.StartedThreadRun {
				return validStartedRun("ok", "gpt-start")
			},
			check: func(t *testing.T, response llmadapter.Response) {
				t.Helper()
				requireUnknownModel(t, response.Execution)
				requireRerouteToModel(t, response, "gpt-start")
			},
		},
		{
			name: "response-scoped reroute stays in backend details",
			run: func() codexsdk.StartedThreadRun {
				run := validStartedRun("ok", "gpt-start")
				run.Run.Notifications = []protocolv2.ServerNotification{modelRerouted("gpt-start", "gpt-rerouted")}
				return run
			},
			check: func(t *testing.T, response llmadapter.Response) {
				t.Helper()
				requireUnknownModel(t, response.Execution)
				requireRerouteToModel(t, response, "gpt-rerouted")
				details := response.BackendDetails.(Details)
				if details.Run.Start.Model != "gpt-start" {
					t.Fatalf("exact start model = %q", details.Run.Start.Model)
				}
			},
		},
		{
			name: "multiple reroutes leave attempt model unknown",
			run: func() codexsdk.StartedThreadRun {
				run := validStartedRun("ok", "gpt-start")
				run.Run.Notifications = []protocolv2.ServerNotification{
					modelRerouted("gpt-start", "gpt-a"),
					modelRerouted("gpt-a", "gpt-b"),
				}
				return run
			},
			check: func(t *testing.T, response llmadapter.Response) {
				t.Helper()
				requireUnknownModel(t, response.Execution)
				requireRerouteToModel(t, response, "gpt-b")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caller := newApplicationCaller(t, &fakeRunner{result: tc.run()})
			response, err := caller.Call(context.Background(), validRequest())
			if err != nil {
				t.Fatal(err)
			}
			requireUnknownProvider(t, response.Execution)
			tc.check(t, response)
		})
	}
}

func requireRerouteToModel(t *testing.T, response llmadapter.Response, want string) {
	t.Helper()
	details, ok := response.BackendDetails.(Details)
	if !ok {
		t.Fatalf("BackendDetails = %#v", response.BackendDetails)
	}
	var to string
	found := false
	for _, notification := range details.Run.Run.Notifications {
		if rerouted, ok := notification.AsModelRerouted(); ok {
			to = rerouted.Params.ToModel
			found = true
		}
	}
	if !found || to != want {
		t.Fatalf("reroute ToModel = (%q, %t), want %q", to, found, want)
	}
}

func TestCallDoesNotFillUnknownModelFromRequestedDefault(t *testing.T) {
	run := validStartedRun("ok", "unused")
	run.Start = protocolv2.ThreadStartResponse{}
	run.Run.Notifications = nil
	runner := &fakeRunner{result: run}
	options := applicationOptions(runner)
	options.Defaults.Thread.Model = protocolv2.Value("gpt-requested")
	caller, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	response, err := caller.Call(context.Background(), validRequest())
	if err != nil {
		t.Fatal(err)
	}
	requireUnknownModel(t, response.Execution)
	requireUnknownProvider(t, response.Execution)
}

func TestCallerWorksThroughLLMAdapterTypedPath(t *testing.T) {
	runner := &fakeRunner{result: validStartedRun(`{"answer":true}`, "gpt")}
	caller := newApplicationCaller(t, runner)
	result, err := llmadapter.Value[map[string]bool](context.Background(), caller, "answer")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Value["answer"] || result.Response.Execution.BackendName != "codex" {
		t.Fatalf("result = %#v", result)
	}
	requireUnknownProvider(t, result.Response.Execution)
}

func TestStreamPreservesProviderCauseAndExactEvidence(t *testing.T) {
	providerErr := errors.New("provider quota")
	run := validStartedRun("partial", "gpt")
	run.Run = codexsdk.ThreadRunResult{}
	run.Run.Diagnostics = []codexsdk.DiagnosticRef{{Kind: "provider", Path: "thread/start"}}
	caller := newApplicationCaller(t, &fakeRunner{result: run})
	inner := &fakeStartedRunStream{result: run, err: providerErr}
	stream := caller.wrapStream(inner, nil)
	got, err := stream.Wait(context.Background())
	if !errors.Is(err, providerErr) {
		t.Fatalf("Wait error = %v", err)
	}
	if !reflect.DeepEqual(got, run) {
		t.Fatalf("Wait result = %#v, want %#v", got, run)
	}
	if err := stream.Close(); err != nil || !inner.closed {
		t.Fatalf("Close = %v, closed=%v", err, inner.closed)
	}
}

func validRequest() llmadapter.Request {
	return llmadapter.Request{
		Prompt:       "prompt",
		OutputSchema: json.RawMessage(`{"type":"object","required":["answer"],"properties":{"answer":{"type":"string"}}}`),
	}
}

func validStartedRun(final, model string) codexsdk.StartedThreadRun {
	phase := protocolv2.Value(protocolv2.MessagePhaseFinalAnswer)
	items := []protocolv2.ThreadItem{}
	if final != "" {
		items = append(items, protocolv2.NewThreadItemAgentMessage(protocolv2.ThreadItemAgentMessage{
			ID: "item-1", Text: final, Phase: phase,
		}))
	}
	return codexsdk.StartedThreadRun{
		Start: protocolv2.ThreadStartResponse{
			ApprovalPolicy:    protocolv2.NewAskForApprovalNever(),
			ApprovalsReviewer: protocolv2.ApprovalsReviewerUser,
			CWD:               "/workspace",
			Model:             model,
			ModelProvider:     "openai",
			Sandbox:           protocolv2.NewSandboxPolicyReadOnly(protocolv2.SandboxPolicyReadOnly{}),
			Thread: protocolv2.Thread{
				CliVersion: "test", CWD: "/workspace", Ephemeral: true, ID: "thread-1",
				ModelProvider: "openai", Preview: "preview", SessionID: "session-1",
				Source: protocolv2.NewSessionSourceAppServer(), Status: protocolv2.NewThreadStatusIdle(),
				Turns: []protocolv2.Turn{},
			},
		},
		Run: codexsdk.ThreadRunResult{
			Turn:          protocolv2.Turn{ID: "turn-1", Items: items, Status: protocolv2.TurnStatusCompleted},
			FinalResponse: final,
			Notifications: []protocolv2.ServerNotification{modelRerouted(model, model)},
		},
	}
}

func modelRerouted(from, to string) protocolv2.ServerNotification {
	return protocolv2.NewServerNotificationModelRerouted(protocolv2.ServerNotificationModelRerouted{
		Params: protocolv2.ModelReroutedNotification{
			FromModel: from, ToModel: to, Reason: protocolv2.ModelRerouteReasonHighRiskCyberActivity, ThreadID: "thread-1", TurnID: "turn-1",
		},
	})
}

func outputSchemaPointer(t *testing.T, raw string) *protocolv2.OutputSchema {
	t.Helper()
	schema, err := protocolv2.OutputSchemaFromJSON([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return &schema
}
