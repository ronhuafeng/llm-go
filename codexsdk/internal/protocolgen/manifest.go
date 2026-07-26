package protocolgen

import (
	"encoding/json"
	"fmt"
	"os"
)

const (
	classifiedManifestStatus        = "classified-manifest"
	manifestDirectionClientToServer = "client_to_server"
	manifestDirectionServerToClient = "server_to_client"
	manifestKindNotification        = "notification"
	manifestKindRequest             = "request"
)

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
	if manifest.SchemaVersion < 2 {
		return Manifest{}, fmt.Errorf("manifest schema_version %d is older than the classified surface contract", manifest.SchemaVersion)
	}
	if err := ValidateSurface(manifest.Surface); err != nil {
		return Manifest{}, fmt.Errorf("validate manifest surface: %w", err)
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
		case manifestKindRequest:
			if entry.ResponseSchema == "" || entry.ResponseSchemaStatus != "declared" {
				return Manifest{}, fmt.Errorf("request method %q missing declared response schema", entry.Method)
			}
		case manifestKindNotification:
			if entry.ResponseSchema != "" || entry.ResponseSchemaStatus != "not_applicable" {
				return Manifest{}, fmt.Errorf("notification method %q must have response_schema_status=not_applicable", entry.Method)
			}
		default:
			return Manifest{}, fmt.Errorf("method %q has unsupported kind %q", entry.Method, entry.Kind)
		}
	}
	return manifest, nil
}

// ApplyWireMessageRoles marks only protocol message roots. Reusable generated
// value types remain neutral and inherit the role of the root that decodes them.
func ApplyWireMessageRoles(plan *ProtocolTypePlan, manifest Manifest) error {
	rolesBySchema := map[string]WireMessageRoles{}
	setRole := func(schemaPath string, role WireMessageRoles) {
		if schemaPath == "" {
			return
		}
		rolesBySchema[schemaPath] |= role
	}
	for _, entry := range manifest.Entries {
		switch {
		case entry.Direction == manifestDirectionClientToServer && entry.Kind == manifestKindRequest:
			setRole(entry.SourceSchema, WireMessageRoleActionBearingMessage)
			setRole(entry.ResponseSchema, WireMessageRoleServerObservation)
		case entry.Direction == manifestDirectionServerToClient && entry.Kind == manifestKindNotification:
			setRole(entry.SourceSchema, WireMessageRoleServerObservation)
		case entry.Direction == manifestDirectionClientToServer && entry.Kind == manifestKindNotification,
			entry.Direction == manifestDirectionServerToClient && entry.Kind == manifestKindRequest:
			setRole(entry.SourceSchema, WireMessageRoleActionBearingMessage)
			if entry.Kind == manifestKindRequest {
				setRole(entry.ResponseSchema, WireMessageRoleActionBearingMessage)
			}
		default:
			return fmt.Errorf("method %q has unsupported wire role direction=%q kind=%q", entry.Method, entry.Direction, entry.Kind)
		}
	}
	for schemaPath, role := range rolesBySchema {
		found := false
		for index := range plan.Types {
			if plan.Types[index].SchemaPath != schemaPath {
				continue
			}
			plan.Types[index].WireMessageRoles = role
			found = true
		}
		if !found {
			return fmt.Errorf("wire message root schema %q is absent from the protocol type plan", schemaPath)
		}
	}
	return nil
}
