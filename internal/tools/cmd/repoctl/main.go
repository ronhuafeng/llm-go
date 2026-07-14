// Command repoctl verifies repository-wide llm-go contracts.
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
	if len(args) == 0 || args[0] != "verify" {
		fmt.Fprintln(os.Stderr, "usage: repoctl verify [-root directory]")
		return 2
	}

	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "repoctl verify: unexpected arguments")
		return 2
	}

	if err := repository.Verify(*root); err != nil {
		fmt.Fprintf(os.Stderr, "repoctl verify: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stdout, "repository contract verified")
	return 0
}
