package codexsdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGeneratedFacadeAccessorsReturnConcreteOpaqueValues(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	loader := &sdkSourceImporter{root: root, fset: token.NewFileSet(), cache: map[string]*types.Package{}}
	pkg, err := loader.Import("github.com/ronhuafeng/llm-go/codexsdk")
	if err != nil {
		t.Fatal(err)
	}
	client := pkg.Scope().Lookup("Client").Type().(*types.Named)
	accessors := types.NewMethodSet(types.NewPointer(client))
	generatedAccessors := 0
	for index := 0; index < accessors.Len(); index++ {
		accessor := accessors.At(index).Obj()
		if !generatedPosition(loader.fset, accessor.Pos()) {
			continue
		}
		generatedAccessors++
		signature := accessor.Type().(*types.Signature)
		if signature.Results().Len() != 1 {
			t.Errorf("Client.%s results = %s, want one concrete facade", accessor.Name(), signature.Results())
			continue
		}
		named, ok := signature.Results().At(0).Type().(*types.Named)
		if !ok || named.Obj().Pkg() != pkg || named.Obj().Name() != accessor.Name() {
			t.Errorf("Client.%s result = %s, want same-named concrete facade", accessor.Name(), signature.Results().At(0).Type())
			continue
		}
		structure, ok := named.Underlying().(*types.Struct)
		if !ok {
			t.Errorf("facade %s underlying type is %T, want struct", accessor.Name(), named.Underlying())
			continue
		}
		for index := 0; index < structure.NumFields(); index++ {
			if structure.Field(index).Exported() {
				t.Errorf("facade %s exposes field %s", accessor.Name(), structure.Field(index).Name())
			}
		}
		methods := types.NewMethodSet(named)
		if methods.Len() == 0 {
			t.Errorf("facade %s has no generated operations", accessor.Name())
		}
		for methodIndex := 0; methodIndex < methods.Len(); methodIndex++ {
			method := methods.At(methodIndex).Obj()
			if !method.Exported() || !generatedPosition(loader.fset, method.Pos()) {
				t.Errorf("facade %s operation %s is not exported generated API", accessor.Name(), method.Name())
			}
		}
	}
	if generatedAccessors == 0 {
		t.Fatal("Client has no generated facade accessors")
	}
}

func TestGeneratedFacadeZeroValuesFailClosed(t *testing.T) {
	clientType := reflect.TypeOf((*Client)(nil))
	contextValue := reflect.ValueOf(context.Background())
	testedFacades := 0
	for accessorIndex := 0; accessorIndex < clientType.NumMethod(); accessorIndex++ {
		accessor := clientType.Method(accessorIndex)
		if accessor.Type.NumIn() != 1 || accessor.Type.NumOut() != 1 {
			continue
		}
		facadeType := accessor.Type.Out(0)
		if facadeType.Kind() != reflect.Struct || facadeType.PkgPath() != clientType.Elem().PkgPath() || facadeType.Name() != accessor.Name {
			continue
		}
		testedFacades++
		facadeValue := reflect.Zero(facadeType)
		for operationIndex := 0; operationIndex < facadeType.NumMethod(); operationIndex++ {
			operation := facadeType.Method(operationIndex)
			arguments := []reflect.Value{facadeValue, contextValue}
			if operation.Type.NumIn() == 3 {
				arguments = append(arguments, reflect.Zero(operation.Type.In(2)))
			}
			if operation.Type.NumIn() < 2 || operation.Type.NumIn() > 3 || operation.Type.NumOut() != 2 {
				t.Errorf("%s.%s has unexpected generated signature %s", facadeType.Name(), operation.Name, operation.Type)
				continue
			}
			calls := [][]reflect.Value{arguments}
			if operation.Type.IsVariadic() {
				// Both omission and one zero-valued optional parameter must
				// preserve the closed-client boundary.
				one := append([]reflect.Value(nil), arguments...)
				one[2] = reflect.MakeSlice(operation.Type.In(2), 1, 1)
				calls = append(calls, one)
			}
			for _, arguments := range calls {
				var results []reflect.Value
				if operation.Type.IsVariadic() {
					results = operation.Func.CallSlice(arguments)
				} else {
					results = operation.Func.Call(arguments)
				}
				err, ok := results[1].Interface().(error)
				if !ok || !errors.Is(err, ErrClientClosed) {
					t.Errorf("zero %s.%s error = %v, want ErrClientClosed", facadeType.Name(), operation.Name, results[1].Interface())
				}
			}
		}
	}
	if testedFacades == 0 {
		t.Fatal("Client has no concrete generated facades")
	}
}

type sdkSourceImporter struct {
	root     string
	fset     *token.FileSet
	cache    map[string]*types.Package
	compiled types.Importer
}

func TestSDKSourceImporterUsesActiveBuildConstraints(t *testing.T) {
	dir := t.TempDir()
	for name, source := range map[string]string{
		"active.go": "//go:build " + build.Default.GOOS + "\n\npackage codexsdk\nconst selected = 1\n",
		"other.go":  "//go:build !" + build.Default.GOOS + "\n\npackage codexsdk\nconst selected = 2\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	loader := &sdkSourceImporter{root: dir, fset: token.NewFileSet(), cache: map[string]*types.Package{}}
	pkg, err := loader.Import("github.com/ronhuafeng/llm-go/codexsdk")
	if err != nil {
		t.Fatal(err)
	}
	selected, ok := pkg.Scope().Lookup("selected").(*types.Const)
	if !ok || selected.Val().ExactString() != "1" {
		t.Fatal("source importer did not select the active Go build")
	}
}

func (i *sdkSourceImporter) Import(path string) (*types.Package, error) {
	if pkg := i.cache[path]; pkg != nil {
		return pkg, nil
	}
	const module = "github.com/ronhuafeng/llm-go/codexsdk"
	if path != module && !strings.HasPrefix(path, module+"/") {
		if i.compiled == nil {
			i.compiled = importer.ForCompiler(i.fset, "gc", i.openExport)
		}
		return i.compiled.Import(path)
	}
	dir := i.root
	if path != module {
		dir = filepath.Join(i.root, strings.TrimPrefix(path, module+"/"))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []*ast.File
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		match, err := build.Default.MatchFile(dir, entry.Name())
		if err != nil {
			return nil, err
		}
		if !match {
			continue
		}
		file, err := parser.ParseFile(i.fset, filepath.Join(dir, entry.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	config := types.Config{Importer: i}
	pkg, err := config.Check(path, i.fset, files, nil)
	if err != nil {
		return nil, err
	}
	i.cache[path] = pkg
	return pkg, nil
}

func (i *sdkSourceImporter) openExport(path string) (io.ReadCloser, error) {
	command := exec.Command("go", "list", "-export", "-json", path)
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("go list %s: %w", path, err)
	}
	var listed struct{ Export string }
	if err := json.Unmarshal(output, &listed); err != nil {
		return nil, err
	}
	return os.Open(listed.Export)
}

func generatedPosition(fset *token.FileSet, position token.Pos) bool {
	filename := filepath.ToSlash(fset.Position(position).Filename)
	return strings.HasSuffix(filename, ".gen.go") || strings.Contains(filename, "/protocolv2/")
}
