package codexsdk

import (
	"context"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func TestUnknownAttachingTurnDoesNotClaimHistoricalTurnNotification(t *testing.T) {
	c := newTurnHandoffClient()
	state := newExactRunState(c, "thread-resume", ResumedThreadRun{Resume: facadeThreadResumeResponse("thread-resume", "gpt")})
	if err := c.registerAttachingExactStream(state); err != nil {
		t.Fatal(err)
	}

	historical := tokenUsageNotification("thread-resume", "turn-history")
	typed, err := exactNotification(historical)
	if err != nil {
		t.Fatal(err)
	}
	if c.routeExactNotification(historical, typed) {
		t.Fatal("historical same-thread usage was attached to an unpublished turn")
	}
	if len(c.pendingEvents["turn-history"]) != 0 {
		t.Fatalf("historical usage was buffered as pending exact evidence: %#v", c.pendingEvents["turn-history"])
	}
	if notifications := exactRunNotifications(state); len(notifications) != 0 {
		t.Fatalf("historical usage leaked onto the attaching run: %#v", notifications)
	}
}

func TestArmedTurnStartPreservesGenuineNotificationWithoutSameThreadGuess(t *testing.T) {
	c := newTurnHandoffClient()
	state := newExactRunState(c, "thread-1", StartedThreadRun{Start: facadeThreadStartResponse("thread-1", "gpt")})
	if err := c.registerAttachingExactStream(state); err != nil {
		t.Fatal(err)
	}
	c.armTurnAttach("turn-1")

	historical := tokenUsageNotification("thread-1", "turn-history")
	historicalTyped, err := exactNotification(historical)
	if err != nil {
		t.Fatal(err)
	}
	if c.routeExactNotification(historical, historicalTyped) {
		t.Fatal("historical usage was claimed after a different turn was armed")
	}

	genuine := tokenUsageNotification("thread-1", "turn-1")
	genuineTyped, err := exactNotification(genuine)
	if err != nil {
		t.Fatal(err)
	}
	if !c.routeExactNotification(genuine, genuineTyped) {
		t.Fatal("proven turn/start identity lost the new-turn notification")
	}
	if len(c.pendingEvents["turn-1"]) != 1 {
		t.Fatalf("pending new-turn evidence = %#v", c.pendingEvents["turn-1"])
	}

	c.attachExactStreamForTurn(state, protocolv2.Turn{ID: "turn-1", Status: protocolv2.TurnStatusInProgress})
	notifications := exactRunNotifications(state)
	if usageCountState(notifications, "turn-history") != 0 {
		t.Fatalf("historical usage attached to new turn: %#v", notifications)
	}
	if usageCountState(notifications, "turn-1") != 1 {
		t.Fatalf("new-turn usage missing after proven handoff: %#v", notifications)
	}
}

func TestResumeRestoredUsageReachesGlobalHandlerAndDoesNotDeadlock(t *testing.T) {
	seenHistorical := make(chan struct{}, 1)
	seenNewTurn := make(chan struct{}, 1)
	t.Setenv("CODEXSDK_FAKE_RECORD", tempRecord(t))
	root, err := New(ClientOptions{
		CWD:     t.TempDir(),
		Command: fakeCommand("resume-restored-usage"),
		ServerNotificationHandler: func(_ context.Context, notification protocolv2.ServerNotification) error {
			if usage, ok := notification.AsThreadTokenUsageUpdated(); ok {
				switch usage.Params.TurnID {
				case "turn-history":
					select {
					case seenHistorical <- struct{}{}:
					default:
					}
				case "turn-1":
					select {
					case seenNewTurn <- struct{}{}:
					default:
					}
				}
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	result, runErr := root.ThreadRunner().Resume(context.Background(), ResumeThreadRunRequest{
		Thread: protocolv2.ThreadResumeParams{ThreadID: "thread-resume"},
		Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{
			protocolv2.NewUserInputText(protocolv2.UserInputText{Text: "continue"}),
		}},
	})
	if runErr != nil {
		t.Fatalf("resume error = %v", runErr)
	}
	if usageCountState(result.Run.Notifications, "turn-history") != 0 {
		t.Fatalf("restored historical usage attached to new turn: %#v", result.Run.Notifications)
	}
	if usageCountState(result.Run.Notifications, result.Run.Turn.ID) == 0 {
		t.Fatalf("new-turn usage missing: %#v", result.Run.Notifications)
	}
	select {
	case <-seenHistorical:
	case <-time.After(time.Second):
		t.Fatal("global handler did not receive restored historical usage")
	}
	select {
	case <-seenNewTurn:
	case <-time.After(time.Second):
		t.Fatal("global handler did not receive new-turn usage")
	}
	if err := root.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
}

func newTurnHandoffClient() *Client {
	return &Client{
		ctx:               context.Background(),
		exactStreams:      map[string]map[*exactRunState]struct{}{},
		exactAttaching:    map[string]map[*exactRunState]struct{}{},
		pendingEvents:     map[string][]rpcNotification{},
		armedThreadAttach: map[string]struct{}{},
		armedTurnAttach:   map[string]struct{}{},
	}
}

func tokenUsageNotification(threadID, turnID string) rpcNotification {
	return rpcNotification{
		method: protocolv2.MethodThreadTokenUsageUpdated,
		params: map[string]any{
			"threadId": threadID,
			"turnId":   turnID,
			"tokenUsage": map[string]any{
				"last":  fakeTokenUsageBreakdown(3, 1, 2, 1, 5),
				"total": fakeTokenUsageBreakdown(30, 10, 20, 5, 50),
			},
		},
	}
}

func exactRunNotifications(state *exactRunState) []protocolv2.ServerNotification {
	state.mu.Lock()
	defer state.mu.Unlock()
	switch result := state.result.(type) {
	case StartedThreadRun:
		return result.Run.Notifications
	case ResumedThreadRun:
		return result.Run.Notifications
	default:
		return nil
	}
}

func usageCountState(notifications []protocolv2.ServerNotification, turnID string) int {
	count := 0
	for _, notification := range notifications {
		usage, ok := notification.AsThreadTokenUsageUpdated()
		if ok && usage.Params.TurnID == turnID {
			count++
		}
	}
	return count
}
