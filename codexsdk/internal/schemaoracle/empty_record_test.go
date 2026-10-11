package schemaoracle

import (
	"encoding/json"
	"strings"
	"testing"
)

const emptyRecordDecoder = `package fixture
import("encoding/json";"fmt")
type Payload struct{Values map[string]any ` + "`json:\"-\"`" + `}
func(p *Payload)UnmarshalJSON(b []byte)error{var value map[string]any;if err:=json.Unmarshal(b,&value);err!=nil{return err};if value==nil{return fmt.Errorf("object required")};if len(value)!=0{return fmt.Errorf("unknown field")};p.Values=value;return nil}
func(p Payload)MarshalJSON()([]byte,error){return json.Marshal(p.Values)}
`

func TestNamedEmptyRecordRuntimeContract(t *testing.T) {
	r := request(`{"type":"object"}`)
	// For this property-free document, disabling the overlay reproduces the old
	// verifier exactly: object members were schema-valid and strict decoding failed.
	old := r
	old.CloseObjects = false
	t.Run("old_verifier_rejects_strict_decoder", func(t *testing.T) { runOracleFixture(t, old, emptyRecordDecoder, false) })
	t.Run("strict_decoder", func(t *testing.T) { runOracleFixture(t, r, emptyRecordDecoder, true) })
	permissive := strings.Replace(emptyRecordDecoder, `if len(value)!=0{return fmt.Errorf("unknown field")};`, "", 1)
	t.Run("preserving_unknown_is_rejected", func(t *testing.T) { runOracleFixture(t, r, permissive, false) })
	t.Run("ignoring_unknown_is_rejected", func(t *testing.T) {
		runOracleFixture(t, r, strings.Replace(permissive, "p.Values=value", "p.Values=map[string]any{}", 1), false)
	})
	for _, key := range []string{"definitions", "$defs"} {
		t.Run("referenced_"+key, func(t *testing.T) {
			r := request(`{"$ref":"#/` + key + `/Empty","` + key + `":{"Empty":{"type":"object"}}}`)
			runOracleFixture(t, r, emptyRecordDecoder, true)
			runOracleFixture(t, r, permissive, false)
		})
	}
}

func TestNamedEmptyRecordOverlayUsesSchemaRole(t *testing.T) {
	tests := []struct {
		name, schema, fragment string
		closed                 bool
	}{
		{"root", `{"type":"object"}`, "", true},
		{"single_type_array", `{"type":["object"]}`, "", true},
		{"explicit_closed", `{"type":"object","additionalProperties":false}`, "", true},
		{"direct_definition", `{"definitions":{"Empty":{"type":"object"}}}`, "#/definitions/Empty", true},
		{"escaped_definition_name", `{"definitions":{"a/b":{"type":"object"}}}`, "#/definitions/a~1b", true},
		{"typed_map", `{"type":"object","additionalProperties":{"type":"string"}}`, "", false},
		{"open_map", `{"type":"object","additionalProperties":true}`, "", false},
		{"explicit_empty_properties_map", `{"type":"object","properties":{},"additionalProperties":true}`, "", false},
		{"union", `{"anyOf":[{"type":"object"},{"type":"string"}]}`, "", false},
		{"type_union", `{"type":["object","string"]}`, "", false},
		{"named_union", `{"$ref":"#/definitions/Open","definitions":{"Open":{"anyOf":[{"type":"object"},{"type":"string"}]}}}`, "", false},
		{"pointer_to_anonymous_property", `{"$ref":"#/definitions/Holder/properties/value","definitions":{"Holder":{"type":"object","properties":{"value":{"type":"object"}}}}}`, "", false},
		{"anonymous_titled_branch", `{"anyOf":[{"title":"LooksNamed","type":"object"},{"type":"string"}]}`, "", false},
		{"true", `true`, "", false},
		{"unconstrained", `{}`, "", false},
		{"annotation_only", `{"title":"AnnotationOnly"}`, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := request(tc.schema)
			if tc.fragment != "" {
				r.Bindings = map[string]string{"target.json" + tc.fragment: "Payload"}
			}
			targets, err := Build(r)
			if err != nil {
				t.Fatal(err)
			}
			if targets[0].CheckFields != tc.closed {
				t.Fatalf("record field check=%v want=%v", targets[0].CheckFields, tc.closed)
			}
			validExtra, invalidExtra := false, false
			for _, p := range targets[0].Probes {
				var object map[string]any
				if json.Unmarshal([]byte(p.JSON), &object) == nil && len(object) > 0 {
					if p.Valid {
						validExtra = true
					} else {
						invalidExtra = true
					}
				}
			}
			if tc.closed {
				if validExtra || !invalidExtra {
					t.Fatalf("closed evidence validExtra=%v invalidExtra=%v", validExtra, invalidExtra)
				}
			} else if !validExtra {
				t.Fatal("open schema has no valid unknown-field witness")
			}
		})
	}
}

func TestExplicitOpenMapsAndAnonymousUnionCatchallsExecute(t *testing.T) {
	openObject := `package fixture
import("encoding/json";"fmt")
type Payload map[string]any
func(p *Payload)UnmarshalJSON(b []byte)error{var value map[string]any;if err:=json.Unmarshal(b,&value);err!=nil{return err};if value==nil{return fmt.Errorf("object required")};*p=value;return nil}
`

	for _, schema := range []string{
		`{"type":"object","additionalProperties":true}`,
		`{"type":"object","properties":{},"additionalProperties":true}`,
		`{"$ref":"#/definitions/Holder/properties/value","definitions":{"Holder":{"type":"object","properties":{"value":{"type":"object"}}}}}`,
	} {
		t.Run(schema, func(t *testing.T) { runOracleFixture(t, request(schema), openObject, true) })
	}
	unionDecoder := `package fixture
import("encoding/json";"fmt")
type Payload json.RawMessage
func(p *Payload)UnmarshalJSON(b []byte)error{var v any;if err:=json.Unmarshal(b,&v);err!=nil{return err};switch v.(type){case string,map[string]any:default:return fmt.Errorf("object or string required")};*p=append((*p)[:0],b...);return nil}
func(p Payload)MarshalJSON()([]byte,error){return []byte(p),nil}
`
	for _, schema := range []string{
		`{"anyOf":[{"type":"object"},{"type":"string"}]}`,
		`{"type":["object","string"]}`,
		`{"$ref":"#/definitions/Open","definitions":{"Open":{"anyOf":[{"type":"object"},{"type":"string"}]}}}`,
	} {
		t.Run(schema, func(t *testing.T) { runOracleFixture(t, request(schema), unionDecoder, true) })
	}
}

func TestNestedNamedEmptyRecordUsesPublicDefault(t *testing.T) {
	r := request(`{"type":"object","properties":{"extra":{"$ref":"#/definitions/Empty"}},"required":["extra"],"definitions":{"Empty":{"type":"object"}}}`)
	strict := `package fixture
import("encoding/json";"fmt")
type Payload struct{Extra map[string]any ` + "`json:\"extra\"`" + `}
func(p *Payload)UnmarshalJSON(b []byte)error{var root map[string]json.RawMessage;if err:=json.Unmarshal(b,&root);err!=nil{return err};if len(root)!=1||root["extra"]==nil{return fmt.Errorf("extra required")};var extra map[string]any;if err:=json.Unmarshal(root["extra"],&extra);err!=nil{return err};if extra==nil{return fmt.Errorf("extra object required")};if len(extra)!=0{return fmt.Errorf("unknown field")};p.Extra=extra;return nil}
`
	runOracleFixture(t, r, strict, true)
	runOracleFixture(t, r, strings.Replace(strict, `if len(extra)!=0{return fmt.Errorf("unknown field")};`, "", 1), false)
}

func TestEmptyTypedMapAndUnconstrainedJSONExecute(t *testing.T) {
	typed := `package fixture
import("encoding/json";"fmt")
type Payload map[string]string
func(p *Payload)UnmarshalJSON(b []byte)error{var value map[string]any;if err:=json.Unmarshal(b,&value);err!=nil{return err};if value==nil{return fmt.Errorf("object required")};out:=Payload{};for key,v:=range value{s,ok:=v.(string);if !ok{return fmt.Errorf("string required")};out[key]=s};*p=out;return nil}
`
	runOracleFixture(t, request(`{"type":"object","additionalProperties":{"type":"string"}}`), typed, true)
	passthrough := `package fixture
import "encoding/json"
type Payload json.RawMessage
func(p *Payload)UnmarshalJSON(b []byte)error{var v any;if err:=json.Unmarshal(b,&v);err!=nil{return err};*p=append((*p)[:0],b...);return nil}
func(p Payload)MarshalJSON()([]byte,error){return []byte(p),nil}
`
	for _, schema := range []string{`true`, `{}`, `{"title":"Unconstrained"}`} {
		t.Run(schema, func(t *testing.T) { runOracleFixture(t, request(schema), passthrough, true) })
	}
}
