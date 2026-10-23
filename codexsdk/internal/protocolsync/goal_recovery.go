package protocolsync

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// GoalRecoveryBinding is the public launch record retained by the trusted
// workflow. It is data, not authorization, until its producing run and artifact
// and all live identities have been independently checked.
type GoalRecoveryBinding struct {
	Schema        int       `json:"schema"`
	Repository    string    `json:"repository"`
	RunID         int64     `json:"run_id"`
	RunAttempt    int       `json:"run_attempt"`
	ControllerSHA string    `json:"controller_sha"`
	Base          string    `json:"base"`
	Branch        string    `json:"branch"`
	Number        int       `json:"number"`
	Head          string    `json:"head"`
	AppBotID      int64     `json:"app_bot_id"`
	TargetRef     string    `json:"target_ref"`
	TargetKind    string    `json:"target_kind"`
	TargetSHA     string    `json:"target_sha"`
	Deadline      time.Time `json:"deadline"`
	URL           string    `json:"url"`
	Confirmed     bool      `json:"confirmed"`
}

// GoalRecoveryRequest is an explicit operator request, never a scheduled retry.
// Original B/U/deadline are obtained only from authenticated retained evidence.
// A new execution needs a separate explicit authorization and finite deadline;
// neither the old deadline nor the old native thread is resumed or rewritten.
type GoalRecoveryRequest struct {
	RepoRoot, OutputDir, Repository, CurrentBaseSHA, ExpectedHead string
	Number                                                        int
	AppBotID                                                      int64
	PriorRunID                                                    int64
	AuthorizeNewBoundedExecution                                  bool
	NewExecutionDeadline                                          time.Time
	RegenerateEmptySeed                                           bool
	API                                                           PublicationAPI
	Lookuper                                                      RemoteLookuper
}

type GoalRecoveryResult struct {
	AdoptedEvidenceDir string              `json:"adopted_evidence_dir,omitempty"`
	Outcome            string              `json:"outcome"`
	Reason             string              `json:"reason"`
	Binding            GoalRecoveryBinding `json:"binding"`
	Target             Target              `json:"target"`
	Classification     string              `json:"classification"`
	ExpectedHead       string              `json:"expected_head"`
	CandidateDir       string              `json:"candidate_dir,omitempty"`
	BindingFile        string              `json:"binding_file,omitempty"`
	PriorRunID         int64               `json:"prior_run_id"`
	ArtifactID         int64               `json:"artifact_id"`
	PriorDeadline      time.Time           `json:"prior_deadline"`
	ExecutionDeadline  time.Time           `json:"execution_deadline"`
}

const goalRecoveryWorkflow = ".github/workflows/codexsdk-upstream-protocol-sync.yml"
const goalRecoveryArchiveLimit = 128 << 20

type goalRecoveryRun struct {
	ID         int64  `json:"id"`
	Attempt    int    `json:"run_attempt"`
	WorkflowID int64  `json:"workflow_id"`
	Path       string `json:"path"`
	Event      string `json:"event"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HeadBranch string `json:"head_branch"`
	HeadSHA    string `json:"head_sha"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	HeadRepository struct {
		FullName string `json:"full_name"`
	} `json:"head_repository"`
}
type goalRecoveryArtifact struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Expired     bool   `json:"expired"`
	Size        int64  `json:"size_in_bytes"`
	Digest      string `json:"digest"`
	WorkflowRun struct {
		ID         int64  `json:"id"`
		HeadSHA    string `json:"head_sha"`
		HeadBranch string `json:"head_branch"`
	} `json:"workflow_run"`
}
type goalRecoveryDownload func(context.Context, GoalRecoveryRequest, int64) ([]byte, error)

// RecoverGoalDraft performs read-only remote observations and materializes only
// isolated local source inputs. It cannot push, create a PR, mark Ready, dispatch
// a workflow, run candidate code, or resurrect a private native runtime.
func RecoverGoalDraft(ctx context.Context, req GoalRecoveryRequest) (GoalRecoveryResult, error) {
	return recoverGoalDraft(ctx, req, downloadGoalRecoveryArtifact, time.Now())
}

func recoverGoalDraft(ctx context.Context, req GoalRecoveryRequest, download goalRecoveryDownload, now time.Time) (result GoalRecoveryResult, retErr error) {
	started := time.Now()
	result.Outcome, result.PriorRunID = "blocked", req.PriorRunID
	result.ExpectedHead = req.ExpectedHead
	defer func() {
		if ctx.Err() != nil {
			retErr = ctx.Err()
		}
		if retErr != nil {
			result.Outcome, result.Reason = "blocked", retErr.Error()
		}
	}()
	if req.API == nil || req.RepoRoot == "" || req.OutputDir == "" || !goalRepositoryPattern.MatchString(req.Repository) || !shaRE.MatchString(req.CurrentBaseSHA) || !shaRE.MatchString(req.ExpectedHead) || req.Number <= 0 || req.AppBotID <= 0 || req.PriorRunID <= 0 {
		return result, errors.New("explicit repository, current main, prior run, PR, expected head and App actor are required")
	}
	head, err := goalGit(ctx, req.RepoRoot, "rev-parse", "HEAD")
	if err != nil || head != req.CurrentBaseSHA || AssertClean(req.RepoRoot) != nil {
		return result, errors.New("recovery controller must be the clean accepted current main checkout")
	}
	run, artifact, err := goalRecoveryProvenance(req)
	if err != nil {
		return result, err
	}
	result.ArtifactID = artifact.ID
	data, err := download(ctx, req, artifact.ID)
	if err != nil {
		return result, err
	}
	if len(data) == 0 || len(data) > goalRecoveryArchiveLimit || int64(len(data)) != artifact.Size || fmt.Sprintf("sha256:%x", sha256.Sum256(data)) != artifact.Digest {
		return result, errors.New("retained artifact size or authenticated digest mismatch")
	}
	files, err := readGoalRecoveryArchive(data)
	if err != nil {
		return result, err
	}
	var binding GoalRecoveryBinding
	var source struct{ Target Target }
	var previous GoalFlowResult
	if json.Unmarshal(files["goal-binding.json"], &binding) != nil || json.Unmarshal(files["goal-source.json"], &source) != nil || json.Unmarshal(files["goal-result.json"], &previous) != nil || previous.Stage == "" {
		return result, errors.New("required retained binding, source or result is missing or malformed")
	}
	result.Binding, result.PriorDeadline = binding, binding.Deadline
	target := Target{RefName: binding.TargetRef, RefKind: binding.TargetKind, PeeledCommitSHA: binding.TargetSHA}
	result.Target = target
	if binding.Schema != 1 || binding.Repository != req.Repository || binding.RunID != run.ID || binding.RunAttempt != run.Attempt || binding.ControllerSHA != run.HeadSHA || binding.Number != req.Number || binding.AppBotID != req.AppBotID || !binding.Confirmed || binding.URL != fmt.Sprintf("https://github.com/%s/pull/%d", req.Repository, req.Number) || !shaRE.MatchString(binding.Base) || !shaRE.MatchString(binding.Head) || !nativeGoalBranch.MatchString(binding.Branch) || !shaRE.MatchString(target.PeeledCommitSHA) || target.RefName == "" || target.RefKind == "" || binding.Deadline.IsZero() {
		return result, errors.New("retained launch binding lacks authenticated original identity or deadline")
	}
	if source.Target.RefName != target.RefName || source.Target.RefKind != target.RefKind || source.Target.PeeledCommitSHA != target.PeeledCommitSHA {
		return result, errors.New("retained source differs from fixed target")
	}
	prefix := syncBranchName(target.RefName, target.PeeledCommitSHA, binding.Base) + "-goal-"
	if !strings.HasPrefix(binding.Branch, prefix) {
		return result, errors.New("retained branch allocation differs from original base or fixed target")
	}
	publication := GoalPublicationRequest{RepoRoot: req.RepoRoot, Repository: req.Repository, BaseBranch: "main", Branch: binding.Branch, BaseSHA: binding.Base, VerificationBaseSHA: req.CurrentBaseSHA, ExpectedHead: req.ExpectedHead, Number: req.Number, AppBotID: req.AppBotID, Target: target, API: req.API, Lookuper: req.Lookuper}
	if err = goalRecoveryCurrent(ctx, req, publication); err != nil {
		return result, err
	}
	output, err := goalRecoveryOutput(req.OutputDir, req.RepoRoot)
	if err != nil {
		return result, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(output)
		}
	}()
	graph := filepath.Join(output, "graph")
	if _, err = goalArtifactGit(ctx, output, "init", "--quiet", graph); err != nil {
		return result, err
	}
	// Import known accepted objects from the controller, then the exact PR head
	// from its existing origin. No ref or object in the controller is changed.
	for _, sha := range []string{binding.Base, binding.ControllerSHA, req.CurrentBaseSHA, binding.Head, req.ExpectedHead} {
		if _, err = goalArtifactGit(ctx, graph, "fetch", "--no-tags", "--no-write-fetch-head", req.RepoRoot, sha); err != nil {
			if sha != req.ExpectedHead && sha != binding.Head {
				return result, errors.New("original/current accepted commit unavailable")
			}
			remote, e := goalGit(ctx, req.RepoRoot, "remote", "get-url", "origin")
			if e != nil || (remote != "https://github.com/"+req.Repository+".git" && remote != "https://github.com/"+req.Repository) {
				return result, errors.New("exact recovery head unavailable from canonical origin")
			}
			if _, err = goalGit(ctx, graph, "fetch", "--no-tags", "--no-write-fetch-head", remote, sha); err != nil {
				return result, errors.New("exact recovery head unavailable")
			}
		}
	}
	seed := goalRecoveryEmptySeed(ctx, graph, binding.Base, req.ExpectedHead)
	if seed {
		result.Classification = "empty_seed"
	} else {
		result.Classification = "candidate"
	}
	if _, err = goalArtifactGit(ctx, graph, "merge-base", "--is-ancestor", binding.Base, req.CurrentBaseSHA); err != nil {
		return result, errors.New("current main no longer descends from original accepted base")
	}
	for _, ancestry := range [][2]string{{binding.Base, binding.ControllerSHA}, {binding.ControllerSHA, req.CurrentBaseSHA}, {binding.Base, binding.Head}, {binding.Head, req.ExpectedHead}} {
		if _, err = goalArtifactGit(ctx, graph, "merge-base", "--is-ancestor", ancestry[0], ancestry[1]); err != nil {
			return result, errors.New("recovery would discard a retained controller or bound head ancestry obligation")
		}
	}

	if _, err = goalArtifactGit(ctx, graph, "merge-base", "--is-ancestor", req.CurrentBaseSHA, req.ExpectedHead); err != nil {
		result.Classification = "divergent_" + result.Classification
		return result, errors.New("current main is not an ancestor of the retained PR head; requires separately authorized exact-lease rewrite or replacement, not automatic append recovery")
	}
	merges, err := goalArtifactGit(ctx, graph, "rev-list", "--min-parents=2", req.CurrentBaseSHA+".."+req.ExpectedHead)
	if err != nil || merges != "" {
		return result, errors.New("retained PR history does not support existing linear fast-forward integration")
	}
	decision, err := ResolveGoalRebinding(graph, binding.Base, req.CurrentBaseSHA, BaselineIdentity{SourceRefName: target.RefName, SourceRefKind: target.RefKind, SourceCommit: target.PeeledCommitSHA})
	if err != nil || decision.Decision != GoalRebindingRun {
		return result, errors.New("fixed target is not admissible on current main: " + decision.Reason)
	}
	result.ExecutionDeadline = binding.Deadline
	if req.AuthorizeNewBoundedExecution {
		if !req.NewExecutionDeadline.After(now) || req.NewExecutionDeadline.After(now.Add(6*time.Hour)) {
			return result, errors.New("explicit new execution requires a future deadline no more than six hours away")
		}
		result.ExecutionDeadline = req.NewExecutionDeadline
	} else {
		if !req.NewExecutionDeadline.IsZero() {
			return result, errors.New("new deadline requires explicit new bounded execution authorization")
		}
		if !binding.Deadline.After(now) {
			return result, errors.New("original attempt deadline expired; explicit new bounded execution authorization is required")
		}
	}
	state := filepath.Join(output, "goal-publication")
	statePrefix := "goal-publication/"
	_, activeIntent := files[statePrefix+goalIntentName]
	_, activeBundle := files[statePrefix+goalBundleName]
	if previous.PublicationIntentHead != "" && (!shaRE.MatchString(previous.PublicationIntentHead) || !activeIntent || !activeBundle) {
		return result, errors.New("retained result records a publication intent whose complete active evidence is unavailable")
	}
	adopted := false
	// A partial active effect is unresolved authority, never a reason to fall
	// back to older source. Adopted evidence is only an already-observed source
	// locator, not an active intent and not a renewed execution deadline.
	if !activeIntent && !activeBundle {
		_, adoptedIntent := files["goal-adopted-publication/"+goalIntentName]
		_, adoptedBundle := files["goal-adopted-publication/"+goalBundleName]
		if adoptedIntent || adoptedBundle {
			adopted = true
			statePrefix = "goal-adopted-publication/"
			state = filepath.Join(output, "goal-adopted-publication")
		}
	}
	if err = os.Mkdir(state, 0700); err != nil {
		return result, err
	}
	for _, name := range []string{goalIntentName, goalBundleName} {
		if content, ok := files[statePrefix+name]; ok {
			if err = os.WriteFile(filepath.Join(state, name), content, 0600); err != nil {
				return result, err
			}
		}
	}
	publication.StateDir, publication.Deadline = state, binding.Deadline
	if adopted {
		var receipt GoalPublicationIntent
		if json.Unmarshal(files[statePrefix+goalIntentName], &receipt) != nil || receipt.IntendedHead != binding.Head || binding.Head != req.ExpectedHead || receipt.Deadline.IsZero() {
			return result, errors.New("adopted source is not the current authenticated launch head")
		}
		// Keep historical receipt authority distinct from this attempt's budget.
		// readGoalPublicationIntent also validates every immutable source binding.
		publication.Deadline = receipt.Deadline
	}
	intent, err := readGoalPublicationIntent(ctx, publication)
	if err != nil {
		return result, err
	}
	if previous.PublicationIntentHead != "" && (intent == nil || intent.IntendedHead != previous.PublicationIntentHead) {
		return result, errors.New("active publication intent differs from the retained result intent head")
	}
	candidate := filepath.Join(output, "candidate")
	if intent == nil {
		if !seed || req.ExpectedHead != binding.Head || previous.Head != "" {
			return result, errors.New("candidate publication has no complete retained source and intent; cannot regenerate it as an empty seed")
		}
		if !req.RegenerateEmptySeed {
			return result, errors.New("empty seed requires explicit regeneration authorization; no candidate or private native thread can be resumed")
		}
		if _, err = goalArtifactGit(ctx, output, "init", "--quiet", candidate); err != nil {
			return result, err
		}
		if _, err = goalArtifactGit(ctx, candidate, "fetch", "--no-tags", graph, req.ExpectedHead); err != nil {
			return result, err
		}
		if _, err = goalArtifactGit(ctx, candidate, "checkout", "--quiet", "--detach", "FETCH_HEAD"); err != nil {
			return result, err
		}
		result.Classification = "regenerate_empty_seed"
	} else {
		if _, err = goalArtifactGit(ctx, graph, "fetch", "--no-tags", "--no-write-fetch-head", req.RepoRoot, intent.VerificationBaseSHA); err != nil {
			return result, errors.New("historically verified accepted baseline unavailable")
		}
		for _, ancestry := range [][2]string{{binding.Base, intent.VerificationBaseSHA}, {intent.VerificationBaseSHA, req.CurrentBaseSHA}} {
			if _, err = goalArtifactGit(ctx, graph, "merge-base", "--is-ancestor", ancestry[0], ancestry[1]); err != nil {
				return result, errors.New("recovery would discard historically verified accepted baseline obligations")
			}
		}
		if req.ExpectedHead != intent.IntendedHead {
			return result, errors.New("retained push is unresolved at its previous or a foreign head; recovery never retries that write")
		}
		tree, e := goalArtifactGit(ctx, graph, "rev-parse", req.ExpectedHead+"^{tree}")
		parents, pe := goalArtifactGit(ctx, graph, "show", "-s", "--format=%P", req.ExpectedHead)
		if e != nil || pe != nil || tree != intent.SnapshotTree || parents != intent.PreviousHead {
			return result, errors.New("observed candidate is not the exact retained single-parent publication")
		}
		if previous.Head != "" && previous.Head != req.ExpectedHead {
			return result, errors.New("retained result and publication intent disagree about observed head")
		}
		if _, err = goalArtifactGit(ctx, output, "init", "--quiet", candidate); err != nil {
			return result, err
		}
		if _, err = goalArtifactGit(ctx, candidate, "fetch", "--no-tags", filepath.Join(state, goalBundleName), "HEAD"); err != nil {
			return result, err
		}
		if _, err = goalArtifactGit(ctx, candidate, "checkout", "--quiet", "--detach", "FETCH_HEAD"); err != nil {
			return result, err
		}
		digest, e := goalSnapshot(ctx, candidate, "")
		if e != nil || digest != intent.SnapshotDigest {
			return result, errors.New("materialized recovery candidate differs from retained verified bytes")
		}
		result.Classification = "retained_candidate"
		result.AdoptedEvidenceDir = state
	}
	// Re-read the terminal run and exact refs after materialization. Admission is
	// a snapshot, not a lease; the trusted launcher repeats its effect boundary.
	again, retainedArtifact, err := goalRecoveryProvenance(req)
	if err != nil || again != run || retainedArtifact != artifact {
		return result, errors.New("prior execution identity changed during recovery admission")
	}
	if err = goalRecoveryCurrent(ctx, req, publication); err != nil {
		return result, err
	}
	if !result.ExecutionDeadline.After(now.Add(time.Since(started))) {
		return result, errors.New("authorized execution deadline expired during recovery admission")
	}
	bindingFile := filepath.Join(output, "recovered-binding.json")
	data, err = json.Marshal(binding)
	if err != nil {
		return result, err
	}
	if err = os.WriteFile(bindingFile, append(data, '\n'), 0600); err != nil {
		return result, err
	}
	result.Outcome, result.Reason = "admitted", "same Draft PR admitted for a fresh native goal; original obligations and fresh verification remain required"
	result.CandidateDir, result.BindingFile = candidate, bindingFile
	keep = true
	return result, nil
}

func goalRecoveryProvenance(req GoalRecoveryRequest) (goalRecoveryRun, goalRecoveryArtifact, error) {
	var run goalRecoveryRun
	var artifact goalRecoveryArtifact
	fail := func(s string) (goalRecoveryRun, goalRecoveryArtifact, error) { return run, artifact, errors.New(s) }
	root := "repos/" + req.Repository
	if req.API.Request("GET", fmt.Sprintf("%s/actions/runs/%d", root, req.PriorRunID), nil, &run) != nil {
		return fail("prior workflow run unavailable")
	}
	if run.ID != req.PriorRunID || run.Attempt <= 0 || run.WorkflowID <= 0 || run.Path != goalRecoveryWorkflow || run.HeadBranch != "main" || !shaRE.MatchString(run.HeadSHA) || run.Repository.FullName != req.Repository || run.HeadRepository.FullName != req.Repository || (run.Event != "workflow_dispatch" && run.Event != "schedule") {
		return fail("prior run is not the trusted main upstream workflow")
	}
	if run.Status != "completed" || (run.Conclusion != "failure" && run.Conclusion != "cancelled" && run.Conclusion != "timed_out") {
		return fail("prior workflow must be terminal and unsuccessful before explicit recovery")
	}
	var workflow struct {
		ID   int64  `json:"id"`
		Path string `json:"path"`
	}
	if req.API.Request("GET", root+"/actions/workflows/codexsdk-upstream-protocol-sync.yml", nil, &workflow) != nil || workflow.ID != run.WorkflowID || workflow.Path != goalRecoveryWorkflow {
		return fail("prior workflow identity cannot be authenticated")
	}
	var listing struct {
		Total     int                    `json:"total_count"`
		Artifacts []goalRecoveryArtifact `json:"artifacts"`
	}
	if req.API.Request("GET", fmt.Sprintf("%s/actions/runs/%d/artifacts?per_page=100", root, run.ID), nil, &listing) != nil || listing.Total != len(listing.Artifacts) || listing.Total > 100 {
		return fail("prior artifact listing unavailable or exceeds bounded recovery")
	}
	name := fmt.Sprintf("protocol-native-goal-%d", run.Attempt)
	found := 0
	for _, a := range listing.Artifacts {
		if a.Name == name {
			artifact = a
			found++
		}
	}
	if found != 1 || artifact.ID <= 0 || artifact.Expired || artifact.Size <= 0 || artifact.Size > goalRecoveryArchiveLimit || artifact.WorkflowRun.ID != run.ID || artifact.WorkflowRun.HeadSHA != run.HeadSHA || artifact.WorkflowRun.HeadBranch != run.HeadBranch || !strings.HasPrefix(artifact.Digest, "sha256:") || !goalDigest(strings.TrimPrefix(artifact.Digest, "sha256:")) {
		return fail("prior artifact is missing, ambiguous, expired or lacks authenticated provenance")
	}
	return run, artifact, nil
}

func goalRecoveryCurrent(ctx context.Context, req GoalRecoveryRequest, binding GoalPublicationRequest) error {
	var repo struct {
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
	}
	if req.API.Request("GET", "repos/"+req.Repository, nil, &repo) != nil || repo.FullName != req.Repository || repo.DefaultBranch != "main" {
		return errors.New("recovery repository/default branch identity changed")
	}
	var pr goalDraftPR
	if req.API.Request("GET", fmt.Sprintf("repos/%s/pulls/%d", req.Repository, req.Number), nil, &pr) != nil || pr.Number != req.Number || pr.State != "open" || !pr.Draft || pr.MergedAt != nil || pr.Head.Repo.FullName != req.Repository || pr.Base.Repo.FullName != req.Repository || pr.Head.Ref != binding.Branch || pr.Base.Ref != "main" || pr.URL != bindingURL(binding) || verifyAppActor(pr.User, req.AppBotID) != nil {
		return errors.New("recovery Draft PR identity or creator changed")
	}
	for branch, expected := range map[string]string{"main": req.CurrentBaseSHA, binding.Branch: req.ExpectedHead} {
		var ref struct {
			Ref    string `json:"ref"`
			Object struct {
				Type string `json:"type"`
				SHA  string `json:"sha"`
			} `json:"object"`
		}
		if req.API.Request("GET", "repos/"+req.Repository+"/git/ref/heads/"+branch, nil, &ref) != nil || ref.Ref != "refs/heads/"+branch || ref.Object.Type != "commit" || ref.Object.SHA != expected {
			return errors.New("recovery current main or exact PR head changed")
		}
	}
	actor, err := publicationHeadActor(PendingRequest{Repository: req.Repository}, req.API, binding.Branch, req.ExpectedHead)
	if err != nil || verifyAppActor(actor, req.AppBotID) != nil {
		return errors.New("recovery head actor cannot be confirmed")
	}
	selected, err := ResolveUpstream(ResolveRequest{UpstreamRef: binding.Target.RefName, Lookuper: goalLookupContext(ctx, req.Lookuper)})
	if err != nil || selected.RefName != binding.Target.RefName || selected.RefKind != binding.Target.RefKind || selected.PeeledCommitSHA != binding.Target.PeeledCommitSHA {
		return errors.New("fixed recovery upstream target changed")
	}
	return ctx.Err()
}
func bindingURL(binding GoalPublicationRequest) string {
	return fmt.Sprintf("https://github.com/%s/pull/%d", binding.Repository, binding.Number)
}
func goalRecoveryEmptySeed(ctx context.Context, root, base, head string) bool {
	parents, e := goalArtifactGit(ctx, root, "show", "-s", "--format=%P", head)
	if e != nil || parents != base {
		return false
	}
	a, e := goalArtifactGit(ctx, root, "rev-parse", base+"^{tree}")
	if e != nil {
		return false
	}
	b, e := goalArtifactGit(ctx, root, "rev-parse", head+"^{tree}")
	return e == nil && a == b
}
func goalRecoveryOutput(output, controller string) (string, error) {
	abs, e := filepath.Abs(output)
	if e != nil {
		return "", e
	}
	root, e := filepath.Abs(controller)
	if e != nil {
		return "", e
	}
	if goalWithin(root, abs) || goalWithin(abs, root) {
		return "", errors.New("recovery output must be separate from the accepted checkout")
	}
	real, e := filepath.EvalSymlinks(filepath.Dir(abs))
	if e != nil || real != filepath.Dir(abs) {
		return "", errors.New("recovery output parent is missing or traverses a symlink")
	}
	if e = os.Mkdir(abs, 0700); e != nil {
		return "", errors.New("recovery output must be a new isolated directory")
	}
	return abs, nil
}
func readGoalRecoveryArchive(data []byte) (map[string][]byte, error) {
	archive, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if e != nil || len(archive.File) > 256 {
		return nil, errors.New("invalid or oversized recovery archive")
	}
	wanted := map[string]bool{"goal-binding.json": true, "goal-source.json": true, "goal-result.json": true, "goal-publication/" + goalIntentName: true, "goal-publication/" + goalBundleName: true, "goal-adopted-publication/" + goalIntentName: true, "goal-adopted-publication/" + goalBundleName: true}
	files := map[string][]byte{}
	seen := map[string]bool{}
	var total uint64
	for _, f := range archive.File {
		if f.Name == "" || path.Clean(f.Name) != strings.TrimSuffix(f.Name, "/") || path.IsAbs(f.Name) || strings.Contains(f.Name, "\\") || strings.HasPrefix(f.Name, "../") || f.Mode()&os.ModeSymlink != 0 || seen[f.Name] {
			return nil, errors.New("unsafe or duplicate recovery archive member")
		}
		seen[f.Name] = true
		if f.UncompressedSize64 > 2*goalRecoveryArchiveLimit {
			return nil, errors.New("expanded recovery archive member exceeds bound")
		}
		total += f.UncompressedSize64
		if total > 2*goalRecoveryArchiveLimit {
			return nil, errors.New("expanded recovery archive exceeds bound")
		}
		if !wanted[f.Name] {
			continue
		}
		limit := int64(1 << 20)
		if strings.HasSuffix(f.Name, ".bundle") {
			limit = goalRecoveryArchiveLimit
		}
		if !f.Mode().IsRegular() || f.UncompressedSize64 > uint64(limit) {
			return nil, errors.New("invalid recovery evidence member")
		}
		reader, e := f.Open()
		if e != nil {
			return nil, errors.New("recovery evidence unreadable")
		}
		b, e := io.ReadAll(io.LimitReader(reader, limit+1))
		closeErr := reader.Close()
		if e != nil || closeErr != nil || int64(len(b)) > limit {
			return nil, errors.New("recovery evidence incomplete")
		}
		files[f.Name] = b
	}
	return files, nil
}

type goalRecoveryArchiveWriter struct{ bytes.Buffer }

func (w *goalRecoveryArchiveWriter) Write(p []byte) (int, error) {
	if w.Len()+len(p) > goalRecoveryArchiveLimit {
		return 0, errors.New("recovery archive exceeds bound")
	}
	return w.Buffer.Write(p)
}
func downloadGoalRecoveryArtifact(ctx context.Context, req GoalRecoveryRequest, id int64) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", "api", fmt.Sprintf("repos/%s/actions/artifacts/%d/zip", req.Repository, id))
	cmd.Dir, cmd.Env = req.RepoRoot, goalReadEnvironment()
	var output goalRecoveryArchiveWriter
	cmd.Stdout = &output
	if cmd.Run() != nil {
		return nil, errors.New("authenticated recovery artifact download failed")
	}
	return output.Bytes(), nil
}
