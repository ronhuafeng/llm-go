// Package livecodex owns the local and CI fixture for the real Codex suite.
package livecodex

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/semver"
)

const baselinePath = "codexsdk/internal/protocolschema/appserver/v2/baseline_metadata.json"

// Version selects an exact installable release from the tested source's baseline.
func Version(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, baselinePath))
	if err != nil {
		return "", errors.New("cannot read the tested revision's Codex baseline")
	}
	var baseline struct {
		Version string `json:"codex_version"`
		Ref     string `json:"source_ref_name"`
	}
	if json.Unmarshal(data, &baseline) != nil {
		return "", errors.New("invalid Codex baseline JSON")
	}
	version := strings.TrimPrefix(baseline.Version, "codex-cli ")
	if baseline.Version != "codex-cli "+version || semver.Canonical("v"+version) != "v"+version || baseline.Ref != "rust-v"+version {
		return "", errors.New("baseline must identify one consistent exact official Codex release")
	}
	return version, nil
}

// Required excludes known unrelated documentation and auxiliary verification.
// Unknown paths are conservative: they require the live suite.
func Required(paths []string) bool {
	for _, path := range paths {
		if strings.HasSuffix(path, ".md") {
			continue
		}
		switch path {
		case "LICENSE", ".github/workflows/govulncheck.yml", ".github/workflows/fuzz.yml", ".github/workflows/portability.yml", ".github/dependabot.yml":
			continue
		}
		return true
	}
	return false
}
