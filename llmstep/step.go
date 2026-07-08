package llmstep

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/ronhuafeng/llmkit-go/llmadapter"
	"github.com/ronhuafeng/llmkit-go/settle"
)

// ErrUnsafeFeedback reports validation feedback rejected by a sanitizer.
var ErrUnsafeFeedback = errors.New("llmstep: unsafe feedback")

// Feedback is sanitized validation information that may be sent to the model on
// a retry.
type Feedback struct {
	Iteration int      `json:"iteration,omitempty"`
	Summary   string   `json:"summary,omitempty"`
	Codes     []string `json:"codes,omitempty"`
	Locations []string `json:"locations,omitempty"`
}

// ValidationResult is the validator's decision for one typed output.
type ValidationResult struct {
	Settled  bool       `json:"settled"`
	Feedback []Feedback `json:"feedback,omitempty"`
}

// FeedbackSanitizer checks and narrows feedback before retry prompt rendering.
type FeedbackSanitizer func([]Feedback) ([]Feedback, error)

// Step describes one typed structured-output LLM operation.
type Step[I any, O any] struct {
	Caller    llmadapter.Caller
	Render    func(context.Context, I, []Feedback) (string, error)
	Validate  func(context.Context, I, O) (ValidationResult, error)
	MaxIter   int
	Sanitizer FeedbackSanitizer
}

// Attempt records one rendered prompt, the feedback used to render it, and the
// validation result for the typed output.
type Attempt struct {
	Iteration  int
	Prompt     string
	Feedback   []Feedback
	Validation ValidationResult
}

// Result is the typed output plus attempt history from RunDetailed.
type Result[O any] struct {
	Output   O
	Attempts []Attempt
}

// Run executes a step and returns only the settled typed output.
func Run[I any, O any](ctx context.Context, step Step[I, O], input I) (O, error) {
	result, err := RunDetailed(ctx, step, input)
	if err != nil {
		var zero O
		return zero, err
	}
	return result.Output, nil
}

// RunDetailed executes a step and returns the settled output with attempt
// history.
func RunDetailed[I any, O any](ctx context.Context, step Step[I, O], input I) (Result[O], error) {
	var result Result[O]

	if step.MaxIter < 1 {
		return result, settle.ErrInvalidMaxIter
	}
	if step.Caller == nil {
		return result, llmadapter.ErrNilCaller
	}
	if step.Render == nil {
		return result, llmadapter.ErrNilRender
	}

	sanitize := step.Sanitizer
	if sanitize == nil {
		sanitize = StrictFeedbackSanitizer
	}

	var feedback []Feedback
	for iter := 1; iter <= step.MaxIter; iter++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		renderFeedback := copyFeedback(feedback)
		prompt, err := step.Render(ctx, input, renderFeedback)
		if err != nil {
			return result, err
		}

		output, err := llmadapter.Value[O](ctx, step.Caller, prompt)
		if err != nil {
			return result, err
		}

		validation := ValidationResult{Settled: true}
		if step.Validate != nil {
			validation, err = step.Validate(ctx, input, output)
			if err != nil {
				return result, err
			}
		}

		attempt := Attempt{
			Iteration:  iter,
			Prompt:     prompt,
			Feedback:   renderFeedback,
			Validation: copyValidationResult(validation),
		}
		if validation.Settled {
			result.Output = output
			result.Attempts = append(result.Attempts, attempt)
			return result, nil
		}

		feedback, err = sanitize(validation.Feedback)
		if err != nil {
			return result, err
		}
		stampMissingIterations(feedback, iter)
		attempt.Validation.Feedback = copyFeedback(feedback)
		result.Attempts = append(result.Attempts, attempt)
	}

	return result, fmt.Errorf("%w: maxIter=%d", settle.ErrUnsettled, step.MaxIter)
}

// StrictFeedbackSanitizer accepts short, identifier-oriented feedback and
// rejects strings that look like secrets, credentials, URLs, or private paths.
func StrictFeedbackSanitizer(feedback []Feedback) ([]Feedback, error) {
	sanitized := make([]Feedback, 0, len(feedback))
	for i, item := range feedback {
		next := Feedback{
			Iteration: item.Iteration,
			Summary:   strings.TrimSpace(item.Summary),
			Codes:     sanitizeStrings(item.Codes),
			Locations: sanitizeStrings(item.Locations),
		}
		if next.Summary == "" && len(next.Codes) == 0 && len(next.Locations) == 0 {
			continue
		}
		if next.Summary != "" && !safeSummary(next.Summary) {
			return nil, fmt.Errorf("%w: feedback[%d].summary", ErrUnsafeFeedback, i)
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

func stampMissingIterations(feedback []Feedback, iteration int) {
	for i := range feedback {
		if feedback[i].Iteration == 0 {
			feedback[i].Iteration = iteration
		}
	}
}

func copyValidationResult(result ValidationResult) ValidationResult {
	return ValidationResult{
		Settled:  result.Settled,
		Feedback: copyFeedback(result.Feedback),
	}
}

func copyFeedback(feedback []Feedback) []Feedback {
	if len(feedback) == 0 {
		return nil
	}
	copied := make([]Feedback, len(feedback))
	for i, item := range feedback {
		copied[i] = Feedback{
			Iteration: item.Iteration,
			Summary:   item.Summary,
			Codes:     append([]string(nil), item.Codes...),
			Locations: append([]string(nil), item.Locations...),
		}
	}
	return copied
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

var unsafeFeedbackPattern = regexp.MustCompile(`(?i)(https?://|www\.|authorization\s*:|bearer\s+[a-z0-9._~+/=-]+|api[_ -]?key|password|passwd|secret|token\s*[:=]|sk-[a-z0-9]{12,}|[a-z]:\\|~[/\\]|/(users|home|var|etc|private|tmp)/)`)

func safeSummary(summary string) bool {
	if len(summary) > 240 || unsafeFeedbackPattern.MatchString(summary) {
		return false
	}
	return printableSingleLine(summary)
}

func safeTokens(tokens []string, field string, feedbackIndex int) error {
	for tokenIndex, token := range tokens {
		if !safeToken(token) {
			return fmt.Errorf("%w: feedback[%d].%s[%d]", ErrUnsafeFeedback, feedbackIndex, field, tokenIndex)
		}
	}
	return nil
}

func safeToken(token string) bool {
	if token == "" || len(token) > 96 || unsafeFeedbackPattern.MatchString(token) {
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

func printableSingleLine(value string) bool {
	for _, r := range value {
		if r == '\n' || r == '\r' || r == '\t' {
			return false
		}
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
