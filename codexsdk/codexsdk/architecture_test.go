package codexsdk

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestCodexSDKDoesNotImportToolkitCallerOrBusinessPackages(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	forbiddenPrefixes := []string{
		"smart-contract",
		"github.com/ronhuafeng/llmkit-go",
		"github.com/ronhuafeng/llmcaller-codex-go",
	}

	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry == nil || entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range file.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			for _, forbidden := range forbiddenPrefixes {
				if importPath == forbidden || strings.HasPrefix(importPath, forbidden+"/") {
					t.Fatalf("codexsdk must stay protocol/transport-only; %s imports %q", path, importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCodexSDKPublicSurfaceHasNoRawProtocolEscapeHatch(t *testing.T) {
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	for _, iface := range publicProtocolInterfaces() {
		for i := 0; i < iface.NumMethod(); i++ {
			method := iface.Method(i)
			switch method.Name {
			case "Call", "CallProtocol", "JSONRPC", "RawCall":
				t.Fatalf("%s exposes raw protocol method %s", iface.Name(), method.Name)
			}
			for arg := 0; arg < method.Type.NumIn(); arg++ {
				argType := method.Type.In(arg)
				if argType == contextType {
					continue
				}
				assertNoPublicRawProtocolType(t, iface.Name()+"."+method.Name+" arg", argType, map[reflect.Type]bool{})
			}
			for result := 0; result < method.Type.NumOut(); result++ {
				assertNoPublicRawProtocolType(t, iface.Name()+"."+method.Name+" result", method.Type.Out(result), map[reflect.Type]bool{})
			}
		}
	}

	publicRootStructs := []reflect.Type{
		reflect.TypeOf(ClientOptions{}),
		reflect.TypeOf(StartThreadRunRequest{}),
		reflect.TypeOf(ResumeThreadRunRequest{}),
		reflect.TypeOf(ThreadRunResult{}),
		reflect.TypeOf(StartedThreadRun{}),
		reflect.TypeOf(ResumedThreadRun{}),
		reflect.TypeOf(ServerRequestResponse{}),
		reflect.TypeOf(ProtocolError{}),
		reflect.TypeOf(TurnError{}),
		reflect.TypeOf(InputStats{}),
		reflect.TypeOf(DiagnosticRef{}),
	}
	for _, typ := range publicRootStructs {
		assertNoPublicRawProtocolType(t, typ.Name(), typ, map[reflect.Type]bool{})
	}
}

func publicProtocolInterfaces() []reflect.Type {
	root := reflect.TypeOf((*Client)(nil)).Elem()
	out := []reflect.Type{root}
	seen := map[reflect.Type]bool{}
	for _, typ := range out {
		seen[typ] = true
	}
	for i := 0; i < root.NumMethod(); i++ {
		method := root.Method(i)
		for result := 0; result < method.Type.NumOut(); result++ {
			typ := method.Type.Out(result)
			if typ.Kind() != reflect.Interface || typ.PkgPath() != "github.com/ronhuafeng/codexsdk-go/codexsdk" || seen[typ] {
				continue
			}
			seen[typ] = true
			out = append(out, typ)
		}
	}
	return out
}

func TestCodexSDKPublishesTypedFactPreservingErrors(t *testing.T) {
	protocolErr := reflect.TypeOf(ProtocolError{})
	for _, field := range []string{"RequestID", "Method", "Code", "Message", "Data", "Err"} {
		if _, ok := protocolErr.FieldByName(field); !ok {
			t.Fatalf("ProtocolError missing %s", field)
		}
	}
	turnErr := reflect.TypeOf(TurnError{})
	for _, field := range []string{"ThreadID", "Turn", "Err"} {
		if _, ok := turnErr.FieldByName(field); !ok {
			t.Fatalf("TurnError missing %s", field)
		}
	}
}

func TestServerRequestResponseConstructorsCoverGeneratedKinds(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	generated := referencedNames(t, filepath.Join(root, "protocolv2", "protocol_types.gen.go"), func(name string) bool {
		return strings.HasPrefix(name, "ServerRequestKind") && name != "ServerRequestKind"
	})
	covered := referencedNames(t, filepath.Join(root, "server_response.go"), func(name string) bool {
		return strings.HasPrefix(name, "ServerRequestKind") && name != "ServerRequestKind"
	})
	if !reflect.DeepEqual(generated, covered) {
		t.Fatalf("generated server request kinds and response constructors differ:\ngenerated=%v\ncovered=%v", generated, covered)
	}
}

func TestExactFailClosedDispatchCoversGeneratedKinds(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	generated := referencedNames(t, filepath.Join(root, "protocolv2", "protocol_types.gen.go"), func(name string) bool {
		return strings.HasPrefix(name, "ServerRequestKind") && name != "ServerRequestKind"
	})
	covered := referencedNames(t, filepath.Join(root, "exact_server_request.go"), func(name string) bool {
		return strings.HasPrefix(name, "ServerRequestKind") && name != "ServerRequestKind"
	})
	if !reflect.DeepEqual(generated, covered) {
		t.Fatalf("generated server request kinds and exact fail-closed dispatch differ:\ngenerated=%v\ncovered=%v", generated, covered)
	}
}

func referencedNames(t *testing.T, path string, include func(string) bool) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	set := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if ok && include(identifier.Name) {
			set[identifier.Name] = true
		}
		return true
	})
	values := make([]string, 0, len(set))
	for value := range set {
		values = append(values, value)
	}
	slices.Sort(values)
	return values
}

func TestProtocolV2PublicSourceHasNoRawPayloadPassthrough(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	protocolDir := filepath.Join(root, "protocolv2")
	err = filepath.WalkDir(protocolDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry == nil || entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		source := string(raw)
		for _, forbidden := range []string{
			"json.RawMessage",
			"map[string]any",
			"map[string]interface{}",
			"type JSONRPCError ",
			"type JSONRPCNotification ",
			"type JSONRPCRequest ",
			"type JSONRPCResponse ",
			"type JSONRPCMessage ",
			"func NewJSONRPCMessage",
		} {
			if strings.Contains(source, forbidden) {
				t.Fatalf("protocolv2 public source %s contains raw passthrough marker %q", path, forbidden)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertNoPublicRawProtocolType(t *testing.T, path string, typ reflect.Type, seen map[reflect.Type]bool) {
	t.Helper()
	if typ == nil {
		return
	}
	if seen[typ] {
		return
	}
	seen[typ] = true

	switch typ.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Chan:
		assertNoPublicRawProtocolType(t, path, typ.Elem(), seen)
	case reflect.Map:
		if typ.Key().Kind() == reflect.String && typ.Elem().Kind() == reflect.Interface && typ.Elem().NumMethod() == 0 {
			t.Fatalf("%s exposes map[string]any protocol payload %s", path, typ)
		}
		assertNoPublicRawProtocolType(t, path+" key", typ.Key(), seen)
		assertNoPublicRawProtocolType(t, path+" value", typ.Elem(), seen)
	case reflect.Struct:
		if typ.PkgPath() == "encoding/json" && typ.Name() == "RawMessage" {
			t.Fatalf("%s exposes json.RawMessage", path)
		}
		if typ.PkgPath() != "" && typ.PkgPath() != "github.com/ronhuafeng/codexsdk-go/codexsdk" {
			return
		}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if !field.IsExported() {
				continue
			}
			assertNoPublicRawProtocolType(t, path+"."+field.Name, field.Type, seen)
		}
	case reflect.Func:
		for i := 0; i < typ.NumIn(); i++ {
			assertNoPublicRawProtocolType(t, path+" func arg", typ.In(i), seen)
		}
		for i := 0; i < typ.NumOut(); i++ {
			assertNoPublicRawProtocolType(t, path+" func result", typ.Out(i), seen)
		}
	case reflect.Interface:
		if typ.NumMethod() == 0 {
			t.Fatalf("%s exposes bare interface protocol payload %s", path, typ)
		}
		if typ.PkgPath() != "github.com/ronhuafeng/codexsdk-go/codexsdk" {
			return
		}
		for i := 0; i < typ.NumMethod(); i++ {
			method := typ.Method(i)
			for arg := 0; arg < method.Type.NumIn(); arg++ {
				assertNoPublicRawProtocolType(t, path+"."+method.Name+" arg", method.Type.In(arg), seen)
			}
			for result := 0; result < method.Type.NumOut(); result++ {
				assertNoPublicRawProtocolType(t, path+"."+method.Name+" result", method.Type.Out(result), seen)
			}
		}
	}
}

func methodCallNames(file *ast.File, name string) map[string]bool {
	calls := map[string]bool{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != name || fn.Recv == nil || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch expr := call.Fun.(type) {
			case *ast.Ident:
				calls[expr.Name] = true
			case *ast.SelectorExpr:
				calls[expr.Sel.Name] = true
			}
			return true
		})
	}
	return calls
}
