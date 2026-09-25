package protocolgen

import (
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
