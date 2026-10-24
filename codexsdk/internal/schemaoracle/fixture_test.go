package schemaoracle

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type collectedTarget struct {
	Target
	Probes []Probe
}

func collectTargets(req Request) ([]collectedTarget, error) {
	var targets []collectedTarget
	err := Walk(req, func(t Target, p *Probe) error {
		if p == nil {
			targets = append(targets, collectedTarget{Target: t})
		} else {
			targets[len(targets)-1].Probes = append(targets[len(targets)-1].Probes, *p)
		}
		return nil
	})
	return targets, err
}
func collectStream(req Request) ([]byte, error) {
	var out bytes.Buffer
	err := Write(req, &out)
	return out.Bytes(), err
}
func runOracleFixture(t *testing.T, r Request, implementation string, wantPass bool) []byte {
	t.Helper()
	dir := t.TempDir()
	root, _ := filepath.Abs("../../..")
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\ngo 1.26.8\nrequire github.com/ronhuafeng/llm-go v0.0.0\nreplace github.com/ronhuafeng/llm-go => "+fmt.Sprintf("%q", root)+"\n"), 0600)
	sum, _ := os.ReadFile(filepath.Join(root, "go.sum"))
	os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0600)
	os.Mkdir(filepath.Join(dir, "subject"), 0700)
	os.WriteFile(filepath.Join(dir, "subject/payload.go"), []byte(strings.Replace(implementation, "package fixture", "package subject", 1)), 0600)
	os.WriteFile(filepath.Join(dir, "subject/payload_test.go"), []byte(`package subject;import("testing";"os");func TestMain(m *testing.M){os.Exit(0)}`), 0600)
	r.ImportPath = "fixture/subject"
	return runCompiledFixture(t, r, dir, wantPass)
}
func runCompiledFixture(t *testing.T, r Request, dir string, wantPass bool) []byte {
	t.Helper()
	source, err := Harness(r)
	if err != nil {
		t.Fatal(err)
	}
	os.Mkdir(filepath.Join(dir, "probe"), 0700)
	os.WriteFile(filepath.Join(dir, "probe/main.go"), source, 0600)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	binary := filepath.Join(dir, "decoder")
	cmd := exec.CommandContext(ctx, "go", "build", "-mod=mod", "-o", binary, "./probe")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile decoder: %v %s", err, out)
	}
	cmd = exec.CommandContext(ctx, binary)
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	var diagnostics bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { e := Write(r, writer); writer.CloseWithError(e); done <- e }()
	stream := bufio.NewReader(reader)
	var header Header
	err = ReadFrame(stream, &header)
	if err == nil {
		err = Check(ctx, header, stream, in, out, &diagnostics)
	}
	reader.Close()
	in.Close()
	out.Close()
	if err != nil {
		cmd.Process.Kill()
	}
	processErr := cmd.Wait()
	diagnostics.Write(stderr.Bytes())
	generationErr := <-done
	if (err == nil && processErr == nil && generationErr == nil) != wantPass {
		t.Fatalf("want pass=%t check=%v process=%v generation=%v\n%s", wantPass, err, processErr, generationErr, diagnostics.Bytes())
	}
	if err != nil {
		fmt.Fprintln(&diagnostics, err)
	}
	return diagnostics.Bytes()
}
