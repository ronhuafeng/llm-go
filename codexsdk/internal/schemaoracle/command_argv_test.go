package schemaoracle

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

const commandArgvRecord = `{"type":"object","properties":{"command":{"type":"array","items":{"type":"string"},"description":"Command argv vector. Empty arrays are rejected."},"other":{"type":"array","items":{"type":"string"}}},"required":["command"]}`

// Source identity, not the Go binding or an arbitrary field named command,
// selects the overlay. Exercise roots, selected definitions and aggregate users.
func TestCommandArgvOverlayLocations(t *testing.T) {
	for _, location := range []string{"root", "definitions", "$defs", "aggregate", "unrelated"} {
		t.Run(location, func(t *testing.T) {
			name, schema, fragment := "v2/CommandExecParams.json", commandArgvRecord, ""
			switch location {
			case "definitions", "$defs":
				name, fragment = "ClientRequest.json", "#/"+location+"/CommandExecParams"
				schema = `{"` + location + `":{"CommandExecParams":` + commandArgvRecord + `}}`
			case "aggregate":
				name = "ClientRequest.json"
				schema = `{"definitions":{"CommandExecParams":` + commandArgvRecord + `},"oneOf":[{"type":"object","properties":{"method":{"const":"command/exec"},"params":{"$ref":"#/definitions/CommandExecParams"}},"required":["method","params"]},{"type":"object","properties":{"method":{"const":"other"},"params":` + commandArgvRecord + `},"required":["method","params"]}]}`
			case "unrelated":
				name = "Tool.json"
			}
			raw := []byte(schema)
			targets, err := Build(Request{Schemas: map[string][]byte{name: raw}, Bindings: map[string]string{name + fragment: "CommandExecParams"}, CloseObjects: true})
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != schema {
				t.Fatal("source bytes changed")
			}
			seenEmpty, seenString, seenNullItem, seenOther, seenUnrelatedEmpty := false, false, false, false, false
			for _, probe := range targets[0].Probes {
				var object map[string]any
				if json.Unmarshal([]byte(probe.JSON), &object) != nil || object == nil {
					continue
				}
				reviewed := location != "unrelated"
				if location == "aggregate" {
					reviewed = object["method"] == "command/exec"
					object, _ = object["params"].(map[string]any)
				}
				if command, ok := object["command"].([]any); ok {
					if len(command) == 0 {
						if reviewed && probe.Valid {
							t.Fatalf("empty argv classification: %s valid=%t", probe.JSON, probe.Valid)
						}
						seenEmpty = true
						if !reviewed && probe.Valid {
							seenUnrelatedEmpty = true
						}
					}
					if len(command) == 1 && command[0] == "" && probe.Valid {
						seenString = true // Empty arguments are allowed; only the vector is non-empty.
					}
					if len(command) == 1 && command[0] == nil {
						if probe.Valid {
							t.Fatalf("null string item accepted: %s", probe.JSON)
						}
						seenNullItem = true
					}
				}
				if other, ok := object["other"].([]any); ok && len(other) == 0 && probe.Valid {
					seenOther = true
				}
			}
			if !seenEmpty || !seenString || !seenNullItem || !seenOther {
				t.Fatalf("missing boundary witnesses: empty=%t string=%t nullItem=%t other=%t", seenEmpty, seenString, seenNullItem, seenOther)
			}
			if (location == "aggregate" || location == "unrelated") && !seenUnrelatedEmpty {
				t.Fatal("unrelated command field lost its valid empty-array witness")
			}
		})
	}
}

func TestCommandArgvOverlayAuditsSourceShape(t *testing.T) {
	for _, field := range []string{
		`{"type":"array","items":{"type":"string"}}`,
		`{"type":"array","items":{"type":"integer"},"minItems":1}`,
		`{"type":["array","null"],"items":{"type":"string"},"minItems":1}`,
		`{"type":"array","items":{"type":"string"},"minItems":2}`,
		`{"type":"array","items":{"type":"string","minLength":1},"minItems":1}`,
		`{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":2}`,
	} {
		schema := `{"type":"object","properties":{"command":` + field + `},"required":["command"]}`
		if _, err := Build(Request{Schemas: map[string][]byte{"CommandExecParams.json": []byte(schema)}, Bindings: map[string]string{"CommandExecParams.json": "Params"}, CloseObjects: true}); err == nil {
			t.Fatalf("unaudited evolution accepted: %s", field)
		}
	}
	// When upstream encodes the same invariant, preserve it without requiring prose.
	schema := `{"type":"object","properties":{"command":{"type":"array","items":{"type":"string"},"minItems":1}},"required":["command"]}`
	if _, err := Build(Request{Schemas: map[string][]byte{"CommandExecParams.json": []byte(schema)}, Bindings: map[string]string{"CommandExecParams.json": "Params"}, CloseObjects: true}); err != nil {
		t.Fatal(err)
	}
}

// All original tests remain intact. This frozen fixture is paired with the
// current CommandExecParams source only after independently checking the entire
// schema for equality. rust-v0.160.0 and rust-v0.162.1 command schemas are equal.
// Accepted argv contract: protocolv2/protocol_types_test.go:3680-3701,3749-3762.
func TestCommandArgvFrozenRuntimeContract(t *testing.T) {
	raw, err := os.ReadFile("../protocolschema/appserver/v2/v2/CommandExecParams.json")
	if err != nil {
		t.Fatal(err)
	}
	decode := func(raw []byte) map[string]any {
		t.Helper()
		var doc map[string]any
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		if err := d.Decode(&doc); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	want := decode(raw)
	// Standalone record overlay is independently established in protocol-sync.md.
	if err = prepareReachable(want, want, "#", true, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	want["properties"].(map[string]any)["command"].(map[string]any)["minItems"] = json.Number("1")
	fixturePath := "testdata/verifier-proof/command_argv_good.go"
	file, err := parser.ParseFile(token.NewFileSet(), fixturePath, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	frozen := ""
	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if ok && len(spec.Names) == 1 && spec.Names[0].Name == "commandSource" {
			literal, ok := spec.Values[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				t.Fatal("fixture schema must be a frozen string literal")
			}
			frozen, err = strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
		}
		return true
	})
	if frozen == "" || !reflect.DeepEqual(decode([]byte(frozen)), want) {
		t.Fatal("frozen fixture must match same-version source and accepted overlays")
	}
	source, err := Generate(Request{Schemas: map[string][]byte{"v2/CommandExecParams.json": raw}, Bindings: map[string]string{"v2/CommandExecParams.json": "CommandExecParams"}, Package: "verifierfixture", CloseObjects: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []string{"good", "empty", "reject_empty_argument", "lossy"} {
		t.Run(fixture, func(t *testing.T) {
			dir := t.TempDir()
			for name, from := range map[string]string{"go.mod": "../../../go.mod", "go.sum": "../../../go.sum", "fixture.go": "testdata/verifier-proof/command_argv_" + fixture + ".go"} {
				body, err := os.ReadFile(from)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, name), body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "oracle_test.go"), source, 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("go", "test", "-count=1", ".")
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			requireOracleOutcome(t, out, err, fixture == "good")
			if fixture == "empty" && !strings.Contains(string(out), `schema-invalid input accepted: {"command":[]`) {
				t.Fatal("mutant failure must establish acceptance of empty argv")
			}
		})
	}
}
