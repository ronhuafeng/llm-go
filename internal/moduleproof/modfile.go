package moduleproof

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

// Replace is one current-source module replacement.
type Replace struct {
	OldPath string
	NewPath string
}

// WriteVerifyModfile copies a committed module manifest into temporary files
// and applies current-source replacements without modifying the source files.
func WriteVerifyModfile(goModPath, goSumPath string, replaces []Replace, destMod, destSum string) error {
	if destMod == "" || destSum == "" {
		return fmt.Errorf("temporary module files are required")
	}
	raw, err := os.ReadFile(goModPath)
	if err != nil {
		return err
	}
	parsed, err := modfile.Parse(goModPath, raw, nil)
	if err != nil {
		return fmt.Errorf("parse %s: %w", goModPath, err)
	}
	for _, replacement := range replaces {
		if replacement.OldPath == "" || replacement.NewPath == "" {
			return fmt.Errorf("replace requires old and new paths")
		}
		if err := parsed.AddReplace(replacement.OldPath, "", replacement.NewPath, ""); err != nil {
			return fmt.Errorf("add replace %s => %s: %w", replacement.OldPath, replacement.NewPath, err)
		}
	}
	parsed.Cleanup()
	if err := os.MkdirAll(filepath.Dir(destMod), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(destMod, modfile.Format(parsed.Syntax), 0o644); err != nil {
		return err
	}
	sum, err := os.ReadFile(goSumPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destSum), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destSum, sum, 0o644)
}
