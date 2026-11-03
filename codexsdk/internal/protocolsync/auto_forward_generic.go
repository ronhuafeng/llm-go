package protocolsync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// VerifyForwardCandidate executes native tests from the exact candidate snapshot.
// B and M bind the integration transaction but never supply tests or schema to H.
// Fresh exact-H CI and review are still required before integration.
func VerifyForwardCandidate(ctx context.Context, candidateRoot, originalBaseRoot, currentBaseRoot, diagnosticDir string) error {
	return verifyForwardCandidate(ctx, candidateRoot, originalBaseRoot, currentBaseRoot, diagnosticDir, runForwardCandidate)
}

type forwardCandidateRequest struct {
	RepoRoot, CandidateDir string
	Output                 io.Writer
}

func runForwardCandidate(ctx context.Context, req forwardCandidateRequest) error {
	if err := os.MkdirAll(filepath.Join(req.RepoRoot, "codexsdk/.cache/openai-codex"), 0700); err != nil {
		return err
	}
	sandbox, err := newNativeSandbox(ctx, req.RepoRoot, req.CandidateDir)
	if err != nil {
		return err
	}
	// Tests may create temporary in-module fixtures; the disposable snapshot
	// is rehashed after execution, while Git and external inputs stay read-only.
	cmd := sandbox.command(ctx, filepath.Join(sandbox.goroot, "bin/go"), "-C", req.RepoRoot, "test", "-count=1", "-race", "./...")
	cmd.Stdout, cmd.Stderr = req.Output, req.Output
	return cmd.Run()
}

func verifyForwardCandidate(ctx context.Context, candidateRoot, originalBaseRoot, currentBaseRoot, diagnosticDir string, run func(context.Context, forwardCandidateRequest) error) (retErr error) {
	roots := []string{candidateRoot, originalBaseRoot, currentBaseRoot}
	for _, root := range roots {
		if !filepath.IsAbs(root) {
			return errors.New("forward candidate inputs must be absolute")
		}
		real, err := filepath.EvalSymlinks(root)
		if err != nil || real != filepath.Clean(root) {
			return errors.New("forward candidate input is missing or traverses a symlink")
		}
	}
	for _, base := range roots[1:] {
		if goalWithin(candidateRoot, base) || goalWithin(base, candidateRoot) {
			return errors.New("forward candidate must be separate from candidate")
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
		log.record("footer", "forward-candidate", map[string]any{"passed": retErr == nil, "complete": log.err == nil, "error": goalError(retErr)})
		if err := log.file.Close(); err != nil && log.err == nil {
			log.err = err
		}
		if log.err != nil {
			retErr = errors.Join(retErr, &GoalVerificationIncompleteError{Err: log.err})
		}
	}()
	work, err := os.MkdirTemp("", "forward-candidate-")
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
		log.record("snapshot", "forward-candidate", map[string]any{"input": labels[i], "sha256": digests[i]})
	}
	defer func() {
		for i, root := range roots {
			for _, path := range []string{root, snapshots[i]} {
				digest, err := goalSnapshot(ctx, path, "")
				if err != nil {
					retErr = errors.Join(retErr, fmt.Errorf("revalidate %s: %w", labels[i], err))
				} else if digest != digests[i] {
					retErr = errors.Join(retErr, fmt.Errorf("%s changed during forward candidate verification", labels[i]))
				}
			}
		}
	}()
	// No target is inferred. The isolated command uses H's implementation,
	// schemas and tests together; B/M snapshots are identity evidence only.
	retained := filepath.Join(work, "readonly-inputs")
	if err := os.Mkdir(retained, 0700); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	output := &goalOutput{log: log, gate: "forward-candidate"}
	err = run(ctx, forwardCandidateRequest{RepoRoot: snapshots[0], CandidateDir: retained, Output: output})
	output.flush(true)
	log.record("native_tests", output.gate, map[string]any{"passed": err == nil, "error": goalError(err)})
	if err != nil {
		return fmt.Errorf("candidate native tests: %w", err)
	}
	if log.err != nil {
		return &GoalVerificationIncompleteError{Err: log.err}
	}
	return nil
}

// ForwardRuntimeImplementationPath identifies the existing generic reconciliation scope.
func ForwardRuntimeImplementationPath(p string) bool {
	return (strings.HasPrefix(p, "llmkit/") || strings.HasPrefix(p, "llmcaller/")) && !strings.Contains(p, "/testdata/") && strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go")
}
