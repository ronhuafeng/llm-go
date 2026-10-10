package protocolupgrade

import (
	"fmt"
	"path/filepath"
	"strings"
)

// OraclePublicRoots reads public wire entrypoints from exact upstream aggregate
// schemas and common.rs response mappings. It does not inspect generated Go or
// persisted manifest/coverage projections. Standalone internal-only payload
// schemas are not public entrypoints merely because the exporter wrote a file.
func OraclePublicRoots(schemaRoot, commonRS string) (map[string]bool, error) {
	mappings, err := parseRequestMappings(commonRS)
	if err != nil {
		return nil, err
	}
	roots := map[string]bool{}
	for _, aggregate := range aggregateSchemas {
		roots[strings.TrimSuffix(aggregate, ".json")] = true
		if err := auditOracleAggregate(filepath.Join(schemaRoot, aggregate)); err != nil {
			return nil, err
		}
		entries, err := loadAggregateEntries(schemaRoot, aggregate)
		if err != nil {
			return nil, err
		}
		direction, kind, err := aggregateKindDirection(aggregate)
		if err != nil {
			return nil, err
		}
		if kind != "request" {
			continue
		}
		for _, entry := range entries {
			mapping, ok := mappings[entry.method]
			want := "client_request_definitions"
			if direction == "server_to_client" {
				want = "server_request_definitions"
			}
			if !ok || mapping.responseType == "" || mapping.macroName != want {
				return nil, fmt.Errorf("oracle public roots: missing response mapping for %s method %q", aggregate, entry.method)
			}
			roots[mapping.responseType] = true
		}
	}
	return roots, nil
}

// The existing extractor recognizes oneOf branches with a single method enum.
// Audit that exact capability before using it: an upstream representation change
// must not silently erase response obligations.
func auditOracleAggregate(path string) error {
	var root map[string]any
	if err := loadJSON(path, &root); err != nil {
		return err
	}
	for _, keyword := range []string{"anyOf", "allOf", "$ref"} {
		if _, exists := root[keyword]; exists {
			return fmt.Errorf("oracle public roots: unsupported aggregate %s keyword %s", path, keyword)
		}
	}
	variants, ok := root["oneOf"].([]any)
	if !ok {
		return fmt.Errorf("oracle public roots: aggregate %s requires oneOf", path)
	}
	for i, value := range variants {
		branch, _ := value.(map[string]any)
		for _, keyword := range []string{"anyOf", "oneOf", "allOf", "$ref"} {
			if _, exists := branch[keyword]; exists {
				return fmt.Errorf("oracle public roots: unsupported aggregate %s branch %d keyword %s", path, i, keyword)
			}
		}
		props, _ := branch["properties"].(map[string]any)
		method, _ := props["method"].(map[string]any)
		values, ok := method["enum"].([]any)
		if !ok || len(values) != 1 {
			return fmt.Errorf("oracle public roots: aggregate %s branch %d requires single method enum", path, i)
		}
		if wire, ok := values[0].(string); !ok || wire == "" {
			return fmt.Errorf("oracle public roots: aggregate %s branch %d requires string method enum", path, i)
		}
		if _, exists := method["const"]; exists {
			return fmt.Errorf("oracle public roots: unsupported aggregate %s branch %d method const", path, i)
		}
	}
	return nil
}

// OracleAcceptedPublicRoots uses only the immutable accepted manifest's response
// associations. Payload roots still come from the accepted raw aggregates, and
// every association must match its raw method position and accepted source SHA.
func OracleAcceptedPublicRoots(schemaRoot string) (map[string]bool, error) {
	var manifest manifestFile
	if err := loadJSON(filepath.Join(schemaRoot, "manifest.json"), &manifest); err != nil {
		return nil, err
	}
	if manifest.SchemaVersion < 2 || manifest.Status != "classified-manifest" {
		return nil, fmt.Errorf("oracle accepted roots: invalid manifest classification")
	}
	var metadata struct {
		SourceCommit string `json:"source_commit"`
	}
	if err := loadJSON(filepath.Join(schemaRoot, "baseline_metadata.json"), &metadata); err != nil {
		return nil, err
	}
	if metadata.SourceCommit == "" {
		return nil, fmt.Errorf("oracle accepted roots: missing accepted source commit")
	}
	entries := map[string]manifestEntry{}
	for _, entry := range manifest.Entries {
		if _, exists := entries[entry.Method]; exists {
			return nil, fmt.Errorf("oracle accepted roots: duplicate method %q", entry.Method)
		}
		entries[entry.Method] = entry
	}
	roots := map[string]bool{}
	for _, aggregate := range aggregateSchemas {
		roots[strings.TrimSuffix(aggregate, ".json")] = true
		if err := auditOracleAggregate(filepath.Join(schemaRoot, aggregate)); err != nil {
			return nil, err
		}
		raw, err := loadAggregateEntries(schemaRoot, aggregate)
		if err != nil {
			return nil, err
		}
		direction, kind, err := aggregateKindDirection(aggregate)
		if err != nil {
			return nil, err
		}
		for _, method := range raw {
			entry, exists := entries[method.method]
			pointer := fmt.Sprintf("%s#/oneOf/%d", aggregate, method.index)
			if !exists || entry.Direction != direction || entry.Kind != kind || entry.SourceSchema != aggregate || entry.SourceRef["aggregate_schema"] != aggregate || entry.SourceRef["aggregate_pointer"] != pointer || entry.SourceRef["baseline_source_commit"] != metadata.SourceCommit {
				return nil, fmt.Errorf("oracle accepted roots: manifest/source mismatch at %s method %q", pointer, method.method)
			}
			delete(entries, method.method)
			if kind != "request" {
				if entry.ResponseType != "" || entry.ResponseSchema != "" {
					return nil, fmt.Errorf("oracle accepted roots: notification %q has response", method.method)
				}
				continue
			}
			path := entry.ResponseSchema
			if entry.ResponseType == "" || path == "" || filepath.IsAbs(path) || filepath.ToSlash(filepath.Clean(path)) != path || strings.HasPrefix(path, "../") || entry.ResponseSchemaStatus != "declared" || entry.SourceRef["response_schema"] != path {
				return nil, fmt.Errorf("oracle accepted roots: invalid response mapping for %q", method.method)
			}
			name, err := typeNameFromSchema(filepath.Join(schemaRoot, path))
			if err != nil {
				return nil, err
			}
			if name != entry.ResponseType {
				return nil, fmt.Errorf("oracle accepted roots: response type/schema mismatch for %q", method.method)
			}
			roots[name] = true
		}
	}
	if len(entries) != 0 {
		return nil, fmt.Errorf("oracle accepted roots: manifest contains %d methods absent from raw aggregates", len(entries))
	}
	return roots, nil
}
