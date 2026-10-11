package protocolsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/codexexec"
)

type VerifierActor interface {
	Run(context.Context, string, string) (codexexec.Result, error)
}

// VerifierRepairConfig is constructed by trusted CLI code, never by a proposal.
// Author and reviewer have fresh homes/sessions; the latter is read-only.
type VerifierRepairConfig struct {
	OriginRunID   int64
	OriginAttempt int
	OutputDir     string
	Actor         func(context.Context, string, bool) (VerifierActor, func(), error)
	// Proof is a deterministic owner-local test seam, not a supplied command.
	Proof func(context.Context, verifierProofRequest) (verifierProofEvidence, error)
}

func verifierJSON(path string, value any) error {
	b, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, append(b, '\n'), 0600)
}
func verifierRead(path string, limit int64) ([]byte, error) {
	info, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("invalid verifier artifact file")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(b)) > limit {
		return nil, errors.New("verifier artifact exceeds bound")
	}
	return b, e
}
func verifierDecode(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return errors.New("trailing verifier JSON")
	}
	return nil
}

// proposeVerifier never changes the running oracle or applies protocol output.
// It can only produce a separately reviewed publication artifact.
func proposeVerifier(ctx context.Context, req RepairRequest, cp *repairCheckpoint, proposal []byte) (bool, error) {
	cfg := req.Verifier
	if cfg == nil || req.VerifierHandoffs != 0 || cp.Round >= req.MaxRounds {
		return false, nil
	}
	if cfg.Actor == nil || cfg.OriginRunID <= 0 || cfg.OriginAttempt < 1 || cfg.OutputDir == "" {
		return false, errors.New("incomplete trusted verifier repair configuration")
	}
	cp.Round++ // Charge before starting, including empty/failed/cancelled attempts.
	cp.Stage = "verifier_proposal"
	if e := saveRepairCheckpoint(req.CheckpointDir, req.ResumeRequest, *cp, proposal); e != nil {
		return false, e
	}
	work, e := os.MkdirTemp("", "verifier-proposal-")
	if e != nil {
		return false, e
	}
	defer os.RemoveAll(work)
	accepted, author := filepath.Join(work, "accepted"), filepath.Join(work, "author")
	for _, root := range []string{accepted, author} {
		cmd := exec.CommandContext(ctx, "git", "clone", "--quiet", "--no-hardlinks", req.RepoRoot, root)
		if out, e := boundedCommandOutput(cmd); e != nil {
			return false, fmt.Errorf("clone verifier workspace: %w: %s", e, boundedRepairDiagnostic(string(out)))
		}
	}
	if e := os.MkdirAll(filepath.Join(author, "codexsdk/.cache/verifier-proof"), 0700); e != nil {
		return false, e
	}
	upgradePatch := filepath.Join(work, "upgrade.patch")
	if e := os.WriteFile(upgradePatch, proposal, 0400); e != nil {
		return false, e
	}
	actor, closeActor, e := cfg.Actor(ctx, author, false)
	if e != nil {
		return false, e
	}
	defer closeActor()
	prompt := fmt.Sprintf(`You are the independent verifier-repair role. Diagnose the concrete failure against immutable upstream source and already accepted public contracts, not the upgrade author's claims.
Accepted source: %s. Candidate source: %s. Upgrade proposal (read only): %s.
Failure: %s
If this is an implementation defect, leave the verifier unchanged and explain the actual counterexample for the upgrade role. Unknown evidence is not permission to relax a rule.
If the existing verifier demonstrably misstates an accepted contract, propose the smallest repair only within codexsdk/internal/schemaoracle/*.go and its testdata/verifier-proof Go fixtures. Do not alter controller, workflows, public API contracts, source bytes, dependencies or credentials. Leave changes uncommitted.
Write bounded proof cases to codexsdk/.cache/verifier-proof/cases.json. Each case has JSON keys name, source (candidate or baseline), schema (relative source path), fragment, binding, good (tracked fixture Go path), counterexamples (mutant fixture path, input JSON string, accept boolean, canonical JSON string when accepted). Good and mutant fixtures must implement the named public binding without TestMain or test entry points. The fixed runner, not your tests, executes old-good red/new-good green/new-mutant red and independently replays each challenge. Every negative fixture must actually violate its claimed challenge.
Add focused permanent regressions and cite exact accepted contract locations. A separate fresh reviewer and all normal checks must approve before your verifier becomes authority. Do not fix the upgrade proposal or broaden unrelated source constraints.`, accepted, req.CandidateDir, upgradePatch, cp.Diagnostic)
	var execution codexexec.Result
	var patch, casesBytes []byte
	var evidence verifierProofEvidence
	session := ""
	retryAuthor := func(cause error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if cp.Round >= req.MaxRounds {
			return fmt.Errorf("verifier author budget exhausted: %w", cause)
		}
		cp.Round++
		cp.Diagnostic = boundedRepairDiagnostic(cause.Error())
		prompt = "Independent fixed verification rejected your verifier proposal. Repair the precise failure in the same permitted scope; do not change authority or weaken fixtures. Feedback: " + cp.Diagnostic
		return saveRepairCheckpoint(req.CheckpointDir, req.ResumeRequest, *cp, proposal)
	}
	for {
		execution, e = actor.Run(ctx, prompt, session)
		if e != nil {
			return false, e
		}
		if execution.Status != "completed" || execution.SessionID == "" || (session != "" && execution.SessionID != session) {
			return false, errors.New("verifier author did not complete identified session")
		}
		session = execution.SessionID
		if e := CheckScope(author, "verifier"); e != nil {
			return false, e
		}
		paths, e := ChangedPaths(author)
		if e != nil {
			return false, e
		}
		if len(paths) == 0 {
			if b, e := verifierRead(execution.FinalMessagePath, 8192); e == nil {
				cp.Diagnostic = boundedRepairDiagnostic(cp.Diagnostic + "\nIndependent verifier analysis: " + string(b))
			}
			return false, nil
		}
		var gofiles []string
		for _, p := range paths {
			if _, e := os.Stat(filepath.Join(author, p)); e == nil {
				gofiles = append(gofiles, filepath.Join(author, p))
			}
		}
		if len(gofiles) > 0 {
			cmd := exec.CommandContext(ctx, "gofmt", append([]string{"-w"}, gofiles...)...)
			if out, e := boundedCommandOutput(cmd); e != nil {
				if e := retryAuthor(fmt.Errorf("format verifier: %w: %s", e, boundedRepairDiagnostic(string(out)))); e != nil {
					return false, e
				}
				continue
			}
		}
		patch, e = repairPatch(author)
		if e != nil {
			return false, e
		}
		casesBytes, e = verifierRead(filepath.Join(author, "codexsdk/.cache/verifier-proof/cases.json"), 1<<20)
		if os.IsNotExist(e) {
			if e := retryAuthor(errors.New("required cases.json is missing")); e != nil {
				return false, e
			}
			continue
		}
		if e != nil {
			return false, e
		}
		var cases []verifierProofCase
		if e := verifierDecode(casesBytes, &cases); e != nil {
			if e := retryAuthor(e); e != nil {
				return false, e
			}
			continue
		}
		casesBytes, e = json.Marshal(cases)
		if e != nil {
			return false, e
		}
		proof := cfg.Proof
		if proof == nil {
			proof = runVerifierProposalProof
		}
		evidence, e = proof(ctx, verifierProofRequest{AcceptedRoot: accepted, CandidateDir: req.CandidateDir, CandidateSHA256: req.CandidateSHA256, ProposalSHA256: repairDigest(proposal), Patch: patch, Cases: cases})
		if e != nil {
			var feedback *verifierFeedbackError
			if !errors.As(e, &feedback) {
				return false, e
			}
			if e := retryAuthor(e); e != nil {
				return false, e
			}
			continue
		}
		if !evidence.Passed {
			return false, errors.New("verifier proof did not pass")
		}
		break
	}
	// Stage only after fixed proof; the tree binds review to every published byte.
	if e := CheckScope(author, "verifier"); e != nil {
		return false, e
	}
	if e := runGit(author, "add", "--all"); e != nil {
		return false, e
	}
	tree, e := gitOutput(author, "write-tree")
	if e != nil {
		return false, e
	}
	tree = strings.TrimSpace(tree)
	proofBytes, e := json.Marshal(evidence)
	if e != nil {
		return false, e
	}
	reviewInput := filepath.Join(work, "proof.json")
	if e := os.WriteFile(reviewInput, proofBytes, 0400); e != nil {
		return false, e
	}
	reviewer, closeReview, e := cfg.Actor(ctx, author, true)
	if e != nil {
		return false, e
	}
	defer closeReview()
	reviewPrompt := fmt.Sprintf(`Independently review this verifier-only proposal; do not write files. Read the accepted code/contracts at %s, immutable source %s, actual fixed-runner evidence %s, the proposal diff and its good/mutant fixtures. Reject unsupported contract changes, erased assertions, weak or unrelated negative examples, self-certification or unsafe source scope. Confirm old-good false rejection, new-good acceptance and actual defective-implementation rejection; executed evidence is required but alone cannot establish semantic correctness. Review exact tree %s and proof digest %s. Return ONLY JSON {"approved":boolean,"proof_sha256":string,"reason":string}. Unknown or insufficient evidence means approved:false.`, accepted, req.CandidateDir, reviewInput, tree, repairDigest(proofBytes))
	reviewExecution, e := reviewer.Run(ctx, reviewPrompt, "")
	if e != nil {
		return false, e
	}
	if reviewExecution.Status != "completed" || reviewExecution.SessionID == "" || reviewExecution.SessionID == execution.SessionID {
		return false, errors.New("independent verifier review session missing")
	}
	reviewBytes, e := verifierRead(reviewExecution.FinalMessagePath, 16384)
	if e != nil {
		return false, e
	}
	var verdict struct {
		Approved    bool   `json:"approved"`
		ProofSHA256 string `json:"proof_sha256"`
		Reason      string `json:"reason"`
	}
	if e := verifierDecode(reviewBytes, &verdict); e != nil {
		return false, e
	}
	if !verdict.Approved || verdict.ProofSHA256 != repairDigest(proofBytes) {
		return false, fmt.Errorf("independent verifier review blocked: %s", boundedRepairDiagnostic(verdict.Reason))
	}
	after, e := repairPatch(author)
	if e != nil {
		return false, e
	}
	if repairDigest(after) != repairDigest(patch) {
		return false, errors.New("verifier proposal changed during proof/review")
	}
	if e := CheckScope(author, "verifier"); e != nil {
		return false, e
	}
	review := VerifierReviewEvidence{Approved: true, AuthorSessionID: execution.SessionID, ReviewerSessionID: reviewExecution.SessionID, CandidateSHA256: req.CandidateSHA256, ProposalSHA256: repairDigest(proposal), VerifierPatchSHA256: repairDigest(patch), ProofSHA256: repairDigest(proofBytes), ReviewedTree: tree, Reason: boundedRepairDiagnostic(verdict.Reason)}
	reviewJSON, e := json.Marshal(review)
	if e != nil {
		return false, e
	}
	h := VerifierHandoff{Version: 1, DeadlineUTC: cp.DeadlineUTC, OriginRunID: cfg.OriginRunID, OriginAttempt: cfg.OriginAttempt, BaseSHA: cp.Base, Target: cp.Target, CandidateSHA256: req.CandidateSHA256, ProposalSHA256: repairDigest(proposal), VerifierPatchSHA256: repairDigest(patch), ProofSHA256: repairDigest(proofBytes), ReviewSHA256: repairDigest(reviewJSON), ReviewedTree: tree, RoundsUsed: cp.Round, MaxRounds: req.MaxRounds, Handoffs: 1}
	if e := ValidateVerifierEvidence(h, patch, proofBytes, reviewJSON, casesBytes); e != nil {
		return false, e
	}
	artifactAbs, e := filepath.Abs(cfg.OutputDir)
	if e != nil {
		return false, e
	}
	repoAbs, e := filepath.Abs(req.RepoRoot)
	if e != nil {
		return false, e
	}
	if rel, e := filepath.Rel(repoAbs, artifactAbs); e != nil || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return false, errors.New("verifier artifact must be outside upgrade workspace")
	}
	if e := os.Mkdir(cfg.OutputDir, 0700); e != nil {
		return false, e
	}
	for name, data := range map[string][]byte{"verifier.patch": patch, "proof.json": proofBytes, "review.json": reviewJSON, "cases.json": casesBytes} {
		if e := os.WriteFile(filepath.Join(cfg.OutputDir, name), data, 0600); e != nil {
			return false, e
		}
	}
	if e := verifierJSON(filepath.Join(cfg.OutputDir, "handoff.json"), h); e != nil {
		return false, e
	}
	cp.VerifierHandoffs = 1
	cp.Status = "verifier_proposed"
	cp.Diagnostic = "independently reviewed verifier proposal requires its own accepted PR before continuing"
	return true, nil
}
