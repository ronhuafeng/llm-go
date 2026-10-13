package protocolgen

import (
	"bytes"
	"fmt"
	"sort"
)

// Keep branch-local constraints separate. A property absent from an open branch
// has no type constraint in that branch, even when another branch declares it.
func planTaggedObjectAlternatives(typ TypePlan, variant *Schema, index int, named map[string]bool, resolver generatedDefinitionNameResolver) ([][]FieldPlan, error) {
	if variant.AnyOf == nil {
		return nil, nil
	}
	if len(variant.AnyOf) == 0 {
		return nil, fmt.Errorf("%s variant %d: empty anyOf", typ.SchemaPath, index)
	}
	if variant.AdditionalProperties.Present || variant.AllOf != nil || variant.Enum != nil || variant.Ref != "" || variant.Bool != nil || len(variant.UnknownKeywords) > 0 {
		return nil, fmt.Errorf("%s variant %d: constrained object product unsupported", typ.SchemaPath, index)
	}
	var alternatives [][]FieldPlan
	for i, branch := range variant.AnyOf {
		if branch == nil || !branch.Type.Only("object") || branch.AdditionalProperties.Present || branch.AnyOf != nil || branch.OneOf != nil || branch.AllOf != nil || branch.Enum != nil || branch.Ref != "" || branch.Bool != nil || branch.Items != nil || branch.Format != "" || branch.Minimum != nil || branch.MinItems != nil || len(branch.Properties) == 0 || len(unmodeledKeywords(branch)) > 0 {
			return nil, fmt.Errorf("%s variant %d alternative %d: unsupported object branch", typ.SchemaPath, index, i)
		}
		var fields []FieldPlan
		for name, property := range branch.Properties {
			if variant.Properties[name] != nil {
				return nil, fmt.Errorf("%s: overlapping alternative property %s unsupported", typ.SchemaPath, name)
			}
			// The compatibility convenience fields currently support bare strings.
			// Other shapes fail closed rather than silently discarding constraints.
			if !property.Type.Only("string") || property.Enum != nil || property.AnyOf != nil || property.OneOf != nil || property.AllOf != nil || hasNonTypeShape(property) || len(unmodeledKeywords(property)) > 0 || property.Format != "" || property.Minimum != nil || property.MinItems != nil || property.Bool != nil {
				return nil, fmt.Errorf("%s: alternative property %s requires a lossless mapping", typ.SchemaPath, name)
			}
			f, err := planField(CoverageField{Field: name, Path: fmt.Sprintf("%s#/oneOf/%d/anyOf/%d/properties/%s", typ.SchemaPath, index, i, name), Required: branch.RequiredSet()[name], Schema: typ.SchemaPath, Type: typ.TypeName, Status: "supported-generated", Stability: typ.Stability}, property)
			if err != nil {
				return nil, err
			}
			f = resolver.ResolveField(f)
			if !canGenerateFirstPassField(f, named) {
				return nil, fmt.Errorf("unsupported alternative field %s", name)
			}
			fields = append(fields, f)
		}
		for name := range branch.RequiredSet() {
			if branch.Properties[name] == nil {
				return nil, fmt.Errorf("undeclared required alternative field %s", name)
			}
		}
		alternatives = append(alternatives, sortedFields(fields))
	}
	return alternatives, nil
}

func alternativeFields(variant TaggedUnionVariantPlan) []FieldPlan {
	byName := map[string]FieldPlan{}
	for _, branch := range variant.ObjectAlternatives {
		for _, f := range branch {
			byName[f.FieldName] = f
		}
	}
	var names []string
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	var fields []FieldPlan
	for _, n := range names {
		fields = append(fields, byName[n])
	}
	return fields
}

func writeAlternativePayloadType(out *bytes.Buffer, variant TaggedUnionVariantPlan) {
	fmt.Fprintf(out, "type %s struct {\n", variant.PayloadTypeName)
	for _, field := range sortedFields(variant.Fields) {
		fmt.Fprintf(out, "%s %s `json:\"%s%s\"`\n", fieldGoName(field.FieldName), generatedGoType(field.GoType), field.FieldName, jsonTagOmitEmpty(field))
	}
	for _, field := range alternativeFields(variant) {
		fmt.Fprintf(out, "// %s is a string convenience; AlternativeFields takes precedence.\n%s string `json:\"-\"`\n", fieldGoName(field.FieldName), fieldGoName(field.FieldName))
	}
	out.WriteString("// AlternativeFields preserves presence and arbitrary JSON in the declared alternative properties.\n// At least one source branch must validate. Other property names are rejected.\nAlternativeFields map[string]JSONValue `json:\"-\"`\n}\n")
}

func writeAlternativeValidator(out *bytes.Buffer, union TaggedUnionPlan, variant TaggedUnionVariantPlan) {
	fmt.Fprintf(out, "\nfunc validate%sAlternatives(source map[string]JSONValue, mode wireDecodeMode) error {\n", variant.PayloadTypeName)
	for _, branch := range variant.ObjectAlternatives {
		out.WriteString("if err := func() error {\nfields := make(map[string]JSONValue, len(source))\nfor key, value := range source { fields[key] = value }\nvar decoded struct {\n")
		for _, f := range branch {
			fmt.Fprintf(out, "%s %s\n", fieldGoName(f.FieldName), generatedGoType(f.GoType))
		}
		out.WriteString("}\n_ = decoded\n")
		for _, f := range branch {
			writeFieldDecoder(out, TypePlan{TypeName: union.TypeName}, f)
		}
		out.WriteString("return nil\n}(); err == nil { return nil }\n")
	}
	fmt.Fprintf(out, "return fmt.Errorf(%q)\n}\n", "decode "+variant.PayloadTypeName+": no anyOf branch holds")
}

func writeAlternativeVariantDecoder(out *bytes.Buffer, union TaggedUnionPlan, variant TaggedUnionVariantPlan) {
	fmt.Fprintf(out, "if err := validate%sAlternatives(fields, mode); err != nil { return err }\n", variant.PayloadTypeName)
	out.WriteString("decoded.AlternativeFields = make(map[string]JSONValue)\n")
	for _, f := range alternativeFields(variant) {
		fmt.Fprintf(out, "if raw, ok := fields[%q]; ok {\ndecoded.AlternativeFields[%q] = raw\nif raw.Kind() == JSONKindString { encoded, _ := json.Marshal(raw); _ = json.Unmarshal(encoded, &decoded.%s) }\ndelete(fields, %q)\n}\n", f.FieldName, f.FieldName, fieldGoName(f.FieldName), f.FieldName)
	}
}

func writeAlternativeVariantMarshal(out *bytes.Buffer, union TaggedUnionPlan, variant TaggedUnionVariantPlan) {
	out.WriteString("var fieldsValue JSONValue\nfields := make(map[string]JSONValue)\n")
	fmt.Fprintf(out, "base, err := json.Marshal(*value.%s)\nif err != nil { return nil, err }\nif err := json.Unmarshal(base, &fields); err != nil { return nil, err }\n", variant.PrivateFieldName)
	for _, f := range alternativeFields(variant) {
		fmt.Fprintf(out, "if value.%s.%s != \"\" { raw, _ := json.Marshal(value.%s.%s); if err := json.Unmarshal(raw, &fieldsValue); err != nil { return nil, err }; fields[%q] = fieldsValue }\n", variant.PrivateFieldName, fieldGoName(f.FieldName), variant.PrivateFieldName, fieldGoName(f.FieldName), f.FieldName)
	}
	fmt.Fprintf(out, "for key, raw := range value.%s.AlternativeFields {\nswitch key {\n", variant.PrivateFieldName)
	for _, f := range alternativeFields(variant) {
		fmt.Fprintf(out, "case %q:\nfields[key] = raw\n", f.FieldName)
	}
	out.WriteString("default: return nil, fmt.Errorf(\"encode alternative: undeclared property %s\", key)\n}\n}\n")
	fmt.Fprintf(out, "if err := validate%sAlternatives(fields, wireDecodeClosed); err != nil { return nil, err }\n", variant.PayloadTypeName)
	fmt.Fprintf(out, "rawType, _ := json.Marshal(%q)\nif err := json.Unmarshal(rawType, &fieldsValue); err != nil { return nil, err }; fields[%q] = fieldsValue\nencoded, err := json.Marshal(fields)\nif err != nil { return nil, err }\nvar checked %s\nif err := checked.unmarshalJSON(encoded, wireDecodeClosed); err != nil { return nil, err }\nreturn encoded, nil\n", variant.DiscriminatorValue, union.Discriminator, union.TypeName)
}
