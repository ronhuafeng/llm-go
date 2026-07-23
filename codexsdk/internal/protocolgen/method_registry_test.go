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
		t.Fatal("generated method registry does not match checked-in protocolv2/method_registry.gen.go")
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

func TestLoadManifestRequiresClassifiedSurfaceSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, []byte(`{"status":"classified-manifest","entries":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(path); err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("LoadManifest error = %v, want schema_version failure", err)
	}
}

func TestApplyWireMessageRolesUsesManifestDirectionAndKind(t *testing.T) {
	plan := ProtocolTypePlan{Types: []TypePlan{
		{SchemaPath: "ReadResponse.json"},
		{SchemaPath: "ServerNotification.json"},
		{SchemaPath: "ClientRequest.json"},
		{SchemaPath: "ServerRequest.json"},
		{SchemaPath: "ApprovalResponse.json"},
	}}
	manifest := Manifest{Entries: []ManifestEntry{
		{Direction: "client_to_server", Kind: "request", ResponseSchema: "ReadResponse.json"},
		{Direction: "server_to_client", Kind: "notification", SourceSchema: "ServerNotification.json"},
		{Direction: "client_to_server", Kind: "notification", SourceSchema: "ClientRequest.json"},
		{Direction: "server_to_client", Kind: "request", SourceSchema: "ServerRequest.json", ResponseSchema: "ApprovalResponse.json"},
	}}
	if err := ApplyWireMessageRoles(&plan, manifest); err != nil {
		t.Fatal(err)
	}

	for _, typ := range plan.Types {
		want := WireMessageRoleActionBearingMessage
		if typ.SchemaPath == "ReadResponse.json" || typ.SchemaPath == "ServerNotification.json" {
			want = WireMessageRoleServerObservation
		}
		if typ.WireMessageRoles != want {
			t.Fatalf("%s wire roles = %b, want %b", typ.SchemaPath, typ.WireMessageRoles, want)
		}
	}
}

func TestApplyWireMessageRolesRecordsEveryPositionForAReusedRoot(t *testing.T) {
	plan := ProtocolTypePlan{Types: []TypePlan{{SchemaPath: "SharedResponse.json"}}}
	manifest := Manifest{Entries: []ManifestEntry{
		{Direction: "client_to_server", Kind: "request", ResponseSchema: "SharedResponse.json"},
		{Direction: "server_to_client", Kind: "request", ResponseSchema: "SharedResponse.json"},
	}}
	if err := ApplyWireMessageRoles(&plan, manifest); err != nil {
		t.Fatal(err)
	}
	roles := plan.Types[0].WireMessageRoles
	if !roles.Has(WireMessageRoleActionBearingMessage) || !roles.Has(WireMessageRoleServerObservation) {
		t.Fatalf("SharedResponse wire roles = %b, want action-bearing and server-observation", roles)
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
