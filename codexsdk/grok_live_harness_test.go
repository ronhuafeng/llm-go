package codexsdk_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk"
	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

const (
	grokLiveEnv       = "GROK_LIVE"
	grokLiveBinEnv    = "GROK_LIVE_CODEX_BIN"
	grokLiveConfigEnv = "GROK_LIVE_CONFIG"

	grokProvider = "grok"
	grokModel    = "grok-4.6"

	probeToolName   = "grok_live_probe"
	probeToolOutput = "GROK_LIVE_TOOL_OK"
	probeToolDesc   = "Return the fixed live validation marker."

	applyPatchFile     = "hello.txt"
	applyPatchSeed     = "HELLO\n"
	applyPatchExpected = "WORLD"

	notificationQueueCapacity = 1 << 16
	rolloutSettle             = 15 * time.Second
)

var (
	uuidShaped = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	codeFence  = regexp.MustCompile("^`{1,3}(?:[a-zA-Z]*\n)?|`{1,3}$")
)

type liveTurn struct {
	ThreadID      string
	TurnID        string
	Status        string
	FinalResponse string
	Items         []protocolv2.ThreadItem
	Provider      string
	Model         string
	DeadlineHit   bool
}

func (run liveTurn) completed() bool {
	return !run.DeadlineHit && run.Status == string(protocolv2.TurnStatusCompleted)
}

func (run liveTurn) reply() string {
	if text := strings.TrimSpace(run.FinalResponse); text != "" {
		return text
	}
	return lastAgentMessage(run.Items)
}

type liveHarness struct {
	t         *testing.T
	home      string
	workspace string
	client    *codexsdk.Client
	requests  *liveServerRequests
}

type liveOptions struct {
	probeTool    bool
	disableShell bool
}

func skipUnlessGrokLive(t *testing.T) {
	t.Helper()
	if os.Getenv(grokLiveEnv) != "1" {
		t.Skip("set GROK_LIVE=1, GROK_LIVE_CODEX_BIN, and GROK_LIVE_CONFIG to run Grok real-provider Live tests")
	}
}

func startGrokLive(t *testing.T, opts liveOptions) *liveHarness {
	t.Helper()
	skipUnlessGrokLive(t)

	binary := strings.TrimSpace(os.Getenv(grokLiveBinEnv))
	configPath := strings.TrimSpace(os.Getenv(grokLiveConfigEnv))
	if binary == "" || configPath == "" {
		t.Fatal("GROK_LIVE_CODEX_BIN and GROK_LIVE_CONFIG are required when GROK_LIVE=1")
	}
	if _, err := os.Stat(binary); err != nil {
		if _, pathErr := exec.LookPath(binary); pathErr != nil {
			t.Fatalf("codex binary is unavailable")
		}
	}

	home := t.TempDir()
	workspace := t.TempDir()
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read Grok profile config: %v", err)
	}
	if opts.disableShell {
		config = ensureShellToolDisabled(config)
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), config, 0o600); err != nil {
		t.Fatalf("write isolated config: %v", err)
	}
	t.Setenv("CODEX_HOME", home)
	t.Setenv("NO_COLOR", "1")

	requests := newLiveServerRequests()
	if opts.probeTool {
		requests.toolName = probeToolName
		requests.toolOutput = probeToolOutput
	}
	experimental := true
	client, err := codexsdk.New(codexsdk.ClientOptions{
		CWD:                       workspace,
		Command:                   []string{binary, "app-server", "--strict-config", "--listen", "stdio://"},
		ServerRequestHandler:      requests.handler,
		NotificationQueueCapacity: notificationQueueCapacity,
		Initialize: protocolv2.InitializeParams{
			ClientInfo: protocolv2.ClientInfo{Name: "llm-go-grok-live", Version: "test"},
			Capabilities: protocolv2.Value(protocolv2.InitializeCapabilities{
				ExperimentalAPI: &experimental,
			}),
		},
	})
	if err != nil {
		t.Fatalf("start App Server: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return &liveHarness{t: t, home: home, workspace: workspace, client: client, requests: requests}
}

func ensureShellToolDisabled(config []byte) []byte {
	if bytes.Contains(config, []byte("shell_tool")) {
		return config
	}
	extra := []byte("\n[features]\nshell_tool = false\n")
	if bytes.Contains(config, []byte("[features]")) {
		extra = []byte("\nshell_tool = false\n")
	}
	out := make([]byte, len(config)+len(extra))
	copy(out, config)
	copy(out[len(config):], extra)
	return out
}

func (h *liveHarness) requireGrokCatalog(ctx context.Context) {
	h.t.Helper()
	listed, err := h.client.Models().List(ctx, protocolv2.ModelListParams{})
	if err != nil {
		h.failStage("catalog_listed", "model/list failed")
	}
	for _, model := range listed.Data {
		if model.ID != grokModel {
			continue
		}
		if model.MultiAgentVersion == nil || model.MultiAgentVersion.Value == nil || *model.MultiAgentVersion.Value != protocolv2.MultiAgentVersionV2 {
			h.failStage("catalog_multi_agent_metadata", "grok-4.6 is missing Multi-Agent V2 metadata")
		}
		hasUltra := false
		for _, option := range model.SupportedReasoningEfforts {
			if string(option.ReasoningEffort) == "ultra" {
				hasUltra = true
				break
			}
		}
		if !hasUltra {
			h.failStage("catalog_ultra_metadata", "grok-4.6 is missing Ultra reasoning metadata")
		}
		return
	}
	h.failStage("catalog_lists_grok_4_6", "model/list does not list grok-4.6")
}

type startTurnOpts struct {
	prompt        string
	deadline      time.Duration
	effort        string
	probeTool     bool
	approvalNever bool
	dangerFull    bool
	disableShell  bool
	threadID      string
}

func (h *liveHarness) runTurn(ctx context.Context, opts startTurnOpts) liveTurn {
	h.t.Helper()
	if opts.deadline <= 0 {
		opts.deadline = 2 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, opts.deadline)
	defer cancel()

	turn := protocolv2.TurnStartParams{
		Input: []protocolv2.UserInput{
			protocolv2.NewUserInputText(protocolv2.UserInputText{Text: opts.prompt}),
		},
	}
	if opts.effort != "" {
		turn.Effort = protocolv2.Value(protocolv2.ReasoningEffort(opts.effort))
	}

	var (
		threadID string
		provider string
		model    string
		waitErr  error
		result   codexsdk.ThreadRunResult
	)
	if opts.threadID == "" {
		thread := protocolv2.ThreadStartParams{
			CWD:           protocolv2.Value(h.workspace),
			Model:         protocolv2.Value(grokModel),
			ModelProvider: protocolv2.Value(grokProvider),
			Ephemeral:     protocolv2.Value(false),
		}
		if opts.approvalNever {
			thread.ApprovalPolicy = protocolv2.Value(protocolv2.NewAskForApprovalNever())
		}
		if opts.dangerFull {
			thread.Sandbox = protocolv2.Value(protocolv2.SandboxModeDangerFullAccess)
		}
		if opts.disableShell {
			thread.Config = protocolv2.Value(map[string]protocolv2.JSONValue{
				"features": protocolv2.JSONObject(map[string]protocolv2.JSONValue{
					"shell_tool": protocolv2.JSONBool(false),
				}),
			})
		}
		if opts.probeTool {
			thread.DynamicTools = protocolv2.Value([]protocolv2.DynamicToolSpec{probeToolSpec()})
		}
		stream, err := h.client.ThreadRunner().StartStream(ctx, codexsdk.StartThreadRunRequest{
			Thread: thread,
			Turn:   turn,
		})
		if err != nil {
			h.failStage("thread_started", "thread/start failed")
		}
		started, err := stream.Wait(runCtx)
		waitErr = err
		result = started.Run
		threadID = started.Start.Thread.ID
		provider = started.Start.ModelProvider
		model = started.Start.Model
	} else {
		stream, err := h.client.ThreadRunner().ResumeStream(ctx, codexsdk.ResumeThreadRunRequest{
			Thread: protocolv2.ThreadResumeParams{ThreadID: opts.threadID},
			Turn:   turn,
		})
		if err != nil {
			h.failStage("thread_resumed", "thread/resume failed")
		}
		resumed, err := stream.Wait(runCtx)
		waitErr = err
		result = resumed.Run
		threadID = opts.threadID
		provider = resumed.Resume.ModelProvider
		model = resumed.Resume.Model
	}

	run := liveTurn{
		ThreadID:      threadID,
		TurnID:        result.Turn.ID,
		Status:        string(result.Turn.Status),
		FinalResponse: result.FinalResponse,
		Items:         result.Turn.Items,
		Provider:      provider,
		Model:         model,
		DeadlineHit:   errors.Is(waitErr, context.DeadlineExceeded),
	}
	if run.DeadlineHit && threadID != "" && run.TurnID != "" {
		interruptCtx, interruptCancel := context.WithTimeout(ctx, 20*time.Second)
		defer interruptCancel()
		_, _ = h.client.Turns().Interrupt(interruptCtx, protocolv2.TurnInterruptParams{
			ThreadID: threadID,
			TurnID:   run.TurnID,
		})
	}
	if waitErr != nil && !run.DeadlineHit && !completedWithoutFinalAnswer(waitErr, run.Status) {
		h.failStage("turn_wait", "App Server wait failed before a terminal result")
	}
	if strings.TrimSpace(run.FinalResponse) == "" {
		run.FinalResponse = lastAgentMessage(run.Items)
	}
	return run
}

func (h *liveHarness) failStage(stage, reason string) {
	h.t.Helper()
	h.t.Fatalf("NOT_PROVEN at %s: %s", stage, reason)
}

func completedWithoutFinalAnswer(err error, status string) bool {
	return err != nil &&
		status == string(protocolv2.TurnStatusCompleted) &&
		strings.Contains(err.Error(), "without final_answer agent message")
}

func lastAgentMessage(items []protocolv2.ThreadItem) string {
	for index := len(items) - 1; index >= 0; index-- {
		if message, ok := items[index].AsAgentMessage(); ok && strings.TrimSpace(message.Text) != "" {
			return strings.TrimSpace(message.Text)
		}
	}
	return ""
}

func probeToolSpec() protocolv2.DynamicToolSpec {
	return protocolv2.NewDynamicToolSpecFunction(protocolv2.DynamicToolSpecFunction{
		Name:        probeToolName,
		Description: probeToolDesc,
		InputSchema: protocolv2.JSONObject(map[string]protocolv2.JSONValue{
			"type":                 protocolv2.JSONString("object"),
			"properties":           protocolv2.JSONObject(map[string]protocolv2.JSONValue{}),
			"additionalProperties": protocolv2.JSONBool(false),
		}),
	})
}

func nonceOf(reply string) string {
	trimmed := strings.TrimSpace(reply)
	return strings.TrimSpace(codeFence.ReplaceAllString(trimmed, ""))
}

func hasCompletedProbe(items []protocolv2.ThreadItem) bool {
	for _, item := range items {
		call, ok := item.AsDynamicToolCall()
		if ok && call.Tool == probeToolName && call.Status == protocolv2.DynamicToolCallStatusCompleted {
			return true
		}
	}
	return false
}

func hasFileChange(items []protocolv2.ThreadItem) bool {
	for _, item := range items {
		change, ok := item.AsFileChange()
		if ok && change.Status == protocolv2.PatchApplyStatusCompleted {
			return true
		}
	}
	return false
}

func hasCommandExecution(items []protocolv2.ThreadItem) bool {
	for _, item := range items {
		if _, ok := item.AsCommandExecution(); ok {
			return true
		}
	}
	return false
}

func applyPatchObserved(items []protocolv2.ThreadItem) bool {
	if hasFileChange(items) {
		return true
	}
	for _, item := range items {
		call, ok := item.AsDynamicToolCall()
		if !ok {
			continue
		}
		name := call.Tool
		if call.Namespace != nil && call.Namespace.Value != nil && *call.Namespace.Value != "" {
			name = *call.Namespace.Value + "." + call.Tool
		}
		if strings.Contains(name, "apply_patch") {
			return true
		}
	}
	return false
}

func imageViewsPath(items []protocolv2.ThreadItem, path string) bool {
	want := filepath.Clean(path)
	for _, item := range items {
		view, ok := item.AsImageView()
		if ok && filepath.Clean(view.Path) == want {
			return true
		}
	}
	return false
}

func verifySavedImage(imageItem protocolv2.ThreadItemImageGeneration) (savedPath string, mime string, err error) {
	if imageItem.SavedPath == nil || imageItem.SavedPath.Value == nil || *imageItem.SavedPath.Value == "" {
		return "", "", errors.New("image result has no saved path")
	}
	savedPath = *imageItem.SavedPath.Value
	saved, err := os.ReadFile(savedPath)
	if err != nil {
		return "", "", errors.New("saved image is not user-accessible")
	}
	payload, err := decodeImagePayload(imageItem.Result)
	if err != nil {
		return "", "", err
	}
	if !bytes.Equal(saved, payload) {
		return "", "", errors.New("saved image differs from the completed result")
	}
	mime = http.DetectContentType(payload)
	switch mime {
	case "image/jpeg", "image/png":
		cfg, format, err := image.DecodeConfig(bytes.NewReader(payload))
		if err != nil {
			return "", "", errors.New("saved image does not decode")
		}
		if (mime == "image/jpeg" && format != "jpeg") || (mime == "image/png" && format != "png") {
			return "", "", errors.New("image MIME does not match decoder")
		}
		if cfg.Width <= 0 || cfg.Height <= 0 {
			return "", "", errors.New("decoded image has no dimensions")
		}
	case "image/webp":
		if !isWebP(payload) {
			return "", "", errors.New("image MIME is webp but the payload is not")
		}
	default:
		return "", "", errors.New("unsupported image content signature")
	}
	return savedPath, mime, nil
}

func decodeImagePayload(result string) ([]byte, error) {
	trimmed := strings.TrimSpace(result)
	if _, data, ok := strings.Cut(trimmed, ","); ok && strings.HasPrefix(trimmed, "data:") {
		trimmed = data
	}
	payload, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, errors.New("image result is not base64")
	}
	return payload, nil
}

func isWebP(payload []byte) bool {
	return len(payload) >= 12 && string(payload[:4]) == "RIFF" && string(payload[8:12]) == "WEBP"
}

type durableImage struct {
	SavedPath string
	Result    string
	Status    string
}

type durableFacts struct {
	encryptedReasoning bool
	applyPatchCall     bool
	fileChange         bool
	commandExecution   bool
	historyImageRef    bool
	images             []durableImage
}

func scanDurableFacts(home string) durableFacts {
	var facts durableFacts
	_ = filepath.WalkDir(filepath.Join(home, "sessions"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, raw := range bytes.Split(data, []byte("\n")) {
			raw = bytes.TrimSpace(raw)
			if len(raw) == 0 {
				continue
			}
			var rec struct {
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			if json.Unmarshal(raw, &rec) != nil {
				continue
			}
			switch rec.Type {
			case "response_item":
				var item struct {
					Type             string `json:"type"`
					Name             string `json:"name"`
					EncryptedContent string `json:"encrypted_content"`
					Arguments        string `json:"arguments"`
				}
				if json.Unmarshal(rec.Payload, &item) != nil {
					continue
				}
				if item.EncryptedContent != "" {
					facts.encryptedReasoning = true
				}
				if item.Type == "function_call" || item.Type == "custom_tool_call" {
					if strings.Contains(item.Name, "apply_patch") {
						facts.applyPatchCall = true
					}
					if strings.Contains(item.Name, "exec_command") || item.Name == "shell" {
						facts.commandExecution = true
					}
					if strings.Contains(item.Arguments, "num_last_images_to_include") || strings.Contains(item.Arguments, "referenced_image_paths") {
						facts.historyImageRef = true
					}
				}
			case "event_msg":
				var event struct {
					Type      string          `json:"type"`
					Item      json.RawMessage `json:"item"`
					Status    string          `json:"status"`
					Result    string          `json:"result"`
					SavedPath string          `json:"saved_path"`
				}
				if json.Unmarshal(rec.Payload, &event) != nil {
					continue
				}
				if event.Type == "image_generation_end" {
					facts.images = append(facts.images, durableImage{
						SavedPath: event.SavedPath,
						Result:    event.Result,
						Status:    event.Status,
					})
					continue
				}
				if event.Type != "item_completed" {
					continue
				}
				var completed struct {
					Type      string `json:"type"`
					Kind      string `json:"kind"`
					Status    string `json:"status"`
					Result    string `json:"result"`
					SavedPath string `json:"savedPath"`
				}
				if json.Unmarshal(event.Item, &completed) != nil {
					continue
				}
				kind := completed.Kind
				if kind == "" {
					kind = completed.Type
				}
				switch {
				case kind == "FileChange" || kind == "fileChange" || completed.Type == "FileChange":
					facts.fileChange = true
				case kind == "CommandExecution" || kind == "commandExecution" || completed.Type == "CommandExecution":
					facts.commandExecution = true
				case kind == "image_gen.generation" || completed.Type == "ImageGeneration" || completed.Type == "imageGeneration":
					status := completed.Status
					if status == "" {
						status = "completed"
					}
					facts.images = append(facts.images, durableImage{
						SavedPath: completed.SavedPath,
						Result:    completed.Result,
						Status:    status,
					})
				}
			}
		}
		return nil
	})
	return facts
}

func durableHasEncryptedReasoning(home string) bool {
	return scanDurableFacts(home).encryptedReasoning
}

func durableContainsToken(home, token string) bool {
	if token == "" {
		return false
	}
	found := false
	needle := []byte(token)
	_ = filepath.WalkDir(filepath.Join(home, "sessions"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if bytes.Contains(data, needle) {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

func waitDurable(timeout time.Duration, ready func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if ready() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

type liveServerRequests struct {
	mu         sync.Mutex
	toolName   string
	toolOutput string
	toolCalls  int
}

func newLiveServerRequests() *liveServerRequests {
	return &liveServerRequests{}
}

func (s *liveServerRequests) handler(_ context.Context, request protocolv2.ServerRequest) (codexsdk.ServerRequestResponse, error) {
	switch request.Kind() {
	case protocolv2.ServerRequestKindItemToolCall:
		call, _ := request.AsItemToolCall()
		return s.answerTool(call.Params)
	case protocolv2.ServerRequestKindItemCommandExecutionRequestApproval:
		return codexsdk.CommandExecutionApprovalResponse(protocolv2.CommandExecutionRequestApprovalResponse{
			Decision: protocolv2.NewCommandExecutionApprovalDecisionDecline(),
		}), nil
	case protocolv2.ServerRequestKindItemFileChangeRequestApproval:
		return codexsdk.FileChangeApprovalResponse(protocolv2.FileChangeRequestApprovalResponse{
			Decision: protocolv2.FileChangeApprovalDecisionDecline,
		}), nil
	case protocolv2.ServerRequestKindItemToolRequestUserInput:
		return codexsdk.ToolUserInputResponse(protocolv2.ToolRequestUserInputResponse{
			Answers: map[string]protocolv2.ToolRequestUserInputAnswer{},
		}), nil
	case protocolv2.ServerRequestKindMCPServerElicitationRequest:
		return codexsdk.MCPElicitationResponse(protocolv2.McpServerElicitationRequestResponse{
			Action: protocolv2.McpServerElicitationActionDecline,
		}), nil
	case protocolv2.ServerRequestKindItemPermissionsRequestApproval:
		return codexsdk.PermissionsApprovalResponse(protocolv2.PermissionsRequestApprovalResponse{
			Permissions: protocolv2.GrantedPermissionProfile{},
		}), nil
	case protocolv2.ServerRequestKindCurrentTimeRead:
		return codexsdk.CurrentTimeResponse(protocolv2.CurrentTimeReadResponse{
			CurrentTimeAt: time.Now().UnixMilli(),
		}), nil
	case protocolv2.ServerRequestKindApplyPatchApproval:
		return codexsdk.ApplyPatchApprovalResponse(protocolv2.ApplyPatchApprovalResponse{
			Decision: protocolv2.NewReviewDecisionDenied(protocolv2.ReviewDecisionDenied{Rejection: "grok live declines approvals"}),
		}), nil
	case protocolv2.ServerRequestKindExecCommandApproval:
		return codexsdk.ExecCommandApprovalResponse(protocolv2.ExecCommandApprovalResponse{
			Decision: protocolv2.NewReviewDecisionDenied(protocolv2.ReviewDecisionDenied{Rejection: "grok live declines approvals"}),
		}), nil
	default:
		return codexsdk.ServerRequestResponse{}, fmt.Errorf("no answer for server request %s", request.Kind())
	}
}

func (s *liveServerRequests) answerTool(params protocolv2.DynamicToolCallParams) (codexsdk.ServerRequestResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.toolName == "" || params.Tool != s.toolName {
		return codexsdk.ServerRequestResponse{}, fmt.Errorf("unexpected dynamic tool request")
	}
	s.toolCalls++
	return codexsdk.DynamicToolResponse(protocolv2.DynamicToolCallResponse{
		ContentItems: []protocolv2.DynamicToolCallOutputContentItem{
			protocolv2.NewDynamicToolCallOutputContentItemInputText(protocolv2.DynamicToolCallOutputContentItemInputText{Text: s.toolOutput}),
		},
		Success: true,
	}), nil
}

func durableTypeInventory(home string) string {
	counts := map[string]int{}
	_ = filepath.WalkDir(filepath.Join(home, "sessions"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, raw := range bytes.Split(data, []byte("\n")) {
			raw = bytes.TrimSpace(raw)
			if len(raw) == 0 {
				continue
			}
			var rec struct {
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			if json.Unmarshal(raw, &rec) != nil {
				counts["unparsed"]++
				continue
			}
			counts["rec:"+rec.Type]++
			var payload struct {
				Type string `json:"type"`
				Kind string `json:"kind"`
			}
			if json.Unmarshal(rec.Payload, &payload) == nil {
				if payload.Type != "" {
					counts["payload:"+payload.Type]++
				}
				if payload.Kind != "" {
					counts["kind:"+payload.Kind]++
				}
			}
			if rec.Type == "event_msg" && payload.Type == "item_completed" {
				var event struct {
					Item json.RawMessage `json:"item"`
				}
				if json.Unmarshal(rec.Payload, &event) == nil {
					var item struct {
						Type string `json:"type"`
						Kind string `json:"kind"`
					}
					if json.Unmarshal(event.Item, &item) == nil {
						if item.Type != "" {
							counts["item:"+item.Type]++
						}
						if item.Kind != "" {
							counts["itemkind:"+item.Kind]++
						}
						if item.Type == "Extension" {
							var obj map[string]json.RawMessage
							if json.Unmarshal(event.Item, &obj) == nil {
								keys := make([]string, 0, len(obj))
								for key := range obj {
									keys = append(keys, key)
								}
								sort.Strings(keys)
								counts["extkeys:"+strings.Join(keys, "+")]++
							}
						}
					}
				}
			}
			if rec.Type == "response_item" && payload.Type == "function_call" {
				var item struct {
					Name string `json:"name"`
				}
				if json.Unmarshal(rec.Payload, &item) == nil && item.Name != "" {
					counts["call:"+item.Name]++
				}
			}
		}
		return nil
	})
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ",")
}
