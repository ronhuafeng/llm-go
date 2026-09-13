package protocolupgrade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/generatedproof"
)

// ApplyRequest is the apply command input.
type ApplyRequest struct {
	Baseline          string
	Candidate         string
	StableCandidate   string
	CommonRS          string
	CommonRSSourceSHA string
	CodexRepo         string
	Reports           string
	TargetRef         string
	TargetKind        string
	TargetSHA         string
	ModuleRoot        string
	SkipCodegen       bool
	Now               func() time.Time
	skipSurface       bool
}

// ApplyResult is the machine-readable apply summary.
type ApplyResult struct {
	Status                       string   `json:"status"`
	AddedSchemas                 []string `json:"added_schemas"`
	CommonRSSourceSHA            string   `json:"common_rs_source_sha"`
	SchemaFileCount              int      `json:"schema_file_count"`
	MethodCount                  int      `json:"method_count"`
	CoverageTypeCount            int      `json:"coverage_type_count"`
	CoverageFieldCount           int      `json:"coverage_field_count"`
	ClassifiedSurfaceCount       int      `json:"classified_surface_count"`
	GeneratedCompatibilityImpact string   `json:"generated_compatibility_impact"`
	TargetRef                    string   `json:"target_ref"`
	TargetSHA                    string   `json:"target_sha"`
}

// Apply copies a generated candidate onto the checked-in protocol surface,
// regenerates deterministic metadata/generated Go, and writes only that surface.
func Apply(req ApplyRequest) (ApplyResult, error) {
	if req.Baseline == "" || req.Candidate == "" || req.StableCandidate == "" || req.CommonRS == "" {
		return ApplyResult{}, fmt.Errorf("baseline, candidate, stable-candidate, and common.rs are required")
	}
	if req.TargetRef == "" || req.TargetKind == "" || req.TargetSHA == "" {
		return ApplyResult{}, fmt.Errorf("target ref, kind, and sha are required")
	}
	now := req.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	sourceSHA, err := loadCommonRSSourceSHA(req.CommonRS, req.CommonRSSourceSHA)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := verifyCommonRSProvenance(req.CommonRS, sourceSHA, req.TargetSHA, req.CodexRepo); err != nil {
		return ApplyResult{}, err
	}

	var oldMetadata map[string]any
	if err := loadJSON(filepath.Join(req.Baseline, "baseline_metadata.json"), &oldMetadata); err != nil {
		return ApplyResult{}, err
	}
	var oldManifest manifestFile
	if err := loadJSON(filepath.Join(req.Baseline, "manifest.json"), &oldManifest); err != nil {
		return ApplyResult{}, err
	}
	var oldCoverage coverageFile
	if err := loadJSON(filepath.Join(req.Baseline, "coverage_matrix.json"), &oldCoverage); err != nil {
		return ApplyResult{}, err
	}

	reports := req.Reports
	if reports == "" {
		reports = filepath.Join(filepath.Dir(req.Candidate), "reports")
	}
	codexVersion := ""
	if raw, err := os.ReadFile(filepath.Join(reports, "drift_summary.json")); err == nil {
		var existing Report
		if unmarshalErr := json.Unmarshal(raw, &existing); unmarshalErr == nil {
			if existing.Target.SourceCommit != "" && existing.Target.SourceCommit != req.TargetSHA {
				return ApplyResult{}, fmt.Errorf("candidate source_commit %s does not match target %s", existing.Target.SourceCommit, req.TargetSHA)
			}
			codexVersion = existing.Target.CodexVersion
		}
	}
	if codexVersion == "" {
		codexVersion = "codex-cli " + strings.TrimPrefix(req.TargetRef, "rust-v")
	}

	preflight, err := Compare(CompareRequest{
		Baseline:        req.Baseline,
		Candidate:       req.Candidate,
		SourceCommit:    req.TargetSHA,
		SourceRef:       req.TargetRef,
		SourceRefKind:   req.TargetKind,
		CodexVersion:    codexVersion,
		Generator:       "cargo",
		GeneratorDetail: "codex app-server generate-json-schema --experimental --out internal/protocolschema/appserver/v2",
	})
	if err != nil {
		return ApplyResult{}, err
	}
	added := append([]string(nil), preflight.FileDiff.Added...)
	fieldSeeds := map[string]bool{}
	for _, rel := range preflight.FileDiff.Added {
		fieldSeeds[rel] = true
	}
	for _, rel := range preflight.FileDiff.Changed {
		fieldSeeds[rel] = true
	}

	if err := copyCandidateSchema(req.Candidate, req.Baseline); err != nil {
		return ApplyResult{}, err
	}

	generatedAt := now()
	if oldCommit, _ := oldMetadata["source_commit"].(string); oldCommit == req.TargetSHA {
		if previous, ok := oldMetadata["generated_at"].(string); ok && previous != "" {
			if parsed, err := time.Parse(time.RFC3339, previous); err == nil {
				generatedAt = parsed
			}
		}
	}
	metadata, err := buildMetadata(req.Baseline, oldMetadata, req.TargetRef, req.TargetKind, req.TargetSHA, codexVersion, generatedAt)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := writeJSON(filepath.Join(req.Baseline, "baseline_metadata.json"), metadata); err != nil {
		return ApplyResult{}, err
	}
	if err := updateManifestGeneration(filepath.Join(req.Baseline, "manifest_generation.json"), req.TargetRef, req.TargetKind, req.TargetSHA); err != nil {
		return ApplyResult{}, err
	}
	mappings, err := parseRequestMappings(req.CommonRS)
	if err != nil {
		return ApplyResult{}, err
	}
	manifest, err := buildManifest(req.Baseline, oldManifest, mappings, req.TargetSHA)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := writeJSON(filepath.Join(req.Baseline, "manifest.json"), manifest); err != nil {
		return ApplyResult{}, err
	}
	coverage, err := buildCoverage(req.Baseline, oldCoverage, manifest, fieldSeeds)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := writeJSON(filepath.Join(req.Baseline, "coverage_matrix.json"), coverage); err != nil {
		return ApplyResult{}, err
	}
	if !req.skipSurface {
		surface, err := deriveSurface(req.StableCandidate, req.Baseline)
		if err != nil {
			return ApplyResult{}, err
		}
		updateManifestSurface(&manifest, surface)
		if err := writeJSON(filepath.Join(req.Baseline, "manifest.json"), manifest); err != nil {
			return ApplyResult{}, err
		}
	}
	generatedCompatibility := compatibilityReport(oldManifest, manifest)
	if err := writeAppliedReports(req, generatedCompatibility, codexVersion); err != nil {
		return ApplyResult{}, err
	}
	if !req.SkipCodegen {
		moduleRoot := req.ModuleRoot
		if moduleRoot == "" {
			moduleRoot = "."
		}
		if err := requireModuleBaseline(moduleRoot, req.Baseline); err != nil {
			return ApplyResult{}, err
		}
		if err := generatedproof.WriteArtifacts(moduleRoot); err != nil {
			return ApplyResult{}, err
		}
	}
	files, err := schemaFiles(req.Baseline)
	if err != nil {
		return ApplyResult{}, err
	}
	if added == nil {
		added = []string{}
	}
	impact, _ := generatedCompatibility["compatibility_impact"].(string)
	return ApplyResult{
		Status:                       "ok",
		AddedSchemas:                 added,
		CommonRSSourceSHA:            sourceSHA,
		SchemaFileCount:              len(files),
		MethodCount:                  len(manifest.Entries),
		CoverageTypeCount:            len(coverage.Types),
		CoverageFieldCount:           len(coverage.Fields),
		ClassifiedSurfaceCount:       len(manifest.Surface),
		GeneratedCompatibilityImpact: impact,
		TargetRef:                    req.TargetRef,
		TargetSHA:                    req.TargetSHA,
	}, nil
}

func writeAppliedReports(req ApplyRequest, generatedCompatibility map[string]any, codexVersion string) error {
	reports := req.Reports
	if reports == "" {
		reports = filepath.Join(filepath.Dir(req.Candidate), "reports")
	}
	report, err := Compare(CompareRequest{
		Baseline:        req.Baseline,
		Candidate:       req.Candidate,
		SourceCommit:    req.TargetSHA,
		SourceRef:       req.TargetRef,
		SourceRefKind:   req.TargetKind,
		CodexVersion:    codexVersion,
		Generator:       "cargo",
		GeneratorDetail: "codex app-server generate-json-schema --experimental --out internal/protocolschema/appserver/v2",
	})
	if err != nil {
		return err
	}
	matrix, err := WriteReports(reports, report, req.Candidate)
	if err != nil {
		return err
	}
	type appliedDrift struct {
		Report
		GeneratedCompatibility map[string]any `json:"generated_compatibility"`
	}
	if err := writeJSON(filepath.Join(req.Baseline, "drift_report.json"), appliedDrift{
		Report:                 report,
		GeneratedCompatibility: generatedCompatibility,
	}); err != nil {
		return err
	}
	matrix.GeneratedCompatibilityUpdates = map[string]any{
		"added":        generatedCompatibility["added"],
		"removed":      generatedCompatibility["removed"],
		"reclassified": generatedCompatibility["reclassified"],
		"changed":      generatedCompatibility["changed"],
	}
	return writeJSON(filepath.Join(req.Baseline, MatrixSkeletonName), matrix)
}

func requireModuleBaseline(moduleRoot, baseline string) error {
	moduleRoot, err := filepath.Abs(moduleRoot)
	if err != nil {
		return err
	}
	baseline, err = filepath.Abs(baseline)
	if err != nil {
		return err
	}
	want := filepath.Join(moduleRoot, filepath.FromSlash(defaultBaselineRel))
	if baseline != want {
		return fmt.Errorf("apply codegen requires baseline %s, got %s", want, baseline)
	}
	return nil
}
