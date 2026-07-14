package protocolgen

import (
	"encoding/json"
	"fmt"
	"os"
)

const classifiedManifestStatus = "classified-manifest"

type Manifest struct {
	Entries       []ManifestEntry `json:"entries"`
	SchemaVersion int             `json:"schema_version"`
	Surface       []SurfaceEntry  `json:"surface"`
	Status        string          `json:"status"`
}

type SurfaceEntry struct {
	Kind      SurfaceKind `json:"kind"`
	Name      string      `json:"name"`
	Owner     string      `json:"owner,omitempty"`
	Signature string      `json:"signature"`
	Stability Stability   `json:"stability"`
}

type ManifestEntry struct {
	Direction             string `json:"direction"`
	FacadeTarget          string `json:"facade_target"`
	Family                string `json:"family"`
	Kind                  string `json:"kind"`
	Method                string `json:"method"`
	ParamsOrPayloadSchema string `json:"params_or_payload_schema"`
	ResponseSchema        string `json:"response_schema"`
	ResponseSchemaStatus  string `json:"response_schema_status"`
	SourceSchema          string `json:"source_schema"`
	Stability             string `json:"stability"`
}

func LoadManifest(path string) (Manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if manifest.Status != classifiedManifestStatus {
		return Manifest{}, fmt.Errorf("manifest status %q is not %q", manifest.Status, classifiedManifestStatus)
	}
	if manifest.SchemaVersion >= 2 {
		if err := ValidateSurface(manifest.Surface); err != nil {
			return Manifest{}, fmt.Errorf("validate manifest surface: %w", err)
		}
	}
	seen := map[string]bool{}
	for _, entry := range manifest.Entries {
		if entry.Method == "" || entry.Direction == "" || entry.Kind == "" || entry.Family == "" || entry.FacadeTarget == "" || entry.Stability == "" {
			return Manifest{}, fmt.Errorf("manifest entry %q is missing required classification facts", entry.Method)
		}
		if seen[entry.Method] {
			return Manifest{}, fmt.Errorf("manifest method %q appears more than once", entry.Method)
		}
		seen[entry.Method] = true
		switch entry.Kind {
		case "request":
			if entry.ResponseSchema == "" || entry.ResponseSchemaStatus != "declared" {
				return Manifest{}, fmt.Errorf("request method %q missing declared response schema", entry.Method)
			}
		case "notification":
			if entry.ResponseSchema != "" || entry.ResponseSchemaStatus != "not_applicable" {
				return Manifest{}, fmt.Errorf("notification method %q must have response_schema_status=not_applicable", entry.Method)
			}
		default:
			return Manifest{}, fmt.Errorf("method %q has unsupported kind %q", entry.Method, entry.Kind)
		}
	}
	return manifest, nil
}
