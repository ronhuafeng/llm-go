package protocolupgrade

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	PlanReady              = "ready"
	PlanSemanticUnresolved = "semantic_unresolved"
)

// PlanIssue is concrete deterministic evidence that requires a handwritten
// generator/semantic change before the candidate can be applied.
type PlanIssue struct {
	Stage  string `json:"stage"`
	Path   string `json:"path,omitempty"`
	Reason string `json:"reason"`
}

// PlanResult describes whether a candidate can be applied without mutating the
// accepted module worktree.
type PlanResult struct {
	Status   string      `json:"status"`
	Preview  ApplyResult `json:"preview,omitempty"`
	Issue    *PlanIssue  `json:"issue,omitempty"`
	prepared *preparedCandidate
}

// IncompatibilityError marks a deterministic schema/generator incompatibility.
// It is distinct from provenance, filesystem, or target-integrity failures.
type IncompatibilityError struct {
	Stage string
	Path  string
	Err   error
}

func (e *IncompatibilityError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("%s incompatibility at %s: %v", e.Stage, e.Path, e.Err)
	}
	return fmt.Sprintf("%s incompatibility: %v", e.Stage, e.Err)
}

func (e *IncompatibilityError) Unwrap() error { return e.Err }

// Plan constructs the candidate in isolation and retains its materializable
// bytes. It never writes the accepted baseline or candidate.
func Plan(req ApplyRequest) (PlanResult, error) {
	return planCandidate(req, false)
}

// VerifyExact rebuilds every semantic protocol artifact from the exact target
// in isolation and compares it with the accepted baseline and generated Go.
// Only baseline_metadata.generated_at is excluded as observation time.
func VerifyExact(req ApplyRequest) (PlanResult, error) {
	if req.ModuleRoot == "" {
		return PlanResult{}, fmt.Errorf("exact verification requires module root, surface derivation, and generated Go")
	}
	return planCandidate(req, true)
}

func planCandidate(req ApplyRequest, verifyExact bool) (PlanResult, error) {
	if req.Baseline == "" || req.Candidate == "" {
		return PlanResult{}, fmt.Errorf("baseline and candidate are required")
	}
	if req.ModuleRoot == "" {
		req.ModuleRoot = filepath.Clean(filepath.Join(req.Baseline, "../../../.."))
	}
	if err := requireModuleBaseline(req.ModuleRoot, req.Baseline); err != nil {
		return PlanResult{}, err
	}
	beforeBaseline, err := snapshotHashes(req.Baseline)
	if err != nil {
		return PlanResult{}, fmt.Errorf("snapshot baseline before plan: %w", err)
	}
	beforeCandidate, err := snapshotHashes(req.Candidate)
	if err != nil {
		return PlanResult{}, fmt.Errorf("snapshot candidate before plan: %w", err)
	}

	tmp, err := os.MkdirTemp("", "protocolupgrade-plan-")
	if err != nil {
		return PlanResult{}, err
	}
	defer os.RemoveAll(tmp)

	plannedBaseline := filepath.Join(tmp, filepath.FromSlash(defaultBaselineRel))
	if err := copyTreeFiles(req.Baseline, plannedBaseline); err != nil {
		return PlanResult{}, fmt.Errorf("copy baseline for plan: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "protocolv2"), 0o755); err != nil {
		return PlanResult{}, err
	}
	if req.ModuleRoot != "" {
		if err := copyHandwrittenProtocolPackage(req.ModuleRoot, tmp); err != nil {
			return PlanResult{}, err
		}
		if err := copyHandwrittenProtocolPackage(filepath.Join(req.ModuleRoot, "protocolv2"), filepath.Join(tmp, "protocolv2")); err != nil {
			return PlanResult{}, fmt.Errorf("copy handwritten protocol package for plan: %w", err)
		}
	}

	planned := req
	planned.Baseline = plannedBaseline
	planned.ModuleRoot = tmp
	planned.Reports = filepath.Join(tmp, "reports")
	preview, applyErr := constructCandidate(planned)

	afterBaseline, err := snapshotHashes(req.Baseline)
	if err != nil {
		return PlanResult{}, err
	}
	afterCandidate, err := snapshotHashes(req.Candidate)
	if err != nil {
		return PlanResult{}, err
	}
	if err := sameSnapshot(beforeBaseline, afterBaseline, "accepted baseline during plan"); err != nil {
		return PlanResult{}, err
	}
	if err := sameSnapshot(beforeCandidate, afterCandidate, "candidate during plan"); err != nil {
		return PlanResult{}, err
	}

	if applyErr != nil {
		if verifyExact {
			return PlanResult{}, fmt.Errorf("cannot reconstruct exact accepted baseline: %w", applyErr)
		}
		var incompatibility *IncompatibilityError
		if errors.As(applyErr, &incompatibility) {
			return PlanResult{
				Status: PlanSemanticUnresolved,
				Issue: &PlanIssue{
					Stage:  incompatibility.Stage,
					Path:   incompatibility.Path,
					Reason: incompatibility.Err.Error(),
				},
			}, nil
		}
		return PlanResult{}, applyErr
	}
	if verifyExact {
		if err := compareExactBaseline(req.Baseline, plannedBaseline, req.ModuleRoot, tmp); err != nil {
			return PlanResult{}, fmt.Errorf("exact baseline verification: %w", err)
		}
	}
	prepared, err := prepareCandidateWrites(req, tmp, planned.Reports, beforeBaseline)
	if err != nil {
		return PlanResult{}, err
	}
	return PlanResult{Status: PlanReady, Preview: preview, prepared: prepared}, nil
}

func copyHandwrittenProtocolPackage(source, destination string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || filepath.Ext(name) != ".go" ||
			strings.HasSuffix(name, ".gen.go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if err := copyFileBytes(filepath.Join(source, name), filepath.Join(destination, name)); err != nil {
			return err
		}
	}
	return nil
}
