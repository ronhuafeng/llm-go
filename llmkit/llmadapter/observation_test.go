package llmadapter

import (
	"context"
	"testing"
)

func TestObservationZeroValueIsUnknown(t *testing.T) {
	var tokens Observation[int64]
	var model Observation[string]
	if tokens.Present() {
		t.Fatal("zero Observation[int64] must be unknown")
	}
	if got, ok := tokens.Value(); ok || got != 0 {
		t.Fatalf("unknown tokens = (%d, %t), want (0, false)", got, ok)
	}
	if model.Present() {
		t.Fatal("zero Observation[string] must be unknown")
	}
	if got, ok := model.Value(); ok || got != "" {
		t.Fatalf("unknown model = (%q, %t), want (\"\", false)", got, ok)
	}
}

func TestObservationDistinguishesObservedZeroFromUnknown(t *testing.T) {
	zero := Observed[int64](0)
	var unknown Observation[int64]
	if !zero.Present() {
		t.Fatal("observed 0 must be present")
	}
	if got, ok := zero.Value(); !ok || got != 0 {
		t.Fatalf("observed 0 = (%d, %t), want (0, true)", got, ok)
	}
	if unknown.Present() || zero == unknown {
		t.Fatalf("observed 0 must not equal unknown: %#v vs %#v", zero, unknown)
	}
}

func TestObservationDistinguishesObservedEmptyModelFromUnknown(t *testing.T) {
	empty := Observed("")
	var unknown Observation[string]
	if !empty.Present() {
		t.Fatal("observed empty model must be present")
	}
	if got, ok := empty.Value(); !ok || got != "" {
		t.Fatalf("observed empty model = (%q, %t), want (\"\", true)", got, ok)
	}
	if unknown.Present() || empty == unknown {
		t.Fatalf("observed empty model must not equal unknown: %#v vs %#v", empty, unknown)
	}
}

func TestValueDetailedDoesNotPromoteRequestIntoNeutralObservations(t *testing.T) {
	result, err := ValueDetailed[bool](context.Background(), callerFunc(func(context.Context, Request) (Response, error) {
		return Response{FinalResponse: `true`}, nil
	}), "Use model gpt-requested and expect 12 input tokens.")
	if err != nil {
		t.Fatal(err)
	}
	if result.Response.Execution.Model.Present() {
		t.Fatalf("Model = %#v, want unknown; request text must not become an observed model", result.Response.Execution.Model)
	}
	if result.Response.Execution.EffectiveModel != "" {
		t.Fatalf("EffectiveModel = %q, want empty; request text must not become a collapsing model view", result.Response.Execution.EffectiveModel)
	}
	if result.Response.Execution.Usage != nil {
		t.Fatalf("Usage = %#v, want nil; request text must not become observed usage", result.Response.Execution.Usage)
	}
	if result.Response.Execution.ProviderName != "" {
		t.Fatalf("ProviderName = %q, want empty when the caller reported no provider", result.Response.Execution.ProviderName)
	}
}

func TestValueDetailedPreservesUnknownAndObservedUsageSnapshots(t *testing.T) {
	usage := &TokenUsage{
		Input: Observed[int64](0),
	}
	result, err := ValueDetailed[bool](context.Background(), callerFunc(func(context.Context, Request) (Response, error) {
		return Response{FinalResponse: `true`, Execution: ExecutionEvidence{Usage: usage, Model: Observed("served")}}, nil
	}), "prompt")
	if err != nil {
		t.Fatal(err)
	}
	usage.Input = Observed[int64](99)
	usage.Output = Observed[int64](7)
	got, ok := result.Response.Execution.Usage.Input.Value()
	if !ok || got != 0 {
		t.Fatalf("published Input = (%d, %t), want observed 0", got, ok)
	}
	if result.Response.Execution.Usage.Output.Present() {
		t.Fatalf("published Output = %#v, want unknown", result.Response.Execution.Usage.Output)
	}
	model, ok := result.Response.Execution.Model.Value()
	if !ok || model != "served" {
		t.Fatalf("published Model = (%q, %t), want observed served", model, ok)
	}
}

func TestScalarCompatibilityViewsDoNotManufactureObservations(t *testing.T) {
	evidence := ExecutionEvidence{EffectiveModel: "gpt-requested"}
	if evidence.Model.Present() {
		t.Fatalf("Model = %#v, want unknown; EffectiveModel must not populate presence", evidence.Model)
	}
	usage := TokenUsage{InputTokens: 0, OutputTokens: 7}
	if usage.Input.Present() || usage.Output.Present() {
		t.Fatalf("token observations = %#v, want unknown; int64 fields must not populate presence", usage)
	}
}
