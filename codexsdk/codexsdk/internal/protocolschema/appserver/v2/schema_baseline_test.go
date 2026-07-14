package v2

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ronhuafeng/codexsdk-go/codexsdk/internal/protocolgen"
)

var metadataFiles = map[string]bool{
	"baseline_metadata.json":      true,
	"coverage_matrix.json":        true,
	"drift_report.json":           true,
	"manifest.json":               true,
	"manifest_generation.json":    true,
	"matrix_update_skeleton.json": true,
}

type manifestFile struct {
	AggregateSchemas []string                   `json:"aggregate_schemas"`
	Entries          []manifestEntry            `json:"entries"`
	SchemaVersion    int                        `json:"schema_version"`
	Status           string                     `json:"status"`
	Surface          []protocolgen.SurfaceEntry `json:"surface"`
}

type manifestEntry struct {
	FacadeTarget          string            `json:"facade_target"`
	FacadeStatus          string            `json:"facade_status"`
	Family                string            `json:"family"`
	Direction             string            `json:"direction"`
	Kind                  string            `json:"kind"`
	Method                string            `json:"method"`
	ParamsOrPayloadSchema string            `json:"params_or_payload_schema"`
	ResponseSchema        string            `json:"response_schema"`
	ResponseSchemaStatus  string            `json:"response_schema_status"`
	SchemaTitle           string            `json:"schema_title"`
	SourceSchema          string            `json:"source_schema"`
	SourceRef             map[string]string `json:"source_ref"`
	Stability             string            `json:"stability"`
	StabilitySource       string            `json:"stability_source"`
}

type coverageFile struct {
	Methods       []coverageMethod `json:"methods"`
	Types         []coverageType   `json:"types"`
	Fields        []coverageField  `json:"fields"`
	Status        string           `json:"status"`
	ValidStatuses []string         `json:"valid_statuses"`
}

type coverageMethod struct {
	Direction      string `json:"direction"`
	ExitCondition  string `json:"exit_condition"`
	Kind           string `json:"kind"`
	Method         string `json:"method"`
	Owner          string `json:"owner"`
	Reason         string `json:"reason"`
	RevisitTrigger string `json:"revisit_trigger"`
	Stability      string `json:"stability"`
	Status         string `json:"status"`
}

type coverageType struct {
	ExitCondition  string `json:"exit_condition"`
	Owner          string `json:"owner"`
	Reason         string `json:"reason"`
	RevisitTrigger string `json:"revisit_trigger"`
	Schema         string `json:"schema"`
	Stability      string `json:"stability"`
	Status         string `json:"status"`
	Type           string `json:"type"`
}

type coverageField struct {
	Field          string `json:"field"`
	ExitCondition  string `json:"exit_condition"`
	Owner          string `json:"owner"`
	Path           string `json:"path"`
	Reason         string `json:"reason"`
	RevisitTrigger string `json:"revisit_trigger"`
	Schema         string `json:"schema"`
	Stability      string `json:"stability"`
	Status         string `json:"status"`
	Type           string `json:"type"`
}

func TestBaselineMetadataIsTraceable(t *testing.T) {
	var metadata struct {
		AggregateSchemas     []string `json:"aggregate_schemas"`
		CodexBinary          string   `json:"codex_binary"`
		CodexVersion         string   `json:"codex_version"`
		ExperimentalIncluded bool     `json:"experimental_included"`
		GeneratedAt          string   `json:"generated_at"`
		GenerationCommand    string   `json:"generation_command"`
		SchemaBundleSHA256   string   `json:"schema_bundle_sha256"`
		SchemaFileCount      int      `json:"schema_file_count"`
		SourceCommit         string   `json:"source_commit"`
		SourceLicense        string   `json:"source_license"`
		SourceRefURL         string   `json:"source_ref_url"`
		SourceRepo           string   `json:"source_repo"`
		SourceSubdir         string   `json:"source_subdir"`
	}
	readJSON(t, "baseline_metadata.json", &metadata)

	if !metadata.ExperimentalIncluded {
		t.Fatal("baseline metadata must record experimental_included=true")
	}
	if !strings.Contains(metadata.GenerationCommand, "generate-json-schema --experimental") {
		t.Fatalf("generation command does not record --experimental: %q", metadata.GenerationCommand)
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(metadata.SourceCommit) {
		t.Fatalf("source commit is not a full git sha: %q", metadata.SourceCommit)
	}
	if metadata.CodexBinary == "" || metadata.CodexVersion == "" || metadata.GeneratedAt == "" || metadata.SourceRepo == "" {
		t.Fatalf("metadata missing traceability fields: %#v", metadata)
	}
	if filepath.IsAbs(metadata.CodexBinary) || filepath.IsAbs(metadata.SourceRepo) {
		t.Fatalf("metadata must use public provenance, not local absolute paths: %#v", metadata)
	}
	if metadata.SourceRepo != "https://github.com/openai/codex" || metadata.SourceLicense != "Apache-2.0" || !strings.Contains(metadata.SourceRefURL, metadata.SourceCommit) {
		t.Fatalf("metadata missing public upstream provenance: %#v", metadata)
	}
	if metadata.SourceSubdir != "codex-rs/app-server-protocol" {
		t.Fatalf("metadata source_subdir = %q, want codex-rs/app-server-protocol", metadata.SourceSubdir)
	}
	if metadata.SchemaFileCount != len(schemaFiles(t)) {
		t.Fatalf("schema_file_count = %d, want %d", metadata.SchemaFileCount, len(schemaFiles(t)))
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(metadata.SchemaBundleSHA256) {
		t.Fatalf("schema bundle checksum is not sha256: %q", metadata.SchemaBundleSHA256)
	}
}

func TestManifestClassifiedMatchesAggregateSchemas(t *testing.T) {
	var manifest manifestFile
	readJSON(t, "manifest.json", &manifest)
	if manifest.Status != "classified-manifest" {
		t.Fatalf("manifest status = %q, want classified-manifest", manifest.Status)
	}

	expectedAggregates := []string{
		"ClientRequest.json",
		"ServerRequest.json",
		"ServerNotification.json",
		"ClientNotification.json",
	}
	if got := append([]string(nil), manifest.AggregateSchemas...); !sameStrings(got, expectedAggregates) {
		t.Fatalf("aggregate schemas = %#v, want %#v", got, expectedAggregates)
	}

	wantMethods := map[string]string{}
	for _, aggregate := range expectedAggregates {
		for _, entry := range aggregateEntries(t, aggregate) {
			wantMethods[entry.Method] = entry.ParamsOrPayloadSchema
		}
	}
	gotMethods := map[string]bool{}
	for _, entry := range manifest.Entries {
		if entry.Method == "" || entry.Direction == "" || entry.Kind == "" || entry.Family == "" || entry.SchemaTitle == "" || entry.SourceSchema == "" {
			t.Fatalf("manifest entry missing required fact: %#v", entry)
		}
		if entry.FacadeTarget == "" {
			t.Fatalf("manifest entry %q missing facade target", entry.Method)
		}
		if entry.Direction == "client_to_server" && entry.Kind == "request" {
			if strings.HasPrefix(entry.FacadeTarget, "internal.") {
				// Explicit internal operations do not belong to a generated facade.
			} else if !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*\(\)\.[A-Za-z][A-Za-z0-9]*$`).MatchString(entry.FacadeTarget) {
				t.Fatalf("manifest entry %q has invalid facade_target %q", entry.Method, entry.FacadeTarget)
			} else {
				switch entry.FacadeStatus {
				case "generated", "deferred_missing_generated_types":
				default:
					t.Fatalf("manifest entry %q has invalid facade_status %q", entry.Method, entry.FacadeStatus)
				}
			}
		}
		if !validStability(entry.Stability) || entry.StabilitySource == "" {
			t.Fatalf("manifest entry %q has invalid stability classification: %#v", entry.Method, entry)
		}
		if entry.SourceRef["aggregate_pointer"] == "" || entry.SourceRef["baseline_source_commit"] == "" || entry.SourceRef["facade_rule"] == "" || entry.SourceRef["stability"] == "" {
			t.Fatalf("manifest entry %q missing source refs: %#v", entry.Method, entry.SourceRef)
		}
		switch entry.Kind {
		case "request":
			if entry.ResponseSchema == "" || entry.ResponseSchemaStatus != "declared" {
				t.Fatalf("request %q missing declared response schema: %#v", entry.Method, entry)
			}
			if !schemaExists(entry.ResponseSchema) {
				t.Fatalf("request %q response schema %q does not exist", entry.Method, entry.ResponseSchema)
			}
			if entry.SourceRef["response_mapping"] == "" || strings.HasPrefix(entry.SourceRef["response_mapping"], "not_applicable") {
				t.Fatalf("request %q missing response mapping source ref: %#v", entry.Method, entry.SourceRef)
			}
		case "notification":
			if entry.ResponseSchema != "" || entry.ResponseSchemaStatus != "not_applicable" {
				t.Fatalf("notification %q should have response_schema_status=not_applicable: %#v", entry.Method, entry)
			}
		default:
			t.Fatalf("manifest entry %q has unknown kind %q", entry.Method, entry.Kind)
		}
		if wantRef := wantMethods[entry.Method]; wantRef != "" && entry.ParamsOrPayloadSchema != wantRef {
			t.Fatalf("manifest entry %q params ref = %q, want %q", entry.Method, entry.ParamsOrPayloadSchema, wantRef)
		}
		gotMethods[entry.Method] = true
	}
	if len(gotMethods) != len(wantMethods) {
		t.Fatalf("manifest method count = %d, want %d", len(gotMethods), len(wantMethods))
	}
	for method := range wantMethods {
		if !gotMethods[method] {
			t.Fatalf("manifest missing method %q", method)
		}
	}
	assertManifestEntry(t, manifest.Entries, "turn/start", "stable", "v2/TurnStartResponse.json", "Turns().Start")
	assertManifestEntry(t, manifest.Entries, "thread/realtime/start", "experimental", "v2/ThreadRealtimeStartResponse.json", "Threads().RealtimeStart")
}

func TestManifestClassifiesEveryGeneratedProtocolExport(t *testing.T) {
	var manifest manifestFile
	readJSON(t, "manifest.json", &manifest)
	if manifest.SchemaVersion < 2 {
		t.Fatalf("manifest schema_version = %d, want at least 2", manifest.SchemaVersion)
	}
	root := filepath.Join("..", "..", "..", "..", "protocolv2")
	var sources [][]byte
	for _, name := range []string{"method_registry.gen.go", "protocol_types.gen.go"} {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, raw)
	}
	if err := protocolgen.ValidateSurface(manifest.Surface); err != nil {
		t.Fatal(err)
	}
	if err := protocolgen.VerifyExportedPackage(sources, manifest.Surface); err != nil {
		t.Fatal(err)
	}
}

func TestCoverageMatrixClassifiesBaselineSurface(t *testing.T) {
	var manifest manifestFile
	readJSON(t, "manifest.json", &manifest)
	var coverage coverageFile
	readJSON(t, "coverage_matrix.json", &coverage)
	if coverage.Status != "classified-manifest" {
		t.Fatalf("coverage status = %q, want classified-manifest", coverage.Status)
	}
	valid := map[string]bool{}
	for _, status := range coverage.ValidStatuses {
		valid[status] = true
	}
	for _, status := range []string{"supported", "supported-generated", "deferred", "intentionally-unsupported"} {
		if !valid[status] {
			t.Fatalf("coverage valid statuses missing %q", status)
		}
	}

	methods := map[string]bool{}
	stableDeferredMethods := []string{}
	for _, method := range coverage.Methods {
		if !valid[method.Status] {
			t.Fatalf("method %q has invalid status %q", method.Method, method.Status)
		}
		if !validStability(method.Stability) || method.Reason == "" || method.ExitCondition == "" || method.Owner == "" || method.RevisitTrigger == "" {
			t.Fatalf("classified manifest method is missing explicit review metadata: %#v", method)
		}
		if method.Stability == "stable" && method.Status == "deferred" {
			stableDeferredMethods = append(stableDeferredMethods, method.Method)
		}
		methods[method.Method] = true
	}
	for _, entry := range manifest.Entries {
		if !methods[entry.Method] {
			t.Fatalf("coverage matrix missing method %q", entry.Method)
		}
	}
	if len(stableDeferredMethods) != 0 {
		sort.Strings(stableDeferredMethods)
		t.Fatalf("stable deferred methods = %#v, want none", stableDeferredMethods)
	}

	types := map[string]bool{}
	stableDeferredTypes := []string{}
	for _, typ := range coverage.Types {
		if !valid[typ.Status] {
			t.Fatalf("type %q has invalid status %q", typ.Schema, typ.Status)
		}
		if !validStability(typ.Stability) || typ.Reason == "" || typ.ExitCondition == "" || typ.Owner == "" || typ.RevisitTrigger == "" {
			t.Fatalf("classified manifest type is missing explicit review metadata: %#v", typ)
		}
		if typ.Stability == "stable" && typ.Status == "deferred" {
			stableDeferredTypes = append(stableDeferredTypes, typ.Schema)
		}
		types[typ.Schema] = true
	}
	for _, schema := range schemaFiles(t) {
		if !types[schema] {
			t.Fatalf("coverage matrix missing schema type %q", schema)
		}
	}
	if len(stableDeferredTypes) != 0 {
		sort.Strings(stableDeferredTypes)
		t.Fatalf("stable deferred types = %#v, want none", stableDeferredTypes)
	}

	fields := map[string]bool{}
	stableDeferredFields := []string{}
	for _, field := range coverage.Fields {
		if !valid[field.Status] {
			t.Fatalf("field %q has invalid status %q", field.Path, field.Status)
		}
		if !validStability(field.Stability) || field.Reason == "" || field.ExitCondition == "" || field.Owner == "" || field.RevisitTrigger == "" {
			t.Fatalf("classified manifest field is missing explicit review metadata: %#v", field)
		}
		if field.Stability == "stable" && field.Status == "deferred" {
			stableDeferredFields = append(stableDeferredFields, field.Path)
		}
		fields[field.Path] = true
	}
	for _, path := range []string{
		"v2/TurnStartParams.json#/properties/outputSchema",
		"v2/TurnStartParams.json#/properties/serviceTier",
	} {
		if !fields[path] {
			t.Fatalf("coverage matrix missing key field %q", path)
		}
	}
	if len(stableDeferredFields) != 0 {
		sort.Strings(stableDeferredFields)
		t.Fatalf("stable deferred fields = %#v, want none", stableDeferredFields)
	}
}

func TestDriftReportAndMatrixUpdateSkeleton(t *testing.T) {
	var drift struct {
		Status         string `json:"status"`
		ComparisonMode string `json:"comparison_mode"`
		FileDiff       struct {
			Added   []string `json:"added"`
			Changed []string `json:"changed"`
			Removed []string `json:"removed"`
		} `json:"file_diff"`
		MethodDiff map[string]struct {
			Added   []string `json:"added"`
			Removed []string `json:"removed"`
		} `json:"method_diff"`
		MatrixUpdateSkeleton string `json:"matrix_update_skeleton"`
	}
	readJSON(t, "drift_report.json", &drift)
	if drift.Status != "clean" || drift.ComparisonMode != "canonical-json" || drift.MatrixUpdateSkeleton != "matrix_update_skeleton.json" {
		t.Fatalf("unexpected drift report summary: %#v", drift)
	}
	if len(drift.FileDiff.Added) != 0 || len(drift.FileDiff.Changed) != 0 || len(drift.FileDiff.Removed) != 0 {
		t.Fatalf("drift report contains file diff: %#v", drift.FileDiff)
	}
	for schema, diff := range drift.MethodDiff {
		if len(diff.Added) != 0 || len(diff.Removed) != 0 {
			t.Fatalf("drift report contains method diff for %s: %#v", schema, diff)
		}
	}

	var update struct {
		FieldUpdates  []any    `json:"field_updates"`
		MethodUpdates []any    `json:"method_updates"`
		Status        string   `json:"status"`
		TypeUpdates   []any    `json:"type_updates"`
		ValidStatuses []string `json:"valid_statuses"`
	}
	readJSON(t, "matrix_update_skeleton.json", &update)
	if update.Status != "empty" {
		t.Fatalf("matrix update skeleton status = %q, want empty", update.Status)
	}
	if len(update.MethodUpdates) != 0 || len(update.TypeUpdates) != 0 || len(update.FieldUpdates) != 0 {
		t.Fatalf("matrix update skeleton should be empty for clean drift: %#v", update)
	}
	for _, status := range update.ValidStatuses {
		if status == "passthrough" || status == "raw" {
			t.Fatalf("matrix update skeleton exposes forbidden status %q", status)
		}
	}
}

func TestLocalCodexSchemaDrift(t *testing.T) {
	if os.Getenv("CODEXSDK_RUN_DRIFT_TEST") != "1" {
		t.Skip("set CODEXSDK_RUN_DRIFT_TEST=1 to compare checked-in baseline with local codex")
	}
	codex, err := exec.LookPath("codex")
	if err != nil {
		t.Fatalf("CODEXSDK_RUN_DRIFT_TEST=1 but codex is unavailable: %v", err)
	}
	out := t.TempDir()
	cmd := exec.Command(codex, "app-server", "generate-json-schema", "--experimental", "--out", out)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate local app-server schema: %v\n%s", err, output)
	}
	diff := compareSchemaDirs(t, ".", out)
	if !diff.clean() {
		t.Fatalf("local codex schema drift detected:\n%s", diff.String())
	}
}

func aggregateEntries(t *testing.T, path string) []manifestEntry {
	t.Helper()
	var schema struct {
		OneOf []struct {
			Properties struct {
				Method struct {
					Enum []string `json:"enum"`
				} `json:"method"`
				Params struct {
					Ref string `json:"$ref"`
				} `json:"params"`
			} `json:"properties"`
		} `json:"oneOf"`
	}
	readJSON(t, path, &schema)
	entries := make([]manifestEntry, 0, len(schema.OneOf))
	for _, variant := range schema.OneOf {
		if len(variant.Properties.Method.Enum) == 0 {
			continue
		}
		entries = append(entries, manifestEntry{
			Method:                variant.Properties.Method.Enum[0],
			ParamsOrPayloadSchema: refName(variant.Properties.Params.Ref),
		})
	}
	return entries
}

func readJSON(t *testing.T, path string, dst any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func schemaExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(filepath.FromSlash(path))
	return err == nil && !info.IsDir()
}

func schemaFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry == nil || entry.IsDir() || filepath.Ext(path) != ".json" || metadataFiles[filepath.Base(path)] {
			return nil
		}
		files = append(files, filepath.ToSlash(path))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	return files
}

func validStability(stability string) bool {
	return stability == "stable" || stability == "experimental"
}

func assertManifestEntry(t *testing.T, entries []manifestEntry, method, stability, responseSchema, facadeTarget string) {
	t.Helper()
	for _, entry := range entries {
		if entry.Method != method {
			continue
		}
		if entry.Stability != stability || entry.ResponseSchema != responseSchema || entry.FacadeTarget != facadeTarget {
			t.Fatalf("manifest entry %q = stability %q response %q facade %q, want %q %q %q", method, entry.Stability, entry.ResponseSchema, entry.FacadeTarget, stability, responseSchema, facadeTarget)
		}
		return
	}
	t.Fatalf("manifest missing method %q", method)
}

type schemaDirDiff struct {
	added   []string
	changed []string
	removed []string
}

func (diff schemaDirDiff) clean() bool {
	return len(diff.added) == 0 && len(diff.changed) == 0 && len(diff.removed) == 0
}

func (diff schemaDirDiff) String() string {
	return fmt.Sprintf("added=%v\nchanged=%v\nremoved=%v", diff.added, diff.changed, diff.removed)
}

func compareSchemaDirs(t *testing.T, baseline, candidate string) schemaDirDiff {
	t.Helper()
	baseHashes := canonicalJSONHashes(t, baseline)
	candidateHashes := canonicalJSONHashes(t, candidate)
	diff := schemaDirDiff{}
	for path := range candidateHashes {
		if _, ok := baseHashes[path]; !ok {
			diff.added = append(diff.added, path)
		}
	}
	for path := range baseHashes {
		if _, ok := candidateHashes[path]; !ok {
			diff.removed = append(diff.removed, path)
			continue
		}
		if baseHashes[path] != candidateHashes[path] {
			diff.changed = append(diff.changed, path)
		}
	}
	sort.Strings(diff.added)
	sort.Strings(diff.changed)
	sort.Strings(diff.removed)
	return diff
}

func canonicalJSONHashes(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	hashes := map[string][32]byte{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry == nil || entry.IsDir() || filepath.Ext(path) != ".json" || metadataFiles[filepath.Base(path)] {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		canonical, err := json.Marshal(value)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		hashes[filepath.ToSlash(rel)] = sha256.Sum256(canonical)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hashes
}

func refName(ref string) string {
	if ref == "" {
		return ""
	}
	parts := strings.Split(ref, "/")
	return parts[len(parts)-1]
}

func sameStrings(got, want []string) bool {
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
