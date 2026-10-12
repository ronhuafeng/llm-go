package schemaoracle

import (
	"strings"
	"testing"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

func TestInvalidDiagnosticSharedConstraintAndUnionAmbiguity(t *testing.T) {
	req := Request{Schemas: map[string][]byte{"Request.json": []byte(`{"definitions":{"Effort":{"type":"string","minLength":1}},"anyOf":[{"type":"null"},{"type":"object","properties":{"a/b":{"$ref":"#/definitions/Effort"},"~effort":{"$ref":"#/definitions/Effort"}},"required":["a/b","~effort"]}]}`)}, Bindings: map[string]string{"Request.json": "Payload"}, CloseObjects: true}
	targets, err := Build(req)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, p := range targets[0].Probes {
		if !p.Valid && strings.Contains(p.JSON, `:""`) && strings.Contains(p.Diagnostic, "minLength") {
			found++
			for _, want := range []string{"source=Request.json#/definitions/Effort/minLength", "minLength: got 0, want 1", `value=""`, "union sampled cause; attribution ambiguous"} {
				if !strings.Contains(p.Diagnostic, want) {
					t.Errorf("missing %q: %s", want, p.Diagnostic)
				}
			}
			if !strings.Contains(p.Diagnostic, "instance=#/a~1b") && !strings.Contains(p.Diagnostic, "instance=#/~0effort") {
				t.Fatal(p.Diagnostic)
			}
		}
	}
	if found < 2 {
		t.Fatalf("shared constraint diagnostics=%d: %+v", found, targets)
	}
}

func TestInvalidDiagnosticLabelsLocalOverlay(t *testing.T) {
	targets, err := Build(Request{Schemas: map[string][]byte{"Record.json": []byte(`{"type":"object","properties":{"field":{"type":"string"}}}`)}, Bindings: map[string]string{"Record.json": "Payload"}, CloseObjects: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range targets[0].Probes {
		if strings.Contains(p.JSON, "__oracle_unknown_field__") {
			if !strings.Contains(p.Diagnostic, "local-overlay=Record.json#/additionalProperties") {
				t.Fatal(p.Diagnostic)
			}
			return
		}
	}
	t.Fatal("missing overlay probe")
}

func TestDiagnosticValueOmitsURLAndBody(t *testing.T) {
	for _, value := range []any{"https://user:private-password@example.test/path?token=private-token", strings.Repeat("private-body-token", 100), map[string]any{"body": "private-body-token", "url": "https://private.test"}} {
		got := diagnosticValue(value)
		if strings.Contains(got, "private") || strings.Contains(got, "https") {
			t.Fatal(got)
		}
	}
}

func TestDiagnosticArrayAndBranchPointers(t *testing.T) {
	targets, err := Build(Request{Schemas: map[string][]byte{"Array.json": []byte(`{"anyOf":[{"type":"null"},{"type":"array","items":{"type":"string","minLength":1},"minItems":1}]}`)}, Bindings: map[string]string{"Array.json": "Payload"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range targets[0].Probes {
		if p.JSON == `[""]` {
			for _, want := range []string{"source=Array.json#/anyOf/1/items/minLength", "instance=#/0", `value=""`} {
				if !strings.Contains(p.Diagnostic, want) {
					t.Fatal(p.Diagnostic)
				}
			}
			return
		}
	}
	t.Fatal("missing array mutation")
}

func TestDiagnosticForeignURLAndTraversalFallback(t *testing.T) {
	leaf := &validator.ValidationError{SchemaURL: "https://user:private-secret@example.test/body?token=private-token#", ErrorKind: &kind.MinLength{Got: 0, Want: 1}}
	got := invalidDiagnostic(nil, "Item.json", "", "#", leaf)
	if strings.Contains(got, "private") || strings.Contains(got, "https") || !strings.Contains(got, "attribution ambiguous") {
		t.Fatal(got)
	}
	leaf.SchemaURL = "https://schema-oracle.invalid/target.json#"
	root := &validator.ValidationError{ErrorKind: &kind.AnyOf{}}
	for i := 0; i < 20001; i++ {
		root.Causes = append(root.Causes, leaf)
	}
	got = invalidDiagnostic(nil, "Item.json", "", "#", root)
	if !strings.Contains(got, "diagnostic traversal truncated") || len(got) > 256 {
		t.Fatal(got)
	}
}

func TestDiagnosticNativeMessagesOmitURLAndBody(t *testing.T) {
	secret := "https://user:private-token@example.test/?body=private-body"
	for _, errorKind := range []validator.ErrorKind{&kind.Enum{Got: secret, Want: []any{secret}}, &kind.Const{Got: secret, Want: secret}, &kind.Format{Got: secret, Want: secret}} {
		leaf := &validator.ValidationError{SchemaURL: "https://schema-oracle.invalid/target.json#", ErrorKind: errorKind}
		got := invalidDiagnostic(map[string]any{}, "Item.json", secret, "#", leaf)
		if strings.Contains(got, "private") || strings.Contains(got, "https") || !strings.Contains(got, "value=<string bytes=") {
			t.Fatal(got)
		}
	}
}

func TestDiagnosticSnapshotPreservesJSONNumbers(t *testing.T) {
	// Annotation numbers need not fit float64. The diagnostic snapshot must not
	// narrow the authoritative UseNumber decode or change probe acceptance.
	targets, err := Build(Request{Schemas: map[string][]byte{"Number.json": []byte(`{"type":"string","default":1e400}`)}, Bindings: map[string]string{"Number.json": "Payload"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || len(targets[0].Probes) == 0 {
		t.Fatal("missing probes")
	}
}

func TestDiagnosticSampleIsIndependentOfCauseOrder(t *testing.T) {
	first := &validator.ValidationError{SchemaURL: "https://schema-oracle.invalid/target.json#/anyOf/0", ErrorKind: &kind.MinLength{Got: 0, Want: 1}}
	second := &validator.ValidationError{SchemaURL: "https://schema-oracle.invalid/target.json#/anyOf/1", ErrorKind: &kind.MinLength{Got: 0, Want: 2}}
	original := map[string]any{"anyOf": []any{map[string]any{"minLength": 1}, map[string]any{"minLength": 2}}}
	root := &validator.ValidationError{ErrorKind: &kind.AnyOf{}, Causes: []*validator.ValidationError{first, second}}
	a := invalidDiagnostic(original, "Item.json", "", "#", root)
	root.Causes = []*validator.ValidationError{second, first}
	b := invalidDiagnostic(original, "Item.json", "", "#", root)
	if a != b || !strings.Contains(a, "#/anyOf/0/minLength") || !strings.Contains(a, "attribution ambiguous") {
		t.Fatalf("order-dependent sample: %s / %s", a, b)
	}
}
