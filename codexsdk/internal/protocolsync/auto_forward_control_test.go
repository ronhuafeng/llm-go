package protocolsync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutoForwardPreservesCurrentGateAuthorityIncludingQuotedPaths(t *testing.T) {
	for _, name := range []string{".github/workflows/verify.yml", "codexsdk/internal/protocolsync/injected_test.go", "codexsdk/internal/checkacceptance/injected\t_test.go", "codexsdk/internal/schemaoracle/新增.go", "codexsdk/internal/generatedcheck/helper.go", "codexsdk/internal/protocolgen/new.go", "codexsdk/internal/protocolschema/appserver/v2/embed.go", "nested/go.mod"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			git := func(args ...string) string {
				t.Helper()
				cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
				out, e := cmd.CombinedOutput()
				if e != nil {
					t.Fatalf("git %v %s", e, out)
				}
				return strings.TrimSuffix(string(out), "\n")
			}
			git("init", "-q")
			git("config", "user.name", "Fixture")
			git("config", "user.email", "fixture@example.invalid")
			git("commit", "--allow-empty", "-qm", "base")
			base := git("rev-parse", "HEAD")
			path := filepath.Join(dir, name)
			if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(path, []byte("malicious authority replacement"), 0600); e != nil {
				t.Fatal(e)
			}
			git("add", "--", name)
			git("commit", "-qm", "candidate")
			head := git("rev-parse", "HEAD")
			if e := validateForwardControls(context.Background(), dir, base, head); e == nil {
				t.Fatal("candidate control replacement was accepted")
			}
		})
	}
	for _, name := range []string{"codexsdk/protocolv2/example.go", "codexsdk/internal/protocolschema/appserver/v2/schema.json", "codexsdk/client.go"} {
		if protectedForwardControl(name) {
			t.Fatalf("production/schema adaptation unexpectedly frozen: %s", name)
		}
	}
}
