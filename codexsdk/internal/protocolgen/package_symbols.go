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
	seen := map[string]string{}
	for _, path := range paths {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, sources[path], 0)
		if err != nil {
			return err
		}
		if file.Name.Name != packageName {
			return fmt.Errorf("package declarations in %s use package %s, want %s", path, file.Name.Name, packageName)
		}
		add := func(scope string, id *ast.Ident) error {
			if id.Name == "_" {
				return nil
			}
			key := scope + id.Name
			location := fset.Position(id.Pos()).String()
			if previous, exists := seen[key]; exists {
				return unsupportedGeneratedSchema(path, "package symbol %s from %s conflicts with %s", key, location, previous)
			}
			seen[key] = location
			return nil
		}
		for _, decl := range file.Decls {
			switch node := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range node.Specs {
					switch item := spec.(type) {
					case *ast.TypeSpec:
						if err := add("", item.Name); err != nil {
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
									if err := add(item.Name.Name+".", id); err != nil {
										return err
									}
								}
							}
						}
					case *ast.ValueSpec:
						for _, id := range item.Names {
							if err := add("", id); err != nil {
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
				if err := add(scope, node.Name); err != nil {
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
