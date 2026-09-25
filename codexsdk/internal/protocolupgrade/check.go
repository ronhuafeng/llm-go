package protocolupgrade

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/generatedcheck"
)

const defaultBaselineRel = "internal/protocolschema/appserver/v2"

// CheckRequest is the check command input.
type CheckRequest struct {
	ModuleRoot string
	Baseline   string
	Candidate  string
}

// Artifact is one generated file compared by Check.
type Artifact struct {
	Path         string `json:"path"`
	Reproducible bool   `json:"reproducible"`
	Diagnostic   string `json:"diagnostic,omitempty"`
}

// CheckResult is the machine-readable check result.
type CheckResult struct {
	Clean     bool       `json:"clean"`
	FileDiff  FileDiff   `json:"file_diff,omitempty"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
}

// Check verifies candidate/baseline schema consistency and generated-source
// reproducibility. It does not observe workflow, run, or tree identity.
func Check(req CheckRequest) (CheckResult, error) {
	root := req.ModuleRoot
	if root == "" {
		root = "."
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return CheckResult{}, err
	}
	baseline := req.Baseline
	if baseline == "" {
		baseline = filepath.Join(root, filepath.FromSlash(defaultBaselineRel))
	}
	result := CheckResult{
		Clean:    true,
		FileDiff: FileDiff{Added: []string{}, Changed: []string{}, Removed: []string{}},
	}
	if req.Candidate != "" {
		baseHashes, err := schemaHashes(baseline)
		if err != nil {
			return CheckResult{}, fmt.Errorf("hash baseline: %w", err)
		}
		candidateHashes, err := schemaHashes(req.Candidate)
		if err != nil {
			return CheckResult{}, fmt.Errorf("hash candidate: %w", err)
		}
		result.FileDiff = fileDiff(baseHashes, candidateHashes)
		if !result.FileDiff.empty() {
			result.Clean = false
			return result, fmt.Errorf("candidate does not match checked-in baseline:\n%s", fileDiffDiagnostic(result.FileDiff))
		}
	}

	generated, err := generatedcheck.RegeneratedFiles(root)
	if err != nil {
		return result, err
	}
	allMatch := true
	for _, rel := range generatedProtocolArtifacts {
		want, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return result, err
		}
		got := generated[rel]
		match := bytes.Equal(want, got)
		artifact := Artifact{Path: rel, Reproducible: match}
		if !match {
			allMatch = false
			artifact.Diagnostic = fmt.Sprintf("%s mismatch: checked-in sha256=%x generated sha256=%x", rel, sha256.Sum256(want), sha256.Sum256(got))
		}
		result.Artifacts = append(result.Artifacts, artifact)
	}
	if !allMatch {
		result.Clean = false
		return result, fmt.Errorf("generated artifacts do not match checked-in outputs: %s", artifactSummary(result.Artifacts))
	}
	return result, nil
}

func fileDiffDiagnostic(diff FileDiff) string {
	var lines []string
	for _, path := range diff.Added {
		lines = append(lines, "added "+path)
	}
	for _, path := range diff.Changed {
		lines = append(lines, "changed "+path)
	}
	for _, path := range diff.Removed {
		lines = append(lines, "removed "+path)
	}
	return strings.Join(lines, "\n")
}

func artifactSummary(artifacts []Artifact) string {
	var parts []string
	for _, artifact := range artifacts {
		if !artifact.Reproducible && artifact.Diagnostic != "" {
			parts = append(parts, artifact.Diagnostic)
		}
	}
	if len(parts) == 0 {
		return "one or more artifacts differ"
	}
	return strings.Join(parts, "; ")
}
