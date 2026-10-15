package protocolsync

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/checkacceptance"
)

func TestGoalProgressEscapesCommandsAndBalancesGroups(t *testing.T) {
	var out bytes.Buffer
	p := &GoalProgress{Output: &out, Actions: true}
	p.group("Local verification")
	p.line("failure\n::error::injected")
	p.group("CI observation")
	p.Failure("deadline\r\n::warning::injected%")
	p.end()
	s := out.String()
	if strings.Count(s, "::group::") != strings.Count(s, "::endgroup::") || strings.Contains(s, "\n::warning::") || strings.Contains(s, "\n::error::") || !strings.Contains(s, "%0D%0A::warning::injected%25") {
		t.Fatal(s)
	}
	out.Reset()
	p.Actions = false
	p.group("Local verification")
	p.Failure("failed")
	if strings.Contains(out.String(), "::") {
		t.Fatal(out.String())
	}
}

func TestGoalCIProgressUsesExistingObservationsWithoutRepeatingUnchangedState(t *testing.T) {
	f := newGoalPublicationFixture(t)
	f.state = checkacceptance.Pending
	var out bytes.Buffer
	f.request.Progress = &GoalProgress{Output: &out, Actions: true}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	o, err := ObserveGoalCI(ctx, f.request, f.request.ExpectedHead, time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) || o.State != checkacceptance.Pending {
		t.Fatalf("%+v %v", o, err)
	}
	if strings.Count(out.String(), "CI for ") != 1 || !strings.Contains(out.String(), f.request.ExpectedHead) || !strings.Contains(out.String(), "Live Codex integration / Live scenarios: pending") {
		t.Fatal(out.String())
	}
	if strings.Contains(out.String(), "::error") {
		t.Fatal("intermediate failure became terminal annotation")
	}
}

type failedProgressWriter struct{}

func (failedProgressWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestGoalProgressWriteFailureDoesNotChangeCIResult(t *testing.T) {
	f := newGoalPublicationFixture(t)
	f.request.Progress = &GoalProgress{Output: failedProgressWriter{}, Actions: true}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	o, err := ObserveGoalCI(ctx, f.request, f.request.ExpectedHead, time.Millisecond)
	if err != nil || o.State != checkacceptance.Succeeded {
		t.Fatalf("%+v %v", o, err)
	}
}
