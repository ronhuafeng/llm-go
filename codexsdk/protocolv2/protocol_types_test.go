package protocolv2

import (
	"encoding/json"
	"strings"
	"testing"
)

func permissionGrantScopePtr(value PermissionGrantScope) *PermissionGrantScope {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func sampleGeneratedHookRunSummary() HookRunSummary {
	return HookRunSummary{
		DisplayOrder: 1,
		Entries: []HookOutputEntry{{
			Kind: HookOutputEntryKindWarning,
			Text: "warn",
		}},
		EventName:     HookEventNamePreToolUse,
		ExecutionMode: HookExecutionModeSync,
		HandlerType:   HookHandlerTypeCommand,
		ID:            "hook-run-1",
		Scope:         HookScopeTurn,
		SourcePath:    "/workspace/.codex/hooks.json",
		StartedAt:     1000,
		Status:        HookRunStatusRunning,
	}
}

func sampleGeneratedThreadGoal() ThreadGoal {
	return ThreadGoal{
		CreatedAt:       1000,
		Objective:       "ship sdk",
		Status:          ThreadGoalStatusActive,
		ThreadID:        "thread-1",
		TimeUsedSeconds: 12,
		TokenBudget:     Value(int64(100000)),
		TokensUsed:      42,
		UpdatedAt:       2000,
	}
}

func sampleGeneratedThread() Thread {
	return Thread{
		AgentNickname: Value("worker"),
		AgentRole:     Null[string](),
		CliVersion:    "0.0.0-test",
		CreatedAt:     1000,
		CWD:           "/workspace",
		Ephemeral:     false,
		ForkedFromID:  Null[string](),
		GitInfo: Value(GitInfo{
			Branch:    Value("main"),
			OriginURL: Null[string](),
			SHA:       Value("abcdef"),
		}),
		ID:            "thread-1",
		ModelProvider: "openai",
		Name:          Value("Thread One"),
		Path:          Null[string](),
		Preview:       "preview",
		SessionID:     "session-1",
		Source:        NewSessionSourceAppServer(),
		Status:        NewThreadStatusIdle(),
		ThreadSource:  Value(ThreadSource("user")),
		Turns: []Turn{{
			ID:     "turn-1",
			Items:  []ThreadItem{NewThreadItemAgentMessage(ThreadItemAgentMessage{ID: "item-1", Text: "done"})},
			Status: TurnStatusCompleted,
		}},
		UpdatedAt: 2000,
	}
}

func TestGeneratedThreadArchiveParamsMarshal(t *testing.T) {
	raw, err := json.Marshal(ThreadArchiveParams{
		ThreadID: "thread-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"threadId":"thread-1"}`; got != want {
		t.Fatalf("ThreadArchiveParams JSON = %s, want %s", got, want)
	}
}

func TestGeneratedThreadTurnLifecycleParamsProtocolMarshalAndUnmarshal(t *testing.T) {
	schema, err := OutputSchemaFromJSON([]byte(`{"type":"object","required":["answer"],"properties":{"answer":{"type":"string"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	textElements := []TextElement{{
		ByteRange:   ByteRange{Start: 0, End: 5},
		Placeholder: Value("selection"),
	}}
	turnRaw, err := json.Marshal(TurnStartParams{
		ThreadID: "thread-1",
		Input: []UserInput{
			NewUserInputText(UserInputText{Text: "hello", TextElements: &textElements}),
			NewUserInputLocalImage(UserInputLocalImage{Path: "/tmp/screenshot.png"}),
			NewUserInputMention(UserInputMention{Name: "README.md", Path: "/workspace/README.md"}),
		},
		OutputSchema: &schema,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantTurn := `{"input":[{"text":"hello","text_elements":[{"byteRange":{"end":5,"start":0},"placeholder":"selection"}],"type":"text"},{"path":"/tmp/screenshot.png","type":"localImage"},{"name":"README.md","path":"/workspace/README.md","type":"mention"}],"outputSchema":{"properties":{"answer":{"type":"string"}},"required":["answer"],"type":"object"},"threadId":"thread-1"}`
	if got := string(turnRaw); got != wantTurn {
		t.Fatalf("TurnStartParams JSON = %s, want %s", got, wantTurn)
	}

	var decodedTurn TurnStartParams
	if err := json.Unmarshal(turnRaw, &decodedTurn); err != nil {
		t.Fatal(err)
	}
	if decodedTurn.ThreadID != "thread-1" || len(decodedTurn.Input) != 3 {
		t.Fatalf("decoded TurnStartParams = %#v", decodedTurn)
	}
	text, ok := decodedTurn.Input[0].AsText()
	if !ok || text.Text != "hello" || text.TextElements == nil || len(*text.TextElements) != 1 {
		t.Fatalf("decoded text input = %#v, ok=%t", text, ok)
	}
	if text.TextElements == nil || (*text.TextElements)[0].Placeholder == nil || (*text.TextElements)[0].Placeholder.Value == nil || *(*text.TextElements)[0].Placeholder.Value != "selection" {
		t.Fatalf("decoded text elements = %#v", text.TextElements)
	}
	mention, ok := decodedTurn.Input[2].AsMention()
	if !ok || mention.Name != "README.md" {
		t.Fatalf("decoded mention input = %#v, ok=%t", mention, ok)
	}
	if decodedTurn.OutputSchema == nil || !decodedTurn.OutputSchema.IsValid() {
		t.Fatalf("decoded output schema = %#v", decodedTurn.OutputSchema)
	}

	booleanSchema, err := OutputSchemaFromJSON([]byte(`true`))
	if err != nil {
		t.Fatal(err)
	}
	booleanSchemaRaw, err := json.Marshal(TurnStartParams{
		ThreadID:     "thread-1",
		Input:        []UserInput{NewUserInputText(UserInputText{Text: "boolean schema"})},
		OutputSchema: &booleanSchema,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantBooleanSchema := `{"input":[{"text":"boolean schema","type":"text"}],"outputSchema":true,"threadId":"thread-1"}`
	if got := string(booleanSchemaRaw); got != wantBooleanSchema {
		t.Fatalf("TurnStartParams boolean outputSchema JSON = %s, want %s", got, wantBooleanSchema)
	}
	var decodedBooleanSchema TurnStartParams
	if err := json.Unmarshal(booleanSchemaRaw, &decodedBooleanSchema); err != nil {
		t.Fatal(err)
	}
	if decodedBooleanSchema.OutputSchema == nil {
		t.Fatal("decoded boolean output schema is nil")
	}
	if got, ok := decodedBooleanSchema.OutputSchema.JSONValue().AsBool(); !ok || !got {
		t.Fatalf("decoded boolean output schema = %#v, ok=%t", got, ok)
	}

	forkRaw, err := json.Marshal(ThreadForkParams{
		ThreadID: "thread-1",
		Config: Value(map[string]JSONValue{
			"model": JSONString("gpt-5"),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(forkRaw), `{"config":{"model":"gpt-5"},"threadId":"thread-1"}`; got != want {
		t.Fatalf("ThreadForkParams JSON = %s, want %s", got, want)
	}

	resumeHistory := []ResponseItem{
		NewResponseItemMessage(ResponseItemMessage{
			Content: []ContentItem{
				NewContentItemInputText(ContentItemInputText{Text: "resume"}),
			},
			Phase: Value(MessagePhaseFinalAnswer),
			Role:  "user",
		}),
		NewResponseItemFunctionCallOutput(ResponseItemFunctionCallOutput{
			CallID: "call-1",
			Output: NewFunctionCallOutputBodyArray([]FunctionCallOutputContentItem{
				NewFunctionCallOutputContentItemInputText(FunctionCallOutputContentItemInputText{Text: "ok"}),
			}),
		}),
		NewResponseItemToolSearchCall(ResponseItemToolSearchCall{
			Arguments: JSONObject(map[string]JSONValue{"query": JSONString("docs")}),
			Execution: "search",
		}),
	}
	resumeRaw, err := json.Marshal(ThreadResumeParams{
		Config:   Value(map[string]JSONValue{"model": JSONString("gpt-5")}),
		History:  Value(resumeHistory),
		ThreadID: "thread-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantResume := `{"config":{"model":"gpt-5"},"history":[{"content":[{"text":"resume","type":"input_text"}],"phase":"final_answer","role":"user","type":"message"},{"call_id":"call-1","output":[{"text":"ok","type":"input_text"}],"type":"function_call_output"},{"arguments":{"query":"docs"},"execution":"search","type":"tool_search_call"}],"threadId":"thread-1"}`
	if got := string(resumeRaw); got != wantResume {
		t.Fatalf("ThreadResumeParams JSON = %s, want %s", got, wantResume)
	}
	var decodedResume ThreadResumeParams
	if err := json.Unmarshal(resumeRaw, &decodedResume); err != nil {
		t.Fatal(err)
	}
	if decodedResume.History == nil || decodedResume.History.Value == nil || len(*decodedResume.History.Value) != 3 {
		t.Fatalf("decoded resume history = %#v", decodedResume.History)
	}
	decodedMessage, ok := (*decodedResume.History.Value)[0].AsMessage()
	if !ok || decodedMessage.Role != "user" || decodedMessage.Phase == nil {
		t.Fatalf("decoded resume message = %#v, ok=%t", decodedMessage, ok)
	}
	if *decodedMessage.Phase.Value != MessagePhaseFinalAnswer {
		t.Fatalf("decoded resume message phase = %#v, ok=%t", decodedMessage.Phase.Value, ok)
	}
	decodedOutput, ok := (*decodedResume.History.Value)[1].AsFunctionCallOutput()
	if !ok {
		t.Fatalf("decoded function call output = %#v, ok=%t", decodedOutput, ok)
	}
	outputItems, ok := decodedOutput.Output.AsArray()
	if !ok || len(outputItems) != 1 {
		t.Fatalf("decoded function call output body = %#v, ok=%t", decodedOutput.Output, ok)
	}
	decodedToolSearch, ok := (*decodedResume.History.Value)[2].AsToolSearchCall()
	if !ok || decodedToolSearch.Arguments.Kind() != JSONKindObject {
		t.Fatalf("decoded tool search call = %#v, ok=%t", decodedToolSearch, ok)
	}
}

func TestGeneratedTurnStartResponseProtocolMarshalAndUnmarshal(t *testing.T) {
	structuredContent := JSONObject(map[string]JSONValue{"answer": JSONString("done")})
	turnRaw, err := json.Marshal(TurnStartResponse{
		Turn: Turn{
			ID: "turn-1",
			Items: []ThreadItem{
				NewThreadItemAgentMessage(ThreadItemAgentMessage{
					ID:    "item-1",
					Phase: Value(MessagePhaseFinalAnswer),
					Text:  "final",
				}),
				NewThreadItemMCPToolCall(ThreadItemMCPToolCall{
					Arguments: JSONObject(map[string]JSONValue{"query": JSONString("docs")}),
					ID:        "item-2",
					Result: Value(McpToolCallResult{
						Content:           []JSONValue{JSONString("ok")},
						StructuredContent: &structuredContent,
					}),
					Server: "docs",
					Status: McpToolCallStatusCompleted,
					Tool:   "search",
				}),
			},
			Status: TurnStatusCompleted,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"turn":{"id":"turn-1","items":[{"id":"item-1","phase":"final_answer","text":"final","type":"agentMessage"},{"arguments":{"query":"docs"},"id":"item-2","result":{"content":["ok"],"structuredContent":{"answer":"done"}},"server":"docs","status":"completed","tool":"search","type":"mcpToolCall"}],"status":"completed"}}`
	if got := string(turnRaw); got != want {
		t.Fatalf("TurnStartResponse JSON = %s, want %s", got, want)
	}

	var decoded TurnStartResponse
	if err := json.Unmarshal(turnRaw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Turn.ID != "turn-1" || decoded.Turn.Status != TurnStatusCompleted || len(decoded.Turn.Items) != 2 {
		t.Fatalf("decoded TurnStartResponse = %#v", decoded)
	}
	message, ok := decoded.Turn.Items[0].AsAgentMessage()
	if !ok || message.Phase == nil || message.Phase.Value == nil || message.Text != "final" {
		t.Fatalf("decoded agent message = %#v, ok=%t", message, ok)
	}
	if *message.Phase.Value != MessagePhaseFinalAnswer {
		t.Fatalf("decoded message phase = %#v", message.Phase.Value)
	}
	toolCall, ok := decoded.Turn.Items[1].AsMCPToolCall()
	if !ok || toolCall.Arguments.Kind() != JSONKindObject || toolCall.Result == nil || toolCall.Result.Value == nil {
		t.Fatalf("decoded MCP tool call = %#v, ok=%t", toolCall, ok)
	}
	if len(toolCall.Result.Value.Content) != 1 || toolCall.Result.Value.StructuredContent == nil || toolCall.Result.Value.StructuredContent.Kind() != JSONKindObject {
		t.Fatalf("decoded MCP tool call result = %#v", toolCall.Result.Value)
	}
}

func TestGeneratedTurnResponseCoreAdjacentPayloadsProtocolMarshalAndUnmarshal(t *testing.T) {
	item := NewThreadItemAgentMessage(ThreadItemAgentMessage{
		ID:   "item-1",
		Text: "done",
	})
	turn := Turn{
		ID:     "turn-1",
		Items:  []ThreadItem{item},
		Status: TurnStatusCompleted,
	}

	startedRaw, err := json.Marshal(TurnStartedNotification{
		ThreadID: "thread-1",
		Turn:     turn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(startedRaw), `{"threadId":"thread-1","turn":{"id":"turn-1","items":[{"id":"item-1","text":"done","type":"agentMessage"}],"status":"completed"}}`; got != want {
		t.Fatalf("TurnStartedNotification JSON = %s, want %s", got, want)
	}
	var decodedStarted TurnStartedNotification
	if err := json.Unmarshal(startedRaw, &decodedStarted); err != nil {
		t.Fatal(err)
	}
	if decodedStarted.ThreadID != "thread-1" || decodedStarted.Turn.ID != "turn-1" {
		t.Fatalf("decoded turn started notification = %#v", decodedStarted)
	}

	completedRaw, err := json.Marshal(TurnCompletedNotification{
		ThreadID: "thread-1",
		Turn:     turn,
	})
	if err != nil {
		t.Fatal(err)
	}
	var decodedCompleted TurnCompletedNotification
	if err := json.Unmarshal(completedRaw, &decodedCompleted); err != nil {
		t.Fatal(err)
	}
	if decodedCompleted.Turn.Status != TurnStatusCompleted {
		t.Fatalf("decoded turn completed notification = %#v", decodedCompleted)
	}

	itemStartedRaw, err := json.Marshal(ItemStartedNotification{
		Item:        item,
		StartedAtMS: 123,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	var decodedItemStarted ItemStartedNotification
	if err := json.Unmarshal(itemStartedRaw, &decodedItemStarted); err != nil {
		t.Fatal(err)
	}
	if decodedItemStarted.StartedAtMS != 123 || decodedItemStarted.Item.Kind() != ThreadItemKindAgentMessage {
		t.Fatalf("decoded item started notification = %#v", decodedItemStarted)
	}

	itemCompletedRaw, err := json.Marshal(ItemCompletedNotification{
		CompletedAtMS: 456,
		Item:          item,
		ThreadID:      "thread-1",
		TurnID:        "turn-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	var decodedItemCompleted ItemCompletedNotification
	if err := json.Unmarshal(itemCompletedRaw, &decodedItemCompleted); err != nil {
		t.Fatal(err)
	}
	if decodedItemCompleted.CompletedAtMS != 456 || decodedItemCompleted.Item.Kind() != ThreadItemKindAgentMessage {
		t.Fatalf("decoded item completed notification = %#v", decodedItemCompleted)
	}

	turnsListParamsRaw, err := json.Marshal(ThreadTurnsListParams{
		ItemsView:     Value(TurnItemsViewSummary),
		Limit:         Value(uint32(10)),
		SortDirection: Value(SortDirectionDesc),
		ThreadID:      "thread-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(turnsListParamsRaw), `{"itemsView":"summary","limit":10,"sortDirection":"desc","threadId":"thread-1"}`; got != want {
		t.Fatalf("ThreadTurnsListParams JSON = %s, want %s", got, want)
	}
	var decodedTurnsListParams ThreadTurnsListParams
	if err := json.Unmarshal(turnsListParamsRaw, &decodedTurnsListParams); err != nil {
		t.Fatal(err)
	}
	if decodedTurnsListParams.ItemsView == nil || decodedTurnsListParams.ItemsView.Value == nil || *decodedTurnsListParams.ItemsView.Value != TurnItemsViewSummary {
		t.Fatalf("decoded thread turns list params = %#v", decodedTurnsListParams)
	}

	turnsListRaw, err := json.Marshal(ThreadTurnsListResponse{
		Data:       []Turn{turn},
		NextCursor: Value("next"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var decodedTurnsList ThreadTurnsListResponse
	if err := json.Unmarshal(turnsListRaw, &decodedTurnsList); err != nil {
		t.Fatal(err)
	}
	if len(decodedTurnsList.Data) != 1 || decodedTurnsList.NextCursor == nil || decodedTurnsList.NextCursor.Value == nil || *decodedTurnsList.NextCursor.Value != "next" {
		t.Fatalf("decoded thread turns list response = %#v", decodedTurnsList)
	}

	itemsListRaw, err := json.Marshal(ThreadItemsListResponse{
		Data: []ThreadItem{item},
	})
	if err != nil {
		t.Fatal(err)
	}
	var decodedItemsList ThreadItemsListResponse
	if err := json.Unmarshal(itemsListRaw, &decodedItemsList); err != nil {
		t.Fatal(err)
	}
	if len(decodedItemsList.Data) != 1 || decodedItemsList.Data[0].Kind() != ThreadItemKindAgentMessage {
		t.Fatalf("decoded thread turn items list response = %#v", decodedItemsList)
	}

	errorRaw, err := json.Marshal(ErrorNotification{
		Error: TurnError{
			CodexErrorInfo: Value(NewCodexErrorInfoOther()),
			Message:        "failed",
		},
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		WillRetry: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var decodedError ErrorNotification
	if err := json.Unmarshal(errorRaw, &decodedError); err != nil {
		t.Fatal(err)
	}
	if decodedError.Error.CodexErrorInfo == nil || decodedError.Error.CodexErrorInfo.Value == nil || decodedError.Error.CodexErrorInfo.Value.Kind() != CodexErrorInfoKindOther {
		t.Fatalf("decoded error notification = %#v", decodedError)
	}

	fileChangeRaw, err := json.Marshal(FileChangePatchUpdatedNotification{
		Changes: []FileUpdateChange{{
			Diff: "@@",
			Kind: NewPatchChangeKindUpdate(PatchChangeKindUpdate{MovePath: Value("new.go")}),
			Path: "old.go",
		}},
		ItemID:   "item-2",
		ThreadID: "thread-1",
		TurnID:   "turn-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	var decodedFileChange FileChangePatchUpdatedNotification
	if err := json.Unmarshal(fileChangeRaw, &decodedFileChange); err != nil {
		t.Fatal(err)
	}
	if len(decodedFileChange.Changes) != 1 || decodedFileChange.Changes[0].Kind.Kind() != PatchChangeKindKindUpdate {
		t.Fatalf("decoded file change patch notification = %#v", decodedFileChange)
	}

	reviewRaw, err := json.Marshal(ReviewStartResponse{
		ReviewThreadID: "review-1",
		Turn:           turn,
	})
	if err != nil {
		t.Fatal(err)
	}
	var decodedReview ReviewStartResponse
	if err := json.Unmarshal(reviewRaw, &decodedReview); err != nil {
		t.Fatal(err)
	}
	if decodedReview.ReviewThreadID != "review-1" || decodedReview.Turn.ID != "turn-1" {
		t.Fatalf("decoded review start response = %#v", decodedReview)
	}
}

func TestGeneratedReviewStartParamsProtocolMarshalAndUnmarshal(t *testing.T) {
	for _, tc := range []struct {
		name     string
		value    ReviewStartParams
		want     string
		wantKind ReviewTargetKind
	}{
		{
			name: "uncommitted changes target",
			value: ReviewStartParams{
				Target:   NewReviewTargetUncommittedChanges(),
				ThreadID: "thread-1",
			},
			want:     `{"target":{"type":"uncommittedChanges"},"threadId":"thread-1"}`,
			wantKind: ReviewTargetKindUncommittedChanges,
		},
		{
			name: "base branch target with null delivery",
			value: ReviewStartParams{
				Delivery: Null[ReviewDelivery](),
				Target: NewReviewTargetBaseBranch(ReviewTargetBaseBranch{
					Branch: "main",
				}),
				ThreadID: "thread-1",
			},
			want:     `{"delivery":null,"target":{"branch":"main","type":"baseBranch"},"threadId":"thread-1"}`,
			wantKind: ReviewTargetKindBaseBranch,
		},
		{
			name: "commit target with detached delivery",
			value: ReviewStartParams{
				Delivery: Value(ReviewDeliveryDetached),
				Target: NewReviewTargetCommit(ReviewTargetCommit{
					SHA:   "abc123",
					Title: Null[string](),
				}),
				ThreadID: "thread-1",
			},
			want:     `{"delivery":"detached","target":{"sha":"abc123","title":null,"type":"commit"},"threadId":"thread-1"}`,
			wantKind: ReviewTargetKindCommit,
		},
		{
			name: "custom target with inline delivery",
			value: ReviewStartParams{
				Delivery: Value(ReviewDeliveryInline),
				Target: NewReviewTargetCustom(ReviewTargetCustom{
					Instructions: "review staged docs",
				}),
				ThreadID: "thread-1",
			},
			want:     `{"delivery":"inline","target":{"instructions":"review staged docs","type":"custom"},"threadId":"thread-1"}`,
			wantKind: ReviewTargetKindCustom,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(raw); got != tc.want {
				t.Fatalf("ReviewStartParams JSON = %s, want %s", got, tc.want)
			}
			var decoded ReviewStartParams
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.ThreadID != tc.value.ThreadID || decoded.Target.Kind() != tc.wantKind {
				t.Fatalf("decoded ReviewStartParams = %#v", decoded)
			}
		})
	}
}

func TestGeneratedThreadLifecycleResponsesProtocolMarshalAndUnmarshal(t *testing.T) {
	thread := sampleGeneratedThread()
	instructionSources := []string{"AGENTS.md"}
	response := ThreadStartResponse{
		ActivePermissionProfile: Value(ActivePermissionProfile{
			Extends: Null[string](),
			ID:      "profile-1",
		}),
		ApprovalPolicy:     NewAskForApprovalNever(),
		ApprovalsReviewer:  ApprovalsReviewerUser,
		CWD:                "/workspace",
		InstructionSources: &instructionSources,
		Model:              "gpt-5",
		ModelProvider:      "openai",
		ReasoningEffort:    Value(ReasoningEffort("high")),
		Sandbox:            NewSandboxPolicyReadOnly(SandboxPolicyReadOnly{NetworkAccess: boolPtr(true)}),
		ServiceTier:        Null[string](),
		Thread:             thread,
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		`"activePermissionProfile":{"extends":null,"id":"profile-1"}`,
		`"approvalPolicy":"never"`,
		`"approvalsReviewer":"user"`,
		`"instructionSources":["AGENTS.md"]`,
		`"reasoningEffort":"high"`,
		`"sandbox":{"networkAccess":true,"type":"readOnly"}`,
		`"serviceTier":null`,
		`"thread":{"agentNickname":"worker","agentRole":null`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("ThreadStartResponse JSON does not contain %s:\n%s", want, text)
		}
	}

	var decoded ThreadStartResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Thread.ID != "thread-1" || decoded.ActivePermissionProfile == nil || decoded.ActivePermissionProfile.Value == nil {
		t.Fatalf("decoded ThreadStartResponse = %#v", decoded)
	}
	if decoded.ServiceTier == nil || decoded.ServiceTier.Value != nil {
		t.Fatalf("decoded serviceTier = %#v, want explicit null", decoded.ServiceTier)
	}
	if decoded.Thread.GitInfo == nil || decoded.Thread.GitInfo.Value == nil || decoded.Thread.GitInfo.Value.SHA == nil {
		t.Fatalf("decoded thread gitInfo = %#v", decoded.Thread.GitInfo)
	}
	if decoded.Thread.Source.Kind() != SessionSourceKindAppServer || decoded.Thread.Status.Kind() != ThreadStatusKindIdle {
		t.Fatalf("decoded thread source/status = %s/%s", decoded.Thread.Source.Kind(), decoded.Thread.Status.Kind())
	}

	resumeRaw, err := json.Marshal(ThreadResumeResponse{
		ApprovalPolicy:    NewAskForApprovalOnRequest(),
		ApprovalsReviewer: ApprovalsReviewerAutoReview,
		CWD:               "/workspace",
		Model:             "gpt-5",
		ModelProvider:     "openai",
		Sandbox:           NewSandboxPolicyDangerFullAccess(),
		Thread:            thread,
	})
	if err != nil {
		t.Fatal(err)
	}
	var decodedResume ThreadResumeResponse
	if err := json.Unmarshal(resumeRaw, &decodedResume); err != nil {
		t.Fatal(err)
	}
	if decodedResume.Thread.ID != "thread-1" || decodedResume.ApprovalPolicy.Kind() != AskForApprovalKindOnRequest {
		t.Fatalf("decoded ThreadResumeResponse = %#v", decodedResume)
	}

	forkRaw, err := json.Marshal(ThreadForkResponse{
		ApprovalPolicy:    NewAskForApprovalUntrusted(),
		ApprovalsReviewer: ApprovalsReviewerGuardianSubagent,
		CWD:               "/workspace",
		Model:             "gpt-5",
		ModelProvider:     "openai",
		Sandbox:           NewSandboxPolicyWorkspaceWrite(SandboxPolicyWorkspaceWrite{WritableRoots: &[]string{"/workspace"}}),
		Thread:            thread,
	})
	if err != nil {
		t.Fatal(err)
	}
	var decodedFork ThreadForkResponse
	if err := json.Unmarshal(forkRaw, &decodedFork); err != nil {
		t.Fatal(err)
	}
	if decodedFork.Thread.ID != "thread-1" || decodedFork.Sandbox.Kind() != SandboxPolicyKindWorkspaceWrite {
		t.Fatalf("decoded ThreadForkResponse = %#v", decodedFork)
	}
}

func TestGeneratedThreadCoreAdjacentPayloadsProtocolMarshalAndUnmarshal(t *testing.T) {
	thread := sampleGeneratedThread()
	listRaw, err := json.Marshal(ThreadListResponse{
		BackwardsCursor: Null[string](),
		Data:            []Thread{thread},
		NextCursor:      Value("next"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var decodedList ThreadListResponse
	if err := json.Unmarshal(listRaw, &decodedList); err != nil {
		t.Fatal(err)
	}
	if len(decodedList.Data) != 1 || decodedList.Data[0].ID != "thread-1" || decodedList.NextCursor == nil || decodedList.NextCursor.Value == nil {
		t.Fatalf("decoded ThreadListResponse = %#v", decodedList)
	}
	if decodedList.BackwardsCursor == nil || decodedList.BackwardsCursor.Value != nil {
		t.Fatalf("decoded backwards cursor = %#v, want explicit null", decodedList.BackwardsCursor)
	}

	for _, tc := range []struct {
		name  string
		value any
	}{
		{name: "read", value: ThreadReadResponse{Thread: thread}},
		{name: "rollback", value: ThreadRollbackResponse{Thread: thread}},
		{name: "metadata update", value: ThreadMetadataUpdateResponse{Thread: thread}},
		{name: "unarchive", value: ThreadUnarchiveResponse{Thread: thread}},
		{name: "started notification", value: ThreadStartedNotification{Thread: thread}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), `"thread":{"agentNickname":"worker"`) {
				t.Fatalf("%s JSON = %s", tc.name, raw)
			}
		})
	}

	statusRaw, err := json.Marshal(ThreadStatusChangedNotification{
		Status:   NewThreadStatusActive(ThreadStatusActive{ActiveFlags: []ThreadActiveFlag{ThreadActiveFlagWaitingOnApproval}}),
		ThreadID: "thread-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(statusRaw), `{"status":{"activeFlags":["waitingOnApproval"],"type":"active"},"threadId":"thread-1"}`; got != want {
		t.Fatalf("ThreadStatusChangedNotification JSON = %s, want %s", got, want)
	}
	var decodedStatus ThreadStatusChangedNotification
	if err := json.Unmarshal(statusRaw, &decodedStatus); err != nil {
		t.Fatal(err)
	}
	active, ok := decodedStatus.Status.AsActive()
	if !ok || len(active.ActiveFlags) != 1 || active.ActiveFlags[0] != ThreadActiveFlagWaitingOnApproval {
		t.Fatalf("decoded ThreadStatusChangedNotification = %#v ok=%t", decodedStatus, ok)
	}
}

func TestGeneratedThreadControlPayloadsProtocolMarshalAndUnmarshal(t *testing.T) {
	goal := sampleGeneratedThreadGoal()
	goalJSON := `{"createdAt":1000,"objective":"ship sdk","status":"active","threadId":"thread-1","timeUsedSeconds":12,"tokenBudget":100000,"tokensUsed":42,"updatedAt":2000}`
	for _, tc := range []struct {
		name   string
		value  any
		target any
		want   string
	}{
		{name: "approve guardian denied action params", value: ThreadApproveGuardianDeniedActionParams{Event: JSONObject(map[string]JSONValue{"outcome": JSONString("deny")}), ThreadID: "thread-1"}, target: &ThreadApproveGuardianDeniedActionParams{}, want: `{"event":{"outcome":"deny"},"threadId":"thread-1"}`},
		{name: "approve guardian denied action response", value: ThreadApproveGuardianDeniedActionResponse{}, target: &ThreadApproveGuardianDeniedActionResponse{}, want: `{}`},
		{name: "archive params", value: ThreadArchiveParams{ThreadID: "thread-1"}, target: &ThreadArchiveParams{}, want: `{"threadId":"thread-1"}`},
		{name: "archive response", value: ThreadArchiveResponse{}, target: &ThreadArchiveResponse{}, want: `{}`},
		{name: "archived notification", value: ThreadArchivedNotification{ThreadID: "thread-1"}, target: &ThreadArchivedNotification{}, want: `{"threadId":"thread-1"}`},
		{name: "background terminals clean params", value: ThreadBackgroundTerminalsCleanParams{ThreadID: "thread-1"}, target: &ThreadBackgroundTerminalsCleanParams{}, want: `{"threadId":"thread-1"}`},
		{name: "background terminals clean response", value: ThreadBackgroundTerminalsCleanResponse{}, target: &ThreadBackgroundTerminalsCleanResponse{}, want: `{}`},
		{name: "closed notification", value: ThreadClosedNotification{ThreadID: "thread-1"}, target: &ThreadClosedNotification{}, want: `{"threadId":"thread-1"}`},
		{name: "compact start params", value: ThreadCompactStartParams{ThreadID: "thread-1"}, target: &ThreadCompactStartParams{}, want: `{"threadId":"thread-1"}`},
		{name: "compact start response", value: ThreadCompactStartResponse{}, target: &ThreadCompactStartResponse{}, want: `{}`},
		{name: "decrement elicitation params", value: ThreadDecrementElicitationParams{ThreadID: "thread-1"}, target: &ThreadDecrementElicitationParams{}, want: `{"threadId":"thread-1"}`},
		{name: "decrement elicitation response", value: ThreadDecrementElicitationResponse{Count: 2, Paused: false}, target: &ThreadDecrementElicitationResponse{}, want: `{"count":2,"paused":false}`},
		{name: "goal clear params", value: ThreadGoalClearParams{ThreadID: "thread-1"}, target: &ThreadGoalClearParams{}, want: `{"threadId":"thread-1"}`},
		{name: "goal clear response", value: ThreadGoalClearResponse{Cleared: true}, target: &ThreadGoalClearResponse{}, want: `{"cleared":true}`},
		{name: "goal cleared notification", value: ThreadGoalClearedNotification{ThreadID: "thread-1"}, target: &ThreadGoalClearedNotification{}, want: `{"threadId":"thread-1"}`},
		{name: "goal get params", value: ThreadGoalGetParams{ThreadID: "thread-1"}, target: &ThreadGoalGetParams{}, want: `{"threadId":"thread-1"}`},
		{name: "goal get response value", value: ThreadGoalGetResponse{Goal: Value(goal)}, target: &ThreadGoalGetResponse{}, want: `{"goal":` + goalJSON + `}`},
		{name: "goal get response null", value: ThreadGoalGetResponse{Goal: Null[ThreadGoal]()}, target: &ThreadGoalGetResponse{}, want: `{"goal":null}`},
		{name: "goal set response", value: ThreadGoalSetResponse{Goal: goal}, target: &ThreadGoalSetResponse{}, want: `{"goal":` + goalJSON + `}`},
		{name: "increment elicitation params", value: ThreadIncrementElicitationParams{ThreadID: "thread-1"}, target: &ThreadIncrementElicitationParams{}, want: `{"threadId":"thread-1"}`},
		{name: "increment elicitation response", value: ThreadIncrementElicitationResponse{Count: 3, Paused: true}, target: &ThreadIncrementElicitationResponse{}, want: `{"count":3,"paused":true}`},
		{name: "inject items params", value: ThreadInjectItemsParams{Items: []JSONValue{JSONString("item")}, ThreadID: "thread-1"}, target: &ThreadInjectItemsParams{}, want: `{"items":["item"],"threadId":"thread-1"}`},
		{name: "inject items response", value: ThreadInjectItemsResponse{}, target: &ThreadInjectItemsResponse{}, want: `{}`},
		{name: "list params cwd string", value: ThreadListParams{Archived: Null[bool](), Cursor: Value("cursor-1"), CWD: Value(NewThreadListCwdFilterString("/repo")), Limit: Value(uint32(10)), ModelProviders: Value([]string{"openai"}), SearchTerm: Value("review"), SortDirection: Value(SortDirectionDesc), SortKey: Value(ThreadSortKeyUpdatedAt), SourceKinds: Value([]ThreadSourceKind{ThreadSourceKindAppServer}), UseStateDbOnly: boolPtr(true)}, target: &ThreadListParams{}, want: `{"archived":null,"cursor":"cursor-1","cwd":"/repo","limit":10,"modelProviders":["openai"],"searchTerm":"review","sortDirection":"desc","sortKey":"updated_at","sourceKinds":["appServer"],"useStateDbOnly":true}`},
		{name: "list params cwd array", value: ThreadListParams{CWD: Value(NewThreadListCwdFilterArray([]string{"/repo", "/work"}))}, target: &ThreadListParams{}, want: `{"cwd":["/repo","/work"]}`},
		{name: "list response", value: ThreadListResponse{BackwardsCursor: Null[string](), Data: []Thread{}, NextCursor: Value("next")}, target: &ThreadListResponse{}, want: `{"backwardsCursor":null,"data":[],"nextCursor":"next"}`},
		{name: "loaded list params", value: ThreadLoadedListParams{Cursor: Null[string](), Limit: Value(uint32(10))}, target: &ThreadLoadedListParams{}, want: `{"cursor":null,"limit":10}`},
		{name: "loaded list response", value: ThreadLoadedListResponse{Data: []string{"thread-1"}, NextCursor: Value("next")}, target: &ThreadLoadedListResponse{}, want: `{"data":["thread-1"],"nextCursor":"next"}`},
		{name: "memory mode set params", value: ThreadMemoryModeSetParams{Mode: ThreadMemoryModeEnabled, ThreadID: "thread-1"}, target: &ThreadMemoryModeSetParams{}, want: `{"mode":"enabled","threadId":"thread-1"}`},
		{name: "memory mode set response", value: ThreadMemoryModeSetResponse{}, target: &ThreadMemoryModeSetResponse{}, want: `{}`},
		{name: "metadata git info update params", value: ThreadMetadataGitInfoUpdateParams{Branch: Value("main"), OriginURL: Null[string](), SHA: Value("abc")}, target: &ThreadMetadataGitInfoUpdateParams{}, want: `{"branch":"main","originUrl":null,"sha":"abc"}`},
		{name: "metadata update params", value: ThreadMetadataUpdateParams{GitInfo: Value(ThreadMetadataGitInfoUpdateParams{Branch: Value("main"), OriginURL: Null[string](), SHA: Value("abc")}), ThreadID: "thread-1"}, target: &ThreadMetadataUpdateParams{}, want: `{"gitInfo":{"branch":"main","originUrl":null,"sha":"abc"},"threadId":"thread-1"}`},
		{name: "name updated notification", value: ThreadNameUpdatedNotification{ThreadID: "thread-1", ThreadName: Null[string]()}, target: &ThreadNameUpdatedNotification{}, want: `{"threadId":"thread-1","threadName":null}`},
		{name: "read params", value: ThreadReadParams{IncludeTurns: boolPtr(true), ThreadID: "thread-1"}, target: &ThreadReadParams{}, want: `{"includeTurns":true,"threadId":"thread-1"}`},
		{name: "set name params", value: ThreadSetNameParams{Name: "Renamed", ThreadID: "thread-1"}, target: &ThreadSetNameParams{}, want: `{"name":"Renamed","threadId":"thread-1"}`},
		{name: "set name response", value: ThreadSetNameResponse{}, target: &ThreadSetNameResponse{}, want: `{}`},
		{name: "shell command params", value: ThreadShellCommandParams{Command: "echo ok", ThreadID: "thread-1"}, target: &ThreadShellCommandParams{}, want: `{"command":"echo ok","threadId":"thread-1"}`},
		{name: "shell command response", value: ThreadShellCommandResponse{}, target: &ThreadShellCommandResponse{}, want: `{}`},
		{name: "unarchive params", value: ThreadUnarchiveParams{ThreadID: "thread-1"}, target: &ThreadUnarchiveParams{}, want: `{"threadId":"thread-1"}`},
		{name: "unarchived notification", value: ThreadUnarchivedNotification{ThreadID: "thread-1"}, target: &ThreadUnarchivedNotification{}, want: `{"threadId":"thread-1"}`},
		{name: "unsubscribe params", value: ThreadUnsubscribeParams{ThreadID: "thread-1"}, target: &ThreadUnsubscribeParams{}, want: `{"threadId":"thread-1"}`},
		{name: "unsubscribe response", value: ThreadUnsubscribeResponse{Status: ThreadUnsubscribeStatusUnsubscribed}, target: &ThreadUnsubscribeResponse{}, want: `{"status":"unsubscribed"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(raw); got != tc.want {
				t.Fatalf("%s JSON = %s, want %s", tc.name, got, tc.want)
			}
			if err := json.Unmarshal(raw, tc.target); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGeneratedThreadControlPayloadsRejectMalformedProtocol(t *testing.T) {
	var archiveResponse ThreadArchiveResponse
	err := json.Unmarshal([]byte(`{"extra":true}`), &archiveResponse)
	if err == nil {
		t.Fatal("expected unknown empty response field to fail")
	}
	if !strings.Contains(err.Error(), `decode ThreadArchiveResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown response field error: %v", err)
	}

	var inject ThreadInjectItemsParams
	err = json.Unmarshal([]byte(`{"threadId":"thread-1"}`), &inject)
	if err == nil {
		t.Fatal("expected missing inject items to fail")
	}
	if !strings.Contains(err.Error(), "decode ThreadInjectItemsParams.items: missing required field") {
		t.Fatalf("unexpected missing inject items error: %v", err)
	}

	_, err = json.Marshal(ThreadInjectItemsParams{ThreadID: "thread-1"})
	if err == nil {
		t.Fatal("expected nil inject items to fail")
	}
	if !strings.Contains(err.Error(), "encode ThreadInjectItemsParams.items: nil is not allowed") {
		t.Fatalf("unexpected nil inject items error: %v", err)
	}

	_, err = json.Marshal(ThreadLoadedListResponse{})
	if err == nil {
		t.Fatal("expected nil loaded list response data to fail")
	}
	if !strings.Contains(err.Error(), "encode ThreadLoadedListResponse.data: nil is not allowed") {
		t.Fatalf("unexpected nil loaded list data error: %v", err)
	}

	_, err = json.Marshal(ThreadApproveGuardianDeniedActionParams{ThreadID: "thread-1"})
	if err == nil {
		t.Fatal("expected invalid guardian event JSONValue marshal to fail")
	}
	if !strings.Contains(err.Error(), "invalid JSONValue") {
		t.Fatalf("unexpected invalid guardian event error: %v", err)
	}

	var guardian ThreadApproveGuardianDeniedActionParams
	err = json.Unmarshal([]byte(`{"threadId":"thread-1"}`), &guardian)
	if err == nil {
		t.Fatal("expected missing guardian event to fail")
	}
	if !strings.Contains(err.Error(), "decode ThreadApproveGuardianDeniedActionParams.event: missing required field") {
		t.Fatalf("unexpected missing guardian event error: %v", err)
	}

	var goalSet ThreadGoalSetResponse
	err = json.Unmarshal([]byte(`{}`), &goalSet)
	if err == nil {
		t.Fatal("expected missing thread goal set response goal to fail")
	}
	if !strings.Contains(err.Error(), "decode ThreadGoalSetResponse.goal: missing required field") {
		t.Fatalf("unexpected missing thread goal set response goal error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"goal":{"createdAt":1000,"objective":"ship sdk","status":"bogus","threadId":"thread-1","timeUsedSeconds":12,"tokenBudget":100000,"tokensUsed":42,"updatedAt":2000}}`), &goalSet)
	if err == nil {
		t.Fatal("expected invalid thread goal status to fail")
	}
	if !strings.Contains(err.Error(), `invalid ThreadGoalStatus enum value "bogus"`) {
		t.Fatalf("unexpected invalid thread goal status error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"goal":{"createdAt":1000,"objective":"ship sdk","status":"active","threadId":"thread-1","timeUsedSeconds":12,"tokenBudget":"bad","tokensUsed":42,"updatedAt":2000}}`), &goalSet)
	if err == nil {
		t.Fatal("expected invalid thread goal tokenBudget to fail")
	}
	if !strings.Contains(err.Error(), "decode ThreadGoal.tokenBudget") {
		t.Fatalf("unexpected invalid thread goal tokenBudget error: %v", err)
	}

	_, err = json.Marshal(NewThreadListCwdFilterArray(nil))
	if err == nil {
		t.Fatal("expected nil thread list cwd array to fail")
	}
	if !strings.Contains(err.Error(), "encode ThreadListCwdFilter.array: nil is not allowed") {
		t.Fatalf("unexpected nil thread list cwd array error: %v", err)
	}

	var list ThreadListParams
	err = json.Unmarshal([]byte(`{"cwd":123}`), &list)
	if err == nil {
		t.Fatal("expected invalid thread list cwd kind to fail")
	}
	if !strings.Contains(err.Error(), "decode ThreadListCwdFilter: expected string or array") {
		t.Fatalf("unexpected invalid thread list cwd error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"cwd":["/repo",null]}`), &list)
	if err == nil {
		t.Fatal("expected invalid thread list cwd array item to fail")
	}
	if !strings.Contains(err.Error(), "decode ThreadListCwdFilter: expected array item 1 to be string") {
		t.Fatalf("unexpected invalid thread list cwd array item error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"sortKey":"bogus"}`), &list)
	if err == nil {
		t.Fatal("expected invalid thread list sortKey to fail")
	}
	if !strings.Contains(err.Error(), `invalid ThreadSortKey enum value "bogus"`) {
		t.Fatalf("unexpected invalid thread list sortKey error: %v", err)
	}

	var metadata ThreadMetadataUpdateParams
	err = json.Unmarshal([]byte(`{"gitInfo":null}`), &metadata)
	if err == nil {
		t.Fatal("expected missing metadata update threadId to fail")
	}
	if !strings.Contains(err.Error(), "decode ThreadMetadataUpdateParams.threadId: missing required field") {
		t.Fatalf("unexpected missing metadata update threadId error: %v", err)
	}

	var memoryMode ThreadMemoryModeSetParams
	err = json.Unmarshal([]byte(`{"mode":"bogus","threadId":"thread-1"}`), &memoryMode)
	if err == nil {
		t.Fatal("expected invalid memory mode to fail")
	}
	if !strings.Contains(err.Error(), `invalid ThreadMemoryMode enum value "bogus"`) {
		t.Fatalf("unexpected invalid memory mode error: %v", err)
	}

	var unsubscribe ThreadUnsubscribeResponse
	err = json.Unmarshal([]byte(`{"status":"bogus"}`), &unsubscribe)
	if err == nil {
		t.Fatal("expected invalid unsubscribe status to fail")
	}
	if !strings.Contains(err.Error(), `invalid ThreadUnsubscribeStatus enum value "bogus"`) {
		t.Fatalf("unexpected invalid unsubscribe status error: %v", err)
	}
}

func TestGeneratedThreadRealtimePayloadsProtocolMarshalAndUnmarshal(t *testing.T) {
	audio := ThreadRealtimeAudioChunk{
		Data:              "base64",
		ItemID:            Value("item-1"),
		NumChannels:       2,
		SampleRate:        24000,
		SamplesPerChannel: Value(uint32(480)),
	}
	audioJSON := `{"data":"base64","itemId":"item-1","numChannels":2,"sampleRate":24000,"samplesPerChannel":480}`
	for _, tc := range []struct {
		name   string
		value  any
		target any
		want   string
	}{
		{name: "append audio params", value: ThreadRealtimeAppendAudioParams{Audio: audio, ThreadID: "thread-1"}, target: &ThreadRealtimeAppendAudioParams{}, want: `{"audio":` + audioJSON + `,"threadId":"thread-1"}`},
		{name: "append audio response", value: ThreadRealtimeAppendAudioResponse{}, target: &ThreadRealtimeAppendAudioResponse{}, want: `{}`},
		{name: "append text params", value: ThreadRealtimeAppendTextParams{Text: "hello", ThreadID: "thread-1"}, target: &ThreadRealtimeAppendTextParams{}, want: `{"text":"hello","threadId":"thread-1"}`},
		{name: "append text response", value: ThreadRealtimeAppendTextResponse{}, target: &ThreadRealtimeAppendTextResponse{}, want: `{}`},
		{name: "closed notification", value: ThreadRealtimeClosedNotification{Reason: Null[string](), ThreadID: "thread-1"}, target: &ThreadRealtimeClosedNotification{}, want: `{"reason":null,"threadId":"thread-1"}`},
		{name: "error notification", value: ThreadRealtimeErrorNotification{Message: "failed", ThreadID: "thread-1"}, target: &ThreadRealtimeErrorNotification{}, want: `{"message":"failed","threadId":"thread-1"}`},
		{name: "item added notification", value: ThreadRealtimeItemAddedNotification{Item: JSONObject(map[string]JSONValue{"id": JSONString("item-1"), "type": JSONString("message")}), ThreadID: "thread-1"}, target: &ThreadRealtimeItemAddedNotification{}, want: `{"item":{"id":"item-1","type":"message"},"threadId":"thread-1"}`},
		{name: "list voices params", value: ThreadRealtimeListVoicesParams{}, target: &ThreadRealtimeListVoicesParams{}, want: `{}`},
		{name: "list voices response", value: ThreadRealtimeListVoicesResponse{Voices: RealtimeVoicesList{
			DefaultV1: RealtimeVoiceAlloy,
			DefaultV2: RealtimeVoiceMarin,
			V1:        []RealtimeVoice{RealtimeVoiceAlloy},
			V2:        []RealtimeVoice{RealtimeVoiceMarin},
		}}, target: &ThreadRealtimeListVoicesResponse{}, want: `{"voices":{"defaultV1":"alloy","defaultV2":"marin","v1":["alloy"],"v2":["marin"]}}`},
		{name: "output audio delta notification", value: ThreadRealtimeOutputAudioDeltaNotification{Audio: audio, ThreadID: "thread-1"}, target: &ThreadRealtimeOutputAudioDeltaNotification{}, want: `{"audio":` + audioJSON + `,"threadId":"thread-1"}`},
		{name: "sdp notification", value: ThreadRealtimeSdpNotification{SDP: "offer", ThreadID: "thread-1"}, target: &ThreadRealtimeSdpNotification{}, want: `{"sdp":"offer","threadId":"thread-1"}`},
		{name: "start params", value: ThreadRealtimeStartParams{
			OutputModality:    RealtimeOutputModalityAudio,
			Prompt:            Null[string](),
			RealtimeSessionID: Value("session-1"),
			ThreadID:          "thread-1",
			Transport:         Value(NewThreadRealtimeStartTransportWebrtc(ThreadRealtimeStartTransportWebrtc{SDP: "offer"})),
			Voice:             Value(RealtimeVoiceMarin),
		}, target: &ThreadRealtimeStartParams{}, want: `{"outputModality":"audio","prompt":null,"realtimeSessionId":"session-1","threadId":"thread-1","transport":{"sdp":"offer","type":"webrtc"},"voice":"marin"}`},
		{name: "start response", value: ThreadRealtimeStartResponse{}, target: &ThreadRealtimeStartResponse{}, want: `{}`},
		{name: "started notification", value: ThreadRealtimeStartedNotification{RealtimeSessionID: Value("session-1"), ThreadID: "thread-1", Version: RealtimeConversationVersionV2}, target: &ThreadRealtimeStartedNotification{}, want: `{"realtimeSessionId":"session-1","threadId":"thread-1","version":"v2"}`},
		{name: "stop params", value: ThreadRealtimeStopParams{ThreadID: "thread-1"}, target: &ThreadRealtimeStopParams{}, want: `{"threadId":"thread-1"}`},
		{name: "stop response", value: ThreadRealtimeStopResponse{}, target: &ThreadRealtimeStopResponse{}, want: `{}`},
		{name: "transcript delta notification", value: ThreadRealtimeTranscriptDeltaNotification{Delta: "hel", Role: "assistant", ThreadID: "thread-1"}, target: &ThreadRealtimeTranscriptDeltaNotification{}, want: `{"delta":"hel","role":"assistant","threadId":"thread-1"}`},
		{name: "transcript done notification", value: ThreadRealtimeTranscriptDoneNotification{Role: "assistant", Text: "hello", ThreadID: "thread-1"}, target: &ThreadRealtimeTranscriptDoneNotification{}, want: `{"role":"assistant","text":"hello","threadId":"thread-1"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(raw); got != tc.want {
				t.Fatalf("%s JSON = %s, want %s", tc.name, got, tc.want)
			}
			if err := json.Unmarshal(raw, tc.target); err != nil {
				t.Fatal(err)
			}
		})
	}

	var decodedClosed ThreadRealtimeClosedNotification
	if err := json.Unmarshal([]byte(`{"reason":null,"threadId":"thread-1"}`), &decodedClosed); err != nil {
		t.Fatal(err)
	}
	if decodedClosed.Reason == nil || decodedClosed.Reason.Value != nil {
		t.Fatalf("decoded closed reason = %#v, want explicit null", decodedClosed.Reason)
	}

	var decodedItem ThreadRealtimeItemAddedNotification
	if err := json.Unmarshal([]byte(`{"item":{"id":"item-1"},"threadId":"thread-1"}`), &decodedItem); err != nil {
		t.Fatal(err)
	}
	if decodedItem.Item.Kind() != JSONKindObject {
		t.Fatalf("decoded realtime item = %#v", decodedItem.Item)
	}

	var decodedStart ThreadRealtimeStartParams
	if err := json.Unmarshal([]byte(`{"outputModality":"text","threadId":"thread-1","transport":{"type":"websocket"},"voice":null}`), &decodedStart); err != nil {
		t.Fatal(err)
	}
	if decodedStart.OutputModality != RealtimeOutputModalityText ||
		decodedStart.Transport == nil ||
		decodedStart.Transport.Value == nil ||
		decodedStart.Transport.Value.Kind() != ThreadRealtimeStartTransportKindWebsocket ||
		decodedStart.Voice == nil ||
		decodedStart.Voice.Value != nil {
		t.Fatalf("decoded realtime start = %#v", decodedStart)
	}

	var decodedStarted ThreadRealtimeStartedNotification
	if err := json.Unmarshal([]byte(`{"realtimeSessionId":"session-1","threadId":"thread-1","version":"v2"}`), &decodedStarted); err != nil {
		t.Fatal(err)
	}
	if decodedStarted.RealtimeSessionID == nil || decodedStarted.RealtimeSessionID.Value == nil || *decodedStarted.RealtimeSessionID.Value != "session-1" || decodedStarted.Version != RealtimeConversationVersionV2 {
		t.Fatalf("decoded realtime started = %#v", decodedStarted)
	}
}

func TestGeneratedThreadRealtimePayloadsRejectMalformedProtocol(t *testing.T) {
	var empty ThreadRealtimeAppendTextResponse
	err := json.Unmarshal([]byte(`{"extra":true}`), &empty)
	if err == nil {
		t.Fatal("expected unknown empty realtime response field to fail")
	}
	if !strings.Contains(err.Error(), `decode ThreadRealtimeAppendTextResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown realtime response field error: %v", err)
	}

	var appendText ThreadRealtimeAppendTextParams
	err = json.Unmarshal([]byte(`{"threadId":"thread-1"}`), &appendText)
	if err == nil {
		t.Fatal("expected missing realtime append text to fail")
	}
	if !strings.Contains(err.Error(), "decode ThreadRealtimeAppendTextParams.text: missing required field") {
		t.Fatalf("unexpected missing realtime text error: %v", err)
	}

	var stop ThreadRealtimeStopParams
	err = json.Unmarshal([]byte(`{"threadId":null}`), &stop)
	if err == nil {
		t.Fatal("expected null realtime stop threadId to fail")
	}
	if !strings.Contains(err.Error(), "decode ThreadRealtimeStopParams.threadId: null is not allowed") {
		t.Fatalf("unexpected null realtime stop threadId error: %v", err)
	}

	var itemAdded ThreadRealtimeItemAddedNotification
	err = json.Unmarshal([]byte(`{"threadId":"thread-1"}`), &itemAdded)
	if err == nil {
		t.Fatal("expected missing realtime item to fail")
	}
	if !strings.Contains(err.Error(), "decode ThreadRealtimeItemAddedNotification.item: missing required field") {
		t.Fatalf("unexpected missing realtime item error: %v", err)
	}

	var outputAudio ThreadRealtimeOutputAudioDeltaNotification
	err = json.Unmarshal([]byte(`{"threadId":"thread-1"}`), &outputAudio)
	if err == nil {
		t.Fatal("expected missing realtime output audio to fail")
	}
	if !strings.Contains(err.Error(), "decode ThreadRealtimeOutputAudioDeltaNotification.audio: missing required field") {
		t.Fatalf("unexpected missing realtime output audio error: %v", err)
	}

	var voices ThreadRealtimeListVoicesResponse
	err = json.Unmarshal([]byte(`{}`), &voices)
	if err == nil {
		t.Fatal("expected missing realtime voices to fail")
	}
	if !strings.Contains(err.Error(), "decode ThreadRealtimeListVoicesResponse.voices: missing required field") {
		t.Fatalf("unexpected missing realtime voices error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"voices":{"defaultV1":"bogus","defaultV2":"marin","v1":["alloy"],"v2":["marin"]}}`), &voices)
	if err == nil {
		t.Fatal("expected invalid realtime voices default to fail")
	}
	if !strings.Contains(err.Error(), `invalid RealtimeVoice enum value "bogus"`) {
		t.Fatalf("unexpected invalid realtime voices default error: %v", err)
	}

	var start ThreadRealtimeStartParams
	err = json.Unmarshal([]byte(`{"threadId":"thread-1"}`), &start)
	if err == nil {
		t.Fatal("expected missing realtime start output modality to fail")
	}
	if !strings.Contains(err.Error(), "decode ThreadRealtimeStartParams.outputModality: missing required field") {
		t.Fatalf("unexpected missing realtime start output modality error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"outputModality":"audio","threadId":"thread-1","transport":{"type":"webrtc"}}`), &start)
	if err == nil {
		t.Fatal("expected missing realtime start webrtc sdp to fail")
	}
	if !strings.Contains(err.Error(), "decode ThreadRealtimeStartTransport.sdp: missing required field") {
		t.Fatalf("unexpected missing realtime start webrtc sdp error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"outputModality":"audio","threadId":"thread-1","transport":{"type":"bogus"}}`), &start)
	if err == nil {
		t.Fatal("expected unknown realtime start transport type to fail")
	}
	if !strings.Contains(err.Error(), `decode ThreadRealtimeStartTransport.type: unknown variant "bogus"`) {
		t.Fatalf("unexpected unknown realtime start transport type error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"outputModality":"audio","threadId":"thread-1","voice":"bogus"}`), &start)
	if err == nil {
		t.Fatal("expected invalid realtime voice to fail")
	}
	if !strings.Contains(err.Error(), `invalid RealtimeVoice enum value "bogus"`) {
		t.Fatalf("unexpected invalid realtime voice error: %v", err)
	}

	var appendAudio ThreadRealtimeAppendAudioParams
	err = json.Unmarshal([]byte(`{"audio":{"data":"base64","numChannels":-1,"sampleRate":24000},"threadId":"thread-1"}`), &appendAudio)
	if err == nil {
		t.Fatal("expected negative realtime audio channel count to fail")
	}
	if !strings.Contains(err.Error(), "decode ThreadRealtimeAudioChunk.numChannels") {
		t.Fatalf("unexpected negative realtime audio channel count error: %v", err)
	}

	var started ThreadRealtimeStartedNotification
	err = json.Unmarshal([]byte(`{"threadId":"thread-1","version":"v3"}`), &started)
	if err == nil {
		t.Fatal("expected invalid realtime version to fail")
	}
	if !strings.Contains(err.Error(), `invalid RealtimeConversationVersion enum value "v3"`) {
		t.Fatalf("unexpected invalid realtime version error: %v", err)
	}
}

func TestGeneratedStableNotificationPayloadsProtocolMarshalAndUnmarshal(t *testing.T) {
	hookRun := sampleGeneratedHookRunSummary()
	hookRunJSON := `{"displayOrder":1,"entries":[{"kind":"warning","text":"warn"}],"eventName":"preToolUse","executionMode":"sync","handlerType":"command","id":"hook-run-1","scope":"turn","sourcePath":"/workspace/.codex/hooks.json","startedAt":1000,"status":"running"}`
	goal := sampleGeneratedThreadGoal()
	goalJSON := `{"createdAt":1000,"objective":"ship sdk","status":"active","threadId":"thread-1","timeUsedSeconds":12,"tokenBudget":100000,"tokensUsed":42,"updatedAt":2000}`
	for _, tc := range []struct {
		name   string
		value  any
		target any
		want   string
	}{
		{name: "agent message delta", value: AgentMessageDeltaNotification{Delta: "hello", ItemID: "item-1", ThreadID: "thread-1", TurnID: "turn-1"}, target: &AgentMessageDeltaNotification{}, want: `{"delta":"hello","itemId":"item-1","threadId":"thread-1","turnId":"turn-1"}`},
		{name: "context compacted", value: ContextCompactedNotification{ThreadID: "thread-1", TurnID: "turn-1"}, target: &ContextCompactedNotification{}, want: `{"threadId":"thread-1","turnId":"turn-1"}`},
		{name: "deprecation notice", value: DeprecationNoticeNotification{Details: Null[string](), Summary: "deprecated"}, target: &DeprecationNoticeNotification{}, want: `{"details":null,"summary":"deprecated"}`},
		{name: "file change output delta", value: FileChangeOutputDeltaNotification{Delta: "patch", ItemID: "item-1", ThreadID: "thread-1", TurnID: "turn-1"}, target: &FileChangeOutputDeltaNotification{}, want: `{"delta":"patch","itemId":"item-1","threadId":"thread-1","turnId":"turn-1"}`},
		{name: "guardian warning", value: GuardianWarningNotification{Message: "guardian warning", ThreadID: "thread-1"}, target: &GuardianWarningNotification{}, want: `{"message":"guardian warning","threadId":"thread-1"}`},
		{name: "hook started", value: HookStartedNotification{Run: hookRun, ThreadID: "thread-1", TurnID: Value("turn-1")}, target: &HookStartedNotification{}, want: `{"run":` + hookRunJSON + `,"threadId":"thread-1","turnId":"turn-1"}`},
		{name: "hook completed", value: HookCompletedNotification{Run: hookRun, ThreadID: "thread-1", TurnID: Null[string]()}, target: &HookCompletedNotification{}, want: `{"run":` + hookRunJSON + `,"threadId":"thread-1","turnId":null}`},
		{name: "mcp oauth completed", value: McpServerOauthLoginCompletedNotification{Error: Null[string](), Name: "server", Success: true}, target: &McpServerOauthLoginCompletedNotification{}, want: `{"error":null,"name":"server","success":true}`},
		{name: "mcp status updated", value: McpServerStatusUpdatedNotification{Error: Null[string](), Name: "server", Status: McpServerStartupStateReady}, target: &McpServerStatusUpdatedNotification{}, want: `{"error":null,"name":"server","status":"ready"}`},
		{name: "mcp tool progress", value: McpToolCallProgressNotification{ItemID: "item-1", Message: "running", ThreadID: "thread-1", TurnID: "turn-1"}, target: &McpToolCallProgressNotification{}, want: `{"itemId":"item-1","message":"running","threadId":"thread-1","turnId":"turn-1"}`},
		{name: "plan delta", value: PlanDeltaNotification{Delta: "plan", ItemID: "item-1", ThreadID: "thread-1", TurnID: "turn-1"}, target: &PlanDeltaNotification{}, want: `{"delta":"plan","itemId":"item-1","threadId":"thread-1","turnId":"turn-1"}`},
		{name: "reasoning summary part added", value: ReasoningSummaryPartAddedNotification{ItemID: "item-1", SummaryIndex: 1, ThreadID: "thread-1", TurnID: "turn-1"}, target: &ReasoningSummaryPartAddedNotification{}, want: `{"itemId":"item-1","summaryIndex":1,"threadId":"thread-1","turnId":"turn-1"}`},
		{name: "reasoning summary text delta", value: ReasoningSummaryTextDeltaNotification{Delta: "why", ItemID: "item-1", SummaryIndex: 1, ThreadID: "thread-1", TurnID: "turn-1"}, target: &ReasoningSummaryTextDeltaNotification{}, want: `{"delta":"why","itemId":"item-1","summaryIndex":1,"threadId":"thread-1","turnId":"turn-1"}`},
		{name: "reasoning text delta", value: ReasoningTextDeltaNotification{ContentIndex: 0, Delta: "think", ItemID: "item-1", ThreadID: "thread-1", TurnID: "turn-1"}, target: &ReasoningTextDeltaNotification{}, want: `{"contentIndex":0,"delta":"think","itemId":"item-1","threadId":"thread-1","turnId":"turn-1"}`},
		{name: "remote control status changed", value: RemoteControlStatusChangedNotification{EnvironmentID: Value("env-1"), InstallationID: "install-1", ServerName: "remote", Status: RemoteControlConnectionStatusConnected}, target: &RemoteControlStatusChangedNotification{}, want: `{"environmentId":"env-1","installationId":"install-1","serverName":"remote","status":"connected"}`},
		{name: "skills changed", value: SkillsChangedNotification{}, target: &SkillsChangedNotification{}, want: `{}`},
		{name: "terminal interaction", value: TerminalInteractionNotification{ItemID: "item-1", ProcessID: "proc-1", Stdin: "ls\n", ThreadID: "thread-1", TurnID: "turn-1"}, target: &TerminalInteractionNotification{}, want: `{"itemId":"item-1","processId":"proc-1","stdin":"ls\n","threadId":"thread-1","turnId":"turn-1"}`},
		{name: "thread token usage updated", value: ThreadTokenUsageUpdatedNotification{
			ThreadID: "thread-1",
			TokenUsage: ThreadTokenUsage{
				Last:  TokenUsageBreakdown{CachedInputTokens: 1, InputTokens: 3, OutputTokens: 2, ReasoningOutputTokens: 1, TotalTokens: 5},
				Total: TokenUsageBreakdown{CachedInputTokens: 10, InputTokens: 30, OutputTokens: 20, ReasoningOutputTokens: 5, TotalTokens: 50},
			},
			TurnID: "turn-1",
		}, target: &ThreadTokenUsageUpdatedNotification{}, want: `{"threadId":"thread-1","tokenUsage":{"last":{"cachedInputTokens":1,"inputTokens":3,"outputTokens":2,"reasoningOutputTokens":1,"totalTokens":5},"total":{"cachedInputTokens":10,"inputTokens":30,"outputTokens":20,"reasoningOutputTokens":5,"totalTokens":50}},"turnId":"turn-1"}`},
		{name: "thread goal updated", value: ThreadGoalUpdatedNotification{Goal: goal, ThreadID: "thread-1", TurnID: Null[string]()}, target: &ThreadGoalUpdatedNotification{}, want: `{"goal":` + goalJSON + `,"threadId":"thread-1","turnId":null}`},
		{name: "turn diff updated", value: TurnDiffUpdatedNotification{Diff: "@@", ThreadID: "thread-1", TurnID: "turn-1"}, target: &TurnDiffUpdatedNotification{}, want: `{"diff":"@@","threadId":"thread-1","turnId":"turn-1"}`},
		{name: "turn plan updated", value: TurnPlanUpdatedNotification{Explanation: Value("why"), Plan: []TurnPlanStep{{Status: TurnPlanStepStatusInProgress, Step: "inspect"}}, ThreadID: "thread-1", TurnID: "turn-1"}, target: &TurnPlanUpdatedNotification{}, want: `{"explanation":"why","plan":[{"status":"inProgress","step":"inspect"}],"threadId":"thread-1","turnId":"turn-1"}`},
		{name: "warning", value: WarningNotification{Message: "warning", ThreadID: Value("thread-1")}, target: &WarningNotification{}, want: `{"message":"warning","threadId":"thread-1"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(raw); got != tc.want {
				t.Fatalf("%s JSON = %s, want %s", tc.name, got, tc.want)
			}
			if err := json.Unmarshal(raw, tc.target); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGeneratedStableNotificationPayloadsRejectMalformedProtocol(t *testing.T) {
	var empty SkillsChangedNotification
	err := json.Unmarshal([]byte(`{"extra":true}`), &empty)
	if err == nil {
		t.Fatal("expected unknown empty notification field to fail")
	}
	if !strings.Contains(err.Error(), `decode SkillsChangedNotification: unknown field "extra"`) {
		t.Fatalf("unexpected unknown empty notification error: %v", err)
	}

	var agentDelta AgentMessageDeltaNotification
	err = json.Unmarshal([]byte(`{"itemId":"item-1","threadId":"thread-1","turnId":"turn-1"}`), &agentDelta)
	if err == nil {
		t.Fatal("expected missing delta to fail")
	}
	if !strings.Contains(err.Error(), "decode AgentMessageDeltaNotification.delta: missing required field") {
		t.Fatalf("unexpected missing delta error: %v", err)
	}

	var warning WarningNotification
	err = json.Unmarshal([]byte(`{"message":null}`), &warning)
	if err == nil {
		t.Fatal("expected null warning message to fail")
	}
	if !strings.Contains(err.Error(), "decode WarningNotification.message: null is not allowed") {
		t.Fatalf("unexpected null warning message error: %v", err)
	}

	var mcpStatus McpServerStatusUpdatedNotification
	err = json.Unmarshal([]byte(`{"name":"server","status":"bogus"}`), &mcpStatus)
	if err == nil {
		t.Fatal("expected invalid MCP startup state to fail")
	}
	if !strings.Contains(err.Error(), `invalid McpServerStartupState enum value "bogus"`) {
		t.Fatalf("unexpected invalid MCP startup state error: %v", err)
	}

	var remote RemoteControlStatusChangedNotification
	err = json.Unmarshal([]byte(`{"installationId":"install-1","serverName":"remote","status":"bogus"}`), &remote)
	if err == nil {
		t.Fatal("expected invalid remote control status to fail")
	}
	if !strings.Contains(err.Error(), `invalid RemoteControlConnectionStatus enum value "bogus"`) {
		t.Fatalf("unexpected invalid remote control status error: %v", err)
	}

	var hookStarted HookStartedNotification
	err = json.Unmarshal([]byte(`{"threadId":"thread-1"}`), &hookStarted)
	if err == nil {
		t.Fatal("expected missing hook run to fail")
	}
	if !strings.Contains(err.Error(), "decode HookStartedNotification.run: missing required field") {
		t.Fatalf("unexpected missing hook run error: %v", err)
	}

	_, err = json.Marshal(HookCompletedNotification{
		Run:      HookRunSummary{DisplayOrder: 1},
		ThreadID: "thread-1",
	})
	if err == nil {
		t.Fatal("expected nil hook entries to fail")
	}
	if !strings.Contains(err.Error(), "encode HookRunSummary.entries: nil is not allowed") {
		t.Fatalf("unexpected nil hook entries error: %v", err)
	}

	var hookCompleted HookCompletedNotification
	err = json.Unmarshal([]byte(`{"run":{"displayOrder":1,"entries":[],"eventName":"bogus","executionMode":"sync","handlerType":"command","id":"hook-run-1","scope":"turn","sourcePath":"/workspace/.codex/hooks.json","startedAt":1000,"status":"running"},"threadId":"thread-1"}`), &hookCompleted)
	if err == nil {
		t.Fatal("expected invalid hook event enum to fail")
	}
	if !strings.Contains(err.Error(), `invalid HookEventName enum value "bogus"`) {
		t.Fatalf("unexpected invalid hook event enum error: %v", err)
	}

	var threadGoalUpdated ThreadGoalUpdatedNotification
	err = json.Unmarshal([]byte(`{"threadId":"thread-1"}`), &threadGoalUpdated)
	if err == nil {
		t.Fatal("expected missing thread goal updated goal to fail")
	}
	if !strings.Contains(err.Error(), "decode ThreadGoalUpdatedNotification.goal: missing required field") {
		t.Fatalf("unexpected missing thread goal updated goal error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"goal":{"createdAt":1000,"objective":"ship sdk","status":"bogus","threadId":"thread-1","timeUsedSeconds":12,"tokensUsed":42,"updatedAt":2000},"threadId":"thread-1"}`), &threadGoalUpdated)
	if err == nil {
		t.Fatal("expected invalid thread goal updated status to fail")
	}
	if !strings.Contains(err.Error(), `invalid ThreadGoalStatus enum value "bogus"`) {
		t.Fatalf("unexpected invalid thread goal updated status error: %v", err)
	}

	var tokenUsage ThreadTokenUsageUpdatedNotification
	err = json.Unmarshal([]byte(`{"threadId":"thread-1","turnId":"turn-1","tokenUsage":{"last":{"cachedInputTokens":1,"inputTokens":3,"outputTokens":2,"reasoningOutputTokens":1,"totalTokens":5}}}`), &tokenUsage)
	if err == nil {
		t.Fatal("expected missing token usage total to fail")
	}
	if !strings.Contains(err.Error(), "decode ThreadTokenUsage.total: missing required field") {
		t.Fatalf("unexpected missing token usage total error: %v", err)
	}

	var turnPlan TurnPlanUpdatedNotification
	err = json.Unmarshal([]byte(`{"threadId":"thread-1","turnId":"turn-1","plan":[{"status":"bogus","step":"inspect"}]}`), &turnPlan)
	if err == nil {
		t.Fatal("expected invalid turn plan status to fail")
	}
	if !strings.Contains(err.Error(), `invalid TurnPlanStepStatus enum value "bogus"`) {
		t.Fatalf("unexpected invalid turn plan status error: %v", err)
	}
}

func TestGeneratedThreadSourceUnionsProtocolMarshalAndUnmarshal(t *testing.T) {
	appServerRaw, err := json.Marshal(NewSessionSourceAppServer())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(appServerRaw), `"appServer"`; got != want {
		t.Fatalf("SessionSource appServer JSON = %s, want %s", got, want)
	}

	custom := NewSessionSourceCustom(SessionSourceCustom{Custom: "desktop"})
	customRaw, err := json.Marshal(custom)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(customRaw), `{"custom":"desktop"}`; got != want {
		t.Fatalf("SessionSource custom JSON = %s, want %s", got, want)
	}
	var decodedCustom SessionSource
	if err := json.Unmarshal(customRaw, &decodedCustom); err != nil {
		t.Fatal(err)
	}
	customPayload, ok := decodedCustom.AsCustom()
	if !ok || customPayload.Custom != "desktop" {
		t.Fatalf("decoded custom SessionSource = %#v ok=%t", customPayload, ok)
	}

	subAgent := NewSessionSourceSubAgent(SessionSourceSubAgent{
		SubAgent: NewSubAgentSourceThreadSpawn(SubAgentSourceThreadSpawn{
			AgentNickname:  Value("worker"),
			AgentPath:      Null[string](),
			AgentRole:      Value("reviewer"),
			Depth:          2,
			ParentThreadID: "thread-parent",
		}),
	})
	subAgentRaw, err := json.Marshal(subAgent)
	if err != nil {
		t.Fatal(err)
	}
	wantSubAgent := `{"subAgent":{"thread_spawn":{"agent_nickname":"worker","agent_path":null,"agent_role":"reviewer","depth":2,"parent_thread_id":"thread-parent"}}}`
	if got := string(subAgentRaw); got != wantSubAgent {
		t.Fatalf("SessionSource subAgent JSON = %s, want %s", got, wantSubAgent)
	}
	var decodedSubAgent SessionSource
	if err := json.Unmarshal(subAgentRaw, &decodedSubAgent); err != nil {
		t.Fatal(err)
	}
	subAgentPayload, ok := decodedSubAgent.AsSubAgent()
	if !ok {
		t.Fatalf("decoded subAgent SessionSource = %#v", decodedSubAgent)
	}
	threadSpawnPayload, ok := subAgentPayload.SubAgent.AsThreadSpawn()
	if !ok || threadSpawnPayload.Depth != 2 || threadSpawnPayload.ParentThreadID != "thread-parent" {
		t.Fatalf("decoded thread_spawn SubAgentSource = %#v ok=%t", threadSpawnPayload, ok)
	}

	otherRaw, err := json.Marshal(NewSubAgentSourceOther(SubAgentSourceOther{Other: "manual"}))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(otherRaw), `{"other":"manual"}`; got != want {
		t.Fatalf("SubAgentSource other JSON = %s, want %s", got, want)
	}
	var decodedOther SubAgentSource
	if err := json.Unmarshal(otherRaw, &decodedOther); err != nil {
		t.Fatal(err)
	}
	otherPayload, ok := decodedOther.AsOther()
	if !ok || otherPayload.Other != "manual" {
		t.Fatalf("decoded other SubAgentSource = %#v ok=%t", otherPayload, ok)
	}
}

func TestGeneratedSimpleUnlockedPayloadsProtocolMarshalAndUnmarshal(t *testing.T) {
	interruptRaw, err := json.Marshal(TurnInterruptParams{
		ThreadID: "thread-1",
		TurnID:   "turn-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(interruptRaw), `{"threadId":"thread-1","turnId":"turn-1"}`; got != want {
		t.Fatalf("TurnInterruptParams JSON = %s, want %s", got, want)
	}

	steerRaw, err := json.Marshal(TurnSteerParams{
		ExpectedTurnID: "turn-1",
		Input:          []UserInput{NewUserInputText(UserInputText{Text: "continue"})},
		ResponsesapiClientMetadata: Value(map[string]string{
			"trace": "turn-steer",
		}),
		ThreadID: "thread-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(steerRaw), `{"expectedTurnId":"turn-1","input":[{"text":"continue","type":"text"}],"responsesapiClientMetadata":{"trace":"turn-steer"},"threadId":"thread-1"}`; got != want {
		t.Fatalf("TurnSteerParams JSON = %s, want %s", got, want)
	}
	var decodedSteer TurnSteerParams
	if err := json.Unmarshal(steerRaw, &decodedSteer); err != nil {
		t.Fatal(err)
	}
	if decodedSteer.ExpectedTurnID != "turn-1" || decodedSteer.ThreadID != "thread-1" ||
		len(decodedSteer.Input) != 1 || decodedSteer.ResponsesapiClientMetadata == nil ||
		decodedSteer.ResponsesapiClientMetadata.Value == nil ||
		(*decodedSteer.ResponsesapiClientMetadata.Value)["trace"] != "turn-steer" {
		t.Fatalf("decoded TurnSteerParams = %#v", decodedSteer)
	}

	memoryResetRaw, err := json.Marshal(MemoryResetResponse{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(memoryResetRaw), `{}`; got != want {
		t.Fatalf("MemoryResetResponse JSON = %s, want %s", got, want)
	}

	mockParamsRaw, err := json.Marshal(MockExperimentalMethodParams{Value: Value("ping")})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(mockParamsRaw), `{"value":"ping"}`; got != want {
		t.Fatalf("MockExperimentalMethodParams JSON = %s, want %s", got, want)
	}
	var decodedMockResponse MockExperimentalMethodResponse
	if err := json.Unmarshal([]byte(`{"echoed":"ping"}`), &decodedMockResponse); err != nil {
		t.Fatal(err)
	}
	if decodedMockResponse.Echoed == nil || decodedMockResponse.Echoed.Value == nil || *decodedMockResponse.Echoed.Value != "ping" {
		t.Fatalf("decoded mock response = %#v", decodedMockResponse)
	}
}

func TestGeneratedCodexErrorInfoProtocolMarshalAndUnmarshal(t *testing.T) {
	httpErrorRaw, err := json.Marshal(NewCodexErrorInfoHTTPConnectionFailed(CodexErrorInfoHTTPConnectionFailed{
		HTTPStatusCode: Value(uint16(503)),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(httpErrorRaw), `{"httpConnectionFailed":{"httpStatusCode":503}}`; got != want {
		t.Fatalf("CodexErrorInfo HTTP JSON = %s, want %s", got, want)
	}
	var decodedHTTP CodexErrorInfo
	if err := json.Unmarshal(httpErrorRaw, &decodedHTTP); err != nil {
		t.Fatal(err)
	}
	httpPayload, ok := decodedHTTP.AsHTTPConnectionFailed()
	if !ok || httpPayload.HTTPStatusCode == nil || httpPayload.HTTPStatusCode.Value == nil || *httpPayload.HTTPStatusCode.Value != 503 {
		t.Fatalf("decoded HTTP CodexErrorInfo = %#v, ok=%t", decodedHTTP, ok)
	}

	activeTurnRaw, err := json.Marshal(NewCodexErrorInfoActiveTurnNotSteerable(CodexErrorInfoActiveTurnNotSteerable{
		TurnKind: NonSteerableTurnKindReview,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(activeTurnRaw), `{"activeTurnNotSteerable":{"turnKind":"review"}}`; got != want {
		t.Fatalf("CodexErrorInfo active turn JSON = %s, want %s", got, want)
	}
	var decodedActiveTurn CodexErrorInfo
	if err := json.Unmarshal(activeTurnRaw, &decodedActiveTurn); err != nil {
		t.Fatal(err)
	}
	activeTurnPayload, ok := decodedActiveTurn.AsActiveTurnNotSteerable()
	if !ok || activeTurnPayload.TurnKind != NonSteerableTurnKindReview {
		t.Fatalf("decoded active turn CodexErrorInfo = %#v, ok=%t", decodedActiveTurn, ok)
	}

	scalarRaw, err := json.Marshal(NewCodexErrorInfoOther())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(scalarRaw), `"other"`; got != want {
		t.Fatalf("CodexErrorInfo scalar JSON = %s, want %s", got, want)
	}
	var decodedScalar CodexErrorInfo
	if err := json.Unmarshal(scalarRaw, &decodedScalar); err != nil {
		t.Fatal(err)
	}
	if _, ok := decodedScalar.AsOther(); !ok {
		t.Fatalf("decoded scalar CodexErrorInfo = %#v", decodedScalar)
	}
}

func TestGeneratedThreadTurnLifecycleParamsRejectMalformedProtocol(t *testing.T) {
	_, err := json.Marshal(TurnStartParams{ThreadID: "thread-1"})
	if err == nil {
		t.Fatal("expected nil turn start input marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode TurnStartParams.input: nil is not allowed") {
		t.Fatalf("unexpected nil turn start input error: %v", err)
	}

	var turn TurnStartParams
	err = json.Unmarshal([]byte(`{"threadId":"thread-1"}`), &turn)
	if err == nil {
		t.Fatal("expected missing turn start input to fail")
	}
	if !strings.Contains(err.Error(), "decode TurnStartParams.input: missing required field") {
		t.Fatalf("unexpected missing turn start input error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"input":[]}`), &turn)
	if err == nil {
		t.Fatal("expected missing turn start threadId to fail")
	}
	if !strings.Contains(err.Error(), "decode TurnStartParams.threadId: missing required field") {
		t.Fatalf("unexpected missing turn start threadId error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"input":[],"outputSchema":null,"threadId":"thread-1"}`), &turn)
	if err == nil {
		t.Fatal("expected null outputSchema to fail")
	}
	if !strings.Contains(err.Error(), "decode TurnStartParams.outputSchema: null is not allowed") {
		t.Fatalf("unexpected null outputSchema error: %v", err)
	}

	var input UserInput
	err = json.Unmarshal([]byte(`{"type":"audio","url":"https://example.test/audio.wav"}`), &input)
	if err == nil {
		t.Fatal("expected unknown user input variant to fail")
	}
	if !strings.Contains(err.Error(), `decode UserInput.type: unknown variant "audio"`) {
		t.Fatalf("unexpected unknown user input variant error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"extra":true,"text":"hello","type":"text"}`), &input)
	if err == nil {
		t.Fatal("expected unknown user input text field to fail")
	}
	if !strings.Contains(err.Error(), `decode UserInput.text: unknown field "extra"`) {
		t.Fatalf("unexpected unknown user input field error: %v", err)
	}

	var resume ThreadResumeParams
	err = json.Unmarshal([]byte(`{"history":[{"type":"other"}]}`), &resume)
	if err == nil {
		t.Fatal("expected missing resume threadId to fail")
	}
	if !strings.Contains(err.Error(), "decode ThreadResumeParams.threadId: missing required field") {
		t.Fatalf("unexpected missing resume threadId error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"history":[{"type":"audio"}],"threadId":"thread-1"}`), &resume)
	if err == nil {
		t.Fatal("expected unknown response item variant to fail")
	}
	if !strings.Contains(err.Error(), `decode ResponseItem.type: unknown variant "audio"`) {
		t.Fatalf("unexpected unknown response item variant error: %v", err)
	}

	var output FunctionCallOutputBody
	err = json.Unmarshal([]byte(`{"text":"nope"}`), &output)
	if err == nil {
		t.Fatal("expected object function call output body to fail")
	}
	if !strings.Contains(err.Error(), "decode FunctionCallOutputBody: expected string or array") {
		t.Fatalf("unexpected function call output body error: %v", err)
	}

	_, err = json.Marshal(NewFunctionCallOutputBodyArray(nil))
	if err == nil {
		t.Fatal("expected nil function call output body array to fail")
	}
	if !strings.Contains(err.Error(), "encode FunctionCallOutputBody.array: nil is not allowed") {
		t.Fatalf("unexpected nil function call output body array error: %v", err)
	}

	var turnResponse TurnStartResponse
	err = json.Unmarshal([]byte(`{"extra":true}`), &turnResponse)
	if err == nil {
		t.Fatal("expected missing turn start response turn to fail")
	}
	if !strings.Contains(err.Error(), "decode TurnStartResponse.turn: missing required field") {
		t.Fatalf("unexpected missing turn start response turn error: %v", err)
	}

	var threadItem ThreadItem
	err = json.Unmarshal([]byte(`{"id":"item-1","type":"audio"}`), &threadItem)
	if err == nil {
		t.Fatal("expected unknown thread item variant to fail")
	}
	if !strings.Contains(err.Error(), `decode ThreadItem.type: unknown variant "audio"`) {
		t.Fatalf("unexpected unknown thread item variant error: %v", err)
	}

	_, err = json.Marshal(McpToolCallResult{})
	if err == nil {
		t.Fatal("expected nil MCP tool call result content to fail")
	}
	if !strings.Contains(err.Error(), "encode McpToolCallResult.content: nil is not allowed") {
		t.Fatalf("unexpected nil MCP tool call result content error: %v", err)
	}

	var codexError CodexErrorInfo
	err = json.Unmarshal([]byte(`"mystery"`), &codexError)
	if err == nil {
		t.Fatal("expected unknown CodexErrorInfo string variant to fail")
	}
	if !strings.Contains(err.Error(), `decode CodexErrorInfo.value: unknown variant "mystery"`) {
		t.Fatalf("unexpected unknown CodexErrorInfo string error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"activeTurnNotSteerable":{}}`), &codexError)
	if err == nil {
		t.Fatal("expected missing activeTurnNotSteerable turnKind to fail")
	}
	if !strings.Contains(err.Error(), "decode CodexErrorInfo.activeTurnNotSteerable.turnKind: missing required field") {
		t.Fatalf("unexpected missing activeTurnNotSteerable turnKind error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"httpConnectionFailed":{"extra":true}}`), &codexError)
	if err == nil {
		t.Fatal("expected unknown CodexErrorInfo object field to fail")
	}
	if !strings.Contains(err.Error(), `decode CodexErrorInfo.httpConnectionFailed: unknown field "extra"`) {
		t.Fatalf("unexpected unknown CodexErrorInfo object field error: %v", err)
	}
}

func TestGeneratedThreadLifecycleResponsesRejectMalformedProtocol(t *testing.T) {
	var threadStart ThreadStartResponse
	err := json.Unmarshal([]byte(`{"approvalPolicy":"never","approvalsReviewer":"user","cwd":"/workspace","model":"gpt-5","modelProvider":"openai","sandbox":{"type":"dangerFullAccess"}}`), &threadStart)
	if err == nil {
		t.Fatal("expected missing thread start response thread to fail")
	}
	if !strings.Contains(err.Error(), "decode ThreadStartResponse.thread: missing required field") {
		t.Fatalf("unexpected missing thread start response thread error: %v", err)
	}

	var sessionSource SessionSource
	err = json.Unmarshal([]byte(`"mobile"`), &sessionSource)
	if err == nil {
		t.Fatal("expected unknown session source string variant to fail")
	}
	if !strings.Contains(err.Error(), `decode SessionSource.value: unknown variant "mobile"`) {
		t.Fatalf("unexpected unknown session source error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"custom":123}`), &sessionSource)
	if err == nil {
		t.Fatal("expected non-string session source custom value to fail")
	}
	if !strings.Contains(err.Error(), "decode SessionSource.custom.custom") {
		t.Fatalf("unexpected malformed session source custom error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"subAgent":{"thread_spawn":{}}}`), &sessionSource)
	if err == nil {
		t.Fatal("expected missing subagent thread_spawn fields to fail")
	}
	if !strings.Contains(err.Error(), "SubAgentSource.thread_spawn.depth") {
		t.Fatalf("unexpected malformed subagent source error: %v", err)
	}

	var threadStatus ThreadStatus
	err = json.Unmarshal([]byte(`{"type":"busy"}`), &threadStatus)
	if err == nil {
		t.Fatal("expected unknown thread status to fail")
	}
	if !strings.Contains(err.Error(), `decode ThreadStatus.type: unknown variant "busy"`) {
		t.Fatalf("unexpected unknown thread status error: %v", err)
	}

	_, err = json.Marshal(Thread{
		CliVersion:    "0.0.0-test",
		CreatedAt:     1,
		CWD:           "/workspace",
		Ephemeral:     false,
		ID:            "thread-1",
		ModelProvider: "openai",
		Preview:       "preview",
		SessionID:     "session-1",
		Source:        NewSessionSourceAppServer(),
		Status:        NewThreadStatusIdle(),
	})
	if err == nil {
		t.Fatal("expected nil thread turns marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode Thread.turns: nil is not allowed") {
		t.Fatalf("unexpected nil thread turns error: %v", err)
	}

	_, err = json.Marshal(NewThreadStatusActive(ThreadStatusActive{}))
	if err == nil {
		t.Fatal("expected nil active thread status flags marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode ThreadStatus.active.activeFlags: nil is not allowed") {
		t.Fatalf("unexpected nil thread status active flags error: %v", err)
	}

	_, err = json.Marshal(ThreadListResponse{})
	if err == nil {
		t.Fatal("expected nil thread list data marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode ThreadListResponse.data: nil is not allowed") {
		t.Fatalf("unexpected nil thread list data error: %v", err)
	}
}

func TestGeneratedGetAccountParamsOmitOptionalFields(t *testing.T) {
	raw, err := json.Marshal(GetAccountParams{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{}`; got != want {
		t.Fatalf("GetAccountParams JSON = %s, want %s", got, want)
	}

	refreshToken := true
	withRefresh, err := json.Marshal(GetAccountParams{RefreshToken: &refreshToken})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(withRefresh), `{"refreshToken":true}`; got != want {
		t.Fatalf("GetAccountParams refresh JSON = %s, want %s", got, want)
	}
}

func TestGeneratedGetAccountResponsePreservesNullableAccount(t *testing.T) {
	response := GetAccountResponse{
		Account:            Value(NewAccountAPIKey()),
		RequiresOpenaiAuth: false,
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"account":{"type":"apiKey"},"requiresOpenaiAuth":false}`
	if got := string(raw); got != want {
		t.Fatalf("GetAccountResponse JSON = %s, want %s", got, want)
	}

	var decoded GetAccountResponse
	if err := json.Unmarshal([]byte(`{"account":null,"requiresOpenaiAuth":true}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Account == nil || decoded.Account.Value != nil || !decoded.RequiresOpenaiAuth {
		t.Fatalf("decoded nullable account = %#v requiresOpenaiAuth=%t", decoded.Account, decoded.RequiresOpenaiAuth)
	}
}

func TestGeneratedAccountUnionMarshalAndAccessors(t *testing.T) {
	account := NewAccountAPIKey()
	if account.Kind() != AccountKindAPIKey {
		t.Fatalf("Account kind = %s, want %s", account.Kind(), AccountKindAPIKey)
	}
	if !account.IsValid() {
		t.Fatal("constructed Account should be valid")
	}
	if _, ok := account.AsAPIKey(); !ok {
		t.Fatal("AsAPIKey returned false for apiKey variant")
	}
	if _, ok := account.AsChatGPT(); ok {
		t.Fatal("AsChatGPT returned true for apiKey variant")
	}
	raw, err := json.Marshal(account)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"type":"apiKey"}`; got != want {
		t.Fatalf("Account JSON = %s, want %s", got, want)
	}
}

func TestGeneratedAccountUnionRejectsMalformedProtocol(t *testing.T) {
	_, err := json.Marshal(Account{})
	if err == nil {
		t.Fatal("expected zero-value Account marshal to fail")
	}
	if !strings.Contains(err.Error(), "invalid Account union value: no variant is set") {
		t.Fatalf("unexpected zero-value Account error: %v", err)
	}

	var account Account
	err = json.Unmarshal([]byte(`{"type":"chatgpt","email":"user@example.test","planType":"bogus"}`), &account)
	if err == nil {
		t.Fatal("expected invalid Account plan type decode to fail")
	}
	if !strings.Contains(err.Error(), `invalid PlanType enum value "bogus"`) {
		t.Fatalf("unexpected invalid Account plan type error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"type":"unknown"}`), &account)
	if err == nil {
		t.Fatal("expected unknown Account type to fail")
	}
	if !strings.Contains(err.Error(), `decode Account.type: unknown variant "unknown"`) {
		t.Fatalf("unexpected unknown Account type error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"type":"chatgpt","planType":"plus"}`), &account)
	if err == nil {
		t.Fatal("expected missing Account email to fail")
	}
	if !strings.Contains(err.Error(), "decode Account.email: missing required field") {
		t.Fatalf("unexpected missing Account email error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"type":"apiKey","extra":true}`), &account)
	if err == nil {
		t.Fatal("expected unknown Account field to fail")
	}
	if !strings.Contains(err.Error(), `decode Account.apiKey: unknown field "extra"`) {
		t.Fatalf("unexpected unknown Account field error: %v", err)
	}
}

func TestGeneratedAccountRateLimitsResponseMarshalAndUnmarshal(t *testing.T) {
	response := GetAccountRateLimitsResponse{
		RateLimits: RateLimitSnapshot{
			Credits: Value(CreditsSnapshot{
				Balance:    Null[string](),
				HasCredits: true,
				Unlimited:  false,
			}),
			LimitID:              Value("codex"),
			PlanType:             Value(PlanTypePlus),
			Primary:              Value(RateLimitWindow{ResetsAt: Value(int64(12345)), UsedPercent: 42, WindowDurationMins: Null[int64]()}),
			RateLimitReachedType: Value(RateLimitReachedTypeRateLimitReached),
		},
		RateLimitsByLimitID: Value(map[string]RateLimitSnapshot{
			"codex": {
				LimitName: Value("Codex"),
				Secondary: Value(RateLimitWindow{
					UsedPercent: 7,
				}),
			},
		}),
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"rateLimits":{"credits":{"balance":null,"hasCredits":true,"unlimited":false},"limitId":"codex","planType":"plus","primary":{"resetsAt":12345,"usedPercent":42,"windowDurationMins":null},"rateLimitReachedType":"rate_limit_reached"},"rateLimitsByLimitId":{"codex":{"limitName":"Codex","secondary":{"usedPercent":7}}}}`
	if got := string(raw); got != want {
		t.Fatalf("GetAccountRateLimitsResponse JSON = %s, want %s", got, want)
	}

	var decoded GetAccountRateLimitsResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.RateLimits.Primary == nil || decoded.RateLimits.Primary.Value == nil || decoded.RateLimits.Primary.Value.UsedPercent != 42 {
		t.Fatalf("decoded primary rate limit = %#v", decoded.RateLimits.Primary)
	}
	if decoded.RateLimitsByLimitID == nil || decoded.RateLimitsByLimitID.Value == nil || (*decoded.RateLimitsByLimitID.Value)["codex"].Secondary == nil {
		t.Fatalf("decoded rateLimitsByLimitId = %#v", decoded.RateLimitsByLimitID)
	}
}

func TestGeneratedAccountNotificationsPreserveNullableEnums(t *testing.T) {
	completedRaw, err := json.Marshal(AccountLoginCompletedNotification{
		Error:   Null[string](),
		LoginID: Value("login-1"),
		Success: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(completedRaw), `{"error":null,"loginId":"login-1","success":false}`; got != want {
		t.Fatalf("AccountLoginCompletedNotification JSON = %s, want %s", got, want)
	}

	raw, err := json.Marshal(AccountUpdatedNotification{
		AuthMode: Null[AuthMode](),
		PlanType: Value(PlanTypeTeam),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"authMode":null,"planType":"team"}`; got != want {
		t.Fatalf("AccountUpdatedNotification JSON = %s, want %s", got, want)
	}

	var decoded AccountUpdatedNotification
	if err := json.Unmarshal([]byte(`{"authMode":"chatgptAuthTokens","planType":null}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.AuthMode == nil || decoded.AuthMode.Value == nil || *decoded.AuthMode.Value != AuthModeChatGPTAuthTokens {
		t.Fatalf("decoded authMode = %#v", decoded.AuthMode)
	}
	if decoded.PlanType == nil || decoded.PlanType.Value != nil {
		t.Fatalf("decoded planType = %#v, want explicit null", decoded.PlanType)
	}
}

func TestGeneratedAccountFamilyRejectsMalformedProtocol(t *testing.T) {
	var accountResponse GetAccountResponse
	err := json.Unmarshal([]byte(`{"account":{"type":"apiKey"}}`), &accountResponse)
	if err == nil {
		t.Fatal("expected missing requiresOpenaiAuth to fail")
	}
	if !strings.Contains(err.Error(), "decode GetAccountResponse.requiresOpenaiAuth: missing required field") {
		t.Fatalf("unexpected missing requiresOpenaiAuth error: %v", err)
	}

	var rateLimitResponse GetAccountRateLimitsResponse
	err = json.Unmarshal([]byte(`{"rateLimitsByLimitId":null}`), &rateLimitResponse)
	if err == nil {
		t.Fatal("expected missing rateLimits to fail")
	}
	if !strings.Contains(err.Error(), "decode GetAccountRateLimitsResponse.rateLimits: missing required field") {
		t.Fatalf("unexpected missing rateLimits error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"rateLimits":{"planType":"bogus"}}`), &rateLimitResponse)
	if err == nil {
		t.Fatal("expected invalid rate limit plan type to fail")
	}
	if !strings.Contains(err.Error(), `invalid PlanType enum value "bogus"`) {
		t.Fatalf("unexpected invalid rate limit plan type error: %v", err)
	}

	var rateLimitNotification AccountRateLimitsUpdatedNotification
	err = json.Unmarshal([]byte(`{"rateLimits":{"unknown":true}}`), &rateLimitNotification)
	if err == nil {
		t.Fatal("expected unknown nested rate limit field to fail")
	}
	if !strings.Contains(err.Error(), `decode RateLimitSnapshot: unknown field "unknown"`) {
		t.Fatalf("unexpected unknown nested rate limit error: %v", err)
	}

	var accountUpdated AccountUpdatedNotification
	err = json.Unmarshal([]byte(`{"authMode":"bogus"}`), &accountUpdated)
	if err == nil {
		t.Fatal("expected invalid authMode to fail")
	}
	if !strings.Contains(err.Error(), `invalid AuthMode enum value "bogus"`) {
		t.Fatalf("unexpected invalid authMode error: %v", err)
	}

	raw, err := json.Marshal(LogoutAccountResponse{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{}`; got != want {
		t.Fatalf("LogoutAccountResponse JSON = %s, want %s", got, want)
	}
	var logout LogoutAccountResponse
	err = json.Unmarshal([]byte(`{"extra":true}`), &logout)
	if err == nil {
		t.Fatal("expected unknown LogoutAccountResponse field to fail")
	}
	if !strings.Contains(err.Error(), `decode LogoutAccountResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown LogoutAccountResponse field error: %v", err)
	}
}

func TestGeneratedFeedbackUploadMarshalAndUnmarshal(t *testing.T) {
	minimalRaw, err := json.Marshal(FeedbackUploadParams{
		Classification: "bug",
		IncludeLogs:    boolPtr(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(minimalRaw), `{"classification":"bug","includeLogs":true}`; got != want {
		t.Fatalf("minimal FeedbackUploadParams JSON = %s, want %s", got, want)
	}

	fullRaw, err := json.Marshal(FeedbackUploadParams{
		Classification: "bug",
		ExtraLogFiles:  Value([]string{"logs/extra.txt"}),
		IncludeLogs:    boolPtr(false),
		Reason:         Null[string](),
		Tags:           Value(map[string]string{"area": "sdk"}),
		ThreadID:       Value("thread-1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"classification":"bug","extraLogFiles":["logs/extra.txt"],"includeLogs":false,"reason":null,"tags":{"area":"sdk"},"threadId":"thread-1"}`
	if got := string(fullRaw); got != want {
		t.Fatalf("full FeedbackUploadParams JSON = %s, want %s", got, want)
	}

	var omitted FeedbackUploadParams
	err = json.Unmarshal([]byte(`{"classification":"bug","includeLogs":true}`), &omitted)
	if err != nil {
		t.Fatal(err)
	}
	if omitted.ExtraLogFiles != nil || omitted.Reason != nil || omitted.Tags != nil || omitted.ThreadID != nil {
		t.Fatalf("decoded omitted feedback optional fields = %#v", omitted)
	}

	var decoded FeedbackUploadParams
	err = json.Unmarshal([]byte(`{"classification":"bug","extraLogFiles":["logs/extra.txt"],"includeLogs":false,"reason":"because","tags":{"area":"sdk"},"threadId":"thread-1"}`), &decoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ExtraLogFiles == nil || decoded.ExtraLogFiles.Value == nil || len(*decoded.ExtraLogFiles.Value) != 1 || (*decoded.ExtraLogFiles.Value)[0] != "logs/extra.txt" {
		t.Fatalf("decoded extraLogFiles = %#v", decoded.ExtraLogFiles)
	}
	if decoded.Reason == nil || decoded.Reason.Value == nil || *decoded.Reason.Value != "because" {
		t.Fatalf("decoded reason = %#v", decoded.Reason)
	}
	if decoded.Tags == nil || decoded.Tags.Value == nil || (*decoded.Tags.Value)["area"] != "sdk" {
		t.Fatalf("decoded tags = %#v", decoded.Tags)
	}
	if decoded.ThreadID == nil || decoded.ThreadID.Value == nil || *decoded.ThreadID.Value != "thread-1" {
		t.Fatalf("decoded threadId = %#v", decoded.ThreadID)
	}

	var nulls FeedbackUploadParams
	err = json.Unmarshal([]byte(`{"classification":"bug","extraLogFiles":null,"includeLogs":false,"reason":null,"tags":null,"threadId":null}`), &nulls)
	if err != nil {
		t.Fatal(err)
	}
	if nulls.ExtraLogFiles == nil || nulls.ExtraLogFiles.Value != nil {
		t.Fatalf("decoded null extraLogFiles = %#v", nulls.ExtraLogFiles)
	}
	if nulls.Reason == nil || nulls.Reason.Value != nil {
		t.Fatalf("decoded null reason = %#v", nulls.Reason)
	}
	if nulls.Tags == nil || nulls.Tags.Value != nil {
		t.Fatalf("decoded null tags = %#v", nulls.Tags)
	}
	if nulls.ThreadID == nil || nulls.ThreadID.Value != nil {
		t.Fatalf("decoded null threadId = %#v", nulls.ThreadID)
	}

	responseRaw, err := json.Marshal(FeedbackUploadResponse{ThreadID: "thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(responseRaw), `{"threadId":"thread-1"}`; got != want {
		t.Fatalf("FeedbackUploadResponse JSON = %s, want %s", got, want)
	}

	var response FeedbackUploadResponse
	if err := json.Unmarshal(responseRaw, &response); err != nil {
		t.Fatal(err)
	}
	if response.ThreadID != "thread-1" {
		t.Fatalf("decoded FeedbackUploadResponse = %#v", response)
	}
}

func TestGeneratedFeedbackUploadRejectsMalformedProtocol(t *testing.T) {
	var params FeedbackUploadParams
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "missing classification",
			raw:  `{"includeLogs":true}`,
			want: "decode FeedbackUploadParams.classification: missing required field",
		},
		{
			name: "null classification",
			raw:  `{"classification":null,"includeLogs":true}`,
			want: "decode FeedbackUploadParams.classification: null is not allowed",
		},
		{
			name: "null includeLogs",
			raw:  `{"classification":"bug","includeLogs":null}`,
			want: "decode FeedbackUploadParams.includeLogs: null is not allowed",
		},
		{
			name: "non string tag value",
			raw:  `{"classification":"bug","includeLogs":true,"tags":{"area":1}}`,
			want: "decode FeedbackUploadParams.tags:",
		},
		{
			name: "non string extra log file",
			raw:  `{"classification":"bug","extraLogFiles":[1],"includeLogs":true}`,
			want: "decode FeedbackUploadParams.extraLogFiles:",
		},
		{
			name: "non string reason",
			raw:  `{"classification":"bug","includeLogs":true,"reason":1}`,
			want: "decode FeedbackUploadParams.reason:",
		},
		{
			name: "non string thread id",
			raw:  `{"classification":"bug","includeLogs":true,"threadId":1}`,
			want: "decode FeedbackUploadParams.threadId:",
		},
		{
			name: "unknown field",
			raw:  `{"classification":"bug","includeLogs":true,"extra":true}`,
			want: `decode FeedbackUploadParams: unknown field "extra"`,
		},
		{
			name: "top level non object",
			raw:  `[]`,
			want: "decode FeedbackUploadParams: expected object",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := json.Unmarshal([]byte(tc.raw), &params)
			if err == nil {
				t.Fatal("expected malformed feedback upload params to fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected malformed feedback upload params error: %v", err)
			}
		})
	}

	var response FeedbackUploadResponse
	err := json.Unmarshal([]byte(`{}`), &response)
	if err == nil {
		t.Fatal("expected missing feedback response threadId to fail")
	}
	if !strings.Contains(err.Error(), "decode FeedbackUploadResponse.threadId: missing required field") {
		t.Fatalf("unexpected missing feedback response threadId error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"threadId":null}`), &response)
	if err == nil {
		t.Fatal("expected null feedback response threadId to fail")
	}
	if !strings.Contains(err.Error(), "decode FeedbackUploadResponse.threadId: null is not allowed") {
		t.Fatalf("unexpected null feedback response threadId error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"threadId":"thread-1","extra":true}`), &response)
	if err == nil {
		t.Fatal("expected unknown feedback response field to fail")
	}
	if !strings.Contains(err.Error(), `decode FeedbackUploadResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown feedback response field error: %v", err)
	}
}

func TestGeneratedCollaborationModeProtocolMarshalAndUnmarshal(t *testing.T) {
	paramsRaw, err := json.Marshal(CollaborationModeListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(paramsRaw), `{}`; got != want {
		t.Fatalf("CollaborationModeListParams JSON = %s, want %s", got, want)
	}

	var params CollaborationModeListParams
	err = json.Unmarshal([]byte(`{}`), &params)
	if err != nil {
		t.Fatal(err)
	}

	response := CollaborationModeListResponse{
		Data: []CollaborationModeMask{{
			Mode:            Value(ModeKindPlan),
			Model:           Null[string](),
			Name:            "Plan",
			ReasoningEffort: Value(ReasoningEffort("medium")),
		}, {
			Name: "Default",
		}},
	}
	responseRaw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"data":[{"mode":"plan","model":null,"name":"Plan","reasoning_effort":"medium"},{"name":"Default"}]}`
	if got := string(responseRaw); got != want {
		t.Fatalf("CollaborationModeListResponse JSON = %s, want %s", got, want)
	}

	var decoded CollaborationModeListResponse
	err = json.Unmarshal([]byte(want), &decoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Data) != 2 || decoded.Data[0].Name != "Plan" || decoded.Data[1].Name != "Default" {
		t.Fatalf("decoded collaboration mode response = %#v", decoded)
	}
	if decoded.Data[0].Mode == nil || decoded.Data[0].Mode.Value == nil || *decoded.Data[0].Mode.Value != ModeKindPlan {
		t.Fatalf("decoded collaboration mode kind = %#v", decoded.Data[0].Mode)
	}
	if decoded.Data[0].Model == nil || decoded.Data[0].Model.Value != nil {
		t.Fatalf("decoded collaboration mode model = %#v, want explicit null", decoded.Data[0].Model)
	}
	if decoded.Data[0].ReasoningEffort == nil || decoded.Data[0].ReasoningEffort.Value == nil || *decoded.Data[0].ReasoningEffort.Value != ReasoningEffort("medium") {
		t.Fatalf("decoded collaboration mode reasoning effort = %#v", decoded.Data[0].ReasoningEffort)
	}
	if decoded.Data[1].Mode != nil || decoded.Data[1].Model != nil || decoded.Data[1].ReasoningEffort != nil {
		t.Fatalf("decoded omitted collaboration mode fields = %#v", decoded.Data[1])
	}

	var nullableDecoded CollaborationModeListResponse
	err = json.Unmarshal([]byte(`{"data":[{"mode":null,"model":"gpt-5","name":"Default","reasoning_effort":null}]}`), &nullableDecoded)
	if err != nil {
		t.Fatal(err)
	}
	item := nullableDecoded.Data[0]
	if item.Mode == nil || item.Mode.Value != nil {
		t.Fatalf("decoded collaboration mode null mode = %#v", item.Mode)
	}
	if item.Model == nil || item.Model.Value == nil || *item.Model.Value != "gpt-5" {
		t.Fatalf("decoded collaboration mode model = %#v", item.Model)
	}
	if item.ReasoningEffort == nil || item.ReasoningEffort.Value != nil {
		t.Fatalf("decoded collaboration mode null reasoning effort = %#v", item.ReasoningEffort)
	}
}

func TestGeneratedCollaborationModeRejectsMalformedProtocol(t *testing.T) {
	var params CollaborationModeListParams
	err := json.Unmarshal([]byte(`{"extra":true}`), &params)
	if err == nil {
		t.Fatal("expected unknown collaboration mode params field to fail")
	}
	if !strings.Contains(err.Error(), `decode CollaborationModeListParams: unknown field "extra"`) {
		t.Fatalf("unexpected unknown collaboration mode params field error: %v", err)
	}

	err = json.Unmarshal([]byte(`[]`), &params)
	if err == nil {
		t.Fatal("expected non-object collaboration mode params to fail")
	}
	if !strings.Contains(err.Error(), "decode CollaborationModeListParams: expected object") {
		t.Fatalf("unexpected collaboration mode params non-object error: %v", err)
	}

	var response CollaborationModeListResponse
	err = json.Unmarshal([]byte(`{}`), &response)
	if err == nil {
		t.Fatal("expected missing collaboration mode data to fail")
	}
	if !strings.Contains(err.Error(), "decode CollaborationModeListResponse.data: missing required field") {
		t.Fatalf("unexpected missing collaboration mode data error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"data":null}`), &response)
	if err == nil {
		t.Fatal("expected null collaboration mode data to fail")
	}
	if !strings.Contains(err.Error(), "decode CollaborationModeListResponse.data: null is not allowed") {
		t.Fatalf("unexpected null collaboration mode data error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"data":[{"mode":"plan"}]}`), &response)
	if err == nil {
		t.Fatal("expected missing collaboration mode name to fail")
	}
	if !strings.Contains(err.Error(), "decode CollaborationModeMask.name: missing required field") {
		t.Fatalf("unexpected missing collaboration mode name error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"data":[{"name":null}]}`), &response)
	if err == nil {
		t.Fatal("expected null collaboration mode name to fail")
	}
	if !strings.Contains(err.Error(), "decode CollaborationModeMask.name: null is not allowed") {
		t.Fatalf("unexpected null collaboration mode name error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"data":[{"mode":"pair","name":"Pair"}]}`), &response)
	if err == nil {
		t.Fatal("expected unknown collaboration mode enum to fail")
	}
	if !strings.Contains(err.Error(), "decode CollaborationModeMask.mode:") {
		t.Fatalf("unexpected unknown collaboration mode enum error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"data":[{"name":"Plan","extra":true}]}`), &response)
	if err == nil {
		t.Fatal("expected unknown collaboration mode mask field to fail")
	}
	if !strings.Contains(err.Error(), `decode CollaborationModeMask: unknown field "extra"`) {
		t.Fatalf("unexpected unknown collaboration mode mask field error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"data":[],"extra":true}`), &response)
	if err == nil {
		t.Fatal("expected unknown collaboration mode response field to fail")
	}
	if !strings.Contains(err.Error(), `decode CollaborationModeListResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown collaboration mode response field error: %v", err)
	}

	_, err = json.Marshal(CollaborationModeListResponse{})
	if err == nil {
		t.Fatal("expected nil collaboration mode data marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode CollaborationModeListResponse.data: nil is not allowed") {
		t.Fatalf("unexpected nil collaboration mode data error: %v", err)
	}
}

func TestGeneratedAppListProtocolMarshalAndUnmarshal(t *testing.T) {
	paramsMinimal, err := json.Marshal(AppsListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(paramsMinimal), `{}`; got != want {
		t.Fatalf("AppsListParams minimal JSON = %s, want %s", got, want)
	}

	paramsFull, err := json.Marshal(AppsListParams{
		Cursor:       Null[string](),
		ForceRefetch: boolPtr(true),
		Limit:        Value(uint32(25)),
		ThreadID:     Value("thread-1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(paramsFull), `{"cursor":null,"forceRefetch":true,"limit":25,"threadId":"thread-1"}`; got != want {
		t.Fatalf("AppsListParams full JSON = %s, want %s", got, want)
	}

	var paramsDecoded AppsListParams
	err = json.Unmarshal([]byte(`{"cursor":"next","limit":null,"threadId":null}`), &paramsDecoded)
	if err != nil {
		t.Fatal(err)
	}
	if paramsDecoded.Cursor == nil || paramsDecoded.Cursor.Value == nil || *paramsDecoded.Cursor.Value != "next" {
		t.Fatalf("decoded app list cursor = %#v", paramsDecoded.Cursor)
	}
	if paramsDecoded.Limit == nil || paramsDecoded.Limit.Value != nil {
		t.Fatalf("decoded app list limit = %#v, want explicit null", paramsDecoded.Limit)
	}
	if paramsDecoded.ThreadID == nil || paramsDecoded.ThreadID.Value != nil {
		t.Fatalf("decoded app list threadId = %#v, want explicit null", paramsDecoded.ThreadID)
	}

	response := AppsListResponse{
		Data: []AppInfo{{
			AppMetadata: Value(AppMetadata{
				Categories: Value([]string{"productivity"}),
				Review:     Value(AppReview{Status: "approved"}),
				Screenshots: Value([]AppScreenshot{{
					FileID:     Null[string](),
					URL:        Value("https://example.com/s.png"),
					UserPrompt: "show it",
				}}),
			}),
			Branding: Value(AppBranding{
				IsDiscoverableApp: true,
				Website:           Value("https://example.com"),
			}),
			Description:        Value("desc"),
			ID:                 "app-1",
			IsAccessible:       boolPtr(true),
			IsEnabled:          boolPtr(false),
			Labels:             Value(map[string]string{"tier": "first-party"}),
			Name:               "App One",
			PluginDisplayNames: &[]string{"Plugin A"},
		}},
		NextCursor: Value("next"),
	}
	responseRaw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"data":[{"appMetadata":{"categories":["productivity"],"review":{"status":"approved"},"screenshots":[{"fileId":null,"url":"https://example.com/s.png","userPrompt":"show it"}]},"branding":{"isDiscoverableApp":true,"website":"https://example.com"},"description":"desc","id":"app-1","isAccessible":true,"isEnabled":false,"labels":{"tier":"first-party"},"name":"App One","pluginDisplayNames":["Plugin A"]}],"nextCursor":"next"}`
	if got := string(responseRaw); got != want {
		t.Fatalf("AppsListResponse JSON = %s, want %s", got, want)
	}

	var responseDecoded AppsListResponse
	err = json.Unmarshal([]byte(want), &responseDecoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(responseDecoded.Data) != 1 || responseDecoded.Data[0].ID != "app-1" || responseDecoded.Data[0].Name != "App One" {
		t.Fatalf("decoded app list response = %#v", responseDecoded)
	}
	if responseDecoded.NextCursor == nil || responseDecoded.NextCursor.Value == nil || *responseDecoded.NextCursor.Value != "next" {
		t.Fatalf("decoded app list nextCursor = %#v", responseDecoded.NextCursor)
	}
	if responseDecoded.Data[0].Labels == nil || responseDecoded.Data[0].Labels.Value == nil || (*responseDecoded.Data[0].Labels.Value)["tier"] != "first-party" {
		t.Fatalf("decoded app info labels = %#v", responseDecoded.Data[0].Labels)
	}

	var nullableDecoded AppsListResponse
	err = json.Unmarshal([]byte(`{"data":[{"id":"app-2","name":"App Two","appMetadata":null,"branding":null,"labels":null}],"nextCursor":null}`), &nullableDecoded)
	if err != nil {
		t.Fatal(err)
	}
	app := nullableDecoded.Data[0]
	if app.AppMetadata == nil || app.AppMetadata.Value != nil {
		t.Fatalf("decoded appMetadata = %#v, want explicit null", app.AppMetadata)
	}
	if app.Branding == nil || app.Branding.Value != nil {
		t.Fatalf("decoded branding = %#v, want explicit null", app.Branding)
	}
	if app.Labels == nil || app.Labels.Value != nil {
		t.Fatalf("decoded labels = %#v, want explicit null", app.Labels)
	}
	if nullableDecoded.NextCursor == nil || nullableDecoded.NextCursor.Value != nil {
		t.Fatalf("decoded nextCursor = %#v, want explicit null", nullableDecoded.NextCursor)
	}

	notificationRaw, err := json.Marshal(AppListUpdatedNotification{Data: response.Data})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(notificationRaw), `{"data":[{"appMetadata":{"categories":["productivity"],"review":{"status":"approved"},"screenshots":[{"fileId":null,"url":"https://example.com/s.png","userPrompt":"show it"}]},"branding":{"isDiscoverableApp":true,"website":"https://example.com"},"description":"desc","id":"app-1","isAccessible":true,"isEnabled":false,"labels":{"tier":"first-party"},"name":"App One","pluginDisplayNames":["Plugin A"]}]}`; got != want {
		t.Fatalf("AppListUpdatedNotification JSON = %s, want %s", got, want)
	}

	var notificationDecoded AppListUpdatedNotification
	err = json.Unmarshal(notificationRaw, &notificationDecoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(notificationDecoded.Data) != 1 || notificationDecoded.Data[0].ID != "app-1" || notificationDecoded.Data[0].Name != "App One" {
		t.Fatalf("decoded app list notification = %#v", notificationDecoded)
	}
}

func TestGeneratedAppListRejectsMalformedProtocol(t *testing.T) {
	var params AppsListParams
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "null force refetch",
			raw:  `{"forceRefetch":null}`,
			want: "decode AppsListParams.forceRefetch: null is not allowed",
		},
		{
			name: "negative limit",
			raw:  `{"limit":-1}`,
			want: "decode AppsListParams.limit:",
		},
		{
			name: "unknown params field",
			raw:  `{"extra":true}`,
			want: `decode AppsListParams: unknown field "extra"`,
		},
		{
			name: "top level non object",
			raw:  `[]`,
			want: "decode AppsListParams: expected object",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := json.Unmarshal([]byte(tc.raw), &params)
			if err == nil {
				t.Fatal("expected malformed app list params to fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected malformed app list params error: %v", err)
			}
		})
	}

	var response AppsListResponse
	err := json.Unmarshal([]byte(`{}`), &response)
	if err == nil {
		t.Fatal("expected missing app list data to fail")
	}
	if !strings.Contains(err.Error(), "decode AppsListResponse.data: missing required field") {
		t.Fatalf("unexpected missing app list data error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"data":null}`), &response)
	if err == nil {
		t.Fatal("expected null app list data to fail")
	}
	if !strings.Contains(err.Error(), "decode AppsListResponse.data: null is not allowed") {
		t.Fatalf("unexpected null app list data error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"data":[{"name":"App One"}]}`), &response)
	if err == nil {
		t.Fatal("expected missing app info id to fail")
	}
	if !strings.Contains(err.Error(), "decode AppInfo.id: missing required field") {
		t.Fatalf("unexpected missing app info id error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"data":[{"id":"app-1"}]}`), &response)
	if err == nil {
		t.Fatal("expected missing app info name to fail")
	}
	if !strings.Contains(err.Error(), "decode AppInfo.name: missing required field") {
		t.Fatalf("unexpected missing app info name error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"data":[{"id":"app-1","name":"App One","labels":{"tier":1}}]}`), &response)
	if err == nil {
		t.Fatal("expected non-string app info label to fail")
	}
	if !strings.Contains(err.Error(), "decode AppInfo.labels:") {
		t.Fatalf("unexpected app info labels error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"data":[{"id":"app-1","name":"App One","extra":true}]}`), &response)
	if err == nil {
		t.Fatal("expected unknown app info field to fail")
	}
	if !strings.Contains(err.Error(), `decode AppInfo: unknown field "extra"`) {
		t.Fatalf("unexpected unknown app info field error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"data":[{"id":"app-1","name":"App One","branding":{}}]}`), &response)
	if err == nil {
		t.Fatal("expected missing app branding discoverability to fail")
	}
	if !strings.Contains(err.Error(), "decode AppBranding.isDiscoverableApp: missing required field") {
		t.Fatalf("unexpected missing app branding field error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"data":[{"id":"app-1","name":"App One","branding":{"isDiscoverableApp":true,"extra":true}}]}`), &response)
	if err == nil {
		t.Fatal("expected unknown app branding field to fail")
	}
	if !strings.Contains(err.Error(), `decode AppBranding: unknown field "extra"`) {
		t.Fatalf("unexpected unknown app branding field error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"data":[{"id":"app-1","name":"App One","appMetadata":{"review":{}}}]}`), &response)
	if err == nil {
		t.Fatal("expected missing app review status to fail")
	}
	if !strings.Contains(err.Error(), "decode AppReview.status: missing required field") {
		t.Fatalf("unexpected missing app review status error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"data":[{"id":"app-1","name":"App One","appMetadata":{"extra":true}}]}`), &response)
	if err == nil {
		t.Fatal("expected unknown app metadata field to fail")
	}
	if !strings.Contains(err.Error(), `decode AppMetadata: unknown field "extra"`) {
		t.Fatalf("unexpected unknown app metadata field error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"data":[{"id":"app-1","name":"App One","appMetadata":{"screenshots":[{}]}}]}`), &response)
	if err == nil {
		t.Fatal("expected missing app screenshot userPrompt to fail")
	}
	if !strings.Contains(err.Error(), "decode AppScreenshot.userPrompt: missing required field") {
		t.Fatalf("unexpected missing app screenshot userPrompt error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"data":[{"id":"app-1","name":"App One","appMetadata":{"screenshots":[{"userPrompt":"show it","extra":true}]}}]}`), &response)
	if err == nil {
		t.Fatal("expected unknown app screenshot field to fail")
	}
	if !strings.Contains(err.Error(), `decode AppScreenshot: unknown field "extra"`) {
		t.Fatalf("unexpected unknown app screenshot field error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"data":[],"extra":true}`), &response)
	if err == nil {
		t.Fatal("expected unknown app list response field to fail")
	}
	if !strings.Contains(err.Error(), `decode AppsListResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown app list response field error: %v", err)
	}

	var notification AppListUpdatedNotification
	err = json.Unmarshal([]byte(`{}`), &notification)
	if err == nil {
		t.Fatal("expected missing app list notification data to fail")
	}
	if !strings.Contains(err.Error(), "decode AppListUpdatedNotification.data: missing required field") {
		t.Fatalf("unexpected missing app list notification data error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"data":null}`), &notification)
	if err == nil {
		t.Fatal("expected null app list notification data to fail")
	}
	if !strings.Contains(err.Error(), "decode AppListUpdatedNotification.data: null is not allowed") {
		t.Fatalf("unexpected null app list notification data error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"data":[],"extra":true}`), &notification)
	if err == nil {
		t.Fatal("expected unknown app list notification field to fail")
	}
	if !strings.Contains(err.Error(), `decode AppListUpdatedNotification: unknown field "extra"`) {
		t.Fatalf("unexpected unknown app list notification field error: %v", err)
	}

	_, err = json.Marshal(AppsListResponse{})
	if err == nil {
		t.Fatal("expected nil app list response data marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode AppsListResponse.data: nil is not allowed") {
		t.Fatalf("unexpected nil app list response data error: %v", err)
	}

	_, err = json.Marshal(AppListUpdatedNotification{})
	if err == nil {
		t.Fatal("expected nil app list notification data marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode AppListUpdatedNotification.data: nil is not allowed") {
		t.Fatalf("unexpected nil app list notification data error: %v", err)
	}
}

func TestGeneratedMarketplaceProtocolMarshalAndUnmarshal(t *testing.T) {
	addMinimal, err := json.Marshal(MarketplaceAddParams{Source: "github:org/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(addMinimal), `{"source":"github:org/repo"}`; got != want {
		t.Fatalf("MarketplaceAddParams minimal JSON = %s, want %s", got, want)
	}

	addFull, err := json.Marshal(MarketplaceAddParams{
		RefName:     Null[string](),
		Source:      "github:org/repo",
		SparsePaths: Value([]string{"plugins/a"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(addFull), `{"refName":null,"source":"github:org/repo","sparsePaths":["plugins/a"]}`; got != want {
		t.Fatalf("MarketplaceAddParams full JSON = %s, want %s", got, want)
	}

	var addDecoded MarketplaceAddParams
	err = json.Unmarshal([]byte(`{"refName":"main","source":"github:org/repo","sparsePaths":null}`), &addDecoded)
	if err != nil {
		t.Fatal(err)
	}
	if addDecoded.RefName == nil || addDecoded.RefName.Value == nil || *addDecoded.RefName.Value != "main" {
		t.Fatalf("decoded add refName = %#v", addDecoded.RefName)
	}
	if addDecoded.SparsePaths == nil || addDecoded.SparsePaths.Value != nil {
		t.Fatalf("decoded add sparsePaths = %#v, want explicit null", addDecoded.SparsePaths)
	}

	removeParamsRaw, err := json.Marshal(MarketplaceRemoveParams{MarketplaceName: "repo"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(removeParamsRaw), `{"marketplaceName":"repo"}`; got != want {
		t.Fatalf("MarketplaceRemoveParams JSON = %s, want %s", got, want)
	}

	addResponseRaw, err := json.Marshal(MarketplaceAddResponse{
		AlreadyAdded:    true,
		InstalledRoot:   "/marketplaces/repo",
		MarketplaceName: "repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(addResponseRaw), `{"alreadyAdded":true,"installedRoot":"/marketplaces/repo","marketplaceName":"repo"}`; got != want {
		t.Fatalf("MarketplaceAddResponse JSON = %s, want %s", got, want)
	}

	removeResponseOmitted, err := json.Marshal(MarketplaceRemoveResponse{
		MarketplaceName: "repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(removeResponseOmitted), `{"marketplaceName":"repo"}`; got != want {
		t.Fatalf("MarketplaceRemoveResponse omitted root JSON = %s, want %s", got, want)
	}

	removeResponseNull, err := json.Marshal(MarketplaceRemoveResponse{
		InstalledRoot:   Null[string](),
		MarketplaceName: "repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(removeResponseNull), `{"installedRoot":null,"marketplaceName":"repo"}`; got != want {
		t.Fatalf("MarketplaceRemoveResponse null root JSON = %s, want %s", got, want)
	}

	removeResponseValue, err := json.Marshal(MarketplaceRemoveResponse{
		InstalledRoot:   Value("/marketplaces/repo"),
		MarketplaceName: "repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(removeResponseValue), `{"installedRoot":"/marketplaces/repo","marketplaceName":"repo"}`; got != want {
		t.Fatalf("MarketplaceRemoveResponse value root JSON = %s, want %s", got, want)
	}

	upgradeParamsOmitted, err := json.Marshal(MarketplaceUpgradeParams{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(upgradeParamsOmitted), `{}`; got != want {
		t.Fatalf("MarketplaceUpgradeParams omitted JSON = %s, want %s", got, want)
	}

	upgradeParamsNull, err := json.Marshal(MarketplaceUpgradeParams{MarketplaceName: Null[string]()})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(upgradeParamsNull), `{"marketplaceName":null}`; got != want {
		t.Fatalf("MarketplaceUpgradeParams null JSON = %s, want %s", got, want)
	}

	upgradeParamsRaw, err := json.Marshal(MarketplaceUpgradeParams{MarketplaceName: Value("repo")})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(upgradeParamsRaw), `{"marketplaceName":"repo"}`; got != want {
		t.Fatalf("MarketplaceUpgradeParams JSON = %s, want %s", got, want)
	}

	upgradeResponseRaw, err := json.Marshal(MarketplaceUpgradeResponse{
		Errors: []MarketplaceUpgradeErrorInfo{{
			MarketplaceName: "repo",
			Message:         "failed",
		}},
		SelectedMarketplaces: []string{"repo"},
		UpgradedRoots:        []string{"/marketplaces/repo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"errors":[{"marketplaceName":"repo","message":"failed"}],"selectedMarketplaces":["repo"],"upgradedRoots":["/marketplaces/repo"]}`
	if got := string(upgradeResponseRaw); got != want {
		t.Fatalf("MarketplaceUpgradeResponse JSON = %s, want %s", got, want)
	}

	var upgradeDecoded MarketplaceUpgradeResponse
	err = json.Unmarshal([]byte(`{"errors":[],"selectedMarketplaces":[],"upgradedRoots":[]}`), &upgradeDecoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(upgradeDecoded.Errors) != 0 || len(upgradeDecoded.SelectedMarketplaces) != 0 || len(upgradeDecoded.UpgradedRoots) != 0 {
		t.Fatalf("decoded empty upgrade response = %#v", upgradeDecoded)
	}
}

func TestGeneratedMarketplaceRejectsMalformedProtocol(t *testing.T) {
	var add MarketplaceAddParams
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "missing add source",
			raw:  `{}`,
			want: "decode MarketplaceAddParams.source: missing required field",
		},
		{
			name: "null add source",
			raw:  `{"source":null}`,
			want: "decode MarketplaceAddParams.source: null is not allowed",
		},
		{
			name: "non string sparse path",
			raw:  `{"source":"repo","sparsePaths":[1]}`,
			want: "decode MarketplaceAddParams.sparsePaths:",
		},
		{
			name: "unknown add field",
			raw:  `{"source":"repo","extra":true}`,
			want: `decode MarketplaceAddParams: unknown field "extra"`,
		},
		{
			name: "top level non object",
			raw:  `[]`,
			want: "decode MarketplaceAddParams: expected object",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := json.Unmarshal([]byte(tc.raw), &add)
			if err == nil {
				t.Fatal("expected malformed marketplace add params to fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected malformed marketplace add params error: %v", err)
			}
		})
	}

	var remove MarketplaceRemoveParams
	err := json.Unmarshal([]byte(`{}`), &remove)
	if err == nil {
		t.Fatal("expected missing marketplace remove name to fail")
	}
	if !strings.Contains(err.Error(), "decode MarketplaceRemoveParams.marketplaceName: missing required field") {
		t.Fatalf("unexpected missing remove name error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"marketplaceName":null}`), &remove)
	if err == nil {
		t.Fatal("expected null marketplace remove name to fail")
	}
	if !strings.Contains(err.Error(), "decode MarketplaceRemoveParams.marketplaceName: null is not allowed") {
		t.Fatalf("unexpected null remove name error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"marketplaceName":"repo","extra":true}`), &remove)
	if err == nil {
		t.Fatal("expected unknown marketplace remove field to fail")
	}
	if !strings.Contains(err.Error(), `decode MarketplaceRemoveParams: unknown field "extra"`) {
		t.Fatalf("unexpected unknown remove field error: %v", err)
	}

	var addResponse MarketplaceAddResponse
	err = json.Unmarshal([]byte(`{"alreadyAdded":false,"marketplaceName":"repo"}`), &addResponse)
	if err == nil {
		t.Fatal("expected missing add response installedRoot to fail")
	}
	if !strings.Contains(err.Error(), "decode MarketplaceAddResponse.installedRoot: missing required field") {
		t.Fatalf("unexpected missing add response installedRoot error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"alreadyAdded":false,"installedRoot":"/repo","marketplaceName":"repo","extra":true}`), &addResponse)
	if err == nil {
		t.Fatal("expected unknown add response field to fail")
	}
	if !strings.Contains(err.Error(), `decode MarketplaceAddResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown add response field error: %v", err)
	}

	var removeResponse MarketplaceRemoveResponse
	err = json.Unmarshal([]byte(`{"marketplaceName":"repo","installedRoot":"repo"}`), &removeResponse)
	if err != nil {
		t.Fatal(err)
	}
	if removeResponse.InstalledRoot == nil || removeResponse.InstalledRoot.Value == nil || *removeResponse.InstalledRoot.Value != "repo" {
		t.Fatalf("decoded remove response installedRoot = %#v", removeResponse.InstalledRoot)
	}

	err = json.Unmarshal([]byte(`{"marketplaceName":"repo","extra":true}`), &removeResponse)
	if err == nil {
		t.Fatal("expected unknown remove response field to fail")
	}
	if !strings.Contains(err.Error(), `decode MarketplaceRemoveResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown remove response field error: %v", err)
	}

	var upgradeParams MarketplaceUpgradeParams
	err = json.Unmarshal([]byte(`{"marketplaceName":null,"extra":true}`), &upgradeParams)
	if err == nil {
		t.Fatal("expected unknown upgrade params field to fail")
	}
	if !strings.Contains(err.Error(), `decode MarketplaceUpgradeParams: unknown field "extra"`) {
		t.Fatalf("unexpected unknown upgrade params field error: %v", err)
	}

	var upgradeResponse MarketplaceUpgradeResponse
	err = json.Unmarshal([]byte(`{"selectedMarketplaces":[],"upgradedRoots":[]}`), &upgradeResponse)
	if err == nil {
		t.Fatal("expected missing upgrade errors to fail")
	}
	if !strings.Contains(err.Error(), "decode MarketplaceUpgradeResponse.errors: missing required field") {
		t.Fatalf("unexpected missing upgrade errors error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"errors":[],"selectedMarketplaces":null,"upgradedRoots":[]}`), &upgradeResponse)
	if err == nil {
		t.Fatal("expected null selected marketplaces to fail")
	}
	if !strings.Contains(err.Error(), "decode MarketplaceUpgradeResponse.selectedMarketplaces: null is not allowed") {
		t.Fatalf("unexpected null selected marketplaces error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"errors":[{"marketplaceName":"repo"}],"selectedMarketplaces":[],"upgradedRoots":[]}`), &upgradeResponse)
	if err == nil {
		t.Fatal("expected missing upgrade error message to fail")
	}
	if !strings.Contains(err.Error(), "decode MarketplaceUpgradeErrorInfo.message: missing required field") {
		t.Fatalf("unexpected missing upgrade error message error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"errors":[{"marketplaceName":null,"message":"failed"}],"selectedMarketplaces":[],"upgradedRoots":[]}`), &upgradeResponse)
	if err == nil {
		t.Fatal("expected null upgrade error marketplace name to fail")
	}
	if !strings.Contains(err.Error(), "decode MarketplaceUpgradeErrorInfo.marketplaceName: null is not allowed") {
		t.Fatalf("unexpected null upgrade error marketplace name error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"errors":[{"marketplaceName":"repo","message":"failed","extra":true}],"selectedMarketplaces":[],"upgradedRoots":[]}`), &upgradeResponse)
	if err == nil {
		t.Fatal("expected unknown upgrade error info field to fail")
	}
	if !strings.Contains(err.Error(), `decode MarketplaceUpgradeErrorInfo: unknown field "extra"`) {
		t.Fatalf("unexpected unknown upgrade error info field error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"errors":[1],"selectedMarketplaces":[],"upgradedRoots":[]}`), &upgradeResponse)
	if err == nil {
		t.Fatal("expected non object upgrade error info to fail")
	}
	if !strings.Contains(err.Error(), "decode MarketplaceUpgradeErrorInfo: expected object") {
		t.Fatalf("unexpected non object upgrade error info error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"errors":[],"selectedMarketplaces":[],"upgradedRoots":[],"extra":true}`), &upgradeResponse)
	if err == nil {
		t.Fatal("expected unknown upgrade response field to fail")
	}
	if !strings.Contains(err.Error(), `decode MarketplaceUpgradeResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown upgrade response field error: %v", err)
	}

	_, err = json.Marshal(MarketplaceUpgradeResponse{})
	if err == nil {
		t.Fatal("expected nil marketplace upgrade arrays marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode MarketplaceUpgradeResponse.errors: nil is not allowed") {
		t.Fatalf("unexpected nil upgrade arrays error: %v", err)
	}
}

func TestGeneratedModelListParamsPreserveNullableFields(t *testing.T) {
	raw, err := json.Marshal(ModelListParams{
		Cursor:        Null[string](),
		IncludeHidden: Value(true),
		Limit:         Value(uint32(20)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"cursor":null,"includeHidden":true,"limit":20}`; got != want {
		t.Fatalf("ModelListParams JSON = %s, want %s", got, want)
	}

	var decoded ModelListParams
	if err := json.Unmarshal([]byte(`{"cursor":"next","includeHidden":null,"limit":0}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Cursor == nil || decoded.Cursor.Value == nil || *decoded.Cursor.Value != "next" {
		t.Fatalf("decoded cursor = %#v", decoded.Cursor)
	}
	if decoded.IncludeHidden == nil || decoded.IncludeHidden.Value != nil {
		t.Fatalf("decoded includeHidden = %#v, want explicit null", decoded.IncludeHidden)
	}
	if decoded.Limit == nil || decoded.Limit.Value == nil || *decoded.Limit.Value != 0 {
		t.Fatalf("decoded limit = %#v", decoded.Limit)
	}
}

func TestGeneratedModelListResponseMarshalAndUnmarshal(t *testing.T) {
	speedTiers := []string{"fast"}
	modalities := []InputModality{InputModalityText, InputModalityImage}
	serviceTiers := []ModelServiceTier{{
		Description: "Default",
		ID:          "auto",
		Name:        "Auto",
	}}
	supportsPersonality := true
	response := ModelListResponse{
		Data: []Model{{
			AdditionalSpeedTiers:   &speedTiers,
			AvailabilityNux:        Value(ModelAvailabilityNux{Message: "try it"}),
			DefaultReasoningEffort: ReasoningEffort("medium"),
			Description:            "desc",
			DisplayName:            "GPT",
			Hidden:                 false,
			ID:                     "model-1",
			InputModalities:        &modalities,
			IsDefault:              true,
			Model:                  "gpt-test",
			ServiceTiers:           &serviceTiers,
			SupportedReasoningEfforts: []ReasoningEffortOption{{
				Description:     "Medium",
				ReasoningEffort: ReasoningEffort("medium"),
			}},
			SupportsPersonality: &supportsPersonality,
			Upgrade:             Null[string](),
			UpgradeInfo: Value(ModelUpgradeInfo{
				MigrationMarkdown: Null[string](),
				Model:             "gpt-next",
				ModelLink:         Value("https://example.test/model"),
				UpgradeCopy:       Value("Upgrade"),
			}),
		}},
		NextCursor: Value("cursor-2"),
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"data":[{"additionalSpeedTiers":["fast"],"availabilityNux":{"message":"try it"},"defaultReasoningEffort":"medium","description":"desc","displayName":"GPT","hidden":false,"id":"model-1","inputModalities":["text","image"],"isDefault":true,"model":"gpt-test","serviceTiers":[{"description":"Default","id":"auto","name":"Auto"}],"supportedReasoningEfforts":[{"description":"Medium","reasoningEffort":"medium"}],"supportsPersonality":true,"upgrade":null,"upgradeInfo":{"migrationMarkdown":null,"model":"gpt-next","modelLink":"https://example.test/model","upgradeCopy":"Upgrade"}}],"nextCursor":"cursor-2"}`
	if got := string(raw); got != want {
		t.Fatalf("ModelListResponse JSON = %s, want %s", got, want)
	}

	var decoded ModelListResponse
	err = json.Unmarshal([]byte(`{"data":[{"defaultReasoningEffort":"medium","description":"desc","displayName":"GPT","hidden":false,"id":"model-1","isDefault":true,"model":"gpt-test","supportedReasoningEfforts":[{"description":"Medium","reasoningEffort":"medium"}],"upgradeInfo":null}],"nextCursor":null}`), &decoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Data) != 1 || decoded.Data[0].Model != "gpt-test" || decoded.Data[0].DefaultReasoningEffort != ReasoningEffort("medium") {
		t.Fatalf("decoded model list response = %#v", decoded)
	}
	if decoded.Data[0].UpgradeInfo == nil || decoded.Data[0].UpgradeInfo.Value != nil {
		t.Fatalf("decoded upgradeInfo = %#v, want explicit null", decoded.Data[0].UpgradeInfo)
	}
	if decoded.NextCursor == nil || decoded.NextCursor.Value != nil {
		t.Fatalf("decoded nextCursor = %#v, want explicit null", decoded.NextCursor)
	}
}

func TestGeneratedModelProviderAndNotifications(t *testing.T) {
	raw, err := json.Marshal(ModelProviderCapabilitiesReadParams{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{}`; got != want {
		t.Fatalf("ModelProviderCapabilitiesReadParams JSON = %s, want %s", got, want)
	}

	var params ModelProviderCapabilitiesReadParams
	err = json.Unmarshal([]byte(`{"extra":true}`), &params)
	if err == nil {
		t.Fatal("expected unknown provider capabilities params field to fail")
	}
	if !strings.Contains(err.Error(), `decode ModelProviderCapabilitiesReadParams: unknown field "extra"`) {
		t.Fatalf("unexpected provider capabilities params error: %v", err)
	}

	responseRaw, err := json.Marshal(ModelProviderCapabilitiesReadResponse{
		ImageGeneration: true,
		NamespaceTools:  false,
		WebSearch:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(responseRaw), `{"imageGeneration":true,"namespaceTools":false,"webSearch":true}`; got != want {
		t.Fatalf("ModelProviderCapabilitiesReadResponse JSON = %s, want %s", got, want)
	}

	reroutedRaw, err := json.Marshal(ModelReroutedNotification{
		FromModel: "gpt-a",
		Reason:    ModelRerouteReasonHighRiskCyberActivity,
		ThreadID:  "thread-1",
		ToModel:   "gpt-b",
		TurnID:    "turn-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(reroutedRaw), `{"fromModel":"gpt-a","reason":"highRiskCyberActivity","threadId":"thread-1","toModel":"gpt-b","turnId":"turn-1"}`; got != want {
		t.Fatalf("ModelReroutedNotification JSON = %s, want %s", got, want)
	}

	verificationRaw, err := json.Marshal(ModelVerificationNotification{
		ThreadID:      "thread-1",
		TurnID:        "turn-1",
		Verifications: []ModelVerification{ModelVerificationTrustedAccessForCyber},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(verificationRaw), `{"threadId":"thread-1","turnId":"turn-1","verifications":["trustedAccessForCyber"]}`; got != want {
		t.Fatalf("ModelVerificationNotification JSON = %s, want %s", got, want)
	}
}

func TestGeneratedModelFamilyRejectsMalformedProtocol(t *testing.T) {
	var response ModelListResponse
	err := json.Unmarshal([]byte(`{"nextCursor":null}`), &response)
	if err == nil {
		t.Fatal("expected missing model list data to fail")
	}
	if !strings.Contains(err.Error(), "decode ModelListResponse.data: missing required field") {
		t.Fatalf("unexpected missing model list data error: %v", err)
	}

	_, err = json.Marshal(ModelListResponse{})
	if err == nil {
		t.Fatal("expected nil model list data marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode ModelListResponse.data: nil is not allowed") {
		t.Fatalf("unexpected nil model list data error: %v", err)
	}

	_, err = json.Marshal(Model{
		DefaultReasoningEffort: ReasoningEffort("medium"),
		Description:            "desc",
		DisplayName:            "GPT",
		Hidden:                 false,
		ID:                     "model-1",
		IsDefault:              true,
		Model:                  "gpt-test",
	})
	if err == nil {
		t.Fatal("expected nil supported reasoning efforts marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode Model.supportedReasoningEfforts: nil is not allowed") {
		t.Fatalf("unexpected nil supported reasoning efforts error: %v", err)
	}

	var model Model
	err = json.Unmarshal([]byte(`{"defaultReasoningEffort":"medium","description":"desc","displayName":"GPT","hidden":false,"id":"model-1","inputModalities":["audio"],"isDefault":true,"model":"gpt-test","supportedReasoningEfforts":[]}`), &model)
	if err == nil {
		t.Fatal("expected invalid input modality to fail")
	}
	if !strings.Contains(err.Error(), `invalid InputModality enum value "audio"`) {
		t.Fatalf("unexpected invalid input modality error: %v", err)
	}

	_, err = json.Marshal(InputModality("audio"))
	if err == nil {
		t.Fatal("expected invalid input modality marshal to fail")
	}
	if !strings.Contains(err.Error(), `invalid InputModality enum value "audio"`) {
		t.Fatalf("unexpected invalid input modality marshal error: %v", err)
	}

	var tier ModelServiceTier
	err = json.Unmarshal([]byte(`{"description":"Default","id":"auto","name":"Auto","extra":true}`), &tier)
	if err == nil {
		t.Fatal("expected unknown model service tier field to fail")
	}
	if !strings.Contains(err.Error(), `decode ModelServiceTier: unknown field "extra"`) {
		t.Fatalf("unexpected unknown model service tier error: %v", err)
	}
}

func TestGeneratedProcessSpawnParamsPreserveNullableFields(t *testing.T) {
	minimalRaw, err := json.Marshal(ProcessSpawnParams{
		Command:       []string{"bash", "-lc", "echo ok"},
		CWD:           "/workspace",
		ProcessHandle: "proc-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(minimalRaw), `{"command":["bash","-lc","echo ok"],"cwd":"/workspace","processHandle":"proc-1"}`; got != want {
		t.Fatalf("minimal ProcessSpawnParams JSON = %s, want %s", got, want)
	}

	streamStdin := true
	streamStdoutStderr := true
	tty := true
	env := map[string]*Nullable[string]{
		"PATH":   Value("/bin"),
		"REMOVE": Null[string](),
	}
	fullRaw, err := json.Marshal(ProcessSpawnParams{
		Command:            []string{"sh"},
		CWD:                "/tmp",
		Env:                Value(env),
		OutputBytesCap:     Null[uint64](),
		ProcessHandle:      "proc-2",
		Size:               Value(ProcessTerminalSize{Cols: 80, Rows: 24}),
		StreamStdin:        &streamStdin,
		StreamStdoutStderr: &streamStdoutStderr,
		TimeoutMS:          Value(int64(1000)),
		Tty:                &tty,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"command":["sh"],"cwd":"/tmp","env":{"PATH":"/bin","REMOVE":null},"outputBytesCap":null,"processHandle":"proc-2","size":{"cols":80,"rows":24},"streamStdin":true,"streamStdoutStderr":true,"timeoutMs":1000,"tty":true}`
	if got := string(fullRaw); got != want {
		t.Fatalf("full ProcessSpawnParams JSON = %s, want %s", got, want)
	}

	var decoded ProcessSpawnParams
	err = json.Unmarshal([]byte(`{"command":["sh"],"cwd":"/tmp","env":{"PATH":"/bin","REMOVE":null},"outputBytesCap":0,"processHandle":"proc-3","size":null}`), &decoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Env == nil || decoded.Env.Value == nil {
		t.Fatalf("decoded env = %#v", decoded.Env)
	}
	decodedEnv := *decoded.Env.Value
	if decodedEnv["PATH"] == nil || decodedEnv["PATH"].Value == nil || *decodedEnv["PATH"].Value != "/bin" {
		t.Fatalf("decoded env PATH = %#v", decodedEnv["PATH"])
	}
	if _, ok := decodedEnv["REMOVE"]; !ok || decodedEnv["REMOVE"] != nil {
		t.Fatalf("decoded env REMOVE = %#v, present=%t; want present null", decodedEnv["REMOVE"], ok)
	}
	if decoded.OutputBytesCap == nil || decoded.OutputBytesCap.Value == nil || *decoded.OutputBytesCap.Value != 0 {
		t.Fatalf("decoded outputBytesCap = %#v", decoded.OutputBytesCap)
	}
	if decoded.Size == nil || decoded.Size.Value != nil {
		t.Fatalf("decoded size = %#v, want explicit null", decoded.Size)
	}
}

func TestGeneratedProcessProtocolMarshalAndUnmarshal(t *testing.T) {
	killRaw, err := json.Marshal(ProcessKillParams{ProcessHandle: "proc-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(killRaw), `{"processHandle":"proc-1"}`; got != want {
		t.Fatalf("ProcessKillParams JSON = %s, want %s", got, want)
	}

	resizeRaw, err := json.Marshal(ProcessResizePtyParams{
		ProcessHandle: "proc-1",
		Size:          ProcessTerminalSize{Cols: 120, Rows: 40},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(resizeRaw), `{"processHandle":"proc-1","size":{"cols":120,"rows":40}}`; got != want {
		t.Fatalf("ProcessResizePtyParams JSON = %s, want %s", got, want)
	}

	outputRaw, err := json.Marshal(ProcessOutputDeltaNotification{
		CapReached:    false,
		DeltaBase64:   "SGVsbG8=",
		ProcessHandle: "proc-1",
		Stream:        ProcessOutputStreamStdout,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(outputRaw), `{"capReached":false,"deltaBase64":"SGVsbG8=","processHandle":"proc-1","stream":"stdout"}`; got != want {
		t.Fatalf("ProcessOutputDeltaNotification JSON = %s, want %s", got, want)
	}

	exitedRaw, err := json.Marshal(ProcessExitedNotification{
		ExitCode:         0,
		ProcessHandle:    "proc-1",
		Stderr:           "",
		StderrCapReached: false,
		Stdout:           "done",
		StdoutCapReached: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(exitedRaw), `{"exitCode":0,"processHandle":"proc-1","stderr":"","stderrCapReached":false,"stdout":"done","stdoutCapReached":false}`; got != want {
		t.Fatalf("ProcessExitedNotification JSON = %s, want %s", got, want)
	}

	for _, tc := range []struct {
		name   string
		decode func() error
	}{
		{
			name: "kill",
			decode: func() error {
				var response ProcessKillResponse
				return json.Unmarshal([]byte(`{}`), &response)
			},
		},
		{
			name: "resize",
			decode: func() error {
				var response ProcessResizePtyResponse
				return json.Unmarshal([]byte(`{}`), &response)
			},
		},
		{
			name: "spawn",
			decode: func() error {
				var response ProcessSpawnResponse
				return json.Unmarshal([]byte(`{}`), &response)
			},
		},
		{
			name: "writeStdin",
			decode: func() error {
				var response ProcessWriteStdinResponse
				return json.Unmarshal([]byte(`{}`), &response)
			},
		},
	} {
		if err := tc.decode(); err != nil {
			t.Fatalf("%s response unmarshal: %v", tc.name, err)
		}
	}
}

func TestGeneratedProcessFamilyRejectsMalformedProtocol(t *testing.T) {
	var spawn ProcessSpawnParams
	err := json.Unmarshal([]byte(`{"cwd":"/tmp","processHandle":"proc-1"}`), &spawn)
	if err == nil {
		t.Fatal("expected missing process spawn command to fail")
	}
	if !strings.Contains(err.Error(), "decode ProcessSpawnParams.command: missing required field") {
		t.Fatalf("unexpected missing command error: %v", err)
	}

	_, err = json.Marshal(ProcessSpawnParams{CWD: "/tmp", ProcessHandle: "proc-1"})
	if err == nil {
		t.Fatal("expected nil process spawn command marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode ProcessSpawnParams.command: nil is not allowed") {
		t.Fatalf("unexpected nil command error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"command":[],"cwd":"/tmp","processHandle":"proc-1","size":{"cols":65536,"rows":24}}`), &spawn)
	if err == nil {
		t.Fatal("expected uint16 overflow to fail")
	}
	if !strings.Contains(err.Error(), "decode ProcessTerminalSize.cols") {
		t.Fatalf("unexpected uint16 overflow error: %v", err)
	}

	var resize ProcessResizePtyParams
	err = json.Unmarshal([]byte(`{"processHandle":"proc-1","size":null}`), &resize)
	if err == nil {
		t.Fatal("expected null required resize size to fail")
	}
	if !strings.Contains(err.Error(), "decode ProcessResizePtyParams.size: null is not allowed") {
		t.Fatalf("unexpected null resize size error: %v", err)
	}

	var output ProcessOutputDeltaNotification
	err = json.Unmarshal([]byte(`{"capReached":false,"deltaBase64":"x","processHandle":"proc-1","stream":"stdin"}`), &output)
	if err == nil {
		t.Fatal("expected invalid process output stream to fail")
	}
	if !strings.Contains(err.Error(), `invalid ProcessOutputStream enum value "stdin"`) {
		t.Fatalf("unexpected invalid output stream error: %v", err)
	}

	_, err = json.Marshal(ProcessOutputStream("stdin"))
	if err == nil {
		t.Fatal("expected invalid process output stream marshal to fail")
	}
	if !strings.Contains(err.Error(), `invalid ProcessOutputStream enum value "stdin"`) {
		t.Fatalf("unexpected invalid output stream marshal error: %v", err)
	}

	var killResponse ProcessKillResponse
	err = json.Unmarshal([]byte(`{"extra":true}`), &killResponse)
	if err == nil {
		t.Fatal("expected unknown process response field to fail")
	}
	if !strings.Contains(err.Error(), `decode ProcessKillResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown process response error: %v", err)
	}
}

func TestGeneratedCommandExecParamsPreserveNullableFields(t *testing.T) {
	minimalRaw, err := json.Marshal(CommandExecParams{
		Command: []string{"bash", "-lc", "echo ok"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(minimalRaw), `{"command":["bash","-lc","echo ok"]}`; got != want {
		t.Fatalf("minimal CommandExecParams JSON = %s, want %s", got, want)
	}

	env := map[string]*Nullable[string]{
		"DROP": Null[string](),
		"KEEP": Value("1"),
	}
	writableRoots := []string{"/workspace/out"}
	sandboxPolicy := NewSandboxPolicyWorkspaceWrite(SandboxPolicyWorkspaceWrite{
		ExcludeSlashTmp:     boolPtr(true),
		ExcludeTmpdirEnvVar: boolPtr(false),
		NetworkAccess:       boolPtr(true),
		WritableRoots:       &writableRoots,
	})
	fullRaw, err := json.Marshal(CommandExecParams{
		Command:            []string{"bash", "-lc", "echo ok"},
		CWD:                Value("/workspace"),
		DisableOutputCap:   boolPtr(true),
		DisableTimeout:     boolPtr(false),
		Env:                Value(env),
		OutputBytesCap:     Value(uint64(4096)),
		PermissionProfile:  Value("profile-1"),
		ProcessID:          Value("proc-1"),
		SandboxPolicy:      Value(sandboxPolicy),
		Size:               Value(CommandExecTerminalSize{Cols: 100, Rows: 30}),
		StreamStdin:        boolPtr(true),
		StreamStdoutStderr: boolPtr(false),
		TimeoutMS:          Value(int64(1000)),
		Tty:                boolPtr(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"command":["bash","-lc","echo ok"],"cwd":"/workspace","disableOutputCap":true,"disableTimeout":false,"env":{"DROP":null,"KEEP":"1"},"outputBytesCap":4096,"permissionProfile":"profile-1","processId":"proc-1","sandboxPolicy":{"excludeSlashTmp":true,"excludeTmpdirEnvVar":false,"networkAccess":true,"type":"workspaceWrite","writableRoots":["/workspace/out"]},"size":{"cols":100,"rows":30},"streamStdin":true,"streamStdoutStderr":false,"timeoutMs":1000,"tty":true}`
	if got := string(fullRaw); got != want {
		t.Fatalf("full CommandExecParams JSON = %s, want %s", got, want)
	}

	var decoded CommandExecParams
	err = json.Unmarshal([]byte(`{"command":["sh"],"cwd":null,"env":{"DROP":null,"KEEP":"1"},"outputBytesCap":null,"permissionProfile":null,"processId":"proc-2","sandboxPolicy":{"type":"dangerFullAccess"},"size":null,"timeoutMs":0}`), &decoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.CWD == nil || decoded.CWD.Value != nil {
		t.Fatalf("decoded cwd = %#v, want explicit null", decoded.CWD)
	}
	if decoded.Env == nil || decoded.Env.Value == nil {
		t.Fatalf("decoded env = %#v", decoded.Env)
	}
	decodedEnv := *decoded.Env.Value
	if decodedEnv["KEEP"] == nil || decodedEnv["KEEP"].Value == nil || *decodedEnv["KEEP"].Value != "1" {
		t.Fatalf("decoded env KEEP = %#v", decodedEnv["KEEP"])
	}
	if _, ok := decodedEnv["DROP"]; !ok || decodedEnv["DROP"] != nil {
		t.Fatalf("decoded env DROP = %#v, present=%t; want present null", decodedEnv["DROP"], ok)
	}
	if decoded.OutputBytesCap == nil || decoded.OutputBytesCap.Value != nil {
		t.Fatalf("decoded outputBytesCap = %#v, want explicit null", decoded.OutputBytesCap)
	}
	if decoded.PermissionProfile == nil || decoded.PermissionProfile.Value != nil {
		t.Fatalf("decoded permissionProfile = %#v, want explicit null", decoded.PermissionProfile)
	}
	if decoded.SandboxPolicy == nil || decoded.SandboxPolicy.Value == nil || decoded.SandboxPolicy.Value.Kind() != SandboxPolicyKindDangerFullAccess {
		t.Fatalf("decoded sandboxPolicy = %#v", decoded.SandboxPolicy)
	}
	if decoded.Size == nil || decoded.Size.Value != nil {
		t.Fatalf("decoded size = %#v, want explicit null", decoded.Size)
	}
	if decoded.TimeoutMS == nil || decoded.TimeoutMS.Value == nil || *decoded.TimeoutMS.Value != 0 {
		t.Fatalf("decoded timeoutMs = %#v", decoded.TimeoutMS)
	}
}

func TestGeneratedCommandExecSandboxPolicyUnionMarshalAndAccessors(t *testing.T) {
	readOnly := NewSandboxPolicyReadOnly(SandboxPolicyReadOnly{
		NetworkAccess: boolPtr(true),
	})
	if readOnly.Kind() != SandboxPolicyKindReadOnly || !readOnly.IsValid() {
		t.Fatalf("SandboxPolicy readOnly kind/valid = %s/%t", readOnly.Kind(), readOnly.IsValid())
	}
	if _, ok := readOnly.AsReadOnly(); !ok {
		t.Fatal("SandboxPolicy AsReadOnly returned false")
	}
	readOnlyRaw, err := json.Marshal(readOnly)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(readOnlyRaw), `{"networkAccess":true,"type":"readOnly"}`; got != want {
		t.Fatalf("SandboxPolicy readOnly JSON = %s, want %s", got, want)
	}

	networkAccess := NetworkAccessEnabled
	externalRaw, err := json.Marshal(NewSandboxPolicyExternalSandbox(SandboxPolicyExternalSandbox{
		NetworkAccess: &networkAccess,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(externalRaw), `{"networkAccess":"enabled","type":"externalSandbox"}`; got != want {
		t.Fatalf("SandboxPolicy externalSandbox JSON = %s, want %s", got, want)
	}

	writableRoots := []string{"/workspace"}
	workspaceRaw, err := json.Marshal(NewSandboxPolicyWorkspaceWrite(SandboxPolicyWorkspaceWrite{
		ExcludeSlashTmp:     boolPtr(false),
		ExcludeTmpdirEnvVar: boolPtr(true),
		NetworkAccess:       boolPtr(false),
		WritableRoots:       &writableRoots,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(workspaceRaw), `{"excludeSlashTmp":false,"excludeTmpdirEnvVar":true,"networkAccess":false,"type":"workspaceWrite","writableRoots":["/workspace"]}`; got != want {
		t.Fatalf("SandboxPolicy workspaceWrite JSON = %s, want %s", got, want)
	}

	dangerRaw, err := json.Marshal(NewSandboxPolicyDangerFullAccess())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(dangerRaw), `{"type":"dangerFullAccess"}`; got != want {
		t.Fatalf("SandboxPolicy dangerFullAccess JSON = %s, want %s", got, want)
	}

	var decoded SandboxPolicy
	if err := json.Unmarshal([]byte(workspaceRaw), &decoded); err != nil {
		t.Fatal(err)
	}
	workspace, ok := decoded.AsWorkspaceWrite()
	if !ok || workspace.WritableRoots == nil || len(*workspace.WritableRoots) != 1 || (*workspace.WritableRoots)[0] != "/workspace" {
		t.Fatalf("decoded SandboxPolicy workspaceWrite = %#v ok=%t", workspace, ok)
	}
}

func TestGeneratedCommandExecProtocolMarshalAndUnmarshal(t *testing.T) {
	responseRaw, err := json.Marshal(CommandExecResponse{
		ExitCode: 0,
		Stderr:   "",
		Stdout:   "ok\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(responseRaw), `{"exitCode":0,"stderr":"","stdout":"ok\n"}`; got != want {
		t.Fatalf("CommandExecResponse JSON = %s, want %s", got, want)
	}

	var response CommandExecResponse
	if err := json.Unmarshal(responseRaw, &response); err != nil {
		t.Fatal(err)
	}
	if response.ExitCode != 0 || response.Stdout != "ok\n" || response.Stderr != "" {
		t.Fatalf("decoded CommandExecResponse = %#v", response)
	}

	resizeRaw, err := json.Marshal(CommandExecResizeParams{
		ProcessID: "proc-1",
		Size:      CommandExecTerminalSize{Cols: 120, Rows: 40},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(resizeRaw), `{"processId":"proc-1","size":{"cols":120,"rows":40}}`; got != want {
		t.Fatalf("CommandExecResizeParams JSON = %s, want %s", got, want)
	}

	writeRaw, err := json.Marshal(CommandExecWriteParams{
		CloseStdin:  boolPtr(false),
		DeltaBase64: Null[string](),
		ProcessID:   "proc-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(writeRaw), `{"closeStdin":false,"deltaBase64":null,"processId":"proc-1"}`; got != want {
		t.Fatalf("CommandExecWriteParams JSON = %s, want %s", got, want)
	}

	var writeDecoded CommandExecWriteParams
	if err := json.Unmarshal([]byte(`{"closeStdin":true,"deltaBase64":"SGVsbG8=","processId":"proc-1"}`), &writeDecoded); err != nil {
		t.Fatal(err)
	}
	if writeDecoded.CloseStdin == nil || *writeDecoded.CloseStdin != true || writeDecoded.DeltaBase64 == nil || writeDecoded.DeltaBase64.Value == nil || *writeDecoded.DeltaBase64.Value != "SGVsbG8=" {
		t.Fatalf("decoded CommandExecWriteParams = %#v", writeDecoded)
	}

	terminateRaw, err := json.Marshal(CommandExecTerminateParams{ProcessID: "proc-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(terminateRaw), `{"processId":"proc-1"}`; got != want {
		t.Fatalf("CommandExecTerminateParams JSON = %s, want %s", got, want)
	}

	outputRaw, err := json.Marshal(CommandExecOutputDeltaNotification{
		CapReached:  true,
		DeltaBase64: "SGVsbG8=",
		ProcessID:   "proc-1",
		Stream:      CommandExecOutputStreamStdout,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(outputRaw), `{"capReached":true,"deltaBase64":"SGVsbG8=","processId":"proc-1","stream":"stdout"}`; got != want {
		t.Fatalf("CommandExecOutputDeltaNotification JSON = %s, want %s", got, want)
	}

	commandExecutionOutputRaw, err := json.Marshal(CommandExecutionOutputDeltaNotification{
		Delta:    "chunk",
		ItemID:   "item-1",
		ThreadID: "thread-1",
		TurnID:   "turn-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(commandExecutionOutputRaw), `{"delta":"chunk","itemId":"item-1","threadId":"thread-1","turnId":"turn-1"}`; got != want {
		t.Fatalf("CommandExecutionOutputDeltaNotification JSON = %s, want %s", got, want)
	}

	for _, tc := range []struct {
		name    string
		marshal func() ([]byte, error)
		decode  func() error
	}{
		{
			name:    "resize",
			marshal: func() ([]byte, error) { return json.Marshal(CommandExecResizeResponse{}) },
			decode: func() error {
				var response CommandExecResizeResponse
				return json.Unmarshal([]byte(`{}`), &response)
			},
		},
		{
			name:    "terminate",
			marshal: func() ([]byte, error) { return json.Marshal(CommandExecTerminateResponse{}) },
			decode: func() error {
				var response CommandExecTerminateResponse
				return json.Unmarshal([]byte(`{}`), &response)
			},
		},
		{
			name:    "write",
			marshal: func() ([]byte, error) { return json.Marshal(CommandExecWriteResponse{}) },
			decode: func() error {
				var response CommandExecWriteResponse
				return json.Unmarshal([]byte(`{}`), &response)
			},
		},
	} {
		raw, err := tc.marshal()
		if err != nil {
			t.Fatalf("%s empty response marshal: %v", tc.name, err)
		}
		if got, want := string(raw), `{}`; got != want {
			t.Fatalf("%s empty response JSON = %s, want %s", tc.name, got, want)
		}
		if err := tc.decode(); err != nil {
			t.Fatalf("%s empty response unmarshal: %v", tc.name, err)
		}
	}
}

func TestGeneratedCommandExecRejectsMalformedProtocol(t *testing.T) {
	var params CommandExecParams
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "missing command",
			raw:  `{}`,
			want: "decode CommandExecParams.command: missing required field",
		},
		{
			name: "null command",
			raw:  `{"command":null}`,
			want: "decode CommandExecParams.command: null is not allowed",
		},
		{
			name: "empty command",
			raw:  `{"command":[]}`,
			want: "decode CommandExecParams.command: must contain at least 1 item",
		},
		{
			name: "unknown params field",
			raw:  `{"command":["sh"],"extra":true}`,
			want: `decode CommandExecParams: unknown field "extra"`,
		},
		{
			name: "non string env value",
			raw:  `{"command":["sh"],"env":{"A":1}}`,
			want: "decode CommandExecParams.env:",
		},
		{
			name: "negative output cap",
			raw:  `{"command":["sh"],"outputBytesCap":-1}`,
			want: "decode CommandExecParams.outputBytesCap:",
		},
		{
			name: "invalid permission profile variant",
			raw:  `{"command":["sh"],"permissionProfile":{"type":"weird"}}`,
			want: "decode CommandExecParams.permissionProfile",
		},
		{
			name: "invalid sandbox policy variant",
			raw:  `{"command":["sh"],"sandboxPolicy":{"type":"weird"}}`,
			want: `decode SandboxPolicy.type: unknown variant "weird"`,
		},
		{
			name: "invalid external sandbox network access",
			raw:  `{"command":["sh"],"sandboxPolicy":{"networkAccess":"bogus","type":"externalSandbox"}}`,
			want: `invalid NetworkAccess enum value "bogus"`,
		},
		{
			name: "terminal size overflow",
			raw:  `{"command":["sh"],"size":{"cols":65536,"rows":24}}`,
			want: "decode CommandExecTerminalSize.cols",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := json.Unmarshal([]byte(tc.raw), &params)
			if err == nil {
				t.Fatal("expected malformed command exec params to fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected malformed command exec params error: %v", err)
			}
		})
	}

	_, err := json.Marshal(CommandExecParams{})
	if err == nil {
		t.Fatal("expected nil command exec command marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode CommandExecParams.command: nil is not allowed") {
		t.Fatalf("unexpected nil command exec command error: %v", err)
	}

	_, err = json.Marshal(CommandExecParams{Command: []string{}})
	if err == nil {
		t.Fatal("expected empty command exec command marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode CommandExecParams.command: must contain at least 1 item") {
		t.Fatalf("unexpected empty command exec command error: %v", err)
	}

	_, err = json.Marshal(SandboxPolicy{})
	if err == nil {
		t.Fatal("expected zero-value SandboxPolicy marshal to fail")
	}
	if !strings.Contains(err.Error(), "invalid SandboxPolicy union value: no variant is set") {
		t.Fatalf("unexpected zero-value SandboxPolicy error: %v", err)
	}

	var response CommandExecResponse
	err = json.Unmarshal([]byte(`{"exitCode":0,"stdout":""}`), &response)
	if err == nil {
		t.Fatal("expected missing CommandExecResponse stderr to fail")
	}
	if !strings.Contains(err.Error(), "decode CommandExecResponse.stderr: missing required field") {
		t.Fatalf("unexpected missing command exec stderr error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"exitCode":0,"stderr":"","stdout":"","extra":true}`), &response)
	if err == nil {
		t.Fatal("expected unknown CommandExecResponse field to fail")
	}
	if !strings.Contains(err.Error(), `decode CommandExecResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown command exec response error: %v", err)
	}

	var resize CommandExecResizeParams
	err = json.Unmarshal([]byte(`{"processId":"proc-1","size":null}`), &resize)
	if err == nil {
		t.Fatal("expected null required command exec resize size to fail")
	}
	if !strings.Contains(err.Error(), "decode CommandExecResizeParams.size: null is not allowed") {
		t.Fatalf("unexpected null command exec resize size error: %v", err)
	}

	var write CommandExecWriteParams
	err = json.Unmarshal([]byte(`{"deltaBase64":"x"}`), &write)
	if err == nil {
		t.Fatal("expected missing command exec write processId to fail")
	}
	if !strings.Contains(err.Error(), "decode CommandExecWriteParams.processId: missing required field") {
		t.Fatalf("unexpected missing command exec write processId error: %v", err)
	}

	var terminate CommandExecTerminateParams
	err = json.Unmarshal([]byte(`{"processId":null}`), &terminate)
	if err == nil {
		t.Fatal("expected null command exec terminate processId to fail")
	}
	if !strings.Contains(err.Error(), "decode CommandExecTerminateParams.processId: null is not allowed") {
		t.Fatalf("unexpected null command exec terminate processId error: %v", err)
	}

	var output CommandExecOutputDeltaNotification
	err = json.Unmarshal([]byte(`{"capReached":false,"deltaBase64":"x","processId":"proc-1","stream":"stdin"}`), &output)
	if err == nil {
		t.Fatal("expected invalid command exec output stream to fail")
	}
	if !strings.Contains(err.Error(), `invalid CommandExecOutputStream enum value "stdin"`) {
		t.Fatalf("unexpected invalid command exec output stream error: %v", err)
	}

	_, err = json.Marshal(CommandExecOutputStream("stdin"))
	if err == nil {
		t.Fatal("expected invalid command exec output stream marshal to fail")
	}
	if !strings.Contains(err.Error(), `invalid CommandExecOutputStream enum value "stdin"`) {
		t.Fatalf("unexpected invalid command exec output stream marshal error: %v", err)
	}

	var commandExecutionOutput CommandExecutionOutputDeltaNotification
	err = json.Unmarshal([]byte(`{"delta":"chunk","itemId":"item-1","threadId":"thread-1"}`), &commandExecutionOutput)
	if err == nil {
		t.Fatal("expected missing command execution output turnId to fail")
	}
	if !strings.Contains(err.Error(), "decode CommandExecutionOutputDeltaNotification.turnId: missing required field") {
		t.Fatalf("unexpected missing command execution output turnId error: %v", err)
	}

	var resizeResponse CommandExecResizeResponse
	err = json.Unmarshal([]byte(`{"extra":true}`), &resizeResponse)
	if err == nil {
		t.Fatal("expected unknown command exec resize response field to fail")
	}
	if !strings.Contains(err.Error(), `decode CommandExecResizeResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown command exec resize response error: %v", err)
	}
}

func TestGeneratedConfigWriteProtocolMarshalAndUnmarshal(t *testing.T) {
	emptyReadRaw, err := json.Marshal(ConfigReadParams{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(emptyReadRaw), `{}`; got != want {
		t.Fatalf("empty ConfigReadParams JSON = %s, want %s", got, want)
	}

	readParamsRaw, err := json.Marshal(ConfigReadParams{
		CWD:           Null[string](),
		IncludeLayers: boolPtr(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(readParamsRaw), `{"cwd":null,"includeLayers":true}`; got != want {
		t.Fatalf("ConfigReadParams JSON = %s, want %s", got, want)
	}

	var readParamsDecoded ConfigReadParams
	if err := json.Unmarshal([]byte(`{"cwd":"/workspace","includeLayers":false}`), &readParamsDecoded); err != nil {
		t.Fatal(err)
	}
	if readParamsDecoded.CWD == nil || readParamsDecoded.CWD.Value == nil || *readParamsDecoded.CWD.Value != "/workspace" {
		t.Fatalf("decoded ConfigReadParams.cwd = %#v", readParamsDecoded.CWD)
	}
	if readParamsDecoded.IncludeLayers == nil || *readParamsDecoded.IncludeLayers {
		t.Fatalf("decoded ConfigReadParams.includeLayers = %#v", readParamsDecoded.IncludeLayers)
	}

	configValue := JSONObject(map[string]JSONValue{
		"enabled": JSONBool(true),
		"mode":    JSONString("fast"),
	})
	batchRaw, err := json.Marshal(ConfigBatchWriteParams{
		Edits: []ConfigEdit{{
			KeyPath:       "features.example",
			MergeStrategy: MergeStrategyUpsert,
			Value:         configValue,
		}},
		ExpectedVersion:  Value("v1"),
		FilePath:         Null[string](),
		ReloadUserConfig: boolPtr(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	wantBatch := `{"edits":[{"keyPath":"features.example","mergeStrategy":"upsert","value":{"enabled":true,"mode":"fast"}}],"expectedVersion":"v1","filePath":null,"reloadUserConfig":true}`
	if got := string(batchRaw); got != wantBatch {
		t.Fatalf("ConfigBatchWriteParams JSON = %s, want %s", got, wantBatch)
	}

	var batchDecoded ConfigBatchWriteParams
	if err := json.Unmarshal([]byte(`{"edits":[{"keyPath":"features.example","mergeStrategy":"replace","value":null}],"expectedVersion":null,"filePath":"/home/user/.codex/config.toml"}`), &batchDecoded); err != nil {
		t.Fatal(err)
	}
	if len(batchDecoded.Edits) != 1 || batchDecoded.Edits[0].MergeStrategy != MergeStrategyReplace {
		t.Fatalf("decoded ConfigBatchWriteParams = %#v", batchDecoded)
	}
	if batchDecoded.Edits[0].Value.Kind() != JSONKindNull {
		t.Fatalf("decoded config edit value kind = %s, want %s", batchDecoded.Edits[0].Value.Kind(), JSONKindNull)
	}
	if batchDecoded.ExpectedVersion == nil || batchDecoded.ExpectedVersion.Value != nil {
		t.Fatalf("decoded expectedVersion = %#v, want explicit null", batchDecoded.ExpectedVersion)
	}
	if batchDecoded.FilePath == nil || batchDecoded.FilePath.Value == nil || *batchDecoded.FilePath.Value != "/home/user/.codex/config.toml" {
		t.Fatalf("decoded filePath = %#v", batchDecoded.FilePath)
	}

	valueRaw, err := json.Marshal(ConfigValueWriteParams{
		ExpectedVersion: Null[string](),
		FilePath:        Value("/home/user/.codex/config.toml"),
		KeyPath:         "model",
		MergeStrategy:   MergeStrategyReplace,
		Value:           JSONString("gpt-5"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(valueRaw), `{"expectedVersion":null,"filePath":"/home/user/.codex/config.toml","keyPath":"model","mergeStrategy":"replace","value":"gpt-5"}`; got != want {
		t.Fatalf("ConfigValueWriteParams JSON = %s, want %s", got, want)
	}

	var valueDecoded ConfigValueWriteParams
	if err := json.Unmarshal([]byte(`{"expectedVersion":"v2","filePath":null,"keyPath":"model","mergeStrategy":"upsert","value":{"model":"gpt-5"}}`), &valueDecoded); err != nil {
		t.Fatal(err)
	}
	if valueDecoded.ExpectedVersion == nil || valueDecoded.ExpectedVersion.Value == nil || *valueDecoded.ExpectedVersion.Value != "v2" {
		t.Fatalf("decoded ConfigValueWriteParams.expectedVersion = %#v", valueDecoded.ExpectedVersion)
	}
	if valueDecoded.FilePath == nil || valueDecoded.FilePath.Value != nil {
		t.Fatalf("decoded ConfigValueWriteParams.filePath = %#v, want explicit null", valueDecoded.FilePath)
	}
	if object, ok := valueDecoded.Value.AsObject(); !ok || object["model"].Kind() != JSONKindString {
		t.Fatalf("decoded ConfigValueWriteParams.value = %#v ok=%t", object, ok)
	}

	responseRaw, err := json.Marshal(ConfigWriteResponse{
		FilePath: "/home/user/.codex/config.toml",
		OverriddenMetadata: Value(OverriddenMetadata{
			EffectiveValue: JSONString("managed"),
			Message:        "overridden by managed layer",
			OverridingLayer: ConfigLayerMetadata{
				Name:    NewConfigLayerSourceMdm(ConfigLayerSourceMdm{Domain: "com.example", Key: "Codex"}),
				Version: "mdm-v1",
			},
		}),
		Status:  WriteStatusOkOverridden,
		Version: "v2",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantResponse := `{"filePath":"/home/user/.codex/config.toml","overriddenMetadata":{"effectiveValue":"managed","message":"overridden by managed layer","overridingLayer":{"name":{"domain":"com.example","key":"Codex","type":"mdm"},"version":"mdm-v1"}},"status":"okOverridden","version":"v2"}`
	if got := string(responseRaw); got != wantResponse {
		t.Fatalf("ConfigWriteResponse JSON = %s, want %s", got, wantResponse)
	}

	var responseDecoded ConfigWriteResponse
	if err := json.Unmarshal(responseRaw, &responseDecoded); err != nil {
		t.Fatal(err)
	}
	if responseDecoded.OverriddenMetadata == nil || responseDecoded.OverriddenMetadata.Value == nil {
		t.Fatalf("decoded overriddenMetadata = %#v", responseDecoded.OverriddenMetadata)
	}
	layer, ok := responseDecoded.OverriddenMetadata.Value.OverridingLayer.Name.AsMdm()
	if !ok || layer.Domain != "com.example" {
		t.Fatalf("decoded overriding layer = %#v ok=%t", layer, ok)
	}

	if err := json.Unmarshal([]byte(`{"filePath":"/home/user/.codex/config.toml","overriddenMetadata":null,"status":"ok","version":"v3"}`), &responseDecoded); err != nil {
		t.Fatal(err)
	}
	if responseDecoded.Status != WriteStatusOk {
		t.Fatalf("decoded status = %s, want %s", responseDecoded.Status, WriteStatusOk)
	}
	if responseDecoded.OverriddenMetadata == nil || responseDecoded.OverriddenMetadata.Value != nil {
		t.Fatalf("decoded overriddenMetadata = %#v, want explicit null", responseDecoded.OverriddenMetadata)
	}

	warningRaw, err := json.Marshal(ConfigWarningNotification{
		Details: Null[string](),
		Path:    Value("/home/user/.codex/config.toml"),
		Range: Value(TextRange{
			End:   TextPosition{Column: 5, Line: 3},
			Start: TextPosition{Column: 1, Line: 3},
		}),
		Summary: "invalid config",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantWarning := `{"details":null,"path":"/home/user/.codex/config.toml","range":{"end":{"column":5,"line":3},"start":{"column":1,"line":3}},"summary":"invalid config"}`
	if got := string(warningRaw); got != wantWarning {
		t.Fatalf("ConfigWarningNotification JSON = %s, want %s", got, wantWarning)
	}

	var warningDecoded ConfigWarningNotification
	if err := json.Unmarshal([]byte(`{"range":null,"summary":"ok"}`), &warningDecoded); err != nil {
		t.Fatal(err)
	}
	if warningDecoded.Range == nil || warningDecoded.Range.Value != nil {
		t.Fatalf("decoded warning range = %#v, want explicit null", warningDecoded.Range)
	}
}

func TestGeneratedConfigLayerSourceUnionMarshalAndAccessors(t *testing.T) {
	cases := []struct {
		name  string
		value ConfigLayerSource
		kind  ConfigLayerSourceKind
		want  string
	}{
		{
			name:  "mdm",
			value: NewConfigLayerSourceMdm(ConfigLayerSourceMdm{Domain: "com.example", Key: "Codex"}),
			kind:  ConfigLayerSourceKindMdm,
			want:  `{"domain":"com.example","key":"Codex","type":"mdm"}`,
		},
		{
			name:  "system",
			value: NewConfigLayerSourceSystem(ConfigLayerSourceSystem{File: "/etc/codex/config.toml"}),
			kind:  ConfigLayerSourceKindSystem,
			want:  `{"file":"/etc/codex/config.toml","type":"system"}`,
		},
		{
			name:  "user",
			value: NewConfigLayerSourceUser(ConfigLayerSourceUser{File: "/home/user/.codex/config.toml"}),
			kind:  ConfigLayerSourceKindUser,
			want:  `{"file":"/home/user/.codex/config.toml","type":"user"}`,
		},
		{
			name:  "project",
			value: NewConfigLayerSourceProject(ConfigLayerSourceProject{DotCodexFolder: "/workspace/.codex"}),
			kind:  ConfigLayerSourceKindProject,
			want:  `{"dotCodexFolder":"/workspace/.codex","type":"project"}`,
		},
		{
			name:  "session flags",
			value: NewConfigLayerSourceSessionFlags(),
			kind:  ConfigLayerSourceKindSessionFlags,
			want:  `{"type":"sessionFlags"}`,
		},
		{
			name:  "legacy file",
			value: NewConfigLayerSourceLegacyManagedConfigTomlFromFile(ConfigLayerSourceLegacyManagedConfigTomlFromFile{File: "/etc/codex/managed_config.toml"}),
			kind:  ConfigLayerSourceKindLegacyManagedConfigTomlFromFile,
			want:  `{"file":"/etc/codex/managed_config.toml","type":"legacyManagedConfigTomlFromFile"}`,
		},
		{
			name:  "legacy mdm",
			value: NewConfigLayerSourceLegacyManagedConfigTomlFromMdm(),
			kind:  ConfigLayerSourceKindLegacyManagedConfigTomlFromMdm,
			want:  `{"type":"legacyManagedConfigTomlFromMdm"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.value.Kind() != tc.kind || !tc.value.IsValid() {
				t.Fatalf("ConfigLayerSource kind/valid = %s/%t, want %s/true", tc.value.Kind(), tc.value.IsValid(), tc.kind)
			}
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(raw); got != tc.want {
				t.Fatalf("ConfigLayerSource JSON = %s, want %s", got, tc.want)
			}
			var decoded ConfigLayerSource
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Kind() != tc.kind {
				t.Fatalf("decoded ConfigLayerSource kind = %s, want %s", decoded.Kind(), tc.kind)
			}
		})
	}

	project := NewConfigLayerSourceProject(ConfigLayerSourceProject{DotCodexFolder: "/workspace/.codex"})
	if payload, ok := project.AsProject(); !ok || payload.DotCodexFolder != "/workspace/.codex" {
		t.Fatalf("ConfigLayerSource AsProject = %#v ok=%t", payload, ok)
	}
	if _, ok := project.AsMdm(); ok {
		t.Fatal("ConfigLayerSource AsMdm returned true for project variant")
	}
}

func TestGeneratedConfigProtocolRejectsMalformedProtocol(t *testing.T) {
	var batch ConfigBatchWriteParams
	err := json.Unmarshal([]byte(`{}`), &batch)
	if err == nil {
		t.Fatal("expected missing config batch edits to fail")
	}
	if !strings.Contains(err.Error(), "decode ConfigBatchWriteParams.edits: missing required field") {
		t.Fatalf("unexpected missing config batch edits error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"edits":null}`), &batch)
	if err == nil {
		t.Fatal("expected null config batch edits to fail")
	}
	if !strings.Contains(err.Error(), "decode ConfigBatchWriteParams.edits: null is not allowed") {
		t.Fatalf("unexpected null config batch edits error: %v", err)
	}

	_, err = json.Marshal(ConfigBatchWriteParams{})
	if err == nil {
		t.Fatal("expected nil config batch edits marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode ConfigBatchWriteParams.edits: nil is not allowed") {
		t.Fatalf("unexpected nil config batch edits error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"edits":[{"keyPath":"model","mergeStrategy":"replace"}]}`), &batch)
	if err == nil {
		t.Fatal("expected missing config edit value to fail")
	}
	if !strings.Contains(err.Error(), "decode ConfigEdit.value: missing required field") {
		t.Fatalf("unexpected missing config edit value error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"edits":[{"keyPath":"model","mergeStrategy":"merge","value":null}]}`), &batch)
	if err == nil {
		t.Fatal("expected invalid config edit mergeStrategy to fail")
	}
	if !strings.Contains(err.Error(), `invalid MergeStrategy enum value "merge"`) {
		t.Fatalf("unexpected invalid mergeStrategy error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"edits":[],"extra":true}`), &batch)
	if err == nil {
		t.Fatal("expected unknown config batch field to fail")
	}
	if !strings.Contains(err.Error(), `decode ConfigBatchWriteParams: unknown field "extra"`) {
		t.Fatalf("unexpected unknown config batch field error: %v", err)
	}

	_, err = json.Marshal(ConfigValueWriteParams{
		KeyPath:       "model",
		MergeStrategy: MergeStrategyReplace,
		Value:         JSONValue{},
	})
	if err == nil {
		t.Fatal("expected invalid JSONValue config value marshal to fail")
	}
	if !strings.Contains(err.Error(), "invalid JSONValue at /") {
		t.Fatalf("unexpected invalid JSONValue config value error: %v", err)
	}

	var valueWrite ConfigValueWriteParams
	err = json.Unmarshal([]byte(`{"mergeStrategy":"replace","value":true}`), &valueWrite)
	if err == nil {
		t.Fatal("expected missing config value keyPath to fail")
	}
	if !strings.Contains(err.Error(), "decode ConfigValueWriteParams.keyPath: missing required field") {
		t.Fatalf("unexpected missing config value keyPath error: %v", err)
	}

	_, err = json.Marshal(ConfigValueWriteParams{
		KeyPath:       "model",
		MergeStrategy: MergeStrategy("merge"),
		Value:         JSONString("gpt-5"),
	})
	if err == nil {
		t.Fatal("expected invalid config value mergeStrategy marshal to fail")
	}
	if !strings.Contains(err.Error(), `invalid MergeStrategy enum value "merge"`) {
		t.Fatalf("unexpected invalid config value mergeStrategy marshal error: %v", err)
	}

	var response ConfigWriteResponse
	err = json.Unmarshal([]byte(`{"filePath":"/config.toml","version":"v1"}`), &response)
	if err == nil {
		t.Fatal("expected missing config write status to fail")
	}
	if !strings.Contains(err.Error(), "decode ConfigWriteResponse.status: missing required field") {
		t.Fatalf("unexpected missing config write status error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"filePath":"/config.toml","status":"bad","version":"v1"}`), &response)
	if err == nil {
		t.Fatal("expected invalid config write status to fail")
	}
	if !strings.Contains(err.Error(), `invalid WriteStatus enum value "bad"`) {
		t.Fatalf("unexpected invalid config write status error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"filePath":"/config.toml","overriddenMetadata":{"message":"m","overridingLayer":{"name":{"type":"sessionFlags"},"version":"v1"}},"status":"okOverridden","version":"v1"}`), &response)
	if err == nil {
		t.Fatal("expected missing overridden effectiveValue to fail")
	}
	if !strings.Contains(err.Error(), "decode OverriddenMetadata.effectiveValue: missing required field") {
		t.Fatalf("unexpected missing overridden effectiveValue error: %v", err)
	}

	_, err = json.Marshal(ConfigWriteResponse{
		FilePath: "/config.toml",
		Status:   WriteStatus("bad"),
		Version:  "v1",
	})
	if err == nil {
		t.Fatal("expected invalid config write status marshal to fail")
	}
	if !strings.Contains(err.Error(), `invalid WriteStatus enum value "bad"`) {
		t.Fatalf("unexpected invalid config write status marshal error: %v", err)
	}

	var layer ConfigLayerSource
	err = json.Unmarshal([]byte(`{"type":"workspace"}`), &layer)
	if err == nil {
		t.Fatal("expected unknown config layer source variant to fail")
	}
	if !strings.Contains(err.Error(), `decode ConfigLayerSource.type: unknown variant "workspace"`) {
		t.Fatalf("unexpected unknown config layer source error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"key":"Codex","type":"mdm"}`), &layer)
	if err == nil {
		t.Fatal("expected missing config layer mdm domain to fail")
	}
	if !strings.Contains(err.Error(), "decode ConfigLayerSource.domain: missing required field") {
		t.Fatalf("unexpected missing config layer mdm domain error: %v", err)
	}

	_, err = json.Marshal(ConfigLayerSource{})
	if err == nil {
		t.Fatal("expected zero-value ConfigLayerSource marshal to fail")
	}
	if !strings.Contains(err.Error(), "invalid ConfigLayerSource union value: no variant is set") {
		t.Fatalf("unexpected zero-value ConfigLayerSource error: %v", err)
	}

	var warning ConfigWarningNotification
	err = json.Unmarshal([]byte(`{}`), &warning)
	if err == nil {
		t.Fatal("expected missing config warning summary to fail")
	}
	if !strings.Contains(err.Error(), "decode ConfigWarningNotification.summary: missing required field") {
		t.Fatalf("unexpected missing warning summary error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"range":{"start":{"column":1,"line":1}},"summary":"warning"}`), &warning)
	if err == nil {
		t.Fatal("expected missing config warning range end to fail")
	}
	if !strings.Contains(err.Error(), "decode TextRange.end: missing required field") {
		t.Fatalf("unexpected nested warning range error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"summary":null}`), &warning)
	if err == nil {
		t.Fatal("expected null config warning summary to fail")
	}
	if !strings.Contains(err.Error(), "decode ConfigWarningNotification.summary: null is not allowed") {
		t.Fatalf("unexpected null config warning summary error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"summary":"warning","extra":true}`), &warning)
	if err == nil {
		t.Fatal("expected unknown config warning field to fail")
	}
	if !strings.Contains(err.Error(), `decode ConfigWarningNotification: unknown field "extra"`) {
		t.Fatalf("unexpected unknown config warning field error: %v", err)
	}

	var position TextPosition
	err = json.Unmarshal([]byte(`{"column":-1,"line":1}`), &position)
	if err == nil {
		t.Fatal("expected negative text position column to fail")
	}
	if !strings.Contains(err.Error(), "decode TextPosition.column") {
		t.Fatalf("unexpected negative text position column error: %v", err)
	}
}

func TestGeneratedConfigReadResponseProtocolMarshalAndUnmarshal(t *testing.T) {
	responseRaw, err := json.Marshal(ConfigReadResponse{
		Config: Config{
			Model: Value("gpt-5"),
			DynamicProperties: map[string]JSONValue{
				"custom": JSONBool(true),
			},
		},
		Origins: map[string]ConfigLayerMetadata{
			"model": {
				Name:    NewConfigLayerSourceUser(ConfigLayerSourceUser{File: "/config.toml"}),
				Version: "v1",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantResponse := `{"config":{"custom":true,"model":"gpt-5"},"origins":{"model":{"name":{"file":"/config.toml","type":"user"},"version":"v1"}}}`
	if got := string(responseRaw); got != wantResponse {
		t.Fatalf("ConfigReadResponse JSON = %s, want %s", got, wantResponse)
	}

	sampleRate, err := JSONNumber(json.Number("0.5"))
	if err != nil {
		t.Fatal(err)
	}
	analyticsRaw, err := json.Marshal(AnalyticsConfig{
		Enabled: Value(true),
		DynamicProperties: map[string]JSONValue{
			"sample_rate": sampleRate,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(analyticsRaw), `{"enabled":true,"sample_rate":0.5}`; got != want {
		t.Fatalf("AnalyticsConfig JSON = %s, want %s", got, want)
	}

	var decoded ConfigReadResponse
	if err := json.Unmarshal([]byte(`{"config":{"analytics":{"enabled":true,"sample":"daily"},"custom":true,"model":"gpt-5","profiles":{"work":{"customProfile":1,"model":"gpt-5-mini"}}},"layers":[{"config":{"model":"gpt-5"},"disabledReason":null,"name":{"type":"sessionFlags"},"version":"v2"}],"origins":{"model":{"name":{"file":"/config.toml","type":"user"},"version":"v1"}}}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Config.Model == nil || decoded.Config.Model.Value == nil || *decoded.Config.Model.Value != "gpt-5" {
		t.Fatalf("decoded Config.model = %#v", decoded.Config.Model)
	}
	if decoded.Config.DynamicProperties["custom"].Kind() != JSONKindBool {
		t.Fatalf("decoded Config dynamic properties = %#v", decoded.Config.DynamicProperties)
	}
	if decoded.Config.Analytics == nil || decoded.Config.Analytics.Value == nil {
		t.Fatalf("decoded Config.analytics = %#v", decoded.Config.Analytics)
	}
	if decoded.Config.Analytics.Value.DynamicProperties["sample"].Kind() != JSONKindString {
		t.Fatalf("decoded AnalyticsConfig dynamic properties = %#v", decoded.Config.Analytics.Value.DynamicProperties)
	}
	profiles, ok := decoded.Config.DynamicProperties["profiles"]
	if !ok || profiles.Kind() != JSONKindObject {
		t.Fatalf("decoded Config profiles dynamic property = %#v, present=%t", profiles, ok)
	}
	if decoded.Layers == nil || decoded.Layers.Value == nil || len(*decoded.Layers.Value) != 1 {
		t.Fatalf("decoded ConfigReadResponse.layers = %#v", decoded.Layers)
	}
	layer := (*decoded.Layers.Value)[0]
	if layer.Config.Kind() != JSONKindObject {
		t.Fatalf("decoded layer config kind = %s, want %s", layer.Config.Kind(), JSONKindObject)
	}
	if layer.DisabledReason == nil || layer.DisabledReason.Value != nil {
		t.Fatalf("decoded layer disabledReason = %#v, want explicit null", layer.DisabledReason)
	}
	if decoded.Origins["model"].Version != "v1" {
		t.Fatalf("decoded origins = %#v", decoded.Origins)
	}
}

func TestGeneratedConfigReadResponseRejectsMalformedProtocol(t *testing.T) {
	var response ConfigReadResponse
	err := json.Unmarshal([]byte(`{"origins":{}}`), &response)
	if err == nil {
		t.Fatal("expected missing config read response config to fail")
	}
	if !strings.Contains(err.Error(), "decode ConfigReadResponse.config: missing required field") {
		t.Fatalf("unexpected missing config read response config error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"config":{}}`), &response)
	if err == nil {
		t.Fatal("expected missing config read response origins to fail")
	}
	if !strings.Contains(err.Error(), "decode ConfigReadResponse.origins: missing required field") {
		t.Fatalf("unexpected missing config read response origins error: %v", err)
	}

	_, err = json.Marshal(ConfigReadResponse{Config: Config{}})
	if err == nil {
		t.Fatal("expected nil config read response origins marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode ConfigReadResponse.origins: nil is not allowed") {
		t.Fatalf("unexpected nil origins error: %v", err)
	}

	_, err = json.Marshal(Config{
		DynamicProperties: map[string]JSONValue{
			"model": JSONString("gpt-5"),
		},
	})
	if err == nil {
		t.Fatal("expected config dynamic property declared-field conflict to fail")
	}
	if !strings.Contains(err.Error(), `encode Config.DynamicProperties["model"]: conflicts with declared field`) {
		t.Fatalf("unexpected config dynamic property conflict error: %v", err)
	}

	var layer ConfigLayer
	err = json.Unmarshal([]byte(`{"name":{"type":"sessionFlags"},"version":"v1"}`), &layer)
	if err == nil {
		t.Fatal("expected missing config layer config to fail")
	}
	if !strings.Contains(err.Error(), "decode ConfigLayer.config: missing required field") {
		t.Fatalf("unexpected missing config layer config error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"config":null,"extra":true,"name":{"type":"sessionFlags"},"version":"v1"}`), &layer)
	if err == nil {
		t.Fatal("expected unknown config layer field to fail")
	}
	if !strings.Contains(err.Error(), `decode ConfigLayer: unknown field "extra"`) {
		t.Fatalf("unexpected unknown config layer field error: %v", err)
	}

	_, err = json.Marshal(ConfigLayer{
		Name:    NewConfigLayerSourceSessionFlags(),
		Version: "v1",
	})
	if err == nil {
		t.Fatal("expected invalid config layer JSONValue marshal to fail")
	}
	if !strings.Contains(err.Error(), "invalid JSONValue") {
		t.Fatalf("unexpected invalid config layer JSONValue error: %v", err)
	}

	var summary ReasoningSummary
	if err := json.Unmarshal([]byte(`"detailed"`), &summary); err != nil {
		t.Fatal(err)
	}
	if summary != ReasoningSummaryDetailed {
		t.Fatalf("decoded ReasoningSummary = %s, want %s", summary, ReasoningSummaryDetailed)
	}
	_, err = json.Marshal(ReasoningSummary("verbose"))
	if err == nil {
		t.Fatal("expected invalid reasoning summary marshal to fail")
	}
	if !strings.Contains(err.Error(), `invalid ReasoningSummary enum value "verbose"`) {
		t.Fatalf("unexpected invalid reasoning summary marshal error: %v", err)
	}

	var appConfig AppConfig
	err = json.Unmarshal([]byte(`{"default_tools_approval_mode":"manual"}`), &appConfig)
	if err == nil {
		t.Fatal("expected invalid app tool approval to fail")
	}
	if !strings.Contains(err.Error(), `invalid AppToolApproval enum value "manual"`) {
		t.Fatalf("unexpected invalid app tool approval error: %v", err)
	}

	var webSearch WebSearchToolConfig
	err = json.Unmarshal([]byte(`{"context_size":"low","extra":true}`), &webSearch)
	if err == nil {
		t.Fatal("expected unknown web search tool config field to fail")
	}
	if !strings.Contains(err.Error(), `decode WebSearchToolConfig: unknown field "extra"`) {
		t.Fatalf("unexpected unknown web search tool config error: %v", err)
	}
}

func TestGeneratedConfigRequirementsProtocolMarshalAndUnmarshal(t *testing.T) {
	responseRaw, err := json.Marshal(ConfigRequirementsReadResponse{
		Requirements: Value(ConfigRequirements{
			AllowedApprovalPolicies: Value([]AskForApproval{
				NewAskForApprovalNever(),
				NewAskForApprovalGranular(AskForApprovalGranular{
					MCPElicitations:    true,
					RequestPermissions: boolPtr(true),
					Rules:              false,
					SandboxApproval:    true,
				}),
			}),
			FeatureRequirements: Value(map[string]bool{
				"alpha": true,
				"beta":  false,
			}),
			Network: Value(NetworkRequirements{
				Domains: Value(map[string]NetworkDomainPermission{
					"example.com": NetworkDomainPermissionAllow,
				}),
				UnixSockets: Null[map[string]NetworkUnixSocketPermission](),
			}),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	wantResponse := `{"requirements":{"allowedApprovalPolicies":["never",{"granular":{"mcp_elicitations":true,"request_permissions":true,"rules":false,"sandbox_approval":true}}],"featureRequirements":{"alpha":true,"beta":false},"network":{"domains":{"example.com":"allow"},"unixSockets":null}}}`
	if got := string(responseRaw); got != wantResponse {
		t.Fatalf("ConfigRequirementsReadResponse JSON = %s, want %s", got, wantResponse)
	}

	nullResponseRaw, err := json.Marshal(ConfigRequirementsReadResponse{
		Requirements: Null[ConfigRequirements](),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(nullResponseRaw), `{"requirements":null}`; got != want {
		t.Fatalf("ConfigRequirementsReadResponse null JSON = %s, want %s", got, want)
	}

	emptyResponseRaw, err := json.Marshal(ConfigRequirementsReadResponse{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(emptyResponseRaw), `{}`; got != want {
		t.Fatalf("ConfigRequirementsReadResponse empty JSON = %s, want %s", got, want)
	}

	var decoded ConfigRequirementsReadResponse
	if err := json.Unmarshal([]byte(`{"requirements":{"allowedApprovalPolicies":["on-request"],"allowedApprovalsReviewers":["user"],"allowedSandboxModes":["workspace-write"],"allowedWebSearchModes":null,"enforceResidency":"us","featureRequirements":{"alpha":true},"hooks":{"PermissionRequest":[{"hooks":[{"type":"prompt"}],"matcher":null}],"PostCompact":[],"PostToolUse":[],"PreCompact":[],"PreToolUse":[],"SessionStart":[],"Stop":[],"SubagentStart":[],"SubagentStop":[],"UserPromptSubmit":[],"managedDir":"/managed","windowsManagedDir":null},"network":{"allowLocalBinding":true,"allowUnixSockets":["/tmp/socket"],"domains":{"api.openai.com":"allow","blocked.example":"deny"},"httpPort":8080,"unixSockets":{"agent":"deny"}}}}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Requirements == nil || decoded.Requirements.Value == nil {
		t.Fatalf("decoded requirements = %#v", decoded.Requirements)
	}
	requirements := decoded.Requirements.Value
	if requirements.AllowedApprovalPolicies == nil || requirements.AllowedApprovalPolicies.Value == nil || len(*requirements.AllowedApprovalPolicies.Value) != 1 {
		t.Fatalf("decoded allowedApprovalPolicies = %#v", requirements.AllowedApprovalPolicies)
	}
	if (*requirements.AllowedApprovalPolicies.Value)[0].Kind() != AskForApprovalKindOnRequest {
		t.Fatalf("decoded allowedApprovalPolicies[0] kind = %s", (*requirements.AllowedApprovalPolicies.Value)[0].Kind())
	}
	if requirements.AllowedWebSearchModes == nil || requirements.AllowedWebSearchModes.Value != nil {
		t.Fatalf("decoded allowedWebSearchModes = %#v, want explicit null", requirements.AllowedWebSearchModes)
	}
	if requirements.Hooks == nil || requirements.Hooks.Value == nil || len(requirements.Hooks.Value.PermissionRequest) != 1 {
		t.Fatalf("decoded hooks = %#v", requirements.Hooks)
	}
	hook := requirements.Hooks.Value.PermissionRequest[0].Hooks[0]
	if hook.Kind() != ConfiguredHookHandlerKindPrompt {
		t.Fatalf("decoded hook kind = %s, want %s", hook.Kind(), ConfiguredHookHandlerKindPrompt)
	}
	if requirements.Network == nil || requirements.Network.Value == nil {
		t.Fatalf("decoded network = %#v", requirements.Network)
	}
	if requirements.Network.Value.HTTPPort == nil || requirements.Network.Value.HTTPPort.Value == nil || *requirements.Network.Value.HTTPPort.Value != 8080 {
		t.Fatalf("decoded httpPort = %#v", requirements.Network.Value.HTTPPort)
	}
	if requirements.Network.Value.Domains == nil || requirements.Network.Value.Domains.Value == nil || (*requirements.Network.Value.Domains.Value)["blocked.example"] != NetworkDomainPermissionDeny {
		t.Fatalf("decoded domains = %#v", requirements.Network.Value.Domains)
	}
}

func TestGeneratedAskForApprovalUnionMarshalAndAccessors(t *testing.T) {
	cases := []struct {
		name  string
		value AskForApproval
		kind  AskForApprovalKind
		want  string
	}{
		{name: "untrusted", value: NewAskForApprovalUntrusted(), kind: AskForApprovalKindUntrusted, want: `"untrusted"`},
		{name: "on request", value: NewAskForApprovalOnRequest(), kind: AskForApprovalKindOnRequest, want: `"on-request"`},
		{name: "never", value: NewAskForApprovalNever(), kind: AskForApprovalKindNever, want: `"never"`},
		{
			name: "granular",
			value: NewAskForApprovalGranular(AskForApprovalGranular{
				MCPElicitations: true,
				Rules:           true,
				SandboxApproval: true,
				SkillApproval:   boolPtr(false),
			}),
			kind: AskForApprovalKindGranular,
			want: `{"granular":{"mcp_elicitations":true,"rules":true,"sandbox_approval":true,"skill_approval":false}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.value.Kind() != tc.kind || !tc.value.IsValid() {
				t.Fatalf("AskForApproval kind/valid = %s/%t, want %s/true", tc.value.Kind(), tc.value.IsValid(), tc.kind)
			}
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(raw); got != tc.want {
				t.Fatalf("AskForApproval JSON = %s, want %s", got, tc.want)
			}
			var decoded AskForApproval
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Kind() != tc.kind {
				t.Fatalf("decoded AskForApproval kind = %s, want %s", decoded.Kind(), tc.kind)
			}
		})
	}

	granular := NewAskForApprovalGranular(AskForApprovalGranular{MCPElicitations: true, Rules: true, SandboxApproval: true})
	if payload, ok := granular.AsGranular(); !ok || !payload.MCPElicitations {
		t.Fatalf("AskForApproval AsGranular = %#v ok=%t", payload, ok)
	}
	if _, ok := granular.AsNever(); ok {
		t.Fatal("AskForApproval AsNever returned true for granular variant")
	}
}

func TestGeneratedConfiguredHookHandlerUnionMarshalAndAccessors(t *testing.T) {
	command := NewConfiguredHookHandlerCommand(ConfiguredHookHandlerCommand{
		Async:         true,
		Command:       "echo ok",
		StatusMessage: Null[string](),
		TimeoutSec:    Value(uint64(30)),
	})
	raw, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"async":true,"command":"echo ok","statusMessage":null,"timeoutSec":30,"type":"command"}`; got != want {
		t.Fatalf("ConfiguredHookHandler command JSON = %s, want %s", got, want)
	}
	var decoded ConfiguredHookHandler
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Kind() != ConfiguredHookHandlerKindCommand {
		t.Fatalf("decoded ConfiguredHookHandler kind = %s", decoded.Kind())
	}
	payload, ok := decoded.AsCommand()
	if !ok || payload.Command != "echo ok" || payload.StatusMessage == nil || payload.StatusMessage.Value != nil {
		t.Fatalf("decoded command hook = %#v ok=%t", payload, ok)
	}

	promptRaw, err := json.Marshal(NewConfiguredHookHandlerPrompt())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(promptRaw), `{"type":"prompt"}`; got != want {
		t.Fatalf("ConfiguredHookHandler prompt JSON = %s, want %s", got, want)
	}
	agentRaw, err := json.Marshal(NewConfiguredHookHandlerAgent())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(agentRaw), `{"type":"agent"}`; got != want {
		t.Fatalf("ConfiguredHookHandler agent JSON = %s, want %s", got, want)
	}
	if _, ok := command.AsPrompt(); ok {
		t.Fatal("ConfiguredHookHandler AsPrompt returned true for command variant")
	}
}

func TestGeneratedConfigRequirementsRejectsMalformedProtocol(t *testing.T) {
	var response ConfigRequirementsReadResponse
	err := json.Unmarshal([]byte(`{"extra":true}`), &response)
	if err == nil {
		t.Fatal("expected unknown config requirements response field to fail")
	}
	if !strings.Contains(err.Error(), `decode ConfigRequirementsReadResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown config requirements response field error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"requirements":{"featureRequirements":{"alpha":"yes"}}}`), &response)
	if err == nil {
		t.Fatal("expected invalid featureRequirements value to fail")
	}
	if !strings.Contains(err.Error(), "decode ConfigRequirements.featureRequirements") {
		t.Fatalf("unexpected invalid featureRequirements error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"requirements":{"allowedApprovalPolicies":["sometimes"]}}`), &response)
	if err == nil {
		t.Fatal("expected unknown approval policy to fail")
	}
	if !strings.Contains(err.Error(), `decode AskForApproval.value: unknown variant "sometimes"`) {
		t.Fatalf("unexpected unknown approval policy error: %v", err)
	}

	var approval AskForApproval
	err = json.Unmarshal([]byte(`{"granular":{"rules":true,"sandbox_approval":true}}`), &approval)
	if err == nil {
		t.Fatal("expected missing granular mcp_elicitations to fail")
	}
	if !strings.Contains(err.Error(), "decode AskForApproval.granular.mcp_elicitations: missing required field") {
		t.Fatalf("unexpected missing granular mcp_elicitations error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"other":{}}`), &approval)
	if err == nil {
		t.Fatal("expected unknown approval object variant to fail")
	}
	if !strings.Contains(err.Error(), `decode AskForApproval: unknown field "other"`) {
		t.Fatalf("unexpected unknown approval object variant error: %v", err)
	}

	_, err = json.Marshal(AskForApproval{})
	if err == nil {
		t.Fatal("expected zero-value AskForApproval marshal to fail")
	}
	if !strings.Contains(err.Error(), "invalid AskForApproval union value: no variant is set") {
		t.Fatalf("unexpected zero-value AskForApproval error: %v", err)
	}

	var hook ConfiguredHookHandler
	err = json.Unmarshal([]byte(`{"type":"shell"}`), &hook)
	if err == nil {
		t.Fatal("expected unknown configured hook handler to fail")
	}
	if !strings.Contains(err.Error(), `decode ConfiguredHookHandler.type: unknown variant "shell"`) {
		t.Fatalf("unexpected unknown hook handler error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"async":true,"type":"command"}`), &hook)
	if err == nil {
		t.Fatal("expected missing configured hook command to fail")
	}
	if !strings.Contains(err.Error(), "decode ConfiguredHookHandler.command: missing required field") {
		t.Fatalf("unexpected missing hook command error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"async":true,"command":"echo ok","timeoutSec":-1,"type":"command"}`), &hook)
	if err == nil {
		t.Fatal("expected negative configured hook timeout to fail")
	}
	if !strings.Contains(err.Error(), "decode ConfiguredHookHandler.timeoutSec") {
		t.Fatalf("unexpected negative hook timeout error: %v", err)
	}

	_, err = json.Marshal(ConfiguredHookHandler{})
	if err == nil {
		t.Fatal("expected zero-value ConfiguredHookHandler marshal to fail")
	}
	if !strings.Contains(err.Error(), "invalid ConfiguredHookHandler union value: no variant is set") {
		t.Fatalf("unexpected zero-value ConfiguredHookHandler error: %v", err)
	}

	var managed ManagedHooksRequirements
	err = json.Unmarshal([]byte(`{"PostCompact":[],"PostToolUse":[],"PreCompact":[],"PreToolUse":[],"SessionStart":[],"Stop":[],"UserPromptSubmit":[]}`), &managed)
	if err == nil {
		t.Fatal("expected missing managed hooks PermissionRequest to fail")
	}
	if !strings.Contains(err.Error(), "decode ManagedHooksRequirements.PermissionRequest: missing required field") {
		t.Fatalf("unexpected missing managed hooks field error: %v", err)
	}

	_, err = json.Marshal(ManagedHooksRequirements{})
	if err == nil {
		t.Fatal("expected nil managed hooks PermissionRequest marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode ManagedHooksRequirements.PermissionRequest: nil is not allowed") {
		t.Fatalf("unexpected nil managed hooks marshal error: %v", err)
	}

	var network NetworkRequirements
	err = json.Unmarshal([]byte(`{"domains":{"example.com":"block"}}`), &network)
	if err == nil {
		t.Fatal("expected invalid network domain permission to fail")
	}
	if !strings.Contains(err.Error(), `invalid NetworkDomainPermission enum value "block"`) {
		t.Fatalf("unexpected invalid network domain permission error: %v", err)
	}

	_, err = json.Marshal(NetworkRequirements{
		Domains: Value(map[string]NetworkDomainPermission{
			"example.com": NetworkDomainPermission("block"),
		}),
	})
	if err == nil {
		t.Fatal("expected invalid network domain permission marshal to fail")
	}
	if !strings.Contains(err.Error(), `invalid NetworkDomainPermission enum value "block"`) {
		t.Fatalf("unexpected invalid network domain permission marshal error: %v", err)
	}
}

func TestGeneratedWindowsSandboxProtocolMarshalAndUnmarshal(t *testing.T) {
	for _, tc := range []struct {
		status WindowsSandboxReadiness
		want   string
	}{
		{status: WindowsSandboxReadinessReady, want: `{"status":"ready"}`},
		{status: WindowsSandboxReadinessNotConfigured, want: `{"status":"notConfigured"}`},
		{status: WindowsSandboxReadinessUpdateRequired, want: `{"status":"updateRequired"}`},
	} {
		readinessRaw, err := json.Marshal(WindowsSandboxReadinessResponse{Status: tc.status})
		if err != nil {
			t.Fatal(err)
		}
		if got := string(readinessRaw); got != tc.want {
			t.Fatalf("WindowsSandboxReadinessResponse JSON = %s, want %s", got, tc.want)
		}
		var decoded WindowsSandboxReadinessResponse
		if err := json.Unmarshal(readinessRaw, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Status != tc.status {
			t.Fatalf("decoded readiness status = %s, want %s", decoded.Status, tc.status)
		}
	}

	startRaw, err := json.Marshal(WindowsSandboxSetupStartParams{
		Mode: WindowsSandboxSetupModeElevated,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(startRaw), `{"mode":"elevated"}`; got != want {
		t.Fatalf("WindowsSandboxSetupStartParams omitted cwd JSON = %s, want %s", got, want)
	}

	startWithCWD, err := json.Marshal(WindowsSandboxSetupStartParams{
		CWD:  Value("/workspace"),
		Mode: WindowsSandboxSetupModeUnelevated,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(startWithCWD), `{"cwd":"/workspace","mode":"unelevated"}`; got != want {
		t.Fatalf("WindowsSandboxSetupStartParams cwd JSON = %s, want %s", got, want)
	}

	startWithNullCWD, err := json.Marshal(WindowsSandboxSetupStartParams{
		CWD:  Null[string](),
		Mode: WindowsSandboxSetupModeElevated,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(startWithNullCWD), `{"cwd":null,"mode":"elevated"}`; got != want {
		t.Fatalf("WindowsSandboxSetupStartParams null cwd JSON = %s, want %s", got, want)
	}

	startResponseRaw, err := json.Marshal(WindowsSandboxSetupStartResponse{Started: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(startResponseRaw), `{"started":true}`; got != want {
		t.Fatalf("WindowsSandboxSetupStartResponse JSON = %s, want %s", got, want)
	}

	completedRaw, err := json.Marshal(WindowsSandboxSetupCompletedNotification{
		Error:   Null[string](),
		Mode:    WindowsSandboxSetupModeElevated,
		Success: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(completedRaw), `{"error":null,"mode":"elevated","success":false}`; got != want {
		t.Fatalf("WindowsSandboxSetupCompletedNotification JSON = %s, want %s", got, want)
	}

	warningRaw, err := json.Marshal(WindowsWorldWritableWarningNotification{
		ExtraCount:  2,
		FailedScan:  false,
		SamplePaths: []string{"/tmp/a", "/tmp/b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(warningRaw), `{"extraCount":2,"failedScan":false,"samplePaths":["/tmp/a","/tmp/b"]}`; got != want {
		t.Fatalf("WindowsWorldWritableWarningNotification JSON = %s, want %s", got, want)
	}

	var completed WindowsSandboxSetupCompletedNotification
	err = json.Unmarshal([]byte(`{"error":"failed","mode":"unelevated","success":false}`), &completed)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Error == nil || completed.Error.Value == nil || *completed.Error.Value != "failed" {
		t.Fatalf("decoded setup completed error = %#v", completed.Error)
	}
	if completed.Mode != WindowsSandboxSetupModeUnelevated || completed.Success {
		t.Fatalf("decoded setup completed = %#v", completed)
	}

	var emptyWarning WindowsWorldWritableWarningNotification
	err = json.Unmarshal([]byte(`{"extraCount":0,"failedScan":false,"samplePaths":[]}`), &emptyWarning)
	if err != nil {
		t.Fatal(err)
	}
	if emptyWarning.ExtraCount != 0 || emptyWarning.FailedScan || len(emptyWarning.SamplePaths) != 0 {
		t.Fatalf("decoded empty warning = %#v", emptyWarning)
	}
}

func TestGeneratedWindowsSandboxRejectsMalformedProtocol(t *testing.T) {
	var readiness WindowsSandboxReadinessResponse
	err := json.Unmarshal([]byte(`{}`), &readiness)
	if err == nil {
		t.Fatal("expected missing readiness status to fail")
	}
	if !strings.Contains(err.Error(), "decode WindowsSandboxReadinessResponse.status: missing required field") {
		t.Fatalf("unexpected missing readiness status error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"status":"unknown"}`), &readiness)
	if err == nil {
		t.Fatal("expected invalid readiness status to fail")
	}
	if !strings.Contains(err.Error(), `invalid WindowsSandboxReadiness enum value "unknown"`) {
		t.Fatalf("unexpected invalid readiness status error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"status":"ready","extra":true}`), &readiness)
	if err == nil {
		t.Fatal("expected unknown readiness field to fail")
	}
	if !strings.Contains(err.Error(), `decode WindowsSandboxReadinessResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown readiness field error: %v", err)
	}

	_, err = json.Marshal(WindowsSandboxSetupMode("unknown"))
	if err == nil {
		t.Fatal("expected invalid setup mode marshal to fail")
	}
	if !strings.Contains(err.Error(), `invalid WindowsSandboxSetupMode enum value "unknown"`) {
		t.Fatalf("unexpected invalid setup mode marshal error: %v", err)
	}

	var start WindowsSandboxSetupStartParams
	err = json.Unmarshal([]byte(`{"cwd":null}`), &start)
	if err == nil {
		t.Fatal("expected missing setup mode to fail")
	}
	if !strings.Contains(err.Error(), "decode WindowsSandboxSetupStartParams.mode: missing required field") {
		t.Fatalf("unexpected missing setup mode error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"cwd":null,"mode":null}`), &start)
	if err == nil {
		t.Fatal("expected null setup mode to fail")
	}
	if !strings.Contains(err.Error(), "decode WindowsSandboxSetupStartParams.mode: null is not allowed") {
		t.Fatalf("unexpected null setup mode error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"mode":"elevated","extra":true}`), &start)
	if err == nil {
		t.Fatal("expected unknown setup start field to fail")
	}
	if !strings.Contains(err.Error(), `decode WindowsSandboxSetupStartParams: unknown field "extra"`) {
		t.Fatalf("unexpected unknown setup start field error: %v", err)
	}

	var startResponse WindowsSandboxSetupStartResponse
	err = json.Unmarshal([]byte(`{}`), &startResponse)
	if err == nil {
		t.Fatal("expected missing setup started to fail")
	}
	if !strings.Contains(err.Error(), "decode WindowsSandboxSetupStartResponse.started: missing required field") {
		t.Fatalf("unexpected missing setup started error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"started":null}`), &startResponse)
	if err == nil {
		t.Fatal("expected null setup started to fail")
	}
	if !strings.Contains(err.Error(), "decode WindowsSandboxSetupStartResponse.started: null is not allowed") {
		t.Fatalf("unexpected null setup started error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"started":true,"extra":true}`), &startResponse)
	if err == nil {
		t.Fatal("expected unknown setup response field to fail")
	}
	if !strings.Contains(err.Error(), `decode WindowsSandboxSetupStartResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown setup response field error: %v", err)
	}

	var completed WindowsSandboxSetupCompletedNotification
	err = json.Unmarshal([]byte(`{"mode":"elevated"}`), &completed)
	if err == nil {
		t.Fatal("expected missing setup success to fail")
	}
	if !strings.Contains(err.Error(), "decode WindowsSandboxSetupCompletedNotification.success: missing required field") {
		t.Fatalf("unexpected missing setup success error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"mode":"elevated","success":null}`), &completed)
	if err == nil {
		t.Fatal("expected null setup success to fail")
	}
	if !strings.Contains(err.Error(), "decode WindowsSandboxSetupCompletedNotification.success: null is not allowed") {
		t.Fatalf("unexpected null setup success error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"mode":"elevated","success":true,"extra":true}`), &completed)
	if err == nil {
		t.Fatal("expected unknown setup completed field to fail")
	}
	if !strings.Contains(err.Error(), `decode WindowsSandboxSetupCompletedNotification: unknown field "extra"`) {
		t.Fatalf("unexpected unknown setup completed field error: %v", err)
	}

	var warning WindowsWorldWritableWarningNotification
	err = json.Unmarshal([]byte(`{"extraCount":-1,"failedScan":false,"samplePaths":[]}`), &warning)
	if err == nil {
		t.Fatal("expected negative world writable extra count to fail")
	}
	if !strings.Contains(err.Error(), "decode WindowsWorldWritableWarningNotification.extraCount") {
		t.Fatalf("unexpected negative extra count error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"extraCount":0,"samplePaths":[]}`), &warning)
	if err == nil {
		t.Fatal("expected missing failedScan to fail")
	}
	if !strings.Contains(err.Error(), "decode WindowsWorldWritableWarningNotification.failedScan: missing required field") {
		t.Fatalf("unexpected missing failedScan error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"extraCount":0,"failedScan":false,"samplePaths":null}`), &warning)
	if err == nil {
		t.Fatal("expected null sample paths to fail")
	}
	if !strings.Contains(err.Error(), "decode WindowsWorldWritableWarningNotification.samplePaths: null is not allowed") {
		t.Fatalf("unexpected null sample paths error: %v", err)
	}

	_, err = json.Marshal(WindowsWorldWritableWarningNotification{})
	if err == nil {
		t.Fatal("expected nil sample paths marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode WindowsWorldWritableWarningNotification.samplePaths: nil is not allowed") {
		t.Fatalf("unexpected nil sample paths error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"extraCount":0,"failedScan":false,"samplePaths":[],"extra":true}`), &warning)
	if err == nil {
		t.Fatal("expected unknown world writable field to fail")
	}
	if !strings.Contains(err.Error(), `decode WindowsWorldWritableWarningNotification: unknown field "extra"`) {
		t.Fatalf("unexpected unknown world writable field error: %v", err)
	}
}

func TestGeneratedSendAddCreditsNudgeEmailMarshal(t *testing.T) {
	paramsRaw, err := json.Marshal(SendAddCreditsNudgeEmailParams{
		CreditType: AddCreditsNudgeCreditTypeUsageLimit,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(paramsRaw), `{"creditType":"usage_limit"}`; got != want {
		t.Fatalf("SendAddCreditsNudgeEmailParams JSON = %s, want %s", got, want)
	}

	responseRaw, err := json.Marshal(SendAddCreditsNudgeEmailResponse{
		Status: AddCreditsNudgeEmailStatusCooldownActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(responseRaw), `{"status":"cooldown_active"}`; got != want {
		t.Fatalf("SendAddCreditsNudgeEmailResponse JSON = %s, want %s", got, want)
	}
}

func TestGeneratedFsParamsMarshalAllOfScalarPaths(t *testing.T) {
	recursive := true
	copyRaw, err := json.Marshal(FsCopyParams{
		DestinationPath: "/workspace/dst",
		Recursive:       &recursive,
		SourcePath:      "/workspace/src",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(copyRaw), `{"destinationPath":"/workspace/dst","recursive":true,"sourcePath":"/workspace/src"}`; got != want {
		t.Fatalf("FsCopyParams JSON = %s, want %s", got, want)
	}

	createRaw, err := json.Marshal(FsCreateDirectoryParams{
		Path:      "/workspace/new",
		Recursive: Null[bool](),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(createRaw), `{"path":"/workspace/new","recursive":null}`; got != want {
		t.Fatalf("FsCreateDirectoryParams JSON = %s, want %s", got, want)
	}

	removeRaw, err := json.Marshal(FsRemoveParams{
		Force:     Value(true),
		Path:      "/workspace/old",
		Recursive: Null[bool](),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(removeRaw), `{"force":true,"path":"/workspace/old","recursive":null}`; got != want {
		t.Fatalf("FsRemoveParams JSON = %s, want %s", got, want)
	}

	watchRaw, err := json.Marshal(FsWatchParams{
		Path:    "/workspace",
		WatchID: "watch-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(watchRaw), `{"path":"/workspace","watchId":"watch-1"}`; got != want {
		t.Fatalf("FsWatchParams JSON = %s, want %s", got, want)
	}

	writeRaw, err := json.Marshal(FsWriteFileParams{
		DataBase64: "SGVsbG8=",
		Path:       "/workspace/file.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(writeRaw), `{"dataBase64":"SGVsbG8=","path":"/workspace/file.txt"}`; got != want {
		t.Fatalf("FsWriteFileParams JSON = %s, want %s", got, want)
	}
}

func TestGeneratedFsResponsesMarshalAndUnmarshal(t *testing.T) {
	changedRaw, err := json.Marshal(FsChangedNotification{
		ChangedPaths: []string{"/workspace/file.txt"},
		WatchID:      "watch-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(changedRaw), `{"changedPaths":["/workspace/file.txt"],"watchId":"watch-1"}`; got != want {
		t.Fatalf("FsChangedNotification JSON = %s, want %s", got, want)
	}

	readDirectoryRaw, err := json.Marshal(FsReadDirectoryResponse{
		Entries: []FsReadDirectoryEntry{{
			FileName:    "file.txt",
			IsDirectory: false,
			IsFile:      true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(readDirectoryRaw), `{"entries":[{"fileName":"file.txt","isDirectory":false,"isFile":true}]}`; got != want {
		t.Fatalf("FsReadDirectoryResponse JSON = %s, want %s", got, want)
	}

	var metadata FsGetMetadataResponse
	if err := json.Unmarshal([]byte(`{"createdAtMs":1,"isDirectory":false,"isFile":true,"isSymlink":false,"modifiedAtMs":2}`), &metadata); err != nil {
		t.Fatal(err)
	}
	if !metadata.IsFile || metadata.CreatedAtMS != 1 || metadata.ModifiedAtMS != 2 {
		t.Fatalf("decoded FsGetMetadataResponse = %#v", metadata)
	}

	var readFile FsReadFileResponse
	if err := json.Unmarshal([]byte(`{"dataBase64":"SGVsbG8="}`), &readFile); err != nil {
		t.Fatal(err)
	}
	if readFile.DataBase64 != "SGVsbG8=" {
		t.Fatalf("decoded FsReadFileResponse = %#v", readFile)
	}

	assertEmptyResponse := func(name string, marshal func() ([]byte, error)) {
		raw, err := marshal()
		if err != nil {
			t.Fatalf("%s empty response marshal: %v", name, err)
		}
		if got, want := string(raw), `{}`; got != want {
			t.Fatalf("%s empty response JSON = %s, want %s", name, got, want)
		}
	}
	assertEmptyResponse("copy", func() ([]byte, error) { return json.Marshal(FsCopyResponse{}) })
	assertEmptyResponse("createDirectory", func() ([]byte, error) { return json.Marshal(FsCreateDirectoryResponse{}) })
	assertEmptyResponse("remove", func() ([]byte, error) { return json.Marshal(FsRemoveResponse{}) })
	assertEmptyResponse("unwatch", func() ([]byte, error) { return json.Marshal(FsUnwatchResponse{}) })
	assertEmptyResponse("writeFile", func() ([]byte, error) { return json.Marshal(FsWriteFileResponse{}) })
}

func TestGeneratedFsRejectsMalformedProtocol(t *testing.T) {
	var copyParams FsCopyParams
	err := json.Unmarshal([]byte(`{"destinationPath":"/dst"}`), &copyParams)
	if err == nil {
		t.Fatal("expected missing sourcePath to fail")
	}
	if !strings.Contains(err.Error(), "decode FsCopyParams.sourcePath: missing required field") {
		t.Fatalf("unexpected missing sourcePath error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"destinationPath":"/dst","recursive":null,"sourcePath":"/src"}`), &copyParams)
	if err == nil {
		t.Fatal("expected null optional recursive pointer to fail")
	}
	if !strings.Contains(err.Error(), "decode FsCopyParams.recursive: null is not allowed") {
		t.Fatalf("unexpected null recursive error: %v", err)
	}

	var watchResponse FsWatchResponse
	err = json.Unmarshal([]byte(`{"path":null}`), &watchResponse)
	if err == nil {
		t.Fatal("expected null FsWatchResponse path to fail")
	}
	if !strings.Contains(err.Error(), "decode FsWatchResponse.path: null is not allowed") {
		t.Fatalf("unexpected null watch path error: %v", err)
	}

	_, err = json.Marshal(FsReadDirectoryResponse{})
	if err == nil {
		t.Fatal("expected nil directory entries marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode FsReadDirectoryResponse.entries: nil is not allowed") {
		t.Fatalf("unexpected nil entries error: %v", err)
	}

	var readDirectory FsReadDirectoryResponse
	err = json.Unmarshal([]byte(`{"entries":[{"fileName":"file.txt","isFile":true}]}`), &readDirectory)
	if err == nil {
		t.Fatal("expected missing directory entry isDirectory to fail")
	}
	if !strings.Contains(err.Error(), "decode FsReadDirectoryEntry.isDirectory: missing required field") {
		t.Fatalf("unexpected missing directory entry field error: %v", err)
	}

	var removeResponse FsRemoveResponse
	err = json.Unmarshal([]byte(`{"extra":true}`), &removeResponse)
	if err == nil {
		t.Fatal("expected unknown empty response field to fail")
	}
	if !strings.Contains(err.Error(), `decode FsRemoveResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown empty response field error: %v", err)
	}
}

func TestGeneratedInitializeParamsAndResponseMarshalAndRejectsMalformedProtocol(t *testing.T) {
	paramsRaw, err := json.Marshal(InitializeParams{
		Capabilities: Value(InitializeCapabilities{
			ExperimentalAPI:           boolPtr(true),
			OptOutNotificationMethods: Null[[]string](),
		}),
		ClientInfo: ClientInfo{
			Name:    "codex-go-sdk",
			Title:   Value("Codex Go SDK"),
			Version: "codex-go-sdk-v1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(paramsRaw), `{"capabilities":{"experimentalApi":true,"optOutNotificationMethods":null},"clientInfo":{"name":"codex-go-sdk","title":"Codex Go SDK","version":"codex-go-sdk-v1"}}`; got != want {
		t.Fatalf("InitializeParams JSON = %s, want %s", got, want)
	}

	var params InitializeParams
	err = json.Unmarshal([]byte(`{"clientInfo":{"name":"codex-go-sdk","version":"codex-go-sdk-v1"}}`), &params)
	if err != nil {
		t.Fatal(err)
	}
	if params.Capabilities != nil || params.ClientInfo.Title != nil {
		t.Fatalf("decoded omitted initialize params optional fields = %#v", params)
	}

	err = json.Unmarshal([]byte(`{"capabilities":{"experimentalApi":true,"optOutNotificationMethods":["thread/started"]},"clientInfo":{"name":"codex-go-sdk","title":null,"version":"codex-go-sdk-v1"}}`), &params)
	if err != nil {
		t.Fatal(err)
	}
	if params.Capabilities == nil || params.Capabilities.Value == nil ||
		params.Capabilities.Value.ExperimentalAPI == nil || *params.Capabilities.Value.ExperimentalAPI != true ||
		params.Capabilities.Value.OptOutNotificationMethods == nil ||
		params.Capabilities.Value.OptOutNotificationMethods.Value == nil ||
		(*params.Capabilities.Value.OptOutNotificationMethods.Value)[0] != "thread/started" ||
		params.ClientInfo.Title == nil || params.ClientInfo.Title.Value != nil {
		t.Fatalf("decoded initialize params = %#v", params)
	}

	err = json.Unmarshal([]byte(`{"capabilities":{},"clientInfo":{"version":"codex-go-sdk-v1"}}`), &params)
	if err == nil {
		t.Fatal("expected missing initialize clientInfo.name to fail")
	}
	if !strings.Contains(err.Error(), "decode ClientInfo.name: missing required field") {
		t.Fatalf("unexpected missing clientInfo.name error: %v", err)
	}

	raw, err := json.Marshal(InitializeResponse{
		CodexHome:      "/home/codex",
		PlatformFamily: "unix",
		PlatformOs:     "macos",
		UserAgent:      "codex-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"codexHome":"/home/codex","platformFamily":"unix","platformOs":"macos","userAgent":"codex-test"}`; got != want {
		t.Fatalf("InitializeResponse JSON = %s, want %s", got, want)
	}

	var decoded InitializeResponse
	err = json.Unmarshal([]byte(`{"codexHome":"/home/codex","platformFamily":"unix","userAgent":"codex-test"}`), &decoded)
	if err == nil {
		t.Fatal("expected missing platformOs to fail")
	}
	if !strings.Contains(err.Error(), "decode InitializeResponse.platformOs: missing required field") {
		t.Fatalf("unexpected missing platformOs error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"codexHome":"/home/codex","platformFamily":"unix","platformOs":"macos","userAgent":"codex-test","extra":true}`), &decoded)
	if err == nil {
		t.Fatal("expected unknown InitializeResponse field to fail")
	}
	if !strings.Contains(err.Error(), `decode InitializeResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown InitializeResponse field error: %v", err)
	}
}

func TestGeneratedExperimentalFeatureEnablementSetParamsMarshalMap(t *testing.T) {
	raw, err := json.Marshal(ExperimentalFeatureEnablementSetParams{
		Enablement: map[string]bool{"feature_a": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"enablement":{"feature_a":true}}`; got != want {
		t.Fatalf("ExperimentalFeatureEnablementSetParams JSON = %s, want %s", got, want)
	}
}

func TestGeneratedSmallUtilityPayloadsProtocolMarshalAndUnmarshal(t *testing.T) {
	for _, tc := range []struct {
		name   string
		value  any
		target any
		want   string
	}{
		{name: "experimental feature enablement set params", value: ExperimentalFeatureEnablementSetParams{Enablement: map[string]bool{"feature_a": true}}, target: &ExperimentalFeatureEnablementSetParams{}, want: `{"enablement":{"feature_a":true}}`},
		{name: "experimental feature enablement set response", value: ExperimentalFeatureEnablementSetResponse{Enablement: map[string]bool{"feature_a": false}}, target: &ExperimentalFeatureEnablementSetResponse{}, want: `{"enablement":{"feature_a":false}}`},
		{name: "experimental feature list params", value: ExperimentalFeatureListParams{Cursor: Null[string](), Limit: Value(uint32(25))}, target: &ExperimentalFeatureListParams{}, want: `{"cursor":null,"limit":25}`},
		{name: "experimental feature list response", value: ExperimentalFeatureListResponse{Data: []ExperimentalFeature{{DefaultEnabled: true, Enabled: false, Name: "feature_a", Stage: ExperimentalFeatureStageBeta}}, NextCursor: Value("cursor-2")}, target: &ExperimentalFeatureListResponse{}, want: `{"data":[{"defaultEnabled":true,"enabled":false,"name":"feature_a","stage":"beta"}],"nextCursor":"cursor-2"}`},
		{name: "external agent config detect params", value: ExternalAgentConfigDetectParams{CWDs: Value([]string{"/repo"}), IncludeHome: boolPtr(true)}, target: &ExternalAgentConfigDetectParams{}, want: `{"cwds":["/repo"],"includeHome":true}`},
		{name: "external agent config detect response", value: ExternalAgentConfigDetectResponse{Items: []ExternalAgentConfigMigrationItem{{CWD: Value("/repo"), Description: "Import command", Details: Value(MigrationDetails{Commands: &[]CommandMigration{{Name: "build"}}}), ItemType: ExternalAgentConfigMigrationItemTypeCOMMANDS}}}, target: &ExternalAgentConfigDetectResponse{}, want: `{"items":[{"cwd":"/repo","description":"Import command","details":{"commands":[{"name":"build"}]},"itemType":"COMMANDS"}]}`},
		{name: "external agent config import params", value: ExternalAgentConfigImportParams{MigrationItems: []ExternalAgentConfigMigrationItem{{CWD: Null[string](), Description: "Import plugins", Details: Value(MigrationDetails{Plugins: &[]PluginsMigration{{MarketplaceName: "local", PluginNames: []string{"plugin-a"}}}}), ItemType: ExternalAgentConfigMigrationItemTypePLUGINS}}}, target: &ExternalAgentConfigImportParams{}, want: `{"migrationItems":[{"cwd":null,"description":"Import plugins","details":{"plugins":[{"marketplaceName":"local","pluginNames":["plugin-a"]}]},"itemType":"PLUGINS"}]}`},
		{name: "hooks list params", value: HooksListParams{CWDs: &[]string{"/repo"}}, target: &HooksListParams{}, want: `{"cwds":["/repo"]}`},
		{name: "hooks list response", value: HooksListResponse{Data: []HooksListEntry{{
			CWD:    "/repo",
			Errors: []HookErrorInfo{{Message: "missing command", Path: "/repo/.codex/hooks.json"}},
			Hooks: []HookMetadata{{
				Command:       Null[string](),
				CurrentHash:   "hash-1",
				DisplayOrder:  1,
				Enabled:       true,
				EventName:     HookEventNamePreToolUse,
				HandlerType:   HookHandlerTypeCommand,
				IsManaged:     false,
				Key:           "hook-1",
				Matcher:       Value("shell"),
				PluginID:      Null[string](),
				Source:        HookSourceProject,
				SourcePath:    "/repo/.codex/hooks.json",
				StatusMessage: Value("trusted"),
				TimeoutSec:    10,
				TrustStatus:   HookTrustStatusTrusted,
			}},
			Warnings: []string{"review hook"},
		}}}, target: &HooksListResponse{}, want: `{"data":[{"cwd":"/repo","errors":[{"message":"missing command","path":"/repo/.codex/hooks.json"}],"hooks":[{"command":null,"currentHash":"hash-1","displayOrder":1,"enabled":true,"eventName":"preToolUse","handlerType":"command","isManaged":false,"key":"hook-1","matcher":"shell","pluginId":null,"source":"project","sourcePath":"/repo/.codex/hooks.json","statusMessage":"trusted","timeoutSec":10,"trustStatus":"trusted"}],"warnings":["review hook"]}]}`},
		{name: "skills config write response", value: SkillsConfigWriteResponse{EffectiveEnabled: true}, target: &SkillsConfigWriteResponse{}, want: `{"effectiveEnabled":true}`},
		{name: "skills list params", value: SkillsListParams{CWDs: &[]string{"/repo"}, ForceReload: boolPtr(true)}, target: &SkillsListParams{}, want: `{"cwds":["/repo"],"forceReload":true}`},
		{name: "skills list response", value: SkillsListResponse{Data: []SkillsListEntry{{
			CWD:    "/repo",
			Errors: []SkillErrorInfo{{Message: "invalid skill", Path: "/repo/.codex/skills/bad/SKILL.md"}},
			Skills: []SkillMetadata{{
				Dependencies: Value(SkillDependencies{
					Tools: []SkillToolDependency{{
						Command:     Value("rg"),
						Description: Null[string](),
						Transport:   Null[string](),
						Type:        "command",
						URL:         Null[string](),
						Value:       "rg",
					}},
				}),
				Description: "review docs",
				Enabled:     true,
				Interface: Value(SkillInterface{
					DisplayName:      Value("Review"),
					ShortDescription: Null[string](),
				}),
				Name:             "review",
				Path:             "/repo/.codex/skills/review/SKILL.md",
				Scope:            SkillScopeRepo,
				ShortDescription: Value("Review docs"),
			}},
		}}}, target: &SkillsListResponse{}, want: `{"data":[{"cwd":"/repo","errors":[{"message":"invalid skill","path":"/repo/.codex/skills/bad/SKILL.md"}],"skills":[{"dependencies":{"tools":[{"command":"rg","description":null,"transport":null,"type":"command","url":null,"value":"rg"}]},"description":"review docs","enabled":true,"interface":{"displayName":"Review","shortDescription":null},"name":"review","path":"/repo/.codex/skills/review/SKILL.md","scope":"repo","shortDescription":"Review docs"}]}]}`},
		{name: "guardian denied action response", value: ThreadApproveGuardianDeniedActionResponse{}, target: &ThreadApproveGuardianDeniedActionResponse{}, want: `{}`},
		{name: "turn interrupt response", value: TurnInterruptResponse{}, target: &TurnInterruptResponse{}, want: `{}`},
		{name: "turn steer response", value: TurnSteerResponse{TurnID: "turn-1"}, target: &TurnSteerResponse{}, want: `{"turnId":"turn-1"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(raw); got != tc.want {
				t.Fatalf("%s JSON = %s, want %s", tc.name, got, tc.want)
			}
			if err := json.Unmarshal(raw, tc.target); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGeneratedSmallUtilityPayloadsRejectMalformedProtocol(t *testing.T) {
	var enablement ExperimentalFeatureEnablementSetParams
	err := json.Unmarshal([]byte(`{}`), &enablement)
	if err == nil {
		t.Fatal("expected missing enablement to fail")
	}
	if !strings.Contains(err.Error(), "decode ExperimentalFeatureEnablementSetParams.enablement: missing required field") {
		t.Fatalf("unexpected missing enablement error: %v", err)
	}

	_, err = json.Marshal(ExperimentalFeatureEnablementSetResponse{})
	if err == nil {
		t.Fatal("expected nil enablement response map to fail")
	}
	if !strings.Contains(err.Error(), "encode ExperimentalFeatureEnablementSetResponse.enablement: nil is not allowed") {
		t.Fatalf("unexpected nil enablement error: %v", err)
	}

	var experimentalFeatures ExperimentalFeatureListResponse
	err = json.Unmarshal([]byte(`{"data":[{"defaultEnabled":true,"enabled":true,"name":"feature_a","stage":"unknown"}]}`), &experimentalFeatures)
	if err == nil {
		t.Fatal("expected unknown experimental feature stage to fail")
	}
	if !strings.Contains(err.Error(), `invalid ExperimentalFeatureStage enum value "unknown"`) {
		t.Fatalf("unexpected experimental feature stage error: %v", err)
	}

	_, err = json.Marshal(ExperimentalFeatureListResponse{})
	if err == nil {
		t.Fatal("expected nil experimental feature list data to fail")
	}
	if !strings.Contains(err.Error(), "encode ExperimentalFeatureListResponse.data: nil is not allowed") {
		t.Fatalf("unexpected nil experimental feature data error: %v", err)
	}

	var externalAgentConfigDetect ExternalAgentConfigDetectResponse
	err = json.Unmarshal([]byte(`{"items":[{"description":"bad","itemType":"UNKNOWN"}]}`), &externalAgentConfigDetect)
	if err == nil {
		t.Fatal("expected unknown external agent config migration item type to fail")
	}
	if !strings.Contains(err.Error(), `invalid ExternalAgentConfigMigrationItemType enum value "UNKNOWN"`) {
		t.Fatalf("unexpected external agent config item type error: %v", err)
	}

	_, err = json.Marshal(ExternalAgentConfigDetectResponse{})
	if err == nil {
		t.Fatal("expected nil external agent config detect items to fail")
	}
	if !strings.Contains(err.Error(), "encode ExternalAgentConfigDetectResponse.items: nil is not allowed") {
		t.Fatalf("unexpected nil external agent config detect items error: %v", err)
	}

	_, err = json.Marshal(ExternalAgentConfigImportParams{})
	if err == nil {
		t.Fatal("expected nil external agent config import migrationItems to fail")
	}
	if !strings.Contains(err.Error(), "encode ExternalAgentConfigImportParams.migrationItems: nil is not allowed") {
		t.Fatalf("unexpected nil external agent config import items error: %v", err)
	}

	var hooks HooksListParams
	err = json.Unmarshal([]byte(`{"cwds":null}`), &hooks)
	if err == nil {
		t.Fatal("expected hooks cwds null to fail")
	}
	if !strings.Contains(err.Error(), "decode HooksListParams.cwds: null is not allowed") {
		t.Fatalf("unexpected hooks cwds null error: %v", err)
	}

	_, err = json.Marshal(HooksListResponse{})
	if err == nil {
		t.Fatal("expected nil hooks list response data to fail")
	}
	if !strings.Contains(err.Error(), "encode HooksListResponse.data: nil is not allowed") {
		t.Fatalf("unexpected nil hooks list response data error: %v", err)
	}

	_, err = json.Marshal(HooksListEntry{
		CWD:      "/repo",
		Errors:   []HookErrorInfo{},
		Warnings: []string{},
	})
	if err == nil {
		t.Fatal("expected nil hooks list entry hooks to fail")
	}
	if !strings.Contains(err.Error(), "encode HooksListEntry.hooks: nil is not allowed") {
		t.Fatalf("unexpected nil hooks list entry hooks error: %v", err)
	}

	var hooksList HooksListResponse
	err = json.Unmarshal([]byte(`{"data":[{"cwd":"/repo","errors":[],"hooks":[{"currentHash":"hash-1","displayOrder":1,"enabled":true,"eventName":"unknown","handlerType":"command","isManaged":false,"key":"hook-1","source":"project","sourcePath":"/repo/.codex/hooks.json","timeoutSec":10,"trustStatus":"trusted"}],"warnings":[]}]}`), &hooksList)
	if err == nil {
		t.Fatal("expected unknown hook event name to fail")
	}
	if !strings.Contains(err.Error(), `invalid HookEventName enum value "unknown"`) {
		t.Fatalf("unexpected hook event name error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"data":[{"cwd":"/repo","errors":[],"hooks":[],"warnings":[],"extra":true}]}`), &hooksList)
	if err == nil {
		t.Fatal("expected unknown hooks list entry field to fail")
	}
	if !strings.Contains(err.Error(), `decode HooksListEntry: unknown field "extra"`) {
		t.Fatalf("unexpected unknown hooks list entry field error: %v", err)
	}

	_, err = json.Marshal(ReviewStartParams{
		ThreadID: "thread-1",
	})
	if err == nil {
		t.Fatal("expected unset review target to fail")
	}
	if !strings.Contains(err.Error(), "invalid ReviewTarget union value: no variant is set") {
		t.Fatalf("unexpected unset review target error: %v", err)
	}

	var review ReviewStartParams
	err = json.Unmarshal([]byte(`{"threadId":"thread-1"}`), &review)
	if err == nil {
		t.Fatal("expected missing review target to fail")
	}
	if !strings.Contains(err.Error(), "decode ReviewStartParams.target: missing required field") {
		t.Fatalf("unexpected missing review target error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"threadId":"thread-1","target":{"type":"unknown"}}`), &review)
	if err == nil {
		t.Fatal("expected unknown review target to fail")
	}
	if !strings.Contains(err.Error(), `decode ReviewTarget.type: unknown variant "unknown"`) {
		t.Fatalf("unexpected unknown review target error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"threadId":"thread-1","target":{"type":"commit"}}`), &review)
	if err == nil {
		t.Fatal("expected missing review commit sha to fail")
	}
	if !strings.Contains(err.Error(), "decode ReviewTarget.sha: missing required field") {
		t.Fatalf("unexpected missing review commit sha error: %v", err)
	}

	var skills SkillsListParams
	err = json.Unmarshal([]byte(`{"forceReload":null}`), &skills)
	if err == nil {
		t.Fatal("expected skills forceReload null to fail")
	}
	if !strings.Contains(err.Error(), "decode SkillsListParams.forceReload: null is not allowed") {
		t.Fatalf("unexpected skills forceReload null error: %v", err)
	}

	_, err = json.Marshal(SkillsListResponse{})
	if err == nil {
		t.Fatal("expected nil skills list response data to fail")
	}
	if !strings.Contains(err.Error(), "encode SkillsListResponse.data: nil is not allowed") {
		t.Fatalf("unexpected nil skills list response data error: %v", err)
	}

	var skillsList SkillsListResponse
	err = json.Unmarshal([]byte(`{}`), &skillsList)
	if err == nil {
		t.Fatal("expected missing skills list response data to fail")
	}
	if !strings.Contains(err.Error(), "decode SkillsListResponse.data: missing required field") {
		t.Fatalf("unexpected missing skills list response data error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"data":[{"cwd":"/repo","errors":[],"skills":[{"description":"review","enabled":true,"name":"review","path":"/repo/.codex/skills/review/SKILL.md","scope":"unknown"}]}]}`), &skillsList)
	if err == nil {
		t.Fatal("expected invalid skill scope to fail")
	}
	if !strings.Contains(err.Error(), `invalid SkillScope enum value "unknown"`) {
		t.Fatalf("unexpected invalid skill scope error: %v", err)
	}

	_, err = json.Marshal(SkillsListEntry{
		CWD:    "/repo",
		Errors: []SkillErrorInfo{},
	})
	if err == nil {
		t.Fatal("expected nil skills list entry skills to fail")
	}
	if !strings.Contains(err.Error(), "encode SkillsListEntry.skills: nil is not allowed") {
		t.Fatalf("unexpected nil skills list entry skills error: %v", err)
	}

	var interrupt TurnInterruptResponse
	err = json.Unmarshal([]byte(`{"extra":true}`), &interrupt)
	if err == nil {
		t.Fatal("expected unknown turn interrupt response field to fail")
	}
	if !strings.Contains(err.Error(), `decode TurnInterruptResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown turn interrupt response error: %v", err)
	}

	var interruptParams TurnInterruptParams
	err = json.Unmarshal([]byte(`{"turnId":"turn-1"}`), &interruptParams)
	if err == nil {
		t.Fatal("expected missing turn interrupt threadId to fail")
	}
	if !strings.Contains(err.Error(), "decode TurnInterruptParams.threadId: missing required field") {
		t.Fatalf("unexpected missing turn interrupt threadId error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"threadId":"thread-1"}`), &interruptParams)
	if err == nil {
		t.Fatal("expected missing turn interrupt turnId to fail")
	}
	if !strings.Contains(err.Error(), "decode TurnInterruptParams.turnId: missing required field") {
		t.Fatalf("unexpected missing turn interrupt turnId error: %v", err)
	}

	_, err = json.Marshal(TurnSteerParams{
		ExpectedTurnID: "turn-1",
		ThreadID:       "thread-1",
	})
	if err == nil {
		t.Fatal("expected nil turn steer input to fail")
	}
	if !strings.Contains(err.Error(), "encode TurnSteerParams.input: nil is not allowed") {
		t.Fatalf("unexpected nil turn steer input error: %v", err)
	}

	var steer TurnSteerParams
	err = json.Unmarshal([]byte(`{"input":[],"threadId":"thread-1"}`), &steer)
	if err == nil {
		t.Fatal("expected missing turn steer expectedTurnId to fail")
	}
	if !strings.Contains(err.Error(), "decode TurnSteerParams.expectedTurnId: missing required field") {
		t.Fatalf("unexpected missing turn steer expectedTurnId error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"expectedTurnId":"turn-1","threadId":"thread-1"}`), &steer)
	if err == nil {
		t.Fatal("expected missing turn steer input to fail")
	}
	if !strings.Contains(err.Error(), "decode TurnSteerParams.input: missing required field") {
		t.Fatalf("unexpected missing turn steer input error: %v", err)
	}

	var steerResponse TurnSteerResponse
	err = json.Unmarshal([]byte(`{}`), &steerResponse)
	if err == nil {
		t.Fatal("expected missing turn steer response turnId to fail")
	}
	if !strings.Contains(err.Error(), "decode TurnSteerResponse.turnId: missing required field") {
		t.Fatalf("unexpected missing turn steer response turnId error: %v", err)
	}
}

func TestGeneratedFuzzyFileSearchParamsPreserveNullableCancellation(t *testing.T) {
	raw, err := json.Marshal(FuzzyFileSearchParams{
		Query: "readme",
		Roots: []string{"/repo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"query":"readme","roots":["/repo"]}`; got != want {
		t.Fatalf("FuzzyFileSearchParams omitted cancellation JSON = %s, want %s", got, want)
	}

	withNull, err := json.Marshal(FuzzyFileSearchParams{
		CancellationToken: Null[string](),
		Query:             "readme",
		Roots:             []string{"/repo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(withNull), `{"cancellationToken":null,"query":"readme","roots":["/repo"]}`; got != want {
		t.Fatalf("FuzzyFileSearchParams null cancellation JSON = %s, want %s", got, want)
	}

	var decoded FuzzyFileSearchParams
	if err := json.Unmarshal([]byte(`{"cancellationToken":"cancel-1","query":"main","roots":["/repo","/tmp"]}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.CancellationToken == nil || decoded.CancellationToken.Value == nil || *decoded.CancellationToken.Value != "cancel-1" {
		t.Fatalf("decoded cancellationToken = %#v", decoded.CancellationToken)
	}
	if len(decoded.Roots) != 2 || decoded.Roots[1] != "/tmp" {
		t.Fatalf("decoded roots = %#v", decoded.Roots)
	}
}

func TestGeneratedFuzzyFileSearchResponseMarshalAndUnmarshalResults(t *testing.T) {
	response := FuzzyFileSearchResponse{
		Files: []FuzzyFileSearchResult{{
			FileName:  "README.md",
			Indices:   Value([]uint32{0, 2}),
			MatchType: FuzzyFileSearchMatchTypeFile,
			Path:      "/repo/README.md",
			Root:      "/repo",
			Score:     99,
		}},
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"files":[{"file_name":"README.md","indices":[0,2],"match_type":"file","path":"/repo/README.md","root":"/repo","score":99}]}`
	if got := string(raw); got != want {
		t.Fatalf("FuzzyFileSearchResponse JSON = %s, want %s", got, want)
	}

	var decoded FuzzyFileSearchResponse
	if err := json.Unmarshal([]byte(`{"files":[{"file_name":"src","indices":null,"match_type":"directory","path":"/repo/src","root":"/repo","score":7}]}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Files) != 1 {
		t.Fatalf("decoded files length = %d, want 1", len(decoded.Files))
	}
	result := decoded.Files[0]
	if result.MatchType != FuzzyFileSearchMatchTypeDirectory || result.Score != 7 {
		t.Fatalf("decoded fuzzy result = %#v", result)
	}
	if result.Indices == nil || result.Indices.Value != nil {
		t.Fatalf("decoded null indices = %#v", result.Indices)
	}
}

func TestGeneratedFuzzyFileSearchSessionTypes(t *testing.T) {
	start, err := json.Marshal(FuzzyFileSearchSessionStartParams{
		Roots:     []string{"/repo"},
		SessionID: "session-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(start), `{"roots":["/repo"],"sessionId":"session-1"}`; got != want {
		t.Fatalf("FuzzyFileSearchSessionStartParams JSON = %s, want %s", got, want)
	}

	update, err := json.Marshal(FuzzyFileSearchSessionUpdateParams{
		Query:     "main",
		SessionID: "session-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(update), `{"query":"main","sessionId":"session-1"}`; got != want {
		t.Fatalf("FuzzyFileSearchSessionUpdateParams JSON = %s, want %s", got, want)
	}

	stop, err := json.Marshal(FuzzyFileSearchSessionStopParams{
		SessionID: "session-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(stop), `{"sessionId":"session-1"}`; got != want {
		t.Fatalf("FuzzyFileSearchSessionStopParams JSON = %s, want %s", got, want)
	}

	for _, tc := range []struct {
		name   string
		decode func() error
	}{
		{
			name: "start",
			decode: func() error {
				var response FuzzyFileSearchSessionStartResponse
				return json.Unmarshal([]byte(`{}`), &response)
			},
		},
		{
			name: "stop",
			decode: func() error {
				var response FuzzyFileSearchSessionStopResponse
				return json.Unmarshal([]byte(`{}`), &response)
			},
		},
		{
			name: "update",
			decode: func() error {
				var response FuzzyFileSearchSessionUpdateResponse
				return json.Unmarshal([]byte(`{}`), &response)
			},
		},
	} {
		if err := tc.decode(); err != nil {
			t.Fatalf("%s response unmarshal: %v", tc.name, err)
		}
	}

	notification := FuzzyFileSearchSessionUpdatedNotification{
		Files: []FuzzyFileSearchResult{{
			FileName:  "main.go",
			MatchType: FuzzyFileSearchMatchTypeFile,
			Path:      "/repo/main.go",
			Root:      "/repo",
			Score:     42,
		}},
		Query:     "main",
		SessionID: "session-1",
	}
	raw, err := json.Marshal(notification)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"files":[{"file_name":"main.go","match_type":"file","path":"/repo/main.go","root":"/repo","score":42}],"query":"main","sessionId":"session-1"}`
	if got := string(raw); got != want {
		t.Fatalf("FuzzyFileSearchSessionUpdatedNotification JSON = %s, want %s", got, want)
	}

	var completed FuzzyFileSearchSessionCompletedNotification
	if err := json.Unmarshal([]byte(`{"sessionId":"session-1"}`), &completed); err != nil {
		t.Fatal(err)
	}
	if completed.SessionID != "session-1" {
		t.Fatalf("completed sessionId = %q, want session-1", completed.SessionID)
	}
}

func TestGeneratedFuzzyFileSearchRejectsMalformedProtocol(t *testing.T) {
	var params FuzzyFileSearchParams
	err := json.Unmarshal([]byte(`{"roots":["/repo"]}`), &params)
	if err == nil {
		t.Fatal("expected missing fuzzy query to fail")
	}
	if !strings.Contains(err.Error(), "decode FuzzyFileSearchParams.query: missing required field") {
		t.Fatalf("unexpected missing query error: %v", err)
	}

	_, err = json.Marshal(FuzzyFileSearchParams{Query: "readme"})
	if err == nil {
		t.Fatal("expected nil fuzzy roots marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode FuzzyFileSearchParams.roots: nil is not allowed") {
		t.Fatalf("unexpected nil roots marshal error: %v", err)
	}

	var response FuzzyFileSearchResponse
	err = json.Unmarshal([]byte(`{"files":[{"file_name":"README.md","match_type":"symlink","path":"/repo/README.md","root":"/repo","score":1}]}`), &response)
	if err == nil {
		t.Fatal("expected invalid fuzzy match type to fail")
	}
	if !strings.Contains(err.Error(), `invalid FuzzyFileSearchMatchType enum value "symlink"`) {
		t.Fatalf("unexpected invalid match type error: %v", err)
	}

	_, err = json.Marshal(FuzzyFileSearchResponse{})
	if err == nil {
		t.Fatal("expected nil fuzzy files marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode FuzzyFileSearchResponse.files: nil is not allowed") {
		t.Fatalf("unexpected nil files marshal error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"files":[],"extra":true}`), &response)
	if err == nil {
		t.Fatal("expected unknown fuzzy response field to fail")
	}
	if !strings.Contains(err.Error(), `decode FuzzyFileSearchResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown response field error: %v", err)
	}

	var result FuzzyFileSearchResult
	err = json.Unmarshal([]byte(`{"match_type":"file","path":"/repo/README.md","root":"/repo","score":1}`), &result)
	if err == nil {
		t.Fatal("expected missing fuzzy file_name to fail")
	}
	if !strings.Contains(err.Error(), "decode FuzzyFileSearchResult.file_name: missing required field") {
		t.Fatalf("unexpected missing file_name error: %v", err)
	}

	_, err = json.Marshal(FuzzyFileSearchResult{
		FileName:  "README.md",
		MatchType: FuzzyFileSearchMatchType("symlink"),
		Path:      "/repo/README.md",
		Root:      "/repo",
		Score:     1,
	})
	if err == nil {
		t.Fatal("expected invalid fuzzy match type marshal to fail")
	}
	if !strings.Contains(err.Error(), `invalid FuzzyFileSearchMatchType enum value "symlink"`) {
		t.Fatalf("unexpected invalid match type marshal error: %v", err)
	}

	var startResponse FuzzyFileSearchSessionStartResponse
	err = json.Unmarshal([]byte(`{"extra":true}`), &startResponse)
	if err == nil {
		t.Fatal("expected unknown fuzzy start response field to fail")
	}
	if !strings.Contains(err.Error(), `decode FuzzyFileSearchSessionStartResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown start response error: %v", err)
	}

	var stopParams FuzzyFileSearchSessionStopParams
	err = json.Unmarshal([]byte(`{}`), &stopParams)
	if err == nil {
		t.Fatal("expected missing fuzzy stop sessionId to fail")
	}
	if !strings.Contains(err.Error(), "decode FuzzyFileSearchSessionStopParams.sessionId: missing required field") {
		t.Fatalf("unexpected missing stop sessionId error: %v", err)
	}

	var stopResponse FuzzyFileSearchSessionStopResponse
	err = json.Unmarshal([]byte(`{"extra":true}`), &stopResponse)
	if err == nil {
		t.Fatal("expected unknown fuzzy stop response field to fail")
	}
	if !strings.Contains(err.Error(), `decode FuzzyFileSearchSessionStopResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown stop response error: %v", err)
	}

	var updateResponse FuzzyFileSearchSessionUpdateResponse
	err = json.Unmarshal([]byte(`{"extra":true}`), &updateResponse)
	if err == nil {
		t.Fatal("expected unknown fuzzy update response field to fail")
	}
	if !strings.Contains(err.Error(), `decode FuzzyFileSearchSessionUpdateResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown update response error: %v", err)
	}

	var updated FuzzyFileSearchSessionUpdatedNotification
	err = json.Unmarshal([]byte(`{"query":"main","sessionId":"session-1"}`), &updated)
	if err == nil {
		t.Fatal("expected missing fuzzy updated files to fail")
	}
	if !strings.Contains(err.Error(), "decode FuzzyFileSearchSessionUpdatedNotification.files: missing required field") {
		t.Fatalf("unexpected missing updated files error: %v", err)
	}

	_, err = json.Marshal(FuzzyFileSearchSessionUpdatedNotification{
		Query:     "main",
		SessionID: "session-1",
	})
	if err == nil {
		t.Fatal("expected nil fuzzy updated files marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode FuzzyFileSearchSessionUpdatedNotification.files: nil is not allowed") {
		t.Fatalf("unexpected nil updated files marshal error: %v", err)
	}
}

func TestGeneratedApplyPatchApprovalParamsMarshalFileChanges(t *testing.T) {
	params := ApplyPatchApprovalParams{
		CallID:         "call-1",
		ConversationID: "thread-1",
		FileChanges: map[string]FileChange{
			"add":    NewFileChangeAdd(FileChangeAdd{Content: "new file"}),
			"delete": NewFileChangeDelete(FileChangeDelete{Content: "old file"}),
			"update": NewFileChangeUpdate(FileChangeUpdate{
				MovePath:    Null[string](),
				UnifiedDiff: "@@ -1 +1 @@",
			}),
		},
		GrantRoot: Value("/repo"),
		Reason:    Null[string](),
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"callId":"call-1","conversationId":"thread-1","fileChanges":{"add":{"content":"new file","type":"add"},"delete":{"content":"old file","type":"delete"},"update":{"move_path":null,"type":"update","unified_diff":"@@ -1 +1 @@"}},"grantRoot":"/repo","reason":null}`
	if got := string(raw); got != want {
		t.Fatalf("ApplyPatchApprovalParams JSON = %s, want %s", got, want)
	}
}

func TestGeneratedApplyPatchApprovalParamsUnmarshalFileChanges(t *testing.T) {
	var params ApplyPatchApprovalParams
	if err := json.Unmarshal([]byte(`{
		"callId":"call-1",
		"conversationId":"thread-1",
		"fileChanges":{
			"add":{"content":"new file","type":"add"},
			"update":{"move_path":"/renamed","type":"update","unified_diff":"@@ -1 +1 @@"}
		},
		"grantRoot":null,
		"reason":"write access"
	}`), &params); err != nil {
		t.Fatal(err)
	}
	if params.ConversationID != "thread-1" {
		t.Fatalf("ConversationID = %q, want thread-1", params.ConversationID)
	}
	add, ok := params.FileChanges["add"].AsAdd()
	if !ok || add.Content != "new file" {
		t.Fatalf("decoded add file change = %#v, ok=%t", add, ok)
	}
	update, ok := params.FileChanges["update"].AsUpdate()
	if !ok || update.MovePath == nil || update.MovePath.Value == nil || *update.MovePath.Value != "/renamed" {
		t.Fatalf("decoded update file change = %#v, ok=%t", update, ok)
	}
	if params.GrantRoot == nil || params.GrantRoot.Value != nil {
		t.Fatalf("decoded grantRoot = %#v", params.GrantRoot)
	}
	if params.Reason == nil || params.Reason.Value == nil || *params.Reason.Value != "write access" {
		t.Fatalf("decoded reason = %#v", params.Reason)
	}
}

func TestGeneratedApplyPatchApprovalParamsRejectsMalformedProtocol(t *testing.T) {
	var params ApplyPatchApprovalParams
	err := json.Unmarshal([]byte(`{"callId":"call-1","conversationId":"thread-1"}`), &params)
	if err == nil {
		t.Fatal("expected missing fileChanges to fail")
	}
	if !strings.Contains(err.Error(), "decode ApplyPatchApprovalParams.fileChanges: missing required field") {
		t.Fatalf("unexpected missing fileChanges error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"callId":"call-1","conversationId":"thread-1","fileChanges":{},"extra":true}`), &params)
	if err == nil {
		t.Fatal("expected unknown apply patch field to fail")
	}
	if !strings.Contains(err.Error(), `decode ApplyPatchApprovalParams: unknown field "extra"`) {
		t.Fatalf("unexpected unknown apply patch field error: %v", err)
	}

	var change FileChange
	err = json.Unmarshal([]byte(`{"type":"replace","content":"file"}`), &change)
	if err == nil {
		t.Fatal("expected unknown file change variant to fail")
	}
	if !strings.Contains(err.Error(), `decode FileChange.type: unknown variant "replace"`) {
		t.Fatalf("unexpected unknown file change variant error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"type":"update","unified_diff":"@@","extra":true}`), &change)
	if err == nil {
		t.Fatal("expected unknown update field to fail")
	}
	if !strings.Contains(err.Error(), `decode FileChange.update: unknown field "extra"`) {
		t.Fatalf("unexpected unknown update field error: %v", err)
	}
}

func TestGeneratedExecCommandApprovalParamsMarshalParsedCommands(t *testing.T) {
	params := ExecCommandApprovalParams{
		ApprovalID:     Null[string](),
		CallID:         "call-1",
		Command:        []string{"rg", "needle"},
		ConversationID: "thread-1",
		CWD:            "/repo",
		ParsedCmd: []ParsedCommand{
			NewParsedCommandRead(ParsedCommandRead{
				Cmd:  "cat README.md",
				Name: "README.md",
				Path: "/repo/README.md",
			}),
			NewParsedCommandListFiles(ParsedCommandListFiles{
				Cmd:  "ls",
				Path: Null[string](),
			}),
			NewParsedCommandSearch(ParsedCommandSearch{
				Cmd:   "rg needle",
				Path:  Value("/repo"),
				Query: Null[string](),
			}),
			NewParsedCommandUnknown(ParsedCommandUnknown{
				Cmd: "custom",
			}),
		},
		Reason: Value("needs shell"),
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"approvalId":null,"callId":"call-1","command":["rg","needle"],"conversationId":"thread-1","cwd":"/repo","parsedCmd":[{"cmd":"cat README.md","name":"README.md","path":"/repo/README.md","type":"read"},{"cmd":"ls","path":null,"type":"list_files"},{"cmd":"rg needle","path":"/repo","query":null,"type":"search"},{"cmd":"custom","type":"unknown"}],"reason":"needs shell"}`
	if got := string(raw); got != want {
		t.Fatalf("ExecCommandApprovalParams JSON = %s, want %s", got, want)
	}
}

func TestGeneratedExecCommandApprovalParamsUnmarshalParsedCommands(t *testing.T) {
	var params ExecCommandApprovalParams
	if err := json.Unmarshal([]byte(`{
		"approvalId":"approval-1",
		"callId":"call-1",
		"command":["rg","needle"],
		"conversationId":"thread-1",
		"cwd":"/repo",
		"parsedCmd":[
			{"cmd":"cat README.md","name":"README.md","path":"/repo/README.md","type":"read"},
			{"cmd":"rg needle","path":null,"query":"needle","type":"search"}
		],
		"reason":null
	}`), &params); err != nil {
		t.Fatal(err)
	}
	if params.ApprovalID == nil || params.ApprovalID.Value == nil || *params.ApprovalID.Value != "approval-1" {
		t.Fatalf("decoded approvalId = %#v", params.ApprovalID)
	}
	if params.Reason == nil || params.Reason.Value != nil {
		t.Fatalf("decoded reason = %#v", params.Reason)
	}
	read, ok := params.ParsedCmd[0].AsRead()
	if !ok || read.Path != "/repo/README.md" {
		t.Fatalf("decoded read command = %#v, ok=%t", read, ok)
	}
	search, ok := params.ParsedCmd[1].AsSearch()
	if !ok || search.Path == nil || search.Path.Value != nil || search.Query == nil || search.Query.Value == nil || *search.Query.Value != "needle" {
		t.Fatalf("decoded search command = %#v, ok=%t", search, ok)
	}
}

func TestGeneratedExecCommandApprovalParamsRejectsMalformedProtocol(t *testing.T) {
	var params ExecCommandApprovalParams
	err := json.Unmarshal([]byte(`{"callId":"call-1","command":["rg"],"conversationId":"thread-1","cwd":"/repo"}`), &params)
	if err == nil {
		t.Fatal("expected missing parsedCmd to fail")
	}
	if !strings.Contains(err.Error(), "decode ExecCommandApprovalParams.parsedCmd: missing required field") {
		t.Fatalf("unexpected missing parsedCmd error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"callId":"call-1","command":["rg"],"conversationId":"thread-1","cwd":"/repo","parsedCmd":[],"extra":true}`), &params)
	if err == nil {
		t.Fatal("expected unknown exec approval field to fail")
	}
	if !strings.Contains(err.Error(), `decode ExecCommandApprovalParams: unknown field "extra"`) {
		t.Fatalf("unexpected unknown exec approval field error: %v", err)
	}

	var command ParsedCommand
	err = json.Unmarshal([]byte(`{"cmd":"rg","type":"replace"}`), &command)
	if err == nil {
		t.Fatal("expected unknown parsed command variant to fail")
	}
	if !strings.Contains(err.Error(), `decode ParsedCommand.type: unknown variant "replace"`) {
		t.Fatalf("unexpected unknown parsed command variant error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"cmd":"rg","type":"search","extra":true}`), &command)
	if err == nil {
		t.Fatal("expected unknown search field to fail")
	}
	if !strings.Contains(err.Error(), `decode ParsedCommand.search: unknown field "extra"`) {
		t.Fatalf("unexpected unknown search field error: %v", err)
	}
}

func TestGeneratedMcpServerToolCallParamsMarshalJSONValue(t *testing.T) {
	arguments := JSONString("payload")
	raw, err := json.Marshal(McpServerToolCallParams{
		Arguments: &arguments,
		Server:    "server-1",
		ThreadID:  "thread-1",
		Tool:      "tool-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"arguments":"payload","server":"server-1","threadId":"thread-1","tool":"tool-1"}`
	if got := string(raw); got != want {
		t.Fatalf("McpServerToolCallParams JSON = %s, want %s", got, want)
	}
}

func TestGeneratedMcpPayloadsProtocolMarshalAndUnmarshal(t *testing.T) {
	toolMeta := JSONObject(map[string]JSONValue{"trace": JSONString("trace-1")})
	toolArguments := JSONObject(map[string]JSONValue{"query": JSONString("docs")})
	responseMeta := JSONObject(map[string]JSONValue{"server": JSONString("server-1")})
	structuredContent := JSONObject(map[string]JSONValue{"answer": JSONString("done")})
	resourceMeta := JSONObject(map[string]JSONValue{"kind": JSONString("resource")})
	statusToolMeta := JSONObject(map[string]JSONValue{"kind": JSONString("tool")})
	annotations := JSONObject(map[string]JSONValue{"audience": JSONString("sdk")})
	icon := JSONObject(map[string]JSONValue{"src": JSONString("icon.png")})
	inputSchema := JSONObject(map[string]JSONValue{"type": JSONString("object")})
	outputSchema := JSONObject(map[string]JSONValue{"type": JSONString("object")})

	for _, tc := range []struct {
		name   string
		value  any
		target any
		want   string
	}{
		{name: "elicitation request params", value: McpServerElicitationRequestParams{ServerName: "server-1", ThreadID: "thread-1", TurnID: Null[string]()}, target: &McpServerElicitationRequestParams{}, want: `{"serverName":"server-1","threadId":"thread-1","turnId":null}`},
		{name: "resource read params", value: McpResourceReadParams{Server: "server-1", ThreadID: Value("thread-1"), URI: "file://README.md"}, target: &McpResourceReadParams{}, want: `{"server":"server-1","threadId":"thread-1","uri":"file://README.md"}`},
		{name: "oauth login params", value: McpServerOauthLoginParams{Name: "server-1", Scopes: Value([]string{"repo", "user"}), TimeoutSecs: Value(int64(30))}, target: &McpServerOauthLoginParams{}, want: `{"name":"server-1","scopes":["repo","user"],"timeoutSecs":30}`},
		{name: "oauth login response", value: McpServerOauthLoginResponse{AuthorizationURL: "https://example.test/oauth"}, target: &McpServerOauthLoginResponse{}, want: `{"authorizationUrl":"https://example.test/oauth"}`},
		{name: "refresh response", value: McpServerRefreshResponse{}, target: &McpServerRefreshResponse{}, want: `{}`},
		{name: "resource read response", value: McpResourceReadResponse{Contents: []ResourceContent{
			NewResourceContentText(ResourceContentText{Meta: &resourceMeta, MimeType: Value("text/plain"), Text: "hello", URI: "file://README.md"}),
			NewResourceContentBlob(ResourceContentBlob{Blob: "Ymlu", URI: "file://blob"}),
		}}, target: &McpResourceReadResponse{}, want: `{"contents":[{"_meta":{"kind":"resource"},"mimeType":"text/plain","text":"hello","uri":"file://README.md"},{"blob":"Ymlu","uri":"file://blob"}]}`},
		{name: "server status list response", value: ListMcpServerStatusResponse{
			Data: []McpServerStatus{{
				AuthStatus: McpAuthStatusOAuth,
				Name:       "server-1",
				ResourceTemplates: []ResourceTemplate{{
					Annotations: &annotations,
					Name:        "docs",
					URITemplate: "file://{path}",
				}},
				Resources: []Resource{{
					Meta:        &resourceMeta,
					Annotations: &annotations,
					Icons:       Value([]JSONValue{icon}),
					MimeType:    Value("text/plain"),
					Name:        "README",
					Size:        Value(int64(12)),
					URI:         "file://README.md",
				}},
				Tools: map[string]Tool{
					"search": {
						Meta:         &statusToolMeta,
						InputSchema:  inputSchema,
						Name:         "search",
						OutputSchema: &outputSchema,
					},
				},
			}},
			NextCursor: Value("cursor-2"),
		}, target: &ListMcpServerStatusResponse{}, want: `{"data":[{"authStatus":"oAuth","name":"server-1","resourceTemplates":[{"annotations":{"audience":"sdk"},"name":"docs","uriTemplate":"file://{path}"}],"resources":[{"_meta":{"kind":"resource"},"annotations":{"audience":"sdk"},"icons":[{"src":"icon.png"}],"mimeType":"text/plain","name":"README","size":12,"uri":"file://README.md"}],"tools":{"search":{"_meta":{"kind":"tool"},"inputSchema":{"type":"object"},"name":"search","outputSchema":{"type":"object"}}}}],"nextCursor":"cursor-2"}`},
		{name: "tool call params", value: McpServerToolCallParams{Meta: &toolMeta, Arguments: &toolArguments, Server: "server-1", ThreadID: "thread-1", Tool: "search"}, target: &McpServerToolCallParams{}, want: `{"_meta":{"trace":"trace-1"},"arguments":{"query":"docs"},"server":"server-1","threadId":"thread-1","tool":"search"}`},
		{name: "tool call response", value: McpServerToolCallResponse{Meta: &responseMeta, Content: []JSONValue{JSONString("ok")}, IsError: Null[bool](), StructuredContent: &structuredContent}, target: &McpServerToolCallResponse{}, want: `{"_meta":{"server":"server-1"},"content":["ok"],"isError":null,"structuredContent":{"answer":"done"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(raw); got != tc.want {
				t.Fatalf("%s JSON = %s, want %s", tc.name, got, tc.want)
			}
			if err := json.Unmarshal(raw, tc.target); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGeneratedMcpPayloadsRejectMalformedProtocol(t *testing.T) {
	var refresh McpServerRefreshResponse
	err := json.Unmarshal([]byte(`{"extra":true}`), &refresh)
	if err == nil {
		t.Fatal("expected unknown refresh response field to fail")
	}
	if !strings.Contains(err.Error(), `decode McpServerRefreshResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown refresh response error: %v", err)
	}

	var resource McpResourceReadParams
	err = json.Unmarshal([]byte(`{"server":"server-1"}`), &resource)
	if err == nil {
		t.Fatal("expected missing resource uri to fail")
	}
	if !strings.Contains(err.Error(), "decode McpResourceReadParams.uri: missing required field") {
		t.Fatalf("unexpected missing resource uri error: %v", err)
	}

	var resourceResponse McpResourceReadResponse
	err = json.Unmarshal([]byte(`{}`), &resourceResponse)
	if err == nil {
		t.Fatal("expected missing resource contents to fail")
	}
	if !strings.Contains(err.Error(), "decode McpResourceReadResponse.contents: missing required field") {
		t.Fatalf("unexpected missing resource contents error: %v", err)
	}
	err = json.Unmarshal([]byte(`{"contents":[{"uri":"file://README.md"}]}`), &resourceResponse)
	if err == nil {
		t.Fatal("expected missing resource content discriminator to fail")
	}
	if !strings.Contains(err.Error(), `decode ResourceContent: expected one of "blob" or "text"`) {
		t.Fatalf("unexpected missing resource content discriminator error: %v", err)
	}
	err = json.Unmarshal([]byte(`{"contents":[{"blob":"Ymlu","text":"hello","uri":"file://README.md"}]}`), &resourceResponse)
	if err == nil {
		t.Fatal("expected ambiguous resource content discriminator to fail")
	}
	if !strings.Contains(err.Error(), "decode ResourceContent: ambiguous object matches multiple variants") {
		t.Fatalf("unexpected ambiguous resource content error: %v", err)
	}
	_, err = json.Marshal(McpResourceReadResponse{})
	if err == nil {
		t.Fatal("expected nil resource contents to fail")
	}
	if !strings.Contains(err.Error(), "encode McpResourceReadResponse.contents: nil is not allowed") {
		t.Fatalf("unexpected nil resource contents error: %v", err)
	}
	_, err = json.Marshal(ResourceContent{})
	if err == nil {
		t.Fatal("expected empty resource content union to fail")
	}
	if !strings.Contains(err.Error(), "invalid ResourceContent union value: no variant is set") {
		t.Fatalf("unexpected empty resource content union error: %v", err)
	}

	var oauthResponse McpServerOauthLoginResponse
	err = json.Unmarshal([]byte(`{"authorizationUrl":null}`), &oauthResponse)
	if err == nil {
		t.Fatal("expected null authorization URL to fail")
	}
	if !strings.Contains(err.Error(), "decode McpServerOauthLoginResponse.authorizationUrl: null is not allowed") {
		t.Fatalf("unexpected null authorization URL error: %v", err)
	}

	var toolResponse McpServerToolCallResponse
	err = json.Unmarshal([]byte(`{"isError":false}`), &toolResponse)
	if err == nil {
		t.Fatal("expected missing tool call response content to fail")
	}
	if !strings.Contains(err.Error(), "decode McpServerToolCallResponse.content: missing required field") {
		t.Fatalf("unexpected missing content error: %v", err)
	}

	var statusResponse ListMcpServerStatusResponse
	err = json.Unmarshal([]byte(`{}`), &statusResponse)
	if err == nil {
		t.Fatal("expected missing status list data to fail")
	}
	if !strings.Contains(err.Error(), "decode ListMcpServerStatusResponse.data: missing required field") {
		t.Fatalf("unexpected missing status list data error: %v", err)
	}
	err = json.Unmarshal([]byte(`{"data":[{"authStatus":"bogus","name":"server","resourceTemplates":[],"resources":[],"tools":{}}]}`), &statusResponse)
	if err == nil {
		t.Fatal("expected invalid MCP auth status to fail")
	}
	if !strings.Contains(err.Error(), `invalid McpAuthStatus enum value "bogus"`) {
		t.Fatalf("unexpected invalid auth status error: %v", err)
	}
	var tool Tool
	err = json.Unmarshal([]byte(`{"name":"search"}`), &tool)
	if err == nil {
		t.Fatal("expected missing tool inputSchema to fail")
	}
	if !strings.Contains(err.Error(), "decode Tool.inputSchema: missing required field") {
		t.Fatalf("unexpected missing tool inputSchema error: %v", err)
	}
	_, err = json.Marshal(ListMcpServerStatusResponse{})
	if err == nil {
		t.Fatal("expected nil status list data to fail")
	}
	if !strings.Contains(err.Error(), "encode ListMcpServerStatusResponse.data: nil is not allowed") {
		t.Fatalf("unexpected nil status list data error: %v", err)
	}
	_, err = json.Marshal(McpServerStatus{AuthStatus: McpAuthStatusOAuth, Name: "server"})
	if err == nil {
		t.Fatal("expected nil MCP server status collections to fail")
	}
	if !strings.Contains(err.Error(), "encode McpServerStatus.resourceTemplates: nil is not allowed") {
		t.Fatalf("unexpected nil MCP server status collections error: %v", err)
	}

	_, err = json.Marshal(McpServerToolCallResponse{})
	if err == nil {
		t.Fatal("expected nil tool call response content to fail")
	}
	if !strings.Contains(err.Error(), "encode McpServerToolCallResponse.content: nil is not allowed") {
		t.Fatalf("unexpected nil content error: %v", err)
	}
}

func TestGeneratedPluginPayloadsProtocolMarshalAndUnmarshal(t *testing.T) {
	assertRoundTrip := func(t *testing.T, value any, target any) {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatal(err)
		}
	}

	summary := PluginSummary{
		AuthPolicy:    PluginAuthPolicyONUSE,
		Enabled:       true,
		ID:            "plugin-1",
		InstallPolicy: PluginInstallPolicyAVAILABLE,
		Installed:     true,
		Name:          "plugin-one",
		Source:        NewPluginSourceRemote(),
	}
	detail := PluginDetail{
		AppTemplates:    []AppTemplateSummary{},
		Apps:            []AppSummary{},
		Hooks:           []PluginHookSummary{},
		MarketplaceName: "market",
		MCPServers:      []string{},
		Skills:          []SkillSummary{},
		Summary:         summary,
	}

	for _, tc := range []struct {
		name   string
		value  any
		target any
	}{
		{name: "install response", value: PluginInstallResponse{AppsNeedingAuth: []AppSummary{}, AuthPolicy: PluginAuthPolicyONINSTALL}, target: &PluginInstallResponse{}},
		{name: "list response", value: PluginListResponse{Marketplaces: []PluginMarketplaceEntry{{Name: "market", Plugins: []PluginSummary{}}}}, target: &PluginListResponse{}},
		{name: "read response", value: PluginReadResponse{Plugin: detail}, target: &PluginReadResponse{}},
		{name: "share list response", value: PluginShareListResponse{Data: []PluginShareListItem{}}, target: &PluginShareListResponse{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertRoundTrip(t, tc.value, tc.target)
		})
	}

	for _, tc := range []struct {
		name   string
		value  any
		target any
		want   string
	}{
		{name: "share delete params", value: PluginShareDeleteParams{RemotePluginID: "remote-1"}, target: &PluginShareDeleteParams{}, want: `{"remotePluginId":"remote-1"}`},
		{name: "share delete response", value: PluginShareDeleteResponse{}, target: &PluginShareDeleteResponse{}, want: `{}`},
		{name: "share list params", value: PluginShareListParams{}, target: &PluginShareListParams{}, want: `{}`},
		{name: "share save params", value: PluginShareSaveParams{Discoverability: Value(PluginShareDiscoverabilityPRIVATE), PluginPath: "/plugins/plugin-one", RemotePluginID: Null[string](), ShareTargets: Value([]PluginShareTarget{{PrincipalID: "group-1", PrincipalType: PluginSharePrincipalTypeGroup, Role: PluginShareTargetRoleReader}})}, target: &PluginShareSaveParams{}, want: `{"discoverability":"PRIVATE","pluginPath":"/plugins/plugin-one","remotePluginId":null,"shareTargets":[{"principalId":"group-1","principalType":"group","role":"reader"}]}`},
		{name: "share save response", value: PluginShareSaveResponse{RemotePluginID: "remote-1", ShareURL: "https://example.test/plugin"}, target: &PluginShareSaveResponse{}, want: `{"remotePluginId":"remote-1","shareUrl":"https://example.test/plugin"}`},
		{name: "share update targets params", value: PluginShareUpdateTargetsParams{Discoverability: PluginShareUpdateDiscoverabilityUNLISTED, RemotePluginID: "remote-1", ShareTargets: []PluginShareTarget{{PrincipalID: "user-1", PrincipalType: PluginSharePrincipalTypeUser, Role: PluginShareTargetRoleEditor}}}, target: &PluginShareUpdateTargetsParams{}, want: `{"discoverability":"UNLISTED","remotePluginId":"remote-1","shareTargets":[{"principalId":"user-1","principalType":"user","role":"editor"}]}`},
		{name: "share update targets response", value: PluginShareUpdateTargetsResponse{Discoverability: PluginShareDiscoverabilityUNLISTED, Principals: []PluginSharePrincipal{{Name: "User One", PrincipalID: "user-1", PrincipalType: PluginSharePrincipalTypeUser, Role: PluginSharePrincipalRoleOwner}}}, target: &PluginShareUpdateTargetsResponse{}, want: `{"discoverability":"UNLISTED","principals":[{"name":"User One","principalId":"user-1","principalType":"user","role":"owner"}]}`},
		{name: "skill read params", value: PluginSkillReadParams{RemoteMarketplaceName: "market", RemotePluginID: "remote-1", SkillName: "review"}, target: &PluginSkillReadParams{}, want: `{"remoteMarketplaceName":"market","remotePluginId":"remote-1","skillName":"review"}`},
		{name: "skill read response", value: PluginSkillReadResponse{Contents: Null[string]()}, target: &PluginSkillReadResponse{}, want: `{"contents":null}`},
		{name: "uninstall params", value: PluginUninstallParams{PluginID: "plugin-1"}, target: &PluginUninstallParams{}, want: `{"pluginId":"plugin-1"}`},
		{name: "uninstall response", value: PluginUninstallResponse{}, target: &PluginUninstallResponse{}, want: `{}`},
		{name: "source local union", value: NewPluginSourceLocal(PluginSourceLocal{Path: "/plugins/local"}), target: &PluginSource{}, want: `{"path":"/plugins/local","type":"local"}`},
		{name: "source git union", value: NewPluginSourceGit(PluginSourceGit{Path: Null[string](), RefName: Value("main"), URL: "https://example.test/repo.git"}), target: &PluginSource{}, want: `{"path":null,"refName":"main","type":"git","url":"https://example.test/repo.git"}`},
		{name: "source remote union", value: NewPluginSourceRemote(), target: &PluginSource{}, want: `{"type":"remote"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(raw); got != tc.want {
				t.Fatalf("%s JSON = %s, want %s", tc.name, got, tc.want)
			}
			if err := json.Unmarshal(raw, tc.target); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGeneratedPluginPayloadsRejectMalformedProtocol(t *testing.T) {
	var installResponse PluginInstallResponse
	err := json.Unmarshal([]byte(`{"authPolicy":"ON_INSTALL"}`), &installResponse)
	if err == nil {
		t.Fatal("expected missing appsNeedingAuth to fail")
	}
	if !strings.Contains(err.Error(), "decode PluginInstallResponse.appsNeedingAuth: missing required field") {
		t.Fatalf("unexpected missing appsNeedingAuth error: %v", err)
	}

	var listResponse PluginListResponse
	err = json.Unmarshal([]byte(`{"featuredPluginIds":[]}`), &listResponse)
	if err == nil {
		t.Fatal("expected missing marketplaces to fail")
	}
	if !strings.Contains(err.Error(), "decode PluginListResponse.marketplaces: missing required field") {
		t.Fatalf("unexpected missing marketplaces error: %v", err)
	}

	var summary PluginSummary
	err = json.Unmarshal([]byte(`{"authPolicy":"ON_USE","availability":"ENABLED","enabled":true,"id":"plugin-1","installPolicy":"AVAILABLE","installed":true,"name":"plugin-one","source":{"type":"remote"}}`), &summary)
	if err == nil {
		t.Fatal("expected invalid plugin availability to fail")
	}
	if !strings.Contains(err.Error(), `decode PluginSummary.availability: invalid PluginAvailability enum value "ENABLED"`) {
		t.Fatalf("unexpected invalid plugin availability error: %v", err)
	}

	var source PluginSource
	err = json.Unmarshal([]byte(`{"type":"archive","url":"https://example.test"}`), &source)
	if err == nil {
		t.Fatal("expected unknown plugin source to fail")
	}
	if !strings.Contains(err.Error(), `decode PluginSource.type: unknown variant "archive"`) {
		t.Fatalf("unexpected unknown plugin source error: %v", err)
	}

	var shareList PluginShareListParams
	err = json.Unmarshal([]byte(`{"extra":true}`), &shareList)
	if err == nil {
		t.Fatal("expected unknown share list field to fail")
	}
	if !strings.Contains(err.Error(), `decode PluginShareListParams: unknown field "extra"`) {
		t.Fatalf("unexpected unknown share list field error: %v", err)
	}

	var deleteParams PluginShareDeleteParams
	err = json.Unmarshal([]byte(`{}`), &deleteParams)
	if err == nil {
		t.Fatal("expected missing remote plugin id to fail")
	}
	if !strings.Contains(err.Error(), "decode PluginShareDeleteParams.remotePluginId: missing required field") {
		t.Fatalf("unexpected missing remote plugin id error: %v", err)
	}

	var saveParams PluginShareSaveParams
	err = json.Unmarshal([]byte(`{"remotePluginId":"remote-1"}`), &saveParams)
	if err == nil {
		t.Fatal("expected missing plugin path to fail")
	}
	if !strings.Contains(err.Error(), "decode PluginShareSaveParams.pluginPath: missing required field") {
		t.Fatalf("unexpected missing plugin path error: %v", err)
	}

	var saveResponse PluginShareSaveResponse
	err = json.Unmarshal([]byte(`{"remotePluginId":"remote-1","shareUrl":null}`), &saveResponse)
	if err == nil {
		t.Fatal("expected null share URL to fail")
	}
	if !strings.Contains(err.Error(), "decode PluginShareSaveResponse.shareUrl: null is not allowed") {
		t.Fatalf("unexpected null share URL error: %v", err)
	}

	_, err = json.Marshal(PluginShareListResponse{})
	if err == nil {
		t.Fatal("expected nil share list data to fail")
	}
	if !strings.Contains(err.Error(), "encode PluginShareListResponse.data: nil is not allowed") {
		t.Fatalf("unexpected nil share list data error: %v", err)
	}

	_, err = json.Marshal(PluginShareUpdateTargetsParams{Discoverability: PluginShareUpdateDiscoverabilityPRIVATE, RemotePluginID: "remote-1"})
	if err == nil {
		t.Fatal("expected nil update targets shareTargets to fail")
	}
	if !strings.Contains(err.Error(), "encode PluginShareUpdateTargetsParams.shareTargets: nil is not allowed") {
		t.Fatalf("unexpected nil update targets shareTargets error: %v", err)
	}

	var skillParams PluginSkillReadParams
	err = json.Unmarshal([]byte(`{"remoteMarketplaceName":"market","remotePluginId":"remote-1"}`), &skillParams)
	if err == nil {
		t.Fatal("expected missing skill name to fail")
	}
	if !strings.Contains(err.Error(), "decode PluginSkillReadParams.skillName: missing required field") {
		t.Fatalf("unexpected missing skill name error: %v", err)
	}

	var uninstallResponse PluginUninstallResponse
	err = json.Unmarshal([]byte(`{"extra":true}`), &uninstallResponse)
	if err == nil {
		t.Fatal("expected unknown uninstall response field to fail")
	}
	if !strings.Contains(err.Error(), `decode PluginUninstallResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown uninstall response error: %v", err)
	}
}

func TestGeneratedStructUnmarshalRejectsUnknownField(t *testing.T) {
	var params ThreadArchiveParams
	err := json.Unmarshal([]byte(`{"threadId":"thread-1","extra":true}`), &params)
	if err == nil {
		t.Fatal("expected unknown generated field to fail")
	}
	if !strings.Contains(err.Error(), `decode ThreadArchiveParams: unknown field "extra"`) {
		t.Fatalf("unexpected unknown field error: %v", err)
	}
}

func TestGeneratedStructUnmarshalRejectsMissingRequiredField(t *testing.T) {
	var params ThreadArchiveParams
	err := json.Unmarshal([]byte(`{}`), &params)
	if err == nil {
		t.Fatal("expected missing generated field to fail")
	}
	if !strings.Contains(err.Error(), "decode ThreadArchiveParams.threadId: missing required field") {
		t.Fatalf("unexpected missing field error: %v", err)
	}
}

func TestGeneratedStructUnmarshalRejectsNullForNonNullableField(t *testing.T) {
	var params ThreadArchiveParams
	err := json.Unmarshal([]byte(`{"threadId":null}`), &params)
	if err == nil {
		t.Fatal("expected non-nullable generated field null to fail")
	}
	if !strings.Contains(err.Error(), "decode ThreadArchiveParams.threadId: null is not allowed") {
		t.Fatalf("unexpected null field error: %v", err)
	}
}

func TestGeneratedStructUnmarshalJSONValueField(t *testing.T) {
	var params McpServerToolCallParams
	err := json.Unmarshal([]byte(`{
		"arguments":{"nested":[null,"text"]},
		"server":"server-1",
		"threadId":"thread-1",
		"tool":"tool-1"
	}`), &params)
	if err != nil {
		t.Fatal(err)
	}
	if params.Arguments == nil {
		t.Fatal("arguments JSONValue was not decoded")
	}
	object, ok := params.Arguments.AsObject()
	if !ok {
		t.Fatalf("arguments kind = %s, want object", params.Arguments.Kind())
	}
	nested, ok := object["nested"].AsArray()
	if !ok || len(nested) != 2 || nested[0].Kind() != JSONKindNull {
		t.Fatalf("decoded arguments nested value = %#v", object["nested"])
	}
}

func TestGeneratedStructUnmarshalPreservesOptionalJSONValueNull(t *testing.T) {
	var params McpServerToolCallParams
	err := json.Unmarshal([]byte(`{
		"arguments":null,
		"server":"server-1",
		"threadId":"thread-1",
		"tool":"tool-1"
	}`), &params)
	if err != nil {
		t.Fatal(err)
	}
	if params.Arguments == nil {
		t.Fatal("arguments explicit JSON null was decoded as absent")
	}
	if params.Arguments.Kind() != JSONKindNull {
		t.Fatalf("arguments kind = %s, want %s", params.Arguments.Kind(), JSONKindNull)
	}
}

func TestGeneratedDynamicToolCallParamsPreservesJSONValueAndNullableNamespace(t *testing.T) {
	raw, err := json.Marshal(DynamicToolCallParams{
		Arguments: JSONString("payload"),
		CallID:    "call-1",
		ThreadID:  "thread-1",
		Tool:      "tool-1",
		TurnID:    "turn-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"arguments":"payload","callId":"call-1","threadId":"thread-1","tool":"tool-1","turnId":"turn-1"}`
	if got := string(raw); got != want {
		t.Fatalf("DynamicToolCallParams omitted namespace JSON = %s, want %s", got, want)
	}

	nullNamespace, err := json.Marshal(DynamicToolCallParams{
		Arguments: JSONString("payload"),
		CallID:    "call-1",
		Namespace: Null[string](),
		ThreadID:  "thread-1",
		Tool:      "tool-1",
		TurnID:    "turn-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	want = `{"arguments":"payload","callId":"call-1","namespace":null,"threadId":"thread-1","tool":"tool-1","turnId":"turn-1"}`
	if got := string(nullNamespace); got != want {
		t.Fatalf("DynamicToolCallParams null namespace JSON = %s, want %s", got, want)
	}

	var decoded DynamicToolCallParams
	if err := json.Unmarshal([]byte(`{"arguments":{"nested":[true]},"callId":"call-1","namespace":"tools","threadId":"thread-1","tool":"tool-1","turnId":"turn-1"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	object, ok := decoded.Arguments.AsObject()
	if !ok {
		t.Fatalf("decoded arguments kind = %s, want object", decoded.Arguments.Kind())
	}
	nested, ok := object["nested"].AsArray()
	if !ok || len(nested) != 1 || nested[0].Kind() != JSONKindBool {
		t.Fatalf("decoded dynamic arguments nested value = %#v", object["nested"])
	}
	if decoded.Namespace == nil || decoded.Namespace.Value == nil || *decoded.Namespace.Value != "tools" {
		t.Fatalf("decoded namespace = %#v", decoded.Namespace)
	}
}

func TestGeneratedDynamicToolCallParamsRejectsMalformedProtocol(t *testing.T) {
	var params DynamicToolCallParams
	err := json.Unmarshal([]byte(`{"callId":"call-1","threadId":"thread-1","tool":"tool-1","turnId":"turn-1"}`), &params)
	if err == nil {
		t.Fatal("expected missing arguments to fail")
	}
	if !strings.Contains(err.Error(), "decode DynamicToolCallParams.arguments: missing required field") {
		t.Fatalf("unexpected missing arguments error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"arguments":{},"callId":"call-1","threadId":"thread-1","tool":"tool-1","turnId":"turn-1","extra":true}`), &params)
	if err == nil {
		t.Fatal("expected unknown dynamic tool field to fail")
	}
	if !strings.Contains(err.Error(), `decode DynamicToolCallParams: unknown field "extra"`) {
		t.Fatalf("unexpected unknown dynamic tool field error: %v", err)
	}
}

func TestGeneratedDynamicToolCallResponseMarshalContentItems(t *testing.T) {
	response := DynamicToolCallResponse{
		ContentItems: []DynamicToolCallOutputContentItem{
			NewDynamicToolCallOutputContentItemInputText(DynamicToolCallOutputContentItemInputText{
				Text: "hello",
			}),
			NewDynamicToolCallOutputContentItemInputImage(DynamicToolCallOutputContentItemInputImage{
				ImageURL: "https://example.test/image.png",
			}),
		},
		Success: true,
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"contentItems":[{"text":"hello","type":"inputText"},{"imageUrl":"https://example.test/image.png","type":"inputImage"}],"success":true}`
	if got := string(raw); got != want {
		t.Fatalf("DynamicToolCallResponse JSON = %s, want %s", got, want)
	}
}

func TestGeneratedDynamicToolCallResponseUnmarshalContentItems(t *testing.T) {
	var response DynamicToolCallResponse
	if err := json.Unmarshal([]byte(`{
		"contentItems":[
			{"text":"hello","type":"inputText"},
			{"imageUrl":"https://example.test/image.png","type":"inputImage"}
		],
		"success":false
	}`), &response); err != nil {
		t.Fatal(err)
	}
	if response.Success {
		t.Fatal("success = true, want false")
	}
	text, ok := response.ContentItems[0].AsInputText()
	if !ok || text.Text != "hello" {
		t.Fatalf("decoded inputText item = %#v, ok=%t", text, ok)
	}
	image, ok := response.ContentItems[1].AsInputImage()
	if !ok || image.ImageURL != "https://example.test/image.png" {
		t.Fatalf("decoded inputImage item = %#v, ok=%t", image, ok)
	}
}

func TestGeneratedDynamicToolCallResponseRejectsMalformedProtocol(t *testing.T) {
	var response DynamicToolCallResponse
	err := json.Unmarshal([]byte(`{"success":true}`), &response)
	if err == nil {
		t.Fatal("expected missing contentItems to fail")
	}
	if !strings.Contains(err.Error(), "decode DynamicToolCallResponse.contentItems: missing required field") {
		t.Fatalf("unexpected missing contentItems error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"contentItems":[]}`), &response)
	if err == nil {
		t.Fatal("expected missing success to fail")
	}
	if !strings.Contains(err.Error(), "decode DynamicToolCallResponse.success: missing required field") {
		t.Fatalf("unexpected missing success error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"contentItems":[],"success":null}`), &response)
	if err == nil {
		t.Fatal("expected null success to fail")
	}
	if !strings.Contains(err.Error(), "decode DynamicToolCallResponse.success: null is not allowed") {
		t.Fatalf("unexpected null success error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"contentItems":[],"success":true,"extra":true}`), &response)
	if err == nil {
		t.Fatal("expected unknown response field to fail")
	}
	if !strings.Contains(err.Error(), `decode DynamicToolCallResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown response field error: %v", err)
	}

	var item DynamicToolCallOutputContentItem
	err = json.Unmarshal([]byte(`{"type":"inputAudio","text":"hello"}`), &item)
	if err == nil {
		t.Fatal("expected unknown content item variant to fail")
	}
	if !strings.Contains(err.Error(), `decode DynamicToolCallOutputContentItem.type: unknown variant "inputAudio"`) {
		t.Fatalf("unexpected unknown content item variant error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"type":"inputText"}`), &item)
	if err == nil {
		t.Fatal("expected missing inputText payload to fail")
	}
	if !strings.Contains(err.Error(), "decode DynamicToolCallOutputContentItem.text: missing required field") {
		t.Fatalf("unexpected missing inputText payload error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"type":"inputImage","imageUrl":"https://example.test/image.png","extra":true}`), &item)
	if err == nil {
		t.Fatal("expected unknown inputImage field to fail")
	}
	if !strings.Contains(err.Error(), `decode DynamicToolCallOutputContentItem.inputImage: unknown field "extra"`) {
		t.Fatalf("unexpected unknown inputImage field error: %v", err)
	}

	_, err = json.Marshal(DynamicToolCallResponse{
		Success: true,
	})
	if err == nil {
		t.Fatal("expected nil contentItems marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode DynamicToolCallResponse.contentItems: nil is not allowed") {
		t.Fatalf("unexpected nil contentItems marshal error: %v", err)
	}
}

func TestGeneratedFileChangeRequestApprovalParamsPreservesNullableStrings(t *testing.T) {
	raw, err := json.Marshal(FileChangeRequestApprovalParams{
		ItemID:      "item-1",
		StartedAtMS: 123,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"itemId":"item-1","startedAtMs":123,"threadId":"thread-1","turnId":"turn-1"}`
	if got := string(raw); got != want {
		t.Fatalf("FileChangeRequestApprovalParams omitted nullable JSON = %s, want %s", got, want)
	}

	withNullable, err := json.Marshal(FileChangeRequestApprovalParams{
		GrantRoot:   Null[string](),
		ItemID:      "item-1",
		Reason:      Value("write access"),
		StartedAtMS: 123,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	want = `{"grantRoot":null,"itemId":"item-1","reason":"write access","startedAtMs":123,"threadId":"thread-1","turnId":"turn-1"}`
	if got := string(withNullable); got != want {
		t.Fatalf("FileChangeRequestApprovalParams nullable JSON = %s, want %s", got, want)
	}

	var decoded FileChangeRequestApprovalParams
	if err := json.Unmarshal([]byte(`{"grantRoot":"/repo","itemId":"item-1","reason":null,"startedAtMs":123,"threadId":"thread-1","turnId":"turn-1"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.GrantRoot == nil || decoded.GrantRoot.Value == nil || *decoded.GrantRoot.Value != "/repo" {
		t.Fatalf("decoded grantRoot = %#v", decoded.GrantRoot)
	}
	if decoded.Reason == nil || decoded.Reason.Value != nil {
		t.Fatalf("decoded reason = %#v", decoded.Reason)
	}
}

func TestGeneratedFileChangeRequestApprovalParamsRejectsMalformedProtocol(t *testing.T) {
	var params FileChangeRequestApprovalParams
	err := json.Unmarshal([]byte(`{"itemId":"item-1","threadId":"thread-1","turnId":"turn-1"}`), &params)
	if err == nil {
		t.Fatal("expected missing startedAtMs to fail")
	}
	if !strings.Contains(err.Error(), "decode FileChangeRequestApprovalParams.startedAtMs: missing required field") {
		t.Fatalf("unexpected missing startedAtMs error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"itemId":"item-1","startedAtMs":123,"threadId":"thread-1","turnId":"turn-1","extra":true}`), &params)
	if err == nil {
		t.Fatal("expected unknown file change approval field to fail")
	}
	if !strings.Contains(err.Error(), `decode FileChangeRequestApprovalParams: unknown field "extra"`) {
		t.Fatalf("unexpected unknown file change approval field error: %v", err)
	}
}

func TestGeneratedFileChangeRequestApprovalResponseMarshalDecision(t *testing.T) {
	raw, err := json.Marshal(FileChangeRequestApprovalResponse{
		Decision: FileChangeApprovalDecisionAcceptForSession,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"decision":"acceptForSession"}`; got != want {
		t.Fatalf("FileChangeRequestApprovalResponse JSON = %s, want %s", got, want)
	}
}

func TestGeneratedFileChangeRequestApprovalResponseUnmarshalDecision(t *testing.T) {
	var response FileChangeRequestApprovalResponse
	if err := json.Unmarshal([]byte(`{"decision":"decline"}`), &response); err != nil {
		t.Fatal(err)
	}
	if response.Decision != FileChangeApprovalDecisionDecline {
		t.Fatalf("decision = %s, want %s", response.Decision, FileChangeApprovalDecisionDecline)
	}
}

func TestGeneratedFileChangeRequestApprovalResponseRejectsMalformedProtocol(t *testing.T) {
	var response FileChangeRequestApprovalResponse
	err := json.Unmarshal([]byte(`{}`), &response)
	if err == nil {
		t.Fatal("expected missing decision to fail")
	}
	if !strings.Contains(err.Error(), "decode FileChangeRequestApprovalResponse.decision: missing required field") {
		t.Fatalf("unexpected missing decision error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"decision":null}`), &response)
	if err == nil {
		t.Fatal("expected null decision to fail")
	}
	if !strings.Contains(err.Error(), "decode FileChangeRequestApprovalResponse.decision: null is not allowed") {
		t.Fatalf("unexpected null decision error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"decision":"bogus"}`), &response)
	if err == nil {
		t.Fatal("expected unknown decision to fail")
	}
	if !strings.Contains(err.Error(), `invalid FileChangeApprovalDecision enum value "bogus"`) {
		t.Fatalf("unexpected unknown decision error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"decision":"accept","extra":true}`), &response)
	if err == nil {
		t.Fatal("expected unknown response field to fail")
	}
	if !strings.Contains(err.Error(), `decode FileChangeRequestApprovalResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown response field error: %v", err)
	}

	_, err = json.Marshal(FileChangeRequestApprovalResponse{
		Decision: FileChangeApprovalDecision("bogus"),
	})
	if err == nil {
		t.Fatal("expected invalid decision marshal to fail")
	}
	if !strings.Contains(err.Error(), `invalid FileChangeApprovalDecision enum value "bogus"`) {
		t.Fatalf("unexpected invalid decision marshal error: %v", err)
	}
}

func TestGeneratedApprovalResponsesMarshalReviewDecision(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{
			name: "apply patch approved",
			value: ApplyPatchApprovalResponse{
				Decision: NewReviewDecisionApproved(),
			},
			want: `{"decision":"approved"}`,
		},
		{
			name: "exec command denied",
			value: ExecCommandApprovalResponse{
				Decision: NewReviewDecisionDenied(),
			},
			want: `{"decision":"denied"}`,
		},
		{
			name: "apply patch exec policy amendment",
			value: ApplyPatchApprovalResponse{
				Decision: NewReviewDecisionApprovedExecpolicyAmendment(ReviewDecisionApprovedExecpolicyAmendment{
					ProposedExecpolicyAmendment: []string{"allow-all"},
				}),
			},
			want: `{"decision":{"approved_execpolicy_amendment":{"proposed_execpolicy_amendment":["allow-all"]}}}`,
		},
		{
			name: "exec command network policy amendment",
			value: ExecCommandApprovalResponse{
				Decision: NewReviewDecisionNetworkPolicyAmendment(ReviewDecisionNetworkPolicyAmendment{
					NetworkPolicyAmendment: NetworkPolicyAmendment{
						Action: NetworkPolicyRuleActionAllow,
						Host:   "example.test",
					},
				}),
			},
			want: `{"decision":{"network_policy_amendment":{"network_policy_amendment":{"action":"allow","host":"example.test"}}}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(raw); got != tc.want {
				t.Fatalf("approval response JSON = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestGeneratedApprovalResponsesUnmarshalReviewDecision(t *testing.T) {
	var applyPatch ApplyPatchApprovalResponse
	if err := json.Unmarshal([]byte(`{"decision":"approved_for_session"}`), &applyPatch); err != nil {
		t.Fatal(err)
	}
	if applyPatch.Decision.Kind() != ReviewDecisionKindApprovedForSession {
		t.Fatalf("apply patch decision kind = %s, want %s", applyPatch.Decision.Kind(), ReviewDecisionKindApprovedForSession)
	}
	if _, ok := applyPatch.Decision.AsApprovedForSession(); !ok {
		t.Fatal("approved_for_session accessor returned ok=false")
	}

	var execCommand ExecCommandApprovalResponse
	if err := json.Unmarshal([]byte(`{"decision":{"network_policy_amendment":{"network_policy_amendment":{"action":"deny","host":"example.test"}}}}`), &execCommand); err != nil {
		t.Fatal(err)
	}
	decision, ok := execCommand.Decision.AsNetworkPolicyAmendment()
	if !ok {
		t.Fatal("network_policy_amendment accessor returned ok=false")
	}
	if decision.NetworkPolicyAmendment.Action != NetworkPolicyRuleActionDeny || decision.NetworkPolicyAmendment.Host != "example.test" {
		t.Fatalf("decoded network policy amendment = %#v", decision.NetworkPolicyAmendment)
	}

	var execPolicy ExecCommandApprovalResponse
	if err := json.Unmarshal([]byte(`{"decision":{"approved_execpolicy_amendment":{"proposed_execpolicy_amendment":["read-only"]}}}`), &execPolicy); err != nil {
		t.Fatal(err)
	}
	amendment, ok := execPolicy.Decision.AsApprovedExecpolicyAmendment()
	if !ok {
		t.Fatal("approved_execpolicy_amendment accessor returned ok=false")
	}
	if len(amendment.ProposedExecpolicyAmendment) != 1 || amendment.ProposedExecpolicyAmendment[0] != "read-only" {
		t.Fatalf("decoded exec policy amendment = %#v", amendment)
	}
}

func TestGeneratedApprovalResponsesRejectMalformedReviewDecision(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "missing decision",
			raw:  `{}`,
			want: "decode ApplyPatchApprovalResponse.decision: missing required field",
		},
		{
			name: "null decision",
			raw:  `{"decision":null}`,
			want: "decode ApplyPatchApprovalResponse.decision: null is not allowed",
		},
		{
			name: "unknown string decision",
			raw:  `{"decision":"maybe"}`,
			want: `decode ReviewDecision.value: unknown variant "maybe"`,
		},
		{
			name: "wrong decision kind",
			raw:  `{"decision":1}`,
			want: "decode ReviewDecision: expected string or object",
		},
		{
			name: "empty object decision",
			raw:  `{"decision":{}}`,
			want: "decode ReviewDecision: expected one object variant",
		},
		{
			name: "unknown object decision",
			raw:  `{"decision":{"unknown":{}}}`,
			want: `decode ReviewDecision: unknown field "unknown"`,
		},
		{
			name: "extra object decision key",
			raw:  `{"decision":{"approved_execpolicy_amendment":{"proposed_execpolicy_amendment":[]},"extra":{}}}`,
			want: `decode ReviewDecision: unknown field "extra"`,
		},
		{
			name: "missing nested required field",
			raw:  `{"decision":{"approved_execpolicy_amendment":{}}}`,
			want: "decode ReviewDecision.approved_execpolicy_amendment.proposed_execpolicy_amendment: missing required field",
		},
		{
			name: "unknown nested field",
			raw:  `{"decision":{"network_policy_amendment":{"network_policy_amendment":{"action":"allow","host":"example.test"},"extra":true}}}`,
			want: `decode ReviewDecision.network_policy_amendment: unknown field "extra"`,
		},
		{
			name: "invalid nested enum",
			raw:  `{"decision":{"network_policy_amendment":{"network_policy_amendment":{"action":"maybe","host":"example.test"}}}}`,
			want: `invalid NetworkPolicyRuleAction enum value "maybe"`,
		},
		{
			name: "unknown response field",
			raw:  `{"decision":"approved","extra":true}`,
			want: `decode ApplyPatchApprovalResponse: unknown field "extra"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var response ApplyPatchApprovalResponse
			err := json.Unmarshal([]byte(tc.raw), &response)
			if err == nil {
				t.Fatal("expected malformed approval response to fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected malformed approval response error: %v", err)
			}
		})
	}

	_, err := json.Marshal(ApplyPatchApprovalResponse{})
	if err == nil {
		t.Fatal("expected unset review decision marshal to fail")
	}
	if !strings.Contains(err.Error(), `invalid ReviewDecision union value: no variant is set`) {
		t.Fatalf("unexpected unset review decision marshal error: %v", err)
	}

	_, err = json.Marshal(ApplyPatchApprovalResponse{
		Decision: NewReviewDecisionApprovedExecpolicyAmendment(ReviewDecisionApprovedExecpolicyAmendment{}),
	})
	if err == nil {
		t.Fatal("expected nil exec policy amendment marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode ReviewDecision.approved_execpolicy_amendment.proposed_execpolicy_amendment: nil is not allowed") {
		t.Fatalf("unexpected nil exec policy amendment marshal error: %v", err)
	}
}

func TestGeneratedCommandExecutionRequestApprovalResponseMarshalDecision(t *testing.T) {
	cases := []struct {
		name  string
		value CommandExecutionRequestApprovalResponse
		want  string
	}{
		{
			name: "accept",
			value: CommandExecutionRequestApprovalResponse{
				Decision: NewCommandExecutionApprovalDecisionAccept(),
			},
			want: `{"decision":"accept"}`,
		},
		{
			name: "cancel",
			value: CommandExecutionRequestApprovalResponse{
				Decision: NewCommandExecutionApprovalDecisionCancel(),
			},
			want: `{"decision":"cancel"}`,
		},
		{
			name: "exec policy amendment",
			value: CommandExecutionRequestApprovalResponse{
				Decision: NewCommandExecutionApprovalDecisionAcceptWithExecpolicyAmendment(CommandExecutionApprovalDecisionAcceptWithExecpolicyAmendment{
					ExecpolicyAmendment: []string{"allow-npm"},
				}),
			},
			want: `{"decision":{"acceptWithExecpolicyAmendment":{"execpolicy_amendment":["allow-npm"]}}}`,
		},
		{
			name: "network policy amendment",
			value: CommandExecutionRequestApprovalResponse{
				Decision: NewCommandExecutionApprovalDecisionApplyNetworkPolicyAmendment(CommandExecutionApprovalDecisionApplyNetworkPolicyAmendment{
					NetworkPolicyAmendment: NetworkPolicyAmendment{
						Action: NetworkPolicyRuleActionDeny,
						Host:   "example.test",
					},
				}),
			},
			want: `{"decision":{"applyNetworkPolicyAmendment":{"network_policy_amendment":{"action":"deny","host":"example.test"}}}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(raw); got != tc.want {
				t.Fatalf("CommandExecutionRequestApprovalResponse JSON = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestGeneratedCommandExecutionRequestApprovalResponseUnmarshalDecision(t *testing.T) {
	var acceptForSession CommandExecutionRequestApprovalResponse
	if err := json.Unmarshal([]byte(`{"decision":"acceptForSession"}`), &acceptForSession); err != nil {
		t.Fatal(err)
	}
	if acceptForSession.Decision.Kind() != CommandExecutionApprovalDecisionKindAcceptForSession {
		t.Fatalf("decision kind = %s, want %s", acceptForSession.Decision.Kind(), CommandExecutionApprovalDecisionKindAcceptForSession)
	}
	if _, ok := acceptForSession.Decision.AsAcceptForSession(); !ok {
		t.Fatal("acceptForSession accessor returned ok=false")
	}

	var execPolicy CommandExecutionRequestApprovalResponse
	if err := json.Unmarshal([]byte(`{"decision":{"acceptWithExecpolicyAmendment":{"execpolicy_amendment":["allow-rg"]}}}`), &execPolicy); err != nil {
		t.Fatal(err)
	}
	execPolicyAmendment, ok := execPolicy.Decision.AsAcceptWithExecpolicyAmendment()
	if !ok {
		t.Fatal("acceptWithExecpolicyAmendment accessor returned ok=false")
	}
	if len(execPolicyAmendment.ExecpolicyAmendment) != 1 || execPolicyAmendment.ExecpolicyAmendment[0] != "allow-rg" {
		t.Fatalf("decoded exec policy amendment = %#v", execPolicyAmendment)
	}

	var networkPolicy CommandExecutionRequestApprovalResponse
	if err := json.Unmarshal([]byte(`{"decision":{"applyNetworkPolicyAmendment":{"network_policy_amendment":{"action":"allow","host":"example.test"}}}}`), &networkPolicy); err != nil {
		t.Fatal(err)
	}
	networkPolicyAmendment, ok := networkPolicy.Decision.AsApplyNetworkPolicyAmendment()
	if !ok {
		t.Fatal("applyNetworkPolicyAmendment accessor returned ok=false")
	}
	if networkPolicyAmendment.NetworkPolicyAmendment.Action != NetworkPolicyRuleActionAllow || networkPolicyAmendment.NetworkPolicyAmendment.Host != "example.test" {
		t.Fatalf("decoded network policy amendment = %#v", networkPolicyAmendment.NetworkPolicyAmendment)
	}
}

func TestGeneratedCommandExecutionRequestApprovalResponseRejectsMalformedDecision(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "missing decision",
			raw:  `{}`,
			want: "decode CommandExecutionRequestApprovalResponse.decision: missing required field",
		},
		{
			name: "null decision",
			raw:  `{"decision":null}`,
			want: "decode CommandExecutionRequestApprovalResponse.decision: null is not allowed",
		},
		{
			name: "unknown string decision",
			raw:  `{"decision":"maybe"}`,
			want: `decode CommandExecutionApprovalDecision.value: unknown variant "maybe"`,
		},
		{
			name: "wrong decision kind",
			raw:  `{"decision":1}`,
			want: "decode CommandExecutionApprovalDecision: expected string or object",
		},
		{
			name: "empty object decision",
			raw:  `{"decision":{}}`,
			want: "decode CommandExecutionApprovalDecision: expected one object variant",
		},
		{
			name: "unknown object decision",
			raw:  `{"decision":{"unknown":{}}}`,
			want: `decode CommandExecutionApprovalDecision: unknown field "unknown"`,
		},
		{
			name: "extra object decision key",
			raw:  `{"decision":{"acceptWithExecpolicyAmendment":{"execpolicy_amendment":[]},"extra":{}}}`,
			want: `decode CommandExecutionApprovalDecision: unknown field "extra"`,
		},
		{
			name: "missing nested required field",
			raw:  `{"decision":{"acceptWithExecpolicyAmendment":{}}}`,
			want: "decode CommandExecutionApprovalDecision.acceptWithExecpolicyAmendment.execpolicy_amendment: missing required field",
		},
		{
			name: "unknown nested field",
			raw:  `{"decision":{"applyNetworkPolicyAmendment":{"network_policy_amendment":{"action":"allow","host":"example.test"},"extra":true}}}`,
			want: `decode CommandExecutionApprovalDecision.applyNetworkPolicyAmendment: unknown field "extra"`,
		},
		{
			name: "invalid nested enum",
			raw:  `{"decision":{"applyNetworkPolicyAmendment":{"network_policy_amendment":{"action":"maybe","host":"example.test"}}}}`,
			want: `invalid NetworkPolicyRuleAction enum value "maybe"`,
		},
		{
			name: "unknown response field",
			raw:  `{"decision":"accept","extra":true}`,
			want: `decode CommandExecutionRequestApprovalResponse: unknown field "extra"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var response CommandExecutionRequestApprovalResponse
			err := json.Unmarshal([]byte(tc.raw), &response)
			if err == nil {
				t.Fatal("expected malformed command execution approval response to fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected malformed command execution approval response error: %v", err)
			}
		})
	}

	_, err := json.Marshal(CommandExecutionRequestApprovalResponse{})
	if err == nil {
		t.Fatal("expected unset command execution approval decision marshal to fail")
	}
	if !strings.Contains(err.Error(), `invalid CommandExecutionApprovalDecision union value: no variant is set`) {
		t.Fatalf("unexpected unset command execution approval decision marshal error: %v", err)
	}

	_, err = json.Marshal(CommandExecutionRequestApprovalResponse{
		Decision: NewCommandExecutionApprovalDecisionAcceptWithExecpolicyAmendment(CommandExecutionApprovalDecisionAcceptWithExecpolicyAmendment{}),
	})
	if err == nil {
		t.Fatal("expected nil exec policy amendment marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode CommandExecutionApprovalDecision.acceptWithExecpolicyAmendment.execpolicy_amendment: nil is not allowed") {
		t.Fatalf("unexpected nil exec policy amendment marshal error: %v", err)
	}
}

func TestGeneratedCommandExecutionRequestApprovalParamsMarshalMinimal(t *testing.T) {
	raw, err := json.Marshal(CommandExecutionRequestApprovalParams{
		ItemID:      "item-1",
		StartedAtMS: 123,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"itemId":"item-1","startedAtMs":123,"threadId":"thread-1","turnId":"turn-1"}`
	if got := string(raw); got != want {
		t.Fatalf("CommandExecutionRequestApprovalParams minimal JSON = %s, want %s", got, want)
	}
}

func TestGeneratedCommandExecutionRequestApprovalParamsMarshalNestedTypedFields(t *testing.T) {
	params := CommandExecutionRequestApprovalParams{
		AdditionalPermissions: Value(AdditionalPermissionProfile{
			FileSystem: Value(AdditionalFileSystemPermissions{
				Entries: Value([]FileSystemSandboxEntry{{
					Access: FileSystemAccessModeRead,
					Path: NewFileSystemPathSpecial(FileSystemPathSpecial{
						Value: NewFileSystemSpecialPathProjectRoots(FileSystemSpecialPathProjectRoots{
							Subpath: Value("src"),
						}),
					}),
				}}),
				GlobScanMaxDepth: Value(uint64(1)),
				Read:             Null[[]string](),
				Write:            Value([]string{"/repo"}),
			}),
			Network: Value(AdditionalNetworkPermissions{
				Enabled: Value(true),
			}),
		}),
		ApprovalID: Null[string](),
		AvailableDecisions: Value([]CommandExecutionApprovalDecision{
			NewCommandExecutionApprovalDecisionAccept(),
			NewCommandExecutionApprovalDecisionApplyNetworkPolicyAmendment(CommandExecutionApprovalDecisionApplyNetworkPolicyAmendment{
				NetworkPolicyAmendment: NetworkPolicyAmendment{
					Action: NetworkPolicyRuleActionAllow,
					Host:   "example.test",
				},
			}),
		}),
		Command: Value("rg needle"),
		CommandActions: Value([]CommandAction{
			NewCommandActionRead(CommandActionRead{
				Command: "cat README.md",
				Name:    "README.md",
				Path:    "/repo/README.md",
			}),
			NewCommandActionSearch(CommandActionSearch{
				Command: "rg needle",
				Path:    Null[string](),
				Query:   Value("needle"),
			}),
		}),
		CWD:    Value("/repo"),
		ItemID: "item-1",
		NetworkApprovalContext: Value(NetworkApprovalContext{
			Host:     "example.test",
			Protocol: NetworkApprovalProtocolHttps,
		}),
		ProposedExecpolicyAmendment: Value([]string{"allow-rg"}),
		ProposedNetworkPolicyAmendments: Value([]NetworkPolicyAmendment{{
			Action: NetworkPolicyRuleActionDeny,
			Host:   "blocked.test",
		}}),
		Reason:      Value("needs access"),
		StartedAtMS: 123,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"additionalPermissions":{"fileSystem":{"entries":[{"access":"read","path":{"type":"special","value":{"kind":"project_roots","subpath":"src"}}}],"globScanMaxDepth":1,"read":null,"write":["/repo"]},"network":{"enabled":true}},"approvalId":null,"availableDecisions":["accept",{"applyNetworkPolicyAmendment":{"network_policy_amendment":{"action":"allow","host":"example.test"}}}],"command":"rg needle","commandActions":[{"command":"cat README.md","name":"README.md","path":"/repo/README.md","type":"read"},{"command":"rg needle","path":null,"query":"needle","type":"search"}],"cwd":"/repo","itemId":"item-1","networkApprovalContext":{"host":"example.test","protocol":"https"},"proposedExecpolicyAmendment":["allow-rg"],"proposedNetworkPolicyAmendments":[{"action":"deny","host":"blocked.test"}],"reason":"needs access","startedAtMs":123,"threadId":"thread-1","turnId":"turn-1"}`
	if got := string(raw); got != want {
		t.Fatalf("CommandExecutionRequestApprovalParams nested JSON = %s, want %s", got, want)
	}
}

func TestGeneratedCommandExecutionRequestApprovalParamsUnmarshalNestedTypedFields(t *testing.T) {
	var params CommandExecutionRequestApprovalParams
	err := json.Unmarshal([]byte(`{
		"additionalPermissions":{
			"fileSystem":{
				"entries":[{"access":"write","path":{"type":"path","path":"/repo"}}],
				"globScanMaxDepth":2,
				"read":null
			},
			"network":{"enabled":null}
		},
		"availableDecisions":["decline"],
		"commandActions":[{"command":"ls","path":null,"type":"listFiles"}],
		"cwd":null,
		"itemId":"item-1",
		"networkApprovalContext":{"host":"example.test","protocol":"socks5Tcp"},
		"proposedNetworkPolicyAmendments":[{"action":"allow","host":"example.test"}],
		"startedAtMs":123,
		"threadId":"thread-1",
		"turnId":"turn-1"
	}`), &params)
	if err != nil {
		t.Fatal(err)
	}
	if params.CWD == nil || params.CWD.Value != nil {
		t.Fatalf("decoded cwd = %#v", params.CWD)
	}
	if params.AdditionalPermissions == nil || params.AdditionalPermissions.Value == nil || params.AdditionalPermissions.Value.FileSystem == nil || params.AdditionalPermissions.Value.FileSystem.Value == nil {
		t.Fatalf("decoded additionalPermissions = %#v", params.AdditionalPermissions)
	}
	fileSystem := params.AdditionalPermissions.Value.FileSystem.Value
	if fileSystem.GlobScanMaxDepth == nil || fileSystem.GlobScanMaxDepth.Value == nil || *fileSystem.GlobScanMaxDepth.Value != 2 {
		t.Fatalf("decoded globScanMaxDepth = %#v", fileSystem.GlobScanMaxDepth)
	}
	if fileSystem.Read == nil || fileSystem.Read.Value != nil {
		t.Fatalf("decoded read permissions = %#v", fileSystem.Read)
	}
	path, ok := (*fileSystem.Entries.Value)[0].Path.AsPath()
	if !ok || path.Path != "/repo" {
		t.Fatalf("decoded filesystem path = %#v, ok=%t", (*fileSystem.Entries.Value)[0].Path, ok)
	}
	decision, ok := (*params.AvailableDecisions.Value)[0].AsDecline()
	if !ok {
		t.Fatalf("decoded available decision = %#v, ok=%t", decision, ok)
	}
	listFiles, ok := (*params.CommandActions.Value)[0].AsListFiles()
	if !ok || listFiles.Path == nil || listFiles.Path.Value != nil {
		t.Fatalf("decoded command action = %#v, ok=%t", listFiles, ok)
	}
	if params.NetworkApprovalContext == nil || params.NetworkApprovalContext.Value == nil || params.NetworkApprovalContext.Value.Protocol != NetworkApprovalProtocolSocks5Tcp {
		t.Fatalf("decoded networkApprovalContext = %#v", params.NetworkApprovalContext)
	}
	if params.ProposedNetworkPolicyAmendments == nil || params.ProposedNetworkPolicyAmendments.Value == nil || (*params.ProposedNetworkPolicyAmendments.Value)[0].Action != NetworkPolicyRuleActionAllow {
		t.Fatalf("decoded proposedNetworkPolicyAmendments = %#v", params.ProposedNetworkPolicyAmendments)
	}
}

func TestGeneratedCommandExecutionRequestApprovalParamsRejectsMalformedProtocol(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "missing item id",
			raw:  `{"startedAtMs":123,"threadId":"thread-1","turnId":"turn-1"}`,
			want: "decode CommandExecutionRequestApprovalParams.itemId: missing required field",
		},
		{
			name: "unknown params field",
			raw:  `{"itemId":"item-1","startedAtMs":123,"threadId":"thread-1","turnId":"turn-1","extra":true}`,
			want: `decode CommandExecutionRequestApprovalParams: unknown field "extra"`,
		},
		{
			name: "unknown command action",
			raw:  `{"commandActions":[{"command":"x","type":"replace"}],"itemId":"item-1","startedAtMs":123,"threadId":"thread-1","turnId":"turn-1"}`,
			want: `decode CommandAction.type: unknown variant "replace"`,
		},
		{
			name: "unknown filesystem special path",
			raw:  `{"additionalPermissions":{"fileSystem":{"entries":[{"access":"read","path":{"type":"special","value":{"kind":"workspace"}}}]}},"itemId":"item-1","startedAtMs":123,"threadId":"thread-1","turnId":"turn-1"}`,
			want: `decode FileSystemSpecialPath.kind: unknown variant "workspace"`,
		},
		{
			name: "invalid network approval protocol",
			raw:  `{"itemId":"item-1","networkApprovalContext":{"host":"example.test","protocol":"ftp"},"startedAtMs":123,"threadId":"thread-1","turnId":"turn-1"}`,
			want: `invalid NetworkApprovalProtocol enum value "ftp"`,
		},
		{
			name: "invalid glob scan depth",
			raw:  `{"additionalPermissions":{"fileSystem":{"globScanMaxDepth":0}},"itemId":"item-1","startedAtMs":123,"threadId":"thread-1","turnId":"turn-1"}`,
			want: "decode AdditionalFileSystemPermissions.globScanMaxDepth: value must be >= 1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var params CommandExecutionRequestApprovalParams
			err := json.Unmarshal([]byte(tc.raw), &params)
			if err == nil {
				t.Fatal("expected malformed command execution approval params to fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected malformed command execution approval params error: %v", err)
			}
		})
	}

	_, err := json.Marshal(CommandExecutionRequestApprovalParams{
		AdditionalPermissions: Value(AdditionalPermissionProfile{
			FileSystem: Value(AdditionalFileSystemPermissions{
				GlobScanMaxDepth: Value(uint64(0)),
			}),
		}),
		ItemID:      "item-1",
		StartedAtMS: 123,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
	})
	if err == nil {
		t.Fatal("expected invalid glob scan depth marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode AdditionalFileSystemPermissions.globScanMaxDepth: value must be >= 1") {
		t.Fatalf("unexpected invalid glob scan depth marshal error: %v", err)
	}
}

func TestGeneratedPermissionsRequestApprovalParamsMarshalNestedProfiles(t *testing.T) {
	params := PermissionsRequestApprovalParams{
		CWD:    "/repo",
		ItemID: "item-1",
		Permissions: RequestPermissionProfile{
			FileSystem: Value(AdditionalFileSystemPermissions{
				Entries: Value([]FileSystemSandboxEntry{{
					Access: FileSystemAccessModeWrite,
					Path: NewFileSystemPathPath(FileSystemPathPath{
						Path: "/repo/src",
					}),
				}}),
				GlobScanMaxDepth: Value(uint64(1)),
				Read:             Null[[]string](),
			}),
			Network: Value(AdditionalNetworkPermissions{
				Enabled: Value(false),
			}),
		},
		Reason:      Null[string](),
		StartedAtMS: 123,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"cwd":"/repo","itemId":"item-1","permissions":{"fileSystem":{"entries":[{"access":"write","path":{"path":"/repo/src","type":"path"}}],"globScanMaxDepth":1,"read":null},"network":{"enabled":false}},"reason":null,"startedAtMs":123,"threadId":"thread-1","turnId":"turn-1"}`
	if got := string(raw); got != want {
		t.Fatalf("PermissionsRequestApprovalParams JSON = %s, want %s", got, want)
	}
}

func TestGeneratedPermissionsRequestApprovalParamsUnmarshalNestedProfiles(t *testing.T) {
	var params PermissionsRequestApprovalParams
	if err := json.Unmarshal([]byte(`{
		"cwd":"/repo",
		"itemId":"item-1",
		"permissions":{
			"fileSystem":{
				"entries":[{"access":"read","path":{"type":"special","value":{"kind":"tmpdir"}}}],
				"write":["/repo/out"]
			},
			"network":{"enabled":null}
		},
		"reason":"needs permissions",
		"startedAtMs":123,
		"threadId":"thread-1",
		"turnId":"turn-1"
	}`), &params); err != nil {
		t.Fatal(err)
	}
	if params.Permissions.FileSystem == nil || params.Permissions.FileSystem.Value == nil {
		t.Fatalf("decoded fileSystem permissions = %#v", params.Permissions.FileSystem)
	}
	fileSystem := params.Permissions.FileSystem.Value
	if fileSystem.Entries == nil || fileSystem.Entries.Value == nil || len(*fileSystem.Entries.Value) != 1 {
		t.Fatalf("decoded entries = %#v", fileSystem.Entries)
	}
	special, ok := (*fileSystem.Entries.Value)[0].Path.AsSpecial()
	if !ok || special.Value.Kind() != FileSystemSpecialPathKindTmpdir {
		t.Fatalf("decoded filesystem special path = %#v, ok=%t", (*fileSystem.Entries.Value)[0].Path, ok)
	}
	if fileSystem.Write == nil || fileSystem.Write.Value == nil || (*fileSystem.Write.Value)[0] != "/repo/out" {
		t.Fatalf("decoded write permissions = %#v", fileSystem.Write)
	}
	if params.Permissions.Network == nil || params.Permissions.Network.Value == nil || params.Permissions.Network.Value.Enabled == nil || params.Permissions.Network.Value.Enabled.Value != nil {
		t.Fatalf("decoded network permissions = %#v", params.Permissions.Network)
	}
	if params.Reason == nil || params.Reason.Value == nil || *params.Reason.Value != "needs permissions" {
		t.Fatalf("decoded reason = %#v", params.Reason)
	}
}

func TestGeneratedPermissionsRequestApprovalResponseMarshalAndUnmarshal(t *testing.T) {
	response := PermissionsRequestApprovalResponse{
		Permissions: GrantedPermissionProfile{
			FileSystem: Value(AdditionalFileSystemPermissions{
				Entries: Value([]FileSystemSandboxEntry{{
					Access: FileSystemAccessModeRead,
					Path: NewFileSystemPathGlobPattern(FileSystemPathGlobPattern{
						Pattern: "/repo/**/*.go",
					}),
				}}),
			}),
			Network: Value(AdditionalNetworkPermissions{
				Enabled: Null[bool](),
			}),
		},
		Scope:            permissionGrantScopePtr(PermissionGrantScopeSession),
		StrictAutoReview: Null[bool](),
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"permissions":{"fileSystem":{"entries":[{"access":"read","path":{"pattern":"/repo/**/*.go","type":"glob_pattern"}}]},"network":{"enabled":null}},"scope":"session","strictAutoReview":null}`
	if got := string(raw); got != want {
		t.Fatalf("PermissionsRequestApprovalResponse JSON = %s, want %s", got, want)
	}

	var decoded PermissionsRequestApprovalResponse
	if err := json.Unmarshal([]byte(`{
		"permissions":{"fileSystem":null,"network":{"enabled":true}},
		"scope":"turn",
		"strictAutoReview":true
	}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Permissions.FileSystem == nil || decoded.Permissions.FileSystem.Value != nil {
		t.Fatalf("decoded fileSystem = %#v", decoded.Permissions.FileSystem)
	}
	if decoded.Permissions.Network == nil || decoded.Permissions.Network.Value == nil || decoded.Permissions.Network.Value.Enabled == nil || decoded.Permissions.Network.Value.Enabled.Value == nil || !*decoded.Permissions.Network.Value.Enabled.Value {
		t.Fatalf("decoded network = %#v", decoded.Permissions.Network)
	}
	if decoded.Scope == nil || *decoded.Scope != PermissionGrantScopeTurn {
		t.Fatalf("decoded scope = %#v", decoded.Scope)
	}
	if decoded.StrictAutoReview == nil || decoded.StrictAutoReview.Value == nil || !*decoded.StrictAutoReview.Value {
		t.Fatalf("decoded strictAutoReview = %#v", decoded.StrictAutoReview)
	}
}

func TestGeneratedPermissionsRequestApprovalRejectsMalformedProtocol(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "missing request permissions",
			raw:  `{"cwd":"/repo","itemId":"item-1","startedAtMs":123,"threadId":"thread-1","turnId":"turn-1"}`,
			want: "decode PermissionsRequestApprovalParams.permissions: missing required field",
		},
		{
			name: "unknown request profile field",
			raw:  `{"cwd":"/repo","itemId":"item-1","permissions":{"extra":true},"startedAtMs":123,"threadId":"thread-1","turnId":"turn-1"}`,
			want: `decode RequestPermissionProfile: unknown field "extra"`,
		},
		{
			name: "invalid filesystem access",
			raw:  `{"cwd":"/repo","itemId":"item-1","permissions":{"fileSystem":{"entries":[{"access":"execute","path":{"type":"path","path":"/repo"}}]}},"startedAtMs":123,"threadId":"thread-1","turnId":"turn-1"}`,
			want: `invalid FileSystemAccessMode enum value "execute"`,
		},
		{
			name: "invalid glob scan depth",
			raw:  `{"cwd":"/repo","itemId":"item-1","permissions":{"fileSystem":{"globScanMaxDepth":0}},"startedAtMs":123,"threadId":"thread-1","turnId":"turn-1"}`,
			want: "decode AdditionalFileSystemPermissions.globScanMaxDepth: value must be >= 1",
		},
		{
			name: "missing response permissions",
			raw:  `{"scope":"turn"}`,
			want: "decode PermissionsRequestApprovalResponse.permissions: missing required field",
		},
		{
			name: "invalid response scope",
			raw:  `{"permissions":{},"scope":"forever"}`,
			want: `invalid PermissionGrantScope enum value "forever"`,
		},
		{
			name: "unknown response field",
			raw:  `{"permissions":{},"extra":true}`,
			want: `decode PermissionsRequestApprovalResponse: unknown field "extra"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Contains(tc.name, "response") {
				var response PermissionsRequestApprovalResponse
				err := json.Unmarshal([]byte(tc.raw), &response)
				if err == nil {
					t.Fatal("expected malformed permissions response to fail")
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("unexpected malformed permissions response error: %v", err)
				}
				return
			}
			var params PermissionsRequestApprovalParams
			err := json.Unmarshal([]byte(tc.raw), &params)
			if err == nil {
				t.Fatal("expected malformed permissions params to fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected malformed permissions params error: %v", err)
			}
		})
	}

	_, err := json.Marshal(PermissionsRequestApprovalResponse{
		Permissions: GrantedPermissionProfile{},
		Scope:       permissionGrantScopePtr(PermissionGrantScope("forever")),
	})
	if err == nil {
		t.Fatal("expected invalid permission grant scope marshal to fail")
	}
	if !strings.Contains(err.Error(), `invalid PermissionGrantScope enum value "forever"`) {
		t.Fatalf("unexpected invalid scope marshal error: %v", err)
	}

	_, err = json.Marshal(PermissionsRequestApprovalParams{
		CWD:    "/repo",
		ItemID: "item-1",
		Permissions: RequestPermissionProfile{
			FileSystem: Value(AdditionalFileSystemPermissions{
				GlobScanMaxDepth: Value(uint64(0)),
			}),
		},
		StartedAtMS: 123,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
	})
	if err == nil {
		t.Fatal("expected invalid request glob scan depth marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode AdditionalFileSystemPermissions.globScanMaxDepth: value must be >= 1") {
		t.Fatalf("unexpected invalid request glob scan depth marshal error: %v", err)
	}
}

func TestGeneratedGuardianApprovalReviewActionMarshalAndUnmarshalVariants(t *testing.T) {
	cases := []struct {
		name   string
		action GuardianApprovalReviewAction
		want   string
		assert func(t *testing.T, action GuardianApprovalReviewAction)
	}{
		{
			name: "command",
			action: NewGuardianApprovalReviewActionCommand(GuardianApprovalReviewActionCommand{
				Command: "rg needle",
				CWD:     "/repo",
				Source:  GuardianCommandSourceShell,
			}),
			want: `{"command":"rg needle","cwd":"/repo","source":"shell","type":"command"}`,
			assert: func(t *testing.T, action GuardianApprovalReviewAction) {
				t.Helper()
				command, ok := action.AsCommand()
				if !ok || command.Command != "rg needle" || command.CWD != "/repo" || command.Source != GuardianCommandSourceShell {
					t.Fatalf("decoded command action = %#v, ok=%t", command, ok)
				}
			},
		},
		{
			name: "execve",
			action: NewGuardianApprovalReviewActionExecve(GuardianApprovalReviewActionExecve{
				Argv:    []string{"-la"},
				CWD:     "/repo",
				Program: "ls",
				Source:  GuardianCommandSourceUnifiedExec,
			}),
			want: `{"argv":["-la"],"cwd":"/repo","program":"ls","source":"unifiedExec","type":"execve"}`,
			assert: func(t *testing.T, action GuardianApprovalReviewAction) {
				t.Helper()
				execve, ok := action.AsExecve()
				if !ok || len(execve.Argv) != 1 || execve.Argv[0] != "-la" || execve.Program != "ls" {
					t.Fatalf("decoded execve action = %#v, ok=%t", execve, ok)
				}
			},
		},
		{
			name: "apply patch",
			action: NewGuardianApprovalReviewActionApplyPatch(GuardianApprovalReviewActionApplyPatch{
				CWD:   "/repo",
				Files: []string{"/repo/file.go"},
			}),
			want: `{"cwd":"/repo","files":["/repo/file.go"],"type":"applyPatch"}`,
			assert: func(t *testing.T, action GuardianApprovalReviewAction) {
				t.Helper()
				applyPatch, ok := action.AsApplyPatch()
				if !ok || len(applyPatch.Files) != 1 || applyPatch.Files[0] != "/repo/file.go" {
					t.Fatalf("decoded apply patch action = %#v, ok=%t", applyPatch, ok)
				}
			},
		},
		{
			name: "network access",
			action: NewGuardianApprovalReviewActionNetworkAccess(GuardianApprovalReviewActionNetworkAccess{
				Host:     "example.test",
				Port:     443,
				Protocol: NetworkApprovalProtocolHttps,
				Target:   "example.test:443",
			}),
			want: `{"host":"example.test","port":443,"protocol":"https","target":"example.test:443","type":"networkAccess"}`,
			assert: func(t *testing.T, action GuardianApprovalReviewAction) {
				t.Helper()
				network, ok := action.AsNetworkAccess()
				if !ok || network.Host != "example.test" || network.Port != 443 || network.Protocol != NetworkApprovalProtocolHttps {
					t.Fatalf("decoded network action = %#v, ok=%t", network, ok)
				}
			},
		},
		{
			name: "mcp tool call",
			action: NewGuardianApprovalReviewActionMCPToolCall(GuardianApprovalReviewActionMCPToolCall{
				ConnectorID:   Null[string](),
				ConnectorName: Value("connector"),
				Server:        "mcp-server",
				ToolName:      "lookup",
				ToolTitle:     Value("Lookup"),
			}),
			want: `{"connectorId":null,"connectorName":"connector","server":"mcp-server","toolName":"lookup","toolTitle":"Lookup","type":"mcpToolCall"}`,
			assert: func(t *testing.T, action GuardianApprovalReviewAction) {
				t.Helper()
				mcpToolCall, ok := action.AsMCPToolCall()
				if !ok || mcpToolCall.ConnectorID == nil || mcpToolCall.ConnectorID.Value != nil ||
					mcpToolCall.ConnectorName == nil || *mcpToolCall.ConnectorName.Value != "connector" ||
					mcpToolCall.ToolTitle == nil || *mcpToolCall.ToolTitle.Value != "Lookup" {
					t.Fatalf("decoded MCP tool call action = %#v, ok=%t", mcpToolCall, ok)
				}
			},
		},
		{
			name: "request permissions",
			action: NewGuardianApprovalReviewActionRequestPermissions(GuardianApprovalReviewActionRequestPermissions{
				Permissions: RequestPermissionProfile{
					FileSystem: Value(AdditionalFileSystemPermissions{
						Entries: Value([]FileSystemSandboxEntry{{
							Access: FileSystemAccessModeRead,
							Path: NewFileSystemPathPath(FileSystemPathPath{
								Path: "/repo",
							}),
						}}),
					}),
					Network: Value(AdditionalNetworkPermissions{
						Enabled: Value(true),
					}),
				},
				Reason: Value("needs read"),
			}),
			want: `{"permissions":{"fileSystem":{"entries":[{"access":"read","path":{"path":"/repo","type":"path"}}]},"network":{"enabled":true}},"reason":"needs read","type":"requestPermissions"}`,
			assert: func(t *testing.T, action GuardianApprovalReviewAction) {
				t.Helper()
				requestPermissions, ok := action.AsRequestPermissions()
				if !ok || requestPermissions.Reason == nil || *requestPermissions.Reason.Value != "needs read" ||
					requestPermissions.Permissions.FileSystem == nil || requestPermissions.Permissions.Network == nil {
					t.Fatalf("decoded request permissions action = %#v, ok=%t", requestPermissions, ok)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.action)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(raw); got != tc.want {
				t.Fatalf("GuardianApprovalReviewAction JSON = %s, want %s", got, tc.want)
			}
			var decoded GuardianApprovalReviewAction
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Kind() != tc.action.Kind() {
				t.Fatalf("decoded action kind = %s, want %s", decoded.Kind(), tc.action.Kind())
			}
			tc.assert(t, decoded)
		})
	}
}

func TestGeneratedItemGuardianApprovalReviewNotificationsMarshalAndUnmarshal(t *testing.T) {
	started := ItemGuardianApprovalReviewStartedNotification{
		Action: NewGuardianApprovalReviewActionCommand(GuardianApprovalReviewActionCommand{
			Command: "go test ./...",
			CWD:     "/repo",
			Source:  GuardianCommandSourceUnifiedExec,
		}),
		Review: GuardianApprovalReview{
			Rationale:         Value("low risk"),
			RiskLevel:         Value(GuardianRiskLevelLow),
			Status:            GuardianApprovalReviewStatusInProgress,
			UserAuthorization: Null[GuardianUserAuthorization](),
		},
		ReviewID:    "review-1",
		StartedAtMS: 1000,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
	}
	rawStarted, err := json.Marshal(started)
	if err != nil {
		t.Fatal(err)
	}
	wantStarted := `{"action":{"command":"go test ./...","cwd":"/repo","source":"unifiedExec","type":"command"},"review":{"rationale":"low risk","riskLevel":"low","status":"inProgress","userAuthorization":null},"reviewId":"review-1","startedAtMs":1000,"threadId":"thread-1","turnId":"turn-1"}`
	if got := string(rawStarted); got != wantStarted {
		t.Fatalf("started guardian review JSON = %s, want %s", got, wantStarted)
	}

	completed := ItemGuardianApprovalReviewCompletedNotification{
		Action: NewGuardianApprovalReviewActionNetworkAccess(GuardianApprovalReviewActionNetworkAccess{
			Host:     "example.test",
			Port:     443,
			Protocol: NetworkApprovalProtocolHttps,
			Target:   "example.test:443",
		}),
		CompletedAtMS:  1100,
		DecisionSource: AutoReviewDecisionSourceAgent,
		Review: GuardianApprovalReview{
			Rationale:         Null[string](),
			RiskLevel:         Value(GuardianRiskLevelMedium),
			Status:            GuardianApprovalReviewStatusApproved,
			UserAuthorization: Value(GuardianUserAuthorizationHigh),
		},
		ReviewID:     "review-1",
		StartedAtMS:  1000,
		TargetItemID: Null[string](),
		ThreadID:     "thread-1",
		TurnID:       "turn-1",
	}
	rawCompleted, err := json.Marshal(completed)
	if err != nil {
		t.Fatal(err)
	}
	wantCompleted := `{"action":{"host":"example.test","port":443,"protocol":"https","target":"example.test:443","type":"networkAccess"},"completedAtMs":1100,"decisionSource":"agent","review":{"rationale":null,"riskLevel":"medium","status":"approved","userAuthorization":"high"},"reviewId":"review-1","startedAtMs":1000,"targetItemId":null,"threadId":"thread-1","turnId":"turn-1"}`
	if got := string(rawCompleted); got != wantCompleted {
		t.Fatalf("completed guardian review JSON = %s, want %s", got, wantCompleted)
	}

	var decoded ItemGuardianApprovalReviewCompletedNotification
	if err := json.Unmarshal([]byte(`{
		"action":{"cwd":"/repo","files":["/repo/file.go"],"type":"applyPatch"},
		"completedAtMs":1200,
		"decisionSource":"agent",
		"review":{"rationale":"patched","riskLevel":null,"status":"denied","userAuthorization":"medium"},
		"reviewId":"review-2",
		"startedAtMs":1000,
		"targetItemId":"item-1",
		"threadId":"thread-1",
		"turnId":"turn-1"
	}`), &decoded); err != nil {
		t.Fatal(err)
	}
	applyPatch, ok := decoded.Action.AsApplyPatch()
	if !ok || len(applyPatch.Files) != 1 || applyPatch.Files[0] != "/repo/file.go" {
		t.Fatalf("decoded action = %#v, ok=%t", applyPatch, ok)
	}
	if decoded.TargetItemID == nil || decoded.TargetItemID.Value == nil || *decoded.TargetItemID.Value != "item-1" {
		t.Fatalf("decoded targetItemId = %#v", decoded.TargetItemID)
	}
	if decoded.Review.RiskLevel == nil || decoded.Review.RiskLevel.Value != nil {
		t.Fatalf("decoded review riskLevel = %#v", decoded.Review.RiskLevel)
	}
	if decoded.Review.UserAuthorization == nil || *decoded.Review.UserAuthorization.Value != GuardianUserAuthorizationMedium {
		t.Fatalf("decoded review userAuthorization = %#v", decoded.Review.UserAuthorization)
	}
}

func TestGeneratedItemGuardianApprovalReviewNotificationsRejectMalformedProtocol(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "missing started action",
			raw:  `{"review":{"status":"inProgress"},"reviewId":"review-1","startedAtMs":1000,"threadId":"thread-1","turnId":"turn-1"}`,
			want: "decode ItemGuardianApprovalReviewStartedNotification.action: missing required field",
		},
		{
			name: "null started timestamp",
			raw:  `{"action":{"command":"ls","cwd":"/repo","source":"shell","type":"command"},"review":{"status":"inProgress"},"reviewId":"review-1","startedAtMs":null,"threadId":"thread-1","turnId":"turn-1"}`,
			want: "decode ItemGuardianApprovalReviewStartedNotification.startedAtMs: null is not allowed",
		},
		{
			name: "unknown action variant",
			raw:  `{"action":{"type":"unknown"},"review":{"status":"inProgress"},"reviewId":"review-1","startedAtMs":1000,"threadId":"thread-1","turnId":"turn-1"}`,
			want: `decode GuardianApprovalReviewAction.type: unknown variant "unknown"`,
		},
		{
			name: "missing action variant field",
			raw:  `{"action":{"cwd":"/repo","source":"shell","type":"command"},"review":{"status":"inProgress"},"reviewId":"review-1","startedAtMs":1000,"threadId":"thread-1","turnId":"turn-1"}`,
			want: "decode GuardianApprovalReviewAction.command: missing required field",
		},
		{
			name: "invalid review enum",
			raw:  `{"action":{"command":"ls","cwd":"/repo","source":"shell","type":"command"},"review":{"status":"unknown"},"reviewId":"review-1","startedAtMs":1000,"threadId":"thread-1","turnId":"turn-1"}`,
			want: `invalid GuardianApprovalReviewStatus enum value "unknown"`,
		},
		{
			name: "unknown review field",
			raw:  `{"action":{"command":"ls","cwd":"/repo","source":"shell","type":"command"},"review":{"status":"inProgress","extra":true},"reviewId":"review-1","startedAtMs":1000,"threadId":"thread-1","turnId":"turn-1"}`,
			want: `decode GuardianApprovalReview: unknown field "extra"`,
		},
		{
			name: "invalid decision source",
			raw:  `{"action":{"command":"ls","cwd":"/repo","source":"shell","type":"command"},"completedAtMs":1100,"decisionSource":"policy","review":{"status":"approved"},"reviewId":"review-1","startedAtMs":1000,"threadId":"thread-1","turnId":"turn-1"}`,
			want: `invalid AutoReviewDecisionSource enum value "policy"`,
		},
		{
			name: "unknown completed field",
			raw:  `{"action":{"command":"ls","cwd":"/repo","source":"shell","type":"command"},"completedAtMs":1100,"decisionSource":"agent","review":{"status":"approved"},"reviewId":"review-1","startedAtMs":1000,"threadId":"thread-1","turnId":"turn-1","extra":true}`,
			want: `decode ItemGuardianApprovalReviewCompletedNotification: unknown field "extra"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Contains(tc.name, "completed") || strings.Contains(tc.name, "decision source") {
				var notification ItemGuardianApprovalReviewCompletedNotification
				err := json.Unmarshal([]byte(tc.raw), &notification)
				if err == nil {
					t.Fatal("expected malformed guardian completed notification to fail")
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("unexpected malformed guardian completed notification error: %v", err)
				}
				return
			}
			var notification ItemGuardianApprovalReviewStartedNotification
			err := json.Unmarshal([]byte(tc.raw), &notification)
			if err == nil {
				t.Fatal("expected malformed guardian started notification to fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected malformed guardian started notification error: %v", err)
			}
		})
	}

	_, err := json.Marshal(GuardianApprovalReviewAction{})
	if err == nil {
		t.Fatal("expected unset guardian approval review action marshal to fail")
	}
	if !strings.Contains(err.Error(), `invalid GuardianApprovalReviewAction union value: no variant is set`) {
		t.Fatalf("unexpected unset guardian action marshal error: %v", err)
	}
}

func TestGeneratedNullableArrayMarshalOmitNullAndValue(t *testing.T) {
	omitted, err := json.Marshal(ExternalAgentConfigDetectParams{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(omitted), `{}`; got != want {
		t.Fatalf("omitted nullable array JSON = %s, want %s", got, want)
	}

	null, err := json.Marshal(ExternalAgentConfigDetectParams{
		CWDs: Null[[]string](),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(null), `{"cwds":null}`; got != want {
		t.Fatalf("null nullable array JSON = %s, want %s", got, want)
	}

	value, err := json.Marshal(ExternalAgentConfigDetectParams{
		CWDs: Value([]string{"/repo"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(value), `{"cwds":["/repo"]}`; got != want {
		t.Fatalf("value nullable array JSON = %s, want %s", got, want)
	}
}

func TestGeneratedNullableArrayUnmarshalPreservesOmitNullAndValue(t *testing.T) {
	var omitted ExternalAgentConfigDetectParams
	if err := json.Unmarshal([]byte(`{}`), &omitted); err != nil {
		t.Fatal(err)
	}
	if omitted.CWDs != nil {
		t.Fatalf("omitted nullable array decoded as %#v", omitted.CWDs)
	}

	var nullValue ExternalAgentConfigDetectParams
	if err := json.Unmarshal([]byte(`{"cwds":null}`), &nullValue); err != nil {
		t.Fatal(err)
	}
	if nullValue.CWDs == nil || nullValue.CWDs.Value != nil {
		t.Fatalf("null nullable array decoded as %#v", nullValue.CWDs)
	}

	var concrete ExternalAgentConfigDetectParams
	if err := json.Unmarshal([]byte(`{"cwds":["/repo"]}`), &concrete); err != nil {
		t.Fatal(err)
	}
	if concrete.CWDs == nil || concrete.CWDs.Value == nil || len(*concrete.CWDs.Value) != 1 || (*concrete.CWDs.Value)[0] != "/repo" {
		t.Fatalf("value nullable array decoded as %#v", concrete.CWDs)
	}
}

func TestGeneratedOptionalNonNullableStillRejectsNull(t *testing.T) {
	var params ExternalAgentConfigDetectParams
	err := json.Unmarshal([]byte(`{"includeHome":null}`), &params)
	if err == nil {
		t.Fatal("expected optional non-nullable field null to fail")
	}
	if !strings.Contains(err.Error(), "decode ExternalAgentConfigDetectParams.includeHome: null is not allowed") {
		t.Fatalf("unexpected optional non-nullable null error: %v", err)
	}
}

func TestGeneratedConstrainedUint32MarshalAndUnmarshal(t *testing.T) {
	raw, err := json.Marshal(ThreadRollbackParams{
		NumTurns: 2,
		ThreadID: "thread-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"numTurns":2,"threadId":"thread-1"}`; got != want {
		t.Fatalf("ThreadRollbackParams JSON = %s, want %s", got, want)
	}

	var params ThreadRollbackParams
	if err := json.Unmarshal([]byte(`{"numTurns":2,"threadId":"thread-1"}`), &params); err != nil {
		t.Fatal(err)
	}
	if params.NumTurns != 2 || params.ThreadID != "thread-1" {
		t.Fatalf("ThreadRollbackParams decoded as %#v", params)
	}
}

func TestGeneratedConstrainedUint32RejectsNegative(t *testing.T) {
	var params ThreadRollbackParams
	err := json.Unmarshal([]byte(`{"numTurns":-1,"threadId":"thread-1"}`), &params)
	if err == nil {
		t.Fatal("expected negative uint32 to fail")
	}
	if !strings.Contains(err.Error(), "decode ThreadRollbackParams.numTurns") {
		t.Fatalf("unexpected negative uint32 error: %v", err)
	}
}

func TestGeneratedConstrainedInt32RejectsOverflow(t *testing.T) {
	var notification ProcessExitedNotification
	err := json.Unmarshal([]byte(`{
		"exitCode":2147483648,
		"processHandle":"proc-1",
		"stderr":"",
		"stderrCapReached":false,
		"stdout":"",
		"stdoutCapReached":false
	}`), &notification)
	if err == nil {
		t.Fatal("expected int32 overflow to fail")
	}
	if !strings.Contains(err.Error(), "decode ProcessExitedNotification.exitCode") {
		t.Fatalf("unexpected int32 overflow error: %v", err)
	}
}

func TestGeneratedInt64ElicitationCountAcceptsNegativeSchemaValue(t *testing.T) {
	var response ThreadDecrementElicitationResponse
	err := json.Unmarshal([]byte(`{"count":-1,"paused":false}`), &response)
	if err != nil {
		t.Fatalf("negative int64 elicitation count rejected: %v", err)
	}
	if response.Count != -1 {
		t.Fatalf("decoded elicitation count = %d, want -1", response.Count)
	}
}

func TestGeneratedNullableConstrainedIntegerPreservesOmitNullAndValue(t *testing.T) {
	omitted, err := json.Marshal(AppsListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(omitted), `{}`; got != want {
		t.Fatalf("omitted constrained integer JSON = %s, want %s", got, want)
	}

	null, err := json.Marshal(AppsListParams{Limit: Null[uint32]()})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(null), `{"limit":null}`; got != want {
		t.Fatalf("null constrained integer JSON = %s, want %s", got, want)
	}

	value, err := json.Marshal(AppsListParams{Limit: Value(uint32(25))})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(value), `{"limit":25}`; got != want {
		t.Fatalf("value constrained integer JSON = %s, want %s", got, want)
	}
}

func TestGeneratedEnumMarshal(t *testing.T) {
	raw, err := json.Marshal(CancelLoginAccountResponse{
		Status: CancelLoginAccountStatusCanceled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"status":"canceled"}`; got != want {
		t.Fatalf("CancelLoginAccountResponse JSON = %s, want %s", got, want)
	}
}

func TestGeneratedEnumArrayMarshal(t *testing.T) {
	raw, err := json.Marshal(ModelVerificationNotification{
		ThreadID:      "thread-1",
		TurnID:        "turn-1",
		Verifications: []ModelVerification{ModelVerificationTrustedAccessForCyber},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"threadId":"thread-1","turnId":"turn-1","verifications":["trustedAccessForCyber"]}`
	if got := string(raw); got != want {
		t.Fatalf("ModelVerificationNotification JSON = %s, want %s", got, want)
	}
}

func TestGeneratedEnumMarshalRejectsInvalidValue(t *testing.T) {
	_, err := json.Marshal(CancelLoginAccountResponse{
		Status: CancelLoginAccountStatus("bogus"),
	})
	if err == nil {
		t.Fatal("expected invalid enum marshal to fail")
	}
	if !strings.Contains(err.Error(), `invalid CancelLoginAccountStatus enum value "bogus"`) {
		t.Fatalf("unexpected invalid enum marshal error: %v", err)
	}
}

func TestGeneratedEnumUnmarshalRejectsInvalidValue(t *testing.T) {
	var response CancelLoginAccountResponse
	err := json.Unmarshal([]byte(`{"status":"bogus"}`), &response)
	if err == nil {
		t.Fatal("expected invalid enum unmarshal to fail")
	}
	if !strings.Contains(err.Error(), `invalid CancelLoginAccountStatus enum value "bogus"`) {
		t.Fatalf("unexpected invalid enum unmarshal error: %v", err)
	}
}

func TestGeneratedSingleOneOfEnumMarshal(t *testing.T) {
	raw, err := json.Marshal(ChatgptAuthTokensRefreshParams{
		Reason: ChatgptAuthTokensRefreshReasonUnauthorized,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"reason":"unauthorized"}`; got != want {
		t.Fatalf("ChatgptAuthTokensRefreshParams JSON = %s, want %s", got, want)
	}
}

func TestGeneratedSingleOneOfEnumRejectsInvalidValue(t *testing.T) {
	_, err := json.Marshal(ChatgptAuthTokensRefreshParams{
		Reason: ChatgptAuthTokensRefreshReason("bogus"),
	})
	if err == nil {
		t.Fatal("expected invalid refresh reason marshal to fail")
	}
	if !strings.Contains(err.Error(), `invalid ChatgptAuthTokensRefreshReason enum value "bogus"`) {
		t.Fatalf("unexpected invalid refresh reason error: %v", err)
	}
}

func TestGeneratedChatgptAuthTokensRefreshParamsPreservesNullableAccountId(t *testing.T) {
	nullAccount, err := json.Marshal(ChatgptAuthTokensRefreshParams{
		PreviousAccountID: Null[string](),
		Reason:            ChatgptAuthTokensRefreshReasonUnauthorized,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(nullAccount), `{"previousAccountId":null,"reason":"unauthorized"}`; got != want {
		t.Fatalf("null previous account JSON = %s, want %s", got, want)
	}

	var decoded ChatgptAuthTokensRefreshParams
	if err := json.Unmarshal([]byte(`{"previousAccountId":"account-1","reason":"unauthorized"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.PreviousAccountID == nil || decoded.PreviousAccountID.Value == nil || *decoded.PreviousAccountID.Value != "account-1" {
		t.Fatalf("decoded PreviousAccountID = %#v", decoded.PreviousAccountID)
	}
}

func TestGeneratedChatgptAuthTokensRefreshParamsRejectsMissingOrNullReason(t *testing.T) {
	var params ChatgptAuthTokensRefreshParams
	err := json.Unmarshal([]byte(`{}`), &params)
	if err == nil {
		t.Fatal("expected missing refresh reason to fail")
	}
	if !strings.Contains(err.Error(), "decode ChatgptAuthTokensRefreshParams.reason: missing required field") {
		t.Fatalf("unexpected missing reason error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"reason":null}`), &params)
	if err == nil {
		t.Fatal("expected null refresh reason to fail")
	}
	if !strings.Contains(err.Error(), "decode ChatgptAuthTokensRefreshParams.reason: null is not allowed") {
		t.Fatalf("unexpected null reason error: %v", err)
	}
}

func TestGeneratedChatgptAuthTokensRefreshParamsRejectsUnknownReason(t *testing.T) {
	var params ChatgptAuthTokensRefreshParams
	err := json.Unmarshal([]byte(`{"reason":"bogus"}`), &params)
	if err == nil {
		t.Fatal("expected unknown refresh reason to fail")
	}
	if !strings.Contains(err.Error(), `invalid ChatgptAuthTokensRefreshReason enum value "bogus"`) {
		t.Fatalf("unexpected unknown reason error: %v", err)
	}
}

func TestGeneratedChatgptAuthTokensRefreshResponsePreservesNullablePlanType(t *testing.T) {
	omitted, err := json.Marshal(ChatgptAuthTokensRefreshResponse{
		AccessToken:      "token-1",
		ChatGPTAccountID: "account-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(omitted), `{"accessToken":"token-1","chatgptAccountId":"account-1"}`; got != want {
		t.Fatalf("omitted ChatGPT plan type JSON = %s, want %s", got, want)
	}

	nullPlanType, err := json.Marshal(ChatgptAuthTokensRefreshResponse{
		AccessToken:      "token-1",
		ChatGPTAccountID: "account-1",
		ChatGPTPlanType:  Null[string](),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(nullPlanType), `{"accessToken":"token-1","chatgptAccountId":"account-1","chatgptPlanType":null}`; got != want {
		t.Fatalf("null ChatGPT plan type JSON = %s, want %s", got, want)
	}

	var decoded ChatgptAuthTokensRefreshResponse
	if err := json.Unmarshal([]byte(`{"accessToken":"token-1","chatgptAccountId":"account-1","chatgptPlanType":"plus"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ChatGPTPlanType == nil || decoded.ChatGPTPlanType.Value == nil || *decoded.ChatGPTPlanType.Value != "plus" {
		t.Fatalf("decoded ChatGPTPlanType = %#v", decoded.ChatGPTPlanType)
	}
}

func TestGeneratedChatgptAuthTokensRefreshResponseRejectsMalformedProtocol(t *testing.T) {
	var response ChatgptAuthTokensRefreshResponse
	err := json.Unmarshal([]byte(`{"chatgptAccountId":"account-1"}`), &response)
	if err == nil {
		t.Fatal("expected missing accessToken to fail")
	}
	if !strings.Contains(err.Error(), "decode ChatgptAuthTokensRefreshResponse.accessToken: missing required field") {
		t.Fatalf("unexpected missing accessToken error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"accessToken":"token-1","chatgptAccountId":null}`), &response)
	if err == nil {
		t.Fatal("expected null chatgptAccountId to fail")
	}
	if !strings.Contains(err.Error(), "decode ChatgptAuthTokensRefreshResponse.chatgptAccountId: null is not allowed") {
		t.Fatalf("unexpected null chatgptAccountId error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"accessToken":"token-1","chatgptAccountId":"account-1","extra":true}`), &response)
	if err == nil {
		t.Fatal("expected unknown ChatGPT auth refresh response field to fail")
	}
	if !strings.Contains(err.Error(), `decode ChatgptAuthTokensRefreshResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown ChatGPT auth refresh response error: %v", err)
	}
}

func TestGeneratedToolRequestUserInputParamsMarshal(t *testing.T) {
	isOther := true
	params := ToolRequestUserInputParams{
		ItemID:   "item-1",
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		Questions: []ToolRequestUserInputQuestion{{
			Header:  "Auth",
			ID:      "token",
			IsOther: &isOther,
			Options: Value([]ToolRequestUserInputOption{{
				Description: "Use the configured account.",
				Label:       "Configured",
			}}),
			Question: "Which account should be used?",
		}},
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"itemId":"item-1","questions":[{"header":"Auth","id":"token","isOther":true,"options":[{"description":"Use the configured account.","label":"Configured"}],"question":"Which account should be used?"}],"threadId":"thread-1","turnId":"turn-1"}`
	if got := string(raw); got != want {
		t.Fatalf("ToolRequestUserInputParams JSON = %s, want %s", got, want)
	}
}

func TestGeneratedToolRequestUserInputQuestionPreservesNullableOptions(t *testing.T) {
	omitted, err := json.Marshal(ToolRequestUserInputQuestion{
		Header:   "Auth",
		ID:       "token",
		Question: "Which account should be used?",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(omitted), `{"header":"Auth","id":"token","question":"Which account should be used?"}`; got != want {
		t.Fatalf("omitted options JSON = %s, want %s", got, want)
	}

	nullOptions, err := json.Marshal(ToolRequestUserInputQuestion{
		Header:   "Auth",
		ID:       "token",
		Options:  Null[[]ToolRequestUserInputOption](),
		Question: "Which account should be used?",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(nullOptions), `{"header":"Auth","id":"token","options":null,"question":"Which account should be used?"}`; got != want {
		t.Fatalf("null options JSON = %s, want %s", got, want)
	}

	var decoded ToolRequestUserInputQuestion
	if err := json.Unmarshal([]byte(`{"header":"Auth","id":"token","options":[{"description":"Use it","label":"Use"}],"question":"Which account should be used?"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Options == nil || decoded.Options.Value == nil || len(*decoded.Options.Value) != 1 || (*decoded.Options.Value)[0].Label != "Use" {
		t.Fatalf("decoded options = %#v", decoded.Options)
	}
}

func TestGeneratedToolRequestUserInputParamsRejectsMalformedProtocol(t *testing.T) {
	var params ToolRequestUserInputParams
	err := json.Unmarshal([]byte(`{"itemId":"item-1","threadId":"thread-1","turnId":"turn-1"}`), &params)
	if err == nil {
		t.Fatal("expected missing questions to fail")
	}
	if !strings.Contains(err.Error(), "decode ToolRequestUserInputParams.questions: missing required field") {
		t.Fatalf("unexpected missing questions error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"itemId":"item-1","questions":[],"threadId":"thread-1","turnId":"turn-1","extra":true}`), &params)
	if err == nil {
		t.Fatal("expected unknown field to fail")
	}
	if !strings.Contains(err.Error(), `decode ToolRequestUserInputParams: unknown field "extra"`) {
		t.Fatalf("unexpected unknown field error: %v", err)
	}

	var question ToolRequestUserInputQuestion
	err = json.Unmarshal([]byte(`{"header":"Auth","id":"token","question":"Which account should be used?","options":[{"description":"Use it","label":"Use","extra":true}]}`), &question)
	if err == nil {
		t.Fatal("expected unknown nested option field to fail")
	}
	if !strings.Contains(err.Error(), `decode ToolRequestUserInputOption: unknown field "extra"`) {
		t.Fatalf("unexpected unknown nested option error: %v", err)
	}
}

func TestGeneratedToolRequestUserInputResponseMarshalAnswers(t *testing.T) {
	raw, err := json.Marshal(ToolRequestUserInputResponse{
		Answers: map[string]ToolRequestUserInputAnswer{
			"choice": {Answers: []string{"Configured", "Other"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"answers":{"choice":{"answers":["Configured","Other"]}}}`
	if got := string(raw); got != want {
		t.Fatalf("ToolRequestUserInputResponse JSON = %s, want %s", got, want)
	}
}

func TestGeneratedToolRequestUserInputResponseUnmarshalAnswers(t *testing.T) {
	var response ToolRequestUserInputResponse
	if err := json.Unmarshal([]byte(`{"answers":{"choice":{"answers":["Configured"]}}}`), &response); err != nil {
		t.Fatal(err)
	}
	answer := response.Answers["choice"]
	if len(answer.Answers) != 1 || answer.Answers[0] != "Configured" {
		t.Fatalf("decoded answers = %#v", response.Answers)
	}
}

func TestGeneratedToolRequestUserInputResponseRejectsMalformedProtocol(t *testing.T) {
	var response ToolRequestUserInputResponse
	err := json.Unmarshal([]byte(`{}`), &response)
	if err == nil {
		t.Fatal("expected missing answers to fail")
	}
	if !strings.Contains(err.Error(), "decode ToolRequestUserInputResponse.answers: missing required field") {
		t.Fatalf("unexpected missing answers error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"answers":null}`), &response)
	if err == nil {
		t.Fatal("expected null answers to fail")
	}
	if !strings.Contains(err.Error(), "decode ToolRequestUserInputResponse.answers: null is not allowed") {
		t.Fatalf("unexpected null answers error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"answers":{},"extra":true}`), &response)
	if err == nil {
		t.Fatal("expected unknown response field to fail")
	}
	if !strings.Contains(err.Error(), `decode ToolRequestUserInputResponse: unknown field "extra"`) {
		t.Fatalf("unexpected unknown response field error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"answers":{"choice":{}}}`), &response)
	if err == nil {
		t.Fatal("expected missing nested answer field to fail")
	}
	if !strings.Contains(err.Error(), "decode ToolRequestUserInputAnswer.answers: missing required field") {
		t.Fatalf("unexpected missing nested answer error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"answers":{"choice":{"answers":["Configured"],"extra":true}}}`), &response)
	if err == nil {
		t.Fatal("expected unknown nested answer field to fail")
	}
	if !strings.Contains(err.Error(), `decode ToolRequestUserInputAnswer: unknown field "extra"`) {
		t.Fatalf("unexpected unknown nested answer error: %v", err)
	}

	_, err = json.Marshal(ToolRequestUserInputResponse{})
	if err == nil {
		t.Fatal("expected nil answers marshal to fail")
	}
	if !strings.Contains(err.Error(), "encode ToolRequestUserInputResponse.answers: nil is not allowed") {
		t.Fatalf("unexpected nil answers marshal error: %v", err)
	}
}

func TestGeneratedLoginAccountParamsMarshalAndAccessors(t *testing.T) {
	streamlined := true
	params := NewLoginAccountParamsChatGPT(LoginAccountParamsChatGPT{
		CodexStreamlinedLogin: &streamlined,
	})
	if params.Kind() != LoginAccountParamsKindChatGPT {
		t.Fatalf("LoginAccountParams kind = %s, want %s", params.Kind(), LoginAccountParamsKindChatGPT)
	}
	if !params.IsValid() {
		t.Fatal("constructed LoginAccountParams should be valid")
	}
	payload, ok := params.AsChatGPT()
	if !ok || payload.CodexStreamlinedLogin == nil || !*payload.CodexStreamlinedLogin {
		t.Fatalf("AsChatGPT payload = %#v, ok=%t", payload, ok)
	}
	if _, ok := params.AsAPIKey(); ok {
		t.Fatal("AsAPIKey returned true for chatgpt variant")
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"codexStreamlinedLogin":true,"type":"chatgpt"}`; got != want {
		t.Fatalf("LoginAccountParams JSON = %s, want %s", got, want)
	}
}

func TestGeneratedLoginAccountParamsNullableVariantRoundTrip(t *testing.T) {
	params := NewLoginAccountParamsChatGPTAuthTokens(LoginAccountParamsChatGPTAuthTokens{
		AccessToken:      "token-1",
		ChatGPTAccountID: "account-1",
		ChatGPTPlanType:  Null[string](),
	})
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"accessToken":"token-1","chatgptAccountId":"account-1","chatgptPlanType":null,"type":"chatgptAuthTokens"}`
	if got := string(raw); got != want {
		t.Fatalf("LoginAccountParams auth tokens JSON = %s, want %s", got, want)
	}

	var decoded LoginAccountParams
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	payload, ok := decoded.AsChatGPTAuthTokens()
	if !ok {
		t.Fatal("decoded LoginAccountParams was not chatgptAuthTokens")
	}
	if payload.AccessToken != "token-1" || payload.ChatGPTAccountID != "account-1" {
		t.Fatalf("decoded auth token payload = %#v", payload)
	}
	if payload.ChatGPTPlanType == nil || payload.ChatGPTPlanType.Value != nil {
		t.Fatalf("decoded ChatGPTPlanType = %#v, want explicit null", payload.ChatGPTPlanType)
	}
}

func TestGeneratedLoginAccountResponseUnmarshalAndMarshal(t *testing.T) {
	var response LoginAccountResponse
	if err := json.Unmarshal([]byte(`{
		"authUrl":"https://example.test/auth",
		"loginId":"login-1",
		"type":"chatgpt"
	}`), &response); err != nil {
		t.Fatal(err)
	}
	payload, ok := response.AsChatGPT()
	if !ok {
		t.Fatal("decoded LoginAccountResponse was not chatgpt")
	}
	if payload.AuthURL != "https://example.test/auth" || payload.LoginID != "login-1" {
		t.Fatalf("decoded response payload = %#v", payload)
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"authUrl":"https://example.test/auth","loginId":"login-1","type":"chatgpt"}`; got != want {
		t.Fatalf("LoginAccountResponse JSON = %s, want %s", got, want)
	}
}

func TestGeneratedLoginAccountUnionRejectsZeroValueMarshal(t *testing.T) {
	_, err := json.Marshal(LoginAccountParams{})
	if err == nil {
		t.Fatal("expected zero-value LoginAccountParams marshal to fail")
	}
	if !strings.Contains(err.Error(), "invalid LoginAccountParams union value: no variant is set") {
		t.Fatalf("unexpected zero-value union error: %v", err)
	}
}

func TestGeneratedLoginAccountUnionRejectsUnknownDiscriminator(t *testing.T) {
	var params LoginAccountParams
	err := json.Unmarshal([]byte(`{"type":"unknown"}`), &params)
	if err == nil {
		t.Fatal("expected unknown LoginAccountParams type to fail")
	}
	if !strings.Contains(err.Error(), `decode LoginAccountParams.type: unknown variant "unknown"`) {
		t.Fatalf("unexpected unknown variant error: %v", err)
	}
}

func TestGeneratedLoginAccountUnionRejectsMissingDiscriminator(t *testing.T) {
	var params LoginAccountParams
	err := json.Unmarshal([]byte(`{"apiKey":"key-1"}`), &params)
	if err == nil {
		t.Fatal("expected missing LoginAccountParams type to fail")
	}
	if !strings.Contains(err.Error(), "decode LoginAccountParams.type: missing required field") {
		t.Fatalf("unexpected missing discriminator error: %v", err)
	}
}

func TestGeneratedLoginAccountUnionRejectsInvalidPayload(t *testing.T) {
	var params LoginAccountParams
	err := json.Unmarshal([]byte(`{"type":"apiKey"}`), &params)
	if err == nil {
		t.Fatal("expected missing apiKey payload to fail")
	}
	if !strings.Contains(err.Error(), "decode LoginAccountParams.apiKey: missing required field") {
		t.Fatalf("unexpected missing payload error: %v", err)
	}

	err = json.Unmarshal([]byte(`{"type":"apiKey","apiKey":"key-1","extra":true}`), &params)
	if err == nil {
		t.Fatal("expected unknown apiKey payload field to fail")
	}
	if !strings.Contains(err.Error(), `decode LoginAccountParams.apiKey: unknown field "extra"`) {
		t.Fatalf("unexpected unknown payload field error: %v", err)
	}
}

func TestGeneratedServerRequestResolvedNotificationMarshalAndUnmarshal(t *testing.T) {
	raw, err := json.Marshal(ServerRequestResolvedNotification{
		RequestID: NewRequestIdString("request-1"),
		ThreadID:  "thread-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"requestId":"request-1","threadId":"thread-1"}`; got != want {
		t.Fatalf("ServerRequestResolvedNotification JSON = %s, want %s", got, want)
	}

	var decoded ServerRequestResolvedNotification
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	requestID, ok := decoded.RequestID.AsString()
	if !ok || requestID != "request-1" || decoded.ThreadID != "thread-1" {
		t.Fatalf("decoded ServerRequestResolvedNotification = %#v", decoded)
	}

	err = json.Unmarshal([]byte(`{"threadId":"thread-1"}`), &decoded)
	if err == nil {
		t.Fatal("expected missing request id to fail")
	}
	if !strings.Contains(err.Error(), "decode ServerRequestResolvedNotification.requestId: missing required field") {
		t.Fatalf("unexpected missing request id error: %v", err)
	}
}

func TestGeneratedClientRequestMarshalAndUnmarshal(t *testing.T) {
	start := NewClientRequestThreadStart(ClientRequestThreadStart{
		ID: NewRequestIdString("go-sdk-1"),
		Params: ThreadStartParams{
			Config: Null[map[string]JSONValue](),
		},
	})
	raw, err := json.Marshal(start)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"id":"go-sdk-1","method":"thread/start","params":{"config":null}}`; got != want {
		t.Fatalf("ClientRequest thread/start JSON = %s, want %s", got, want)
	}
	var decodedStart ClientRequest
	if err := json.Unmarshal(raw, &decodedStart); err != nil {
		t.Fatal(err)
	}
	if decodedStart.Kind() != ClientRequestKindThreadStart || !decodedStart.IsValid() {
		t.Fatalf("decoded ClientRequest thread/start kind/isValid = %s/%t", decodedStart.Kind(), decodedStart.IsValid())
	}
	startPayload, ok := decodedStart.AsThreadStart()
	if !ok || startPayload.ID.Kind() != RequestIdKindString || startPayload.Params.Config == nil {
		t.Fatalf("decoded ClientRequest thread/start = %#v, ok=%t", startPayload, ok)
	}

	reset := NewClientRequestMemoryReset(ClientRequestMemoryReset{ID: NewRequestIdInt64(7)})
	resetRaw, err := json.Marshal(reset)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(resetRaw), `{"id":7,"method":"memory/reset"}`; got != want {
		t.Fatalf("ClientRequest memory/reset JSON = %s, want %s", got, want)
	}
	var decodedReset ClientRequest
	if err := json.Unmarshal([]byte(`{"id":7,"method":"memory/reset","params":null}`), &decodedReset); err != nil {
		t.Fatal(err)
	}
	if payload, ok := decodedReset.AsMemoryReset(); !ok || payload.ID.Kind() != RequestIdKindInt64 {
		t.Fatalf("decoded ClientRequest memory/reset = %#v, ok=%t", payload, ok)
	}

	enable := NewClientRequestRemoteControlEnable(ClientRequestRemoteControlEnable{
		ID: NewRequestIdString("remote-1"),
	})
	enableRaw, err := json.Marshal(enable)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(enableRaw), `{"id":"remote-1","method":"remoteControl/enable"}`; got != want {
		t.Fatalf("ClientRequest remoteControl/enable JSON = %s, want %s", got, want)
	}
	var decodedEnable ClientRequest
	if err := json.Unmarshal(enableRaw, &decodedEnable); err != nil {
		t.Fatal(err)
	}
	enablePayload, ok := decodedEnable.AsRemoteControlEnable()
	if !ok {
		t.Fatalf("decoded ClientRequest remoteControl/enable = %#v, ok=%t", enablePayload, ok)
	}
	if enablePayload.ID.Kind() != RequestIdKindString {
		t.Fatalf("decoded ClientRequest remoteControl/enable = %#v, ok=%t", enablePayload, ok)
	}

	var decodedDisable ClientRequest
	if err := json.Unmarshal([]byte(`{"id":"remote-2","method":"remoteControl/disable","params":null}`), &decodedDisable); err != nil {
		t.Fatal(err)
	}
	disablePayload, ok := decodedDisable.AsRemoteControlDisable()
	if !ok || disablePayload.ID.Kind() != RequestIdKindString {
		t.Fatalf("decoded ClientRequest remoteControl/disable = %#v, ok=%t", disablePayload, ok)
	}
}

func TestGeneratedServerRequestMarshalAndUnmarshal(t *testing.T) {
	request := NewServerRequestItemCommandExecutionRequestApproval(ServerRequestItemCommandExecutionRequestApproval{
		ID: NewRequestIdString("approval-1"),
		Params: CommandExecutionRequestApprovalParams{
			ItemID:      "item-1",
			StartedAtMS: 123,
			ThreadID:    "thread-1",
			TurnID:      "turn-1",
		},
	})
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"approval-1","method":"item/commandExecution/requestApproval","params":{"itemId":"item-1","startedAtMs":123,"threadId":"thread-1","turnId":"turn-1"}}`
	if got := string(raw); got != want {
		t.Fatalf("ServerRequest item/commandExecution/requestApproval JSON = %s, want %s", got, want)
	}
	var decoded ServerRequest
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Kind() != ServerRequestKindItemCommandExecutionRequestApproval || !decoded.IsValid() {
		t.Fatalf("decoded ServerRequest kind/isValid = %s/%t", decoded.Kind(), decoded.IsValid())
	}
	payload, ok := decoded.AsItemCommandExecutionRequestApproval()
	if !ok || payload.Params.ItemID != "item-1" || payload.Params.ThreadID != "thread-1" {
		t.Fatalf("decoded ServerRequest payload = %#v, ok=%t", payload, ok)
	}
}

func TestGeneratedRequestAggregatesRejectMalformedProtocol(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		target  any
		wantErr string
	}{
		{
			name:    "zero value client marshal",
			target:  ClientRequest{},
			wantErr: "invalid ClientRequest union value: no variant is set",
		},
		{
			name:    "zero value server marshal",
			target:  ServerRequest{},
			wantErr: "invalid ServerRequest union value: no variant is set",
		},
		{
			name:    "client unknown method",
			input:   `{"id":"go-sdk-1","method":"bogus","params":{}}`,
			target:  &ClientRequest{},
			wantErr: `decode ClientRequest.method: unknown variant "bogus"`,
		},
		{
			name:    "client missing method",
			input:   `{"id":"go-sdk-1","params":{}}`,
			target:  &ClientRequest{},
			wantErr: "decode ClientRequest.method: missing required field",
		},
		{
			name:    "client missing id",
			input:   `{"method":"thread/start","params":{}}`,
			target:  &ClientRequest{},
			wantErr: "decode ClientRequest.id: missing required field",
		},
		{
			name:    "client missing params",
			input:   `{"id":"go-sdk-1","method":"thread/start"}`,
			target:  &ClientRequest{},
			wantErr: "decode ClientRequest.params: missing required field",
		},
		{
			name:    "client no params request rejects non-null params",
			input:   `{"id":"go-sdk-1","method":"memory/reset","params":{}}`,
			target:  &ClientRequest{},
			wantErr: "decode ClientRequest.memory/reset.params: expected null",
		},
		{
			name:    "client unexpected field",
			input:   `{"id":"go-sdk-1","method":"thread/start","params":{},"extra":true}`,
			target:  &ClientRequest{},
			wantErr: `decode ClientRequest.thread/start: unknown field "extra"`,
		},
		{
			name:    "server unknown method",
			input:   `{"id":"approval-1","method":"bogus","params":{}}`,
			target:  &ServerRequest{},
			wantErr: `decode ServerRequest.method: unknown variant "bogus"`,
		},
		{
			name:    "server missing params",
			input:   `{"id":"approval-1","method":"item/commandExecution/requestApproval"}`,
			target:  &ServerRequest{},
			wantErr: "decode ServerRequest.params: missing required field",
		},
		{
			name:    "server unexpected field",
			input:   `{"id":"approval-1","method":"item/commandExecution/requestApproval","params":{"itemId":"item-1","startedAtMs":123,"threadId":"thread-1","turnId":"turn-1"},"extra":true}`,
			target:  &ServerRequest{},
			wantErr: `decode ServerRequest.item/commandExecution/requestApproval: unknown field "extra"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.input == "" {
				_, err = json.Marshal(tc.target)
			} else {
				err = json.Unmarshal([]byte(tc.input), tc.target)
			}
			if err == nil {
				t.Fatalf("expected %s to fail", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("unexpected %s error: %v", tc.name, err)
			}
		})
	}
}

func TestGeneratedRequestIdStringMarshalAndAccessors(t *testing.T) {
	id := NewRequestIdString("request-1")
	if id.Kind() != RequestIdKindString {
		t.Fatalf("RequestId kind = %s, want %s", id.Kind(), RequestIdKindString)
	}
	if !id.IsValid() {
		t.Fatal("constructed string RequestId should be valid")
	}
	value, ok := id.AsString()
	if !ok || value != "request-1" {
		t.Fatalf("AsString = %q, %t", value, ok)
	}
	if value, ok := id.AsInt64(); ok || value != 0 {
		t.Fatalf("AsInt64 = %d, %t; want zero false", value, ok)
	}
	raw, err := json.Marshal(id)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `"request-1"`; got != want {
		t.Fatalf("RequestId string JSON = %s, want %s", got, want)
	}

	var decoded RequestId
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if value, ok := decoded.AsString(); !ok || value != "request-1" {
		t.Fatalf("decoded AsString = %q, %t", value, ok)
	}
}

func TestGeneratedRequestIdInt64MarshalAndAccessors(t *testing.T) {
	id := NewRequestIdInt64(42)
	if id.Kind() != RequestIdKindInt64 {
		t.Fatalf("RequestId kind = %s, want %s", id.Kind(), RequestIdKindInt64)
	}
	if !id.IsValid() {
		t.Fatal("constructed int64 RequestId should be valid")
	}
	value, ok := id.AsInt64()
	if !ok || value != 42 {
		t.Fatalf("AsInt64 = %d, %t", value, ok)
	}
	raw, err := json.Marshal(id)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `42`; got != want {
		t.Fatalf("RequestId int64 JSON = %s, want %s", got, want)
	}

	var decoded RequestId
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if value, ok := decoded.AsInt64(); !ok || value != 42 {
		t.Fatalf("decoded AsInt64 = %d, %t", value, ok)
	}
}

func TestGeneratedRequestIdRejectsZeroValueMarshal(t *testing.T) {
	_, err := json.Marshal(RequestId{})
	if err == nil {
		t.Fatal("expected zero-value RequestId marshal to fail")
	}
	if !strings.Contains(err.Error(), "invalid RequestId union value: no variant is set") {
		t.Fatalf("unexpected zero-value RequestId error: %v", err)
	}
}

func TestGeneratedRequestIdRejectsNonInt64Number(t *testing.T) {
	var id RequestId
	err := json.Unmarshal([]byte(`1.5`), &id)
	if err == nil {
		t.Fatal("expected float RequestId to fail")
	}
	if !strings.Contains(err.Error(), "decode RequestId: expected int64 JSON number") {
		t.Fatalf("unexpected float RequestId error: %v", err)
	}
}

func TestGeneratedRequestIdRejectsInt64Overflow(t *testing.T) {
	var id RequestId
	err := json.Unmarshal([]byte(`9223372036854775808`), &id)
	if err == nil {
		t.Fatal("expected overflowing RequestId to fail")
	}
	if !strings.Contains(err.Error(), "decode RequestId: expected int64 JSON number") {
		t.Fatalf("unexpected overflowing RequestId error: %v", err)
	}
}

func TestGeneratedRequestIdRejectsWrongJSONKind(t *testing.T) {
	var id RequestId
	err := json.Unmarshal([]byte(`null`), &id)
	if err == nil {
		t.Fatal("expected null RequestId to fail")
	}
	if !strings.Contains(err.Error(), "decode RequestId: expected string or int64 JSON number") {
		t.Fatalf("unexpected null RequestId error: %v", err)
	}
}

func TestGeneratedClientNotificationMarshalAndAccessors(t *testing.T) {
	notification := NewClientNotificationInitialized()
	if notification.Kind() != ClientNotificationKindInitialized {
		t.Fatalf("ClientNotification kind = %s, want %s", notification.Kind(), ClientNotificationKindInitialized)
	}
	if !notification.IsValid() {
		t.Fatal("constructed ClientNotification should be valid")
	}
	if _, ok := notification.AsInitialized(); !ok {
		t.Fatal("AsInitialized returned false for initialized notification")
	}
	raw, err := json.Marshal(notification)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"method":"initialized"}`; got != want {
		t.Fatalf("ClientNotification JSON = %s, want %s", got, want)
	}

	var decoded ClientNotification
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Kind() != ClientNotificationKindInitialized {
		t.Fatalf("decoded ClientNotification kind = %s", decoded.Kind())
	}
}

func TestGeneratedClientNotificationRejectsZeroValueMarshal(t *testing.T) {
	_, err := json.Marshal(ClientNotification{})
	if err == nil {
		t.Fatal("expected zero-value ClientNotification marshal to fail")
	}
	if !strings.Contains(err.Error(), "invalid ClientNotification union value: no variant is set") {
		t.Fatalf("unexpected zero-value ClientNotification error: %v", err)
	}
}

func TestGeneratedClientNotificationRejectsUnknownMethod(t *testing.T) {
	var notification ClientNotification
	err := json.Unmarshal([]byte(`{"method":"bogus"}`), &notification)
	if err == nil {
		t.Fatal("expected unknown ClientNotification method to fail")
	}
	if !strings.Contains(err.Error(), `decode ClientNotification.method: unknown variant "bogus"`) {
		t.Fatalf("unexpected unknown method error: %v", err)
	}
}

func TestGeneratedClientNotificationRejectsMissingMethod(t *testing.T) {
	var notification ClientNotification
	err := json.Unmarshal([]byte(`{}`), &notification)
	if err == nil {
		t.Fatal("expected missing ClientNotification method to fail")
	}
	if !strings.Contains(err.Error(), "decode ClientNotification.method: missing required field") {
		t.Fatalf("unexpected missing method error: %v", err)
	}
}

func TestGeneratedClientNotificationRejectsNullMethod(t *testing.T) {
	var notification ClientNotification
	err := json.Unmarshal([]byte(`{"method":null}`), &notification)
	if err == nil {
		t.Fatal("expected null ClientNotification method to fail")
	}
	if !strings.Contains(err.Error(), "decode ClientNotification.method: null is not allowed") {
		t.Fatalf("unexpected null method error: %v", err)
	}
}

func TestGeneratedClientNotificationRejectsUnexpectedPayload(t *testing.T) {
	var notification ClientNotification
	err := json.Unmarshal([]byte(`{"method":"initialized","params":{}}`), &notification)
	if err == nil {
		t.Fatal("expected unexpected ClientNotification payload to fail")
	}
	if !strings.Contains(err.Error(), `decode ClientNotification.initialized: unknown field "params"`) {
		t.Fatalf("unexpected payload error: %v", err)
	}
}

func TestGeneratedServerNotificationMarshalAndAccessors(t *testing.T) {
	errorNotification := NewServerNotificationError(ServerNotificationError{
		Params: ErrorNotification{
			Error:     TurnError{Message: "failed"},
			ThreadID:  "thread-1",
			TurnID:    "turn-1",
			WillRetry: false,
		},
	})
	if errorNotification.Kind() != ServerNotificationKindError || !errorNotification.IsValid() {
		t.Fatalf("ServerNotification error kind/isValid = %s/%t", errorNotification.Kind(), errorNotification.IsValid())
	}
	errorRaw, err := json.Marshal(errorNotification)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(errorRaw), `{"method":"error","params":{"error":{"message":"failed"},"threadId":"thread-1","turnId":"turn-1","willRetry":false}}`; got != want {
		t.Fatalf("ServerNotification error JSON = %s, want %s", got, want)
	}
	var decodedError ServerNotification
	if err := json.Unmarshal(errorRaw, &decodedError); err != nil {
		t.Fatal(err)
	}
	decodedErrorPayload, ok := decodedError.AsError()
	if !ok || decodedErrorPayload.Params.Error.Message != "failed" || decodedErrorPayload.Params.ThreadID != "thread-1" {
		t.Fatalf("decoded ServerNotification error = %#v, ok=%t", decodedErrorPayload, ok)
	}

	tokenUsage := NewServerNotificationThreadTokenUsageUpdated(ServerNotificationThreadTokenUsageUpdated{
		Params: ThreadTokenUsageUpdatedNotification{
			ThreadID: "thread-1",
			TokenUsage: ThreadTokenUsage{
				Last:  TokenUsageBreakdown{CachedInputTokens: 1, InputTokens: 3, OutputTokens: 2, ReasoningOutputTokens: 1, TotalTokens: 5},
				Total: TokenUsageBreakdown{CachedInputTokens: 10, InputTokens: 30, OutputTokens: 20, ReasoningOutputTokens: 5, TotalTokens: 50},
			},
			TurnID: "turn-1",
		},
	})
	tokenUsageRaw, err := json.Marshal(tokenUsage)
	if err != nil {
		t.Fatal(err)
	}
	wantTokenUsage := `{"method":"thread/tokenUsage/updated","params":{"threadId":"thread-1","tokenUsage":{"last":{"cachedInputTokens":1,"inputTokens":3,"outputTokens":2,"reasoningOutputTokens":1,"totalTokens":5},"total":{"cachedInputTokens":10,"inputTokens":30,"outputTokens":20,"reasoningOutputTokens":5,"totalTokens":50}},"turnId":"turn-1"}}`
	if got := string(tokenUsageRaw); got != wantTokenUsage {
		t.Fatalf("ServerNotification token usage JSON = %s, want %s", got, wantTokenUsage)
	}
	var decodedTokenUsage ServerNotification
	if err := json.Unmarshal(tokenUsageRaw, &decodedTokenUsage); err != nil {
		t.Fatal(err)
	}
	tokenUsagePayload, ok := decodedTokenUsage.AsThreadTokenUsageUpdated()
	if !ok || tokenUsagePayload.Params.TokenUsage.Total.TotalTokens != 50 {
		t.Fatalf("decoded ServerNotification token usage = %#v, ok=%t", tokenUsagePayload, ok)
	}

	sdp := NewServerNotificationThreadRealtimeSDP(ServerNotificationThreadRealtimeSDP{
		Params: ThreadRealtimeSdpNotification{SDP: "offer", ThreadID: "thread-1"},
	})
	sdpRaw, err := json.Marshal(sdp)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(sdpRaw), `{"method":"thread/realtime/sdp","params":{"sdp":"offer","threadId":"thread-1"}}`; got != want {
		t.Fatalf("ServerNotification realtime sdp JSON = %s, want %s", got, want)
	}
	var decodedSDP ServerNotification
	if err := json.Unmarshal(sdpRaw, &decodedSDP); err != nil {
		t.Fatal(err)
	}
	sdpPayload, ok := decodedSDP.AsThreadRealtimeSDP()
	if !ok || sdpPayload.Params.SDP != "offer" {
		t.Fatalf("decoded ServerNotification sdp = %#v, ok=%t", sdpPayload, ok)
	}

	skillsChanged := NewServerNotificationSkillsChanged(ServerNotificationSkillsChanged{Params: SkillsChangedNotification{}})
	skillsChangedRaw, err := json.Marshal(skillsChanged)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(skillsChangedRaw), `{"method":"skills/changed","params":{}}`; got != want {
		t.Fatalf("ServerNotification empty params JSON = %s, want %s", got, want)
	}
}

func TestGeneratedServerNotificationRejectsMalformedProtocol(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:    "zero value marshal",
			wantErr: "invalid ServerNotification union value: no variant is set",
		},
		{
			name:    "unknown method",
			input:   `{"method":"bogus","params":{}}`,
			wantErr: `decode ServerNotification.method: unknown variant "bogus"`,
		},
		{
			name:    "missing method",
			input:   `{"params":{}}`,
			wantErr: "decode ServerNotification.method: missing required field",
		},
		{
			name:    "null method",
			input:   `{"method":null,"params":{}}`,
			wantErr: "decode ServerNotification.method: null is not allowed",
		},
		{
			name:    "missing params",
			input:   `{"method":"skills/changed"}`,
			wantErr: "decode ServerNotification.params: missing required field",
		},
		{
			name:    "null params",
			input:   `{"method":"skills/changed","params":null}`,
			wantErr: "decode ServerNotification.params: null is not allowed",
		},
		{
			name:    "unexpected top-level field",
			input:   `{"method":"skills/changed","params":{},"extra":true}`,
			wantErr: `decode ServerNotification.skills/changed: unknown field "extra"`,
		},
		{
			name:    "nested params unknown field",
			input:   `{"method":"thread/realtime/sdp","params":{"sdp":"offer","threadId":"thread-1","extra":true}}`,
			wantErr: `decode ServerNotification.params: decode ThreadRealtimeSdpNotification: unknown field "extra"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.input == "" {
				_, err = json.Marshal(ServerNotification{})
			} else {
				var notification ServerNotification
				err = json.Unmarshal([]byte(tc.input), &notification)
			}
			if err == nil {
				t.Fatalf("expected %s to fail", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("unexpected %s error: %v", tc.name, err)
			}
		})
	}
}
