package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk"
	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
	codexcaller "github.com/ronhuafeng/llm-go/llmcaller/codex"
	"github.com/ronhuafeng/llm-go/llmkit/llmadapter"
)

// TestLiveCodexSmoke is deliberately outside the ordinary PR gate. It proves
// that the composed public path still works against a real Codex app-server,
// while semantic correctness remains owned by deterministic tests and fakes.
func TestLiveCodexSmoke(t *testing.T) {
	if os.Getenv("LLMGO_LIVE_CODEX") != "1" {
		t.Skip("set LLMGO_LIVE_CODEX=1 to run the real Codex smoke test")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Fatalf("codex CLI is required: %v", err)
	}
	provider, err := liveSmokeProvider()
	if err != nil {
		t.Fatal(err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", "..", ".."))

	client, err := codexsdk.New(codexsdk.ClientOptions{
		CWD:     root,
		Command: []string{"codex", "app-server", "--listen", "stdio://"},
	})
	if err != nil {
		t.Log("live_failure.stage=startup")
		t.Fatal(err)
	}
	defer client.Close()

	options := liveSmokeApplicationOptions(client.ThreadRunner(), provider)
	options.Defaults.Thread.CWD = protocolv2.Value(root)
	if model := os.Getenv("LLMGO_LIVE_CODEX_MODEL"); model != "" {
		options.Defaults.Thread.Model = protocolv2.Value(model)
	}
	caller, err := codexcaller.New(options)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	type result struct {
		Answer string `json:"answer"`
	}
	got, err := llmadapter.Value[result](ctx, caller, `Return JSON with answer set to "ok".`)
	if err != nil {
		reportLiveFailure(t, client, liveThreadStart(got.Response), err)
		t.Fatal(err)
	}
	if got.Value.Answer == "" {
		t.Fatal("typed result is empty")
	}
	if got.Response.Execution.BackendName != "codex" {
		t.Fatalf("backend = %q, want codex", got.Response.Execution.BackendName)
	}
	requireUnknownProvider(t, got.Response.Execution)
	requireUnknownModel(t, got.Response.Execution)
	details, ok := got.Response.BackendDetails.(codexcaller.Details)
	if !ok {
		t.Fatalf("backend details = %T, want codexcaller.Details", got.Response.BackendDetails)
	}
	if details.Run.Start.Thread.ID == "" || details.Run.Run.Turn.ID == "" {
		t.Fatalf("exact run is missing thread/turn identity: %#v", details.Run)
	}
	if details.Run.Start.ModelProvider != provider {
		t.Fatalf("thread model provider = %q, want %s", details.Run.Start.ModelProvider, provider)
	}
	if details.Run.Start.Model == "" {
		t.Fatal("thread-start model was not observed")
	}
	if details.Run.Run.Turn.Status != protocolv2.TurnStatusCompleted {
		t.Fatalf("turn status = %v, want completed", details.Run.Run.Turn.Status)
	}
}

func liveThreadStart(response llmadapter.Response) protocolv2.ThreadStartResponse {
	details, ok := response.BackendDetails.(codexcaller.Details)
	if !ok {
		return protocolv2.ThreadStartResponse{}
	}
	return details.Run.Start
}
