// Command repoctl verifies repository-wide llm-go contracts and emits typed CI evidence.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ronhuafeng/llm-go/internal/tools/internal/repository"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "verify":
		return runVerify(args[1:])
	case "affected":
		return runAffected(args[1:])
	case "verify-module":
		return runVerifyModule(args[1:])
	case "verify-checkout":
		return runVerifyCheckout(args[1:])
	case "release-plan":
		return runReleasePlan(args[1:])
	case "authorize-tag":
		return runAuthorizeTag(args[1:])
	case "release-notes":
		return runReleaseNotes(args[1:])
	case "verify-tag":
		return runVerifyTag(args[1:])
	case "validate-draft-release":
		return runValidateDraftRelease(args[1:])
	case "finalize-release":
		return runFinalizeRelease(args[1:])
	case "validate-tag-ref":
		return runValidateTagRef(args[1:])
	default:
		usage()
		return 2
	}
}

func runValidateTagRef(args []string) int {
	flags := flag.NewFlagSet("validate-tag-ref", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	input := flags.String("input", "", "git ls-remote tag-ref evidence path")
	tag := flags.String("tag", "", "exact release tag")
	commit := flags.String("commit", "", "exact release commit")
	if !parseFlags(flags, args, "repoctl validate-tag-ref") || *input == "" || *tag == "" || *commit == "" {
		fmt.Fprintln(os.Stderr, "repoctl validate-tag-ref: -input, -tag, and -commit are required")
		return 2
	}
	data, err := os.ReadFile(*input)
	if err == nil {
		err = repository.ValidateRemoteTagRefs(data, *tag, *commit)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "repoctl validate-tag-ref: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stdout, "annotated remote tag matches release commit")
	return 0
}

func runFinalizeRelease(args []string) int {
	flags := flag.NewFlagSet("finalize-release", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	planPath := flags.String("plan", "", "release-plan JSON path")
	minimum := flags.String("minimum", "", "minimum-Go evidence path")
	current := flags.String("current", "", "current-Go evidence path")
	race := flags.String("race", "", "race evidence path")
	checkout := flags.String("checkout", "", "checkout evidence path")
	output := flags.String("output", "", "release authorization JSON path")
	if !parseFlags(flags, args, "repoctl finalize-release") || *planPath == "" || *minimum == "" || *current == "" || *race == "" || *checkout == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "repoctl finalize-release: -plan, -minimum, -current, -race, -checkout, and -output are required")
		return 2
	}
	plan, err := repository.ReadReleasePlan(*planPath)
	var authorization repository.ReleaseAuthorization
	if err == nil {
		authorization, err = repository.BuildReleaseAuthorization(plan, *planPath, *minimum, *current, *race, *checkout)
	}
	if err == nil {
		err = repository.WriteReleaseAuthorization(*output, authorization)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "repoctl finalize-release: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stdout, authorization.AuthorizationDigest)
	return 0
}

func runValidateDraftRelease(args []string) int {
	flags := flag.NewFlagSet("validate-draft-release", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	input := flags.String("input", "", "GitHub Release JSON path")
	tag := flags.String("tag", "", "exact release tag")
	if !parseFlags(flags, args, "repoctl validate-draft-release") || *input == "" || *tag == "" {
		fmt.Fprintln(os.Stderr, "repoctl validate-draft-release: -input and -tag are required")
		return 2
	}
	data, err := os.ReadFile(*input)
	if err == nil {
		err = repository.ValidateDraftRelease(data, *tag)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "repoctl validate-draft-release: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stdout, "matching Draft Release may be reused")
	return 0
}

func runReleasePlan(args []string) int {
	flags := flag.NewFlagSet("release-plan", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	root := flags.String("root", ".", "repository root")
	module := flags.String("module", "", "registered public module ID")
	version := flags.String("version", "", "target stable SemVer")
	commit := flags.String("commit", "", "exact release commit")
	mainRef := flags.String("main", "origin/main", "authoritative main ref")
	output := flags.String("output", "-", "release-plan JSON path, or - for stdout")
	if !parseFlags(flags, args, "repoctl release-plan") || *module == "" || *version == "" || *commit == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "repoctl release-plan: -module, -version, -commit, and -output are required")
		return 2
	}
	plan, err := repository.BuildReleasePlan(*root, *module, *version, *commit, *mainRef)
	if err == nil {
		err = repository.WriteReleasePlan(*output, plan)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "repoctl release-plan: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stderr, plan.PlanDigest)
	return 0
}

func runAuthorizeTag(args []string) int {
	flags := flag.NewFlagSet("authorize-tag", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	root := flags.String("root", ".", "repository root")
	planPath := flags.String("plan", "", "approved release-plan JSON path")
	authorizationPath := flags.String("authorization", "", "approved release authorization JSON path")
	evidenceDir := flags.String("evidence-dir", "", "directory containing authorized plan and evidence")
	digest := flags.String("digest", "", "approved release-authorization digest")
	commit := flags.String("commit", "", "exact approved commit")
	tag := flags.String("tag", "", "exact approved tag")
	mainRef := flags.String("main", "origin/main", "authoritative main ref")
	if !parseFlags(flags, args, "repoctl authorize-tag") || *planPath == "" || *authorizationPath == "" || *evidenceDir == "" || *digest == "" || *commit == "" || *tag == "" {
		fmt.Fprintln(os.Stderr, "repoctl authorize-tag: -plan, -authorization, -evidence-dir, -digest, -commit, and -tag are required")
		return 2
	}
	plan, err := repository.ReadReleasePlan(*planPath)
	var authorization repository.ReleaseAuthorization
	if err == nil {
		authorization, err = repository.ReadReleaseAuthorization(*authorizationPath)
	}
	if err == nil {
		err = repository.AuthorizeTag(*root, plan, authorization, *evidenceDir, *digest, *commit, *tag, *mainRef)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "repoctl authorize-tag: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stdout, "approved release authorization permits "+*tag)
	return 0
}

func runReleaseNotes(args []string) int {
	flags := flag.NewFlagSet("release-notes", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	planPath := flags.String("plan", "", "release-plan JSON path")
	output := flags.String("output", "-", "release notes path, or - for stdout")
	if !parseFlags(flags, args, "repoctl release-notes") || *planPath == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "repoctl release-notes: -plan and -output are required")
		return 2
	}
	plan, err := repository.ReadReleasePlan(*planPath)
	var notes string
	if err == nil {
		notes, err = repository.ReleaseNotes(plan)
	}
	if err == nil {
		if *output == "-" {
			_, err = fmt.Fprint(os.Stdout, notes)
		} else {
			err = os.WriteFile(*output, []byte(notes), 0o644)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "repoctl release-notes: %v\n", err)
		return 1
	}
	return 0
}

func runVerifyTag(args []string) int {
	flags := flag.NewFlagSet("verify-tag", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	root := flags.String("root", ".", "repository root")
	planPath := flags.String("plan", "", "approved release-plan JSON path")
	authorizationPath := flags.String("authorization", "", "approved release authorization JSON path")
	evidenceDir := flags.String("evidence-dir", "", "directory containing authorized plan and evidence")
	digest := flags.String("digest", "", "approved release-authorization digest")
	evidencePath := flags.String("evidence", "", "published evidence JSON path")
	timeout := flags.Duration("timeout", 10*time.Minute, "maximum public proxy propagation wait")
	retry := flags.Duration("retry", 15*time.Second, "public proxy retry interval")
	commandTimeout := flags.Duration("command-timeout", 2*time.Minute, "maximum duration of one Go command")
	if !parseFlags(flags, args, "repoctl verify-tag") || *planPath == "" || *authorizationPath == "" || *evidenceDir == "" || *digest == "" || *evidencePath == "" {
		fmt.Fprintln(os.Stderr, "repoctl verify-tag: -plan, -authorization, -evidence-dir, -digest, and -evidence are required")
		return 2
	}
	plan, err := repository.ReadReleasePlan(*planPath)
	var authorization repository.ReleaseAuthorization
	if err != nil {
		fmt.Fprintf(os.Stderr, "repoctl verify-tag: %v\n", err)
		return 1
	}
	authorization, err = repository.ReadReleaseAuthorization(*authorizationPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "repoctl verify-tag: %v\n", err)
		return 1
	}
	evidence, verifyErr := repository.VerifyPublishedTag(context.Background(), *root, plan, authorization, *evidenceDir, *digest, repository.PublishOptions{
		Proxy: "https://proxy.golang.org", SumDB: "sum.golang.org", Timeout: *timeout,
		RetryInterval: *retry, CommandTimeout: *commandTimeout,
	})
	writeErr := repository.WritePublishedEvidence(*evidencePath, evidence)
	if verifyErr != nil {
		fmt.Fprintf(os.Stderr, "repoctl verify-tag: %v\n", verifyErr)
	}
	if writeErr != nil {
		fmt.Fprintf(os.Stderr, "repoctl verify-tag: write evidence: %v\n", writeErr)
	}
	if verifyErr != nil || writeErr != nil {
		return 1
	}
	return 0
}

func runVerify(args []string) int {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	root := flags.String("root", ".", "repository root")
	if !parseFlags(flags, args, "repoctl verify") {
		return 2
	}
	if err := repository.Verify(*root); err != nil {
		fmt.Fprintf(os.Stderr, "repoctl verify: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stdout, "repository contract verified")
	return 0
}

func runAffected(args []string) int {
	flags := flag.NewFlagSet("affected", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	root := flags.String("root", ".", "repository root")
	base := flags.String("base", "", "base revision; empty or all-zero means all tracked paths")
	head := flags.String("head", "HEAD", "head revision")
	output := flags.String("output", "-", "affected-plan JSON path, or - for stdout")
	if !parseFlags(flags, args, "repoctl affected") {
		return 2
	}
	plan, err := repository.Affected(*root, *base, *head)
	if err == nil {
		err = repository.WriteAffectedPlan(*output, plan)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "repoctl affected: %v\n", err)
		return 1
	}
	return 0
}

func runVerifyModule(args []string) int {
	flags := flag.NewFlagSet("verify-module", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	root := flags.String("root", ".", "repository root")
	module := flags.String("module", "", "registered module ID")
	stage := flags.String("stage", "", "verification stage: minimum, current, or race")
	evidencePath := flags.String("evidence", "", "evidence JSON path")
	if !parseFlags(flags, args, "repoctl verify-module") || *module == "" || *stage == "" || *evidencePath == "" {
		fmt.Fprintln(os.Stderr, "repoctl verify-module: -module, -stage, and -evidence are required")
		return 2
	}
	evidence, verifyErr := repository.VerifyModule(*root, *module, *stage)
	writeErr := repository.WriteEvidence(*evidencePath, evidence)
	if verifyErr != nil {
		fmt.Fprintf(os.Stderr, "repoctl verify-module: %v\n", verifyErr)
	}
	if writeErr != nil {
		fmt.Fprintf(os.Stderr, "repoctl verify-module: write evidence: %v\n", writeErr)
	}
	if verifyErr != nil || writeErr != nil {
		return 1
	}
	return 0
}

func runVerifyCheckout(args []string) int {
	flags := flag.NewFlagSet("verify-checkout", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	root := flags.String("root", ".", "repository root")
	planPath := flags.String("plan", "", "affected-plan JSON path")
	evidencePath := flags.String("evidence", "", "evidence JSON path")
	if !parseFlags(flags, args, "repoctl verify-checkout") || *planPath == "" || *evidencePath == "" {
		fmt.Fprintln(os.Stderr, "repoctl verify-checkout: -plan and -evidence are required")
		return 2
	}
	plan, err := repository.ReadAffectedPlan(*planPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "repoctl verify-checkout: %v\n", err)
		return 1
	}
	evidence, verifyErr := repository.VerifyCheckout(*root, plan)
	writeErr := repository.WriteEvidence(*evidencePath, evidence)
	if verifyErr != nil {
		fmt.Fprintf(os.Stderr, "repoctl verify-checkout: %v\n", verifyErr)
	}
	if writeErr != nil {
		fmt.Fprintf(os.Stderr, "repoctl verify-checkout: write evidence: %v\n", writeErr)
	}
	if verifyErr != nil || writeErr != nil {
		return 1
	}
	return 0
}

func parseFlags(flags *flag.FlagSet, args []string, command string) bool {
	if err := flags.Parse(args); err != nil {
		return false
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "%s: unexpected arguments\n", command)
		return false
	}
	return true
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: repoctl <verify|affected|verify-module|verify-checkout|release-plan|finalize-release|authorize-tag|release-notes|verify-tag|validate-draft-release|validate-tag-ref> [options]")
}
