package protocolsync

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/checkacceptance"
)

// GoalHandoff is a trusted controller output, produced only after successful
// native completion and cleanup. It is not an attestation or candidate input.
type GoalHandoff struct {
	VerificationBase string    `json:"verification_base"`
	Repository       string    `json:"repository"`
	Number           int       `json:"pr_number"`
	Head             string    `json:"expected_head"`
	OriginalBase     string    `json:"original_base"`
	Branch           string    `json:"branch"`
	AppBotID         int64     `json:"app_bot_id"`
	TargetRef        string    `json:"target_ref"`
	TargetKind       string    `json:"target_kind"`
	TargetSHA        string    `json:"target_sha"`
	Deadline         time.Time `json:"deadline"`
}

// CompletedGoalHandoff must be called by the launcher only after successful
// runtime cleanup. Failed command results are never exported as handoffs.
func CompletedGoalHandoff(binding GoalPublicationRequest, result GoalFlowResult, deadline time.Time) (GoalHandoff, error) {
	if goalBinding(binding) != nil || result.NativeStatus != "complete" || result.Failure != "" || result.Stage != "Final acceptance" || !shaRE.MatchString(result.Head) || result.Head == binding.ExpectedHead || result.SnapshotDigest == "" || !goalSuccessfulChecks(result.Checks, result.Head) || !deadline.After(time.Now()) {
		return GoalHandoff{}, errors.New("goal has no completed exact-head handoff")
	}
	verifiedBase := result.VerificationBaseSHA
	if verifiedBase == "" {
		verifiedBase = binding.BaseSHA
	}
	if !shaRE.MatchString(verifiedBase) {
		return GoalHandoff{}, errors.New("verified baseline identity unavailable")
	}
	return GoalHandoff{VerificationBase: verifiedBase, Repository: binding.Repository, Number: binding.Number, Head: result.Head, OriginalBase: binding.BaseSHA, Branch: binding.Branch, AppBotID: binding.AppBotID, TargetRef: binding.Target.RefName, TargetKind: binding.Target.RefKind, TargetSHA: binding.Target.PeeledCommitSHA, Deadline: deadline}, nil
}
func goalSuccessfulChecks(o checkacceptance.Observation, head string) bool {
	if o.Head != head || o.State != checkacceptance.Succeeded || o.WorkflowState != checkacceptance.Succeeded || o.WorkflowRunID <= 0 || o.WorkflowAttempt <= 0 {
		return false
	}
	required := checkacceptance.RequiredNames()
	if len(o.Checks) != len(required) {
		return false
	}
	seen := map[string]bool{}
	for _, c := range o.Checks {
		if seen[c.Name] || c.State != checkacceptance.Succeeded || c.RunID <= 0 || c.Conclusion != "success" {
			return false
		}
		seen[c.Name] = true
	}
	for _, name := range required {
		if !seen[name] {
			return false
		}
	}
	return true
}

// ReadyGoal validates exact identity and fresh existing CI before the single
// Ready mutation. Uncertain writes are reconciled once, never blindly retried.
func ReadyGoal(ctx context.Context, root string, h GoalHandoff, api PublicationAPI, lookup RemoteLookuper) error {
	if !shaRE.MatchString(h.VerificationBase) {
		return errors.New("verified baseline handoff identity missing")
	}
	if !h.Deadline.After(time.Now()) {
		return errors.New("goal handoff deadline expired")
	}
	ctx, cancel := context.WithDeadline(ctx, h.Deadline)
	defer cancel()
	r := GoalPublicationRequest{RepoRoot: root, Repository: h.Repository, BaseBranch: "main", Branch: h.Branch, BaseSHA: h.OriginalBase, VerificationBaseSHA: h.VerificationBase, ExpectedHead: h.Head, Number: h.Number, AppBotID: h.AppBotID, Target: Target{RefName: h.TargetRef, RefKind: h.TargetKind, PeeledCommitSHA: h.TargetSHA}, API: api, Lookuper: lookup}
	if err := goalBinding(r); err != nil {
		return err
	}
	pr, err := goalHandoffCurrent(ctx, r)
	if err != nil {
		return err
	}
	checks, err := checkacceptance.Observe(api, h.Repository, h.Head)
	if err != nil || !goalSuccessfulChecks(checks, h.Head) {
		return errors.New("handoff exact-head checks unavailable or unsuccessful")
	}
	pr, err = goalHandoffCurrent(ctx, r)
	if err != nil {
		return err
	}
	if !pr.Draft {
		return nil
	}
	if pr.NodeID == "" {
		return errors.New("goal PR node identity missing")
	}
	// GraphQL is GitHub's native Ready operation. Its response is not authority;
	// fresh PR and branch observations establish the effect even on lost response.
	_ = api.Request("POST", "graphql", map[string]any{"query": "mutation($id:ID!){markPullRequestReadyForReview(input:{pullRequestId:$id}){pullRequest{id isDraft}}}", "variables": map[string]any{"id": pr.NodeID}}, nil)
	actual, err := goalHandoffCurrent(ctx, r)
	if err != nil || actual.Draft {
		return errors.New("Ready outcome unconfirmed; reconcile the same handoff")
	}
	checks, err = checkacceptance.Observe(api, h.Repository, h.Head)
	if err != nil || !goalSuccessfulChecks(checks, h.Head) {
		return errors.New("Ready head no longer has successful checks")
	}
	_, err = goalHandoffCurrent(ctx, r)
	return err
}

type goalReadyPR struct {
	goalDraftPR
	NodeID string `json:"node_id"`
}

func goalHandoffCurrent(ctx context.Context, r GoalPublicationRequest) (goalReadyPR, error) {
	var pr goalReadyPR
	if _, err := goalMainForHandoff(ctx, r); err != nil {
		return pr, err
	}
	if ctx.Err() != nil {
		return pr, ctx.Err()
	}
	if r.API.Request("GET", fmt.Sprintf("repos/%s/pulls/%d", r.Repository, r.Number), nil, &pr) != nil {
		return pr, errors.New("cannot read handoff PR")
	}
	if pr.Number != r.Number || pr.State != "open" || pr.MergedAt != nil || pr.Head.Ref != r.Branch || pr.Head.Repo.FullName != r.Repository || pr.Base.Ref != r.BaseBranch || pr.Base.Repo.FullName != r.Repository || pr.URL != fmt.Sprintf("https://github.com/%s/pull/%d", r.Repository, r.Number) || verifyAppActor(pr.User, r.AppBotID) != nil {
		return pr, errors.New("handoff PR identity changed")
	}
	head, err := goalRemoteHead(ctx, r, r.Branch)
	if err != nil || head != r.ExpectedHead {
		return pr, errors.New("handoff remote head changed")
	}
	actor, err := publicationHeadActor(PendingRequest{Repository: r.Repository}, r.API, r.Branch, head)
	if err != nil || verifyAppActor(actor, r.AppBotID) != nil {
		return pr, errors.New("handoff head actor changed")
	}
	target, err := ResolveUpstream(ResolveRequest{UpstreamRef: r.Target.RefName, Lookuper: goalLookupContext(ctx, r.Lookuper)})
	if err != nil || target.RefName != r.Target.RefName || target.RefKind != r.Target.RefKind || target.PeeledCommitSHA != r.Target.PeeledCommitSHA {
		return pr, errors.New("handoff target changed")
	}
	var file struct{ Encoding, Content string }
	path := fmt.Sprintf("repos/%s/contents/codexsdk/internal/protocolschema/appserver/v2/baseline_metadata.json?ref=%s", r.Repository, head)
	if r.API.Request("GET", path, nil, &file) != nil || file.Encoding != "base64" {
		return pr, errors.New("handoff baseline unavailable")
	}
	data, err := base64.StdEncoding.DecodeString(file.Content)
	if err != nil {
		return pr, errors.New("handoff baseline malformed")
	}
	baseline, err := decodeBaselineIdentity(data)
	if err != nil || baseline.SourceCommit != r.Target.PeeledCommitSHA || baseline.SourceRefName != r.Target.RefName || baseline.SourceRefKind != r.Target.RefKind {
		return pr, errors.New("handoff is an empty seed or different target")
	}
	return pr, ctx.Err()
}
