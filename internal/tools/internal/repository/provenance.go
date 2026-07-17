package repository

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const provenanceFilename = "migration-provenance.json"

var objectIDPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type provenanceManifest struct {
	FormatVersion int                `json:"format_version"`
	Imports       []provenanceImport `json:"imports"`
}

type provenanceImport struct {
	ID     string `json:"id"`
	Source struct {
		Repository  string `json:"repository"`
		Tag         string `json:"tag"`
		TagObject   string `json:"tag_object"`
		TagEvidence string `json:"tag_evidence"`
		Commit      string `json:"commit"`
		Tree        string `json:"tree"`
	} `json:"source"`
	Destination struct {
		Directory string `json:"directory"`
		Module    string `json:"module"`
		FirstTag  string `json:"first_tag"`
	} `json:"destination"`
	Relocation struct {
		Commit  string `json:"commit"`
		Parent  string `json:"parent"`
		Subtree string `json:"subtree"`
	} `json:"relocation"`
	Merge struct {
		Commit       string `json:"commit"`
		FirstParent  string `json:"first_parent"`
		SecondParent string `json:"second_parent"`
	} `json:"merge"`
}

type gitRepository interface {
	output(args ...string) (string, error)
}

type commandGit struct {
	root string
}

func (git commandGit) output(args ...string) (string, error) {
	commandArgs := append([]string{"-C", git.root}, args...)
	command := exec.Command("git", commandArgs...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func verifyProvenance(root string, registered registry, git gitRepository) []string {
	manifest, err := loadProvenance(root)
	if err != nil {
		return []string{err.Error()}
	}

	var violations []string
	if manifest.FormatVersion != 2 {
		violations = append(violations, fmt.Sprintf("%s: unsupported format_version %d", provenanceFilename, manifest.FormatVersion))
	}

	published := make(map[string]module)
	for _, candidate := range registered.Modules {
		if candidate.Published {
			published[candidate.ID] = candidate
		}
	}
	if len(manifest.Imports) != len(published) {
		violations = append(violations, fmt.Sprintf("%s: got %d imports, want exactly %d published modules", provenanceFilename, len(manifest.Imports), len(published)))
	}

	rootTags, err := git.output("tag", "--list", "v*")
	if err != nil {
		violations = append(violations, fmt.Sprintf("inspect root legacy tags: %v", err))
	} else if rootTags != "" {
		violations = append(violations, fmt.Sprintf("colliding root legacy tags are prohibited: %s", strings.Join(strings.Fields(rootTags), ", ")))
	}

	seenIDs := make(map[string]bool)
	for _, imported := range manifest.Imports {
		prefix := fmt.Sprintf("provenance import %q", imported.ID)
		registeredModule, ok := published[imported.ID]
		if !ok {
			violations = append(violations, fmt.Sprintf("%s has no published registry entry", prefix))
			continue
		}
		if seenIDs[imported.ID] {
			violations = append(violations, fmt.Sprintf("%s is duplicated", prefix))
			continue
		}
		seenIDs[imported.ID] = true

		violations = append(violations, validateProvenanceFields(root, prefix, registeredModule, imported)...)
		violations = append(violations, validateProvenanceGit(prefix, imported, git)...)
	}
	for id := range published {
		if !seenIDs[id] {
			violations = append(violations, fmt.Sprintf("%s: missing import for published module %q", provenanceFilename, id))
		}
	}

	sort.Strings(violations)
	return violations
}

func loadProvenance(root string) (provenanceManifest, error) {
	data, err := os.ReadFile(filepath.Join(root, provenanceFilename))
	if err != nil {
		return provenanceManifest{}, fmt.Errorf("read %s: %w", provenanceFilename, err)
	}
	var manifest provenanceManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return provenanceManifest{}, fmt.Errorf("decode %s: %w", provenanceFilename, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return provenanceManifest{}, fmt.Errorf("decode %s: trailing JSON value", provenanceFilename)
		}
		return provenanceManifest{}, fmt.Errorf("decode %s: %w", provenanceFilename, err)
	}
	return manifest, nil
}

func validateProvenanceFields(root, prefix string, registeredModule module, imported provenanceImport) []string {
	var violations []string
	if imported.Source.Repository == "" || !strings.HasPrefix(imported.Source.Repository, "https://") {
		violations = append(violations, fmt.Sprintf("%s source repository must be an HTTPS URL", prefix))
	}
	if !isStableVersion(imported.Source.Tag) {
		violations = append(violations, fmt.Sprintf("%s source tag %q is not stable SemVer", prefix, imported.Source.Tag))
	}
	for field, value := range map[string]string{
		"source tag object":   imported.Source.TagObject,
		"source commit":       imported.Source.Commit,
		"source tree":         imported.Source.Tree,
		"relocation commit":   imported.Relocation.Commit,
		"relocation parent":   imported.Relocation.Parent,
		"relocation subtree":  imported.Relocation.Subtree,
		"merge commit":        imported.Merge.Commit,
		"merge first parent":  imported.Merge.FirstParent,
		"merge second parent": imported.Merge.SecondParent,
	} {
		if !objectIDPattern.MatchString(value) {
			violations = append(violations, fmt.Sprintf("%s %s %q is not a full Git object ID", prefix, field, value))
		}
	}
	if imported.Destination.Directory != registeredModule.Dir {
		violations = append(violations, fmt.Sprintf("%s destination %q does not match registry directory %q", prefix, imported.Destination.Directory, registeredModule.Dir))
	}
	if imported.Destination.Module == "" || !strings.HasSuffix(imported.Destination.Module, "/"+imported.Destination.Directory) {
		violations = append(violations, fmt.Sprintf("%s destination module %q does not end in /%s", prefix, imported.Destination.Module, imported.Destination.Directory))
	}
	firstTagPrefix := imported.Destination.Directory + "/"
	if !strings.HasPrefix(imported.Destination.FirstTag, firstTagPrefix) || !isStableVersion(strings.TrimPrefix(imported.Destination.FirstTag, firstTagPrefix)) {
		violations = append(violations, fmt.Sprintf("%s first tag %q does not use directory-prefixed stable SemVer", prefix, imported.Destination.FirstTag))
	}
	if imported.Relocation.Parent != imported.Source.Commit {
		violations = append(violations, fmt.Sprintf("%s relocation parent does not equal source commit", prefix))
	}
	if imported.Relocation.Subtree != imported.Source.Tree {
		violations = append(violations, fmt.Sprintf("%s relocation subtree does not equal source tree", prefix))
	}
	if imported.Merge.SecondParent != imported.Relocation.Commit {
		violations = append(violations, fmt.Sprintf("%s merge second parent does not equal relocation commit", prefix))
	}
	violations = append(violations, validateTagEvidence(root, prefix, imported)...)
	return violations
}

func validateTagEvidence(root, prefix string, imported provenanceImport) []string {
	evidence := filepath.ToSlash(filepath.Clean(imported.Source.TagEvidence))
	if imported.Source.TagEvidence == "" || evidence != imported.Source.TagEvidence || filepath.IsAbs(imported.Source.TagEvidence) || !strings.HasPrefix(evidence, "docs/migration/tag-objects/") {
		return []string{fmt.Sprintf("%s tag evidence path %q is invalid", prefix, imported.Source.TagEvidence)}
	}
	payload, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(evidence)))
	if err != nil {
		return []string{fmt.Sprintf("%s read tag evidence: %v", prefix, err)}
	}
	gotObject := tagObjectID(payload)
	var violations []string
	if gotObject != imported.Source.TagObject {
		violations = append(violations, fmt.Sprintf("%s tag evidence hashes to %s, manifest records %s", prefix, gotObject, imported.Source.TagObject))
	}

	headers := payload
	if separator := bytes.Index(payload, []byte("\n\n")); separator >= 0 {
		headers = payload[:separator]
	}
	values := make(map[string]string)
	for _, line := range strings.Split(string(headers), "\n") {
		name, value, ok := strings.Cut(line, " ")
		if ok {
			values[name] = value
		}
	}
	if values["type"] != "commit" {
		violations = append(violations, fmt.Sprintf("%s tag evidence type is %q, want commit", prefix, values["type"]))
	}
	if values["tag"] != imported.Source.Tag {
		violations = append(violations, fmt.Sprintf("%s tag evidence names %q, manifest records %q", prefix, values["tag"], imported.Source.Tag))
	}
	if values["object"] != imported.Source.Commit {
		violations = append(violations, fmt.Sprintf("%s tag evidence targets %q, manifest records %q", prefix, values["object"], imported.Source.Commit))
	}
	return violations
}

func tagObjectID(payload []byte) string {
	hasher := sha1.New() // Git SHA-1 object identity, not a security digest.
	_, _ = fmt.Fprintf(hasher, "tag %d%c", len(payload), byte(0))
	_, _ = hasher.Write(payload)
	return fmt.Sprintf("%x", hasher.Sum(nil))
}

func validateProvenanceGit(prefix string, imported provenanceImport, git gitRepository) []string {
	var violations []string
	sourceTree, err := git.output("rev-parse", imported.Source.Commit+"^{tree}")
	if err != nil {
		return append(violations, fmt.Sprintf("%s source commit is not reachable: %v", prefix, err))
	}
	if sourceTree != imported.Source.Tree {
		violations = append(violations, fmt.Sprintf("%s source tree is %s, manifest records %s", prefix, sourceTree, imported.Source.Tree))
	}

	relocationParents, err := git.output("show", "-s", "--format=%P", imported.Relocation.Commit)
	if err != nil {
		violations = append(violations, fmt.Sprintf("%s relocation commit is not reachable: %v", prefix, err))
	} else if relocationParents != imported.Relocation.Parent {
		violations = append(violations, fmt.Sprintf("%s relocation parents are %q, want exactly %q", prefix, relocationParents, imported.Relocation.Parent))
	}

	relocationTree, err := git.output("rev-parse", imported.Relocation.Commit+":"+imported.Destination.Directory)
	if err != nil {
		violations = append(violations, fmt.Sprintf("%s relocation destination is missing: %v", prefix, err))
	} else if relocationTree != imported.Relocation.Subtree {
		violations = append(violations, fmt.Sprintf("%s relocated subtree is %s, want %s", prefix, relocationTree, imported.Relocation.Subtree))
	}
	violations = append(violations, validateRelocationPrefix(prefix, imported, git)...)

	mergeParents, err := git.output("show", "-s", "--format=%P", imported.Merge.Commit)
	wantMergeParents := imported.Merge.FirstParent + " " + imported.Merge.SecondParent
	if err != nil {
		violations = append(violations, fmt.Sprintf("%s merge commit is not reachable: %v", prefix, err))
	} else if mergeParents != wantMergeParents {
		violations = append(violations, fmt.Sprintf("%s merge parents are %q, want %q", prefix, mergeParents, wantMergeParents))
	}
	mergeTree, err := git.output("rev-parse", imported.Merge.Commit+":"+imported.Destination.Directory)
	if err != nil {
		violations = append(violations, fmt.Sprintf("%s merge destination is missing: %v", prefix, err))
	} else if mergeTree != imported.Relocation.Subtree {
		violations = append(violations, fmt.Sprintf("%s merge subtree is %s, want %s", prefix, mergeTree, imported.Relocation.Subtree))
	}
	if _, err := git.output("merge-base", "--is-ancestor", imported.Source.Commit, "HEAD"); err != nil {
		violations = append(violations, fmt.Sprintf("%s source commit is not an ancestor of HEAD: %v", prefix, err))
	}
	if _, err := git.output("merge-base", "--is-ancestor", imported.Relocation.Commit, "HEAD"); err != nil {
		violations = append(violations, fmt.Sprintf("%s relocation commit is not an ancestor of HEAD: %v", prefix, err))
	}
	if _, err := git.output("merge-base", "--is-ancestor", imported.Merge.Commit, "HEAD"); err != nil {
		violations = append(violations, fmt.Sprintf("%s merge commit is not an ancestor of HEAD: %v", prefix, err))
	}
	return violations
}

func validateRelocationPrefix(prefix string, imported provenanceImport, git gitRepository) []string {
	parts := strings.Split(imported.Destination.Directory, "/")
	var violations []string
	var parent string
	for _, part := range parts {
		object := imported.Relocation.Commit
		if parent != "" {
			object += ":" + parent
		}
		entries, err := git.output("ls-tree", "--name-only", object)
		if err != nil {
			return append(violations, fmt.Sprintf("%s relocation prefix %q cannot be read: %v", prefix, parent, err))
		}
		if entries != part {
			return append(violations, fmt.Sprintf("%s relocation prefix %q contains %q, want only %q", prefix, parent, entries, part))
		}
		if parent == "" {
			parent = part
		} else {
			parent += "/" + part
		}
	}
	return violations
}
