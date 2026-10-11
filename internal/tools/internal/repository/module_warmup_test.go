package repository

import (
	"archive/zip"
	"bytes"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A tidy-clean module can omit zip checksums for unused modules in its dependency
// graph. Downloading ALL graph modules warms offline verification, but must not
// rewrite the accepted tree. All proxy content below is a trusted local fixture.
func TestModuleWarmupUsesDisposableModfile(t *testing.T) {
	base := t.TempDir()
	proxy := filepath.Join(base, "proxy")
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	module := func(name, mod, source string) {
		t.Helper()
		versionDir := filepath.Join(proxy, filepath.FromSlash(name), "@v")
		write(filepath.Join(versionDir, "v1.0.0.mod"), mod)
		write(filepath.Join(versionDir, "v1.0.0.info"), `{"Version":"v1.0.0","Time":"2026-01-01T00:00:00Z"}`)
		var archive bytes.Buffer
		zw := zip.NewWriter(&archive)
		for _, file := range []struct{ name, body string }{{"go.mod", mod}, {"value.go", source}} {
			entry, err := zw.Create(name + "@v1.0.0/" + file.name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = entry.Write([]byte(file.body)); err != nil {
				t.Fatal(err)
			}
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(versionDir, "v1.0.0.zip"), archive.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	module("example.test/graphonly", "module example.test/graphonly\n\ngo 1.26.8\n", "package graphonly;const Value=42\n")
	module("example.test/direct", "module example.test/direct\n\ngo 1.26.8\n\nrequire example.test/graphonly v1.0.0\n", "package direct;const Value=42\n")
	proxyURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(proxy)}).String()
	run := func(root, cache, proxyValue string, args ...string) {
		t.Helper()
		cmd := exec.Command("go", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOMODCACHE="+cache, "GOPROXY="+proxyValue, "GOSUMDB=off", "GONOPROXY=none", "GONOSUMDB=*", "GOPRIVATE=", "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS="+os.Getenv("GOFLAGS")+" -modcacherw")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	read := func(path string) []byte {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	seed := filepath.Join(base, "seed")
	write(filepath.Join(seed, "go.mod"), "module fixture\n\ngo 1.26.8\n\nrequire example.test/direct v1.0.0\n")
	source := `package fixture
import("testing";"example.test/direct")
func TestCachedDependency(t *testing.T){if direct.Value!=42{t.Fatal("wrong fixture dependency")}}
`
	write(filepath.Join(seed, "fixture_test.go"), source)
	// Establish a genuinely tidy-clean accepted baseline, rather than synthesizing
	// invalid or incomplete sums. This setup itself uses only our file proxy.
	run(seed, filepath.Join(base, "seed-cache"), proxyURL, "mod", "tidy")
	originalMod, originalSum := read(filepath.Join(seed, "go.mod")), read(filepath.Join(seed, "go.sum"))
	if bytes.Contains(originalSum, []byte("example.test/graphonly v1.0.0 h1:")) {
		t.Fatal("fixture baseline already has graph-only zip checksum")
	}
	run(seed, filepath.Join(base, "seed-cache"), proxyURL, "mod", "tidy", "-diff")
	copyRoot := func(name string) string {
		root := filepath.Join(base, name)
		write(filepath.Join(root, "go.mod"), string(originalMod))
		write(filepath.Join(root, "go.sum"), string(originalSum))
		write(filepath.Join(root, "fixture_test.go"), source)
		return root
	}
	unsafe := copyRoot("unsafe")
	run(unsafe, filepath.Join(base, "unsafe-cache"), proxyURL, "mod", "download", "all")
	unsafeSum := read(filepath.Join(unsafe, "go.sum"))
	if bytes.Equal(unsafeSum, originalSum) || !bytes.Contains(unsafeSum, []byte("example.test/graphonly v1.0.0 h1:")) {
		t.Fatal("old warmup did not demonstrate accepted go.sum mutation")
	}
	safe := copyRoot("accepted")
	scratchMod := filepath.Join(base, "modfile", "go.mod")
	write(scratchMod, string(originalMod))
	write(filepath.Join(filepath.Dir(scratchMod), "go.sum"), string(originalSum))
	for _, name := range []string{"go.mod", "go.sum"} {
		if err := os.Chmod(filepath.Join(safe, name), 0444); err != nil {
			t.Fatal(err)
		}
	}
	// Snapshot checks remain necessary even when the test process can bypass mode
	// bits. The command is never asked to chmod or repair accepted source files.
	unchanged := func() {
		t.Helper()
		if !bytes.Equal(read(filepath.Join(safe, "go.mod")), originalMod) || !bytes.Equal(read(filepath.Join(safe, "go.sum")), originalSum) {
			t.Fatal("warmup changed accepted module files")
		}
	}
	cache := filepath.Join(base, "safe-cache") // independent of both previous caches
	run(safe, cache, proxyURL, "mod", "download", "-modfile", scratchMod, "all")
	unchanged()
	if !bytes.Contains(read(filepath.Join(filepath.Dir(scratchMod), "go.sum")), []byte("example.test/graphonly v1.0.0 h1:")) {
		t.Fatal("disposable sum file did not receive graph checksum")
	}
	if err := os.Rename(proxy, proxy+"-unavailable"); err != nil {
		t.Fatal(err)
	}
	// With the proxy unavailable, ALL modules must now come from the new cache.
	run(safe, cache, "off", "mod", "download", "-modfile", scratchMod, "all")
	unchanged()
	// Actual compilation/test uses the unchanged original files in readonly mode.
	run(safe, cache, "off", "test", "-mod=readonly", "-count=1", "./...")
	unchanged()
}
