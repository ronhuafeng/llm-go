package schemaoracle

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unsafe"
)

type testWireProbe struct {
	JSON       int
	Valid      bool
	Diagnostic int
}
type testWireCorpus struct {
	Targets     [][]testWireProbe
	Values      []json.RawMessage
	Diagnostics []string
}

func decodeWireCorpus(t *testing.T, r io.Reader) testWireCorpus {
	t.Helper()
	var c testWireCorpus
	if err := json.NewDecoder(r).Decode(&c); err != nil {
		t.Fatal(err)
	}
	return c
}
func assertWireProbes(t *testing.T, targets []Target, c testWireCorpus) {
	t.Helper()
	if len(targets) != len(c.Targets) {
		t.Fatal("target count changed")
	}
	for i, target := range targets {
		if len(target.Probes) != len(c.Targets[i]) {
			t.Fatal("probe count changed", target.Schema)
		}
		for j, p := range target.Probes {
			q := c.Targets[i][j]
			if q.JSON < 0 || q.JSON >= len(c.Values) || q.Diagnostic < 0 || q.Diagnostic >= len(c.Diagnostics) || string(c.Values[q.JSON]) != p.JSON || q.Valid != p.Valid || c.Diagnostics[q.Diagnostic] != p.Diagnostic {
				t.Fatalf("wire changed %s probe %d", target.Schema, j)
			}
		}
	}
}
func TestFullTargetCorpusBudgetAndIdentity(t *testing.T) {
	testFullTargetCorpusBudgetAndIdentity(t, "rust-v0.162.1", 128968,
		"c74c50d65f5e74a1e17cc448fec276d5465eac447a807c95c4b3a139c16aa389",
		"040fc52c499408681c6d76ae42fdaa148ed804c4ca4ef2a6df863c33045c3f7f")
}

func testFullTargetCorpusBudgetAndIdentity(t *testing.T, version string, wantProbes int, wantSequence, wantMetadata string) {
	t.Helper()
	f, err := os.Open("testdata/" + version + "-full-target.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	var fixture struct {
		Schemas  map[string]json.RawMessage
		Bindings map[string]string
	}
	if err = json.NewDecoder(z).Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	req := Request{Context: context.Background(), Schemas: map[string][]byte{}, Bindings: fixture.Bindings, Package: "schemaoracleprobe", ImportPath: "github.com/ronhuafeng/llm-go/codexsdk/protocolv2", CloseObjects: true}
	for k, v := range fixture.Schemas {
		req.Schemas[k] = v
	}
	targets, err := Build(req)
	if err != nil {
		t.Fatal(err)
	}
	source, err := Render(req, targets)
	if err != nil {
		t.Fatal(err)
	}
	// This is the production Build+Render path. Decode the actual generated
	// constant, not a separate encoder or a projected size calculation.
	line := strings.SplitN(strings.SplitN(string(source), "const frozenCorpus = ", 2)[1], "\n", 2)[0]
	encoded, err := strconv.Unquote(line)
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(compressed) > maxCompressedBytes {
		t.Fatal("compressed bound")
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxCorpusBytes+1))
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	if len(raw) > maxCorpusBytes {
		t.Fatal("decompressed bound")
	}
	corpus := decodeWireCorpus(t, bytes.NewReader(raw))
	assertWireProbes(t, targets, corpus)
	sequence, metadata := sha256.New(), sha256.New()
	count := 0
	for i, target := range targets {
		for _, p := range corpus.Targets[i] {
			fmt.Fprintf(sequence, "%s\x00%t\x00%s\n", target.Schema, p.Valid, corpus.Values[p.JSON])
			count++
		}
		target.Probes = nil
		b, _ := json.Marshal(target)
		metadata.Write(b)
		metadata.Write([]byte("\n"))
	}
	if len(targets) != 65 || count != wantProbes {
		t.Fatalf("coverage changed: %d targets %d probes", len(targets), count)
	}
	if got := fmt.Sprintf("%x", sequence.Sum(nil)); got != wantSequence {
		t.Fatal("original input/verdict/order changed", got)
	}
	if got := fmt.Sprintf("%x", metadata.Sum(nil)); got != wantMetadata {
		t.Fatal("target bindings or acceptance metadata changed", got)
	}
	t.Logf("targets=%d probes=%d serialized=%d compressed=%d", len(targets), count, len(raw), len(compressed))
}
func TestDiagnosticPoolSharesRetainedMetadata(t *testing.T) {
	pool := newDiagnosticPool()
	a := Probe{Diagnostic: strings.Clone("constraint"), Location: &DiagnosticLocation{Source: "source", Instance: "/value", Attribution: "exact"}}
	b := Probe{Diagnostic: strings.Clone(a.Diagnostic), Location: &DiagnosticLocation{Source: "source", Instance: "/value", Attribution: "exact"}}
	if pool.intern(&a) == 0 || pool.intern(&b) != 0 {
		t.Fatal("unique payload accounting")
	}
	if a.Location != b.Location || unsafe.StringData(a.Diagnostic) != unsafe.StringData(b.Diagnostic) {
		t.Fatal("metadata was only discounted, not shared")
	}
	if a.Location.Source != "source" || a.Location.Instance != "/value" {
		t.Fatal("controller location lost")
	}
}
func TestRawCorpusPreservesNumbersAndEscapes(t *testing.T) {
	probes := []Probe{{JSON: `{"large":123456789012345678901234567890,"exponent":1e1000,"negative":-0,"escaped":"<>&\u2028\"\\"}`, Valid: true}, {JSON: `"literal"`, Diagnostic: "bad"}}
	targets := []Target{{Probes: probes}}
	compressed, err := compressCorpus(context.Background(), targets)
	if err != nil {
		t.Fatal(err)
	}
	z, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	assertWireProbes(t, targets, decodeWireCorpus(t, z))
}

func TestRawCorpusCannotBeMutatedByCandidateDecoder(t *testing.T) {
	r := request(`{"type":"string","const":"abc"}`)
	output := runOracleFixture(t, r, `package fixture
 import("encoding/json";"fmt")
 type Payload string
 func(p *Payload)UnmarshalJSON(b []byte)error{if string(b)!=fmt.Sprintf("%q","abc"){return fmt.Errorf("invalid")};b[1]='z';type plain Payload;return json.Unmarshal(b,(*plain)(p))}
 `, false)
	if !bytes.Contains(output, []byte("wire value lost")) {
		t.Fatalf("did not reject input mutation: %s", output)
	}
}

func TestGeneratedCorpusRejectsInvalidDiagnosticReference(t *testing.T) {
	testGeneratedCorpusRejectsInvalidReference(t, "Diagnostic", "len(frozen.Diagnostics)", "frozen diagnostic reference out of range")
}

func TestGeneratedCorpusRejectsInvalidJSONReference(t *testing.T) {
	for _, invalid := range []string{"len(frozen.Values)", "-1"} {
		t.Run(invalid, func(t *testing.T) {
			testGeneratedCorpusRejectsInvalidReference(t, "JSON", invalid, "frozen JSON reference out of range")
		})
	}
}

func testGeneratedCorpusRejectsInvalidReference(t *testing.T, field, value, message string) {
	t.Helper()
	source, err := Generate(request(`{"type":"string"}`))
	if err != nil {
		t.Fatal(err)
	}
	original := string(source)
	changed := strings.Replace(original, "for i, p := range probes {", "for i, p := range probes { p."+field+" = "+value, 1)
	if changed == original {
		t.Fatal("fixture mutation failed")
	}
	dir := t.TempDir()
	for name, body := range map[string]string{"go.mod": "module fixture\ngo 1.26.8\n", "payload.go": "package fixture;type Payload string", "oracle_test.go": changed} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil || !bytes.Contains(out, []byte(message)) {
		t.Fatalf("bad %s index was not rejected: %v %s", field, err, out)
	}
}

func TestBuildSharesWireValuesAcrossIndependentTargets(t *testing.T) {
	targets, err := Build(Request{
		Schemas:  map[string][]byte{"A.json": []byte(`{"type":"boolean"}`), "B.json": []byte(`{"type":"boolean"}`)},
		Bindings: map[string]string{"A.json": "A", "B.json": "B"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || len(targets[0].Probes) == 0 || len(targets[0].Probes) != len(targets[1].Probes) {
		t.Fatal("independent target obligations lost")
	}
	for i, a := range targets[0].Probes {
		b := targets[1].Probes[i]
		if a.JSON != b.JSON || unsafe.StringData(a.JSON) != unsafe.StringData(b.JSON) {
			t.Fatal("identical wire bytes were discounted without sharing storage")
		}
	}
}

func TestWireValueTablePreservesIndependentVerdicts(t *testing.T) {
	targets := []Target{
		{Schema: "A", Probes: []Probe{{JSON: `true`, Valid: true}, {JSON: `true`, Valid: false, Diagnostic: "invalid A"}}},
		{Schema: "B", Probes: []Probe{{JSON: `true`, Valid: false, Diagnostic: "invalid B"}}},
	}
	compressed, err := compressCorpus(context.Background(), targets)
	if err != nil {
		t.Fatal(err)
	}
	z, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	corpus := decodeWireCorpus(t, z)
	assertWireProbes(t, targets, corpus)
	if len(corpus.Values) != 1 || string(corpus.Values[0]) != "true" {
		t.Fatal("wire bytes were not shared")
	}
}
