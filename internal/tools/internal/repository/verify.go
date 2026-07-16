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
	violations = append(violations, verifyArchivalEvidence(absoluteRoot)...)
	if len(violations) == 0 {
		return nil
	}

	sort.Strings(violations)
	return fmt.Errorf("repository contract violations:\n- %s", strings.Join(violations, "\n- "))
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
