package integration

import (
	"context"
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

// TestLiveCodexSmoke is the migration entry point; #377 replaces its shallow
// result with the two accepted stories using this same fixture and gate.
func TestLiveCodexSmoke(t *testing.T) {
	if os.Getenv("LLMGO_LIVE_CODEX") != "1" {
		t.Skip("set LLMGO_LIVE_CODEX=1 to run the real Codex smoke test")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", "..", ".."))
	fixture, err := livecodex.Prepare(root, t.TempDir(), os.Getenv("MINI_CODEX_BASE_URL"), os.Getenv("MINI_CODEX_API_KEY"))
	if err != nil {
		t.Log("live_failure.stage=fixture")
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", fixture.Home)

	client, err := codexsdk.New(codexsdk.ClientOptions{
		CWD:     fixture.Workspace,
		Command: []string{"codex", "app-server", "--listen", "stdio://"},
	})
	if err != nil {
		t.Log("live_failure.stage=startup")
		t.Fatal("cannot start the live app-server")
	}
	defer client.Close()

	options := liveSmokeApplicationOptions(client.ThreadRunner(), livecodex.Provider)
	options.Defaults.Thread.CWD = protocolv2.Value(fixture.Workspace)
	options.Defaults.Thread.Model = protocolv2.Value(livecodex.Model)
	options.Defaults.Turn.Effort = protocolv2.Value(protocolv2.ReasoningEffort(livecodex.Reasoning))
	caller, err := codexcaller.New(options)
	if err != nil {
		t.Fatal("cannot construct the composed caller")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	type result struct {
		Answer string `json:"answer"`
	}
	got, err := llmadapter.Value[result](ctx, caller, `Return JSON with answer set to "ok".`)
	if err != nil {
		reportLiveFailure(t, client, liveThreadStart(got.Response), err)
		t.Fatal("live composed turn failed; see typed failure facts")
	}
	if got.Value.Answer == "" {
		failLiveAssertion(t, "typed result is empty")
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
	if details.Run.Start.Thread.ID == "" || details.Run.Run.Turn.ID == "" {
		failLiveAssertion(t, "exact run is missing thread/turn identity")
	}
	if details.Run.Start.ModelProvider != livecodex.Provider {
		failLiveAssertion(t, "thread model provider does not match the isolated fixture")
	}
	if details.Run.Start.Model == "" {
		failLiveAssertion(t, "thread-start model was not observed")
	}
	if details.Run.Run.Turn.Status != protocolv2.TurnStatusCompleted {
		failLiveAssertion(t, "turn did not complete")
	}
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
