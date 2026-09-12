package codexsdk

import (
	"context"
	"errors"
	"fmt"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

var (
	ErrClientClosed             = errors.New("codexsdk: client closed")
	ErrStreamClosed             = errors.New("codexsdk: stream closed")
	ErrTurnFailed               = errors.New("codexsdk: turn failed")
	ErrTurnInterrupted          = errors.New("codexsdk: turn interrupted")
	ErrNotificationBackpressure = errors.New("codexsdk: notification backpressure")
	ErrHandlerFailed            = errors.New("codexsdk: handler failed")
	ErrExactServerRequest       = errors.New("codexsdk: exact server request failed closed")
	ErrMissingThreadID          = errors.New("codexsdk: thread response missing thread id")
	ErrMissingTurnID            = errors.New("codexsdk: turn response missing turn id")
	ErrTurnAdmissionRejected    = errors.New("codexsdk: turn admission rejected")
)

type ExactServerRequestError struct {
	Kind   protocolv2.ServerRequestKind
	Reason string
}

func (e *ExactServerRequestError) Error() string {
	reason := e.Reason
	if reason == "" {
		reason = "requires application data"
	}
	return "codexsdk: exact server request " + string(e.Kind) + " " + reason
}

func (e *ExactServerRequestError) Unwrap() error { return ErrExactServerRequest }

// AdmitTurn inspects the exact decoded thread-start Server Observation and the
// exact pending TurnStartParams after thread/start and before any turn/start.
// The pending params include the composition-owned thread ID that will be sent.
// A non-nil error rejects continuation fail-closed. A nil callback preserves
// current Exact Run behavior. The decision is caller-owned; the SDK does not
// merge requested overrides into observed facts or define an authorization
// policy.
type AdmitTurn func(protocolv2.ThreadStartResponse, protocolv2.TurnStartParams) error

type StartThreadRunRequest struct {
	Thread    protocolv2.ThreadStartParams
	Turn      protocolv2.TurnStartParams
	AdmitTurn AdmitTurn
}

// TurnAdmissionError is the fail-closed cause of rejected pre-turn admission.
// The exact thread-start observation remains on StartedThreadRun.Start and is
// not rewritten. Unwrap includes ErrTurnAdmissionRejected and the
// caller-owned rejection error.
type TurnAdmissionError struct {
	Err error
}

func (e *TurnAdmissionError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Err == nil {
		return ErrTurnAdmissionRejected.Error()
	}
	return fmt.Sprintf("%s: %v", ErrTurnAdmissionRejected, e.Err)
}

func (e *TurnAdmissionError) Unwrap() error {
	if e == nil {
		return nil
	}
	if e.Err == nil {
		return ErrTurnAdmissionRejected
	}
	return errors.Join(ErrTurnAdmissionRejected, e.Err)
}

type ResumeThreadRunRequest struct {
	Thread protocolv2.ThreadResumeParams
	Turn   protocolv2.TurnStartParams
}

type ThreadRunResult struct {
	Turn          protocolv2.Turn
	Usage         *protocolv2.ThreadTokenUsage
	Notifications []protocolv2.ServerNotification
	// FinalResponse is the text of the last observed agent message whose phase
	// is final_answer. FinalResponsePresent distinguishes an observed empty
	// string from the absence of any final-answer observation.
	FinalResponse        string
	FinalResponsePresent bool
	InputStats           InputStats
	Diagnostics          []DiagnosticRef
}

type StartedThreadRun struct {
	Start protocolv2.ThreadStartResponse
	Run   ThreadRunResult
}

type ResumedThreadRun struct {
	Resume protocolv2.ThreadResumeResponse
	Run    ThreadRunResult
}

type ThreadRunner interface {
	Start(context.Context, StartThreadRunRequest) (StartedThreadRun, error)
	Resume(context.Context, ResumeThreadRunRequest) (ResumedThreadRun, error)
	StartStream(context.Context, StartThreadRunRequest) (*Stream[StartedThreadRun], error)
	ResumeStream(context.Context, ResumeThreadRunRequest) (*Stream[ResumedThreadRun], error)
}

type ServerNotificationHandler func(context.Context, protocolv2.ServerNotification) error

type ClientOptions struct {
	CWD                       string
	Command                   []string
	Initialize                protocolv2.InitializeParams
	ServerRequestHandler      ServerRequestHandler
	ServerNotificationHandler ServerNotificationHandler
	NotificationQueueCapacity int
}

type ServerRequestHandler func(context.Context, protocolv2.ServerRequest) (ServerRequestResponse, error)

type ServerRequestResponse struct {
	kind  protocolv2.ServerRequestKind
	value any
}

type InputStats struct {
	ItemsCount      int
	TextBytes       int
	AttachmentCount int
	InputItemsHash  string
}

type DiagnosticRef struct {
	Kind      string
	ID        string
	Path      string
	SizeBytes int64
	SHA256    string
}
