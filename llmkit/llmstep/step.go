package llmstep

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"unicode"

	"github.com/ronhuafeng/llm-go/llmkit/llmadapter"
)

// ErrUnsafeRepair reports model-facing repair input rejected by a sanitizer.
var ErrUnsafeRepair = errors.New("llmstep: unsafe repair")

var ErrNilRender = errors.New("llmstep: render is nil")

// ErrInvalidMaxIter reports a step configured with a non-positive retry bound.
var ErrInvalidMaxIter = errors.New("llmstep: maxIter must be at least 1")

// ErrUnsettled reports that no attempt produced an accepted judgment before
// the retry bound was exhausted.
var ErrUnsettled = errors.New("llmstep: output remains unsettled")

// ErrNoJudgment reports that a proposition was produced without a
// deterministic judgment, so the step cannot accept it.
var ErrNoJudgment = errors.New("llmstep: no deterministic judgment")

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

// Repair is sanitizer-owned, iteration-stamped information eligible for a
// later prompt render. It is a projection of findings, not the judgment.
type Repair struct {
	Iteration int      `json:"iteration,omitempty"`
	Summary   string   `json:"summary,omitempty"`
	Codes     []string `json:"codes,omitempty"`
	Locations []string `json:"locations,omitempty"`
}

// RepairSanitizer projects judgment findings into model-facing repair input.
type RepairSanitizer func([]Finding) ([]Repair, error)

// Step describes one typed structured-output LLM operation.
type Step[I any, O any] struct {
	Caller    llmadapter.Caller
	Render    func(context.Context, I, []Repair) (string, error)
	Validate  func(context.Context, I, O) (Judgment, error)
	MaxIter   int
	Sanitizer RepairSanitizer
}

type Stage string

const (
	StageRender   Stage = "render"
	StageRequest  Stage = "request"
	StageCall     Stage = "call"
	StageDecode   Stage = "decode"
	StageValidate Stage = "validate"
	StageSanitize Stage = "sanitize"
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
	// NextRepair is the sanitizer-owned, iteration-stamped repair supplied to
	// the next Render call when another attempt exists, published as an
	// isolated snapshot. It is nil when no later render will run.
	NextRepair []Repair
	Err        error
}

// Result is the typed output plus attempt history from RunDetailed.
type Result[O any] struct {
	// Output follows ordinary Go value semantics and is not generically cloned.
	Output    O
	HasOutput bool
	// Attempts is an owned snapshot. Its judgment and repair slices are
	// isolated, while generic outputs retain ordinary Go value semantics.
	Attempts []Attempt[O]
}

// RunDetailed executes a step and returns the accepted output with attempt
// history.
func RunDetailed[I any, O any](ctx context.Context, step Step[I, O], input I) (Result[O], error) {
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

	sanitize := step.Sanitizer
	if sanitize == nil {
		sanitize = StrictRepairSanitizer
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

		call, err := llmadapter.ValueDetailed[O](ctx, step.Caller, prompt)
		attempt.Call = call
		if err != nil {
			stage := valueStage(err)
			return fail(result, attempt, stage, err)
		}
		result.Output = call.Value
		result.HasOutput = true

		if step.Validate == nil {
			result.Attempts = append(result.Attempts, attempt)
			return snapshotResult(result), ErrNoJudgment
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
			result.Attempts = append(result.Attempts, attempt)
			return snapshotResult(result), nil
		}
		if iter == step.MaxIter {
			result.Attempts = append(result.Attempts, attempt)
			return snapshotResult(result), fmt.Errorf("%w: maxIter=%d", ErrUnsettled, step.MaxIter)
		}

		nextRepair, err := sanitize(copyFindings(judgment.Findings))
		if err != nil {
			return fail(result, attempt, StageSanitize, err)
		}
		nextRepair = copyRepair(nextRepair)
		stampIterations(nextRepair, iter)
		attempt.NextRepair = copyRepair(nextRepair)
		repair = copyRepair(nextRepair)
		result.Attempts = append(result.Attempts, attempt)
	}

	return snapshotResult(result), fmt.Errorf("%w: maxIter=%d", ErrUnsettled, step.MaxIter)
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

// StrictRepairSanitizer accepts identifier-oriented Codes and Locations and
// rejects every non-empty free-form Summary. It is not a DLP system, secret
// scanner, or privacy guarantee; applications must still redact validator
// findings before returning them.
func StrictRepairSanitizer(findings []Finding) ([]Repair, error) {
	sanitized := make([]Repair, 0, len(findings))
	for i, item := range findings {
		if strings.TrimSpace(item.Summary) != "" {
			return nil, fmt.Errorf("%w: findings[%d].summary", ErrUnsafeRepair, i)
		}
		next := Repair{
			Codes:     sanitizeStrings(item.Codes),
			Locations: sanitizeStrings(item.Locations),
		}
		if len(next.Codes) == 0 && len(next.Locations) == 0 {
			continue
		}
		if err := safeTokens(next.Codes, "codes", i); err != nil {
			return nil, err
		}
		if err := safeTokens(next.Locations, "locations", i); err != nil {
			return nil, err
		}
		sanitized = append(sanitized, next)
	}
	return sanitized, nil
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

func sanitizeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	sanitized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			sanitized = append(sanitized, value)
		}
	}
	return sanitized
}

var unsafeRepairPattern = regexp.MustCompile(`(?i)(https?://|www\.|authorization\s*:|bearer\s+[a-z0-9._~+/=-]+|api[_ -]?key|password|passwd|secret|token\s*[:=]|sk-[a-z0-9]{12,}|[a-z]:\\|~[/\\]|/(users|home|var|etc|private|tmp)/)`)

func safeTokens(tokens []string, field string, findingIndex int) error {
	for tokenIndex, token := range tokens {
		if !safeToken(token) {
			return fmt.Errorf("%w: findings[%d].%s[%d]", ErrUnsafeRepair, findingIndex, field, tokenIndex)
		}
	}
	return nil
}

func safeToken(token string) bool {
	if token == "" || len(token) > 96 || unsafeRepairPattern.MatchString(token) {
		return false
	}
	for _, r := range token {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		switch r {
		case '_', '-', '.', ':', '#':
			continue
		default:
			return false
		}
	}
	return true
}
