package protocolgen

import (
	"bytes"
	"fmt"
	"strings"
)

func mixedUnionSchemaVariants(schema *Schema) []*Schema {
	if schema == nil || len(schema.OneOf) > 0 && len(schema.AnyOf) > 0 {
		return nil
	}
	if len(schema.OneOf) > 0 {
		return schema.OneOf
	}
	return schema.AnyOf
}

func isOpenMixedUnionFallback(schema *Schema) bool {
	if schema == nil || schema.Bool != nil || len(schema.Type.Values) == 0 || hasNonTypeShape(schema) || len(unmodeledKeywords(schema)) != 0 || schema.Default.Present || len(schema.Definitions) != 0 {
		return false
	}
	for _, kind := range schema.Type.Values {
		if kind != "string" && kind != "object" {
			return false
		}
	}
	return true
}

func openMixedUnionVariant(typ TypePlan) MixedUnionVariantPlan {
	return MixedUnionVariantPlan{
		AccessorName: "AsUnknown", ConstructorName: "New" + typ.TypeName + "Unknown",
		DiscriminatorValue: "unknown", GoName: "Unknown", JSONKind: "unknown",
		PayloadTypeName: typ.TypeName + "Unknown", PrivateFieldName: "unknown",
		DirectValueField: &FieldPlan{FieldName: "value", GoType: "protocolv2.JSONValue", Kind: FieldPlanJSONValue, Required: true, WireAllowsNull: false, Path: typ.SchemaPath, SchemaPath: typ.SchemaPath, TypeName: typ.TypeName, Stability: typ.Stability},
	}
}

func openMixedUnionKindCondition(union MixedUnionPlan, value string) string {
	var conditions []string
	for _, kind := range union.CatchAllKinds {
		conditions = append(conditions, value+".Kind() == JSONKind"+variantGoName(kind))
	}
	return strings.Join(conditions, " || ")
}

func writeOpenMixedUnionMarshal(out *bytes.Buffer, union MixedUnionPlan, variant MixedUnionVariantPlan) {
	fmt.Fprintf(out, "if !(%s) { return nil, invalidUnionVariant(%q, %q) }\n", openMixedUnionKindCondition(union, "value."+variant.PrivateFieldName+".Value"), union.TypeName, variant.DiscriminatorValue)
	fmt.Fprintf(out, "return json.Marshal(value.%s.Value)\n", variant.PrivateFieldName)
}

// Try a strict known decoding first. Observation tolerance must not discard
// source-valid data in an open alternative: retain it in the catch-all instead.
func writeOpenMixedUnionUnmarshal(out *bytes.Buffer, union MixedUnionPlan) {
	fmt.Fprintf(out, "func (value *%s) unmarshalJSON(data []byte, mode wireDecodeMode) error {\n", union.TypeName)
	out.WriteString("raw, err := ParseJSONValue(data)\nif err != nil { return err }\n")
	fmt.Fprintf(out, "var known %s\n", union.TypeName)
	out.WriteString("if err := known.unmarshalKnownJSON(data, wireDecodeClosed); err == nil { *value = known; return nil }\n")
	fmt.Fprintf(out, "if %s {\n", openMixedUnionKindCondition(union, "raw"))
	fmt.Fprintf(out, "payload := %sUnknown{Value: raw}\n*value = %s{kind: %sKindUnknown, unknown: &payload}\nreturn nil\n}\n", union.TypeName, union.TypeName, union.TypeName)
	fmt.Fprintf(out, "return fmt.Errorf(%q)\n}\n\n", "decode "+union.TypeName+": expected a supported union JSON kind")
}
