package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolsync"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolupgrade"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(stderr, "protocolupgrade: command is required: sync, diagnose, resume, verify-candidate, compare, apply, check, scope, stage, or publish\n")
		return 2
	}
	switch args[0] {
	case "sync":
		return runSync(args[1:], stdout, stderr, false)
	case "diagnose":
		return runSync(args[1:], stdout, stderr, true)
	case "resume":
		return runResume(args[1:], stdout, stderr)
	case "verify-candidate":
		return runVerifyCandidate(args[1:], stderr)
	case "compare":
		return runCompare(args[1:], stdout, stderr)
	case "apply":
		return runApply(args[1:], stdout, stderr)
	case "check":
		return runCheck(args[1:], stdout, stderr)
	case "scope":
		return runScope(args[1:], stderr)
	case "stage":
		return runStage(args[1:], stdout, stderr)
	case "publish":
		return runPublish(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "protocolupgrade: unknown command %q\n", args[0])
		return 2
	}
}

func runVerifyCandidate(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("verify-candidate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("candidate-dir", "", "candidate root")
	digest := fs.String("candidate-sha256", "", "initial candidate digest")
	ref := fs.String("target-ref", "", "selected upstream ref")
	kind := fs.String("target-kind", "", "selected upstream kind")
	sha := fs.String("target-sha", "", "selected upstream commit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := protocolsync.VerifyCandidate(*dir, *digest, *ref, *kind, *sha); err != nil {
		fmt.Fprintf(stderr, "protocolupgrade verify-candidate: %v\n", err)
		return 1
	}
	return 0
}

func runCompare(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	baseline := fs.String("baseline", "", "checked-in schema baseline directory")
	candidate := fs.String("candidate", "", "candidate schema directory")
	reports := fs.String("reports", "", "output directory for drift reports")
	sourceCommit := fs.String("source-commit", "", "resolved upstream source commit")
	sourceRef := fs.String("source-ref", "", "upstream tag/ref name")
	sourceRefKind := fs.String("source-ref-kind", "", "upstream target kind")
	codexVersion := fs.String("codex-version", "", "Codex generator version text")
	generator := fs.String("generator", "", "generator mode or provenance label")
	generatorDetail := fs.String("generator-detail", "", "generator command or candidate provenance")
	jsonOut := fs.Bool("json", false, "print drift summary JSON to stdout")
	verbose := fs.Bool("verbose", false, "print human-readable report paths and counts to stderr")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *reports == "" && !*jsonOut {
		fmt.Fprintf(stderr, "protocolupgrade compare: at least one of -reports or -json is required\n")
		return 2
	}
	report, err := protocolupgrade.Compare(protocolupgrade.CompareRequest{
		Baseline:        *baseline,
		Candidate:       *candidate,
		SourceCommit:    *sourceCommit,
		SourceRef:       *sourceRef,
		SourceRefKind:   *sourceRefKind,
		CodexVersion:    *codexVersion,
		Generator:       *generator,
		GeneratorDetail: *generatorDetail,
	})
	if err != nil {
		fmt.Fprintf(stderr, "protocolupgrade compare: %v\n", err)
		return 1
	}
	if *reports != "" {
		if _, err := protocolupgrade.WriteReports(*reports, report, *candidate); err != nil {
			fmt.Fprintf(stderr, "protocolupgrade compare: %v\n", err)
			return 1
		}
	}
	if *jsonOut {
		if err := encodeJSON(stdout, report); err != nil {
			fmt.Fprintf(stderr, "protocolupgrade compare: %v\n", err)
			return 1
		}
	}
	if *verbose {
		fmt.Fprintln(stderr, protocolupgrade.DiffCountSummary(report))
		if *reports != "" {
			fmt.Fprintf(stderr, "drift summary: %s/drift_summary.json\n", *reports)
			fmt.Fprintf(stderr, "matrix update skeleton: %s/%s\n", *reports, protocolupgrade.MatrixSkeletonName)
			fmt.Fprintf(stderr, "summary: %s/SUMMARY.md\n", *reports)
		}
	}
	return 0
}

func runApply(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	baseline := fs.String("baseline", "internal/protocolschema/appserver/v2", "checked-in schema baseline directory")
	candidate := fs.String("candidate", "", "trusted candidate schema directory")
	stableCandidate := fs.String("stable-candidate", "", "schema generated without experimental visibility")
	codexRepo := fs.String("codex-repo", "", "local openai/codex clone used to verify common.rs")
	reports := fs.String("reports", "", "candidate drift report directory")
	commonRS := fs.String("common-rs", "", "upstream common.rs response mapping source")
	commonRSSourceSHA := fs.String("common-rs-source-sha", "", "commit SHA that produced -common-rs")
	targetRef := fs.String("target-ref", "", "selected upstream ref name")
	targetKind := fs.String("target-kind", "", "selected upstream ref kind")
	targetSHA := fs.String("target-sha", "", "selected upstream commit SHA")
	moduleRoot := fs.String("module-root", ".", "codexsdk module root for generated Go")
	jsonOut := fs.Bool("json", false, "print a machine-readable summary")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	req := protocolupgrade.ApplyRequest{
		Baseline:          *baseline,
		Candidate:         *candidate,
		StableCandidate:   *stableCandidate,
		CodexRepo:         *codexRepo,
		Reports:           *reports,
		CommonRS:          *commonRS,
		CommonRSSourceSHA: *commonRSSourceSHA,
		TargetRef:         *targetRef,
		TargetKind:        *targetKind,
		TargetSHA:         *targetSHA,
		ModuleRoot:        *moduleRoot,
	}
	planned, err := protocolupgrade.Plan(req)
	if err != nil {
		fmt.Fprintf(stderr, "protocolupgrade apply: plan: %v\n", err)
		return 1
	}
	if planned.Status != protocolupgrade.PlanReady {
		if planned.Issue != nil {
			fmt.Fprintf(stderr, "protocolupgrade apply: unresolved %s %s: %s\n", planned.Issue.Stage, planned.Issue.Path, planned.Issue.Reason)
		} else {
			fmt.Fprintf(stderr, "protocolupgrade apply: candidate is not ready\n")
		}
		return 1
	}
	result, err := planned.Apply()
	if err != nil {
		fmt.Fprintf(stderr, "protocolupgrade apply: %v\n", err)
		return 1
	}
	if *jsonOut {
		if err := encodeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "protocolupgrade apply: %v\n", err)
			return 1
		}
	}
	return 0
}

func runCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	moduleRoot := fs.String("module-root", ".", "codexsdk module root")
	baseline := fs.String("baseline", "", "checked-in schema baseline directory")
	candidate := fs.String("candidate", "", "optional candidate schema directory")
	jsonOut := fs.Bool("json", false, "print a machine-readable summary")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	result, err := protocolupgrade.Check(protocolupgrade.CheckRequest{
		ModuleRoot: *moduleRoot,
		Baseline:   *baseline,
		Candidate:  *candidate,
	})
	if *jsonOut {
		if encodeErr := encodeJSON(stdout, result); encodeErr != nil {
			fmt.Fprintf(stderr, "protocolupgrade check: %v\n", encodeErr)
			if err == nil {
				err = encodeErr
			}
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "protocolupgrade check: %v\n", err)
		return 1
	}
	return 0
}

func runSync(args []string, stdout, stderr io.Writer, diagnostic bool) int {
	command := "sync"
	if diagnostic {
		command = "diagnose"
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoRoot := fs.String("repo-root", "", "repository root")
	moduleRoot := fs.String("module-root", "", "codexsdk module root")
	upstreamRepo := fs.String("upstream-repo", "https://github.com/openai/codex.git", "openai/codex git remote")
	upstreamRef := fs.String("upstream-ref", "", "optional tag, ref, or full SHA; empty selects the latest stable rust-vX.Y.Z tag")
	allowDowngrade := fs.Bool("allow-downgrade", false, "allow an explicit older stable tag")
	forceCompare := fs.Bool("force-compare", false, "generate and compare even when the baseline already matches")
	validationOnly := fs.Bool("validation-only", false, "verify the exact accepted baseline without applying or publishing")
	eventName := fs.String("event-name", "", "GitHub event name for scheduled vs manual policy")
	jsonOut := fs.Bool("json", false, "print a machine-readable result")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *repoRoot == "" {
		fmt.Fprintf(stderr, "protocolupgrade %s: -repo-root is required\n", command)
		return 2
	}
	result, err := protocolsync.Sync(protocolsync.SyncRequest{
		RepoRoot:       *repoRoot,
		ModuleRoot:     *moduleRoot,
		UpstreamRepo:   *upstreamRepo,
		UpstreamRef:    *upstreamRef,
		AllowDowngrade: *allowDowngrade,
		ForceCompare:   *forceCompare,
		ValidationOnly: *validationOnly,
		Diagnostic:     diagnostic,
		EventName:      *eventName,
	})
	if *jsonOut {
		if encodeErr := encodeJSON(stdout, result); encodeErr != nil {
			fmt.Fprintf(stderr, "protocolupgrade %s: %v\n", command, encodeErr)
			if err == nil {
				err = encodeErr
			}
		}
	}
	if writeErr := protocolsync.WriteGitHubOutput(os.Getenv("GITHUB_OUTPUT"), result); writeErr != nil {
		fmt.Fprintf(stderr, "protocolupgrade %s: write github output: %v\n", command, writeErr)
		if err == nil {
			err = writeErr
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "protocolupgrade %s: %v\n", command, err)
		return 1
	}
	fmt.Fprintf(stderr, "protocolupgrade %s: %s\n", command, result.Reason)
	return 0
}

func runResume(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoRoot := fs.String("repo-root", "", "repository root")
	moduleRoot := fs.String("module-root", "", "codexsdk module root")
	candidateDir := fs.String("candidate-dir", "", "existing candidate root from the initial sync plan")
	candidateSHA256 := fs.String("candidate-sha256", "", "sha256 of the complete initial candidate set")
	targetRef := fs.String("target-ref", "", "selected upstream ref name")
	targetKind := fs.String("target-kind", "", "selected upstream ref kind")
	targetSHA := fs.String("target-sha", "", "selected upstream commit SHA")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	result, err := protocolsync.Resume(protocolsync.ResumeRequest{
		RepoRoot:        *repoRoot,
		ModuleRoot:      *moduleRoot,
		CandidateDir:    *candidateDir,
		CandidateSHA256: *candidateSHA256,
		TargetRef:       *targetRef,
		TargetKind:      *targetKind,
		TargetSHA:       *targetSHA,
	})
	if writeErr := protocolsync.WriteGitHubOutput(os.Getenv("GITHUB_OUTPUT"), result); writeErr != nil {
		fmt.Fprintf(stderr, "protocolupgrade resume: write github output: %v\n", writeErr)
		if err == nil {
			err = writeErr
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "protocolupgrade resume: %v\n", err)
		return 1
	}
	fmt.Fprintf(stderr, "protocolupgrade resume: %s\n", result.Reason)
	return 0
}

func runScope(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("scope", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoRoot := fs.String("repo-root", "", "repository root")
	phase := fs.String("phase", "agent", "agent, mechanical or final")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *repoRoot == "" {
		fmt.Fprintln(stderr, "protocolupgrade scope: -repo-root is required")
		return 2
	}
	if err := protocolsync.CheckScope(*repoRoot, *phase); err != nil {
		fmt.Fprintf(stderr, "protocolupgrade scope: %v\n", err)
		return 1
	}
	return 0
}

func runStage(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("stage", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoRoot := fs.String("repo-root", "", "repository root")
	phase := fs.String("phase", "final", "mechanical or final")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *repoRoot == "" {
		fmt.Fprintf(stderr, "protocolupgrade stage: -repo-root is required\n")
		return 2
	}
	paths, err := protocolsync.StagePaths(*repoRoot, *phase)
	if err != nil {
		fmt.Fprintf(stderr, "protocolupgrade stage: %v\n", err)
		return 1
	}
	fmt.Fprintf(stderr, "protocolupgrade stage: %d paths\n", len(paths))
	return 0
}

func runPublish(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoRoot := fs.String("repo-root", "", "repository root")
	baseBranch := fs.String("base-branch", "", "protected landing branch")
	branchPrefix := fs.String("branch-prefix", "codex/sync-upstream", "sync branch prefix")
	targetRef := fs.String("target-ref", "", "selected upstream ref")
	targetKind := fs.String("target-kind", "", "selected upstream kind")
	targetSHA := fs.String("target-sha", "", "selected upstream commit")
	remote := fs.String("remote", "origin", "git remote")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	prURL, err := protocolsync.Publish(protocolsync.PublishRequest{
		RepoRoot:         *repoRoot,
		BaseBranch:       *baseBranch,
		BranchPrefix:     *branchPrefix,
		TargetRef:        *targetRef,
		TargetKind:       *targetKind,
		TargetSHA:        *targetSHA,
		Remote:           *remote,
		GitHubOutputPath: os.Getenv("GITHUB_OUTPUT"),
	})
	if err != nil {
		fmt.Fprintf(stderr, "protocolupgrade publish: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, prURL)
	return 0
}

func encodeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(value)
}
