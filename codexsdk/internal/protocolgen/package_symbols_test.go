package protocolgen

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateGeneratedPackageChecksEveryArtifactAndUnexportedName(t *testing.T) {
	err := ValidateGeneratedPackage("protocolv2", "", map[string][]byte{
		"new_generator.gen.go":  []byte("package protocolv2\nfunc helper() {}\n"),
		"protocol_types.gen.go": []byte("package protocolv2\nvar helper int\n"),
	})
	if err == nil || !strings.Contains(err.Error(), "package symbol helper") ||
		!strings.Contains(err.Error(), "new_generator.gen.go") ||
		!strings.Contains(err.Error(), "protocol_types.gen.go") {
		t.Fatalf("collision = %v, want both generated source paths", err)
	}
}

func TestPackageMemberScopes(t *testing.T) {
	for _, test := range []struct {
		name, source string
		conflict     bool
	}{
		{"separate receivers", "type A struct{}; type B struct{}; func (A) Read(){}; func (B) Read(){}", false},
		{"field and method", "type A struct{Read int}; func (*A) Read(){}", true},
		{"duplicate methods", "type A struct{}; func (A) Read(){}; func (*A) Read(){}", true},
		{"package and method", "type A struct{}; var Read int; func (A) Read(){}", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateGeneratedPackage("codexsdk", "", map[string][]byte{"sdk_surface.gen.go": []byte("package codexsdk\n" + test.source)})
			if (err != nil) != test.conflict {
				t.Fatalf("conflict = %v, error = %v", test.conflict, err)
			}
		})
	}
}

func TestHandwrittenCollisionIsNotSchemaIncompatibility(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.go", "b.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package protocolv2\nvar duplicate int\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	err := ValidateGeneratedPackage("protocolv2", dir, nil)
	var unsupported *UnsupportedSchemaError
	if err == nil || errors.As(err, &unsupported) {
		t.Fatalf("expected ordinary handwritten source error, got %v", err)
	}
}

func TestCollisionRepairEligibilityDoesNotDependOnSchemaPath(t *testing.T) {
	for _, sources := range []map[string][]string{nil, {"a.gen.go:type:Name": {"v2/Name.json"}}} {
		err := validateGeneratedPackage("protocolv2", "", map[string][]byte{
			"a.gen.go": []byte("package protocolv2; type Name int"),
			"b.gen.go": []byte("package protocolv2; var Name int"),
		}, sources)
		var unsupported *UnsupportedSchemaError
		if !errors.As(err, &unsupported) {
			t.Fatalf("collision lost owner-assigned incompatibility: %v", err)
		}
		if !strings.Contains(err.Error(), "a.gen.go") || !strings.Contains(err.Error(), "b.gen.go") {
			t.Fatalf("lost Go diagnostic locations: %v", err)
		}
	}
}
