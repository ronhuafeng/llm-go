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

func TestDocumentedExamplesSkipUnpublishedArchivedInstall(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "mod/README.md", "go get example.test/pending@v9.9.9\n")
	writeFile(t, root, "mod/example_test.go", "package undocumented_test\n")
	writeFile(t, root, "mod/.changes/releases/v9.9.9/pending.json", `{"format_version":1}`+"\n")
	initGitRepo(t, root)
	candidate := module{
		ID: "pending", Dir: "mod", Published: true,
		path: "example.test/pending",
	}
	if err := verifyDocumentedExamples(root, candidate); err != nil {
		t.Fatal(err)
	}
}

func TestUnpublishedArchivedInstallUsesLocalTags(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "llmcaller/codex/.changes/releases/v9.9.9/pending.json", `{"format_version":1}`+"\n")
	initGitRepo(t, root)
	candidate := module{
		ID: "codex-adapter", Dir: "llmcaller/codex", Published: true,
		path: "example.test/adapter",
	}

	pending, err := unpublishedArchivedInstall(root, candidate, "v9.9.9")
	if err != nil || !pending {
		t.Fatalf("archived without tag: pending=%v err=%v", pending, err)
	}

	runGit(t, root, "tag", "llmkit/v9.9.9")
	runGit(t, root, "tag", "llmcaller/codex/v9.9.8")
	pending, err = unpublishedArchivedInstall(root, candidate, "v9.9.9")
	if err != nil || !pending {
		t.Fatalf("other-module or other-version tag: pending=%v err=%v", pending, err)
	}

	runGit(t, root, "tag", "llmcaller/codex/v9.9.9")
	pending, err = unpublishedArchivedInstall(root, candidate, "v9.9.9")
	if err != nil || pending {
		t.Fatalf("archived with local module tag: pending=%v err=%v", pending, err)
	}
}

func TestUnpublishedArchivedInstallRequiresArchiveFragments(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "mod/README.md", "go get example.test/pending@v9.9.9\n")
	initGitRepo(t, root)
	candidate := module{ID: "pending", Dir: "mod", Published: true, path: "example.test/pending"}
	pending, err := unpublishedArchivedInstall(root, candidate, "v9.9.9")
	if err != nil || pending {
		t.Fatalf("no archive: pending=%v err=%v", pending, err)
	}
}

func TestDocumentedExamplesRejectUnpublishedInstallWithoutArchive(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "mod/README.md", "go get example.test/pending@v9.9.9\n")
	writeFile(t, root, "mod/example_test.go", "package undocumented_test\n")
	candidate := module{
		ID: "pending", Dir: "mod", Published: true,
		path: "example.test/pending",
	}
	err := verifyDocumentedExamples(root, candidate)
	if err == nil || !strings.Contains(err.Error(), "example.test/pending@v9.9.9") {
		t.Fatalf("error = %v, want unresolved published tuple", err)
	}
}

func TestPRVerificationCheckoutFetchesTags(t *testing.T) {
	root := repositoryRoot(t)
	workflow, err := os.ReadFile(filepath.Join(root, ".github/workflows/pr-verification.yml"))
	if err != nil {
		t.Fatal(err)
	}
	checkouts := strings.Count(string(workflow), "uses: actions/checkout@v7")
	fetches := strings.Count(string(workflow), "fetch-tags: true")
	if checkouts == 0 || fetches != checkouts {
		t.Fatalf("PR verification checkout steps = %d, fetch-tags = %d; tags are required so archived README versions are not treated as pending after publish", checkouts, fetches)
	}
}

func initGitRepo(t *testing.T, root string) {
	t.Helper()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.name", "Test")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "fixture")
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
