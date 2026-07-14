package protocolgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func schemaRoot() string {
	return filepath.Join("..", "protocolschema", "appserver", "v2")
}

func TestBuildProtocolTypePlanClassifiesBaseline(t *testing.T) {
	plan, err := BuildProtocolTypePlan(schemaRoot())
	if err != nil {
		t.Fatal(err)
	}
	matrix, err := LoadCoverageMatrix(filepath.Join(schemaRoot(), "coverage_matrix.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(plan.Types), len(matrix.Types); got != want {
		t.Fatalf("type count = %d, want coverage matrix type count %d", got, want)
	}
	if got, want := len(plan.Fields), len(matrix.Fields); got != want {
		t.Fatalf("field count = %d, want coverage matrix field count %d", got, want)
	}
	if got, ok := plan.TypeBySchema("codex_app_server_protocol.v2.schemas.json"); !ok || got.Kind != TypePlanAggregateBundle {
		t.Fatalf("aggregate v2 schema kind = %v, ok=%v; want %s", got.Kind, ok, TypePlanAggregateBundle)
	}
	if got, ok := plan.TypeBySchema("ClientRequest.json"); !ok || got.Kind != TypePlanTaggedUnionCandidate {
		t.Fatalf("ClientRequest kind = %v, ok=%v; want %s", got.Kind, ok, TypePlanTaggedUnionCandidate)
	}
	if got, ok := plan.TypeBySchema("RequestId.json"); !ok || got.Kind != TypePlanScalarUnionCandidate {
		t.Fatalf("RequestId kind = %v, ok=%v; want %s", got.Kind, ok, TypePlanScalarUnionCandidate)
	}
}

func TestBuildProtocolTypePlanAppliesReviewedOverlays(t *testing.T) {
	plan, err := BuildProtocolTypePlan(schemaRoot())
	if err != nil {
		t.Fatal(err)
	}

	if outputSchema, ok := optionalField(plan, "v2/TurnStartParams.json#/properties/outputSchema"); ok {
		if outputSchema.Kind != FieldPlanOutputSchema {
			t.Fatalf("outputSchema kind = %s, want %s", outputSchema.Kind, FieldPlanOutputSchema)
		}
		if outputSchema.GoType != "*protocolv2.OutputSchema" {
			t.Fatalf("outputSchema GoType = %q, want *protocolv2.OutputSchema", outputSchema.GoType)
		}
		if outputSchema.WireAllowsNull {
			t.Fatal("outputSchema must be omit/value, not null/value")
		}
	}

	serviceTierPaths := []string{
		"v2/ThreadForkParams.json#/properties/serviceTier",
		"v2/ThreadForkResponse.json#/properties/serviceTier",
		"v2/ThreadResumeParams.json#/properties/serviceTier",
		"v2/ThreadResumeResponse.json#/properties/serviceTier",
		"v2/ThreadStartParams.json#/properties/serviceTier",
		"v2/ThreadStartResponse.json#/properties/serviceTier",
		"v2/TurnStartParams.json#/properties/serviceTier",
	}
	for _, path := range serviceTierPaths {
		field, ok := optionalField(plan, path)
		if !ok {
			continue
		}
		if field.Kind != FieldPlanNullableServiceTier {
			t.Fatalf("%s kind = %s, want %s", path, field.Kind, FieldPlanNullableServiceTier)
		}
		if field.GoType != "*protocolv2.Nullable[string]" {
			t.Fatalf("%s GoType = %q, want *protocolv2.Nullable[string]", path, field.GoType)
		}
		if !field.WireAllowsNull || !field.WireOmitAllowed {
			t.Fatalf("%s must preserve omit/null/value semantics", path)
		}
	}

	for _, path := range []string{
		"v2/ThreadForkParams.json#/properties/config",
		"v2/ThreadResumeParams.json#/properties/config",
		"v2/ThreadStartParams.json#/properties/config",
	} {
		field, ok := optionalField(plan, path)
		if !ok {
			continue
		}
		if field.Kind != FieldPlanJSONValueMap {
			t.Fatalf("%s kind = %s, want %s", path, field.Kind, FieldPlanJSONValueMap)
		}
		if field.GoType != "*protocolv2.Nullable[map[string]protocolv2.JSONValue]" {
			t.Fatalf("%s GoType = %q, want *protocolv2.Nullable[map[string]protocolv2.JSONValue]", path, field.GoType)
		}
	}

	for _, path := range []string{
		"v2/McpServerToolCallResponse.json#/properties/content",
		"v2/ThreadInjectItemsParams.json#/properties/items",
	} {
		field, ok := optionalField(plan, path)
		if !ok {
			continue
		}
		if field.Kind != FieldPlanArrayJSONValue {
			t.Fatalf("%s kind = %s, want %s", path, field.Kind, FieldPlanArrayJSONValue)
		}
		if field.GoType != "[]protocolv2.JSONValue" {
			t.Fatalf("%s GoType = %q, want []protocolv2.JSONValue", path, field.GoType)
		}
	}

	if conversationID, ok := optionalField(plan, "ApplyPatchApprovalParams.json#/properties/conversationId"); ok {
		if conversationID.Kind != FieldPlanScalar || conversationID.GoType != "string" {
			t.Fatalf("ThreadId scalar alias field = kind %s GoType %q, want scalar string", conversationID.Kind, conversationID.GoType)
		}
	}

	if fsCopyDestination, ok := optionalField(plan, "v2/FsCopyParams.json#/properties/destinationPath"); ok {
		if fsCopyDestination.Kind != FieldPlanScalar || fsCopyDestination.GoType != "string" {
			t.Fatalf("AbsolutePathBuf allOf scalar alias field = kind %s GoType %q, want scalar string", fsCopyDestination.Kind, fsCopyDestination.GoType)
		}
	}
}

func TestBuildProtocolTypePlanClassifiesDynamicJSONFields(t *testing.T) {
	plan, err := BuildProtocolTypePlan(schemaRoot())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"DynamicToolCallParams.json#/properties/arguments",
		"JSONRPCErrorError.json#/properties/data",
		"JSONRPCNotification.json#/properties/params",
		"JSONRPCRequest.json#/properties/params",
		"JSONRPCResponse.json#/properties/result",
		"McpServerElicitationRequestResponse.json#/properties/_meta",
		"McpServerElicitationRequestResponse.json#/properties/content",
		"v2/ConfigValueWriteParams.json#/properties/value",
		"v2/McpServerToolCallParams.json#/properties/_meta",
		"v2/McpServerToolCallParams.json#/properties/arguments",
		"v2/McpServerToolCallResponse.json#/properties/_meta",
		"v2/McpServerToolCallResponse.json#/properties/structuredContent",
		"v2/ThreadApproveGuardianDeniedActionParams.json#/properties/event",
		"v2/ThreadRealtimeItemAddedNotification.json#/properties/item",
		"v2/TurnModerationMetadataNotification.json#/properties/metadata",
	} {
		field, ok := optionalField(plan, path)
		if !ok {
			continue
		}
		if field.Kind != FieldPlanJSONValue {
			t.Fatalf("%s kind = %s, want %s", path, field.Kind, FieldPlanJSONValue)
		}
		if !strings.Contains(field.GoType, "protocolv2.JSONValue") {
			t.Fatalf("%s GoType = %q, want protocolv2.JSONValue", path, field.GoType)
		}
	}
}

func TestBuildProtocolTypePlanSupportsConstrainedIntegerScalars(t *testing.T) {
	plan, err := BuildProtocolTypePlan(schemaRoot())
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		path   string
		kind   FieldPlanKind
		goType string
	}{
		{
			path:   "v2/ThreadRollbackParams.json#/properties/numTurns",
			kind:   FieldPlanScalar,
			goType: "uint32",
		},
		{
			path:   "v2/AppsListParams.json#/properties/limit",
			kind:   FieldPlanNullableScalar,
			goType: "*protocolv2.Nullable[uint32]",
		},
		{
			path:   "v2/ProcessExitedNotification.json#/properties/exitCode",
			kind:   FieldPlanScalar,
			goType: "int32",
		},
		{
			path:   "v2/ThreadIncrementElicitationResponse.json#/properties/count",
			kind:   FieldPlanScalar,
			goType: "int64",
		},
		{
			path:   "FileChangeRequestApprovalParams.json#/properties/startedAtMs",
			kind:   FieldPlanScalar,
			goType: "int64",
		},
		{
			path:   "CommandExecutionRequestApprovalParams.json#/properties/startedAtMs",
			kind:   FieldPlanScalar,
			goType: "int64",
		},
	} {
		field, ok := optionalField(plan, tt.path)
		if !ok {
			continue
		}
		if field.Kind != tt.kind {
			t.Fatalf("%s kind = %s, want %s", tt.path, field.Kind, tt.kind)
		}
		if field.GoType != tt.goType {
			t.Fatalf("%s GoType = %q, want %q", tt.path, field.GoType, tt.goType)
		}
		if !strings.Contains(field.Reason, "constrained") {
			t.Fatalf("%s reason %q does not describe constrained support", tt.path, field.Reason)
		}
	}
}

func TestBuildProtocolTypePlanPreservesNullableTypedMapValues(t *testing.T) {
	plan, err := BuildProtocolTypePlan(schemaRoot())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"v2/CommandExecParams.json#/properties/env",
		"v2/ProcessSpawnParams.json#/properties/env",
	} {
		env, ok := optionalField(plan, path)
		if !ok {
			continue
		}
		if env.Kind != FieldPlanTypedMap {
			t.Fatalf("%s kind = %s, want %s", path, env.Kind, FieldPlanTypedMap)
		}
		if env.GoType != "*protocolv2.Nullable[map[string]*protocolv2.Nullable[string]]" {
			t.Fatalf("%s GoType = %q, want *protocolv2.Nullable[map[string]*protocolv2.Nullable[string]]", path, env.GoType)
		}
		if !env.WireAllowsNull || !env.WireOmitAllowed {
			t.Fatalf("%s must preserve omit/null/value semantics", path)
		}
	}
}

func TestPlanFieldSupportsUint16ConstrainedInteger(t *testing.T) {
	coverage := CoverageField{
		Field:     "cols",
		Path:      "Example.json#/properties/cols",
		Required:  true,
		Schema:    "Example.json",
		Stability: "stable",
		Status:    "deferred",
		Type:      "Example",
	}
	minimum := 0.0
	field, err := planField(coverage, &Schema{
		Format:  "uint16",
		Minimum: &minimum,
		Type:    SchemaTypeSet{Values: []string{"integer"}},
		UnknownKeywords: []string{
			"format",
			"minimum",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if field.Kind != FieldPlanScalar {
		t.Fatalf("uint16 constrained integer kind = %s, want %s", field.Kind, FieldPlanScalar)
	}
	if field.GoType != "uint16" {
		t.Fatalf("uint16 constrained integer GoType = %q, want uint16", field.GoType)
	}
	if field.Minimum != nil {
		t.Fatalf("uint16 zero minimum should be represented by Go type, got %#v", field.Minimum)
	}
}

func TestConstrainedIntegerMinimumAboveZeroIsSupported(t *testing.T) {
	coverage := CoverageField{
		Field:     "count",
		Path:      "Example.json#/properties/count",
		Required:  true,
		Schema:    "Example.json",
		Stability: "stable",
		Status:    "deferred",
		Type:      "Example",
	}
	minimum := 1.0
	field, err := planField(coverage, &Schema{
		Format:  "uint32",
		Minimum: &minimum,
		Type:    SchemaTypeSet{Values: []string{"integer"}},
		UnknownKeywords: []string{
			"format",
			"minimum",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if field.Kind != FieldPlanScalar {
		t.Fatalf("minimum=1 constrained integer kind = %s, want %s", field.Kind, FieldPlanScalar)
	}
	if field.Minimum == nil || *field.Minimum != 1 {
		t.Fatalf("minimum=1 field minimum = %#v", field.Minimum)
	}
	if !strings.Contains(field.Reason, "constrained") {
		t.Fatalf("minimum=1 reason %q does not describe constrained support", field.Reason)
	}
}

func TestArrayPlannerDefersConstrainedStringItems(t *testing.T) {
	coverage := CoverageField{
		Field:     "paths",
		Path:      "Example.json#/properties/paths",
		Required:  true,
		Schema:    "Example.json",
		Stability: "stable",
		Status:    "deferred",
		Type:      "Example",
	}
	field, err := planField(coverage, &Schema{
		Items: &Schema{
			Type:            SchemaTypeSet{Values: []string{"string"}},
			UnknownKeywords: []string{"pattern"},
		},
		Type: SchemaTypeSet{Values: []string{"array"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if field.Kind != FieldPlanUnionDeferred {
		t.Fatalf("constrained string item array kind = %s, want %s", field.Kind, FieldPlanUnionDeferred)
	}
	if !strings.Contains(field.Reason, "item schema needs reviewed generation policy") {
		t.Fatalf("constrained string item array reason = %q", field.Reason)
	}
}

func TestArrayPlannerOnlyAcceptsRedundantIntegerItemMinimum(t *testing.T) {
	coverage := CoverageField{
		Field:     "indices",
		Path:      "Example.json#/properties/indices",
		Required:  true,
		Schema:    "Example.json",
		Stability: "stable",
		Status:    "deferred",
		Type:      "Example",
	}
	zero := 0.0
	one := 1.0
	cases := []struct {
		name     string
		format   string
		minimum  *float64
		wantKind FieldPlanKind
		wantType string
	}{
		{
			name:     "unsigned zero minimum",
			format:   "uint32",
			minimum:  &zero,
			wantKind: FieldPlanArrayScalar,
			wantType: "[]uint32",
		},
		{
			name:     "unsigned nonzero minimum",
			format:   "uint32",
			minimum:  &one,
			wantKind: FieldPlanUnionDeferred,
		},
		{
			name:     "signed zero minimum",
			format:   "int64",
			minimum:  &zero,
			wantKind: FieldPlanUnionDeferred,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			field, err := planField(coverage, &Schema{
				Items: &Schema{
					Format:          tc.format,
					Minimum:         tc.minimum,
					Type:            SchemaTypeSet{Values: []string{"integer"}},
					UnknownKeywords: []string{"format", "minimum"},
				},
				Type: SchemaTypeSet{Values: []string{"array"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if field.Kind != tc.wantKind {
				t.Fatalf("array integer item kind = %s, want %s", field.Kind, tc.wantKind)
			}
			if tc.wantType != "" && field.GoType != tc.wantType {
				t.Fatalf("array integer item GoType = %q, want %q", field.GoType, tc.wantType)
			}
		})
	}
}

func TestBuildProtocolTypePlanNormalizesCommandApprovalNullableRefs(t *testing.T) {
	plan, err := BuildProtocolTypePlan(schemaRoot())
	if err != nil {
		t.Fatal(err)
	}
	if field, ok := optionalField(plan, "CommandExecutionRequestApprovalParams.json#/properties/additionalPermissions"); ok {
		if field.Kind != FieldPlanNullableRef || field.GoType != "*protocolv2.Nullable[AdditionalPermissionProfile]" {
			t.Fatalf("additionalPermissions = kind %s GoType %q", field.Kind, field.GoType)
		}
	}
	if field, ok := optionalField(plan, "CommandExecutionRequestApprovalParams.json#/properties/cwd"); ok {
		if field.Kind != FieldPlanNullableScalar || field.GoType != "*protocolv2.Nullable[string]" {
			t.Fatalf("cwd = kind %s GoType %q", field.Kind, field.GoType)
		}
	}
}

func TestBuildProtocolTypePlanNormalizesCommandExecReviewedRefs(t *testing.T) {
	plan, err := BuildProtocolTypePlan(schemaRoot())
	if err != nil {
		t.Fatal(err)
	}
	if command, ok := optionalField(plan, "v2/CommandExecParams.json#/properties/command"); ok {
		if command.Kind != FieldPlanArrayString || command.GoType != "[]string" {
			t.Fatalf("command = kind %s GoType %q, want array string []string", command.Kind, command.GoType)
		}
		if command.MinItems == nil || *command.MinItems != 1 {
			t.Fatalf("command MinItems = %#v, want 1", command.MinItems)
		}
	}
	for _, tt := range []struct {
		path   string
		kind   FieldPlanKind
		goType string
	}{
		{
			path:   "v2/CommandExecParams.json#/properties/permissionProfile",
			kind:   FieldPlanNullableScalar,
			goType: "*protocolv2.Nullable[string]",
		},
		{
			path:   "v2/CommandExecParams.json#/properties/processId",
			kind:   FieldPlanNullableScalar,
			goType: "*protocolv2.Nullable[string]",
		},
		{
			path:   "v2/CommandExecParams.json#/properties/sandboxPolicy",
			kind:   FieldPlanNullableRef,
			goType: "*protocolv2.Nullable[SandboxPolicy]",
		},
		{
			path:   "v2/CommandExecParams.json#/properties/size",
			kind:   FieldPlanNullableRef,
			goType: "*protocolv2.Nullable[CommandExecTerminalSize]",
		},
		{
			path:   "v2/CommandExecResizeParams.json#/properties/size",
			kind:   FieldPlanAllOfRef,
			goType: "CommandExecTerminalSize",
		},
	} {
		field, ok := optionalField(plan, tt.path)
		if !ok {
			continue
		}
		if field.Kind != tt.kind || field.GoType != tt.goType {
			t.Fatalf("%s = kind %s GoType %q, want kind %s GoType %q", tt.path, field.Kind, field.GoType, tt.kind, tt.goType)
		}
	}
}

func TestPlanFieldNormalizesNestedNullableAnyOf(t *testing.T) {
	coverage := CoverageField{
		Field:     "reasoning_effort",
		Path:      "Example.json#/properties/reasoning_effort",
		Required:  false,
		Schema:    "Example.json",
		Stability: "experimental",
		Status:    "deferred",
		Type:      "Example",
	}
	field, err := planField(coverage, &Schema{
		AnyOf: []*Schema{{
			AnyOf: []*Schema{{
				Ref: "#/definitions/ReasoningEffort",
			}, {
				Type: SchemaTypeSet{Values: []string{"null"}},
			}},
		}, {
			Type: SchemaTypeSet{Values: []string{"null"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if field.Kind != FieldPlanNullableRef {
		t.Fatalf("nested nullable anyOf kind = %s, want %s", field.Kind, FieldPlanNullableRef)
	}
	if field.GoType != "*protocolv2.Nullable[ReasoningEffort]" {
		t.Fatalf("nested nullable anyOf GoType = %q, want *protocolv2.Nullable[ReasoningEffort]", field.GoType)
	}
	if !field.WireAllowsNull || !field.WireOmitAllowed {
		t.Fatal("nested nullable anyOf must preserve omit/null/value semantics")
	}
}

func TestPlanFieldClassifiesNullableRefFixture(t *testing.T) {
	coverage := CoverageField{
		Field:     "profile",
		Path:      "Example.json#/properties/profile",
		Required:  false,
		Schema:    "Example.json",
		Stability: "stable",
		Status:    "supported-generated",
		Type:      "Example",
	}
	field, err := planField(coverage, &Schema{
		AnyOf: []*Schema{{
			Ref: "#/definitions/Profile",
		}, {
			Type: SchemaTypeSet{Values: []string{"null"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if field.Kind != FieldPlanNullableRef {
		t.Fatalf("nullable ref fixture kind = %s, want %s", field.Kind, FieldPlanNullableRef)
	}
	if field.GoType != "*protocolv2.Nullable[Profile]" {
		t.Fatalf("nullable ref fixture GoType = %q, want *protocolv2.Nullable[Profile]", field.GoType)
	}
	if !field.WireAllowsNull || !field.WireOmitAllowed {
		t.Fatal("nullable ref fixture must preserve omit/null/value semantics")
	}
}

func TestProtocolTypePlanDoesNotPlanRawPublicPassthrough(t *testing.T) {
	plan, err := BuildProtocolTypePlan(schemaRoot())
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range plan.Fields {
		for _, forbidden := range []string{"json.RawMessage", "map[string]any", "interface{}", "UnknownFields", "AdditionalFields", "Extra"} {
			if strings.Contains(field.GoType, forbidden) {
				t.Fatalf("field %s plans forbidden passthrough type %q", field.Path, field.GoType)
			}
		}
	}
}

func TestProtocolTypePlanFailsClosedForOverlayShapeDrift(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "v2"), 0o700); err != nil {
		t.Fatal(err)
	}
	coverage := `{
		"status": "classified-manifest",
		"types": [{
			"schema": "v2/TurnStartParams.json",
			"stability": "stable",
			"status": "deferred",
			"type": "TurnStartParams"
		}],
		"fields": [{
			"field": "serviceTier",
			"path": "v2/TurnStartParams.json#/properties/serviceTier",
			"required": false,
			"schema": "v2/TurnStartParams.json",
			"stability": "stable",
			"status": "deferred",
			"type": "TurnStartParams"
		}]
	}`
	if err := os.WriteFile(filepath.Join(root, "coverage_matrix.json"), []byte(coverage), 0o600); err != nil {
		t.Fatal(err)
	}
	schema := `{
		"title": "TurnStartParams",
		"type": "object",
		"properties": {
			"serviceTier": {"type": ["integer", "null"]}
		}
	}`
	if err := os.WriteFile(filepath.Join(root, "v2", "TurnStartParams.json"), []byte(schema), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := BuildProtocolTypePlan(root)
	if err == nil {
		t.Fatal("BuildProtocolTypePlan accepted drifted serviceTier overlay shape")
	}
	if !strings.Contains(err.Error(), "serviceTier overlay") {
		t.Fatalf("error %q does not name serviceTier overlay", err)
	}
}

func TestProtocolTypePlanFailsClosedForRequestIdScalarUnionDrift(t *testing.T) {
	cases := map[string]string{
		"missing integer branch": `{
			"title": "RequestId",
			"anyOf": [{"type": "string"}]
		}`,
		"wrong integer format": `{
			"title": "RequestId",
			"anyOf": [
				{"type": "string"},
				{"type": "integer", "format": "int32"}
			]
		}`,
		"added null branch": `{
			"title": "RequestId",
			"anyOf": [
				{"type": "string"},
				{"type": "integer", "format": "int64"},
				{"type": "null"}
			]
		}`,
		"duplicate scalar kind": `{
			"title": "RequestId",
			"anyOf": [
				{"type": "string"},
				{"type": "string"}
			]
		}`,
	}

	for name, schema := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			coverage := `{
				"status": "classified-manifest",
				"types": [{
					"schema": "RequestId.json",
					"stability": "stable",
					"status": "deferred",
					"type": "RequestId"
				}],
				"fields": []
			}`
			if err := os.WriteFile(filepath.Join(root, "coverage_matrix.json"), []byte(coverage), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "RequestId.json"), []byte(schema), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := BuildProtocolTypePlan(root)
			if err == nil {
				t.Fatal("BuildProtocolTypePlan accepted drifted RequestId scalar union")
			}
			if !strings.Contains(err.Error(), "RequestId.json") {
				t.Fatalf("error %q does not include RequestId.json", err)
			}
		})
	}
}

func TestProtocolTypePlanFailsClosedForUnreviewedShape(t *testing.T) {
	root := t.TempDir()
	coverage := `{
		"status": "classified-manifest",
		"types": [{
			"schema": "Example.json",
			"stability": "stable",
			"status": "deferred",
			"type": "Example"
		}],
		"fields": [{
			"field": "value",
			"path": "Example.json#/properties/value",
			"required": true,
			"schema": "Example.json",
			"stability": "stable",
			"status": "deferred",
			"type": "Example"
		}]
	}`
	if err := os.WriteFile(filepath.Join(root, "coverage_matrix.json"), []byte(coverage), 0o600); err != nil {
		t.Fatal(err)
	}
	schema := `{
		"title": "Example",
		"type": "object",
		"required": ["value"],
		"properties": {
			"value": {
				"not": {"type": "string"}
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(root, "Example.json"), []byte(schema), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := BuildProtocolTypePlan(root)
	if err == nil {
		t.Fatal("BuildProtocolTypePlan accepted unreviewed schema shape")
	}
	if !strings.Contains(err.Error(), "Example.json#/properties/value") {
		t.Fatalf("error %q does not include field path", err)
	}
}

func TestScalarAliasRefGoTypeRecognizesLegacyAppPathString(t *testing.T) {
	goType, ok := scalarAliasRefGoType("#/definitions/LegacyAppPathString")
	if !ok || goType != "string" {
		t.Fatalf("LegacyAppPathString alias = (%q, %v), want (string, true)", goType, ok)
	}
}

func mustField(t *testing.T, plan ProtocolTypePlan, path string) FieldPlan {
	t.Helper()
	field, ok := plan.FieldByPath(path)
	if !ok {
		t.Fatalf("missing field plan for %s", path)
	}
	return field
}

func optionalField(plan ProtocolTypePlan, path string) (FieldPlan, bool) {
	return plan.FieldByPath(path)
}
