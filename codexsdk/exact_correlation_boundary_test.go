package codexsdk

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func TestExactCorrelationConflictingThreadDoesNotChangeRun(t *testing.T) {
	for _, phase := range []string{"active", "pending"} {
		for _, status := range []protocolv2.TurnStatus{protocolv2.TurnStatusCompleted, protocolv2.TurnStatusFailed, protocolv2.TurnStatusInterrupted} {
			t.Run(phase+"/"+string(status), func(t *testing.T) {
				c := newTurnHandoffClient()
				c.retention = &notificationRetention{limit: NotificationRetentionBudget{MaxCount: 100, MaxBytes: 1 << 20}}
				s := newExactRunState(c, "thread-owner", StartedThreadRun{Start: facadeThreadStartResponse("thread-owner", "fixture")})
				if err := c.registerAttachingExactStream(s); err != nil {
					t.Fatal(err)
				}
				if err := c.armTurnAttach("turn-owner", s); err != nil {
					t.Fatal(err)
				}
				if phase == "active" {
					c.attachExactStreamForTurn(s, correlationInProgressTurn("turn-owner"))
				}
				before := correlationRunSnapshot(s)
				count, bytes := retentionFacts(c.retention)
				for _, n := range []rpcNotification{
					tokenUsageNotification("thread-other", "turn-owner"),
					correlationTerminal("thread-other", "turn-owner", status, "wrong answer"),
				} {
					routed, completions, evidence := c.routeExactNotificationBeforeTerminalCompletion(n, correlationTyped(t, n))
					if routed || len(completions) != 0 || evidence != nil {
						t.Fatalf("conflicting thread acquired a run or completion fence: routed=%v completions=%d evidence=%v", routed, len(completions), evidence != nil)
					}
				}
				if got := correlationRunSnapshot(s); !reflect.DeepEqual(got, before) {
					t.Fatalf("conflicting frame changed run: before=%#v after=%#v", before, got)
				}
				if gotCount, gotBytes := retentionFacts(c.retention); gotCount != count || gotBytes != bytes {
					t.Fatalf("conflicting frame retained evidence: (%d,%d) -> (%d,%d)", count, bytes, gotCount, gotBytes)
				}
				correlationRequireOpen(t, s)

				for _, n := range []rpcNotification{
					tokenUsageNotification("thread-owner", "turn-owner"),
					correlationTerminal("thread-owner", "turn-owner", protocolv2.TurnStatusCompleted, "right answer"),
				} {
					if !c.routeExactNotification(n, correlationTyped(t, n)) {
						t.Fatal("matching evidence was not routed")
					}
				}
				if phase == "pending" {
					c.attachExactStreamForTurn(s, correlationInProgressTurn("turn-owner"))
				}
				result, err := (&Stream[StartedThreadRun]{state: s}).Wait(correlationWaitContext(t))
				if err != nil || len(result.Run.Notifications) != 2 || result.Run.Usage == nil || result.Run.Usage.Total.TotalTokens != 50 || result.Run.Turn.Status != protocolv2.TurnStatusCompleted || !result.Run.FinalResponsePresent || result.Run.FinalResponse != "right answer" {
					t.Fatalf("matching completion = %#v, err=%v", result.Run, err)
				}
			})
		}
	}
}

func TestExactCorrelationReplayRejectsConflictingThread(t *testing.T) {
	for _, withEvidence := range []bool{false, true} {
		for _, status := range []protocolv2.TurnStatus{protocolv2.TurnStatusCompleted, protocolv2.TurnStatusFailed, protocolv2.TurnStatusInterrupted} {
			name := "plain/" + string(status)
			if withEvidence {
				name = "evidence/" + string(status)
			}
			t.Run(name, func(t *testing.T) {
				c := newTurnHandoffClient()
				c.retention = &notificationRetention{limit: NotificationRetentionBudget{MaxCount: 100, MaxBytes: 1 << 20}}
				s := newExactRunState(c, "thread-owner", StartedThreadRun{Start: facadeThreadStartResponse("thread-owner", "fixture")})
				if err := c.registerAttachingExactStream(s); err != nil {
					t.Fatal(err)
				}
				// Seed the replay seam deliberately: even stale or misattributed pending
				// records must pass the same ownership check as live ingestion.
				pending := []rpcNotification{
					tokenUsageNotification("thread-other", "turn-owner"),
					correlationTerminal("thread-other", "turn-owner", status, "wrong answer"),
				}
				for i := range pending {
					var err error
					pending[i].retained, err = c.retention.reserve(4, 4*retainedValueBytes(pending[i].params))
					if err != nil {
						t.Fatal(err)
					}
					if withEvidence {
						pending[i].evidence = &notificationEvidence{ready: make(chan struct{}), state: s}
					}
				}
				c.pendingEvents["turn-owner"] = pending
				c.attachExactStreamForTurn(s, correlationInProgressTurn("turn-owner"))
				correlationRequireOpen(t, s)
				got := correlationRunSnapshot(s)
				if len(got.Notifications) != 0 || got.Usage != nil || got.Turn.Status != protocolv2.TurnStatusInProgress || got.FinalResponsePresent || got.FinalResponse != "" {
					t.Fatalf("conflicting replay changed run: %#v", got)
				}
				for _, n := range pending {
					if n.evidence != nil {
						select {
						case <-n.evidence.ready:
						default:
							t.Fatal("rejected replay left its ready fence blocked")
						}
					}
				}
				if count, bytes := retentionFacts(c.retention); count != 0 || bytes != 0 {
					t.Fatalf("rejected replay retained payloads: count=%d bytes=%d", count, bytes)
				}
				terminal := correlationTerminal("thread-owner", "turn-owner", protocolv2.TurnStatusCompleted, "right answer")
				if !c.routeExactNotification(terminal, correlationTyped(t, terminal)) {
					t.Fatal("matching terminal did not complete after rejected replay")
				}
				result, err := (&Stream[StartedThreadRun]{state: s}).Wait(correlationWaitContext(t))
				if err != nil || len(result.Run.Notifications) != 1 || result.Run.FinalResponse != "right answer" {
					t.Fatalf("matching completion after replay = %#v, err=%v", result.Run, err)
				}
			})
		}
	}
}

func TestExactCorrelationCallbacksKeepConflictingWireIdentity(t *testing.T) {
	for _, phase := range []string{"active", "pending"} {
		t.Run(phase, func(t *testing.T) {
			typedSeen := make(chan notificationIdentity, 4)
			rawSeen := make(chan RawNotification, 4)
			c := retentionClient(t, 100, 1<<20, ClientOptions{
				ServerNotificationHandler: func(_ context.Context, n protocolv2.ServerNotification) error {
					typedSeen <- extractNotificationIdentity(n)
					return nil
				},
				RawNotifications: &RawNotificationSubscription{
					Methods: []string{protocolv2.MethodThreadTokenUsageUpdated, protocolv2.MethodTurnCompleted},
					Handler: func(_ context.Context, n RawNotification) error { rawSeen <- n; return nil },
				},
			})
			s := newExactRunState(c, "thread-owner", StartedThreadRun{Start: facadeThreadStartResponse("thread-owner", "fixture")})
			if err := c.registerAttachingExactStream(s); err != nil {
				t.Fatal(err)
			}
			if err := c.armTurnAttach("turn-owner", s); err != nil {
				t.Fatal(err)
			}
			if phase == "active" {
				c.attachExactStreamForTurn(s, correlationInProgressTurn("turn-owner"))
			}
			for _, n := range []rpcNotification{
				tokenUsageNotification("thread-other", "turn-owner"),
				correlationTerminal("thread-other", "turn-owner", protocolv2.TurnStatusCompleted, "wrong answer"),
			} {
				c.routeNotification(n)
				select {
				case raw := <-rawSeen:
					if raw.Method != n.method || raw.ThreadID != "thread-other" || raw.TurnID != "turn-owner" {
						t.Fatalf("raw identity rewritten: %#v", raw)
					}
				case <-time.After(time.Second):
					t.Fatal("unrelated raw callback waited for exact attachment")
				}
				select {
				case identity := <-typedSeen:
					if identity != (notificationIdentity{threadID: "thread-other", turnID: "turn-owner"}) {
						t.Fatalf("typed identity rewritten: %#v", identity)
					}
				case <-time.After(time.Second):
					t.Fatal("unrelated typed callback waited for exact attachment")
				}
			}
			if got := correlationRunSnapshot(s); len(got.Notifications) != 0 || got.Usage != nil || got.FinalResponsePresent {
				t.Fatalf("global callback evidence leaked into exact run: %#v", got)
			}
			correlationRequireOpen(t, s)
		})
	}
}

func TestExactCorrelationRetainsThreadOnlyAndTurnOnlyMatching(t *testing.T) {
	for _, phase := range []string{"active", "pending"} {
		t.Run(phase, func(t *testing.T) {
			c := newTurnHandoffClient()
			s := newExactRunState(c, "thread-owner", StartedThreadRun{Start: facadeThreadStartResponse("thread-owner", "fixture")})
			if err := c.registerAttachingExactStream(s); err != nil {
				t.Fatal(err)
			}
			if err := c.armTurnAttach("turn-owner", s); err != nil {
				t.Fatal(err)
			}
			if phase == "active" {
				c.attachExactStreamForTurn(s, correlationInProgressTurn("turn-owner"))
			}
			for _, n := range []rpcNotification{
				{method: "guardianWarning", params: map[string]any{"threadId": "thread-owner", "message": "thread fact"}},
				tokenUsageNotification("", "turn-owner"),
			} {
				routed, completions, evidence := c.routeExactNotificationBeforeTerminalCompletion(n, correlationTyped(t, n))
				if !routed || len(completions) != 0 {
					t.Fatalf("single-identity observation rejected: %s", n.method)
				}
				if evidence != nil && evidence.state != s {
					t.Fatal("turn-only pending evidence lost its response-proved owner")
				}
			}
			if phase == "pending" {
				c.attachExactStreamForTurn(s, correlationInProgressTurn("turn-owner"))
			}
			got := correlationRunSnapshot(s)
			if len(got.Notifications) != 2 || got.Usage == nil || got.Usage.Total.TotalTokens != 50 {
				t.Fatalf("single-identity facts missing: %#v", got)
			}
			if identity := extractNotificationIdentity(got.Notifications[1]); identity.threadID != "" || identity.turnID != "turn-owner" {
				t.Fatalf("turn-only evidence identity rewritten: %#v", identity)
			}
		})
	}
}

func TestExactCorrelationPendingOwnerSurvivesOtherAttachmentFailure(t *testing.T) {
	for _, outcome := range []string{"attach", "failure", "close"} {
		t.Run(outcome, func(t *testing.T) {
			c := retentionClient(t, 100, 1<<20)
			a := newExactRunState(c, "thread-a", StartedThreadRun{Start: facadeThreadStartResponse("thread-a", "fixture")})
			b := newExactRunState(c, "thread-b", StartedThreadRun{Start: facadeThreadStartResponse("thread-b", "fixture")})
			for _, s := range []*exactRunState{a, b} {
				if err := c.registerAttachingExactStream(s); err != nil {
					t.Fatal(err)
				}
			}
			// The turn/start response proves A's owner, but the publication barrier
			// leaves A before setTurn while B's turn/start is still unresolved.
			if err := c.armTurnAttach("turn-a", a); err != nil {
				t.Fatal(err)
			}
			releaseAttach := make(chan struct{})
			attached := make(chan struct{})
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(releaseAttach) }) }
			t.Cleanup(func() { release(); <-attached })
			go func() {
				<-releaseAttach
				c.attachExactStreamForTurn(a, correlationInProgressTurn("turn-a"))
				close(attached)
			}()
			good := tokenUsageNotification("thread-a", "turn-a")
			routed, _, evidence := c.routeExactNotificationBeforeTerminalCompletion(good, correlationTyped(t, good))
			if !routed || evidence == nil || evidence.state != a || a.turnIDSnapshot() != "" || b.turnIDSnapshot() != "" {
				t.Fatal("A's proven pending evidence did not retain A before turn publication")
			}
			beforeCount, beforeBytes := retentionFacts(c.retention)
			bad := tokenUsageNotification("thread-b", "turn-a")
			routed, completions, foreignEvidence := c.routeExactNotificationBeforeTerminalCompletion(bad, correlationTyped(t, bad))
			if routed || len(completions) != 0 || foreignEvidence != nil {
				t.Fatal("conflicting frame acquired B's evidence state or a ready fence")
			}
			if count, bytes := retentionFacts(c.retention); count != beforeCount || bytes != beforeBytes {
				t.Fatal("conflicting frame retained pending evidence")
			}
			bFailure := errors.New("B turn/start rejected")
			c.finishAttachingExactStream(b, bFailure)
			bResult, err := (&Stream[StartedThreadRun]{state: b}).Wait(correlationWaitContext(t))
			if !errors.Is(err, bFailure) || len(bResult.Run.Notifications) != 0 || bResult.Run.Usage != nil || bResult.Run.Turn.ID != "" || bResult.Run.FinalResponsePresent {
				t.Fatalf("B's failed start acquired A's evidence: %#v err=%v", bResult.Run, err)
			}
			select {
			case <-evidence.ready:
				t.Fatal("B's failure resolved A's pending evidence")
			default:
			}
			correlationRequireOpen(t, a)

			var cause error
			switch outcome {
			case "failure":
				cause = errors.New("transport failed before A publication")
				c.failClient(cause)
			case "close":
				cause = ErrClientClosed
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if cause != nil {
				partial, err := (&Stream[StartedThreadRun]{state: a}).Wait(correlationWaitContext(t))
				if !errors.Is(err, cause) || len(partial.Run.Notifications) != 1 || partial.Run.Usage == nil || partial.Run.Usage.Total.TotalTokens != 50 {
					t.Fatalf("A lost its own pending evidence before setTurn: %#v err=%v", partial.Run, err)
				}
			}
			release()
			<-attached
			select {
			case <-evidence.ready:
			default:
				t.Fatal("A's pending ready fence was not released")
			}
			terminal := correlationTerminal("thread-a", "turn-a", protocolv2.TurnStatusCompleted, "right answer")
			c.routeExactNotification(terminal, correlationTyped(t, terminal))
			result, err := (&Stream[StartedThreadRun]{state: a}).Wait(correlationWaitContext(t))
			if cause != nil {
				if !errors.Is(err, cause) || result.Run.FinalResponsePresent || len(result.Run.Notifications) != 1 {
					t.Fatalf("late attachment/terminal replaced first failure: %#v err=%v", result.Run, err)
				}
				if result.Run.Turn.ID != "turn-a" || result.Run.Turn.Status != protocolv2.TurnStatusInProgress {
					t.Fatal("late response lost initial turn facts when no terminal was observed")
				}
			} else if err != nil || result.Run.FinalResponse != "right answer" || len(result.Run.Notifications) != 2 {
				t.Fatalf("A did not complete from its own evidence: %#v err=%v", result.Run, err)
			}
			c.turnMu.Lock()
			remaining := len(c.pendingEvents) + len(c.replayingEvents) + len(c.armedTurnAttach) + len(c.attachmentRetention)
			c.turnMu.Unlock()
			if remaining != 0 {
				t.Fatalf("settled attachments retained pending ownership: %d", remaining)
			}
		})
	}
}

func TestExactCorrelationLateAttachPreservesPendingTerminal(t *testing.T) {
	for _, outcome := range []string{"failure", "close"} {
		for _, status := range []protocolv2.TurnStatus{protocolv2.TurnStatusCompleted, protocolv2.TurnStatusFailed, protocolv2.TurnStatusInterrupted} {
			t.Run(outcome+"/"+string(status), func(t *testing.T) {
				c := retentionClient(t, 100, 1<<20)
				s := newExactRunState(c, "thread-owner", StartedThreadRun{Start: facadeThreadStartResponse("thread-owner", "fixture")})
				if err := c.registerAttachingExactStream(s); err != nil {
					t.Fatal(err)
				}
				if err := c.armTurnAttach("turn-owner", s); err != nil {
					t.Fatal(err)
				}
				terminal := correlationTerminal("thread-owner", "turn-owner", status, "observed final")
				if status == protocolv2.TurnStatusFailed {
					terminal.params["turn"].(map[string]any)["error"] = map[string]any{"message": "native failed", "codexErrorInfo": "usageLimitExceeded"}
				}
				var pendingEvidence []*notificationEvidence
				for _, n := range []rpcNotification{tokenUsageNotification("thread-owner", "turn-owner"), terminal} {
					routed, completions, evidence := c.routeExactNotificationBeforeTerminalCompletion(n, correlationTyped(t, n))
					if !routed || len(completions) != 0 || evidence == nil || evidence.state != s {
						t.Fatal("response-proved observation was not pending before setTurn")
					}
					pendingEvidence = append(pendingEvidence, evidence)
				}
				if s.turnIDSnapshot() != "" {
					t.Fatal("test did not pause before turn publication")
				}
				cause := error(ErrClientClosed)
				if outcome == "failure" {
					cause = errors.New("transport failed before turn publication")
					c.failClient(cause)
				} else if err := c.Close(); err != nil {
					t.Fatal(err)
				}
				stream := &Stream[StartedThreadRun]{state: s}
				before, err := stream.Wait(correlationWaitContext(t))
				if !errors.Is(err, cause) || before.Run.Turn.Status != status || len(before.Run.Turn.Items) != 1 || !before.Run.FinalResponsePresent || before.Run.FinalResponse != "observed final" || len(before.Run.Notifications) != 2 || before.Run.Usage == nil {
					t.Fatalf("failure did not preserve pending terminal facts: %#v err=%v", before.Run, err)
				}
				if status == protocolv2.TurnStatusFailed && before.Run.Turn.Error == nil {
					t.Fatal("native turn error was not preserved")
				}
				for _, evidence := range pendingEvidence {
					select {
					case <-evidence.ready:
					default:
						t.Fatal("settlement did not release pending evidence fence")
					}
				}
				// The runner resumes with the earlier turn/start response only after
				// client settlement has published newer terminal observations.
				c.attachExactStreamForTurn(s, correlationInProgressTurn("turn-owner"))
				after, err := stream.Wait(correlationWaitContext(t))
				if !errors.Is(err, cause) || !reflect.DeepEqual(after.Run, before.Run) {
					t.Fatalf("late attachment changed settled run: turn status %q -> %q, item count %d -> %d, final %q -> %q, err=%v", before.Run.Turn.Status, after.Run.Turn.Status, len(before.Run.Turn.Items), len(after.Run.Turn.Items), before.Run.FinalResponse, after.Run.FinalResponse, err)
				}
				if s.turnIDSnapshot() != "turn-owner" {
					t.Fatal("late attachment did not bind the response-proved turn identity")
				}
			})
		}
	}
}

func correlationTerminal(threadID, turnID string, status protocolv2.TurnStatus, text string) rpcNotification {
	return rpcNotification{method: protocolv2.MethodTurnCompleted, params: map[string]any{
		"threadId": threadID,
		"turn": map[string]any{"id": turnID, "status": string(status), "items": []map[string]any{
			{"id": "final", "type": "agentMessage", "phase": "final_answer", "text": text},
		}},
	}}
}

func correlationInProgressTurn(turnID string) protocolv2.Turn {
	return protocolv2.Turn{ID: turnID, Status: protocolv2.TurnStatusInProgress, Items: []protocolv2.ThreadItem{}}
}

func correlationTyped(t *testing.T, n rpcNotification) protocolv2.ServerNotification {
	t.Helper()
	typed, err := exactNotification(n)
	if err != nil {
		t.Fatal(err)
	}
	return typed
}

func correlationRunSnapshot(s *exactRunState) ThreadRunResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.result.(StartedThreadRun).Run
}

func correlationRequireOpen(t *testing.T, s *exactRunState) {
	t.Helper()
	select {
	case <-s.done:
		t.Fatalf("unrelated evidence settled run: %v", (&Stream[StartedThreadRun]{state: s}).Err())
	default:
	}
}

func correlationWaitContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	return ctx
}
