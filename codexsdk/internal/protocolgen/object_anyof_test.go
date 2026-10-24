package protocolgen

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracle"
)

func TestGeneratedObjectAnyOfAgainstIndependentOracle(t *testing.T) {
	const schema = `{"oneOf":[{"type":"object","properties":{"type":{"type":"string","enum":["image"]},"detail":{"type":"string"}},"required":["type"],"anyOf":[{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]},{"type":"object","properties":{"fileId":{"type":"string"}},"required":["fileId"]}]},{"type":"object","properties":{"type":{"type":"string","enum":["bounded"]}},"required":["type"],"anyOf":[{"type":"object","properties":{"count":{"type":"integer","format":"int64","minimum":2}},"required":["count"]},{"type":"object","properties":{"items":{"type":"array","items":{"type":"string"}}},"required":["items"]}]}]}`
	plan := ProtocolTypePlan{Types: []TypePlan{{Kind: TypePlanTaggedUnionCandidate, GeneratedRoot: true, TypeName: "Input", SchemaPath: "Input.json", Schema: mustParseSchema(t, schema)}}}
	runGeneratedUnionOracle(t, schema, "Input", plan)
}

func runGeneratedUnionOracle(t *testing.T, schema, name string, plan ProtocolTypePlan) {
	t.Helper()
	if err := ApplyWireMessageRoles(&plan, Manifest{Entries: []ManifestEntry{{Kind: "request", Direction: "client_to_server", Method: "input/read", SourceSchema: name + ".json"}}}); err != nil {
		t.Fatal(err)
	}
	generated, err := GenerateProtocolTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	spec := schemaoracle.Request{Schemas: map[string][]byte{name + ".json": []byte(schema)}, Bindings: map[string]string{name + ".json": name}, CloseObjects: true}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := os.MkdirTemp(filepath.Join(repo, "codexsdk"), "protocol-anyof-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(pkg) })
	files := map[string][]byte{"protocol_types.gen.go": generated}
	for _, name := range []string{"nullable.go", "json_value.go"} {
		raw, err := os.ReadFile(filepath.Join(repo, "codexsdk", "protocolv2", name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = raw
	}
	for name, raw := range files {
		if err := os.WriteFile(filepath.Join(pkg, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	spec.ImportPath = "github.com/ronhuafeng/llm-go/codexsdk/" + filepath.Base(pkg)
	source, err := schemaoracle.Harness(spec)
	if err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(pkg, "probe")
	if err = os.Mkdir(probe, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(probe, "main.go"), source, 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(pkg, "decoder")
	cmd := exec.Command("go", "build", "-o", binary, "./codexsdk/"+filepath.Base(pkg)+"/probe")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile generated decoder: %v %s", err, out)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd = exec.CommandContext(ctx, binary)
	input, _ := cmd.StdinPipe()
	output, _ := cmd.StdoutPipe()
	var diagnostics, stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { err := schemaoracle.Write(spec, writer); writer.CloseWithError(err); done <- err }()
	stream := bufio.NewReader(reader)
	var header schemaoracle.Header
	err = schemaoracle.ReadFrame(stream, &header)
	if err == nil {
		err = schemaoracle.Check(ctx, header, stream, input, output, &diagnostics)
	}
	input.Close()
	output.Close()
	reader.Close()
	if err != nil {
		cancel()
	}
	runErr := cmd.Wait()
	diagnostics.Write(stderr.Bytes())
	generationErr := <-done
	if err != nil || runErr != nil || generationErr != nil {
		t.Fatalf("generated contracts: check=%v process=%v expectations=%v\n%s", err, runErr, generationErr, diagnostics.Bytes())
	}

}

func TestObjectAnyOfRejectsUnrepresentedConstraints(t *testing.T) {
	for _, branch := range []string{
		`{"type":"object","properties":{"url":{"type":"string","pattern":"x"}},"required":["url"]}`,
		`{"type":"object","properties":{"url":{"type":"string"}},"required":["missing"]}`,
		`{"type":"object","properties":{"url":{"type":"string"}},"additionalProperties":false}`,
	} {
		schema := mustParseSchema(t, `{"oneOf":[{"type":"object","properties":{"type":{"type":"string","enum":["image"]}},"required":["type"],"anyOf":[`+branch+`]}]}`)
		_, err := GenerateProtocolTypes(ProtocolTypePlan{Types: []TypePlan{{Kind: TypePlanTaggedUnionCandidate, GeneratedRoot: true, TypeName: "Input", SchemaPath: "Input.json", Schema: schema}}})
		if err == nil {
			t.Fatalf("unrepresented branch accepted: %s", branch)
		}
	}
}
