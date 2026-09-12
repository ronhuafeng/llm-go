package llmadapter_test

import (
	"context"
	"fmt"
	"maps"

	"github.com/ronhuafeng/llm-go/llmkit/llmadapter"
	"github.com/ronhuafeng/llm-go/llmkit/llmschema"
)

type isolatedDetails struct {
	Headers map[string]string
}

func (isolatedDetails) BackendName() string { return "example-backend" }

type ownershipCaller struct {
	runtimeHeaders map[string]string
}

// ownershipCaller implements llmadapter.Caller as inference only: it returns a
// proposition and isolated evidence. The request cannot grant an effect, and
// model output is not authority.
func (caller ownershipCaller) Call(context.Context, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{
		FinalResponse: llmadapter.Observed(`true`),
		Execution:     llmadapter.ExecutionEvidence{BackendName: "example-backend"},
		BackendDetails: isolatedDetails{
			Headers: maps.Clone(caller.runtimeHeaders),
		},
	}, nil
}

func ExampleObservation() {
	var unknown llmadapter.Observation[int64]
	zero := llmadapter.Observed[int64](0)
	fmt.Println(unknown.Present())
	count, ok := zero.Value()
	fmt.Println(ok, count)
	// Output:
	// false
	// true 0
}

func ExampleExecutionEvidence_identity() {
	evidence := llmadapter.ExecutionEvidence{BackendName: "codex"}
	_, providerKnown := evidence.ProviderName.Value()
	fmt.Println(evidence.BackendName, providerKnown)
	evidence.ObserveProviderName("observed-provider")
	provider, providerKnown := evidence.ProviderName.Value()
	fmt.Println(providerKnown, provider)
	// Output:
	// codex false
	// true observed-provider
}

func ExampleValue_backendDetailsOwnership() {
	runtimeHeaders := map[string]string{"trace": "trace-1"}
	result, err := llmadapter.Value[bool](context.Background(), ownershipCaller{
		runtimeHeaders: runtimeHeaders,
	}, "Return true.")
	if err != nil {
		panic(err)
	}

	// The adapter cloned its backend-specific reference fields before
	// publication, so later runtime mutations cannot change published details.
	runtimeHeaders["trace"] = "trace-2"
	details := result.Response.BackendDetails.(isolatedDetails)
	fmt.Println(details.Headers["trace"])

	// Output:
	// trace-1
}

func ExampleValueWithContract() {
	contract, err := llmschema.Compile[bool]()
	if err != nil {
		panic(err)
	}
	result, err := llmadapter.ValueWithContract(
		context.Background(),
		ownershipCaller{runtimeHeaders: map[string]string{}},
		"Return true.",
		contract,
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Value)
	// Output: true
}
