package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/generatedproof"
)

func main() {
	moduleRoot := flag.String("module-root", ".", "codexsdk module root")
	expectedCommit := flag.String("expected-upstream-commit", "", "expected baseline source_commit")
	expectedRef := flag.String("expected-upstream-ref", "", "expected baseline source_ref_name")
	expectedRepo := flag.String("expected-repository-commit", "", "expected git HEAD of the module checkout")
	jsonOut := flag.String("json-out", "", "write machine-readable proof JSON to this path")
	writeArtifacts := flag.Bool("write-artifacts", false, "regenerate and write artifacts; do not prove")
	flag.Parse()

	if *writeArtifacts {
		if *jsonOut != "" || *expectedCommit != "" || *expectedRef != "" || *expectedRepo != "" {
			fmt.Fprintf(os.Stderr, "generatedproof: -write-artifacts cannot be combined with proof flags\n")
			os.Exit(2)
		}
		if err := generatedproof.WriteArtifacts(*moduleRoot); err != nil {
			fmt.Fprintf(os.Stderr, "generatedproof: %v\n", err)
			os.Exit(1)
		}
		return
	}

	result, err := generatedproof.Prove(generatedproof.Request{
		ModuleRoot:               *moduleRoot,
		ExpectedUpstreamCommit:   *expectedCommit,
		ExpectedUpstreamRef:      *expectedRef,
		ExpectedRepositoryCommit: *expectedRepo,
	})
	if *jsonOut != "" {
		raw, encodeErr := json.MarshalIndent(result, "", "  ")
		if encodeErr != nil {
			fmt.Fprintf(os.Stderr, "generatedproof: encode result: %v\n", encodeErr)
			os.Exit(2)
		}
		if writeErr := os.WriteFile(*jsonOut, append(raw, '\n'), 0o644); writeErr != nil {
			fmt.Fprintf(os.Stderr, "generatedproof: write %s: %v\n", *jsonOut, writeErr)
			os.Exit(2)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "generatedproof: %v\n", err)
		os.Exit(1)
	}
}
