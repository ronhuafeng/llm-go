package codexcaller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk"
	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
	"github.com/ronhuafeng/llm-go/llmkit/llmadapter"
	"github.com/ronhuafeng/llm-go/llmkit/llmschema"
	"github.com/santhosh-tekuri/jsonschema/v6"
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
	if err := applyAdmitTurn(request, runner.result.Start); err != nil {
		return codexsdk.StartedThreadRun{Start: runner.result.Start}, err
	}
	return runner.result, runner.err
}

func (runner *fakeRunner) StartStream(ctx context.Context, request codexsdk.StartThreadRunRequest) (*codexsdk.Stream[codexsdk.StartedThreadRun], error) {
	runner.streamRequests = append(runner.streamRequests, request)
	if err := applyAdmitTurn(request, runner.result.Start); err != nil {
		return nil, err
	}
	return nil, runner.streamErr
}

func applyAdmitTurn(request codexsdk.StartThreadRunRequest, start protocolv2.ThreadStartResponse) error {
	if start.Thread.ID == "" {
		return nil
	}
	field := reflect.ValueOf(&request).Elem().FieldByName("AdmitTurn")
	if !field.IsValid() || field.IsNil() {
		return nil
	}
	results := field.Call([]reflect.Value{reflect.ValueOf(start)})
	if results[0].IsNil() {
		return nil
	}
	return results[0].Interface().(error)
}

func startRequestHasAdmitTurn() bool {
	_, ok := reflect.TypeOf(codexsdk.StartThreadRunRequest{}).FieldByName("AdmitTurn")
	return ok
}

func unisolatableTurn() protocolv2.Turn {
	return protocolv2.Turn{
		ID: "invalid-turn", StartedAt: protocolv2.Value(int64(1)), Status: protocolv2.TurnStatusInProgress,
	}
}

func requireObservedModel(t *testing.T, evidence llmadapter.ExecutionEvidence, want string) {
	t.Helper()
	if !executionHasModelObservation() {
		return
	}
	got, ok := observationString(evidence, "Model")
	if !ok || got != want {
		t.Fatalf("Model = (%q, %t), want observed %q", got, ok, want)
	}
}

func requireUnknownModel(t *testing.T, evidence llmadapter.ExecutionEvidence) {
	t.Helper()
	if !executionHasModelObservation() {
		return
	}
	if got, ok := observationString(evidence, "Model"); ok {
		t.Fatalf("Model = (%q, true), want unknown", got)
	}
}

func requireObservedInput(t *testing.T, usage *llmadapter.TokenUsage, want int64) {
	t.Helper()
	if usage == nil || !tokenUsageHasInputObservation() {
		return
	}
	got, ok := observationInt64(*usage, "Input")
	if !ok || got != want {
		t.Fatalf("Input = (%d, %t), want observed %d", got, ok, want)
	}
}

func executionHasModelObservation() bool {
	_, ok := reflect.TypeOf(llmadapter.ExecutionEvidence{}).FieldByName("Model")
	return ok
}

func tokenUsageHasInputObservation() bool {
	_, ok := reflect.TypeOf(llmadapter.TokenUsage{}).FieldByName("Input")
	return ok
}

func observationString(value any, field string) (string, bool) {
	got, ok := observationValue(value, field)
	if !ok {
		return "", false
	}
	text, _ := got.(string)
	return text, true
}

func observationInt64(value any, field string) (int64, bool) {
	got, ok := observationValue(value, field)
	if !ok {
		return 0, false
	}
	n, _ := got.(int64)
	return n, true
}

func observationValue(value any, field string) (any, bool) {
	target := reflect.ValueOf(value)
	if target.Kind() == reflect.Pointer {
		if target.IsNil() {
			return nil, false
		}
		target = target.Elem()
	}
	observed := target.FieldByName(field)
	if !observed.IsValid() {
		return nil, false
	}
	method := observed.MethodByName("Value")
	if !method.IsValid() {
		return nil, false
	}
	results := method.Call(nil)
	if len(results) != 2 || !results[1].Bool() {
		return nil, false
	}
	return results[0].Interface(), true
}

func setAdmitTurn(request *codexsdk.StartThreadRunRequest, admit func(protocolv2.ThreadStartResponse) error) bool {
	field := reflect.ValueOf(request).Elem().FieldByName("AdmitTurn")
	if !field.IsValid() || !field.CanSet() {
		return false
	}
	field.Set(reflect.ValueOf(admit))
	return true
}

func admitTurnAttached(request codexsdk.StartThreadRunRequest) bool {
	field := reflect.ValueOf(&request).Elem().FieldByName("AdmitTurn")
	return field.IsValid() && !field.IsNil()
}

var _ ThreadRunner = (*fakeRunner)(nil)
var _ llmadapter.Caller = (*Caller)(nil)

type nullAwareString struct {
	SawNull bool
}

type typedProviderError struct {
	code string
}

func (err *typedProviderError) Error() string { return "provider terminal cause: " + err.code }

func newReadOnlyEphemeralCaller(t *testing.T, runner ThreadRunner) *Caller {
	t.Helper()
	caller, err := New(ReadOnlyEphemeralOptions(runner))
	if err != nil {
		t.Fatal(err)
	}
	return caller
}

func requireMissingThreadProfileError(t *testing.T, err error, want string) {
	t.Helper()
	if !errors.Is(err, codexsdk.ErrMissingThreadID) || !errors.Is(err, ErrEffectiveProfile) || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want missing-thread and profile causes containing %q", err, want)
	}
}

func requireTurnAdmissionProfileError(t *testing.T, err error, want string) {
	t.Helper()
	if !errors.Is(err, ErrEffectiveProfile) || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want ErrEffectiveProfile containing %q", err, want)
	}
}

func (value *nullAwareString) UnmarshalJSON(data []byte) error {
	value.SawNull = string(data) == "null"
	return nil
}

func TestNewRejectsCallerOwnedAdmitTurn(t *testing.T) {
	if !startRequestHasAdmitTurn() {
		t.Skip("published SDK tuple does not expose AdmitTurn")
	}
	options := ReadOnlyEphemeralOptions(&fakeRunner{})
	if !setAdmitTurn(&options.Defaults, func(protocolv2.ThreadStartResponse) error { return nil }) {
		t.Fatal("could not set AdmitTurn on defaults")
	}
	if _, err := New(options); err == nil {
		t.Fatal("New accepted caller-owned AdmitTurn")
	}
}

func TestNeutralCallerRequiresNamedSafetyProfile(t *testing.T) {
	runner := &fakeRunner{result: validStartedRun("ok", "gpt")}
	if _, err := New(Options{Runner: runner}); !errors.Is(err, ErrMissingSafetyProfile) {
		t.Fatalf("New error = %v, want ErrMissingSafetyProfile", err)
	}
	if len(runner.requests) != 0 || len(runner.streamRequests) != 0 {
		t.Fatalf("unrestricted construction invoked the runner: starts=%d streams=%d", len(runner.requests), len(runner.streamRequests))
	}
}

func TestNewValidatesRunnerAndOwnedDefaults(t *testing.T) {
	if _, err := New(Options{}); !errors.Is(err, ErrNilThreadRunner) {
		t.Fatalf("New error = %v, want ErrNilThreadRunner", err)
	}
	var typedNil *fakeRunner
	if _, err := New(Options{Runner: typedNil}); !errors.Is(err, ErrNilThreadRunner) {
		t.Fatalf("typed nil error = %v", err)
	}
	runner := &fakeRunner{}
	tests := []codexsdk.StartThreadRunRequest{
		{Turn: protocolv2.TurnStartParams{ThreadID: "owned", Input: nil}},
		{Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{}}},
		{Turn: protocolv2.TurnStartParams{OutputSchema: outputSchemaPointer(t, `true`)}},
	}
	for _, defaults := range tests {
		options := ReadOnlyEphemeralOptions(runner)
		options.Defaults.Turn.ThreadID = defaults.Turn.ThreadID
		options.Defaults.Turn.Input = defaults.Turn.Input
		options.Defaults.Turn.OutputSchema = defaults.Turn.OutputSchema
		if _, err := New(options); err == nil {
			t.Fatalf("New accepted conflicting defaults: %#v", defaults.Turn)
		}
	}
}

func TestCallerBuildsExactRequestAndProjectsEvidence(t *testing.T) {
	run := validStartedRun("final", "gpt-start")
	run.Run.Usage = &protocolv2.ThreadTokenUsage{Total: protocolv2.TokenUsageBreakdown{
		InputTokens: 11, CachedInputTokens: 3, OutputTokens: 5, ReasoningOutputTokens: 2,
	}}
	run.Run.Notifications = []protocolv2.ServerNotification{modelRerouted("gpt-start", "gpt-rerouted")}
	runner := &fakeRunner{result: run}
	defaults := codexsdk.StartThreadRunRequest{
		Thread: protocolv2.ThreadStartParams{
			Model:                 protocolv2.Value("gpt-request"),
			RuntimeWorkspaceRoots: protocolv2.Value([]string{"/workspace"}),
		},
		Turn: protocolv2.TurnStartParams{Effort: protocolv2.Value(protocolv2.ReasoningEffort("high"))},
	}
	options := ReadOnlyEphemeralOptions(runner)
	options.Defaults.Thread.Model = defaults.Thread.Model
	options.Defaults.Thread.RuntimeWorkspaceRoots = defaults.Thread.RuntimeWorkspaceRoots
	options.Defaults.Turn.Effort = defaults.Turn.Effort
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
	if response.FinalResponse != "final" || response.Execution.ProviderName != "codex" || response.Execution.EffectiveModel != "gpt-rerouted" {
		t.Fatalf("response = %#v", response)
	}
	if response.Execution.Usage == nil || response.Execution.Usage.InputTokens != 11 || response.Execution.Usage.ReasoningOutputTokens != 2 {
		t.Fatalf("neutral usage = %#v", response.Execution.Usage)
	}
	requireObservedModel(t, response.Execution, "gpt-rerouted")
	requireObservedInput(t, response.Execution.Usage, 11)
	details, ok := response.ProviderDetails.(Details)
	if !ok || details.ProviderName() != "codex" || !reflect.DeepEqual(details.Run, run) {
		t.Fatalf("details = %#v", response.ProviderDetails)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("requests = %d", len(runner.requests))
	}
	request := runner.requests[0]
	if request.Turn.ThreadID != "" || len(request.Turn.Input) != 1 {
		t.Fatalf("adapter-owned turn fields = %#v", request.Turn)
	}
	text, ok := request.Turn.Input[0].AsText()
	if !ok || text.Text != "answer as JSON" || request.Turn.OutputSchema == nil {
		t.Fatalf("turn input/schema = %#v", request.Turn)
	}
	if request.Thread.Model == nil || request.Thread.Model.Value == nil || *request.Thread.Model.Value != "gpt-request" {
		t.Fatalf("exact defaults were not preserved: %#v", request.Thread)
	}
}

func TestCallerPreservesStartOnlyPartialEvidence(t *testing.T) {
	providerErr := errors.New("start failed after negotiation")
	run := codexsdk.StartedThreadRun{Start: protocolv2.ThreadStartResponse{Model: "effective-model"}}
	runner := &fakeRunner{result: run, err: providerErr}
	caller, err := New(ReadOnlyEphemeralOptions(runner))
	if err != nil {
		t.Fatal(err)
	}
	response, err := caller.Call(context.Background(), validRequest())
	if !errors.Is(err, providerErr) || response.Execution.EffectiveModel != "effective-model" {
		t.Fatalf("response=%#v err=%v", response, err)
	}
	requireObservedModel(t, response.Execution, "effective-model")
	details, ok := response.ProviderDetails.(Details)
	if !ok || details.Run.Start.Model != "effective-model" {
		t.Fatalf("details = %#v", response.ProviderDetails)
	}
}

func TestCallerPreservesPartialRunAndCause(t *testing.T) {
	providerErr := errors.New("turn failed")
	run := validStartedRun("", "gpt-start")
	run.Run.Turn.Status = protocolv2.TurnStatusFailed
	run.Run.FinalResponse = "partial"
	runner := &fakeRunner{result: run, err: providerErr}
	caller, err := New(ReadOnlyEphemeralOptions(runner))
	if err != nil {
		t.Fatal(err)
	}
	response, err := caller.Call(context.Background(), validRequest())
	if !errors.Is(err, providerErr) {
		t.Fatalf("error = %v, want provider cause", err)
	}
	if response.FinalResponse != "partial" || response.Execution.ProviderName != "codex" {
		t.Fatalf("partial response = %#v", response)
	}
	details, ok := response.ProviderDetails.(Details)
	if !ok || details.Run.Start.Thread.ID == "" || details.Run.Run.Turn.Status != protocolv2.TurnStatusFailed {
		t.Fatalf("partial details = %#v", details)
	}
}

func TestCallDetailedAndStreamShareRequestConstruction(t *testing.T) {
	streamErr := errors.New("stream unavailable")
	runner := &fakeRunner{result: validStartedRun("ok", "gpt"), streamErr: streamErr}
	caller, err := New(ReadOnlyEphemeralOptions(runner))
	if err != nil {
		t.Fatal(err)
	}
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
	left, _ := json.Marshal(runner.requests[0])
	right, _ := json.Marshal(runner.streamRequests[0])
	if !bytesEqual(left, right) {
		t.Fatalf("request construction differs:\n%s\n%s", left, right)
	}
}

func TestCallIsProjectionOfDetailedResult(t *testing.T) {
	run := validStartedRun("ok", "gpt")
	runner := &fakeRunner{result: run}
	caller, err := New(ReadOnlyEphemeralOptions(runner))
	if err != nil {
		t.Fatal(err)
	}
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

func TestCallerPublishesImmutableDetailsAndDefaults(t *testing.T) {
	run := validStartedRun("ok", "gpt")
	runner := &fakeRunner{result: run}
	roots := []string{"/one"}
	options := ReadOnlyEphemeralOptions(runner)
	options.Defaults.Thread.RuntimeWorkspaceRoots = protocolv2.Value(roots)
	caller, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	roots[0] = "/mutated"
	response, err := caller.Call(context.Background(), validRequest())
	if err != nil {
		t.Fatal(err)
	}
	requestedRoots := runner.requests[0].Thread.RuntimeWorkspaceRoots
	if requestedRoots == nil || requestedRoots.Value == nil || (*requestedRoots.Value)[0] != "/one" {
		t.Fatalf("defaults aliased caller input: %#v", requestedRoots)
	}
	details := response.ProviderDetails.(Details)
	runner.result.Run.Notifications[0] = modelRerouted("mutated", "mutated")
	detailsAgain := response.ProviderDetails.(Details)
	rerouted, ok := detailsAgain.Run.Run.Notifications[0].AsModelRerouted()
	if !ok || rerouted.Params.FromModel != "gpt" || details.Run.Run.Notifications[0].Kind() != protocolv2.ServerNotificationKindModelRerouted {
		t.Fatal("details snapshot was aliased")
	}
}

func TestCallerIsolatesPartialTurnWithoutIdentity(t *testing.T) {
	run := validStartedRun("partial", "gpt")
	run.Run.Turn.ID = ""
	runner := &fakeRunner{result: run}
	caller, err := New(ReadOnlyEphemeralOptions(runner))
	if err != nil {
		t.Fatal(err)
	}
	response, err := caller.Call(context.Background(), validRequest())
	if err != nil {
		t.Fatal(err)
	}
	details := response.ProviderDetails.(Details)
	runner.result.Run.Turn.Items[0] = protocolv2.NewThreadItemAgentMessage(protocolv2.ThreadItemAgentMessage{
		ID: "mutated", Text: "mutated",
	})
	message, ok := details.Run.Run.Turn.Items[0].AsAgentMessage()
	if !ok || message.ID != "item-1" || message.Text != "partial" {
		t.Fatalf("partial turn details aliased runner result: %#v", details.Run.Run.Turn.Items)
	}
}

func TestCallOmitsProviderDetailsWhenSnapshotFails(t *testing.T) {
	run := validStartedRun("safe-final", "safe-model")
	run.Run.Turn = unisolatableTurn()
	run.Run.Notifications = []protocolv2.ServerNotification{modelRerouted("safe-model", "isolated-reroute")}
	run.Run.Usage = &protocolv2.ThreadTokenUsage{Total: protocolv2.TokenUsageBreakdown{
		InputTokens: 3, CachedInputTokens: 0, OutputTokens: 0, ReasoningOutputTokens: 0,
	}}
	runner := &fakeRunner{result: run}
	caller, err := New(ReadOnlyEphemeralOptions(runner))
	if err != nil {
		t.Fatal(err)
	}

	response, err := caller.Call(context.Background(), validRequest())
	if err == nil || !strings.Contains(err.Error(), "Turn.items") {
		t.Fatalf("Call error = %v, want snapshot failure", err)
	}
	if response.ProviderDetails != nil {
		t.Fatalf("ProviderDetails = %#v, want no unisolated run", response.ProviderDetails)
	}
	if response.FinalResponse != "safe-final" || response.Execution.ProviderName != "codex" || response.Execution.EffectiveModel != "isolated-reroute" {
		t.Fatalf("independent neutral evidence = %#v", response)
	}
	if response.Execution.Usage == nil || response.Execution.Usage.InputTokens != 3 {
		t.Fatalf("usage = %#v, want independently isolated usage", response.Execution.Usage)
	}
	requireObservedModel(t, response.Execution, "isolated-reroute")
	requireObservedInput(t, response.Execution.Usage, 3)
	runner.result.Run.Usage.Total.InputTokens = 99
	runner.result.Start.Model = "mutated-start"
	if response.Execution.Usage.InputTokens != 3 || response.Execution.EffectiveModel != "isolated-reroute" {
		t.Fatalf("published neutral evidence aliased runner state: %#v", response.Execution)
	}
}

func TestCallPreservesRerouteWhenUnrelatedNotificationIsMalformed(t *testing.T) {
	run := validStartedRun("ok", "safe-model")
	run.Run.Notifications = []protocolv2.ServerNotification{
		{},
		modelRerouted("safe-model", "from-good-reroute"),
	}
	run.Run.Usage = &protocolv2.ThreadTokenUsage{Total: protocolv2.TokenUsageBreakdown{
		InputTokens: 0, CachedInputTokens: 0, OutputTokens: 0, ReasoningOutputTokens: 0,
	}}
	caller := newReadOnlyEphemeralCaller(t, &fakeRunner{result: run})
	response, err := caller.Call(context.Background(), validRequest())
	if err == nil || !strings.Contains(err.Error(), "ServerNotification") {
		t.Fatalf("Call error = %v, want notification isolation failure", err)
	}
	if response.ProviderDetails != nil {
		t.Fatalf("ProviderDetails = %#v, want omitted exact details", response.ProviderDetails)
	}
	if response.Execution.EffectiveModel != "from-good-reroute" {
		t.Fatalf("effective model = %q, want independently isolated reroute", response.Execution.EffectiveModel)
	}
	if response.Execution.Usage == nil || response.Execution.Usage.InputTokens != 0 {
		t.Fatalf("usage = %#v, want observed zero", response.Execution.Usage)
	}
	requireObservedModel(t, response.Execution, "from-good-reroute")
	requireObservedInput(t, response.Execution.Usage, 0)
}

func TestCallDoesNotFillUnknownModelFromRequestedDefault(t *testing.T) {
	run := validStartedRun("ok", "unused")
	run.Start = protocolv2.ThreadStartResponse{}
	run.Run.Notifications = nil
	runner := &fakeRunner{result: run}
	options := ReadOnlyEphemeralOptions(runner)
	options.Defaults.Thread.Model = protocolv2.Value("gpt-requested")
	caller, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	response, err := caller.Call(context.Background(), validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if response.Execution.EffectiveModel != "" {
		t.Fatalf("EffectiveModel = %q, want unknown; requested model must not fill observation", response.Execution.EffectiveModel)
	}
	requireUnknownModel(t, response.Execution)
}

func TestCallLeavesUsageUnknownWhenProviderOmitsIt(t *testing.T) {
	caller := newReadOnlyEphemeralCaller(t, &fakeRunner{result: validStartedRun("ok", "gpt")})
	response, err := caller.Call(context.Background(), validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if response.Execution.Usage != nil {
		t.Fatalf("Usage = %#v, want unreported", response.Execution.Usage)
	}
}

func TestAdmitTurnRejectsUnknownEffectiveFactsBeforeTurn(t *testing.T) {
	if !startRequestHasAdmitTurn() {
		t.Skip("published SDK tuple does not expose AdmitTurn")
	}
	cases := []struct {
		name   string
		mutate func(*codexsdk.StartedThreadRun)
		want   string
	}{
		{name: "approval", mutate: func(run *codexsdk.StartedThreadRun) {
			run.Start.ApprovalPolicy = protocolv2.AskForApproval{}
		}, want: "unknown"},
		{name: "sandbox", mutate: func(run *codexsdk.StartedThreadRun) {
			run.Start.Sandbox = protocolv2.SandboxPolicy{}
		}, want: "unknown"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			run := validStartedRun("must-not-execute", "gpt")
			testCase.mutate(&run)
			runner := &fakeRunner{result: run}
			caller := newReadOnlyEphemeralCaller(t, runner)
			got, err := caller.CallDetailed(context.Background(), validRequest())
			requireTurnAdmissionProfileError(t, err, testCase.want)
			if got.Start.Thread.ID != run.Start.Thread.ID || got.Run.Turn.ID != "" {
				t.Fatalf("CallDetailed unknown fact rejection = %#v, want start-only evidence", got)
			}
			response, callErr := caller.Call(context.Background(), validRequest())
			requireTurnAdmissionProfileError(t, callErr, testCase.want)
			if response.FinalResponse != "" {
				t.Fatalf("Call published turn output after unknown fact: %#v", response)
			}
			_, streamErr := caller.CallStream(context.Background(), validRequest())
			requireTurnAdmissionProfileError(t, streamErr, testCase.want)
		})
	}
}

func TestReadOnlyEphemeralProfileSetsAndVerifiesExactPolicy(t *testing.T) {
	run := validStartedRun("ok", "gpt")
	runner := &fakeRunner{result: run}
	options := ReadOnlyEphemeralOptions(runner)
	caller, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := caller.CallDetailed(context.Background(), validRequest()); err != nil {
		t.Fatal(err)
	}
	request := runner.requests[0]
	assertReadOnlyEphemeralRequest(t, request)
	if request.Thread.Ephemeral == nil || request.Thread.Ephemeral.Value == nil || !*request.Thread.Ephemeral.Value {
		t.Fatalf("thread ephemeral = %#v", request.Thread.Ephemeral)
	}
	if request.Thread.Sandbox == nil || request.Thread.Sandbox.Value == nil || *request.Thread.Sandbox.Value != protocolv2.SandboxModeReadOnly {
		t.Fatalf("thread sandbox = %#v", request.Thread.Sandbox)
	}
	if request.Thread.ApprovalPolicy == nil || request.Thread.ApprovalPolicy.Value == nil || request.Thread.ApprovalPolicy.Value.Kind() != protocolv2.AskForApprovalKindNever {
		t.Fatalf("thread approval = %#v", request.Thread.ApprovalPolicy)
	}
	if request.Turn.SandboxPolicy == nil || request.Turn.SandboxPolicy.Value == nil || request.Turn.SandboxPolicy.Value.Kind() != protocolv2.SandboxPolicyKindReadOnly {
		t.Fatalf("turn sandbox = %#v", request.Turn.SandboxPolicy)
	}
	if request.Turn.ApprovalPolicy == nil || request.Turn.ApprovalPolicy.Value == nil || request.Turn.ApprovalPolicy.Value.Kind() != protocolv2.AskForApprovalKindNever {
		t.Fatalf("turn approval = %#v", request.Turn.ApprovalPolicy)
	}

}

func TestEffectiveProfileContractIsSharedByCallAndDetailed(t *testing.T) {
	type profileCase struct {
		name   string
		mutate func(*codexsdk.StartedThreadRun)
		want   string
	}
	cases := []profileCase{
		{name: "valid"},
		{name: "approval", mutate: func(run *codexsdk.StartedThreadRun) {
			run.Start.ApprovalPolicy = protocolv2.NewAskForApprovalOnRequest()
		}, want: "not never"},
		{name: "sandbox", mutate: func(run *codexsdk.StartedThreadRun) {
			run.Start.Sandbox = protocolv2.NewSandboxPolicyDangerFullAccess()
		}, want: "not read-only"},
		{name: "ephemeral", mutate: func(run *codexsdk.StartedThreadRun) {
			run.Start.Thread.Ephemeral = false
		}, want: "not ephemeral"},
	}
	paths := []struct {
		name string
		call func(*Caller, *fakeRunner) (codexsdk.StartedThreadRun, error)
	}{
		{name: "Call", call: func(caller *Caller, _ *fakeRunner) (codexsdk.StartedThreadRun, error) {
			response, err := caller.Call(context.Background(), validRequest())
			if details, ok := response.ProviderDetails.(Details); ok {
				return details.Run, err
			}
			return codexsdk.StartedThreadRun{}, err
		}},
		{name: "CallDetailed", call: func(caller *Caller, _ *fakeRunner) (codexsdk.StartedThreadRun, error) {
			return caller.CallDetailed(context.Background(), validRequest())
		}},
		{name: "CallStream", call: func(caller *Caller, runner *fakeRunner) (codexsdk.StartedThreadRun, error) {
			stream, err := caller.CallStream(context.Background(), validRequest())
			if err != nil {
				return codexsdk.StartedThreadRun{Start: runner.result.Start}, err
			}
			return stream.Wait(context.Background())
		}},
	}

	for _, testCase := range cases {
		for _, path := range paths {
			t.Run(testCase.name+"/"+path.name, func(t *testing.T) {
				if testCase.want != "" && !startRequestHasAdmitTurn() {
					t.Skip("published SDK tuple does not expose AdmitTurn")
				}
				run := validStartedRun("ok", "gpt")
				if testCase.mutate != nil {
					testCase.mutate(&run)
				}
				runner := &fakeRunner{result: run}
				caller, err := New(ReadOnlyEphemeralOptions(runner))
				if err != nil {
					t.Fatal(err)
				}
				got, err := path.call(caller, runner)
				if testCase.want == "" {
					if path.name == "CallStream" {
						return
					}
					if err != nil {
						t.Fatalf("call error = %v", err)
					}
					if !reflect.DeepEqual(got, run) {
						t.Fatalf("exact result = %#v, want %#v", got, run)
					}
				} else {
					requireTurnAdmissionProfileError(t, err, testCase.want)
					if got.Start.Thread.ID != run.Start.Thread.ID || got.Run.Turn.ID != "" || got.Run.Turn.Status == protocolv2.TurnStatusCompleted {
						t.Fatalf("admission rejection continued into turn execution: %#v", got)
					}
				}
			})
		}
	}
}

func TestCallDetailedValidatesDecodedMissingThreadIDApproval(t *testing.T) {
	partial := validStartedRun("", "decoded-model")
	partial.Start.Thread.ID = ""
	partial.Run = codexsdk.ThreadRunResult{}
	partial.Start.ApprovalPolicy = protocolv2.NewAskForApprovalOnRequest()
	runner := &fakeRunner{result: partial, err: codexsdk.ErrMissingThreadID}
	caller := newReadOnlyEphemeralCaller(t, runner)

	got, err := caller.CallDetailed(context.Background(), validRequest())
	requireMissingThreadProfileError(t, err, "not never")
	if !reflect.DeepEqual(got, partial) {
		t.Fatalf("CallDetailed result = %#v, want exact partial evidence %#v", got, partial)
	}
}

func TestCallValidatesDecodedMissingThreadIDSandboxAndProjectsEvidence(t *testing.T) {
	partial := validStartedRun("", "decoded-model")
	partial.Start.Thread.ID = ""
	partial.Run = codexsdk.ThreadRunResult{}
	partial.Start.Sandbox = protocolv2.NewSandboxPolicyDangerFullAccess()
	roots := []string{"/decoded-root"}
	partial.Start.RuntimeWorkspaceRoots = &roots
	runner := &fakeRunner{result: partial, err: codexsdk.ErrMissingThreadID}
	caller := newReadOnlyEphemeralCaller(t, runner)

	response, err := caller.Call(context.Background(), validRequest())
	requireMissingThreadProfileError(t, err, "not read-only")
	if response.Execution.ProviderName != "codex" || response.Execution.EffectiveModel != "decoded-model" {
		t.Fatalf("Call evidence = %#v, want decoded start projection", response.Execution)
	}
	details, ok := response.ProviderDetails.(Details)
	if !ok || !reflect.DeepEqual(details.Run, partial) {
		t.Fatalf("Call details = %#v, want exact partial evidence %#v", response.ProviderDetails, partial)
	}
	(*runner.result.Start.RuntimeWorkspaceRoots)[0] = "/mutated"
	gotRoots := details.Run.Start.RuntimeWorkspaceRoots
	if gotRoots == nil || (*gotRoots)[0] != "/decoded-root" {
		t.Fatalf("Call details alias runner result: %#v", gotRoots)
	}
}

func TestStreamValidatesDecodedMissingThreadIDEphemeralOnWaitAndErr(t *testing.T) {
	partial := validStartedRun("", "decoded-model")
	partial.Start.Thread.ID = ""
	partial.Start.Thread.Ephemeral = false
	partial.Run = codexsdk.ThreadRunResult{}
	missingIDErr := fmt.Errorf("malformed decoded start: %w", codexsdk.ErrMissingThreadID)
	runner := &fakeRunner{}
	caller := newReadOnlyEphemeralCaller(t, runner)
	stream := caller.wrapStream(&fakeStartedRunStream{result: partial, err: missingIDErr}, nil)

	got, waitErr := stream.Wait(context.Background())
	requireMissingThreadProfileError(t, waitErr, "not ephemeral")
	if !reflect.DeepEqual(got, partial) {
		t.Fatalf("Wait result = %#v, want exact partial evidence %#v", got, partial)
	}
	streamErr := stream.Err()
	requireMissingThreadProfileError(t, streamErr, "not ephemeral")
	if streamErr.Error() != waitErr.Error() {
		t.Fatalf("Err = %q, Wait error = %q, want stable terminal causes", streamErr, waitErr)
	}
}

func TestCallProjectsZeroValuedDecodedMissingThreadIDEvidence(t *testing.T) {
	runner := &fakeRunner{err: codexsdk.ErrMissingThreadID}
	caller := newReadOnlyEphemeralCaller(t, runner)

	response, err := caller.Call(context.Background(), validRequest())
	requireMissingThreadProfileError(t, err, "unknown")
	if response.Execution.ProviderName != "codex" {
		t.Fatalf("Call evidence = %#v, want decoded start provider projection", response.Execution)
	}
	details, ok := response.ProviderDetails.(Details)
	if !ok || !reflect.DeepEqual(details.Run, codexsdk.StartedThreadRun{}) {
		t.Fatalf("Call details = %#v, want typed zero-valued decoded evidence", response.ProviderDetails)
	}
}

func TestCallDoesNotSynthesizeProfileMismatchBeforeStartResponse(t *testing.T) {
	transportErr := errors.New("thread/start transport failed")
	runner := &fakeRunner{err: transportErr}
	caller := newReadOnlyEphemeralCaller(t, runner)

	response, err := caller.Call(context.Background(), validRequest())
	if !errors.Is(err, transportErr) || errors.Is(err, ErrEffectiveProfile) {
		t.Fatalf("Call error = %v, want transport cause without profile mismatch", err)
	}
	if !reflect.DeepEqual(response, llmadapter.Response{}) {
		t.Fatalf("Call response = %#v, want no synthetic evidence", response)
	}
}

func TestStreamJoinsProviderAndProfileErrorsWithoutLosingExactEvidence(t *testing.T) {
	providerErr := &typedProviderError{code: "quota"}
	run := validStartedRun("partial", "gpt")
	run.Start.ApprovalPolicy = protocolv2.NewAskForApprovalOnRequest()
	run.Start.Sandbox = protocolv2.NewSandboxPolicyDangerFullAccess()
	run.Start.Thread.Ephemeral = false
	run.Run = codexsdk.ThreadRunResult{}
	run.Run.Diagnostics = []codexsdk.DiagnosticRef{{Kind: "provider", Path: "thread/start"}}

	runner := &fakeRunner{result: run, err: providerErr}
	caller, err := New(ReadOnlyEphemeralOptions(runner))
	if err != nil {
		t.Fatal(err)
	}
	inner := &fakeStartedRunStream{result: run, err: providerErr, notifications: run.Run.Notifications}
	stream := caller.wrapStream(inner, nil)
	got, err := stream.Wait(context.Background())
	if !errors.Is(err, providerErr) || !errors.Is(err, ErrEffectiveProfile) {
		t.Fatalf("Wait error = %v, want provider and profile causes", err)
	}
	var typedWaitErr *typedProviderError
	if !errors.As(err, &typedWaitErr) || typedWaitErr.code != "quota" {
		t.Fatalf("Wait error = %v, want typed provider cause", err)
	}
	if !reflect.DeepEqual(got, run) || got.Run.Turn.ID != "" {
		t.Fatalf("Wait result = %#v, want start-only evidence %#v", got, run)
	}
	streamErr := stream.Err()
	if !errors.Is(streamErr, providerErr) || !errors.Is(streamErr, ErrEffectiveProfile) {
		t.Fatalf("Err = %v, want provider and profile causes", streamErr)
	}
	var typedStreamErr *typedProviderError
	if !errors.As(streamErr, &typedStreamErr) || typedStreamErr.code != "quota" {
		t.Fatalf("Err = %v, want typed provider cause", streamErr)
	}
	if err := stream.Close(); err != nil || !inner.closed {
		t.Fatalf("Close = %v, closed=%v", err, inner.closed)
	}

	activeRun := run
	activeRun.Run.Turn.Status = protocolv2.TurnStatusInProgress
	active := caller.wrapStream(&fakeStartedRunStream{result: activeRun}, nil)
	if err := active.Err(); err != nil {
		t.Fatalf("active stream Err = %v, want nil until terminal", err)
	}
}

func TestReadOnlyEphemeralProfileRejectsConflictingDefaults(t *testing.T) {
	runner := &fakeRunner{}
	tests := []struct {
		name   string
		mutate func(*Options)
	}{
		{
			name: "thread sandbox",
			mutate: func(options *Options) {
				options.Defaults.Thread.Sandbox = protocolv2.Value(protocolv2.SandboxModeDangerFullAccess)
			},
		},
		{
			name: "thread approval",
			mutate: func(options *Options) {
				options.Defaults.Thread.ApprovalPolicy = protocolv2.Value(protocolv2.NewAskForApprovalOnRequest())
			},
		},
		{
			name: "thread ephemeral",
			mutate: func(options *Options) {
				options.Defaults.Thread.Ephemeral = protocolv2.Value(false)
			},
		},
		{
			name: "turn sandbox",
			mutate: func(options *Options) {
				options.Defaults.Turn.SandboxPolicy = protocolv2.Value(protocolv2.NewSandboxPolicyWorkspaceWrite(protocolv2.SandboxPolicyWorkspaceWrite{}))
			},
		},
		{
			name: "turn approval",
			mutate: func(options *Options) {
				options.Defaults.Turn.ApprovalPolicy = protocolv2.Value(protocolv2.NewAskForApprovalOnRequest())
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := ReadOnlyEphemeralOptions(runner)
			test.mutate(&options)
			if _, err := New(options); err == nil {
				t.Fatal("New accepted conflicting profile default")
			}
			if len(runner.requests) != 0 || len(runner.streamRequests) != 0 {
				t.Fatalf("runner invoked: starts=%d streams=%d", len(runner.requests), len(runner.streamRequests))
			}
		})
	}
}

func TestReadOnlyEphemeralProfileNormalizesUnsetDefaultsAndPreservesExactDefaults(t *testing.T) {
	runner := &fakeRunner{result: validStartedRun("ok", "gpt")}
	options := ReadOnlyEphemeralOptions(runner)
	options.Defaults.Thread.Ephemeral = nil
	options.Defaults.Thread.Sandbox = nil
	options.Defaults.Thread.ApprovalPolicy = nil
	options.Defaults.Turn.SandboxPolicy = nil
	options.Defaults.Turn.ApprovalPolicy = nil
	options.Defaults.Thread.Model = protocolv2.Value("gpt-request")
	options.Defaults.Thread.CWD = protocolv2.Value("/workspace/project")
	options.Defaults.Thread.ServiceTier = protocolv2.Value("flex")
	options.Defaults.Thread.RuntimeWorkspaceRoots = protocolv2.Value([]string{"/workspace"})
	options.Defaults.Turn.Effort = protocolv2.Value(protocolv2.ReasoningEffort("high"))

	caller, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := caller.CallDetailed(context.Background(), validRequest()); err != nil {
		t.Fatal(err)
	}
	request := runner.requests[0]
	assertReadOnlyEphemeralRequest(t, request)
	if request.Thread.Model == nil || request.Thread.Model.Value == nil || *request.Thread.Model.Value != "gpt-request" ||
		request.Thread.CWD == nil || request.Thread.CWD.Value == nil || *request.Thread.CWD.Value != "/workspace/project" ||
		request.Thread.ServiceTier == nil || request.Thread.ServiceTier.Value == nil || *request.Thread.ServiceTier.Value != "flex" ||
		request.Thread.RuntimeWorkspaceRoots == nil || request.Thread.RuntimeWorkspaceRoots.Value == nil || !reflect.DeepEqual(*request.Thread.RuntimeWorkspaceRoots.Value, []string{"/workspace"}) ||
		request.Turn.Effort == nil || request.Turn.Effort.Value == nil || *request.Turn.Effort.Value != protocolv2.ReasoningEffort("high") {
		t.Fatalf("non-profile exact defaults changed: %#v %#v", request.Thread, request.Turn)
	}
}

func TestReadOnlyEphemeralProfileReappliesSafeRequestOnEveryCallPath(t *testing.T) {
	tests := []struct {
		name string
		call func(*Caller) error
	}{
		{name: "Call", call: func(caller *Caller) error {
			_, err := caller.Call(context.Background(), validRequest())
			return err
		}},
		{name: "CallDetailed", call: func(caller *Caller) error {
			_, err := caller.CallDetailed(context.Background(), validRequest())
			return err
		}},
		{name: "CallStream", call: func(caller *Caller) error {
			_, err := caller.CallStream(context.Background(), validRequest())
			return err
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeRunner{result: validStartedRun("unsafe", "gpt")}
			caller, err := New(ReadOnlyEphemeralOptions(runner))
			if err != nil {
				t.Fatal(err)
			}
			caller.defaults.Thread.Ephemeral = protocolv2.Value(false)
			caller.defaults.Thread.Sandbox = protocolv2.Value(protocolv2.SandboxModeDangerFullAccess)
			caller.defaults.Thread.ApprovalPolicy = protocolv2.Value(protocolv2.NewAskForApprovalOnRequest())
			caller.defaults.Turn.SandboxPolicy = protocolv2.Value(protocolv2.NewSandboxPolicyWorkspaceWrite(protocolv2.SandboxPolicyWorkspaceWrite{}))
			caller.defaults.Turn.ApprovalPolicy = protocolv2.Value(protocolv2.NewAskForApprovalOnRequest())
			if err := test.call(caller); err != nil {
				t.Fatalf("call failed after request profile reapplication: %v", err)
			}
			if len(runner.requests)+len(runner.streamRequests) != 1 {
				t.Fatalf("runner invocations: starts=%d streams=%d", len(runner.requests), len(runner.streamRequests))
			}
			if len(runner.requests) == 1 {
				assertReadOnlyEphemeralRequest(t, runner.requests[0])
			} else {
				assertReadOnlyEphemeralRequest(t, runner.streamRequests[0])
			}
		})
	}
}

func assertReadOnlyEphemeralRequest(t *testing.T, request codexsdk.StartThreadRunRequest) {
	t.Helper()
	if request.Thread.Ephemeral == nil || request.Thread.Ephemeral.Value == nil || !*request.Thread.Ephemeral.Value {
		t.Fatalf("thread ephemeral = %#v", request.Thread.Ephemeral)
	}
	if request.Thread.Sandbox == nil || request.Thread.Sandbox.Value == nil || *request.Thread.Sandbox.Value != protocolv2.SandboxModeReadOnly {
		t.Fatalf("thread sandbox = %#v", request.Thread.Sandbox)
	}
	if request.Thread.ApprovalPolicy == nil || request.Thread.ApprovalPolicy.Value == nil || request.Thread.ApprovalPolicy.Value.Kind() != protocolv2.AskForApprovalKindNever {
		t.Fatalf("thread approval = %#v", request.Thread.ApprovalPolicy)
	}
	if request.Turn.SandboxPolicy == nil || request.Turn.SandboxPolicy.Value == nil || request.Turn.SandboxPolicy.Value.Kind() != protocolv2.SandboxPolicyKindReadOnly {
		t.Fatalf("turn sandbox = %#v", request.Turn.SandboxPolicy)
	}
	if request.Turn.ApprovalPolicy == nil || request.Turn.ApprovalPolicy.Value == nil || request.Turn.ApprovalPolicy.Value.Kind() != protocolv2.AskForApprovalKindNever {
		t.Fatalf("turn approval = %#v", request.Turn.ApprovalPolicy)
	}
	if startRequestHasAdmitTurn() && !admitTurnAttached(request) {
		t.Fatal("AdmitTurn was not attached")
	}
}

func TestCallerWorksThroughLLMAdapterDetailedPath(t *testing.T) {
	runner := &fakeRunner{result: validStartedRun(`{"answer":true}`, "gpt")}
	caller, err := New(ReadOnlyEphemeralOptions(runner))
	if err != nil {
		t.Fatal(err)
	}
	result, err := llmadapter.ValueDetailed[map[string]bool](context.Background(), caller, "answer")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Value["answer"] || result.Response.Execution.ProviderName != "codex" {
		t.Fatalf("result = %#v", result)
	}
}

func TestStrictOutputSchemaCompatibilityMatrix(t *testing.T) {
	t.Run("required-scalar-preserved", func(t *testing.T) {
		type output struct {
			Name string `json:"name"`
		}
		assertSchemaJSONValueEqual(t, schemaFor[output](t))
	})
	t.Run("optional-pointer-preserved", func(t *testing.T) {
		type output struct {
			Name string  `json:"name"`
			Note *string `json:"note,omitempty"`
		}
		assertGoSchemaAccepted[output](t)
		assertDecodedValuesEqual[output](t, `{"name":"x"}`, `{"name":"x","note":null}`)
	})
	t.Run("optional-scalar-fails-closed", func(t *testing.T) {
		type output struct {
			Name  string `json:"name"`
			Score int    `json:"score,omitempty"`
		}
		assertSchemaError(t, schemaFor[output](t), "optional_non_nullable", "/properties/score")
	})
	t.Run("nested-optional-pointer-preserved", func(t *testing.T) {
		type child struct {
			Note *string `json:"note,omitempty"`
		}
		type output struct {
			Child child `json:"child"`
		}
		assertGoSchemaAccepted[output](t)
		assertDecodedValuesEqual[output](t, `{"child":{}}`, `{"child":{"note":null}}`)
	})
	t.Run("optional-map-fails-closed", func(t *testing.T) {
		type output struct {
			Labels map[string]int `json:"labels,omitempty"`
		}
		assertSchemaError(t, schemaFor[output](t), "optional_non_nullable", "/properties/labels")
	})
	t.Run("optional-slice-preserved", func(t *testing.T) {
		type output struct {
			Items []string `json:"items,omitempty"`
		}
		assertGoSchemaAccepted[output](t)
		assertDecodedValuesEqual[output](t, `{}`, `{"items":null}`)
	})
	t.Run("optional-pointer-to-slice-preserved", func(t *testing.T) {
		type output struct {
			Items *[]string `json:"items,omitempty"`
		}
		assertGoSchemaAccepted[output](t)
		assertDecodedValuesEqual[output](t, `{}`, `{"items":null}`)
	})
	t.Run("optional-raw-message-has-decoding-limitation", func(t *testing.T) {
		type output struct {
			Payload json.RawMessage `json:"payload,omitempty"`
		}
		assertGoSchemaAccepted[output](t)

		var absent, explicitNull output
		if err := json.Unmarshal([]byte(`{}`), &absent); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(`{"payload":null}`), &explicitNull); err != nil {
			t.Fatal(err)
		}
		if absent.Payload != nil || string(explicitNull.Payload) != "null" {
			t.Fatalf("RawMessage absence/null distinction changed: absent=%q null=%q", absent.Payload, explicitNull.Payload)
		}
	})
	t.Run("custom-unmarshaler-has-decoding-limitation", func(t *testing.T) {
		raw := json.RawMessage(`{"type":"object","properties":{"value":{"type":["object","null"]}}}`)
		if _, err := StrictOutputSchemaFromJSON(raw); err != nil {
			t.Fatal(err)
		}

		type output struct {
			Value nullAwareString `json:"value,omitempty"`
		}
		var absent, explicitNull output
		if err := json.Unmarshal([]byte(`{}`), &absent); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(`{"value":null}`), &explicitNull); err != nil {
			t.Fatal(err)
		}
		if absent.Value.SawNull || !explicitNull.Value.SawNull {
			t.Fatal("custom unmarshaler did not demonstrate the documented absence/null distinction")
		}
	})
	t.Run("local-ref-preserved", func(t *testing.T) {
		raw := json.RawMessage(`{"type":"object","properties":{"note":{"$ref":"#/$defs/note"}},"$defs":{"note":{"anyOf":[{"type":"string"},{"type":"null"}]}}}`)
		schema, err := StrictOutputSchemaFromJSON(raw)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := schema.MarshalJSON()
		if !strings.Contains(string(encoded), `"required":["note"]`) ||
			!strings.Contains(string(encoded), `"$ref":"#/$defs/note"`) ||
			!strings.Contains(string(encoded), `"anyOf":[{"type":"string"},{"type":"null"}]`) {
			t.Fatalf("schema = %s", encoded)
		}
	})
	t.Run("nested-ref-with-sibling-constraint-fails-closed", func(t *testing.T) {
		assertSchemaError(t,
			json.RawMessage(`{"type":"object","properties":{"value":{"$ref":"#/$defs/outer","type":["string","null"]}},"$defs":{"outer":{"$ref":"#/$defs/inner","type":["string","null"]},"inner":{"type":"string"}}}`),
			"optional_non_nullable", "/properties/value")
	})
	t.Run("boolean-schema-has-codex-limitation", func(t *testing.T) {
		for _, raw := range []json.RawMessage{json.RawMessage(`true`), json.RawMessage(`false`)} {
			assertSchemaJSONValueEqual(t, raw)
		}
	})
	t.Run("draft-2020-12-preserved", func(t *testing.T) {
		raw := json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","required":["note"],"properties":{"note":{"type":["string","null"],"minLength":2}}}`)
		schema, err := StrictOutputSchemaFromJSON(raw)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := schema.MarshalJSON()
		if !strings.Contains(string(encoded), `"$schema":"https://json-schema.org/draft/2020-12/schema"`) ||
			!strings.Contains(string(encoded), `"minLength":2`) {
			t.Fatalf("draft or constraint changed: %s", encoded)
		}
	})
	t.Run("draft-7-ref-sibling-limitation", func(t *testing.T) {
		_, err := StrictOutputSchemaFromJSON(json.RawMessage(`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"value":{"$ref":"#/$defs/nullable","type":"string"}},"$defs":{"nullable":{"type":["string","null"]}}}`))
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("unversioned-tuple-fails-closed", func(t *testing.T) {
		assertSchemaError(t, json.RawMessage(`{"type":"array","items":[{"type":"string"}]}`), "invalid_schema", "")
		if _, err := StrictOutputSchemaFromJSON(json.RawMessage(`{"$schema":"http://json-schema.org/draft-07/schema#","type":"array","items":[{"type":"string"}]}`)); err != nil {
			t.Fatalf("explicit Draft 7 tuple rejected: %v", err)
		}
	})
	t.Run("annotation-data-does-not-select-draft-7", func(t *testing.T) {
		assertSchemaError(t,
			json.RawMessage(`{"type":"object","properties":{"value":{"$ref":"#/$defs/nullable","type":"string","default":{"items":[{}]}}},"$defs":{"nullable":{"type":["string","null"]}}}`),
			"optional_non_nullable", "/properties/value")
	})
	t.Run("unsupported-draft-fails-closed", func(t *testing.T) {
		assertSchemaError(t, json.RawMessage(`{"$schema":"https://json-schema.org/draft/9999/schema","type":"object"}`), "invalid_schema", "")
		assertSchemaError(t, json.RawMessage(`{"$schema":"https://json-schema.org/draft/2019-09/schema","type":"object"}`), "invalid_schema", "")
		assertSchemaError(t, json.RawMessage(`{"$schema":7,"type":"object"}`), "invalid_schema", "")
	})
	t.Run("unknown-annotation-preserved", func(t *testing.T) {
		schema, err := StrictOutputSchemaFromJSON(json.RawMessage(`{"type":"object","required":["name"],"properties":{"name":{"type":"string","x-note":{"level":2}}}}`))
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := schema.MarshalJSON()
		if !strings.Contains(string(encoded), `"x-note":{"level":2}`) {
			t.Fatalf("unknown keyword changed: %s", encoded)
		}
	})
	t.Run("unknown-assertion-has-validation-limitation", func(t *testing.T) {
		schema, err := StrictOutputSchemaFromJSON(json.RawMessage(`{"type":"object","required":["name"],"properties":{"name":{"type":"string","x-must-equal":"fixed"}}}`))
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := schema.MarshalJSON()
		if !strings.Contains(string(encoded), `"x-must-equal":"fixed"`) {
			t.Fatalf("unknown assertion changed: %s", encoded)
		}
	})
	t.Run("dynamic-anchor-has-resolution-limitation", func(t *testing.T) {
		schema, err := StrictOutputSchemaFromJSON(json.RawMessage(`{"$dynamicAnchor":"node","type":"object"}`))
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := schema.MarshalJSON()
		if !strings.Contains(string(encoded), `"$dynamicAnchor":"node"`) {
			t.Fatalf("dynamic anchor changed: %s", encoded)
		}
	})
	t.Run("vocabulary-fails-closed", func(t *testing.T) {
		assertSchemaError(t,
			json.RawMessage(`{"$vocabulary":{"https://example.test/vocab":true},"type":"object"}`),
			"invalid_schema", "")
	})
	t.Run("additional-properties-schema-preserved", func(t *testing.T) {
		schema, err := StrictOutputSchemaFromJSON(json.RawMessage(`{"type":"object","additionalProperties":{"type":"object","maxProperties":2,"x-note":{"level":3},"properties":{"note":{"type":["string","null"]}}}}`))
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := schema.MarshalJSON()
		if !strings.Contains(string(encoded), `"required":["note"]`) ||
			!strings.Contains(string(encoded), `"maxProperties":2`) ||
			!strings.Contains(string(encoded), `"x-note":{"level":3}`) {
			t.Fatalf("nested additionalProperties schema was not normalized: %s", encoded)
		}
	})
	t.Run("conditional-null-fails-closed", func(t *testing.T) {
		assertSchemaError(t,
			json.RawMessage(`{"type":"object","properties":{"value":{"if":{"type":"null"},"then":false,"else":true}}}`),
			"optional_non_nullable", "/properties/value")
	})
	t.Run("cyclic-ref-fails-closed", func(t *testing.T) {
		assertSchemaError(t, json.RawMessage(`{"$defs":{"node":{"$ref":"#/$defs/node"}},"$ref":"#/$defs/node"}`), "cyclic_ref", "/$defs/node/$ref")
	})
	t.Run("external-ref-fails-closed", func(t *testing.T) {
		assertSchemaError(t, json.RawMessage(`{"$ref":"https://example.test/schema"}`), "external_ref", "/$ref")
	})
	t.Run("unresolvable-ref-fails-closed", func(t *testing.T) {
		assertSchemaError(t, json.RawMessage(`{"$ref":"#/$defs/missing"}`), "unresolvable_ref", "/$ref")
	})
	t.Run("dynamic-ref-fails-closed", func(t *testing.T) {
		assertSchemaError(t, json.RawMessage(`{"$dynamicRef":"#node"}`), "unsupported_dynamic_ref", "/$dynamicRef")
	})
}

func TestStrictOutputSchemaUsesJSONSchemaSemanticsForNullAdmission(t *testing.T) {
	tests := []struct {
		name     string
		schema   string
		wantKind string
	}{
		{
			name:     "reference rejects null while sibling admits it",
			schema:   `{"type":"object","properties":{"x":{"$ref":"#/$defs/nonNull","type":["string","null"]}},"$defs":{"nonNull":{"type":"string"}}}`,
			wantKind: "optional_non_nullable",
		},
		{
			name:     "reference admits null while sibling rejects it",
			schema:   `{"type":"object","properties":{"x":{"$ref":"#/$defs/nullable","type":"string"}},"$defs":{"nullable":{"type":["string","null"]}}}`,
			wantKind: "optional_non_nullable",
		},
		{
			name:   "reference and sibling both admit null",
			schema: `{"type":"object","properties":{"x":{"$ref":"#/$defs/nullable","type":["string","null"]}},"$defs":{"nullable":{"type":["string","null"]}}}`,
		},
		{
			name:     "nested local references with siblings",
			schema:   `{"type":"object","properties":{"x":{"$ref":"#/$defs/outer","type":["string","null"]}},"$defs":{"outer":{"$ref":"#/$defs/inner","type":["string","null"]},"inner":{"type":"string"}}}`,
			wantKind: "optional_non_nullable",
		},
		{
			name:     "allOf requires every branch to admit null",
			schema:   `{"type":"object","properties":{"x":{"allOf":[{"type":["string","null"]},{"type":"string"}]}}}`,
			wantKind: "optional_non_nullable",
		},
		{
			name:   "anyOf accepts one matching branch",
			schema: `{"type":"object","properties":{"x":{"anyOf":[{"type":"string"},{"type":"null"}]}}}`,
		},
		{
			name:     "oneOf rejects two matching branches",
			schema:   `{"type":"object","properties":{"x":{"oneOf":[{}, {"type":"null"}]}}}`,
			wantKind: "optional_non_nullable",
		},
		{
			name:     "not rejects null",
			schema:   `{"type":"object","properties":{"x":{"not":{"const":null}}}}`,
			wantKind: "optional_non_nullable",
		},
		{
			name:   "enum accepts null",
			schema: `{"type":"object","properties":{"x":{"enum":[null,"x"]}}}`,
		},
		{
			name:     "conditional applies matching then branch",
			schema:   `{"type":"object","properties":{"x":{"if":{"type":"null"},"then":{"const":"not-null"},"else":true}}}`,
			wantKind: "optional_non_nullable",
		},
		{
			name:   "draft seven ignores reference siblings",
			schema: `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"x":{"$ref":"#/$defs/nullable","type":"string"}},"$defs":{"nullable":{"type":["string","null"]}}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schema, err := StrictOutputSchemaFromJSON(json.RawMessage(test.schema))
			if test.wantKind != "" {
				var policyErr *SchemaPolicyError
				if !errors.As(err, &policyErr) || policyErr.Kind != test.wantKind || policyErr.Path != "/properties/x" {
					t.Fatalf("error = %#v, want %s at /properties/x", policyErr, test.wantKind)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := schema.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(encoded), `"required":["x"]`) {
				t.Fatalf("schema = %s", encoded)
			}
		})
	}
}

func TestStrictOutputSchemaFailsClosedWhenNullProbeSchemaDoesNotCompile(t *testing.T) {
	assertSchemaErrorKind(t, json.RawMessage(`{"type":"object","properties":{"x":{"type":["null",1]}}}`), "nullable_analysis")
	assertSchemaErrorKind(t, json.RawMessage(`{"type":"object","required":["x"],"properties":{"x":{"type":["null",1]}}}`), "invalid_schema")
}

func TestStrictOutputSchemaDecisionMatchesDirectValidator(t *testing.T) {
	propertySchemas := []string{
		`{"type":"null"}`,
		`{"type":"string"}`,
		`{"allOf":[{"type":["string","null"]},{"const":null}]}`,
		`{"anyOf":[{"type":"string"},{"enum":[null]}]}`,
		`{"oneOf":[{}, {"type":"null"}]}`,
		`{"not":{"enum":[null]}}`,
		`{"if":{"type":"null"},"then":false,"else":true}`,
	}

	for _, propertySchema := range propertySchemas {
		raw := json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"x":` + propertySchema + `}}`)
		var document any
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.UseNumber()
		if err := decoder.Decode(&document); err != nil {
			t.Fatal(err)
		}
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource("https://test.invalid/schema.json", document); err != nil {
			t.Fatal(err)
		}
		property, err := compiler.Compile("https://test.invalid/schema.json#/properties/x")
		if err != nil {
			t.Fatal(err)
		}
		wantPromotion := property.Validate(nil) == nil
		_, transformErr := StrictOutputSchemaFromJSON(raw)
		gotPromotion := transformErr == nil
		if gotPromotion != wantPromotion {
			t.Errorf("property %s: promoted = %v, direct validator accepts null = %v, error = %v", propertySchema, gotPromotion, wantPromotion, transformErr)
		}
	}
}

func TestCallerRejectsUncertainNullAdmissionBeforeRunnerInvocation(t *testing.T) {
	runner := &fakeRunner{}
	caller, err := New(ReadOnlyEphemeralOptions(runner))
	if err != nil {
		t.Fatal(err)
	}
	_, err = caller.CallDetailed(context.Background(), llmadapter.Request{
		Prompt:       "must not run",
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"x":{"type":["null",1]}}}`),
	})
	var policyErr *SchemaPolicyError
	if !errors.As(err, &policyErr) || policyErr.Kind != "nullable_analysis" || policyErr.Path != "/properties/x" {
		t.Fatalf("error = %#v", policyErr)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("runner requests = %d", len(runner.requests))
	}
}

func TestStrictOutputSchemaRejectsDuplicateKeysAndPreservesPointerPath(t *testing.T) {
	assertSchemaErrorKind(t, json.RawMessage(`{"type":"object","type":"string"}`), "invalid_json")
	_, err := StrictOutputSchemaFromJSON(json.RawMessage(`{"type":"object","properties":{"a/b~c":{"type":"string"}}}`))
	var policyErr *SchemaPolicyError
	if !errors.As(err, &policyErr) || policyErr.Path != "/properties/a~1b~0c" {
		t.Fatalf("error = %#v", policyErr)
	}
}

func TestStrictOutputSchemaTraversesSupportedSubschemaPositions(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		kind string
	}{
		{"additional items", `{"additionalItems":{"type":"object","properties":{"value":{"type":"string"}}}}`, "optional_non_nullable"},
		{"content schema", `{"contentSchema":{"type":"object","properties":{"value":{"type":"string"}}}}`, "optional_non_nullable"},
		{"tuple items", `{"$schema":"http://json-schema.org/draft-07/schema#","items":[{"type":"object","properties":{"value":{"type":"string"}}}]}`, "optional_non_nullable"},
		{"schema dependency", `{"dependencies":{"value":{"type":"object","properties":{"nested":{"type":"string"}}}}}`, "optional_non_nullable"},
		{"dynamic ref", `{"$dynamicRef":"#node"}`, "unsupported_dynamic_ref"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertSchemaErrorKind(t, json.RawMessage(test.raw), test.kind)
		})
	}

	if _, err := StrictOutputSchemaFromJSON(json.RawMessage(`{"dependencies":{"value":["other"]}}`)); err != nil {
		t.Fatalf("property dependency rejected: %v", err)
	}
}

func assertGoSchemaAccepted[T any](t *testing.T) {
	t.Helper()
	if _, err := StrictOutputSchemaFromJSON(schemaFor[T](t)); err != nil {
		t.Fatalf("schema rejected: %v\n%s", err, schemaFor[T](t))
	}
}

func assertDecodedValuesEqual[T any](t *testing.T, absentJSON, nullJSON string) {
	t.Helper()
	var absent, explicitNull T
	if err := json.Unmarshal([]byte(absentJSON), &absent); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(nullJSON), &explicitNull); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(absent, explicitNull) {
		t.Fatalf("absence/null decoded values differ: absent=%#v null=%#v", absent, explicitNull)
	}
}

func assertSchemaJSONValueEqual(t *testing.T, raw json.RawMessage) {
	t.Helper()
	schema, err := StrictOutputSchemaFromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := schema.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var before, after any
	if err := json.Unmarshal(raw, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("schema JSON value changed:\nbefore: %s\nafter:  %s", raw, encoded)
	}
}

func schemaFor[T any](t *testing.T) json.RawMessage {
	t.Helper()
	raw, err := llmschema.SchemaJSONFor[T]()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertSchemaErrorKind(t *testing.T, raw json.RawMessage, kind string) {
	t.Helper()
	_, err := StrictOutputSchemaFromJSON(raw)
	var policyErr *SchemaPolicyError
	if !errors.As(err, &policyErr) || policyErr.Kind != kind {
		t.Fatalf("error = %v, want SchemaPolicyError kind %s", err, kind)
	}
}

func assertSchemaError(t *testing.T, raw json.RawMessage, kind, path string) {
	t.Helper()
	_, err := StrictOutputSchemaFromJSON(raw)
	var policyErr *SchemaPolicyError
	if !errors.As(err, &policyErr) || policyErr.Kind != kind || policyErr.Path != path {
		t.Fatalf("error = %#v, want SchemaPolicyError kind %s at %q", policyErr, kind, path)
	}

	runner := &fakeRunner{}
	caller, err := New(ReadOnlyEphemeralOptions(runner))
	if err != nil {
		t.Fatal(err)
	}
	_, err = caller.CallDetailed(context.Background(), llmadapter.Request{Prompt: "must not run", OutputSchema: raw})
	policyErr = nil
	if !errors.As(err, &policyErr) || policyErr.Kind != kind || policyErr.Path != path {
		t.Fatalf("Caller error = %#v, want SchemaPolicyError kind %s at %q", policyErr, kind, path)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("runner received %d requests after fail-closed schema error", len(runner.requests))
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

func bytesEqual(left, right []byte) bool { return string(left) == string(right) }
