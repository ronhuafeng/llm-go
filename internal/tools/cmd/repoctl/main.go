// Command repoctl verifies repository-wide llm-go contracts and emits typed CI evidence.
package main

import (
	"flag"
	"fmt"
	"os"

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
	default:
		usage()
		return 2
	}
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
	fmt.Fprintln(os.Stderr, "usage: repoctl <verify|affected|verify-module|verify-checkout> [options]")
}
