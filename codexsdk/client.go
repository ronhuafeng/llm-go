package codexsdk

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/wirejson"
	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

const (
	defaultClientName           = "codex-go-sdk"
	defaultClientTitle          = "Codex Go SDK"
	maxInboundJSONRPCFrameBytes = 16 << 20
)

var (
	errInboundFrameTooLarge     = errors.New("codexsdk: app-server JSON-RPC frame too large")
	errInboundFrameUnterminated = errors.New("codexsdk: app-server JSON-RPC frame missing newline")
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

	closeMu             sync.Mutex
	closed              bool
	failure             error
	normalClosing       bool
	constructing        bool
	closeReason         error
	callbackFailure     error
	callbackWork        atomic.Int64
	handlerWG           sync.WaitGroup
	closeCauseOnce      sync.Once
	closeCausePublished chan struct{}

	shutdownOnce        sync.Once
	shutdownDone        chan struct{}
	handlersDone        chan struct{}
	processDone         chan struct{}
	startupDone         chan struct{}
	dispatchStop        chan struct{}
	dispatcherDone      chan struct{}
	notifications       chan acceptedNotification
	retention           *notificationRetention
	attachmentRetention map[string]*retentionLease

	writeMu sync.Mutex
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	cmd     *exec.Cmd

	nextID  atomic.Uint64
	pending map[string]pendingCall

	turnMu                     sync.Mutex
	exactStreams               map[string]map[*exactRunState]struct{}
	exactAttaching             map[string]map[*exactRunState]struct{}
	pendingEvents              map[string][]rpcNotification
	armedThreadAttach          map[string]struct{}
	armedTurnAttach            map[string]*exactRunState
	pendingThreadNotifications map[string][]protocolv2.ServerNotification
	pendingThreadRetention     map[string][]*retentionLease
	pendingDiagnosticRetention map[string][]*retentionLease
	// replayingEvents keeps accepted evidence visible to terminalization after
	// attachment removes it from pendingEvents and until replay commits it.
	replayingEvents    map[*exactRunState][]rpcNotification
	pendingDiagnostics map[string][]DiagnosticRef
	pendingGlobal      error

	// testAfterExactStreamPublished pauses the deterministic test seam after an
	// exact stream becomes live and before pending evidence is replayed.
	testAfterExactStreamPublished         func()
	testAfterExactTurnPublished           func()
	testBeforeExactStreamOrderGate        func()
	testBeforeExactTurnAttach             func()
	testPendingExactNotification          func(rpcNotification)
	testBeforePendingTerminalFence        func()
	testAfterCloseCausePublished          func()
	testAfterServerRequestFailureResponse func()
	testBeforePendingAdmission            func()
	testAfterThreadStartResponse          func()
	testBeforeEvidenceWait                func()

	readerDone chan struct{}

	runtimeAppServer     RuntimeAppServerObservation
	runtimeCompatibility RuntimeCompatibility
}

type rpcResponse struct {
	result map[string]any
	err    error
}

// exactCallIntent is supplied only by ThreadRunner. A thread response precedes
// run construction; a turn response already has its exact run owner.
type exactCallIntent struct {
	thread bool
	turn   *exactRunState
}

type pendingCall struct {
	exact    exactCallIntent
	method   string
	response chan rpcResponse
	validate func(map[string]any) error
}

type rpcNotification struct {
	envelope map[string]any
	method   string
	params   map[string]any
	evidence *notificationEvidence
	retained *retentionLease
}

type notificationEvidence struct {
	ready      chan struct{}
	dispatched chan struct{}
	state      *exactRunState
	once       sync.Once
	completion func()
	err        error
}

func (e *notificationEvidence) releaseReady() {
	if e == nil || e.ready == nil {
		return
	}
	e.once.Do(func() {
		close(e.ready)
	})
}

type acceptedNotification struct {
	raw                 *RawNotification
	notification        protocolv2.ServerNotification
	evidence            *notificationEvidence
	dispatched          chan struct{}
	retained            *retentionLease
	terminalCompletions []func()
}

// New uses a 15-second deadline for launch and initialize.
func New(options ClientOptions) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultStartupTimeout)
	defer cancel()
	return NewContext(ctx, options)
}

// NewContext bounds launch and initialize with ctx. Cancellation after successful
// construction does not close the returned client. Input is copied before launch.
func NewContext(ctx context.Context, options ClientOptions) (*Client, error) {
	if ctx == nil {
		return nil, errors.New("codexsdk: startup context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Copy caller memory synchronously. Validation (including filesystem/runtime
	// schema work), launch and initialize run under the caller's bounded wait.
	owned, err := copyStartupOptions(options)
	if err != nil {
		return nil, err
	}
	type outcome struct {
		client *Client
		err    error
	}
	ready := make(chan outcome, 1)
	go func() { c, err := newClientContext(ctx, owned); ready <- outcome{c, err} }()
	select {
	case result := <-ready:
		return result.client, result.err
	case <-ctx.Done():
		select {
		case result := <-ready:
			return result.client, result.err
		default:
		}
		return nil, ctx.Err()
	}
}

func copyStartupOptions(options ClientOptions) (ClientOptions, error) {
	options.Command = append([]string(nil), options.Command...)
	if options.Env != nil {
		owned := make([]string, len(options.Env))
		copy(owned, options.Env)
		options.Env = owned
	}
	if options.RawNotifications != nil {
		owned := *options.RawNotifications
		owned.Methods = append([]string(nil), owned.Methods...)
		options.RawNotifications = &owned
	}
	if !reflect.DeepEqual(options.Initialize, protocolv2.InitializeParams{}) {
		cloned, err := cloneJSON(options.Initialize)
		if err != nil {
			return ClientOptions{}, fmt.Errorf("codexsdk: invalid ClientOptions.Initialize: %w", err)
		}
		options.Initialize = cloned
	}
	return options, nil
}

func newClientContext(ctx context.Context, options ClientOptions) (*Client, error) {
	normalized, err := validateOptions(options)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	clientCtx, cancel := context.WithCancel(context.Background())
	handlerCtx, handlerCancel := context.WithCancel(clientCtx)
	c := &Client{
		options:                    normalized,
		startupDone:                make(chan struct{}),
		retention:                  &notificationRetention{limit: normalized.NotificationRetention},
		attachmentRetention:        map[string]*retentionLease{},
		ctx:                        clientCtx,
		cancel:                     cancel,
		handlerCtx:                 handlerCtx,
		handlerCancel:              handlerCancel,
		exactStreams:               map[string]map[*exactRunState]struct{}{},
		exactAttaching:             map[string]map[*exactRunState]struct{}{},
		pendingEvents:              map[string][]rpcNotification{},
		armedThreadAttach:          map[string]struct{}{},
		armedTurnAttach:            map[string]*exactRunState{},
		pendingThreadNotifications: map[string][]protocolv2.ServerNotification{},
		pendingThreadRetention:     map[string][]*retentionLease{},
		pendingDiagnosticRetention: map[string][]*retentionLease{},
		pendingDiagnostics:         map[string][]DiagnosticRef{},
		pending:                    map[string]pendingCall{},
		readerDone:                 make(chan struct{}),
		dispatchStop:               make(chan struct{}),
		dispatcherDone:             make(chan struct{}),
		closeCausePublished:        make(chan struct{}),
	}
	queueCapacity := normalized.NotificationQueueCapacity
	if queueCapacity == 0 {
		queueCapacity = 64
	}
	c.notifications = make(chan acceptedNotification, queueCapacity)
	go c.notificationDispatcher()

	watchDone := make(chan struct{})
	c.constructing = true
	go func() {
		select {
		case <-watchDone:
		case <-ctx.Done():
			cancel, claimed := c.claimStartupFailure(ctx.Err())
			if claimed {
				if cancel != nil {
					cancel()
				}
				c.startClientFailureTeardown()
				c.publishClaimedClientFailure(ctx.Err())
			}
		}
	}()
	err = c.initialize(ctx, normalized)
	if err != nil {
		c.failClient(err)
	}
	c.closeMu.Lock()
	c.constructing = false
	if c.closeReason != nil {
		err = c.closeReason
	}
	c.closeMu.Unlock()
	close(watchDone)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Client) initialize(ctx context.Context, normalized ClientOptions) error {
	if err := c.start(); err != nil {
		return err
	}
	initializeParamsMap, err := encodeProtocolParams(protocolv2.MethodInitialize, normalized.Initialize)
	if err != nil {
		return err
	}
	initializeResult, err := c.call(ctx, protocolv2.MethodInitialize, initializeParamsMap)
	if err != nil {
		return err
	}
	response, err := decodeInitializeResponse(initializeResult)
	if err != nil {
		return err
	}
	c.runtimeAppServer = ObserveRuntimeAppServer(response)
	c.runtimeCompatibility = RuntimeCompatibilityOf(response)
	return c.notify(protocolv2.NewClientNotificationInitialized())
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

func decodeInitializeResponse(result map[string]any) (protocolv2.InitializeResponse, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return protocolv2.InitializeResponse{}, fmt.Errorf("codexsdk: initialize response invalid: %w", err)
	}
	var response protocolv2.InitializeResponse
	if err := wirejson.Unmarshal(raw, &response, wirejson.ServerObservation); err != nil {
		return protocolv2.InitializeResponse{}, fmt.Errorf("codexsdk: initialize response invalid: %w", err)
	}
	return response, nil
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
	env, err := childEnvironment(options.Env)
	if err != nil {
		return ClientOptions{}, err
	}
	options.Env = env
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
	if options.NotificationRetention.MaxCount < 0 || options.NotificationRetention.MaxBytes < 0 {
		return ClientOptions{}, errors.New("codexsdk: invalid notification retention budget")
	}
	if options.NotificationRetention.MaxCount == 0 {
		options.NotificationRetention.MaxCount = DefaultNotificationRetentionCount
	}
	if options.NotificationRetention.MaxBytes == 0 {
		options.NotificationRetention.MaxBytes = DefaultNotificationRetentionBytes
	}
	return validateRawSubscription(options)
}

// Close joins admitted callback work within DefaultShutdownTimeout. Use
// CloseContext for a caller-selected deadline and explicit cleanup observations.
func (c *Client) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
	defer cancel()
	_, err := c.CloseContext(ctx)
	return err
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
	if c.startupDone != nil {
		defer close(c.startupDone)
	}
	command := c.options.Command
	cmd := exec.CommandContext(c.ctx, command[0], command[1:]...)
	// Keep the pre-launch context check, but let cleanupProcess own termination.
	// CommandContext's default Kill can destroy a launcher before it forwards
	// Interrupt and joins its native child. Cancellation still starts bounded
	// cleanup, including when it races Start; cleanup waits for startupDone.
	cmd.Cancel = nil
	cmd.Dir = c.options.CWD
	cmd.Env = c.options.Env
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
	return c.callValidated(ctx, method, params, nil, exactCallIntent{})
}

func (c *Client) callValidated(ctx context.Context, method string, params map[string]any, validate func(map[string]any) error, exact exactCallIntent) (map[string]any, error) {
	if c == nil || c.ctx == nil {
		return nil, ErrClientClosed
	}
	id := "go-sdk-" + strconv.FormatUint(c.nextID.Add(1), 10)
	ch := make(chan rpcResponse, 1)
	if c.testBeforePendingAdmission != nil {
		c.testBeforePendingAdmission()
	}
	if err := c.admitPending(id, pendingCall{method: method, response: ch, validate: validate, exact: exact}); err != nil {
		return nil, err
	}
	payload := map[string]any{"id": id, "method": method}
	if params != nil {
		payload["params"] = params
	}
	if err := c.write(payload); err != nil {
		if _, owned := c.takePending(id); owned {
			return nil, err
		}
		response := <-ch
		return response.result, response.err
	}
	select {
	case response := <-ch:
		return response.result, response.err
	case <-ctx.Done():
		if _, owned := c.takePending(id); owned {
			return nil, ctx.Err()
		}
		response := <-ch
		return response.result, response.err
	}
}

func (c *Client) admitPending(id string, call pendingCall) error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed {
		return ErrClientClosed
	}
	if c.pending == nil {
		c.pending = map[string]pendingCall{}
	}
	c.pending[id] = call
	return nil
}

func (c *Client) takePending(id string) (pendingCall, bool) {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	call, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	return call, ok
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
	c.readLoopWithLimit(stdout, maxInboundJSONRPCFrameBytes)
}

func (c *Client) readLoopWithLimit(stdout io.Reader, maxBytes int) {
	defer close(c.readerDone)
	reader := bufio.NewReader(stdout)
	for {
		line, err := readFrame(reader, maxBytes)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if c.isClosed() {
				c.awaitClosedReadLoopSettlement()
				return
			}
			c.failClient(err)
			return
		}
		if err := c.ingestJSONRPCLine(line); err != nil {
			sum := sha256.Sum256(line)
			c.failClient(fmt.Errorf("codexsdk: invalid app-server JSON-RPC line bytes=%d sha256=%s: %w", len(line), hex.EncodeToString(sum[:]), err))
			return
		}
	}
	if c.isClosed() {
		c.awaitClosedReadLoopSettlement()
		return
	}
	c.failClient(errors.New("codexsdk: app-server closed stdout"))
}

// A close/failure publisher owns the pending-prefix snapshot. Process teardown
// can close stdout before that publication finishes; a second failAll must not
// terminalize streams while the first publisher is still committing evidence.
func (c *Client) awaitClosedReadLoopSettlement() {
	if c.closeCausePublished != nil {
		c.awaitCloseCausePublication()
		return
	}
	c.failAll(ErrClientClosed)
}

func readFrame(reader *bufio.Reader, maxBytes int) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, errors.New("codexsdk: invalid inbound JSON-RPC frame limit")
	}
	capacity := min(maxBytes, 64<<10)
	frame := make([]byte, 0, capacity)
	digest := sha256.New()
	consumed := 0
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(fragment) > 0 {
			consumed += len(fragment)
			_, _ = digest.Write(fragment)
			if consumed > maxBytes {
				return nil, fmt.Errorf("%w: prefix_bytes=%d prefix_sha256=%s", errInboundFrameTooLarge, consumed, hex.EncodeToString(digest.Sum(nil)))
			}
			frame = append(frame, fragment...)
		}
		switch {
		case err == nil:
			return frame, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && consumed == 0:
			return nil, io.EOF
		case errors.Is(err, io.EOF):
			return nil, fmt.Errorf("%w: bytes=%d sha256=%s", errInboundFrameUnterminated, consumed, hex.EncodeToString(digest.Sum(nil)))
		default:
			return nil, fmt.Errorf("codexsdk: read app-server JSON-RPC frame bytes=%d sha256=%s: %w", consumed, hex.EncodeToString(digest.Sum(nil)), err)
		}
	}
}

func (c *Client) ingestJSONRPCLine(line []byte) error {
	if err := validateJSONRPCEnvelope(line); err != nil {
		return err
	}
	var message map[string]any
	if err := unmarshalJSONPreserveNumbers(line, &message); err != nil {
		return err
	}
	c.handleMessage(message)
	return nil
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
		c.routeNotification(rpcNotification{method: method, params: params, envelope: message})
	}
}

func (c *Client) routeResponse(message map[string]any) {
	id := fmt.Sprint(message["id"])
	pending, ok := c.takePending(id)
	if !ok {
		return
	}
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
	if pending.exact.thread && (pending.method == protocolv2.MethodThreadStart || pending.method == protocolv2.MethodThreadResume) {
		if threadID := threadIDFromProtocolResult(result); threadID != "" {
			if err := c.armThreadAttach(threadID); err != nil {
				pending.response <- rpcResponse{err: err}
				c.failClient(err)
				return
			}
		}
	}
	if pending.exact.turn != nil && pending.method == protocolv2.MethodTurnStart {
		if turnID := turnIDFromProtocolResult(result); turnID != "" {
			if err := c.armTurnAttach(turnID, pending.exact.turn); err != nil {
				pending.response <- rpcResponse{err: err}
				c.failClient(err)
				return
			}
		}
	}
	pending.response <- rpcResponse{result: result}
}

func threadIDFromProtocolResult(result map[string]any) string {
	thread, _ := result["thread"].(map[string]any)
	id, _ := thread["id"].(string)
	return id
}

func turnIDFromProtocolResult(result map[string]any) string {
	turn, _ := result["turn"].(map[string]any)
	id, _ := turn["id"].(string)
	return id
}

func (c *Client) armThreadAttach(threadID string) error {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()
	if err := c.retainAttachmentLocked("thread:" + threadID); err != nil {
		return err
	}
	if c.armedThreadAttach == nil {
		c.armedThreadAttach = map[string]struct{}{}
	}
	c.armedThreadAttach[threadID] = struct{}{}
	return nil
}
func (c *Client) armTurnAttach(turnID string, owner *exactRunState) error {
	if turnID == "" || owner == nil {
		return nil
	}
	c.turnMu.Lock()
	defer c.turnMu.Unlock()
	if err := c.retainAttachmentLocked("turn:" + turnID); err != nil {
		return err
	}
	if c.armedTurnAttach == nil {
		c.armedTurnAttach = map[string]*exactRunState{}
	}
	c.armedTurnAttach[turnID] = owner
	return nil
}
func (c *Client) retainAttachmentLocked(key string) error {
	if c.isClosed() {
		return ErrClientClosed
	}
	if _, exists := c.attachmentRetention[key]; exists {
		return nil
	}
	l, err := c.retention.reserve(1, 2*retainedValueBytes(key))
	if err != nil {
		return err
	}
	if c.attachmentRetention == nil {
		c.attachmentRetention = map[string]*retentionLease{}
	}
	c.attachmentRetention[key] = l
	return nil
}
func (c *Client) releaseAttachmentLocked(key string) {
	c.attachmentRetention[key].release()
	delete(c.attachmentRetention, key)
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
	var raw *RawNotification
	if c.requiresRawNotification(notification.method) {
		var err error
		raw, err = rawNotification(notification)
		if err != nil {
			c.failClient(err)
			return
		}
		if notification.method == RawResponseItemCompletedExtension {
			_, _ = c.enqueueAcceptedNotification(acceptedNotification{raw: raw}, false)
			return
		}
	}
	// The raw observer owns its JSON value; pending typed replay needs only params.
	notification.envelope = nil
	method, known := protocolv2.LookupMethod(notification.method)
	if !known || method.Direction != protocolv2.MethodDirectionServerToClient || method.Kind != protocolv2.MethodKindNotification {
		return
	}
	typed, err := exactNotification(notification)
	if err != nil {
		if raw != nil {
			err = ErrRawNotificationDecode
		}
		c.routeExactNotificationError(notification, err)
		return
	}
	var terminalCompletions []func()
	var evidence *notificationEvidence
	defer func() {
		accepted := acceptedNotification{notification: typed, evidence: evidence, raw: raw}
		if c.hasNotificationHandler() {
			// The serial dispatcher owns this terminal fence. The transport reader
			// must remain free to route RPC responses requested by callbacks.
			accepted.terminalCompletions = terminalCompletions
			terminalCompletions = nil
		}
		if _, err := c.enqueueAcceptedNotification(accepted, false); err != nil {
			c.failClient(err)
			return
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
	if err := wirejson.Unmarshal(raw, &typed, wirejson.ServerObservation); err != nil {
		return protocolv2.ServerNotification{}, fmt.Errorf("codexsdk: decode %s notification: %w", notification.method, err)
	}
	return typed, nil
}

func (c *Client) routeExactNotificationBeforeTerminalCompletion(notification rpcNotification, typed protocolv2.ServerNotification) (bool, []func(), *notificationEvidence) {
	class, identity := attributionFor(typed)
	if class == notificationAttributionGlobal || class == notificationAttributionUnsupported {
		return false, nil, nil
	}
	c.turnMu.Lock()
	if c.isClosed() {
		c.turnMu.Unlock()
		return false, nil, nil
	}
	var targets []*exactRunState
	switch class {
	case notificationAttributionTurn:
		if identity.turnID == "" {
			c.turnMu.Unlock()
			return false, nil, nil
		}
		for stream := range c.exactStreams[identity.turnID] {
			if identity.matchesRun(stream.threadID, stream.turnIDSnapshot()) {
				targets = append(targets, stream)
			}
		}
		for _, streams := range c.exactAttaching {
			for stream := range streams {
				turnID := stream.turnIDSnapshot()
				if turnID == identity.turnID && identity.matchesRun(stream.threadID, turnID) {
					targets = append(targets, stream)
				}
			}
		}
	case notificationAttributionThread:
		if identity.threadID == "" {
			c.turnMu.Unlock()
			return false, nil, nil
		}
		for _, streams := range c.exactStreams {
			for stream := range streams {
				if identity.matchesRun(stream.threadID, stream.turnIDSnapshot()) {
					targets = append(targets, stream)
				}
			}
		}
		for _, streams := range c.exactAttaching {
			for stream := range streams {
				if identity.matchesRun(stream.threadID, stream.turnIDSnapshot()) {
					targets = append(targets, stream)
				}
			}
		}
		if len(targets) == 0 {
			if _, armed := c.armedThreadAttach[identity.threadID]; armed {
				if c.pendingThreadNotifications == nil {
					c.pendingThreadNotifications = map[string][]protocolv2.ServerNotification{}
				}
				retained, err := c.retention.reserve(3, 3*retainedValueBytes(typed))
				if err != nil {
					c.turnMu.Unlock()
					c.failClient(err)
					return false, nil, nil
				}
				if c.pendingThreadRetention == nil {
					c.pendingThreadRetention = map[string][]*retentionLease{}
				}
				c.pendingThreadRetention[identity.threadID] = append(c.pendingThreadRetention[identity.threadID], retained)
				c.pendingThreadNotifications[identity.threadID] = append(c.pendingThreadNotifications[identity.threadID], typed)
			}
		}
	}
	if len(targets) == 0 {
		if owner := c.armedTurnAttach[identity.turnID]; owner != nil && identity.turnID != "" && identity.matchesRun(owner.threadID, owner.turnIDSnapshot()) {
			retained, err := c.retention.reserve(4, retainedValueBytes(notification.params)+3*retainedValueBytes(typed))
			if err != nil {
				c.turnMu.Unlock()
				c.failClient(err)
				return false, nil, nil
			}
			notification.retained = retained
			evidence := &notificationEvidence{ready: make(chan struct{}), state: owner}
			if c.hasNotificationHandler() {
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
	turnID := turnIDFromNotification(notification.params)
	ref := DiagnosticRef{Kind: "notification_decode_error", ID: turnID, Path: notification.method, SizeBytes: int64(len(raw)), SHA256: hex.EncodeToString(sum[:])}
	threadID := threadIDFromNotification(notification.params)
	c.turnMu.Lock()
	if c.isClosed() {
		c.turnMu.Unlock()
		return
	}
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
		l, budgetErr := c.retention.reserve(1, retainedValueBytes(ref))
		if budgetErr != nil {
			c.turnMu.Unlock()
			c.failClient(budgetErr)
			return
		}
		if c.pendingDiagnosticRetention == nil {
			c.pendingDiagnosticRetention = map[string][]*retentionLease{}
		}
		c.pendingDiagnosticRetention[turnID] = append(c.pendingDiagnosticRetention[turnID], l)
		c.pendingDiagnostics[turnID] = append(c.pendingDiagnostics[turnID], ref)
	}
	c.turnMu.Unlock()
	for _, stream := range targets {
		if budgetErr := stream.addDiagnosticOrdered(ref); budgetErr != nil {
			c.failClient(budgetErr)
			return
		}
	}
	c.failClient(err)
}

// attachExactStreamLocked requires turnMu and the stream notification order
// mutex. It releases both before returning.
func (c *Client) attachExactStreamLocked(stream *exactRunState) {
	stream.mu.Lock()
	terminal := stream.terminal
	stream.mu.Unlock()
	turnID := stream.turnIDSnapshot()
	delete(c.armedTurnAttach, turnID)
	c.releaseAttachmentLocked("turn:" + turnID)
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
	diagnosticRetention := c.pendingDiagnosticRetention[turnID]
	delete(c.pendingDiagnosticRetention, turnID)
	defer func() {
		for _, l := range diagnosticRetention {
			l.release()
		}
	}()
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
				var complete func()
				complete, err = stream.acceptRetainedBeforeTerminalCompletion(typed, notification.retained)
				if complete != nil {
					complete()
				}
			}
		} else {
			err = notification.resolveEvidence(func(typed protocolv2.ServerNotification) (func(), error) {
				return stream.acceptRetainedBeforeTerminalCompletion(typed, notification.retained)
			})
			if notification.evidenceCompletion() != nil {
				if c.testBeforePendingTerminalFence != nil {
					c.testBeforePendingTerminalFence()
				}
				// With callbacks, the dispatcher completes after observing ready
				// and delivering this frame. Replay must not wait under the order
				// gate, which the reader may need before an RPC response.
				if !c.hasNotificationHandler() {
					notification.completeEvidenceTerminal()
				}
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
	for i, diagnostic := range diagnostics {
		var l *retentionLease
		if i < len(diagnosticRetention) {
			l = diagnosticRetention[i]
		}
		if err := stream.addDiagnosticRetained(diagnostic, l); err != nil {
			stream.notificationOrderMu.Unlock()
			c.failClient(err)
			return
		}
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
	pending := c.replayingEvents[stream]
	delete(c.replayingEvents, stream)
	c.turnMu.Unlock()
	for _, n := range pending {
		n.retained.release()
	}
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
	// Failure snapshots and attachment replay traverse the same pending prefix
	// in order. The evidence Once serializes each commit; the state mutex protects
	// its contents. Do not acquire notificationOrderMu inside Once: attachment
	// holds that mutex while replaying the same evidence.
	return n.resolveEvidence(func(typed protocolv2.ServerNotification) (func(), error) {
		return n.evidence.state.acceptRetainedBeforeTerminalCompletion(typed, n.retained)
	})
}

func (n rpcNotification) resolveReplayingEvidence() error {
	if n.evidence == nil || n.evidence.state == nil {
		return nil
	}
	return n.resolveEvidence(func(typed protocolv2.ServerNotification) (func(), error) {
		return n.evidence.state.acceptRetainedBeforeTerminalCompletion(typed, n.retained)
	})
}

func (c *Client) awaitDispatch(dispatched <-chan struct{}) {
	select {
	case <-dispatched:
	case <-c.closeCausePublished:
	}
}
func (c *Client) awaitEvidenceHandler(n rpcNotification) {
	if n.evidence != nil && n.evidence.dispatched != nil {
		c.awaitDispatch(n.evidence.dispatched)
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

func (c *Client) registerAttachingExactStream(stream *exactRunState) error {
	c.turnMu.Lock()
	if c.isClosed() {
		c.turnMu.Unlock()
		return ErrClientClosed
	}
	if len(c.exactAttaching[stream.threadID]) != 0 {
		c.turnMu.Unlock()
		return fmt.Errorf("%w: thread_id=%s", errConcurrentTurnStart, stream.threadID)
	}
	if c.exactAttaching[stream.threadID] == nil {
		c.exactAttaching[stream.threadID] = map[*exactRunState]struct{}{}
	}
	c.exactAttaching[stream.threadID][stream] = struct{}{}
	pending := append([]protocolv2.ServerNotification(nil), c.pendingThreadNotifications[stream.threadID]...)
	retained := c.pendingThreadRetention[stream.threadID]
	delete(c.pendingThreadRetention, stream.threadID)
	delete(c.pendingThreadNotifications, stream.threadID)
	delete(c.armedThreadAttach, stream.threadID)
	c.releaseAttachmentLocked("thread:" + stream.threadID)
	c.turnMu.Unlock()
	defer func() {
		for _, l := range retained {
			l.release()
		}
	}()
	for i, typed := range pending {
		var lease *retentionLease
		if i < len(retained) {
			lease = retained[i]
		}
		complete, err := stream.acceptRetainedBeforeTerminalCompletion(typed, lease)
		if err != nil {
			c.failClient(err)
			return err
		}
		if complete != nil {
			complete()
		}
	}
	return nil
}

func (c *Client) finishAttachingExactStream(stream *exactRunState, finishErr error) {
	stream.notificationOrderMu.Lock()
	c.turnMu.Lock()
	delete(c.exactAttaching[stream.threadID], stream)
	if len(c.exactAttaching[stream.threadID]) == 0 {
		delete(c.exactAttaching, stream.threadID)
	}
	for turnID, owner := range c.armedTurnAttach {
		if owner == stream {
			delete(c.armedTurnAttach, turnID)
			c.releaseAttachmentLocked("turn:" + turnID)
		}
	}
	var pending []rpcNotification
	for turnID, notifications := range c.pendingEvents {
		kept := notifications[:0]
		for _, notification := range notifications {
			if notification.evidence != nil && notification.evidence.state == stream {
				pending = append(pending, notification)
				continue
			}
			kept = append(kept, notification)
		}
		clear(notifications[len(kept):])
		if len(kept) == 0 {
			delete(c.pendingEvents, turnID)
		} else {
			c.pendingEvents[turnID] = kept
		}
	}
	c.turnMu.Unlock()

	defer func() {
		for _, n := range pending {
			n.retained.release()
		}
	}()
	var deliveryErr error
	for _, notification := range pending {
		err := notification.resolveEvidence(func(typed protocolv2.ServerNotification) (func(), error) {
			return stream.acceptRetainedBeforeTerminalCompletion(typed, notification.retained)
		})
		if deliveryErr == nil && err != nil {
			deliveryErr = err
		}
	}
	stream.notificationOrderMu.Unlock()
	if deliveryErr != nil {
		stream.finish(deliveryErr)
		c.failExactNotificationDelivery(stream, deliveryErr)
		return
	}
	for _, notification := range pending {
		if notification.evidenceCompletion() != nil {
			c.awaitEvidenceHandler(notification)
			notification.completeEvidenceTerminal()
		}
	}
	stream.finish(finishErr)
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
	err = c.firstCloseReason(err)
	c.closeMu.Lock()
	pendingCalls := make([]pendingCall, 0, len(c.pending))
	for id, call := range c.pending {
		delete(c.pending, id)
		pendingCalls = append(pendingCalls, call)
	}
	c.closeMu.Unlock()
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
	c.pendingEvents = map[string][]rpcNotification{}
	for _, leases := range c.pendingThreadRetention {
		for _, l := range leases {
			l.release()
		}
	}
	c.pendingThreadNotifications = map[string][]protocolv2.ServerNotification{}
	c.pendingThreadRetention = map[string][]*retentionLease{}
	for _, l := range c.attachmentRetention {
		l.release()
	}
	c.attachmentRetention = map[string]*retentionLease{}
	c.armedThreadAttach = map[string]struct{}{}
	c.armedTurnAttach = map[string]*exactRunState{}
	diagnostics := c.pendingDiagnostics
	diagnosticLeases := c.pendingDiagnosticRetention
	c.pendingDiagnostics = map[string][]DiagnosticRef{}
	c.pendingDiagnosticRetention = map[string][]*retentionLease{}
	var replayingEvidence []rpcNotification
	for _, notifications := range c.replayingEvents {
		for _, notification := range notifications {
			if notification.evidence != nil {
				notification.retained.retain()
				replayingEvidence = append(replayingEvidence, notification)
			}
		}
	}
	c.turnMu.Unlock()
	for _, notification := range pendingEvidence {
		_ = notification.resolvePendingEvidence()
		notification.retained.release()
	}
	for _, notification := range replayingEvidence {
		_ = notification.resolveReplayingEvidence()
		notification.retained.release()
	}
	for turnID, refs := range diagnostics {
		for i, ref := range refs {
			var l *retentionLease
			if i < len(diagnosticLeases[turnID]) {
				l = diagnosticLeases[turnID][i]
			}
			for _, stream := range exactStreams {
				if stream.turnIDSnapshot() == turnID {
					_ = stream.addDiagnosticRetained(ref, l)
				}
			}
			l.release()
		}
	}
	for _, stream := range exactStreams {
		stream.finish(err)
	}
	for _, call := range pendingCalls {
		call.response <- rpcResponse{err: err}
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

func turnIDFromNotification(params map[string]any) string {
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
	return ""
}

func threadIDFromNotification(params map[string]any) string {
	if params == nil {
		return ""
	}
	threadID, _ := params["threadId"].(string)
	return threadID
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
