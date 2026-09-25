package protocolupgrade

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolgen"
)

func TestPlanReadyDoesNotMutateAcceptedBaseline(t *testing.T) {
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
		Now:               func() time.Time { return time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC) },
	}
	before, err := snapshotHashes(fix.baseline)
	if err != nil {
		t.Fatal(err)
	}
	planned, err := Plan(req)
	if err != nil {
		t.Fatal(err)
	}
	if planned.Status != PlanReady {
		t.Fatalf("plan = %+v", planned)
	}
	after, err := snapshotHashes(fix.baseline)
	if err != nil {
		t.Fatal(err)
	}
	if err := sameSnapshot(before, after, "accepted baseline"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(fix.baseline, "AddedType.json")); !os.IsNotExist(err) {
		t.Fatalf("plan wrote candidate schema into accepted baseline: %v", err)
	}
}

func TestPlanRunsCanonicalCodegenInIsolation(t *testing.T) {
	root := copyModuleForCheck(t)
	baseline := filepath.Join(root, filepath.FromSlash(defaultBaselineRel))
	candidate := t.TempDir()
	if err := copyTree(baseline, candidate); err != nil {
		t.Fatal(err)
	}
	commonRS := filepath.Join(root, "common.rs")
	writeBaselineMappingFixture(t, baseline, commonRS)
	sha := strings.Repeat("b", 40)
	before, err := snapshotHashes(baseline)
	if err != nil {
		t.Fatal(err)
	}
	planned, err := Plan(ApplyRequest{
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
		Now:               func() time.Time { return time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if planned.Status != PlanReady {
		t.Fatalf("plan = %+v", planned)
	}
	after, err := snapshotHashes(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if err := sameSnapshot(before, after, "accepted baseline"); err != nil {
		t.Fatal(err)
	}
}

func TestPlanReturnsStructuredSemanticIncompatibility(t *testing.T) {
	fix := writeApplyFixture(t)
	if err := os.WriteFile(fix.commonRS, []byte("client_request_definitions! {}\nserver_request_definitions! {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	planned, err := Plan(ApplyRequest{
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
	})
	if err != nil {
		t.Fatal(err)
	}
	if planned.Status != PlanSemanticUnresolved || planned.Issue == nil || planned.Issue.Stage != "manifest" || planned.Issue.Path != "ClientRequest.json#/oneOf/0" || !strings.Contains(planned.Issue.Reason, "missing response mapping") {
		t.Fatalf("plan = %+v", planned)
	}
}

func TestPlanReportsUnsupportedSchemaFromGenerator(t *testing.T) {
	root := copyModuleForCheck(t)
	baseline := filepath.Join(root, filepath.FromSlash(defaultBaselineRel))
	candidate := t.TempDir()
	if err := copyTree(baseline, candidate); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, filepath.Join(candidate, "ArbitraryPayload.json"), map[string]any{
		"title": "ArbitraryPayload", "type": "object",
		"properties": map[string]any{"value": map[string]any{"not": map[string]any{"type": "string"}}},
	})
	commonRS := filepath.Join(root, "common.rs")
	writeBaselineMappingFixture(t, baseline, commonRS)
	sha := strings.Repeat("b", 40)
	planned, err := Plan(ApplyRequest{
		Baseline: baseline, Candidate: candidate, StableCandidate: candidate,
		CommonRS: commonRS, CommonRSSourceSHA: sha,
		TargetRef: "rust-v0.154.0", TargetKind: "stable_rust_tag", TargetSHA: sha,
		ModuleRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if planned.Status != PlanSemanticUnresolved || planned.Issue == nil || planned.Issue.Stage != "surface" || planned.Issue.Path != "ArbitraryPayload.json#/properties/value" {
		t.Fatalf("plan = %+v, want typed surface incompatibility", planned)
	}
	writeJSONFile(t, filepath.Join(candidate, "ArbitraryPayload.json"), map[string]any{
		"title": "ArbitraryPayload", "type": "object",
		"properties": map[string]any{"value": map[string]any{"type": "string"}},
	})
	repaired, err := Plan(ApplyRequest{
		Baseline: baseline, Candidate: candidate, StableCandidate: candidate,
		CommonRS: commonRS, CommonRSSourceSHA: sha,
		TargetRef: "rust-v0.154.0", TargetKind: "stable_rust_tag", TargetSHA: sha,
		ModuleRoot: root,
	})
	if err != nil || repaired.Status != PlanReady {
		t.Fatalf("same candidate after representation repair: plan = %+v, err = %v", repaired, err)
	}
}

func TestPlanReportsSelectedUnsupportedDefinition(t *testing.T) {
	root := copyModuleForCheck(t)
	baseline := filepath.Join(root, filepath.FromSlash(defaultBaselineRel))
	candidate := t.TempDir()
	if err := copyTree(baseline, candidate); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, filepath.Join(candidate, "v2", "BedrockDiscoverParams.json"), map[string]any{
		"title": "BedrockDiscoverParams", "type": "object",
		"properties":  map[string]any{"value": map[string]any{"$ref": "#/definitions/Odd~1Name"}},
		"definitions": map[string]any{"Odd/Name": map[string]any{"not": map[string]any{"type": "string"}}},
	})
	commonRS := filepath.Join(root, "common.rs")
	writeBaselineMappingFixture(t, baseline, commonRS)
	sha := strings.Repeat("b", 40)
	planned, err := Plan(ApplyRequest{
		Baseline: baseline, Candidate: candidate, StableCandidate: candidate,
		CommonRS: commonRS, CommonRSSourceSHA: sha,
		TargetRef: "rust-v0.154.0", TargetKind: "stable_rust_tag", TargetSHA: sha,
		ModuleRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if planned.Status != PlanSemanticUnresolved || planned.Issue == nil || planned.Issue.Stage != "surface" || planned.Issue.Path != "v2/BedrockDiscoverParams.json#/definitions/Odd~1Name" {
		t.Fatalf("plan = %+v issue = %+v, want selected definition incompatibility", planned, planned.Issue)
	}
}

func TestPlanReportsGeneratedEnumNameCollision(t *testing.T) {
	root := copyModuleForCheck(t)
	baseline := filepath.Join(root, filepath.FromSlash(defaultBaselineRel))
	candidate := t.TempDir()
	if err := copyTree(baseline, candidate); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, filepath.Join(candidate, "v2", "BedrockDiscoverParams.json"), map[string]any{
		"title": "BedrockDiscoverParams", "type": "object",
		"properties": map[string]any{"value": map[string]any{"$ref": "#/definitions/OddName"}},
		"definitions": map[string]any{"OddName": map[string]any{
			"type": "string", "enum": []string{"foo-bar", "foo_bar"},
		}},
	})
	commonRS := filepath.Join(root, "common.rs")
	writeBaselineMappingFixture(t, baseline, commonRS)
	sha := strings.Repeat("b", 40)
	planned, err := Plan(ApplyRequest{
		Baseline: baseline, Candidate: candidate, StableCandidate: candidate,
		CommonRS: commonRS, CommonRSSourceSHA: sha,
		TargetRef: "rust-v0.154.0", TargetKind: "stable_rust_tag", TargetSHA: sha,
		ModuleRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if planned.Status != PlanSemanticUnresolved || planned.Issue == nil || planned.Issue.Stage != "surface" || planned.Issue.Path != "v2/BedrockDiscoverParams.json#/definitions/OddName" {
		t.Fatalf("plan = %+v issue = %+v, want enum source pointer", planned, planned.Issue)
	}
}

func TestPlanReportsUnrepresentableGeneratedTypeName(t *testing.T) {
	root := copyModuleForCheck(t)
	baseline := filepath.Join(root, filepath.FromSlash(defaultBaselineRel))
	candidate := t.TempDir()
	if err := copyTree(baseline, candidate); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(candidate, "ClientRequest.json")
	var schema map[string]any
	if err := loadJSON(path, &schema); err != nil {
		t.Fatal(err)
	}
	schema["title"] = "Bad-Name"
	writeJSONFile(t, path, schema)
	commonRS := filepath.Join(root, "common.rs")
	writeBaselineMappingFixture(t, baseline, commonRS)
	sha := strings.Repeat("b", 40)
	planned, err := Plan(ApplyRequest{
		Baseline: baseline, Candidate: candidate, StableCandidate: candidate,
		CommonRS: commonRS, CommonRSSourceSHA: sha,
		TargetRef: "rust-v0.154.0", TargetKind: "stable_rust_tag", TargetSHA: sha,
		ModuleRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if planned.Status != PlanSemanticUnresolved || planned.Issue == nil || planned.Issue.Stage != "surface" || planned.Issue.Path != "ClientRequest.json" {
		t.Fatalf("plan = %+v issue = %+v, want generated type source", planned, planned.Issue)
	}
}

func TestPlanReportsUnrepresentableGeneratedDefinitionName(t *testing.T) {
	root := copyModuleForCheck(t)
	baseline := filepath.Join(root, filepath.FromSlash(defaultBaselineRel))
	candidate := t.TempDir()
	if err := copyTree(baseline, candidate); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, filepath.Join(candidate, "v2", "BedrockDiscoverParams.json"), map[string]any{
		"title": "BedrockDiscoverParams", "type": "object",
		"properties": map[string]any{"value": map[string]any{"$ref": "#/definitions/Odd-Name"}},
		"definitions": map[string]any{"Odd-Name": map[string]any{
			"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}},
		}},
	})
	commonRS := filepath.Join(root, "common.rs")
	writeBaselineMappingFixture(t, baseline, commonRS)
	sha := strings.Repeat("b", 40)
	planned, err := Plan(ApplyRequest{
		Baseline: baseline, Candidate: candidate, StableCandidate: candidate,
		CommonRS: commonRS, CommonRSSourceSHA: sha,
		TargetRef: "rust-v0.154.0", TargetKind: "stable_rust_tag", TargetSHA: sha,
		ModuleRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if planned.Status != PlanSemanticUnresolved || planned.Issue == nil || planned.Issue.Stage != "surface" || planned.Issue.Path != "v2/BedrockDiscoverParams.json#/definitions/Odd-Name" {
		t.Fatalf("plan = %+v issue = %+v, want generated definition source", planned, planned.Issue)
	}
}

func TestPlanReportsUnrepresentableWireFieldName(t *testing.T) {
	root := copyModuleForCheck(t)
	baseline := filepath.Join(root, filepath.FromSlash(defaultBaselineRel))
	candidate := t.TempDir()
	if err := copyTree(baseline, candidate); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, filepath.Join(candidate, "v2", "BedrockDiscoverParams.json"), map[string]any{
		"title": "BedrockDiscoverParams", "type": "object",
		"required": []string{"a~/b\""},
		"properties": map[string]any{"a~/b\"": map[string]any{
			"type": "array", "items": map[string]any{"type": "string"},
		}},
	})
	commonRS := filepath.Join(root, "common.rs")
	writeBaselineMappingFixture(t, baseline, commonRS)
	sha := strings.Repeat("b", 40)
	planned, err := Plan(ApplyRequest{
		Baseline: baseline, Candidate: candidate, StableCandidate: candidate,
		CommonRS: commonRS, CommonRSSourceSHA: sha,
		TargetRef: "rust-v0.154.0", TargetKind: "stable_rust_tag", TargetSHA: sha,
		ModuleRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if planned.Status != PlanSemanticUnresolved || planned.Issue == nil || planned.Issue.Stage != "surface" || planned.Issue.Path != "v2/BedrockDiscoverParams.json#/properties/a~0~1b\"" {
		t.Fatalf("plan = %+v issue = %+v, want wire field source", planned, planned.Issue)
	}
}

func TestPlanReportsGeneratedFieldNameCollision(t *testing.T) {
	root := copyModuleForCheck(t)
	baseline := filepath.Join(root, filepath.FromSlash(defaultBaselineRel))
	candidate := t.TempDir()
	if err := copyTree(baseline, candidate); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, filepath.Join(candidate, "v2", "BedrockDiscoverParams.json"), map[string]any{
		"title": "BedrockDiscoverParams", "type": "object",
		"properties": map[string]any{
			"foo-bar": map[string]any{"type": "string"},
			"foo_bar": map[string]any{"type": "string"},
		},
	})
	commonRS := filepath.Join(root, "common.rs")
	writeBaselineMappingFixture(t, baseline, commonRS)
	sha := strings.Repeat("b", 40)
	planned, err := Plan(ApplyRequest{
		Baseline: baseline, Candidate: candidate, StableCandidate: candidate,
		CommonRS: commonRS, CommonRSSourceSHA: sha,
		TargetRef: "rust-v0.154.0", TargetKind: "stable_rust_tag", TargetSHA: sha,
		ModuleRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if planned.Status != PlanSemanticUnresolved || planned.Issue == nil || planned.Issue.Stage != "surface" || planned.Issue.Path != "v2/BedrockDiscoverParams.json#/properties/foo_bar" {
		t.Fatalf("plan = %+v issue = %+v, want colliding field source", planned, planned.Issue)
	}
}

func TestPlanAcceptsRepresentableUnfamiliarFieldNames(t *testing.T) {
	root := copyModuleForCheck(t)
	baseline := filepath.Join(root, filepath.FromSlash(defaultBaselineRel))
	candidate := t.TempDir()
	if err := copyTree(baseline, candidate); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, filepath.Join(candidate, "v2", "BedrockDiscoverParams.json"), map[string]any{
		"title": "BedrockDiscoverParams", "type": "object",
		"required": []string{"-", "é"},
		"properties": map[string]any{
			"-": map[string]any{"type": "string"},
			"é": map[string]any{"type": "string"},
		},
	})
	commonRS := filepath.Join(root, "common.rs")
	writeBaselineMappingFixture(t, baseline, commonRS)
	sha := strings.Repeat("b", 40)
	planned, err := Plan(ApplyRequest{
		Baseline: baseline, Candidate: candidate, StableCandidate: candidate,
		CommonRS: commonRS, CommonRSSourceSHA: sha,
		TargetRef: "rust-v0.154.0", TargetKind: "stable_rust_tag", TargetSHA: sha,
		ModuleRoot: root,
	})
	if err != nil || planned.Status != PlanReady {
		t.Fatalf("plan = %+v, err = %v, want ready", planned, err)
	}
}

func TestPlanReportsGeneratedMemberCollision(t *testing.T) {
	for _, tc := range []struct {
		name   string
		schema map[string]any
		path   string
	}{
		{
			name: "method",
			schema: map[string]any{
				"title": "BedrockDiscoverParams", "type": "object",
				"properties": map[string]any{"unmarshalJSON": map[string]any{"type": "string"}},
			},
			path: "v2/BedrockDiscoverParams.json#/properties/unmarshalJSON",
		},
		{
			name: "dynamic properties",
			schema: map[string]any{
				"title": "BedrockDiscoverParams", "type": "object",
				"properties": map[string]any{"value": map[string]any{"$ref": "#/definitions/Extras"}},
				"definitions": map[string]any{"Extras": map[string]any{
					"type": "object", "additionalProperties": true,
					"properties": map[string]any{"dynamicProperties": map[string]any{"type": "string"}},
				}},
			},
			path: "v2/BedrockDiscoverParams.json#/definitions/Extras/properties/dynamicProperties",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := copyModuleForCheck(t)
			baseline := filepath.Join(root, filepath.FromSlash(defaultBaselineRel))
			candidate := t.TempDir()
			if err := copyTree(baseline, candidate); err != nil {
				t.Fatal(err)
			}
			writeJSONFile(t, filepath.Join(candidate, "v2", "BedrockDiscoverParams.json"), tc.schema)
			commonRS := filepath.Join(root, "common.rs")
			writeBaselineMappingFixture(t, baseline, commonRS)
			sha := strings.Repeat("b", 40)
			planned, err := Plan(ApplyRequest{
				Baseline: baseline, Candidate: candidate, StableCandidate: candidate,
				CommonRS: commonRS, CommonRSSourceSHA: sha,
				TargetRef: "rust-v0.154.0", TargetKind: "stable_rust_tag", TargetSHA: sha,
				ModuleRoot: root,
			})
			if err != nil {
				t.Fatal(err)
			}
			if planned.Status != PlanSemanticUnresolved || planned.Issue == nil || planned.Issue.Stage != "surface" || planned.Issue.Path != tc.path {
				t.Fatalf("plan = %+v issue = %+v, want generated member collision at %s", planned, planned.Issue, tc.path)
			}
		})
	}
}

func TestPlanReportsFacadeNameCollisionAsSemanticDrift(t *testing.T) {
	root := copyModuleForCheck(t)
	baseline := filepath.Join(root, filepath.FromSlash(defaultBaselineRel))
	candidate, stable := t.TempDir(), t.TempDir()
	for _, dir := range []string{candidate, stable} {
		if err := copyTree(baseline, dir); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "ClientRequest.json")
		var schema map[string]any
		if err := loadJSON(path, &schema); err != nil {
			t.Fatal(err)
		}
		variants := schema["oneOf"].([]any)
		variants = append(variants, map[string]any{
			"title": "FuzzyFileSearchSearchRequest", "type": "object",
			"required": []string{"id", "method", "params"},
			"properties": map[string]any{
				"id":     map[string]any{"$ref": "#/definitions/RequestId"},
				"method": map[string]any{"enum": []string{"fuzzyFileSearch/search"}, "type": "string"},
				"params": map[string]any{"$ref": "#/definitions/FuzzyFileSearchParams"},
			},
		})
		schema["oneOf"] = variants
		writeJSONFile(t, path, schema)
	}
	commonRS := filepath.Join(root, "common.rs")
	writeBaselineMappingFixture(t, baseline, commonRS)
	data, err := os.ReadFile(commonRS)
	if err != nil {
		t.Fatal(err)
	}
	marker := "}\nserver_request_definitions!"
	if !strings.Contains(string(data), marker) {
		t.Fatal("mapping fixture is missing server request boundary")
	}
	common := strings.Replace(string(data), marker, "  FuzzyFileSearchSearch => \"fuzzyFileSearch/search\" {\n    response: FuzzyFileSearchResponse,\n  },\n"+marker, 1)
	if err := os.WriteFile(commonRS, []byte(common), 0o644); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("b", 40)
	planned, err := Plan(ApplyRequest{
		Baseline: baseline, Candidate: candidate, StableCandidate: stable,
		CommonRS: commonRS, CommonRSSourceSHA: sha,
		TargetRef: "rust-v0.154.0", TargetKind: "stable_rust_tag", TargetSHA: sha,
		ModuleRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if planned.Status != PlanSemanticUnresolved || planned.Issue == nil || planned.Issue.Stage != "codegen" || planned.Issue.Path != "ClientRequest.json" || !strings.Contains(planned.Issue.Reason, "FuzzyFileSearch().Search") {
		t.Fatalf("plan = %+v issue = %+v, want typed facade collision", planned, planned.Issue)
	}
}

func TestPlanKeepsSourceAndEnvironmentFailuresOrdinary(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(t *testing.T, req *ApplyRequest)
	}{
		{name: "missing common.rs", mutate: func(t *testing.T, req *ApplyRequest) {
			req.CommonRS = filepath.Join(t.TempDir(), "missing.rs")
		}},
		{name: "malformed common.rs", mutate: func(t *testing.T, req *ApplyRequest) {
			if err := os.WriteFile(req.CommonRS, []byte("client_request_definitions! { broken"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "source SHA mismatch", mutate: func(t *testing.T, req *ApplyRequest) {
			req.CommonRSSourceSHA = strings.Repeat("b", 40)
		}},
		{name: "temporary directory unavailable", mutate: func(t *testing.T, req *ApplyRequest) {
			t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fix := writeApplyFixture(t)
			req := ApplyRequest{
				Baseline: fix.baseline, Candidate: fix.candidate, StableCandidate: fix.stable,
				CommonRS: fix.commonRS, CommonRSSourceSHA: fix.sha, Reports: fix.reports,
				TargetRef: "rust-v1.2.3", TargetKind: "stable_rust_tag", TargetSHA: fix.sha,
				SkipCodegen: true, skipSurface: true,
			}
			test.mutate(t, &req)
			planned, err := Plan(req)
			var incompatibility *IncompatibilityError
			if err == nil || errors.As(err, &incompatibility) || planned.Status == PlanSemanticUnresolved {
				t.Fatalf("plan = %+v, err = %v; want ordinary failure", planned, err)
			}
		})
	}
}

func TestClassifyUnsupportedPreservesCauseAndIgnoresOrdinaryErrors(t *testing.T) {
	cause := errors.New("unrepresented schema shape")
	owner := &protocolgen.UnsupportedSchemaError{Path: "Arbitrary.json#/properties/value", Err: cause}
	err := classifyUnsupported("codegen", owner)
	var incompatibility *IncompatibilityError
	if !errors.As(err, &incompatibility) || incompatibility.Stage != "codegen" || incompatibility.Path != owner.Path || !errors.Is(err, cause) {
		t.Fatalf("typed error = %v, want structured cause-preserving incompatibility", err)
	}
	ordinary := classifyUnsupported("codegen", fmt.Errorf("read schema: %w", os.ErrNotExist))
	if errors.As(ordinary, &incompatibility) || !errors.Is(ordinary, os.ErrNotExist) {
		t.Fatalf("ordinary error = %v, want original read failure", ordinary)
	}
}

func TestPlanDoesNotWriteConfiguredReportsDirectory(t *testing.T) {
	fix := writeApplyFixture(t)
	reports := filepath.Join(fix.module, "external-reports")
	_, err := Plan(ApplyRequest{
		Baseline:          fix.baseline,
		Candidate:         fix.candidate,
		StableCandidate:   fix.stable,
		CommonRS:          fix.commonRS,
		CommonRSSourceSHA: fix.sha,
		Reports:           reports,
		TargetRef:         "rust-v1.2.3",
		TargetKind:        "stable_rust_tag",
		TargetSHA:         fix.sha,
		SkipCodegen:       true,
		skipSurface:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(reports); !os.IsNotExist(err) {
		t.Fatalf("plan wrote caller reports directory: %v", err)
	}
	if strings.TrimSpace(reports) == "" {
		t.Fatal("fixture reports path unexpectedly empty")
	}
}
