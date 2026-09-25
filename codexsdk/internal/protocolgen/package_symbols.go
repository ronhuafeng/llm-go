package protocolgen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ValidateGeneratedPackage checks declarations in one real Go package, including
// receiver scopes. Source locations come directly from parsed files; generation
// does not reconstruct upstream ownership from emitted names.
func ValidateGeneratedPackage(packageName, handwrittenDir string, generated map[string][]byte) error {
	return validateGeneratedPackage(packageName, handwrittenDir, generated, nil)
}

// knownSources contains only facts already selected by construction. Helpers
// without a direct schema owner keep their Go location; no naming is replayed.
func validateGeneratedPackage(packageName, handwrittenDir string, generated map[string][]byte, knownSources map[string][]string) error {
	sources := map[string][]byte{}
	if handwrittenDir != "" {
		entries, err := os.ReadDir(handwrittenDir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, ".gen.go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			source, err := os.ReadFile(filepath.Join(handwrittenDir, name))
			if err != nil {
				return err
			}
			sources[name] = source
		}
	}
	for name, source := range generated {
		if _, exists := sources[name]; exists {
			return fmt.Errorf("generated file %s overlaps handwritten source", name)
		}
		sources[name] = source
	}
	paths := make([]string, 0, len(sources))
	for path := range sources {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	type declaration struct {
		location  string
		generated bool
		sources   []string
	}
	seen := map[string]declaration{}
	for _, path := range paths {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, sources[path], 0)
		if err != nil {
			return err
		}
		if file.Name.Name != packageName {
			return fmt.Errorf("package declarations in %s use package %s, want %s", path, file.Name.Name, packageName)
		}
		add := func(kind, scope string, id *ast.Ident) error {
			if id.Name == "_" {
				return nil
			}
			key := scope + id.Name
			location := fset.Position(id.Pos()).String()
			_, isGenerated := generated[path]
			sources := knownSources[path+":"+kind+":"+scope+id.Name]
			if previous, exists := seen[key]; exists {
				reason := fmt.Sprintf("package symbol %s from %s conflicts with %s", key, location, previous.location)
				if !isGenerated && !previous.generated {
					return fmt.Errorf("%s", reason)
				}
				owners := append(append([]string(nil), sources...), previous.sources...)
				sourcePath := path
				if len(owners) > 0 {
					sourcePath = owners[0]
					reason += "; selected schema sources: " + strings.Join(owners, ", ")
				}
				return unsupportedGeneratedSchema(sourcePath, "%s", reason)
			}
			seen[key] = declaration{location: location, generated: isGenerated, sources: sources}
			return nil
		}
		for _, decl := range file.Decls {
			switch node := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range node.Specs {
					switch item := spec.(type) {
					case *ast.TypeSpec:
						if err := add("type", "", item.Name); err != nil {
							return err
						}
						if structure, ok := item.Type.(*ast.StructType); ok {
							for _, field := range structure.Fields.List {
								names := field.Names
								if len(names) == 0 {
									if id := receiverIdentifier(field.Type); id != nil {
										names = []*ast.Ident{id}
									}
								}
								for _, id := range names {
									if err := add("field", item.Name.Name+".", id); err != nil {
										return err
									}
								}
							}
						}
					case *ast.ValueSpec:
						for _, id := range item.Names {
							if err := add(node.Tok.String(), "", id); err != nil {
								return err
							}
						}
					}
				}
			case *ast.FuncDecl:
				scope := ""
				if node.Recv != nil {
					id := receiverIdentifier(node.Recv.List[0].Type)
					if id == nil {
						return fmt.Errorf("unsupported receiver in %s", path)
					}
					scope = id.Name + "."
				} else if node.Name.Name == "init" {
					continue
				}
				if err := add("func", scope, node.Name); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func receiverIdentifier(expr ast.Expr) *ast.Ident {
	switch node := expr.(type) {
	case *ast.Ident:
		return node
	case *ast.StarExpr:
		return receiverIdentifier(node.X)
	case *ast.IndexExpr:
		return receiverIdentifier(node.X)
	case *ast.IndexListExpr:
		return receiverIdentifier(node.X)
	case *ast.SelectorExpr:
		return node.Sel
	}
	return nil
}
