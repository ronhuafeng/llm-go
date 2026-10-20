package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolsync"
	"io"
	"time"
)

func runGoalIntegrationGuard(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("goal-integration-guard", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("controller-root", ".", "trusted repository checkout")
	head := fs.String("expected-head", "", "exact accepted candidate")
	original := fs.String("original-base", "", "original accepted base")
	verified := fs.String("verification-base", "", "last actually verified accepted baseline")
	current := fs.String("current-base", "", "current accepted main")
	ref := fs.String("target-ref", "", "fixed upstream ref")
	kind := fs.String("target-kind", "", "fixed upstream kind")
	sha := fs.String("target-sha", "", "fixed upstream commit")
	deadlineText := fs.String("deadline", "", "inherited UTC deadline")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return 2
	}
	deadline, err := time.Parse(time.RFC3339, *deadlineText)
	if err != nil || !deadline.After(time.Now()) {
		fmt.Fprintln(stderr, "integration deadline expired or invalid")
		return 1
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	if err := protocolsync.ValidateGoalIntegrationHandoff(ctx, *root, *original, *verified, *current, *head, protocolsync.Target{RefName: *ref, RefKind: *kind, PeeledCommitSHA: *sha}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, "Exact fixed-target handoff remains valid")
	return 0
}
