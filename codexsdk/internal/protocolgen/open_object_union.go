package protocolgen

import (
	"bytes"
	"fmt"
)

func writeOpenObjectUnionMarshal(out *bytes.Buffer, union UntaggedObjectUnionPlan, variant UntaggedObjectUnionVariantPlan) {
	fmt.Fprintf(out, "encoded, err := json.Marshal(*value.%s)\nif err != nil { return nil, err }\n", variant.PrivateFieldName)
	fmt.Fprintf(out, "fields, err := decodeObjectFields(encoded, %q)\nif err != nil { return nil, err }\n", union.TypeName)
	out.WriteString("for name, raw := range value.extra { if _, known := fields[name]; !known { fields[name] = raw } }\n")
	out.WriteString("data, err := json.Marshal(fields)\nif err != nil { return nil, err }\n")
	writeObjectAnyOfMarshalValidation(out, union)
}

func writeObjectAnyOfMarshalValidation(out *bytes.Buffer, union UntaggedObjectUnionPlan) {
	fmt.Fprintf(out, "var validated %s\nif err := validated.unmarshalJSON(data, wireDecodeClosed); err != nil { return nil, err }\nreturn data, nil\n", union.TypeName)
}

// anyOf succeeds when one complete branch holds. Keep other fields verbatim
// only for source-declared open alternatives; closed records keep their policy.
func writeObjectAnyOfUnmarshal(out *bytes.Buffer, union UntaggedObjectUnionPlan) {
	fmt.Fprintf(out, "func (value *%s) UnmarshalJSON(data []byte) error { return value.unmarshalJSON(data, wireDecodeClosed) }\n\n", union.TypeName)
	fmt.Fprintf(out, "func (value *%s) unmarshalJSON(data []byte, mode wireDecodeMode) error {\n", union.TypeName)
	fmt.Fprintf(out, "input, err := decodeObjectFields(data, %q)\nif err != nil { return err }\n", union.TypeName)
	if union.TypeName == "ResourceContent" {
		out.WriteString("matches := 0\n")
		for _, variant := range union.Variants {
			fmt.Fprintf(out, "if _, present := input[%q]; present { matches++ }\n", variant.DiscriminatorValue)
		}
		fmt.Fprintf(out, "if matches > 1 { return fmt.Errorf(%q) }\n", "decode ResourceContent: ambiguous object matches multiple variants")
	}
	for _, variant := range union.Variants {
		out.WriteString("if err := func() error {\nfields := cloneJSONValueMap(input)\nvar err error\n")
		fmt.Fprintf(out, "var decoded %s\n", variant.PayloadTypeName)
		for _, field := range sortedFields(variant.Fields) {
			writeFieldDecoder(out, TypePlan{TypeName: union.TypeName}, field)
		}
		if !variant.Open {
			fmt.Fprintf(out, "if err := rejectUnexpectedFieldsForMode(fields, %q, mode); err != nil { return err }\n", union.TypeName+"."+variant.DiscriminatorValue)
		}
		fmt.Fprintf(out, "*value = %s{kind: %s, %s: &decoded", union.TypeName, untaggedObjectUnionKindConstName(union, variant), variant.PrivateFieldName)
		if variant.Open {
			out.WriteString(", extra: fields")
		}
		out.WriteString("}\nreturn nil\n}(); err == nil { return nil }\n")
	}
	fmt.Fprintf(out, "return fmt.Errorf(%q)\n}\n", "decode "+union.TypeName+": expected one of "+untaggedObjectUnionExpectedFields(union))
}
