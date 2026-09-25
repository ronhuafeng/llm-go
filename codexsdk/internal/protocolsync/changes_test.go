package protocolsync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssertCleanAndStagePaths(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	if err := AssertClean(repo); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "codexsdk", "client.go"), []byte("package codexsdk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AssertClean(repo); err == nil {
		t.Fatal("dirty worktree must fail assert-clean")
	}
	if _, err := StagePaths(repo, "mechanical"); err == nil || !strings.Contains(err.Error(), "mechanical") {
		t.Fatalf("handwritten file must not stage as mechanical: %v", err)
	}
	if _, err := StagePaths(repo, "final"); err != nil {
		t.Fatal(err)
	}
	cached, err := gitOutput(repo, "diff", "--cached", "--name-only")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cached, "codexsdk/client.go") {
		t.Fatalf("staged paths = %q", cached)
	}
}

func TestValidatePathsAgentAllowsHandwrittenAndRejectsMechanical(t *testing.T) {
	if err := validatePaths([]string{"codexsdk/client.go", "codexsdk/internal/protocolgen/type_plan.go"}, "agent"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"codexsdk/sdk_surface.gen.go",
		"codexsdk/protocolv2/protocol_types.gen.go",
		"codexsdk/internal/protocolschema/appserver/v2/manifest.json",
		"codexsdk/internal/protocolsync/sync.go",
		"codexsdk/internal/protocolsync/changes.go",
		"codexsdk/internal/protocolsync/publish.go",
		"codexsdk/internal/protocolupgrade/plan.go",
		"codexsdk/internal/protocolupgrade/apply.go",
		"codexsdk/internal/protocolupgrade/exact.go",
		"codexsdk/internal/generatedcheck/check.go",
		"codexsdk/internal/cmd/protocolupgrade/main.go",
	} {
		if err := validatePaths([]string{path}, "agent"); err == nil {
			t.Fatalf("agent path %s should be rejected as mechanical", path)
		}
	}
}

func TestStagePathsRejectsEmptySet(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	if _, err := StagePaths(repo, "final"); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidatePathsRejectsCacheAndAgents(t *testing.T) {
	err := validatePaths([]string{"codexsdk/.cache/openai-codex/README"}, "final")
	if err == nil || !strings.Contains(err.Error(), "final") {
		t.Fatalf("err = %v", err)
	}
	err = validatePaths([]string{"README.md"}, "final")
	if err == nil {
		t.Fatal("non-codexsdk path must fail final scope")
	}
}

func TestFinalScopeRejectsControlAndUnknownPhase(t *testing.T) {
	for _, phase := range []string{"agent", "final", "typo"} {
		if err := validatePaths([]string{"codexsdk/internal/protocolsync/sync.go"}, phase); err == nil || !strings.Contains(err.Error(), "needs-maintainer") {
			t.Fatalf("phase %s allowed control change: %v", phase, err)
		}
	}
	if err := validatePaths([]string{"codexsdk/internal/protocolgen/type_plan.go", "codexsdk/protocolv2/protocol_types.gen.go"}, "final"); err != nil {
		t.Fatal(err)
	}
}

func TestMechanicalScopeRejectsHandwrittenBaselineControl(t *testing.T) {
	for _, p := range []string{
		"codexsdk/internal/protocolschema/appserver/v2/baseline.go",
		"codexsdk/internal/protocolschema/appserver/v2/schema_baseline_test.go",
		"codexsdk/internal/protocolschema/appserver/v2/new_control.go",
		"codexsdk/protocolv2/unknown.gen.go",
	} {
		for _, phase := range []string{"mechanical", "final"} {
			if err := validatePaths([]string{p}, phase); err == nil {
				t.Fatalf("%s admits non-output %s", phase, p)
			}
		}
	}
}
