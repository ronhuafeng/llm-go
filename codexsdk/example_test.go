package codexsdk_test

import (
	"context"
	"log"

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
