package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

const reportFormatVersion = 1

type inventoryImpact string

const (
	impactMetadataOnly inventoryImpact = "metadata-only"
	impactAdditive     inventoryImpact = "additive"
	impactBreaking     inventoryImpact = "breaking"
)

type inventoryReport struct {
	FormatVersion  int             `json:"format_version"`
	BaselineSHA256 string          `json:"baseline_sha256"`
	TargetSHA256   string          `json:"target_sha256"`
	Impact         inventoryImpact `json:"impact"`
}

func main() {
	baselinePath := flag.String("baseline", "", "canonical inventory from the comparison tag")
	targetPath := flag.String("target", "", "canonical inventory from the candidate source")
	flag.Parse()
	if *baselinePath == "" || *targetPath == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: apiinventoryreport -baseline <path> -target <path>")
		os.Exit(2)
	}
	baseline, err := os.ReadFile(*baselinePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read baseline inventory: %v\n", err)
		os.Exit(1)
	}
	target, err := os.ReadFile(*targetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read target inventory: %v\n", err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(buildReport(baseline, target)); err != nil {
		fmt.Fprintf(os.Stderr, "encode inventory report: %v\n", err)
		os.Exit(1)
	}
}

func buildReport(baseline, target []byte) inventoryReport {
	return inventoryReport{
		FormatVersion:  reportFormatVersion,
		BaselineSHA256: sha256Hex(baseline),
		TargetSHA256:   sha256Hex(target),
		Impact:         classifyInventory(baseline, target),
	}
}

func classifyInventory(baseline, target []byte) inventoryImpact {
	baselineLines := inventoryLines(baseline)
	targetLines := inventoryLines(target)
	for line := range baselineLines {
		if !targetLines[line] {
			return impactBreaking
		}
	}
	if len(baselineLines) == len(targetLines) {
		return impactMetadataOnly
	}
	return impactAdditive
}

func inventoryLines(inventory []byte) map[string]bool {
	lines := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(inventory)), "\n") {
		if line != "" {
			lines[line] = true
		}
	}
	return lines
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
