package protocolsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// VerifyForwardBaselines proves a mechanical reconciliation against the accepted
// public/runtime assertions of both original B and current M. The caller supplies
// sanitized, detached checkouts of exact H, B, and M. It neither resolves an
// upstream target nor invokes a model, and does not replace fresh exact-H CI.
// Execution uses only disposable snapshots in the existing credential-free
// oracle sandbox; retained diagnostics must be outside all three inputs.
func VerifyForwardBaselines(ctx context.Context, candidateRoot, originalBaseRoot, currentBaseRoot, diagnosticDir string) error {
	return verifyForwardBaselines(ctx, candidateRoot, originalBaseRoot, currentBaseRoot, diagnosticDir, runForwardBaseline)
}

func runForwardBaseline(ctx context.Context, req oracleRequest) error {
	if err := os.MkdirAll(filepath.Join(req.RepoRoot, "codexsdk/.cache/openai-codex"), 0700); err != nil {
		return err
	}
	sandbox, err := newOracleSandbox(ctx, req.RepoRoot, req.CandidateDir)
	if err != nil {
		return err
	}
	// Compilation may write only its private build state, never the frozen H.
	sandbox.readonly = append(sandbox.readonly, req.RepoRoot)
	req.ForwardRuntime = true
	return runBaselineOracle(ctx, req, sandbox)
}

func verifyForwardBaselines(ctx context.Context, candidateRoot, originalBaseRoot, currentBaseRoot, diagnosticDir string, run func(context.Context, oracleRequest) error) (retErr error) {
	roots := []string{candidateRoot, originalBaseRoot, currentBaseRoot}
	for _, root := range roots {
		if !filepath.IsAbs(root) {
			return errors.New("forward baseline inputs must be absolute")
		}
		real, err := filepath.EvalSymlinks(root)
		if err != nil || real != filepath.Clean(root) {
			return errors.New("forward baseline input is missing or traverses a symlink")
		}
	}
	for _, base := range roots[1:] {
		if goalWithin(candidateRoot, base) || goalWithin(base, candidateRoot) {
			return errors.New("forward baseline must be separate from candidate")
		}
	}
	if !filepath.IsAbs(diagnosticDir) {
		return errors.New("forward diagnostic directory must be absolute")
	}
	for _, root := range roots {
		if goalWithin(root, diagnosticDir) {
			return errors.New("forward diagnostics must be outside inputs")
		}
	}
	if err := os.MkdirAll(diagnosticDir, 0700); err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(diagnosticDir)
	if err != nil || real != filepath.Clean(diagnosticDir) {
		return errors.New("forward diagnostic directory must not traverse a symlink")
	}
	log, err := newGoalDiagnostics(diagnosticDir)
	if err != nil {
		return err
	}
	defer func() {
		log.record("footer", "forward-baselines", map[string]any{"passed": retErr == nil, "complete": log.err == nil, "error": goalError(retErr)})
		if err := log.file.Close(); err != nil && log.err == nil {
			log.err = err
		}
		if log.err != nil {
			retErr = errors.Join(retErr, &GoalVerificationIncompleteError{Err: log.err})
		}
	}()
	work, err := os.MkdirTemp("", "forward-baselines-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	labels := []string{"candidate", "original", "current"}
	snapshots := make([]string, len(roots))
	digests := make([]string, len(roots))
	for i, root := range roots {
		snapshots[i] = filepath.Join(work, labels[i])
		digests[i], err = goalSnapshot(ctx, root, snapshots[i])
		if err != nil {
			return fmt.Errorf("freeze %s: %w", labels[i], err)
		}
		log.record("snapshot", "forward-baselines", map[string]any{"input": labels[i], "sha256": digests[i]})
	}
	defer func() {
		for i, root := range roots {
			for _, path := range []string{root, snapshots[i]} {
				digest, err := goalSnapshot(ctx, path, "")
				if err != nil {
					retErr = errors.Join(retErr, fmt.Errorf("revalidate %s: %w", labels[i], err))
				} else if digest != digests[i] {
					retErr = errors.Join(retErr, fmt.Errorf("%s changed during forward baseline verification", labels[i]))
				}
			}
		}
	}()
	// This lane has no upstream target. The oracle only needs an existing external
	// read-only input mount; no schema metadata or target identity is inferred.
	retained := filepath.Join(work, "readonly-inputs")
	if err := os.Mkdir(retained, 0700); err != nil {
		return err
	}
	for i := 1; i < len(snapshots); i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		output := &goalOutput{log: log, gate: "forward-baseline-" + labels[i]}
		req := oracleRequest{RepoRoot: snapshots[0], BaseRoot: snapshots[i], ModuleRoot: filepath.Join(snapshots[i], "codexsdk"), CandidateDir: retained, Output: output}
		err := run(ctx, req)
		output.flush(true)
		log.record("baseline", output.gate, map[string]any{"passed": err == nil, "error": goalError(err)})
		if err != nil {
			return fmt.Errorf("%s baseline: %w", labels[i], err)
		}
		if log.err != nil {
			return &GoalVerificationIncompleteError{Err: log.err}
		}
	}
	return nil
}

// ForwardBaselineImplementationPath identifies the additional production Go
// inputs exercised by targetless baseline reconciliation. Accepted testdata and
// test files remain owned by B/M, never by the candidate.
func ForwardBaselineImplementationPath(p string) bool {
	return (strings.HasPrefix(p, "llmkit/") || strings.HasPrefix(p, "llmcaller/")) && !strings.Contains(p, "/testdata/") && strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go")
}

func forwardRuntimePackages(root string) ([]string, error) {
	var packages []string
	for _, family := range []string{"llmkit", "llmcaller"} {
		dir := filepath.Join(root, family)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !e.IsDir() {
				return nil
			}
			if e.Name() == "testdata" || strings.HasPrefix(e.Name(), ".") {
				return filepath.SkipDir
			}
			entries, err := os.ReadDir(p)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
					rel, err := filepath.Rel(filepath.Join(root, "codexsdk"), p)
					if err != nil {
						return err
					}
					packages = append(packages, rel)
					break
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(packages)
	return packages, nil
}

func overlayForwardRuntime(base, candidate string) error {
	names := map[string]bool{"go.mod": true, "go.sum": true}
	for _, root := range []string{base, candidate} {
		paths, err := gitNUL(root, "ls-files", "-z")
		if err != nil {
			return err
		}
		for _, p := range paths {
			if ForwardBaselineImplementationPath(p) {
				names[p] = true
				continue
			}
			if !(strings.HasPrefix(p, "llmkit/") || strings.HasPrefix(p, "llmcaller/")) || strings.Contains(p, "/testdata/") || strings.HasSuffix(p, ".go") || strings.HasSuffix(p, ".md") || filepath.Base(p) == "LICENSE" || filepath.Base(p) == ".gitignore" {
				continue
			}
			before, be := os.ReadFile(filepath.Join(base, p))
			after, ae := os.ReadFile(filepath.Join(candidate, p))
			if (be != nil && !os.IsNotExist(be)) || (ae != nil && !os.IsNotExist(ae)) || (be == nil) != (ae == nil) || !bytes.Equal(before, after) {
				return fmt.Errorf("unsupported forward runtime input drift: %s", p)
			}
		}
	}
	for p := range names {
		from, to := filepath.Join(candidate, p), filepath.Join(base, p)
		info, err := os.Lstat(from)
		if os.IsNotExist(err) {
			if err = os.Remove(to); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		real, err := filepath.EvalSymlinks(from)
		if err != nil || real != from || !info.Mode().IsRegular() {
			return fmt.Errorf("unsafe forward implementation input: %s", p)
		}
		data, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		if err = os.MkdirAll(filepath.Dir(to), 0700); err != nil {
			return err
		}
		if err = os.WriteFile(to, data, 0600); err != nil {
			return err
		}
	}
	return nil
}
