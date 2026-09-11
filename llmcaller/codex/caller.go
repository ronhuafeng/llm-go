package codexcaller

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/ronhuafeng/llm-go/codexsdk"
	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
	"github.com/ronhuafeng/llm-go/llmkit/llmadapter"
)

var (
	// ErrNilThreadRunner reports a missing or typed-nil runner.
	ErrNilThreadRunner = errors.New("llmcaller/codex: thread runner is nil")
	// ErrMissingSchemaJSON reports a request without an output schema.
	ErrMissingSchemaJSON = errors.New("llmcaller/codex: output schema JSON is required")
	// ErrMissingAdmission reports construction of a neutral Caller without an
	// application-owned pre-turn admission function.
	ErrMissingAdmission = errors.New("llmcaller/codex: application-owned AdmitTurn is required")
)

// ThreadRunner is the exact subset of codexsdk.ThreadRunner used by Caller.
type ThreadRunner interface {
	Start(context.Context, codexsdk.StartThreadRunRequest) (codexsdk.StartedThreadRun, error)
	StartStream(context.Context, codexsdk.StartThreadRunRequest) (*codexsdk.Stream[codexsdk.StartedThreadRun], error)
}

// Options configures a Caller with exact generated Codex defaults.
// Defaults.AdmitTurn is application-owned and required. The adapter forwards it
// unchanged so the application can decide whether decoded effective execution
// facts are acceptable before turn/start.
type Options struct {
	Runner   ThreadRunner
	Defaults codexsdk.StartThreadRunRequest
}

// Details retains the exact Codex run behind a neutral response.
type Details struct {
	Run codexsdk.StartedThreadRun
}

type startedRunStream interface {
	Next(context.Context) bool
	Notification() protocolv2.ServerNotification
	Wait(context.Context) (codexsdk.StartedThreadRun, error)
	Result() (codexsdk.StartedThreadRun, bool)
	Err() error
	Close() error
}

// Stream preserves exact SDK stream observation. Application-owned AdmitTurn
// has already run inside the exact SDK before turn/start.
type Stream struct {
	stream   startedRunStream
	sdk      *codexsdk.Stream[codexsdk.StartedThreadRun]
	finalize func(codexsdk.StartedThreadRun, error) (codexsdk.StartedThreadRun, error)
}

// SDKStream returns the underlying exact SDK stream as a typed escape hatch.
func (s *Stream) SDKStream() *codexsdk.Stream[codexsdk.StartedThreadRun] {
	if s == nil {
		return nil
	}
	return s.sdk
}

// Next advances over the exact SDK notification history.
func (s *Stream) Next(ctx context.Context) bool {
	return s != nil && s.stream != nil && s.stream.Next(ctx)
}

// Notification returns the current exact SDK notification.
func (s *Stream) Notification() protocolv2.ServerNotification {
	if s == nil || s.stream == nil {
		return protocolv2.ServerNotification{}
	}
	return s.stream.Notification()
}

// Wait returns the exact terminal or partial result together with SDK errors.
func (s *Stream) Wait(ctx context.Context) (codexsdk.StartedThreadRun, error) {
	if s == nil || s.stream == nil {
		return codexsdk.StartedThreadRun{}, codexsdk.ErrStreamClosed
	}
	run, err := s.stream.Wait(ctx)
	return s.finalizeRun(run, err)
}

// Result returns the latest exact SDK result snapshot without consuming it.
func (s *Stream) Result() (codexsdk.StartedThreadRun, bool) {
	if s == nil || s.stream == nil {
		return codexsdk.StartedThreadRun{}, false
	}
	return s.stream.Result()
}

// Err returns the exact SDK terminal cause together with any snapshot failure.
func (s *Stream) Err() error {
	if s == nil || s.stream == nil {
		return codexsdk.ErrStreamClosed
	}
	run, ok := s.stream.Result()
	if !ok {
		return s.stream.Err()
	}
	streamErr := s.stream.Err()
	if streamErr == nil && !isTerminalRun(run) {
		return nil
	}
	_, err := s.finalizeRun(run, streamErr)
	return err
}

func isTerminalRun(run codexsdk.StartedThreadRun) bool {
	switch run.Run.Turn.Status {
	case protocolv2.TurnStatusCompleted, protocolv2.TurnStatusFailed, protocolv2.TurnStatusInterrupted:
		return true
	default:
		return false
	}
}

// Close cancels the shared exact SDK run.
func (s *Stream) Close() error {
	if s == nil || s.stream == nil {
		return nil
	}
	return s.stream.Close()
}

func (s *Stream) finalizeRun(run codexsdk.StartedThreadRun, err error) (codexsdk.StartedThreadRun, error) {
	if s.finalize == nil {
		return run, err
	}
	return s.finalize(run, err)
}

// BackendName returns the execution backend identity used by neutral evidence.
// It does not claim an upstream model-provider identity.
func (Details) BackendName() string { return "codex" }

// Caller adapts neutral structured calls to exact Codex thread runs.
type Caller struct {
	runner   ThreadRunner
	defaults codexsdk.StartThreadRunRequest
}

var _ llmadapter.Caller = (*Caller)(nil)

// New validates options and clones mutable defaults. The application must
// provide Defaults.AdmitTurn; the adapter does not choose an execution policy.
func New(options Options) (*Caller, error) {
	if isNil(options.Runner) {
		return nil, ErrNilThreadRunner
	}
	if options.Defaults.Turn.ThreadID != "" {
		return nil, errors.New("llmcaller/codex: Defaults.Turn.ThreadID is adapter-owned")
	}
	if options.Defaults.Turn.Input != nil {
		return nil, errors.New("llmcaller/codex: Defaults.Turn.Input is adapter-owned")
	}
	if options.Defaults.Turn.OutputSchema != nil {
		return nil, errors.New("llmcaller/codex: Defaults.Turn.OutputSchema is adapter-owned")
	}
	if options.Defaults.AdmitTurn == nil {
		return nil, ErrMissingAdmission
	}
	defaults, err := cloneStartRequest(options.Defaults)
	if err != nil {
		return nil, fmt.Errorf("llmcaller/codex: clone defaults: %w", err)
	}
	return &Caller{runner: options.Runner, defaults: defaults}, nil
}

// Call executes the detailed path and projects its available neutral facts.
func (c *Caller) Call(ctx context.Context, request llmadapter.Request) (llmadapter.Response, error) {
	run, runErr := c.startRun(ctx, request)
	if !hasRunEvidence(run, runErr) {
		return llmadapter.Response{}, runErr
	}
	cloned, cloneErr := cloneStartedRun(run)
	return projectNeutralResponse(run, cloned, cloneErr), errors.Join(runErr, cloneErr)
}

// CallDetailed executes a structured call and returns the exact run, including
// partial evidence when an error also occurs.
func (c *Caller) CallDetailed(ctx context.Context, request llmadapter.Request) (codexsdk.StartedThreadRun, error) {
	run, runErr := c.startRun(ctx, request)
	return finalizeRun(run, runErr)
}

func (c *Caller) startRun(ctx context.Context, request llmadapter.Request) (codexsdk.StartedThreadRun, error) {
	if c == nil || isNil(c.runner) {
		return codexsdk.StartedThreadRun{}, ErrNilThreadRunner
	}
	startRequest, err := c.request(request)
	if err != nil {
		return codexsdk.StartedThreadRun{}, err
	}
	return c.runner.Start(ctx, startRequest)
}

func finalizeRun(run codexsdk.StartedThreadRun, runErr error) (codexsdk.StartedThreadRun, error) {
	cloned, cloneErr := cloneStartedRun(run)
	if cloneErr != nil {
		cloned = run
	}
	return cloned, errors.Join(runErr, cloneErr)
}

// CallStream starts the same exact request through the SDK streaming path.
func (c *Caller) CallStream(ctx context.Context, request llmadapter.Request) (*Stream, error) {
	if c == nil || isNil(c.runner) {
		return nil, ErrNilThreadRunner
	}
	startRequest, err := c.request(request)
	if err != nil {
		return nil, err
	}
	stream, streamErr := c.runner.StartStream(ctx, startRequest)
	if stream == nil {
		return nil, streamErr
	}
	return c.wrapStream(stream, stream), streamErr
}

func (c *Caller) wrapStream(stream startedRunStream, sdk *codexsdk.Stream[codexsdk.StartedThreadRun]) *Stream {
	return &Stream{stream: stream, sdk: sdk, finalize: finalizeRun}
}

func (c *Caller) request(request llmadapter.Request) (codexsdk.StartThreadRunRequest, error) {
	outputSchema, err := StrictOutputSchemaFromJSON(request.OutputSchema)
	if err != nil {
		return codexsdk.StartThreadRunRequest{}, err
	}
	startRequest, err := cloneStartRequest(c.defaults)
	if err != nil {
		return codexsdk.StartThreadRunRequest{}, err
	}
	startRequest.Turn.ThreadID = ""
	startRequest.Turn.Input = []protocolv2.UserInput{
		protocolv2.NewUserInputText(protocolv2.UserInputText{Text: request.Prompt}),
	}
	startRequest.Turn.OutputSchema = &outputSchema
	return startRequest, nil
}

func responseFromRun(run codexsdk.StartedThreadRun) llmadapter.Response {
	cloned, cloneErr := cloneStartedRun(run)
	return projectNeutralResponse(run, cloned, cloneErr)
}

func projectNeutralResponse(run, cloned codexsdk.StartedThreadRun, cloneErr error) llmadapter.Response {
	response := llmadapter.Response{
		FinalResponse: run.Run.FinalResponse,
		Execution: llmadapter.ExecutionEvidence{
			BackendName: "codex",
		},
	}
	if model, ok := isolatedServedModel(run); ok {
		response.Execution.ObserveModel(model)
	}
	if usage, err := isolatedNeutralUsage(run.Run.Usage); err == nil {
		response.Execution.Usage = usage
	}
	if cloneErr == nil {
		response.BackendDetails = Details{Run: cloned}
	}
	return response
}

func isolatedServedModel(run codexsdk.StartedThreadRun) (string, bool) {
	var model string
	ok := !reflect.DeepEqual(run.Start, protocolv2.ThreadStartResponse{})
	if ok {
		model = run.Start.Model
	}
	for _, notification := range run.Run.Notifications {
		var cloned protocolv2.ServerNotification
		if err := cloneGenerated(notification, &cloned); err != nil {
			continue
		}
		if rerouted, routed := cloned.AsModelRerouted(); routed {
			model = rerouted.Params.ToModel
			ok = true
		}
	}
	return model, ok
}

func isolatedNeutralUsage(usage *protocolv2.ThreadTokenUsage) (*llmadapter.TokenUsage, error) {
	if usage == nil {
		return nil, nil
	}
	var cloned protocolv2.ThreadTokenUsage
	if err := cloneGenerated(*usage, &cloned); err != nil {
		return nil, err
	}
	projected := &llmadapter.TokenUsage{}
	projected.ObserveCounts(cloned.Total.InputTokens, cloned.Total.CachedInputTokens, cloned.Total.OutputTokens, cloned.Total.ReasoningOutputTokens)
	return projected, nil
}

func hasRunEvidence(run codexsdk.StartedThreadRun, runErr error) bool {
	return !reflect.DeepEqual(run, codexsdk.StartedThreadRun{}) || hasDecodedStart(run, runErr)
}

func hasDecodedStart(run codexsdk.StartedThreadRun, runErr error) bool {
	return run.Start.Thread.ID != "" || errors.Is(runErr, codexsdk.ErrMissingThreadID)
}
