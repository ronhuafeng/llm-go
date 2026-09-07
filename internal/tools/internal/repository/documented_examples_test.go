package repository

import (
	"fmt"
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

func TestUnpublishedInstallClassification(t *testing.T) {
	if unpublishedInstall(nil) {
		t.Fatal("nil is published")
	}
	if !unpublishedInstall(fmt.Errorf("go list -m -json github.com/ronhuafeng/llm-go/llmkit@v0.12.0: exit status 1: go: github.com/ronhuafeng/llm-go/llmkit@v0.12.0: GOVCS disallows using git")) {
		t.Fatal("GOVCS denial is unpublished")
	}
	if unpublishedInstall(fmt.Errorf("go list -m -json: exit status 1: dial tcp: i/o timeout")) {
		t.Fatal("network failure is not unpublished")
	}
}

func TestDocumentedExamplesSkipUnpublishedArchivedInstall(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "mod/README.md", "go get github.com/ronhuafeng/llm-go/llmkit@v9.9.9\n")
	writeFile(t, root, "mod/example_test.go", "package undocumented_test\n")
	writeFile(t, root, "mod/.changes/releases/v9.9.9/pending.json", `{"format_version":1}`+"\n")
	candidate := module{
		ID: "pending", Dir: "mod", Published: true,
		path: "github.com/ronhuafeng/llm-go/llmkit",
	}
	if err := verifyDocumentedExamples(root, candidate); err != nil {
		t.Fatal(err)
	}
}

func TestDocumentedExamplesRejectUnpublishedInstallWithoutArchive(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "mod/README.md", "go get github.com/ronhuafeng/llm-go/llmkit@v9.9.9\n")
	writeFile(t, root, "mod/example_test.go", "package undocumented_test\n")
	candidate := module{
		ID: "pending", Dir: "mod", Published: true,
		path: "github.com/ronhuafeng/llm-go/llmkit",
	}
	err := verifyDocumentedExamples(root, candidate)
	if err == nil || !strings.Contains(err.Error(), "llmkit@v9.9.9") {
		t.Fatalf("error = %v, want unresolved published tuple", err)
	}
}

func TestDocumentedAdapterExamplesCompileAgainstDeclaredInstall(t *testing.T) {
	root := repositoryRoot(t)
	candidate := module{
		ID: "codex-adapter", Dir: "llmcaller/codex", Published: true,
		path: "github.com/ronhuafeng/llm-go/llmcaller/codex",
	}
	if err := verifyDocumentedExamples(root, candidate); err != nil {
		t.Fatal(err)
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
