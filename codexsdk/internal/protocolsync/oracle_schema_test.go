package protocolsync

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOracleCoverageFollowsSourceChangesAndReferences(t *testing.T) {
	old, next, generated := t.TempDir(), t.TempDir(), t.TempDir()
	before := `{"title":"Envelope","type":"object","properties":{"child":{"$ref":"#/definitions/Child"}},"required":["child"],"definitions":{"Child":{"type":"object","properties":{"old":{"type":"boolean"}},"required":["old"]}}}`
	after := strings.ReplaceAll(before, `"old"`, `"new"`)
	for _, p := range []struct{ root, raw string }{{old, before}, {next, after}} {
		if e := os.MkdirAll(filepath.Join(p.root, "v2"), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(p.root, "v2/Envelope.json"), []byte(p.raw), 0600); e != nil {
			t.Fatal(e)
		}
	}
	req, err := schemaOracleRequest(old, next, generated)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Bindings) != 2 || req.Bindings["v2/Envelope.json"] != "Envelope" || req.Bindings["v2/Envelope.json#/definitions/Child"] != "Child" {
		t.Fatalf("coverage=%v", req.Bindings)
	}
	// Proposed output cannot erase its own missing target from coverage.
	if e := os.WriteFile(filepath.Join(generated, "protocol_types.gen.go"), []byte("package sample\n"), 0600); e != nil {
		t.Fatal(e)
	}
	again, err := schemaOracleRequest(old, next, generated)
	if err != nil || len(again.Bindings) != 2 {
		t.Fatalf("candidate selected coverage: %v %v", again.Bindings, err)
	}
}
func TestOracleRejectsRemovedGeneratedType(t *testing.T) {
	old, next, generated := t.TempDir(), t.TempDir(), t.TempDir()
	for _, p := range []string{old, next} {
		if e := os.MkdirAll(filepath.Join(p, "v2"), 0700); e != nil {
			t.Fatal(e)
		}
		writeFile(t, filepath.Join(p, "v2/Kept.json"), `{"title":"Kept","type":"object","properties":{"ok":{"type":"boolean"}}}`)
	}
	writeFile(t, filepath.Join(old, "v2/Removed.json"), `{"title":"Removed","type":"object","properties":{"old":{"type":"boolean"}}}`)
	writeFile(t, filepath.Join(generated, "protocol_types.gen.go"), "package sample\ntype Removed struct { Old bool }\n")
	if _, err := schemaOracleRequest(old, next, generated); err == nil || !strings.Contains(err.Error(), "removed source type") {
		t.Fatalf("got %v", err)
	}
}

func TestOracleTestCompletionCannotBeReplacedByExitZero(t *testing.T) {
	for _, tc := range []struct {
		events string
		want   bool
	}{
		{`{"Action":"pass","Package":"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracleprobe"}` + "\n", false},
		{`{"Action":"run","Package":"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracleprobe","Test":"TestTrustedSchemaOracle"}` + "\n" + `{"Action":"pass","Package":"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracleprobe","Test":"TestTrustedSchemaOracle"}` + "\n", true},
		{`{"Action":"output","Package":"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracleprobe","Output":"--- PASS: TestTrustedSchemaOracle"}` + "\n", false},
	} {
		var events oracleTestEvents
		for _, p := range []byte(tc.events) {
			_, _ = events.Write([]byte{p})
		}
		if got := events.started && events.passed && !events.failed && !events.invalid; got != tc.want {
			t.Fatalf("completion=%t want=%t", got, tc.want)
		}
	}
}

func TestOracleCoverageIncludesRootV1AndFutureSchemaDirectories(t *testing.T) {
	for _, rel := range []string{"RefreshResponse.json", "v1/InitializeResponse.json", "v3/FutureResponse.json"} {
		t.Run(rel, func(t *testing.T) {
			old, next := t.TempDir(), t.TempDir()
			before := `{"title":"Payload","type":"object","properties":{"value":{"type":"string"}}}`
			after := strings.Replace(before, `"type":"string"`, `"type":"boolean"`, 1)
			for _, p := range []struct{ root, raw string }{{old, before}, {next, after}} {
				writeFile(t, filepath.Join(p.root, rel), p.raw)
			}
			request, err := schemaOracleRequest(old, next, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if request.Bindings[rel] != "Payload" {
				t.Fatalf("raw source excluded: %v", request.Bindings)
			}
		})
	}
}

func TestOracleShapePreservesAnnotationNamedPropertiesAndLiteralValues(t *testing.T) {
	for _, field := range []string{"description", "title", "definitions", "$schema", "$comment"} {
		t.Run(field, func(t *testing.T) {
			old, next := t.TempDir(), t.TempDir()
			before := fmt.Sprintf(`{"title":"Payload","type":"object","properties":{%q:{"type":"string"}}}`, field)
			after := strings.Replace(before, `"type":"string"`, `"type":"boolean"`, 1)
			writeFile(t, filepath.Join(old, "Payload.json"), before)
			writeFile(t, filepath.Join(next, "Payload.json"), after)
			request, err := schemaOracleRequest(old, next, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if request.Bindings["Payload.json"] != "Payload" {
				t.Fatalf("property %q disappeared from source comparison", field)
			}
		})
	}
	var a, b any
	if err := json.Unmarshal([]byte(`{"const":{"description":"before","title":"data"}}`), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"const":{"description":"after","title":"data"}}`), &b); err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(oracleShape(a), oracleShape(b)) {
		t.Fatal("literal JSON mistaken for annotations")
	}
}

func TestOracleReferenceNamedDefinitionsIsNotAContainer(t *testing.T) {
	old, next := t.TempDir(), t.TempDir()
	before := `{"title":"Envelope","type":"object","properties":{"definitions":{"$ref":"#/definitions/Child"}},"definitions":{"Child":{"type":"object","properties":{"value":{"type":"string"}}}}}`
	after := strings.Replace(before, `"type":"string"`, `"type":"boolean"`, 1)
	writeFile(t, filepath.Join(old, "Envelope.json"), before)
	writeFile(t, filepath.Join(next, "Envelope.json"), after)
	request, err := schemaOracleRequest(old, next, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Bindings) != 2 || request.Bindings["Envelope.json"] != "Envelope" {
		t.Fatalf("reference through definitions property lost: %v", request.Bindings)
	}
	refs := map[string]bool{}
	var doc any
	_ = json.Unmarshal([]byte(`{"const":{"$ref":"#/definitions/NotASchema"},"properties":{"$ref":{"type":"string"}}}`), &doc)
	oracleRefs(doc, refs)
	if len(refs) != 0 {
		t.Fatalf("literal data treated as schema refs: %v", refs)
	}
}

func TestOracleContractErrorsAreNotCandidateRepairEvidence(t *testing.T) {
	assertContract := func(err error) {
		t.Helper()
		var contract *oracleContractError
		if !errors.As(err, &contract) {
			t.Fatalf("expected trusted oracle contract error, got %v", err)
		}
	}
	t.Run("read", func(t *testing.T) {
		_, err := schemaOracleRequest(filepath.Join(t.TempDir(), "missing"), t.TempDir(), t.TempDir())
		assertContract(err)
	})
	t.Run("ambiguous", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "Root.json"), `{"title":"Duplicate","type":"object","properties":{"value":{"type":"string"}}}`)
		writeFile(t, filepath.Join(root, "v1/Root.json"), `{"title":"Duplicate","type":"object","properties":{"value":{"type":"boolean"}}}`)
		_, _, err := oracleSchemaFacts(root)
		assertContract(err)
	})
	t.Run("unbound_inline", func(t *testing.T) {
		old, next := t.TempDir(), t.TempDir()
		writeFile(t, filepath.Join(old, "Alias.json"), `{"title":"Alias","type":"string"}`)
		writeFile(t, filepath.Join(next, "Alias.json"), `{"title":"Alias","type":"boolean"}`)
		_, err := schemaOracleRequest(old, next, t.TempDir())
		assertContract(err)
	})
	t.Run("transport_binding", func(t *testing.T) {
		old, next := t.TempDir(), t.TempDir()
		writeFile(t, filepath.Join(old, "JSONRPCRequest.json"), `{"title":"JSONRPCRequest","type":"object","properties":{"value":{"type":"string"}}}`)
		writeFile(t, filepath.Join(next, "JSONRPCRequest.json"), `{"title":"JSONRPCRequest","type":"object","properties":{"value":{"type":"boolean"}}}`)
		_, err := schemaOracleRequest(old, next, t.TempDir())
		assertContract(err)
	})
	t.Run("candidate_declaration_retained", func(t *testing.T) {
		old, next, generated := t.TempDir(), t.TempDir(), t.TempDir()
		for _, root := range []string{old, next} {
			writeFile(t, filepath.Join(root, "Kept.json"), `{"title":"Kept","type":"object","properties":{}}`)
		}
		writeFile(t, filepath.Join(old, "Removed.json"), `{"title":"Removed","type":"object","properties":{}}`)
		writeFile(t, filepath.Join(generated, "types.gen.go"), "package protocolv2;type Removed struct{}")
		_, err := schemaOracleRequest(old, next, generated)
		var contract *oracleContractError
		if err == nil || errors.As(err, &contract) {
			t.Fatalf("residual candidate declaration misclassified: %v", err)
		}
	})
}

func TestOracleSelectionDoesNotIgnoreNestedDefinitionsOrDialect(t *testing.T) {
	t.Run("nested definitions fail explicitly", func(t *testing.T) {
		old, next, generated := t.TempDir(), t.TempDir(), t.TempDir()
		raw := `{"title":"Envelope","type":"object","properties":{"outer":{"$ref":"#/definitions/Outer"}},"definitions":{"Outer":{"type":"object","properties":{"inner":{"$ref":"#/definitions/Outer/definitions/Inner"}},"definitions":{"Inner":{"type":"string"}}}}}`
		writeFile(t, filepath.Join(old, "Envelope.json"), raw)
		writeFile(t, filepath.Join(next, "Envelope.json"), strings.ReplaceAll(raw, `"type":"string"`, `"type":"boolean"`))
		_, err := schemaOracleRequest(old, next, generated)
		var contract *oracleContractError
		if !errors.As(err, &contract) || !strings.Contains(err.Error(), "Envelope.json/definitions/Outer/definitions") {
			t.Fatalf("nested definition silently ignored: %v", err)
		}
	})
	t.Run("dialect selects dependent contracts", func(t *testing.T) {
		old, next, generated := t.TempDir(), t.TempDir(), t.TempDir()
		raw := `{"$schema":"http://json-schema.org/draft-07/schema#","title":"Envelope","type":"object","properties":{"value":{"$ref":"#/definitions/Child"}},"definitions":{"Child":{"type":"object","properties":{"value":{"type":"string"}}}}}`
		writeFile(t, filepath.Join(old, "Envelope.json"), raw)
		writeFile(t, filepath.Join(next, "Envelope.json"), strings.ReplaceAll(raw, "http://json-schema.org/draft-07/schema#", "https://json-schema.org/draft/2020-12/schema"))
		req, err := schemaOracleRequest(old, next, generated)
		if err != nil || len(req.Bindings) != 2 {
			t.Fatalf("dialect ignored: %v %v", req.Bindings, err)
		}
	})
}
