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
	if len(violations) == 0 {
		return nil
	}

	sort.Strings(violations)
	return fmt.Errorf("repository contract violations:\n- %s", strings.Join(violations, "\n- "))
}
