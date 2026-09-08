package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadOnlyEphemeralOptionsDocsStateConfidentialityBoundary(t *testing.T) {
	file := parseCallerFile(t, "caller.go")
	comment := strings.Join(strings.Fields(strings.ToLower(functionDoc(t, file, "ReadOnlyEphemeralOptions"))), " ")
	for _, want := range []string{
		"effect-safe, not disclosure-safe",
		"read-only is not confidential",
		"allowed read",
		"model and provider",
		"cwd",
		"workspace",
		"input selection",
		"confidentiality boundary",
		"ephemeral is not a provider-retention",
	} {
		if !strings.Contains(comment, want) {
			t.Fatalf("ReadOnlyEphemeralOptions docs = %q, want to state %q", comment, want)
		}
	}
}

func parseCallerFile(t *testing.T, name string) *ast.File {
	t.Helper()
	path := filepath.Join(repoRoot(t), name)
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func functionDoc(t *testing.T, file *ast.File, name string) string {
	t.Helper()
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != name || fn.Recv != nil {
			continue
		}
		if fn.Doc != nil {
			return fn.Doc.Text()
		}
		t.Fatalf("exported function %s has no doc comment", name)
	}
	t.Fatalf("exported function %s not found", name)
	return ""
}
