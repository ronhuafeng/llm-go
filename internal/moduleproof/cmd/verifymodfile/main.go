package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ronhuafeng/llm-go/internal/moduleproof"
)

type replaceFlags []moduleproof.Replace

func (r *replaceFlags) String() string {
	parts := make([]string, 0, len(*r))
	for _, item := range *r {
		parts = append(parts, item.OldPath+"="+item.NewPath)
	}
	return strings.Join(parts, ",")
}

func (r *replaceFlags) Set(value string) error {
	oldPath, newPath, ok := strings.Cut(value, "=")
	if !ok || oldPath == "" || newPath == "" {
		return fmt.Errorf("replace must be module=path, got %q", value)
	}
	*r = append(*r, moduleproof.Replace{OldPath: oldPath, NewPath: newPath})
	return nil
}

func main() {
	goMod := flag.String("go-mod", "", "committed go.mod path")
	goSum := flag.String("go-sum", "", "committed go.sum path")
	destMod := flag.String("out-mod", "", "temporary go.mod path")
	destSum := flag.String("out-sum", "", "temporary go.sum path")
	var replaces replaceFlags
	flag.Var(&replaces, "replace", "current-source replacement module=relativePath (repeatable)")
	flag.Parse()
	if err := moduleproof.WriteVerifyModfile(*goMod, *goSum, replaces, *destMod, *destSum); err != nil {
		fmt.Fprintf(os.Stderr, "verifymodfile: %v\n", err)
		os.Exit(1)
	}
}
