package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/generatedproof"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("generatedproof", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	moduleRoot := fs.String("module-root", ".", "codexsdk module root")
	expectedCommit := fs.String("expected-upstream-commit", "", "expected baseline source_commit")
	expectedRef := fs.String("expected-upstream-ref", "", "expected baseline source_ref_name")
	expectedKind := fs.String("expected-upstream-kind", "", "expected baseline source_ref_kind")
	expectedRepo := fs.String("expected-repository-commit", "", "expected git HEAD of the module checkout")
	jsonOut := fs.String("json-out", "", "write machine-readable proof JSON to this path")
	writeArtifacts := fs.Bool("write-artifacts", false, "regenerate and write artifacts; do not prove")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *writeArtifacts {
		if *jsonOut != "" || *expectedCommit != "" || *expectedRef != "" || *expectedKind != "" || *expectedRepo != "" {
			fmt.Fprintf(os.Stderr, "generatedproof: -write-artifacts cannot be combined with proof flags\n")
			return 2
		}
		if err := generatedproof.WriteArtifacts(*moduleRoot); err != nil {
			fmt.Fprintf(os.Stderr, "generatedproof: %v\n", err)
			return 1
		}
		return 0
	}

	result, err := generatedproof.Prove(generatedproof.Request{
		ModuleRoot:               *moduleRoot,
		ExpectedUpstreamCommit:   *expectedCommit,
		ExpectedUpstreamRef:      *expectedRef,
		ExpectedUpstreamKind:     *expectedKind,
		ExpectedRepositoryCommit: *expectedRepo,
	})
	if *jsonOut != "" {
		raw, encodeErr := json.MarshalIndent(result, "", "  ")
		if encodeErr != nil {
			fmt.Fprintf(os.Stderr, "generatedproof: encode result: %v\n", encodeErr)
			return 2
		}
		if writeErr := os.WriteFile(*jsonOut, append(raw, '\n'), 0o644); writeErr != nil {
			fmt.Fprintf(os.Stderr, "generatedproof: write %s: %v\n", *jsonOut, writeErr)
			return 2
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "generatedproof: %v\n", err)
		return 1
	}
	return 0
}
