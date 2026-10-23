package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolsync"
)

// Recovery only admits and materializes authenticated source inputs. The caller
// must separately start a fresh native goal, preserving B/U and the returned
// execution deadline. No old private thread or previous CI result is resumed.
func runGoalRecover(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("goal-recover", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("controller-root", "", "clean accepted current main checkout")
	output := fs.String("output-dir", "", "new isolated local recovery directory")
	repo := fs.String("repository", "", "fixed repository")
	current := fs.String("current-base-sha", "", "accepted current main SHA")
	actor := fs.Int64("app-bot-id", 0, "existing publication App actor")
	run := fs.Int64("prior-run-id", 0, "explicit prior upstream workflow run")
	number := fs.Int("pr-number", 0, "existing Draft PR to reuse")
	head := fs.String("expected-head", "", "explicit expected current PR head")
	authorize := fs.Bool("authorize-new-bounded-execution", false, "explicitly authorize a new bounded execution, not renewal of the prior attempt")
	deadline := fs.String("new-execution-deadline", "", "explicit UTC deadline for an authorized new execution, at most six hours")
	regenerate := fs.Bool("regenerate-empty-seed", false, "explicitly authorize regeneration when no candidate was retained and the PR is still its exact empty seed")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return 2
	}
	var fixed time.Time
	if *deadline != "" {
		var err error
		fixed, err = time.Parse(time.RFC3339, *deadline)
		if err != nil {
			fmt.Fprintln(stderr, "invalid explicit new execution deadline")
			return 2
		}
	}
	if *authorize != (*deadline != "") {
		fmt.Fprintln(stderr, "new execution authorization and its explicit deadline must be supplied together")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	result, err := protocolsync.RecoverGoalDraft(ctx, protocolsync.GoalRecoveryRequest{RepoRoot: *root, OutputDir: *output, Repository: *repo, CurrentBaseSHA: *current, ExpectedHead: *head, Number: *number, AppBotID: *actor, PriorRunID: *run, AuthorizeNewBoundedExecution: *authorize, NewExecutionDeadline: fixed, RegenerateEmptySeed: *regenerate, API: protocolsync.NewGoalAPI(ctx, *root)})
	if json.NewEncoder(stdout).Encode(result) != nil {
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, result.Reason)
		return 1
	}
	return 0
}
