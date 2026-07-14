package llmstep_test

import (
	"context"
	"fmt"

	"github.com/ronhuafeng/llmkit-go/llmadapter"
	"github.com/ronhuafeng/llmkit-go/llmstep"
)

type exampleCaller struct {
	responses []llmadapter.Response
}

func (caller *exampleCaller) Call(ctx context.Context, request llmadapter.Request) (llmadapter.Response, error) {
	if len(caller.responses) == 0 {
		return llmadapter.Response{}, nil
	}
	response := caller.responses[0]
	caller.responses = caller.responses[1:]
	return response, nil
}

type reviewInput struct {
	Question string
}

type reviewResult struct {
	Verdict string `json:"verdict"`
}

func ExampleRun() {
	caller := &exampleCaller{responses: []llmadapter.Response{
		{FinalResponse: `{"verdict":"maybe"}`},
		{FinalResponse: `{"verdict":"pass"}`},
	}}

	result, err := llmstep.Run(context.Background(), llmstep.Step[reviewInput, reviewResult]{
		Caller: caller,
		Render: func(ctx context.Context, input reviewInput, feedback []llmstep.Feedback) (string, error) {
			if len(feedback) > 0 {
				return input.Question + " Fix: " + feedback[0].Codes[0], nil
			}
			return input.Question, nil
		},
		Validate: func(ctx context.Context, input reviewInput, output reviewResult) (llmstep.ValidationResult, error) {
			if output.Verdict == "pass" || output.Verdict == "fail" {
				return llmstep.ValidationResult{Settled: true}, nil
			}
			return llmstep.ValidationResult{
				Feedback: []llmstep.Feedback{{
					Summary: "verdict must be pass or fail",
					Codes:   []string{"invalid_verdict"},
				}},
			}, nil
		},
		MaxIter: 2,
	}, reviewInput{Question: "Review this patch."})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.Verdict)

	// Output:
	// pass
}
