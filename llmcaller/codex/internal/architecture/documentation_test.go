package architecture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecurityGuidanceUsesCurrentExactAndAdapterContracts(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "SECURITY.md"))
	if err != nil {
		t.Fatal(err)
	}
	guidance := string(data)
	for _, current := range []string{"codexsdk.Client.ThreadRunner()", "ReadOnlyEphemeralOptions", "protocolv2"} {
		if !strings.Contains(guidance, current) {
			t.Errorf("security guidance does not name current contract %q", current)
		}
	}
}
