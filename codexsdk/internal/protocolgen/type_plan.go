package protocolgen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ProtocolTypePlan struct {
	Fields []FieldPlan
	Types  []TypePlan
}

type TypePlan struct {
	Fields                []FieldPlan
	GeneratedDefinitions  map[string]bool
	GeneratedRoot         bool
	Kind                  TypePlanKind
	OpenDynamicProperties bool
	Reason                string
	Schema                *Schema
	SchemaPath            string
	Stability             string
	Status                string
	TypeName              string
	WireMessageRoles      WireMessageRoles
}

type WireMessageRoles uint8

const (
	WireMessageRoleActionBearingMessage WireMessageRoles = 1 << iota
	WireMessageRoleServerObservation
)

func (roles WireMessageRoles) Has(role WireMessageRoles) bool {
	return roles&role != 0
}

type TypePlanKind string

const (
	TypePlanAggregateBundle       TypePlanKind = "aggregate_bundle"
	TypePlanAnyOfDeferred         TypePlanKind = "anyof_deferred"
	TypePlanEmptyStructCandidate  TypePlanKind = "empty_struct_candidate"
	TypePlanObjectStructCandidate TypePlanKind = "object_struct_candidate"
	TypePlanScalarUnionCandidate  TypePlanKind = "scalar_union_candidate"
	TypePlanTaggedUnionCandidate  TypePlanKind = "tagged_union_candidate"
)

type FieldPlan struct {
	FieldName       string
	GoType          string
	Kind            FieldPlanKind
	MinItems        *uint64
	Minimum         *float64
	Path            string
	RefPath         string
	Reason          string
	Required        bool
	SchemaPath      string
	Stability       string
	TypeName        string
	WireAllowsNull  bool
	WireOmitAllowed bool
}

type FieldPlanKind string

const (
	FieldPlanAllOfRef            FieldPlanKind = "allof_ref"
	FieldPlanArrayJSONValue      FieldPlanKind = "array_json_value"
	FieldPlanArrayRef            FieldPlanKind = "array_ref"
	FieldPlanArrayScalar         FieldPlanKind = "array_scalar"
	FieldPlanArrayString         FieldPlanKind = "array_string"
	FieldPlanBool                FieldPlanKind = "bool"
	FieldPlanConstrainedDeferred FieldPlanKind = "constrained_deferred"
	FieldPlanDescriptionOnly     FieldPlanKind = "description_only_deferred"
	FieldPlanJSONValue           FieldPlanKind = "json_value"
	FieldPlanJSONValueMap        FieldPlanKind = "json_value_map"
	FieldPlanNullableRef         FieldPlanKind = "nullable_ref"
	FieldPlanNullableScalar      FieldPlanKind = "nullable_scalar"
	FieldPlanNullableServiceTier FieldPlanKind = "nullable_service_tier"
	FieldPlanOutputSchema        FieldPlanKind = "output_schema"
	FieldPlanRef                 FieldPlanKind = "ref"
	FieldPlanScalar              FieldPlanKind = "scalar"
	FieldPlanStringEnum          FieldPlanKind = "string_enum"
	FieldPlanTypedMap            FieldPlanKind = "typed_map"
	FieldPlanUnionDeferred       FieldPlanKind = "union_deferred"
)

func BuildProtocolTypePlan(schemaRoot string) (ProtocolTypePlan, error) {
	matrix, err := LoadCoverageMatrix(filepath.Join(schemaRoot, "coverage_matrix.json"))
	if err != nil {
		return ProtocolTypePlan{}, err
	}
	schemas, err := LoadCoverageSchemas(schemaRoot, matrix)
	if err != nil {
		return ProtocolTypePlan{}, err
	}
	fieldsBySchema := map[string][]CoverageField{}
	for _, field := range matrix.Fields {
		fieldsBySchema[field.Schema] = append(fieldsBySchema[field.Schema], field)
	}
	for schemaPath := range fieldsBySchema {
		sort.Slice(fieldsBySchema[schemaPath], func(i, j int) bool {
			return fieldsBySchema[schemaPath][i].Field < fieldsBySchema[schemaPath][j].Field
		})
	}

	var plan ProtocolTypePlan
	for _, file := range schemas {
		typePlan, err := planType(file)
		if err != nil {
			return ProtocolTypePlan{}, err
		}
		for _, coverageField := range fieldsBySchema[file.Path] {
			fieldSchema := file.Schema.Properties[coverageField.Field]
			if fieldSchema == nil {
				return ProtocolTypePlan{}, fmt.Errorf("coverage field %s is not present in schema %s", coverageField.Path, file.Path)
			}
			fieldPlan, err := planField(coverageField, fieldSchema)
			if err != nil {
				return ProtocolTypePlan{}, err
			}
			typePlan.Fields = append(typePlan.Fields, fieldPlan)
			plan.Fields = append(plan.Fields, fieldPlan)
		}
		plan.Types = append(plan.Types, typePlan)
	}
	if err := markReachableGeneratedDefinitions(&plan, schemaRoot); err != nil {
		return ProtocolTypePlan{}, err
	}
	resolver, err := newGeneratedDefinitionNameResolver(plan)
	if err != nil {
		return ProtocolTypePlan{}, err
	}
	resolveProtocolTypePlanRefs(&plan, resolver)
	return plan, nil
}

func markReachableGeneratedDefinitions(plan *ProtocolTypePlan, schemaRoot string) error {
	if plan == nil {
		return fmt.Errorf("protocol type plan is nil")
	}
	byDocument := map[string]int{}
	byTypeName := map[string][]int{}
	for index := range plan.Types {
		typ := &plan.Types[index]
		if typ.Schema == nil {
			continue
		}
		document := schemaDocumentPath(typ.SchemaPath)
		if document == "" {
			continue
		}
		byDocument[document] = index
		byTypeName[typ.TypeName] = append(byTypeName[typ.TypeName], index)
		if typ.GeneratedDefinitions == nil {
			typ.GeneratedDefinitions = map[string]bool{}
		}
	}

	visitedSchemas := map[string]bool{}
	visitedDefinitions := map[string]bool{}
	visitedTypes := map[int]bool{}
	var walkSchema func(string, string, string, string, *Schema, bool) error
	var walkRef func(string, string) error
	var walkType func(int) error

	walkType = func(index int) error {
		if visitedTypes[index] {
			return nil
		}
		visitedTypes[index] = true
		typ := &plan.Types[index]
		typ.GeneratedRoot = true
		for _, field := range typ.Fields {
			if field.RefPath != "" {
				if err := walkRef(typ.SchemaPath, field.RefPath); err != nil {
					return err
				}
			}
		}
		switch typ.Kind {
		case TypePlanTaggedUnionCandidate, TypePlanScalarUnionCandidate, TypePlanAnyOfDeferred:
			return walkSchema(schemaDocumentPath(typ.SchemaPath), typ.SchemaPath, typ.Stability, typ.TypeName, typ.Schema, false)
		default:
			return nil
		}
	}

	walkRef = func(currentDocument, ref string) error {
		absolute := absoluteRefPath(currentDocument, ref)
		document, fragment, hasFragment := strings.Cut(absolute, "#")
		index, ok := byDocument[document]
		if !ok {
			return nil
		}
		target := &plan.Types[index]
		if !hasFragment || fragment == "" {
			return walkType(index)
		}
		const prefix = "/definitions/"
		if !strings.HasPrefix(fragment, prefix) {
			return nil
		}
		token := strings.TrimPrefix(fragment, prefix)
		if before, _, ok := strings.Cut(token, "/"); ok {
			token = before
		}
		name := strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		definition := target.Schema.Definitions[name]
		if definition == nil {
			return fmt.Errorf("schema ref %s resolves missing definition %s in %s", ref, name, document)
		}
		if isJSONRPCEnvelopeSchema(document) && isJSONRPCEnvelopeSchema(name) {
			// Handwritten envelope validation owns these shapes. Other reachable
			// definitions, such as trace context, still need generated types.
			return nil
		}
		for _, topLevelIndex := range byTypeName[name] {
			topLevel := &plan.Types[topLevelIndex]
			if topLevel.SchemaPath == document || !isGeneratedTopLevelType(*topLevel) {
				continue
			}
			same, err := sameGeneratedRootShape(definition, target.Schema.Definitions, topLevel.Schema)
			if err != nil {
				return err
			}
			if same {
				return walkType(topLevelIndex)
			}
		}
		definitionPath := definitionSchemaPath(document, name)
		target.GeneratedDefinitions[name] = true
		if visitedDefinitions[definitionPath] {
			return nil
		}
		visitedDefinitions[definitionPath] = true
		return walkSchema(document, definitionPath, target.Stability, name, definition, false)
	}

	walkSchema = func(document, schemaPath, stability, typeName string, schema *Schema, expandObjectPayload bool) error {
		if schema == nil || visitedSchemas[schemaPath] {
			return nil
		}
		visitedSchemas[schemaPath] = true
		if schema.Ref != "" {
			if err := walkRef(document, schema.Ref); err != nil {
				return err
			}
		}
		if len(schema.Properties) > 0 {
			required := schema.RequiredSet()
			names := make([]string, 0, len(schema.Properties))
			for name := range schema.Properties {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				property := schema.Properties[name]
				if property != nil && property.Type.Only("null") && !required[name] {
					// Optional null-only fields carry no typed dependencies.
					continue
				}
				fieldPath := schemaPropertyPath(schemaPath, name)
				field, err := planField(CoverageField{
					Field:     name,
					Path:      fieldPath,
					Required:  required[name],
					Schema:    document,
					Stability: stability,
					Status:    "supported-generated",
					Type:      typeName,
				}, property)
				if err != nil {
					return fmt.Errorf("dependency %s: %w", fieldPath, err)
				}
				if field.RefPath != "" {
					if err := walkRef(document, field.RefPath); err != nil {
						return err
					}
				}
				if expandObjectPayload && len(schema.Properties) == 1 && required[name] &&
					schema.AdditionalProperties.Bool != nil && !*schema.AdditionalProperties.Bool &&
					property != nil && property.Type.Only("object") && len(property.Properties) > 0 {
					if err := walkSchema(document, fieldPath, stability, typeName, property, false); err != nil {
						return err
					}
				}
			}
		}
		if schema.Type.Only("array") && schema.Items != nil {
			if schema.Items.Ref != "" {
				if err := walkRef(document, schema.Items.Ref); err != nil {
					return err
				}
			} else if err := walkSchema(document, nestedSchemaPath(schemaPath, "items", 0), stability, typeName, schema.Items, false); err != nil {
				return err
			}
		}
		if len(schema.Properties) == 0 && schema.AdditionalProperties.Schema != nil && schema.AdditionalProperties.Schema.Ref != "" {
			if err := walkRef(document, schema.AdditionalProperties.Schema.Ref); err != nil {
				return err
			}
		}
		for _, group := range []struct {
			keyword  string
			variants []*Schema
		}{
			{keyword: "allOf", variants: schema.AllOf},
			{keyword: "anyOf", variants: schema.AnyOf},
			{keyword: "oneOf", variants: schema.OneOf},
		} {
			for index, variant := range group.variants {
				if err := walkSchema(document, nestedSchemaPath(schemaPath, group.keyword, index), stability, typeName, variant, group.keyword == "oneOf" && isMixedUnionDefinitionSchema(schema)); err != nil {
					return err
				}
			}
		}
		return nil
	}

	rootIndexes, err := generatedDefinitionRootIndexes(plan, schemaRoot)
	if err != nil {
		return err
	}
	for index := range rootIndexes {
		if err := walkType(index); err != nil {
			return err
		}
	}
	for index := range plan.Types {
		typ := &plan.Types[index]
		if typ.Kind == TypePlanScalarUnionCandidate && typ.Status == "supported-generated" {
			if err := walkType(index); err != nil {
				return err
			}
		}
	}
	return nil
}

func sameGeneratedRootShape(definition *Schema, sourceDefinitions map[string]*Schema, topLevel *Schema) (bool, error) {
	left := cloneSchemaWithoutDocumentation(definition)
	right := cloneSchemaWithoutDocumentation(topLevel)
	left.Definitions = nil
	right.Definitions = nil
	leftShape, err := json.Marshal(left)
	if err != nil {
		return false, err
	}
	rightShape, err := json.Marshal(right)
	if err != nil {
		return false, err
	}
	if !bytes.Equal(leftShape, rightShape) {
		return false, nil
	}
	for name, topDefinition := range topLevel.Definitions {
		sourceDefinition := sourceDefinitions[name]
		if sourceDefinition == nil {
			return false, nil
		}
		sourceShape, err := generatedSchemaShape(sourceDefinition)
		if err != nil {
			return false, err
		}
		topShape, err := generatedSchemaShape(topDefinition)
		if err != nil {
			return false, err
		}
		if !bytes.Equal(sourceShape, topShape) {
			return false, nil
		}
	}
	return true, nil
}

func generatedDefinitionRootIndexes(plan *ProtocolTypePlan, schemaRoot string) (map[int]bool, error) {
	fallback := func() map[int]bool {
		roots := map[int]bool{}
		for index := range plan.Types {
			typ := &plan.Types[index]
			if typ.Schema == nil || isAggregateBundle(typ.SchemaPath) || isJSONRPCEnvelopeSchema(typ.SchemaPath) {
				continue
			}
			typ.GeneratedRoot = true
			roots[index] = true
		}
		return roots
	}
	if schemaRoot == "" {
		return fallback(), nil
	}
	manifestPath := filepath.Join(schemaRoot, "manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		if os.IsNotExist(err) {
			return fallback(), nil
		}
		return nil, err
	}
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	byTypeName := map[string][]int{}
	bySchemaPath := map[string]int{}
	for index := range plan.Types {
		typ := &plan.Types[index]
		byTypeName[typ.TypeName] = append(byTypeName[typ.TypeName], index)
		bySchemaPath[typ.SchemaPath] = index
	}
	roots := map[int]bool{}
	for _, entry := range manifest.Entries {
		if index, ok := bySchemaPath[entry.SourceSchema]; ok {
			plan.Types[index].GeneratedRoot = true
			roots[index] = true
		}
		for _, index := range byTypeName[entry.ParamsOrPayloadSchema] {
			plan.Types[index].GeneratedRoot = true
			roots[index] = true
		}
		if index, ok := bySchemaPath[entry.ResponseSchema]; ok {
			plan.Types[index].GeneratedRoot = true
			roots[index] = true
		}
	}
	for index := range plan.Types {
		typ := &plan.Types[index]
		if typ.Status == "supported-generated" &&
			(typ.Kind == TypePlanScalarUnionCandidate || isClosedRPCErrorRoot(*typ) ||
				isJSONRPCEnvelopeSchema(typ.SchemaPath) && typ.Kind == TypePlanObjectStructCandidate) {
			typ.GeneratedRoot = true
			roots[index] = true
		}
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("manifest has no generated protocol type roots")
	}
	return roots, nil
}

func isClosedRPCErrorRoot(typ TypePlan) bool {
	schema := typ.Schema
	if schema == nil || !schema.Type.Only("object") || isJSONRPCEnvelopeSchema(typ.SchemaPath) {
		return false
	}
	code, message, data := schema.Properties["code"], schema.Properties["message"], schema.Properties["data"]
	required := schema.RequiredSet()
	return code != nil && code.Type.Only("integer") && code.Format == "int64" &&
		message != nil && message.Type.Only("string") && data != nil &&
		required["code"] && required["message"] && required["data"] &&
		schema.AdditionalProperties.Bool != nil && !*schema.AdditionalProperties.Bool
}

func schemaDocumentPath(path string) string {
	if before, _, ok := strings.Cut(path, "#"); ok {
		return before
	}
	return path
}

func schemaPropertyPath(schemaPath, field string) string {
	if strings.Contains(schemaPath, "#") {
		return schemaPath + "/properties/" + field
	}
	return schemaPath + "#/properties/" + field
}

func nestedSchemaPath(schemaPath, keyword string, index int) string {
	if keyword == "items" {
		if strings.Contains(schemaPath, "#") {
			return schemaPath + "/items"
		}
		return schemaPath + "#/items"
	}
	if strings.Contains(schemaPath, "#") {
		return fmt.Sprintf("%s/%s/%d", schemaPath, keyword, index)
	}
	return fmt.Sprintf("%s#/%s/%d", schemaPath, keyword, index)
}

func (p ProtocolTypePlan) TypeBySchema(path string) (TypePlan, bool) {
	for _, typ := range p.Types {
		if typ.SchemaPath == path {
			return typ, true
		}
	}
	return TypePlan{}, false
}

func (p ProtocolTypePlan) FieldByPath(path string) (FieldPlan, bool) {
	for _, field := range p.Fields {
		if field.Path == path {
			return field, true
		}
	}
	return FieldPlan{}, false
}

type generatedDefinitionNameResolver struct {
	namesByPath    map[string]string
	topLevelReuses map[string]bool
}

type generatedDefinitionSource struct {
	baseName       string
	encoded        []byte
	kind           generatedDefinitionKind
	parentTypeName string
	path           string
	shape          []byte
}

type generatedTopLevelSource struct {
	kind  generatedDefinitionKind
	shape []byte
}

func newGeneratedDefinitionNameResolver(plan ProtocolTypePlan) (generatedDefinitionNameResolver, error) {
	plan = normalizeExplicitProtocolTypePlan(plan)
	usedNames := map[string]bool{}
	topLevelPlans := map[string]TypePlan{}
	for _, typ := range plan.Types {
		if typ.TypeName == "" || !isGeneratedTopLevelType(typ) {
			continue
		}
		usedNames[typ.TypeName] = true
		kind := classifyGeneratedDefinition(typ.Schema)
		if kind == generatedDefinitionUnsupported {
			continue
		}
		topLevelPlans[typ.TypeName] = typ
	}

	byBaseName := map[string][]generatedDefinitionSource{}
	for _, typ := range plan.Types {
		if typ.Schema == nil || len(typ.Schema.Definitions) == 0 {
			continue
		}
		for name, schema := range typ.Schema.Definitions {
			if !isGeneratedDefinitionNameResolverSource(typ, name, schema) {
				continue
			}
			kind := classifyGeneratedDefinition(schema)
			if kind == generatedDefinitionUnsupported {
				continue
			}
			encoded, err := json.Marshal(schema)
			if err != nil {
				return generatedDefinitionNameResolver{}, fmt.Errorf("generated definition %s in %s cannot be encoded: %w", name, typ.SchemaPath, err)
			}
			shape, err := generatedSchemaShape(schema)
			if err != nil {
				return generatedDefinitionNameResolver{}, fmt.Errorf("generated definition %s in %s shape cannot be encoded: %w", name, typ.SchemaPath, err)
			}
			byBaseName[name] = append(byBaseName[name], generatedDefinitionSource{
				baseName:       name,
				encoded:        encoded,
				kind:           kind,
				parentTypeName: typ.TypeName,
				path:           definitionSchemaPath(typ.SchemaPath, name),
				shape:          shape,
			})
		}
	}

	resolver := generatedDefinitionNameResolver{
		namesByPath:    map[string]string{},
		topLevelReuses: map[string]bool{},
	}
	var baseNames []string
	for name := range byBaseName {
		baseNames = append(baseNames, name)
	}
	sort.Strings(baseNames)
	for _, baseName := range baseNames {
		sources := byBaseName[baseName]
		sort.Slice(sources, func(i, j int) bool {
			return sources[i].path < sources[j].path
		})
		if topLevelPlan, ok := topLevelPlans[baseName]; ok {
			topLevelShape, err := generatedSchemaShape(topLevelPlan.Schema)
			if err != nil {
				return generatedDefinitionNameResolver{}, fmt.Errorf("generated top-level type %s in %s cannot be encoded: %w", topLevelPlan.TypeName, topLevelPlan.SchemaPath, err)
			}
			topLevel := generatedTopLevelSource{kind: classifyGeneratedDefinition(topLevelPlan.Schema), shape: topLevelShape}
			remaining := sources[:0]
			for _, source := range sources {
				if source.kind == topLevel.kind && bytes.Equal(source.shape, topLevel.shape) {
					resolver.namesByPath[source.path] = baseName
					resolver.topLevelReuses[source.path] = true
					continue
				}
				remaining = append(remaining, source)
			}
			sources = remaining
			if len(sources) == 0 {
				continue
			}
		}
		bySignature := map[string][]generatedDefinitionSource{}
		for _, source := range sources {
			signature := string(source.kind) + "\x00" + string(source.encoded)
			bySignature[signature] = append(bySignature[signature], source)
		}
		if len(bySignature) == 1 {
			typeName := claimGeneratedDefinitionTypeName(baseName, usedNames)
			for _, source := range sources {
				resolver.namesByPath[source.path] = typeName
			}
			continue
		}

		var signatures []string
		for signature := range bySignature {
			signatures = append(signatures, signature)
		}
		sort.Slice(signatures, func(i, j int) bool {
			return bySignature[signatures[i]][0].path < bySignature[signatures[j]][0].path
		})
		for _, signature := range signatures {
			signatureSources := bySignature[signature]
			sort.Slice(signatureSources, func(i, j int) bool {
				return signatureSources[i].path < signatureSources[j].path
			})
			typeName := claimGeneratedDefinitionTypeName(definitionScopedTypeName(signatureSources[0].parentTypeName, baseName), usedNames)
			for _, source := range signatureSources {
				resolver.namesByPath[source.path] = typeName
			}
		}
	}
	return resolver, nil
}

func isGeneratedDefinitionNameResolverSource(parent TypePlan, name string, schema *Schema) bool {
	return isGeneratedDefinitionSelected(parent, name)
}

func generatedSchemaShape(schema *Schema) ([]byte, error) {
	normalized := cloneSchemaWithoutDocumentation(schema)
	return json.Marshal(normalized)
}

func cloneSchemaWithoutDocumentation(schema *Schema) *Schema {
	if schema == nil {
		return nil
	}
	cloned := *schema
	cloned.Description = ""
	cloned.Title = ""
	cloned.Items = cloneSchemaWithoutDocumentation(schema.Items)
	cloned.AdditionalProperties = schema.AdditionalProperties
	cloned.AdditionalProperties.Schema = cloneSchemaWithoutDocumentation(schema.AdditionalProperties.Schema)
	cloned.AllOf = cloneSchemaSliceWithoutDocumentation(schema.AllOf)
	cloned.AnyOf = cloneSchemaSliceWithoutDocumentation(schema.AnyOf)
	cloned.OneOf = cloneSchemaSliceWithoutDocumentation(schema.OneOf)
	if schema.Properties != nil {
		cloned.Properties = make(map[string]*Schema, len(schema.Properties))
		for name, child := range schema.Properties {
			cloned.Properties[name] = cloneSchemaWithoutDocumentation(child)
		}
	}
	if schema.Definitions != nil {
		cloned.Definitions = make(map[string]*Schema, len(schema.Definitions))
		for name, child := range schema.Definitions {
			cloned.Definitions[name] = cloneSchemaWithoutDocumentation(child)
		}
	}
	return &cloned
}

func cloneSchemaSliceWithoutDocumentation(in []*Schema) []*Schema {
	if in == nil {
		return nil
	}
	out := make([]*Schema, len(in))
	for index, schema := range in {
		out[index] = cloneSchemaWithoutDocumentation(schema)
	}
	return out
}

func isGeneratedDefinitionSelected(parent TypePlan, name string) bool {
	if !parent.GeneratedDefinitions[name] {
		return false
	}
	if parent.Schema != nil {
		schema := parent.Schema.Definitions[name]
		if classifyGeneratedDefinition(schema) == generatedDefinitionScalarAlias {
			if _, ok := inlineScalarAliasGoType(name); ok {
				return false
			}
		}
	}
	return true
}

func isGeneratedTopLevelType(typ TypePlan) bool {
	if !typ.GeneratedRoot || isAggregateBundle(typ.SchemaPath) || isJSONRPCEnvelopeSchema(typ.SchemaPath) {
		return false
	}
	switch typ.Kind {
	case TypePlanEmptyStructCandidate,
		TypePlanObjectStructCandidate,
		TypePlanScalarUnionCandidate,
		TypePlanTaggedUnionCandidate,
		TypePlanAnyOfDeferred:
		return true
	default:
		return false
	}
}

func normalizeExplicitProtocolTypePlan(plan ProtocolTypePlan) ProtocolTypePlan {
	explicit := len(plan.Types) > 0
	for _, typ := range plan.Types {
		if typ.Status != "" {
			explicit = false
			break
		}
	}
	if !explicit {
		return plan
	}
	for index := range plan.Types {
		typ := &plan.Types[index]
		typ.GeneratedRoot = true
		if typ.GeneratedDefinitions == nil {
			typ.GeneratedDefinitions = map[string]bool{}
		}
		if typ.Schema == nil {
			continue
		}
		for name, schema := range typ.Schema.Definitions {
			if classifyGeneratedDefinition(schema) != generatedDefinitionUnsupported {
				typ.GeneratedDefinitions[name] = true
			}
		}
	}
	return plan
}

func definitionSchemaPath(schemaPath string, name string) string {
	return schemaPath + "#/definitions/" + name
}

func definitionScopedTypeName(parentTypeName string, baseName string) string {
	if parentTypeName == "" {
		return baseName
	}
	return parentTypeName + baseName
}

func claimGeneratedDefinitionTypeName(preferred string, used map[string]bool) string {
	if preferred == "" {
		preferred = "GeneratedDefinition"
	}
	if reservedProtocolTypeName(preferred) {
		preferred += "Value"
	}
	if !used[preferred] {
		used[preferred] = true
		return preferred
	}
	for index := 2; ; index++ {
		candidate := fmt.Sprintf("%s%d", preferred, index)
		if reservedProtocolTypeName(candidate) || used[candidate] {
			continue
		}
		used[candidate] = true
		return candidate
	}
}

func (r generatedDefinitionNameResolver) ReusesTopLevel(schemaPath string, name string) bool {
	return r.topLevelReuses[definitionSchemaPath(schemaPath, name)]
}

func (r generatedDefinitionNameResolver) NameForDefinition(schemaPath string, name string) (string, bool) {
	return r.NameForPath(definitionSchemaPath(schemaPath, name))
}

func (r generatedDefinitionNameResolver) NameForPath(path string) (string, bool) {
	name, ok := r.namesByPath[path]
	return name, ok
}

func (r generatedDefinitionNameResolver) ResolveField(field FieldPlan) FieldPlan {
	if field.RefPath == "" {
		return field
	}
	typeName, ok := r.NameForPath(field.RefPath)
	if !ok {
		return field
	}
	field.GoType = replaceLeafGoType(field.GoType, refTypeName(field.RefPath), typeName)
	return field
}

func resolveProtocolTypePlanRefs(plan *ProtocolTypePlan, resolver generatedDefinitionNameResolver) {
	for index := range plan.Fields {
		plan.Fields[index] = resolver.ResolveField(plan.Fields[index])
	}
	for typeIndex := range plan.Types {
		for fieldIndex := range plan.Types[typeIndex].Fields {
			plan.Types[typeIndex].Fields[fieldIndex] = resolver.ResolveField(plan.Types[typeIndex].Fields[fieldIndex])
		}
	}
}

func replaceLeafGoType(goType string, oldLeaf string, newLeaf string) string {
	if oldLeaf == "" || newLeaf == "" || oldLeaf == newLeaf {
		return goType
	}
	switch {
	case goType == oldLeaf:
		return newLeaf
	case strings.HasPrefix(goType, "*"):
		return "*" + replaceLeafGoType(strings.TrimPrefix(goType, "*"), oldLeaf, newLeaf)
	case strings.HasPrefix(goType, "[]"):
		return "[]" + replaceLeafGoType(strings.TrimPrefix(goType, "[]"), oldLeaf, newLeaf)
	case strings.HasPrefix(goType, "map[string]"):
		return "map[string]" + replaceLeafGoType(strings.TrimPrefix(goType, "map[string]"), oldLeaf, newLeaf)
	case strings.HasPrefix(goType, "protocolv2.Nullable[") && strings.HasSuffix(goType, "]"):
		inner := strings.TrimSuffix(strings.TrimPrefix(goType, "protocolv2.Nullable["), "]")
		return "protocolv2.Nullable[" + replaceLeafGoType(inner, oldLeaf, newLeaf) + "]"
	case strings.HasPrefix(goType, "Nullable[") && strings.HasSuffix(goType, "]"):
		inner := strings.TrimSuffix(strings.TrimPrefix(goType, "Nullable["), "]")
		return "Nullable[" + replaceLeafGoType(inner, oldLeaf, newLeaf) + "]"
	default:
		return goType
	}
}

func absoluteRefPath(schemaPath string, ref string) string {
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "#") {
		document := schemaPath
		if before, _, ok := strings.Cut(schemaPath, "#"); ok {
			document = before
		}
		return document + ref
	}
	return ref
}

func planType(file SchemaFile) (TypePlan, error) {
	schema := file.Schema
	plan := TypePlan{
		Schema:     schema,
		SchemaPath: file.Path,
		Stability:  file.Stability,
		Status:     file.Status,
		TypeName:   file.TypeName,
	}
	switch {
	case isAggregateBundle(file.Path):
		plan.Kind = TypePlanAggregateBundle
		plan.Reason = "aggregate schema bundle is a generator input, not a public protocol type"
	case len(schema.OneOf) > 0:
		if !isTaggedUnionDefinitionSchema(schema) {
			return TypePlan{}, fmt.Errorf("top-level oneOf schema %s has unsupported union shape", file.Path)
		}
		plan.Kind = TypePlanTaggedUnionCandidate
		if len(schema.Properties) > 0 {
			plan.Reason = "top-level object properties plus discriminator-backed oneOf"
		} else {
			plan.Reason = "top-level discriminator-backed oneOf"
		}
	case schema.Type.Only("object") && len(schema.Properties) > 0:
		plan.Kind = TypePlanObjectStructCandidate
		plan.Reason = "object schema with top-level properties"
	case schema.Type.Only("object"):
		plan.Kind = TypePlanEmptyStructCandidate
		plan.Reason = "object schema without top-level properties"
	case len(schema.AnyOf) > 0:
		if isSupportedScalarUnion(schema.AnyOf) {
			plan.Kind = TypePlanScalarUnionCandidate
			plan.Reason = "top-level anyOf with losslessly supported scalar JSON kinds"
			return plan, nil
		}
		if isTopLevelNullableRefWrapper(schema.AnyOf) {
			plan.Kind = TypePlanAnyOfDeferred
			plan.Reason = "top-level nullable ref wrapper is represented by aggregate request params handling"
			return plan, nil
		}
		if isUntaggedObjectUnionDefinitionSchema(schema) {
			plan.Kind = TypePlanAnyOfDeferred
			plan.Reason = "top-level untagged object union"
			return plan, nil
		}
		if isJSONRPCEnvelopeSchema(file.Path) {
			plan.Kind = TypePlanAnyOfDeferred
			plan.Reason = "JSON-RPC envelope anyOf is transport-owned rather than public protocol payload"
			return plan, nil
		}
		return TypePlan{}, fmt.Errorf("top-level anyOf schema %s has unsupported union shape", file.Path)
	default:
		return TypePlan{}, fmt.Errorf("schema %s has unsupported top-level shape", file.Path)
	}
	return plan, nil
}

func isTopLevelNullableRefWrapper(variants []*Schema) bool {
	if len(variants) != 2 {
		return false
	}
	var hasRef bool
	var hasNull bool
	for _, variant := range variants {
		switch {
		case variant != nil && variant.Ref != "":
			hasRef = true
		case variant != nil && variant.Type.Only("null"):
			hasNull = true
		default:
			return false
		}
	}
	return hasRef && hasNull
}
func isSupportedScalarUnion(variants []*Schema) bool {
	if len(variants) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, variant := range variants {
		if variant == nil {
			return false
		}
		switch {
		case variant.Type.Only("string"):
			if hasNonTypeShape(variant) {
				return false
			}
			if len(unmodeledKeywords(variant)) > 0 {
				return false
			}
			seen["string"] = true
		case variant.Type.Only("integer") && variant.Format == "int64":
			if hasNonTypeShape(variant) {
				return false
			}
			if !sameStrings(unmodeledKeywords(variant), []string{"format"}) {
				return false
			}
			seen["integer"] = true
		case variant.Type.Only("array") && variant.Items != nil && (variant.Items.Ref != "" || variant.Items.Type.Only("string")):
			if variant.Ref != "" ||
				len(variant.Properties) > 0 ||
				len(variant.OneOf) > 0 ||
				len(variant.AnyOf) > 0 ||
				len(variant.AllOf) > 0 ||
				len(variant.Enum) > 0 ||
				variant.AdditionalProperties.Present ||
				len(unmodeledKeywords(variant)) > 0 {
				return false
			}
			seen["array"] = true
		default:
			return false
		}
	}
	return len(seen) == len(variants) && len(seen) >= 2
}

func planField(coverage CoverageField, schema *Schema) (FieldPlan, error) {
	plan := FieldPlan{
		FieldName:       coverage.Field,
		Path:            coverage.Path,
		Required:        coverage.Required,
		SchemaPath:      coverage.Schema,
		Stability:       coverage.Stability,
		TypeName:        coverage.Type,
		WireOmitAllowed: !coverage.Required,
		WireAllowsNull:  schemaAllowsNull(schema),
	}
	if overlay, ok, err := overlayFieldPlan(plan, schema); ok || err != nil {
		return overlay, err
	}
	if schema.IsTrueSchema() {
		plan.Kind = FieldPlanJSONValue
		plan.GoType = optionalGoType(plan.Required, "protocolv2.JSONValue")
		plan.Reason = "protocol-native unconstrained JSON value"
		return plan, nil
	}
	if schema.IsFalseSchema() {
		return FieldPlan{}, fmt.Errorf("field %s uses unsupported false schema", coverage.Path)
	}
	if len(schema.Type.Values) == 0 && schema.Description != "" && !hasStructuralShape(schema) && len(unmodeledKeywords(schema)) == 0 {
		plan.Kind = FieldPlanDescriptionOnly
		plan.Reason = "schema field only has description; typed representation is deferred until reviewed"
		return plan, nil
	}
	if arrayCanPlanBeforeRecursiveConstraints(schema) {
		if nullableType, ok := schema.Type.NullableSingle(); ok {
			return planNullableField(plan, schema, nullableType)
		}
		return planArrayField(plan, schema, false)
	}
	constraints, unknown := partitionUnmodeledKeywords(unmodeledKeywords(schema))
	if len(unknown) > 0 {
		return FieldPlan{}, fmt.Errorf("field %s has unreviewed JSON Schema keywords: %s", coverage.Path, strings.Join(unknown, ", "))
	}
	if len(constraints) > 0 {
		if constrained, ok, err := planConstrainedField(plan, schema, constraints); ok || err != nil {
			return constrained, err
		}
		plan.Kind = FieldPlanConstrainedDeferred
		plan.Reason = fmt.Sprintf("schema has validation keywords that require generated validation before support: %s", strings.Join(constraints, ", "))
		return plan, nil
	}
	if schema.Ref != "" {
		if scalarAlias, ok := inlineScalarAliasGoType(schema.Ref); ok {
			plan.Kind = FieldPlanScalar
			plan.GoType = optionalGoType(plan.Required, scalarAlias)
			plan.Reason = "reviewed scalar alias ref"
			return plan, nil
		}
		plan.Kind = FieldPlanRef
		plan.GoType = optionalGoType(plan.Required, refTypeName(schema.Ref))
		plan.RefPath = absoluteRefPath(plan.SchemaPath, schema.Ref)
		plan.Reason = "direct schema ref"
		return plan, nil
	}
	if len(schema.AllOf) == 1 && schema.AllOf[0].Ref != "" {
		if scalarAlias, ok := inlineScalarAliasGoType(schema.AllOf[0].Ref); ok {
			plan.Kind = FieldPlanScalar
			plan.GoType = optionalGoType(plan.Required, scalarAlias)
			plan.Reason = "reviewed scalar alias allOf ref"
			return plan, nil
		}
		plan.Kind = FieldPlanAllOfRef
		plan.GoType = optionalGoType(plan.Required, refTypeName(schema.AllOf[0].Ref))
		plan.RefPath = absoluteRefPath(plan.SchemaPath, schema.AllOf[0].Ref)
		plan.Reason = "single allOf ref normalized as ref"
		return plan, nil
	}
	if len(schema.OneOf) > 0 {
		plan.Kind = FieldPlanUnionDeferred
		plan.Reason = "field-level oneOf needs generated union support"
		return plan, nil
	}
	if len(schema.AnyOf) > 0 {
		return planAnyOfField(plan, schema)
	}
	if nullableType, ok := schema.Type.NullableSingle(); ok {
		return planNullableField(plan, schema, nullableType)
	}
	if schema.Type.Only("array") {
		return planArrayField(plan, schema, false)
	}
	if schema.Type.Only("object") {
		return planObjectField(plan, schema)
	}
	if schema.Type.Only("string") && len(schema.Enum) > 0 {
		plan.Kind = FieldPlanStringEnum
		plan.GoType = optionalGoType(plan.Required, enumGoType(schema))
		plan.Reason = "string enum"
		return plan, nil
	}
	if schema.Type.Only("string") || schema.Type.Only("boolean") || schema.Type.Only("integer") {
		plan.Kind = scalarFieldKind(schema)
		goType, err := scalarGoType(schema, schema.Type.Values[0])
		if err != nil {
			return FieldPlan{}, err
		}
		plan.GoType = optionalGoType(plan.Required, goType)
		plan.Reason = "scalar field"
		return plan, nil
	}
	return FieldPlan{}, fmt.Errorf("field %s has unsupported schema shape", coverage.Path)
}

func arrayCanPlanBeforeRecursiveConstraints(schema *Schema) bool {
	if schema == nil || schema.Items == nil {
		return false
	}
	directConstraints, directUnknown := partitionUnmodeledKeywords(schema.UnknownKeywords)
	if len(directConstraints) > 0 || len(directUnknown) > 0 {
		return false
	}
	if schema.Type.Only("array") {
		return true
	}
	if nullableType, ok := schema.Type.NullableSingle(); ok {
		return nullableType == "array"
	}
	return false
}

func planConstrainedField(plan FieldPlan, schema *Schema, constraints []string) (FieldPlan, bool, error) {
	if schema.Format == "double" && len(constraints) == 1 && constraints[0] == "format" {
		if nullableType, ok := schema.Type.NullableSingle(); ok && nullableType == "number" {
			plan.Kind = FieldPlanNullableScalar
			plan.GoType = nullableGoType(plan.Required, "float64")
			plan.WireAllowsNull = true
			plan.Reason = "nullable double represented with float64"
			return plan, true, nil
		}
		if schema.Type.Only("number") {
			plan.Kind = FieldPlanScalar
			plan.GoType = optionalGoType(plan.Required, "float64")
			plan.Reason = "double represented with float64"
			return plan, true, nil
		}
	}
	if !supportedIntegerConstraints(schema, constraints) {
		return FieldPlan{}, false, nil
	}
	goType, err := integerGoType(schema)
	if err != nil {
		return FieldPlan{}, false, err
	}
	if nullableType, ok := schema.Type.NullableSingle(); ok {
		if nullableType != "integer" {
			return FieldPlan{}, false, nil
		}
		plan.Kind = FieldPlanNullableScalar
		plan.GoType = nullableGoType(plan.Required, goType)
		plan.Minimum = nonZeroMinimum(schema.Minimum)
		plan.WireAllowsNull = true
		plan.Reason = "constrained nullable integer represented with generated numeric type validation"
		return plan, true, nil
	}
	if !schema.Type.Only("integer") {
		return FieldPlan{}, false, nil
	}
	plan.Kind = FieldPlanScalar
	plan.GoType = optionalGoType(plan.Required, goType)
	plan.Minimum = nonZeroMinimum(schema.Minimum)
	plan.Reason = "constrained integer represented with generated numeric type validation"
	return plan, true, nil
}

func nonZeroMinimum(minimum *float64) *float64 {
	if minimum == nil || *minimum == 0 {
		return nil
	}
	return minimum
}

func supportedIntegerConstraints(schema *Schema, constraints []string) bool {
	if schema == nil {
		return false
	}
	integer := schema.Type.Only("integer")
	if nullableType, ok := schema.Type.NullableSingle(); ok {
		integer = nullableType == "integer"
	}
	if !integer {
		return false
	}
	for _, constraint := range constraints {
		switch constraint {
		case "format":
		case "minimum":
			if schema.Minimum == nil || *schema.Minimum != float64(int64(*schema.Minimum)) {
				return false
			}
			if integerFormatIsUnsigned(schema.Format) && *schema.Minimum < 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func overlayFieldPlan(plan FieldPlan, schema *Schema) (FieldPlan, bool, error) {
	switch {
	case plan.Path == "v2/TurnStartParams.json#/properties/outputSchema":
		if !isDescriptionOnlySchema(schema) {
			return FieldPlan{}, true, fmt.Errorf("field %s outputSchema overlay no longer matches description-only schema shape", plan.Path)
		}
		plan.Kind = FieldPlanOutputSchema
		plan.GoType = optionalGoType(plan.Required, "protocolv2.OutputSchema")
		plan.Reason = "turn-level output JSON Schema contract"
		return plan, true, nil
	case plan.Path == "v2/ThreadApproveGuardianDeniedActionParams.json#/properties/event":
		if !isDescriptionOnlySchema(schema) {
			return FieldPlan{}, true, fmt.Errorf("field %s GuardianAssessmentEvent overlay no longer matches description-only JsonValue schema shape", plan.Path)
		}
		plan.Kind = FieldPlanJSONValue
		plan.GoType = optionalGoType(plan.Required, "protocolv2.JSONValue")
		plan.Reason = "reviewed protocol JsonValue carrying serialized GuardianAssessmentEvent"
		return plan, true, nil
	case plan.Path == "v2/GetAccountRateLimitsResponse.json#/properties/rateLimitUpsell":
		if !isDescriptionOnlySchema(schema) {
			return FieldPlan{}, true, fmt.Errorf("field %s rate limit upsell overlay no longer matches description-only JsonValue schema shape", plan.Path)
		}
		plan.Kind = FieldPlanJSONValue
		plan.GoType = optionalGoType(plan.Required, "protocolv2.JSONValue")
		plan.Reason = "reviewed backend-owned rate limit upsell JSON value"
		return plan, true, nil
	case plan.Path == "McpServerElicitationRequestParams.json#/definitions/McpElicitationSchema/properties/properties":
		if !schema.Type.Only("object") || schema.AdditionalProperties.Schema == nil || schema.AdditionalProperties.Schema.Ref != "#/definitions/McpElicitationPrimitiveSchema" {
			return FieldPlan{}, true, fmt.Errorf("field %s elicitation properties overlay no longer matches primitive schema map", plan.Path)
		}
		plan.Kind = FieldPlanJSONValueMap
		plan.GoType = nullableAwareGoType(plan.Required, plan.WireAllowsNull, "map[string]protocolv2.JSONValue")
		plan.Reason = "reviewed MCP elicitation form properties preserved as JSON values"
		return plan, true, nil
	case plan.Path == "McpServerElicitationRequestResponse.json#/properties/_meta" ||
		plan.Path == "McpServerElicitationRequestResponse.json#/properties/content":
		if !isDescriptionOnlySchema(schema) {
			return FieldPlan{}, true, fmt.Errorf("field %s MCP elicitation JSONValue overlay no longer matches description-only schema shape", plan.Path)
		}
		plan.Kind = FieldPlanJSONValue
		plan.GoType = optionalGoType(plan.Required, "protocolv2.JSONValue")
		plan.Reason = "reviewed MCP elicitation dynamic JSON value"
		return plan, true, nil
	case plan.Path == "v2/ThreadRealtimeItemCompletedNotification.json#/properties/item" ||
		plan.Path == "v2/ThreadRealtimeItemStartedNotification.json#/properties/item":
		if schema.Ref != "#/definitions/ThreadRealtimeItem" ||
			len(schema.Type.Values) > 0 ||
			len(schema.Properties) > 0 ||
			len(schema.OneOf) > 0 ||
			len(schema.AnyOf) > 0 ||
			len(schema.AllOf) > 0 ||
			len(schema.Enum) > 0 ||
			schema.Items != nil ||
			schema.AdditionalProperties.Present ||
			len(unmodeledKeywords(schema)) > 0 {
			return FieldPlan{}, true, fmt.Errorf("field %s realtime item JSONValue overlay no longer matches ThreadRealtimeItem ref shape", plan.Path)
		}
		plan.Kind = FieldPlanJSONValue
		plan.GoType = optionalGoType(plan.Required, "protocolv2.JSONValue")
		plan.Reason = "reviewed experimental realtime item preserved as protocol-native JSON"
		return plan, true, nil
	case plan.Path == "v2/CommandExecParams.json#/properties/command":
		if !commandExecCommandSchemaMatchesReviewedMinItems(schema) {
			return FieldPlan{}, true, fmt.Errorf("field %s command overlay no longer matches reviewed non-empty argv schema shape", plan.Path)
		}
		minItems := uint64(1)
		plan.Kind = FieldPlanArrayString
		plan.GoType = optionalGoType(plan.Required, "[]string")
		plan.MinItems = &minItems
		plan.Reason = "reviewed non-empty command argv vector"
		return plan, true, nil
	case isServiceTierPath(plan.Path):
		nullableType, ok := schema.Type.NullableSingle()
		if !ok || nullableType != "string" || len(unmodeledKeywords(schema)) > 0 || hasNonTypeShape(schema) {
			return FieldPlan{}, true, fmt.Errorf("field %s serviceTier overlay no longer matches nullable string schema shape", plan.Path)
		}
		plan.Kind = FieldPlanNullableServiceTier
		plan.GoType = optionalGoType(plan.Required, "protocolv2.Nullable[string]")
		plan.WireAllowsNull = true
		plan.Reason = "reviewed omit/null/value service tier semantics"
		return plan, true, nil
	default:
		return FieldPlan{}, false, nil
	}
}

func commandExecCommandSchemaMatchesReviewedMinItems(schema *Schema) bool {
	if schema == nil || !schema.Type.Only("array") || schema.Items == nil || !schema.Items.Type.Only("string") {
		return false
	}
	if len(unmodeledKeywords(schema.Items)) > 0 || hasNonTypeShape(schema.Items) {
		return false
	}
	keywords := unmodeledKeywords(schema)
	if len(keywords) > 1 || len(keywords) == 1 && keywords[0] != "minItems" {
		return false
	}
	if schema.MinItems != nil {
		return *schema.MinItems == 1
	}
	return strings.Contains(schema.Description, "Empty arrays are rejected.")
}

func planAnyOfField(plan FieldPlan, schema *Schema) (FieldPlan, error) {
	inner, ok := nullableUnionLeaf(schema.AnyOf)
	if !ok {
		plan.Kind = FieldPlanUnionDeferred
		plan.Reason = "field-level anyOf is not a reviewed nullable shape"
		return plan, nil
	}
	plan.WireAllowsNull = true
	switch {
	case inner.Ref != "":
		if scalarAlias, ok := inlineScalarAliasGoType(inner.Ref); ok {
			plan.Kind = FieldPlanNullableScalar
			plan.GoType = nullableGoType(plan.Required, scalarAlias)
			plan.Reason = "nullable scalar alias ref represented with Nullable"
			return plan, nil
		}
		plan.Kind = FieldPlanNullableRef
		plan.GoType = nullableGoType(plan.Required, refTypeName(inner.Ref))
		plan.RefPath = absoluteRefPath(plan.SchemaPath, inner.Ref)
		plan.Reason = "nullable ref represented with Nullable"
		return plan, nil
	case inner.Type.Only("string") || inner.Type.Only("boolean") || inner.Type.Only("integer"):
		plan.Kind = FieldPlanNullableScalar
		goType, err := scalarGoType(inner, inner.Type.Values[0])
		if err != nil {
			return FieldPlan{}, err
		}
		plan.GoType = nullableGoType(plan.Required, goType)
		plan.Reason = "nullable scalar represented with Nullable"
		return plan, nil
	case len(inner.AllOf) == 1 && inner.AllOf[0].Ref != "":
		plan.Kind = FieldPlanNullableRef
		plan.GoType = nullableGoType(plan.Required, refTypeName(inner.AllOf[0].Ref))
		plan.RefPath = absoluteRefPath(plan.SchemaPath, inner.AllOf[0].Ref)
		plan.Reason = "nullable single allOf ref represented with Nullable"
		return plan, nil
	default:
		plan.Kind = FieldPlanUnionDeferred
		plan.Reason = "nullable anyOf inner schema needs reviewed generation policy"
		return plan, nil
	}
}

func planNullableField(plan FieldPlan, schema *Schema, nullableType string) (FieldPlan, error) {
	plan.WireAllowsNull = true
	switch nullableType {
	case "string", "boolean", "integer":
		plan.Kind = FieldPlanNullableScalar
		goType, err := scalarGoType(schema, nullableType)
		if err != nil {
			return FieldPlan{}, err
		}
		plan.GoType = nullableGoType(plan.Required, goType)
		plan.Reason = "nullable scalar represented with Nullable"
		return plan, nil
	case "array":
		return planArrayField(plan, schema, true)
	case "object":
		return planObjectField(plan, schema)
	default:
		return FieldPlan{}, fmt.Errorf("field %s has unsupported nullable type %q", plan.Path, nullableType)
	}
}

func planArrayField(plan FieldPlan, schema *Schema, nullable bool) (FieldPlan, error) {
	if schema.Items == nil {
		return FieldPlan{}, fmt.Errorf("field %s array has no item schema", plan.Path)
	}
	fieldRequired := plan.Required && !nullable
	switch {
	case schema.Items.Ref != "":
		if scalarAlias, ok := inlineScalarAliasGoType(schema.Items.Ref); ok {
			if scalarAlias != "string" {
				return FieldPlan{}, fmt.Errorf("field %s has unsupported array scalar alias item type %s", plan.Path, scalarAlias)
			}
			plan.Kind = FieldPlanArrayString
			plan.GoType = optionalOrNullableGoType(fieldRequired, nullable, "[]string")
			plan.Reason = "array of scalar alias strings"
			return plan, nil
		}
		plan.Kind = FieldPlanArrayRef
		plan.GoType = optionalOrNullableGoType(fieldRequired, nullable, "[]"+refTypeName(schema.Items.Ref))
		plan.RefPath = absoluteRefPath(plan.SchemaPath, schema.Items.Ref)
		plan.Reason = "array of refs"
		return plan, nil
	case schema.Items.Type.Only("string"):
		if len(unmodeledKeywords(schema.Items)) > 0 || hasNonTypeShape(schema.Items) {
			plan.Kind = FieldPlanUnionDeferred
			plan.Reason = "array string item schema needs reviewed generation policy"
			return plan, nil
		}
		plan.Kind = FieldPlanArrayString
		plan.GoType = optionalOrNullableGoType(fieldRequired, nullable, "[]string")
		plan.Reason = "array of strings"
		return plan, nil
	case schema.Items.Type.Only("boolean") || schema.Items.Type.Only("integer"):
		itemType, ok, err := arrayScalarItemGoType(plan.Path, schema.Items)
		if err != nil {
			return FieldPlan{}, err
		}
		if !ok {
			plan.Kind = FieldPlanUnionDeferred
			plan.Reason = "array scalar item schema needs reviewed generation policy"
			return plan, nil
		}
		plan.Kind = FieldPlanArrayScalar
		plan.GoType = optionalOrNullableGoType(fieldRequired, nullable, "[]"+itemType)
		plan.Reason = "array of scalar values"
		return plan, nil
	case schema.Items.IsTrueSchema():
		plan.Kind = FieldPlanArrayJSONValue
		plan.GoType = optionalOrNullableGoType(fieldRequired, nullable, "[]protocolv2.JSONValue")
		plan.Reason = "array of protocol-native JSON values"
		return plan, nil
	default:
		plan.Kind = FieldPlanUnionDeferred
		plan.Reason = "array item schema needs reviewed generation policy"
		return plan, nil
	}
}

func arrayScalarItemGoType(path string, schema *Schema) (string, bool, error) {
	if schema == nil {
		return "", false, fmt.Errorf("field %s array has no item schema", path)
	}
	switch {
	case schema.Type.Only("boolean"):
		if len(unmodeledKeywords(schema)) > 0 || hasNonTypeShape(schema) {
			return "", false, nil
		}
		return "bool", true, nil
	case schema.Type.Only("integer"):
		constraints, unknown := partitionUnmodeledKeywords(unmodeledKeywords(schema))
		if len(unknown) > 0 || hasNonTypeShape(schema) {
			return "", false, nil
		}
		if len(constraints) > 0 && !supportedIntegerConstraints(schema, constraints) {
			return "", false, nil
		}
		if !arrayIntegerItemConstraintsAreRepresentedByGoType(schema, constraints) {
			return "", false, nil
		}
		goType, err := scalarGoType(schema, "integer")
		if err != nil {
			return "", false, err
		}
		return goType, true, nil
	default:
		return "", false, nil
	}
}

func arrayIntegerItemConstraintsAreRepresentedByGoType(schema *Schema, constraints []string) bool {
	for _, constraint := range constraints {
		switch constraint {
		case "format":
			continue
		case "minimum":
			if schema.Minimum == nil {
				return false
			}
			if *schema.Minimum == 0 && integerFormatIsUnsigned(schema.Format) {
				continue
			}
			return false
		default:
			return false
		}
	}
	return true
}

func planObjectField(plan FieldPlan, schema *Schema) (FieldPlan, error) {
	if !schema.AdditionalProperties.Present {
		plan.Kind = FieldPlanUnionDeferred
		plan.Reason = "inline object field needs named generated struct policy"
		return plan, nil
	}
	if schema.AdditionalProperties.Bool != nil {
		if *schema.AdditionalProperties.Bool {
			plan.Kind = FieldPlanJSONValueMap
			plan.GoType = nullableAwareGoType(plan.Required, plan.WireAllowsNull, "map[string]protocolv2.JSONValue")
			plan.Reason = "dynamic protocol map with unconstrained JSON values"
			return plan, nil
		}
		plan.Kind = FieldPlanUnionDeferred
		plan.Reason = "closed inline object field needs named generated struct policy"
		return plan, nil
	}
	valueType, refPath, err := mapValueType(plan.Path, plan.SchemaPath, schema.AdditionalProperties.Schema)
	if err != nil {
		return FieldPlan{}, err
	}
	plan.Kind = FieldPlanTypedMap
	plan.GoType = nullableAwareGoType(plan.Required, plan.WireAllowsNull, "map[string]"+valueType)
	plan.RefPath = refPath
	plan.Reason = "object map with typed additionalProperties"
	return plan, nil
}

func mapValueType(path string, schemaPath string, schema *Schema) (string, string, error) {
	switch {
	case schema == nil:
		return "", "", fmt.Errorf("field %s additionalProperties has no schema", path)
	case schema.Ref != "":
		return refTypeName(schema.Ref), absoluteRefPath(schemaPath, schema.Ref), nil
	case schema.Type.Only("string"):
		return "string", "", nil
	case schema.Type.Only("boolean"):
		return "bool", "", nil
	case schema.Type.Only("integer"):
		goType, err := scalarGoType(schema, "integer")
		return goType, "", err
	case schema.Type.Only("array") && plainStringArraySchema(schema):
		return "[]string", "", nil
	case schema.Type.Has("null"):
		nonNull, ok := schema.Type.NullableSingle()
		if !ok {
			return "", "", fmt.Errorf("field %s has unsupported nullable additionalProperties schema", path)
		}
		switch nonNull {
		case "string", "boolean", "integer":
			goType, err := scalarGoType(schema, nonNull)
			if err != nil {
				return "", "", err
			}
			return "*protocolv2.Nullable[" + goType + "]", "", nil
		default:
			return "", "", fmt.Errorf("field %s has unsupported nullable additionalProperties type %q", path, nonNull)
		}
	case schema.IsTrueSchema():
		return "protocolv2.JSONValue", "", nil
	default:
		return "", "", fmt.Errorf("field %s has unsupported additionalProperties schema", path)
	}
}

func plainStringArraySchema(schema *Schema) bool {
	return schema != nil &&
		schema.Items != nil &&
		schema.Items.Type.Only("string") &&
		len(unmodeledKeywords(schema)) == 0 &&
		!schema.Default.Present &&
		!schema.Items.Default.Present &&
		schema.Ref == "" &&
		len(schema.Properties) == 0 &&
		len(schema.OneOf) == 0 &&
		len(schema.AnyOf) == 0 &&
		len(schema.AllOf) == 0 &&
		len(schema.Enum) == 0 &&
		len(schema.Definitions) == 0 &&
		len(schema.Required) == 0 &&
		!schema.AdditionalProperties.Present &&
		!hasNonTypeShape(schema.Items)
}

func nullableUnionInner(variants []*Schema) (*Schema, bool) {
	if len(variants) != 2 {
		return nil, false
	}
	var inner *Schema
	var nulls int
	for _, variant := range variants {
		if variant.Type.Only("null") {
			nulls++
			continue
		}
		inner = variant
	}
	return inner, nulls == 1 && inner != nil
}

func nullableUnionLeaf(variants []*Schema) (*Schema, bool) {
	inner, ok := nullableUnionInner(variants)
	if !ok {
		return nil, false
	}
	for isPureAnyOfWrapper(inner) {
		next, ok := nullableUnionInner(inner.AnyOf)
		if !ok {
			break
		}
		inner = next
	}
	return inner, true
}

func isPureAnyOfWrapper(schema *Schema) bool {
	return schema != nil &&
		len(schema.AnyOf) > 0 &&
		len(unmodeledKeywords(schema)) == 0 &&
		schema.Ref == "" &&
		len(schema.Type.Values) == 0 &&
		len(schema.Enum) == 0 &&
		len(schema.Required) == 0 &&
		len(schema.Properties) == 0 &&
		len(schema.OneOf) == 0 &&
		len(schema.AllOf) == 0 &&
		len(schema.Definitions) == 0 &&
		schema.Items == nil &&
		!schema.Default.Present &&
		!schema.AdditionalProperties.Present
}

func schemaAllowsNull(schema *Schema) bool {
	if schema == nil {
		return false
	}
	if schema.Type.Has("null") {
		return true
	}
	if _, ok := nullableUnionInner(schema.AnyOf); ok {
		return true
	}
	return false
}

func hasStructuralShape(schema *Schema) bool {
	return schema.Ref != "" ||
		len(schema.Type.Values) > 0 ||
		len(schema.Properties) > 0 ||
		len(schema.OneOf) > 0 ||
		len(schema.AnyOf) > 0 ||
		len(schema.AllOf) > 0 ||
		schema.Items != nil ||
		schema.AdditionalProperties.Present
}

func hasNonTypeShape(schema *Schema) bool {
	return schema.Ref != "" ||
		len(schema.Properties) > 0 ||
		len(schema.OneOf) > 0 ||
		len(schema.AnyOf) > 0 ||
		len(schema.AllOf) > 0 ||
		len(schema.Enum) > 0 ||
		schema.Items != nil ||
		schema.AdditionalProperties.Present
}

func isDescriptionOnlySchema(schema *Schema) bool {
	return schema != nil &&
		schema.Description != "" &&
		!schema.IsTrueSchema() &&
		!schema.IsFalseSchema() &&
		!hasStructuralShape(schema) &&
		len(unmodeledKeywords(schema)) == 0
}

func partitionUnmodeledKeywords(keywords []string) (constraints []string, unknown []string) {
	for _, keyword := range keywords {
		if isRecognizedConstraintKeyword(keyword) {
			constraints = append(constraints, keyword)
		} else {
			unknown = append(unknown, keyword)
		}
	}
	return constraints, unknown
}

func isRecognizedConstraintKeyword(keyword string) bool {
	switch keyword {
	case "const", "enumNames", "format", "maximum", "maxItems", "maxLength", "minimum", "minItems", "minLength", "pattern", "writeOnly":
		return true
	default:
		return false
	}
}

func scalarFieldKind(schema *Schema) FieldPlanKind {
	if schema.Type.Only("boolean") {
		return FieldPlanBool
	}
	return FieldPlanScalar
}

func scalarGoType(schema *Schema, schemaType string) (string, error) {
	switch schemaType {
	case "boolean":
		return "bool", nil
	case "integer":
		return integerGoType(schema)
	case "string":
		return "string", nil
	default:
		return "", fmt.Errorf("unsupported scalar schema type %q", schemaType)
	}
}

// inlineScalarAliasGoType is an explicit public-representation overlay for upstream string aliases that remain plain Go strings.
func inlineScalarAliasGoType(ref string) (string, bool) {
	switch refTypeName(ref) {
	case "AgentPath":
		return "string", true
	case "ApiPathString":
		return "string", true
	case "LegacyAppPathString":
		return "string", true
	case "AbsolutePathBuf":
		return "string", true
	case "ThreadId":
		return "string", true
	default:
		return "", false
	}
}

func integerGoType(schema *Schema) (string, error) {
	if schema == nil {
		return "", fmt.Errorf("integer schema is nil")
	}
	switch schema.Format {
	case "", "int64":
		return "int64", nil
	case "int32":
		return "int32", nil
	case "uint", "uint64":
		return "uint64", nil
	case "uint16":
		return "uint16", nil
	case "uint32":
		return "uint32", nil
	default:
		return "", fmt.Errorf("unsupported integer format %q", schema.Format)
	}
}

func integerFormatIsUnsigned(format string) bool {
	switch format {
	case "uint", "uint16", "uint32", "uint64":
		return true
	default:
		return false
	}
}

func enumGoType(schema *Schema) string {
	if schema.Title != "" {
		return schema.Title
	}
	return "string"
}

func optionalGoType(required bool, typ string) string {
	if required {
		return typ
	}
	return "*" + typ
}

func optionalOrNullableGoType(required bool, nullable bool, typ string) string {
	if nullable {
		return nullableGoType(required, typ)
	}
	return optionalGoType(required, typ)
}

func nullableAwareGoType(required bool, nullable bool, typ string) string {
	if nullable {
		return nullableGoType(required, typ)
	}
	return optionalGoType(required, typ)
}

func nullableGoType(required bool, typ string) string {
	nullable := "protocolv2.Nullable[" + typ + "]"
	if required {
		return nullable
	}
	return "*" + nullable
}

func isAggregateBundle(path string) bool {
	return path == "codex_app_server_protocol.schemas.json" || path == "codex_app_server_protocol.v2.schemas.json"
}

func isServiceTierPath(path string) bool {
	switch path {
	case "v2/ThreadForkParams.json#/properties/serviceTier",
		"v2/ThreadForkResponse.json#/properties/serviceTier",
		"v2/ThreadResumeParams.json#/properties/serviceTier",
		"v2/ThreadResumeResponse.json#/properties/serviceTier",
		"v2/ThreadStartParams.json#/properties/serviceTier",
		"v2/ThreadStartResponse.json#/properties/serviceTier",
		"v2/TurnStartParams.json#/properties/serviceTier":
		return true
	default:
		return false
	}
}

func CountTypePlanKinds(types []TypePlan) map[TypePlanKind]int {
	counts := map[TypePlanKind]int{}
	for _, typ := range types {
		counts[typ.Kind]++
	}
	return counts
}

func CountFieldPlanKinds(fields []FieldPlan) map[FieldPlanKind]int {
	counts := map[FieldPlanKind]int{}
	for _, field := range fields {
		counts[field.Kind]++
	}
	return counts
}

func (f FieldPlan) IsDeferred() bool {
	return strings.Contains(string(f.Kind), "deferred")
}
