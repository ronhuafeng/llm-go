package codexsdk

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2"
)

func TestRealAppServerSmokeStartResumeFork(t *testing.T) {
	if os.Getenv("CODEXSDK_REAL_APP_SERVER_SMOKE") != "1" {
		t.Skip("set CODEXSDK_REAL_APP_SERVER_SMOKE=1 to launch a real codex app-server")
	}
	model := strings.TrimSpace(os.Getenv("CODEXSDK_REAL_APP_SERVER_MODEL"))
	if model == "" {
		t.Fatal("CODEXSDK_REAL_APP_SERVER_MODEL is required when CODEXSDK_REAL_APP_SERVER_SMOKE=1")
	}
	command := realAppServerSmokeCommand()
	if len(command) == 0 {
		t.Fatal("real app-server command is empty")
	}
	if _, err := exec.LookPath(command[0]); err != nil {
		t.Fatalf("real app-server command %q is unavailable: %v", command[0], err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	root, err := New(ClientOptions{
		CWD:     t.TempDir(),
		Command: command,
	})
	if err != nil {
		t.Fatalf("start real app-server client: %v", err)
	}
	defer root.Close()

	// Resume needs a persisted rollout; fork stays ephemeral below.
	started, err := root.ThreadRunner().Start(ctx, StartThreadRunRequest{
		Thread: protocolv2.ThreadStartParams{
			Model:          protocolv2.Value(model),
			Ephemeral:      protocolv2.Value(false),
			ApprovalPolicy: protocolv2.Value(protocolv2.NewAskForApprovalNever()),
		},
		Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{
			protocolv2.NewUserInputText(protocolv2.UserInputText{Text: "Reply with a short confirmation for codexsdk start smoke."}),
		}},
	})
	if err != nil {
		t.Fatalf("real StartThread smoke failed: %v", err)
	}
	if started.Start.Thread.ID == "" {
		t.Fatalf("real StartThread smoke returned result without thread id: %#v", started)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := root.Threads().Archive(cleanupCtx, protocolv2.ThreadArchiveParams{ThreadID: started.Start.Thread.ID}); err != nil {
			t.Logf("archive real app-server smoke thread %s: %v", started.Start.Thread.ID, err)
		}
	}()
	if started.Run.Turn.ID == "" || started.Run.FinalResponse == "" {
		t.Fatalf("real StartThread smoke returned incomplete result: %#v", started)
	}

	resumed, err := root.ThreadRunner().Resume(ctx, ResumeThreadRunRequest{
		Thread: protocolv2.ThreadResumeParams{
			ThreadID:       started.Start.Thread.ID,
			ApprovalPolicy: protocolv2.Value(protocolv2.NewAskForApprovalNever()),
		},
		Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{
			protocolv2.NewUserInputText(protocolv2.UserInputText{Text: "Reply with a short confirmation for codexsdk resume smoke."}),
		}},
	})
	if err != nil {
		t.Fatalf("real ResumeThread smoke failed: %v", err)
	}
	if resumed.Resume.Thread.ID == "" || resumed.Run.Turn.ID == "" || resumed.Run.FinalResponse == "" {
		t.Fatalf("real ResumeThread smoke returned incomplete result: %#v", resumed)
	}

	ephemeral := true
	forked, err := root.Threads().Fork(ctx, protocolv2.ThreadForkParams{
		ThreadID:       started.Start.Thread.ID,
		Ephemeral:      &ephemeral,
		ApprovalPolicy: protocolv2.Value(protocolv2.NewAskForApprovalNever()),
	})
	if err != nil {
		t.Fatalf("real ForkThread smoke failed: %v", err)
	}
	if forked.Thread.ID == "" {
		t.Fatalf("real ForkThread smoke returned incomplete result: %#v", forked)
	}
}

func realAppServerSmokeCommand() []string {
	raw := strings.TrimSpace(os.Getenv("CODEXSDK_REAL_APP_SERVER_COMMAND"))
	if raw == "" {
		return []string{"codex", "app-server", "--listen", "stdio://"}
	}
	return strings.Fields(raw)
}
