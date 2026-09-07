package repository

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

func documentedInstallVersion(readme []byte, modulePath string) (string, error) {
	if modulePath == "" {
		return "", fmt.Errorf("documented install version is missing a module path")
	}
	prefix := modulePath + "@"
	var versions []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(readme), "\n") {
		idx := strings.Index(line, prefix)
		if idx < 0 {
			continue
		}
		version := strings.TrimSpace(line[idx+len(prefix):])
		if cut := strings.IndexAny(version, " \t`\"'"); cut >= 0 {
			version = version[:cut]
		}
		if version == "" || !strings.HasPrefix(version, "v") {
			continue
		}
		if !seen[version] {
			seen[version] = true
			versions = append(versions, version)
		}
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("README.md does not declare an install version for %s", modulePath)
	}
	if len(versions) > 1 {
		return "", fmt.Errorf("README.md declares multiple install versions for %s: %s", modulePath, strings.Join(versions, ", "))
	}
	return versions[0], nil
}

func exampleTestFiles(moduleRoot string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(moduleRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "internal" || strings.HasPrefix(entry.Name(), ".") {
				if path != moduleRoot {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if entry.Name() == "example_test.go" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func verifyDocumentedExamples(root string, candidate module) error {
	if !candidate.Published || candidate.path == "" {
		return nil
	}
	moduleRoot := filepath.Join(root, filepath.FromSlash(candidate.Dir))
	examples, err := exampleTestFiles(moduleRoot)
	if err != nil {
		return err
	}
	if len(examples) == 0 {
		return nil
	}
	readme, err := os.ReadFile(filepath.Join(moduleRoot, "README.md"))
	if err != nil {
		return fmt.Errorf("read %s README: %w", candidate.ID, err)
	}
	version, err := documentedInstallVersion(readme, candidate.path)
	if err != nil {
		return fmt.Errorf("module %s: %w", candidate.ID, err)
	}
	pending, err := unpublishedArchivedInstall(root, candidate, version)
	if err != nil {
		return fmt.Errorf("module %s: %w", candidate.ID, err)
	}
	if pending {
		return nil
	}
	return compileExamplesAgainstPublished(moduleRoot, examples, candidate.path, version)
}

// unpublishedArchivedInstall reports whether checkout should skip compiling
// documented examples because the README version is archived but not yet a
// local module tag. Do not probe proxy.golang.org here: a pre-tag lookup
// plants a negative cache that later blocks verify-tag.
func unpublishedArchivedInstall(root string, candidate module, version string) (bool, error) {
	moduleRoot := filepath.Join(root, filepath.FromSlash(candidate.Dir))
	if !archivedReleaseFragments(moduleRoot, version) {
		return false, nil
	}
	exists, err := localModuleTagExists(root, candidate, version)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	return true, nil
}

func archivedReleaseFragments(moduleRoot, version string) bool {
	matches, err := filepath.Glob(filepath.Join(moduleRoot, ".changes", "releases", version, "*.json"))
	return err == nil && len(matches) > 0
}

func localModuleTagExists(root string, candidate module, version string) (bool, error) {
	if candidate.Dir == "" || version == "" {
		return false, fmt.Errorf("local module tag check is missing a module directory or version")
	}
	tag := candidate.Dir + "/" + version
	output, err := gitOutput(root, "tag", "--list", tag)
	if err != nil {
		return false, err
	}
	for _, found := range strings.Fields(output) {
		if found == tag {
			return true, nil
		}
	}
	return false, nil
}

func publishedConsumerEnvironment() map[string]string {
	return map[string]string{
		"GOWORK":      "off",
		"GOTOOLCHAIN": "local",
		"GOPROXY":     "https://proxy.golang.org,direct",
		"GOSUMDB":     "sum.golang.org",
		"GOVCS":       "*:off",
	}
}

func compileExamplesAgainstPublished(moduleRoot string, examples []string, modulePath, version string) error {
	temporary, err := os.MkdirTemp("", "llm-go-documented-examples-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	runner := commandRunner{directory: temporary, environment: publishedConsumerEnvironment()}
	if err := runner.run("go", "mod", "init", "example.test/documented-examples"); err != nil {
		return err
	}
	if err := runner.run("go", "get", modulePath+"@"+version); err != nil {
		return fmt.Errorf("resolve documented tuple %s@%s: %w", modulePath, version, err)
	}
	for _, source := range examples {
		rel, err := filepath.Rel(moduleRoot, source)
		if err != nil {
			return err
		}
		dest := filepath.Join(temporary, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := copyFile(source, dest); err != nil {
			return err
		}
	}
	if err := runner.run("go", "mod", "tidy"); err != nil {
		return fmt.Errorf("documented examples for %s@%s do not resolve without replace: %w", modulePath, version, err)
	}
	modData, err := os.ReadFile(filepath.Join(temporary, "go.mod"))
	if err != nil {
		return err
	}
	parsed, err := modfile.Parse("go.mod", modData, nil)
	if err != nil {
		return fmt.Errorf("parse documented-example go.mod: %w", err)
	}
	if len(parsed.Replace) > 0 {
		return fmt.Errorf("documented examples for %s@%s introduced a replace directive", modulePath, version)
	}
	if err := runner.run("go", "test", "./..."); err != nil {
		return fmt.Errorf("documented examples for %s@%s do not compile against the declared published tuple: %w", modulePath, version, err)
	}
	return nil
}

func copyFile(source, dest string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func compilePublishedZipExamples(zipPath, modulePath, version string) error {
	temporary, err := os.MkdirTemp("", "llm-go-published-examples-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	prefix := modulePath + "@" + version + "/"
	examples, err := extractZipExamples(zipPath, prefix, temporary)
	if err != nil {
		return err
	}
	if len(examples) == 0 {
		return nil
	}
	return compileExamplesAgainstPublished(temporary, examples, modulePath, version)
}

func extractZipExamples(zipPath, prefix, destRoot string) ([]string, error) {
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("open module zip: %w", err)
	}
	defer archive.Close()
	var examples []string
	for _, file := range archive.File {
		name := strings.TrimPrefix(file.Name, prefix)
		if name == file.Name || filepath.Base(name) != "example_test.go" {
			continue
		}
		if file.FileInfo().IsDir() {
			continue
		}
		dest := filepath.Join(destRoot, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, err
		}
		reader, err := file.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(reader)
		closeErr := reader.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return nil, err
		}
		examples = append(examples, dest)
	}
	return examples, nil
}
