package protocolsync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracle"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

func repairDigest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func repairPatch(root string) ([]byte, error) {
	paths, err := ChangedPaths(root)
	if err != nil {
		return nil, err
	}
	if len(paths) > 0 {
		args := append([]string{"add", "-N", "--"}, paths...)
		if err = runGit(root, args...); err != nil {
			return nil, err
		}
	}
	cmd := exec.Command("git", "-C", root, "diff", "--binary", "--no-ext-diff", "HEAD", "--")
	return cmd.Output()
}

// Discard excess command text in memory as well as in retained diagnostics.
type repairOutput struct{ strings.Builder }

func (w *repairOutput) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := 65536 - w.Len(); remaining > 0 {
		_, _ = w.Builder.Write(p[:min(remaining, n)])
	}
	return n, nil
}
func oracleCommandOutput(output io.Writer, cmd *exec.Cmd) ([]byte, error) {
	var b repairOutput
	if output == nil {
		output = io.Discard
	}
	combined := io.MultiWriter(&b, output)
	cmd.Stdout = combined
	cmd.Stderr = combined
	err := cmd.Run()
	return []byte(b.String()), err
}

// A failed subtest name selects a source-derived expectation, never a claimed
// actual value. The event parser retains failures and execution summaries.
func writeTargetOracleProbe(output io.Writer, targets []schemaoracle.Target, test string) error {
	prefix, indexText, ok := strings.Cut(test, "/probe_")
	if !ok {
		return nil
	}
	index, err := strconv.Atoi(indexText)
	if err != nil {
		return nil
	}
	for _, target := range targets {
		if prefix != "TestTrustedSchemaOracle/"+target.Schema || index < 0 || index >= len(target.Probes) {
			continue
		}
		p := target.Probes[index]
		return json.NewEncoder(output).Encode(struct {
			Kind       string                           `json:"kind"`
			Trust      string                           `json:"trust"`
			Schema     string                           `json:"schema"`
			Index      int                              `json:"index"`
			Valid      bool                             `json:"expected_valid"`
			Constraint string                           `json:"constraint"`
			Location   *schemaoracle.DiagnosticLocation `json:"location,omitempty"`
			Input      json.RawMessage                  `json:"input"`
		}{"schema_probe_expectation", "source-derived", target.Schema, index, p.Valid, p.Diagnostic, p.Location, json.RawMessage(p.JSON)})
	}
	return nil
}

type serializedOracleOutput struct {
	mu     sync.Mutex
	writer io.Writer
}

func (w *serializedOracleOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(p)
}
