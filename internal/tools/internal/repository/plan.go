package repository

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const affectedPlanFormatVersion = 1

// AffectedPlan is the deterministic contract between repository discovery and
// the GitHub Actions matrix. Module relationships come from the modules'
// go.mod files; the registry supplies identity and location only.
type AffectedPlan struct {
	FormatVersion     int              `json:"format_version"`
	Base              string           `json:"base"`
	Head              string           `json:"head"`
	ChangedPaths      []string         `json:"changed_paths"`
	Modules           []AffectedModule `json:"modules"`
	PublicModules     []AffectedModule `json:"public_modules"`
	WorkspaceRequired bool             `json:"workspace_required"`
}

type AffectedModule struct {
	ID        string `json:"id"`
	Dir       string `json:"dir"`
	Published bool   `json:"published"`
	MinimumGo string `json:"minimum_go"`
}

// Affected computes the changed module closure between two commits.
func Affected(root, base, head string) (AffectedPlan, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return AffectedPlan{}, fmt.Errorf("resolve repository root: %w", err)
	}
	registered, err := loadPopulatedRegistry(root)
	if err != nil {
		return AffectedPlan{}, err
	}
	resolvedHead, err := resolveCommit(root, head)
	if err != nil {
		return AffectedPlan{}, fmt.Errorf("resolve head: %w", err)
	}

	var resolvedBase string
	var paths []string
	if base == "" || isZeroObjectID(base) {
		paths, err = trackedPaths(root, resolvedHead)
	} else {
		resolvedBase, err = resolveCommit(root, base)
		if err == nil {
			paths, err = changedPaths(root, resolvedBase, resolvedHead)
		}
	}
	if err != nil {
		return AffectedPlan{}, err
	}

	return affectedPlan(registered, resolvedBase, resolvedHead, paths), nil
}

func affectedPlan(registered registry, base, head string, paths []string) AffectedPlan {
	paths = normalizedPaths(paths)
	affected := map[string]bool{}
	rootChange := false
	for _, changed := range paths {
		owner := moduleForPath(registered, changed)
		if owner == "" {
			rootChange = true
			break
		}
		affected[owner] = true
	}
	if rootChange {
		for _, candidate := range registered.Modules {
			affected[candidate.ID] = true
		}
	}

	owners := make(map[string]string, len(registered.Modules))
	for _, candidate := range registered.Modules {
		owners[candidate.path] = candidate.ID
	}
	reverse := map[string][]string{}
	for _, candidate := range registered.Modules {
		for _, required := range candidate.requires {
			if dependency := ownerForImport(required, owners); dependency != "" && dependency != candidate.ID {
				reverse[dependency] = append(reverse[dependency], candidate.ID)
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for dependency, consumers := range reverse {
			if !affected[dependency] {
				continue
			}
			for _, consumer := range consumers {
				if !affected[consumer] {
					affected[consumer] = true
					changed = true
				}
			}
		}
	}

	plan := AffectedPlan{
		FormatVersion: affectedPlanFormatVersion,
		Base:          base,
		Head:          head,
		ChangedPaths:  paths,
		Modules:       make([]AffectedModule, 0),
		PublicModules: make([]AffectedModule, 0),
	}
	for _, candidate := range registered.Modules {
		if !affected[candidate.ID] {
			continue
		}
		entry := AffectedModule{
			ID:        candidate.ID,
			Dir:       candidate.Dir,
			Published: candidate.Published,
			MinimumGo: candidate.goVersion,
		}
		plan.Modules = append(plan.Modules, entry)
		if entry.Published {
			plan.PublicModules = append(plan.PublicModules, entry)
		}
	}
	plan.WorkspaceRequired = len(plan.PublicModules) != 0
	return plan
}

func (plan AffectedPlan) Validate() error {
	if plan.FormatVersion != affectedPlanFormatVersion {
		return fmt.Errorf("unsupported affected plan format_version %d", plan.FormatVersion)
	}
	if plan.Head == "" {
		return fmt.Errorf("affected plan has no head commit")
	}
	if !sort.StringsAreSorted(plan.ChangedPaths) {
		return fmt.Errorf("affected plan changed_paths are not sorted")
	}
	seen := map[string]bool{}
	lastID := ""
	for _, candidate := range plan.Modules {
		if candidate.ID == "" || candidate.Dir == "" || candidate.MinimumGo == "" {
			return fmt.Errorf("affected plan contains incomplete module entry")
		}
		if seen[candidate.ID] {
			return fmt.Errorf("affected plan contains duplicate module %q", candidate.ID)
		}
		if lastID != "" && candidate.ID < lastID {
			return fmt.Errorf("affected plan modules are not sorted")
		}
		seen[candidate.ID] = true
		lastID = candidate.ID
	}
	seenPublic := map[string]bool{}
	for _, candidate := range plan.PublicModules {
		if !candidate.Published || !seen[candidate.ID] {
			return fmt.Errorf("affected plan contains invalid public module %q", candidate.ID)
		}
		if seenPublic[candidate.ID] {
			return fmt.Errorf("affected plan contains duplicate public module %q", candidate.ID)
		}
		seenPublic[candidate.ID] = true
	}
	if plan.WorkspaceRequired != (len(plan.PublicModules) != 0) {
		return fmt.Errorf("affected plan workspace_required disagrees with public_modules")
	}
	return nil
}

func ReadAffectedPlan(path string) (AffectedPlan, error) {
	var plan AffectedPlan
	if err := readStrictJSON(path, &plan); err != nil {
		return AffectedPlan{}, fmt.Errorf("read affected plan: %w", err)
	}
	if err := plan.Validate(); err != nil {
		return AffectedPlan{}, err
	}
	return plan, nil
}

func WriteAffectedPlan(path string, plan AffectedPlan) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	return writeJSON(path, plan)
}

func moduleForPath(registered registry, changed string) string {
	bestID := ""
	bestLength := -1
	for _, candidate := range registered.Modules {
		if changed == candidate.Dir || strings.HasPrefix(changed, candidate.Dir+"/") {
			if len(candidate.Dir) > bestLength {
				bestID = candidate.ID
				bestLength = len(candidate.Dir)
			}
		}
	}
	return bestID
}

func normalizedPaths(paths []string) []string {
	unique := map[string]bool{}
	for _, path := range paths {
		path = filepath.ToSlash(filepath.Clean(path))
		if path != "." && path != "" && !strings.HasPrefix(path, "../") {
			unique[path] = true
		}
	}
	result := make([]string, 0, len(unique))
	for path := range unique {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func resolveCommit(root, revision string) (string, error) {
	if revision == "" {
		revision = "HEAD"
	}
	output, err := gitOutput(root, "rev-parse", "--verify", revision+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func changedPaths(root, base, head string) ([]string, error) {
	// Disable rename collapsing so a cross-module move affects both the source
	// and destination owners.
	output, err := gitBytes(root, "diff", "--no-renames", "--name-only", "-z", base, head, "--")
	if err != nil {
		return nil, fmt.Errorf("list changed paths: %w", err)
	}
	return splitNUL(output), nil
}

func trackedPaths(root, head string) ([]string, error) {
	output, err := gitBytes(root, "ls-tree", "-r", "--name-only", "-z", head)
	if err != nil {
		return nil, fmt.Errorf("list tracked paths: %w", err)
	}
	return splitNUL(output), nil
}

func gitOutput(root string, args ...string) (string, error) {
	output, err := gitBytes(root, args...)
	return string(output), err
}

func gitBytes(root string, args ...string) ([]byte, error) {
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func splitNUL(data []byte) []string {
	parts := bytes.Split(data, []byte{0})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) != 0 {
			result = append(result, string(part))
		}
	}
	return result
}

func isZeroObjectID(value string) bool {
	if value == "" {
		return false
	}
	return strings.Trim(value, "0") == ""
}

func readStrictJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return err
	}
	return nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if path == "-" {
		_, err = os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
