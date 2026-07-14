package codexsdk_test

import (
	"context"
	"os"

	"github.com/ronhuafeng/codexsdk-go/codexsdk"
	"github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2"
)

func ExampleThreadRunner_start() {
	ctx := context.Background()
	root, err := codexsdk.New(codexsdk.ClientOptions{
		CWD:     os.TempDir(),
		Command: []string{"codex", "app-server", "--listen", "stdio://"},
	})
	if err != nil {
		panic(err)
	}
	defer root.Close()

	result, err := root.ThreadRunner().Start(ctx, codexsdk.StartThreadRunRequest{
		Thread: protocolv2.ThreadStartParams{Model: protocolv2.Value("gpt-5")},
		Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{
			protocolv2.NewUserInputText(protocolv2.UserInputText{Text: "Reply briefly."}),
		}},
	})
	if err != nil {
		panic(err)
	}
	_ = result.Run.FinalResponse
	_ = result.Run.Notifications
}

func ExampleStream_Wait() {
	ctx := context.Background()
	root, err := codexsdk.New(codexsdk.ClientOptions{
		CWD:     os.TempDir(),
		Command: []string{"codex", "app-server", "--listen", "stdio://"},
	})
	if err != nil {
		panic(err)
	}
	defer root.Close()

	stream, err := root.ThreadRunner().StartStream(ctx, codexsdk.StartThreadRunRequest{
		Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{
			protocolv2.NewUserInputText(protocolv2.UserInputText{Text: "Reply briefly."}),
		}},
	})
	if err != nil {
		panic(err)
	}
	defer stream.Close()

	result, err := stream.Wait(ctx)
	if err != nil {
		// Result retains exact evidence accepted before failure.
		panic(err)
	}
	_ = result.Run.Notifications
}
