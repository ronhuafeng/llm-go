package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocumentedInstallVersionRequiresOneRelease(t *testing.T) {
	const modulePath = "github.com/ronhuafeng/llm-go/llmkit"
	version, err := documentedInstallVersion([]byte("go get github.com/ronhuafeng/llm-go/llmkit@v0.11.0\n"), modulePath)
	if err != nil || version != "v0.11.0" {
		t.Fatalf("version = %q, err = %v", version, err)
	}
	if _, err := documentedInstallVersion([]byte("install the latest tag\n"), modulePath); err == nil || !strings.Contains(err.Error(), "does not declare") {
		t.Fatalf("missing version error = %v", err)
	}
	mixed := []byte("go get github.com/ronhuafeng/llm-go/llmkit@v0.11.0\ngo get github.com/ronhuafeng/llm-go/llmkit@v0.10.0\n")
	if _, err := documentedInstallVersion(mixed, modulePath); err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("multiple version error = %v", err)
	}
}

func TestDocumentedLLMKitExamplesCompileAgainstPublishedInstall(t *testing.T) {
	root := repositoryRoot(t)
	candidate := module{
		ID: "llmkit", Dir: "llmkit", Published: true,
		path: "github.com/ronhuafeng/llm-go/llmkit",
	}
	if err := verifyDocumentedExamples(root, candidate); err != nil {
		t.Fatal(err)
	}
}

func TestDocumentedAdapterExamplesRejectStalePublishedTuple(t *testing.T) {
	root := repositoryRoot(t)
	candidate := module{
		ID: "codex-adapter", Dir: "llmcaller/codex", Published: true,
		path: "github.com/ronhuafeng/llm-go/llmcaller/codex",
	}
	err := verifyDocumentedExamples(root, candidate)
	if err == nil {
		t.Fatal("expected adapter README install tuple to fail current Value examples")
	}
	if !strings.Contains(err.Error(), "llmcaller/codex@v0.8.0") && !strings.Contains(err.Error(), "declared published tuple") {
		t.Fatalf("error = %v, want documented adapter tuple failure", err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", "..", "..", ".."))
}
