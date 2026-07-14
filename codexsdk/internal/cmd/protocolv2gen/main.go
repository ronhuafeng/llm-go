package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/ronhuafeng/codexsdk-go/codexsdk/internal/protocolgen"
)

const defaultSchemaRoot = "codexsdk/internal/protocolschema/appserver/v2"

func main() {
	schemaRootFlag := flag.String("schema-root", "", "checked-in app-server v2 schema baseline root; defaults to the manifest directory or the checked-in baseline")
	manifestPathFlag := flag.String("manifest", "", "classified app-server v2 manifest path; defaults to <schema-root>/manifest.json")
	outDir := flag.String("out", "codexsdk/protocolv2", "protocolv2 output directory")
	stdout := flag.String("stdout", "", "write one generated artifact to stdout instead of files: method-registry or protocol-types")
	stableSource := flag.String("stable-source", "", "generated Go source from schemas without experimental visibility")
	completeSource := flag.String("complete-source", "", "generated Go source from schemas with experimental visibility")
	flag.Parse()

	if err := run(*schemaRootFlag, *manifestPathFlag, *outDir, *stdout, *stableSource, *completeSource, os.Stdout); err != nil {
		fatal(err)
	}
}

func run(schemaRootFlag, manifestPathFlag, outDir, stdout, stableSource, completeSource string, writer io.Writer) error {
	schemaRoot, manifestPath := resolveSchemaInputs(schemaRootFlag, manifestPathFlag)
	switch stdout {
	case "":
	case "method-registry":
		methodRegistry, err := generateMethodRegistry(manifestPath)
		if err != nil {
			return err
		}
		_, err = io.Copy(writer, bytes.NewReader(methodRegistry))
		return err
	case "protocol-types":
		protocolTypes, err := generateProtocolTypes(schemaRoot)
		if err != nil {
			return err
		}
		_, err = io.Copy(writer, bytes.NewReader(protocolTypes))
		return err
	case "classified-surface":
		if stableSource == "" || completeSource == "" {
			return fmt.Errorf("-stdout classified-surface requires -stable-source and -complete-source")
		}
		stable, err := readGeneratedSources(stableSource)
		if err != nil {
			return err
		}
		complete, err := readGeneratedSources(completeSource)
		if err != nil {
			return err
		}
		surface, err := protocolgen.ClassifyExportedPackage(stable, complete)
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(surface)
	default:
		return fmt.Errorf("-stdout must be method-registry, protocol-types, or classified-surface")
	}

	methodRegistry, err := generateMethodRegistry(manifestPath)
	if err != nil {
		return err
	}
	protocolTypes, err := generateProtocolTypes(schemaRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "method_registry.gen.go"), methodRegistry, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "protocol_types.gen.go"), protocolTypes, 0o644); err != nil {
		return err
	}
	return nil
}

func readGeneratedSources(path string) ([][]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	paths := []string{path}
	if info.IsDir() {
		matches, err := filepath.Glob(filepath.Join(path, "*.go"))
		if err != nil {
			return nil, err
		}
		paths = matches
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("generated source path %q has no Go files", path)
	}
	sources := make([][]byte, 0, len(paths))
	for _, sourcePath := range paths {
		source, err := os.ReadFile(sourcePath)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	return sources, nil
}

func generateMethodRegistry(manifestPath string) ([]byte, error) {
	manifest, err := protocolgen.LoadManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	return protocolgen.GenerateMethodRegistry(manifest)
}

func generateProtocolTypes(schemaRoot string) ([]byte, error) {
	typePlan, err := protocolgen.BuildProtocolTypePlan(schemaRoot)
	if err != nil {
		return nil, err
	}
	return protocolgen.GenerateProtocolTypes(typePlan)
}

func resolveSchemaInputs(schemaRootFlag, manifestPathFlag string) (schemaRoot string, manifestPath string) {
	switch {
	case schemaRootFlag != "":
		schemaRoot = schemaRootFlag
	case manifestPathFlag != "":
		schemaRoot = filepath.Dir(manifestPathFlag)
	default:
		schemaRoot = defaultSchemaRoot
	}
	if manifestPathFlag != "" {
		manifestPath = manifestPathFlag
	} else {
		manifestPath = filepath.Join(schemaRoot, "manifest.json")
	}
	return schemaRoot, manifestPath
}

func fatal(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "protocolv2gen: %v\n", err)
	os.Exit(1)
}
