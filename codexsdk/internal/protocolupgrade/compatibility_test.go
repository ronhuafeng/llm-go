package protocolupgrade

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreparedCandidateCompatibilityIncludesTestsWithoutExecuting(t *testing.T) {
	root := t.TempDir()
	put := func(name, source string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	put("go.mod", "module fixture\n\ngo 1.26\n")
	put("codexsdk/sdk_surface.gen.go", "package codexsdk\n")
	put("codexsdk/protocolv2/protocol_types.gen.go", "package protocolv2\ntype Removed struct{}\n")
	put("codexsdk/protocolv2/compat_test.go", "package protocolv2\nimport \"testing\"\nfunc init(){panic(\"must never execute\")}\nfunc TestCompatibility(t *testing.T){_ = Removed{}}\n")
	makePlan := func(source string) PlanResult {
		return PlanResult{Status: PlanReady, prepared: &preparedCandidate{request: ApplyRequest{ModuleRoot: filepath.Join(root, "codexsdk")}, files: map[string][]byte{"protocolv2/protocol_types.gen.go": []byte(source)}}}
	}
	result, err := makePlan("package protocolv2\n").CheckCompatibility()
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != PlanSemanticUnresolved || result.Issue == nil || !strings.Contains(result.Issue.Reason, "undefined: Removed") {
		t.Fatalf("result = %+v", result)
	}
	if _, err := result.Apply(); err == nil {
		t.Fatal("incompatible plan remained applicable")
	}
	accepted, err := os.ReadFile(filepath.Join(root, "codexsdk/protocolv2/protocol_types.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(accepted), "type Removed") {
		t.Fatal("accepted file changed")
	}
	result, err = makePlan(string(accepted)).CheckCompatibility()
	if err != nil || result.Status != PlanReady {
		t.Fatalf("compatible result=%+v err=%v", result, err)
	}
	put("codexsdk/protocolv2/compat_test.go", "package protocolv2\nvar _ = MissingDependency\n")
	_, err = makePlan("package protocolv2\n").CheckCompatibility()
	if err == nil {
		t.Fatal("broken accepted build was misclassified as candidate incompatibility")
	}
}
