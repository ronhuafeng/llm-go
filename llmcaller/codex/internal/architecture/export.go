package architecture

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

const adapterModule = "github.com/ronhuafeng/llm-go/llmcaller/codex"

// ExportPublicAPI derives the exported adapter surface from source. It is not a
// committed allowlist; callers compare two derived texts when they need a
// compatibility report.
func ExportPublicAPI(root string) (string, error) {
	loader := &callerSourceImporter{root: root, fset: token.NewFileSet()}
	pkg, err := loader.Import(adapterModule)
	if err != nil {
		return "", err
	}
	declarations := exportedDeclarations(pkg)
	sort.Strings(declarations)
	if len(declarations) == 0 {
		return "", fmt.Errorf("derived public API is empty")
	}
	return strings.Join(declarations, "\n") + "\n", nil
}

type callerSourceImporter struct {
	root     string
	fset     *token.FileSet
	compiled types.Importer
}

func (i *callerSourceImporter) Import(path string) (*types.Package, error) {
	if path != adapterModule {
		if i.compiled == nil {
			i.compiled = importer.ForCompiler(i.fset, "gc", i.openExport)
		}
		return i.compiled.Import(path)
	}
	entries, err := os.ReadDir(i.root)
	if err != nil {
		return nil, err
	}
	var files []*ast.File
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(i.fset, filepath.Join(i.root, entry.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return (&types.Config{Importer: i}).Check(adapterModule, i.fset, files, nil)
}

func (i *callerSourceImporter) openExport(path string) (io.ReadCloser, error) {
	command := exec.Command("go", "list", "-export", "-json", path)
	command.Env = append(os.Environ(), "GOWORK=off")
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

func exportedDeclarations(pkg *types.Package) []string {
	qualifier := func(other *types.Package) string { return other.Path() }
	var declarations []string
	for _, name := range pkg.Scope().Names() {
		object := pkg.Scope().Lookup(name)
		if !object.Exported() {
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
			if method.Exported() {
				declarations = append(declarations, fmt.Sprintf("method %s.%s.%s%s", pkg.Path(), named.Obj().Name(), method.Name(), types.TypeString(method.Type(), qualifier)))
			}
		}
	}
	return declarations
}

func publicObjectString(object types.Object, qualifier types.Qualifier) string {
	typeName, ok := object.(*types.TypeName)
	if !ok || typeName.IsAlias() {
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

	var fields []string
	for index := 0; index < structure.NumFields(); index++ {
		field := structure.Field(index)
		if !field.Exported() {
			continue
		}
		declaration := types.TypeString(field.Type(), qualifier)
		if !field.Embedded() {
			declaration = field.Name() + " " + declaration
		}
		if tag := structure.Tag(index); tag != "" {
			declaration += " `" + tag + "`"
		}
		fields = append(fields, declaration)
	}
	return fmt.Sprintf("type %s.%s%s struct{%s}", qualifier(typeName.Pkg()), typeName.Name(), publicTypeParameters(named, qualifier), strings.Join(fields, "; "))
}

func publicTypeParameters(named *types.Named, qualifier types.Qualifier) string {
	parameters := named.TypeParams()
	if parameters == nil || parameters.Len() == 0 {
		return ""
	}
	declarations := make([]string, 0, parameters.Len())
	for index := 0; index < parameters.Len(); index++ {
		parameter := parameters.At(index)
		declarations = append(declarations, parameter.Obj().Name()+" "+types.TypeString(parameter.Constraint(), qualifier))
	}
	return "[" + strings.Join(declarations, ", ") + "]"
}
