package protocolupgrade

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	Status  string      `json:"status"`
	Preview ApplyResult `json:"preview,omitempty"`
	Issue   *PlanIssue  `json:"issue,omitempty"`
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

var (
	fieldPathRE  = regexp.MustCompile(`field ([^ ]+) has `)
	schemaPathRE = regexp.MustCompile(`([A-Za-z0-9_./-]+\.json#[^ :;]+)`)
)

func wrapIncompatibility(stage string, err error) error {
	if err == nil {
		return nil
	}
	var existing *IncompatibilityError
	if errors.As(err, &existing) {
		return err
	}
	return &IncompatibilityError{
		Stage: stage,
		Path:  incompatibilityPath(err.Error()),
		Err:   err,
	}
}

func incompatibilityPath(message string) string {
	if match := fieldPathRE.FindStringSubmatch(message); len(match) == 2 {
		return match[1]
	}
	if match := schemaPathRE.FindStringSubmatch(message); len(match) == 2 {
		return match[1]
	}
	return ""
}

// Plan proves that Apply can materialize the candidate in an isolated module
// root. It never writes the accepted baseline or candidate.
func Plan(req ApplyRequest) (PlanResult, error) {
	if req.Baseline == "" || req.Candidate == "" {
		return PlanResult{}, fmt.Errorf("baseline and candidate are required")
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

	planned := req
	planned.Baseline = plannedBaseline
	planned.ModuleRoot = tmp
	planned.Reports = filepath.Join(tmp, "reports")
	preview, applyErr := Apply(planned)

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
	return PlanResult{Status: PlanReady, Preview: preview}, nil
}
