package repository

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Verify checks the repository registry, workspace, ownership graph, and
// immutable migration provenance from the supplied repository root.
func Verify(root string) error {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}

	registered, err := loadRegistry(absoluteRoot)
	if err != nil {
		return err
	}

	var violations []string
	violations = append(violations, verifyArchitecture(absoluteRoot, &registered)...)
	violations = append(violations, verifyProvenance(absoluteRoot, registered, commandGit{root: absoluteRoot})...)
	violations = append(violations, verifyPendingFirstTagAPIInventories(absoluteRoot, registered)...)
	if len(violations) == 0 {
		return nil
	}

	sort.Strings(violations)
	return fmt.Errorf("repository contract violations:\n- %s", strings.Join(violations, "\n- "))
}

func verifyPendingFirstTagAPIInventories(root string, registered registry) []string {
	provenance, err := loadProvenance(root)
	if err != nil {
		return []string{err.Error()}
	}
	var violations []string
	for _, imported := range provenance.Imports {
		candidate, ok := findModule(registered, imported.ID)
		if !ok || candidate.path != imported.Destination.Module {
			continue
		}
		tags, err := gitOutput(root, "tag", "--list", imported.Destination.FirstTag)
		if err != nil {
			violations = append(violations, fmt.Sprintf("inspect first tag for module %s: %v", candidate.ID, err))
			continue
		}
		if strings.TrimSpace(tags) != "" {
			continue
		}
		inventoryPath, err := apiInventoryPath(candidate.ID)
		if err != nil {
			violations = append(violations, err.Error())
			continue
		}
		if _, err := validateFirstTagAPIInventory(root, candidate, inventoryPath); err != nil {
			violations = append(violations, fmt.Sprintf("module %s pending first-tag API inventory: %v", candidate.ID, err))
		}
	}
	return violations
}

func loadPopulatedRegistry(root string) (registry, error) {
	registered, err := loadRegistry(root)
	if err != nil {
		return registry{}, err
	}
	if violations := verifyRegisteredModules(root, &registered); len(violations) != 0 {
		sort.Strings(violations)
		return registry{}, fmt.Errorf("module registry violations:\n- %s", strings.Join(violations, "\n- "))
	}
	return registered, nil
}
