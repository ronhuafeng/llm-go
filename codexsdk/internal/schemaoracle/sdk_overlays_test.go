package schemaoracle

import (
	"strings"
	"testing"
)

func TestCommandArgvPublicOverlayExecutes(t *testing.T) {
	r := Request{Schemas: map[string][]byte{"v2/CommandExecParams.json": []byte(`{"type":"object","properties":{"command":{"type":"array","items":{"type":"string"}}},"required":["command"]}`)}, Bindings: map[string]string{"v2/CommandExecParams.json": "Payload"}, CloseObjects: true, Package: "fixture"}
	good := `package fixture
import("encoding/json";"fmt")
type Payload struct {Command []string ` + "`json:\"command\"`" + `}
func(p *Payload)UnmarshalJSON(b []byte)error{var fields map[string]json.RawMessage;if err:=json.Unmarshal(b,&fields);err!=nil{return err};if len(fields)!=1||fields["command"]==nil{return fmt.Errorf("invalid fields")};var items []json.RawMessage;if err:=json.Unmarshal(fields["command"],&items);err!=nil{return err};if len(items)==0{return fmt.Errorf("empty argv")};p.Command=make([]string,len(items));for i,item:=range items{if string(item)=="null"{return fmt.Errorf("null item")};if err:=json.Unmarshal(item,&p.Command[i]);err!=nil{return err}};return nil}
`
	t.Run("existing_nonempty_contract_passes", func(t *testing.T) { runOracleFixture(t, r, good, true) })
	t.Run("empty_argv_defect_fails", func(t *testing.T) {
		runOracleFixture(t, r, strings.Replace(good, `if len(items)==0{return fmt.Errorf("empty argv")};`, "", 1), false)
	})
	// The Go binding cannot extend this overlay to an unrelated source record.
	r.Schemas = map[string][]byte{"Other.json": r.Schemas["v2/CommandExecParams.json"]}
	r.Bindings = map[string]string{"Other.json": "CommandExecParams"}
	targets, err := Build(r)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, probe := range targets[0].Probes {
		if probe.JSON == `{"command":[]}` && probe.Valid {
			found = true
		}
	}
	if !found {
		t.Fatal("unrelated source command narrowed")
	}
}

func TestOpaqueRealtimePublicOverlayExecutes(t *testing.T) {
	const record = `{"type":"object","properties":{"item":{"$ref":"#/definitions/ThreadRealtimeItem"},"threadId":{"type":"string"}},"required":["item","threadId"]}`
	const definition = `"ThreadRealtimeItem":{"type":"object","properties":{"type":{"type":"string","enum":["known"]}},"required":["type"]}`
	good := `package fixture
import("encoding/json";"fmt")
type Payload struct {Item json.RawMessage ` + "`json:\"item\"`" + `;ThreadID string ` + "`json:\"threadId\"`" + `}
func(p *Payload)UnmarshalJSON(b []byte)error{var m map[string]json.RawMessage;if err:=json.Unmarshal(b,&m);err!=nil{return err};if len(m)!=2||m["item"]==nil||m["threadId"]==nil||string(m["threadId"])=="null"{return fmt.Errorf("invalid fields")};if err:=json.Unmarshal(m["threadId"],&p.ThreadID);err!=nil{return err};p.Item=append(p.Item[:0],m["item"]...);return nil}
`
	for _, owner := range []string{"ThreadRealtimeItemStartedNotification", "ThreadRealtimeItemCompletedNotification"} {
		r := Request{Schemas: map[string][]byte{"v2/" + owner + ".json": []byte(strings.TrimSuffix(record, "}") + `,"definitions":{` + definition + `}}`)}, Bindings: map[string]string{"v2/" + owner + ".json": "Payload"}, CloseObjects: true, Package: "fixture"}
		t.Run(owner+"/opaque_values_pass", func(t *testing.T) { runOracleFixture(t, r, good, true) })
		t.Run(owner+"/lost_item_fails", func(t *testing.T) {
			runOracleFixture(t, r, strings.Replace(good, `p.Item=append(p.Item[:0],m["item"]...)`, `p.Item=json.RawMessage("null")`, 1), false)
		})
		t.Run(owner+"/missing_identity_fails", func(t *testing.T) {
			runOracleFixture(t, r, strings.Replace(good, `if err:=json.Unmarshal(m["threadId"],&p.ThreadID);err!=nil{return err}`, `p.ThreadID=""`, 1), false)
		})
	}
}
