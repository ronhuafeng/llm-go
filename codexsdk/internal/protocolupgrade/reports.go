package protocolupgrade

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MatrixUpdate is the review skeleton written next to a drift report.
type MatrixUpdate struct {
	Status                        string           `json:"status"`
	Source                        string           `json:"source"`
	ValidStatuses                 []string         `json:"valid_statuses"`
	MethodUpdates                 []map[string]any `json:"method_updates"`
	TypeUpdates                   []map[string]any `json:"type_updates"`
	FieldUpdates                  []any            `json:"field_updates"`
	GeneratedCompatibilityUpdates any              `json:"generated_compatibility_updates,omitempty"`
}

var validCoverageStatuses = []string{
	"supported",
	"supported-generated",
	"deferred",
	"intentionally-unsupported",
}

// WriteReports writes drift_summary.json, matrix_update_skeleton.json, and SUMMARY.md.
func WriteReports(dir string, report Report, generatedSchema string) (MatrixUpdate, error) {
	if dir == "" {
		return MatrixUpdate{}, fmt.Errorf("reports directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return MatrixUpdate{}, err
	}
	matrix := matrixFromReport(report)
	if err := writeJSON(filepath.Join(dir, "drift_summary.json"), report); err != nil {
		return MatrixUpdate{}, err
	}
	if err := writeJSON(filepath.Join(dir, MatrixSkeletonName), matrix); err != nil {
		return MatrixUpdate{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "SUMMARY.md"), []byte(summaryMarkdown(generatedSchema, dir, report)), 0o644); err != nil {
		return MatrixUpdate{}, err
	}
	return matrix, nil
}

func matrixFromReport(report Report) MatrixUpdate {
	status := "empty"
	if report.Status != StatusClean {
		status = StatusReviewRequired
	}
	methodUpdates := []map[string]any{}
	for _, schema := range aggregateSchemas {
		delta := report.MethodDiff[schema]
		for _, method := range delta.Added {
			methodUpdates = append(methodUpdates, map[string]any{
				"method":        method,
				"source_schema": schema,
				"change":        "added",
				"status":        StatusReviewRequired,
			})
		}
		for _, method := range delta.Removed {
			methodUpdates = append(methodUpdates, map[string]any{
				"method":        method,
				"source_schema": schema,
				"change":        "removed",
				"status":        StatusReviewRequired,
			})
		}
	}
	typeUpdates := []map[string]any{}
	for _, path := range report.FileDiff.Added {
		typeUpdates = append(typeUpdates, map[string]any{"schema": path, "change": "added", "status": StatusReviewRequired})
	}
	for _, path := range report.FileDiff.Changed {
		typeUpdates = append(typeUpdates, map[string]any{"schema": path, "change": "changed", "status": StatusReviewRequired})
	}
	for _, path := range report.FileDiff.Removed {
		typeUpdates = append(typeUpdates, map[string]any{"schema": path, "change": "removed", "status": StatusReviewRequired})
	}
	return MatrixUpdate{
		Status:        status,
		Source:        "drift_summary.json",
		ValidStatuses: append([]string(nil), validCoverageStatuses...),
		MethodUpdates: methodUpdates,
		TypeUpdates:   typeUpdates,
		FieldUpdates:  []any{},
	}
}

func summaryMarkdown(generatedSchema, reportsDir string, report Report) string {
	return strings.Join([]string{
		"# Codex SDK Upstream Tracking",
		"",
		fmt.Sprintf("- status: `%s`", report.Status),
		fmt.Sprintf("- source repo: `%s`", report.Target.SourceRepo),
		fmt.Sprintf("- source ref: `%s`", report.Target.SourceRefName),
		fmt.Sprintf("- source ref kind: `%s`", report.Target.SourceRefKind),
		fmt.Sprintf("- source commit: `%s`", report.Target.SourceCommit),
		fmt.Sprintf("- codex version: `%s`", report.Target.CodexVersion),
		fmt.Sprintf("- generated schema: `%s`", generatedSchema),
		fmt.Sprintf("- drift summary: `%s`", filepath.Join(reportsDir, "drift_summary.json")),
		fmt.Sprintf("- matrix update skeleton: `%s`", filepath.Join(reportsDir, MatrixSkeletonName)),
		"",
		"Review the generated reports before updating the checked-in baseline.",
		"",
	}, "\n")
}

func DiffCountSummary(report Report) string {
	methodDeltas := 0
	for _, delta := range report.MethodDiff {
		methodDeltas += len(delta.Added) + len(delta.Removed)
	}
	return fmt.Sprintf(
		"status=%s files_added=%d files_changed=%d files_removed=%d method_deltas=%d",
		report.Status,
		len(report.FileDiff.Added),
		len(report.FileDiff.Changed),
		len(report.FileDiff.Removed),
		methodDeltas,
	)
}

func writeJSON(path string, value any) error {
	raw, err := marshalJSON(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func marshalJSON(value any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
