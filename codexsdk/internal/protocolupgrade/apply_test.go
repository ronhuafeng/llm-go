package protocolupgrade

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolgen"
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

func TestBuildCoverageDerivesFieldsAndPreservesAnnotations(t *testing.T) {
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
	coverage, err := buildCoverage(root, root, coverageFile{
		Fields:        nil,
		Methods:       nil,
		SchemaVersion: 1,
		Types:         []map[string]any{{"schema": "ChangedResponse.json", "stability": "stable"}},
		ValidStatuses: validCoverageStatuses,
	}, manifestFile{})
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
	preserved, err := buildCoverage(preserveRoot, preserveRoot, coverageFile{
		Fields:        []map[string]any{oldField},
		Methods:       nil,
		SchemaVersion: 1,
		Types:         []map[string]any{{"schema": "ChangedResponse.json", "stability": "stable"}},
		ValidStatuses: validCoverageStatuses,
	}, manifestFile{})
	if err != nil {
		t.Fatal(err)
	}
	if len(preserved.Fields) != 1 || preserved.Fields[0]["reason"] != "Reviewed custom field coverage." {
		t.Fatalf("reviewed field was not preserved: %#v", preserved.Fields)
	}
}

func TestBuildCoverageRejectsContradictoryStableFields(t *testing.T) {
	for _, test := range []struct {
		name, field    string
		stableRequired []string
		want           string
	}{
		{name: "stable field absent from complete", field: "other", want: "absent from complete"},
		{name: "requiredness differs", field: "value", stableRequired: []string{"value"}, want: "requiredness differs"},
	} {
		t.Run(test.name, func(t *testing.T) {
			complete := t.TempDir()
			stable := t.TempDir()
			writeJSONFile(t, filepath.Join(complete, "Value.json"), map[string]any{
				"title": "Value", "type": "object",
				"properties": map[string]any{"value": map[string]any{"type": "string"}},
			})
			writeJSONFile(t, filepath.Join(stable, "Value.json"), map[string]any{
				"title": "Value", "type": "object", "required": test.stableRequired,
				"properties": map[string]any{test.field: map[string]any{"type": "string"}},
			})
			_, err := buildCoverage(complete, stable, coverageFile{SchemaVersion: 1}, manifestFile{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want %q", err, test.want)
			}
		})
	}
}

func TestSchemaTypeIndexRejectsAmbiguousNames(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"v1/Foo.json", "v2/Foo.json"} {
		writeJSONFile(t, filepath.Join(root, path), map[string]any{"title": "Foo", "type": "object"})
	}
	_, err := schemaTypeIndex(root)
	if err == nil || !strings.Contains(err.Error(), "ambiguous schema type Foo") {
		t.Fatalf("got %v, want ambiguous schema type", err)
	}
}

func TestApplyDoesNotCarryHistoricalDeferredFacadeStatus(t *testing.T) {
	fix := writeApplyFixture(t)
	manifestPath := filepath.Join(fix.baseline, "manifest.json")
	var manifest manifestFile
	if err := loadJSON(manifestPath, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) != 1 {
		t.Fatalf("fixture manifest entries = %d, want 1", len(manifest.Entries))
	}
	manifest.Entries[0].FacadeStatus = "deferred_missing_generated_types"
	if err := writeJSON(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}

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
		SkipCodegen:       true,
		skipSurface:       true,
		Now:               func() time.Time { return time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	var regenerated manifestFile
	if err := loadJSON(manifestPath, &regenerated); err != nil {
		t.Fatal(err)
	}
	if len(regenerated.Entries) != 1 || regenerated.Entries[0].FacadeStatus != "generated" {
		t.Fatalf("regenerated facade status = %#v, want derived generated", regenerated.Entries)
	}
}

func TestApplyUsesCurrentSchemaRequiredness(t *testing.T) {
	for _, test := range []struct {
		name            string
		oldRequired     bool
		currentRequired bool
	}{
		{name: "optional becomes required", oldRequired: false, currentRequired: true},
		{name: "required becomes optional", oldRequired: true, currentRequired: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fix := writeApplyFixture(t)
			for _, root := range []string{fix.candidate, fix.stable} {
				var schema map[string]any
				path := filepath.Join(root, "ThreadStartParams.json")
				if err := loadJSON(path, &schema); err != nil {
					t.Fatal(err)
				}
				if test.currentRequired {
					schema["required"] = []string{"prompt"}
				} else {
					delete(schema, "required")
				}
				writeJSONFile(t, path, schema)
			}
			coveragePath := filepath.Join(fix.baseline, "coverage_matrix.json")
			var old coverageFile
			if err := loadJSON(coveragePath, &old); err != nil {
				t.Fatal(err)
			}
			old.Fields = []map[string]any{{
				"field": "prompt", "path": "ThreadStartParams.json#/properties/prompt",
				"schema": "ThreadStartParams.json", "type": "ThreadStartParams",
				"required": test.oldRequired, "stability": "stable", "status": "supported-generated",
			}}
			if err := writeJSON(coveragePath, old); err != nil {
				t.Fatal(err)
			}
			_, err := Apply(ApplyRequest{
				Baseline: fix.baseline, Candidate: fix.candidate, StableCandidate: fix.stable,
				CommonRS: fix.commonRS, CommonRSSourceSHA: fix.sha, TargetRef: "rust-v1.2.3",
				TargetKind: "stable_rust_tag", TargetSHA: fix.sha, SkipCodegen: true, skipSurface: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			var generated coverageFile
			if err := loadJSON(coveragePath, &generated); err != nil {
				t.Fatal(err)
			}
			for _, field := range generated.Fields {
				if field["path"] == "ThreadStartParams.json#/properties/prompt" {
					if field["required"] != test.currentRequired {
						t.Fatalf("required = %v, want %v", field["required"], test.currentRequired)
					}
					return
				}
			}
			t.Fatal("prompt field missing from current coverage")
		})
	}
}

func TestApplyUsesCurrentStableVisibility(t *testing.T) {
	for _, test := range []struct {
		name          string
		oldStability  string
		stableVisible bool
		wantStability string
	}{
		{name: "experimental becomes stable", oldStability: "experimental", stableVisible: true, wantStability: "stable"},
		{name: "stable becomes experimental", oldStability: "stable", stableVisible: false, wantStability: "experimental"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fix := writeApplyFixture(t)
			manifestPath := filepath.Join(fix.baseline, "manifest.json")
			var old manifestFile
			if err := loadJSON(manifestPath, &old); err != nil {
				t.Fatal(err)
			}
			old.Entries[0].Stability = test.oldStability
			if err := writeJSON(manifestPath, old); err != nil {
				t.Fatal(err)
			}
			if !test.stableVisible {
				writeJSONFile(t, filepath.Join(fix.stable, "ClientRequest.json"), map[string]any{"oneOf": []any{}})
				if err := os.Remove(filepath.Join(fix.stable, "ThreadStartParams.json")); err != nil {
					t.Fatal(err)
				}
			}
			coveragePath := filepath.Join(fix.baseline, "coverage_matrix.json")
			var oldCoverage coverageFile
			if err := loadJSON(coveragePath, &oldCoverage); err != nil {
				t.Fatal(err)
			}
			for _, typ := range oldCoverage.Types {
				if typ["schema"] == "ThreadStartParams.json" {
					typ["stability"] = test.oldStability
				}
			}
			if err := writeJSON(coveragePath, oldCoverage); err != nil {
				t.Fatal(err)
			}
			_, err := Apply(ApplyRequest{
				Baseline: fix.baseline, Candidate: fix.candidate, StableCandidate: fix.stable,
				CommonRS: fix.commonRS, CommonRSSourceSHA: fix.sha, TargetRef: "rust-v1.2.3",
				TargetKind: "stable_rust_tag", TargetSHA: fix.sha, SkipCodegen: true, skipSurface: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			var generated manifestFile
			if err := loadJSON(manifestPath, &generated); err != nil {
				t.Fatal(err)
			}
			if got := generated.Entries[0].Stability; got != test.wantStability {
				t.Fatalf("stability = %s, want %s", got, test.wantStability)
			}
			var coverage coverageFile
			if err := loadJSON(coveragePath, &coverage); err != nil {
				t.Fatal(err)
			}
			for _, typ := range coverage.Types {
				if typ["schema"] == "ThreadStartParams.json" && typ["stability"] != test.wantStability {
					t.Fatalf("type stability = %v, want %s", typ["stability"], test.wantStability)
				}
			}
		})
	}
}

func TestApplyRejectsMissingCurrentResponseMapping(t *testing.T) {
	fix := writeApplyFixture(t)
	if err := os.WriteFile(fix.commonRS, []byte("client_request_definitions! {}\nserver_request_definitions! {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Apply(ApplyRequest{
		Baseline: fix.baseline, Candidate: fix.candidate, StableCandidate: fix.stable,
		CommonRS: fix.commonRS, CommonRSSourceSHA: fix.sha, TargetRef: "rust-v1.2.3",
		TargetKind: "stable_rust_tag", TargetSHA: fix.sha, SkipCodegen: true, skipSurface: true,
	})
	if err == nil || !strings.Contains(err.Error(), "missing response mapping") {
		t.Fatalf("missing current mapping must fail, got %v", err)
	}
}

func TestApplyUsesChangedCurrentResponseMapping(t *testing.T) {
	fix := writeApplyFixture(t)
	for _, root := range []string{fix.candidate, fix.stable} {
		writeJSONFile(t, filepath.Join(root, "ReplacementResponse.json"), map[string]any{
			"title": "ReplacementResponse", "type": "object",
			"properties": map[string]any{"result": map[string]any{"type": "string"}},
		})
	}
	common := `client_request_definitions! {
  ThreadStart => "thread/start" {
    response: ReplacementResponse,
  },
}

server_request_definitions! {}
`
	if err := os.WriteFile(fix.commonRS, []byte(common), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Apply(ApplyRequest{
		Baseline: fix.baseline, Candidate: fix.candidate, StableCandidate: fix.stable,
		CommonRS: fix.commonRS, CommonRSSourceSHA: fix.sha, TargetRef: "rust-v1.2.3",
		TargetKind: "stable_rust_tag", TargetSHA: fix.sha, SkipCodegen: true, skipSurface: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var manifest manifestFile
	if err := loadJSON(filepath.Join(fix.baseline, "manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	entry := manifest.Entries[0]
	if entry.ResponseType != "ReplacementResponse" || entry.ResponseSchema != "ReplacementResponse.json" {
		t.Fatalf("response mapping came from old manifest: %+v", entry)
	}
}

func TestApplyDerivedFactsIgnoreOldMetadata(t *testing.T) {
	type facts struct {
		entries               []manifestEntry
		classificationSources map[string]any
		methods               []map[string]any
		types                 []map[string]any
		fields                []map[string]any
	}
	derive := func(t *testing.T, stale bool) facts {
		t.Helper()
		fix := writeApplyFixture(t)
		if stale {
			manifestPath := filepath.Join(fix.baseline, "manifest.json")
			var old manifestFile
			if err := loadJSON(manifestPath, &old); err != nil {
				t.Fatal(err)
			}
			entry := &old.Entries[0]
			entry.Stability = "experimental"
			entry.ResponseType = "OldResponse"
			entry.ResponseSchema = "OldResponse.json"
			entry.FacadeTarget = "Old().Value"
			entry.SourceRef["response_mapping"] = "old"
			old.ClassificationSources = map[string]any{"response_schema": "stale accepted baseline mapping"}
			if err := writeJSON(manifestPath, old); err != nil {
				t.Fatal(err)
			}
			coveragePath := filepath.Join(fix.baseline, "coverage_matrix.json")
			var oldCoverage coverageFile
			if err := loadJSON(coveragePath, &oldCoverage); err != nil {
				t.Fatal(err)
			}
			oldCoverage.Methods = []map[string]any{{"method": "thread/start", "status": "intentionally-unsupported"}}
			for _, typ := range oldCoverage.Types {
				if typ["schema"] == "ThreadStartParams.json" {
					typ["stability"] = "experimental"
					typ["status"] = "intentionally-unsupported"
				}
			}
			oldCoverage.Fields = []map[string]any{{
				"field": "prompt", "path": "ThreadStartParams.json#/properties/prompt",
				"schema": "ThreadStartParams.json", "type": "ThreadStartParams",
				"required": true, "stability": "experimental", "status": "intentionally-unsupported",
			}}
			if err := writeJSON(coveragePath, oldCoverage); err != nil {
				t.Fatal(err)
			}
		}
		_, err := Apply(ApplyRequest{
			Baseline: fix.baseline, Candidate: fix.candidate, StableCandidate: fix.stable,
			CommonRS: fix.commonRS, CommonRSSourceSHA: fix.sha, TargetRef: "rust-v1.2.3",
			TargetKind: "stable_rust_tag", TargetSHA: fix.sha, SkipCodegen: true, skipSurface: true,
			Now: func() time.Time { return time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC) },
		})
		if err != nil {
			t.Fatal(err)
		}
		var manifest manifestFile
		if err := loadJSON(filepath.Join(fix.baseline, "manifest.json"), &manifest); err != nil {
			t.Fatal(err)
		}
		var coverage coverageFile
		if err := loadJSON(filepath.Join(fix.baseline, "coverage_matrix.json"), &coverage); err != nil {
			t.Fatal(err)
		}
		out := facts{entries: manifest.Entries, classificationSources: manifest.ClassificationSources}
		for _, method := range coverage.Methods {
			out.methods = append(out.methods, map[string]any{
				"direction": method["direction"], "kind": method["kind"], "method": method["method"],
				"source_schema": method["source_schema"], "stability": method["stability"], "status": method["status"],
			})
		}
		for _, typ := range coverage.Types {
			out.types = append(out.types, map[string]any{
				"schema": typ["schema"], "stability": typ["stability"], "status": typ["status"], "type": typ["type"],
			})
		}
		for _, field := range coverage.Fields {
			out.fields = append(out.fields, map[string]any{
				"field": field["field"], "path": field["path"], "required": field["required"],
				"schema": field["schema"], "stability": field["stability"], "status": field["status"], "type": field["type"],
			})
		}
		return out
	}
	baseline := derive(t, false)
	stale := derive(t, true)
	if !reflect.DeepEqual(baseline, stale) {
		t.Fatalf("old derived metadata changed current facts:\nnormal=%#v\nstale=%#v", baseline, stale)
	}
}

func TestApplyPreservesCurrentPresenceAndNullability(t *testing.T) {
	fix := writeApplyFixture(t)
	for _, root := range []string{fix.candidate, fix.stable} {
		writeJSONFile(t, filepath.Join(root, "ThreadStartParams.json"), map[string]any{
			"title": "ThreadStartParams", "type": "object", "required": []string{"prompt"},
			"properties": map[string]any{
				"prompt": map[string]any{"type": []string{"string", "null"}},
				"label":  map[string]any{"type": []string{"string", "null"}},
			},
		})
	}
	_, err := Apply(ApplyRequest{
		Baseline: fix.baseline, Candidate: fix.candidate, StableCandidate: fix.stable,
		CommonRS: fix.commonRS, CommonRSSourceSHA: fix.sha, TargetRef: "rust-v1.2.3",
		TargetKind: "stable_rust_tag", TargetSHA: fix.sha, SkipCodegen: true, skipSurface: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var coverage coverageFile
	if err := loadJSON(filepath.Join(fix.baseline, "coverage_matrix.json"), &coverage); err != nil {
		t.Fatal(err)
	}
	planRoot := t.TempDir()
	schemaBytes, err := os.ReadFile(filepath.Join(fix.baseline, "ThreadStartParams.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planRoot, "ThreadStartParams.json"), schemaBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	var types, fields []map[string]any
	for _, typ := range coverage.Types {
		if typ["schema"] == "ThreadStartParams.json" {
			types = append(types, typ)
		}
	}
	for _, field := range coverage.Fields {
		if field["schema"] == "ThreadStartParams.json" {
			fields = append(fields, field)
		}
	}
	writeJSONFile(t, filepath.Join(planRoot, "coverage_matrix.json"), map[string]any{
		"status": "classified-manifest", "types": types, "fields": fields,
	})
	plan, err := protocolgen.BuildProtocolTypePlan(planRoot)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		required bool
		goType   string
	}{
		"prompt": {required: true, goType: "protocolv2.Nullable[string]"},
		"label":  {required: false, goType: "*protocolv2.Nullable[string]"},
	}
	for _, field := range plan.Fields {
		if field.SchemaPath != "ThreadStartParams.json" {
			continue
		}
		expected, ok := want[field.FieldName]
		if !ok {
			continue
		}
		if field.Required != expected.required || !field.WireAllowsNull || field.GoType != expected.goType {
			t.Fatalf("%s: required=%v null=%v GoType=%s", field.FieldName, field.Required, field.WireAllowsNull, field.GoType)
		}
		delete(want, field.FieldName)
	}
	if len(want) != 0 {
		t.Fatalf("missing derived fields: %v", want)
	}
	for index := range plan.Types {
		if plan.Types[index].TypeName == "ThreadStartParams" {
			plan.Types[index].WireMessageRoles = protocolgen.WireMessageRoleActionBearingMessage
		}
	}
	generated, err := protocolgen.GenerateProtocolTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	wirePackage, err := os.MkdirTemp(filepath.Join(root, "codexsdk"), "protocol-upgrade-wire-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(wirePackage) })
	for name, contents := range map[string][]byte{
		"protocol_types.gen.go": generated,
		"presence_test.go": []byte(`package protocolv2
import (
  "encoding/json"
  "reflect"
  "testing"
)
func TestCurrentPresenceAndNullability(t *testing.T) {
  cases := []struct{ wire, roundtrip string; labelPresent bool }{
    {"{\"prompt\":null}", "{\"prompt\":null}", false},
    {"{\"prompt\":null,\"label\":null}", "{\"prompt\":null,\"label\":null}", true},
    {"{\"prompt\":\"hello\",\"label\":\"name\"}", "{\"prompt\":\"hello\",\"label\":\"name\"}", true},
  }
  for _, tc := range cases {
    var value ThreadStartParams
    if err := json.Unmarshal([]byte(tc.wire), &value); err != nil { t.Fatal(err) }
    if (value.Label != nil) != tc.labelPresent { t.Fatalf("label presence for %s", tc.wire) }
    encoded, err := json.Marshal(value)
    if err != nil { t.Fatal(err) }
    var got, want map[string]any
    if err := json.Unmarshal(encoded, &got); err != nil { t.Fatal(err) }
    if err := json.Unmarshal([]byte(tc.roundtrip), &want); err != nil { t.Fatal(err) }
    if !reflect.DeepEqual(got, want) { t.Fatalf("roundtrip %s: %s", tc.wire, encoded) }
  }
  var missing ThreadStartParams
  if err := json.Unmarshal([]byte("{}"), &missing); err == nil { t.Fatal("missing required prompt accepted") }
}
`),
	} {
		if err := os.WriteFile(filepath.Join(wirePackage, name), contents, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"nullable.go", "json_value.go"} {
		body, err := os.ReadFile(filepath.Join(root, "codexsdk", "protocolv2", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wirePackage, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "./codexsdk/"+filepath.Base(wirePackage))
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated fixture wire test: %v\n%s", err, output)
	}
}

func TestApplyUsesCurrentFieldSet(t *testing.T) {
	fix := writeApplyFixture(t)
	for _, root := range []string{fix.candidate, fix.stable} {
		writeJSONFile(t, filepath.Join(root, "ThreadStartParams.json"), map[string]any{
			"title": "ThreadStartParams", "type": "object",
			"properties": map[string]any{"message": map[string]any{"type": "string"}},
		})
	}
	coveragePath := filepath.Join(fix.baseline, "coverage_matrix.json")
	var old coverageFile
	if err := loadJSON(coveragePath, &old); err != nil {
		t.Fatal(err)
	}
	old.Fields = []map[string]any{{
		"field": "prompt", "path": "ThreadStartParams.json#/properties/prompt",
		"schema": "ThreadStartParams.json", "type": "ThreadStartParams",
		"required": false, "stability": "stable", "status": "supported-generated",
	}}
	if err := writeJSON(coveragePath, old); err != nil {
		t.Fatal(err)
	}
	_, err := Apply(ApplyRequest{
		Baseline: fix.baseline, Candidate: fix.candidate, StableCandidate: fix.stable,
		CommonRS: fix.commonRS, CommonRSSourceSHA: fix.sha, TargetRef: "rust-v1.2.3",
		TargetKind: "stable_rust_tag", TargetSHA: fix.sha, SkipCodegen: true, skipSurface: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var generated coverageFile
	if err := loadJSON(coveragePath, &generated); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, field := range generated.Fields {
		if field["schema"] == "ThreadStartParams.json" {
			got = append(got, field["field"].(string))
		}
	}
	if !reflect.DeepEqual(got, []string{"message"}) {
		t.Fatalf("current fields = %v, want message only", got)
	}
}

func TestApplyUsesCurrentFieldVisibility(t *testing.T) {
	fix := writeApplyFixture(t)
	writeJSONFile(t, filepath.Join(fix.candidate, "ThreadStartParams.json"), map[string]any{
		"title": "ThreadStartParams", "type": "object",
		"properties": map[string]any{
			"prompt":           map[string]any{"type": "string"},
			"experimentalOnly": map[string]any{"type": "string"},
		},
	})
	_, err := Apply(ApplyRequest{
		Baseline: fix.baseline, Candidate: fix.candidate, StableCandidate: fix.stable,
		CommonRS: fix.commonRS, CommonRSSourceSHA: fix.sha, TargetRef: "rust-v1.2.3",
		TargetKind: "stable_rust_tag", TargetSHA: fix.sha, SkipCodegen: true, skipSurface: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var coverage coverageFile
	if err := loadJSON(filepath.Join(fix.baseline, "coverage_matrix.json"), &coverage); err != nil {
		t.Fatal(err)
	}
	for _, field := range coverage.Fields {
		if field["path"] == "ThreadStartParams.json#/properties/experimentalOnly" {
			if field["stability"] != "experimental" {
				t.Fatalf("field stability = %v, want experimental", field["stability"])
			}
			return
		}
	}
	t.Fatal("experimentalOnly field missing")
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
	var previous manifestFile
	if err := loadJSON(filepath.Join(baseline, "manifest.json"), &previous); err != nil {
		t.Fatal(err)
	}
	candidate := t.TempDir()
	if err := copyTree(baseline, candidate); err != nil {
		t.Fatal(err)
	}
	commonRS := filepath.Join(root, "common.rs")
	writeBaselineMappingFixture(t, baseline, commonRS)
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
	var generated manifestFile
	if err := loadJSON(filepath.Join(baseline, "manifest.json"), &generated); err != nil {
		t.Fatal(err)
	}
	previousTargets := map[string]string{}
	for _, entry := range previous.Entries {
		previousTargets[entry.Method] = entry.FacadeTarget
	}
	var targetChanges []string
	for _, entry := range generated.Entries {
		if old := previousTargets[entry.Method]; old != "" && old != entry.FacadeTarget {
			targetChanges = append(targetChanges, fmt.Sprintf("%s: %s -> %s", entry.Method, old, entry.FacadeTarget))
		}
	}
	if len(targetChanges) != 0 {
		t.Fatalf("current baseline facade target changes:\n%s", strings.Join(targetChanges, "\n"))
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

// The full-baseline pipeline tests need complete mapping input. Their oracle is
// reproducibility; focused mapping tests use independent, small source fixtures.
func writeBaselineMappingFixture(t *testing.T, baseline, path string) {
	t.Helper()
	var manifest manifestFile
	if err := loadJSON(filepath.Join(baseline, "manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for _, direction := range []struct{ name, macro string }{
		{name: "client_to_server", macro: "client_request_definitions"},
		{name: "server_to_client", macro: "server_request_definitions"},
	} {
		fmt.Fprintf(&body, "%s! {\n", direction.macro)
		for _, entry := range manifest.Entries {
			if entry.Kind != "request" || entry.Direction != direction.name {
				continue
			}
			fmt.Fprintf(&body, "  %s => %q {\n    response: %s,\n  },\n", entry.SourceVariant, entry.Method, entry.ResponseType)
		}
		body.WriteString("}\n")
	}
	if err := os.WriteFile(path, []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

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
