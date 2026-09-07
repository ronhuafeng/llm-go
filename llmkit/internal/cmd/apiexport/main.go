package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ronhuafeng/llm-go/llmkit/internal/architecture"
)

func main() {
	root := flag.String("root", "", "module root to export; defaults to the working directory")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: apiexport [-root <module-root>]")
		os.Exit(2)
	}
	sourceRoot, err := resolveRoot(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve module root: %v\n", err)
		os.Exit(1)
	}
	inventory, err := architecture.ExportPublicAPI(sourceRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "export public API: %v\n", err)
		os.Exit(1)
	}
	if _, err := os.Stdout.WriteString(inventory); err != nil {
		fmt.Fprintf(os.Stderr, "write public API: %v\n", err)
		os.Exit(1)
	}
}

func resolveRoot(root string) (string, error) {
	if root != "" {
		return root, nil
	}
	return os.Getwd()
}
