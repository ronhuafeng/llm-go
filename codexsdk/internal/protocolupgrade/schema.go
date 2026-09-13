package protocolupgrade

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	StatusClean             = "clean"
	StatusReviewRequired    = "review-required"
	ComparisonCanonicalJSON = "canonical-json"
	MatrixSkeletonName      = "matrix_update_skeleton.json"
	sourceRepo              = "https://github.com/openai/codex"
	canonicalJSONNote       = "Schema comparisons use canonical JSON; object member ordering is irrelevant."
)

var metadataFiles = map[string]bool{
	"baseline_metadata.json":      true,
	"coverage_matrix.json":        true,
	"drift_report.json":           true,
	"manifest.json":               true,
	"manifest_generation.json":    true,
	"matrix_update_skeleton.json": true,
}

var aggregateSchemas = []string{
	"ClientRequest.json",
	"ServerRequest.json",
	"ServerNotification.json",
	"ClientNotification.json",
}

func schemaFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry == nil || entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || metadataFiles[entry.Name()] {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func schemaHashes(root string) (map[string]string, error) {
	files, err := schemaFiles(root)
	if err != nil {
		return nil, err
	}
	hashes := make(map[string]string, len(files))
	for _, rel := range files {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		canonical, err := canonicalJSON(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		sum := sha256.Sum256(canonical)
		hashes[rel] = hex.EncodeToString(sum[:])
	}
	return hashes, nil
}

func schemaBundleSHA256(root string) (string, error) {
	hashes, err := schemaHashes(root)
	if err != nil {
		return "", err
	}
	canonical, err := json.Marshal(hashes)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalJSON(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func fileDiff(baseline, candidate map[string]string) FileDiff {
	diff := FileDiff{Added: []string{}, Changed: []string{}, Removed: []string{}}
	for path := range candidate {
		if _, ok := baseline[path]; !ok {
			diff.Added = append(diff.Added, path)
		}
	}
	for path, hash := range baseline {
		candidateHash, ok := candidate[path]
		if !ok {
			diff.Removed = append(diff.Removed, path)
			continue
		}
		if hash != candidateHash {
			diff.Changed = append(diff.Changed, path)
		}
	}
	sort.Strings(diff.Added)
	sort.Strings(diff.Changed)
	sort.Strings(diff.Removed)
	return diff
}

func aggregateMethodNames(root, rel string) ([]string, error) {
	path := filepath.Join(root, filepath.FromSlash(rel))
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var schema struct {
		OneOf []struct {
			Properties struct {
				Method struct {
					Enum []string `json:"enum"`
				} `json:"method"`
			} `json:"properties"`
		} `json:"oneOf"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("decode %s: %w", rel, err)
	}
	var names []string
	for _, item := range schema.OneOf {
		if len(item.Properties.Method.Enum) == 0 {
			continue
		}
		names = append(names, item.Properties.Method.Enum[0])
	}
	sort.Strings(names)
	return names, nil
}

func aggregateMethodDiff(baseline, candidate string) (map[string]MethodDelta, error) {
	diff := make(map[string]MethodDelta, len(aggregateSchemas))
	for _, rel := range aggregateSchemas {
		before, err := aggregateMethodNames(baseline, rel)
		if err != nil {
			return nil, err
		}
		after, err := aggregateMethodNames(candidate, rel)
		if err != nil {
			return nil, err
		}
		diff[rel] = MethodDelta{
			Added:   sortedSetDiff(after, before),
			Removed: sortedSetDiff(before, after),
		}
	}
	return diff, nil
}

func sortedSetDiff(left, right []string) []string {
	inRight := map[string]bool{}
	for _, value := range right {
		inRight[value] = true
	}
	out := []string{}
	for _, value := range left {
		if !inRight[value] {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func refName(ref string) string {
	if ref == "" {
		return ""
	}
	parts := strings.Split(ref, "/")
	return parts[len(parts)-1]
}

func snapshotHashes(root string) (map[string]string, error) {
	hashes := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry == nil || entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		hashes[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	return hashes, err
}
