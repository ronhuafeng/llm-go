package livecodex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func fixtureBaseline(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "codexsdk/internal/protocolschema/appserver/v2/baseline_metadata.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"codex_version":"codex-cli 0.159.0","source_ref_name":"rust-v0.159.0"}`), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func fakeCLI(t *testing.T, version string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte("#!/bin/sh\nprintf '%s\\n' 'codex-cli "+version+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestPrepareIsolatedNativeMiniFixture(t *testing.T) {
	fakeCLI(t, "0.159.0")
	dir := t.TempDir()
	f, err := Prepare(fixtureBaseline(t), dir, "https://mini.example/v1", "private-test-key")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(f.Home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private-test-key") {
		t.Fatal("key was written into config")
	}
	var config map[string]any
	if err := toml.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config["model"] != "gpt-6-sol" || config["model_reasoning_effort"] != "high" || config["model_provider"] != "mini" {
		t.Fatal("wrong canonical model or provider")
	}
	mini := config["model_providers"].(map[string]any)["mini"].(map[string]any)
	if mini["base_url"] != "https://mini.example/v1" || mini["env_key"] != "MINI_CODEX_API_KEY" || mini["wire_api"] != "responses" {
		t.Fatal("wrong native provider configuration")
	}
	if mini["requires_openai_auth"] != false || mini["supports_websockets"] != false {
		t.Fatal("Mini must explicitly use its Bearer key with HTTP/SSE transport")
	}
	if f.Workspace == f.Home || !strings.HasPrefix(f.Workspace, dir+string(os.PathSeparator)) {
		t.Fatal("workspace is not separately isolated")
	}
	if _, err := os.Stat(filepath.Join(f.Home, "auth.json")); !os.IsNotExist(err) {
		t.Fatal("fixture inherited ambient auth")
	}
}

func TestPrepareRejectsMissingPrerequisitesWithoutLeakingInputs(t *testing.T) {
	for _, test := range []struct{ name, base, key, version string }{
		{"missing key", "https://mini.example/v1", "", "0.159.0"},
		{"embedded key newline", "https://mini.example/v1", "secret-key\nsuffix", "0.159.0"},
		{"embedded key space", "https://mini.example/v1", "secret-key suffix", "0.159.0"},
		{"unnormalized key", "https://mini.example/v1", "\nsecret-key\n", "0.159.0"},
		{"missing base", "", "secret-key", "0.159.0"},
		{"endpoint", "https://mini.example/v1/responses", "secret-key", "0.159.0"},
		{"credential URL", "https://user:secret@mini.example/v1", "secret-key", "0.159.0"},
		{"query credential", "https://mini.example/v1?key=secret", "secret-key", "0.159.0"},
		{"wrong runtime", "https://mini.example/v1", "secret-key", "0.160.0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fakeCLI(t, test.version)
			_, err := Prepare(fixtureBaseline(t), t.TempDir(), test.base, test.key)
			if err == nil {
				t.Fatal("missing or mismatched prerequisite accepted")
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "mini.example") {
				t.Fatal("unsafe setup diagnostic")
			}
		})
	}
}
