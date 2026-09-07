package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

func TestCallerDocsDefineInferenceCapability(t *testing.T) {
	file := parseAdapterFile(t, "adapter.go")
	spec := exportedTypeSpec(t, file, "Caller")
	comment := strings.Join(strings.Fields(strings.ToLower(typeDoc(spec))), " ")
	for _, want := range []string{
		"inference capability",
		"does not grant",
		"effect-free",
		"independently authorized outside the model request",
		"prompt is not authority",
		"external effect",
	} {
		if !strings.Contains(comment, want) {
			t.Fatalf("Caller docs = %q, want to state %q", comment, want)
		}
	}
}

func TestRequestSurfaceIsInferenceOnly(t *testing.T) {
	file := parseAdapterFile(t, "adapter.go")
	spec := exportedTypeSpec(t, file, "Request")
	structure, ok := spec.Type.(*ast.StructType)
	if !ok {
		t.Fatal("Request must be a struct")
	}
	got := map[string]string{}
	for _, field := range structure.Fields.List {
		names := field.Names
		if len(names) == 0 {
			t.Fatalf("Request embeds %s; the inference-only surface must use explicit fields", exprString(field.Type))
		}
		for _, name := range names {
			if !name.IsExported() {
				continue
			}
			got[name.Name] = exprString(field.Type)
			lower := strings.ToLower(name.Name)
			for _, banned := range []string{
				"tool", "effect", "authorit", "permission", "grant", "write",
				"mutat", "execut", "sandbox", "approval", "policy",
			} {
				if strings.Contains(lower, banned) {
					t.Fatalf("Request field %s expands the inference-only surface into effect or authority configuration", name.Name)
				}
			}
		}
	}
	want := map[string]string{
		"Prompt":       "string",
		"OutputSchema": "json.RawMessage",
	}
	if len(got) != len(want) {
		t.Fatalf("Request fields = %#v, want only %#v", got, want)
	}
	for name, typ := range want {
		if got[name] != typ {
			t.Fatalf("Request.%s type = %s, want %s", name, got[name], typ)
		}
	}
}

func TestCallerInterfaceIsCallOnly(t *testing.T) {
	file := parseAdapterFile(t, "adapter.go")
	spec := exportedTypeSpec(t, file, "Caller")
	iface, ok := spec.Type.(*ast.InterfaceType)
	if !ok {
		t.Fatal("Caller must be an interface")
	}
	if len(iface.Methods.List) != 1 {
		t.Fatalf("Caller methods = %d, want only Call", len(iface.Methods.List))
	}
	method := iface.Methods.List[0]
	if len(method.Names) != 1 || method.Names[0].Name != "Call" {
		t.Fatalf("Caller method = %#v, want Call", method.Names)
	}
}

func parseAdapterFile(t *testing.T, name string) *ast.File {
	t.Helper()
	path := filepath.Join(repoRoot(t), "llmadapter", name)
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func exportedTypeSpec(t *testing.T, file *ast.File, name string) *ast.TypeSpec {
	t.Helper()
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != name {
				continue
			}
			if gen.Doc != nil {
				typeSpec.Doc = gen.Doc
			}
			return typeSpec
		}
	}
	t.Fatalf("exported type %s not found", name)
	return nil
}

func typeDoc(spec *ast.TypeSpec) string {
	if spec.Doc != nil {
		return spec.Doc.Text()
	}
	return ""
}

func exprString(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		return exprString(value.X) + "." + value.Sel.Name
	default:
		return ""
	}
}
