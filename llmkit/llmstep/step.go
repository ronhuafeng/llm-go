package llmstep

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/ronhuafeng/llm-go/llmkit/llmadapter"
	"github.com/ronhuafeng/llm-go/llmkit/llmschema"
)

var ErrNilRender = errors.New("llmstep: render is nil")

// ErrNilValidate reports a step configured without a deterministic judge.
var ErrNilValidate = errors.New("llmstep: validate is nil")

// ErrInvalidMaxIter reports a step configured with a non-positive retry bound.
var ErrInvalidMaxIter = errors.New("llmstep: maxIter must be at least 1")

// ErrMissingRepairProjection reports a rejected attempt that can retry but has
// no application-owned projection for model-facing repair input.
var ErrMissingRepairProjection = errors.New("llmstep: repair projection is required for retry")

// ErrExhausted reports that no attempt produced an accepted judgment before
// MaxIter was exhausted.
var ErrExhausted = errors.New("llmstep: no accepted judgment before maxIter")

// Finding is a validator-owned fact about a proposition. It is not
// model-facing repair input.
type Finding struct {
	Summary   string   `json:"summary,omitempty"`
	Codes     []string `json:"codes,omitempty"`
	Locations []string `json:"locations,omitempty"`
}

// Judgment is a deterministic acceptance or rejection of a proposition.
type Judgment struct {
	Accepted bool      `json:"accepted"`
	Findings []Finding `json:"findings,omitempty"`
}

// Repair is application-projected, iteration-stamped information eligible for
// a later prompt render. It is a projection of findings, not the judgment.
type Repair struct {
	Iteration int      `json:"iteration,omitempty"`
	Summary   string   `json:"summary,omitempty"`
	Codes     []string `json:"codes,omitempty"`
	Locations []string `json:"locations,omitempty"`
}

// RepairProjector is the application-owned projection from judgment findings
// to model-facing repair input. The toolkit does not inspect or reinterpret
// the content returned by this hook.
type RepairProjector func([]Finding) ([]Repair, error)

// Step describes one typed structured-output LLM operation. Caller, Render,
// and Validate are required configuration; a nil Validate is rejected before
// Render or Caller.Call. ProjectRepair is required only when a rejected
// attempt will actually retry with model-facing repair input.
type Step[I any, O any] struct {
	Caller        llmadapter.Caller
	Render        func(context.Context, I, []Repair) (string, error)
	Validate      func(context.Context, I, O) (Judgment, error)
	MaxIter       int
	ProjectRepair RepairProjector
}

type Stage string

const (
	StageRender        Stage = "render"
	StageRequest       Stage = "request"
	StageCall          Stage = "call"
	StageDecode        Stage = "decode"
	StageValidate      Stage = "validate"
	StageProjectRepair Stage = "project_repair"
)

type StepError struct {
	Stage     Stage
	Iteration int
	Err       error
}

func (e *StepError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("llmstep: iteration %d %s stage: %v", e.Iteration, e.Stage, e.Err)
}

func (e *StepError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Attempt records one run attempt without retaining the rendered prompt.
type Attempt[O any] struct {
	Iteration int
	// Repair is the owned repair snapshot supplied to this attempt's Render
	// call, including its Codes and Locations slices.
	Repair []Repair
	Call   llmadapter.ValueResult[O]
	// Judgment is the validator-owned decision exactly as returned, including
	// nil-versus-empty slice shape, published as an isolated snapshot. Nil
	// means no judgment occurred. Generic values in Call retain ordinary Go
	// value semantics.
	Judgment *Judgment
	// NextRepair is the application-projected, iteration-stamped repair supplied
	// to the next Render call when another attempt exists, published as an
	// isolated snapshot. It is nil when no later render will run.
	NextRepair []Repair
	Err        error
}

// Result is the typed output plus attempt history from Run.
type Result[O any] struct {
	// Output is the accepted proposition only. Rejected or unjudged decoded
	// values remain in Attempts[n].Call. Output follows ordinary Go value
	// semantics and is not generically cloned.
	Output    O
	HasOutput bool
	// Attempts is an owned snapshot. Its judgment and repair slices are
	// isolated, while generic outputs retain ordinary Go value semantics.
	Attempts []Attempt[O]
}

// Run executes a step and returns the accepted output with attempt history.
func Run[I any, O any](ctx context.Context, step Step[I, O], input I) (Result[O], error) {
	var result Result[O]

	if step.MaxIter < 1 {
		return result, ErrInvalidMaxIter
	}
	if isNilCaller(step.Caller) {
		return result, llmadapter.ErrNilCaller
	}
	if step.Render == nil {
		return result, ErrNilRender
	}
	if step.Validate == nil {
		return result, ErrNilValidate
	}

	contract, err := llmschema.Compile[O]()
	if err != nil {
		return result, err
	}

	var repair []Repair
	for iter := 1; iter <= step.MaxIter; iter++ {
		attempt := Attempt[O]{Iteration: iter, Repair: copyRepair(repair)}
		if err := ctx.Err(); err != nil {
			return fail(result, attempt, StageRender, err)
		}

		renderRepair := copyRepair(attempt.Repair)
		prompt, err := step.Render(ctx, input, renderRepair)
		if err != nil {
			return fail(result, attempt, StageRender, err)
		}
		if err := ctx.Err(); err != nil {
			return fail(result, attempt, StageRender, err)
		}

		call, err := llmadapter.ValueWithContract[O](ctx, step.Caller, prompt, contract)
		attempt.Call = call
		if err != nil {
			stage := valueStage(err)
			return fail(result, attempt, stage, err)
		}

		judgment, err := step.Validate(ctx, input, call.Value)
		attempt.Judgment = copyJudgment(&judgment)
		if err != nil {
			return fail(result, attempt, StageValidate, err)
		}
		if err := ctx.Err(); err != nil {
			return fail(result, attempt, StageValidate, err)
		}
		if judgment.Accepted {
			result.Output = call.Value
			result.HasOutput = true
			result.Attempts = append(result.Attempts, attempt)
			return snapshotResult(result), nil
		}
		if iter == step.MaxIter {
			result.Attempts = append(result.Attempts, attempt)
			return snapshotResult(result), fmt.Errorf("%w: maxIter=%d", ErrExhausted, step.MaxIter)
		}
		if step.ProjectRepair == nil {
			return fail(result, attempt, StageProjectRepair, ErrMissingRepairProjection)
		}

		nextRepair, err := step.ProjectRepair(copyFindings(judgment.Findings))
		if err != nil {
			return fail(result, attempt, StageProjectRepair, err)
		}
		nextRepair = copyRepair(nextRepair)
		stampIterations(nextRepair, iter)
		attempt.NextRepair = copyRepair(nextRepair)
		repair = copyRepair(nextRepair)
		result.Attempts = append(result.Attempts, attempt)
	}

	return snapshotResult(result), fmt.Errorf("%w: maxIter=%d", ErrExhausted, step.MaxIter)
}

func isNilCaller(caller llmadapter.Caller) bool {
	if caller == nil {
		return true
	}

	value := reflect.ValueOf(caller)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func valueStage(err error) Stage {
	var valueErr *llmadapter.ValueError
	if errors.As(err, &valueErr) {
		switch valueErr.Stage {
		case llmadapter.ValueStageRequest:
			return StageRequest
		case llmadapter.ValueStageCall:
			return StageCall
		case llmadapter.ValueStageDecode:
			return StageDecode
		}
	}
	return StageCall
}

func fail[O any](result Result[O], attempt Attempt[O], stage Stage, err error) (Result[O], error) {
	stepErr := &StepError{Stage: stage, Iteration: attempt.Iteration, Err: err}
	attempt.Err = stepErr
	result.Attempts = append(result.Attempts, attempt)
	return snapshotResult(result), stepErr
}

func snapshotResult[O any](result Result[O]) Result[O] {
	result.Attempts = append([]Attempt[O](nil), result.Attempts...)
	for i := range result.Attempts {
		result.Attempts[i].Repair = copyRepair(result.Attempts[i].Repair)
		result.Attempts[i].Judgment = copyJudgment(result.Attempts[i].Judgment)
		result.Attempts[i].NextRepair = copyRepair(result.Attempts[i].NextRepair)
	}
	return result
}

func stampIterations(repair []Repair, iteration int) {
	for i := range repair {
		repair[i].Iteration = iteration
	}
}

func copyJudgment(judgment *Judgment) *Judgment {
	if judgment == nil {
		return nil
	}
	copied := *judgment
	copied.Findings = copyFindings(judgment.Findings)
	return &copied
}

func copyFindings(findings []Finding) []Finding {
	if findings == nil {
		return nil
	}
	copied := make([]Finding, len(findings))
	for i, item := range findings {
		copied[i] = Finding{
			Summary:   item.Summary,
			Codes:     copyStrings(item.Codes),
			Locations: copyStrings(item.Locations),
		}
	}
	return copied
}

func copyRepair(repair []Repair) []Repair {
	if repair == nil {
		return nil
	}
	copied := make([]Repair, len(repair))
	for i, item := range repair {
		copied[i] = Repair{
			Iteration: item.Iteration,
			Summary:   item.Summary,
			Codes:     copyStrings(item.Codes),
			Locations: copyStrings(item.Locations),
		}
	}
	return copied
}

func copyStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append(make([]string, 0, len(values)), values...)
}
