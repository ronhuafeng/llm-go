package protocolupgrade

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/generatedcheck"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolgen"
)

func deriveSurface(stableSchema, completeSchema, moduleRoot string) ([]map[string]any, map[string][]byte, error) {
	handwrittenDir := filepath.Join(moduleRoot, "protocolv2")
	tmp, err := os.MkdirTemp("", "protocolupgrade-surface-")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(tmp)
	stableRoot := filepath.Join(tmp, "stable-schema")
	completeRoot := filepath.Join(tmp, "complete-schema")
	if err := prepareGenerationRoot(stableSchema, completeSchema, stableRoot); err != nil {
		return nil, nil, err
	}
	if err := prepareCompleteGenerationRoot(completeSchema, completeRoot); err != nil {
		return nil, nil, err
	}
	stableSource, err := generatePackage(stableRoot, handwrittenDir)
	if err != nil {
		return nil, nil, fmt.Errorf("generate stable protocol package: %w", err)
	}
	completeSource, err := generatePackage(completeRoot, handwrittenDir)
	if err != nil {
		return nil, nil, fmt.Errorf("generate complete protocol package: %w", err)
	}
	// Experimental member maps depend on the classified generated fields. First
	// classify the independent sources, then regenerate those maps from that
	// classification before recording their own exported signatures.
	preliminary, err := protocolgen.ClassifyExportedPackage([][]byte{stableSource.MethodRegistry, stableSource.ProtocolTypes}, [][]byte{completeSource.MethodRegistry, completeSource.ProtocolTypes})
	if err != nil {
		return nil, nil, err
	}
	stableManifest, err := protocolgen.LoadMethodFacts(filepath.Join(stableRoot, "manifest.json"))
	if err != nil {
		return nil, nil, err
	}
	completeManifest, err := protocolgen.LoadMethodFacts(filepath.Join(completeRoot, "manifest.json"))
	if err != nil {
		return nil, nil, err
	}
	stableManifest.Surface = preliminary
	completeManifest.Surface = preliminary
	stableSource.ExperimentalMembers, err = protocolgen.GenerateExperimentalMembers(stableManifest)
	if err != nil {
		return nil, nil, err
	}
	completeSource.ExperimentalMembers, err = protocolgen.GenerateExperimentalMembers(completeManifest)
	if err != nil {
		return nil, nil, err
	}
	surface, err := protocolgen.ClassifyExportedPackage([][]byte{stableSource.MethodRegistry, stableSource.ProtocolTypes, stableSource.ExperimentalMembers}, [][]byte{completeSource.MethodRegistry, completeSource.ProtocolTypes, completeSource.ExperimentalMembers})
	if err != nil {
		return nil, nil, err
	}
	out := make([]map[string]any, 0, len(surface))
	for _, entry := range surface {
		item := map[string]any{
			"kind":      string(entry.Kind),
			"name":      entry.Name,
			"signature": entry.Signature,
			"stability": string(entry.Stability),
		}
		if entry.Owner != "" {
			item["owner"] = entry.Owner
		}
		out = append(out, item)
	}
	completeManifest.Surface = surface
	files, err := generatedcheck.FilesFromPackage(moduleRoot, completeManifest, completeSource)
	if err != nil {
		return nil, nil, err
	}
	return out, files, nil
}

func prepareGenerationRoot(source, complete, destination string) error {
	if err := copyTreeFiles(source, destination); err != nil {
		return err
	}
	var coverage coverageFile
	if err := loadJSON(filepath.Join(complete, "coverage_matrix.json"), &coverage); err != nil {
		return err
	}
	schemas := map[string]bool{}
	files, err := schemaFiles(destination)
	if err != nil {
		return err
	}
	for _, rel := range files {
		schemas[rel] = true
	}
	filteredTypes := []map[string]any{}
	for _, item := range coverage.Types {
		schema, _ := item["schema"].(string)
		if schemas[schema] {
			filteredTypes = append(filteredTypes, item)
		}
	}
	coverage.Types = filteredTypes
	filteredFields := []map[string]any{}
	for _, item := range coverage.Fields {
		schema, _ := item["schema"].(string)
		path, _ := item["path"].(string)
		if schemas[schema] && pointerExists(destination, schema, path) {
			filteredFields = append(filteredFields, item)
		}
	}
	coverage.Fields = filteredFields
	if err := writeJSON(filepath.Join(destination, "coverage_matrix.json"), coverage); err != nil {
		return err
	}
	var manifest manifestFile
	if err := loadJSON(filepath.Join(complete, "manifest.json"), &manifest); err != nil {
		return err
	}
	stableMethods := map[string]bool{}
	for _, entry := range manifest.Entries {
		if entry.Stability == "stable" {
			stableMethods[entry.Method] = true
		}
	}
	filteredEntries := []manifestEntry{}
	for _, entry := range manifest.Entries {
		if stableMethods[entry.Method] {
			filteredEntries = append(filteredEntries, entry)
		}
	}
	manifest.Entries = filteredEntries
	manifest.Surface = nil
	return writeJSON(filepath.Join(destination, "manifest.json"), manifest)
}

func prepareCompleteGenerationRoot(source, destination string) error {
	if err := copyTreeFiles(source, destination); err != nil {
		return err
	}
	var manifest manifestFile
	if err := loadJSON(filepath.Join(destination, "manifest.json"), &manifest); err != nil {
		return err
	}
	manifest.Surface = nil
	return writeJSON(filepath.Join(destination, "manifest.json"), manifest)
}

func generatePackage(schemaRoot, handwrittenDir string) (protocolgen.ProtocolPackage, error) {
	plan, err := protocolgen.BuildProtocolTypePlan(schemaRoot)
	if err != nil {
		return protocolgen.ProtocolPackage{}, err
	}
	manifest, err := protocolgen.LoadMethodFacts(filepath.Join(schemaRoot, "manifest.json"))
	if err != nil {
		return protocolgen.ProtocolPackage{}, err
	}
	return protocolgen.BuildProtocolPackage(plan, manifest, handwrittenDir)
}

func copyTreeFiles(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFileBytes(path, target)
	})
}

func updateManifestSurface(manifest *manifestFile, surface []map[string]any) {
	manifest.Surface = surface
	if manifest.ClassificationSources == nil {
		manifest.ClassificationSources = map[string]any{}
	}
	manifest.ClassificationSources["generated_surface"] = "exported Go identities compared between generation without and with experimental schema visibility"
}

func facadeCompatibilitySurface(manifest manifestFile) []map[string]any {
	byAccessor := map[string][]map[string]any{}
	for _, entry := range manifest.Entries {
		if entry.Direction != "client_to_server" || entry.Kind != "request" {
			continue
		}
		match := facadeCaptureRE.FindStringSubmatch(entry.FacadeTarget)
		if match == nil || entry.FacadeStatus != "generated" {
			continue
		}
		accessor, operation := match[1], match[2]
		signature := "func(context.Context"
		if entry.ParamsOrPayloadSchema != "" {
			signature += ", protocolv2." + entry.ParamsOrPayloadSchema
		}
		signature += ") (protocolv2." + entry.ResponseType + ", error)"
		byAccessor[accessor] = append(byAccessor[accessor], map[string]any{
			"kind":      "method",
			"name":      "codexsdk." + accessor + "." + operation,
			"owner":     "codexsdk." + accessor,
			"signature": signature,
			"stability": entry.Stability,
		})
	}
	var surface []map[string]any
	for accessor, methods := range byAccessor {
		stabilities := map[string]bool{}
		for _, method := range methods {
			stabilities[method["stability"].(string)] = true
		}
		typeStability := "mixed"
		if len(stabilities) == 1 {
			for stability := range stabilities {
				typeStability = stability
			}
		}
		surface = append(surface, map[string]any{
			"kind":      "type",
			"name":      "codexsdk." + accessor,
			"owner":     "",
			"signature": "struct{/* opaque */}",
			"stability": typeStability,
		})
		surface = append(surface, methods...)
	}
	sort.Slice(surface, func(i, j int) bool {
		leftKind, _ := surface[i]["kind"].(string)
		rightKind, _ := surface[j]["kind"].(string)
		if leftKind != rightKind {
			return leftKind < rightKind
		}
		leftName, _ := surface[i]["name"].(string)
		rightName, _ := surface[j]["name"].(string)
		return leftName < rightName
	})
	return surface
}

func compatibilityReport(base, target manifestFile) map[string]any {
	before := surfaceIndex(base)
	after := surfaceIndex(target)
	var added, removed, reclassified, changed []map[string]any
	for _, key := range sortedSurfaceKeys(after) {
		if _, ok := before[key]; !ok {
			item := after[key]
			added = append(added, map[string]any{"kind": key.kind, "name": key.name, "classification": item.classification, "signature": item.signature})
		}
	}
	for _, key := range sortedSurfaceKeys(before) {
		if _, ok := after[key]; !ok {
			item := before[key]
			removed = append(removed, map[string]any{"kind": key.kind, "name": key.name, "classification": item.classification, "signature": item.signature})
		}
	}
	for _, key := range sortedSurfaceKeys(before) {
		afterItem, ok := after[key]
		if !ok {
			continue
		}
		beforeItem := before[key]
		if beforeItem.classification != afterItem.classification {
			reclassified = append(reclassified, map[string]any{
				"kind": key.kind,
				"name": key.name,
				"from": beforeItem.classification,
				"to":   afterItem.classification,
			})
		}
		if beforeItem.signature != afterItem.signature {
			changed = append(changed, map[string]any{
				"kind":           key.kind,
				"name":           key.name,
				"classification": beforeItem.classification,
				"from_signature": beforeItem.signature,
				"to_signature":   afterItem.signature,
			})
		}
	}
	var implementationObligations []map[string]any
	for _, item := range added {
		if item["kind"] == "interface_method" {
			implementationObligations = append(implementationObligations, item)
		}
	}
	supportRank := map[string]int{"experimental": 0, "mixed": 1, "stable": 2}
	var weakened, strengthened []map[string]any
	for _, item := range reclassified {
		from, _ := item["from"].(string)
		to, _ := item["to"].(string)
		if supportRank[to] < supportRank[from] {
			weakened = append(weakened, item)
		}
		if supportRank[to] > supportRank[from] {
			strengthened = append(strengthened, item)
		}
	}
	incompatible := len(removed) > 0 || len(changed) > 0 || len(implementationObligations) > 0 || len(weakened) > 0
	releaseImpact := "metadata-only"
	if incompatible {
		releaseImpact = "breaking"
	} else if len(added) > 0 || len(strengthened) > 0 {
		releaseImpact = "additive"
	}
	compatibilityImpact := "additive_or_metadata_only"
	if incompatible {
		compatibilityImpact = "incompatible"
	}
	if added == nil {
		added = []map[string]any{}
	}
	if removed == nil {
		removed = []map[string]any{}
	}
	if reclassified == nil {
		reclassified = []map[string]any{}
	}
	if changed == nil {
		changed = []map[string]any{}
	}
	if implementationObligations == nil {
		implementationObligations = []map[string]any{}
	}
	if weakened == nil {
		weakened = []map[string]any{}
	}
	if strengthened == nil {
		strengthened = []map[string]any{}
	}
	return map[string]any{
		"policy":                              "go_source_compatibility_with_classification_metadata",
		"compatibility_impact":                compatibilityImpact,
		"release_impact":                      releaseImpact,
		"added":                               added,
		"removed":                             removed,
		"reclassified":                        reclassified,
		"changed":                             changed,
		"external_implementation_obligations": implementationObligations,
		"weakened_support":                    weakened,
		"strengthened_support":                strengthened,
	}
}

type surfaceKey struct {
	kind string
	name string
}

type surfaceFact struct {
	classification string
	signature      string
}

func surfaceIndex(manifest manifestFile) map[surfaceKey]surfaceFact {
	result := map[surfaceKey]surfaceFact{}
	entries := append([]map[string]any{}, manifest.Surface...)
	entries = append(entries, facadeCompatibilitySurface(manifest)...)
	for _, raw := range entries {
		kind, _ := raw["kind"].(string)
		name, _ := raw["name"].(string)
		stability, _ := raw["stability"].(string)
		signature, _ := raw["signature"].(string)
		if kind == "" || name == "" || stability == "" || signature == "" {
			continue
		}
		result[surfaceKey{kind: kind, name: name}] = surfaceFact{classification: stability, signature: signature}
	}
	return result
}

func sortedSurfaceKeys(index map[surfaceKey]surfaceFact) []surfaceKey {
	keys := make([]surfaceKey, 0, len(index))
	for key := range index {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].kind != keys[j].kind {
			return keys[i].kind < keys[j].kind
		}
		return keys[i].name < keys[j].name
	})
	return keys
}

var facadeCaptureRE = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]*)\(\)\.([A-Za-z][A-Za-z0-9]*)$`)
