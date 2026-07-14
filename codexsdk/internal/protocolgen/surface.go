package protocolgen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"sort"
)

type Stability string

const (
	StabilityStable       Stability = "stable"
	StabilityExperimental Stability = "experimental"
	StabilityMixed        Stability = "mixed"
)

type SurfaceKind string

const (
	SurfaceConst     SurfaceKind = "const"
	SurfaceField     SurfaceKind = "field"
	SurfaceFunc      SurfaceKind = "func"
	SurfaceInterface SurfaceKind = "interface_method"
	SurfaceMethod    SurfaceKind = "method"
	SurfaceType      SurfaceKind = "type"
	SurfaceValue     SurfaceKind = "value"
	SurfaceVar       SurfaceKind = "var"
)

// ClassifyExportedSurface compares generated package sources. Stable source is
// the package generated without experimental schema visibility; complete source
// is generated with it. Every exported complete identity receives one entry.
func ClassifyExportedSurface(stableSource, completeSource []byte) ([]SurfaceEntry, error) {
	return ClassifyExportedPackage([][]byte{stableSource}, [][]byte{completeSource})
}

func ClassifyExportedPackage(stableSources, completeSources [][]byte) ([]SurfaceEntry, error) {
	stable, err := exportedPackageSurface(stableSources)
	if err != nil {
		return nil, fmt.Errorf("parse stable generated source: %w", err)
	}
	complete, err := exportedPackageSurface(completeSources)
	if err != nil {
		return nil, fmt.Errorf("parse complete generated source: %w", err)
	}

	entries := make([]SurfaceEntry, 0, len(complete))
	for key, item := range complete {
		stability := StabilityExperimental
		if stableItem, ok := stable[key]; ok && stableItem.Kind == item.Kind {
			stability = StabilityStable
		}
		entries = append(entries, SurfaceEntry{Name: item.Name, Kind: item.Kind, Owner: item.Owner, Signature: item.Signature, Stability: stability})
	}
	markMixedOwners(entries)
	sortSurface(entries)
	if err := ValidateSurface(entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func ValidateSurface(entries []SurfaceEntry) error {
	if len(entries) == 0 {
		return fmt.Errorf("surface has no exported identities")
	}
	seen := map[string]bool{}
	types := map[string]Stability{}
	ownerStability := map[string]map[Stability]bool{}
	for _, entry := range entries {
		key := string(entry.Kind) + "\x00" + entry.Name
		if entry.Name == "" || entry.Signature == "" {
			return fmt.Errorf("surface entry is missing name or signature: %#v", entry)
		}
		switch entry.Kind {
		case SurfaceConst, SurfaceField, SurfaceFunc, SurfaceInterface, SurfaceMethod, SurfaceType, SurfaceValue, SurfaceVar:
		default:
			return fmt.Errorf("surface entry %q has unsupported kind %q", entry.Name, entry.Kind)
		}
		switch entry.Stability {
		case StabilityStable, StabilityExperimental:
		case StabilityMixed:
			if entry.Kind != SurfaceType {
				return fmt.Errorf("non-type surface entry %q cannot be mixed", entry.Name)
			}
		default:
			return fmt.Errorf("surface entry %q is unclassified", entry.Name)
		}
		if seen[key] {
			return fmt.Errorf("surface identity %s %q appears more than once", entry.Kind, entry.Name)
		}
		seen[key] = true
		if entry.Kind == SurfaceType {
			types[entry.Name] = entry.Stability
			if ownerStability[entry.Name] == nil {
				ownerStability[entry.Name] = map[Stability]bool{}
			}
			if entry.Stability == StabilityExperimental {
				ownerStability[entry.Name][StabilityExperimental] = true
			} else {
				ownerStability[entry.Name][StabilityStable] = true
			}
		}
		if entry.Owner != "" {
			if ownerStability[entry.Owner] == nil {
				ownerStability[entry.Owner] = map[Stability]bool{}
			}
			ownerStability[entry.Owner][entry.Stability] = true
		}
	}
	for _, entry := range entries {
		if entry.Owner != "" {
			if _, ok := types[entry.Owner]; !ok {
				return fmt.Errorf("surface entry %q references unknown owner type %q", entry.Name, entry.Owner)
			}
		}
		switch entry.Kind {
		case SurfaceField, SurfaceInterface, SurfaceMethod, SurfaceValue:
			if entry.Owner == "" {
				return fmt.Errorf("surface member %q has no owner type", entry.Name)
			}
		}
	}
	for name, stability := range types {
		mixedMembers := ownerStability[name][StabilityStable] && ownerStability[name][StabilityExperimental]
		if (stability == StabilityMixed) != mixedMembers {
			return fmt.Errorf("surface type %q stability %q does not match member classifications", name, stability)
		}
	}
	return nil
}

func VerifyExportedSurface(completeSource []byte, entries []SurfaceEntry) error {
	return VerifyExportedPackage([][]byte{completeSource}, entries)
}

func VerifyExportedPackage(completeSources [][]byte, entries []SurfaceEntry) error {
	exported, err := exportedPackageSurface(completeSources)
	if err != nil {
		return err
	}
	classified := map[string]SurfaceEntry{}
	for _, entry := range entries {
		classified[string(entry.Kind)+"\x00"+entry.Name] = entry
	}
	for key, item := range exported {
		entry, ok := classified[key]
		if !ok {
			return fmt.Errorf("exported generated %s %q is unclassified", item.Kind, item.Name)
		}
		if entry.Signature != item.Signature {
			return fmt.Errorf("exported generated %s %q signature is %q, classified as %q", item.Kind, item.Name, item.Signature, entry.Signature)
		}
	}
	for key := range classified {
		if _, ok := exported[key]; !ok {
			return fmt.Errorf("classified generated identity %q is not exported", key)
		}
	}
	return nil
}

func markMixedOwners(entries []SurfaceEntry) {
	owners := map[string]map[Stability]bool{}
	for _, entry := range entries {
		if entry.Kind == SurfaceType {
			if owners[entry.Name] == nil {
				owners[entry.Name] = map[Stability]bool{}
			}
			owners[entry.Name][entry.Stability] = true
		}
		owner := memberOwner(entry)
		if owner == "" {
			continue
		}
		if owners[owner] == nil {
			owners[owner] = map[Stability]bool{}
		}
		owners[owner][entry.Stability] = true
	}
	for i := range entries {
		if entries[i].Kind == SurfaceType && owners[entries[i].Name][StabilityStable] && owners[entries[i].Name][StabilityExperimental] {
			entries[i].Stability = StabilityMixed
		}
	}
}

func memberOwner(entry SurfaceEntry) string {
	if entry.Owner != "" {
		return entry.Owner
	}
	if entry.Kind != SurfaceField && entry.Kind != SurfaceInterface && entry.Kind != SurfaceMethod {
		return ""
	}
	for i := 0; i < len(entry.Name); i++ {
		if entry.Name[i] == '.' {
			return entry.Name[:i]
		}
	}
	return ""
}

type exportedIdentity struct {
	Kind      SurfaceKind
	Name      string
	Owner     string
	Signature string
}

func exportedPackageSurface(sources [][]byte) (map[string]exportedIdentity, error) {
	result := map[string]exportedIdentity{}
	for index, source := range sources {
		items, err := exportedFileSurface(source, fmt.Sprintf("generated_%d.go", index))
		if err != nil {
			return nil, err
		}
		for key, item := range items {
			if previous, ok := result[key]; ok && previous != item {
				return nil, fmt.Errorf("exported identity %q has conflicting declarations", item.Name)
			}
			result[key] = item
		}
	}
	return result, nil
}

func exportedFileSurface(source []byte, filename string) (map[string]exportedIdentity, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, filename, source, 0)
	if err != nil {
		return nil, err
	}
	result := map[string]exportedIdentity{}
	add := func(kind SurfaceKind, name, owner, signature string) {
		item := exportedIdentity{Kind: kind, Name: name, Owner: owner, Signature: signature}
		result[string(kind)+"\x00"+name] = item
	}
	for _, decl := range file.Decls {
		switch node := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range node.Specs {
				switch item := spec.(type) {
				case *ast.TypeSpec:
					if !item.Name.IsExported() {
						continue
					}
					add(SurfaceType, item.Name.Name, "", formatNode(fileSet, item.Type))
					collectTypeMembers(fileSet, add, item.Name.Name, item.Type)
				case *ast.ValueSpec:
					kind := SurfaceVar
					if node.Tok == token.CONST {
						kind = SurfaceConst
					}
					owner := expressionName(item.Type)
					if node.Tok == token.CONST && owner != "" {
						kind = SurfaceValue
					}
					for index, name := range item.Names {
						if name.IsExported() {
							signature := formatNode(fileSet, item.Type)
							if len(item.Values) > 0 {
								valueIndex := index
								if valueIndex >= len(item.Values) {
									valueIndex = len(item.Values) - 1
								}
								signature += " = " + formatNode(fileSet, item.Values[valueIndex])
							}
							add(kind, name.Name, owner, signature)
						}
					}
				}
			}
		case *ast.FuncDecl:
			if !node.Name.IsExported() {
				continue
			}
			if node.Recv == nil {
				add(SurfaceFunc, node.Name.Name, "", formatNode(fileSet, node.Type))
				continue
			}
			if owner := receiverName(node.Recv.List[0].Type); owner != "" && ast.IsExported(owner) {
				add(SurfaceMethod, owner+"."+node.Name.Name, owner, formatNode(fileSet, node.Type))
			}
		}
	}
	return result, nil
}

func collectTypeMembers(fileSet *token.FileSet, add func(SurfaceKind, string, string, string), owner string, expression ast.Expr) {
	switch node := expression.(type) {
	case *ast.StructType:
		for _, field := range node.Fields.List {
			if len(field.Names) == 0 {
				if name := embeddedFieldName(field.Type); ast.IsExported(name) {
					signature := formatNode(fileSet, field.Type)
					if field.Tag != nil {
						signature += " " + field.Tag.Value
					}
					add(SurfaceField, owner+"."+name, owner, signature)
				}
			}
			for _, name := range field.Names {
				if name.IsExported() {
					signature := formatNode(fileSet, field.Type)
					if field.Tag != nil {
						signature += " " + field.Tag.Value
					}
					add(SurfaceField, owner+"."+name.Name, owner, signature)
				}
			}
		}
	case *ast.InterfaceType:
		for _, field := range node.Methods.List {
			for _, name := range field.Names {
				if name.IsExported() {
					add(SurfaceInterface, owner+"."+name.Name, owner, formatNode(fileSet, field.Type))
				}
			}
		}
	}
}

func embeddedFieldName(expression ast.Expr) string {
	switch node := expression.(type) {
	case *ast.Ident:
		return node.Name
	case *ast.StarExpr:
		return embeddedFieldName(node.X)
	case *ast.SelectorExpr:
		return node.Sel.Name
	default:
		return ""
	}
}

func formatNode(fileSet *token.FileSet, node any) string {
	if node == nil {
		return "implicit"
	}
	var out bytes.Buffer
	if err := format.Node(&out, fileSet, node); err != nil {
		panic(fmt.Sprintf("format parsed generated node: %v", err))
	}
	return out.String()
}

func expressionName(expression ast.Expr) string {
	if expression == nil {
		return ""
	}
	return receiverName(expression)
}

func receiverName(expression ast.Expr) string {
	switch node := expression.(type) {
	case *ast.Ident:
		return node.Name
	case *ast.StarExpr:
		return receiverName(node.X)
	default:
		return ""
	}
}

func sortSurface(entries []SurfaceEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name != entries[j].Name {
			return entries[i].Name < entries[j].Name
		}
		return entries[i].Kind < entries[j].Kind
	})
}
