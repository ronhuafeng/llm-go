package schemaoracle

import (
	"strings"
	"testing"
)

func TestRealtimeLifecycleOverlayRetainsOpaqueValuesAndRecordConstraints(t *testing.T) {
	const schema = `{"type":"object","properties":{"threadId":{"type":"string"},"item":{"$ref":"#/definitions/ThreadRealtimeItem"}},"required":["threadId","item"],"definitions":{"ThreadRealtimeItem":{"type":"object","properties":{"type":{"type":"string","enum":["future"]}},"required":["type"]}}}`
	const good = `package fixture
import("encoding/json";"fmt")
type Payload struct{ThreadID string ` + "`json:\"threadId\"`" + `;Item json.RawMessage ` + "`json:\"item\"`" + `}
func(p *Payload)UnmarshalJSON(b []byte)error{var fields map[string]json.RawMessage;if json.Unmarshal(b,&fields)!=nil||fields==nil{return fmt.Errorf("object")};for key:=range fields{if key!="threadId"&&key!="item"{return fmt.Errorf("unknown")}};if fields["item"]==nil{return fmt.Errorf("item required")};if fields["threadId"]==nil||string(fields["threadId"])=="null"||json.Unmarshal(fields["threadId"],&p.ThreadID)!=nil{return fmt.Errorf("threadId")};p.Item=fields["item"];return nil}`
	for _, name := range []string{"ThreadRealtimeItemStartedNotification", "ThreadRealtimeItemCompletedNotification"} {
		r := request(schema)
		r.Schemas = map[string][]byte{name + ".json": []byte(schema)}
		r.Bindings = map[string]string{name + ".json": "Payload"}
		t.Run(name+"/accepted_opaque_evolution", func(t *testing.T) { runOracleFixture(t, r, good, true) })
		bad := strings.Replace(good, `if fields["threadId"]==nil||string(fields["threadId"])=="null"||json.Unmarshal(fields["threadId"],&p.ThreadID)!=nil{return fmt.Errorf("threadId")};`, `if fields["threadId"]!=nil{_ = json.Unmarshal(fields["threadId"],&p.ThreadID)};`, 1)
		t.Run(name+"/requiredness_defect", func(t *testing.T) { runOracleFixture(t, r, bad, false) })
		r.Schemas = map[string][]byte{name + ".json": []byte(strings.Replace(schema, `"item":{"$ref":"#/definitions/ThreadRealtimeItem"}`, `"item":{"type":"string"}`, 1))}
		if _, err := Build(r); err == nil {
			t.Fatal("accepted drifted realtime overlay source")
		}
	}
}

func TestCommandArgvOverlayKeepsNonEmptyContract(t *testing.T) {
	const schema = `{"type":"object","properties":{"command":{"type":"array","items":{"type":"string"},"description":"Empty arrays are rejected."}},"required":["command"]}`
	r := request(schema)
	r.Schemas = map[string][]byte{"CommandExecParams.json": []byte(schema)}
	r.Bindings = map[string]string{"CommandExecParams.json": "Payload"}
	const good = `package fixture
import("encoding/json";"fmt")
type Payload struct{Command []string ` + "`json:\"command\"`" + `}
func(p *Payload)UnmarshalJSON(b []byte)error{var fields map[string]json.RawMessage;if json.Unmarshal(b,&fields)!=nil||fields==nil||len(fields)!=1{return fmt.Errorf("record")};var raw []json.RawMessage;if json.Unmarshal(fields["command"],&raw)!=nil||raw==nil{return fmt.Errorf("array")};if len(raw)==0{return fmt.Errorf("empty argv")};p.Command=make([]string,len(raw));for i,v:=range raw{if string(v)=="null"||json.Unmarshal(v,&p.Command[i])!=nil{return fmt.Errorf("string")}};return nil}`
	t.Run("accepted_argv", func(t *testing.T) { runOracleFixture(t, r, good, true) })
	bad := strings.Replace(good, `if len(raw)==0{return fmt.Errorf("empty argv")};`, "", 1)
	t.Run("empty_argv_defect", func(t *testing.T) { runOracleFixture(t, r, bad, false) })
}
