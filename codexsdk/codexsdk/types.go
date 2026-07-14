package codexsdk

import (
	"context"
	"errors"
	"github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2"
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

type StartThreadRunRequest struct {
	Thread protocolv2.ThreadStartParams
	Turn   protocolv2.TurnStartParams
}

type ResumeThreadRunRequest struct {
	Thread protocolv2.ThreadResumeParams
	Turn   protocolv2.TurnStartParams
}

type ThreadRunResult struct {
	Turn          protocolv2.Turn
	Usage         *protocolv2.ThreadTokenUsage
	Notifications []protocolv2.ServerNotification
	FinalResponse string
	InputStats    InputStats
	Diagnostics   []DiagnosticRef
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
