package protocolsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// runVerifierAcceptance compares the complete permanent acceptance boundary.
// Only disposable views receive the exact target schemas; neither source tree,
// generated SDK, nor the retained upgrade proposal is changed or executed.
func runVerifierAcceptance(ctx context.Context, oldRoot, newRoot, candidate string) ([]verifierProofLane, bool, error) {
	return verifierAcceptanceWithGate(ctx, oldRoot, newRoot, candidate, runVerifierSourceGate)
}

type verifierAcceptanceGate func(context.Context, string, string, string, map[string]bool) (verifierProofLane, error)

func verifierAcceptanceWithGate(ctx context.Context, oldRoot, newRoot, candidate string, gate verifierAcceptanceGate) ([]verifierProofLane, bool, error) {
	var lanes []verifierProofLane
	oldTests, err := selectedExecutableTests(filepath.Join(oldRoot, "codexsdk"), []string{"internal/schemaoracle"})
	if err != nil {
		return lanes, false, err
	}
	newTests, err := selectedExecutableTests(filepath.Join(newRoot, "codexsdk"), []string{"internal/schemaoracle"})
	if err != nil {
		return lanes, false, err
	}
	// Names preserve execution obligations, not the old test bodies or fixtures.
	for name := range oldTests {
		newTests[name] = true
	}
	digest, err := verifierAcceptanceSchemaDigest(candidate)
	if err != nil {
		return lanes, false, err
	}
	temp, err := os.MkdirTemp("", "verifier-acceptance-")
	if err != nil {
		return lanes, false, err
	}
	defer os.RemoveAll(temp)
	oldRed := false
	for _, version := range []string{"old", "new"} {
		source, expected := oldRoot, oldTests
		if version == "new" {
			source, expected = newRoot, newTests
		}
		patch, err := repairPatch(source)
		if err != nil {
			return lanes, false, err
		}
		for _, view := range []string{"baseline", "target"} {
			if err := ctx.Err(); err != nil {
				return lanes, false, err
			}
			root := filepath.Join(temp, version+"-"+view)
			if err := runGit(source, "clone", "--quiet", "--no-hardlinks", source, root); err != nil {
				return lanes, false, err
			}
			if len(patch) > 0 {
				if err := applyProposalPatchBytes(root, patch); err != nil {
					return lanes, false, err
				}
				if err := CheckScope(root, "verifier"); err != nil {
					return lanes, false, err
				}
			}
			if view == "target" {
				if err := verifierAcceptanceTarget(root, candidate); err != nil {
					return lanes, false, err
				}
			}
			if err := os.MkdirAll(filepath.Join(root, "codexsdk/.cache/openai-codex"), 0700); err != nil {
				return lanes, false, err
			}
			lane, gateErr := gate(ctx, root, candidate, "permanent-tests", expected)
			lane.Case, lane.Lane = "acceptance", version+"-"+view
			lanes = append(lanes, lane)
			current, err := verifierAcceptanceSchemaDigest(candidate)
			if err != nil {
				return lanes, false, err
			}
			if current != digest {
				return lanes, false, fmt.Errorf("verifier acceptance target source changed")
			}
			after, err := repairPatch(source)
			if err != nil {
				return lanes, false, err
			}
			if repairDigest(after) != repairDigest(patch) {
				return lanes, false, fmt.Errorf("verifier acceptance source tree changed")
			}
			if gateErr != nil {
				var assertion *verifierAssertionError
				if version == "old" && view == "target" && lane.Started && lane.Failed && !lane.Passed && errors.As(gateErr, &assertion) {
					oldRed = true
					continue
				}
				return lanes, false, gateErr
			}
			if !lane.Passed {
				return lanes, false, fmt.Errorf("verifier acceptance lacks passing execution evidence")
			}
		}
	}
	return lanes, oldRed, nil
}

// Metadata belongs to provenance, not the raw schema view. These are the same
// non-schema projections preserved by protocolupgrade's schema copy operation.
func verifierAcceptanceMetadata(name string) bool {
	switch name {
	case "baseline_metadata.json", "coverage_matrix.json", "drift_report.json", "manifest.json", "manifest_generation.json", "matrix_update_skeleton.json":
		return true
	}
	return false
}

func verifierAcceptanceSchemaFiles(root string, target bool) ([]string, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("verifier acceptance schema root is not a directory")
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("verifier acceptance source is not regular")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasSuffix(rel, ".json") || verifierAcceptanceMetadata(filepath.Base(rel)) {
			if target {
				return fmt.Errorf("verifier acceptance target contains non-schema path %s", rel)
			}
			return nil
		}
		if !isMechanicalPath(mechanicalPrefix + "/" + rel) {
			return fmt.Errorf("verifier acceptance schema path outside mechanical scope")
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if target && len(files) == 0 {
		return nil, fmt.Errorf("verifier acceptance target schema is empty")
	}
	return files, nil
}

func verifierAcceptanceSchemaDigest(candidate string) (string, error) {
	root := filepath.Join(candidate, "schema")
	files, err := verifierAcceptanceSchemaFiles(root, true)
	if err != nil {
		return "", err
	}
	var identity strings.Builder
	for _, rel := range files {
		raw, err := verifierProofReadSchema(root, rel)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&identity, "%d:%s:%s\n", len(rel), rel, repairDigest(raw))
	}
	return repairDigest([]byte(identity.String())), nil
}

func verifierAcceptanceTarget(root, candidate string) error {
	from := filepath.Join(candidate, "schema")
	files, err := verifierAcceptanceSchemaFiles(from, true)
	if err != nil {
		return err
	}
	destination := filepath.Join(root, mechanicalPrefix)
	if err := os.MkdirAll(destination, 0755); err != nil {
		return err
	}
	previous, err := verifierAcceptanceSchemaFiles(destination, false)
	if err != nil {
		return err
	}
	for _, rel := range previous {
		if err := os.Remove(filepath.Join(destination, filepath.FromSlash(rel))); err != nil {
			return err
		}
	}
	for _, rel := range files {
		raw, err := verifierProofReadSchema(from, rel)
		if err != nil {
			return err
		}
		path := filepath.Join(destination, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(path, raw, 0644); err != nil {
			return err
		}
	}
	return nil
}
