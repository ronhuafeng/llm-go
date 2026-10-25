package protocolsync

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolgen"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracle"
)

const transportRequestID = `{"anyOf":[{"type":"string"},{"type":"integer","format":"int64"}]}`
const expandedTransportRequestID = `{"anyOf":[{"type":"string"},{"type":"integer","format":"int64"},{"type":"array","items":{"type":"string"}}]}`

func TestOracleTransportDependencyClosure(t *testing.T) {
	for _, hops := range []bool{false, true} {
		for _, mutation := range []string{"unchanged", "changed", "missing dependency", "missing envelope"} {
			t.Run(strings.Join([]string{map[bool]string{false: "direct", true: "multi-hop"}[hops], mutation}, "/"), func(t *testing.T) {
				old, next := t.TempDir(), t.TempDir()
				reference := "RequestId"
				extra := ""
				if hops {
					reference = "Header"
					extra = `,"Header":{"type":"object","properties":{"id":{"$ref":"#/definitions/RequestId"}}}`
				}
				before := `{"title":"JSONRPCRequest","type":"object","properties":{"id":{"$ref":"#/definitions/` + reference + `"},"method":{"type":"string"}},"required":["id","method"],"definitions":{"RequestId":` + transportRequestID + extra + `}}`
				after := before
				switch mutation {
				case "changed":
					after = strings.Replace(before, transportRequestID, expandedTransportRequestID, 1)
				case "missing dependency":
					after = strings.Replace(before, `"RequestId":`+transportRequestID, `"Unrelated":`+transportRequestID, 1)
				case "missing envelope":
					after = `{"title":"Unrelated","type":"object"}`
				}
				writeFile(t, filepath.Join(old, "JSONRPCRequest.json"), before)
				writeFile(t, filepath.Join(next, "JSONRPCRequest.json"), after)
				_, err := schemaOracleRequest(old, next, t.TempDir())
				if mutation == "unchanged" {
					if err != nil {
						t.Fatal(err)
					}
				} else {
					var contract *oracleContractError
					want := "changed handwritten transport contract JSONRPCRequest.json"
					if mutation == "missing dependency" {
						want = "reference #/definitions/RequestId is missing"
					}
					if !errors.As(err, &contract) || !strings.Contains(err.Error(), want) {
						t.Fatalf("unbound closure drift admitted: %v", err)
					}
				}
			})
		}
	}
}

func TestOracleTransportGuardWithRealExpandedRequestIDDecoder(t *testing.T) {
	old, next := t.TempDir(), t.TempDir()
	before := `{"title":"JSONRPCRequest","type":"object","properties":{"id":{"$ref":"#/definitions/RequestId"},"method":{"type":"string"}},"required":["id","method"],"definitions":{"RequestId":` + transportRequestID + `}}`
	after := strings.Replace(before, transportRequestID, expandedTransportRequestID, 1)
	for root, raw := range map[string]string{old: before, next: after} {
		writeFile(t, filepath.Join(root, "JSONRPCRequest.json"), raw)
	}
	prior, _, err := oracleSchemaFacts(old)
	if err != nil {
		t.Fatal(err)
	}
	current, docs, err := oracleSchemaFacts(next)
	if err != nil {
		t.Fatal(err)
	}
	if prior["JSONRPCRequest"].shape != current["JSONRPCRequest"].shape {
		t.Fatal("fixture changed the envelope's own shape")
	}
	// This is the supported generated-child contract the old exemption could
	// certify without checking its consuming handwritten envelope.
	matrix := protocolgen.CoverageMatrix{Types: []protocolgen.CoverageType{{Schema: "JSONRPCRequest.json", Type: "JSONRPCRequest"}}, Fields: []protocolgen.CoverageField{
		{Schema: "JSONRPCRequest.json", Type: "JSONRPCRequest", Field: "id", Path: "JSONRPCRequest.json#/properties/id", Required: true},
		{Schema: "JSONRPCRequest.json", Type: "JSONRPCRequest", Field: "method", Path: "JSONRPCRequest.json#/properties/method", Required: true},
	}}
	// Include a real generated message root, as in production, so the wire
	// decoder registration is exercised along with the child union.
	writeFile(t, filepath.Join(next, "FixtureRequest.json"), `{"title":"FixtureRequest","type":"object"}`)
	matrix.Types = append(matrix.Types, protocolgen.CoverageType{Schema: "FixtureRequest.json", Type: "FixtureRequest"})
	manifest := protocolgen.Manifest{Entries: []protocolgen.ManifestEntry{{SourceSchema: "FixtureRequest.json", Method: "fixture/request", Kind: "request", Direction: "client_to_server"}}}
	plan, err := protocolgen.BuildProtocolTypePlanFromFacts(next, matrix, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := protocolgen.ApplyWireMessageRoles(&plan, manifest); err != nil {
		t.Fatal(err)
	}
	generated, err := protocolgen.GenerateProtocolTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := os.MkdirTemp(filepath.Join(repo, "codexsdk"), "transport-oracle-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(pkg)
	writeFile(t, filepath.Join(pkg, "protocol_types.gen.go"), string(generated))
	for _, name := range []string{"nullable.go", "json_value.go"} {
		raw, err := os.ReadFile(filepath.Join(repo, "codexsdk/protocolv2", name))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(pkg, name), string(raw))
	}
	spec := schemaoracle.Request{Schemas: docs, Bindings: map[string]string{"JSONRPCRequest.json#/definitions/RequestId": "RequestId"}, ImportPath: "github.com/ronhuafeng/llm-go/codexsdk/" + filepath.Base(pkg), CloseObjects: true}
	source, err := schemaoracle.Harness(spec)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(pkg, "probe/main.go"), string(source))
	binary := filepath.Join(pkg, "decoder")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", binary, "./codexsdk/"+filepath.Base(pkg)+"/probe")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("real generated decoder: %v %s", err, out)
	}
	var stream, diagnostics bytes.Buffer
	if err := schemaoracle.Write(spec, &stream); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(&stream)
	var header schemaoracle.Header
	if err := schemaoracle.ReadFrame(reader, &header); err != nil {
		t.Fatal(err)
	}
	if err := checkDecoderProcess(ctx, exec.CommandContext(ctx, binary), header, reader, &diagnostics); err != nil {
		t.Fatalf("supported child contract failed: %v %s", err, diagnostics.String())
	}
	if _, err := schemaOracleRequest(old, next, t.TempDir()); err == nil {
		t.Fatal("generated child success bypassed the transport closure guard")
	}
	// Exercise the actual SDK consumer too, without exporting a raw API or
	// giving the hypothetical array ID execution/correlation authority.
	cmd = exec.CommandContext(ctx, "go", "test", "./codexsdk", "-run", "^TestJSONRPCUnsupportedArrayIDDoesNotAuthorizeRequest$", "-count=1", "-v")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil || !bytes.Contains(out, []byte("--- PASS: TestJSONRPCUnsupportedArrayIDDoesNotAuthorizeRequest")) {
		t.Fatalf("actual transport contract did not execute and pass: %v %s", err, out)
	}
	t.Log("real generated expanded RequestId passed; actual transport retained ID policy; selector rejected unbound consumer drift")
}
