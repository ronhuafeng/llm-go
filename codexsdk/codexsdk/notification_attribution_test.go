package codexsdk

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2"
)

func TestEveryGeneratedServerNotificationKindHasAttribution(t *testing.T) {
	seen := map[protocolv2.ServerNotificationKind]bool{}
	for _, method := range protocolv2.AllMethods() {
		if method.Direction != protocolv2.MethodDirectionServerToClient || method.Kind != protocolv2.MethodKindNotification {
			continue
		}
		kind := protocolv2.ServerNotificationKind(method.Method)
		seen[kind] = true
		if class := notificationAttribution[kind]; class == notificationAttributionUnsupported {
			t.Errorf("generated notification %q has no attribution class", kind)
		}
	}
	for kind := range notificationAttribution {
		if !seen[kind] {
			t.Errorf("attribution manifest contains non-generated notification %q", kind)
		}
	}
}

func TestUnknownFutureNotificationKindFailsClosed(t *testing.T) {
	future := protocolv2.ServerNotificationKind("future/generated-notification")
	if got := attributionClassForKind(future); got != notificationAttributionUnsupported {
		t.Fatalf("future kind attribution = %v, want unsupported until explicitly classified", got)
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

func TestAttributionClassesFollowGeneratedSchemaIdentityFacts(t *testing.T) {
	root := filepath.Join("internal", "protocolschema", "appserver", "v2")
	raw, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Entries []struct {
			Direction string `json:"direction"`
			Kind      string `json:"kind"`
			Method    string `json:"method"`
			Schema    string `json:"params_or_payload_schema"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, entry := range manifest.Entries {
		if entry.Direction != "server_to_client" || entry.Kind != "notification" {
			continue
		}
		var matches []string
		err := filepath.WalkDir(root, func(path string, item os.DirEntry, walkErr error) error {
			if walkErr == nil && !item.IsDir() && item.Name() == entry.Schema+".json" {
				matches = append(matches, path)
			}
			return walkErr
		})
		if err != nil || len(matches) != 1 {
			t.Fatalf("schema %s matches %v, err=%v", entry.Schema, matches, err)
		}
		schemaRaw, err := os.ReadFile(matches[0])
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Required    []string                   `json:"required"`
			Properties  map[string]json.RawMessage `json:"properties"`
			Definitions map[string]struct {
				Required []string `json:"required"`
			} `json:"definitions"`
		}
		if err := json.Unmarshal(schemaRaw, &schema); err != nil {
			t.Fatal(err)
		}
		required := func(name string) bool {
			for _, candidate := range schema.Required {
				if candidate == name {
					return true
				}
			}
			return false
		}
		want := notificationAttributionGlobal
		if required("turnId") {
			want = notificationAttributionTurn
		} else if required("turn") {
			var turnProperty struct {
				Ref string `json:"$ref"`
			}
			_ = json.Unmarshal(schema.Properties["turn"], &turnProperty)
			definition := filepath.Base(turnProperty.Ref)
			for _, field := range schema.Definitions[definition].Required {
				if field == "id" {
					want = notificationAttributionTurn
				}
			}
		} else if required("threadId") {
			want = notificationAttributionThread
		}
		if got := notificationAttribution[protocolv2.ServerNotificationKind(entry.Method)]; got != want {
			t.Errorf("%s attribution = %v, want %v from required schema identity", entry.Method, got, want)
		}
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
