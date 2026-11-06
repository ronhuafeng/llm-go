package protocolsync

import (
	"context"
	"errors"
	"path"
	"strings"
)

// Automated native integration preserves the accepted controller and gate
// decisions. protocolgen is candidate implementation, like the generated SDK:
// it may evolve with its native tests under the unchanged source, generated,
// provenance and Live gates. It does not replace the trusted controller binary
// or its independent exact-source comparison. Control package trees remain
// protected in full, including new init, TestMain and helper files. This is an
// integration eligibility rule, not a publication path allowlist.
func protectedForwardControl(p string) bool {
	switch path.Base(p) {
	case "go.mod", "go.sum", "go.work", "go.work.sum":
		return true
	}
	for _, prefix := range []string{".github/", "internal/tools/", "codexsdk/internal/cmd/", "codexsdk/internal/checkacceptance/", "codexsdk/internal/codexexec/", "codexsdk/internal/protocolsync/", "codexsdk/internal/protocolupgrade/", "codexsdk/internal/generatedcheck/"} {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return strings.HasPrefix(p, "codexsdk/internal/protocolschema/") && strings.HasSuffix(p, ".go")
}
func validateForwardControls(ctx context.Context, root, main, head string) error {
	names, e := forwardLocalGitCommand(ctx, root, "diff", "--name-only", "--no-renames", "-z", main, head, "--").Output()
	if e != nil {
		return e
	}
	for _, name := range strings.Split(string(names), "\x00") {
		if protectedForwardControl(name) {
			return errors.New("native candidate changes accepted gate/control authority; explicit reviewed manual integration required")
		}
	}
	return nil
}

// Generic mechanical reconciliation retains its bounded SDK/runtime scope.
// The exact candidate owns both implementation and native behavior tests;
// control-changing proposals still require the reviewed manual route.
func validateGenericForwardScope(ctx context.Context, root, main, head string) error {
	names, err := forwardLocalGitCommand(ctx, root, "diff", "--name-only", "--no-renames", "-z", main, head, "--").Output()
	if err != nil {
		return err
	}
	for _, name := range strings.Split(string(names), "\x00") {
		if name == "" {
			continue
		}
		if ForwardRuntimeImplementationPath(name) || (strings.HasSuffix(name, "_test.go") && !strings.Contains(name, "/testdata/") && (strings.HasPrefix(name, "llmkit/") || strings.HasPrefix(name, "llmcaller/"))) {
			continue
		}
		dir := path.Dir(name)
		if strings.HasSuffix(name, ".go") && (dir == "codexsdk" || dir == "codexsdk/protocolv2" || dir == "codexsdk/internal/wirejson") {
			continue
		}
		if name == "README.md" || (strings.HasPrefix(name, "docs/") && strings.HasSuffix(name, ".md")) {
			continue
		}
		return errors.New("generic reconciliation changes paths outside native runtime scope; reviewed manual reconciliation required")
	}
	return nil
}
