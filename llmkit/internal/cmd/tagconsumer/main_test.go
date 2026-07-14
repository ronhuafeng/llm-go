package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunStableTagUsesFreshConsumerCacheAndWritesEvidence(t *testing.T) {
	t.Setenv("GOMODCACHE", filepath.Join(t.TempDir(), "prefilled-host-cache"))
	options := testConfig(t)
	valid := moduleInfo{
		Path: modulePath, Version: options.tag, Sum: "h1:module", GoModSum: "h1:gomod",
		Origin: moduleOrigin{VCS: "git", Hash: options.commit, Ref: "refs/tags/" + options.tag},
	}
	var probeCache string
	var consumerCache string
	var invocations int
	command := func(_ context.Context, _ time.Duration, directory string, environment []string, name string, arguments ...string) ([]byte, error) {
		invocations++
		values := environmentValues(environment)
		if strings.Contains(values["GOMODCACHE"], "prefilled-host-cache") {
			t.Fatalf("command used prefilled host cache: %s", values["GOMODCACHE"])
		}
		switch {
		case name == "go" && strings.Join(arguments, " ") == "mod download -json "+modulePath+"@"+options.tag:
			probeCache = values["GOMODCACHE"]
			if err := os.MkdirAll(probeCache, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(probeCache, "probe-only"), []byte("prefilled"), 0o600); err != nil {
				t.Fatal(err)
			}
			return json.Marshal(valid)
		case name == "bash":
			consumerCache = values["GOMODCACHE"]
			if consumerCache == probeCache {
				t.Fatalf("clean consumer reused probe cache %s", consumerCache)
			}
			if _, err := os.Stat(filepath.Join(consumerCache, "probe-only")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("probe cache marker reached clean consumer: %v", err)
			}
			consumerDir := arguments[len(arguments)-1]
			if err := os.MkdirAll(consumerDir, 0o755); err != nil {
				t.Fatal(err)
			}
			return nil, nil
		case name == "go" && strings.Join(arguments, " ") == "list -m -json "+modulePath:
			if directory != filepath.Join(options.workdir, "consumer") {
				t.Fatalf("go list directory = %s", directory)
			}
			resolved := valid
			resolved.Origin = moduleOrigin{}
			return json.Marshal(resolved)
		default:
			t.Fatalf("unexpected command: %s %s", name, strings.Join(arguments, " "))
			return nil, nil
		}
	}

	if err := runWithCommand(context.Background(), options, command); err != nil {
		t.Fatal(err)
	}
	if invocations != 3 || probeCache == "" || consumerCache == "" {
		t.Fatalf("invocations=%d probe=%q consumer=%q", invocations, probeCache, consumerCache)
	}
	data, err := os.ReadFile(options.evidencePath)
	if err != nil {
		t.Fatal(err)
	}
	var evidence tagEvidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Tag != options.tag || evidence.Module.Version != options.tag || evidence.Module.Sum == "" || evidence.Module.GoModSum == "" || evidence.TagCommit != options.commit {
		t.Fatalf("evidence = %#v", evidence)
	}
}

func TestRunUnavailableTagFailsClosedWithoutConsumerOrEvidence(t *testing.T) {
	options := testConfig(t)
	options.propagationTimeout = 5 * time.Millisecond
	options.retryInterval = time.Millisecond
	attempts := 0
	command := func(_ context.Context, _ time.Duration, _ string, _ []string, name string, _ ...string) ([]byte, error) {
		if name != "go" {
			t.Fatalf("unavailable tag invoked %s consumer", name)
		}
		attempts++
		return nil, errors.New("404 Not Found")
	}
	err := runWithCommand(context.Background(), options, command)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "404 Not Found") {
		t.Fatalf("runWithCommand error = %v", err)
	}
	if attempts < 2 {
		t.Fatalf("proxy attempts = %d, want bounded retry", attempts)
	}
	if _, err := os.Stat(options.evidencePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unavailable tag evidence exists: %v", err)
	}
}

func TestValidateResolvedModuleRequiresExactTagIntegrityAndCommit(t *testing.T) {
	valid := moduleInfo{
		Path:     modulePath,
		Version:  "v0.4.1",
		Sum:      "h1:module",
		GoModSum: "h1:gomod",
		Origin:   moduleOrigin{VCS: "git", Hash: strings.Repeat("a", 40), Ref: "refs/tags/v0.4.1"},
	}
	if err := validateResolvedModule(valid, "v0.4.1", strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		change func(*moduleInfo)
		commit string
		want   string
	}{
		{name: "wrong path", change: func(info *moduleInfo) { info.Path = "example.com/wrong" }, want: "module path"},
		{name: "wrong version", change: func(info *moduleInfo) { info.Version = "v0.4.0" }, want: "resolved version"},
		{name: "missing module sum", change: func(info *moduleInfo) { info.Sum = "" }, want: "module sum"},
		{name: "missing go.mod sum", change: func(info *moduleInfo) { info.GoModSum = "" }, want: "go.mod sum"},
		{name: "missing origin", change: func(info *moduleInfo) { info.Origin.Hash = "" }, want: "origin commit"},
		{name: "wrong commit", change: func(*moduleInfo) {}, commit: strings.Repeat("b", 40), want: "tag commit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := valid
			tt.change(&info)
			commit := tt.commit
			if commit == "" {
				commit = strings.Repeat("a", 40)
			}
			if err := validateResolvedModule(info, "v0.4.1", commit); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateResolvedModule error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestProxyEnvironmentIgnoresPrefilledHostCachesAndDisablesFallback(t *testing.T) {
	t.Setenv("GOPROXY", "https://untrusted.invalid,direct")
	t.Setenv("GOMODCACHE", "/shared/module-cache")
	t.Setenv("GOCACHE", "/shared/build-cache")
	t.Setenv("GOPATH", "/shared/gopath")
	t.Setenv("GOFLAGS", "-modfile=/tmp/untrusted.mod")

	workdir := filepath.Join(string(filepath.Separator), "isolated")
	values := environmentValues(proxyEnvironment(workdir))
	wants := map[string]string{
		"GO111MODULE": "on",
		"GOCACHE":     filepath.Join(workdir, "buildcache"),
		"GOENV":       "off",
		"GOFLAGS":     "",
		"GOMODCACHE":  filepath.Join(workdir, "modcache"),
		"GOPATH":      filepath.Join(workdir, "gopath"),
		"GOTOOLCHAIN": "local",
		"GOVCS":       "*:off",
		"GOPROXY":     publicProxy,
		"GOPRIVATE":   "",
		"GONOPROXY":   "",
		"GONOSUMDB":   "",
		"GOSUMDB":     "sum.golang.org",
		"GOWORK":      "off",
	}
	for key, want := range wants {
		if got := values[key]; got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestRetryUntilAvailableIsBoundedAndRetainsLastProxyFailure(t *testing.T) {
	attempts := 0
	err := retryUntilAvailable(context.Background(), time.Millisecond, func() error {
		attempts++
		if attempts < 3 {
			return errors.New("not propagated")
		}
		return nil
	})
	if err != nil || attempts != 3 {
		t.Fatalf("err=%v attempts=%d", err, attempts)
	}

	ctx, cancel := context.WithCancel(context.Background())
	err = retryUntilAvailable(ctx, time.Millisecond, func() error {
		cancel()
		return errors.New("tag unavailable")
	})
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "tag unavailable") {
		t.Fatalf("bounded unavailable-tag error = %v", err)
	}
}

func TestWriteEvidenceRecordsExactReviewableFacts(t *testing.T) {
	evidence := tagEvidence{
		FormatVersion: 1,
		Proxy:         publicProxy,
		Tag:           "v0.4.1",
		TagCommit:     strings.Repeat("a", 40),
		Module: evidenceModule{
			Path: modulePath, Version: "v0.4.1", Sum: "h1:module", GoModSum: "h1:gomod",
			Origin: moduleOrigin{VCS: "git", Hash: strings.Repeat("a", 40), Ref: "refs/tags/v0.4.1"},
		},
	}
	var output bytes.Buffer
	if err := writeEvidence(&output, evidence); err != nil {
		t.Fatal(err)
	}
	var contract map[string]any
	if err := json.Unmarshal(output.Bytes(), &contract); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"format_version", "proxy", "tag", "tag_commit", "module"} {
		if _, ok := contract[field]; !ok {
			t.Errorf("evidence missing top-level field %q: %#v", field, contract)
		}
	}
	module, ok := contract["module"].(map[string]any)
	if !ok {
		t.Fatalf("module evidence = %#v", contract["module"])
	}
	for _, field := range []string{"path", "version", "sum", "go_mod_sum", "origin"} {
		if _, ok := module[field]; !ok {
			t.Errorf("evidence module missing field %q: %#v", field, module)
		}
	}
	origin, ok := module["origin"].(map[string]any)
	if !ok || origin["hash"] != strings.Repeat("a", 40) {
		t.Fatalf("origin evidence = %#v", module["origin"])
	}
}

func testConfig(t *testing.T) config {
	t.Helper()
	root := t.TempDir()
	return config{
		tag: "v0.4.1", commit: strings.Repeat("a", 40), repository: root,
		workdir: filepath.Join(root, "work"), evidencePath: filepath.Join(root, "artifact", "tag-evidence.json"),
		propagationTimeout: 50 * time.Millisecond, retryInterval: time.Millisecond, commandTimeout: time.Second,
	}
}

func environmentValues(environment []string) map[string]string {
	values := make(map[string]string, len(environment))
	for _, item := range environment {
		key, value, _ := strings.Cut(item, "=")
		values[key] = value
	}
	return values
}
