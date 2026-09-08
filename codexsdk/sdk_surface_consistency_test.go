package codexsdk

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type generatedFacadeManifest struct {
	Entries []generatedFacadeEntry `json:"entries"`
}

type generatedFacadeEntry struct {
	Direction             string `json:"direction"`
	FacadeStatus          string `json:"facade_status"`
	FacadeTarget          string `json:"facade_target"`
	Kind                  string `json:"kind"`
	Method                string `json:"method"`
	ParamsOrPayloadSchema string `json:"params_or_payload_schema"`
	ResponseType          string `json:"response_type"`
}

func TestGeneratedSDKFacadeMatchesClassifiedManifest(t *testing.T) {
	manifestRaw, err := os.ReadFile(filepath.Join("internal", "protocolschema", "appserver", "v2", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest generatedFacadeManifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		t.Fatal(err)
	}

	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	loader := &sdkSourceImporter{root: root, fset: token.NewFileSet(), cache: map[string]*types.Package{}}
	pkg, err := loader.Import("github.com/ronhuafeng/llm-go/codexsdk")
	if err != nil {
		t.Fatal(err)
	}
	client, ok := pkg.Scope().Lookup("Client").Type().(*types.Named)
	if !ok {
		t.Fatal("Client is not a named type")
	}
	clientMethods := types.NewMethodSet(types.NewPointer(client))
	wireMethodConstants := methodConstantsByWireName(t)
	facadeCalls := generatedFacadeProtocolCalls(t)

	expectedCalls := map[string]bool{}
	generatedEntries := 0
	for _, entry := range manifest.Entries {
		if entry.Direction != "client_to_server" || entry.Kind != "request" || entry.FacadeStatus != "generated" {
			continue
		}
		generatedEntries++
		accessorName, operationName, ok := splitGeneratedFacadeTarget(entry.FacadeTarget)
		if !ok {
			t.Errorf("method %q has invalid generated facade_target %q", entry.Method, entry.FacadeTarget)
			continue
		}
		key := accessorName + "." + operationName
		expectedCalls[key] = true

		accessor := lookupGeneratedMethod(clientMethods, accessorName)
		if accessor == nil {
			t.Errorf("manifest method %q expects Client.%s, but accessor is missing", entry.Method, accessorName)
			continue
		}
		accessorSignature := accessor.Type().(*types.Signature)
		if accessorSignature.Results().Len() != 1 {
			t.Errorf("Client.%s results = %s, want one facade value", accessorName, accessorSignature.Results())
			continue
		}
		facade, ok := accessorSignature.Results().At(0).Type().(*types.Named)
		if !ok || facade.Obj().Pkg() != pkg || facade.Obj().Name() != accessorName {
			t.Errorf("Client.%s result = %s, want codexsdk.%s", accessorName, accessorSignature.Results().At(0).Type(), accessorName)
			continue
		}
		operation := lookupGeneratedMethod(types.NewMethodSet(facade), operationName)
		if operation == nil {
			t.Errorf("manifest method %q expects %s.%s, but operation is missing", entry.Method, accessorName, operationName)
			continue
		}
		assertGeneratedFacadeSignature(t, key, operation.Type().(*types.Signature), entry)

		wantConstant, ok := wireMethodConstants[entry.Method]
		if !ok {
			t.Errorf("manifest method %q has no checked-in protocol method constant", entry.Method)
			continue
		}
		if got := facadeCalls[key]; got != wantConstant {
			t.Errorf("%s calls protocolv2.%s, want protocolv2.%s for method %q", key, got, wantConstant, entry.Method)
		}
	}
	if generatedEntries == 0 {
		t.Fatal("classified manifest has no generated facade entries")
	}
	for key := range facadeCalls {
		if !expectedCalls[key] {
			t.Errorf("sdk_surface.gen.go contains generated operation %s absent from classified manifest", key)
		}
	}
}

func assertGeneratedFacadeSignature(t *testing.T, key string, signature *types.Signature, entry generatedFacadeEntry) {
	t.Helper()
	wantParams := 1
	if entry.ParamsOrPayloadSchema != "" {
		wantParams++
	}
	if signature.Params().Len() != wantParams {
		t.Errorf("%s params = %s, want context.Context plus manifest params %q", key, signature.Params(), entry.ParamsOrPayloadSchema)
		return
	}
	if !isNamedType(signature.Params().At(0).Type(), "context", "Context") {
		t.Errorf("%s first param = %s, want context.Context", key, signature.Params().At(0).Type())
	}
	if entry.ParamsOrPayloadSchema != "" && !isNamedType(signature.Params().At(1).Type(), "github.com/ronhuafeng/llm-go/codexsdk/protocolv2", entry.ParamsOrPayloadSchema) {
		t.Errorf("%s params type = %s, want protocolv2.%s", key, signature.Params().At(1).Type(), entry.ParamsOrPayloadSchema)
	}
	if signature.Results().Len() != 2 {
		t.Errorf("%s results = %s, want protocol response and error", key, signature.Results())
		return
	}
	if !isNamedType(signature.Results().At(0).Type(), "github.com/ronhuafeng/llm-go/codexsdk/protocolv2", entry.ResponseType) {
		t.Errorf("%s response type = %s, want protocolv2.%s", key, signature.Results().At(0).Type(), entry.ResponseType)
	}
	if !types.Identical(signature.Results().At(1).Type(), types.Universe.Lookup("error").Type()) {
		t.Errorf("%s second result = %s, want error", key, signature.Results().At(1).Type())
	}
}

func isNamedType(value types.Type, packagePath, name string) bool {
	named, ok := value.(*types.Named)
	if !ok || named.Obj().Name() != name {
		return false
	}
	pkg := named.Obj().Pkg()
	if packagePath == "" {
		return pkg == nil
	}
	return pkg != nil && pkg.Path() == packagePath
}

func lookupGeneratedMethod(set *types.MethodSet, name string) *types.Func {
	for index := 0; index < set.Len(); index++ {
		method, ok := set.At(index).Obj().(*types.Func)
		if ok && method.Name() == name {
			return method
		}
	}
	return nil
}

func splitGeneratedFacadeTarget(target string) (string, string, bool) {
	accessor, operation, ok := strings.Cut(target, "().")
	return accessor, operation, ok && accessor != "" && operation != "" && !strings.Contains(operation, ".")
}

func methodConstantsByWireName(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join("protocolv2", "method_registry.gen.go"), nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	constants := map[string]string{}
	for _, declaration := range file.Decls {
		decl, ok := declaration.(*ast.GenDecl)
		if !ok || decl.Tok != token.CONST {
			continue
		}
		for _, spec := range decl.Specs {
			values, ok := spec.(*ast.ValueSpec)
			if !ok || len(values.Names) != len(values.Values) {
				continue
			}
			for index, name := range values.Names {
				if !strings.HasPrefix(name.Name, "Method") {
					continue
				}
				literal, ok := values.Values[index].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				wire, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				constants[wire] = name.Name
			}
		}
	}
	return constants
}

func generatedFacadeProtocolCalls(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sdk_surface.gen.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]string{}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || len(function.Recv.List) != 1 || function.Body == nil {
			continue
		}
		receiver := generatedReceiverName(function.Recv.List[0].Type)
		if receiver == "" {
			continue
		}
		key := receiver + "." + function.Name.Name
		var methodConstant string
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (selector.Sel.Name != "callProtocol" && selector.Sel.Name != "callProtocolNoParams") || len(call.Args) == 0 {
				return true
			}
			method, ok := call.Args[0].(*ast.SelectorExpr)
			if !ok {
				t.Errorf("%s protocol call does not use a protocolv2 method constant", key)
				return false
			}
			pkg, ok := method.X.(*ast.Ident)
			if !ok || pkg.Name != "protocolv2" {
				t.Errorf("%s protocol call method = %s, want protocolv2 constant", key, method.Sel.Name)
				return false
			}
			if methodConstant != "" {
				t.Errorf("%s contains more than one protocol call", key)
				return false
			}
			methodConstant = method.Sel.Name
			return true
		})
		if methodConstant == "" {
			t.Errorf("%s contains no callProtocol/callProtocolNoParams invocation", key)
			continue
		}
		if previous := calls[key]; previous != "" {
			t.Errorf("duplicate generated operation %s", key)
			continue
		}
		calls[key] = methodConstant
	}
	return calls
}

func generatedReceiverName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		if identifier, ok := value.X.(*ast.Ident); ok {
			return identifier.Name
		}
	}
	return ""
}
