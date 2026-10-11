package protocolsync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PrepareVerifierContinuation restores only the independently retained handwritten
// proposal. No old-base acceptance proof or generated tree is reused.
func PrepareVerifierContinuation(req VerifierContinuationRequest) (VerifierContinuation, SyncResult, error) {
	c, e := ContinueVerifier(req)
	if e != nil {
		return c, SyncResult{}, e
	}
	result := SyncResult{Target: c.Publication.Handoff.Target, Stage: "verifier_continuation"}
	if c.Duplicate {
		result.Outcome = "continuation_duplicate"
		return c, result, nil
	}
	if c.CandidateDir == "" || c.ProposalPatchPath == "" {
		return c, result, fmt.Errorf("continuation requires verified checkpoint materialization")
	}
	if e := AssertClean(req.RepoRoot); e != nil {
		return c, result, e
	}
	h := c.Publication.Handoff
	head, e := gitOutput(req.RepoRoot, "rev-parse", "HEAD")
	if e != nil || strings.TrimSpace(head) != c.MainSHA {
		return c, result, fmt.Errorf("local accepted main differs from continued head")
	}
	readAPI := req.ReadAPI
	if readAPI == nil {
		readAPI = req.API
	}
	pending, e := InspectPending(PendingRequest{RepoRoot: req.RepoRoot, Repository: req.Repository, BaseBranch: req.BaseBranch, BaseSHA: c.MainSHA, Remote: req.Remote, AppBotID: req.AppBotID, Target: h.Target, ReadChecks: true, API: readAPI})
	if e != nil {
		return c, result, e
	}
	result.Publication = &pending
	if pending.Reusable {
		result.Outcome = OutcomePRPending
		return c, result, nil
	}
	patch, e := verifierRead(c.ProposalPatchPath, 16<<20)
	if e != nil {
		return c, result, e
	}
	if repairDigest(patch) != h.ProposalSHA256 {
		return c, result, fmt.Errorf("continued proposal digest changed")
	}
	if e := VerifyCandidate(c.CandidateDir, h.CandidateSHA256, h.Target.RefName, h.Target.RefKind, h.Target.PeeledCommitSHA); e != nil {
		return c, result, e
	}
	// Reject an out-of-scope retained patch before it can mutate the accepted checkout.
	scopeDir, e := os.MkdirTemp("", "verifier-continuation-scope-")
	if e != nil {
		return c, result, e
	}
	defer os.RemoveAll(scopeDir)
	clone := filepath.Join(scopeDir, "repo")
	if e := runGit(req.RepoRoot, "clone", "--quiet", "--no-hardlinks", req.RepoRoot, clone); e != nil {
		return c, result, e
	}
	if e := applyProposalPatchBytes(clone, patch); e != nil {
		return c, result, e
	}
	if e := CheckScope(clone, "agent"); e != nil {
		return c, result, e
	}
	if e := applyProposalPatchBytes(req.RepoRoot, patch); e != nil {
		return c, result, e
	}
	if e := CheckScope(req.RepoRoot, "agent"); e != nil {
		return c, result, e
	}
	result.Outcome = OutcomeSemanticUnresolved
	result.CandidateDir = c.CandidateDir
	result.CandidateSHA256 = h.CandidateSHA256
	if e := verifierJSON(filepath.Join(req.OutputDir, "continuation.json"), c); e != nil {
		return c, result, e
	}
	return c, result, nil
}

// BindVerifierContinuation is used only after native continuation resolution in
// this trusted workflow. It rechecks local identities immediately before repair.
func BindVerifierContinuation(req *RepairRequest, receipt string) error {
	b, e := verifierRead(receipt, 1<<20)
	if e != nil {
		return e
	}
	var c VerifierContinuation
	if e := verifierDecode(b, &c); e != nil {
		return e
	}
	h := c.Publication.Handoff
	if c.Duplicate || c.MainSHA == "" || h.Handoffs != 1 || h.MaxRounds < 1 || h.MaxRounds > 12 || h.RoundsUsed < 1 || h.RoundsUsed > h.MaxRounds {
		return fmt.Errorf("invalid continued repair identity or budget")
	}
	head, e := gitOutput(req.RepoRoot, "rev-parse", "HEAD")
	if e != nil {
		return e
	}
	if strings.TrimSpace(head) != c.MainSHA {
		return fmt.Errorf("continued main identity changed")
	}
	if req.TargetRef != h.Target.RefName || req.TargetKind != h.Target.RefKind || req.TargetSHA != h.Target.PeeledCommitSHA || req.CandidateSHA256 != h.CandidateSHA256 {
		return fmt.Errorf("continued target identity changed")
	}
	patch, e := repairPatch(req.RepoRoot)
	if e != nil {
		return e
	}
	if repairDigest(patch) != h.ProposalSHA256 {
		return fmt.Errorf("continued handwritten proposal changed")
	}
	if e := CheckScope(req.RepoRoot, "agent"); e != nil {
		return e
	}
	if e := VerifyCandidate(req.CandidateDir, h.CandidateSHA256, req.TargetRef, req.TargetKind, req.TargetSHA); e != nil {
		return e
	}
	deadline, e := time.Parse(time.RFC3339Nano, h.DeadlineUTC)
	if e != nil || !time.Now().Before(deadline) {
		return fmt.Errorf("continued repair deadline expired or invalid")
	}
	// The receipt must not be a path the model can rewrite during repair.
	receiptAbs, e := filepath.EvalSymlinks(receipt)
	if e != nil {
		return e
	}
	root, e := filepath.EvalSymlinks(req.RepoRoot)
	if e != nil {
		return e
	}
	if rel, e := filepath.Rel(root, receiptAbs); e != nil || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))) {
		return fmt.Errorf("continuation receipt must be outside proposal workspace")
	}
	req.DeadlineUTC = h.DeadlineUTC
	req.MaxRounds = h.MaxRounds
	req.ConsumedRounds = h.RoundsUsed
	req.VerifierHandoffs = 1
	req.VerifyBeforeModel = true
	req.Verifier = nil
	return nil
}
