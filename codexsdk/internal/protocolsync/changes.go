package protocolsync

import (
	"bytes"
	"fmt"
	"os/exec"
	"path"
	"sort"
	"strings"
)

const mechanicalPrefix = "codexsdk/internal/protocolschema/appserver/v2"

// ChangedPaths lists dirty tracked and untracked paths in repoRoot.
func ChangedPaths(repoRoot string) ([]string, error) {
	tracked, err := gitNUL(repoRoot, "diff", "--name-only", "--no-renames", "-z", "HEAD", "--")
	if err != nil {
		return nil, err
	}
	untracked, err := gitNUL(repoRoot, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var paths []string
	for _, p := range append(tracked, untracked...) {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, nil
}

// AssertClean fails if the protocol worktree is dirty.
func AssertClean(repoRoot string) error {
	paths, err := ChangedPaths(repoRoot)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return nil
	}
	return fmt.Errorf("sync worktree must be clean before apply:\n- %s", strings.Join(paths, "\n- "))
}

func validatePaths(paths []string, phase string) error {
	var invalid []string
	for _, p := range paths {
		if phase == "mechanical" {
			if !isMechanicalPath(p) {
				invalid = append(invalid, p)
			}
			continue
		}
		if !strings.HasPrefix(p, "codexsdk/") || strings.HasPrefix(p, "codexsdk/.cache/") || strings.HasPrefix(p, "codexsdk/.agents/") {
			invalid = append(invalid, p)
			continue
		}
		if phase == "agent" && !isAgentProposalPath(p) {
			invalid = append(invalid, p)
		}
	}
	if len(invalid) == 0 {
		return nil
	}
	return fmt.Errorf("sync changes escape the %s scope:\n- %s", phase, strings.Join(invalid, "\n- "))
}

// Agent proposals may change runtime or generator implementation and focused
// tests. Sync policy, candidate identity, acceptance, and publication code are
// reviewed through ordinary development rather than through the same proposal.
func isAgentProposalPath(p string) bool {
	if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, ".gen.go") {
		return false
	}
	if dir := path.Dir(p); dir == "codexsdk" || dir == "codexsdk/protocolv2" || dir == "codexsdk/internal/protocolgen" || dir == "codexsdk/internal/wirejson" {
		return true
	}
	if path.Dir(p) != "codexsdk/internal/protocolupgrade" {
		return false
	}
	if strings.HasSuffix(p, "_test.go") {
		return true
	}
	switch path.Base(p) {
	case "manifest.go", "surface.go", "commonrs.go":
		return true
	default:
		return false
	}
}

func isMechanicalPath(p string) bool {
	if p == mechanicalPrefix || strings.HasPrefix(p, mechanicalPrefix+"/") {
		return true
	}
	if p == "codexsdk/sdk_surface.gen.go" {
		return true
	}
	return path.Dir(p) == "codexsdk/protocolv2" && strings.HasSuffix(p, ".gen.go")
}

// StagePaths git-adds the current dirty set after validating it for phase.
func StagePaths(repoRoot, phase string) ([]string, error) {
	paths, err := ChangedPaths(repoRoot)
	if err != nil {
		return nil, err
	}
	if err := validatePaths(paths, phase); err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("sync change set is empty")
	}
	var input bytes.Buffer
	for _, p := range paths {
		input.WriteString(p)
		input.WriteByte(0)
	}
	cmd := exec.Command("git", "-C", repoRoot, "add", "-A", "--pathspec-from-file=-", "--pathspec-file-nul")
	cmd.Stdin = &input
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git add: %w: %s", err, bytes.TrimSpace(out))
	}
	return paths, nil
}

func gitNUL(repoRoot string, args ...string) ([]string, error) {
	cmd := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	var paths []string
	for _, raw := range bytes.Split(out, []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		paths = append(paths, string(raw))
	}
	return paths, nil
}
