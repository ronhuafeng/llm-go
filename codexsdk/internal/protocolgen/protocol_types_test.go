package protocolgen

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateProtocolTypesMatchesCheckedInOutput(t *testing.T) {
	schemaRoot := filepath.Join("..", "protocolschema", "appserver", "v2")
	plan, err := BuildProtocolTypePlan(schemaRoot)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := LoadManifest(filepath.Join(schemaRoot, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyWireMessageRoles(&plan, manifest); err != nil {
		t.Fatal(err)
	}
	generated, err := GenerateProtocolTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	generatedAgain, err := GenerateProtocolTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, generatedAgain) {
		t.Fatal("generated protocol types are not reproducible")
	}
	checkedIn, err := os.ReadFile(filepath.Join("..", "..", "protocolv2", "protocol_types.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, checkedIn) {
		t.Fatal("generated protocol types do not match checked-in protocolv2/protocol_types.gen.go")
	}
}

func TestSelectFirstPassGeneratedTypes(t *testing.T) {
	plan, err := BuildProtocolTypePlan(filepath.Join("..", "protocolschema", "appserver", "v2"))
	if err != nil {
		t.Fatal(err)
	}
	selected, err := SelectFirstPassGeneratedTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) == 0 {
		t.Fatal("selected generated type count = 0")
	}
	seen := map[string]string{}
	for _, typ := range selected {
		if typ.TypeName == "" || typ.SchemaPath == "" {
			t.Fatalf("selected generated type has incomplete identity: %#v", typ)
		}
		if previous, ok := seen[typ.TypeName]; ok {
			t.Fatalf("selected generated type %s appears in both %s and %s", typ.TypeName, previous, typ.SchemaPath)
		}
		seen[typ.TypeName] = typ.SchemaPath
		switch typ.Kind {
		case TypePlanEmptyStructCandidate, TypePlanObjectStructCandidate:
		default:
			t.Fatalf("selected generated type %s has unsupported kind %s", typ.TypeName, typ.Kind)
		}
		for _, field := range typ.Fields {
			assertGeneratedFieldPlan(t, typ.TypeName, field)
		}
	}
}

func TestJSONRPCMessageIsNotPublicGeneratedSurface(t *testing.T) {
	plan, err := BuildProtocolTypePlan(filepath.Join("..", "protocolschema", "appserver", "v2"))
	if err != nil {
		t.Fatal(err)
	}
	selected, err := SelectFirstPassGeneratedTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range selected {
		if strings.HasPrefix(typ.TypeName, "JSONRPC") {
			t.Fatalf("JSON-RPC envelope type %s must not be public generated protocolv2 surface", typ.TypeName)
		}
	}
}

func TestGeneratedDefinitionSourcesStayCanonical(t *testing.T) {
	plan, err := BuildProtocolTypePlan(filepath.Join("..", "protocolschema", "appserver", "v2"))
	if err != nil {
		t.Fatal(err)
	}

	type source struct {
		encoded []byte
		kind    string
		path    string
	}

	byName := map[string]source{}
	selectedCount := 0
	for _, typ := range plan.Types {
		if typ.Schema == nil || len(typ.Schema.Definitions) == 0 {
			continue
		}
		for name, schema := range typ.Schema.Definitions {
			kind, ok := selectedGeneratedDefinitionKindForTest(typ.SchemaPath, name, schema)
			if !ok {
				continue
			}
			if kind == "" {
				t.Fatalf("generated definition %s in %s has unsupported schema shape", name, typ.SchemaPath)
			}
			if strings.Contains(kind, "+") {
				t.Fatalf("generated definition %s in %s maps to multiple generated kinds: %s", name, typ.SchemaPath, kind)
			}
			selectedCount++
			encoded := encodedSchema(t, schema)
			previous, ok := byName[name]
			if !ok {
				byName[name] = source{
					encoded: encoded,
					kind:    kind,
					path:    typ.SchemaPath,
				}
				continue
			}
			if previous.kind != kind {
				t.Fatalf("generated definition %s maps to multiple generated kinds: %s in %s, %s in %s", name, previous.kind, previous.path, kind, typ.SchemaPath)
			}
			if !bytes.Equal(previous.encoded, encoded) {
				t.Fatalf("generated definition %s maps to one Go type but schema differs between %s and %s", name, previous.path, typ.SchemaPath)
			}
		}
	}
	if selectedCount == 0 {
		t.Fatal("selected generated definition count = 0")
	}
}

func TestSelectGeneratedEnums(t *testing.T) {
	plan, err := BuildProtocolTypePlan(filepath.Join("..", "protocolschema", "appserver", "v2"))
	if err != nil {
		t.Fatal(err)
	}
	enums, err := SelectGeneratedEnums(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(enums) == 0 {
		t.Fatal("selected generated enum count = 0")
	}
	seen := map[string]bool{}
	for _, enum := range enums {
		if enum.TypeName == "" {
			t.Fatalf("generated enum has empty type name: %#v", enum)
		}
		if seen[enum.TypeName] {
			t.Fatalf("generated enum %s appears more than once", enum.TypeName)
		}
		seen[enum.TypeName] = true
		if len(enum.Values) == 0 {
			t.Fatalf("generated enum %s has no values", enum.TypeName)
		}
		if len(enum.Sources) == 0 {
			t.Fatalf("generated enum %s has no source schemas", enum.TypeName)
		}
		valueSeen := map[string]bool{}
		for _, value := range enum.Values {
			if value == "" {
				t.Fatalf("generated enum %s contains empty value", enum.TypeName)
			}
			if valueSeen[value] {
				t.Fatalf("generated enum %s contains duplicate value %q", enum.TypeName, value)
			}
			valueSeen[value] = true
		}
		for _, source := range enum.Sources {
			typ, ok := plan.TypeBySchema(source)
			if !ok || typ.Schema == nil || typ.Schema.Definitions[enum.TypeName] == nil {
				t.Fatalf("generated enum %s source %s does not contain its definition", enum.TypeName, source)
			}
			sourceValues, ok := stringEnumValues(typ.Schema.Definitions[enum.TypeName])
			if !ok || !sameStrings(sourceValues, enum.Values) {
				t.Fatalf("generated enum %s values %v do not match source %s values %v", enum.TypeName, enum.Values, source, sourceValues)
			}
		}
	}
}

func TestStringEnumValuesRejectsImpureSingleOneOfWrapper(t *testing.T) {
	stringEnum := func(value string) *Schema {
		return &Schema{
			Type: SchemaTypeSet{Values: []string{"string"}},
			Enum: []string{value},
		}
	}
	if values, ok := stringEnumValues(&Schema{OneOf: []*Schema{stringEnum("known")}}); !ok || strings.Join(values, ",") != "known" {
		t.Fatalf("pure single-oneOf enum values = %v, ok=%t", values, ok)
	}

	trueAdditionalProperties := true
	defaultWrapped := mustParseSchema(t, `{"oneOf":[{"type":"string","enum":["known"]}],"default":"known"}`)
	requiredWrapped := mustParseSchema(t, `{"oneOf":[{"type":"string","enum":["known"]}],"required":["value"]}`)
	cases := map[string]*Schema{
		"outer anyOf": {
			OneOf: []*Schema{stringEnum("known")},
			AnyOf: []*Schema{stringEnum("other")},
		},
		"outer ref": {
			OneOf: []*Schema{stringEnum("known")},
			Ref:   "#/definitions/Other",
		},
		"outer properties": {
			OneOf:      []*Schema{stringEnum("known")},
			Properties: map[string]*Schema{"extra": stringEnum("other")},
		},
		"outer additionalProperties": {
			OneOf: []*Schema{stringEnum("known")},
			AdditionalProperties: AdditionalProperties{
				Bool:    &trueAdditionalProperties,
				Present: true,
			},
		},
		"outer type": {
			OneOf: []*Schema{stringEnum("known")},
			Type:  SchemaTypeSet{Values: []string{"string"}},
		},
		"outer default":  defaultWrapped,
		"outer required": requiredWrapped,
	}
	for name, schema := range cases {
		t.Run(name, func(t *testing.T) {
			if values, ok := stringEnumValues(schema); ok {
				t.Fatalf("impure wrapper was accepted as enum: %v", values)
			}
		})
	}
}

func TestStringEnumValuesAcceptsPureMultiOneOf(t *testing.T) {
	stringEnum := func(value string) *Schema {
		return &Schema{
			Type: SchemaTypeSet{Values: []string{"string"}},
			Enum: []string{value},
		}
	}
	schema := &Schema{OneOf: []*Schema{
		stringEnum("accept"),
		stringEnum("decline"),
	}}
	if values, ok := stringEnumValues(schema); !ok || strings.Join(values, ",") != "accept,decline" {
		t.Fatalf("multi-oneOf enum values = %v, ok=%t", values, ok)
	}
	modelSchema := &Schema{OneOf: []*Schema{
		stringEnum("text"),
		stringEnum("image"),
	}}
	if values, ok := stringEnumValues(modelSchema); !ok || strings.Join(values, ",") != "text,image" {
		t.Fatalf("InputModality multi-oneOf enum values = %v, ok=%t", values, ok)
	}
	processSchema := &Schema{OneOf: []*Schema{
		stringEnum("stdout"),
		stringEnum("stderr"),
	}}
	if values, ok := stringEnumValues(processSchema); !ok || strings.Join(values, ",") != "stdout,stderr" {
		t.Fatalf("stream multi-oneOf enum values = %v, ok=%t", values, ok)
	}
	mixed := &Schema{OneOf: []*Schema{
		stringEnum("accept"),
		{
			Type:       SchemaTypeSet{Values: []string{"object"}},
			Properties: map[string]*Schema{"value": stringEnum("decline")},
		},
	}}
	if values, ok := stringEnumValues(mixed); ok {
		t.Fatalf("mixed multi-oneOf enum was accepted: %v", values)
	}
}

func mustParseSchema(t *testing.T, raw string) *Schema {
	t.Helper()
	var schema Schema
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		t.Fatal(err)
	}
	return &schema
}

func TestSelectGeneratedScalarUnions(t *testing.T) {
	plan, err := BuildProtocolTypePlan(filepath.Join("..", "protocolschema", "appserver", "v2"))
	if err != nil {
		t.Fatal(err)
	}
	unions, err := SelectGeneratedScalarUnions(plan)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, union := range unions {
		if union.TypeName == "" || union.SchemaPath == "" {
			t.Fatalf("generated scalar union has incomplete identity: %#v", union)
		}
		if seen[union.TypeName] {
			t.Fatalf("generated scalar union %s appears more than once", union.TypeName)
		}
		seen[union.TypeName] = true
		if len(union.Variants) == 0 {
			t.Fatalf("generated scalar union %s has no variants", union.TypeName)
		}
		variantSeen := map[string]bool{}
		for _, variant := range union.Variants {
			if variant.JSONKind == "" || variant.GoType == "" ||
				variant.ConstructorName == "" || variant.AccessorName == "" ||
				variant.GoName == "" || variant.PrivateFieldName == "" {
				t.Fatalf("generated scalar union %s has incomplete variant: %#v", union.TypeName, variant)
			}
			if variantSeen[variant.JSONKind] {
				t.Fatalf("generated scalar union %s has duplicate JSON kind %q", union.TypeName, variant.JSONKind)
			}
			variantSeen[variant.JSONKind] = true
		}
	}
}

func TestSelectGeneratedMixedUnions(t *testing.T) {
	plan, err := BuildProtocolTypePlan(filepath.Join("..", "protocolschema", "appserver", "v2"))
	if err != nil {
		t.Fatal(err)
	}
	unions, err := SelectGeneratedMixedUnions(plan)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, union := range unions {
		if union.TypeName == "" || union.SchemaPath == "" {
			t.Fatalf("generated mixed union has incomplete identity: %#v", union)
		}
		if seen[union.TypeName] {
			t.Fatalf("generated mixed union %s appears more than once", union.TypeName)
		}
		seen[union.TypeName] = true
		if len(union.Variants) == 0 {
			t.Fatalf("generated mixed union %s has no variants", union.TypeName)
		}
		variantSeen := map[string]bool{}
		for _, variant := range union.Variants {
			if variant.DiscriminatorValue == "" || variant.JSONKind == "" ||
				variant.ConstructorName == "" || variant.AccessorName == "" ||
				variant.GoName == "" || variant.PrivateFieldName == "" {
				t.Fatalf("generated mixed union %s has incomplete variant: %#v", union.TypeName, variant)
			}
			if variantSeen[variant.DiscriminatorValue] {
				t.Fatalf("generated mixed union %s has duplicate discriminator value %q", union.TypeName, variant.DiscriminatorValue)
			}
			variantSeen[variant.DiscriminatorValue] = true
			if variant.DirectValueField != nil {
				assertGeneratedFieldPlan(t, union.TypeName, *variant.DirectValueField)
			}
			for _, field := range variant.Fields {
				assertGeneratedFieldPlan(t, union.TypeName, field)
			}
		}
	}
}

func TestSelectGeneratedUntaggedObjectUnions(t *testing.T) {
	plan, err := BuildProtocolTypePlan(filepath.Join("..", "protocolschema", "appserver", "v2"))
	if err != nil {
		t.Fatal(err)
	}
	unions, err := SelectGeneratedUntaggedObjectUnions(plan)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, union := range unions {
		if union.TypeName == "" || union.SchemaPath == "" {
			t.Fatalf("generated untagged object union has incomplete identity: %#v", union)
		}
		if seen[union.TypeName] {
			t.Fatalf("generated untagged object union %s appears more than once", union.TypeName)
		}
		seen[union.TypeName] = true
		if len(union.Variants) == 0 {
			t.Fatalf("generated untagged object union %s has no variants", union.TypeName)
		}
		variantSeen := map[string]bool{}
		for _, variant := range union.Variants {
			if variant.DiscriminatorValue == "" || variant.PayloadTypeName == "" ||
				variant.ConstructorName == "" || variant.AccessorName == "" ||
				variant.GoName == "" || variant.PrivateFieldName == "" {
				t.Fatalf("generated untagged object union %s has incomplete variant: %#v", union.TypeName, variant)
			}
			if variantSeen[variant.DiscriminatorValue] {
				t.Fatalf("generated untagged object union %s has duplicate discriminator value %q", union.TypeName, variant.DiscriminatorValue)
			}
			variantSeen[variant.DiscriminatorValue] = true
			for _, field := range variant.Fields {
				assertGeneratedFieldPlan(t, union.TypeName, field)
			}
		}
	}
}

func encodedSchema(t *testing.T, schema *Schema) []byte {
	t.Helper()
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func selectedGeneratedDefinitionKindForTest(schemaPath string, name string, schema *Schema) (string, bool) {
	if isImplicitGeneratedStringEnumDefinitionSchema(schema) {
		return string(generatedDefinitionStringEnum), true
	}
	if !isReviewedGeneratedDefinition(schemaPath, name) {
		return "", false
	}
	kind := classifyGeneratedDefinition(schema)
	if kind == generatedDefinitionUnsupported {
		return "", true
	}
	return string(kind), true
}

func mustGeneratedDefinitionNameResolver(t *testing.T, types ...TypePlan) generatedDefinitionNameResolver {
	t.Helper()
	resolver, err := newGeneratedDefinitionNameResolver(ProtocolTypePlan{Types: types})
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func assertGeneratedFieldPlan(t *testing.T, owner string, field FieldPlan) {
	t.Helper()
	if field.FieldName == "" || field.Path == "" || field.GoType == "" || field.Kind == "" {
		t.Fatalf("%s has incomplete generated field plan: %#v", owner, field)
	}
	if field.WireAllowsNull && field.Kind != FieldPlanJSONValue && !isNullableGoType(field.GoType) {
		t.Fatalf("%s nullable field %s does not use Nullable support: %s", owner, field.Path, field.GoType)
	}
}

func schemaForGeneratedPlanPath(plan ProtocolTypePlan, path string) *Schema {
	if schemaPath, definitionName, ok := strings.Cut(path, "#/definitions/"); ok {
		parent, exists := plan.TypeBySchema(schemaPath)
		if !exists || parent.Schema == nil {
			return nil
		}
		return parent.Schema.Definitions[definitionName]
	}
	typ, ok := plan.TypeBySchema(path)
	if !ok {
		return nil
	}
	return typ.Schema
}

func taggedVariantByValue(variants []TaggedUnionVariantPlan) map[string]TaggedUnionVariantPlan {
	byValue := map[string]TaggedUnionVariantPlan{}
	for _, variant := range variants {
		byValue[variant.DiscriminatorValue] = variant
	}
	return byValue
}

func TestSelectGeneratedTaggedUnions(t *testing.T) {
	plan, err := BuildProtocolTypePlan(filepath.Join("..", "protocolschema", "appserver", "v2"))
	if err != nil {
		t.Fatal(err)
	}
	unions, err := SelectGeneratedTaggedUnions(plan)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, union := range unions {
		if union.TypeName == "" || union.SchemaPath == "" || union.Discriminator == "" {
			t.Fatalf("generated tagged union has incomplete identity: %#v", union)
		}
		if seen[union.TypeName] {
			t.Fatalf("generated tagged union %s appears more than once", union.TypeName)
		}
		seen[union.TypeName] = true
		schema := schemaForGeneratedPlanPath(plan, union.SchemaPath)
		if schema == nil || len(schema.OneOf) == 0 {
			t.Fatalf("generated tagged union %s source %s is not a oneOf schema", union.TypeName, union.SchemaPath)
		}
		if got, want := len(union.Variants), len(schema.OneOf); got != want {
			t.Fatalf("generated tagged union %s variant count = %d, want schema oneOf count %d", union.TypeName, got, want)
		}
		variantByValue := taggedVariantByValue(union.Variants)
		for index, variantSchema := range schema.OneOf {
			discriminator, value, err := variantDiscriminator(variantSchema)
			if err != nil {
				t.Fatalf("generated tagged union %s schema variant %d: %v", union.TypeName, index, err)
			}
			if discriminator != union.Discriminator {
				t.Fatalf("generated tagged union %s discriminator = %q, want schema discriminator %q", union.TypeName, union.Discriminator, discriminator)
			}
			variant, ok := variantByValue[value]
			if !ok {
				t.Fatalf("generated tagged union %s missing schema discriminator value %q", union.TypeName, value)
			}
			if variant.PayloadTypeName == "" || variant.ConstructorName == "" ||
				variant.AccessorName == "" || variant.GoName == "" || variant.PrivateFieldName == "" {
				t.Fatalf("generated tagged union %s has incomplete variant: %#v", union.TypeName, variant)
			}
			for _, field := range variant.Fields {
				assertGeneratedFieldPlan(t, union.TypeName, field)
			}
		}
	}
}

func TestSelectGeneratedTaggedUnionsSupportsReviewedNullableRefPayload(t *testing.T) {
	schema := mustParseSchema(t, `{
		"oneOf": [{
			"type": "object",
			"required": ["method"],
			"properties": {
				"method": {"type": "string", "enum": ["account/usage/read"]},
				"params": {
					"anyOf": [
						{"$ref": "#/definitions/GetAccountTokenUsageParams"},
						{"type": "null"}
					]
				}
			}
		}],
		"definitions": {
			"GetAccountTokenUsageParams": {
				"type": "object",
				"properties": {"threadId": {"type": ["string", "null"]}}
			}
		}
	}`)
	plan := ProtocolTypePlan{Types: []TypePlan{{
		Kind:       TypePlanTaggedUnionCandidate,
		Schema:     schema,
		SchemaPath: "ClientRequest.json",
		Stability:  "stable",
		TypeName:   "ClientRequest",
	}}}

	unions, err := SelectGeneratedTaggedUnions(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(unions) != 1 || len(unions[0].Variants) != 1 || len(unions[0].Variants[0].Fields) != 1 {
		t.Fatalf("nullable-ref tagged union plan = %#v", unions)
	}
	field := unions[0].Variants[0].Fields[0]
	if field.Kind != FieldPlanNullableRef || field.GoType != "*protocolv2.Nullable[GetAccountTokenUsageParams]" {
		t.Fatalf("nullable-ref params field = kind %s GoType %q", field.Kind, field.GoType)
	}
	if !field.WireAllowsNull || !field.WireOmitAllowed {
		t.Fatal("optional nullable-ref params must preserve omit/null/value semantics")
	}
}

func TestSelectGeneratedTaggedUnionsSupportsReviewedDynamicJSONField(t *testing.T) {
	schema := mustParseSchema(t, `{
		"definitions": {
			"ConfiguredHookHandler": {
				"oneOf": [
					{
						"type": "object",
						"required": ["type"],
						"properties": {"type": {"type": "string", "enum": ["command"]}}
					},
					{
						"type": "object",
						"required": ["input", "type"],
						"properties": {
							"type": {"type": "string", "enum": ["mcp_tool"]},
							"input": {"type": "object", "additionalProperties": true}
						}
					}
				]
			}
		}
	}`)
	plan := ProtocolTypePlan{Types: []TypePlan{{
		Schema:     schema,
		SchemaPath: "v2/ConfigRequirementsReadResponse.json",
		Stability:  "stable",
		TypeName:   "ConfigRequirementsReadResponse",
	}}}

	unions, err := SelectGeneratedTaggedUnions(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(unions) != 1 {
		t.Fatalf("configured hook tagged unions = %#v", unions)
	}
	mcpTool, ok := taggedVariantByValue(unions[0].Variants)["mcp_tool"]
	if !ok || len(mcpTool.Fields) != 1 {
		t.Fatalf("mcp_tool configured hook variant = %#v, ok=%v", mcpTool, ok)
	}
	field := mcpTool.Fields[0]
	if field.Kind != FieldPlanJSONValueMap || field.GoType != "map[string]protocolv2.JSONValue" || !field.Required {
		t.Fatalf("configured hook input field = kind %s GoType %q required=%v", field.Kind, field.GoType, field.Required)
	}
}

func TestSelectGeneratedTaggedUnionsSupportsReviewedNestedNullableUnion(t *testing.T) {
	schema := mustParseSchema(t, `{
		"definitions": {
			"ThreadItem": {
				"oneOf": [{
					"type": "object",
					"required": ["type"],
					"properties": {
						"type": {"type": "string", "enum": ["imageGeneration"]},
						"failure": {
							"anyOf": [
								{"$ref": "#/definitions/ImageGenerationFailure"},
								{"type": "null"}
							]
						}
					}
				}]
			},
			"ImageGenerationFailure": {
				"oneOf": [{
					"type": "object",
					"required": ["limitId", "type"],
					"properties": {
						"type": {"type": "string", "enum": ["usageLimitExceeded"]},
						"limitId": {"type": "string"}
					}
				}]
			}
		}
	}`)
	plan := ProtocolTypePlan{Types: []TypePlan{{
		Schema:     schema,
		SchemaPath: "v2/TurnStartResponse.json",
		Stability:  "stable",
		TypeName:   "TurnStartResponse",
	}}}

	unions, err := SelectGeneratedTaggedUnions(plan)
	if err != nil {
		t.Fatal(err)
	}
	var threadItem TaggedUnionPlan
	for _, union := range unions {
		if union.TypeName == "ThreadItem" {
			threadItem = union
			break
		}
	}
	if len(threadItem.Variants) != 1 || len(threadItem.Variants[0].Fields) != 1 {
		t.Fatalf("ThreadItem tagged union plan = %#v", threadItem)
	}
	field := threadItem.Variants[0].Fields[0]
	if field.Kind != FieldPlanNullableRef || field.GoType != "*protocolv2.Nullable[ImageGenerationFailure]" {
		t.Fatalf("image generation failure field = kind %s GoType %q", field.Kind, field.GoType)
	}
	if !field.WireAllowsNull || !field.WireOmitAllowed {
		t.Fatal("optional image generation failure must preserve omit/null/value semantics")
	}
}

func TestSelectGeneratedTaggedUnionsIncludesReviewedBedrockSetupParams(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "v2"), 0o755); err != nil {
		t.Fatal(err)
	}
	coverage := `{
		"status": "classified-manifest",
		"types": [{
			"schema": "v2/BedrockSetupParams.json",
			"stability": "experimental",
			"status": "supported-generated",
			"type": "BedrockSetupParams"
		}],
		"fields": []
	}`
	schema := `{
		"title": "BedrockSetupParams",
		"oneOf": [
			{
				"type": "object",
				"required": ["profile", "region", "type"],
				"properties": {
					"type": {"type": "string", "enum": ["profile"]},
					"profile": {"type": "string"},
					"region": {"type": "string"}
				}
			},
			{
				"type": "object",
				"required": ["region", "type"],
				"properties": {
					"type": {"type": "string", "enum": ["environment"]},
					"region": {"type": "string"}
				}
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(root, "coverage_matrix.json"), []byte(coverage), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "v2", "BedrockSetupParams.json"), []byte(schema), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := BuildProtocolTypePlan(root)
	if err != nil {
		t.Fatal(err)
	}
	unions, err := SelectGeneratedTaggedUnions(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(unions) != 1 || unions[0].TypeName != "BedrockSetupParams" || unions[0].Discriminator != "type" {
		t.Fatalf("Bedrock setup tagged unions = %#v", unions)
	}
	variants := taggedVariantByValue(unions[0].Variants)
	if len(variants) != 2 {
		t.Fatalf("Bedrock setup variants = %#v", variants)
	}
	if _, ok := variants["profile"]; !ok {
		t.Fatal("Bedrock setup union is missing profile variant")
	}
	if _, ok := variants["environment"]; !ok {
		t.Fatal("Bedrock setup union is missing environment variant")
	}
}

func TestSelectFirstPassGeneratedTypesIncludesReviewedBedrockDiscoverResponse(t *testing.T) {
	schema := mustParseSchema(t, `{
		"definitions": {
			"AwsCredentialType": {"type": "string", "enum": ["accessKeys", "bedrockApiKey"]},
			"BedrockAwsProfile": {
				"type": "object",
				"required": ["name"],
				"properties": {
					"name": {"type": "string"},
					"region": {"type": ["string", "null"]}
				}
			},
			"BedrockEnvironmentCredential": {
				"type": "object",
				"required": ["type"],
				"properties": {
					"region": {"type": ["string", "null"]},
					"type": {"$ref": "#/definitions/AwsCredentialType"}
				}
			}
		}
	}`)
	plan := ProtocolTypePlan{Types: []TypePlan{{
		Fields: []FieldPlan{
			{
				FieldName:  "environmentCredentials",
				GoType:     "[]BedrockEnvironmentCredential",
				Kind:       FieldPlanArrayRef,
				Path:       "v2/BedrockDiscoverResponse.json#/properties/environmentCredentials",
				RefPath:    "v2/BedrockDiscoverResponse.json#/definitions/BedrockEnvironmentCredential",
				Required:   true,
				SchemaPath: "v2/BedrockDiscoverResponse.json",
				Stability:  "experimental",
				TypeName:   "BedrockDiscoverResponse",
			},
			{
				FieldName:  "profiles",
				GoType:     "[]BedrockAwsProfile",
				Kind:       FieldPlanArrayRef,
				Path:       "v2/BedrockDiscoverResponse.json#/properties/profiles",
				RefPath:    "v2/BedrockDiscoverResponse.json#/definitions/BedrockAwsProfile",
				Required:   true,
				SchemaPath: "v2/BedrockDiscoverResponse.json",
				Stability:  "experimental",
				TypeName:   "BedrockDiscoverResponse",
			},
		},
		Kind:       TypePlanObjectStructCandidate,
		Schema:     schema,
		SchemaPath: "v2/BedrockDiscoverResponse.json",
		Stability:  "experimental",
		TypeName:   "BedrockDiscoverResponse",
	}}}

	selected, err := SelectFirstPassGeneratedTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	selectedNames := map[string]bool{}
	for _, typ := range selected {
		selectedNames[typ.TypeName] = true
	}
	for _, name := range []string{"BedrockAwsProfile", "BedrockDiscoverResponse", "BedrockEnvironmentCredential"} {
		if !selectedNames[name] {
			t.Fatalf("selected Bedrock discover types %v do not include %s", selectedNames, name)
		}
	}
}

func TestSelectFirstPassGeneratedTypesIncludesReviewedMisalignmentModels(t *testing.T) {
	schema := mustParseSchema(t, `{
		"definitions": {
			"MisalignmentSteer": {
				"type": "object",
				"required": ["message"],
				"properties": {"message": {"type": "string"}}
			},
			"MisalignmentErrorDetails": {
				"type": "object",
				"properties": {
					"detailedExplanation": {"type": ["string", "null"]},
					"steer": {
						"anyOf": [
							{"$ref": "#/definitions/MisalignmentSteer"},
							{"type": "null"}
						]
					}
				}
			}
		}
	}`)
	plan := ProtocolTypePlan{Types: []TypePlan{{
		Kind:       TypePlanObjectStructCandidate,
		Schema:     schema,
		SchemaPath: "v2/TurnStartResponse.json",
		Stability:  "stable",
		TypeName:   "TurnStartResponse",
	}}}

	selected, err := SelectFirstPassGeneratedTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	selectedNames := map[string]bool{}
	for _, typ := range selected {
		selectedNames[typ.TypeName] = true
	}
	for _, name := range []string{"MisalignmentErrorDetails", "MisalignmentSteer"} {
		if !selectedNames[name] {
			t.Fatalf("selected turn response dependencies %v do not include %s", selectedNames, name)
		}
	}
}

func TestGeneratedTurnSettingsUpdateStatusEnablesResponseType(t *testing.T) {
	schema := mustParseSchema(t, `{
		"definitions": {
			"TurnSettingsUpdateStatus": {
				"oneOf": [
					{"type": "string", "enum": ["applied"]},
					{"type": "string", "enum": ["targetUnavailable"]}
				]
			}
		}
	}`)
	plan := ProtocolTypePlan{Types: []TypePlan{{
		Fields: []FieldPlan{{
			FieldName:  "status",
			GoType:     "TurnSettingsUpdateStatus",
			Kind:       FieldPlanRef,
			Path:       "v2/TurnSettingsUpdateResponse.json#/properties/status",
			RefPath:    "v2/TurnSettingsUpdateResponse.json#/definitions/TurnSettingsUpdateStatus",
			Required:   true,
			SchemaPath: "v2/TurnSettingsUpdateResponse.json",
			Stability:  "experimental",
			TypeName:   "TurnSettingsUpdateResponse",
		}},
		Kind:       TypePlanObjectStructCandidate,
		Schema:     schema,
		SchemaPath: "v2/TurnSettingsUpdateResponse.json",
		Stability:  "experimental",
		TypeName:   "TurnSettingsUpdateResponse",
	}}}

	enums, err := SelectGeneratedEnums(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(enums) != 1 || enums[0].TypeName != "TurnSettingsUpdateStatus" ||
		!sameStrings(enums[0].Values, []string{"applied", "targetUnavailable"}) {
		t.Fatalf("generated turn settings enums = %#v", enums)
	}
	selected, err := SelectFirstPassGeneratedTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].TypeName != "TurnSettingsUpdateResponse" {
		t.Fatalf("selected turn settings response types = %#v", selected)
	}
}

func TestSelectFirstPassGeneratedTypesIncludesReviewedTurnToolOutput(t *testing.T) {
	schema := mustParseSchema(t, `{
		"definitions": {
			"TurnToolOutput": {
				"type": "object",
				"required": ["name", "output"],
				"properties": {
					"name": {"type": "string"},
					"namespace": {"type": ["string", "null"]},
					"output": {"type": "string"}
				}
			}
		}
	}`)
	plan := ProtocolTypePlan{Types: []TypePlan{{
		Kind:       TypePlanObjectStructCandidate,
		Schema:     schema,
		SchemaPath: "v2/TurnStartParams.json",
		Stability:  "stable",
		TypeName:   "TurnStartParams",
	}}}

	selected, err := SelectFirstPassGeneratedTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	selectedNames := map[string]bool{}
	for _, typ := range selected {
		selectedNames[typ.TypeName] = true
	}
	if !selectedNames["TurnToolOutput"] {
		t.Fatalf("selected turn start dependencies %v do not include TurnToolOutput", selectedNames)
	}
}

func TestResponseUsageMetadataKeepsRawResponseCompletedNotificationGenerated(t *testing.T) {
	schema := mustParseSchema(t, `{
		"definitions": {
			"ResponseUsageMetadata": {
				"type": "object",
				"properties": {"amount": {"type": ["string", "null"]}}
			}
		}
	}`)
	plan := ProtocolTypePlan{Types: []TypePlan{{
		Fields: []FieldPlan{{
			FieldName:       "usageMetadata",
			GoType:          "*protocolv2.Nullable[ResponseUsageMetadata]",
			Kind:            FieldPlanNullableRef,
			Path:            "v2/RawResponseCompletedNotification.json#/properties/usageMetadata",
			RefPath:         "v2/RawResponseCompletedNotification.json#/definitions/ResponseUsageMetadata",
			Required:        false,
			SchemaPath:      "v2/RawResponseCompletedNotification.json",
			Stability:       "stable",
			TypeName:        "RawResponseCompletedNotification",
			WireAllowsNull:  true,
			WireOmitAllowed: true,
		}},
		Kind:       TypePlanObjectStructCandidate,
		Schema:     schema,
		SchemaPath: "v2/RawResponseCompletedNotification.json",
		Stability:  "stable",
		TypeName:   "RawResponseCompletedNotification",
	}}}

	selected, err := SelectFirstPassGeneratedTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	selectedNames := map[string]bool{}
	for _, typ := range selected {
		selectedNames[typ.TypeName] = true
	}
	for _, name := range []string{"RawResponseCompletedNotification", "ResponseUsageMetadata"} {
		if !selectedNames[name] {
			t.Fatalf("selected raw response types %v do not include %s", selectedNames, name)
		}
	}
}

func TestSelectFirstPassGeneratedTypesIncludesReviewedAutoReviewRequirements(t *testing.T) {
	schema := mustParseSchema(t, `{
		"definitions": {
			"AutoReviewRequirements": {
				"type": "object",
				"properties": {
					"ignoreRules": {"type": ["array", "null"], "items": {"type": "string"}},
					"requiredOnModels": {"type": ["array", "null"], "items": {"type": "string"}}
				}
			},
			"ConfigRequirements": {
				"type": "object",
				"properties": {
					"autoReview": {
						"anyOf": [
							{"$ref": "#/definitions/AutoReviewRequirements"},
							{"type": "null"}
						]
					}
				}
			}
		}
	}`)
	plan := ProtocolTypePlan{Types: []TypePlan{{
		Fields: []FieldPlan{{
			FieldName:       "requirements",
			GoType:          "*protocolv2.Nullable[ConfigRequirements]",
			Kind:            FieldPlanNullableRef,
			Path:            "v2/ConfigRequirementsReadResponse.json#/properties/requirements",
			RefPath:         "v2/ConfigRequirementsReadResponse.json#/definitions/ConfigRequirements",
			SchemaPath:      "v2/ConfigRequirementsReadResponse.json",
			Stability:       "stable",
			TypeName:        "ConfigRequirementsReadResponse",
			WireAllowsNull:  true,
			WireOmitAllowed: true,
		}},
		Kind:       TypePlanObjectStructCandidate,
		Schema:     schema,
		SchemaPath: "v2/ConfigRequirementsReadResponse.json",
		Stability:  "stable",
		TypeName:   "ConfigRequirementsReadResponse",
	}}}

	selected, err := SelectFirstPassGeneratedTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	selectedNames := map[string]bool{}
	for _, typ := range selected {
		selectedNames[typ.TypeName] = true
	}
	for _, name := range []string{"AutoReviewRequirements", "ConfigRequirements", "ConfigRequirementsReadResponse"} {
		if !selectedNames[name] {
			t.Fatalf("selected config requirements types %v do not include %s", selectedNames, name)
		}
	}
}

func TestSelectFirstPassGeneratedTypesIncludesReviewedProjectModels(t *testing.T) {
	responseSchema := mustParseSchema(t, `{
		"definitions": {
			"AbsolutePathBuf": {"type": "string"},
			"ProjectRoot": {
				"type": "object",
				"required": ["path"],
				"properties": {"path": {"$ref": "#/definitions/AbsolutePathBuf"}}
			},
			"Project": {
				"type": "object",
				"required": ["id", "metadata", "roots"],
				"properties": {
					"id": {"type": "string"},
					"metadata": {"type": "object", "additionalProperties": {"type": "string"}},
					"roots": {"type": "array", "items": {"$ref": "#/definitions/ProjectRoot"}}
				}
			}
		}
	}`)
	plan := ProtocolTypePlan{Types: []TypePlan{
		{
			Fields: []FieldPlan{{
				FieldName:  "roots",
				GoType:     "[]ProjectRoot",
				Kind:       FieldPlanArrayRef,
				Path:       "v2/ProjectCreateParams.json#/properties/roots",
				RefPath:    "v2/ProjectCreateParams.json#/definitions/ProjectRoot",
				Required:   true,
				SchemaPath: "v2/ProjectCreateParams.json",
				Stability:  "experimental",
				TypeName:   "ProjectCreateParams",
			}},
			Kind:       TypePlanObjectStructCandidate,
			Schema:     mustParseSchema(t, `{"type":"object"}`),
			SchemaPath: "v2/ProjectCreateParams.json",
			Stability:  "experimental",
			TypeName:   "ProjectCreateParams",
		},
		{
			Fields: []FieldPlan{{
				FieldName:  "project",
				GoType:     "Project",
				Kind:       FieldPlanRef,
				Path:       "v2/ProjectCreateResponse.json#/properties/project",
				RefPath:    "v2/ProjectCreateResponse.json#/definitions/Project",
				Required:   true,
				SchemaPath: "v2/ProjectCreateResponse.json",
				Stability:  "experimental",
				TypeName:   "ProjectCreateResponse",
			}},
			Kind:       TypePlanObjectStructCandidate,
			Schema:     responseSchema,
			SchemaPath: "v2/ProjectCreateResponse.json",
			Stability:  "experimental",
			TypeName:   "ProjectCreateResponse",
		},
	}}

	selected, err := SelectFirstPassGeneratedTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	selectedNames := map[string]bool{}
	for _, typ := range selected {
		selectedNames[typ.TypeName] = true
	}
	for _, name := range []string{"ProjectRoot", "Project", "ProjectCreateParams", "ProjectCreateResponse"} {
		if !selectedNames[name] {
			t.Fatalf("selected project types %v do not include %s", selectedNames, name)
		}
	}
}

func TestSelectFirstPassGeneratedTypesIncludesReviewedServerDiagnosticsModels(t *testing.T) {
	schema := mustParseSchema(t, `{
		"definitions": {
			"ServerDiagnosticsGauge": {
				"type": "object",
				"required": ["name", "value"],
				"properties": {
					"name": {"type": "string"},
					"value": {"type": "integer", "format": "uint64", "minimum": 0}
				}
			},
			"ServerDiagnosticsProcess": {
				"type": "object",
				"required": ["id"],
				"properties": {
					"id": {"type": "integer", "format": "uint32", "minimum": 0},
					"residentMemoryBytes": {"type": ["integer", "null"], "format": "uint64", "minimum": 0}
				}
			}
		}
	}`)
	plan := ProtocolTypePlan{Types: []TypePlan{{
		Fields: []FieldPlan{
			{
				FieldName:  "gauges",
				GoType:     "[]ServerDiagnosticsGauge",
				Kind:       FieldPlanArrayRef,
				Path:       "v2/ServerDiagnosticsResponse.json#/properties/gauges",
				RefPath:    "v2/ServerDiagnosticsResponse.json#/definitions/ServerDiagnosticsGauge",
				Required:   true,
				SchemaPath: "v2/ServerDiagnosticsResponse.json",
				Stability:  "experimental",
				TypeName:   "ServerDiagnosticsResponse",
			},
			{
				FieldName:  "process",
				GoType:     "ServerDiagnosticsProcess",
				Kind:       FieldPlanRef,
				Path:       "v2/ServerDiagnosticsResponse.json#/properties/process",
				RefPath:    "v2/ServerDiagnosticsResponse.json#/definitions/ServerDiagnosticsProcess",
				Required:   true,
				SchemaPath: "v2/ServerDiagnosticsResponse.json",
				Stability:  "experimental",
				TypeName:   "ServerDiagnosticsResponse",
			},
		},
		Kind:       TypePlanObjectStructCandidate,
		Schema:     schema,
		SchemaPath: "v2/ServerDiagnosticsResponse.json",
		Stability:  "experimental",
		TypeName:   "ServerDiagnosticsResponse",
	}}}

	selected, err := SelectFirstPassGeneratedTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	selectedNames := map[string]bool{}
	for _, typ := range selected {
		selectedNames[typ.TypeName] = true
	}
	for _, name := range []string{"ServerDiagnosticsGauge", "ServerDiagnosticsProcess", "ServerDiagnosticsResponse"} {
		if !selectedNames[name] {
			t.Fatalf("selected server diagnostics types %v do not include %s", selectedNames, name)
		}
	}
}

func TestSelectFirstPassGeneratedTypesIncludesReviewedThreadSectionAppearance(t *testing.T) {
	schema := mustParseSchema(t, `{
		"definitions": {
			"ThreadSectionAppearance": {
				"type": "object",
				"properties": {
					"color": {"type": ["string", "null"]},
					"icon": {"type": ["string", "null"]}
				}
			},
			"ThreadSection": {
				"type": "object",
				"required": ["id", "name"],
				"properties": {
					"id": {"type": "string"},
					"name": {"type": "string"},
					"appearance": {
						"anyOf": [
							{"$ref": "#/definitions/ThreadSectionAppearance"},
							{"type": "null"}
						]
					}
				}
			},
			"Thread": {
				"type": "object",
				"properties": {
					"section": {
						"anyOf": [
							{"$ref": "#/definitions/ThreadSection"},
							{"type": "null"}
						]
					}
				}
			}
		}
	}`)
	plan := ProtocolTypePlan{Types: []TypePlan{{
		Fields: []FieldPlan{{
			FieldName:  "thread",
			GoType:     "Thread",
			Kind:       FieldPlanRef,
			Path:       "v2/ThreadStartResponse.json#/properties/thread",
			RefPath:    "v2/ThreadStartResponse.json#/definitions/Thread",
			Required:   true,
			SchemaPath: "v2/ThreadStartResponse.json",
			Stability:  "stable",
			TypeName:   "ThreadStartResponse",
		}},
		Kind:       TypePlanObjectStructCandidate,
		Schema:     schema,
		SchemaPath: "v2/ThreadStartResponse.json",
		Stability:  "stable",
		TypeName:   "ThreadStartResponse",
	}}}

	selected, err := SelectFirstPassGeneratedTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	selectedNames := map[string]bool{}
	for _, typ := range selected {
		selectedNames[typ.TypeName] = true
	}
	for _, name := range []string{"ThreadSectionAppearance", "ThreadSection", "Thread", "ThreadStartResponse"} {
		if !selectedNames[name] {
			t.Fatalf("selected thread section types %v do not include %s", selectedNames, name)
		}
	}
}

func TestSelectFirstPassGeneratedTypesIncludesReviewedQueuedSubmission(t *testing.T) {
	schema := mustParseSchema(t, `{
		"definitions": {
			"QueuedSubmission": {
				"type": "object",
				"required": ["clientUserMessageId", "id", "input"],
				"properties": {
					"clientUserMessageId": {"type": "string"},
					"id": {"type": "string"},
					"input": {"type": "array", "items": {"$ref": "#/definitions/UserInput"}}
				}
			}
		}
	}`)
	plan := ProtocolTypePlan{Types: []TypePlan{
		{
			Kind:       TypePlanEmptyStructCandidate,
			Schema:     mustParseSchema(t, `{"type":"object"}`),
			SchemaPath: "v2/UserInputFixture.json",
			Stability:  "experimental",
			TypeName:   "UserInput",
		},
		{
			Fields: []FieldPlan{{
				FieldName:  "queuedSubmission",
				GoType:     "QueuedSubmission",
				Kind:       FieldPlanRef,
				Path:       "v2/ThreadQueueAddResponse.json#/properties/queuedSubmission",
				RefPath:    "v2/ThreadQueueAddResponse.json#/definitions/QueuedSubmission",
				Required:   true,
				SchemaPath: "v2/ThreadQueueAddResponse.json",
				Stability:  "experimental",
				TypeName:   "ThreadQueueAddResponse",
			}},
			Kind:       TypePlanObjectStructCandidate,
			Schema:     schema,
			SchemaPath: "v2/ThreadQueueAddResponse.json",
			Stability:  "experimental",
			TypeName:   "ThreadQueueAddResponse",
		},
	}}

	selected, err := SelectFirstPassGeneratedTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	selectedNames := map[string]bool{}
	for _, typ := range selected {
		selectedNames[typ.TypeName] = true
	}
	for _, name := range []string{"UserInput", "QueuedSubmission", "ThreadQueueAddResponse"} {
		if !selectedNames[name] {
			t.Fatalf("selected queue types %v do not include %s", selectedNames, name)
		}
	}
}

func TestGeneratedDefinitionClassifierUsesSchemaShape(t *testing.T) {
	cases := map[string]struct {
		schema string
		kind   generatedDefinitionKind
	}{
		"object struct": {
			schema: `{"type":"object","properties":{"name":{"type":"string"}}}`,
			kind:   generatedDefinitionStruct,
		},
		"string enum": {
			schema: `{"type":"string","enum":["allow","deny"]}`,
			kind:   generatedDefinitionStringEnum,
		},
		"scalar alias": {
			schema: `{"type":"string","minLength":1}`,
			kind:   generatedDefinitionScalarAlias,
		},
		"scalar union": {
			schema: `{
				"anyOf": [
					{"type":"string"},
					{"type":"integer","format":"int64"}
				]
			}`,
			kind: generatedDefinitionScalarUnion,
		},
		"tagged union": {
			schema: `{
				"oneOf": [
					{
						"type":"object",
						"required":["type"],
						"properties":{"type":{"type":"string","enum":["function"]}}
					},
					{
						"type":"object",
						"required":["type"],
						"properties":{"type":{"type":"string","enum":["namespace"]}}
					}
				]
			}`,
			kind: generatedDefinitionTaggedUnion,
		},
		"mixed union": {
			schema: `{
				"oneOf": [
					{"type":"string","enum":["auto"]},
					{
						"type":"object",
						"required":["value"],
						"properties":{"value":{"type":"string"}}
					}
				]
			}`,
			kind: generatedDefinitionMixedUnion,
		},
		"untagged object union": {
			schema: `{
				"anyOf": [
					{"type":"object","properties":{"path":{"type":"string"}}},
					{"type":"object","properties":{"url":{"type":"string"}}}
				]
			}`,
			kind: generatedDefinitionUntaggedObjectUnion,
		},
		"unsupported true schema": {
			schema: `true`,
			kind:   generatedDefinitionUnsupported,
		},
		"unsupported array": {
			schema: `{"type":"array","items":{"type":"string"}}`,
			kind:   generatedDefinitionUnsupported,
		},
	}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			if got := classifyGeneratedDefinition(mustParseSchema(t, tt.schema)); got != tt.kind {
				t.Fatalf("classifyGeneratedDefinition() = %s, want %s", got, tt.kind)
			}
		})
	}
}

func TestFirstPassTypesIncludeReviewedRPCDependencies(t *testing.T) {
	plan, err := BuildProtocolTypePlan(filepath.Join("..", "protocolschema", "appserver", "v2"))
	if err != nil {
		t.Fatal(err)
	}
	types, err := SelectFirstPassGeneratedTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	generated := map[string]bool{}
	for _, typ := range types {
		generated[typ.TypeName] = true
	}
	for _, name := range []string{
		"AppsInstalledResponse",
		"InstalledApp",
		"AppsReadResponse",
		"AppToolSummary",
		"ConnectorMetadata",
		"ConfigReadResponse",
		"Config",
		"BrowserUseConfig",
		"BrowserUseOriginPolicyConfig",
		"ComputerUseConfig",
		"ComputerUseMacosConfig",
		"ComputerUseWindowsConfig",
		"ComputerUseWindowsExeConfig",
		"ConfigRequirementsReadResponse",
		"ConfigRequirements",
		"BrowserUseOriginPolicy",
		"BrowserUseRequirements",
		"ComputerUseMacosRequirements",
		"ComputerUseRequirements",
		"ComputerUseWindowsExeRequirement",
		"ComputerUseWindowsRequirements",
		"InAppBrowserRequirements",
		"McpServerEventNotification",
		"McpServerEventStreamNotification",
		"FeedbackRequirements",
		"EnvironmentStatusResponse",
		"ExternalAgentConfigDetectResponse",
		"ExternalAgentDetectedConnectorCandidate",
		"ExternalAgentConfigImportHistoriesReadResponse",
		"ExternalAgentConfigImportHistoryRecordParams",
		"ExternalAgentConfigImportHistoryRecordSuccessParams",
		"ExternalAgentConfigImportHistoryRecordTypeResultParams",
		"ExternalAgentImportedConnectorCandidate",
		"PluginSearchParams",
		"PluginSearchResponse",
		"PluginSearchResult",
		"PluginReadResponse",
		"PluginDetail",
		"ScheduledTaskSummary",
		"ThreadItemsListResponse",
		"ThreadItemEntry",
		"ThreadSection",
		"ThreadRealtimeStartParams",
		"ThreadRealtimeInitialItem",
		"ThreadRealtimeItemCompletedNotification",
		"ThreadRealtimeItemStartedNotification",
		"ThreadSearchOccurrencesResponse",
		"ThreadSearchOccurrence",
		"ThreadSearchTextRange",
	} {
		if !generated[name] {
			t.Errorf("first-pass generated types do not include new RPC type dependency %s", name)
		}
	}
	enums, err := SelectGeneratedEnums(plan)
	if err != nil {
		t.Fatal(err)
	}
	wantEnums := map[string]bool{
		"EnvironmentStatusKind":                false,
		"ExternalAgentImportedConnectorSource": false,
		"ScheduledTaskWeekday":                 false,
		"CodexResponseHandoffMode":             false,
		"ConversationTextRole":                 false,
	}
	for _, enum := range enums {
		if _, ok := wantEnums[enum.TypeName]; ok {
			wantEnums[enum.TypeName] = true
		}
	}
	for name, found := range wantEnums {
		if !found {
			t.Errorf("generated enums do not include %s", name)
		}
	}
	taggedUnions, err := SelectGeneratedTaggedUnions(plan)
	if err != nil {
		t.Fatal(err)
	}
	foundScheduledTaskSchedule := false
	for _, union := range taggedUnions {
		if union.TypeName == "ScheduledTaskSchedule" {
			foundScheduledTaskSchedule = true
			break
		}
	}
	if !foundScheduledTaskSchedule {
		t.Error("generated tagged unions do not include ScheduledTaskSchedule")
	}
}

func TestGeneratedDefinitionSelectionFollowsSchemaShape(t *testing.T) {
	objectParent := TypePlan{
		SchemaPath: "v2/ThreadStartParams.json",
		Stability:  "stable",
		TypeName:   "ThreadStartParams",
		Schema: &Schema{
			Definitions: map[string]*Schema{
				"DynamicToolSpec": mustParseSchema(t, `{
					"type": "object",
					"required": ["description", "inputSchema", "name"],
					"properties": {
						"deferLoading": {"type": "boolean"},
						"description": {"type": "string"},
						"inputSchema": true,
						"name": {"type": "string"},
						"namespace": {"type": ["string", "null"]}
					}
				}`),
			},
		},
	}
	objectResolver := mustGeneratedDefinitionNameResolver(t, objectParent)
	taggedCandidates, err := generatedDefinitionTaggedUnionCandidates(objectParent, objectResolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(taggedCandidates) != 0 {
		t.Fatalf("object DynamicToolSpec tagged candidate count = %d, want 0", len(taggedCandidates))
	}
	structCandidates, err := generatedDefinitionTypeCandidates(objectParent, objectResolver)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(structCandidates), 1; got != want {
		t.Fatalf("object DynamicToolSpec struct candidate count = %d, want %d", got, want)
	}
	if structCandidates[0].TypeName != "DynamicToolSpec" || structCandidates[0].Kind != TypePlanObjectStructCandidate {
		t.Fatalf("object DynamicToolSpec candidate = %#v", structCandidates[0])
	}
	fields := map[string]FieldPlan{}
	for _, field := range structCandidates[0].Fields {
		fields[field.FieldName] = field
	}
	if fields["description"].GoType != "string" || !fields["description"].Required ||
		fields["inputSchema"].Kind != FieldPlanJSONValue || !fields["inputSchema"].Required ||
		fields["name"].GoType != "string" || !fields["name"].Required {
		t.Fatalf("object DynamicToolSpec fields = %#v", fields)
	}

	unionParent := TypePlan{
		SchemaPath: "v2/ThreadStartParams.json",
		Stability:  "stable",
		TypeName:   "ThreadStartParams",
		Schema: &Schema{
			Definitions: map[string]*Schema{
				"DynamicToolSpec": mustParseSchema(t, `{
					"oneOf": [
						{
							"type": "object",
							"title": "FunctionDynamicToolSpec",
							"required": ["description", "inputSchema", "name", "type"],
							"properties": {
								"description": {"type": "string"},
								"inputSchema": true,
								"name": {"type": "string"},
								"type": {"type": "string", "enum": ["function"], "title": "FunctionDynamicToolSpecType"}
							}
						},
						{
							"type": "object",
							"title": "NamespaceDynamicToolSpec",
							"required": ["description", "name", "tools", "type"],
							"properties": {
								"description": {"type": "string"},
								"name": {"type": "string"},
								"tools": {"type": "array", "items": {"$ref": "#/definitions/DynamicToolNamespaceTool"}},
								"type": {"type": "string", "enum": ["namespace"], "title": "NamespaceDynamicToolSpecType"}
							}
						}
					]
				}`),
			},
		},
	}
	unionResolver := mustGeneratedDefinitionNameResolver(t, unionParent)
	structCandidates, err = generatedDefinitionTypeCandidates(unionParent, unionResolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(structCandidates) != 0 {
		t.Fatalf("union DynamicToolSpec struct candidate count = %d, want 0", len(structCandidates))
	}
	taggedCandidates, err = generatedDefinitionTaggedUnionCandidates(unionParent, unionResolver)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(taggedCandidates), 1; got != want {
		t.Fatalf("union DynamicToolSpec tagged candidate count = %d, want %d", got, want)
	}
	if taggedCandidates[0].TypeName != "DynamicToolSpec" || taggedCandidates[0].Kind != TypePlanTaggedUnionCandidate {
		t.Fatalf("union DynamicToolSpec candidate = %#v", taggedCandidates[0])
	}
}

func TestGeneratedDefinitionNameResolverReusesSameNameSameShape(t *testing.T) {
	schema := mustParseSchema(t, `{
		"type": "string",
		"minLength": 1
	}`)
	plan := ProtocolTypePlan{Types: []TypePlan{{
		SchemaPath: "v2/ConfigReadResponse.json",
		TypeName:   "ConfigReadResponse",
		Schema: &Schema{Definitions: map[string]*Schema{
			"ReasoningEffort": schema,
		}},
	}, {
		SchemaPath: "v2/ThreadStartParams.json",
		TypeName:   "ThreadStartParams",
		Schema: &Schema{Definitions: map[string]*Schema{
			"ReasoningEffort": schema,
		}},
	}}}
	resolver, err := newGeneratedDefinitionNameResolver(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"v2/ConfigReadResponse.json", "v2/ThreadStartParams.json"} {
		if got, ok := resolver.NameForDefinition(path, "ReasoningEffort"); !ok || got != "ReasoningEffort" {
			t.Fatalf("%s ReasoningEffort resolved to %q, ok=%t", path, got, ok)
		}
	}
}

func TestGeneratedDefinitionNameResolverSplitsSameNameDifferentShapes(t *testing.T) {
	plan := ProtocolTypePlan{Types: []TypePlan{{
		SchemaPath: "v2/ConfigReadResponse.json",
		TypeName:   "ConfigReadResponse",
		Schema: &Schema{Definitions: map[string]*Schema{
			"ReasoningEffort": mustParseSchema(t, `{
				"type": "object",
				"required": ["value"],
				"properties": {
					"value": {"type": "string"}
				}
			}`),
		}},
	}, {
		SchemaPath: "v2/ThreadStartParams.json",
		TypeName:   "ThreadStartParams",
		Schema: &Schema{Definitions: map[string]*Schema{
			"ReasoningEffort": mustParseSchema(t, `{
				"type": "string",
				"minLength": 1
			}`),
		}},
	}}}
	resolver, err := newGeneratedDefinitionNameResolver(plan)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := resolver.NameForDefinition("v2/ConfigReadResponse.json", "ReasoningEffort"); !ok || got != "ConfigReadResponseReasoningEffort" {
		t.Fatalf("object ReasoningEffort resolved to %q, ok=%t", got, ok)
	}
	if got, ok := resolver.NameForDefinition("v2/ThreadStartParams.json", "ReasoningEffort"); !ok || got != "ThreadStartParamsReasoningEffort" {
		t.Fatalf("alias ReasoningEffort resolved to %q, ok=%t", got, ok)
	}
	field := resolver.ResolveField(FieldPlan{
		GoType:     "*[]ReasoningEffort",
		Kind:       FieldPlanArrayRef,
		RefPath:    "v2/ThreadStartParams.json#/definitions/ReasoningEffort",
		SchemaPath: "v2/ThreadStartParams.json",
	})
	if field.GoType != "*[]ThreadStartParamsReasoningEffort" {
		t.Fatalf("resolved field GoType = %q", field.GoType)
	}
}

func TestGenerateProtocolTypesEmitsNullableField(t *testing.T) {
	generated, err := GenerateProtocolTypes(ProtocolTypePlan{Types: []TypePlan{{
		Kind:       TypePlanObjectStructCandidate,
		SchemaPath: "Example.json",
		TypeName:   "Example",
		Fields: []FieldPlan{{
			FieldName:       "serviceTier",
			GoType:          "*protocolv2.Nullable[string]",
			Kind:            FieldPlanNullableServiceTier,
			Path:            "Example.json#/properties/serviceTier",
			Required:        false,
			WireAllowsNull:  true,
			WireOmitAllowed: true,
		}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	want := "ServiceTier *Nullable[string] `json:\"serviceTier,omitempty\"`"
	if !strings.Contains(text, want) {
		t.Fatalf("generated nullable protocol type does not contain %q:\n%s", want, text)
	}
}

func TestGenerateProtocolTypesEmitsRequiredCollectionMarshalGuards(t *testing.T) {
	minItems := uint64(1)
	generated, err := GenerateProtocolTypes(ProtocolTypePlan{Types: []TypePlan{{
		Kind:       TypePlanObjectStructCandidate,
		SchemaPath: "Example.json",
		TypeName:   "Example",
		Fields: []FieldPlan{{
			FieldName:      "items",
			GoType:         "[]string",
			Kind:           FieldPlanArrayString,
			MinItems:       &minItems,
			Path:           "Example.json#/properties/items",
			Required:       true,
			WireAllowsNull: false,
		}, {
			FieldName:      "labels",
			GoType:         "map[string]string",
			Kind:           FieldPlanTypedMap,
			Path:           "Example.json#/properties/labels",
			Required:       true,
			WireAllowsNull: false,
		}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	for _, want := range []string{
		"func (value Example) MarshalJSON() ([]byte, error)",
		`return nil, fmt.Errorf("encode Example.items: nil is not allowed")`,
		`return nil, fmt.Errorf("encode Example.items: must contain at least 1 item")`,
		`return nil, fmt.Errorf("encode Example.labels: nil is not allowed")`,
		`return fmt.Errorf("decode Example.items: must contain at least 1 item")`,
		"return json.Marshal(wire(value))",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated required collection marshal guard does not contain %q:\n%s", want, text)
		}
	}
}

func TestGenerateProtocolTypesEmitsTaggedUnionBoundary(t *testing.T) {
	plan, err := BuildProtocolTypePlan(filepath.Join("..", "protocolschema", "appserver", "v2"))
	if err != nil {
		t.Fatal(err)
	}
	generated, err := GenerateProtocolTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	for _, want := range []string{
		"type LoginAccountParams struct {\n\tkind",
		"func NewLoginAccountParamsAPIKey(payload LoginAccountParamsAPIKey) LoginAccountParams",
		"func (value LoginAccountParams) AsAPIKey() (LoginAccountParamsAPIKey, bool)",
		"func (value *LoginAccountParams) UnmarshalJSON(data []byte) error",
		`return unknownUnionVariant("LoginAccountParams", "type", variant)`,
		"type ClientNotification struct {\n\tkind",
		"func NewClientNotificationInitialized() ClientNotification",
		"func (value ClientNotification) AsInitialized() (ClientNotificationInitialized, bool)",
		`return unknownUnionVariant("ClientNotification", "method", variant)`,
		"type ClientRequest struct {\n\tkind",
		"func NewClientRequestThreadStart(payload ClientRequestThreadStart) ClientRequest",
		"func (value ClientRequest) AsMemoryReset() (ClientRequestMemoryReset, bool)",
		`return unknownUnionVariant("ClientRequest", "method", variant)`,
		"type ServerNotification struct {\n\tkind",
		"func NewServerNotificationThreadTokenUsageUpdated(payload ServerNotificationThreadTokenUsageUpdated) ServerNotification",
		"func (value ServerNotification) AsThreadRealtimeSDP() (ServerNotificationThreadRealtimeSDP, bool)",
		`return unknownUnionVariant("ServerNotification", "method", variant)`,
		"type ServerRequest struct {\n\tkind",
		"func NewServerRequestItemCommandExecutionRequestApproval(payload ServerRequestItemCommandExecutionRequestApproval) ServerRequest",
		"func (value ServerRequest) AsItemToolCall() (ServerRequestItemToolCall, bool)",
		`return unknownUnionVariant("ServerRequest", "method", variant)`,
		"type FileChange struct {\n\tkind",
		"func NewFileChangeUpdate(payload FileChangeUpdate) FileChange",
		"func (value FileChange) AsUpdate() (FileChangeUpdate, bool)",
		`return unknownUnionVariant("FileChange", "type", variant)`,
		"type ParsedCommand struct {\n\tkind",
		"func NewParsedCommandSearch(payload ParsedCommandSearch) ParsedCommand",
		"func (value ParsedCommand) AsSearch() (ParsedCommandSearch, bool)",
		`return unknownUnionVariant("ParsedCommand", "type", variant)`,
		"type DynamicToolCallOutputContentItem struct {\n\tkind",
		"func NewDynamicToolCallOutputContentItemInputText(payload DynamicToolCallOutputContentItemInputText) DynamicToolCallOutputContentItem",
		"func (value DynamicToolCallOutputContentItem) AsInputImage() (DynamicToolCallOutputContentItemInputImage, bool)",
		`return unknownUnionVariant("DynamicToolCallOutputContentItem", "type", variant)`,
		"type Account struct {\n\tkind",
		"func NewAccountChatGPT(payload AccountChatGPT) Account",
		"func (value Account) AsAmazonBedrock() (AccountAmazonBedrock, bool)",
		`return unknownUnionVariant("Account", "type", variant)`,
		"type SandboxPolicy struct {\n\tkind",
		"func NewSandboxPolicyWorkspaceWrite(payload SandboxPolicyWorkspaceWrite) SandboxPolicy",
		"func (value SandboxPolicy) AsReadOnly() (SandboxPolicyReadOnly, bool)",
		`return unknownUnionVariant("SandboxPolicy", "type", variant)`,
		"type ConfigLayerSource struct {\n\tkind",
		"func NewConfigLayerSourceMdm(payload ConfigLayerSourceMdm) ConfigLayerSource",
		"func (value ConfigLayerSource) AsSessionFlags() (ConfigLayerSourceSessionFlags, bool)",
		`return unknownUnionVariant("ConfigLayerSource", "type", variant)`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated tagged union output does not contain %q", want)
		}
	}
	for _, forbidden := range []string{
		"APIKey *LoginAccountParamsAPIKey",
		"UnknownVariant",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated tagged union output contains forbidden marker %q", forbidden)
		}
	}
}

func TestGenerateProtocolTypesEmitsScalarUnionBoundary(t *testing.T) {
	plan, err := BuildProtocolTypePlan(filepath.Join("..", "protocolschema", "appserver", "v2"))
	if err != nil {
		t.Fatal(err)
	}
	generated, err := GenerateProtocolTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	for _, want := range []string{
		"type RequestId struct {\n\tkind",
		"func NewRequestIdString(value string) RequestId",
		"func NewRequestIdInt64(value int64) RequestId",
		"func (value RequestId) AsString() (string, bool)",
		"func (value RequestId) AsInt64() (int64, bool)",
		"func (value *RequestId) UnmarshalJSON(data []byte) error",
		"func NewThreadListCwdFilterArray(value []string) ThreadListCwdFilter",
		`return fmt.Errorf("decode ThreadListCwdFilter: expected array item %d to be string", index)`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated scalar union output does not contain %q", want)
		}
	}
	for _, forbidden := range []string{
		"StringValue",
		"Int64Value",
		"UnknownVariant",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated scalar union output contains forbidden marker %q", forbidden)
		}
	}
}

func TestGenerateProtocolTypesDoesNotExposeJSONRPCEnvelopeSurface(t *testing.T) {
	plan, err := BuildProtocolTypePlan(filepath.Join("..", "protocolschema", "appserver", "v2"))
	if err != nil {
		t.Fatal(err)
	}
	generated, err := GenerateProtocolTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	for _, forbidden := range []string{
		"type JSONRPCError ",
		"type JSONRPCNotification ",
		"type JSONRPCRequest ",
		"type JSONRPCResponse ",
		"type JSONRPCMessage ",
		"func NewJSONRPCMessage",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated protocolv2 output exposes JSON-RPC envelope surface %q", forbidden)
		}
	}
}

func TestGenerateProtocolTypesEmitsMixedUnionBoundary(t *testing.T) {
	plan, err := BuildProtocolTypePlan(filepath.Join("..", "protocolschema", "appserver", "v2"))
	if err != nil {
		t.Fatal(err)
	}
	generated, err := GenerateProtocolTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	for _, want := range []string{
		"type ReviewDecision struct {\n\tkind",
		"type CommandExecutionApprovalDecision struct {\n\tkind",
		"func NewCommandExecutionApprovalDecisionAcceptWithExecpolicyAmendment(payload CommandExecutionApprovalDecisionAcceptWithExecpolicyAmendment) CommandExecutionApprovalDecision",
		"func NewCommandExecutionApprovalDecisionApplyNetworkPolicyAmendment(payload CommandExecutionApprovalDecisionApplyNetworkPolicyAmendment) CommandExecutionApprovalDecision",
		"func (value CommandExecutionApprovalDecision) AsApplyNetworkPolicyAmendment() (CommandExecutionApprovalDecisionApplyNetworkPolicyAmendment, bool)",
		`return unknownUnionVariant("CommandExecutionApprovalDecision", "value", variant)`,
		"func NewReviewDecisionApproved() ReviewDecision",
		"func NewReviewDecisionApprovedExecpolicyAmendment(payload ReviewDecisionApprovedExecpolicyAmendment) ReviewDecision",
		"func NewReviewDecisionNetworkPolicyAmendment(payload ReviewDecisionNetworkPolicyAmendment) ReviewDecision",
		"func (value ReviewDecision) AsNetworkPolicyAmendment() (ReviewDecisionNetworkPolicyAmendment, bool)",
		`return unknownUnionVariant("ReviewDecision", "value", variant)`,
		`return nil, fmt.Errorf("encode ReviewDecision.approved_execpolicy_amendment.proposed_execpolicy_amendment: nil is not allowed")`,
		"type NetworkPolicyAmendment struct {",
		"Decision ReviewDecision `json:\"decision\"`",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated mixed union output does not contain %q", want)
		}
	}
	for _, forbidden := range []string{
		"ReviewDecision string",
		"ReviewDecisionUnknownVariant",
		"CommandExecutionApprovalDecision string",
		"CommandExecutionApprovalDecisionUnknownVariant",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated mixed union output contains forbidden marker %q", forbidden)
		}
	}
}

func TestGeneratedProtocolTypesKeepTypedBoundary(t *testing.T) {
	plan, err := BuildProtocolTypePlan(filepath.Join("..", "protocolschema", "appserver", "v2"))
	if err != nil {
		t.Fatal(err)
	}
	generated, err := GenerateProtocolTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	for _, forbidden := range []string{"json.RawMessage", "map[string]any", "interface{}", "UnknownFields", "AdditionalFields"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated protocol types contain forbidden public passthrough marker %q", forbidden)
		}
	}
}

func TestFirstPassSelectionRejectsRefMapLeafTypes(t *testing.T) {
	typ := TypePlan{
		Kind:       TypePlanObjectStructCandidate,
		SchemaPath: "Example.json",
		TypeName:   "Example",
		Fields: []FieldPlan{{
			FieldName: "answers",
			GoType:    "map[string]ToolRequestUserInputAnswer",
			Kind:      FieldPlanTypedMap,
			Path:      "Example.json#/properties/answers",
			Required:  true,
		}},
	}
	selected, err := SelectFirstPassGeneratedTypes(ProtocolTypePlan{Types: []TypePlan{typ}})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 0 {
		t.Fatalf("ref map leaf type was selected: %#v", selected)
	}
}

func TestGeneratedTypeSelectionResolvesEnumStructNameCollision(t *testing.T) {
	enumSchema := &Schema{
		Type: SchemaTypeSet{Values: []string{"object"}},
		Definitions: map[string]*Schema{
			"Example": {
				Type: SchemaTypeSet{Values: []string{"string"}},
				Enum: []string{"known"},
			},
		},
	}
	typ := TypePlan{
		Kind:       TypePlanEmptyStructCandidate,
		Schema:     enumSchema,
		SchemaPath: "Example.json",
		TypeName:   "Example",
	}
	plan := ProtocolTypePlan{Types: []TypePlan{typ}}
	enums, err := SelectGeneratedEnums(plan)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(enums), 1; got != want {
		t.Fatalf("generated enum count = %d, want %d", got, want)
	}
	if enums[0].TypeName != "Example2" {
		t.Fatalf("generated enum type name = %q, want Example2", enums[0].TypeName)
	}
	selected, err := SelectFirstPassGeneratedTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(selected), 1; got != want {
		t.Fatalf("selected generated type count = %d, want %d", got, want)
	}
	if selected[0].TypeName != "Example" {
		t.Fatalf("selected generated type name = %q, want Example", selected[0].TypeName)
	}
}

func TestFieldGoNameUsesGoAcronyms(t *testing.T) {
	cases := map[string]string{
		"authorizationUrl": "AuthorizationURL",
		"chatgptAccountId": "ChatGPTAccountID",
		"cwds":             "CWDs",
		"httpStatusCode":   "HTTPStatusCode",
		"threadIds":        "ThreadIDs",
		"threadId":         "ThreadID",
		"uri":              "URI",
	}
	for field, want := range cases {
		if got := fieldGoName(field); got != want {
			t.Fatalf("fieldGoName(%q) = %q, want %q", field, got, want)
		}
	}
}

func TestLeafGoTypePeelsNullableArrays(t *testing.T) {
	if got, want := leafGoType("*Nullable[[]ToolRequestUserInputOption]"), "ToolRequestUserInputOption"; got != want {
		t.Fatalf("leafGoType nullable array = %q, want %q", got, want)
	}
}
