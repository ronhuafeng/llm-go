package protocolgen

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// The outer record declares the union of branch fields, but each open branch
// constrains only its own fields. A field can therefore have an arbitrary JSON
// value when another branch matches. Preserve those values and validate the
// original branch constraints separately instead of intersecting their types.
func taggedVariantAnyOf(typ TypePlan, variant *Schema, discriminator string, index int, named map[string]bool, resolver generatedDefinitionNameResolver) ([][]FieldPlan, []FieldPlan, error) {
	if len(variant.AnyOf) == 0 {
		return nil, nil, nil
	}
	fail := func(reason string) ([][]FieldPlan, []FieldPlan, error) {
		return nil, nil, unsupportedGeneratedSchema(fmt.Sprintf("%s/oneOf/%d/anyOf", typ.SchemaPath, index), "%s", reason)
	}
	if variant.AdditionalProperties.Present || len(variant.AllOf) != 0 || len(variant.OneOf) != 0 {
		return fail("unsupported constrained object product")
	}
	var branches [][]FieldPlan
	keys := map[string]int{}
	for branchIndex, branch := range variant.AnyOf {
		if branch == nil || !branch.Type.Only("object") || len(branch.Properties) == 0 || branch.AdditionalProperties.Present || branch.Ref != "" || len(branch.AnyOf) != 0 || len(branch.OneOf) != 0 || len(branch.AllOf) != 0 || len(unmodeledKeywords(branch)) != 0 {
			return fail("expected open object alternatives with supported field constraints")
		}
		for _, key := range branch.Required {
			if _, exists := branch.Properties[key]; !exists {
				return fail("required alternative field is undeclared")
			}
		}
		for key := range branch.Properties {
			if _, exists := variant.Properties[key]; exists {
				return fail("overlapping outer and alternative fields need conjunctive representation")
			}
			if _, exists := keys[key]; !exists {
				keys[key] = branchIndex
			}
		}
		fields, nulls, err := taggedUnionVariantFields(typ, branch, discriminator, index, named, resolver)
		if err != nil {
			return nil, nil, err
		}
		if len(nulls) != 0 {
			return fail("null-only alternative fields need explicit representation")
		}
		branches = append(branches, fields)
	}
	var names []string
	for name := range keys {
		names = append(names, name)
	}
	sort.Strings(names)
	sourcePath := typ.SchemaPath
	if !strings.Contains(sourcePath, "#") {
		sourcePath += "#"
	}
	var fields []FieldPlan
	for _, name := range names {
		fields = append(fields, FieldPlan{
			Path:            fmt.Sprintf("%s/oneOf/%d/anyOf/%d/properties/%s", sourcePath, index, keys[name], escapedJSONPointerToken(name)),
			SchemaPath:      typ.SchemaPath,
			TypeName:        typ.TypeName,
			Stability:       typ.Stability,
			Reason:          "open object anyOf branch field",
			FieldName:       name,
			GoType:          "*protocolv2.JSONValue",
			Kind:            FieldPlanJSONValue,
			WireOmitAllowed: true,
			WireAllowsNull:  true,
		})
	}
	return branches, fields, nil
}

func writeTaggedVariantAnyOfValidator(out *bytes.Buffer, variant TaggedUnionVariantPlan) {
	if len(variant.AnyOf) == 0 {
		return
	}
	fmt.Fprintf(out, "\nfunc validate%sAnyOf(input map[string]JSONValue, mode wireDecodeMode) error {\n", variant.PayloadTypeName)
	for _, branch := range variant.AnyOf {
		out.WriteString("if err := func() error {\nfields := cloneJSONValueMap(input)\nvar err error\nvar decoded struct {\n")
		for _, field := range branch {
			fmt.Fprintf(out, "%s %s\n", fieldGoName(field.FieldName), generatedGoType(field.GoType))
		}
		out.WriteString("}\n")
		for _, field := range branch {
			writeNamedFieldDecoder(out, TypePlan{TypeName: variant.PayloadTypeName}, field, "decoded")
		}
		out.WriteString("return nil\n}(); err == nil { return nil }\n")
	}
	fmt.Fprintf(out, "return fmt.Errorf(%q)\n}\n", "decode "+variant.PayloadTypeName+": no anyOf branch matches")
}
