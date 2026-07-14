package llmadapter_test

import (
	"context"
	"fmt"
	"maps"

	"github.com/ronhuafeng/llmkit-go/llmadapter"
)

type isolatedDetails struct {
	Headers map[string]string
}

func (isolatedDetails) ProviderName() string { return "example" }

type ownershipCaller struct {
	runtimeHeaders map[string]string
}

func (caller ownershipCaller) Call(context.Context, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{
		FinalResponse: `true`,
		Execution:     llmadapter.ExecutionEvidence{ProviderName: "example"},
		ProviderDetails: isolatedDetails{
			Headers: maps.Clone(caller.runtimeHeaders),
		},
	}, nil
}

func ExampleValueDetailed_providerDetailsOwnership() {
	runtimeHeaders := map[string]string{"trace": "trace-1"}
	result, err := llmadapter.ValueDetailed[bool](context.Background(), ownershipCaller{
		runtimeHeaders: runtimeHeaders,
	}, "Return true.")
	if err != nil {
		panic(err)
	}

	// The adapter cloned its provider-specific reference fields before
	// publication, so later runtime mutations cannot change published details.
	runtimeHeaders["trace"] = "trace-2"
	details := result.Response.ProviderDetails.(isolatedDetails)
	fmt.Println(details.Headers["trace"])

	// Output:
	// trace-1
}
