package protocolsync

import (
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// verifyOfficialSchema compares only version-bound source facts. Native behavior
// assertions evolve with the candidate and never come from a different version.
func verifyOfficialSchema(candidate, rebuilt string) error {
	identity, err := loadBaselineIdentity(filepath.Join(candidate, "codexsdk", defaultBaselineRel, "baseline_metadata.json"))
	if err != nil {
		return err
	}
	if len(identity.SourceCommit) != 40 {
		return fmt.Errorf("invalid candidate source identity")
	}
	if _, err := hex.DecodeString(identity.SourceCommit); err != nil {
		return err
	}
	marker, err := os.ReadFile(filepath.Join(rebuilt, "common.rs.source_sha"))
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(marker)) != identity.SourceCommit {
		return fmt.Errorf("rebuilt official source identity differs from candidate")
	}
	expected, err := officialSchemaTree(filepath.Join(rebuilt, "schema"))
	if err != nil {
		return err
	}
	actual, err := officialSchemaTree(filepath.Join(candidate, "codexsdk", defaultBaselineRel))
	if err != nil {
		return err
	}
	if len(expected) == 0 || !reflect.DeepEqual(expected, actual) {
		return fmt.Errorf("candidate schema differs from rebuilt official source")
	}
	return nil
}
func officialSchemaTree(root string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("nonregular official schema input")
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasSuffix(rel, ".json") {
			return nil
		}
		switch rel {
		case "baseline_metadata.json", "coverage_matrix.json", "drift_report.json", "manifest.json", "manifest_generation.json", "matrix_update_skeleton.json":
			return nil
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		files[rel] = repairDigest(b)
		return nil
	})
	return files, err
}
