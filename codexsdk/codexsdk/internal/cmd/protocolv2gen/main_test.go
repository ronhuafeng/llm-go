package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveSchemaInputsUsesManifestDirectoryWhenSchemaRootOmitted(t *testing.T) {
	schemaRoot, manifestPath := resolveSchemaInputs("", "/tmp/appserver/v2/manifest.json")
	if schemaRoot != "/tmp/appserver/v2" {
		t.Fatalf("schemaRoot = %q, want /tmp/appserver/v2", schemaRoot)
	}
	if manifestPath != "/tmp/appserver/v2/manifest.json" {
		t.Fatalf("manifestPath = %q, want /tmp/appserver/v2/manifest.json", manifestPath)
	}
}

func TestResolveSchemaInputsHonorsExplicitSchemaRoot(t *testing.T) {
	schemaRoot, manifestPath := resolveSchemaInputs("/tmp/schema-root", "/tmp/custom/manifest.json")
	if schemaRoot != "/tmp/schema-root" {
		t.Fatalf("schemaRoot = %q, want /tmp/schema-root", schemaRoot)
	}
	if manifestPath != "/tmp/custom/manifest.json" {
		t.Fatalf("manifestPath = %q, want /tmp/custom/manifest.json", manifestPath)
	}
}

func TestRunStdoutModeWritesOnlyRequestedArtifact(t *testing.T) {
	schemaRoot := filepath.Join("..", "..", "protocolschema", "appserver", "v2")
	outDir := filepath.Join(t.TempDir(), "generated")
	var stdout bytes.Buffer

	if err := run(schemaRoot, "", outDir, "method-registry", "", "", &stdout); err != nil {
		t.Fatal(err)
	}
	checkedIn, err := os.ReadFile(filepath.Join("..", "..", "..", "protocolv2", "method_registry.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stdout.Bytes(), checkedIn) {
		t.Fatal("stdout method-registry output does not match checked-in artifact")
	}
	if _, err := os.Stat(outDir); !os.IsNotExist(err) {
		t.Fatalf("stdout mode wrote output directory: err=%v", err)
	}
}

func TestRunRejectsUnknownStdoutArtifact(t *testing.T) {
	schemaRoot := filepath.Join("..", "..", "protocolschema", "appserver", "v2")
	err := run(schemaRoot, "", t.TempDir(), "both", "", "", &bytes.Buffer{})
	if err == nil {
		t.Fatal("run accepted unknown stdout artifact")
	}
	if !strings.Contains(err.Error(), "-stdout must be method-registry, protocol-types, or classified-surface") {
		t.Fatalf("unknown stdout artifact error = %v", err)
	}
}
