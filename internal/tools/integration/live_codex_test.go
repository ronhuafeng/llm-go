package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk"
	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
	"github.com/ronhuafeng/llm-go/internal/tools/internal/livecodex"
	codexcaller "github.com/ronhuafeng/llm-go/llmcaller/codex"
	"github.com/ronhuafeng/llm-go/llmkit/llmadapter"
)

type liveArithmeticResult struct {
	Number int `json:"number"`
}

func TestLiveCodexStructuredCall(t *testing.T) {
	client, fixture := newLiveCodexClient(t)
	caller, err := codexcaller.New(liveApplicationOptions(client.ThreadRunner(), fixture))
	if err != nil {
		t.Fatal("cannot construct the composed caller")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	t.Log("live_story.stage=structured-call outcome=started")
	// Value validates against its public compiled schema before typed decoding.
	got, err := llmadapter.Value[liveArithmeticResult](ctx, caller, "What is 3 + 4? Return the result according to the output schema.")
	if err != nil {
		reportLiveFailure(t, client, liveThreadStart(got.Response), err)
		t.Fatal("live structured call failed; see typed failure facts")
	}
	if got.Value.Number != 7 {
		failLiveAssertion(t, "structured arithmetic result is not 7")
	}
	if got.Response.Execution.BackendName != "codex" {
		failLiveAssertion(t, "backend is not codex")
	}
	requireUnknownProvider(t, got.Response.Execution)
	requireUnknownModel(t, got.Response.Execution)
	details, ok := got.Response.BackendDetails.(codexcaller.Details)
	if !ok {
		failLiveAssertion(t, "backend details are not Codex details")
	}
	if details.Run.Start.Thread.ID == "" {
		failLiveAssertion(t, "composed call is missing its new thread identity")
	}
	if details.Run.Start.ModelProvider != livecodex.Provider || details.Run.Start.Model == "" {
		failLiveAssertion(t, "thread configuration does not match the isolated fixture")
	}
	if err := liveTurnError(details.Run.Run, ""); err != nil {
		failLiveAssertion(t, err.Error())
	}
	t.Log("live_story.stage=structured-call outcome=completed")
}

func TestLiveCodexThreadContinuation(t *testing.T) {
	client, fixture := newLiveCodexClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	runner := client.ThreadRunner()
	startRequest := liveApplicationOptions(runner, fixture).Defaults
	startRequest.Thread.Ephemeral = protocolv2.Value(false)
	startRequest.Turn.Input = liveTextInput("What is 2 + 3? Reply briefly.")
	startRequest.AdmitTurn = func(start protocolv2.ThreadStartResponse, pending protocolv2.TurnStartParams) error {
		if err := admitReadOnlyPolicies(start.ApprovalPolicy, start.Sandbox, pending); err != nil {
			return err
		}
		if start.Thread.Ephemeral || start.Thread.ID == "" || pending.ThreadID != start.Thread.ID {
			return errors.New("live persistent Start did not admit the intended thread")
		}
		return admitLiveProvider(start, livecodex.Provider)
	}
	t.Log("live_story.stage=persistent-start outcome=started")
	started, err := runner.Start(ctx, startRequest)
	cleanupLiveThread(t, client, started.Start.Thread.ID)
	if err != nil {
		reportLiveFailure(t, client, started.Start, err)
		t.Fatal("live persistent Start failed; see typed failure facts")
	}
	parentID := started.Start.Thread.ID
	if parentID == "" || started.Start.Thread.Ephemeral {
		failLiveAssertion(t, "persistent Start did not return a persistent thread")
	}
	if err := liveTurnError(started.Run, ""); err != nil {
		failLiveAssertion(t, err.Error())
	}
	t.Log("live_story.stage=persistent-start outcome=completed")

	t.Log("live_story.stage=resume outcome=started")
	resumed, err := runner.Resume(ctx, liveResumeRequest(fixture, parentID, "What is 4 + 5? Reply briefly."))
	if err != nil {
		reportLiveFailure(t, client, resumeFailureObservation(resumed.Resume), err)
		t.Fatal("live Resume failed; see typed failure facts")
	}
	if resumed.Resume.Thread.ID != parentID {
		failLiveAssertion(t, "Resume returned a different parent thread")
	}
	if err := liveTurnError(resumed.Run, started.Run.Turn.ID); err != nil {
		failLiveAssertion(t, err.Error())
	}
	t.Log("live_story.stage=resume outcome=completed")

	t.Log("live_story.stage=fork outcome=started")
	ephemeral := false
	forked, err := client.Threads().Fork(ctx, protocolv2.ThreadForkParams{
		ThreadID: parentID, Ephemeral: &ephemeral,
		CWD: protocolv2.Value(fixture.Workspace), Model: protocolv2.Value(livecodex.Model),
		ModelProvider:  protocolv2.Value(livecodex.Provider),
		ApprovalPolicy: protocolv2.Value(protocolv2.NewAskForApprovalNever()),
		Sandbox:        protocolv2.Value(protocolv2.SandboxModeReadOnly),
	})
	if forked.Thread.ID != parentID {
		cleanupLiveThread(t, client, forked.Thread.ID)
	}
	if err != nil {
		reportLiveFailure(t, client, protocolv2.ThreadStartResponse{}, err)
		t.Fatal("live Fork failed; see typed failure facts")
	}
	forkID := forked.Thread.ID
	if forkID == "" || forkID == parentID {
		failLiveAssertion(t, "Fork did not return a distinct thread")
	}
	t.Log("live_story.stage=fork outcome=completed")

	// Resume is the public composed continuation API. Its pending turn is
	// admitted only for the identity returned by Fork, never for the parent.
	t.Log("live_story.stage=fork-turn outcome=started")
	forkWork, err := runner.Resume(ctx, liveResumeRequest(fixture, forkID, "What is 6 + 7? Reply briefly."))
	if err != nil {
		reportLiveFailure(t, client, resumeFailureObservation(forkWork.Resume), err)
		t.Fatal("live forked work failed; see typed failure facts")
	}
	if forkWork.Resume.Thread.ID != forkID {
		failLiveAssertion(t, "new work did not use the returned fork identity")
	}
	// Turn IDs are compared within the parent above, not across distinct threads.
	if err := liveTurnError(forkWork.Run, ""); err != nil {
		failLiveAssertion(t, err.Error())
	}
	t.Log("live_story.stage=fork-turn outcome=completed")
}

func newLiveCodexClient(t *testing.T) (*codexsdk.Client, livecodex.Fixture) {
	t.Helper()
	if os.Getenv("LLMGO_LIVE_CODEX") != "1" {
		t.Skip("use livecodex run to execute the real Codex stories")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal("cannot find the live fixture source")
	}
	root := filepath.Clean(filepath.Join(wd, "..", "..", ".."))
	fixture, err := livecodex.Prepare(root, t.TempDir(), os.Getenv("MINI_CODEX_BASE_URL"), os.Getenv("MINI_CODEX_API_KEY"))
	if err != nil {
		t.Log("live_failure.stage=fixture")
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", fixture.Home)
	client, err := codexsdk.New(codexsdk.ClientOptions{
		CWD: fixture.Workspace, Command: []string{"codex", "app-server", "--listen", "stdio://"},
	})
	if err != nil {
		t.Log("live_failure.stage=startup")
		t.Fatal("cannot start the live app-server")
	}
	// Registered before thread cleanup so the client stays open for Archive.
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Log("live_failure.stage=cleanup")
			t.Error("cannot close the owned live app-server")
		}
	})
	return client, fixture
}

func cleanupLiveThread(t *testing.T, client *codexsdk.Client, threadID string) {
	t.Helper()
	if threadID == "" {
		return
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := client.Threads().Archive(ctx, protocolv2.ThreadArchiveParams{ThreadID: threadID}); err != nil {
			t.Log("live_failure.stage=cleanup")
			t.Error("cannot archive an owned live thread")
		}
	})
}

func liveTextInput(prompt string) []protocolv2.UserInput {
	return []protocolv2.UserInput{protocolv2.NewUserInputText(protocolv2.UserInputText{Text: prompt})}
}

func liveResumeRequest(fixture livecodex.Fixture, threadID, prompt string) codexsdk.ResumeThreadRunRequest {
	return codexsdk.ResumeThreadRunRequest{
		Thread: protocolv2.ThreadResumeParams{
			ThreadID: threadID, CWD: protocolv2.Value(fixture.Workspace),
			Model: protocolv2.Value(livecodex.Model), ModelProvider: protocolv2.Value(livecodex.Provider),
			ApprovalPolicy: protocolv2.Value(protocolv2.NewAskForApprovalNever()),
			Sandbox:        protocolv2.Value(protocolv2.SandboxModeReadOnly),
		},
		Turn: protocolv2.TurnStartParams{
			Input: liveTextInput(prompt), Effort: protocolv2.Value(protocolv2.ReasoningEffort(livecodex.Reasoning)),
			ApprovalPolicy: protocolv2.Value(protocolv2.NewAskForApprovalNever()),
			SandboxPolicy:  protocolv2.Value(protocolv2.NewSandboxPolicyReadOnly(protocolv2.SandboxPolicyReadOnly{})),
		},
		AdmitTurn: func(resume protocolv2.ThreadResumeResponse, pending protocolv2.TurnStartParams) error {
			if err := admitReadOnlyPolicies(resume.ApprovalPolicy, resume.Sandbox, pending); err != nil {
				return err
			}
			if threadID == "" || resume.Thread.ID != threadID || pending.ThreadID != threadID || resume.Thread.Ephemeral {
				return errors.New("live Resume did not admit the intended persistent thread")
			}
			if resume.ModelProvider != livecodex.Provider {
				return errors.New("live Resume provider does not match the isolated fixture")
			}
			return nil
		},
	}
}

func resumeFailureObservation(resume protocolv2.ThreadResumeResponse) protocolv2.ThreadStartResponse {
	// Only observed model/provider fields are used by failure diagnostics.
	return protocolv2.ThreadStartResponse{Model: resume.Model, ModelProvider: resume.ModelProvider}
}

func liveThreadStart(response llmadapter.Response) protocolv2.ThreadStartResponse {
	details, ok := response.BackendDetails.(codexcaller.Details)
	if !ok {
		return protocolv2.ThreadStartResponse{}
	}
	return details.Run.Start
}

func failLiveAssertion(t *testing.T, message string) {
	t.Helper()
	t.Log("live_failure.stage=assertion")
	t.Fatal(message)
}
