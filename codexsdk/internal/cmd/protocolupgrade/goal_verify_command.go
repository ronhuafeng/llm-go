package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolsync"
)

// runGoalVerify is shared by the native goal and CI gate selectors. Its only
// stdout payload is the diagnostics path; native exits remain authoritative.
func runGoalVerify(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("repo-root", ".", "candidate repository root")
	gate := flags.String("gate", "local", "local, source, generated, or provenance (Live is CI-only)")
	diagnostics := flags.String("diagnostic-dir", "", "controller-owned directory outside candidate")
	cache := flags.String("trusted-cache-root", "", "launcher-prewarmed cache outside candidate")
	base := flags.String("base-root", os.Getenv("LLMGO_GOAL_BASE_ROOT"), "accepted baseline checkout")
	candidate := flags.String("candidate-dir", os.Getenv("LLMGO_GOAL_CANDIDATE_DIR"), "trusted freshly rebuilt upstream facts")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "verify: unexpected positional arguments")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := protocolsync.VerifyGoal(ctx, protocolsync.GoalVerifyRequest{RepoRoot: *root, DiagnosticDir: *diagnostics, Gate: *gate, TrustedCacheRoot: *cache, BaseRoot: *base, CandidateDir: *candidate})
	defer result.Close()
	if result.DiagnosticPath != "" {
		fmt.Fprintln(stdout, result.DiagnosticPath)
	}
	if err != nil {
		fmt.Fprintln(stderr, "verify: failed; read diagnostic JSONL (incomplete evidence is not acceptance)")
		return 1
	}
	return 0
}

// runGoalOfflineProvenance is invoked by the fixed local gate inside its offline
// sandbox. It composes existing Sync validation; it has no publication authority.
func runGoalOfflineProvenance(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("verify-provenance-offline", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("repo-root", ".", "isolated candidate root")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		return 2
	}
	if err := protocolsync.VerifyOfflineProvenance(*root); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func runCIGate(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("verify-ci-gate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("repo-root", ".", "exact clean CI checkout")
	gate := flags.String("gate", "", "source, generated, provenance, or live")
	dir := flags.String("diagnostic-dir", "", "diagnostics outside checkout")
	base := flags.String("base-root", os.Getenv("LLMGO_GOAL_BASE_ROOT"), "exact accepted PR base checkout")
	candidate := flags.String("candidate-dir", os.Getenv("LLMGO_GOAL_CANDIDATE_DIR"), "trusted freshly rebuilt upstream facts")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := protocolsync.VerifyCIGate(ctx, protocolsync.GoalVerifyRequest{RepoRoot: *root, Gate: *gate, DiagnosticDir: *dir, BaseRoot: *base, CandidateDir: *candidate})
	if result.DiagnosticPath != "" {
		fmt.Fprintln(stdout, result.DiagnosticPath)
	}
	if err != nil {
		fmt.Fprintln(stderr, "CI gate failed; read its diagnostic JSONL")
		return 1
	}
	return 0
}
