package codexsdk

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2"
)

func fakeCommand(mode string, extra ...string) []string {
	args := []string{os.Args[0], "-test.run=TestHelperProcess", "--", mode}
	args = append(args, extra...)
	return args
}

func fakeLateApprovalDuringFailureCommand(notificationAccepted, failureObserved, lateRequestSent string) []string {
	return fakeCommand("late-approval-during-failure", notificationAccepted, failureObserved, lateRequestSent)
}

func fakeHandlerErrorThenTransportCloseCommand(handlerFailureObserved string) []string {
	return fakeCommand("handler-error-then-transport-close", handlerFailureObserved)
}

func fakeProtocolFailureMultipleStreamsCommand(protocolFailureRelease string) []string {
	return fakeCommand("protocol-failure-multiple-streams", protocolFailureRelease)
}

func fakeAuthRefreshAfterNotificationCommand(notificationAccepted string) []string {
	return fakeCommand("auth-refresh-after-notification", notificationAccepted)
}

func fakeNotificationOverflowCommand(notificationAccepted string) []string {
	return fakeCommand("notification-overflow", notificationAccepted)
}

func tempRecord(t *testing.T) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "codexsdk-record-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func readRecords(t *testing.T, path string) []map[string]any {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var records []map[string]any
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("bad record %q: %v", scanner.Text(), err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return records
}

func firstRecord(records []map[string]any, kind, method string) map[string]any {
	for _, record := range records {
		if record["kind"] != kind {
			continue
		}
		if method == "" || record["method"] == method {
			return record
		}
	}
	return nil
}

func recordsByMethod(records []map[string]any, kind, method string) []map[string]any {
	var out []map[string]any
	for _, record := range records {
		if record["kind"] == kind && record["method"] == method {
			out = append(out, record)
		}
	}
	return out
}

func mustOutputSchema(t *testing.T, raw string) protocolv2.OutputSchema {
	t.Helper()
	schema, err := protocolv2.OutputSchemaFromJSON([]byte(raw))
	if err != nil {
		t.Fatalf("parse output schema %s: %v", raw, err)
	}
	return schema
}

func assertJSONEqual(t *testing.T, got any, want string) {
	t.Helper()
	gotRaw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal got JSON value: %v", err)
	}
	var gotValue any
	if err := json.Unmarshal(gotRaw, &gotValue); err != nil {
		t.Fatalf("unmarshal got JSON value %q: %v", gotRaw, err)
	}
	var wantValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("unmarshal want JSON value %q: %v", want, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("JSON value = %#v, want %#v", gotValue, wantValue)
	}
}

func waitForRecord(t *testing.T, path, kind, method string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if firstRecord(readRecords(t, path), kind, method) != nil {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func stringify(values []any) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value.(string))
	}
	return out
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("CODEXSDK_FAKE_RECORD") == "" {
		return
	}
	idx := 0
	for i, arg := range os.Args {
		if arg == "--" {
			idx = i + 1
			break
		}
	}
	if idx == 0 || idx >= len(os.Args) {
		return
	}
	runFakeAppServer(os.Args[idx], os.Args[idx+1:])
	os.Exit(0)
}

func runFakeAppServer(mode string, extra []string) {
	record := os.Getenv("CODEXSDK_FAKE_RECORD")
	appendRecord(record, map[string]any{"kind": "process", "argv": os.Args[1:], "cwd": mustGetwd(), "extra": extra})
	scanner := bufio.NewScanner(os.Stdin)
	threadCounter := 0
	resumeCounter := 0
	turnCounter := 0
	pendingConcurrent := []string{}
	for scanner.Scan() {
		var message map[string]any
		_ = json.Unmarshal(scanner.Bytes(), &message)
		method, _ := message["method"].(string)
		if method == "" {
			appendRecord(record, map[string]any{"kind": "recv-response", "result": message["result"], "error": message["error"]})
			if mode == "approval" || mode == "approval-before-turn-start" || mode == "file-approval" || mode == "user-input" || mode == "notification-and-approval" || mode == "late-approval-during-close" {
				completeTurn("thread-1", "turn-1")
			}
			continue
		}
		appendRecord(record, map[string]any{"kind": "recv", "method": method, "params": message["params"]})
		id := message["id"]
		switch method {
		case "account/login/cancel":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.CancelLoginAccountResponse{
					Status: protocolv2.CancelLoginAccountStatusCanceled,
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"status": "canceled"}})
		case "account/login/start":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.NewLoginAccountResponseChatGPT(protocolv2.LoginAccountResponseChatGPT{
					AuthURL: "https://example.test/auth",
					LoginID: "login-1",
				}))
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"authUrl": "https://example.test/auth", "loginId": "login-1", "type": "chatgpt"}})
		case "account/logout":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.LogoutAccountResponse{})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "account/rateLimits/read":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.GetAccountRateLimitsResponse{
					RateLimits: protocolv2.RateLimitSnapshot{
						PlanType: protocolv2.Value(protocolv2.PlanTypePlus),
					},
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"rateLimits": map[string]any{"planType": "plus"}}})
		case "account/rateLimitResetCredit/consume":
			send(map[string]any{"id": id, "result": map[string]any{"outcome": "reset"}})
		case "account/read":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.GetAccountResponse{
					Account:            protocolv2.Value(protocolv2.NewAccountAPIKey()),
					RequiresOpenaiAuth: false,
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"account": map[string]any{"email": "user@example.test", "planType": "plus", "type": "chatgpt"}, "requiresOpenaiAuth": false}})
		case "account/sendAddCreditsNudgeEmail":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.SendAddCreditsNudgeEmailResponse{
					Status: protocolv2.AddCreditsNudgeEmailStatusSent,
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"status": "sent"}})
		case "app/list":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.AppsListResponse{
					Data: []protocolv2.AppInfo{{ID: "app-1", Name: "App One"}},
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"data": []map[string]any{}}})
		case "collaborationMode/list":
			if mode == "collaboration-modes-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"nextCursor": "cursor-1"}})
				continue
			}
			if mode == "facade" {
				sendProtocolResult(id, facadeCollaborationModeListResponse())
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"data": []map[string]any{}}})
		case "command/exec":
			if mode == "command-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"exitCode": 0, "stdout": "ok\n"}})
				continue
			}
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.CommandExecResponse{
					ExitCode: 0,
					Stderr:   "",
					Stdout:   "ok\n",
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"exitCode": 0, "stderr": "", "stdout": "ok\n"}})
		case "command/exec/resize":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.CommandExecResizeResponse{})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "command/exec/terminate":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.CommandExecTerminateResponse{})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "command/exec/write":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.CommandExecWriteResponse{})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "config/batchWrite":
			if mode == "facade" {
				sendProtocolResult(id, facadeConfigWriteResponse())
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"filePath": "/home/user/.codex/config.toml", "status": "ok", "version": "v2"}})
		case "config/mcpServer/reload":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.McpServerRefreshResponse{})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "config/read":
			if mode == "config-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"config": map[string]any{"model": "gpt-5"}}})
				continue
			}
			if mode == "facade" {
				sendProtocolResult(id, facadeConfigReadResponse())
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"config": map[string]any{"model": "gpt-5"}, "origins": map[string]any{}}})
		case "config/value/write":
			if mode == "facade" {
				sendProtocolResult(id, facadeConfigWriteResponse())
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"filePath": "/home/user/.codex/config.toml", "status": "ok", "version": "v2"}})
		case "configRequirements/read":
			if mode == "config-requirements-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"extra": true}})
				continue
			}
			if mode == "facade" {
				sendProtocolResult(id, facadeConfigRequirementsReadResponse())
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"requirements": map[string]any{}}})
		case "experimentalFeature/enablement/set":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.ExperimentalFeatureEnablementSetResponse{
					Enablement: map[string]bool{
						"feature_a": true,
						"feature_b": false,
					},
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"enablement": map[string]bool{"feature_a": true}}})
		case "experimentalFeature/list":
			if mode == "experimental-features-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"nextCursor": "cursor-2"}})
				continue
			}
			if mode == "facade" {
				sendProtocolResult(id, facadeExperimentalFeatureListResponse())
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"data": []map[string]any{}}})
		case "externalAgentConfig/detect":
			if mode == "external-agent-configs-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{}})
				continue
			}
			if mode == "facade" {
				sendProtocolResult(id, facadeExternalAgentConfigDetectResponse())
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"items": []map[string]any{}}})
		case "externalAgentConfig/import":
			if mode == "facade" {
				send(map[string]any{"id": id, "result": map[string]any{}})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"importId": "import-1"}})
		case "feedback/upload":
			if mode == "feedback-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{}})
				continue
			}
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.FeedbackUploadResponse{ThreadID: "thread-1"})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"threadId": "thread-1"}})
		case "fuzzyFileSearch":
			if mode == "fuzzy-file-search-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{}})
				continue
			}
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.FuzzyFileSearchResponse{
					Files: []protocolv2.FuzzyFileSearchResult{{
						FileName:  "README.md",
						Indices:   protocolv2.Value([]uint32{0, 2}),
						MatchType: protocolv2.FuzzyFileSearchMatchTypeFile,
						Path:      "/repo/README.md",
						Root:      "/repo",
						Score:     99,
					}},
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"files": []map[string]any{}}})
		case "fuzzyFileSearch/sessionStart":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.FuzzyFileSearchSessionStartResponse{})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "fuzzyFileSearch/sessionStop":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.FuzzyFileSearchSessionStopResponse{})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "fuzzyFileSearch/sessionUpdate":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.FuzzyFileSearchSessionUpdateResponse{})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "hooks/list":
			if mode == "hooks-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{}})
				continue
			}
			if mode == "facade" {
				sendProtocolResult(id, facadeHooksListResponse())
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"data": []map[string]any{}}})
		case "marketplace/add":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.MarketplaceAddResponse{
					AlreadyAdded:    false,
					InstalledRoot:   "/codex/marketplaces/local",
					MarketplaceName: "local",
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"alreadyAdded": false, "installedRoot": "/codex/marketplaces/local", "marketplaceName": "local"}})
		case "marketplace/remove":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.MarketplaceRemoveResponse{
					InstalledRoot:   protocolv2.Value("/codex/marketplaces/local"),
					MarketplaceName: "local",
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"installedRoot": "/codex/marketplaces/local", "marketplaceName": "local"}})
		case "marketplace/upgrade":
			if mode == "marketplace-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"selectedMarketplaces": []string{"local"}, "upgradedRoots": []string{"/codex/marketplaces/local"}}})
				continue
			}
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.MarketplaceUpgradeResponse{
					Errors:               []protocolv2.MarketplaceUpgradeErrorInfo{},
					SelectedMarketplaces: []string{"local"},
					UpgradedRoots:        []string{"/codex/marketplaces/local"},
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"errors": []map[string]any{}, "selectedMarketplaces": []string{"local"}, "upgradedRoots": []string{"/codex/marketplaces/local"}}})
		case "memory/reset":
			if mode == "memory-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"extra": true}})
				continue
			}
			sendProtocolResult(id, protocolv2.MemoryResetResponse{})
		case "mock/experimentalMethod":
			if mode == "mock-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"extra": true}})
				continue
			}
			var echoed *protocolv2.Nullable[string]
			if params, ok := message["params"].(map[string]any); ok {
				if value, ok := params["value"]; ok {
					if value == nil {
						echoed = protocolv2.Null[string]()
					} else if text, ok := value.(string); ok {
						echoed = protocolv2.Value(text)
					}
				}
			}
			sendProtocolResult(id, protocolv2.MockExperimentalMethodResponse{Echoed: echoed})
		case "plugin/install":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.PluginInstallResponse{
					AppsNeedingAuth: []protocolv2.AppSummary{{
						ID:   "app-1",
						Name: "App One",
					}},
					AuthPolicy: protocolv2.PluginAuthPolicyONINSTALL,
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"appsNeedingAuth": []map[string]any{}, "authPolicy": "ON_INSTALL"}})
		case "plugin/list":
			if mode == "plugins-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"featuredPluginIds": []string{"plugin-1"}}})
				continue
			}
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.PluginListResponse{
					FeaturedPluginIDs:     &[]string{"plugin-1"},
					MarketplaceLoadErrors: &[]protocolv2.MarketplaceLoadErrorInfo{},
					Marketplaces: []protocolv2.PluginMarketplaceEntry{{
						Name:    "market",
						Plugins: []protocolv2.PluginSummary{facadePluginSummary()},
					}},
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"marketplaces": []map[string]any{}}})
		case "plugin/read":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.PluginReadResponse{
					Plugin: facadePluginDetail(),
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"plugin": map[string]any{}}})
		case "plugin/share/delete":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.PluginShareDeleteResponse{})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "plugin/share/list":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.PluginShareListResponse{
					Data: []protocolv2.PluginShareListItem{{
						LocalPluginPath: protocolv2.Value("/plugins/plugin-one"),
						Plugin:          facadePluginSummary(),
					}},
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"data": []map[string]any{}}})
		case "plugin/share/save":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.PluginShareSaveResponse{
					RemotePluginID: "remote-1",
					ShareURL:       "https://example.test/plugin",
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"remotePluginId": "remote-1", "shareUrl": "https://example.test/plugin"}})
		case "plugin/share/updateTargets":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.PluginShareUpdateTargetsResponse{
					Discoverability: protocolv2.PluginShareDiscoverabilityPRIVATE,
					Principals: []protocolv2.PluginSharePrincipal{{
						Name:          "User One",
						PrincipalID:   "user-1",
						PrincipalType: protocolv2.PluginSharePrincipalTypeUser,
						Role:          protocolv2.PluginSharePrincipalRoleOwner,
					}},
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"discoverability": "PRIVATE", "principals": []map[string]any{}}})
		case "plugin/skill/read":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.PluginSkillReadResponse{
					Contents: protocolv2.Value("skill contents"),
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"contents": "skill contents"}})
		case "plugin/uninstall":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.PluginUninstallResponse{})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "process/kill":
			if mode == "process-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"extra": true}})
				continue
			}
			sendProtocolResult(id, protocolv2.ProcessKillResponse{})
		case "process/resizePty":
			if mode == "process-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"extra": true}})
				continue
			}
			sendProtocolResult(id, protocolv2.ProcessResizePtyResponse{})
		case "process/spawn":
			if mode == "process-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"extra": true}})
				continue
			}
			sendProtocolResult(id, protocolv2.ProcessSpawnResponse{})
		case "process/writeStdin":
			if mode == "process-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"extra": true}})
				continue
			}
			sendProtocolResult(id, protocolv2.ProcessWriteStdinResponse{})
		case "mcpServer/oauth/login":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.McpServerOauthLoginResponse{
					AuthorizationURL: "https://example.test/oauth",
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"authorizationUrl": "https://example.test/oauth"}})
		case "mcpServer/resource/read":
			if mode == "mcp-servers-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{}})
				continue
			}
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.McpResourceReadResponse{
					Contents: []protocolv2.ResourceContent{
						protocolv2.NewResourceContentText(protocolv2.ResourceContentText{
							Text: "hello",
							URI:  "file://README.md",
						}),
					},
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"contents": []map[string]any{{"text": "hello", "uri": "file://README.md"}}}})
		case "mcpServer/tool/call":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.McpServerToolCallResponse{
					Content: []protocolv2.JSONValue{protocolv2.JSONString("ok")},
					IsError: protocolv2.Value(false),
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"content": []string{"ok"}, "isError": false}})
		case "mcpServerStatus/list":
			if mode == "mcp-server-status-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"nextCursor": "cursor-2"}})
				continue
			}
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.ListMcpServerStatusResponse{
					Data: []protocolv2.McpServerStatus{{
						AuthStatus:        protocolv2.McpAuthStatusOAuth,
						Name:              "server-1",
						ResourceTemplates: []protocolv2.ResourceTemplate{},
						Resources:         []protocolv2.Resource{},
						Tools: map[string]protocolv2.Tool{
							"search": {
								InputSchema: protocolv2.JSONObject(map[string]protocolv2.JSONValue{"type": protocolv2.JSONString("object")}),
								Name:        "search",
							},
						},
					}},
					NextCursor: protocolv2.Value("cursor-2"),
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"data": []map[string]any{}, "nextCursor": "cursor-2"}})
		case "model/list":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.ModelListResponse{
					Data:       []protocolv2.Model{},
					NextCursor: protocolv2.Value("model-cursor"),
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"data": []map[string]any{}}})
		case "modelProvider/capabilities/read":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.ModelProviderCapabilitiesReadResponse{
					ImageGeneration: false,
					NamespaceTools:  true,
					WebSearch:       true,
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"imageGeneration": false, "namespaceTools": false, "webSearch": false}})
		case "review/start":
			if mode == "review-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"reviewThreadId": "review-thread-1"}})
				continue
			}
			if mode == "facade" {
				sendProtocolResult(id, facadeReviewStartResponse())
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"reviewThreadId": "review-thread-1", "turn": map[string]any{"id": "turn-review-1", "items": []map[string]any{}, "status": "inProgress"}}})
		case "skills/config/write":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.SkillsConfigWriteResponse{EffectiveEnabled: true})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"effectiveEnabled": false}})
		case "skills/list":
			if mode == "skills-list-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{}})
				continue
			}
			if mode == "facade" {
				sendProtocolResult(id, facadeSkillsListResponse())
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"data": []map[string]any{}}})
		case "fs/copy":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.FsCopyResponse{})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "fs/createDirectory":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.FsCreateDirectoryResponse{})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "fs/getMetadata":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.FsGetMetadataResponse{
					CreatedAtMS:  1000,
					IsDirectory:  false,
					IsFile:       true,
					IsSymlink:    false,
					ModifiedAtMS: 2000,
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "fs/readDirectory":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.FsReadDirectoryResponse{
					Entries: []protocolv2.FsReadDirectoryEntry{{
						FileName:    "file.txt",
						IsDirectory: false,
						IsFile:      true,
					}},
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "fs/readFile":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.FsReadFileResponse{DataBase64: "ZmFrZQ=="})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "fs/remove":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.FsRemoveResponse{})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "fs/unwatch":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.FsUnwatchResponse{})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "fs/watch":
			if mode == "facade" {
				params, _ := message["params"].(map[string]any)
				path, _ := params["path"].(string)
				sendProtocolResult(id, protocolv2.FsWatchResponse{Path: path})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "fs/writeFile":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.FsWriteFileResponse{})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "initialize":
			if mode == "initialize-error" {
				send(map[string]any{"id": id, "error": map[string]any{"code": -32000, "message": "initialize failed"}})
				continue
			}
			if mode == "initialize-close-stdout" {
				return
			}
			if mode == "initialize-malformed-stdout" {
				fmt.Fprintln(os.Stdout, "{not-json initialize_secret transcript")
				return
			}
			if mode == "bad-initialize" {
				send(map[string]any{"id": id, "result": map[string]any{"codexHome": "/tmp/codex-home", "platformFamily": "unix", "userAgent": "fake-app-server"}})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"codexHome": "/tmp/codex-home", "platformFamily": "unix", "platformOs": "darwin", "userAgent": "fake-app-server"}})
		case "initialized":
		case "thread/start":
			if mode == "stderr-exit" {
				fmt.Fprintln(os.Stderr, "stderr diagnostic secret transcript")
				return
			}
			if mode == "malformed-stdout" {
				fmt.Fprintln(os.Stdout, "{not-json stdout_secret transcript")
				return
			}
			threadCounter++
			if mode == "thread-start-missing-id-once" && threadCounter == 1 {
				response := facadeThreadStartResponse("", "decoded-model")
				sendProtocolResult(id, response)
				sendExactReroute("", "turn-ghost", "unattributed", "must-not-attach")
				continue
			}
			if mode == "thread-start-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": "thread-" + itoa(threadCounter)}}})
				continue
			}
			if mode == "facade" {
				params, _ := message["params"].(map[string]any)
				model, _ := params["model"].(string)
				sendProtocolResult(id, facadeThreadStartResponse("thread-"+itoa(threadCounter), model))
				continue
			}
			params, _ := message["params"].(map[string]any)
			model, _ := params["model"].(string)
			sendProtocolResult(id, facadeThreadStartResponse("thread-"+itoa(threadCounter), model))
		case "thread/resume":
			resumeCounter++
			params, _ := message["params"].(map[string]any)
			if mode == "thread-resume-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": "thread-existing"}}})
				continue
			}
			if mode == "thread-resume-missing-id-once" && resumeCounter == 1 {
				sendProtocolResult(id, facadeThreadResumeResponse("", "decoded-resume-model"))
				sendExactReroute("", "turn-ghost", "unattributed", "must-not-attach")
				continue
			}
			if mode == "facade" {
				threadID, _ := params["threadId"].(string)
				model, _ := params["model"].(string)
				sendProtocolResult(id, facadeThreadResumeResponse(threadID, model))
				continue
			}
			threadID, _ := params["threadId"].(string)
			model, _ := params["model"].(string)
			sendProtocolResult(id, facadeThreadResumeResponse(threadID, model))
		case "thread/fork":
			if mode == "facade" {
				params, _ := message["params"].(map[string]any)
				parentThreadID, _ := params["threadId"].(string)
				model, _ := params["model"].(string)
				sendProtocolResult(id, facadeThreadForkResponse("thread-fork", parentThreadID, model))
				continue
			}
			params, _ := message["params"].(map[string]any)
			parentThreadID, _ := params["threadId"].(string)
			model, _ := params["model"].(string)
			sendProtocolResult(id, facadeThreadForkResponse("thread-fork", parentThreadID, model))
		case "thread/goal/clear":
			if mode == "thread-goal-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{}})
				continue
			}
			sendProtocolResult(id, protocolv2.ThreadGoalClearResponse{Cleared: true})
		case "thread/goal/get":
			if mode == "thread-goal-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"goal": map[string]any{"threadId": "thread-1"}}})
				continue
			}
			params, _ := message["params"].(map[string]any)
			threadID, _ := params["threadId"].(string)
			sendProtocolResult(id, protocolv2.ThreadGoalGetResponse{
				Goal: protocolv2.Value(facadeThreadGoal(defaultString(threadID, "thread-1"), "ship protocol coverage", protocolv2.ThreadGoalStatusActive, protocolv2.Value(int64(4096)))),
			})
		case "thread/goal/set":
			if mode == "thread-goal-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"goal": map[string]any{"threadId": "thread-1"}}})
				continue
			}
			params, _ := message["params"].(map[string]any)
			threadID, _ := params["threadId"].(string)
			objective, _ := params["objective"].(string)
			statusText, _ := params["status"].(string)
			status := protocolv2.ThreadGoalStatus(statusText)
			if !status.IsValid() {
				status = protocolv2.ThreadGoalStatusActive
			}
			tokenBudget := protocolv2.Value(int64(4096))
			if value, ok := params["tokenBudget"].(float64); ok {
				tokenBudget = protocolv2.Value(int64(value))
			}
			sendProtocolResult(id, protocolv2.ThreadGoalSetResponse{
				Goal: facadeThreadGoal(defaultString(threadID, "thread-1"), defaultString(objective, "ship protocol coverage"), status, tokenBudget),
			})
		case "thread/increment_elicitation":
			if mode == "thread-support-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"paused": true}})
				continue
			}
			sendProtocolResult(id, protocolv2.ThreadIncrementElicitationResponse{Count: 3, Paused: true})
		case "thread/decrement_elicitation":
			if mode == "thread-support-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"paused": false}})
				continue
			}
			sendProtocolResult(id, protocolv2.ThreadDecrementElicitationResponse{Count: 2, Paused: false})
		case "thread/memoryMode/set":
			if mode == "thread-support-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"extra": true}})
				continue
			}
			sendProtocolResult(id, protocolv2.ThreadMemoryModeSetResponse{})
		case "thread/backgroundTerminals/clean":
			if mode == "thread-support-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"extra": true}})
				continue
			}
			sendProtocolResult(id, protocolv2.ThreadBackgroundTerminalsCleanResponse{})
		case "thread/items/list":
			if mode == "thread-turns-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"nextCursor": "next-item"}})
				continue
			}
			sendProtocolResult(id, protocolv2.ThreadItemsListResponse{
				BackwardsCursor: protocolv2.Null[string](),
				Data:            []protocolv2.ThreadItem{facadeThreadAgentMessageItem("item-agent-1", "final text")},
				NextCursor:      protocolv2.Value("next-item"),
			})
		case "thread/turns/list":
			if mode == "thread-turns-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"nextCursor": "next-turn"}})
				continue
			}
			sendProtocolResult(id, protocolv2.ThreadTurnsListResponse{
				BackwardsCursor: protocolv2.Null[string](),
				Data:            []protocolv2.Turn{facadeThreadTurn("turn-list-1", []protocolv2.ThreadItem{facadeThreadAgentMessageItem("item-agent-1", "final text")})},
				NextCursor:      protocolv2.Value("next-turn"),
			})
		case "thread/realtime/start":
			if mode == "thread-realtime-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"extra": true}})
				continue
			}
			sendProtocolResult(id, protocolv2.ThreadRealtimeStartResponse{})
		case "thread/realtime/appendAudio":
			if mode == "thread-realtime-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"extra": true}})
				continue
			}
			sendProtocolResult(id, protocolv2.ThreadRealtimeAppendAudioResponse{})
		case "thread/realtime/appendText":
			if mode == "thread-realtime-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"extra": true}})
				continue
			}
			sendProtocolResult(id, protocolv2.ThreadRealtimeAppendTextResponse{})
		case "thread/realtime/listVoices":
			if mode == "thread-realtime-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{}})
				continue
			}
			sendProtocolResult(id, protocolv2.ThreadRealtimeListVoicesResponse{
				Voices: protocolv2.RealtimeVoicesList{
					DefaultV1: protocolv2.RealtimeVoiceAlloy,
					DefaultV2: protocolv2.RealtimeVoiceMarin,
					V1:        []protocolv2.RealtimeVoice{protocolv2.RealtimeVoiceAlloy},
					V2:        []protocolv2.RealtimeVoice{protocolv2.RealtimeVoiceMarin},
				},
			})
		case "thread/realtime/stop":
			if mode == "thread-realtime-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"extra": true}})
				continue
			}
			sendProtocolResult(id, protocolv2.ThreadRealtimeStopResponse{})
		case "thread/approveGuardianDeniedAction":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.ThreadApproveGuardianDeniedActionResponse{})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "thread/archive":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.ThreadArchiveResponse{})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "thread/compact/start":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.ThreadCompactStartResponse{})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "thread/inject_items":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.ThreadInjectItemsResponse{})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "thread/list":
			if mode == "thread-list-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"nextCursor": "next-thread"}})
				continue
			}
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.ThreadListResponse{
					BackwardsCursor: protocolv2.Null[string](),
					Data:            []protocolv2.Thread{facadeThread("thread-list-1", nil)},
					NextCursor:      protocolv2.Value("next-thread"),
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"data": []any{}}})
		case "thread/loaded/list":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.ThreadLoadedListResponse{
					Data:       []string{"thread-1"},
					NextCursor: protocolv2.Value("loaded-next"),
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"data": []any{}}})
		case "thread/metadata/update":
			if mode == "facade" {
				params, _ := message["params"].(map[string]any)
				threadID, _ := params["threadId"].(string)
				sendProtocolResult(id, protocolv2.ThreadMetadataUpdateResponse{Thread: facadeThread(defaultString(threadID, "thread-1"), nil)})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": "thread-1"}}})
		case "thread/name/set":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.ThreadSetNameResponse{})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "thread/read":
			if mode == "facade" {
				params, _ := message["params"].(map[string]any)
				threadID, _ := params["threadId"].(string)
				sendProtocolResult(id, protocolv2.ThreadReadResponse{Thread: facadeThread(defaultString(threadID, "thread-1"), nil)})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": "thread-1"}}})
		case "thread/rollback":
			if mode == "facade" {
				params, _ := message["params"].(map[string]any)
				threadID, _ := params["threadId"].(string)
				sendProtocolResult(id, protocolv2.ThreadRollbackResponse{Thread: facadeThread(defaultString(threadID, "thread-1"), nil)})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": "thread-1"}}})
		case "thread/shellCommand":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.ThreadShellCommandResponse{})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "thread/unarchive":
			if mode == "facade" {
				params, _ := message["params"].(map[string]any)
				threadID, _ := params["threadId"].(string)
				sendProtocolResult(id, protocolv2.ThreadUnarchiveResponse{Thread: facadeThread(defaultString(threadID, "thread-1"), nil)})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": "thread-1"}}})
		case "thread/unsubscribe":
			if mode == "thread-unsubscribe-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"status": "bogus"}})
				continue
			}
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.ThreadUnsubscribeResponse{Status: protocolv2.ThreadUnsubscribeStatusUnsubscribed})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"status": "unsubscribed"}})
		case "turn/start":
			turnCounter++
			turnID := "turn-" + itoa(turnCounter)
			if mode == "thread-start-missing-id-once" {
				turnID = "turn-ghost"
			}
			params, _ := message["params"].(map[string]any)
			threadID, _ := params["threadId"].(string)
			if mode == "turn-start-missing-id-once" && turnCounter == 1 {
				sendProtocolResult(id, protocolv2.TurnStartResponse{
					Turn: protocolv2.Turn{
						ID:        "",
						Items:     []protocolv2.ThreadItem{facadeThreadAgentMessageItem("decoded-item", "decoded partial turn")},
						StartedAt: protocolv2.Value(int64(1234)),
						Status:    protocolv2.TurnStatusInProgress,
					},
				})
				sendExactReroute("", "", "unattributed", "must-not-attach")
				continue
			}
			if mode == "turn-start-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"turn": map[string]any{"id": turnID, "status": "inProgress"}}})
				continue
			}
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.TurnStartResponse{
					Turn: protocolv2.Turn{
						ID:     turnID,
						Items:  []protocolv2.ThreadItem{},
						Status: protocolv2.TurnStatusInProgress,
					},
				})
				continue
			}
			if mode == "approval-before-turn-start" {
				send(map[string]any{"id": "server-approval-1", "method": "item/commandExecution/requestApproval", "params": fakeCommandApprovalParams(threadID, turnID)})
			}
			if mode == "pending-terminal-protocol-failure" {
				completeTurn(threadID, turnID)
				_, _ = fmt.Fprintln(os.Stdout, "{")
				return
			}
			if mode == "pending-terminal-close" {
				completeTurn(threadID, turnID)
				waitForFakePath(extra[0])
			}
			sendProtocolResult(id, protocolv2.TurnStartResponse{
				Turn: protocolv2.Turn{
					ID:     turnID,
					Items:  []protocolv2.ThreadItem{},
					Status: protocolv2.TurnStatusInProgress,
				},
			})
			switch mode {
			case "exact-history-wait":
				for index := 0; index < exactStreamHistoryContractNotifications; index++ {
					sendExactReroute(threadID, turnID, "model-"+itoa(index), "model-"+itoa(index+1))
				}
				completeTurn(threadID, turnID)
			case "failed":
				send(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": threadID, "turn": map[string]any{"id": turnID, "items": []map[string]any{}, "status": "failed", "error": map[string]any{"message": "native failed", "codexErrorInfo": "usageLimitExceeded"}}}})
			case "interrupted":
				send(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": threadID, "turn": map[string]any{"id": turnID, "items": []map[string]any{}, "status": "interrupted"}}})
			case "delta-without-final":
				send(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": threadID, "turnId": turnID, "itemId": "item-delta", "delta": "delta-only-output"}})
				send(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": threadID, "turn": map[string]any{"id": turnID, "status": "completed", "items": []map[string]any{}}}})
			case "turn-completed-in-progress":
				send(map[string]any{"method": "item/completed", "params": map[string]any{"completedAtMs": 1234, "threadId": threadID, "turnId": turnID, "item": map[string]any{"id": "item-" + turnID, "type": "agentMessage", "text": "non-terminal", "phase": "final_answer"}}})
				send(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": threadID, "turn": map[string]any{"id": turnID, "status": "inProgress", "items": []map[string]any{}}}})
			case "hang":
			case "approval":
				send(map[string]any{"id": "server-approval-1", "method": "item/commandExecution/requestApproval", "params": fakeCommandApprovalParams(threadID, turnID)})
			case "notification-and-approval":
				send(map[string]any{"method": "item/completed", "params": map[string]any{"completedAtMs": 1, "threadId": threadID, "turnId": turnID, "item": map[string]any{"id": "item-before-close", "type": "agentMessage", "text": "partial", "phase": "commentary"}}})
				send(map[string]any{"id": "server-approval-1", "method": "item/commandExecution/requestApproval", "params": fakeCommandApprovalParams(threadID, turnID)})
			case "notification-overflow":
				notificationAccepted := extra[0]
				sendExactReroute(threadID, turnID, "model-a", "model-b")
				waitForFakePath(notificationAccepted)
				sendExactReroute(threadID, turnID, "model-b", "model-c")
				sendExactReroute(threadID, turnID, "model-c", "model-d")
			case "late-approval-during-close":
				send(map[string]any{"id": "server-approval-1", "method": "item/commandExecution/requestApproval", "params": fakeCommandApprovalParams(threadID, turnID)})
				waitForFakePath(extra[0])
				send(map[string]any{"id": "server-approval-late", "method": "item/commandExecution/requestApproval", "params": fakeCommandApprovalParams(threadID, turnID)})
			case "late-approval-during-failure":
				notificationAccepted, failureObserved, lateRequestSent := extra[0], extra[1], extra[2]
				send(map[string]any{"method": "item/completed", "params": map[string]any{"completedAtMs": 1, "threadId": threadID, "turnId": turnID, "item": map[string]any{"id": "partial", "type": "agentMessage", "text": "partial", "phase": "commentary"}}})
				waitForFakePath(notificationAccepted)
				send(map[string]any{"id": "server-approval-1", "method": "item/commandExecution/requestApproval", "params": fakeCommandApprovalParams(threadID, turnID)})
				waitForFakePath(failureObserved)
				send(map[string]any{"id": "server-approval-late", "method": "item/commandExecution/requestApproval", "params": fakeCommandApprovalParams(threadID, turnID)})
				if err := os.WriteFile(lateRequestSent, []byte("sent"), 0o600); err != nil {
					return
				}
			case "handler-error-then-transport-close":
				handlerFailureObserved := extra[0]
				send(map[string]any{"method": "item/completed", "params": map[string]any{"completedAtMs": 1, "threadId": threadID, "turnId": turnID, "item": map[string]any{"id": "partial", "type": "agentMessage", "text": "partial", "phase": "commentary"}}})
				waitForFakePath(handlerFailureObserved)
				return
			case "protocol-failure-multiple-streams":
				protocolFailureRelease := extra[0]
				send(map[string]any{"method": "item/completed", "params": map[string]any{"completedAtMs": int64(turnCounter), "threadId": threadID, "turnId": turnID, "item": map[string]any{"id": "partial-" + turnID, "type": "agentMessage", "text": "partial", "phase": "commentary"}}})
				if turnCounter == 2 {
					waitForFakePath(protocolFailureRelease)
					_, _ = fmt.Fprintln(os.Stdout, "{")
					return
				}
			case "approval-before-turn-start":
			case "file-approval":
				send(map[string]any{"id": "server-file-1", "method": "item/fileChange/requestApproval", "params": map[string]any{"itemId": "item-file", "startedAtMs": 1, "threadId": threadID, "turnId": turnID}})
			case "user-input":
				send(map[string]any{"id": "server-input-1", "method": "item/tool/requestUserInput", "params": map[string]any{"itemId": "item-input", "questions": []map[string]any{{"header": "Choice", "id": "choice", "question": "Choose"}}, "threadId": threadID, "turnId": turnID}})
			case "auth-refresh-after-notification":
				notificationAccepted := extra[0]
				send(map[string]any{"method": "item/completed", "params": map[string]any{"completedAtMs": 1, "threadId": threadID, "turnId": turnID, "item": map[string]any{"id": "item-before-auth", "type": "agentMessage", "text": "partial", "phase": "commentary"}}})
				waitForFakePath(notificationAccepted)
				send(map[string]any{"id": "server-auth-1", "method": "account/chatgptAuthTokens/refresh", "params": map[string]any{"reason": "unauthorized"}})
			case "approval-before-attach":
				send(map[string]any{"id": "server-approval-1", "method": "item/commandExecution/requestApproval", "params": fakeCommandApprovalParams(threadID, turnID)})
			case "tool-call-before-attach":
				send(map[string]any{"id": "server-tool-1", "method": protocolv2.MethodItemToolCall, "params": fakeDynamicToolCallParams(threadID, turnID)})
			case "blocking-approval-concurrent":
				if turnCounter == 1 {
					send(map[string]any{"id": "server-approval-1", "method": "item/commandExecution/requestApproval", "params": fakeCommandApprovalParams(threadID, turnID)})
				} else {
					completeTurn(threadID, turnID)
				}
			case "blocking-approval-delayed":
				time.Sleep(25 * time.Millisecond)
				send(map[string]any{"id": "server-approval-1", "method": "item/commandExecution/requestApproval", "params": fakeCommandApprovalParams(threadID, turnID)})
			case "unsupported":
				send(map[string]any{"id": "server-unsupported-1", "method": "unknown/serverRequest", "params": map[string]any{"threadId": threadID, "turnId": turnID, "itemId": "item-unsupported"}})
			case "unsupported-no-turn":
				send(map[string]any{"id": "server-unsupported-1", "method": "unknown/serverRequest", "params": map[string]any{"threadId": threadID, "itemId": "item-unsupported"}})
			case "unknown-notification":
				send(map[string]any{"method": "upstream/newNotification", "params": map[string]any{"threadId": threadID, "turnId": turnID}})
			case "malformed-background-notification":
				send(map[string]any{"method": "skills/changed", "params": map[string]any{"extra": true}})
			case "background-notification":
				send(map[string]any{"method": "skills/changed", "params": map[string]any{}})
				completeTurn(threadID, turnID)
			case "native-notifications":
				send(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": threadID, "turnId": turnID, "itemId": "item-delta", "delta": "native delta"}})
				send(map[string]any{"method": "error", "params": map[string]any{"threadId": threadID, "turnId": turnID, "willRetry": true, "error": map[string]any{"message": "native warning", "codexErrorInfo": "serverOverloaded"}}})
				send(map[string]any{"method": "model/rerouted", "params": map[string]any{"threadId": threadID, "turnId": turnID, "fromModel": "from-model", "toModel": "to-model", "reason": "highRiskCyberActivity"}})
				send(map[string]any{"method": "model/verification", "params": map[string]any{"threadId": threadID, "turnId": turnID, "verifications": []string{string(protocolv2.ModelVerificationTrustedAccessForCyber)}}})
				send(map[string]any{"method": "configWarning", "params": map[string]any{"summary": "native config summary", "details": "native config details", "path": "/tmp/config.toml"}})
				completeTurn(threadID, turnID)
			case "malformed-notification":
				send(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": threadID, "turnId": turnID, "delta": "missing item id"}})
			case "malformed-token-usage-notification":
				send(map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": threadID, "turnId": turnID, "tokenUsage": map[string]any{"last": fakeTokenUsageBreakdown(3, 1, 2, 1, 5)}}})
			case "malformed-turn-plan-notification":
				send(map[string]any{"method": "turn/plan/updated", "params": map[string]any{"threadId": threadID, "turnId": turnID}})
			case "malformed-hook-started-notification":
				send(map[string]any{"method": "hook/started", "params": map[string]any{"threadId": threadID, "turnId": turnID}})
			case "malformed-hook-completed-notification":
				send(map[string]any{"method": "hook/completed", "params": map[string]any{"run": map[string]any{
					"displayOrder":  1,
					"entries":       []any{},
					"eventName":     "preToolUse",
					"executionMode": "sync",
					"handlerType":   "command",
					"id":            "hook-run-1",
					"scope":         "turn",
					"sourcePath":    "/workspace/.codex/hooks.json",
					"startedAt":     1000,
					"status":        "running",
				}}})
			case "malformed-thread-goal-updated-notification":
				send(map[string]any{"method": "thread/goal/updated", "params": map[string]any{"threadId": threadID}})
			case "malformed-realtime-output-audio-notification":
				send(map[string]any{"method": "thread/realtime/outputAudio/delta", "params": map[string]any{"threadId": threadID}})
			case "malformed-guardian-review-started-notification":
				send(map[string]any{"method": "item/autoApprovalReview/started", "params": map[string]any{
					"review":      map[string]any{"status": "inProgress"},
					"reviewId":    "review-1",
					"startedAtMs": 123,
					"threadId":    threadID,
					"turnId":      turnID,
				}})
			case "malformed-guardian-review-completed-notification":
				send(map[string]any{"method": "item/autoApprovalReview/completed", "params": map[string]any{
					"action":        map[string]any{"command": "ls", "cwd": "/repo", "source": "shell", "type": "command"},
					"completedAtMs": 456,
					"review":        map[string]any{"status": "approved"},
					"reviewId":      "review-1",
					"startedAtMs":   123,
					"threadId":      threadID,
					"turnId":        turnID,
				}})
			case "long-line":
				completeLongTurn(threadID, turnID)
			case "concurrent":
				pendingConcurrent = append(pendingConcurrent, threadID+"|"+turnID)
				if len(pendingConcurrent) == 2 {
					parts2 := strings.Split(pendingConcurrent[1], "|")
					parts1 := strings.Split(pendingConcurrent[0], "|")
					completeTurn(parts2[0], parts2[1])
					completeTurn(parts1[0], parts1[1])
				}
			default:
				completeTurn(threadID, turnID)
			}
		case "turn/interrupt":
			if mode == "turn-interrupt-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{"extra": true}})
				continue
			}
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.TurnInterruptResponse{})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{}})
		case "turn/steer":
			if mode == "turn-steer-malformed-response" {
				send(map[string]any{"id": id, "result": map[string]any{}})
				continue
			}
			if mode == "facade" {
				params, _ := message["params"].(map[string]any)
				turnID, _ := params["expectedTurnId"].(string)
				sendProtocolResult(id, protocolv2.TurnSteerResponse{TurnID: defaultString(turnID, "turn-1")})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"turnId": "turn-1"}})
		case "windowsSandbox/readiness":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.WindowsSandboxReadinessResponse{
					Status: protocolv2.WindowsSandboxReadinessReady,
				})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"status": "ready"}})
		case "windowsSandbox/setupStart":
			if mode == "facade" {
				sendProtocolResult(id, protocolv2.WindowsSandboxSetupStartResponse{Started: true})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"started": false}})
		default:
			send(map[string]any{"id": id, "result": map[string]any{}})
		}
	}
}

func waitForFakePath(path string) {
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
}
func fakeCommandApprovalParams(threadID, turnID string) map[string]any {
	return map[string]any{
		"command":     "echo ok",
		"cwd":         "/tmp",
		"itemId":      "item-approval",
		"reason":      "fake approval",
		"startedAtMs": 123,
		"threadId":    threadID,
		"turnId":      turnID,
	}
}

func sendExactReroute(threadID, turnID, fromModel, toModel string) {
	send(map[string]any{"method": "model/rerouted", "params": map[string]any{
		"threadId":  threadID,
		"turnId":    turnID,
		"fromModel": fromModel,
		"toModel":   toModel,
		"reason":    "highRiskCyberActivity",
	}})
}

func fakeAuthRefreshParams() map[string]any {
	return map[string]any{
		"reason": "unauthorized",
	}
}

func fakeDynamicToolCallParams(threadID, turnID string) map[string]any {
	return map[string]any{
		"arguments": map[string]any{"query": "status"},
		"callId":    "call-tool",
		"threadId":  threadID,
		"tool":      "lookup",
		"turnId":    turnID,
	}
}

func fakeToolUserInputParams(threadID, turnID string) map[string]any {
	return map[string]any{
		"itemId": "item-input",
		"questions": []any{
			map[string]any{
				"header":   "Confirm",
				"id":       "q1",
				"question": "Continue?",
			},
		},
		"threadId": threadID,
		"turnId":   turnID,
	}
}

func fakeMCPElicitationParams(threadID, turnID string) map[string]any {
	return map[string]any{
		"serverName": "mcp-server",
		"threadId":   threadID,
		"turnId":     turnID,
	}
}

func jsonValuePtr(value protocolv2.JSONValue) *protocolv2.JSONValue {
	return &value
}

func sendProtocolResult(id any, result any) {
	raw, err := json.Marshal(result)
	if err != nil {
		panic(err)
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		panic(err)
	}
	send(map[string]any{"id": id, "result": decoded})
}

func facadeThreadStartResponse(threadID string, model string) protocolv2.ThreadStartResponse {
	return protocolv2.ThreadStartResponse{
		ApprovalPolicy:    protocolv2.NewAskForApprovalNever(),
		ApprovalsReviewer: protocolv2.ApprovalsReviewerUser,
		CWD:               "/workspace/facade",
		Model:             defaultString(model, "gpt-facade"),
		ModelProvider:     "openai",
		Sandbox:           protocolv2.NewSandboxPolicyReadOnly(protocolv2.SandboxPolicyReadOnly{}),
		Thread:            facadeThread(threadID, nil),
	}
}

func facadeThreadResumeResponse(threadID string, model string) protocolv2.ThreadResumeResponse {
	return protocolv2.ThreadResumeResponse{
		ApprovalPolicy:    protocolv2.NewAskForApprovalNever(),
		ApprovalsReviewer: protocolv2.ApprovalsReviewerUser,
		CWD:               "/workspace/facade",
		Model:             defaultString(model, "gpt-facade"),
		ModelProvider:     "openai",
		Sandbox:           protocolv2.NewSandboxPolicyReadOnly(protocolv2.SandboxPolicyReadOnly{}),
		Thread:            facadeThread(threadID, nil),
	}
}

func facadeThreadForkResponse(threadID string, parentThreadID string, model string) protocolv2.ThreadForkResponse {
	return protocolv2.ThreadForkResponse{
		ApprovalPolicy:    protocolv2.NewAskForApprovalNever(),
		ApprovalsReviewer: protocolv2.ApprovalsReviewerUser,
		CWD:               "/workspace/facade",
		Model:             defaultString(model, "gpt-facade"),
		ModelProvider:     "openai",
		Sandbox:           protocolv2.NewSandboxPolicyReadOnly(protocolv2.SandboxPolicyReadOnly{}),
		Thread:            facadeThread(threadID, &parentThreadID),
	}
}

func facadeThreadGoal(threadID string, objective string, status protocolv2.ThreadGoalStatus, tokenBudget *protocolv2.Nullable[int64]) protocolv2.ThreadGoal {
	return protocolv2.ThreadGoal{
		CreatedAt:       1000,
		Objective:       objective,
		Status:          status,
		ThreadID:        threadID,
		TimeUsedSeconds: 3,
		TokenBudget:     tokenBudget,
		TokensUsed:      42,
		UpdatedAt:       2000,
	}
}

func facadeThreadTurn(turnID string, items []protocolv2.ThreadItem) protocolv2.Turn {
	view := protocolv2.TurnItemsViewFull
	return protocolv2.Turn{
		CompletedAt: protocolv2.Value(int64(2000)),
		DurationMS:  protocolv2.Value(int64(1000)),
		ID:          turnID,
		Items:       items,
		ItemsView:   &view,
		StartedAt:   protocolv2.Value(int64(1000)),
		Status:      protocolv2.TurnStatusCompleted,
	}
}

func facadeThreadAgentMessageItem(itemID string, text string) protocolv2.ThreadItem {
	return protocolv2.NewThreadItemAgentMessage(protocolv2.ThreadItemAgentMessage{
		ID:   itemID,
		Text: text,
	})
}

func facadeRealtimeAudioChunk() protocolv2.ThreadRealtimeAudioChunk {
	return protocolv2.ThreadRealtimeAudioChunk{
		Data:              "base64",
		ItemID:            protocolv2.Value("item-1"),
		NumChannels:       2,
		SampleRate:        24000,
		SamplesPerChannel: protocolv2.Value(uint32(480)),
	}
}

func facadeConfigReadResponse() protocolv2.ConfigReadResponse {
	return protocolv2.ConfigReadResponse{
		Config: protocolv2.Config{
			Model: protocolv2.Value("gpt-5"),
		},
		Origins: map[string]protocolv2.ConfigLayerMetadata{
			"model": {
				Name: protocolv2.NewConfigLayerSourceUser(protocolv2.ConfigLayerSourceUser{
					File: "/config.toml",
				}),
				Version: "v1",
			},
		},
	}
}

func facadeConfigWriteResponse() protocolv2.ConfigWriteResponse {
	return protocolv2.ConfigWriteResponse{
		FilePath: "/home/user/.codex/config.toml",
		Status:   protocolv2.WriteStatusOk,
		Version:  "v2",
	}
}

func facadeConfigRequirementsReadResponse() protocolv2.ConfigRequirementsReadResponse {
	return protocolv2.ConfigRequirementsReadResponse{
		Requirements: protocolv2.Value(protocolv2.ConfigRequirements{
			FeatureRequirements: protocolv2.Value(map[string]bool{
				"alpha": true,
				"beta":  false,
			}),
			Network: protocolv2.Value(protocolv2.NetworkRequirements{
				Domains: protocolv2.Value(map[string]protocolv2.NetworkDomainPermission{
					"example.com": protocolv2.NetworkDomainPermissionAllow,
				}),
			}),
		}),
	}
}

func facadeExperimentalFeatureListResponse() protocolv2.ExperimentalFeatureListResponse {
	return protocolv2.ExperimentalFeatureListResponse{
		Data: []protocolv2.ExperimentalFeature{{
			Announcement:   protocolv2.Null[string](),
			DefaultEnabled: true,
			Description:    protocolv2.Value("Feature A"),
			DisplayName:    protocolv2.Value("Feature A"),
			Enabled:        false,
			Name:           "feature_a",
			Stage:          protocolv2.ExperimentalFeatureStageBeta,
		}},
		NextCursor: protocolv2.Value("cursor-2"),
	}
}

func facadeCollaborationModeListResponse() protocolv2.CollaborationModeListResponse {
	return protocolv2.CollaborationModeListResponse{
		Data: []protocolv2.CollaborationModeMask{{
			Mode:            protocolv2.Value(protocolv2.ModeKindPlan),
			Model:           protocolv2.Null[string](),
			Name:            "Plan",
			ReasoningEffort: protocolv2.Value(protocolv2.ReasoningEffort("medium")),
		}, {
			Name: "Default",
		}},
	}
}

func facadeExternalAgentConfigDetectResponse() protocolv2.ExternalAgentConfigDetectResponse {
	return protocolv2.ExternalAgentConfigDetectResponse{
		Items: []protocolv2.ExternalAgentConfigMigrationItem{{
			CWD:         protocolv2.Value("/repo"),
			Description: "Import command",
			Details: protocolv2.Value(protocolv2.MigrationDetails{
				Commands: &[]protocolv2.CommandMigration{{
					Name: "build",
				}},
			}),
			ItemType: protocolv2.ExternalAgentConfigMigrationItemTypeCOMMANDS,
		}},
	}
}

func facadeHooksListResponse() protocolv2.HooksListResponse {
	return protocolv2.HooksListResponse{
		Data: []protocolv2.HooksListEntry{{
			CWD: "/repo",
			Errors: []protocolv2.HookErrorInfo{{
				Message: "missing command",
				Path:    "/repo/.codex/hooks.json",
			}},
			Hooks: []protocolv2.HookMetadata{{
				Command:       protocolv2.Null[string](),
				CurrentHash:   "hash-1",
				DisplayOrder:  1,
				Enabled:       true,
				EventName:     protocolv2.HookEventNamePreToolUse,
				HandlerType:   protocolv2.HookHandlerTypeCommand,
				IsManaged:     false,
				Key:           "hook-1",
				Matcher:       protocolv2.Value("shell"),
				PluginID:      protocolv2.Null[string](),
				Source:        protocolv2.HookSourceProject,
				SourcePath:    "/repo/.codex/hooks.json",
				StatusMessage: protocolv2.Value("trusted"),
				TimeoutSec:    10,
				TrustStatus:   protocolv2.HookTrustStatusTrusted,
			}},
			Warnings: []string{"review hook"},
		}},
	}
}

func facadeReviewStartResponse() protocolv2.ReviewStartResponse {
	return protocolv2.ReviewStartResponse{
		ReviewThreadID: "review-thread-1",
		Turn: protocolv2.Turn{
			ID:     "turn-review-1",
			Items:  []protocolv2.ThreadItem{},
			Status: protocolv2.TurnStatusInProgress,
		},
	}
}

func facadeSkillsListResponse() protocolv2.SkillsListResponse {
	return protocolv2.SkillsListResponse{
		Data: []protocolv2.SkillsListEntry{{
			CWD: "/repo",
			Errors: []protocolv2.SkillErrorInfo{{
				Message: "invalid skill",
				Path:    "/repo/.codex/skills/bad/SKILL.md",
			}},
			Skills: []protocolv2.SkillMetadata{{
				Dependencies: protocolv2.Value(protocolv2.SkillDependencies{
					Tools: []protocolv2.SkillToolDependency{{
						Command:     protocolv2.Value("rg"),
						Description: protocolv2.Null[string](),
						Transport:   protocolv2.Null[string](),
						Type:        "command",
						URL:         protocolv2.Null[string](),
						Value:       "rg",
					}},
				}),
				Description: "review docs",
				Enabled:     true,
				Interface: protocolv2.Value(protocolv2.SkillInterface{
					DisplayName:      protocolv2.Value("Review"),
					ShortDescription: protocolv2.Null[string](),
				}),
				Name:             "review",
				Path:             "/repo/.codex/skills/review/SKILL.md",
				Scope:            protocolv2.SkillScopeRepo,
				ShortDescription: protocolv2.Value("Review docs"),
			}},
		}},
	}
}

func facadePluginSummary() protocolv2.PluginSummary {
	availability := protocolv2.PluginAvailabilityAVAILABLE
	keywords := []string{"featured"}
	return protocolv2.PluginSummary{
		AuthPolicy:    protocolv2.PluginAuthPolicyONUSE,
		Availability:  &availability,
		Enabled:       true,
		ID:            "plugin-1",
		InstallPolicy: protocolv2.PluginInstallPolicyAVAILABLE,
		Installed:     true,
		Interface: protocolv2.Value(protocolv2.PluginInterface{
			Capabilities:   []string{"skills"},
			ScreenshotURLs: []string{},
			Screenshots:    []string{},
		}),
		Keywords: &keywords,
		Name:     "plugin-one",
		ShareContext: protocolv2.Value(protocolv2.PluginShareContext{
			RemotePluginID: "remote-1",
			SharePrincipals: protocolv2.Value([]protocolv2.PluginSharePrincipal{{
				Name:          "User One",
				PrincipalID:   "user-1",
				PrincipalType: protocolv2.PluginSharePrincipalTypeUser,
				Role:          protocolv2.PluginSharePrincipalRoleOwner,
			}}),
			ShareURL: protocolv2.Value("https://example.test/share"),
		}),
		Source: protocolv2.NewPluginSourceRemote(),
	}
}

func facadePluginDetail() protocolv2.PluginDetail {
	return protocolv2.PluginDetail{
		AppTemplates: []protocolv2.AppTemplateSummary{},
		Apps: []protocolv2.AppSummary{{
			ID:   "app-1",
			Name: "App One",
		}},
		Hooks: []protocolv2.PluginHookSummary{{
			EventName: protocolv2.HookEventNamePreToolUse,
			Key:       "hook-1",
		}},
		MarketplaceName: "market",
		MarketplacePath: protocolv2.Null[string](),
		MCPServers:      []string{"mcp-1"},
		Skills: []protocolv2.SkillSummary{{
			Description: "review",
			Enabled:     true,
			Interface:   protocolv2.Value(protocolv2.SkillInterface{}),
			Name:        "review",
		}},
		Summary: facadePluginSummary(),
	}
}

func facadeThread(threadID string, forkedFromID *string) protocolv2.Thread {
	forkedFrom := protocolv2.Null[string]()
	if forkedFromID != nil {
		forkedFrom = protocolv2.Value(*forkedFromID)
	}
	return protocolv2.Thread{
		AgentNickname: protocolv2.Null[string](),
		AgentRole:     protocolv2.Null[string](),
		CliVersion:    "0.0.0-test",
		CreatedAt:     1000,
		CWD:           "/workspace/facade",
		Ephemeral:     false,
		ForkedFromID:  forkedFrom,
		GitInfo:       protocolv2.Null[protocolv2.GitInfo](),
		ID:            threadID,
		ModelProvider: "openai",
		Name:          protocolv2.Null[string](),
		Path:          protocolv2.Null[string](),
		Preview:       "facade preview",
		SessionID:     "session-" + threadID,
		Source:        protocolv2.NewSessionSourceAppServer(),
		Status:        protocolv2.NewThreadStatusIdle(),
		ThreadSource:  protocolv2.Value(protocolv2.ThreadSource("user")),
		Turns:         []protocolv2.Turn{},
		UpdatedAt:     2000,
	}
}

func completeTurn(threadID, turnID string) {
	item := map[string]any{"id": "item-" + turnID, "type": "agentMessage", "text": "final-" + turnID, "phase": "final_answer"}
	send(map[string]any{"method": "item/completed", "params": map[string]any{"completedAtMs": 1234, "threadId": threadID, "turnId": turnID, "item": item}})
	send(map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": threadID, "turnId": turnID, "tokenUsage": map[string]any{"last": fakeTokenUsageBreakdown(3, 1, 2, 1, 5), "total": fakeTokenUsageBreakdown(30, 10, 20, 5, 50)}}})
	send(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": threadID, "turn": map[string]any{"id": turnID, "status": "completed", "items": []map[string]any{item}}}})
}

func fakeTokenUsageBreakdown(inputTokens, cachedInputTokens, outputTokens, reasoningOutputTokens, totalTokens int) map[string]any {
	return map[string]any{
		"cachedInputTokens":     cachedInputTokens,
		"inputTokens":           inputTokens,
		"outputTokens":          outputTokens,
		"reasoningOutputTokens": reasoningOutputTokens,
		"totalTokens":           totalTokens,
	}
}

func completeLongTurn(threadID, turnID string) {
	longText := strings.Repeat("x", 70*1024)
	send(map[string]any{"method": "item/completed", "params": map[string]any{"completedAtMs": 1234, "threadId": threadID, "turnId": turnID, "item": map[string]any{"id": "item-" + turnID, "type": "agentMessage", "text": longText, "phase": "final_answer"}}})
	send(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": threadID, "turn": map[string]any{"id": turnID, "status": "completed", "items": []map[string]any{}}}})
}

func send(message map[string]any) {
	raw, _ := json.Marshal(message)
	os.Stdout.Write(append(raw, '\n'))
}

func appendRecord(path string, record map[string]any) {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	raw, _ := json.Marshal(record)
	file.Write(append(raw, '\n'))
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return wd
}

func itoa(value int) string {
	return strconv.Itoa(value)
}

type recordingWriteCloser struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *recordingWriteCloser) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *recordingWriteCloser) Close() error {
	return nil
}

func (w *recordingWriteCloser) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buf.Bytes()...)
}

func (w *recordingWriteCloser) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}
