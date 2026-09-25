package protocolupgrade

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

var generatedProtocolArtifacts = []string{
	"protocolv2/method_registry.gen.go",
	"protocolv2/protocol_types.gen.go",
	"protocolv2/experimental_members.gen.go",
	"sdk_surface.gen.go",
}

var exactMetadataFiles = []string{
	"baseline_metadata.json",
	"manifest_generation.json",
	"manifest.json",
	"coverage_matrix.json",
}

func compareExactBaseline(baseline, rebuilt, moduleRoot, rebuiltModuleRoot string) error {
	acceptedSchemas, err := schemaHashes(baseline)
	if err != nil {
		return fmt.Errorf("hash accepted schemas: %w", err)
	}
	rebuiltSchemas, err := schemaHashes(rebuilt)
	if err != nil {
		return fmt.Errorf("hash rebuilt schemas: %w", err)
	}
	if diff := fileDiff(acceptedSchemas, rebuiltSchemas); !diff.empty() {
		return &VerificationError{Err: fmt.Errorf("schema mismatch:\n%s", fileDiffDiagnostic(diff))}
	}
	for _, name := range exactMetadataFiles {
		accepted, err := semanticJSON(filepath.Join(baseline, name), name == "baseline_metadata.json")
		if err != nil {
			return err
		}
		candidate, err := semanticJSON(filepath.Join(rebuilt, name), name == "baseline_metadata.json")
		if err != nil {
			return err
		}
		if !bytes.Equal(accepted, candidate) {
			return &VerificationError{Err: fmt.Errorf("%s differs from exact upstream reconstruction", name)}
		}
	}
	for _, rel := range generatedProtocolArtifacts {
		accepted, err := os.ReadFile(filepath.Join(moduleRoot, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		candidate, err := os.ReadFile(filepath.Join(rebuiltModuleRoot, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		if !bytes.Equal(accepted, candidate) {
			return &VerificationError{Err: fmt.Errorf("%s differs from exact upstream reconstruction: accepted sha256=%x rebuilt sha256=%x", rel, sha256.Sum256(accepted), sha256.Sum256(candidate))}
		}
	}
	return nil
}

func semanticJSON(path string, omitGeneratedAt bool) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !omitGeneratedAt {
		return canonicalJSON(raw)
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if object == nil {
		return nil, fmt.Errorf("%s must contain a JSON object", path)
	}
	delete(object, "generated_at")
	return json.Marshal(object)
}
