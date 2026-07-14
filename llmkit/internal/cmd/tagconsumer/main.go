// Command tagconsumer proves that a tagged llmkit-go module can be resolved by
// a clean consumer exclusively through the public Go proxy.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	modulePath  = "github.com/ronhuafeng/llmkit-go"
	publicProxy = "https://proxy.golang.org"
)

var (
	stableTagRE = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	commitRE    = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
)

type moduleOrigin struct {
	VCS  string `json:"vcs"`
	URL  string `json:"url"`
	Hash string `json:"hash"`
	Ref  string `json:"ref"`
}

type moduleInfo struct {
	Path     string       `json:"Path"`
	Version  string       `json:"Version"`
	Sum      string       `json:"Sum"`
	GoModSum string       `json:"GoModSum"`
	Origin   moduleOrigin `json:"Origin"`
	Error    string       `json:"Error,omitempty"`
}

type evidenceModule struct {
	Path     string       `json:"path"`
	Version  string       `json:"version"`
	Sum      string       `json:"sum"`
	GoModSum string       `json:"go_mod_sum"`
	Origin   moduleOrigin `json:"origin"`
}

type tagEvidence struct {
	FormatVersion int            `json:"format_version"`
	Proxy         string         `json:"proxy"`
	Tag           string         `json:"tag"`
	TagCommit     string         `json:"tag_commit"`
	Module        evidenceModule `json:"module"`
}

type config struct {
	tag                string
	commit             string
	repository         string
	workdir            string
	evidencePath       string
	propagationTimeout time.Duration
	retryInterval      time.Duration
	commandTimeout     time.Duration
}

type commandFunction func(context.Context, time.Duration, string, []string, string, ...string) ([]byte, error)

func main() {
	var options config
	flag.StringVar(&options.tag, "tag", "", "exact stable module tag to resolve")
	flag.StringVar(&options.commit, "commit", "", "commit referenced by the immutable tag")
	flag.StringVar(&options.repository, "repository", ".", "tagged repository checkout")
	flag.StringVar(&options.workdir, "workdir", "", "isolated consumer and cache root")
	flag.StringVar(&options.evidencePath, "evidence", "tag-evidence.json", "evidence artifact path")
	flag.DurationVar(&options.propagationTimeout, "timeout", 10*time.Minute, "maximum public proxy propagation wait")
	flag.DurationVar(&options.retryInterval, "retry-interval", 15*time.Second, "public proxy retry interval")
	flag.DurationVar(&options.commandTimeout, "command-timeout", 10*time.Minute, "maximum time for one external command")
	flag.Parse()
	if err := run(context.Background(), options); err != nil {
		fmt.Fprintln(os.Stderr, "tag consumer gate:", err)
		os.Exit(1)
	}
}

func run(parent context.Context, options config) error {
	return runWithCommand(parent, options, commandOutputBounded)
}

func runWithCommand(parent context.Context, options config, command commandFunction) error {
	if !stableTagRE.MatchString(options.tag) {
		return fmt.Errorf("tag %q is not an exact stable Go module version", options.tag)
	}
	if !commitRE.MatchString(options.commit) {
		return fmt.Errorf("tag commit %q is not a full Git commit hash", options.commit)
	}
	if options.workdir == "" || options.repository == "" || options.evidencePath == "" {
		return errors.New("repository, workdir, and evidence paths are required")
	}
	if options.propagationTimeout <= 0 || options.retryInterval <= 0 || options.commandTimeout <= 0 {
		return errors.New("timeout and retry bounds must be positive")
	}
	for _, directory := range []string{options.workdir, filepath.Dir(options.evidencePath)} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
	}

	// The availability probe and the actual consumer intentionally use separate
	// empty caches. A successful probe must not prefill the evidence-producing
	// consumer's module state.
	probeEnvironment := proxyEnvironment(filepath.Join(options.workdir, "probe-state"))
	propagationContext, cancelPropagation := context.WithTimeout(parent, options.propagationTimeout)
	var resolved moduleInfo
	err := retryUntilAvailable(propagationContext, options.retryInterval, func() error {
		output, commandErr := command(
			propagationContext,
			options.commandTimeout,
			options.repository,
			probeEnvironment,
			"go", "mod", "download", "-json", modulePath+"@"+options.tag,
		)
		if commandErr != nil {
			return commandErr
		}
		var candidate moduleInfo
		if err := json.Unmarshal(output, &candidate); err != nil {
			return fmt.Errorf("decode proxy module metadata: %w", err)
		}
		if candidate.Error != "" {
			return errors.New(candidate.Error)
		}
		resolved = candidate
		return nil
	})
	cancelPropagation()
	if err != nil {
		return fmt.Errorf("resolve %s@%s exclusively through %s: %w", modulePath, options.tag, publicProxy, err)
	}
	if err := validateResolvedModule(resolved, options.tag, options.commit); err != nil {
		return fmt.Errorf("validate immutable proxy resolution: %w", err)
	}

	consumerDir := filepath.Join(options.workdir, "consumer")
	consumerEnvironment := proxyEnvironment(filepath.Join(options.workdir, "consumer-state"))
	if _, err := command(
		parent,
		options.commandTimeout,
		options.repository,
		consumerEnvironment,
		"bash", filepath.Join(options.repository, "scripts", "ci", "verify-clean-consumer.sh"), "version", options.tag, consumerDir,
	); err != nil {
		return err
	}
	listed, err := command(parent, options.commandTimeout, consumerDir, consumerEnvironment, "go", "list", "-m", "-json", modulePath)
	if err != nil {
		return err
	}
	var consumerResolved moduleInfo
	if err := json.Unmarshal(listed, &consumerResolved); err != nil {
		return fmt.Errorf("decode clean consumer module metadata: %w", err)
	}
	// go list may omit Origin for an already downloaded module. The download
	// metadata remains the source of origin evidence, while the consumer must
	// independently agree on the exact version and sums.
	consumerResolved.Origin = resolved.Origin
	if err := validateResolvedModule(consumerResolved, options.tag, options.commit); err != nil {
		return fmt.Errorf("validate clean consumer resolution: %w", err)
	}

	evidence := tagEvidence{
		FormatVersion: 1,
		Proxy:         publicProxy,
		Tag:           options.tag,
		TagCommit:     strings.ToLower(options.commit),
		Module: evidenceModule{
			Path:     consumerResolved.Path,
			Version:  consumerResolved.Version,
			Sum:      consumerResolved.Sum,
			GoModSum: consumerResolved.GoModSum,
			Origin:   consumerResolved.Origin,
		},
	}
	file, err := os.Create(options.evidencePath)
	if err != nil {
		return err
	}
	if err := writeEvidence(file, evidence); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return writeEvidence(os.Stdout, evidence)
}

func validateResolvedModule(info moduleInfo, tag string, commit string) error {
	if info.Path != modulePath {
		return fmt.Errorf("module path = %q, want %q", info.Path, modulePath)
	}
	if info.Version != tag {
		return fmt.Errorf("resolved version = %q, want exact tag %q", info.Version, tag)
	}
	if info.Sum == "" {
		return errors.New("resolved module is missing module sum")
	}
	if info.GoModSum == "" {
		return errors.New("resolved module is missing go.mod sum")
	}
	if info.Origin.Hash == "" {
		return errors.New("resolved module is missing origin commit")
	}
	if !strings.EqualFold(info.Origin.Hash, commit) {
		return fmt.Errorf("proxy origin %s does not match immutable tag commit %s", info.Origin.Hash, commit)
	}
	return nil
}

func retryUntilAvailable(ctx context.Context, interval time.Duration, attempt func() error) error {
	var lastErr error
	for {
		if err := attempt(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%w: last proxy error: %v", ctx.Err(), lastErr)
		case <-timer.C:
		}
	}
}

func writeEvidence(writer io.Writer, evidence tagEvidence) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(evidence)
}

func proxyEnvironment(workdir string) []string {
	blocked := map[string]bool{
		"GO111MODULE": true, "GOCACHE": true, "GOENV": true, "GOFLAGS": true, "GOMODCACHE": true,
		"GONOPROXY": true, "GONOSUMDB": true, "GOPATH": true, "GOPRIVATE": true,
		"GOPROXY": true, "GOSUMDB": true, "GOTOOLCHAIN": true, "GOVCS": true, "GOWORK": true,
	}
	environment := make([]string, 0, len(os.Environ())+14)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if !blocked[key] {
			environment = append(environment, item)
		}
	}
	return append(environment,
		"GO111MODULE=on",
		"GOCACHE="+filepath.Join(workdir, "buildcache"),
		"GOENV=off",
		"GOFLAGS=",
		"GOMODCACHE="+filepath.Join(workdir, "modcache"),
		"GONOPROXY=",
		"GONOSUMDB=",
		"GOPATH="+filepath.Join(workdir, "gopath"),
		"GOPRIVATE=",
		"GOPROXY="+publicProxy,
		"GOSUMDB=sum.golang.org",
		"GOTOOLCHAIN=local",
		"GOVCS=*:off",
		"GOWORK=off",
	)
}

func commandOutputBounded(parent context.Context, timeout time.Duration, directory string, environment []string, name string, arguments ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = directory
	command.Env = environment
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(arguments, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}
