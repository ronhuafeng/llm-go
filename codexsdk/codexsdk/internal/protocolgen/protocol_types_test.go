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
	plan, err := BuildProtocolTypePlan(filepath.Join("..", "protocolschema", "appserver", "v2"))
	if err != nil {
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
		t.Fatal("generated protocol types do not match checked-in codexsdk/protocolv2/protocol_types.gen.go")
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

func definitionExists(typ TypePlan, name string) bool {
	return typ.Schema != nil && typ.Schema.Definitions[name] != nil
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

func TestGenerateProtocolTypesEmitsNullableDecoder(t *testing.T) {
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
	for _, want := range []string{
		"ServiceTier *Nullable[string] `json:\"serviceTier,omitempty\"`",
		`decodeNullableJSONField[string](fields, "serviceTier", "Example.serviceTier", &decoded.ServiceTier)`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated nullable protocol type does not contain %q:\n%s", want, text)
		}
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
