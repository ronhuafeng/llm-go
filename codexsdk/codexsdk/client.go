package codexsdk

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultClientName  = "codex-go-sdk"
	defaultClientTitle = "Codex Go SDK"
)

// Client owns one Codex app-server process and its transport, callbacks, and
// exact generated protocol facades. Construct connected clients with New.
// The zero value is safe but inert.
type Client struct {
	options       ClientOptions
	ctx           context.Context
	cancel        context.CancelFunc
	handlerCtx    context.Context
	handlerCancel context.CancelFunc

	closeMu       sync.Mutex
	closed        bool
	failure       error
	normalClosing bool
	handlerWG     sync.WaitGroup

	shutdownOnce   sync.Once
	dispatchStop   chan struct{}
	dispatcherDone chan struct{}
	notifications  chan acceptedNotification

	writeMu sync.Mutex
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	cmd     *exec.Cmd

	nextID  atomic.Uint64
	pending sync.Map

	turnMu         sync.Mutex
	exactStreams   map[string]map[*exactRunState]struct{}
	exactAttaching map[string]map[*exactRunState]struct{}
	pendingEvents  map[string][]rpcNotification
	// replayingEvents keeps accepted evidence visible to terminalization after
	// attachment removes it from pendingEvents and until replay commits it.
	replayingEvents    map[*exactRunState][]rpcNotification
	pendingDiagnostics map[string][]DiagnosticRef
	pendingGlobal      error

	// testAfterExactStreamPublished pauses the deterministic test seam after an
	// exact stream becomes live and before pending evidence is replayed.
	testAfterExactStreamPublished  func()
	testAfterExactTurnPublished    func()
	testBeforeExactStreamOrderGate func()
	testBeforeExactTurnAttach      func()
	testPendingExactNotification   func(rpcNotification)
	testBeforePendingTerminalFence func()

	readerDone chan struct{}
}

type rpcResponse struct {
	result map[string]any
	err    error
}

type pendingCall struct {
	method   string
	response chan rpcResponse
	validate func(map[string]any) error
}

type rpcNotification struct {
	method   string
	params   map[string]any
	evidence *notificationEvidence
}

type notificationEvidence struct {
	ready      chan struct{}
	dispatched chan struct{}
	state      *exactRunState
	once       sync.Once
	completion func()
	err        error
}

type acceptedNotification struct {
	notification protocolv2.ServerNotification
	evidence     *notificationEvidence
	dispatched   chan struct{}
}

func New(options ClientOptions) (*Client, error) {
	normalized, err := validateOptions(options)
	if err != nil {
		return nil, err
	}
	clientCtx, cancel := context.WithCancel(context.Background())
	handlerCtx, handlerCancel := context.WithCancel(clientCtx)
	c := &Client{
		options:            normalized,
		ctx:                clientCtx,
		cancel:             cancel,
		handlerCtx:         handlerCtx,
		handlerCancel:      handlerCancel,
		exactStreams:       map[string]map[*exactRunState]struct{}{},
		exactAttaching:     map[string]map[*exactRunState]struct{}{},
		pendingEvents:      map[string][]rpcNotification{},
		pendingDiagnostics: map[string][]DiagnosticRef{},
		readerDone:         make(chan struct{}),
		dispatchStop:       make(chan struct{}),
		dispatcherDone:     make(chan struct{}),
	}
	queueCapacity := normalized.NotificationQueueCapacity
	if queueCapacity == 0 {
		queueCapacity = 64
	}
	c.notifications = make(chan acceptedNotification, queueCapacity)
	go c.notificationDispatcher()
	if err := c.start(); err != nil {
		cancel()
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	initializeParamsMap, err := encodeProtocolParams(protocolv2.MethodInitialize, normalized.Initialize)
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	initializeResult, err := c.call(ctx, protocolv2.MethodInitialize, initializeParamsMap)
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	if err := validateInitializeResponse(initializeResult); err != nil {
		_ = c.Close()
		return nil, err
	}
	if err := c.notify(protocolv2.NewClientNotificationInitialized()); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func initializeParams(options ClientOptions) protocolv2.InitializeParams {
	return protocolv2.InitializeParams{
		Capabilities: protocolv2.Value(protocolv2.InitializeCapabilities{}),
		ClientInfo: protocolv2.ClientInfo{
			Name:    defaultClientName,
			Title:   protocolv2.Value(defaultClientTitle),
			Version: "codex-go-sdk-v1",
		},
	}
}

func validateInitializeResponse(result map[string]any) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("codexsdk: initialize response invalid: %w", err)
	}
	var response protocolv2.InitializeResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return fmt.Errorf("codexsdk: initialize response invalid: %w", err)
	}
	return nil
}

func validateOptions(options ClientOptions) (ClientOptions, error) {
	if strings.TrimSpace(options.CWD) == "" {
		return ClientOptions{}, errors.New("codexsdk: ClientOptions.CWD is required")
	}
	info, err := os.Stat(options.CWD)
	if err != nil {
		return ClientOptions{}, fmt.Errorf("codexsdk: invalid ClientOptions.CWD: %w", err)
	}
	if !info.IsDir() {
		return ClientOptions{}, fmt.Errorf("codexsdk: ClientOptions.CWD is not a directory: %s", options.CWD)
	}
	if len(options.Command) == 0 || strings.TrimSpace(options.Command[0]) == "" {
		return ClientOptions{}, errors.New("codexsdk: ClientOptions.Command is required")
	}
	options.Command = append([]string(nil), options.Command...)
	if reflect.DeepEqual(options.Initialize, protocolv2.InitializeParams{}) {
		options.Initialize = initializeParams(options)
	} else {
		cloned, err := cloneJSON(options.Initialize)
		if err != nil {
			return ClientOptions{}, fmt.Errorf("codexsdk: invalid ClientOptions.Initialize: %w", err)
		}
		options.Initialize = cloned
	}
	if strings.TrimSpace(options.Initialize.ClientInfo.Name) == "" || strings.TrimSpace(options.Initialize.ClientInfo.Version) == "" {
		return ClientOptions{}, errors.New("codexsdk: ClientOptions.Initialize.ClientInfo name and version are required")
	}
	if options.NotificationQueueCapacity < 0 {
		return ClientOptions{}, errors.New("codexsdk: ClientOptions.NotificationQueueCapacity must not be negative")
	}
	return options, nil
}

func (c *Client) Close() error {
	if c == nil || c.ctx == nil {
		return nil
	}
	c.shutdownOnce.Do(c.shutdown)
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	return c.failure
}

func (c *Client) checkOpen() error {
	if c == nil || c.ctx == nil {
		return ErrClientClosed
	}
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed {
		return ErrClientClosed
	}
	return nil
}

func (c *Client) start() error {
	command := c.options.Command
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = c.options.CWD
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	c.stdin = stdin
	c.stdout = stdout
	c.cmd = cmd
	go c.drainStderr(stderr)
	go c.readLoop(stdout)
	return nil
}

func (c *Client) call(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	return c.callValidated(ctx, method, params, nil)
}

func (c *Client) callValidated(ctx context.Context, method string, params map[string]any, validate func(map[string]any) error) (map[string]any, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	id := "go-sdk-" + strconv.FormatUint(c.nextID.Add(1), 10)
	ch := make(chan rpcResponse, 1)
	c.pending.Store(id, pendingCall{method: method, response: ch, validate: validate})
	payload := map[string]any{"id": id, "method": method}
	if params != nil {
		payload["params"] = params
	}
	if err := c.write(payload); err != nil {
		c.pending.Delete(id)
		return nil, err
	}
	select {
	case response := <-ch:
		return response.result, response.err
	case <-ctx.Done():
		c.pending.Delete(id)
		return nil, ctx.Err()
	}
}

func (c *Client) notify(payload any) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	return c.write(payload)
}

func (c *Client) write(payload any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if c.stdin == nil {
		return errors.New("codexsdk: app-server stdin is not available")
	}
	_, err = c.stdin.Write(append(raw, '\n'))
	return err
}

func (c *Client) readLoop(stdout io.Reader) {
	defer close(c.readerDone)
	reader := bufio.NewReader(stdout)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if c.isClosed() {
				c.failAll(ErrClientClosed)
				return
			}
			c.failClient(err)
			return
		}
		if err := validateJSONRPCEnvelope(line); err != nil {
			sum := sha256.Sum256(line)
			c.failClient(fmt.Errorf("codexsdk: invalid app-server JSON-RPC line bytes=%d sha256=%s: %w", len(line), hex.EncodeToString(sum[:]), err))
			return
		}
		var message map[string]any
		if err := json.Unmarshal(line, &message); err != nil {
			sum := sha256.Sum256(line)
			c.failClient(fmt.Errorf("codexsdk: invalid app-server JSON-RPC line bytes=%d sha256=%s: %w", len(line), hex.EncodeToString(sum[:]), err))
			return
		}
		c.handleMessage(message)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if c.isClosed() {
				c.failAll(ErrClientClosed)
				return
			}
			c.failClient(err)
			return
		}
	}
	if c.isClosed() {
		c.failAll(ErrClientClosed)
		return
	}
	c.failClient(errors.New("codexsdk: app-server closed stdout"))
}

func (c *Client) handleMessage(message map[string]any) {
	_, hasID := message["id"]
	method, hasMethod := message["method"].(string)
	if hasID && hasMethod {
		c.handleServerRequest(message)
		return
	}
	if hasID {
		c.routeResponse(message)
		return
	}
	if hasMethod {
		params, _ := message["params"].(map[string]any)
		c.routeNotification(rpcNotification{method: method, params: params})
	}
}

func (c *Client) routeResponse(message map[string]any) {
	id := fmt.Sprint(message["id"])
	raw, ok := c.pending.LoadAndDelete(id)
	if !ok {
		return
	}
	pending := raw.(pendingCall)
	if rawError := message["error"]; rawError != nil {
		pending.response <- rpcResponse{err: protocolError(message["id"], pending.method, rawError)}
		return
	}
	result, _ := message["result"].(map[string]any)
	if result == nil {
		pending.response <- rpcResponse{err: fmt.Errorf("codexsdk: app-server %s response missing object result", pending.method)}
		return
	}
	if pending.validate != nil {
		if err := pending.validate(result); err != nil {
			pending.response <- rpcResponse{err: err}
			return
		}
	}
	pending.response <- rpcResponse{result: result}
}

func protocolError(id any, method string, rawError any) error {
	errorObject, _ := rawError.(map[string]any)
	code := int64(intFromAny(errorObject["code"]))
	message, _ := errorObject["message"].(string)
	var data *protocolv2.JSONValue
	if rawData, ok := errorObject["data"]; ok {
		encoded, err := json.Marshal(rawData)
		if err == nil {
			parsed, err := protocolv2.ParseJSONValue(encoded)
			if err == nil {
				data = &parsed
			}
		}
	}
	requestID := protocolv2.NewRequestIdString(fmt.Sprint(id))
	return &ProtocolError{
		RequestID: requestID,
		Method:    method,
		Code:      code,
		Message:   message,
		Data:      data,
		Err:       errors.New("codexsdk: protocol request failed"),
	}
}

func (c *Client) routeNotification(notification rpcNotification) {
	if c.isClosed() {
		return
	}
	typed, err := exactNotification(notification)
	if err != nil {
		c.routeExactNotificationError(notification, err)
		return
	}
	var terminalCompletions []func()
	var evidence *notificationEvidence
	defer func() {
		dispatched, err := c.enqueueNotification(typed, evidence, len(terminalCompletions) > 0)
		if err != nil {
			c.failClient(err)
			return
		}
		if dispatched != nil && len(terminalCompletions) > 0 {
			<-dispatched
		}
		for _, complete := range terminalCompletions {
			complete()
		}
	}()
	var routed bool
	routed, terminalCompletions, evidence = c.routeExactNotificationBeforeTerminalCompletion(notification, typed)
	if routed {
		return
	}
}

func exactNotification(notification rpcNotification) (protocolv2.ServerNotification, error) {
	raw, err := json.Marshal(map[string]any{"method": notification.method, "params": notification.params})
	if err != nil {
		return protocolv2.ServerNotification{}, fmt.Errorf("codexsdk: decode %s notification: %w", notification.method, err)
	}
	var typed protocolv2.ServerNotification
	if err := json.Unmarshal(raw, &typed); err != nil {
		return protocolv2.ServerNotification{}, fmt.Errorf("codexsdk: decode %s notification: %w", notification.method, err)
	}
	return typed, nil
}

func (c *Client) routeExactNotification(notification rpcNotification, typed protocolv2.ServerNotification) bool {
	routed, completions, _ := c.routeExactNotificationBeforeTerminalCompletion(notification, typed)
	for _, complete := range completions {
		complete()
	}
	return routed
}

func (c *Client) routeExactNotificationBeforeTerminalCompletion(notification rpcNotification, typed protocolv2.ServerNotification) (bool, []func(), *notificationEvidence) {
	class, identity := attributionFor(typed)
	if class == notificationAttributionGlobal || class == notificationAttributionUnsupported {
		return false, nil, nil
	}
	c.turnMu.Lock()
	var targets []*exactRunState
	var pendingExact []*exactRunState
	switch class {
	case notificationAttributionTurn:
		if identity.turnID == "" {
			c.turnMu.Unlock()
			return false, nil, nil
		}
		for stream := range c.exactStreams[identity.turnID] {
			targets = append(targets, stream)
		}
		for stream := range c.exactAttaching[identity.threadID] {
			turnID := stream.turnIDSnapshot()
			if turnID == identity.turnID {
				targets = append(targets, stream)
			} else if turnID == "" {
				pendingExact = append(pendingExact, stream)
			}
		}
	case notificationAttributionThread:
		if identity.threadID == "" {
			c.turnMu.Unlock()
			return false, nil, nil
		}
		for _, streams := range c.exactStreams {
			for stream := range streams {
				if stream.threadID == identity.threadID {
					targets = append(targets, stream)
				}
			}
		}
		for candidateThreadID, streams := range c.exactAttaching {
			for stream := range streams {
				if candidateThreadID == identity.threadID {
					targets = append(targets, stream)
				}
			}
		}
	}
	if len(targets) == 0 && len(pendingExact) == 1 {
		evidence := &notificationEvidence{ready: make(chan struct{}), state: pendingExact[0]}
		if c.options.ServerNotificationHandler != nil {
			evidence.dispatched = make(chan struct{})
		}
		notification.evidence = evidence
		c.pendingEvents[identity.turnID] = append(c.pendingEvents[identity.turnID], notification)
		c.turnMu.Unlock()
		if c.testPendingExactNotification != nil {
			c.testPendingExactNotification(notification)
		}
		return true, nil, evidence
	}
	c.turnMu.Unlock()
	var deliveryErr error
	var failedStream *exactRunState
	var terminalCompletions []func()
	for _, stream := range targets {
		completion, err := stream.acceptOrderedBeforeTerminalCompletion(typed)
		if completion != nil {
			terminalCompletions = append(terminalCompletions, completion)
		}
		if err != nil {
			if deliveryErr == nil {
				deliveryErr = err
				failedStream = stream
			}
		}
	}
	if deliveryErr != nil {
		c.failExactNotificationDelivery(failedStream, deliveryErr)
	}
	return len(targets) > 0, terminalCompletions, nil
}

func (c *Client) failExactNotificationDelivery(stream *exactRunState, err error) {
	if errors.Is(err, ErrNotificationBackpressure) {
		err = fmt.Errorf("%w: turn_id=%s", ErrNotificationBackpressure, stream.turnIDSnapshot())
	}
	c.failClient(err)
}

func (c *Client) routeExactNotificationError(notification rpcNotification, err error) {
	raw, _ := json.Marshal(notification.params)
	sum := sha256.Sum256(raw)
	ref := DiagnosticRef{Kind: "notification_decode_error", ID: turnIDFromNotification(notification.method, notification.params), Path: notification.method, SizeBytes: int64(len(raw)), SHA256: hex.EncodeToString(sum[:])}
	turnID := turnIDFromNotification(notification.method, notification.params)
	threadID := threadIDFromNotification(notification.params)
	c.turnMu.Lock()
	var targets []*exactRunState
	for candidateTurnID, streams := range c.exactStreams {
		for stream := range streams {
			if (turnID != "" && candidateTurnID == turnID) || (turnID == "" && (threadID == "" || stream.threadID == threadID)) {
				targets = append(targets, stream)
			}
		}
	}
	for candidateThreadID, streams := range c.exactAttaching {
		for stream := range streams {
			if threadID == "" || candidateThreadID == threadID {
				targets = append(targets, stream)
			}
		}
	}
	if len(targets) == 0 && turnID != "" {
		c.pendingDiagnostics[turnID] = append(c.pendingDiagnostics[turnID], ref)
	}
	c.turnMu.Unlock()
	for _, stream := range targets {
		stream.addDiagnosticOrdered(ref)
	}
	c.failClient(err)
}

func (c *Client) attachExactStream(stream *exactRunState) {
	c.turnMu.Lock()
	if c.testBeforeExactStreamOrderGate != nil {
		c.testBeforeExactStreamOrderGate()
	}
	stream.notificationOrderMu.Lock()
	c.attachExactStreamLocked(stream)
}

// attachExactStreamLocked requires turnMu and the stream notification order
// mutex. It releases both before returning.
func (c *Client) attachExactStreamLocked(stream *exactRunState) {
	stream.mu.Lock()
	terminal := stream.terminal
	stream.mu.Unlock()
	turnID := stream.turnIDSnapshot()
	delete(c.exactAttaching[stream.threadID], stream)
	if len(c.exactAttaching[stream.threadID]) == 0 {
		delete(c.exactAttaching, stream.threadID)
	}
	if terminal {
		c.turnMu.Unlock()
		stream.notificationOrderMu.Unlock()
		return
	}
	if c.exactStreams[turnID] == nil {
		c.exactStreams[turnID] = map[*exactRunState]struct{}{}
	}
	c.exactStreams[turnID][stream] = struct{}{}
	pending := append([]rpcNotification(nil), c.pendingEvents[turnID]...)
	delete(c.pendingEvents, turnID)
	if len(pending) > 0 {
		if c.replayingEvents == nil {
			c.replayingEvents = map[*exactRunState][]rpcNotification{}
		}
		c.replayingEvents[stream] = pending
	}
	diagnostics := append([]DiagnosticRef(nil), c.pendingDiagnostics[turnID]...)
	delete(c.pendingDiagnostics, turnID)
	globalErr := c.pendingGlobal
	c.pendingGlobal = nil
	c.turnMu.Unlock()
	if c.testAfterExactStreamPublished != nil {
		c.testAfterExactStreamPublished()
	}
	for _, notification := range pending {
		var err error
		if notification.evidence == nil {
			var typed protocolv2.ServerNotification
			typed, err = exactNotification(notification)
			if err == nil {
				err = stream.accept(typed)
			}
		} else {
			err = notification.resolveEvidence(func(typed protocolv2.ServerNotification) (func(), error) {
				return stream.acceptStateBeforeTerminalCompletion(typed)
			})
			if notification.evidenceCompletion() != nil {
				if c.testBeforePendingTerminalFence != nil {
					c.testBeforePendingTerminalFence()
				}
				notification.awaitEvidenceHandler()
				notification.completeEvidenceTerminal()
			}
		}
		if err != nil {
			c.finishExactPendingReplay(stream)
			stream.notificationOrderMu.Unlock()
			c.failExactNotificationDelivery(stream, err)
			return
		}
	}
	c.finishExactPendingReplay(stream)
	for _, diagnostic := range diagnostics {
		stream.addDiagnostic(diagnostic)
	}
	if globalErr != nil {
		stream.notificationOrderMu.Unlock()
		c.failClient(globalErr)
		return
	}
	stream.notificationOrderMu.Unlock()
}

func (c *Client) finishExactPendingReplay(stream *exactRunState) {
	c.turnMu.Lock()
	delete(c.replayingEvents, stream)
	c.turnMu.Unlock()
}

func (n rpcNotification) resolveEvidence(accept func(protocolv2.ServerNotification) (func(), error)) error {
	if n.evidence == nil {
		return nil
	}
	n.evidence.once.Do(func() {
		typed, err := exactNotification(n)
		if err == nil {
			n.evidence.completion, err = accept(typed)
		}
		n.evidence.err = err
		close(n.evidence.ready)
	})
	return n.evidence.err
}

func (n rpcNotification) resolvePendingEvidence() error {
	if n.evidence == nil || n.evidence.state == nil {
		return nil
	}
	return n.resolveEvidence(n.evidence.state.acceptOrderedBeforeTerminalCompletion)
}

func (n rpcNotification) resolveReplayingEvidence() error {
	if n.evidence == nil || n.evidence.state == nil {
		return nil
	}
	return n.resolveEvidence(n.evidence.state.acceptStateBeforeTerminalCompletion)
}

func (n rpcNotification) awaitEvidenceHandler() {
	if n.evidence != nil && n.evidence.dispatched != nil {
		<-n.evidence.dispatched
	}
}

func (n rpcNotification) evidenceCompletion() func() {
	if n.evidence == nil {
		return nil
	}
	return n.evidence.completion
}

func (n rpcNotification) completeEvidenceTerminal() {
	if completion := n.evidenceCompletion(); completion != nil {
		completion()
	}
}

func (c *Client) registerAttachingExactStream(stream *exactRunState) {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()
	if c.exactAttaching[stream.threadID] == nil {
		c.exactAttaching[stream.threadID] = map[*exactRunState]struct{}{}
	}
	c.exactAttaching[stream.threadID][stream] = struct{}{}
}

func (c *Client) unregisterAttachingExactStream(stream *exactRunState) {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()
	delete(c.exactAttaching[stream.threadID], stream)
	if len(c.exactAttaching[stream.threadID]) == 0 {
		delete(c.exactAttaching, stream.threadID)
	}
}

func (c *Client) hasExactRun(threadID, turnID string) bool {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()
	if turnID != "" && len(c.exactStreams[turnID]) != 0 {
		return true
	}
	if threadID != "" && len(c.exactAttaching[threadID]) != 0 {
		return true
	}
	if threadID == "" && turnID == "" {
		return len(c.exactStreams) != 0 || len(c.exactAttaching) != 0
	}
	return false
}

func (c *Client) unregisterExactRun(stream *exactRunState) {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()
	delete(c.exactAttaching[stream.threadID], stream)
	if len(c.exactAttaching[stream.threadID]) == 0 {
		delete(c.exactAttaching, stream.threadID)
	}
	turnID := stream.turnIDSnapshot()
	delete(c.exactStreams[turnID], stream)
	if len(c.exactStreams[turnID]) == 0 {
		delete(c.exactStreams, turnID)
	}
}

func (c *Client) failAll(err error) {
	var pendingCalls []pendingCall
	c.pending.Range(func(key, value any) bool {
		c.pending.Delete(key)
		pendingCalls = append(pendingCalls, value.(pendingCall))
		return true
	})
	c.turnMu.Lock()
	var exactStreams []*exactRunState
	for _, byTurn := range c.exactStreams {
		for stream := range byTurn {
			exactStreams = append(exactStreams, stream)
		}
	}
	for _, byThread := range c.exactAttaching {
		for stream := range byThread {
			exactStreams = append(exactStreams, stream)
		}
	}
	var pendingEvidence []rpcNotification
	for _, notifications := range c.pendingEvents {
		for _, notification := range notifications {
			if notification.evidence != nil {
				pendingEvidence = append(pendingEvidence, notification)
			}
		}
	}
	var replayingEvidence []rpcNotification
	for _, notifications := range c.replayingEvents {
		for _, notification := range notifications {
			if notification.evidence != nil {
				replayingEvidence = append(replayingEvidence, notification)
			}
		}
	}
	c.turnMu.Unlock()
	for _, notification := range pendingEvidence {
		_ = notification.resolvePendingEvidence()
	}
	for _, notification := range replayingEvidence {
		_ = notification.resolveReplayingEvidence()
	}
	for _, call := range pendingCalls {
		call.response <- rpcResponse{err: err}
	}
	for _, stream := range exactStreams {
		stream.finish(err)
	}
}

func (c *Client) bestEffortInterrupt(threadID, turnID string) {
	go func() {
		parent := c.ctx
		if parent == nil {
			parent = context.Background()
		}
		ctx, cancel := context.WithTimeout(parent, time.Second)
		defer cancel()
		var response protocolv2.TurnInterruptResponse
		_ = c.callProtocol(ctx, protocolv2.MethodTurnInterrupt, protocolv2.TurnInterruptParams{
			ThreadID: threadID,
			TurnID:   turnID,
		}, &response)
	}()
}

func (c *Client) isClosed() bool {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	return c.closed
}

func (c *Client) drainStderr(stderr io.Reader) {
	_, _ = io.Copy(io.Discard, stderr)
}

func turnIDFromNotification(method string, params map[string]any) string {
	if params == nil {
		return ""
	}
	if turnID, _ := params["turnId"].(string); turnID != "" {
		return turnID
	}
	if turn, _ := params["turn"].(map[string]any); turn != nil {
		if turnID, _ := turn["id"].(string); turnID != "" {
			return turnID
		}
	}
	if method == "turn/completed" {
		return stringAt(params, "turn", "id")
	}
	if method == "error" {
		return stringAt(params, "error", "turnId")
	}
	return ""
}

func threadIDFromNotification(params map[string]any) string {
	if params == nil {
		return ""
	}
	return identityString(params, "threadId", "thread_id")
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func stringAt(value map[string]any, path ...string) string {
	current := any(value)
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = object[key]
	}
	result, _ := current.(string)
	return result
}

func identityString(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := obj[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	default:
		return 0
	}
}
