package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolsync"
)

type forwardFlags struct {
	root, repository, base, head, original, ref, kind, sha, deadline, verificationBase string
	pr                                                                                 int
	publisher, integrator                                                              int64
	rebased                                                                            bool
}

func parseForwardFlags(args []string, stderr io.Writer) (forwardFlags, error) {
	var f forwardFlags
	fs := flag.NewFlagSet("auto-forward", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&f.root, "controller-root", "", "trusted main checkout")
	fs.StringVar(&f.repository, "repository", "", "fixed repository")
	fs.StringVar(&f.base, "base-branch", "main", "protected default branch")
	fs.IntVar(&f.pr, "pr-number", 0, "same repository PR")
	fs.StringVar(&f.head, "expected-head", "", "exact authorized head")
	fs.StringVar(&f.verificationBase, "verification-base", "", "last certified accepted M")
	fs.StringVar(&f.original, "original-base", "", "original accepted B")
	fs.StringVar(&f.ref, "target-ref", "", "fixed upstream ref")
	fs.StringVar(&f.kind, "target-kind", "", "fixed upstream kind")
	fs.StringVar(&f.sha, "target-sha", "", "fixed upstream SHA")
	fs.StringVar(&f.deadline, "deadline", "", "original absolute RFC3339 deadline")
	fs.Int64Var(&f.publisher, "publication-app-bot-id", 0, "trusted original publishing actor")
	fs.Int64Var(&f.integrator, "integration-app-bot-id", 0, "trusted integration actor")
	fs.BoolVar(&f.rebased, "rebased-head", false, "head from preceding trusted integration phase")

	if e := fs.Parse(args); e != nil {
		return f, e
	}
	if fs.NArg() != 0 {
		return f, errors.New("unexpected arguments")
	}
	return f, nil
}
func forwardContext(f forwardFlags) (context.Context, func(), error) {
	deadline, e := time.Parse(time.RFC3339, f.deadline)
	if e != nil || !deadline.After(time.Now()) || deadline.After(time.Now().Add(6*time.Hour)) {
		return nil, nil, errors.New("remaining original deadline must be positive and at most six hours")
	}
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithDeadline(signalCtx, deadline)
	return ctx, func() { cancel(); stop() }, nil
}
func (f forwardFlags) request(ctx context.Context) protocolsync.ForwardRequest {
	return protocolsync.ForwardRequest{RepoRoot: f.root, Repository: f.repository, BaseBranch: f.base, ExpectedHead: f.head, OriginalBase: f.original, VerificationBase: f.verificationBase, Number: f.pr, Target: protocolsync.Target{RefName: f.ref, RefKind: f.kind, PeeledCommitSHA: f.sha}, PublicationAppBotID: f.publisher, IntegrationAppBotID: f.integrator, RebasedHead: f.rebased, API: protocolsync.NewGoalAPI(ctx, f.root), APIForContext: func(call context.Context) protocolsync.PublicationAPI { return protocolsync.NewGoalAPI(call, f.root) }, Review: func(call context.Context) error {
		cmd := exec.CommandContext(call, "gh", "pr", "view", fmt.Sprint(f.pr), "--repo", f.repository, "--json", "reviewDecision")
		cmd.Env = forwardReadEnv()
		data, e := cmd.Output()
		if e != nil {
			return errors.New("review policy observation unavailable")
		}
		var value struct {
			ReviewDecision string `json:"reviewDecision"`
		}
		if json.Unmarshal(data, &value) != nil {
			return errors.New("invalid review decision")
		}
		switch value.ReviewDecision {
		case "", "APPROVED":
			return nil
		default:
			return errors.New("current review policy is not satisfied")
		}
	}}
}
func forwardReadEnv() []string {
	var env []string
	for _, name := range []string{"PATH", "HOME", "TMPDIR", "CI"} {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}

	token := os.Getenv("GH_READ_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	return append(env, "GH_TOKEN="+token)
}
func runAutoForward(args []string, stdout, stderr io.Writer) int {
	f, e := parseForwardFlags(args, stderr)
	if e != nil {
		return 2
	}
	ctx, cancel, e := forwardContext(f)
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 2
	}
	defer cancel()
	app, e := newRepositoryAppCredentials("AUTO_FORWARD", f.repository, stderr)
	if e != nil {
		fmt.Fprintln(stderr, "integration App configuration unavailable")
		return 1
	}
	req := f.request(ctx)
	req.Token = func(call context.Context, workflows bool) (string, func(), error) {
		permissions := map[string]string{"contents": "write"}
		if workflows {
			permissions["workflows"] = "write"
		}
		return app.tokenWithPermissions(call, permissions)
	}
	op, e := protocolsync.NewNativeForward(req)
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	verifier, e := newForwardVerifier(ctx, f, op, stderr)
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	req.VerifyCandidate = verifier.verify
	req.RepairCandidate = verifier.repair
	op, e = protocolsync.NewNativeForward(req)
	if e != nil {
		return 1
	}
	verifier.op = op
	head := f.head
	result, e := protocolsync.AutoForward(ctx, head, 15*time.Second, op)
	if json.NewEncoder(stdout).Encode(result) != nil {
		return 1
	}
	if output := os.Getenv("GITHUB_OUTPUT"); output != "" {
		file, err := os.OpenFile(output, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return 1
		}
		_, err = fmt.Fprintf(file, "status=%s\nhead=%s\nmain=%s\nrebased_head=%t\n", result.Status, result.Head, result.Main, op.RebasedHead(result.Head))
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			return 1
		}
	}
	// Conflicts are a bounded handoff within this same run, not failed CI made green.
	// A generic/manual proposal has no fixed target for automatic semantic repair.

	if e != nil || result.Status == "blocked" {
		fmt.Fprintln(stderr, result.Reason)
		return 1
	}
	return 0
}

func detachedForwardRoot(ctx context.Context, root, parent, sha string) (string, error) {
	dir := filepath.Join(parent, sha)
	if err := protocolsync.PrepareForwardCheckout(ctx, root, dir, sha); err != nil {
		return "", err
	}
	return dir, nil
}
