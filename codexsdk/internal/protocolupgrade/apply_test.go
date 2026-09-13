package protocolupgrade

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVerifyCommonRSSourceSHAMustMatchTarget(t *testing.T) {
	dir := t.TempDir()
	commonRS := filepath.Join(dir, "common.rs")
	if err := os.WriteFile(commonRS, []byte("client_request_definitions! {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := verifyCommonRSProvenance(commonRS, strings.Repeat("1", 40), strings.Repeat("2", 40), "")
	if err == nil || !strings.Contains(err.Error(), "does not match target") {
		t.Fatalf("got %v, want source SHA mismatch", err)
	}
}

func TestVerifyCommonRSContentMatchesCodexRepo(t *testing.T) {
	root := t.TempDir()
	codexRepo := filepath.Join(root, "codex")
	commonPath := filepath.Join(codexRepo, filepath.FromSlash(commonRSRef))
	if err := os.MkdirAll(filepath.Dir(commonPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(commonPath, []byte("client_request_definitions! {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, codexRepo, "init", "-q")
	runGit(t, codexRepo, "config", "user.email", "codex@example.com")
	runGit(t, codexRepo, "config", "user.name", "Codex")
	runGit(t, codexRepo, "add", commonRSRef)
	runGit(t, codexRepo, "commit", "-q", "-m", "common")
	sha := strings.TrimSpace(runGitOutput(t, codexRepo, "rev-parse", "HEAD"))

	candidate := filepath.Join(root, "common.rs")
	if err := os.WriteFile(candidate, []byte("client_request_definitions! {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyCommonRSProvenance(candidate, sha, sha, codexRepo); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidate, []byte("server_request_definitions! {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := verifyCommonRSProvenance(candidate, sha, sha, codexRepo)
	if err == nil || !strings.Contains(err.Error(), "content does not match") {
		t.Fatalf("got %v, want content mismatch", err)
	}
}

func TestBuildCoverageSeedsMissingFieldsAndPreservesReviewedFields(t *testing.T) {
	root := t.TempDir()
	writeJSONFile(t, filepath.Join(root, "ChangedResponse.json"), map[string]any{
		"title":    "ChangedResponse",
		"type":     "object",
		"required": []string{"value"},
		"properties": map[string]any{
			"optional": map[string]any{"type": "boolean"},
			"value":    map[string]any{"type": "string"},
		},
	})
	coverage, err := buildCoverage(root, coverageFile{
		Fields:        nil,
		Methods:       nil,
		SchemaVersion: 1,
		Types:         []map[string]any{{"schema": "ChangedResponse.json", "stability": "stable"}},
		ValidStatuses: validCoverageStatuses,
	}, manifestFile{}, map[string]bool{"ChangedResponse.json": true})
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]map[string]any{}
	for _, field := range coverage.Fields {
		path, _ := field["path"].(string)
		fields[path] = field
	}
	if fields["ChangedResponse.json#/properties/value"]["type"] != "ChangedResponse" {
		t.Fatalf("value field type = %#v", fields["ChangedResponse.json#/properties/value"])
	}
	if fields["ChangedResponse.json#/properties/value"]["required"] != true {
		t.Fatal("value field should be required")
	}
	if fields["ChangedResponse.json#/properties/optional"]["required"] != false {
		t.Fatal("optional field should not be required")
	}

	preserveRoot := t.TempDir()
	writeJSONFile(t, filepath.Join(preserveRoot, "ChangedResponse.json"), map[string]any{
		"title":    "ChangedResponse",
		"type":     "object",
		"required": []string{"value"},
		"properties": map[string]any{
			"value": map[string]any{"type": "string"},
		},
	})
	oldField := map[string]any{
		"field":     "value",
		"owner":     "codex-go-sdk",
		"path":      "ChangedResponse.json#/properties/value",
		"reason":    "Reviewed custom field coverage.",
		"required":  true,
		"schema":    "ChangedResponse.json",
		"stability": "stable",
		"status":    "supported",
		"type":      "ChangedResponse",
	}
	preserved, err := buildCoverage(preserveRoot, coverageFile{
		Fields:        []map[string]any{oldField},
		Methods:       nil,
		SchemaVersion: 1,
		Types:         []map[string]any{{"schema": "ChangedResponse.json", "stability": "stable"}},
		ValidStatuses: validCoverageStatuses,
	}, manifestFile{}, map[string]bool{"ChangedResponse.json": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(preserved.Fields) != 1 || preserved.Fields[0]["reason"] != "Reviewed custom field coverage." {
		t.Fatalf("reviewed field was not preserved: %#v", preserved.Fields)
	}
}

func TestApplyCopiesCandidateAndIsIdempotent(t *testing.T) {
	fix := writeApplyFixture(t)
	req := ApplyRequest{
		Baseline:          fix.baseline,
		Candidate:         fix.candidate,
		StableCandidate:   fix.stable,
		CommonRS:          fix.commonRS,
		CommonRSSourceSHA: fix.sha,
		Reports:           fix.reports,
		TargetRef:         "rust-v1.2.3",
		TargetKind:        "stable_rust_tag",
		TargetSHA:         fix.sha,
		SkipCodegen:       true,
		skipSurface:       true,
		Now:               func() time.Time { return time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC) },
	}
	first, err := Apply(req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "ok" {
		t.Fatalf("status = %q", first.Status)
	}
	if !containsString(first.AddedSchemas, "AddedType.json") {
		t.Fatalf("added schemas = %#v, want AddedType.json", first.AddedSchemas)
	}
	if _, err := os.Stat(filepath.Join(fix.baseline, "AddedType.json")); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(fix.module, "not-allowed.txt")
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("apply wrote outside the protocol surface")
	}
	afterFirst, err := schemaHashes(fix.baseline)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Apply(req)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != "ok" {
		t.Fatalf("second status = %q", second.Status)
	}
	afterSecond, err := schemaHashes(fix.baseline)
	if err != nil {
		t.Fatal(err)
	}
	if err := sameSnapshot(afterFirst, afterSecond, "schema files"); err != nil {
		t.Fatal(err)
	}
	if second.SchemaFileCount != first.SchemaFileCount || second.MethodCount != first.MethodCount {
		t.Fatalf("second apply changed counts: %#v vs %#v", second, first)
	}
}

func TestApplyRegeneratesGeneratedGoThroughCanonicalGenerator(t *testing.T) {
	root := copyModuleForCheck(t)
	baseline := filepath.Join(root, filepath.FromSlash(defaultBaselineRel))
	candidate := t.TempDir()
	if err := copyTree(baseline, candidate); err != nil {
		t.Fatal(err)
	}
	commonRS := filepath.Join(root, "common.rs")
	if err := os.WriteFile(commonRS, []byte(tinyCommonRS), 0o644); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("b", 40)
	_, err := Apply(ApplyRequest{
		Baseline:          baseline,
		Candidate:         candidate,
		StableCandidate:   candidate,
		CommonRS:          commonRS,
		CommonRSSourceSHA: sha,
		Reports:           filepath.Join(root, "reports"),
		TargetRef:         "rust-v0.154.0",
		TargetKind:        "stable_rust_tag",
		TargetSHA:         sha,
		ModuleRoot:        root,
		SkipCodegen:       false,
		Now:               func() time.Time { return time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Check(CheckRequest{ModuleRoot: root}); err != nil {
		t.Fatalf("generated Go was not reproducible after apply: %v", err)
	}
}

func TestApplyCodegenRequiresModuleBaseline(t *testing.T) {
	fix := writeApplyFixture(t)
	_, err := Apply(ApplyRequest{
		Baseline:          fix.baseline,
		Candidate:         fix.candidate,
		StableCandidate:   fix.stable,
		CommonRS:          fix.commonRS,
		CommonRSSourceSHA: fix.sha,
		Reports:           fix.reports,
		TargetRef:         "rust-v1.2.3",
		TargetKind:        "stable_rust_tag",
		TargetSHA:         fix.sha,
		ModuleRoot:        t.TempDir(),
		SkipCodegen:       false,
		skipSurface:       true,
	})
	if err == nil || !strings.Contains(err.Error(), defaultBaselineRel) {
		t.Fatalf("got %v, want baseline path diagnostic", err)
	}
}

func TestWriteReportsNamesGeneratedSchemaPath(t *testing.T) {
	root := t.TempDir()
	baseline := filepath.Join(root, "baseline")
	candidate := filepath.Join(root, "candidate")
	reports := filepath.Join(root, "reports")
	writeSchemaSet(t, baseline, []string{"thread/start"}, nil)
	writeSchemaSet(t, candidate, []string{"thread/start"}, nil)
	report, err := Compare(CompareRequest{
		Baseline:      baseline,
		Candidate:     candidate,
		SourceCommit:  strings.Repeat("1", 40),
		SourceRef:     "rust-v0.141.0",
		SourceRefKind: "stable_rust_tag",
		CodexVersion:  "codex-cli test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteReports(reports, report, candidate); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(reports, "SUMMARY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), candidate) {
		t.Fatalf("SUMMARY.md missing generated schema path %s:\n%s", candidate, raw)
	}
}

func TestUpdateManifestGenerationRequiresObjectInputs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest_generation.json")
	writeJSONFile(t, path, map[string]any{"inputs": "old"})
	err := updateManifestGeneration(path, "rust-v1.2.3", "stable_rust_tag", strings.Repeat("1", 40))
	if err == nil || !strings.Contains(err.Error(), "inputs") {
		t.Fatalf("got %v, want inputs error", err)
	}
}

type applyFixture struct {
	module    string
	baseline  string
	candidate string
	stable    string
	commonRS  string
	reports   string
	sha       string
}

func writeApplyFixture(t *testing.T) applyFixture {
	t.Helper()
	module := t.TempDir()
	baseline := filepath.Join(module, filepath.FromSlash(defaultBaselineRel))
	candidate := filepath.Join(module, "candidate")
	stable := filepath.Join(module, "stable")
	reports := filepath.Join(module, "reports")
	sha := strings.Repeat("a", 40)
	writeTinyProtocol(t, baseline, false)
	writeTinyProtocol(t, candidate, true)
	writeTinyProtocol(t, stable, true)
	commonRS := filepath.Join(module, "common.rs")
	if err := os.WriteFile(commonRS, []byte(tinyCommonRS), 0o644); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, filepath.Join(baseline, "baseline_metadata.json"), map[string]any{
		"schema_version": 1,
		"source_commit":  strings.Repeat("0", 40),
		"generated_at":   "2026-01-01T00:00:00Z",
	})
	writeJSONFile(t, filepath.Join(baseline, "manifest_generation.json"), map[string]any{
		"inputs": map[string]any{
			"source_ref_name": "rust-v0.1.0",
			"source_ref_kind": "stable_rust_tag",
			"source_commit":   strings.Repeat("0", 40),
		},
	})
	writeJSONFile(t, filepath.Join(baseline, "manifest.json"), map[string]any{
		"schema_version":    2,
		"status":            "classified-manifest",
		"aggregate_schemas": aggregateSchemas,
		"description":       "test manifest",
		"classification_sources": map[string]any{
			"method_surface": "test",
		},
		"surface": []map[string]any{
			{"kind": "type", "name": "SurfaceSeed", "signature": "struct{}", "stability": "stable"},
		},
		"entries": []map[string]any{
			{
				"direction":                "client_to_server",
				"facade_target":            "Threads().Start",
				"facade_status":            "generated",
				"family":                   "thread",
				"kind":                     "request",
				"method":                   "thread/start",
				"params_or_payload_schema": "ThreadStartParams",
				"response_schema":          "ThreadStartResponse.json",
				"response_schema_status":   "declared",
				"response_type":            "ThreadStartResponse",
				"schema_title":             "Thread/startRequest",
				"source_schema":            "ClientRequest.json",
				"source_variant":           "ThreadStart",
				"stability":                "stable",
				"stability_source":         "present_in_stable_schema",
				"source_ref": map[string]string{
					"response_mapping": commonRSRef + "#client_request_definitions/ThreadStart",
				},
			},
		},
	})
	writeJSONFile(t, filepath.Join(baseline, "coverage_matrix.json"), map[string]any{
		"schema_version": 1,
		"status":         "classified-manifest",
		"valid_statuses": validCoverageStatuses,
		"methods":        []any{},
		"fields":         []any{},
		"types": []map[string]any{
			{"schema": "ClientRequest.json", "stability": "stable", "status": "supported-generated", "type": "ClientRequest"},
			{"schema": "ServerRequest.json", "stability": "stable", "status": "supported-generated", "type": "ServerRequest"},
			{"schema": "ServerNotification.json", "stability": "stable", "status": "supported-generated", "type": "ServerNotification"},
			{"schema": "ClientNotification.json", "stability": "stable", "status": "supported-generated", "type": "ClientNotification"},
			{"schema": "ThreadStartParams.json", "stability": "stable", "status": "supported-generated", "type": "ThreadStartParams"},
			{"schema": "ThreadStartResponse.json", "stability": "stable", "status": "supported-generated", "type": "ThreadStartResponse"},
		},
	})
	return applyFixture{
		module:    module,
		baseline:  baseline,
		candidate: candidate,
		stable:    stable,
		commonRS:  commonRS,
		reports:   reports,
		sha:       sha,
	}
}

func writeTinyProtocol(t *testing.T, root string, withAdded bool) {
	t.Helper()
	writeJSONFile(t, filepath.Join(root, "ClientRequest.json"), map[string]any{
		"oneOf": []any{
			map[string]any{
				"title": "Thread/startRequest",
				"properties": map[string]any{
					"method": map[string]any{"enum": []string{"thread/start"}},
					"params": map[string]any{"$ref": "ThreadStartParams.json"},
				},
			},
		},
	})
	writeJSONFile(t, filepath.Join(root, "ServerRequest.json"), map[string]any{"oneOf": []any{}})
	writeJSONFile(t, filepath.Join(root, "ServerNotification.json"), map[string]any{"oneOf": []any{}})
	writeJSONFile(t, filepath.Join(root, "ClientNotification.json"), map[string]any{"oneOf": []any{}})
	writeJSONFile(t, filepath.Join(root, "ThreadStartParams.json"), map[string]any{
		"title": "ThreadStartParams",
		"type":  "object",
		"properties": map[string]any{
			"prompt": map[string]any{"type": "string"},
		},
	})
	writeJSONFile(t, filepath.Join(root, "ThreadStartResponse.json"), map[string]any{
		"title": "ThreadStartResponse",
		"type":  "object",
		"properties": map[string]any{
			"threadId": map[string]any{"type": "string"},
		},
	})
	if withAdded {
		writeJSONFile(t, filepath.Join(root, "AddedType.json"), map[string]any{
			"title": "AddedType",
			"type":  "object",
			"properties": map[string]any{
				"value": map[string]any{"type": "string"},
			},
		})
	}
}

const tinyCommonRS = `client_request_definitions! {
  ThreadStart => "thread/start" {
    response: ThreadStartResponse,
  },
}

server_request_definitions! {
}
`

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return string(out)
}
