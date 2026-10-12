package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolsync"
)

func runGoalPrepare(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("goal-prepare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bindingFile := fs.String("binding-file", "", "retained uncertain preparation locator; reconciliation only")
	root := fs.String("controller-root", "", "accepted checkout")
	repo := fs.String("repository", "", "fixed repository")
	baseBranch := fs.String("base-branch", "main", "accepted default branch")
	base := fs.String("base-sha", "", "accepted base SHA")
	actor := fs.Int64("app-bot-id", 0, "existing publishing App actor")
	ref := fs.String("target-ref", "", "exact upstream ref")
	kind := fs.String("target-kind", "", "exact upstream kind")
	sha := fs.String("target-sha", "", "exact upstream commit")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	request := protocolsync.GoalPublicationRequest{RepoRoot: *root, Repository: *repo, Remote: "origin", BaseBranch: *baseBranch, BaseSHA: *base, AppBotID: *actor, Target: protocolsync.Target{RefName: *ref, RefKind: *kind, PeeledCommitSHA: *sha}, API: protocolsync.NewGoalAPI(ctx, *root)}
	if *bindingFile != "" {
		var locator struct {
			Number             int
			Branch, Head, Base string
		}
		data, e := os.ReadFile(*bindingFile)
		if e != nil || len(data) > 8192 || json.Unmarshal(data, &locator) != nil || locator.Base != *base || locator.Branch == "" || locator.Head == "" {
			fmt.Fprintln(stderr, "invalid retained preparation locator")
			return 2
		}
		request.Number = locator.Number
		request.Branch = locator.Branch
		request.ExpectedHead = locator.Head
	}
	result, err := protocolsync.PrepareGoalDraft(ctx, request)
	// Retain only public effect locators, including an uncertain creation. Never
	// serialize API implementations or configuration containing credentials.
	e := json.NewEncoder(stdout).Encode(map[string]any{"number": result.Binding.Number, "branch": result.Binding.Branch, "head": result.Binding.ExpectedHead, "base": result.Binding.BaseSHA, "url": result.URL, "confirmed": err == nil})
	if e != nil {
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, "goal preparation incomplete; reconcile retained branch/head before retrying")
		return 1
	}
	return 0
}
