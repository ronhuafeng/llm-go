package livecodex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectedVersionRequiresExactStableRelease(t *testing.T) {
	for _, value := range []string{"0.160.0", "1.2.3"} {
		if got, err := SelectedVersion(value); err != nil || got != value {
			t.Fatalf("version=%q err=%v", got, err)
		}
	}
	for _, value := range []string{"", "latest", "v0.160.0", "0.160", "0.160.0-alpha.1", "0.160.0+build", "01.2.3", "0.160.0\n0.161.0", "PRIVATE"} {
		if _, err := SelectedVersion(value); err == nil || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatalf("invalid release diagnostic: %v", err)
		}
	}
}

func TestVersionResolvesOfficialLatestExactlyOnce(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\n' \"$@\" > \"$NPM_ARGS\"\nprintf x >> \"$NPM_CALLS\"\nprintf '0.171.2\n'\n"
	if err := os.WriteFile(filepath.Join(dir, "npm"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	argsPath, callsPath := filepath.Join(dir, "args"), filepath.Join(dir, "calls")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NPM_ARGS", argsPath)
	t.Setenv("NPM_CALLS", callsPath)
	version, err := Version()
	if err != nil || version != "0.171.2" {
		t.Fatalf("version=%q err=%v", version, err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(args) != "view\n--registry=https://registry.npmjs.org\n@openai/codex@latest\nversion\n" {
		t.Fatalf("unexpected registry query: %q", args)
	}
	calls, err := os.ReadFile(callsPath)
	if err != nil || string(calls) != "x" {
		t.Fatal("latest must be resolved exactly once")
	}
}

func TestRelevanceIncludesSharedCodeAndUnknownPaths(t *testing.T) {
	for _, test := range []struct {
		path     string
		required bool
	}{
		{"codexsdk/client.go", true},
		{"codexsdk/internal/protocolschema/appserver/v2/baseline_metadata.json", true},
		{"llmcaller/codex/caller.go", true},
		{"llmkit/llmschema/validate.go", true},
		{"internal/tools/integration/live_codex_test.go", true},
		{"go.sum", true},
		{".github/workflows/auto-forward.yml", true},
		{".github/workflows/pr-verification.yml", true},
		{"new-runtime-input", true},
		{"README.md", false},
		{"docs/issues.md", false},
		{".github/workflows/govulncheck.yml", false},
	} {
		t.Run(test.path, func(t *testing.T) {
			if got := Required([]string{test.path}); got != test.required {
				t.Fatalf("required=%v, want %v", got, test.required)
			}
		})
	}
	if Required(nil) {
		t.Fatal("empty diff must be not applicable")
	}
}

func TestVersionResolutionFailureNeverFallsBackOrLeaksOutput(t *testing.T) {
	for _, script := range []string{
		"#!/bin/sh\nprintf 'PRIVATE' >&2\nexit 1\n",
		"#!/bin/sh\nprintf 'PRIVATE\\n'\n",
		"#!/bin/sh\nprintf '0.171.2-alpha.1\\n'\n",
		"#!/bin/sh\nprintf '0.171.2\\n0.171.3\\n'\n",
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "npm"), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		if version, err := Version(); err == nil || version != "" || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatalf("unsafe/fallback resolution: version=%q err=%v", version, err)
		}
	}
}
