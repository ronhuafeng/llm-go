package protocolsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/checkacceptance"
)

// GoalPublicationRequest is an immutable controller binding. CandidateDir is the
// retained, clean snapshot that passed local verification; SnapshotDigest is its
// whole-tree goalSnapshot digest, not a protocol schema artifact digest. API and
// the Git subprocess environment are supplied by the credential-isolated host.
type GoalPublicationRequest struct {
	RepoRoot, Repository, Remote, BaseBranch, Branch, BaseSHA, ExpectedHead string
	CandidateDir, SnapshotDigest                                            string
	// StateDir is private host-owned durable evidence, outside the candidate.
	StateDir string
	// Deadline is the original goal deadline, never reset during recovery.
	Deadline time.Time
	Number   int
	// VerificationBaseSHA is M for one accepted-contract attempt; BaseSHA remains B.
	VerificationBaseSHA string
	AppBotID            int64
	Target              Target
	API                 PublicationAPI
	Lookuper            RemoteLookuper
	// PublicationToken obtains a fresh host-only token at the write boundary.
	// The returned cleanup revokes it after that one Git operation.
	PublicationToken func(context.Context) (string, func(), error)
	Progress         *GoalProgress
}

// GoalPublication.Head is our Git-confirmed effect locator. It may accompany an
// error when later PR/base checks fail; a nonempty Head alone is not acceptance.
type GoalPublication struct {
	Number         int                    `json:"number"`
	Head           string                 `json:"head"`
	PreviousHead   string                 `json:"previous_head"`
	SnapshotDigest string                 `json:"snapshot_sha256"`
	ChangedPaths   []string               `json:"changed_paths"`
	Intent         *GoalPublicationIntent `json:"intent,omitempty"`
	EffectState    GoalPublicationEffect  `json:"effect_state,omitempty"`
}

type goalDraftPR struct {
	publicationPR
	Draft bool `json:"draft"`
}
type goalPush func(context.Context, GoalPublicationRequest, string) error

var goalRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// AppendGoalDraft appends exactly one commit, never rewrites a ref or changes PR
// readiness. A failed push is read back once, never replayed. No candidate code
// or hooks execute here. Main may still race after the final precondition read;
// the postcondition detects that case and leaves publication unconfirmed.
func AppendGoalDraft(ctx context.Context, req GoalPublicationRequest) (GoalPublication, error) {
	return appendGoalDraft(ctx, req, nil)
}
func appendGoalDraft(ctx context.Context, req GoalPublicationRequest, push goalPush) (GoalPublication, error) {
	var result GoalPublication
	if !req.Deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, req.Deadline)
		defer cancel()
	}
	if err := goalBinding(req); err != nil {
		return result, err
	}
	// A retained intent has consumed this write attempt even when its readback
	// failed. Only an observed own intended head can advance the binding.
	prior, err := ReconcileGoalPublication(ctx, req)
	if err != nil {
		return prior, err
	}
	if prior.Intent != nil && req.ExpectedHead != prior.Head {
		return prior, nil
	}
	if err := goalCurrent(ctx, req, req.ExpectedHead); err != nil {
		return result, err
	}
	if err := AssertClean(req.CandidateDir); err != nil {
		return result, errors.New("verified snapshot is not clean")
	}
	work, err := os.MkdirTemp("", "goal-publication-")
	if err != nil {
		return result, errors.New("cannot freeze publication snapshot")
	}
	defer os.RemoveAll(work)
	snapshot := filepath.Join(work, "candidate")
	digest, err := goalSnapshot(ctx, req.CandidateDir, snapshot)
	if err != nil || digest != req.SnapshotDigest {
		return result, errors.New("verified snapshot digest changed")
	}
	// Import only the fixed snapshot object. Its commit metadata never becomes
	// publication history; the trusted controller creates the single parent.
	if _, err = goalGit(ctx, req.RepoRoot, "fetch", "--no-tags", "--no-write-fetch-head", snapshot, "HEAD"); err != nil {
		return result, err
	}
	tree, err := goalGit(ctx, snapshot, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return result, err
	}
	if _, err = goalGit(ctx, req.RepoRoot, "merge-base", "--is-ancestor", req.BaseSHA, req.ExpectedHead); err != nil {
		return result, errors.New("bound prior head does not descend from base")
	}
	paths, err := gitNUL(req.RepoRoot, "diff", "--name-only", "--no-renames", "-z", req.BaseSHA, tree, "--")
	if err != nil {
		return result, errors.New("cannot inspect candidate changes")
	}
	// File layout is review evidence, not publication authority. The trusted
	// target, snapshot, Draft binding and exact-head checks remain authoritative.
	identity, err := gitBaselineIdentity(req.RepoRoot, tree)
	if err != nil || identity.SourceCommit != req.Target.PeeledCommitSHA || identity.SourceRefName != req.Target.RefName || identity.SourceRefKind != req.Target.RefKind {
		return result, errors.New("candidate source identity differs from bound target")
	}
	oldTree, err := goalGit(ctx, req.RepoRoot, "rev-parse", req.ExpectedHead+"^{tree}")
	if err != nil {
		return result, err
	}
	if oldTree == tree {
		if err := goalCurrent(ctx, req, req.ExpectedHead); err != nil {
			return result, err
		}
		return GoalPublication{Number: req.Number, Head: req.ExpectedHead, PreviousHead: req.ExpectedHead, SnapshotDigest: req.SnapshotDigest, ChangedPaths: paths, Intent: prior.Intent, EffectState: GoalPublicationObserved}, nil
	}
	digest, err = goalSnapshot(ctx, req.CandidateDir, "")
	if err != nil || digest != req.SnapshotDigest {
		return result, errors.New("snapshot changed before publication")
	}
	head, err := goalGit(ctx, req.RepoRoot, "-c", "user.name=Protocol goal controller", "-c", "user.email=protocol-goal@localhost", "commit-tree", tree, "-p", req.ExpectedHead, "-m", "Append verified protocol goal candidate")
	if err != nil {
		return result, err
	}
	if err = goalCurrent(ctx, req, req.ExpectedHead); err != nil {
		return result, err
	}
	if push == nil {
		// Preparation cannot update Git refs. Do not consume an intent merely
		// because a fresh token is unavailable. Credentials stay in this private
		// closure, never in the retained bundle or binding.
		execute, cleanup, prepareErr := prepareGoalDraftPush(ctx, req, head)
		if prepareErr != nil {
			return result, prepareErr
		}
		defer cleanup()
		push = func(context.Context, GoalPublicationRequest, string) error { return execute() }
	}
	intent, err := writeGoalPublicationIntent(ctx, req, snapshot, head, tree)
	if err != nil {
		return result, err
	}
	result = goalIntentResult(intent)
	result.ChangedPaths = paths
	pushErr := safeGoalPublicationError("push", push(ctx, req, head))
	// Even if cancellation interrupted push, one bounded readback establishes
	// whether the effect happened. It cannot initiate another write.
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	actual, readErr := goalRemoteHead(readCtx, req, req.Branch)
	if readErr != nil {
		return result, goalPublicationOutcomeError("readback", "unknown", errors.Join(pushErr, readErr))
	}
	if actual != head {
		if pushErr != nil {
			return result, goalPublicationOutcomeError("readback", "unconfirmed", pushErr)
		}
		return result, goalPublicationOutcomeError("readback", "head_changed", nil)
	}
	// Exact Git readback confirms our own effect even if the PR projection lags.
	// Preserve its locator on postcondition failure; this is not acceptance and
	// never adopts an arbitrary remote head.
	if err = goalCurrentAuthority(readCtx, req, head, false); err != nil {
		return result, err
	}
	result.Head = head
	result.EffectState = GoalPublicationObserved
	if err = goalCurrent(readCtx, req, head); err != nil {
		return result, err
	}
	return result, nil
}

// GoalPublicationError carries only closed diagnostics. Raw Git output,
// credential command errors and token transport responses never escape here.
type GoalPublicationError struct {
	Stage    string
	Category string
	ExitCode int
	Token    *AppTokenError
	Cause    *GoalPublicationError
}

func (e *GoalPublicationError) Error() string {
	stage, category := "unknown", "operation_failed"
	switch e.Stage {
	case "token", "push", "git", "readback":
		stage = e.Stage
	}
	switch e.Category {
	case "token_unavailable", "git_failed", "unknown", "unconfirmed", "head_changed":
		category = e.Category
	}
	message := fmt.Sprintf("publication %s: %s", stage, category)
	if e.ExitCode > 0 {
		message += fmt.Sprintf(" (exit %d)", e.ExitCode)
	}
	if e.Token != nil {
		message += ": " + e.Token.Error()
	}
	if e.Cause != nil {
		message += "; " + e.Cause.Error()
	}
	return message
}
func (e *GoalPublicationError) Unwrap() error {
	if e.Token != nil {
		return e.Token
	}
	if e.Cause != nil {
		return e.Cause
	}
	return nil
}
func safeGoalPublicationError(stage string, err error) error {
	if err == nil {
		return nil
	}
	var publication *GoalPublicationError
	if errors.As(err, &publication) {
		return publication
	}
	failure := &GoalPublicationError{Stage: stage, Category: "git_failed"}
	if stage == "token" {
		failure.Category = "token_unavailable"
	}
	var token *AppTokenError
	if errors.As(err, &token) {
		failure.Stage, failure.Category = "token", "token_unavailable"
		copy := *token
		failure.Token = &copy
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		failure.ExitCode = exit.ExitCode()
	}
	return failure
}
func goalPublicationOutcomeError(stage, category string, cause error) error {
	failure := &GoalPublicationError{Stage: stage, Category: category}
	if cause != nil {
		errors.As(safeGoalPublicationError("push", cause), &failure.Cause)
	}
	return failure
}

func pushGoalDraft(ctx context.Context, r GoalPublicationRequest, head string) error {
	execute, cleanup, err := prepareGoalDraftPush(ctx, r, head)
	if err != nil {
		return err
	}
	defer cleanup()
	return execute()
}

// prepareGoalDraftPush performs no Git ref update. Successful preparation owns
// exactly one private token/hook lifetime; callers retain intent before execute.
func prepareGoalDraftPush(ctx context.Context, r GoalPublicationRequest, head string) (execute func() error, cleanup func(), err error) {
	hooks, removeHook, err := prepareExactRefHook(head, r.ExpectedHead, r.Branch)
	if err != nil {
		return nil, nil, safeGoalPublicationError("push", err)
	}
	cleanup = removeHook
	args := []string{"-c", "core.hooksPath=" + hooks, "-c", "core.fsmonitor=false"}
	var token string
	if r.PublicationToken != nil {
		var release func()
		token, release, err = r.PublicationToken(ctx)
		cleanup = func() {
			if release != nil {
				release()
			}
			removeHook()
		}
		if err != nil {
			cleanup()
			return nil, nil, safeGoalPublicationError("token", err)
		}
		if token == "" {
			cleanup()
			return nil, nil, safeGoalPublicationError("token", &AppTokenError{Failure: AppTokenMissing})
		}
		args = append(args, "-c", "credential.helper=", "-c", "credential.helper=!gh auth git-credential")
	}
	args = append(args, "-C", r.RepoRoot, "push", "--porcelain", goalRemote(r), head+":refs/heads/"+r.Branch)
	execute = func() error {
		cmd := exec.CommandContext(ctx, "git", args...)
		for _, entry := range os.Environ() {
			if token == "" || !strings.HasPrefix(entry, "GH_TOKEN=") {
				cmd.Env = append(cmd.Env, entry)
			}
		}
		if token != "" {
			cmd.Env = append(cmd.Env, "GH_TOKEN="+token)
		}
		return safeGoalPublicationError("push", cmd.Run())
	}
	return execute, cleanup, nil
}

func goalBinding(r GoalPublicationRequest) error {
	if r.API == nil || r.RepoRoot == "" || r.Number <= 0 || r.AppBotID <= 0 || !goalRepositoryPattern.MatchString(r.Repository) || !shaRE.MatchString(r.BaseSHA) || !shaRE.MatchString(r.ExpectedHead) || !shaRE.MatchString(r.Target.PeeledCommitSHA) || (r.VerificationBaseSHA != "" && !shaRE.MatchString(r.VerificationBaseSHA)) {
		return errors.New("incomplete trusted goal publication binding")
	}
	for _, ref := range []string{r.Branch, r.BaseBranch} {
		if ref == "" || strings.HasPrefix(ref, "-") || strings.ContainsAny(ref, "\r\n") {
			return errors.New("invalid bound branch")
		}
		if exec.Command("git", "check-ref-format", "refs/heads/"+ref).Run() != nil {
			return errors.New("invalid bound branch")
		}
	}
	if !strings.HasPrefix(r.Branch, "codex/sync-upstream-") {
		return errors.New("goal branch is outside the fixed sync namespace")
	}
	if r.Branch == r.BaseBranch {
		return errors.New("goal branch must differ from base")
	}
	if strings.HasPrefix(goalRemote(r), "-") {
		return errors.New("invalid bound remote")
	}
	return nil
}
func goalRemote(r GoalPublicationRequest) string {
	if r.Remote == "" {
		return "origin"
	}
	return r.Remote
}
func goalGit(ctx context.Context, root string, args ...string) (string, error) {
	fixed := []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-C", root}
	out, err := exec.CommandContext(ctx, "git", append(fixed, args...)...).Output()
	if err != nil {
		return "", safeGoalPublicationError("git", err)
	}
	return strings.TrimSpace(string(out)), nil
}
func goalRemoteHead(ctx context.Context, r GoalPublicationRequest, branch string) (string, error) {
	out, err := goalGit(ctx, r.RepoRoot, "ls-remote", goalRemote(r), "refs/heads/"+branch)
	if err != nil {
		return "", fmt.Errorf("bound remote ref cannot be confirmed: %w", err)
	}
	fields := strings.Fields(out)
	if len(fields) != 2 || fields[1] != "refs/heads/"+branch || !shaRE.MatchString(fields[0]) {
		return "", errors.New("bound remote ref cannot be confirmed")
	}
	return fields[0], nil
}
func goalCurrent(ctx context.Context, r GoalPublicationRequest, head string) error {
	return goalCurrentAuthority(ctx, r, head, true)
}

func goalCurrentAuthority(ctx context.Context, r GoalPublicationRequest, head string, requireMain bool) (resultErr error) {
	defer func() {
		if err := ctx.Err(); err != nil {
			resultErr = err
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	var pr goalDraftPR
	if err := r.API.Request("GET", fmt.Sprintf("repos/%s/pulls/%d", r.Repository, r.Number), nil, &pr); err != nil {
		return errors.New("cannot read bound draft PR")
	}
	// PR head/base SHA projections can lag. Their fixed repository/ref pairs
	// identify the branches; exact live Git refs own H and the current main M.
	if pr.Number != r.Number || pr.State != "open" || !pr.Draft || pr.MergedAt != nil || pr.Head.Ref != r.Branch || pr.Head.Repo.FullName != r.Repository || pr.Base.Ref != r.BaseBranch || pr.Base.Repo.FullName != r.Repository {
		return errors.New("draft PR identity changed")
	}
	if err := verifyAppActor(pr.User, r.AppBotID); err != nil {
		return errors.New("draft PR creator differs from bound actor")
	}
	actor, err := publicationHeadActor(PendingRequest{Repository: r.Repository}, r.API, r.Branch, head)
	if err != nil || verifyAppActor(actor, r.AppBotID) != nil {
		return errors.New("draft PR head actor cannot be confirmed")
	}
	if requireMain {
		base, err := goalRemoteHead(ctx, r, r.BaseBranch)
		if err != nil {
			return fmt.Errorf("main cannot be confirmed: %w", err)
		}
		if base != goalVerificationBase(r) {
			return errors.New("main moved from verified accepted base")
		}
	}
	actual, err := goalRemoteHead(ctx, r, r.Branch)
	if err != nil {
		return fmt.Errorf("draft branch cannot be confirmed: %w", err)
	}
	if actual != head {
		return errors.New("draft branch moved from bound head")
	}
	target, err := ResolveUpstream(ResolveRequest{UpstreamRef: r.Target.RefName, Lookuper: goalLookupContext(ctx, r.Lookuper)})
	if err != nil || target.RefName != r.Target.RefName || target.RefKind != r.Target.RefKind || target.PeeledCommitSHA != r.Target.PeeledCommitSHA {
		return errors.New("upstream target changed")
	}
	return ctx.Err()
}

// ObserveGoalCI only observes existing CI; it never dispatches or retries Live.
// A deadline is mandatory. API implementations must bound their own I/O; this
// loop bounds polls and does not leave detached network goroutines on timeout.
func ObserveGoalCI(ctx context.Context, req GoalPublicationRequest, head string, pollInterval time.Duration) (checkacceptance.Observation, error) {
	var observation checkacceptance.Observation
	if _, ok := ctx.Deadline(); !ok {
		return observation, errors.New("CI observation requires a deadline")
	}
	if err := goalBinding(req); err != nil {
		return observation, err
	}
	if !shaRE.MatchString(head) {
		return observation, errors.New("exact CI head required")
	}
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	lastProgress := ""
	for {
		if err := goalCurrent(ctx, req, head); err != nil {
			return observation, err
		}
		next, err := checkacceptance.Observe(req.API, req.Repository, head)
		if err != nil {
			return observation, errors.New("exact-head CI observation unavailable")
		}
		observation = next
		if err := goalCurrent(ctx, req, head); err != nil {
			return observation, err
		}
		if message := goalCIProgress(req.Repository, observation); message != lastProgress {
			req.Progress.line(message)
			lastProgress = message
		}
		switch observation.State {
		case checkacceptance.Succeeded, checkacceptance.Failed, checkacceptance.Cancelled:
			return observation, nil
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return observation, ctx.Err()
		case <-timer.C:
		}
	}
}

func goalVerificationBase(r GoalPublicationRequest) string {
	if r.VerificationBaseSHA != "" {
		return r.VerificationBaseSHA
	}
	return r.BaseSHA
}
