package publicapi

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const sdkModule = "github.com/ronhuafeng/llm-go/codexsdk"

// Export derives the exported SDK surface from source, omitting generated
// protocol files. Callers compare two derived texts when they need a
// compatibility report.
func Export(root string) (string, error) {
	loader := &sdkSourceImporter{root: root, fset: token.NewFileSet(), cache: map[string]*types.Package{}}
	pkg, err := loader.Import(sdkModule)
	if err != nil {
		return "", err
	}
	declarations := classifiedDeclarations(loader.fset, pkg)
	sort.Strings(declarations)
	if len(declarations) == 0 {
		return "", fmt.Errorf("derived public API is empty")
	}
	return strings.Join(declarations, "\n") + "\n", nil
}

type sdkSourceImporter struct {
	root     string
	fset     *token.FileSet
	cache    map[string]*types.Package
	compiled types.Importer
}

func (i *sdkSourceImporter) Import(path string) (*types.Package, error) {
	if pkg := i.cache[path]; pkg != nil {
		return pkg, nil
	}
	if path != sdkModule && !strings.HasPrefix(path, sdkModule+"/") {
		if i.compiled == nil {
			i.compiled = importer.ForCompiler(i.fset, "gc", i.openExport)
		}
		return i.compiled.Import(path)
	}
	dir := i.root
	if path != sdkModule {
		dir = filepath.Join(i.root, strings.TrimPrefix(path, sdkModule+"/"))
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
		file, err := parser.ParseFile(i.fset, filepath.Join(dir, entry.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	pkg, err := (&types.Config{Importer: i}).Check(path, i.fset, files, nil)
	if err != nil {
		return nil, err
	}
	i.cache[path] = pkg
	return pkg, nil
}

func (i *sdkSourceImporter) openExport(path string) (io.ReadCloser, error) {
	command := exec.Command("go", "list", "-export", "-json", path)
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("go list %s: %w", path, err)
	}
	var listed struct{ Export string }
	if err := json.Unmarshal(output, &listed); err != nil {
		return nil, fmt.Errorf("go list %s: %w", path, err)
	}
	return os.Open(listed.Export)
}

func classifiedDeclarations(fset *token.FileSet, pkg *types.Package) []string {
	qualifier := func(other *types.Package) string { return other.Path() }
	var declarations []string
	for _, name := range pkg.Scope().Names() {
		object := pkg.Scope().Lookup(name)
		if !object.Exported() || generatedPosition(fset, object.Pos()) {
			continue
		}
		declarations = append(declarations, publicObjectString(object, qualifier))
		typeName, ok := object.(*types.TypeName)
		if !ok {
			continue
		}
		named, ok := typeName.Type().(*types.Named)
		if !ok {
			continue
		}
		methods := types.NewMethodSet(types.NewPointer(named))
		for index := 0; index < methods.Len(); index++ {
			method := methods.At(index).Obj()
			if method.Exported() && !generatedPosition(fset, method.Pos()) {
				declarations = append(declarations, fmt.Sprintf("method %s.%s.%s%s", pkg.Path(), named.Obj().Name(), method.Name(), types.TypeString(method.Type(), qualifier)))
			}
		}
	}
	return declarations
}

func publicObjectString(object types.Object, qualifier types.Qualifier) string {
	typeName, ok := object.(*types.TypeName)
	if !ok {
		return types.ObjectString(object, qualifier)
	}
	named, ok := typeName.Type().(*types.Named)
	if !ok {
		return types.ObjectString(object, qualifier)
	}
	structure, ok := named.Underlying().(*types.Struct)
	if !ok {
		return types.ObjectString(object, qualifier)
	}
	for index := 0; index < structure.NumFields(); index++ {
		if structure.Field(index).Exported() {
			return types.ObjectString(object, qualifier)
		}
	}
	return fmt.Sprintf("type %s.%s struct{ /* unexported fields */ }", object.Pkg().Path(), object.Name())
}

func generatedPosition(fset *token.FileSet, position token.Pos) bool {
	filename := filepath.ToSlash(fset.Position(position).Filename)
	return strings.HasSuffix(filename, ".gen.go") || strings.Contains(filename, "/protocolv2/")
}
