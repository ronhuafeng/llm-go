package integration_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ronhuafeng/llm-go/llmkit/llmadapter"
	"github.com/ronhuafeng/llm-go/llmkit/llmstep"
)

type deployRequest struct {
	Question string
}

type deployProposition struct {
	Target   string `json:"target"`
	Approved bool   `json:"approved"`
}

type authorityToEffectInput struct {
	modelJSON string
	allow     []string
	deployer  *exampleDeployer
}

type authorityToEffectResult struct {
	accepted   bool
	authorized bool
	invoked    int
	effectErr  error
}

type exampleDeployer struct {
	calls []string
	err   error
}

func (d *exampleDeployer) Deploy(target string) error {
	d.calls = append(d.calls, target)
	return d.err
}

type exampleCaller struct {
	body string
}

func (caller exampleCaller) Call(context.Context, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{FinalResponse: caller.body}, nil
}

func authorizeDeploy(prop deployProposition, allow []string) bool {
	for _, target := range allow {
		if target == prop.Target {
			return true
		}
	}
	return false
}

func applyDeploy(prop deployProposition, authorized bool, deployer *exampleDeployer) (invoked int, err error) {
	if !authorized {
		return 0, nil
	}
	return 1, deployer.Deploy(prop.Target)
}

func acceptDeployProposition(modelJSON string) (deployProposition, bool, error) {
	result, err := llmstep.Run(context.Background(), llmstep.Step[deployRequest, deployProposition]{
		Caller: exampleCaller{body: modelJSON},
		Render: func(context.Context, deployRequest, []llmstep.Repair) (string, error) {
			return "Propose a deploy target.", nil
		},
		Validate: func(_ context.Context, _ deployRequest, output deployProposition) (llmstep.Judgment, error) {
			if output.Target == "" {
				return llmstep.Judgment{}, nil
			}
			return llmstep.Judgment{Accepted: true}, nil
		},
		MaxIter: 1,
	}, deployRequest{Question: "deploy"})
	if err != nil {
		return deployProposition{}, false, err
	}
	return result.Output, true, nil
}

func runAuthorityToEffect(t *testing.T, in authorityToEffectInput) authorityToEffectResult {
	t.Helper()
	prop, accepted, err := acceptDeployProposition(in.modelJSON)
	if err != nil {
		t.Fatal(err)
	}
	authorized := authorizeDeploy(prop, in.allow)
	invoked, effectErr := applyDeploy(prop, authorized, in.deployer)
	return authorityToEffectResult{
		accepted:   accepted,
		authorized: authorized,
		invoked:    invoked,
		effectErr:  effectErr,
	}
}

func TestAuthorityToEffectAcceptedDeniedDoesNotInvokeEffect(t *testing.T) {
	deployer := &exampleDeployer{}
	got := runAuthorityToEffect(t, authorityToEffectInput{
		modelJSON: `{"target":"prod","approved":true}`,
		allow:     []string{"staging"},
		deployer:  deployer,
	})
	if !got.accepted || got.authorized || got.invoked != 0 || len(deployer.calls) != 0 {
		t.Fatalf("denied path = %#v calls=%v", got, deployer.calls)
	}
}

func TestAuthorityToEffectAcceptedGrantedInvokesEffect(t *testing.T) {
	deployer := &exampleDeployer{}
	got := runAuthorityToEffect(t, authorityToEffectInput{
		modelJSON: `{"target":"staging","approved":false}`,
		allow:     []string{"staging"},
		deployer:  deployer,
	})
	if !got.accepted || !got.authorized || got.invoked != 1 || got.effectErr != nil || len(deployer.calls) != 1 || deployer.calls[0] != "staging" {
		t.Fatalf("granted path = %#v calls=%v", got, deployer.calls)
	}
}

func TestAuthorityToEffectAuthorizedFailureIsNotAuthorization(t *testing.T) {
	deployer := &exampleDeployer{err: errors.New("deploy failed")}
	got := runAuthorityToEffect(t, authorityToEffectInput{
		modelJSON: `{"target":"staging","approved":true}`,
		allow:     []string{"staging"},
		deployer:  deployer,
	})
	if !got.accepted || !got.authorized || got.invoked != 1 || got.effectErr == nil || got.effectErr.Error() != "deploy failed" {
		t.Fatalf("failed execution path = %#v", got)
	}
}

func Example_authorityToEffect() {
	cases := []struct {
		name      string
		modelJSON string
		allow     []string
		deployErr error
	}{
		{name: "denied", modelJSON: `{"target":"prod","approved":true}`, allow: []string{"staging"}},
		{name: "granted", modelJSON: `{"target":"staging","approved":false}`, allow: []string{"staging"}},
		{name: "failed", modelJSON: `{"target":"staging","approved":true}`, allow: []string{"staging"}, deployErr: errors.New("deploy failed")},
	}
	for _, tc := range cases {
		prop, accepted, err := acceptDeployProposition(tc.modelJSON)
		if err != nil {
			panic(err)
		}
		deployer := &exampleDeployer{err: tc.deployErr}
		authorized := authorizeDeploy(prop, tc.allow)
		invoked, effectErr := applyDeploy(prop, authorized, deployer)
		fmt.Printf("%s accepted=%t authorized=%t invoked=%d err=%v\n", tc.name, accepted, authorized, invoked, effectErr)
	}
	// Output:
	// denied accepted=true authorized=false invoked=0 err=<nil>
	// granted accepted=true authorized=true invoked=1 err=<nil>
	// failed accepted=true authorized=true invoked=1 err=deploy failed
}
