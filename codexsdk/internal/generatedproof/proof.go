package generatedproof

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
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

// Request is the expected identities a caller already believes. Prove observes
// git HEAD and baseline metadata itself and fails closed on mismatch.
type Request struct {
	ModuleRoot               string
	ExpectedRepositoryCommit string
	ExpectedUpstreamCommit   string
	ExpectedUpstreamRef      string
	ExpectedUpstreamKind     string
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
	RepositoryTree                 string     `json:"repository_tree,omitempty"`
	WorktreeOverlay                *bool      `json:"worktree_overlay,omitempty"`
	UpstreamRef                    string     `json:"upstream_ref,omitempty"`
	UpstreamRefKind                string     `json:"upstream_ref_kind,omitempty"`
	UpstreamCommit                 string     `json:"upstream_commit,omitempty"`
	GeneratedArtifactsReproducible *bool      `json:"generated_artifacts_reproducible,omitempty"`
	BaselineCommitMatches          *bool      `json:"baseline_commit_matches,omitempty"`
	BaselinePathLeak               *bool      `json:"baseline_path_leak,omitempty"`
	Artifacts                      []Artifact `json:"artifacts,omitempty"`
}

type baselineMetadata struct {
	SourceCommit  string `json:"source_commit"`
	SourceRefName string `json:"source_ref_name"`
	SourceRefKind string `json:"source_ref_kind"`
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

	result := Result{}
	if head, err := observeGitHEAD(root); err != nil {
		if req.ExpectedRepositoryCommit != "" {
			return result, fmt.Errorf("observe repository commit: %w", err)
		}
	} else {
		result.RepositoryCommit = head
		if tree, overlay, treeErr := observeWorktreeTree(root, head); treeErr != nil {
			return result, treeErr
		} else {
			result.RepositoryTree = tree
			result.WorktreeOverlay = &overlay
		}
		if req.ExpectedRepositoryCommit != "" && head != req.ExpectedRepositoryCommit {
			return result, fmt.Errorf("repository commit=%s, want %s", head, req.ExpectedRepositoryCommit)
		}
	}

	metadata, err := loadBaselineMetadata(filepath.Join(root, baselineRel, "baseline_metadata.json"))
	if err != nil {
		return result, err
	}
	result.UpstreamCommit = metadata.SourceCommit
	result.UpstreamRef = metadata.SourceRefName
	result.UpstreamRefKind = metadata.SourceRefKind

	if req.ExpectedUpstreamCommit != "" {
		matches := metadata.SourceCommit == req.ExpectedUpstreamCommit
		result.BaselineCommitMatches = &matches
		if !matches {
			return result, fmt.Errorf("baseline source_commit=%s, want %s", metadata.SourceCommit, req.ExpectedUpstreamCommit)
		}
	}
	if req.ExpectedUpstreamRef != "" && metadata.SourceRefName != req.ExpectedUpstreamRef {
		return result, fmt.Errorf("baseline source_ref_name=%s, want %s", metadata.SourceRefName, req.ExpectedUpstreamRef)
	}
	if req.ExpectedUpstreamKind != "" && metadata.SourceRefKind != req.ExpectedUpstreamKind {
		return result, fmt.Errorf("baseline source_ref_kind=%s, want %s", metadata.SourceRefKind, req.ExpectedUpstreamKind)
	}

	leaks, err := scanBaselinePathLeaks(filepath.Join(root, baselineRel))
	if err != nil {
		return result, err
	}
	leaked := len(leaks) > 0
	result.BaselinePathLeak = &leaked
	if leaked {
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
	}
	result.GeneratedArtifactsReproducible = &allMatch
	if !allMatch {
		return result, fmt.Errorf("generated artifacts do not match checked-in outputs: %s", mismatchSummary(result.Artifacts))
	}
	return result, nil
}

// WriteArtifacts regenerates protocol artifacts and writes them. It does not
// compare against checked-in files and is not a proof.
func WriteArtifacts(moduleRoot string) error {
	root := moduleRoot
	if root == "" {
		root = "."
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	generated, err := generateArtifacts(root)
	if err != nil {
		return err
	}
	files := []struct {
		rel  string
		data []byte
	}{
		{methodRegistry, generated.methodRegistry},
		{protocolTypes, generated.protocolTypes},
		{experimentalMem, generated.experimentalMembers},
		{sdkSurface, generated.sdkSurface},
	}
	for _, item := range files {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(item.rel)), item.data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

type generatedSet struct {
	methodRegistry      []byte
	protocolTypes       []byte
	experimentalMembers []byte
	sdkSurface          []byte
}

// RegeneratedFiles returns the canonical generated protocol artifacts for moduleRoot
// without writing them or observing git identity.
func RegeneratedFiles(moduleRoot string) (map[string][]byte, error) {
	generated, err := generateArtifacts(moduleRoot)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{
		methodRegistry:  generated.methodRegistry,
		protocolTypes:   generated.protocolTypes,
		experimentalMem: generated.experimentalMembers,
		sdkSurface:      generated.sdkSurface,
	}, nil
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
	if strings.TrimSpace(metadata.SourceRefName) == "" {
		return baselineMetadata{}, fmt.Errorf("baseline source_ref_name is empty")
	}
	if strings.TrimSpace(metadata.SourceRefKind) == "" {
		return baselineMetadata{}, fmt.Errorf("baseline source_ref_kind is empty")
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

func mismatchSummary(artifacts []Artifact) string {
	var parts []string
	for _, artifact := range artifacts {
		if !artifact.Reproducible && artifact.Diagnostic != "" {
			parts = append(parts, artifact.Diagnostic)
		}
	}
	if len(parts) == 0 {
		return "one or more artifacts differ"
	}
	return strings.Join(parts, "; ")
}

func observeWorktreeTree(dir, head string) (string, bool, error) {
	index, err := os.CreateTemp("", "generatedproof-index-")
	if err != nil {
		return "", false, err
	}
	indexPath := index.Name()
	index.Close()
	defer os.Remove(indexPath)
	env := append(os.Environ(), "GIT_INDEX_FILE="+indexPath)
	readTree := exec.Command("git", "-C", dir, "read-tree", head)
	readTree.Env = env
	if out, err := readTree.CombinedOutput(); err != nil {
		return "", false, fmt.Errorf("git read-tree: %w: %s", err, bytes.TrimSpace(out))
	}
	add := exec.Command("git", "-C", dir, "add", "-A")
	add.Env = env
	if out, err := add.CombinedOutput(); err != nil {
		return "", false, fmt.Errorf("git add -A for tree identity: %w: %s", err, bytes.TrimSpace(out))
	}
	writeTree := exec.Command("git", "-C", dir, "write-tree")
	writeTree.Env = env
	out, err := writeTree.Output()
	if err != nil {
		return "", false, fmt.Errorf("git write-tree: %w", err)
	}
	tree := strings.TrimSpace(string(out))
	headTreeCmd := exec.Command("git", "-C", dir, "rev-parse", head+"^{tree}")
	headTreeOut, err := headTreeCmd.Output()
	if err != nil {
		return "", false, fmt.Errorf("git rev-parse HEAD^{tree}: %w", err)
	}
	overlay := tree != strings.TrimSpace(string(headTreeOut))
	return tree, overlay, nil
}

func observeGitHEAD(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	sha := strings.TrimSpace(string(out))
	if !sourceCommitRE.MatchString(sha) {
		return "", fmt.Errorf("git HEAD %q is not a full git sha", sha)
	}
	return sha, nil
}
