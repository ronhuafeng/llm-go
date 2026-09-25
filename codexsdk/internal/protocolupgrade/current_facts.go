package protocolupgrade

import "github.com/ronhuafeng/llm-go/codexsdk/internal/protocolgen"

// derivedCoverage keeps current wire facts separate from persisted annotations.
// Only buildCoverage constructs it, before historical annotations are attached.
type derivedCoverage struct {
	coverageFile
	facts protocolgen.CoverageMatrix
}

func coverageTypeFact(item map[string]any) protocolgen.CoverageType {
	schema, _ := item["schema"].(string)
	stability, _ := item["stability"].(string)
	status, _ := item["status"].(string)
	name, _ := item["type"].(string)
	return protocolgen.CoverageType{Schema: schema, Stability: stability, Status: status, Type: name}
}

func coverageFieldFact(item map[string]any) protocolgen.CoverageField {
	schema, _ := item["schema"].(string)
	stability, _ := item["stability"].(string)
	status, _ := item["status"].(string)
	name, _ := item["type"].(string)
	field, _ := item["field"].(string)
	path, _ := item["path"].(string)
	required, _ := item["required"].(bool)
	return protocolgen.CoverageField{Schema: schema, Stability: stability, Status: status, Type: name, Field: field, Path: path, Required: required}
}

func methodFacts(manifest manifestFile) protocolgen.Manifest {
	result := protocolgen.Manifest{SchemaVersion: manifest.SchemaVersion, Status: manifest.Status}
	for _, entry := range manifest.Entries {
		result.Entries = append(result.Entries, protocolgen.ManifestEntry{
			Direction: entry.Direction, FacadeStatus: entry.FacadeStatus, FacadeTarget: entry.FacadeTarget,
			Family: entry.Family, Kind: entry.Kind, Method: entry.Method, ParamsOrPayloadSchema: entry.ParamsOrPayloadSchema,
			ResponseSchema: entry.ResponseSchema, ResponseSchemaStatus: entry.ResponseSchemaStatus, ResponseType: entry.ResponseType,
			SourceSchema: entry.SourceSchema, Stability: entry.Stability,
		})
	}
	return result
}
