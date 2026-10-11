// Package livecodex owns the local and CI fixture for the real Codex suite.
package livecodex

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// Version resolves official npm latest exactly once. Callers retain this value
// for installation and verification throughout the run; schema provenance is
// deliberately not a runtime-version selector.
func Version() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "npm", "view", "--registry=https://registry.npmjs.org", "@openai/codex@latest", "version").Output()
	if err != nil {
		return "", errors.New("cannot resolve official Codex latest release")
	}
	return SelectedVersion(strings.TrimSpace(string(out)))
}

// SelectedVersion admits only a concrete stable release selected for this run.
func SelectedVersion(version string) (string, error) {
	if semver.Canonical("v"+version) != "v"+version || semver.Prerelease("v"+version) != "" || semver.Build("v"+version) != "" {
		return "", errors.New("CODEX_CLI_VERSION must be one exact stable official release resolved for this run")
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
