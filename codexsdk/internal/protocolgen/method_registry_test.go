package protocolgen

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateMethodRegistryMatchesCheckedInOutput(t *testing.T) {
	manifest, err := LoadManifest(filepath.Join("..", "protocolschema", "appserver", "v2", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	generated, err := GenerateMethodRegistry(manifest)
	if err != nil {
		t.Fatal(err)
	}
	checkedIn, err := os.ReadFile(filepath.Join("..", "..", "protocolv2", "method_registry.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, checkedIn) {
		t.Fatal("generated method registry does not match checked-in codexsdk/protocolv2/method_registry.gen.go")
	}
}

func TestGeneratedMethodRegistryKeepsTypedBoundary(t *testing.T) {
	manifest, err := LoadManifest(filepath.Join("..", "protocolschema", "appserver", "v2", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	generated, err := GenerateMethodRegistry(manifest)
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	for _, forbidden := range []string{"json.RawMessage", "map[string]any", "UnknownFields", "AdditionalFields"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated method registry contains forbidden public passthrough marker %q", forbidden)
		}
	}
}

func TestLoadManifestRejectsSkeleton(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, []byte(`{"status":"baseline-skeleton","entries":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(path); err == nil {
		t.Fatal("LoadManifest accepted baseline-skeleton manifest")
	}
}

func TestMethodConstNameUsesGoAcronyms(t *testing.T) {
	cases := map[string]string{
		"account/chatgptAuthTokens/refresh": "MethodAccountChatGPTAuthTokensRefresh",
		"fs/readFile":                       "MethodFSReadFile",
		"mcpServer/oauth/login":             "MethodMCPServerOAuthLogin",
	}
	for method, want := range cases {
		if got := methodConstName(method); got != want {
			t.Fatalf("methodConstName(%q) = %q, want %q", method, got, want)
		}
	}
}
