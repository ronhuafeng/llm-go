package fulltarget164_test

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
	"strconv"
	"strings"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracle"
)

// Keep the full upstream regression in its own package: it and the accepted
// baseline each exercise the complete production corpus under the standard
// per-package race-test timeout. These limits match the production bounds.
const (
	maxCorpusBytes     = 128 << 20
	maxCompressedBytes = 8 << 20
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
func assertWireProbes(t *testing.T, targets []schemaoracle.Target, c testWireCorpus) {
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
func TestUpstream164FullTargetCorpusBudgetAndIdentity(t *testing.T) {
	// Frozen with the accepted oracle, one target per Build so that the old
	// global byte limit does not suppress the final target obligations.
	testFullTargetCorpusBudgetAndIdentity(t, "rust-v0.164.0", 139921,
		"47e9e66a3ac207125da8991d6cd96e7c9914f6d2754a59e4d5577c2dedb3f76f",
		"640a30f807adaa36953ecbdeb6bfb1bd3f9792dd8627ee3877b01f0888a6d27a")
}

func testFullTargetCorpusBudgetAndIdentity(t *testing.T, version string, wantProbes int, wantSequence, wantMetadata string) {
	t.Helper()
	f, err := os.Open("../testdata/" + version + "-full-target.json.gz")
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
	req := schemaoracle.Request{Context: context.Background(), Schemas: map[string][]byte{}, Bindings: fixture.Bindings, Package: "schemaoracleprobe", ImportPath: "github.com/ronhuafeng/llm-go/codexsdk/protocolv2", CloseObjects: true}
	for k, v := range fixture.Schemas {
		req.Schemas[k] = v
	}
	targets, err := schemaoracle.Build(req)
	if err != nil {
		t.Fatal(err)
	}
	source, err := schemaoracle.Render(req, targets)
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
