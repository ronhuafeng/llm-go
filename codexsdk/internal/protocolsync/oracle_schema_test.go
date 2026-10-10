package protocolsync

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracle"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOracleCoverageFollowsSourceChangesAndReferences(t *testing.T) {
	old, next, generated := t.TempDir(), t.TempDir(), t.TempDir()
	before := `{"title":"Envelope","type":"object","properties":{"child":{"$ref":"#/definitions/Child"}},"required":["child"],"definitions":{"Child":{"type":"object","properties":{"old":{"type":"boolean"}},"required":["old"]}}}`
	after := strings.ReplaceAll(before, `"old"`, `"new"`)
	for _, p := range []struct{ root, raw string }{{old, before}, {next, after}} {
		if e := os.MkdirAll(filepath.Join(p.root, "v2"), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(p.root, "v2/Envelope.json"), []byte(p.raw), 0600); e != nil {
			t.Fatal(e)
		}
	}
	req, err := schemaOracleRequest(old, next, generated)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Bindings) != 2 || req.Bindings["v2/Envelope.json"] != "Envelope" || req.Bindings["v2/Envelope.json#/definitions/Child"] != "Child" {
		t.Fatalf("coverage=%v", req.Bindings)
	}
	// Proposed output cannot erase its own missing target from coverage.
	if e := os.WriteFile(filepath.Join(generated, "protocol_types.gen.go"), []byte("package sample\n"), 0600); e != nil {
		t.Fatal(e)
	}
	again, err := schemaOracleRequest(old, next, generated)
	if err != nil || len(again.Bindings) != 2 {
		t.Fatalf("candidate selected coverage: %v %v", again.Bindings, err)
	}
}
func TestOracleRejectsRemovedGeneratedType(t *testing.T) {
	old, next, generated := t.TempDir(), t.TempDir(), t.TempDir()
	for _, p := range []string{old, next} {
		if e := os.MkdirAll(filepath.Join(p, "v2"), 0700); e != nil {
			t.Fatal(e)
		}
		writeFile(t, filepath.Join(p, "v2/Kept.json"), `{"title":"Kept","type":"object","properties":{"ok":{"type":"boolean"}}}`)
	}
	writeFile(t, filepath.Join(old, "v2/Removed.json"), `{"title":"Removed","type":"object","properties":{"old":{"type":"boolean"}}}`)
	writeFile(t, filepath.Join(generated, "protocol_types.gen.go"), "package sample\ntype Removed struct { Old bool }\n")
	if _, err := schemaOracleRequest(old, next, generated); err == nil || !strings.Contains(err.Error(), "removed source type") {
		t.Fatalf("got %v", err)
	}
}

func TestOracleTestCompletionCannotBeReplacedByExitZero(t *testing.T) {
	for _, tc := range []struct {
		events string
		want   bool
	}{
		{`{"Action":"pass","Package":"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracleprobe"}` + "\n", false},
		{`{"Action":"run","Package":"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracleprobe","Test":"TestTrustedSchemaOracle"}` + "\n" + `{"Action":"pass","Package":"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracleprobe","Test":"TestTrustedSchemaOracle"}` + "\n", true},
		{`{"Action":"output","Package":"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracleprobe","Output":"--- PASS: TestTrustedSchemaOracle"}` + "\n", false},
	} {
		var events oracleTestEvents
		for _, p := range []byte(tc.events) {
			_, _ = events.Write([]byte{p})
		}
		if got := events.started && events.passed && !events.failed && !events.invalid; got != tc.want {
			t.Fatalf("completion=%t want=%t", got, tc.want)
		}
	}
}

func TestOracleCoverageIncludesRootV1AndFutureSchemaDirectories(t *testing.T) {
	for _, rel := range []string{"RefreshResponse.json", "v1/InitializeResponse.json", "v3/FutureResponse.json"} {
		t.Run(rel, func(t *testing.T) {
			old, next := t.TempDir(), t.TempDir()
			before := `{"title":"Payload","type":"object","properties":{"value":{"type":"string"}}}`
			after := strings.Replace(before, `"type":"string"`, `"type":"boolean"`, 1)
			for _, p := range []struct{ root, raw string }{{old, before}, {next, after}} {
				writeFile(t, filepath.Join(p.root, rel), p.raw)
			}
			request, err := schemaOracleRequest(old, next, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if request.Bindings[rel] != "Payload" {
				t.Fatalf("raw source excluded: %v", request.Bindings)
			}
		})
	}
}

func TestOracleShapePreservesAnnotationNamedPropertiesAndLiteralValues(t *testing.T) {
	for _, field := range []string{"description", "title", "definitions", "$schema", "$comment"} {
		t.Run(field, func(t *testing.T) {
			old, next := t.TempDir(), t.TempDir()
			before := fmt.Sprintf(`{"title":"Payload","type":"object","properties":{%q:{"type":"string"}}}`, field)
			after := strings.Replace(before, `"type":"string"`, `"type":"boolean"`, 1)
			writeFile(t, filepath.Join(old, "Payload.json"), before)
			writeFile(t, filepath.Join(next, "Payload.json"), after)
			request, err := schemaOracleRequest(old, next, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if request.Bindings["Payload.json"] != "Payload" {
				t.Fatalf("property %q disappeared from source comparison", field)
			}
		})
	}
	var a, b any
	if err := json.Unmarshal([]byte(`{"const":{"description":"before","title":"data"}}`), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"const":{"description":"after","title":"data"}}`), &b); err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(oracleShape(a), oracleShape(b)) {
		t.Fatal("literal JSON mistaken for annotations")
	}
}

func TestOracleReferenceNamedDefinitionsIsNotAContainer(t *testing.T) {
	old, next := t.TempDir(), t.TempDir()
	before := `{"title":"Envelope","type":"object","properties":{"definitions":{"$ref":"#/definitions/Child"}},"definitions":{"Child":{"type":"object","properties":{"value":{"type":"string"}}}}}`
	after := strings.Replace(before, `"type":"string"`, `"type":"boolean"`, 1)
	writeFile(t, filepath.Join(old, "Envelope.json"), before)
	writeFile(t, filepath.Join(next, "Envelope.json"), after)
	request, err := schemaOracleRequest(old, next, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Bindings) != 2 || request.Bindings["Envelope.json"] != "Envelope" {
		t.Fatalf("reference through definitions property lost: %v", request.Bindings)
	}
	refs := map[string]bool{}
	var doc any
	_ = json.Unmarshal([]byte(`{"const":{"$ref":"#/definitions/NotASchema"},"properties":{"$ref":{"type":"string"}}}`), &doc)
	oracleRefs(doc, refs)
	if len(refs) != 0 {
		t.Fatalf("literal data treated as schema refs: %v", refs)
	}
}

func TestOracleContractErrorsAreNotCandidateRepairEvidence(t *testing.T) {
	assertContract := func(err error) {
		t.Helper()
		var contract *oracleContractError
		if !errors.As(err, &contract) {
			t.Fatalf("expected trusted oracle contract error, got %v", err)
		}
	}
	t.Run("read", func(t *testing.T) {
		_, err := schemaOracleRequest(filepath.Join(t.TempDir(), "missing"), t.TempDir(), t.TempDir())
		assertContract(err)
	})
	t.Run("ambiguous", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "Root.json"), `{"title":"Duplicate","type":"object","properties":{"value":{"type":"string"}}}`)
		writeFile(t, filepath.Join(root, "v1/Root.json"), `{"title":"Duplicate","type":"object","properties":{"value":{"type":"boolean"}}}`)
		_, _, err := oracleSchemaFacts(root)
		assertContract(err)
	})
	t.Run("unbound_inline", func(t *testing.T) {
		old, next := t.TempDir(), t.TempDir()
		writeFile(t, filepath.Join(old, "Alias.json"), `{"title":"Alias","type":"string"}`)
		writeFile(t, filepath.Join(next, "Alias.json"), `{"title":"Alias","type":"boolean"}`)
		_, err := schemaOracleRequest(old, next, t.TempDir())
		assertContract(err)
	})
	t.Run("transport_binding", func(t *testing.T) {
		old, next := t.TempDir(), t.TempDir()
		writeFile(t, filepath.Join(old, "JSONRPCRequest.json"), `{"title":"JSONRPCRequest","type":"object","properties":{"value":{"type":"string"}}}`)
		writeFile(t, filepath.Join(next, "JSONRPCRequest.json"), `{"title":"JSONRPCRequest","type":"object","properties":{"value":{"type":"boolean"}}}`)
		_, err := schemaOracleRequest(old, next, t.TempDir())
		assertContract(err)
	})
	t.Run("candidate_declaration_retained", func(t *testing.T) {
		old, next, generated := t.TempDir(), t.TempDir(), t.TempDir()
		for _, root := range []string{old, next} {
			writeFile(t, filepath.Join(root, "Kept.json"), `{"title":"Kept","type":"object","properties":{}}`)
		}
		writeFile(t, filepath.Join(old, "Removed.json"), `{"title":"Removed","type":"object","properties":{}}`)
		writeFile(t, filepath.Join(generated, "types.gen.go"), "package protocolv2;type Removed struct{}")
		_, err := schemaOracleRequest(old, next, generated)
		var contract *oracleContractError
		if err == nil || errors.As(err, &contract) {
			t.Fatalf("residual candidate declaration misclassified: %v", err)
		}
	})
}

func TestOracleSelectionDoesNotIgnoreNestedDefinitionsOrDialect(t *testing.T) {
	t.Run("nested definitions fail explicitly", func(t *testing.T) {
		old, next, generated := t.TempDir(), t.TempDir(), t.TempDir()
		raw := `{"title":"Envelope","type":"object","properties":{"outer":{"$ref":"#/definitions/Outer"}},"definitions":{"Outer":{"type":"object","properties":{"inner":{"$ref":"#/definitions/Outer/definitions/Inner"}},"definitions":{"Inner":{"type":"string"}}}}}`
		writeFile(t, filepath.Join(old, "Envelope.json"), raw)
		writeFile(t, filepath.Join(next, "Envelope.json"), strings.ReplaceAll(raw, `"type":"string"`, `"type":"boolean"`))
		_, err := schemaOracleRequest(old, next, generated)
		var contract *oracleContractError
		if !errors.As(err, &contract) || !strings.Contains(err.Error(), "Envelope.json/definitions/Outer/definitions") {
			t.Fatalf("nested definition silently ignored: %v", err)
		}
	})
	t.Run("dialect selects dependent contracts", func(t *testing.T) {
		old, next, generated := t.TempDir(), t.TempDir(), t.TempDir()
		raw := `{"$schema":"http://json-schema.org/draft-07/schema#","title":"Envelope","type":"object","properties":{"value":{"$ref":"#/definitions/Child"}},"definitions":{"Child":{"type":"object","properties":{"value":{"type":"string"}}}}}`
		writeFile(t, filepath.Join(old, "Envelope.json"), raw)
		writeFile(t, filepath.Join(next, "Envelope.json"), strings.ReplaceAll(raw, "http://json-schema.org/draft-07/schema#", "https://json-schema.org/draft/2020-12/schema"))
		req, err := schemaOracleRequest(old, next, generated)
		if err != nil || len(req.Bindings) != 2 {
			t.Fatalf("dialect ignored: %v %v", req.Bindings, err)
		}
	})
}

func TestOraclePublicReachabilityCompilesAndRetainsMissingBinding(t *testing.T) {
	old, next, generated := t.TempDir(), t.TempDir(), t.TempDir()
	for _, root := range []string{old, next} {
		for _, name := range []string{"ClientNotification", "ServerRequest", "ServerNotification"} {
			writeFile(t, filepath.Join(root, name+".json"), `{"title":"`+name+`","oneOf":[]}`)
		}
		writeFile(t, filepath.Join(root, "ClientRequest.json"), `{"title":"ClientRequest","oneOf":[{"type":"object","properties":{"method":{"type":"string","enum":["read"]},"params":{"$ref":"#/definitions/Payload"}},"required":["method","params"]}],"definitions":{"Payload":{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}}}`)
		writeFile(t, filepath.Join(root, "ReadResponse.json"), `{"title":"ReadResponse","type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`)
		writeFile(t, filepath.Join(root, "InternalNotification.json"), `{"title":"InternalNotification","type":"object","properties":{"hidden":{"type":"string"}}}`)
	}
	writeFile(t, filepath.Join(next, "ReadResponse.json"), `{"title":"ReadResponse","type":"object","properties":{"header":{"$ref":"#/definitions/RequestHeader"}},"required":["header"],"definitions":{"RequestHeader":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}}}`)
	writeFile(t, filepath.Join(next, "InternalNotification.json"), `{"title":"InternalNotification","type":"object","properties":{"hidden":{"type":"boolean"}}}`)
	common := filepath.Join(t.TempDir(), "common.rs")
	writeFile(t, common, "client_request_definitions! {\n Read => \"read\" {\n response: ReadResponse,\n },\n}\nserver_request_definitions! {\n}\n")
	writeOracleAcceptedManifest(t, old, map[string]string{"read": "ReadResponse"})
	request, err := schemaOraclePublicRequest(old, next, generated, common)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"ReadResponse.json": "ReadResponse", "ReadResponse.json#/definitions/RequestHeader": "RequestHeader"}
	if !reflect.DeepEqual(request.Bindings, want) {
		t.Fatalf("public bindings=%v", request.Bindings)
	}
	unscoped, err := schemaOracleRequest(old, next, generated)
	if err != nil {
		t.Fatal(err)
	}
	if unscoped.Bindings["InternalNotification.json"] != "InternalNotification" {
		t.Fatal("regression fixture did not reproduce old extra binding")
	}
	compile := func(req schemaoracle.Request, header bool) ([]byte, error) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "go.mod"), "module fixture\n\ngo 1.26.8\n")
		types := "package subject\ntype ReadResponse struct{}\n"
		if header {
			types += "type RequestHeader struct{}\n"
		}
		writeFile(t, filepath.Join(dir, "subject", "types.go"), types)
		req.Package = "probe"
		req.ImportPath = "fixture/subject"
		source, err := schemaoracle.Generate(req)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "probe", "contract_test.go"), string(source))
		cmd := exec.Command("go", "test", "-c", "-o", filepath.Join(dir, "probe.test"), "./probe")
		cmd.Dir = dir
		return cmd.CombinedOutput()
	}
	if output, err := compile(unscoped, true); err == nil || !strings.Contains(string(output), "undefined: subject.InternalNotification") {
		t.Fatalf("old selector should fail compile: %v %s", err, output)
	}
	if output, err := compile(request, true); err != nil {
		t.Fatalf("public source selection should compile: %v %s", err, output)
	}
	if output, err := compile(request, false); err == nil || !strings.Contains(string(output), "undefined: subject.RequestHeader") {
		t.Fatalf("missing reachable binding must fail compile: %v %s", err, output)
	}
}

func TestOraclePublicRootsFailClosed(t *testing.T) {
	old, next, generated := t.TempDir(), t.TempDir(), t.TempDir()
	common := filepath.Join(t.TempDir(), "common.rs")
	writeFile(t, common, "client_request_definitions! {\n}\nserver_request_definitions! {\n}\n")
	for _, name := range []string{"ClientRequest", "ServerRequest", "ClientNotification", "ServerNotification"} {
		raw := `{"title":"` + name + `","oneOf":[]}`
		writeFile(t, filepath.Join(old, name+".json"), raw)
		writeFile(t, filepath.Join(next, name+".json"), raw)
	}
	writeFile(t, filepath.Join(next, "ClientRequest.json"), `{"title":"ClientRequest","oneOf":[{"properties":{"method":{"enum":["unmapped"]}}}]}`)
	_, err := schemaOraclePublicRequest(old, next, generated, common)
	var contract *oracleContractError
	if !errors.As(err, &contract) || !strings.Contains(err.Error(), "missing response mapping") {
		t.Fatalf("unmapped public request: %v", err)
	}
	if err := os.Remove(common); err != nil {
		t.Fatal(err)
	}
	_, err = schemaOraclePublicRequest(old, next, generated, common)
	if !errors.As(err, &contract) {
		t.Fatalf("unreadable upstream roots: %v", err)
	}
}

func TestOraclePublicRootsRejectUnsupportedAggregateRepresentations(t *testing.T) {
	for _, raw := range []string{
		`{"title":"ClientRequest","oneOf":[{"properties":{"method":{"const":"read"}}}]}`,
		`{"title":"ClientRequest","anyOf":[{"properties":{"method":{"enum":["read"]}}}]}`,
		`{"title":"ClientRequest","oneOf":[{"properties":{"method":{"enum":["read","other"]}}}]}`,
	} {
		t.Run(raw, func(t *testing.T) {
			old, next, generated := t.TempDir(), t.TempDir(), t.TempDir()
			common := filepath.Join(t.TempDir(), "common.rs")
			writeFile(t, common, "client_request_definitions! {\n Read => \"read\" {\n response: ReadResponse,\n },\n}\nserver_request_definitions! {\n}\n")
			for _, root := range []string{old, next} {
				for _, name := range []string{"ClientRequest", "ServerRequest", "ClientNotification", "ServerNotification"} {
					writeFile(t, filepath.Join(root, name+".json"), `{"title":"`+name+`","oneOf":[]}`)
				}
				writeFile(t, filepath.Join(root, "ReadResponse.json"), `{"title":"ReadResponse","type":"object"}`)
			}
			writeOracleAcceptedManifest(t, old, nil)
			writeFile(t, filepath.Join(next, "ClientRequest.json"), raw)
			_, err := schemaOraclePublicRequest(old, next, generated, common)
			var contract *oracleContractError
			if !errors.As(err, &contract) || !strings.Contains(err.Error(), "oracle public roots:") || !(strings.Contains(err.Error(), "single method enum") || strings.Contains(err.Error(), "keyword anyOf")) {
				t.Fatalf("unsupported root must block response omission: %v", err)
			}
		})
	}
}

// Fixture manifest response associations are authored independently of generated
// declarations, with the same accepted-source consistency checks as production.
func writeOracleAcceptedManifest(t *testing.T, root string, responses map[string]string) {
	t.Helper()
	var entries []map[string]any
	for _, aggregate := range []string{"ClientRequest", "ClientNotification", "ServerRequest", "ServerNotification"} {
		raw, err := os.ReadFile(filepath.Join(root, aggregate+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			OneOf []struct {
				Properties struct {
					Method struct {
						Enum []string `json:"enum"`
					} `json:"method"`
				} `json:"properties"`
			} `json:"oneOf"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		direction := "client_to_server"
		if strings.HasPrefix(aggregate, "Server") {
			direction = "server_to_client"
		}
		kind := "notification"
		if strings.HasSuffix(aggregate, "Request") {
			kind = "request"
		}
		for i, branch := range doc.OneOf {
			if len(branch.Properties.Method.Enum) != 1 {
				t.Fatal("fixture method")
			}
			method := branch.Properties.Method.Enum[0]
			refs := map[string]string{"aggregate_schema": aggregate + ".json", "aggregate_pointer": fmt.Sprintf("%s.json#/oneOf/%d", aggregate, i), "baseline_source_commit": "accepted"}
			entry := map[string]any{"method": method, "direction": direction, "kind": kind, "source_schema": aggregate + ".json", "source_ref": refs}
			if kind == "request" {
				name := responses[method]
				if name == "" {
					t.Fatal("fixture response")
				}
				entry["response_type"] = name
				entry["response_schema"] = name + ".json"
				entry["response_schema_status"] = "declared"
				refs["response_schema"] = name + ".json"
			}
			entries = append(entries, entry)
		}
	}
	raw, err := json.Marshal(map[string]any{"schema_version": 2, "status": "classified-manifest", "entries": entries})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "manifest.json"), string(raw))
	writeFile(t, filepath.Join(root, "baseline_metadata.json"), `{"source_commit":"accepted"}`)
}

func TestOraclePublicExposureTransitions(t *testing.T) {
	for _, transition := range []string{"becomes_public", "becomes_internal", "deleted_internal_scalar"} {
		t.Run(transition, func(t *testing.T) {
			old, next, generated := t.TempDir(), t.TempDir(), t.TempDir()
			common := filepath.Join(t.TempDir(), "common.rs")
			writeFile(t, common, "client_request_definitions! {\n}\nserver_request_definitions! {\n}\n")
			internal := `{"title":"Payload","type":"object","properties":{"value":{"type":"string"}}}`
			public := `{"title":"ServerNotification","oneOf":[{"properties":{"method":{"enum":["notice"]},"params":{"$ref":"#/definitions/Payload"}}}],"definitions":{"Payload":` + internal + `}}`
			for _, root := range []string{old, next} {
				for _, name := range []string{"ClientRequest", "ServerRequest", "ClientNotification", "ServerNotification"} {
					writeFile(t, filepath.Join(root, name+".json"), `{"title":"`+name+`","oneOf":[]}`)
				}
				writeFile(t, filepath.Join(root, "Payload.json"), internal)
			}
			if transition == "becomes_public" {
				writeFile(t, filepath.Join(next, "ServerNotification.json"), public)
			}
			if transition == "becomes_internal" {
				writeFile(t, filepath.Join(old, "ServerNotification.json"), public)
			}
			if transition == "deleted_internal_scalar" {
				writeFile(t, filepath.Join(old, "InternalScalar.json"), `{"title":"InternalScalar","type":"string"}`)
			}
			writeOracleAcceptedManifest(t, old, nil)
			// Candidate projections cannot rewrite accepted exposure or target roots.
			writeFile(t, filepath.Join(next, "manifest.json"), `{"entries":[{"method":"fabricated"}]}`)
			writeFile(t, filepath.Join(generated, "types.gen.go"), "package subject\n")
			req, err := schemaOraclePublicRequest(old, next, generated, common)
			if err != nil {
				t.Fatal(err)
			}
			if transition == "becomes_public" {
				found := false
				for _, name := range req.Bindings {
					found = found || name == "Payload"
				}
				if !found {
					t.Fatalf("newly public unchanged schema lost: %v", req.Bindings)
				}
			}
			if transition == "deleted_internal_scalar" && len(req.Bindings) != 0 {
				t.Fatalf("internal scalar became obligation: %v", req.Bindings)
			}
			if transition == "becomes_internal" {
				writeFile(t, filepath.Join(generated, "types.gen.go"), "package subject\ntype Payload struct{}\n")
				_, err = schemaOraclePublicRequest(old, next, generated, common)
				var contract *oracleContractError
				if err == nil || errors.As(err, &contract) || !strings.Contains(err.Error(), "remains generated") {
					t.Fatalf("stale formerly public binding not rejected: %v", err)
				}
			}
		})
	}
}

func TestOracleAcceptedResponseAssociationsAreBoundToSource(t *testing.T) {
	for _, mutation := range []string{"commit", "index", "direction", "kind", "response_type", "response_schema", "missing", "duplicate", "extra"} {
		t.Run(mutation, func(t *testing.T) {
			old, next, generated := t.TempDir(), t.TempDir(), t.TempDir()
			common := filepath.Join(t.TempDir(), "common.rs")
			writeFile(t, common, "client_request_definitions! {\n Read => \"read\" {\n response: ReadResponse,\n },\n}\nserver_request_definitions! {\n}\n")
			for _, root := range []string{old, next} {
				for _, name := range []string{"ClientRequest", "ServerRequest", "ClientNotification", "ServerNotification"} {
					writeFile(t, filepath.Join(root, name+".json"), `{"title":"`+name+`","oneOf":[]}`)
				}
				writeFile(t, filepath.Join(root, "ClientRequest.json"), `{"title":"ClientRequest","oneOf":[{"properties":{"method":{"enum":["read"]}}}]}`)
				writeFile(t, filepath.Join(root, "ReadResponse.json"), `{"title":"ReadResponse","type":"object"}`)
			}
			writeOracleAcceptedManifest(t, old, map[string]string{"read": "ReadResponse"})
			path := filepath.Join(old, "manifest.json")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			entries := doc["entries"].([]any)
			entry := entries[0].(map[string]any)
			refs := entry["source_ref"].(map[string]any)
			switch mutation {
			case "commit":
				refs["baseline_source_commit"] = "unaccepted"
			case "index":
				refs["aggregate_pointer"] = "ClientRequest.json#/oneOf/1"
			case "direction":
				entry["direction"] = "server_to_client"
			case "kind":
				entry["kind"] = "notification"
			case "response_type":
				entry["response_type"] = "OtherResponse"
			case "response_schema":
				entry["response_schema"] = "missing.json"
				refs["response_schema"] = "missing.json"
			case "missing":
				doc["entries"] = []any{}
			case "duplicate":
				doc["entries"] = append(entries, entry)
			case "extra":
				doc["entries"] = append(entries, map[string]any{"method": "extra"})
			}
			raw, err = json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, path, string(raw))
			_, err = schemaOraclePublicRequest(old, next, generated, common)
			var contract *oracleContractError
			if !errors.As(err, &contract) {
				t.Fatalf("untrusted accepted association admitted: %v", err)
			}
		})
	}
}
