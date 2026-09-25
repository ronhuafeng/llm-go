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

// PackageSymbol is an actual package-level Go declaration and its source.
// SchemaPath points to the upstream owner when a declaration comes from a schema.
type PackageSymbol struct {
	Name       string
	SourcePath string
	SchemaPath string
}

// ValidateGeneratedPackage uses one namespace for all protocolv2 generators and
// the handwritten declarations they share a package with. It reads declarations
// from generated Go, so adding an emitter cannot silently bypass this check.
func ValidateGeneratedPackage(plan ProtocolTypePlan, manifest Manifest, handwrittenDir string, generated map[string][]byte) ([]PackageSymbol, error) {
	typeOrigins, err := generatedTypeOrigins(plan)
	if err != nil {
		return nil, err
	}
	methodOrigins := map[string]string{}
	for _, entry := range manifest.Entries {
		if entry.SourceSchema != "" {
			methodOrigins["const\x00"+methodConstName(entry.Method)] = entry.SourceSchema
		}
	}
	var symbols []PackageSymbol
	if handwrittenDir != "" {
		entries, err := os.ReadDir(handwrittenDir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, ".gen.go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(handwrittenDir, name)
			source, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			claims, err := packageDeclarations(filepath.ToSlash(filepath.Join("protocolv2", name)), source, nil)
			if err != nil {
				return nil, err
			}
			symbols = append(symbols, claims...)
		}
	}
	files := make([]string, 0, len(generated))
	for name := range generated {
		files = append(files, name)
	}
	sort.Strings(files)
	for _, name := range files {
		var origins map[string]string
		switch filepath.Base(name) {
		case "protocol_types.gen.go":
			origins = typeOrigins
		case "method_registry.gen.go":
			origins = methodOrigins
		}
		claims, err := packageDeclarations(name, generated[name], origins)
		if err != nil {
			return nil, err
		}
		symbols = append(symbols, claims...)
	}
	seen := map[string]PackageSymbol{}
	for _, symbol := range symbols {
		if previous, exists := seen[symbol.Name]; exists {
			path := symbol.SchemaPath
			if path == "" {
				path = previous.SchemaPath
			}
			reason := fmt.Sprintf("package symbol %s from %s conflicts with %s", symbol.Name, symbolOrigin(symbol), symbolOrigin(previous))
			if path != "" {
				return symbols, unsupportedGeneratedSchema(path, "%s", reason)
			}
			return symbols, fmt.Errorf("%s", reason)
		}
		seen[symbol.Name] = symbol
	}
	return symbols, nil
}

func symbolOrigin(symbol PackageSymbol) string {
	if symbol.SchemaPath != "" {
		return symbol.SchemaPath
	}
	return symbol.SourcePath
}

func packageDeclarations(path string, source []byte, origins map[string]string) ([]PackageSymbol, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, source, 0)
	if err != nil {
		return nil, fmt.Errorf("parse package declarations in %s: %w", path, err)
	}
	if file.Name.Name != "protocolv2" {
		return nil, fmt.Errorf("package declarations in %s use package %s, want protocolv2", path, file.Name.Name)
	}
	var symbols []PackageSymbol
	add := func(kind, name string) {
		if name != "_" {
			symbols = append(symbols, PackageSymbol{Name: name, SourcePath: path, SchemaPath: origins[kind+"\x00"+name]})
		}
	}
	for _, declaration := range file.Decls {
		switch node := declaration.(type) {
		case *ast.GenDecl:
			kind := "var"
			if node.Tok == token.CONST {
				kind = "const"
			}
			for _, spec := range node.Specs {
				switch item := spec.(type) {
				case *ast.TypeSpec:
					add("type", item.Name.Name)
				case *ast.ValueSpec:
					for _, name := range item.Names {
						add(kind, name.Name)
					}
				}
			}
		case *ast.FuncDecl:
			if node.Recv == nil && node.Name.Name != "init" {
				add("func", node.Name.Name)
			}
		}
	}
	return symbols, nil
}

func generatedTypeOrigins(plan ProtocolTypePlan) (map[string]string, error) {
	origins := map[string]string{}
	mark := func(kind, name, path string) {
		if name != "" && path != "" {
			origins[kind+"\x00"+name] = path
		}
	}
	for _, typ := range plan.Types {
		if isGeneratedTopLevelType(typ) {
			mark("type", typ.TypeName, typ.SchemaPath)
		}
	}
	resolver, err := newGeneratedDefinitionNameResolver(plan)
	if err != nil {
		return nil, err
	}
	for _, typ := range plan.Types {
		for name := range typ.GeneratedDefinitions {
			if !isGeneratedDefinitionSelected(typ, name) || resolver.ReusesTopLevel(typ.SchemaPath, name) {
				continue
			}
			if generatedName, ok := resolver.NameForDefinition(typ.SchemaPath, name); ok {
				mark("type", generatedName, unsupportedDefinitionPath(typ.SchemaPath, name))
			}
		}
	}
	enums, err := SelectGeneratedEnums(plan)
	if err != nil {
		return nil, err
	}
	for _, enum := range enums {
		if len(enum.Sources) == 0 {
			continue
		}
		path := origins["type\x00"+enum.TypeName]
		if path == "" {
			path = enum.Sources[0]
		}
		for _, value := range enum.Values {
			mark("const", enumConstName(enum.TypeName, value), path)
		}
	}
	scalar, err := SelectGeneratedScalarUnions(plan)
	if err != nil {
		return nil, err
	}
	for _, union := range scalar {
		mark("type", union.TypeName+"Kind", union.SchemaPath)
		for _, variant := range union.Variants {
			mark("const", scalarUnionKindConstName(union, variant), union.SchemaPath)
			mark("func", variant.ConstructorName, union.SchemaPath)
		}
	}
	mixed, err := SelectGeneratedMixedUnions(plan)
	if err != nil {
		return nil, err
	}
	for _, union := range mixed {
		mark("type", union.TypeName+"Kind", union.SchemaPath)
		for _, variant := range union.Variants {
			mark("const", mixedUnionKindConstName(union, variant), union.SchemaPath)
			mark("type", variant.PayloadTypeName, union.SchemaPath)
			mark("func", variant.ConstructorName, union.SchemaPath)
		}
	}
	untagged, err := SelectGeneratedUntaggedObjectUnions(plan)
	if err != nil {
		return nil, err
	}
	for _, union := range untagged {
		mark("type", union.TypeName+"Kind", union.SchemaPath)
		for _, variant := range union.Variants {
			mark("const", untaggedObjectUnionKindConstName(union, variant), union.SchemaPath)
			mark("type", variant.PayloadTypeName, union.SchemaPath)
			mark("func", variant.ConstructorName, union.SchemaPath)
		}
	}
	tagged, err := SelectGeneratedTaggedUnions(plan)
	if err != nil {
		return nil, err
	}
	for _, union := range tagged {
		mark("type", union.TypeName+"Kind", union.SchemaPath)
		for _, variant := range union.Variants {
			mark("const", taggedUnionKindConstName(union, variant), union.SchemaPath)
			mark("type", variant.PayloadTypeName, union.SchemaPath)
			mark("func", variant.ConstructorName, union.SchemaPath)
		}
	}
	return origins, nil
}
