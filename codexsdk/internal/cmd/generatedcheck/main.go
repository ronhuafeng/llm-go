package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/generatedcheck"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("generatedcheck", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	moduleRoot := fs.String("module-root", ".", "codexsdk module root")
	expectedCommit := fs.String("expected-upstream-commit", "", "expected baseline source_commit")
	expectedRef := fs.String("expected-upstream-ref", "", "expected baseline source_ref_name")
	expectedKind := fs.String("expected-upstream-kind", "", "expected baseline source_ref_kind")
	writeArtifacts := fs.Bool("write-artifacts", false, "regenerate and write artifacts; do not check")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *writeArtifacts {
		if *expectedCommit != "" || *expectedRef != "" || *expectedKind != "" {
			fmt.Fprintf(os.Stderr, "generatedcheck: -write-artifacts cannot be combined with expected-upstream flags\n")
			return 2
		}
		if err := generatedcheck.WriteArtifacts(*moduleRoot); err != nil {
			fmt.Fprintf(os.Stderr, "generatedcheck: %v\n", err)
			return 1
		}
		return 0
	}

	if err := generatedcheck.Check(generatedcheck.Request{
		ModuleRoot:             *moduleRoot,
		ExpectedUpstreamCommit: *expectedCommit,
		ExpectedUpstreamRef:    *expectedRef,
		ExpectedUpstreamKind:   *expectedKind,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "generatedcheck: %v\n", err)
		return 1
	}
	return 0
}
