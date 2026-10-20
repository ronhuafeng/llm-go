package protocolsync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/checkacceptance"
)

// This fixture observes only controller decisions. Native Git conflict
// materialization and isolated fresh-goal completion have their own real-file
// tests; none of these convergence cases make a remote write or model call.
type forwardConvergenceFake struct {
	*forwardFake
	t                   *testing.T
	deadline            time.Time
	moves, heads        []string
	parents             map[string]string
	proved              map[string]bool
	proofCalls          map[string]int
	headChecks          map[string]int
	events              []string
	failProofHead       string
	unresolvedRepair    bool
	beforePublishMain   string
	publicationAttempts int
}

func newForwardConvergenceFake(t *testing.T, ctx context.Context) *forwardConvergenceFake {
	t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("fixture requires the original deadline")
	}
	return &forwardConvergenceFake{forwardFake: newForwardFake(), t: t, deadline: deadline, parents: map[string]string{}, proved: map[string]bool{}, proofCalls: map[string]int{}, headChecks: map[string]int{}}
}
func (f *forwardConvergenceFake) budget(ctx context.Context) {
	f.t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok || !deadline.Equal(f.deadline) {
		f.t.Fatal("controller renewed the original repair/proof/effect deadline")
	}
}
func (f *forwardConvergenceFake) Ancestor(_ context.Context, a, b string) (bool, error) {
	if a == b || f.parents[b] == a {
		return true, nil
	}
	if a == f.state.Head && b == f.state.Main {
		return f.integrated, nil
	}
	return a == f.state.Main && b == f.state.Head && f.linear, nil
}
func (f *forwardConvergenceFake) Rebase(ctx context.Context, main, head string) (string, error) {
	f.budget(ctx)
	f.rebases++
	f.events = append(f.events, "repair:"+main+":"+head)
	if f.unresolvedRepair {
		return "", ErrForwardConflict
	}
	if f.rebases > len(f.heads) {
		f.t.Fatal("unexpected extra repair/rebase allocation")
	}
	next := f.heads[f.rebases-1]
	f.parents[next] = main
	return next, nil
}
func (f *forwardConvergenceFake) Verify(ctx context.Context, main, head string) error {
	f.budget(ctx)
	key := main + ":" + head
	if f.proved[key] {
		return nil
	}
	f.proofCalls[key]++
	f.events = append(f.events, "verify:"+key)
	if head == f.failProofHead {
		return errors.New("original/current baseline proof failed")
	}
	f.proved[key] = true
	return nil
}
func (f *forwardConvergenceFake) Publish(ctx context.Context, old, next, main string) error {
	f.budget(ctx)
	if !f.proved[main+":"+next] {
		f.t.Fatal("controller published before verifying the exact new M/H pair")
	}
	if old != f.state.Head || main != f.state.Main {
		f.t.Fatal("controller publication discarded an external head/main update")
	}
	f.publicationAttempts++
	if f.beforePublishMain != "" && f.publicationAttempts == 1 {
		f.state.Main, f.linear = f.beforePublishMain, false
		f.events = append(f.events, "publication_stale:"+main+":"+next)
		return errors.New("main advanced immediately before branch publication")
	}
	f.events = append(f.events, "publish:"+main+":"+next)
	f.published++
	f.state.Head, f.linear = next, true
	return nil
}
func (f *forwardConvergenceFake) Checks(ctx context.Context, head string) (checkacceptance.Observation, error) {
	f.budget(ctx)
	if head != f.state.Head {
		f.t.Fatal("controller reused checks for another candidate")
	}
	f.checkCalls++
	f.headChecks[head]++
	return checkacceptance.Observation{Head: head, State: f.checks}, nil
}
func (f *forwardConvergenceFake) Forward(ctx context.Context, head, main string) error {
	f.budget(ctx)
	if !f.proved[main+":"+head] || f.headChecks[head] < 2 {
		f.t.Fatal("main write preceded current-main proof or repeated exact-head CI observation")
	}
	f.events = append(f.events, "forward:"+main+":"+head)
	f.forwarded++
	if f.forwarded <= len(f.moves) {
		f.state.Main = f.moves[f.forwarded-1]
		f.linear = false
		return errors.New("concurrent main advance rejected non-fast-forward write")
	}
	f.state.Main, f.integrated, f.state.Merged = head, true, true
	return nil
}

func TestAutoForwardConvergesAcrossMultipleMainMovesAndRepairsWithinOriginalDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	f := newForwardConvergenceFake(t, ctx)
	secondMain, finalHead := strings.Repeat("e", 40), strings.Repeat("f", 40)
	f.moves = []string{forwardOther, secondMain}
	f.heads = []string{forwardNew, finalHead}
	result, err := AutoForward(ctx, forwardH, time.Millisecond, f)
	if err != nil || result.Status != "integrated" || result.Head != finalHead || f.rebases != 2 || f.published != 2 || f.forwarded != 3 || f.cleaned != 1 {
		t.Fatalf("did not converge after two competing advances: result=%+v error=%v events=%v", result, err, f.events)
	}
	for _, pair := range [][2]string{{forwardM, forwardH}, {forwardOther, forwardNew}, {secondMain, finalHead}} {
		key := pair[0] + ":" + pair[1]
		if f.proofCalls[key] != 1 || f.headChecks[pair[1]] < 2 {
			t.Fatalf("old proof or old checks reused for %s: proofs=%v checks=%v", key, f.proofCalls, f.headChecks)
		}
	}
	if ctx.Err() != nil {
		t.Fatal("repair cancelled the caller's original controller context")
	}
}

func TestAutoForwardImmutableProofFailureBlocksPublicationAndIntegration(t *testing.T) {
	for _, divergent := range []bool{false, true} {
		t.Run(fmt.Sprint(divergent), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			f := newForwardConvergenceFake(t, ctx)
			f.linear = !divergent
			f.failProofHead = forwardH
			if divergent {
				f.heads = []string{forwardNew}
				f.failProofHead = forwardNew
			}
			result, err := AutoForward(ctx, forwardH, time.Millisecond, f)
			if err == nil || result.Status == "integrated" || f.published != 0 || f.forwarded != 0 || f.checkCalls != 0 {
				t.Fatalf("failed immutable proof crossed an effect/CI boundary: %+v %v events=%v", result, err, f.events)
			}
		})
	}
}

func TestAutoForwardStaleLinearMainRequiresNewImmutableProof(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	f := newForwardConvergenceFake(t, ctx)
	f.proved[forwardM+":"+forwardH] = true
	f.state.Main = forwardOther
	result, err := AutoForward(ctx, forwardH, time.Millisecond, f)
	if err != nil || result.Status != "integrated" || f.rebases != 0 || f.proofCalls[forwardOther+":"+forwardH] != 1 {
		t.Fatalf("new main inherited an old baseline proof despite unchanged H: %+v %v proofs=%v", result, err, f.proofCalls)
	}
}

func TestAutoForwardUnresolvedRepairStopsWithoutFreshBudgetOrPublication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	f := newForwardConvergenceFake(t, ctx)
	f.moves = []string{forwardOther, strings.Repeat("e", 40)}
	f.unresolvedRepair = true
	result, err := AutoForward(ctx, forwardH, time.Millisecond, f)
	if !errors.Is(err, ErrForwardConflict) || result.Status != "repair_required" || f.rebases != 1 || f.published != 0 || f.forwarded != 1 {
		t.Fatalf("unresolved repair continued with another budget/effect: %+v %v events=%v", result, err, f.events)
	}
}

func TestNativeForwardProofCacheCannotCrossCurrentMain(t *testing.T) {
	root, main, head, base := forwardNativeFixture(t, "clean")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	calls := 0
	op := &NativeForward{proofs: map[string]bool{base + ":" + head: true}, req: ForwardRequest{RepoRoot: root, Target: Target{PeeledCommitSHA: forwardH}, VerifyCandidate: func(call context.Context, gotMain, gotHead string) error {
		calls++
		if gotMain != main || gotHead != head {
			t.Fatal("immutable proof callback received the wrong candidate/main")
		}
		deadline, ok := call.Deadline()
		original, _ := ctx.Deadline()
		if !ok || !deadline.Equal(original) {
			t.Fatal("native proof reset the deadline")
		}
		return nil
	}}}
	for _, current := range []string{base, main, main} {
		if err := op.Verify(ctx, current, head); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("current-main proof was skipped or needlessly repeated: %d", calls)
	}
}

func TestAutoForwardMainMovesImmediatelyBeforePublicationRetainsOldHead(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	f := newForwardConvergenceFake(t, ctx)
	f.linear = false
	f.beforePublishMain = forwardOther
	finalHead := strings.Repeat("f", 40)
	f.heads = []string{forwardNew, finalHead}
	result, err := AutoForward(ctx, forwardH, time.Millisecond, f)
	if err != nil || result.Status != "integrated" || result.Head != finalHead || f.rebases != 2 || f.publicationAttempts != 2 || f.published != 1 || f.forwarded != 1 {
		t.Fatalf("unpublished candidate was adopted or main movement did not converge: %+v %v events=%v", result, err, f.events)
	}
	if f.headChecks[forwardNew] != 0 || f.proofCalls[forwardM+":"+forwardNew] != 1 || f.proofCalls[forwardOther+":"+finalHead] != 1 {
		t.Fatal("unpublished candidate inherited checks or old-main proof was reused")
	}
	want := "repair:" + forwardOther + ":" + forwardH
	found := false
	for _, event := range f.events {
		found = found || event == want
	}
	if !found {
		t.Fatal("reconciliation adopted the unpublished head instead of the confirmed old head")
	}
}
