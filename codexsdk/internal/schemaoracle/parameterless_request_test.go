package schemaoracle

import (
	"strings"
	"testing"
)

// The invented methods prove that eligibility is source shape/context, not a
// memory/reset name exception. The second branch must retain nullable params.
const parameterlessFixtureSchema = `{"oneOf":[{"type":"object","properties":{"id":{"type":"string"},"method":{"type":"string","enum":["fixture/empty"]},"params":{"type":"null"}},"required":["id","method"]},{"type":"object","properties":{"id":{"type":"string"},"method":{"type":"string","enum":["fixture/value"]},"params":{"type":["string","null"]}},"required":["id","method"]}]}`

const parameterlessFixtureDecoder = `package fixture
import("encoding/json";"fmt")
type Payload map[string]any
func(p *Payload)UnmarshalJSON(b []byte)error{var root map[string]any;if json.Unmarshal(b,&root)!=nil||root==nil{return fmt.Errorf("object")};for key:=range root{if key!="id"&&key!="method"&&key!="params"{return fmt.Errorf("unknown field")}};if _,ok:=root["id"].(string);!ok{return fmt.Errorf("id")};method,ok:=root["method"].(string);if !ok{return fmt.Errorf("method")};switch method{case "fixture/empty":if value,present:=root["params"];present&&value!=nil{return fmt.Errorf("null params")};case "fixture/value":if value,present:=root["params"];present&&value!=nil{if _,ok:=value.(string);!ok{return fmt.Errorf("string params")}};default:return fmt.Errorf("method")};*p=root;return nil}
func(p Payload)MarshalJSON()([]byte,error){out:=map[string]any{};for key,value:=range p{out[key]=value};if out["method"]=="fixture/empty"{if value,present:=out["params"];present&&value==nil{delete(out,"params")}};MUTATION;return json.Marshal(out)}
`

func parameterlessFixtureRequest(schema string) Request {
	return Request{Schemas: map[string][]byte{"ClientRequest.json": []byte(schema)}, Bindings: map[string]string{"ClientRequest.json": "Payload"}, Package: "fixture", CloseObjects: true}
}

func TestParameterlessRequestCanonicalOmissionExecutes(t *testing.T) {
	r := parameterlessFixtureRequest(parameterlessFixtureSchema)
	good := strings.Replace(parameterlessFixtureDecoder, "MUTATION", "", 1)
	runOracleFixture(t, r, good, true)
	// Disabling source-envelope identity reproduces the old exact-equality rule
	// on exactly the same schema and compiled decoder, without rewriting probes.
	old := request(parameterlessFixtureSchema)
	out := runOracleFixture(t, old, good, false)
	if !strings.Contains(string(out), "wire value lost:") || !strings.Contains(string(out), `"params":null`) {
		t.Fatalf("old rule did not reject canonical null omission:\n%s", out)
	}
}

func TestParameterlessRequestDoesNotRelaxWireValidation(t *testing.T) {
	r := parameterlessFixtureRequest(parameterlessFixtureSchema)
	for _, tc := range []struct{ name, from, to, cause string }{
		{"invalid_null_only_params", `return fmt.Errorf("null params")`, `/* accept invalid params */`, "schema-invalid input accepted:"},
		{"unknown_fields", `return fmt.Errorf("unknown field")`, `/* accept unknown fields */`, "schema-invalid input accepted:"},
		{"known_id_type", `return fmt.Errorf("id")`, `/* accept missing/invalid id */`, "schema-invalid input accepted:"},
		{"unknown_method", `default:return fmt.Errorf("method")`, `default:/* accept unknown method */`, "schema-invalid input accepted:"},
		{"nullable_non_null_branch", "MUTATION", `if out["method"]=="fixture/value"{delete(out,"params")}`, "wire value lost:"},
		{"changed_method", "MUTATION", `out["method"]="fixture/value"`, "wire value lost:"},
		{"changed_id", "MUTATION", `out["id"]=false`, "wire value lost:"},
		{"added_unknown_field", "MUTATION", `out["unexpected"]=true`, "wire value lost:"},
		{"added_null", "MUTATION", `if _,present:=p["params"];!present{out["params"]=nil}`, "wire value lost:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			implementation := strings.Replace(parameterlessFixtureDecoder, tc.from, tc.to, 1)
			implementation = strings.Replace(implementation, "MUTATION", "", 1)
			out := runOracleFixture(t, r, implementation, false)
			if !strings.Contains(string(out), tc.cause) {
				t.Fatalf("wrong negative evidence, want %s:\n%s", tc.cause, out)
			}
		})
	}
}

func TestParameterlessRequestOmissionStaysAtEnvelopeBoundary(t *testing.T) {
	good := strings.Replace(parameterlessFixtureDecoder, "MUTATION", "", 1)
	for _, name := range []string{"Tool.json", "ServerRequest.json", "ServerNotification.json"} {
		t.Run(name, func(t *testing.T) {
			r := Request{Schemas: map[string][]byte{name: []byte(parameterlessFixtureSchema)}, Bindings: map[string]string{name: "Payload"}, Package: "fixture", CloseObjects: true}
			out := runOracleFixture(t, r, good, false)
			if !strings.Contains(string(out), "wire value lost:") {
				t.Fatalf("non-client envelope lost exact equality:\n%s", out)
			}
		})
	}
	t.Run("nested_definition", func(t *testing.T) {
		r := parameterlessFixtureRequest(`{"definitions":{"Envelope":` + parameterlessFixtureSchema + `}}`)
		r.Bindings = map[string]string{"ClientRequest.json#/definitions/Envelope": "Payload"}
		out := runOracleFixture(t, r, good, false)
		if !strings.Contains(string(out), "wire value lost:") {
			t.Fatalf("nested object granted envelope canonicalization:\n%s", out)
		}
	})
	t.Run("required_params", func(t *testing.T) {
		r := parameterlessFixtureRequest(strings.Replace(parameterlessFixtureSchema, `"required":["id","method"]`, `"required":["id","method","params"]`, 1))
		out := runOracleFixture(t, r, good, false)
		if !strings.Contains(string(out), "wire value lost:") || !strings.Contains(string(out), "schema-invalid input accepted:") {
			t.Fatalf("required params presence relaxed:\n%s", out)
		}
	})
	t.Run("nullable_params", func(t *testing.T) {
		r := parameterlessFixtureRequest(strings.Replace(parameterlessFixtureSchema, `"params":{"type":"null"}`, `"params":{"type":["string","null"]}`, 1))
		out := runOracleFixture(t, r, good, false)
		if !strings.Contains(string(out), "wire value lost:") {
			t.Fatalf("nullable params relaxed:\n%s", out)
		}
	})
	t.Run("parent_required_params", func(t *testing.T) {
		r := parameterlessFixtureRequest(strings.Replace(parameterlessFixtureSchema, `{"oneOf":`, `{"required":["params"],"oneOf":`, 1))
		// Existing witness construction cannot satisfy this compound parent
		// requiredness. Preserve its fail-closed construction error; do not
		// manufacture compiled proof from an unconstructed source contract.
		if _, err := Generate(r); err == nil || !strings.Contains(err.Error(), "missing property 'params'") {
			t.Fatalf("compound requiredness did not fail closed: %v", err)
		}
	})
	t.Run("ambiguous_method_context", func(t *testing.T) {
		// The same method can identify two branches distinguished by params.
		// Refuse canonicalization without a unique direct source branch.
		schema := strings.Replace(parameterlessFixtureSchema, `"fixture/value"`, `"fixture/empty"`, 1)
		schema = strings.Replace(schema, `"type":["string","null"]`, `"type":"string"`, 1)
		last := strings.LastIndex(schema, `"required":["id","method"]`)
		schema = schema[:last] + strings.Replace(schema[last:], `"required":["id","method"]`, `"required":["id","method","params"]`, 1)
		code := strings.Replace(good, `return fmt.Errorf("null params")`, `if _,ok:=value.(string);!ok{return fmt.Errorf("string or null")}`, 1)
		out := runOracleFixture(t, parameterlessFixtureRequest(schema), code, false)
		if !strings.Contains(string(out), "wire value lost:") {
			t.Fatalf("ambiguous method context granted canonicalization:\n%s", out)
		}
	})
}

func TestParameterlessRequestKeepsOrdinaryNullMembers(t *testing.T) {
	for _, field := range []string{"other", "nested"} {
		t.Run(field, func(t *testing.T) {
			typeSchema := `{"type":"null"}`
			if field == "nested" {
				typeSchema = `{"type":"object","properties":{"params":{"type":"null"}}}`
			}
			schema := strings.Replace(parameterlessFixtureSchema, `"params":{"type":"null"}`, `"params":{"type":"null"},"`+field+`":`+typeSchema, 1)
			code := strings.Replace(parameterlessFixtureDecoder, `key!="params"`, `key!="params"&&key!="`+field+`"`, 1)
			validation := `if value,present:=root["other"];present&&value!=nil{return fmt.Errorf("other")};`
			if field == "nested" {
				validation = `if value,present:=root["nested"];present{nested,ok:=value.(map[string]any);if !ok||len(nested)>1{return fmt.Errorf("nested")};for key,value:=range nested{if key!="params"||value!=nil{return fmt.Errorf("nested params")}}};`
			}
			code = strings.Replace(code, `*p=root;return nil`, validation+`*p=root;return nil`, 1)
			runOracleFixture(t, parameterlessFixtureRequest(schema), strings.Replace(code, "MUTATION", "", 1), true)
			mutation := `delete(out,"other")`
			if field == "nested" {
				mutation = `if nested,ok:=out["nested"].(map[string]any);ok{delete(nested,"params")}`
			}
			code = strings.Replace(code, "MUTATION", mutation, 1)
			out := runOracleFixture(t, parameterlessFixtureRequest(schema), code, false)
			if !strings.Contains(string(out), "wire value lost:") {
				t.Fatalf("ordinary null member was equated to absence:\n%s", out)
			}
		})
	}
}
