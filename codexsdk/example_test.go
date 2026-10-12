package codexsdk_test

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk"
	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

// ExampleNew is compiled as consumer documentation but is not executed by the
// ordinary test suite because it requires a real local Codex app-server.
func ExampleNew() {
	client, err := codexsdk.New(codexsdk.ClientOptions{
		CWD:     ".",
		Command: []string{"codex", "app-server", "--listen", "stdio://"},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	_, _ = client.Models().List(context.Background(), protocolv2.ModelListParams{})
}

// ExampleNew_environment compiles the process-scoped replacement contract.
func ExampleNew_environment() {
	// This is a complete child environment. Add any other application-owned
	// variables explicitly. Credentials remain environment data, never argv.
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"CODEX_HOME=/path/to/isolated/codex-home",
		"AZURE_OPENAI_API_KEY=" + os.Getenv("AZURE_OPENAI_API_KEY"),
	}
	client, err := codexsdk.New(codexsdk.ClientOptions{
		CWD:     ".",
		Command: []string{"codex", "app-server", "--listen", "stdio://"},
		Env:     env,
	})
	if err != nil {
		log.Fatal("cannot start client")
	}
	defer client.Close()
}

// ExampleNewContext compiles the caller-owned startup deadline.
func ExampleNewContext() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := codexsdk.NewContext(ctx, codexsdk.ClientOptions{CWD: ".", Command: []string{"codex", "app-server", "--listen", "stdio://"}})
	if err != nil {
		log.Fatal("cannot initialize client")
	}
	defer client.Close()
}

func ExampleClient_CloseContext() {
	client, err := codexsdk.New(codexsdk.ClientOptions{CWD: ".", Command: []string{"codex", "app-server", "--listen", "stdio://"}})
	if err != nil {
		log.Fatal("cannot initialize client")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := client.CloseContext(ctx)
	if err != nil {
		log.Print("shutdown incomplete or failed")
	}
	if !result.ProcessReaped || result.CallbackWorkPending > 0 {
		// Cleanup continues; a Go callback ignoring cancellation remains unresolved.
	}
}
