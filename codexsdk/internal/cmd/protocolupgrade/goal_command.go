package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/codexexec"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolsync"
)

// runNativeGoal is a trusted launch surface, not a model-callable command tool.
// The model only receives verify_upstream's fixed empty argument schema.
func runNativeGoal(args []string, stdout, stderr io.Writer) (code int) {
	progress := &protocolsync.GoalProgress{Output: stderr, Actions: os.Getenv("GITHUB_ACTIONS") == "true"}
	flags := flag.NewFlagSet("goal", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("controller-root", "", "trusted accepted checkout")
	upstream := flags.String("candidate-dir", "", "trusted freshly rebuilt upstream schema and common source")
	workspace := flags.String("workspace", "", "isolated prepared candidate checkout")
	state := flags.String("state-dir", "", "private native runtime state")
	diagnostics := flags.String("diagnostic-dir", "", "dedicated read-only-to-model JSONL directory")
	repository := flags.String("repository", "", "fixed repository")
	branch := flags.String("branch", "", "fixed existing Draft branch")
	baseBranch := flags.String("base-branch", "main", "accepted base branch")
	base := flags.String("base-sha", "", "accepted base SHA")
	head := flags.String("expected-head", "", "fixed Draft prior head")
	number := flags.Int("pr-number", 0, "fixed existing Draft PR")
	actor := flags.Int64("app-bot-id", 0, "existing publishing App actor")
	ref := flags.String("target-ref", "", "fixed upstream ref")
	kind := flags.String("target-kind", "", "fixed upstream kind")
	sha := flags.String("target-sha", "", "fixed upstream peeled SHA")
	runtimeDirs := flags.String("runtime-dirs", "", "comma-separated exact trusted runtime directories")
	deadlineText := flags.String("deadline", "", "UTC deadline fixed at workflow start")
	handoffFile := flags.String("handoff-file", "", "trusted success-only handoff outside candidate")
	duration := flags.Duration("timeout", 2*time.Hour, "finite native goal lifetime including verification")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return 2
	}
	if *duration <= 0 || *duration > 6*time.Hour || *root == "" || *workspace == "" || *state == "" || *diagnostics == "" || *upstream == "" {
		progress.Failure("Explicit trusted paths and finite lifetime required")
		return 2
	}
	result := protocolsync.GoalFlowResult{Stage: "Goal preparation"}
	fail := func(message string) int {
		result.Failure = message
		return 1
	}
	defer func() {
		if code != 0 && result.Failure == "" {
			result.Failure = "Goal incomplete; inspect retained JSONL diagnostics"
		}
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			code = 1
			progress.Failure("Could not retain the final goal result")
		} else if code != 0 {
			progress.Failure(result.Failure)
		}
	}()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	deadline := time.Now().Add(*duration)
	if *deadlineText != "" {
		fixed, err := time.Parse(time.RFC3339, *deadlineText)
		if err != nil || !fixed.After(time.Now()) || fixed.After(time.Now().Add(6*time.Hour)) {
			return fail("Invalid fixed workflow deadline")
		}
		if fixed.Before(deadline) {
			deadline = fixed
		}
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	target := protocolsync.Target{RefName: *ref, RefKind: *kind, PeeledCommitSHA: *sha}
	binding := protocolsync.GoalPublicationRequest{RepoRoot: *root, Repository: *repository, Remote: "origin", BaseBranch: *baseBranch, Branch: *branch, BaseSHA: *base, ExpectedHead: *head, Number: *number, AppBotID: *actor, Target: target, API: protocolsync.NewGoalAPI(ctx, *root)}
	app, err := newGoalAppCredentials(*repository, stderr)
	if err != nil {
		return fail("Publication App credentials unavailable")
	}
	binding.PublicationToken = app.token
	acceptedHead, err := exec.CommandContext(ctx, "git", "-C", *root, "rev-parse", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(acceptedHead)) != *base || protocolsync.AssertClean(*root) != nil {
		return fail("Accepted checkout does not match fixed base")
	}
	// Validate all trusted identity values before any native model invocation.
	if err := protocolsync.ValidateGoalBinding(ctx, binding); err != nil {
		return fail("Fixed Draft binding unavailable")
	}
	prompt, err := os.ReadFile(filepath.Join(*root, ".agents/skills/codexsdk-sync-upstream/goal.txt"))
	if err != nil {
		return fail("Accepted goal prompt unavailable")
	}
	objective := strings.NewReplacer("${BASE_SHA}", *base, "${TARGET_REF}", *ref, "${TARGET_SHA}", *sha, "${PR_URL}", fmt.Sprintf("https://github.com/%s/pull/%d", *repository, *number), "${BRANCH}", *branch).Replace(string(prompt))
	dirs, err := codexexec.NativeGoalRuntimePaths("codex")
	if err != nil {
		return fail("Exact native runtime paths unavailable")
	}
	dirs = append(dirs, splitRuntimeDirs(*runtimeDirs)...)
	moduleCache, err := exec.CommandContext(ctx, "go", "env", "GOMODCACHE").Output()
	if err != nil {
		return fail("Prepared module cache unavailable")
	}
	executor, err := codexexec.New(ctx, codexexec.Config{Workspace: *workspace, StateDir: *state, Version: os.Getenv("CODEX_CLI_VERSION"), BaseURL: os.Getenv("CODEX_RESPONSES_API_ENDPOINT"), APIKey: os.Getenv("AZURE_OPENAI_API_KEY"), Model: "gpt-6.1-sol", Reasoning: "high", GoalSandbox: &codexexec.GoalSandboxConfig{RuntimeDirs: dirs, ModuleCache: strings.TrimSpace(string(moduleCache)), ReadOnlyDirs: []string{*diagnostics}}})
	if err != nil {
		return fail("Native runtime preparation failed")
	}
	var runErr error
	result, runErr = protocolsync.RunUpstreamGoal(ctx, protocolsync.GoalFlowRequest{Objective: objective, Local: protocolsync.GoalVerifyRequest{RepoRoot: *workspace, DiagnosticDir: *diagnostics, TrustedCacheRoot: filepath.Join(*root, "codexsdk/.cache"), BaseRoot: *root, CandidateDir: *upstream}, Publication: binding, Progress: progress}, executor)
	closeErr := executor.Close()
	if closeErr != nil {
		result.Failure = "Native runtime cleanup failed"
	}
	if runErr != nil || closeErr != nil {
		return 1
	}
	if *handoffFile != "" {
		deadline, _ := ctx.Deadline()
		handoff, err := protocolsync.CompletedGoalHandoff(binding, result, deadline)
		if err != nil {
			return fail("Completed goal handoff validation failed")
		}
		data, err := json.Marshal(handoff)
		if err != nil {
			return fail("Cannot encode completed handoff")
		}
		file, err := os.OpenFile(*handoffFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return fail("Cannot create exclusive completed handoff")
		}
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			return fail("Completed handoff write failed")
		}
	}
	return 0
}
func splitRuntimeDirs(s string) []string {
	var dirs []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			dirs = append(dirs, p)
		}
	}
	return dirs
}
