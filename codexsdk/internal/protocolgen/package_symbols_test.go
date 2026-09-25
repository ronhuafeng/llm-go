package protocolgen

import (
	"strings"
	"testing"
)

func TestValidateGeneratedPackageChecksEveryArtifactAndUnexportedName(t *testing.T) {
	_, err := ValidateGeneratedPackage(ProtocolTypePlan{}, Manifest{}, "", map[string][]byte{
		"new_generator.gen.go":  []byte("package protocolv2\nfunc helper() {}\n"),
		"protocol_types.gen.go": []byte("package protocolv2\nvar helper int\n"),
	})
	if err == nil || !strings.Contains(err.Error(), "package symbol helper") ||
		!strings.Contains(err.Error(), "new_generator.gen.go") ||
		!strings.Contains(err.Error(), "protocol_types.gen.go") {
		t.Fatalf("collision = %v, want both generated source paths", err)
	}
}
