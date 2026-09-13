package protocolupgrade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareCleanDoesNotMutateAndIgnoresMetadata(t *testing.T) {
	root := t.TempDir()
	baseline := filepath.Join(root, "baseline")
	candidate := filepath.Join(root, "candidate")
	writeSchemaSet(t, baseline, []string{"thread/start"}, nil)
	writeSchemaSet(t, candidate, []string{"thread/start"}, nil)
	writeJSONFile(t, filepath.Join(candidate, "baseline_metadata.json"), map[string]string{"ignored": "different"})
	beforeBaseline, err := snapshotHashes(baseline)
	if err != nil {
		t.Fatal(err)
	}
	beforeCandidate, err := snapshotHashes(candidate)
	if err != nil {
		t.Fatal(err)
	}

	report, err := Compare(CompareRequest{
		Baseline:        baseline,
		Candidate:       candidate,
		SourceCommit:    strings.Repeat("1", 40),
		SourceRef:       "rust-v0.141.0",
		SourceRefKind:   "stable_rust_tag",
		CodexVersion:    "codex-cli 0.141.0",
		Generator:       "cargo",
		GeneratorDetail: "cargo run",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusClean {
		t.Fatalf("status = %q, want %s", report.Status, StatusClean)
	}
	if !report.FileDiff.empty() {
		t.Fatalf("file diff = %#v, want empty", report.FileDiff)
	}
	afterBaseline, err := snapshotHashes(baseline)
	if err != nil {
		t.Fatal(err)
	}
	afterCandidate, err := snapshotHashes(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := sameSnapshot(beforeBaseline, afterBaseline, "baseline"); err != nil {
		t.Fatal(err)
	}
	if err := sameSnapshot(beforeCandidate, afterCandidate, "candidate"); err != nil {
		t.Fatal(err)
	}
}

func TestCompareReportsAddedChangedRemovedFiles(t *testing.T) {
	root := t.TempDir()
	baseline := filepath.Join(root, "baseline")
	candidate := filepath.Join(root, "candidate")
	writeSchemaSet(t, baseline, nil, map[string]any{"Removed.json": map[string]any{"type": "string"}})
	writeSchemaSet(t, candidate, nil, map[string]any{"Added.json": map[string]any{"type": "string"}})
	writeJSONFile(t, filepath.Join(candidate, "Shared.json"), map[string]any{
		"type":       "object",
		"properties": map[string]any{"value": map[string]any{"type": "number"}},
	})

	report, err := Compare(CompareRequest{
		Baseline:        baseline,
		Candidate:       candidate,
		SourceCommit:    strings.Repeat("1", 40),
		SourceRef:       "manual",
		SourceRefKind:   "manual_ref",
		CodexVersion:    "codex-cli test",
		Generator:       "compare-only",
		GeneratorDetail: "candidate schema",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusReviewRequired {
		t.Fatalf("status = %q, want %s", report.Status, StatusReviewRequired)
	}
	if got, want := report.FileDiff.Added, []string{"Added.json"}; !stringSlicesEqual(got, want) {
		t.Fatalf("added = %#v, want %#v", got, want)
	}
	if got, want := report.FileDiff.Changed, []string{"Shared.json"}; !stringSlicesEqual(got, want) {
		t.Fatalf("changed = %#v, want %#v", got, want)
	}
	if got, want := report.FileDiff.Removed, []string{"Removed.json"}; !stringSlicesEqual(got, want) {
		t.Fatalf("removed = %#v, want %#v", got, want)
	}
}

func TestCompareReportsAddedAndRemovedMethods(t *testing.T) {
	root := t.TempDir()
	baseline := filepath.Join(root, "baseline")
	candidate := filepath.Join(root, "candidate")
	writeSchemaSet(t, baseline, []string{"thread/start"}, nil)
	writeSchemaSet(t, candidate, []string{"thread/resume"}, nil)

	report, err := Compare(CompareRequest{
		Baseline:        baseline,
		Candidate:       candidate,
		SourceCommit:    strings.Repeat("1", 40),
		SourceRef:       "manual",
		SourceRefKind:   "manual_ref",
		CodexVersion:    "codex-cli test",
		Generator:       "compare-only",
		GeneratorDetail: "candidate schema",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusReviewRequired {
		t.Fatalf("status = %q, want %s", report.Status, StatusReviewRequired)
	}
	delta := report.MethodDiff["ClientRequest.json"]
	if got, want := delta.Added, []string{"thread/resume"}; !stringSlicesEqual(got, want) {
		t.Fatalf("methods added = %#v, want %#v", got, want)
	}
	if got, want := delta.Removed, []string{"thread/start"}; !stringSlicesEqual(got, want) {
		t.Fatalf("methods removed = %#v, want %#v", got, want)
	}

	reports := filepath.Join(root, "reports")
	matrix, err := WriteReports(reports, report)
	if err != nil {
		t.Fatal(err)
	}
	if matrix.Status != StatusReviewRequired {
		t.Fatalf("matrix status = %q", matrix.Status)
	}
	raw, err := os.ReadFile(filepath.Join(reports, "drift_summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var loaded Report
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.Status != StatusReviewRequired {
		t.Fatalf("written status = %q", loaded.Status)
	}
}

func writeSchemaSet(t *testing.T, root string, clientMethods []string, extra map[string]any) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, filepath.Join(root, "ClientRequest.json"), aggregateSchema(clientMethods))
	writeJSONFile(t, filepath.Join(root, "ServerRequest.json"), aggregateSchema(nil))
	writeJSONFile(t, filepath.Join(root, "ServerNotification.json"), aggregateSchema(nil))
	writeJSONFile(t, filepath.Join(root, "ClientNotification.json"), aggregateSchema(nil))
	writeJSONFile(t, filepath.Join(root, "Shared.json"), map[string]any{
		"type":       "object",
		"properties": map[string]any{"value": map[string]any{"type": "string"}},
	})
	for rel, value := range extra {
		writeJSONFile(t, filepath.Join(root, rel), value)
	}
	writeJSONFile(t, filepath.Join(root, "baseline_metadata.json"), map[string]string{"ignored": filepath.Base(root)})
}

func aggregateSchema(methods []string) map[string]any {
	oneOf := []any{}
	for _, method := range methods {
		oneOf = append(oneOf, map[string]any{
			"title": method + "Request",
			"properties": map[string]any{
				"method": map[string]any{"enum": []string{method}},
				"params": map[string]any{"$ref": "#/definitions/" + method + "Params"},
			},
		})
	}
	return map[string]any{"oneOf": oneOf}
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func stringSlicesEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
