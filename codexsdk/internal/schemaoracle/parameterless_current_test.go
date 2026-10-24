package schemaoracle

import (
	"encoding/json"
	"os"
	"testing"
)

func TestAcceptedSDKParameterlessRequestOracle(t *testing.T) {
	accepted, err := os.ReadFile("../protocolschema/appserver/v2/ClientRequest.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(accepted, &doc); err != nil {
		t.Fatal(err)
	}
	branches := []any{}
	for _, node := range doc["oneOf"].([]any) {
		branch := node.(map[string]any)
		props := branch["properties"].(map[string]any)
		params, _ := props["params"].(map[string]any)
		if params["type"] == "null" {
			branches = append(branches, node)
		}
	}
	defs := doc["definitions"].(map[string]any)
	projection := map[string]any{"$schema": doc["$schema"], "definitions": map[string]any{"RequestId": defs["RequestId"]}, "oneOf": branches}
	accepted, err = json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) == 0 {
		t.Fatal("accepted source has no parameterless contract")
	}
	r := Request{Schemas: map[string][]byte{"ClientRequest.json": accepted}, Bindings: map[string]string{"ClientRequest.json": "ClientRequest"}, ImportPath: "github.com/ronhuafeng/llm-go/codexsdk/protocolv2", CloseObjects: true}
	runCurrentTypes(t, r)
}
