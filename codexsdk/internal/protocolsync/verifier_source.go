package protocolsync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// runVerifierSourceGates runs root tidy/vet on accepted+patch before temporary
// proof code is injected. Permanent tests belong to the baseline/target matrix.
// Full root CI remains required: some accepted tests need writable temp folders.
func runVerifierSourceGates(ctx context.Context, root, candidate string) ([]verifierProofLane, error) {
	var lanes []verifierProofLane
	for _, gate := range []string{"tidy", "vet"} {
		lane, err := runVerifierSourceGate(ctx, root, candidate, gate, nil)
		lanes = append(lanes, lane)
		if err != nil {
			return lanes, err
		}
	}
	return lanes, nil
}
func verifierSourceCommand(ctx context.Context, s oracleSandbox, gate string) (*exec.Cmd, error) {
	var args []string
	switch gate {
	case "tidy":
		args = []string{"mod", "tidy", "-diff"}
	case "vet":
		args = []string{"vet", "./..."}
	case "permanent-tests":
		args = []string{"test", "-race", "-count=1", "-json", "./codexsdk/internal/schemaoracle"}
	default:
		return nil, fmt.Errorf("unknown verifier source gate %q", gate)
	}
	s.readonly = append(s.readonly, s.root)
	// oracleSandbox's cwd is codexsdk; -C is handled by the trusted Go executable
	// before the subcommand, so these are genuinely root-module gates.
	cmd := s.command(ctx, filepath.Join(s.goroot, "bin", "go"), append([]string{"-C", s.root}, args...)...)
	return cmd, nil
}
func runVerifierSourceGate(ctx context.Context, root, candidate, gate string, expected map[string]bool) (verifierProofLane, error) {
	lane := verifierProofLane{Case: "source", Lane: gate}
	s, err := newVerifierProofSandbox(ctx, root, candidate)
	if err != nil {
		return lane, err
	}
	defer os.RemoveAll(s.work)
	cmd, err := verifierSourceCommand(ctx, s, gate)
	if err != nil {
		return lane, err
	}
	before, err := repairPatch(root)
	if err != nil {
		return lane, err
	}
	var stdout, stderr verifierSourceOutput
	events := newVerifierSourceEvents(expected)
	if gate == "permanent-tests" {
		cmd.Stdout = events
	} else {
		cmd.Stdout = &stdout
	}
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	after, err := repairPatch(root)
	if err != nil {
		return lane, err
	}
	if repairDigest(before) != repairDigest(after) {
		return lane, fmt.Errorf("verifier source gate changed frozen input")
	}
	if ctx.Err() != nil {
		return lane, ctx.Err()
	}
	if stdout.truncated || stderr.truncated {
		return lane, fmt.Errorf("verifier source %s output truncated; outcome unknown", gate)
	}
	lane.Started = runErr == nil
	if _, ok := runErr.(*exec.ExitError); ok {
		lane.Started = true
	}
	if gate == "permanent-tests" {
		_, _ = events.baseline.diagnostic.Write([]byte(stderr.String()))
		lane.Diagnostic = boundedRepairDiagnostic(events.baseline.diagnostic.String())
		err = verifierSourceTestOutcome(events, runErr, stderr.String())
	} else {
		lane.Diagnostic = boundedRepairDiagnostic(stdout.String() + stderr.String())
		err = verifierSourceToolOutcome(gate, stdout.String(), stderr.String(), runErr)
	}
	lane.Passed = err == nil
	lane.Failed = err != nil
	return lane, err
}

// Incomplete tool output cannot safely distinguish a source failure from a
// later cache/sandbox error, even if its retained prefix looks like a tidy diff.
type verifierSourceOutput struct {
	repairOutput
	truncated bool
}

func (w *verifierSourceOutput) Write(p []byte) (int, error) {
	if w.Len()+len(p) > 65536 {
		w.truncated = true
	}
	return w.repairOutput.Write(p)
}

// Only trusted Go's complete, narrowly shaped tidy diff is author feedback.
// Missing cache, a new dependency requiring network, or unknown tool output is
// infrastructure/unknown evidence and cannot consume another author turn.
func verifierSourceToolOutcome(gate, stdout, stderr string, runErr error) error {
	output := stdout + stderr
	if runErr == nil {
		if gate == "tidy" && strings.TrimSpace(output) != "" {
			return fmt.Errorf("verifier tidy returned unexplained output: %s", boundedRepairDiagnostic(output))
		}
		return nil
	}
	cause := fmt.Errorf("verifier source %s: %v: %s", gate, runErr, boundedRepairDiagnostic(output))
	exit, ok := runErr.(*exec.ExitError)
	if !ok || exit.ExitCode() != 1 || verifierSourceInfrastructure(output) {
		return cause
	}
	if gate == "tidy" && stderr == "" && verifierTidyDiff(stdout) {
		return verifierFeedback(cause)
	}
	if gate == "vet" && verifierGoDiagnostic.MatchString(output) {
		return verifierFeedback(cause)
	}
	return cause
}

var verifierGoDiagnostic = regexp.MustCompile(`(?m)^[^\n]*\.go:[0-9]+:[0-9]+: .+`)

func verifierSourceInfrastructure(output string) bool {
	lower := strings.ToLower(output)
	for _, marker := range []string{"goproxy=off", "gosumdb=off", "checksum database", "go: downloading ", "module lookup disabled", "no such file or directory", "executable file not found", "missing go.sum entry", "cannot find module", "network is unreachable", "network access", "permission denied", "operation not permitted", "read-only file system", "no space left on device", "resource temporarily unavailable", "signal: killed", "fatal error:", "panic:"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
func verifierTidyDiff(output string) bool {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	seen := map[string]bool{}
	i := 0
	for i < len(lines) {
		name := ""
		for _, file := range []string{"go.mod", "go.sum"} {
			if lines[i] == "diff current/"+file+" tidy/"+file {
				name = file
			}
		}
		if name == "" || seen[name] || i+3 >= len(lines) {
			return false
		}
		seen[name] = true
		if lines[i+1] != "--- current/"+name || lines[i+2] != "+++ tidy/"+name {
			return false
		}
		i += 3
		hunk, change := false, false
		for i < len(lines) && !strings.HasPrefix(lines[i], "diff ") {
			line := lines[i]
			if line == "" && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "diff ") {
				// Go separates consecutive file diffs by an empty line.
			} else if strings.HasPrefix(line, "@@ ") && strings.HasSuffix(line, " @@") {
				hunk = true
			} else if len(line) > 0 && hunk && strings.ContainsRune(" +-", rune(line[0])) {
				change = change || line[0] == '+' || line[0] == '-'
			} else {
				return false
			}
			i++
		}
		if !hunk || !change {
			return false
		}
	}
	return len(seen) > 0
}

// verifierAssertionError distinguishes an executed permanent assertion from a
// compiler, environment, or incomplete test outcome. Only the former can prove
// that the old acceptance boundary falsely rejected a target.
type verifierAssertionError struct{ error }

var verifierAssertionDiagnostic = regexp.MustCompile(`(?m)^\s*[^\s]+\.go:[0-9]+: .+`)

type verifierSourceEvents struct {
	failedExpected, assertionOutput, eventRuns                                      map[string]bool
	baseline                                                                        baselineTestEvents
	failedTest, buildFailed, compilerDiagnostic, infrastructure, skipped, malformed bool
}

func newVerifierSourceEvents(expected map[string]bool) *verifierSourceEvents {
	return &verifierSourceEvents{failedExpected: map[string]bool{}, assertionOutput: map[string]bool{}, eventRuns: map[string]bool{}, baseline: baselineTestEvents{expected: expected, ran: map[string]bool{}, passed: map[string]bool{}}}
}
func (w *verifierSourceEvents) Write(p []byte) (int, error) {
	framed := w.baseline.pending + string(p)
	n, err := w.baseline.Write(p)
	for {
		line, rest, ok := strings.Cut(framed, "\n")
		if !ok {
			if len(framed) > 4<<20 {
				w.malformed = true
			}
			break
		}
		framed = rest
		var e struct{ Action, Package, Test, Output string }
		if json.Unmarshal([]byte(line), &e) != nil {
			w.malformed = true
			continue
		}
		key := e.Package + "/" + e.Test
		if w.baseline.expected[key] {
			switch e.Action {
			case "run":
				w.eventRuns[key] = true
			case "pass", "fail":
				if !w.eventRuns[key] {
					w.malformed = true
				}
			}
		}
		if verifierSourceInfrastructure(e.Output) {
			w.infrastructure = true
		}
		if e.Action == "build-output" && verifierGoDiagnostic.MatchString(e.Output) {
			w.compilerDiagnostic = true
		}
		if e.Action == "build-fail" {
			w.buildFailed = true
		}
		if e.Action == "output" && e.Test != "" && verifierAssertionDiagnostic.MatchString(e.Output) {
			key := e.Package + "/" + strings.SplitN(e.Test, "/", 2)[0]
			if w.baseline.expected[key] && w.eventRuns[key] {
				w.assertionOutput[key] = true
			}
		}
		if e.Action == "fail" && e.Test != "" {
			w.failedTest = true
			key := e.Package + "/" + e.Test
			if w.baseline.expected[key] && w.eventRuns[key] {
				w.failedExpected[key] = true
			}
		}
		if e.Package == "github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracle" && e.Action == "skip" && e.Test != "" {
			w.skipped = true
		}
	}
	return n, err
}
func verifierSourceTestOutcome(events *verifierSourceEvents, runErr error, stderr string) error {
	proofErr := events.baseline.verify()
	if runErr == nil && proofErr == nil && !events.skipped {
		return nil
	}
	cause := fmt.Errorf("verifier permanent source tests: process=%v evidence=%v: %s", runErr, proofErr, boundedRepairDiagnostic(events.baseline.diagnostic.String()))
	if events.malformed || events.baseline.pending != "" || events.infrastructure || verifierSourceInfrastructure(stderr) || events.skipped {
		return cause
	}
	exit, ok := runErr.(*exec.ExitError)
	if ok && exit.ExitCode() == 1 && !events.buildFailed && !events.compilerDiagnostic && strings.TrimSpace(stderr) == "" && len(events.failedExpected) > 0 {
		complete := len(events.baseline.expected) > 0
		for key := range events.baseline.expected {
			complete = complete && events.baseline.ran[key] && (events.baseline.passed[key] || events.failedExpected[key] && events.assertionOutput[key])
		}
		if complete {
			return verifierFeedback(&verifierAssertionError{cause})
		}
	}
	if ok && exit.ExitCode() == 1 && (events.failedTest || events.buildFailed && events.compilerDiagnostic) {
		return verifierFeedback(cause)
	}
	return cause
}
