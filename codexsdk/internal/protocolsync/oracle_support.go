package protocolsync

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os/exec"
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

type serializedOracleOutput struct {
	mu     sync.Mutex
	writer io.Writer
}

func (w *serializedOracleOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(p)
}
