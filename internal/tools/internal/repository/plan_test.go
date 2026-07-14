package repository

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAffectedPlanUsesChangedPathsAndDeclaredRelationships(t *testing.T) {
	registered := planningFixture()
	tests := []struct {
		name  string
		paths []string
		want  []string
	}{
		{name: "toolkit closes over adapter", paths: []string{"llmkit/llmstep/step.go"}, want: []string{"codex-adapter", "llmkit"}},
		{name: "SDK closes over adapter", paths: []string{"codexsdk/codexsdk/client.go"}, want: []string{"codex-adapter", "codexsdk"}},
		{name: "adapter is a leaf", paths: []string{"llmcaller/codex/caller.go"}, want: []string{"codex-adapter"}},
		{name: "tools remain isolated", paths: []string{"internal/tools/cmd/repoctl/main.go"}, want: []string{"repo-tools"}},
		{name: "root orchestration affects all", paths: []string{"go.work"}, want: []string{"codex-adapter", "codexsdk", "llmkit", "repo-tools"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := affectedPlan(registered, "base", "head", test.paths)
			var got []string
			for _, candidate := range plan.Modules {
				got = append(got, candidate.ID)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("affected modules = %v, want %v", got, test.want)
			}
			if plan.WorkspaceRequired != (test.name != "tools remain isolated") {
				t.Fatalf("workspace_required = %t", plan.WorkspaceRequired)
			}
		})
	}
}

func TestAffectedPlanNormalizesChangedPaths(t *testing.T) {
	plan := affectedPlan(planningFixture(), "base", "head", []string{
		"llmkit/z.go",
		"llmkit/../llmkit/a.go",
		"llmkit/z.go",
	})
	want := []string{"llmkit/a.go", "llmkit/z.go"}
	if !reflect.DeepEqual(plan.ChangedPaths, want) {
		t.Fatalf("changed paths = %v, want %v", plan.ChangedPaths, want)
	}
}

func TestAffectedPlanRejectsUntrustedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	writeFile(t, filepath.Dir(path), filepath.Base(path), `{
  "format_version": 1,
  "base": "base",
  "head": "head",
  "changed_paths": [],
  "modules": [],
  "public_modules": [],
  "workspace_required": false,
  "unexpected": true
}`)
	_, err := ReadAffectedPlan(path)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("ReadAffectedPlan error = %v, want unknown-field rejection", err)
	}
}

func TestEvidenceSeparatesCheckoutFromPublishedArtifacts(t *testing.T) {
	evidence := newEvidence(EvidenceSubject{Kind: "checkout_source", Commit: "commit", Tree: "tree"})
	evidence.evidence.DoesNotProve = []string{"public proxy availability"}
	if evidence.evidence.Subject.Kind != "checkout_source" {
		t.Fatalf("kind = %q", evidence.evidence.Subject.Kind)
	}
	if len(evidence.evidence.DoesNotProve) == 0 {
		t.Fatal("checkout evidence does not state its proof boundary")
	}
}

func TestModuleArchiveRejectsRepositoryOnlyPaths(t *testing.T) {
	registered := planningFixture()
	candidate, ok := findModule(registered, "llmkit")
	if !ok {
		t.Fatal("missing fixture module")
	}
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/kit\n\ngo 1.23.0\n")
	writeFile(t, root, "kit.go", "package kit\n")
	if err := verifyModuleArchive(root, candidate, registered); err != nil {
		t.Fatalf("clean module archive: %v", err)
	}
	writeFile(t, root, ".github/workflows/ci.yml", "name: legacy\n")
	if err := verifyModuleArchive(root, candidate, registered); err == nil || !strings.Contains(err.Error(), "repository-only path") {
		t.Fatalf("repository path error = %v", err)
	}
}

func TestModuleArchiveRejectsSiblingModulePaths(t *testing.T) {
	registered := planningFixture()
	candidate, ok := findModule(registered, "llmkit")
	if !ok {
		t.Fatal("missing fixture module")
	}
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/kit\n\ngo 1.23.0\n")
	writeFile(t, root, "codexsdk/copied.go", "package codexsdk\n")
	if err := verifyModuleArchive(root, candidate, registered); err == nil || !strings.Contains(err.Error(), "sibling module path") {
		t.Fatalf("sibling path error = %v", err)
	}
}

func TestWorkspaceCanaryUsesEphemeralWorkspaceMetadata(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.work", "go 1.23.0\n")
	command := []string{"go", "env", "GOWORK"}
	if err := runWorkspaceCanary(root, command); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(root, ".repoctl-*.work*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary workspace residue: %v", matches)
	}
}

func TestAdapterUsesCurrentWorkspaceModules(t *testing.T) {
	registered := planningFixture()
	if !adapterUsesCurrentWorkspaceModules(registered) {
		t.Fatal("fixture adapter should use current toolkit and SDK paths")
	}
	adapter, ok := findModule(registered, "codex-adapter")
	if !ok {
		t.Fatal("missing adapter")
	}
	adapter.requires[0] = "example.com/legacy-kit"
	for index := range registered.Modules {
		if registered.Modules[index].ID == adapter.ID {
			registered.Modules[index] = adapter
		}
	}
	if adapterUsesCurrentWorkspaceModules(registered) {
		t.Fatal("legacy toolkit requirement must not claim current composition")
	}
}

func TestSourceIdentityRejectsTrackedAndUntrackedChanges(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "tracked.txt", "original\n")
	writeFile(t, root, ".gitignore", "ignored.go\n")
	runGit(t, root, "init")
	runGit(t, root, "config", "user.name", "Test")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "add", "tracked.txt", ".gitignore")
	runGit(t, root, "commit", "-m", "fixture")

	if err := verifySourceIdentity(root, nil); err != nil {
		t.Fatalf("clean source identity: %v", err)
	}
	writeFile(t, root, "tracked.txt", "changed\n")
	if err := verifySourceIdentity(root, nil); err == nil || !strings.Contains(err.Error(), "differs from HEAD") {
		t.Fatalf("tracked change error = %v", err)
	}
	writeFile(t, root, "tracked.txt", "original\n")
	writeFile(t, root, "new.go", "package fixture\n")
	if err := verifySourceIdentity(root, nil); err == nil || !strings.Contains(err.Error(), "untracked source") {
		t.Fatalf("untracked change error = %v", err)
	}
	if err := verifySourceIdentity(root, map[string]bool{"new.go": true}); err != nil {
		t.Fatalf("allowed untracked evidence file: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "new.go")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "ignored.go", "package fixture\n")
	if err := verifySourceIdentity(root, nil); err == nil || !strings.Contains(err.Error(), "ignored.go") {
		t.Fatalf("ignored source error = %v", err)
	}
}

func TestAffectedReadsExactGitRange(t *testing.T) {
	root := newArchitectureFixture(t)
	writeFile(t, root, "llmcaller/codex/go.mod", "module example.com/llmcaller/codex\n\ngo 1.23.0\n\nrequire example.com/llmkit v0.0.0\n")
	runGit(t, root, "init")
	runGit(t, root, "config", "user.name", "Test")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "base")
	base := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
	writeFile(t, root, "llmkit/package.go", "package fixture\n\nconst Changed = true\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "change toolkit")
	head := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))

	plan, err := Affected(root, base, head)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, candidate := range plan.Modules {
		got = append(got, candidate.ID)
	}
	want := []string{"codex-adapter", "llmkit"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("affected modules = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(plan.ChangedPaths, []string{"llmkit/package.go"}) {
		t.Fatalf("changed paths = %v", plan.ChangedPaths)
	}
}

func TestAffectedIncludesBothOwnersOfCrossModuleRename(t *testing.T) {
	root := newArchitectureFixture(t)
	writeFile(t, root, "llmcaller/codex/go.mod", "module example.com/llmcaller/codex\n\ngo 1.23.0\n\nrequire example.com/llmkit v0.0.0\n")
	runGit(t, root, "init")
	runGit(t, root, "config", "user.name", "Test")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "base")
	base := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
	runGit(t, root, "mv", "llmkit/package.go", "internal/tools/moved.go")
	runGit(t, root, "commit", "-m", "move across modules")
	head := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))

	plan, err := Affected(root, base, head)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, candidate := range plan.Modules {
		got = append(got, candidate.ID)
	}
	want := []string{"codex-adapter", "llmkit", "repo-tools"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("affected modules = %v, want %v", got, want)
	}
	wantPaths := []string{"internal/tools/moved.go", "llmkit/package.go"}
	if !reflect.DeepEqual(plan.ChangedPaths, wantPaths) {
		t.Fatalf("changed paths = %v, want %v", plan.ChangedPaths, wantPaths)
	}
}

func TestVerifyCheckoutRecomputesAffectedPlan(t *testing.T) {
	root := newArchitectureFixture(t)
	runGit(t, root, "init")
	runGit(t, root, "config", "user.name", "Test")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "base")
	base := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
	writeFile(t, root, "llmkit/package.go", "package fixture\n\nconst Changed = true\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "change")
	head := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
	plan, err := Affected(root, base, head)
	if err != nil {
		t.Fatal(err)
	}
	plan.Modules = []AffectedModule{}
	plan.PublicModules = []AffectedModule{}
	plan.WorkspaceRequired = false
	_, err = VerifyCheckout(root, plan)
	if err == nil || !strings.Contains(err.Error(), "does not match the checkout diff") {
		t.Fatalf("VerifyCheckout error = %v", err)
	}
}

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func planningFixture() registry {
	return registry{Modules: []module{
		{ID: "codex-adapter", Dir: "llmcaller/codex", Published: true, path: "example.com/adapter", goVersion: "1.23.0", requires: []string{"example.com/kit", "example.com/sdk"}},
		{ID: "codexsdk", Dir: "codexsdk", Published: true, path: "example.com/sdk", goVersion: "1.23.0"},
		{ID: "llmkit", Dir: "llmkit", Published: true, path: "example.com/kit", goVersion: "1.23.0"},
		{ID: "repo-tools", Dir: "internal/tools", path: "example.com/tools", goVersion: "1.23.0"},
	}}
}
