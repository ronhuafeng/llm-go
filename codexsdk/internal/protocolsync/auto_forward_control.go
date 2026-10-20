package protocolsync

import (
	"context"
	"errors"
	"path"
	"strings"
)

// Automated native integration cannot let the candidate replace its own gate
// authority. This is an integration eligibility rule, not a publication path
// allowlist: control-changing proposals remain reviewable and may use the
// explicit manual FF route after review. Full package trees include new init,
// TestMain and helper files, not only today's known entrypoints.
func protectedForwardControl(p string) bool {
	switch path.Base(p) {
	case "go.mod", "go.sum", "go.work", "go.work.sum":
		return true
	}
	for _, prefix := range []string{".github/", "internal/tools/", "codexsdk/internal/cmd/", "codexsdk/internal/checkacceptance/", "codexsdk/internal/codexexec/", "codexsdk/internal/protocolsync/", "codexsdk/internal/protocolupgrade/", "codexsdk/internal/generatedcheck/", "codexsdk/internal/schemaoracle/", "codexsdk/internal/protocolgen/"} {
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

// Generic mechanical reconciliation has no target-schema lane. Its immutable
// runtime oracle covers these SDK roots and accepted llmkit/llmcaller packages. Other engineering changes
// retain the reviewed same-H FF route until broader accepted-test proof exists.
func validateGenericForwardScope(ctx context.Context, root, main, head string) error {
	names, err := forwardLocalGitCommand(ctx, root, "diff", "--name-only", "--no-renames", "-z", main, head, "--").Output()
	if err != nil {
		return err
	}
	for _, name := range strings.Split(string(names), "\x00") {
		if name == "" {
			continue
		}
		if ForwardBaselineImplementationPath(name) || (strings.HasSuffix(name, "_test.go") && !strings.Contains(name, "/testdata/") && (strings.HasPrefix(name, "llmkit/") || strings.HasPrefix(name, "llmcaller/"))) {
			continue
		}
		dir := path.Dir(name)
		if strings.HasSuffix(name, ".go") && (dir == "codexsdk" || dir == "codexsdk/protocolv2" || dir == "codexsdk/internal/wirejson") {
			continue
		}
		if name == "README.md" || (strings.HasPrefix(name, "docs/") && strings.HasSuffix(name, ".md")) {
			continue
		}
		return errors.New("generic reconciliation changes paths outside existing immutable runtime proof; reviewed manual reconciliation required")
	}
	return nil
}
