package schemaoracle

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// Exact unmodified optional-null-only branches and RequestId from
// rust-v0.162.1 / 092d3acd6bec3e3a14bdc7e7a2810ab628ab759d,
// ClientRequest.json (branches 35,36,109,110,111,118,129,136,141,145,161,164).
// This projection tests canonical parameterless encoding against the accepted
// SDK; it does not claim whole-target compatibility or apply an SDK repair.
const targetParameterlessRequests = `{"$schema":"http://json-schema.org/draft-07/schema#","definitions":{"RequestId":{"anyOf":[{"type":"string"},{"format":"int64","type":"integer"}]}},"oneOf":[{"properties":{"id":{"$ref":"#/definitions/RequestId"},"method":{"enum":["memory/reset"],"title":"Memory/resetRequestMethod","type":"string"},"params":{"type":"null"}},"required":["id","method"],"title":"Memory/resetRequest","type":"object"},{"description":"Start a best-effort background compression pass for cold local rollouts.","properties":{"id":{"$ref":"#/definitions/RequestId"},"method":{"enum":["rollout/compress"],"title":"Rollout/compressRequestMethod","type":"string"},"params":{"type":"null"}},"required":["id","method"],"title":"Rollout/compressRequest","type":"object"},{"properties":{"id":{"$ref":"#/definitions/RequestId"},"method":{"enum":["account/gatewayOAuth/read"],"title":"Account/gatewayOAuth/readRequestMethod","type":"string"},"params":{"type":"null"}},"required":["id","method"],"title":"Account/gatewayOAuth/readRequest","type":"object"},{"properties":{"id":{"$ref":"#/definitions/RequestId"},"method":{"enum":["account/gatewayOAuth/login"],"title":"Account/gatewayOAuth/loginRequestMethod","type":"string"},"params":{"type":"null"}},"required":["id","method"],"title":"Account/gatewayOAuth/loginRequest","type":"object"},{"properties":{"id":{"$ref":"#/definitions/RequestId"},"method":{"enum":["account/gatewayOAuth/cancel"],"title":"Account/gatewayOAuth/cancelRequestMethod","type":"string"},"params":{"type":"null"}},"required":["id","method"],"title":"Account/gatewayOAuth/cancelRequest","type":"object"},{"properties":{"id":{"$ref":"#/definitions/RequestId"},"method":{"enum":["remoteControl/status/read"],"title":"RemoteControl/status/readRequestMethod","type":"string"},"params":{"type":"null"}},"required":["id","method"],"title":"RemoteControl/status/readRequest","type":"object"},{"properties":{"id":{"$ref":"#/definitions/RequestId"},"method":{"enum":["config/mcpServer/reload"],"title":"Config/mcpServer/reloadRequestMethod","type":"string"},"params":{"type":"null"}},"required":["id","method"],"title":"Config/mcpServer/reloadRequest","type":"object"},{"properties":{"id":{"$ref":"#/definitions/RequestId"},"method":{"enum":["windowsSandbox/readiness"],"title":"WindowsSandbox/readinessRequestMethod","type":"string"},"params":{"type":"null"}},"required":["id","method"],"title":"WindowsSandbox/readinessRequest","type":"object"},{"properties":{"id":{"$ref":"#/definitions/RequestId"},"method":{"enum":["account/logout"],"title":"Account/logoutRequestMethod","type":"string"},"params":{"type":"null"}},"required":["id","method"],"title":"Account/logoutRequest","type":"object"},{"properties":{"id":{"$ref":"#/definitions/RequestId"},"method":{"enum":["account/workspaceMessages/read"],"title":"Account/workspaceMessages/readRequestMethod","type":"string"},"params":{"type":"null"}},"required":["id","method"],"title":"Account/workspaceMessages/readRequest","type":"object"},{"properties":{"id":{"$ref":"#/definitions/RequestId"},"method":{"enum":["externalAgentConfig/import/readHistories"],"title":"ExternalAgentConfig/import/readHistoriesRequestMethod","type":"string"},"params":{"type":"null"}},"required":["id","method"],"title":"ExternalAgentConfig/import/readHistoriesRequest","type":"object"},{"properties":{"id":{"$ref":"#/definitions/RequestId"},"method":{"enum":["configRequirements/read"],"title":"ConfigRequirements/readRequestMethod","type":"string"},"params":{"type":"null"}},"required":["id","method"],"title":"ConfigRequirements/readRequest","type":"object"}]}`

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
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for label, raw := range map[string][]byte{"accepted": accepted, "target": []byte(targetParameterlessRequests)} {
		t.Run(label, func(t *testing.T) {
			r := Request{Schemas: map[string][]byte{"ClientRequest.json": raw}, Bindings: map[string]string{"ClientRequest.json": "ClientRequest"}, Package: "schemaoracleprobe", ImportPath: "github.com/ronhuafeng/llm-go/codexsdk/protocolv2", CloseObjects: true}
			source, err := Generate(r)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			mod := "module oraclefixture\n\ngo 1.26.8\n\nrequire github.com/ronhuafeng/llm-go v0.0.0\nreplace github.com/ronhuafeng/llm-go => " + strconv.Quote(root) + "\n"
			for name, body := range map[string][]byte{"go.mod": []byte(mod), "oracle_test.go": source} {
				if err = os.WriteFile(filepath.Join(dir, name), body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("go", "test", "-mod=mod", "-count=1", ".")
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			requireOracleOutcome(t, out, err, true)
		})
	}
}
