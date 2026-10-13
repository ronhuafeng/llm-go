package schemaoracle

import (
	"strings"
	"testing"
)

func TestSDKResourceOverlayPreservesAcceptedExclusiveRecords(t *testing.T) {
	const schema = `{"anyOf":[{"type":"object","required":["text","uri"],"properties":{"text":{"type":"string"},"uri":{"type":"string"}}},{"type":"object","required":["blob","uri"],"properties":{"blob":{"type":"string"},"uri":{"type":"string"}}}]}`
	r := request(schema)
	r.Schemas = map[string][]byte{"McpResourceReadResponse.json": []byte(`{"type":"object","required":["content"],"properties":{"content":{"$ref":"#/definitions/ResourceContent"}},"definitions":{"ResourceContent":` + schema + `}}`)}
	r.Bindings = map[string]string{"McpResourceReadResponse.json": "Payload"}
	const code = `package fixture
import("encoding/json";"fmt")
type Payload struct{Content map[string]json.RawMessage ` + "`json:\"content\"`" + `}
func(p *Payload)UnmarshalJSON(b []byte)error{
 var root map[string]json.RawMessage;if err:=json.Unmarshal(b,&root);err!=nil{return err};if len(root)!=1||root["content"]==nil{return fmt.Errorf("root")}
 var c map[string]json.RawMessage;if err:=json.Unmarshal(root["content"],&c);err!=nil{return err};if len(c)!=2||c["uri"]==nil{return fmt.Errorf("content")}
 var uri any;if err:=json.Unmarshal(c["uri"],&uri);err!=nil{return err};if _,ok:=uri.(string);!ok{return fmt.Errorf("uri")}
 text,t:=c["text"];blob,both:=c["blob"];if t==both{return fmt.Errorf("exclusive")};if both{text=blob}
 var s any;if err:=json.Unmarshal(text,&s);err!=nil{return err};if _,ok:=s.(string);!ok{return fmt.Errorf("payload")}
 p.Content=c;return nil
}
`
	t.Run("accepted_closed_union", func(t *testing.T) { runOracleFixture(t, r, code, true) })
	t.Run("lost_resource_payload", func(t *testing.T) {
		runOracleFixture(t, r, strings.Replace(code, "p.Content=c", "delete(c,\"uri\");p.Content=c", 1), false)
	})
}

func TestSDKCommandOverlayExecutesContractAndDefect(t *testing.T) {
	const schema = `{"type":"object","required":["command"],"properties":{"command":{"type":"array","items":{"type":"string"},"description":"Command argv vector. Empty arrays are rejected."}}}`
	r := request(schema)
	r.Schemas = map[string][]byte{"v2/CommandExecParams.json": []byte(schema)}
	r.Bindings = map[string]string{"v2/CommandExecParams.json": "Payload"}
	const code = `package fixture
import("encoding/json";"fmt")
type Payload struct {Command []string ` + "`json:\"command\"`" + `}
func(p *Payload)UnmarshalJSON(b []byte)error{
 var fields map[string]json.RawMessage;if err:=json.Unmarshal(b,&fields);err!=nil{return err};if len(fields)!=1||fields["command"]==nil{return fmt.Errorf("fields")}
 var raw []any;if err:=json.Unmarshal(fields["command"],&raw);err!=nil{return err};if len(raw)==0{return fmt.Errorf("empty")}
 for _,v:=range raw{if _,ok:=v.(string);!ok{return fmt.Errorf("item")}}
 type wire Payload;return json.Unmarshal(b,(*wire)(p))
}
`
	t.Run("accepted_nonempty_argv", func(t *testing.T) { runOracleFixture(t, r, code, true) })
	t.Run("empty_argv_defect", func(t *testing.T) {
		runOracleFixture(t, r, strings.Replace(code, "if len(raw)==0", "if raw==nil", 1), false)
	})
}

func TestSDKRealtimeOverlayPreservesSourceProbesAndOuterConstraints(t *testing.T) {
	const schema = `{"type":"object","required":["item","threadId"],"properties":{"threadId":{"type":"string"},"item":{"$ref":"#/definitions/ThreadRealtimeItem"}},"definitions":{"ThreadRealtimeItem":{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}}}`
	r := request(schema)
	r.Schemas = map[string][]byte{"v2/ThreadRealtimeItemStartedNotification.json": []byte(schema)}
	r.Bindings = map[string]string{"v2/ThreadRealtimeItemStartedNotification.json": "Payload"}
	const code = `package fixture
import("encoding/json";"fmt")
type Payload struct{Item json.RawMessage ` + "`json:\"item\"`" + `;ThreadID string ` + "`json:\"threadId\"`" + `}
func(p *Payload)UnmarshalJSON(b []byte)error{
 var fields map[string]json.RawMessage;if err:=json.Unmarshal(b,&fields);err!=nil{return err};if len(fields)!=2||fields["item"]==nil||fields["threadId"]==nil{return fmt.Errorf("fields")}
 var thread any;if err:=json.Unmarshal(fields["threadId"],&thread);err!=nil{return err};if _,ok:=thread.(string);!ok{return fmt.Errorf("thread")}
 type wire Payload;return json.Unmarshal(b,(*wire)(p))
}
`
	t.Run("opaque_item_contract", func(t *testing.T) { runOracleFixture(t, r, code, true) })
	t.Run("lost_item_defect", func(t *testing.T) {
		runOracleFixture(t, r, strings.Replace(code, "return json.Unmarshal(b,(*wire)(p))", "err:=json.Unmarshal(b,(*wire)(p));p.Item=json.RawMessage(`{}`);return err", 1), false)
	})
	targets, err := Build(r)
	if err != nil {
		t.Fatal(err)
	}
	sourceFieldProbe, outerDefect := false, false
	for _, p := range targets[0].Probes {
		sourceFieldProbe = sourceFieldProbe || p.Valid && strings.Contains(p.JSON, `"id":`)
		outerDefect = outerDefect || !p.Valid && strings.Contains(p.JSON, `"__oracle_unknown_field__"`)
	}
	if !sourceFieldProbe || !outerDefect {
		t.Fatalf("source=%v outer=%v", sourceFieldProbe, outerDefect)
	}
}
