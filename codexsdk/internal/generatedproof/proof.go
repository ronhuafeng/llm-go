package generatedproof

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolgen"
)

const (
	baselineRel     = "internal/protocolschema/appserver/v2"
	methodRegistry  = "protocolv2/method_registry.gen.go"
	protocolTypes   = "protocolv2/protocol_types.gen.go"
	experimentalMem = "protocolv2/experimental_members.gen.go"
	sdkSurface      = "sdk_surface.gen.go"
)

var (
	sourceCommitRE = regexp.MustCompile(`^[0-9a-f]{40}$`)
	pathLeakRE     = regexp.MustCompile(`(/Users/|/home/)`)
	cacheMarkers   = []string{".cache/codexsdk-upstream", ".cache/openai-codex"}
)

// Request is the observed inputs for one generated-artifact proof.
type Request struct {
	ModuleRoot             string
	ExpectedUpstreamCommit string
	UpstreamRef            string
	RepositoryCommit       string
	WriteArtifacts         bool
}

// Artifact names one compared generated file.
type Artifact struct {
	Path         string `json:"path"`
	Reproducible bool   `json:"reproducible"`
	Diagnostic   string `json:"diagnostic,omitempty"`
}

// Result is the machine-readable proof. Unobserved fields are omitted.
type Result struct {
	RepositoryCommit               string     `json:"repository_commit,omitempty"`
	UpstreamRef                    string     `json:"upstream_ref,omitempty"`
	UpstreamCommit                 string     `json:"upstream_commit,omitempty"`
	GeneratedArtifactsReproducible bool       `json:"generated_artifacts_reproducible"`
	BaselineCommitMatches          *bool      `json:"baseline_commit_matches,omitempty"`
	BaselinePathLeak               bool       `json:"baseline_path_leak"`
	Artifacts                      []Artifact `json:"artifacts"`
}

type baselineMetadata struct {
	SourceCommit  string `json:"source_commit"`
	SourceRefName string `json:"source_ref_name"`
}

// Prove regenerates checked-in protocol artifacts and compares them to disk.
func Prove(req Request) (Result, error) {
	root := req.ModuleRoot
	if root == "" {
		root = "."
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Result{}, err
	}

	result := Result{
		RepositoryCommit: req.RepositoryCommit,
		UpstreamRef:      req.UpstreamRef,
	}

	metadata, err := loadBaselineMetadata(filepath.Join(root, baselineRel, "baseline_metadata.json"))
	if err != nil {
		return result, err
	}
	result.UpstreamCommit = metadata.SourceCommit
	if result.UpstreamRef == "" {
		result.UpstreamRef = metadata.SourceRefName
	}

	if req.ExpectedUpstreamCommit != "" {
		matches := metadata.SourceCommit == req.ExpectedUpstreamCommit
		result.BaselineCommitMatches = &matches
		if !matches {
			return result, fmt.Errorf("baseline source_commit=%s, want %s", metadata.SourceCommit, req.ExpectedUpstreamCommit)
		}
	}

	leaks, err := scanBaselinePathLeaks(filepath.Join(root, baselineRel))
	if err != nil {
		return result, err
	}
	if len(leaks) > 0 {
		result.BaselinePathLeak = true
		return result, fmt.Errorf("checked-in protocol baseline contains local or cache paths:\n%s", strings.Join(leaks, "\n"))
	}

	generated, err := generateArtifacts(root)
	if err != nil {
		return result, err
	}

	checked := []struct {
		rel  string
		data []byte
	}{
		{methodRegistry, generated.methodRegistry},
		{protocolTypes, generated.protocolTypes},
		{experimentalMem, generated.experimentalMembers},
		{sdkSurface, generated.sdkSurface},
	}

	allMatch := true
	for _, item := range checked {
		want, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(item.rel)))
		if err != nil {
			return result, err
		}
		match := bytes.Equal(want, item.data)
		artifact := Artifact{Path: item.rel, Reproducible: match}
		if !match {
			allMatch = false
			artifact.Diagnostic = mismatchDiagnostic(item.rel, want, item.data)
		}
		result.Artifacts = append(result.Artifacts, artifact)
		if req.WriteArtifacts {
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(item.rel)), item.data, 0o644); err != nil {
				return result, err
			}
		}
	}
	result.GeneratedArtifactsReproducible = allMatch
	if !allMatch {
		return result, fmt.Errorf("generated artifacts do not match checked-in outputs")
	}
	return result, nil
}

type generatedSet struct {
	methodRegistry      []byte
	protocolTypes       []byte
	experimentalMembers []byte
	sdkSurface          []byte
}

func generateArtifacts(moduleRoot string) (generatedSet, error) {
	schemaRoot := filepath.Join(moduleRoot, filepath.FromSlash(baselineRel))
	manifestPath := filepath.Join(schemaRoot, "manifest.json")
	typePlan, err := protocolgen.BuildProtocolTypePlan(schemaRoot)
	if err != nil {
		return generatedSet{}, err
	}
	manifest, err := protocolgen.LoadManifest(manifestPath)
	if err != nil {
		return generatedSet{}, err
	}
	if err := protocolgen.ApplyWireMessageRoles(&typePlan, manifest); err != nil {
		return generatedSet{}, err
	}
	protocolTypesSource, err := protocolgen.GenerateProtocolTypes(typePlan)
	if err != nil {
		return generatedSet{}, err
	}
	methodRegistrySource, err := protocolgen.GenerateMethodRegistry(manifest)
	if err != nil {
		return generatedSet{}, err
	}
	experimentalMembers, err := protocolgen.GenerateExperimentalMembers(manifest)
	if err != nil {
		return generatedSet{}, err
	}
	sdkSurfaceSource, err := GenerateSDKSurface(manifest, methodRegistrySource, protocolTypesSource)
	if err != nil {
		return generatedSet{}, err
	}
	return generatedSet{
		methodRegistry:      methodRegistrySource,
		protocolTypes:       protocolTypesSource,
		experimentalMembers: experimentalMembers,
		sdkSurface:          sdkSurfaceSource,
	}, nil
}

func loadBaselineMetadata(path string) (baselineMetadata, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return baselineMetadata{}, err
	}
	var metadata baselineMetadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return baselineMetadata{}, fmt.Errorf("decode baseline metadata: %w", err)
	}
	if !sourceCommitRE.MatchString(metadata.SourceCommit) {
		return baselineMetadata{}, fmt.Errorf("baseline source_commit %q is not a full git sha", metadata.SourceCommit)
	}
	return metadata, nil
}

func scanBaselinePathLeaks(root string) ([]string, error) {
	var leaks []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(raw)
		rel, _ := filepath.Rel(root, path)
		if pathLeakRE.MatchString(text) {
			leaks = append(leaks, filepath.ToSlash(rel)+": local filesystem path")
		}
		for _, marker := range cacheMarkers {
			if strings.Contains(text, marker) {
				leaks = append(leaks, filepath.ToSlash(rel)+": "+marker)
			}
		}
		return nil
	})
	return leaks, err
}

func mismatchDiagnostic(rel string, want, got []byte) string {
	return fmt.Sprintf("%s mismatch: checked-in sha256=%x generated sha256=%x", rel, sha256.Sum256(want), sha256.Sum256(got))
}
