package livecodex

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/pelletier/go-toml/v2"
)

const (
	Model     = "gpt-6-sol"
	Reasoning = "high"
	Provider  = "llm-go-live"
)

type Fixture struct{ Home, Workspace string }

// NormalizeKey removes surrounding paste whitespace, without repairing a token
// containing whitespace. The returned key belongs only in the child environment.
func NormalizeKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", errors.New("AZURE_OPENAI_API_KEY is required")
	}
	if strings.ContainsFunc(key, unicode.IsSpace) {
		return "", errors.New("AZURE_OPENAI_API_KEY contains whitespace")
	}
	return key, nil
}

// Prepare validates the native API base, uses
// caller-owned temporary state, and never persists the provider key.
func Prepare(root, directory, baseURL, key string) (Fixture, error) {
	canonicalKey, err := NormalizeKey(key)
	if err != nil {
		return Fixture{}, err
	}
	if canonicalKey != key {
		return Fixture{}, errors.New("AZURE_OPENAI_API_KEY must be normalized before fixture setup; use livecodex run")
	}
	baseURL = strings.TrimSpace(baseURL)
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(baseURL, "#") {
		return Fixture{}, errors.New("CODEX_RESPONSES_API_ENDPOINT must be an HTTP(S) URL without credentials, query parameters, or a fragment")
	}
	baseURL = strings.TrimRight(baseURL, "/")
	version, err := Version(root)
	if err != nil {
		return Fixture{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "codex", "--version").Output()
	if err != nil {
		return Fixture{}, errors.New("official codex CLI is required")
	}
	if strings.TrimSpace(string(out)) != "codex-cli "+version {
		return Fixture{}, errors.New("installed Codex version does not match the tested baseline")
	}
	f := Fixture{Home: filepath.Join(directory, "codex-home"), Workspace: filepath.Join(directory, "workspace")}
	for _, path := range []string{f.Home, f.Workspace} {
		if err := os.Mkdir(path, 0700); err != nil {
			return Fixture{}, errors.New("cannot create isolated live fixture")
		}
	}
	config := map[string]any{
		"model": Model, "model_reasoning_effort": Reasoning, "model_provider": Provider,
		"model_providers": map[string]any{Provider: map[string]any{
			"name": "llm-go live provider", "base_url": baseURL, "env_key": "AZURE_OPENAI_API_KEY", "wire_api": "responses",
			"requires_openai_auth": false, "supports_websockets": false,
		}},
	}
	data, err := toml.Marshal(config)
	if err != nil {
		return Fixture{}, errors.New("cannot encode native Codex configuration")
	}
	if err := os.WriteFile(filepath.Join(f.Home, "config.toml"), data, 0600); err != nil {
		return Fixture{}, errors.New("cannot write isolated Codex configuration")
	}
	return f, nil
}
