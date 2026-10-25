package codexsdk

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func TestEveryGeneratedServerNotificationKindHasAttribution(t *testing.T) {
	for _, method := range protocolv2.AllMethods() {
		if method.Direction != protocolv2.MethodDirectionServerToClient || method.Kind != protocolv2.MethodKindNotification {
			continue
		}
		kind := protocolv2.ServerNotificationKind(method.Method)
		if !knownServerNotificationKind(kind) {
			t.Errorf("generated notification %q is not a known server notification", kind)
		}
		if class := attributionClassForKind(kind); class == notificationAttributionUnsupported {
			t.Errorf("generated notification %q has no attribution class", kind)
		}
	}
}

func TestUnknownFutureNotificationKindFailsClosed(t *testing.T) {
	future := protocolv2.ServerNotificationKind("future/generated-notification")
	if got := attributionClassForKind(future); got != notificationAttributionUnsupported {
		t.Fatalf("future kind attribution = %v, want unsupported until explicitly classified", got)
	}
}

func TestNotificationRoutingIgnoresMethodsOutsideKnownServerNotifications(t *testing.T) {
	for _, test := range []struct {
		name   string
		method string
	}{
		{name: "unknown method", method: "future/server-observation"},
		{name: "known client request", method: protocolv2.MethodThreadRead},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := &Client{
				ctx:            context.Background(),
				notifications:  make(chan acceptedNotification, 1),
				exactStreams:   map[string]map[*exactRunState]struct{}{},
				exactAttaching: map[string]map[*exactRunState]struct{}{},
			}
			c.routeNotification(rpcNotification{
				method: test.method,
				params: map[string]any{"future": true},
			})

			if c.isClosed() {
				t.Fatalf("unsupported notification method closed the client: %v", c.failure)
			}
			select {
			case accepted := <-c.notifications:
				t.Fatalf("unsupported notification method reached typed handler queue as %s", accepted.notification.Kind())
			default:
			}
		})
	}
}

func TestExactNotificationAcceptsAdditionalMembersRecursively(t *testing.T) {
	typed, err := exactNotification(rpcNotification{
		method: protocolv2.MethodAccountRateLimitsUpdated,
		params: map[string]any{
			"futureParamsMember": true,
			"rateLimits": map[string]any{
				"futureSnapshotMember": true,
				"secondary": map[string]any{
					"futureWindowMember": true,
					"usedPercent":        7,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, ok := typed.AsAccountRateLimitsUpdated()
	if !ok || updated.Params.RateLimits.Secondary == nil || updated.Params.RateLimits.Secondary.Value == nil || updated.Params.RateLimits.Secondary.Value.UsedPercent != 7 {
		t.Fatalf("decoded account/rateLimits/updated = %#v, ok=%t", updated, ok)
	}
}

func TestIdentifierFreeGlobalFamiliesOnlyReachGlobalQueue(t *testing.T) {
	c := &Client{
		ctx: context.Background(), notifications: make(chan acceptedNotification, 3),
		exactStreams: map[string]map[*exactRunState]struct{}{}, exactAttaching: map[string]map[*exactRunState]struct{}{},
	}
	first := newExactRunState(c, "thread-a", StartedThreadRun{})
	first.turnID = "turn-a"
	second := newExactRunState(c, "thread-b", StartedThreadRun{})
	second.turnID = "turn-b"
	c.exactStreams[first.turnID] = map[*exactRunState]struct{}{first: {}}
	c.exactStreams[second.turnID] = map[*exactRunState]struct{}{second: {}}
	inputs := []rpcNotification{
		{method: "account/updated", params: map[string]any{}},
		{method: "account/rateLimits/updated", params: map[string]any{"rateLimits": map[string]any{}}},
		{method: "configWarning", params: map[string]any{"summary": "check config"}},
	}
	wants := []protocolv2.ServerNotification{
		protocolv2.NewServerNotificationAccountUpdated(protocolv2.ServerNotificationAccountUpdated{Params: protocolv2.AccountUpdatedNotification{}}),
		protocolv2.NewServerNotificationAccountRateLimitsUpdated(protocolv2.ServerNotificationAccountRateLimitsUpdated{Params: protocolv2.AccountRateLimitsUpdatedNotification{RateLimits: protocolv2.RateLimitSnapshot{}}}),
		protocolv2.NewServerNotificationConfigWarning(protocolv2.ServerNotificationConfigWarning{Params: protocolv2.ConfigWarningNotification{Summary: "check config"}}),
	}
	for index, input := range inputs {
		c.routeNotification(input)
		got := <-c.notifications
		gotJSON, gotErr := got.notification.MarshalJSON()
		wantJSON, wantErr := wants[index].MarshalJSON()
		if gotErr != nil || wantErr != nil || string(gotJSON) != string(wantJSON) {
			t.Fatalf("global queue value = %s (%v), want independently constructed %s (%v)", gotJSON, gotErr, wantJSON, wantErr)
		}
	}
	if len(exactNotificationKinds(first)) != 0 || len(exactNotificationKinds(second)) != 0 {
		t.Fatalf("global families contaminated runs: %#v / %#v", exactNotificationKinds(first), exactNotificationKinds(second))
	}
}

func TestConcurrentAttributionDoesNotDuplicateOrCrossRuns(t *testing.T) {
	c := &Client{exactStreams: map[string]map[*exactRunState]struct{}{}, exactAttaching: map[string]map[*exactRunState]struct{}{}}
	first := newExactRunState(c, "thread-shared", StartedThreadRun{})
	first.turnID = "turn-1"
	second := newExactRunState(c, "thread-shared", StartedThreadRun{})
	second.turnID = "turn-2"
	other := newExactRunState(c, "thread-other", StartedThreadRun{})
	other.turnID = "turn-3"
	for _, state := range []*exactRunState{first, second, other} {
		c.exactStreams[state.turnID] = map[*exactRunState]struct{}{state: {}}
	}
	const repetitions = 20
	var wg sync.WaitGroup
	route := func(n rpcNotification) {
		defer wg.Done()
		typed, err := exactNotification(n)
		if err != nil {
			t.Error(err)
			return
		}
		c.routeExactNotification(n, typed)
	}
	for index := 0; index < repetitions; index++ {
		for _, state := range []*exactRunState{first, second, other} {
			wg.Add(1)
			go route(rpcNotification{method: "model/rerouted", params: map[string]any{
				"threadId": state.threadID, "turnId": state.turnID, "fromModel": "a", "toModel": "b", "reason": "highRiskCyberActivity",
			}})
		}
		wg.Add(1)
		go route(rpcNotification{method: "guardianWarning", params: map[string]any{"threadId": "thread-shared", "message": "notice"}})
	}
	wg.Wait()
	if got := len(exactNotificationKinds(first)); got != repetitions*2 {
		t.Fatalf("first evidence count = %d", got)
	}
	if got := len(exactNotificationKinds(second)); got != repetitions*2 {
		t.Fatalf("second evidence count = %d", got)
	}
	if got := len(exactNotificationKinds(other)); got != repetitions {
		t.Fatalf("other evidence count = %d", got)
	}
}

func TestAttributionUsesPresentCorrelationNotSchemaOptionality(t *testing.T) {
	warningPresent, err := exactNotification(rpcNotification{method: "warning", params: map[string]any{"message": "notice", "threadId": "thread-a"}})
	if err != nil {
		t.Fatal(err)
	}
	class, identity := attributionFor(warningPresent)
	if class != notificationAttributionThread || identity.threadID != "thread-a" || identity.turnID != "" {
		t.Fatalf("present warning attribution = (%v, %#v)", class, identity)
	}

	warningAbsent, err := exactNotification(rpcNotification{method: "warning", params: map[string]any{"message": "notice"}})
	if err != nil {
		t.Fatal(err)
	}
	class, identity = attributionFor(warningAbsent)
	if class != notificationAttributionGlobal || identity.threadID != "" {
		t.Fatalf("absent warning attribution = (%v, %#v)", class, identity)
	}

	started := protocolv2.NewServerNotificationThreadStarted(protocolv2.ServerNotificationThreadStarted{
		Params: protocolv2.ThreadStartedNotification{Thread: facadeThread("thread-nested", nil)},
	})
	class, identity = attributionFor(started)
	if class != notificationAttributionThread || identity.threadID != "thread-nested" {
		t.Fatalf("nested thread/started attribution = (%v, %#v)", class, identity)
	}

	mcp, err := exactNotification(rpcNotification{method: "mcpServer/startupStatus/updated", params: map[string]any{
		"name": "docs", "status": "ready", "threadId": "thread-mcp",
	}})
	if err != nil {
		t.Fatal(err)
	}
	class, identity = attributionFor(mcp)
	if class != notificationAttributionThread || identity.threadID != "thread-mcp" {
		t.Fatalf("mcp startup attribution = (%v, %#v)", class, identity)
	}
}

func TestExactAttributionSeparatesTurnThreadAndGlobalFacts(t *testing.T) {
	c := &Client{
		ctx:            context.Background(),
		notifications:  make(chan acceptedNotification, 8),
		exactStreams:   map[string]map[*exactRunState]struct{}{},
		exactAttaching: map[string]map[*exactRunState]struct{}{},
	}
	first := newExactRunState(c, "thread-a", StartedThreadRun{})
	first.turnID = "turn-a"
	second := newExactRunState(c, "thread-b", StartedThreadRun{})
	second.turnID = "turn-b"
	c.exactStreams[first.turnID] = map[*exactRunState]struct{}{first: {}}
	c.exactStreams[second.turnID] = map[*exactRunState]struct{}{second: {}}

	c.routeNotification(rpcNotification{method: "model/rerouted", params: map[string]any{
		"threadId": first.threadID, "turnId": first.turnID, "fromModel": "a", "toModel": "b", "reason": "highRiskCyberActivity",
	}})
	if got := exactNotificationKinds(first); len(got) != 1 || got[0] != protocolv2.ServerNotificationKindModelRerouted {
		t.Fatalf("matching turn evidence = %#v", got)
	}
	if got := exactNotificationKinds(second); len(got) != 0 {
		t.Fatalf("unrelated turn evidence = %#v", got)
	}

	c.routeNotification(rpcNotification{method: "guardianWarning", params: map[string]any{"threadId": first.threadID, "message": "notice"}})
	if got := exactNotificationKinds(first); len(got) != 2 || got[1] != protocolv2.ServerNotificationKindGuardianWarning {
		t.Fatalf("thread evidence = %#v", got)
	}
	if got := exactNotificationKinds(second); len(got) != 0 {
		t.Fatalf("other-thread evidence = %#v", got)
	}

	c.routeNotification(rpcNotification{method: "skills/changed", params: map[string]any{}})
	if got := exactNotificationKinds(first); len(got) != 2 {
		t.Fatalf("global fact contaminated first run: %#v", got)
	}
	if got := exactNotificationKinds(second); len(got) != 0 {
		t.Fatalf("global fact contaminated second run: %#v", got)
	}
	var globalKinds []protocolv2.ServerNotificationKind
	for len(c.notifications) > 0 {
		globalKinds = append(globalKinds, (<-c.notifications).notification.Kind())
	}
	wantGlobal := []protocolv2.ServerNotificationKind{
		protocolv2.ServerNotificationKindModelRerouted,
		protocolv2.ServerNotificationKindGuardianWarning,
		protocolv2.ServerNotificationKindSkillsChanged,
	}
	if len(globalKinds) != len(wantGlobal) {
		t.Fatalf("global handler queue = %#v, want %#v", globalKinds, wantGlobal)
	}
	for index := range wantGlobal {
		if globalKinds[index] != wantGlobal[index] {
			t.Fatalf("global handler queue = %#v, want %#v", globalKinds, wantGlobal)
		}
	}
}

func TestThreadAttributionReachesEveryCurrentRunOnSameThread(t *testing.T) {
	c := &Client{exactStreams: map[string]map[*exactRunState]struct{}{}, exactAttaching: map[string]map[*exactRunState]struct{}{}}
	first := newExactRunState(c, "thread-shared", StartedThreadRun{})
	first.turnID = "turn-1"
	second := newExactRunState(c, "thread-shared", StartedThreadRun{})
	second.turnID = "turn-2"
	c.exactStreams[first.turnID] = map[*exactRunState]struct{}{first: {}}
	c.exactStreams[second.turnID] = map[*exactRunState]struct{}{second: {}}
	n := rpcNotification{method: "guardianWarning", params: map[string]any{"threadId": "thread-shared", "message": "notice"}}
	typed, err := exactNotification(n)
	if err != nil {
		t.Fatal(err)
	}
	if !c.routeExactNotification(n, typed) {
		t.Fatal("thread fact was not attributed")
	}
	if len(exactNotificationKinds(first)) != 1 || len(exactNotificationKinds(second)) != 1 {
		t.Fatalf("same-thread evidence = %#v / %#v", exactNotificationKinds(first), exactNotificationKinds(second))
	}
}

func TestRunEvidenceAppendPrecedesGlobalHandlerEnqueue(t *testing.T) {
	c := &Client{
		ctx:            context.Background(),
		notifications:  make(chan acceptedNotification, 1),
		exactStreams:   map[string]map[*exactRunState]struct{}{},
		exactAttaching: map[string]map[*exactRunState]struct{}{},
	}
	state := newExactRunState(c, "thread-order", StartedThreadRun{})
	state.turnID = "turn-order"
	c.exactStreams[state.turnID] = map[*exactRunState]struct{}{state: {}}
	atGate := make(chan struct{})
	release := make(chan struct{})
	state.testAtNotificationOrderGate = func() { close(atGate); <-release }
	done := make(chan struct{})
	go func() {
		c.routeNotification(rpcNotification{method: "model/rerouted", params: map[string]any{
			"threadId": state.threadID, "turnId": state.turnID, "fromModel": "a", "toModel": "b", "reason": "highRiskCyberActivity",
		}})
		close(done)
	}()
	select {
	case <-atGate:
	case <-time.After(time.Second):
		t.Fatal("notification did not reach per-run append gate")
	}
	select {
	case got := <-c.notifications:
		t.Fatalf("global handler queue received %s before run append", got.notification.Kind())
	default:
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("notification routing did not finish")
	}
	if len(exactNotificationKinds(state)) != 1 {
		t.Fatal("run evidence was not appended")
	}
	if got := <-c.notifications; got.notification.Kind() != protocolv2.ServerNotificationKindModelRerouted {
		t.Fatalf("global handler queue received %s", got.notification.Kind())
	}
}

func exactNotificationKinds(state *exactRunState) []protocolv2.ServerNotificationKind {
	state.mu.Lock()
	defer state.mu.Unlock()
	result := state.result.(StartedThreadRun)
	kinds := make([]protocolv2.ServerNotificationKind, len(result.Run.Notifications))
	for index, notification := range result.Run.Notifications {
		kinds[index] = notification.Kind()
	}
	return kinds
}

func TestAttributionExtractsGeneratedIdentity(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantClass  notificationAttributionClass
		wantThread string
		wantTurn   string
	}{
		{"turn fields", `{"method":"model/rerouted","params":{"threadId":"thread-a","turnId":"turn-a","fromModel":"a","toModel":"b","reason":"highRiskCyberActivity"}}`, notificationAttributionTurn, "thread-a", "turn-a"},
		{"nested turn", `{"method":"turn/started","params":{"threadId":"thread-b","turn":{"id":"turn-b","items":[],"status":"inProgress"}}}`, notificationAttributionTurn, "thread-b", "turn-b"},
		{"thread only", `{"method":"guardianWarning","params":{"threadId":"thread-c","message":"notice"}}`, notificationAttributionThread, "thread-c", ""},
		{"global", `{"method":"skills/changed","params":{}}`, notificationAttributionGlobal, "", ""},
		{"optional warning thread", `{"method":"warning","params":{"message":"notice","threadId":"thread-w"}}`, notificationAttributionThread, "thread-w", ""},
		{"optional warning absent", `{"method":"warning","params":{"message":"notice"}}`, notificationAttributionGlobal, "", ""},
		{"mcp startup thread", `{"method":"mcpServer/startupStatus/updated","params":{"name":"docs","status":"ready","threadId":"thread-m"}}`, notificationAttributionThread, "thread-m", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var notification protocolv2.ServerNotification
			if err := notification.UnmarshalJSON([]byte(test.raw)); err != nil {
				t.Fatal(err)
			}
			class, identity := attributionFor(notification)
			if class != test.wantClass || identity.threadID != test.wantThread || identity.turnID != test.wantTurn {
				t.Fatalf("attribution = (%v, %#v), want (%v, %q, %q)", class, identity, test.wantClass, test.wantThread, test.wantTurn)
			}
		})
	}
}

func TestWarningAndMCPStartupAttachOnlyWhenThreadIDIsPresent(t *testing.T) {
	c := &Client{
		ctx:            context.Background(),
		notifications:  make(chan acceptedNotification, 8),
		exactStreams:   map[string]map[*exactRunState]struct{}{},
		exactAttaching: map[string]map[*exactRunState]struct{}{},
	}
	matching := newExactRunState(c, "thread-a", StartedThreadRun{})
	matching.turnID = "turn-a"
	other := newExactRunState(c, "thread-b", StartedThreadRun{})
	other.turnID = "turn-b"
	c.exactStreams[matching.turnID] = map[*exactRunState]struct{}{matching: {}}
	c.exactStreams[other.turnID] = map[*exactRunState]struct{}{other: {}}

	c.routeNotification(rpcNotification{method: "warning", params: map[string]any{"message": "notice", "threadId": "thread-a"}})
	if got := exactNotificationKinds(matching); len(got) != 1 || got[0] != protocolv2.ServerNotificationKindWarning {
		t.Fatalf("present warning evidence = %#v", got)
	}
	if got := exactNotificationKinds(other); len(got) != 0 {
		t.Fatalf("unrelated run received warning: %#v", got)
	}

	c.routeNotification(rpcNotification{method: "warning", params: map[string]any{"message": "global"}})
	if got := exactNotificationKinds(matching); len(got) != 1 {
		t.Fatalf("absent warning contaminated run: %#v", got)
	}

	c.routeNotification(rpcNotification{method: "mcpServer/startupStatus/updated", params: map[string]any{"name": "docs", "status": "ready", "threadId": "thread-a"}})
	if got := exactNotificationKinds(matching); len(got) != 2 || got[1] != protocolv2.ServerNotificationKindMCPServerStartupStatusUpdated {
		t.Fatalf("mcp startup evidence = %#v", got)
	}
	if got := exactNotificationKinds(other); len(got) != 0 {
		t.Fatalf("unrelated run received mcp startup: %#v", got)
	}
}

func TestThreadStartedIsPreservedAcrossAttachRegistrationRace(t *testing.T) {
	c := newTransportHarness()
	c.notifications = make(chan acceptedNotification, 8)
	started := protocolv2.NewServerNotificationThreadStarted(protocolv2.ServerNotificationThreadStarted{
		Params: protocolv2.ThreadStartedNotification{Thread: facadeThread("thread-nested", nil)},
	})
	c.testAfterThreadStartResponse = func() {
		raw, err := json.Marshal(started)
		if err != nil {
			t.Error(err)
			return
		}
		var envelope struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Error(err)
			return
		}
		c.routeNotification(rpcNotification{method: envelope.Method, params: envelope.Params})
	}
	done := make(chan *Stream[StartedThreadRun], 1)
	go func() {
		stream, err := c.ThreadRunner().StartStream(context.Background(), StartThreadRunRequest{
			Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{}},
		})
		if err != nil {
			t.Error(err)
		}
		done <- stream
	}()
	startID := waitForWrittenRequest(t, c.stdin.(*recordingWriteCloser), protocolv2.MethodThreadStart, 1)
	c.routeResponse(map[string]any{
		"id":     startID,
		"result": protocolResultMap(t, facadeThreadStartResponse("thread-nested", "model")),
	})
	turnID := waitForWrittenRequest(t, c.stdin.(*recordingWriteCloser), protocolv2.MethodTurnStart, 1)
	c.routeResponse(map[string]any{
		"id": turnID,
		"result": protocolResultMap(t, protocolv2.TurnStartResponse{Turn: protocolv2.Turn{
			ID: "turn-nested", Items: []protocolv2.ThreadItem{}, Status: protocolv2.TurnStatusInProgress,
		}}),
	})
	stream := <-done
	if stream == nil {
		t.Fatal("StartStream returned nil")
	}
	defer stream.Close()
	result, ok := stream.Result()
	if !ok {
		t.Fatal("missing Exact Run result after attach")
	}
	found := false
	for _, notification := range result.Run.Notifications {
		if notification.Kind() == protocolv2.ServerNotificationKindThreadStarted {
			found = true
			payload, ok := notification.AsThreadStarted()
			if !ok || payload.Params.Thread.ID != "thread-nested" {
				t.Fatalf("thread/started payload = %#v ok=%v", payload, ok)
			}
		}
	}
	if !found {
		t.Fatalf("thread/started was lost across attach registration: %#v", result.Run.Notifications)
	}
	later := newExactRunState(c, "thread-nested", StartedThreadRun{})
	later.turnID = "turn-later"
	if kinds := exactNotificationKinds(later); len(kinds) != 0 {
		t.Fatalf("buffered thread/started leaked into a later run: %#v", kinds)
	}
}
