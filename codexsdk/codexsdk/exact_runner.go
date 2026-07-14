package codexsdk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2"
)

type exactRunner struct{ client *Client }

type Stream[R any] struct {
	mu      sync.Mutex
	state   *exactRunState
	cursor  int
	current protocolv2.ServerNotification
}

type exactRunState struct {
	client   *Client
	threadID string
	// turnID is guarded by mu, including reads performed while routing the run.
	turnID  string
	updated chan struct{}
	done    chan struct{}

	// notificationOrderMu preserves the ingestion order of pending and live
	// notifications across attachment. It is per run so unrelated turns do not
	// serialize behind one another.
	notificationOrderMu              sync.Mutex
	testAtNotificationOrderGate      func()
	testAfterNotificationOrderLocked func()
	mu                               sync.Mutex
	result                           any
	hasResult                        bool
	err                              error
	terminal                         bool
}

type TurnError struct {
	ThreadID string
	Turn     protocolv2.Turn
	Err      error
}

func (e *TurnError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("codexsdk: thread %s turn %s status %s: %v", e.ThreadID, e.Turn.ID, e.Turn.Status, e.Err)
}

func (e *TurnError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (c *Client) ThreadRunner() ThreadRunner { return &exactRunner{client: c} }

func (r *exactRunner) Start(ctx context.Context, request StartThreadRunRequest) (StartedThreadRun, error) {
	stream, err := r.StartStream(ctx, request)
	if err != nil {
		return StartedThreadRun{}, err
	}
	return drainExactStream(ctx, stream)
}

func (r *exactRunner) Resume(ctx context.Context, request ResumeThreadRunRequest) (ResumedThreadRun, error) {
	stream, err := r.ResumeStream(ctx, request)
	if err != nil {
		return ResumedThreadRun{}, err
	}
	return drainExactStream(ctx, stream)
}

func (r *exactRunner) StartStream(ctx context.Context, request StartThreadRunRequest) (*Stream[StartedThreadRun], error) {
	if r == nil || r.client == nil {
		return nil, ErrClientClosed
	}
	if err := r.client.checkOpen(); err != nil {
		return nil, err
	}
	if request.Turn.ThreadID != "" {
		return nil, errors.New("codexsdk: StartThreadRunRequest.Turn.ThreadID is composition-owned")
	}
	threadParams, err := cloneJSON(request.Thread)
	if err != nil {
		return nil, fmt.Errorf("codexsdk: clone thread/start params: %w", err)
	}
	turnParams, err := cloneJSON(request.Turn)
	if err != nil {
		return nil, fmt.Errorf("codexsdk: clone turn/start params: %w", err)
	}
	var started protocolv2.ThreadStartResponse
	if err := r.client.callProtocol(ctx, protocolv2.MethodThreadStart, threadParams, &started); err != nil {
		return nil, err
	}
	initial := StartedThreadRun{Start: started, Run: ThreadRunResult{InputStats: exactInputStats(turnParams.Input)}}
	if started.Thread.ID == "" {
		state := newExactRunState(r.client, "", initial)
		state.finish(fmt.Errorf("codexsdk: thread/start response missing thread id: %w", ErrMissingThreadID))
		return &Stream[StartedThreadRun]{state: state}, nil
	}
	state := r.client.newExactRunState(started.Thread.ID, initial)
	r.client.registerAttachingExactStream(state)
	stream := &Stream[StartedThreadRun]{state: state}
	turnParams.ThreadID = started.Thread.ID
	var turnStarted protocolv2.TurnStartResponse
	if err := r.client.callProtocol(ctx, protocolv2.MethodTurnStart, turnParams, &turnStarted); err != nil {
		r.client.unregisterAttachingExactStream(state)
		state.finish(err)
		return stream, nil
	}
	if turnStarted.Turn.ID == "" {
		finishMissingTurnID(r.client, state, turnStarted.Turn)
		return stream, nil
	}
	if r.client.testBeforeExactTurnAttach != nil {
		r.client.testBeforeExactTurnAttach()
	}
	r.client.attachExactStreamForTurn(state, turnStarted.Turn)
	return stream, nil
}

func (r *exactRunner) ResumeStream(ctx context.Context, request ResumeThreadRunRequest) (*Stream[ResumedThreadRun], error) {
	if r == nil || r.client == nil {
		return nil, ErrClientClosed
	}
	if err := r.client.checkOpen(); err != nil {
		return nil, err
	}
	if request.Turn.ThreadID != "" {
		return nil, errors.New("codexsdk: ResumeThreadRunRequest.Turn.ThreadID is composition-owned")
	}
	threadParams, err := cloneJSON(request.Thread)
	if err != nil {
		return nil, fmt.Errorf("codexsdk: clone thread/resume params: %w", err)
	}
	turnParams, err := cloneJSON(request.Turn)
	if err != nil {
		return nil, fmt.Errorf("codexsdk: clone turn/start params: %w", err)
	}
	var resumed protocolv2.ThreadResumeResponse
	if err := r.client.callProtocol(ctx, protocolv2.MethodThreadResume, threadParams, &resumed); err != nil {
		return nil, err
	}
	initial := ResumedThreadRun{Resume: resumed, Run: ThreadRunResult{InputStats: exactInputStats(turnParams.Input)}}
	threadID := resumed.Thread.ID
	if threadID == "" {
		threadID = threadParams.ThreadID
	}
	if threadID == "" {
		state := newExactRunState(r.client, "", initial)
		state.finish(fmt.Errorf("codexsdk: thread/resume response missing thread id: %w", ErrMissingThreadID))
		return &Stream[ResumedThreadRun]{state: state}, nil
	}
	state := r.client.newExactRunState(threadID, initial)
	r.client.registerAttachingExactStream(state)
	stream := &Stream[ResumedThreadRun]{state: state}
	turnParams.ThreadID = threadID
	var turnStarted protocolv2.TurnStartResponse
	if err := r.client.callProtocol(ctx, protocolv2.MethodTurnStart, turnParams, &turnStarted); err != nil {
		r.client.unregisterAttachingExactStream(state)
		state.finish(err)
		return stream, nil
	}
	if turnStarted.Turn.ID == "" {
		finishMissingTurnID(r.client, state, turnStarted.Turn)
		return stream, nil
	}
	r.client.attachExactStreamForTurn(state, turnStarted.Turn)
	return stream, nil
}

func finishMissingTurnID(client *Client, state *exactRunState, turn protocolv2.Turn) {
	state.setTurn(turn)
	client.unregisterAttachingExactStream(state)
	state.finish(fmt.Errorf("codexsdk: turn/start response missing turn id: %w", ErrMissingTurnID))
}

func drainExactStream[R any](ctx context.Context, stream *Stream[R]) (R, error) {
	defer stream.Close()
	for stream.Next(ctx) {
	}
	result, _ := stream.Result()
	return result, stream.Err()
}

func (s *Stream[R]) Next(ctx context.Context) bool {
	if s == nil || s.state == nil {
		return false
	}
	for {
		s.mu.Lock()
		s.state.mu.Lock()
		notification, ok := s.state.notificationAtLocked(s.cursor)
		terminal := s.state.terminal
		updated := s.state.updated
		if ok {
			s.cursor++
		}
		s.state.mu.Unlock()
		if ok {
			s.current, _ = cloneJSON(notification)
			s.mu.Unlock()
			return true
		}
		s.mu.Unlock()
		if terminal {
			return false
		}
		select {
		case <-updated:
		case <-ctx.Done():
			s.state.cancel(ctx.Err())
			return false
		case <-s.state.done:
		}
	}
}

func (s *Stream[R]) Notification() protocolv2.ServerNotification {
	if s == nil {
		return protocolv2.ServerNotification{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned, _ := cloneJSON(s.current)
	return cloned
}

// Wait observes run completion without consuming notifications or owning the
// run lifecycle. Context cancellation stops only this call and returns the
// latest immutable partial result; use Close to cancel the shared run.
func (s *Stream[R]) Wait(ctx context.Context) (R, error) {
	var zero R
	if s == nil || s.state == nil {
		return zero, ErrStreamClosed
	}
	if result, err, terminal := s.waitSnapshot(nil); terminal {
		return result, err
	}
	select {
	case <-s.state.done:
		result, err, _ := s.waitSnapshot(nil)
		return result, err
	case <-ctx.Done():
		// Recheck terminal state while taking the snapshot so an already
		// completed run wins over caller-local cancellation.
		result, err, _ := s.waitSnapshot(ctx.Err())
		return result, err
	}
}

func (s *Stream[R]) waitSnapshot(waitErr error) (R, error, bool) {
	var zero R
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	result, ok := s.state.result.(R)
	if !ok || !s.state.hasResult {
		if s.state.terminal {
			return zero, s.state.err, true
		}
		return zero, waitErr, false
	}
	cloned, err := cloneExactResult(result)
	if err != nil {
		return zero, err, s.state.terminal
	}
	if s.state.terminal {
		return cloned, s.state.err, true
	}
	return cloned, waitErr, false
}

func (s *Stream[R]) Result() (R, bool) {
	var zero R
	if s == nil || s.state == nil {
		return zero, false
	}
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	result, ok := s.state.result.(R)
	hasResult := s.state.hasResult
	if !ok || !hasResult {
		return zero, false
	}
	cloned, err := cloneExactResult(result)
	if err != nil {
		return zero, false
	}
	return cloned, true
}

func cloneExactResult[R any](result R) (R, error) {
	var zero R
	switch typed := any(result).(type) {
	case StartedThreadRun:
		start, err := cloneJSON(typed.Start)
		if err != nil {
			return zero, err
		}
		run, err := cloneThreadRunResult(typed.Run)
		if err != nil {
			return zero, err
		}
		return any(StartedThreadRun{Start: start, Run: run}).(R), nil
	case ResumedThreadRun:
		resume, err := cloneJSON(typed.Resume)
		if err != nil {
			return zero, err
		}
		run, err := cloneThreadRunResult(typed.Run)
		if err != nil {
			return zero, err
		}
		return any(ResumedThreadRun{Resume: resume, Run: run}).(R), nil
	default:
		return cloneJSON(result)
	}
}

func cloneThreadRunResult(run ThreadRunResult) (ThreadRunResult, error) {
	cloned := run
	if run.Turn.ID != "" || run.Turn.Items != nil || run.Turn.Status != "" {
		turn, err := cloneJSON(run.Turn)
		if err != nil {
			return ThreadRunResult{}, err
		}
		cloned.Turn = turn
	}
	if run.Usage != nil {
		usage, err := cloneJSON(*run.Usage)
		if err != nil {
			return ThreadRunResult{}, err
		}
		cloned.Usage = &usage
	}
	cloned.Notifications = make([]protocolv2.ServerNotification, len(run.Notifications))
	for index, notification := range run.Notifications {
		copy, err := cloneJSON(notification)
		if err != nil {
			return ThreadRunResult{}, err
		}
		cloned.Notifications[index] = copy
	}
	cloned.Diagnostics = append([]DiagnosticRef(nil), run.Diagnostics...)
	return cloned, nil
}

func (s *Stream[R]) Err() error {
	if s == nil || s.state == nil {
		return ErrStreamClosed
	}
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	return s.state.err
}

func (s *Stream[R]) Close() error {
	if s == nil || s.state == nil {
		return nil
	}
	s.state.cancel(ErrStreamClosed)
	return nil
}

func newExactRunState(client *Client, threadID string, result any) *exactRunState {
	return &exactRunState{
		client:    client,
		threadID:  threadID,
		updated:   make(chan struct{}),
		done:      make(chan struct{}),
		result:    result,
		hasResult: true,
	}
}

func (c *Client) newExactRunState(threadID string, result any) *exactRunState {
	return newExactRunState(c, threadID, result)
}

func (s *exactRunState) notificationAtLocked(index int) (protocolv2.ServerNotification, bool) {
	switch result := s.result.(type) {
	case StartedThreadRun:
		if index < len(result.Run.Notifications) {
			return result.Run.Notifications[index], true
		}
	case ResumedThreadRun:
		if index < len(result.Run.Notifications) {
			return result.Run.Notifications[index], true
		}
	}
	return protocolv2.ServerNotification{}, false
}

func (s *exactRunState) setTurn(turn protocolv2.Turn) {
	s.mu.Lock()
	s.turnID = turn.ID
	s.updateRunLocked(func(run *ThreadRunResult) { run.Turn = turn })
	s.mu.Unlock()
}

func (c *Client) attachExactStreamForTurn(stream *exactRunState, turn protocolv2.Turn) {
	stream.notificationOrderMu.Lock()
	stream.setTurn(turn)
	if c.testAfterExactTurnPublished != nil {
		c.testAfterExactTurnPublished()
	}
	c.turnMu.Lock()
	c.attachExactStreamLocked(stream)
}

func (s *exactRunState) turnIDSnapshot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turnID
}

func (s *exactRunState) accept(notification protocolv2.ServerNotification) error {
	completeTerminal, err := s.acceptStateBeforeTerminalCompletion(notification)
	if completeTerminal != nil {
		completeTerminal()
	}
	return err
}

func (s *exactRunState) acceptStateBeforeTerminalCompletion(notification protocolv2.ServerNotification) (func(), error) {
	cloned, err := cloneJSON(notification)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.terminal {
		s.mu.Unlock()
		return nil, nil
	}
	s.updateRunLocked(func(run *ThreadRunResult) {
		run.Notifications = append(run.Notifications, cloned)
		if usage, ok := cloned.AsThreadTokenUsageUpdated(); ok {
			copy := usage.Params.TokenUsage
			run.Usage = &copy
		}
	})
	terminal, terminalErr := s.applyTerminalLocked(cloned)
	close(s.updated)
	s.updated = make(chan struct{})
	s.mu.Unlock()
	if terminal {
		return func() {
			if s.finishState(terminalErr) {
				s.unregister()
			}
		}, nil
	}
	return nil, nil
}

func (s *exactRunState) acceptOrdered(notification protocolv2.ServerNotification) error {
	completion, err := s.acceptOrderedBeforeTerminalCompletion(notification)
	if completion != nil {
		completion()
	}
	return err
}

func (s *exactRunState) acceptOrderedBeforeTerminalCompletion(notification protocolv2.ServerNotification) (func(), error) {
	if s.testAtNotificationOrderGate != nil {
		s.testAtNotificationOrderGate()
	}
	s.notificationOrderMu.Lock()
	if s.testAfterNotificationOrderLocked != nil {
		s.testAfterNotificationOrderLocked()
	}
	completion, err := s.acceptStateBeforeTerminalCompletion(notification)
	s.notificationOrderMu.Unlock()
	return completion, err
}

func (s *exactRunState) applyTerminalLocked(notification protocolv2.ServerNotification) (bool, error) {
	completed, ok := notification.AsTurnCompleted()
	if !ok {
		return false, nil
	}
	turn := completed.Params.Turn
	resultTurn, _ := cloneJSON(turn)
	s.updateRunLocked(func(run *ThreadRunResult) {
		run.Turn = resultTurn
		run.FinalResponse = finalResponseFromExactTurn(resultTurn)
	})
	switch turn.Status {
	case protocolv2.TurnStatusCompleted:
		if finalResponseFromExactTurn(turn) == "" {
			return true, errors.New("codexsdk: turn completed without final_answer agent message")
		}
		return true, nil
	case protocolv2.TurnStatusFailed:
		errorTurn, _ := cloneJSON(turn)
		return true, &TurnError{ThreadID: s.threadID, Turn: errorTurn, Err: ErrTurnFailed}
	case protocolv2.TurnStatusInterrupted:
		errorTurn, _ := cloneJSON(turn)
		return true, &TurnError{ThreadID: s.threadID, Turn: errorTurn, Err: ErrTurnInterrupted}
	default:
		return true, &TurnError{ThreadID: s.threadID, Turn: turn, Err: fmt.Errorf("unexpected terminal status %q", turn.Status)}
	}
}

func (s *exactRunState) updateRunLocked(update func(*ThreadRunResult)) {
	switch result := s.result.(type) {
	case StartedThreadRun:
		update(&result.Run)
		s.result = result
	case ResumedThreadRun:
		update(&result.Run)
		s.result = result
	}
}

func (s *exactRunState) addDiagnostic(ref DiagnosticRef) {
	s.mu.Lock()
	s.updateRunLocked(func(run *ThreadRunResult) {
		run.Diagnostics = append(run.Diagnostics, ref)
	})
	s.mu.Unlock()
}

func (s *exactRunState) addDiagnosticOrdered(ref DiagnosticRef) {
	s.notificationOrderMu.Lock()
	defer s.notificationOrderMu.Unlock()
	s.addDiagnostic(ref)
}

func (s *exactRunState) finish(err error) {
	if s.finishState(err) {
		s.unregister()
	}
}

func (s *exactRunState) finishState(err error) bool {
	s.mu.Lock()
	if s.terminal {
		s.mu.Unlock()
		return false
	}
	s.err = err
	s.terminal = true
	close(s.done)
	s.mu.Unlock()
	return true
}

func (s *exactRunState) unregister() {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client != nil {
		client.unregisterExactRun(s)
	}
}

func (s *exactRunState) cancel(err error) {
	s.mu.Lock()
	if s.terminal {
		s.mu.Unlock()
		return
	}
	client := s.client
	threadID := s.threadID
	turnID := s.turnID
	s.mu.Unlock()
	s.finish(err)
	if client != nil && turnID != "" {
		client.bestEffortInterrupt(threadID, turnID)
	}
}

func finalResponseFromExactTurn(turn protocolv2.Turn) string {
	for index := len(turn.Items) - 1; index >= 0; index-- {
		message, ok := turn.Items[index].AsAgentMessage()
		if !ok || message.Text == "" || message.Phase == nil || message.Phase.Value == nil {
			continue
		}
		if *message.Phase.Value == protocolv2.MessagePhaseFinalAnswer {
			return message.Text
		}
	}
	return ""
}

func exactInputStats(input []protocolv2.UserInput) InputStats {
	stats := InputStats{ItemsCount: len(input)}
	for _, item := range input {
		if text, ok := item.AsText(); ok {
			stats.TextBytes += len([]byte(text.Text))
		}
		switch item.Kind() {
		case protocolv2.UserInputKindImage, protocolv2.UserInputKindLocalImage, protocolv2.UserInputKindSkill, protocolv2.UserInputKindMention:
			stats.AttachmentCount++
		}
	}
	raw, _ := json.Marshal(input)
	sum := sha256.Sum256(raw)
	stats.InputItemsHash = hex.EncodeToString(sum[:])
	return stats
}

func cloneJSON[T any](value T) (T, error) {
	var cloned T
	raw, err := json.Marshal(value)
	if err != nil {
		return cloned, err
	}
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return cloned, err
	}
	return cloned, nil
}
