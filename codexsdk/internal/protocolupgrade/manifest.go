package protocolupgrade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var familyAccessors = map[string]string{
	"account":             "Accounts()",
	"app":                 "Apps()",
	"applyPatchApproval":  "ServerRequests()",
	"collaborationMode":   "CollaborationModes()",
	"command":             "Commands()",
	"config":              "Config()",
	"configRequirements":  "ConfigRequirements()",
	"configWarning":       "ServerNotifications()",
	"deprecationNotice":   "ServerNotifications()",
	"error":               "ServerNotifications()",
	"execCommandApproval": "ServerRequests()",
	"experimentalFeature": "ExperimentalFeatures()",
	"externalAgentConfig": "ExternalAgentConfigs()",
	"feedback":            "Feedback()",
	"fs":                  "FS()",
	"fuzzyFileSearch":     "FuzzyFileSearch()",
	"guardianWarning":     "ServerNotifications()",
	"hook":                "Hooks()",
	"hooks":               "Hooks()",
	"initialize":          "Initialize()",
	"initialized":         "ClientNotifications()",
	"marketplace":         "Marketplace()",
	"mcpServer":           "MCPServers()",
	"mcpServerStatus":     "MCPServerStatus()",
	"memory":              "Memory()",
	"mock":                "Mock()",
	"model":               "Models()",
	"modelProvider":       "ModelProviders()",
	"plugin":              "Plugins()",
	"process":             "Processes()",
	"remoteControl":       "RemoteControl()",
	"review":              "Reviews()",
	"serverRequest":       "ServerRequests()",
	"skills":              "Skills()",
	"thread":              "Threads()",
	"turn":                "Turns()",
	"warning":             "ServerNotifications()",
	"windows":             "Windows()",
	"windowsSandbox":      "WindowsSandbox()",
	"attestation":         "ServerRequests()",
	"environment":         "Environments()",
	"permissionProfile":   "PermissionProfiles()",
}

var rootOperationOverrides = map[string]string{
	"applyPatchApproval":  "ApplyPatchApproval",
	"configWarning":       "ConfigWarning",
	"deprecationNotice":   "DeprecationNotice",
	"error":               "Error",
	"execCommandApproval": "ExecCommandApproval",
	"fuzzyFileSearch":     "Search",
	"guardianWarning":     "GuardianWarning",
	"initialize":          "Initialize",
	"initialized":         "Initialized",
	"warning":             "Warning",
}

var rootInternalOverrides = map[string]string{
	"initialize":  "internal.InitializeHandshake.Request",
	"initialized": "internal.InitializeHandshake.InitializedNotification",
}

var facadeTargetRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*\(\)\.[A-Za-z][A-Za-z0-9]*$`)
var tokenRE = regexp.MustCompile(`[A-Za-z0-9]+`)

type manifestFile struct {
	AggregateSchemas      []string         `json:"aggregate_schemas"`
	ClassificationSources map[string]any   `json:"classification_sources,omitempty"`
	Description           string           `json:"description"`
	Entries               []manifestEntry  `json:"entries"`
	Surface               []map[string]any `json:"surface"`
	SchemaVersion         int              `json:"schema_version"`
	Status                string           `json:"status"`
}

type manifestEntry struct {
	Direction             string            `json:"direction"`
	FacadeTarget          string            `json:"facade_target"`
	Family                string            `json:"family"`
	Kind                  string            `json:"kind"`
	Method                string            `json:"method"`
	ParamsOrPayloadSchema string            `json:"params_or_payload_schema"`
	ResponseSchema        string            `json:"response_schema"`
	ResponseSchemaStatus  string            `json:"response_schema_status"`
	ResponseType          string            `json:"response_type"`
	SchemaTitle           string            `json:"schema_title"`
	SourceRef             map[string]string `json:"source_ref"`
	SourceSchema          string            `json:"source_schema"`
	SourceVariant         string            `json:"source_variant"`
	Stability             string            `json:"stability"`
	StabilitySource       string            `json:"stability_source"`
	FacadeStatus          string            `json:"facade_status,omitempty"`
}

type aggregateEntry struct {
	aggregate   string
	index       int
	method      string
	paramsRef   string
	schemaTitle string
}

type coverageFile struct {
	Description   string           `json:"description"`
	Fields        []map[string]any `json:"fields"`
	Methods       []map[string]any `json:"methods"`
	SchemaVersion int              `json:"schema_version"`
	Status        string           `json:"status"`
	Types         []map[string]any `json:"types"`
	ValidStatuses []string         `json:"valid_statuses"`
}

func loadJSON(path string, dst any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func pascalCase(value string) string {
	parts := tokenRE.FindAllString(value, -1)
	var b strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		if len(part) > 1 {
			b.WriteString(part[1:])
		}
	}
	return b.String()
}

func titleVariant(title string) string {
	for _, suffix := range []string{"Request", "Notification"} {
		if strings.HasSuffix(title, suffix) {
			title = title[:len(title)-len(suffix)]
			break
		}
	}
	return pascalCase(title)
}

func typeNameFromSchema(path string) (string, error) {
	var data map[string]any
	if err := loadJSON(path, &data); err != nil {
		return "", err
	}
	if title, ok := data["title"].(string); ok && title != "" {
		return title, nil
	}
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), nil
}

func schemaFilesMap(root string) (map[string]string, error) {
	files, err := schemaFiles(root)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, rel := range files {
		out[strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))] = rel
	}
	return out, nil
}

func schemaPathForType(root, typeName string) (string, error) {
	if typeName == "" {
		return "", nil
	}
	byFile, err := schemaFilesMap(root)
	if err != nil {
		return "", err
	}
	if rel, ok := byFile[typeName]; ok {
		return rel, nil
	}
	files, err := schemaFiles(root)
	if err != nil {
		return "", err
	}
	for _, rel := range files {
		var data map[string]any
		if err := loadJSON(filepath.Join(root, filepath.FromSlash(rel)), &data); err != nil {
			continue
		}
		if title, _ := data["title"].(string); title == typeName {
			return rel, nil
		}
	}
	return "", nil
}

func aggregateKindDirection(aggregate string) (string, string, error) {
	switch aggregate {
	case "ClientRequest.json":
		return "client_to_server", "request", nil
	case "ServerRequest.json":
		return "server_to_client", "request", nil
	case "ServerNotification.json":
		return "server_to_client", "notification", nil
	case "ClientNotification.json":
		return "client_to_server", "notification", nil
	default:
		return "", "", fmt.Errorf("unsupported aggregate schema: %s", aggregate)
	}
}

func loadAggregateEntries(root, aggregate string) ([]aggregateEntry, error) {
	var schema struct {
		OneOf []struct {
			Title      string `json:"title"`
			Properties struct {
				Method struct {
					Enum []string `json:"enum"`
				} `json:"method"`
				Params struct {
					Ref string `json:"$ref"`
				} `json:"params"`
			} `json:"properties"`
		} `json:"oneOf"`
	}
	if err := loadJSON(filepath.Join(root, aggregate), &schema); err != nil {
		return nil, err
	}
	var entries []aggregateEntry
	for index, variant := range schema.OneOf {
		if len(variant.Properties.Method.Enum) == 0 {
			continue
		}
		entries = append(entries, aggregateEntry{
			aggregate:   aggregate,
			index:       index,
			method:      variant.Properties.Method.Enum[0],
			paramsRef:   variant.Properties.Params.Ref,
			schemaTitle: variant.Title,
		})
	}
	return entries, nil
}

func facadeTarget(entry aggregateEntry, direction, kind string, existing *manifestEntry, mapping *requestMapping) string {
	if existing != nil && existing.FacadeTarget != "" {
		return existing.FacadeTarget
	}
	variant := titleVariant(entry.schemaTitle)
	if mapping != nil {
		variant = mapping.variant
	}
	if direction == "server_to_client" && kind == "request" {
		return "ServerRequests()." + variant
	}
	if direction == "server_to_client" && kind == "notification" {
		return "ServerNotifications()." + titleVariant(entry.schemaTitle)
	}
	if direction == "client_to_server" && kind == "notification" {
		return "ClientNotifications()." + titleVariant(entry.schemaTitle)
	}
	family := strings.SplitN(entry.method, "/", 2)[0]
	if override, ok := rootInternalOverrides[entry.method]; ok {
		return override
	}
	if override, ok := rootOperationOverrides[family]; ok {
		return override
	}
	accessor := familyAccessors[family]
	if accessor == "" {
		accessor = pascalCase(family) + "()"
	}
	suffix := entry.method
	if parts := strings.SplitN(entry.method, "/", 2); len(parts) == 2 {
		suffix = parts[1]
	}
	return accessor + "." + pascalCase(suffix)
}

func buildManifest(root string, old manifestFile, mappings map[string]requestMapping, sourceCommit string) (manifestFile, error) {
	if old.SchemaVersion < 2 {
		return manifestFile{}, fmt.Errorf("classified manifest schema_version must be at least 2")
	}
	oldEntries := map[string]manifestEntry{}
	for _, entry := range old.Entries {
		oldEntries[entry.Method] = entry
	}
	aggregates := old.AggregateSchemas
	if len(aggregates) == 0 {
		aggregates = append([]string(nil), aggregateSchemas...)
	}
	var entries []manifestEntry
	for _, aggregate := range aggregates {
		direction, kind, err := aggregateKindDirection(aggregate)
		if err != nil {
			return manifestFile{}, err
		}
		aggEntries, err := loadAggregateEntries(root, aggregate)
		if err != nil {
			return manifestFile{}, err
		}
		for _, aggregateEntry := range aggEntries {
			var existingPtr *manifestEntry
			if existing, ok := oldEntries[aggregateEntry.method]; ok {
				copy := existing
				existingPtr = &copy
			}
			var mappingPtr *requestMapping
			if mapping, ok := mappings[aggregateEntry.method]; ok {
				copy := mapping
				mappingPtr = &copy
			}
			paramsName := refName(aggregateEntry.paramsRef)
			paramsSchema, err := schemaPathForType(root, paramsName)
			if err != nil {
				return manifestFile{}, err
			}
			stability := "stable"
			stabilitySource := "present_in_stable_schema"
			if existingPtr != nil && existingPtr.Stability != "" {
				stability = existingPtr.Stability
				stabilitySource = existingPtr.StabilitySource
			} else if mappingPtr != nil && mappingPtr.experimental {
				stability = "experimental"
				stabilitySource = "experimental_only_in_schema"
			}
			stabilityText := "present in schema generated without --experimental"
			if stability == "experimental" {
				stabilityText = "absent from schema generated without --experimental"
			}
			responseSchema := ""
			responseStatus := "not_applicable"
			responseType := ""
			responseMapping := "not_applicable:notification_does_not_expect_response"
			if kind == "request" {
				responseStatus = "declared"
				if mappingPtr != nil {
					responseType = mappingPtr.responseType
				} else if existingPtr != nil {
					responseType = existingPtr.ResponseType
				}
				resolved, err := schemaPathForType(root, responseType)
				if err != nil {
					return manifestFile{}, err
				}
				responseSchema = resolved
				if responseSchema == "" && existingPtr != nil {
					responseSchema = existingPtr.ResponseSchema
				}
				if responseSchema == "" {
					return manifestFile{}, fmt.Errorf("unable to resolve response schema for request method %q", aggregateEntry.method)
				}
				if mappingPtr != nil {
					responseMapping = commonRSRef + "#" + mappingPtr.macroName + "/" + mappingPtr.variant
				} else if existingPtr != nil && existingPtr.SourceRef != nil {
					responseMapping = existingPtr.SourceRef["response_mapping"]
				}
				if responseMapping == "" || strings.HasPrefix(responseMapping, "not_applicable") {
					return manifestFile{}, fmt.Errorf("missing response mapping for request method %q", aggregateEntry.method)
				}
			}
			target := facadeTarget(aggregateEntry, direction, kind, existingPtr, mappingPtr)
			entry := manifestEntry{
				Direction:             direction,
				FacadeTarget:          target,
				Family:                strings.SplitN(aggregateEntry.method, "/", 2)[0],
				Kind:                  kind,
				Method:                aggregateEntry.method,
				ParamsOrPayloadSchema: paramsName,
				ResponseSchema:        responseSchema,
				ResponseSchemaStatus:  responseStatus,
				ResponseType:          responseType,
				SchemaTitle:           aggregateEntry.schemaTitle,
				SourceRef: map[string]string{
					"aggregate_pointer":        fmt.Sprintf("%s#/oneOf/%d", aggregate, aggregateEntry.index),
					"aggregate_schema":         aggregate,
					"baseline_source_commit":   sourceCommit,
					"facade_rule":              "manifest_generation.json#facade_target_rule",
					"params_or_payload_schema": paramsSchema,
					"response_mapping":         responseMapping,
					"response_schema":          responseSchema,
					"stability":                stabilityText,
				},
				SourceSchema:    aggregate,
				SourceVariant:   titleVariant(aggregateEntry.schemaTitle),
				Stability:       stability,
				StabilitySource: stabilitySource,
			}
			if mappingPtr != nil {
				entry.SourceVariant = mappingPtr.variant
			}
			if direction == "client_to_server" && kind == "request" && facadeTargetRE.MatchString(target) {
				// Facade availability is derived from the current generated
				// protocol surface. Historical deferred status is not authority.
				entry.FacadeStatus = "generated"
			}
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Method < entries[j].Method })
	description := old.Description
	if description == "" {
		description = "Classified app-server protocol manifest."
	}
	return manifestFile{
		AggregateSchemas:      aggregates,
		ClassificationSources: old.ClassificationSources,
		Description:           description,
		Entries:               entries,
		Surface:               old.Surface,
		SchemaVersion:         old.SchemaVersion,
		Status:                "classified-manifest",
	}, nil
}

func jsonPointerExists(document any, pointer string) bool {
	if pointer == "" || pointer == "#" {
		return true
	}
	if strings.HasPrefix(pointer, "#") {
		pointer = pointer[1:]
	}
	if !strings.HasPrefix(pointer, "/") {
		return false
	}
	current := document
	for _, rawPart := range strings.Split(pointer, "/")[1:] {
		part := strings.ReplaceAll(strings.ReplaceAll(rawPart, "~1", "/"), "~0", "~")
		switch typed := current.(type) {
		case map[string]any:
			next, ok := typed[part]
			if !ok {
				return false
			}
			current = next
		case []any:
			idx := 0
			if _, err := fmt.Sscanf(part, "%d", &idx); err != nil || idx < 0 || idx >= len(typed) {
				return false
			}
			current = typed[idx]
		default:
			return false
		}
	}
	return true
}

func pointerExists(root, schema, path string) bool {
	if schema == "" || path == "" {
		return false
	}
	schemaPath, pointer, ok := strings.Cut(path, "#")
	if !ok {
		schemaPath, pointer = path, ""
	} else {
		pointer = "#" + pointer
	}
	if schemaPath != schema {
		return false
	}
	var document any
	if err := loadJSON(filepath.Join(root, filepath.FromSlash(schema)), &document); err != nil {
		return false
	}
	return jsonPointerExists(document, pointer)
}

func defaultMethodCoverage(entry manifestEntry) map[string]any {
	return map[string]any{
		"direction":       entry.Direction,
		"exit_condition":  "Regenerate method registry and add handwritten facade coverage only if this upstream method becomes part of the public SDK surface.",
		"kind":            entry.Kind,
		"method":          entry.Method,
		"owner":           "codex-go-sdk",
		"reason":          fmt.Sprintf("Generated protocolv2 method registry coverage for %s %s method; no handwritten facade is claimed until explicitly added.", entry.Direction, entry.Kind),
		"revisit_trigger": "protocolv2 generation or upstream schema drift",
		"source_schema":   entry.SourceSchema,
		"stability":       entry.Stability,
		"status":          "supported-generated",
	}
}

func defaultTypeCoverage(root, schema, stability string) (map[string]any, error) {
	typ, err := typeNameFromSchema(filepath.Join(root, filepath.FromSlash(schema)))
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"exit_condition":  "Regenerate if upstream schema drift changes this generated protocol type.",
		"owner":           "codex-go-sdk",
		"reason":          "Generated from the checked-in upstream app-server schema baseline.",
		"revisit_trigger": "protocolv2 generation or upstream schema drift",
		"schema":          schema,
		"stability":       stability,
		"status":          "supported-generated",
		"type":            typ,
	}, nil
}

func defaultFieldCoverage(root, schema, field string, required bool, stability string) (map[string]any, error) {
	typ, err := typeNameFromSchema(filepath.Join(root, filepath.FromSlash(schema)))
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"exit_condition":  fmt.Sprintf("Regenerate if upstream schema drift changes %s.%s semantics.", typ, field),
		"field":           field,
		"owner":           "codex-go-sdk",
		"path":            schema + "#/properties/" + field,
		"reason":          fmt.Sprintf("Generated as a strict typed %s field.", typ),
		"required":        required,
		"revisit_trigger": "protocolv2 generation or upstream schema drift",
		"schema":          schema,
		"stability":       stability,
		"status":          "supported-generated",
		"type":            typ,
	}, nil
}

func topLevelObjectFields(root, schema, stability string) ([]map[string]any, error) {
	var data map[string]any
	if err := loadJSON(filepath.Join(root, filepath.FromSlash(schema)), &data); err != nil {
		return nil, err
	}
	if data["type"] != "object" {
		return nil, nil
	}
	properties, _ := data["properties"].(map[string]any)
	if properties == nil {
		return nil, nil
	}
	requiredSet := map[string]bool{}
	if required, ok := data["required"].([]any); ok {
		for _, item := range required {
			if name, ok := item.(string); ok {
				requiredSet[name] = true
			}
		}
	}
	var names []string
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	var fields []map[string]any
	for _, name := range names {
		field, err := defaultFieldCoverage(root, schema, name, requiredSet[name], stability)
		if err != nil {
			return nil, err
		}
		fields = append(fields, field)
	}
	return fields, nil
}

func buildCoverage(root string, old coverageFile, manifest manifestFile, fieldSeedSchemas map[string]bool) (coverageFile, error) {
	if old.SchemaVersion != 1 {
		return coverageFile{}, fmt.Errorf("coverage matrix schema_version must be 1")
	}
	validStatuses := old.ValidStatuses
	if len(validStatuses) == 0 {
		validStatuses = append([]string(nil), validCoverageStatuses...)
	}
	oldMethods := map[string]map[string]any{}
	for _, item := range old.Methods {
		if method, _ := item["method"].(string); method != "" {
			oldMethods[method] = item
		}
	}
	var methods []map[string]any
	for _, entry := range manifest.Entries {
		method := defaultMethodCoverage(entry)
		if existing, ok := oldMethods[entry.Method]; ok {
			method = cloneMap(existing)
		}
		method["direction"] = entry.Direction
		method["kind"] = entry.Kind
		method["method"] = entry.Method
		method["source_schema"] = entry.SourceSchema
		method["stability"] = entry.Stability
		methods = append(methods, method)
	}
	schemaPaths, err := schemaFiles(root)
	if err != nil {
		return coverageFile{}, err
	}
	oldTypes := map[string]map[string]any{}
	for _, item := range old.Types {
		if schema, _ := item["schema"].(string); schema != "" {
			oldTypes[schema] = item
		}
	}
	manifestStability := map[string]string{}
	for _, entry := range manifest.Entries {
		if entry.ResponseSchema != "" {
			manifestStability[entry.ResponseSchema] = entry.Stability
		}
		paramsSchema, err := schemaPathForType(root, entry.ParamsOrPayloadSchema)
		if err != nil {
			return coverageFile{}, err
		}
		if paramsSchema != "" {
			if _, ok := manifestStability[paramsSchema]; !ok {
				manifestStability[paramsSchema] = entry.Stability
			}
		}
	}
	var types []map[string]any
	for _, schema := range schemaPaths {
		stability := manifestStability[schema]
		if stability == "" {
			if oldType, ok := oldTypes[schema]; ok {
				stability, _ = oldType["stability"].(string)
			}
			if stability == "" {
				stability = "stable"
			}
		}
		typ, err := defaultTypeCoverage(root, schema, stability)
		if err != nil {
			return coverageFile{}, err
		}
		if existing, ok := oldTypes[schema]; ok {
			typ = cloneMap(existing)
			typ["schema"] = schema
		}
		if _, ok := manifestStability[schema]; ok {
			typ["stability"] = manifestStability[schema]
		}
		types = append(types, typ)
	}
	fieldsByPath := map[string]map[string]any{}
	for _, field := range old.Fields {
		schema, _ := field["schema"].(string)
		path, _ := field["path"].(string)
		if pointerExists(root, schema, path) {
			fieldsByPath[path] = cloneMap(field)
		}
	}
	for schema := range fieldSeedSchemas {
		found := false
		for _, path := range schemaPaths {
			if path == schema {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		stability := manifestStability[schema]
		if stability == "" {
			if oldType, ok := oldTypes[schema]; ok {
				stability, _ = oldType["stability"].(string)
			}
			if stability == "" {
				stability = "stable"
			}
		}
		fields, err := topLevelObjectFields(root, schema, stability)
		if err != nil {
			return coverageFile{}, err
		}
		for _, field := range fields {
			path, _ := field["path"].(string)
			if _, ok := fieldsByPath[path]; !ok {
				fieldsByPath[path] = field
			}
		}
	}
	var fields []map[string]any
	for _, field := range fieldsByPath {
		fields = append(fields, field)
	}
	sort.Slice(fields, func(i, j int) bool {
		left, _ := fields[i]["path"].(string)
		right, _ := fields[j]["path"].(string)
		return left < right
	})
	sort.Slice(methods, func(i, j int) bool {
		left, _ := methods[i]["method"].(string)
		right, _ := methods[j]["method"].(string)
		return left < right
	})
	sort.Slice(types, func(i, j int) bool {
		left, _ := types[i]["schema"].(string)
		right, _ := types[j]["schema"].(string)
		return left < right
	})
	description := old.Description
	if description == "" {
		description = "Coverage classification for the checked-in Codex app-server protocol baseline."
	}
	return coverageFile{
		Description:   description,
		Fields:        fields,
		Methods:       methods,
		SchemaVersion: old.SchemaVersion,
		Status:        "classified-manifest",
		Types:         types,
		ValidStatuses: validStatuses,
	}, nil
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func updateManifestGeneration(path, targetRef, targetKind, targetSHA string) error {
	var data map[string]any
	if err := loadJSON(path, &data); err != nil {
		return err
	}
	inputs, ok := data["inputs"].(map[string]any)
	if !ok {
		return fmt.Errorf("manifest generation inputs must be an object")
	}
	inputs["source_ref_name"] = targetRef
	inputs["source_ref_kind"] = targetKind
	inputs["source_commit"] = targetSHA
	data["inputs"] = inputs
	return writeJSON(path, data)
}

func buildMetadata(root string, old map[string]any, targetRef, targetKind, targetSHA, codexVersion string, generatedAt time.Time) (map[string]any, error) {
	version, ok := old["schema_version"].(float64)
	if !ok || int(version) != 1 {
		if versionInt, ok := old["schema_version"].(int); !ok || versionInt != 1 {
			return nil, fmt.Errorf("baseline metadata schema_version must be 1")
		}
	}
	files, err := schemaFiles(root)
	if err != nil {
		return nil, err
	}
	bundle, err := schemaBundleSHA256(root)
	if err != nil {
		return nil, err
	}
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	generatedAt = generatedAt.Truncate(time.Second)
	return map[string]any{
		"aggregate_schemas":         append([]string(nil), aggregateSchemas...),
		"codex_binary":              "codex",
		"codex_version":             codexVersion,
		"experimental_included":     true,
		"generated_at":              generatedAt.Format("2006-01-02T15:04:05Z"),
		"generation_command":        "codex app-server generate-json-schema --experimental --out internal/protocolschema/appserver/v2",
		"schema_bundle_sha256":      bundle,
		"schema_file_count":         len(files),
		"schema_output_layout":      "root JSON files plus v1/ and v2/",
		"schema_version":            1,
		"source_commit":             targetSHA,
		"source_dirty":              false,
		"source_license":            "Apache-2.0",
		"source_ref_kind":           targetKind,
		"source_ref_name":           targetRef,
		"source_ref_url":            "https://github.com/openai/codex/tree/" + targetSHA,
		"source_repo":               sourceRepo,
		"source_schema_command_ref": "codex app-server generate-json-schema",
		"source_subdir":             "codex-rs/app-server-protocol",
	}, nil
}

func copyCandidateSchema(candidate, baseline string) error {
	current, err := schemaFiles(baseline)
	if err != nil {
		return err
	}
	for _, rel := range current {
		if err := os.Remove(filepath.Join(baseline, filepath.FromSlash(rel))); err != nil {
			return err
		}
	}
	files, err := schemaFiles(candidate)
	if err != nil {
		return err
	}
	for _, rel := range files {
		src := filepath.Join(candidate, filepath.FromSlash(rel))
		dst := filepath.Join(baseline, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := copyFileBytes(src, dst); err != nil {
			return err
		}
	}
	return nil
}

func copyFileBytes(src, dst string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, raw, 0o644)
}
