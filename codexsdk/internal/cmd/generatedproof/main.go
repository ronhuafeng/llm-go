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
	upstreamRef := flag.String("upstream-ref", "", "upstream ref name observed by the caller")
	repositoryCommit := flag.String("repository-commit", "", "repository commit this proof ran against")
	jsonOut := flag.String("json-out", "", "write machine-readable proof JSON to this path")
	writeArtifacts := flag.Bool("write-artifacts", false, "write regenerated artifacts into the module tree")
	flag.Parse()

	result, err := generatedproof.Prove(generatedproof.Request{
		ModuleRoot:             *moduleRoot,
		ExpectedUpstreamCommit: *expectedCommit,
		UpstreamRef:            *upstreamRef,
		RepositoryCommit:       *repositoryCommit,
		WriteArtifacts:         *writeArtifacts,
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
