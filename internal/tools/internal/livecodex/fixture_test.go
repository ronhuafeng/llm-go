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

func TestPrepareIsolatedNativeResponsesFixture(t *testing.T) {
	fakeCLI(t, "0.159.0")
	dir := t.TempDir()
	f, err := Prepare(fixtureBaseline(t), dir, "https://provider.example/v1", "private-test-key")
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
	if config["model"] != "gpt-6-sol" || config["model_reasoning_effort"] != "high" || config["model_provider"] != "llm-go-live" {
		t.Fatal("wrong canonical model or provider")
	}
	provider := config["model_providers"].(map[string]any)["llm-go-live"].(map[string]any)
	if provider["name"] != "llm-go live provider" || provider["base_url"] != "https://provider.example/v1" || provider["env_key"] != "AZURE_OPENAI_API_KEY" || provider["wire_api"] != "responses" {
		t.Fatal("wrong native provider configuration")
	}
	if provider["requires_openai_auth"] != false || provider["supports_websockets"] != false {
		t.Fatal("live provider must explicitly use its Bearer key with HTTP/SSE transport")
	}
	if f.Workspace == f.Home || !strings.HasPrefix(f.Workspace, dir+string(os.PathSeparator)) {
		t.Fatal("workspace is not separately isolated")
	}
	if _, err := os.Stat(filepath.Join(f.Home, "auth.json")); !os.IsNotExist(err) {
		t.Fatal("fixture inherited ambient auth")
	}
}

func TestPrepareRejectsMissingPrerequisitesWithoutLeakingInputs(t *testing.T) {
	for _, test := range []struct{ name, endpoint, key, version string }{
		{"missing key", "https://provider.example/v1", "", "0.159.0"},
		{"embedded key newline", "https://provider.example/v1", "secret-key\nsuffix", "0.159.0"},
		{"embedded key space", "https://provider.example/v1", "secret-key suffix", "0.159.0"},
		{"unnormalized key", "https://provider.example/v1", "\nsecret-key\n", "0.159.0"},
		{"missing endpoint", "", "secret-key", "0.159.0"},
		{"unsupported scheme", "file:///secret", "secret-key", "0.159.0"},
		{"credential URL", "https://user:secret@provider.example/v1", "secret-key", "0.159.0"},
		{"query credential", "https://provider.example/v1?key=secret", "secret-key", "0.159.0"},
		{"fragment", "https://provider.example/v1#secret", "secret-key", "0.159.0"},
		{"empty query", "https://provider.example/v1?", "secret-key", "0.159.0"},
		{"empty fragment", "https://provider.example/v1#", "secret-key", "0.159.0"},
		{"wrong runtime", "https://provider.example/v1", "secret-key", "0.160.0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fakeCLI(t, test.version)
			_, err := Prepare(fixtureBaseline(t), t.TempDir(), test.endpoint, test.key)
			if err == nil {
				t.Fatal("missing or mismatched prerequisite accepted")
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "provider.example") {
				t.Fatal("unsafe setup diagnostic")
			}
		})
	}
}
