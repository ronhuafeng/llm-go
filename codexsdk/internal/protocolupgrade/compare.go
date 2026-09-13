package protocolupgrade

import (
	"fmt"
)

// Target is the selected upstream identity recorded with a comparison.
type Target struct {
	SourceRepo         string `json:"source_repo"`
	SourceRefName      string `json:"source_ref_name"`
	SourceRefKind      string `json:"source_ref_kind"`
	SourceCommit       string `json:"source_commit"`
	CodexVersion       string `json:"codex_version"`
	Generator          string `json:"generator"`
	GeneratorDetail    string `json:"generator_detail"`
	SchemaBundleSHA256 string `json:"schema_bundle_sha256"`
	CanonicalJSONNote  string `json:"canonical_json_note"`
}

// FileDiff is the canonical-JSON schema file delta.
type FileDiff struct {
	Added   []string `json:"added"`
	Changed []string `json:"changed"`
	Removed []string `json:"removed"`
}

func (d FileDiff) empty() bool {
	return len(d.Added) == 0 && len(d.Changed) == 0 && len(d.Removed) == 0
}

// MethodDelta is added/removed methods for one aggregate schema.
type MethodDelta struct {
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
}

func (d MethodDelta) empty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0
}

// Report is the machine-readable compare result.
type Report struct {
	Status               string                 `json:"status"`
	ComparisonMode       string                 `json:"comparison_mode"`
	Target               Target                 `json:"target"`
	FileDiff             FileDiff               `json:"file_diff"`
	MethodDiff           map[string]MethodDelta `json:"method_diff"`
	MatrixUpdateSkeleton string                 `json:"matrix_update_skeleton"`
}

// CompareRequest is the compare command input.
type CompareRequest struct {
	Baseline        string
	Candidate       string
	SourceCommit    string
	SourceRef       string
	SourceRefKind   string
	CodexVersion    string
	Generator       string
	GeneratorDetail string
}

// Compare reports concrete schema drift and does not mutate either directory.
func Compare(req CompareRequest) (Report, error) {
	if req.Baseline == "" {
		return Report{}, fmt.Errorf("baseline directory is required")
	}
	if req.Candidate == "" {
		return Report{}, fmt.Errorf("candidate directory is required")
	}
	beforeBaseline, err := snapshotHashes(req.Baseline)
	if err != nil {
		return Report{}, fmt.Errorf("snapshot baseline: %w", err)
	}
	beforeCandidate, err := snapshotHashes(req.Candidate)
	if err != nil {
		return Report{}, fmt.Errorf("snapshot candidate: %w", err)
	}

	baseHashes, err := schemaHashes(req.Baseline)
	if err != nil {
		return Report{}, fmt.Errorf("hash baseline: %w", err)
	}
	candidateHashes, err := schemaHashes(req.Candidate)
	if err != nil {
		return Report{}, fmt.Errorf("hash candidate: %w", err)
	}
	files := fileDiff(baseHashes, candidateHashes)
	methods, err := aggregateMethodDiff(req.Baseline, req.Candidate)
	if err != nil {
		return Report{}, err
	}
	bundle, err := schemaBundleSHA256(req.Candidate)
	if err != nil {
		return Report{}, err
	}
	status := StatusClean
	if !files.empty() {
		status = StatusReviewRequired
	}
	for _, delta := range methods {
		if !delta.empty() {
			status = StatusReviewRequired
			break
		}
	}
	report := Report{
		Status:         status,
		ComparisonMode: ComparisonCanonicalJSON,
		Target: Target{
			SourceRepo:         sourceRepo,
			SourceRefName:      req.SourceRef,
			SourceRefKind:      req.SourceRefKind,
			SourceCommit:       req.SourceCommit,
			CodexVersion:       req.CodexVersion,
			Generator:          req.Generator,
			GeneratorDetail:    req.GeneratorDetail,
			SchemaBundleSHA256: bundle,
			CanonicalJSONNote:  canonicalJSONNote,
		},
		FileDiff:             files,
		MethodDiff:           methods,
		MatrixUpdateSkeleton: MatrixSkeletonName,
	}
	afterBaseline, err := snapshotHashes(req.Baseline)
	if err != nil {
		return Report{}, err
	}
	afterCandidate, err := snapshotHashes(req.Candidate)
	if err != nil {
		return Report{}, err
	}
	if err := sameSnapshot(beforeBaseline, afterBaseline, "baseline"); err != nil {
		return Report{}, err
	}
	if err := sameSnapshot(beforeCandidate, afterCandidate, "candidate"); err != nil {
		return Report{}, err
	}
	return report, nil
}

func sameSnapshot(before, after map[string]string, label string) error {
	if len(before) != len(after) {
		return fmt.Errorf("%s mutated during compare", label)
	}
	for path, hash := range before {
		if after[path] != hash {
			return fmt.Errorf("%s mutated during compare: %s", label, path)
		}
	}
	return nil
}
