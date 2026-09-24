package generatedcheck

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

// Request is optional expected baseline identity a caller already believes.
type Request struct {
	ModuleRoot             string
	ExpectedUpstreamCommit string
	ExpectedUpstreamRef    string
	ExpectedUpstreamKind   string
}

type baselineMetadata struct {
	SourceCommit  string `json:"source_commit"`
	SourceRefName string `json:"source_ref_name"`
	SourceRefKind string `json:"source_ref_kind"`
}

// Check regenerates owned protocol/SDK files and compares them with the
// checked-in outputs. It also validates immutable upstream identity and
// rejects local/cache path leakage.
func Check(req Request) error {
	root := req.ModuleRoot
	if root == "" {
		root = "."
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}

	metadata, err := loadBaselineMetadata(filepath.Join(root, baselineRel, "baseline_metadata.json"))
	if err != nil {
		return err
	}
	if req.ExpectedUpstreamCommit != "" && metadata.SourceCommit != req.ExpectedUpstreamCommit {
		return fmt.Errorf("baseline source_commit=%s, want %s", metadata.SourceCommit, req.ExpectedUpstreamCommit)
	}
	if req.ExpectedUpstreamRef != "" && metadata.SourceRefName != req.ExpectedUpstreamRef {
		return fmt.Errorf("baseline source_ref_name=%s, want %s", metadata.SourceRefName, req.ExpectedUpstreamRef)
	}
	if req.ExpectedUpstreamKind != "" && metadata.SourceRefKind != req.ExpectedUpstreamKind {
		return fmt.Errorf("baseline source_ref_kind=%s, want %s", metadata.SourceRefKind, req.ExpectedUpstreamKind)
	}

	leaks, err := scanBaselinePathLeaks(filepath.Join(root, baselineRel))
	if err != nil {
		return err
	}
	if len(leaks) > 0 {
		return fmt.Errorf("checked-in protocol baseline contains local or cache paths:\n%s", strings.Join(leaks, "\n"))
	}

	generated, err := generateArtifacts(root)
	if err != nil {
		return err
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

	var mismatches []string
	for _, item := range checked {
		want, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(item.rel)))
		if err != nil {
			return err
		}
		if !bytes.Equal(want, item.data) {
			mismatches = append(mismatches, mismatchDiagnostic(item.rel, want, item.data))
		}
	}
	if len(mismatches) > 0 {
		return fmt.Errorf("generated artifacts do not match checked-in outputs: %s", strings.Join(mismatches, "; "))
	}
	return nil
}

// WriteArtifacts regenerates protocol artifacts and writes them. It does not
// compare against checked-in files.
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
// without writing them.
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
	diagnostic := fmt.Sprintf("%s mismatch: checked-in sha256=%x generated sha256=%x", rel, sha256.Sum256(want), sha256.Sum256(got))
	if line, wantLine, gotLine, ok := firstTextMismatch(want, got); ok {
		diagnostic += fmt.Sprintf("; first difference line %d: checked-in=%q generated=%q", line, wantLine, gotLine)
	}
	return diagnostic
}

func firstTextMismatch(want, got []byte) (int, string, string, bool) {
	wantLines := strings.Split(string(want), "\n")
	gotLines := strings.Split(string(got), "\n")
	limit := len(wantLines)
	if len(gotLines) < limit {
		limit = len(gotLines)
	}
	for index := 0; index < limit; index++ {
		if wantLines[index] != gotLines[index] {
			return index + 1, wantLines[index], gotLines[index], true
		}
	}
	if len(wantLines) == len(gotLines) {
		return 0, "", "", false
	}
	if len(wantLines) > limit {
		return limit + 1, wantLines[limit], "<missing>", true
	}
	return limit + 1, "<missing>", gotLines[limit], true
}
