package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareCLIWritesReportsSilently(t *testing.T) {
	root := t.TempDir()
	baseline := filepath.Join(root, "baseline")
	candidate := filepath.Join(root, "candidate")
	reports := filepath.Join(root, "reports")
	writeSchemaSet(t, baseline)
	writeSchemaSet(t, candidate)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := run([]string{
		"compare",
		"-baseline", baseline,
		"-candidate", candidate,
		"-reports", reports,
		"-source-commit", strings.Repeat("1", 40),
		"-source-ref", "rust-v0.141.0",
		"-source-ref-kind", "stable_rust_tag",
		"-codex-version", "codex-cli test",
		"-generator", "compare-only",
		"-generator-detail", "candidate schema",
	}, stdout, stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("compare CLI should be silent, stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(reports, "drift_summary.json")); err != nil {
		t.Fatal(err)
	}
}

func TestCompareCLIJSONDoesNotWriteReports(t *testing.T) {
	root := t.TempDir()
	baseline := filepath.Join(root, "baseline")
	candidate := filepath.Join(root, "candidate")
	writeSchemaSet(t, baseline)
	writeSchemaSet(t, candidate)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := run([]string{
		"compare",
		"-baseline", baseline,
		"-candidate", candidate,
		"-source-commit", strings.Repeat("1", 40),
		"-source-ref", "rust-v0.141.0",
		"-source-ref-kind", "stable_rust_tag",
		"-codex-version", "codex-cli test",
		"-generator", "compare-only",
		"-generator-detail", "candidate schema",
		"-json",
	}, stdout, stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != "clean" {
		t.Fatalf("status = %#v", payload["status"])
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, "reports")); err == nil {
		t.Fatal("json mode wrote reports")
	}
}

func TestCheckCLIAcceptsCheckedInModule(t *testing.T) {
	moduleRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := run([]string{"check", "-module-root", moduleRoot}, stdout, stderr)
	if code != 0 {
		t.Fatalf("checked-in module failed check: %s", stderr.String())
	}
}

func writeSchemaSet(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	aggregate := []byte(`{"oneOf":[{"properties":{"method":{"enum":["thread/start"]},"params":{"$ref":"#/definitions/ThreadStartParams"}}}]}` + "\n")
	for _, name := range []string{"ClientRequest.json", "ServerRequest.json", "ServerNotification.json", "ClientNotification.json"} {
		data := []byte(`{"oneOf":[]}` + "\n")
		if name == "ClientRequest.json" {
			data = aggregate
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
