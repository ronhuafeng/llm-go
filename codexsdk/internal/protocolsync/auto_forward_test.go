package protocolsync

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/checkacceptance"
)

var forwardH = strings.Repeat("a", 40)
var forwardM = strings.Repeat("b", 40)
var forwardNew = strings.Repeat("c", 40)
var forwardOther = strings.Repeat("d", 40)

type forwardFake struct {
	state                                                                ForwardState
	linear, integrated                                                   bool
	published, forwarded, cleaned, checkCalls, rebases                   int
	checks                                                               checkacceptance.State
	failPublish, failForward, conflict, leaseRace, mainRace, lateFailure bool
	decision                                                             string
}

func (f *forwardFake) Read(context.Context, string) (ForwardState, error) { return f.state, nil }
func (f *forwardFake) Ancestor(_ context.Context, a, b string) (bool, error) {
	if a == b {
		return true, nil
	}
	if a == f.state.Head && b == f.state.Main {
		return f.integrated, nil
	}
	if a == f.state.Main {
		return f.linear || b == forwardNew, nil
	}
	return false, nil
}
func (f *forwardFake) Policy(context.Context, string, string) (string, error) {
	if f.decision != "" {
		return f.decision, nil
	}
	return "run", nil
}
func (f *forwardFake) Rebase(context.Context, string, string) (string, error) {
	f.rebases++
	if f.conflict {
		return "", ErrForwardConflict
	}
	return forwardNew, nil
}
func (f *forwardFake) Publish(_ context.Context, old, next, main string) error {
	f.published++
	if f.leaseRace {
		f.state.Head = forwardOther
		return errors.New("lease rejected")
	}
	f.state.Head = next
	f.linear = true
	if f.failPublish {
		return errors.New("lost push response")
	}
	return nil
}
func (f *forwardFake) Checks(_ context.Context, head string) (checkacceptance.Observation, error) {
	f.checkCalls++
	state := f.checks
	if f.lateFailure && f.checkCalls > 1 {
		state = checkacceptance.Failed
	}
	return checkacceptance.Observation{Head: head, State: state}, nil
}
func (f *forwardFake) Verify(context.Context, string, string) error { return nil }
func (f *forwardFake) Review(context.Context) error                 { return nil }
func (f *forwardFake) Forward(context.Context, string, string) error {
	f.forwarded++
	if f.mainRace && f.forwarded == 1 {
		f.state.Main = forwardOther
		f.linear = false
		return errors.New("non-fast-forward")
	}
	f.state.Main = f.state.Head
	f.integrated = true
	f.state.Merged = true
	if f.failForward {
		return errors.New("lost main push response")
	}
	return nil
}
func (f *forwardFake) Cleanup(context.Context, string) error { f.cleaned++; return nil }
func newForwardFake() *forwardFake {
	return &forwardFake{state: ForwardState{Head: forwardH, Main: forwardM, Branch: "topic", Ready: true}, linear: true, checks: checkacceptance.Succeeded}
}
func TestAutoForwardLostResponseReadbackAndIdempotence(t *testing.T) {
	for _, rebase := range []bool{false, true} {
		t.Run(map[bool]string{false: "main", true: "branch"}[rebase], func(t *testing.T) {
			f := newForwardFake()
			f.linear = !rebase
			f.failPublish = true
			f.failForward = true
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			got, e := AutoForward(ctx, forwardH, time.Millisecond, f)
			if e != nil || got.Status != "integrated" {
				t.Fatalf("%+v %v", got, e)
			}
			pushes := f.forwarded + f.published
			got, e = AutoForward(ctx, got.Head, time.Millisecond, f)
			if e != nil || got.Status != "integrated" || f.forwarded+f.published != pushes {
				t.Fatalf("duplicate changed effects %+v %v", got, e)
			}
			if f.checkCalls < 2 {
				t.Fatal("must recheck exact-head checks")
			}
		})
	}
}
func TestAutoForwardExternalEditLeaseAndConflictStop(t *testing.T) {
	for _, name := range []string{"stale", "lease", "conflict"} {
		t.Run(name, func(t *testing.T) {
			f := newForwardFake()
			switch name {
			case "stale":
				f.state.Head = forwardOther
			case "lease":
				f.linear = false
				f.leaseRace = true
			case "conflict":
				f.linear = false
				f.conflict = true
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			got, e := AutoForward(ctx, forwardH, time.Millisecond, f)
			if e == nil || f.forwarded != 0 {
				t.Fatalf("unsafe effect %+v %v", got, e)
			}
			if name == "conflict" && (got.Status != "repair_required" || f.published != 0) {
				t.Fatalf("conflict crossed publication %+v", got)
			}
			if name == "lease" && f.state.Head != forwardOther {
				t.Fatal("external edit lost")
			}
		})
	}
}
func TestAutoForwardAllNonSuccessfulCurrentChecksBlock(t *testing.T) {
	for _, state := range []checkacceptance.State{checkacceptance.Missing, checkacceptance.Pending, checkacceptance.Failed, checkacceptance.Cancelled} {
		t.Run(string(state), func(t *testing.T) {
			f := newForwardFake()
			f.checks = state
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			_, e := AutoForward(ctx, forwardH, time.Millisecond, f)
			if e == nil || f.forwarded != 0 {
				t.Fatal("unaccepted head integrated")
			}
		})
	}
	f := newForwardFake()
	f.lateFailure = true
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, e := AutoForward(ctx, forwardH, time.Millisecond, f)
	if e == nil || f.forwarded != 0 {
		t.Fatal("late failed check accepted")
	}
}
func TestAutoForwardMainRaceRebasesUnderSameDeadline(t *testing.T) {
	f := newForwardFake()
	f.mainRace = true
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, e := AutoForward(ctx, forwardH, time.Millisecond, f)
	if e != nil || got.Head != forwardNew || f.forwarded != 2 || f.published != 1 {
		t.Fatalf("%+v %v effects %+v", got, e, f)
	}
}
func TestAutoForwardRequiresDeadlineAndStopsSupersession(t *testing.T) {
	f := newForwardFake()
	if _, e := AutoForward(context.Background(), forwardH, 0, f); e == nil {
		t.Fatal("unbounded controller")
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if _, e := AutoForward(ctx, forwardH, 0, f); e == nil || f.forwarded != 0 {
		t.Fatal("expired deadline wrote")
	}
	ctx, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	f.decision = "superseded"
	got, e := AutoForward(ctx, forwardH, 0, f)
	if e != nil || got.Status != "superseded" || f.forwarded != 0 {
		t.Fatalf("%+v %v", got, e)
	}
}
