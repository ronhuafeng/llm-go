package protocolsync

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func saveRepairCheckpoint(dir string, req ResumeRequest, cp repairCheckpoint, patch []byte) error {
	if info, e := os.Lstat(dir); e == nil && !info.IsDir() {
		return errors.New("checkpoint is not a directory")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	candidate := filepath.Join(dir, "candidate")
	if _, err := os.Lstat(candidate); os.IsNotExist(err) {
		copied, cleanup, e := copyVerifiedCandidate(req.CandidateDir, req.CandidateSHA256, req.TargetRef, req.TargetKind, req.TargetSHA)
		if e != nil {
			return e
		}
		defer cleanup()
		// Both paths are runner-local; retain immutable bytes, never Codex home.
		if e = os.Rename(copied, candidate); e != nil {
			return e
		}
	}
	if err := VerifyCandidate(candidate, cp.CandidateSHA256, cp.Target.RefName, cp.Target.RefKind, cp.Target.PeeledCommitSHA); err != nil {
		return err
	}
	cp.ProposalSHA256 = repairDigest(patch)
	if cp.Feedback.File != "" {
		if _, err := feedbackPath(dir, cp.Feedback, cp, patch); err != nil {
			return err
		}
	}
	if err := atomicRepairWrite(filepath.Join(dir, "proposal.patch"), patch); err != nil {
		return err
	}
	if err := retainFeedback(dir, cp.Feedback); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}
	return atomicRepairWrite(filepath.Join(dir, "checkpoint.json"), data)
}
func atomicRepairWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "checkpoint-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RestoreRepair consumes a checkpoint only after the workflow has verified its
// native run/artifact origin. The artifact is a cache, not accepted state: fresh
// local HEAD, remote main, upstream ref and all bytes must agree before replay.
func RestoreRepair(root, dir, remote, baseBranch string, lookup RemoteLookuper, publication *PendingRequest) (SyncResult, error) {
	result := SyncResult{Stage: "recovery"}
	if err := AssertClean(root); err != nil {
		return result, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "checkpoint.json"))
	if err != nil {
		return result, err
	}
	var cp repairCheckpoint
	if err = json.Unmarshal(raw, &cp); err != nil {
		return result, err
	}
	if cp.Version != 1 || !shaRE.MatchString(cp.Base) || cp.Status == "verified" {
		return result, errors.New("checkpoint is not a recoverable repair")
	}
	base, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(base) != cp.Base {
		return result, errors.New("checkpoint base is stale; reconstruct from current main")
	}
	current, err := remotePublicationHead(root, remote, baseBranch)
	if err != nil {
		return result, err
	}
	if current != cp.Base {
		return result, errors.New("remote base changed; checkpoint is stale")
	}
	target, err := ResolveUpstream(ResolveRequest{Remote: defaultRemote, UpstreamRef: cp.Target.RefName, Lookuper: lookup})
	if err != nil {
		return result, err
	}
	if target.RefKind != cp.Target.RefKind || target.PeeledCommitSHA != cp.Target.PeeledCommitSHA {
		return result, errors.New("checkpoint upstream target changed")
	}
	candidate := filepath.Join(dir, "candidate")
	if err = VerifyCandidate(candidate, cp.CandidateSHA256, target.RefName, target.RefKind, target.PeeledCommitSHA); err != nil {
		return result, err
	}
	var pending *PendingPublication
	if publication != nil {
		request := *publication
		request.RepoRoot = root
		request.BaseSHA = cp.Base
		request.Target = target
		request.ReadChecks = true
		observed, e := InspectPending(request)
		if e != nil {
			return result, e
		}
		pending = &observed
	}
	patch, err := os.ReadFile(filepath.Join(dir, "proposal.patch"))
	if err != nil {
		return result, err
	}
	if repairDigest(patch) != cp.ProposalSHA256 {
		return result, errors.New("checkpoint proposal digest mismatch")
	}
	if cp.Feedback.File != "" {
		if _, err := feedbackPath(dir, cp.Feedback, cp, patch); err != nil {
			return result, err
		}
	}
	// Validate paths in a disposable clone before applying any bytes to the real
	// workspace; a recovery artifact cannot enlarge the repair scope.
	temp, err := os.MkdirTemp("", "protocol-recovery-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(temp)
	if err = runGit(root, "clone", "--quiet", "--no-hardlinks", root, temp); err != nil {
		return result, err
	}
	if err = applyProposalPatchBytes(temp, patch); err != nil {
		return result, err
	}
	if err = CheckScope(temp, "agent"); err != nil {
		return result, err
	}
	if err = applyProposalPatchBytes(root, patch); err != nil {
		return result, err
	}
	return SyncResult{Publication: pending, Outcome: OutcomeSemanticUnresolved, Stage: "recovery", Target: target, Candidate: filepath.Join(candidate, "schema"), CandidateDir: candidate, CandidateSHA256: cp.CandidateSHA256, Reason: fmt.Sprintf("restored bounded %s checkpoint; no acceptance proof reused", cp.Status)}, nil
}
