package llmstep_test

import (
	"context"
	"fmt"

	"github.com/ronhuafeng/llm-go/llmkit/llmadapter"
	"github.com/ronhuafeng/llm-go/llmkit/llmstep"
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

func ExampleRunDetailed() {
	caller := &exampleCaller{responses: []llmadapter.Response{
		{FinalResponse: `{"verdict":"maybe"}`},
		{FinalResponse: `{"verdict":"pass"}`},
	}}

	result, err := llmstep.RunDetailed(context.Background(), llmstep.Step[reviewInput, reviewResult]{
		Caller: caller,
		Render: func(ctx context.Context, input reviewInput, repair []llmstep.Repair) (string, error) {
			if len(repair) > 0 {
				return input.Question + " Fix: " + repair[0].Codes[0], nil
			}
			return input.Question, nil
		},
		Validate: func(ctx context.Context, input reviewInput, output reviewResult) (llmstep.Judgment, error) {
			if output.Verdict == "pass" || output.Verdict == "fail" {
				return llmstep.Judgment{Accepted: true}, nil
			}
			return llmstep.Judgment{
				Findings: []llmstep.Finding{{
					Codes: []string{"invalid_verdict"},
				}},
			}, nil
		},
		MaxIter: 2,
	}, reviewInput{Question: "Review this patch."})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.Output.Verdict)
	fmt.Println(len(result.Attempts))

	// Output:
	// pass
	// 2
}
