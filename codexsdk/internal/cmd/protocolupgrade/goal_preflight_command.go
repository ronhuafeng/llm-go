package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolsync"
	"io"
	"time"
)

func runGoalPreflight(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("goal-preflight", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("controller-root", "", "accepted checkout")
	repo := fs.String("repository", "", "fixed repository")
	baseBranch := fs.String("base-branch", "main", "accepted default branch")
	base := fs.String("base-sha", "", "accepted base SHA")
	actor := fs.Int64("app-bot-id", 0, "existing publishing App actor")
	upstream := fs.String("upstream-ref", "", "explicit upstream selection; otherwise latest stable")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := protocolsync.GoalPreflight(ctx, protocolsync.GoalPreflightRequest{RepoRoot: *root, Repository: *repo, BaseBranch: *baseBranch, BaseSHA: *base, AppBotID: *actor, UpstreamRef: *upstream, API: protocolsync.NewGoalAPI(ctx, *root)})
	if json.NewEncoder(stdout).Encode(result) != nil {
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, result.Reason)
		return 1
	}
	return 0
}
