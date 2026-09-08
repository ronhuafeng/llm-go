package v2

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed baseline_metadata.json
var baselineMetadataJSON []byte

// BaselineMetadata is the checked-in generated-protocol provenance recorded
// next to the schema baseline. It is the canonical authority for the
// upstream ref/commit the generated surface was built from.
type BaselineMetadata struct {
	SourceCommit  string `json:"source_commit"`
	SourceRefKind string `json:"source_ref_kind"`
	SourceRefName string `json:"source_ref_name"`
	SourceRepo    string `json:"source_repo"`
}

// LoadBaselineMetadata decodes the checked-in baseline_metadata.json.
func LoadBaselineMetadata() (BaselineMetadata, error) {
	var metadata BaselineMetadata
	if err := json.Unmarshal(baselineMetadataJSON, &metadata); err != nil {
		return BaselineMetadata{}, fmt.Errorf("decode baseline_metadata.json: %w", err)
	}
	if metadata.SourceCommit == "" || metadata.SourceRefKind == "" || metadata.SourceRefName == "" || metadata.SourceRepo == "" {
		return BaselineMetadata{}, fmt.Errorf("baseline_metadata.json missing public upstream provenance")
	}
	return metadata, nil
}
