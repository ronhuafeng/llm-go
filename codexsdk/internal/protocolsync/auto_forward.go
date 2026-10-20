package protocolsync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/checkacceptance"
)

// ForwardState contains observations, never cached acceptance. Head is the Git
// ref; GitHub's head projection is not substituted for it.
type ForwardState struct {
	Head, Main, Branch string
	Ready, Merged      bool
}
type ForwardResult struct {
	Status string                      `json:"status"`
	Head   string                      `json:"head"`
	Main   string                      `json:"main"`
	Reason string                      `json:"reason,omitempty"`
	Checks checkacceptance.Observation `json:"checks"`
}

// ForwardOperations is the narrow existing repository effect boundary. Main is
// always a normal FF. The one permitted rewrite is a branch lease at old H.
// Native Git/API implementations below revalidate identity at each effect.
type ForwardOperations interface {
	Read(context.Context, string) (ForwardState, error)
	Ancestor(context.Context, string, string) (bool, error)
	Policy(context.Context, string, string) (string, error)
	Rebase(context.Context, string, string) (string, error)
	Publish(context.Context, string, string, string) error
	Checks(context.Context, string) (checkacceptance.Observation, error)
	Review(context.Context) error
	Verify(context.Context, string, string) error
	Forward(context.Context, string, string) error
	Cleanup(context.Context, string) error
}

var ErrForwardConflict = errors.New("mechanical rebase conflicts; bounded fresh repair required")

// AutoForward converges within the caller's original deadline. It does not
// allocate a new budget, retry failed CI, merge a PR, or adopt an external edit.
// Lost write responses are always resolved by a read before any next effect.
func AutoForward(ctx context.Context, expected string, poll time.Duration, op ForwardOperations) (result ForwardResult, err error) {
	result.Status, result.Head = "blocked", expected
	if _, bounded := ctx.Deadline(); !bounded || !shaRE.MatchString(expected) || op == nil {
		return result, errors.New("exact head and original finite deadline required")
	}
	if poll <= 0 {
		poll = 15 * time.Second
	}
	fail := func(e error) (ForwardResult, error) { result.Reason = e.Error(); return result, e }
	for {
		if e := ctx.Err(); e != nil {
			return fail(e)
		}
		state, e := op.Read(ctx, result.Head)
		if e != nil {
			return fail(e)
		}
		result.Main = state.Main
		integrated, e := op.Ancestor(ctx, result.Head, state.Main)
		if e != nil {
			return fail(e)
		}
		if integrated {
			if e = op.Cleanup(ctx, result.Head); e != nil {
				return fail(e)
			}
			result.Status = "integrated"
			result.Reason = "exact candidate is contained in main; PR/ref cleanup confirmed"
			return result, nil
		}
		if !state.Ready || state.Merged || state.Head != result.Head {
			return fail(errors.New("PR identity, readiness or expected head changed"))
		}
		decision, e := op.Policy(ctx, state.Main, result.Head)
		if e != nil {
			return fail(e)
		}
		if decision != "run" {
			result.Status = decision
			result.Reason = "fixed target no longer eligible for integration"
			return result, nil
		}
		linear, e := op.Ancestor(ctx, state.Main, result.Head)
		if e != nil {
			return fail(e)
		}
		if linear {
			if err := op.Verify(ctx, state.Main, result.Head); err != nil {
				fresh, readErr := op.Read(ctx, result.Head)
				if readErr == nil && fresh.Head == result.Head && fresh.Ready && fresh.Main != state.Main {
					continue
				}
				if !errors.Is(err, ErrForwardSemanticProof) {
					return fail(err)
				}
				// A linear candidate can still violate newly accepted main's
				// runtime assertions. Reuse the same fresh bounded repair path.
				linear = false
			}
		}
		if !linear {
			next, e := op.Rebase(ctx, state.Main, result.Head)
			if e != nil {
				fresh, readErr := op.Read(ctx, result.Head)
				if readErr == nil && fresh.Head == result.Head && fresh.Ready && fresh.Main != state.Main {
					continue
				}
				if errors.Is(e, ErrForwardConflict) {
					result.Status = "repair_required"
				}
				return fail(e)
			}
			if !shaRE.MatchString(next) || next == result.Head {
				return fail(errors.New("reconciliation did not produce a new exact candidate"))
			}
			descends, e := op.Ancestor(ctx, state.Main, next)
			if e != nil || !descends {
				return fail(errors.New("reconstructed candidate does not descend from current main"))
			}
			if e = op.Verify(ctx, state.Main, next); e != nil {
				fresh, re := op.Read(ctx, result.Head)
				if re == nil && fresh.Head == result.Head && fresh.Ready && fresh.Main != state.Main {
					continue
				}
				return fail(e)
			}
			// No old-head check is transferable to a new candidate.
			old := result.Head
			pushErr := op.Publish(ctx, old, next, state.Main)
			readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			observed, readErr := op.Read(readCtx, next)
			cancel()
			if readErr != nil {
				return fail(errors.New("branch publication outcome unknown; inspect native refs before retry"))
			}
			if observed.Head != next {
				if observed.Head == old && observed.Main != state.Main && observed.Ready {
					continue
				}
				if observed.Head != old {
					return fail(errors.New("branch lease lost to an external edit; preserved remote head"))
				}
				if pushErr != nil {
					return fail(pushErr)
				}
				return fail(errors.New("branch write was not confirmed"))
			}
			result.Head = next
			result.Checks = checkacceptance.Observation{}
			continue
		}

		checks, e := op.Checks(ctx, result.Head)
		result.Checks = checks
		if e != nil {
			return fail(e)
		}
		if checks.Head != result.Head {
			return fail(errors.New("CI observation belongs to another head"))
		}
		switch checks.State {
		case checkacceptance.Failed, checkacceptance.Cancelled:
			return fail(fmt.Errorf("current candidate CI is %s; no automatic retry", checks.State))
		case checkacceptance.Missing, checkacceptance.Pending:
			timer := time.NewTimer(poll)
			select {
			case <-ctx.Done():
				timer.Stop()
				return fail(ctx.Err())
			case <-timer.C:
			}
			continue
		case checkacceptance.Succeeded:
		default:
			return fail(errors.New("unknown CI outcome"))
		}
		if e = op.Review(ctx); e != nil {
			return fail(e)
		}
		fresh, e := op.Read(ctx, result.Head)
		if e != nil {
			return fail(e)
		}
		if !fresh.Ready || fresh.Head != result.Head {
			return fail(errors.New("candidate changed before integration"))
		}
		if fresh.Main != state.Main {
			continue
		}
		// Re-observe the same four checks after identity and review. Native Forward
		// repeats identity/ancestry immediately before the non-force Git effect.
		checks, e = op.Checks(ctx, result.Head)
		result.Checks = checks
		if e != nil {
			return fail(e)
		}
		if checks.Head != result.Head || checks.State != checkacceptance.Succeeded {
			return fail(errors.New("final exact-head checks are not successful"))
		}
		pushErr := op.Forward(ctx, result.Head, state.Main)
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		after, readErr := op.Read(readCtx, result.Head)
		if readErr == nil {
			var contained bool
			contained, readErr = op.Ancestor(readCtx, result.Head, after.Main)
			if readErr == nil && contained {
				cancel()
				continue
			}
		}
		cancel()
		if readErr != nil {
			return fail(errors.New("main update outcome unknown; inspect native refs before retry"))
		}
		if after.Head != result.Head {
			return fail(errors.New("candidate changed during integration"))
		}
		if after.Main != state.Main {
			continue
		} // confirmed competing main update; same budget
		if pushErr != nil {
			return fail(pushErr)
		}
		return fail(errors.New("main readback does not contain verified candidate"))
	}
}

// ErrForwardSemanticProof denotes a complete local assertion failure eligible for isolated repair.
var ErrForwardSemanticProof = errors.New("local immutable contract assertion failed")
