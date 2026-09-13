package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolupgrade"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(stderr, "protocolupgrade: command is required: compare, apply, or check\n")
		return 2
	}
	switch args[0] {
	case "compare":
		return runCompare(args[1:], stdout, stderr)
	case "apply":
		return runApply(args[1:], stdout, stderr)
	case "check":
		return runCheck(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "protocolupgrade: unknown command %q\n", args[0])
		return 2
	}
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
		if _, err := protocolupgrade.WriteReports(*reports, report); err != nil {
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
	skipCodegen := fs.Bool("skip-codegen", false, "do not regenerate protocolv2 Go files")
	jsonOut := fs.Bool("json", false, "print a machine-readable summary")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	result, err := protocolupgrade.Apply(protocolupgrade.ApplyRequest{
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
		SkipCodegen:       *skipCodegen,
	})
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

func encodeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(value)
}
