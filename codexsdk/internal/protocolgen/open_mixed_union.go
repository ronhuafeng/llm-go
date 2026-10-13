package protocolgen

import (
	"bytes"
	"fmt"
)

// The exact source's unconstrained string/object branch admits future values,
// including malformed known shapes that satisfy that branch. Keep the known
// public constructors and add a protocol-native, kind-restricted representation.
func writeOpenMixedUnionAPI(out *bytes.Buffer, union MixedUnionPlan) {
	fmt.Fprintf(out, "const %sKindUnknown %sKind = \"__unknown\"\n\n", union.TypeName, union.TypeName)
	fmt.Fprintf(out, "func New%sUnknown(value JSONValue) %s { return %s{kind: %sKindUnknown, unknown: value.clone()} }\n\n", union.TypeName, union.TypeName, union.TypeName, union.TypeName)
	fmt.Fprintf(out, "func (value %s) AsUnknown() (JSONValue, bool) { if value.kind != %sKindUnknown { return JSONValue{}, false }; return value.unknown.clone(), true }\n\n", union.TypeName, union.TypeName)
}

func writeOpenMixedUnionDecoder(out *bytes.Buffer, union MixedUnionPlan) {
	fmt.Fprintf(out, "func (value *%s) unmarshalJSON(data []byte, mode wireDecodeMode) error {\n", union.TypeName)
	fmt.Fprintf(out, "var known %s\nif err := known.unmarshalKnownJSON(data, wireDecodeClosed); err == nil { *value = known; return nil }\n", union.TypeName)
	out.WriteString("raw, err := ParseJSONValue(data)\nif err != nil { return err }\nif raw.Kind() != JSONKindString && raw.Kind() != JSONKindObject { return fmt.Errorf(\"decode open union: expected string or object\") }\n")
	fmt.Fprintf(out, "*value = New%sUnknown(raw)\nreturn nil\n}\n\n", union.TypeName)
}
