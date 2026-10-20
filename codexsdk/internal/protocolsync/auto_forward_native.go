package protocolsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/checkacceptance"
)

// ForwardRequest is supplied by a trusted workflow, never by candidate code.
// Tokens live only in the parent and are minted/revoked for each individual Git
// write. A workflows grant is requested only when already installed; denial is
// an explicit configuration blocker, never permission expansion.
type ForwardRequest struct {
	RepoRoot, Repository, BaseBranch, ExpectedHead, OriginalBase, VerificationBase string
	Number                                                                         int
	RebasedHead                                                                    bool // only the trusted previous integration phase sets this
	Target                                                                         Target
	PublicationAppBotID, IntegrationAppBotID                                       int64
	API                                                                            PublicationAPI
	APIForContext                                                                  func(context.Context) PublicationAPI
	Token                                                                          func(context.Context, bool) (string, func(), error)
	Review                                                                         func(context.Context) error
	VerifyCandidate                                                                func(context.Context, string, string) error
	RepairCandidate                                                                func(context.Context, string, string, string) (GoalVerifyResult, error)
	PollInterval                                                                   time.Duration
}
type NativeForward struct {
	semanticFailures map[string]error
	proofs           map[string]bool
	req              ForwardRequest
	branch           string
	lastMain         string
	writers          map[string]int64
}

func NewNativeForward(r ForwardRequest) (*NativeForward, error) {
	if r.API == nil || r.Review == nil || r.Token == nil || !goalRepositoryPattern.MatchString(r.Repository) || r.Number <= 0 || r.BaseBranch != "main" || !shaRE.MatchString(r.ExpectedHead) || r.RepoRoot == "" {
		return nil, errors.New("incomplete trusted integration binding")
	}
	if r.Target.PeeledCommitSHA != "" || r.Target.RefName != "" || r.Target.RefKind != "" {
		if !shaRE.MatchString(r.VerificationBase) || !shaRE.MatchString(r.OriginalBase) || !shaRE.MatchString(r.Target.PeeledCommitSHA) || r.Target.RefName == "" || r.Target.RefKind == "" || r.PublicationAppBotID <= 0 || r.IntegrationAppBotID <= 0 {
			return nil, errors.New("incomplete fixed target or App actor binding")
		}
	} else if (r.OriginalBase != "" || r.VerificationBase != "") && (!shaRE.MatchString(r.OriginalBase) || !shaRE.MatchString(r.VerificationBase)) {
		return nil, errors.New("incomplete immutable baseline binding")
	}
	writer := r.PublicationAppBotID
	if r.RebasedHead {
		writer = r.IntegrationAppBotID
	}
	return &NativeForward{req: r, lastMain: r.VerificationBase, proofs: map[string]bool{r.VerificationBase + ":" + r.ExpectedHead: true}, writers: map[string]int64{r.ExpectedHead: writer}}, nil
}
func (n *NativeForward) Read(ctx context.Context, expected string) (ForwardState, error) {
	var out ForwardState
	if e := ctx.Err(); e != nil {
		return out, e
	}
	api := n.req.API
	if n.req.APIForContext != nil {
		api = n.req.APIForContext(ctx)
	}
	var pr goalDraftPR
	if e := api.Request("GET", fmt.Sprintf("repos/%s/pulls/%d", n.req.Repository, n.req.Number), nil, &pr); e != nil {
		return out, errors.New("cannot read integration PR")
	}
	if pr.Number != n.req.Number || pr.Base.Ref != n.req.BaseBranch || pr.Head.Repo.FullName != n.req.Repository || pr.Base.Repo.FullName != n.req.Repository || pr.Head.Ref == n.req.BaseBranch || pr.Head.Ref == "" {
		return out, errors.New("integration PR identity changed")
	}
	if _, e := goalGit(ctx, n.req.RepoRoot, "check-ref-format", "refs/heads/"+pr.Head.Ref); e != nil {
		return out, e
	}
	if n.branch != "" && n.branch != pr.Head.Ref {
		return out, errors.New("PR branch changed")
	}
	n.branch = pr.Head.Ref
	if n.req.Target.PeeledCommitSHA != "" {
		prefix := syncBranchName(n.req.Target.RefName, n.req.Target.PeeledCommitSHA, n.req.OriginalBase) + "-goal-"
		if !strings.HasPrefix(n.branch, prefix) || len(strings.TrimPrefix(n.branch, prefix)) != 16 || verifyAppActor(pr.User, n.req.PublicationAppBotID) != nil {
			return out, errors.New("native goal allocation or creator differs from handoff")
		}
	}
	main, e := n.remote(ctx, n.req.BaseBranch)
	if e != nil || !shaRE.MatchString(main) {
		return out, errors.New("current main cannot be confirmed")
	}
	head, e := n.remote(ctx, n.branch)
	if e != nil {
		return out, e
	}
	out = ForwardState{Head: head, Main: main, Branch: n.branch, Ready: pr.State == "open" && !pr.Draft && pr.MergedAt == nil, Merged: pr.MergedAt != nil}
	refs := []string{"fetch", "--no-tags", "origin", main}
	if head != "" {
		refs = append(refs, head)
	}
	if _, e = goalGit(ctx, n.req.RepoRoot, refs...); e != nil {
		return out, e
	}
	if n.lastMain != "" && n.lastMain != main {
		if _, err := goalGit(ctx, n.req.RepoRoot, "fetch", "--no-tags", "origin", n.lastMain); err != nil {
			return out, err
		}
		monotonic, err := n.Ancestor(ctx, n.lastMain, main)
		if err != nil || !monotonic {
			return out, errors.New("main reset or diverged from previously observed accepted baseline")
		}
	}
	n.lastMain = main
	if head == expected && n.req.Target.PeeledCommitSHA != "" {
		actor, e := publicationHeadActor(PendingRequest{Repository: n.req.Repository}, api, n.branch, expected)
		if e != nil || verifyAppActor(actor, n.writers[expected]) != nil {
			return out, errors.New("current native branch writer is not the bound publication/integration App")
		}
	}
	return out, nil
}
func (n *NativeForward) remote(ctx context.Context, branch string) (string, error) {
	out, e := goalGit(ctx, n.req.RepoRoot, "ls-remote", "origin", "refs/heads/"+branch)
	if e != nil {
		return "", e
	}
	if out == "" {
		return "", nil
	}
	fields := strings.Fields(out)
	if len(fields) != 2 || fields[1] != "refs/heads/"+branch || !shaRE.MatchString(fields[0]) {
		return "", errors.New("ambiguous native ref")
	}
	return fields[0], nil
}
func (n *NativeForward) Ancestor(ctx context.Context, a, b string) (bool, error) {
	if !shaRE.MatchString(a) || !shaRE.MatchString(b) {
		return false, errors.New("invalid ancestry commit")
	}
	cmd := exec.CommandContext(ctx, "git", "-C", n.req.RepoRoot, "merge-base", "--is-ancestor", a, b)
	e := cmd.Run()
	if e == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(e, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, errors.New("ancestry observation unavailable")
}
func (n *NativeForward) Policy(ctx context.Context, main, head string) (string, error) {
	merges, err := goalGit(ctx, n.req.RepoRoot, "rev-list", "--merges", main+".."+head)
	if err != nil || merges != "" {
		return "", errors.New("candidate contains merge commits; linear integration required")
	}
	if n.req.Target.PeeledCommitSHA == "" {
		return "run", nil
	}
	if _, e := goalGit(ctx, n.req.RepoRoot, "fetch", "--no-tags", "origin", n.req.OriginalBase); e != nil {
		return "", e
	}
	desc, e := n.Ancestor(ctx, n.req.OriginalBase, main)
	if e != nil || !desc {
		return "", errors.New("current main does not retain original accepted base")
	}
	target, e := ResolveUpstream(ResolveRequest{UpstreamRef: n.req.Target.RefName, Lookuper: goalLookupContext(ctx, nil)})
	if e != nil || target.RefName != n.req.Target.RefName || target.RefKind != n.req.Target.RefKind || target.PeeledCommitSHA != n.req.Target.PeeledCommitSHA {
		return "", errors.New("fixed upstream target moved or is unavailable")
	}
	identity, e := gitBaselineIdentity(n.req.RepoRoot, head)
	fixed := BaselineIdentity{SourceRefName: n.req.Target.RefName, SourceRefKind: n.req.Target.RefKind, SourceCommit: n.req.Target.PeeledCommitSHA}
	if e != nil || identity != fixed {
		return "", errors.New("candidate does not retain exact target identity")
	}
	decision, e := ResolveGoalRebinding(n.req.RepoRoot, n.req.OriginalBase, main, fixed)
	return decision.Decision, e
}

// Rebase prepares an independent clone with disabled hooks/configured helpers.
// Mechanical Git never uses ours/theirs, rebase exec, autosquash or merge commits.
// Only conflicts or completed semantic failures can invoke the trusted isolated
// repair callback, and only a fixed-U proposal has authority for that callback.
func (n *NativeForward) Rebase(ctx context.Context, main, head string) (string, error) {
	dir, e := os.MkdirTemp("", "auto-forward-rebase-")
	if e != nil {
		return "", e
	}
	defer os.RemoveAll(dir)
	candidate := filepath.Join(dir, "candidate")
	if n.req.Target.PeeledCommitSHA == "" && (!shaRE.MatchString(n.req.OriginalBase) || !shaRE.MatchString(n.req.VerificationBase)) {
		return "", errors.New("mechanical reconciliation requires immutable baseline bindings")
	}
	if e = prepareForwardRebase(ctx, n.req.RepoRoot, candidate, main, head); e == nil {
		next, err := forwardLocalGit(ctx, candidate, "rev-parse", "HEAD")
		if err != nil {
			return "", err
		}
		if _, err = forwardLocalGit(ctx, n.req.RepoRoot, "fetch", "--no-tags", "--no-write-fetch-head", candidate, next); err != nil {
			return "", err
		}
		// A clean text rebase can still violate the immutable local contract.
		// Only a completed assertion failure may enter isolated semantic repair;
		// control/target violations, infrastructure errors and cancellation stop.
		if e = n.Verify(ctx, main, next); e == nil {
			return next, nil
		}
		if !errors.Is(e, ErrForwardSemanticProof) {
			return "", e
		}
	} else if !errors.Is(e, ErrForwardConflict) {
		return "", e
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if n.req.Target.PeeledCommitSHA == "" || n.req.RepairCandidate == nil {
		// Ordinary proposals have no immutable U for a model repair objective.
		// A conflict or completed semantic failure therefore remains explicit.
		return "", e
	}
	verified, err := n.req.RepairCandidate(ctx, candidate, main, head)
	defer verified.Close()
	if err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if status, err := forwardLocalGit(ctx, verified.SnapshotRoot, "status", "--porcelain", "--untracked-files=all", "--ignore-submodules=all"); err != nil || status != "" {
		return "", errors.New("repair snapshot changed")
	}
	digest, err := goalSnapshot(ctx, verified.SnapshotRoot, "")
	if err != nil || digest != verified.SnapshotDigest {
		return "", errors.New("repair snapshot digest changed")
	}
	if _, err = forwardLocalGit(ctx, n.req.RepoRoot, "fetch", "--no-tags", "--no-write-fetch-head", verified.SnapshotRoot, "HEAD"); err != nil {
		return "", err
	}
	tree, err := forwardLocalGit(ctx, verified.SnapshotRoot, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return "", err
	}
	next, err := forwardLocalGit(ctx, n.req.RepoRoot, "-c", "user.name=Auto-forward controller", "-c", "user.email=auto-forward@localhost", "commit-tree", tree, "-p", main, "-m", "Reconstruct locally verified integration repair")
	if err != nil {
		return "", err
	}
	if err = validateForwardControls(ctx, n.req.RepoRoot, main, next); err != nil {
		return "", err
	}
	n.proofs[main+":"+next] = true
	return next, nil
}

// PrepareForwardCheckout materializes one exact already-fetched commit without
// copying host credentials/configuration or executing ambient Git helpers.
func PrepareForwardCheckout(ctx context.Context, root, destination, sha string) error {
	if !shaRE.MatchString(sha) || !filepath.IsAbs(root) || !filepath.IsAbs(destination) {
		return errors.New("isolated checkout requires exact commit and absolute paths")
	}
	for _, step := range []struct {
		root string
		args []string
	}{
		{root, []string{"clone", "--no-hardlinks", "--no-local", "--no-checkout", root, destination}},
		{destination, []string{"fetch", "--no-tags", "--no-write-fetch-head", root, sha}},
		{destination, []string{"remote", "remove", "origin"}},
		{destination, []string{"checkout", "--detach", sha}},
	} {
		if _, err := forwardLocalGit(ctx, step.root, step.args...); err != nil {
			return err
		}
	}
	actual, err := forwardLocalGit(ctx, destination, "rev-parse", "HEAD")
	if err != nil || actual != sha {
		return errors.New("isolated checkout does not match exact commit")
	}
	status, err := forwardLocalGit(ctx, destination, "status", "--porcelain", "--untracked-files=all", "--ignore-submodules=all")
	if err != nil || status != "" {
		return errors.New("isolated exact checkout is not clean")
	}
	return nil
}

func prepareForwardRebase(ctx context.Context, root, candidate, main, head string) error {
	if !shaRE.MatchString(main) || !shaRE.MatchString(head) {
		return errors.New("mechanical rebase requires exact main and candidate commits")
	}
	if err := PrepareForwardCheckout(ctx, root, candidate, head); err != nil {
		return err
	}
	// Read fetches immutable objects, not persistent refs. Explicitly import M
	// too; cloning the controller does not copy FETCH_HEAD-only objects.
	if _, err := forwardLocalGit(ctx, candidate, "fetch", "--no-tags", "--no-write-fetch-head", root, main); err != nil {
		return err
	}
	fork, e := forwardLocalGit(ctx, candidate, "merge-base", main, head)
	if e != nil {
		return e
	}
	merges, e := forwardLocalGit(ctx, candidate, "rev-list", "--merges", fork+".."+head)
	if e != nil || merges != "" {
		return errors.New("non-linear candidate history requires explicit repair")
	}
	_, e = forwardLocalGit(ctx, candidate, "-c", "user.name=Auto-forward controller", "-c", "user.email=auto-forward@localhost", "rebase", "--no-autostash", "--no-update-refs", "--no-autosquash", "--no-rebase-merges", "--reapply-cherry-picks", "--empty=keep", "--onto", main, fork)
	if e != nil {
		conflicts, _ := forwardLocalGit(ctx, candidate, "diff", "--name-only", "--diff-filter=U")
		if conflicts == "" {
			return errors.New("mechanical rebase failed without a repairable conflict")
		}
		// A stopped rebase contains only the prefix through its first conflict.
		// Reconstruct the entire original proposal delta on M before semantic
		// repair, including later commits, binary additions, renames and deletes.
		patch, err := forwardLocalGitCommand(ctx, candidate, "diff", "--binary", "--full-index", "--no-ext-diff", "--no-textconv", fork, head, "--").Output()
		if err != nil {
			return errors.New("complete proposal delta is unavailable")
		}
		if _, err = forwardLocalGit(ctx, candidate, "rebase", "--abort"); err != nil {
			return err
		}
		if _, err = forwardLocalGit(ctx, candidate, "checkout", "--detach", main); err != nil {
			return err
		}
		apply := forwardLocalGitCommand(ctx, candidate, "apply", "--3way", "--index", "--whitespace=nowarn")
		apply.Stdin = bytes.NewReader(patch)
		if err = apply.Run(); err != nil {
			conflicts, probeErr := forwardLocalGit(ctx, candidate, "diff", "--name-only", "--diff-filter=U")
			if probeErr != nil || conflicts == "" {
				return errors.New("complete proposal delta could not be materialized for repair")
			}
		}
		// Even if aggregation avoided the intermediate conflict, semantic repair
		// must inspect and verify this complete candidate before any publication.
		return ErrForwardConflict
	}
	return nil
}

// forwardLocalGit is only for local data transformations, never network or
// authorized repository effects. Candidate attributes cannot select ambient
// filters, merge drivers, hooks, signing commands or credential helpers.
func forwardLocalGit(ctx context.Context, root string, args ...string) (string, error) {
	out, err := forwardLocalGitCommand(ctx, root, args...).Output()
	if err != nil {
		return "", errors.New("isolated mechanical Git operation failed")
	}
	return strings.TrimSpace(string(out)), nil
}

func forwardLocalGitCommand(ctx context.Context, root string, args ...string) *exec.Cmd {
	fixed := []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "core.attributesFile=/dev/null", "-c", "credential.helper=", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false", "-c", "submodule.recurse=false", "-C", root}
	cmd := exec.CommandContext(ctx, "git", append(fixed, args...)...)
	// This allowlist also excludes provider/App credentials and all GIT_*
	// environment overrides. The fixed operation has no authentication need.
	cmd.Env = repairEnvironment()
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_ATTR_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_TEMPLATE_DIR=")
	return cmd
}

func (n *NativeForward) write(ctx context.Context, args []string, workflows bool, mainProof ...string) error {
	hooks := "/dev/null"
	if len(mainProof) != 0 {
		if len(mainProof) != 2 {
			return errors.New("invalid main effect proof")
		}
		var cleanup func()
		var err error
		hooks, cleanup, err = prepareForwardMainHook(mainProof[0], mainProof[1])
		if err != nil {
			return err
		}
		defer cleanup()
	}
	token, release, e := n.req.Token(ctx, workflows)
	if e != nil {
		return errors.New("integration App lacks required existing Contents/Workflows write grant; ask an administrator, do not expand grants automatically")
	}
	defer release()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.hooksPath=" + hooks, "-c", "core.fsmonitor=false", "-c", "credential.helper=", "-c", "credential.helper=!gh auth git-credential", "-C", n.req.RepoRoot}, args...)...)
	cmd.Env = append(repairEnvironment(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GH_TOKEN="+token)

	if e = cmd.Run(); e != nil {
		return errors.New("integration Git effect rejected; observe refs and check installed App permissions")
	}
	return nil
}
func (n *NativeForward) needsWorkflows(ctx context.Context, a, b string) (bool, error) {
	out, e := goalGit(ctx, n.req.RepoRoot, "diff", "--name-only", a, b, "--", ".github/workflows")
	return out != "", e
}
func (n *NativeForward) Publish(ctx context.Context, old, next, main string) error {
	current, e := n.Read(ctx, old)
	if e != nil {
		return e
	}
	if !current.Ready || current.Head != old || current.Main != main {
		return errors.New("candidate/main moved before branch lease")
	}
	if e = n.Verify(ctx, main, next); e != nil {
		return e
	}
	decision, e := n.Policy(ctx, main, next)
	if e != nil {
		return e
	}
	if decision != "run" {
		return errors.New("fixed target superseded before publication")
	}
	workflows, e := n.needsWorkflows(ctx, old, next)
	if e != nil {
		return e
	}
	// Narrow actor transition: only a controller-created candidate from this
	// invocation is admitted under the integration App after the lease effect.
	n.writers[next] = n.req.IntegrationAppBotID
	return n.write(ctx, []string{"push", "--porcelain", "--force-with-lease=refs/heads/" + n.branch + ":" + old, "origin", next + ":refs/heads/" + n.branch}, workflows)
}
func (n *NativeForward) Checks(ctx context.Context, head string) (checkacceptance.Observation, error) {
	if e := ctx.Err(); e != nil {
		return checkacceptance.Observation{}, e
	}
	return checkacceptance.Observe(n.req.API, n.req.Repository, head)
}
func (n *NativeForward) Review(ctx context.Context) error { return n.req.Review(ctx) }
func (n *NativeForward) Forward(ctx context.Context, head, main string) error {
	current, e := n.Read(ctx, head)
	if e != nil {
		return e
	}
	if !current.Ready || current.Head != head || current.Main != main {
		return errors.New("candidate/main changed at integration boundary")
	}
	linear, e := n.Ancestor(ctx, main, head)
	if e != nil || !linear {
		return errors.New("main fast-forward precondition changed")
	}
	if e = n.Verify(ctx, main, head); e != nil {
		return e
	}
	if e = n.Review(ctx); e != nil {
		return e
	}
	checks, e := n.Checks(ctx, head)
	if e != nil {
		return e
	}
	if checks.Head != head || checks.State != checkacceptance.Succeeded {
		return errors.New("four exact-head checks required immediately before main write")
	}
	decision, e := n.Policy(ctx, main, head)
	if e != nil {
		return e
	}
	if decision != "run" {
		return errors.New("target no longer eligible before main write")
	}
	workflows, e := n.needsWorkflows(ctx, main, head)
	if e != nil {
		return e
	}
	return n.write(ctx, []string{"push", "--porcelain", "origin", head + ":refs/heads/" + n.req.BaseBranch}, workflows, head, main)
}
func (n *NativeForward) Cleanup(ctx context.Context, head string) error {
	for {
		current, e := n.Read(ctx, head)
		if e != nil {
			return e
		}
		contains, e := n.Ancestor(ctx, head, current.Main)
		if e != nil || !contains {
			return errors.New("main no longer contains candidate; branch retained")
		}
		if current.Merged {
			if current.Head == "" {
				return nil
			}
			if current.Head != head {
				return errors.New("integrated branch changed; preserved external edit")
			}
			var prs []publicationPR
			path := fmt.Sprintf("repos/%s/pulls?state=open&head=%s&per_page=100", n.req.Repository, url.QueryEscape(strings.Split(n.req.Repository, "/")[0]+":"+n.branch))
			if e = n.req.API.Request("GET", path, nil, &prs); e != nil || len(prs) != 0 {
				return errors.New("open PRs still use branch or cannot be ruled out; branch retained")
			}
			// A deletion is also leased, so a race cannot remove an external edit.
			e = n.write(ctx, []string{"push", "--porcelain", "--force-with-lease=refs/heads/" + n.branch + ":" + head, "origin", ":refs/heads/" + n.branch}, false)
			readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			actual, re := n.remote(readCtx, n.branch)
			cancel()
			if re != nil {
				return errors.New("branch deletion outcome unknown")
			}
			if actual == "" {
				return nil
			}
			if e != nil {
				return e
			}
			return errors.New("integrated branch deletion not confirmed")
		}
		if !current.Ready {
			return errors.New("GitHub has not confirmed merged PR; branch retained")
		}
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.New("deadline while waiting for GitHub merged observation; branch retained")
		case <-timer.C:
		}
	}
}

// Branch and RebasedHead expose only trusted continuation facts to subsequent
// jobs in this same workflow execution, never candidate-authored input.
func (n *NativeForward) Branch() string { return n.branch }
func (n *NativeForward) RebasedHead(head string) bool {
	return n.writers[head] > 0 && n.writers[head] == n.req.IntegrationAppBotID
}

// Verify binds every newly observed M/H pair to the original and current
// accepted contract lanes before publication or integration. CI remains a
// separate mandatory exact-head proof; this cache cannot cross M or H.
func (n *NativeForward) Verify(ctx context.Context, main, head string) error {
	key := main + ":" + head
	if n.req.Target.PeeledCommitSHA == "" && main == n.req.VerificationBase && head == n.req.ExpectedHead && n.proofs[key] {
		// Preserve the explicit reviewed manual FF path for its original M/H.
		// Any automatically reconciled candidate requires immutable proof and
		// must not replace its accepted gate/control authority.
		return nil
	}
	if n.req.Target.PeeledCommitSHA == "" {
		if err := validateGenericForwardScope(ctx, n.req.RepoRoot, main, head); err != nil {
			return err
		}
	}
	if err := validateForwardControls(ctx, n.req.RepoRoot, main, head); err != nil {
		return err
	}
	if n.proofs[key] {
		return nil
	}
	if err := n.semanticFailures[key]; err != nil {
		return err
	}
	if n.req.VerifyCandidate == nil {
		return errors.New("immutable current-main verification unavailable")
	}
	if err := n.req.VerifyCandidate(ctx, main, head); err != nil {
		if errors.Is(err, ErrForwardSemanticProof) && ctx.Err() == nil {
			if n.semanticFailures == nil {
				n.semanticFailures = map[string]error{}
			}
			// Do not replay a completed failed proof of unchanged M/H when a
			// linear candidate enters the fresh semantic repair path.
			n.semanticFailures[key] = err
		}
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	n.proofs[key] = true
	return nil
}
