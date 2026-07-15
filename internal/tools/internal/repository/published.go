package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/sumdb/dirhash"
)

const publishedEvidenceFormatVersion = 1

type PublishedEvidence struct {
	FormatVersion  int                  `json:"format_version"`
	Subject        PublishedSubject     `json:"subject"`
	Environment    PublishedEnvironment `json:"environment"`
	Resolved       PublishedResolution  `json:"resolved"`
	Checks         []PublishedCheck     `json:"checks"`
	EvidenceDigest string               `json:"evidence_digest"`
}

type PublishedSubject struct {
	Kind                string `json:"kind"`
	Module              string `json:"module"`
	Version             string `json:"version"`
	Tag                 string `json:"tag"`
	TagCommit           string `json:"tag_commit"`
	PlanDigest          string `json:"plan_digest"`
	AuthorizationDigest string `json:"authorization_digest"`
}

type PublishedEnvironment struct {
	GOPROXY        string `json:"goproxy"`
	GOSUMDB        string `json:"gosumdb"`
	GOWORK         string `json:"gowork"`
	GOVCS          string `json:"govcs"`
	FreshCaches    bool   `json:"fresh_caches"`
	SeparateCaches bool   `json:"separate_probe_validation_consumer_caches"`
}

type PublishedResolution struct {
	Path        string        `json:"path"`
	Version     string        `json:"version"`
	Sum         string        `json:"sum"`
	GoModSum    string        `json:"go_mod_sum"`
	ZipSHA256   string        `json:"zip_sha256"`
	GoModSHA256 string        `json:"go_mod_sha256"`
	Origin      *ModuleOrigin `json:"origin"`
}

type ModuleOrigin struct {
	VCS  string `json:"vcs"`
	URL  string `json:"url"`
	Hash string `json:"hash"`
	Ref  string `json:"ref"`
}

type PublishedCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type PublishOptions struct {
	Proxy          string
	SumDB          string
	Timeout        time.Duration
	RetryInterval  time.Duration
	CommandTimeout time.Duration
}

type moduleDownload struct {
	Path        string        `json:"Path"`
	Version     string        `json:"Version"`
	Info        string        `json:"Info"`
	GoMod       string        `json:"GoMod"`
	Zip         string        `json:"Zip"`
	Dir         string        `json:"Dir"`
	Sum         string        `json:"Sum"`
	GoModSum    string        `json:"GoModSum"`
	Origin      *ModuleOrigin `json:"Origin"`
	Error       string        `json:"Error"`
	zipSHA256   string
	goModSHA256 string
	contentSum  string
}

type publishedCommand func(context.Context, string, map[string]string, ...string) ([]byte, error)

func VerifyPublishedTag(ctx context.Context, root string, plan ReleasePlan, authorization ReleaseAuthorization, evidenceDirectory, expectedDigest string, options PublishOptions) (PublishedEvidence, error) {
	evidence := PublishedEvidence{
		FormatVersion: publishedEvidenceFormatVersion,
		Subject: PublishedSubject{
			Kind: "published_module", Module: plan.Subject.ModulePath, Version: plan.Subject.TargetVersion,
			Tag: plan.Subject.Tag, TagCommit: plan.Subject.Commit, PlanDigest: plan.PlanDigest, AuthorizationDigest: authorization.AuthorizationDigest,
		},
		Environment: PublishedEnvironment{
			GOPROXY: options.Proxy, GOSUMDB: options.SumDB, GOWORK: "off", GOVCS: "*:off",
			FreshCaches: true, SeparateCaches: true,
		},
	}
	finish := func(err error) (PublishedEvidence, error) {
		if digestErr := finalizePublishedEvidence(&evidence); digestErr != nil && err == nil {
			err = digestErr
		}
		return evidence, err
	}
	check := func(name string, run func() error) error {
		entry := PublishedCheck{Name: name, Status: "passed"}
		if err := run(); err != nil {
			entry.Status = "failed"
			entry.Error = err.Error()
			evidence.Checks = append(evidence.Checks, entry)
			return fmt.Errorf("%s: %w", name, err)
		}
		evidence.Checks = append(evidence.Checks, entry)
		return nil
	}
	if err := check("approved release authorization", func() error {
		if err := plan.Validate(); err != nil {
			return err
		}
		if err := ValidateReleaseAuthorizationFiles(plan, authorization, evidenceDirectory); err != nil {
			return err
		}
		if expectedDigest == "" || authorization.AuthorizationDigest != expectedDigest {
			return fmt.Errorf("authorization digest %q does not match approved digest %q", authorization.AuthorizationDigest, expectedDigest)
		}
		return nil
	}); err != nil {
		return finish(err)
	}
	if err := check("immutable tag commit", func() error {
		commit, err := resolveCommit(root, plan.Subject.Tag)
		if err != nil {
			return err
		}
		if commit != plan.Subject.Commit {
			return fmt.Errorf("tag %s resolves to %s, plan requires %s", plan.Subject.Tag, commit, plan.Subject.Commit)
		}
		return nil
	}); err != nil {
		return finish(err)
	}
	if err := check("exclusive public resolution policy", func() error {
		if options.Proxy != "https://proxy.golang.org" || options.SumDB != "sum.golang.org" || options.Timeout <= 0 || options.RetryInterval <= 0 || options.CommandTimeout <= 0 {
			return fmt.Errorf("public proxy verification options are invalid or non-public")
		}
		return nil
	}); err != nil {
		return finish(err)
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	moduleVersion := plan.Subject.ModulePath + "@" + plan.Subject.TargetVersion
	if err := check("public proxy propagation", func() error {
		return waitForProxy(deadlineCtx, moduleVersion, options, runPublishedCommand)
	}); err != nil {
		return finish(err)
	}

	if err := check("proxy artifact resolution", func() error {
		resolved, err := downloadFromFreshCache(deadlineCtx, moduleVersion, options, runPublishedCommand)
		if err != nil {
			return err
		}
		resolution, err := validateModuleDownload(resolved, plan)
		if err != nil {
			return err
		}
		evidence.Resolved = resolution
		return nil
	}); err != nil {
		return finish(err)
	}
	if err := check("isolated typed consumer", func() error {
		return runPublishedConsumer(deadlineCtx, plan, options, runPublishedCommand)
	}); err != nil {
		return finish(err)
	}
	return finish(nil)
}

func waitForProxy(ctx context.Context, moduleVersion string, options PublishOptions, command publishedCommand) error {
	var lastErr error
	for {
		probeRoot, err := os.MkdirTemp("", "llm-go-proxy-probe-")
		if err != nil {
			return err
		}
		environment := publishedEnvironment(filepath.Join(probeRoot, "gopath"), filepath.Join(probeRoot, "gomodcache"), filepath.Join(probeRoot, "gocache"), options)
		commandCtx, cancel := context.WithTimeout(ctx, options.CommandTimeout)
		output, commandErr := command(commandCtx, probeRoot, environment, "go", "list", "-m", "-json", moduleVersion)
		cancel()
		removeErr := os.RemoveAll(probeRoot)
		if commandErr == nil {
			var resolved struct {
				Path    string `json:"Path"`
				Version string `json:"Version"`
			}
			if err := json.Unmarshal(output, &resolved); err != nil {
				return fmt.Errorf("decode proxy probe: %w", err)
			}
			wantPath, wantVersion, _ := strings.Cut(moduleVersion, "@")
			if resolved.Path != wantPath || resolved.Version != wantVersion {
				return fmt.Errorf("proxy probe resolved %s@%s, want %s", resolved.Path, resolved.Version, moduleVersion)
			}
			if removeErr != nil {
				return removeErr
			}
			return nil
		}
		lastErr = commandErr
		if removeErr != nil {
			return removeErr
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("module did not reach public proxy before deadline: %w (last error: %v)", ctx.Err(), lastErr)
		case <-time.After(options.RetryInterval):
		}
	}
}

func downloadFromFreshCache(ctx context.Context, moduleVersion string, options PublishOptions, command publishedCommand) (download moduleDownload, err error) {
	root, err := os.MkdirTemp("", "llm-go-proxy-validate-")
	if err != nil {
		return moduleDownload{}, err
	}
	defer func() {
		if cleanupErr := os.RemoveAll(root); cleanupErr != nil && err == nil {
			err = fmt.Errorf("remove validation cache: %w", cleanupErr)
		}
	}()
	environment := publishedEnvironment(filepath.Join(root, "gopath"), filepath.Join(root, "gomodcache"), filepath.Join(root, "gocache"), options)
	commandCtx, cancel := context.WithTimeout(ctx, options.CommandTimeout)
	defer cancel()
	output, err := command(commandCtx, root, environment, "go", "mod", "download", "-json", moduleVersion)
	if err != nil {
		return moduleDownload{}, err
	}
	if err := json.Unmarshal(output, &download); err != nil {
		return moduleDownload{}, fmt.Errorf("decode module download: %w", err)
	}
	if download.Error != "" {
		return moduleDownload{}, fmt.Errorf("module download: %s", download.Error)
	}
	download.zipSHA256, err = fileSHA256(download.Zip)
	if err != nil {
		return moduleDownload{}, fmt.Errorf("hash module zip: %w", err)
	}
	download.goModSHA256, err = fileSHA256(download.GoMod)
	if err != nil {
		return moduleDownload{}, fmt.Errorf("hash module go.mod: %w", err)
	}
	download.contentSum, err = dirhash.HashZip(download.Zip, dirhash.Hash1)
	if err != nil {
		return moduleDownload{}, fmt.Errorf("hash canonical module content: %w", err)
	}
	return download, nil
}

func validateModuleDownload(download moduleDownload, plan ReleasePlan) (PublishedResolution, error) {
	if download.Path != plan.Subject.ModulePath || download.Version != plan.Subject.TargetVersion {
		return PublishedResolution{}, fmt.Errorf("download resolved %s@%s, plan requires %s@%s", download.Path, download.Version, plan.Subject.ModulePath, plan.Subject.TargetVersion)
	}
	if download.Sum == "" || download.GoModSum == "" {
		return PublishedResolution{}, fmt.Errorf("download is missing module or go.mod checksum")
	}
	if download.Zip == "" || download.GoMod == "" {
		return PublishedResolution{}, fmt.Errorf("download is missing zip or go.mod artifact")
	}
	if download.Origin == nil || download.Origin.VCS != "git" || canonicalRepositoryURL(download.Origin.URL) != plan.Subject.RepositoryURL || download.Origin.Hash != plan.Subject.Commit || download.Origin.Ref != "refs/tags/"+plan.Subject.Tag {
		return PublishedResolution{}, fmt.Errorf("proxy origin does not bind tag %s to commit %s", plan.Subject.Tag, plan.Subject.Commit)
	}
	if download.zipSHA256 == "" || download.goModSHA256 == "" || download.contentSum == "" {
		return PublishedResolution{}, fmt.Errorf("download artifact digests are missing")
	}
	if download.contentSum != download.Sum {
		return PublishedResolution{}, fmt.Errorf("proxy module sum %s does not match canonical zip content %s", download.Sum, download.contentSum)
	}
	if download.contentSum != plan.ArchiveSum {
		return PublishedResolution{}, fmt.Errorf("proxy canonical module content %s does not match planned archive %s", download.contentSum, plan.ArchiveSum)
	}
	wantGoMod := ""
	for _, input := range plan.Inputs {
		if input.Path == filepath.ToSlash(filepath.Join(plan.Subject.ModuleDir, "go.mod")) {
			wantGoMod = input.SHA256
			break
		}
	}
	if wantGoMod == "" || download.goModSHA256 != wantGoMod {
		return PublishedResolution{}, fmt.Errorf("proxy go.mod SHA-256 %s does not match planned go.mod %s", download.goModSHA256, wantGoMod)
	}
	return PublishedResolution{
		Path: download.Path, Version: download.Version, Sum: download.Sum, GoModSum: download.GoModSum,
		ZipSHA256: download.zipSHA256, GoModSHA256: download.goModSHA256, Origin: download.Origin,
	}, nil
}

func runPublishedConsumer(ctx context.Context, plan ReleasePlan, options PublishOptions, command publishedCommand) (err error) {
	root, err := os.MkdirTemp("", "llm-go-published-consumer-")
	if err != nil {
		return err
	}
	defer func() {
		if cleanupErr := os.RemoveAll(root); cleanupErr != nil && err == nil {
			err = fmt.Errorf("remove consumer cache: %w", cleanupErr)
		}
	}()
	goMod := "module example.test/llm-go-published-consumer\n\ngo 1.23.0\n\nrequire " + plan.Subject.ModulePath + " " + plan.Subject.TargetVersion + "\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(goMod), 0o644); err != nil {
		return err
	}
	source, err := publishedConsumerSource(plan.Subject.ModuleID, plan.Subject.ModulePath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0o644); err != nil {
		return err
	}
	environment := publishedEnvironment(filepath.Join(root, "gopath"), filepath.Join(root, "gomodcache"), filepath.Join(root, "gocache"), options)
	tidyEnvironment := cloneEnvironment(environment)
	tidyEnvironment["GOFLAGS"] = "-modcacherw"
	commandCtx, cancel := context.WithTimeout(ctx, options.CommandTimeout)
	_, err = command(commandCtx, root, tidyEnvironment, "go", "mod", "tidy")
	cancel()
	if err != nil {
		return err
	}
	commandCtx, cancel = context.WithTimeout(ctx, options.CommandTimeout)
	defer cancel()
	_, err = command(commandCtx, root, environment, "go", "run", ".")
	return err
}

func publishedConsumerSource(moduleID, modulePath string) (string, error) {
	switch moduleID {
	case "llmkit":
		return `package main

import (
	"fmt"

	"` + modulePath + `/llmschema"
)

type answer struct {
	Text string ` + "`json:\"text\"`" + `
}

func main() {
	value, err := llmschema.Decode[answer]([]byte(` + "`{\"text\":\"verified\"}`" + `))
	if err != nil || value.Text != "verified" {
		panic(fmt.Sprintf("published llmkit decode failed: value=%+v err=%v", value, err))
	}
}
`, nil
	case "codexsdk":
		return `package main

import (
	"context"
	"errors"
	"fmt"

	"` + modulePath + `"
	"` + modulePath + `/protocolv2"
)

func main() {
	ctx := context.Background()
	var client codexsdk.Client

	if _, err := client.Models().List(ctx, protocolv2.ModelListParams{}); !errors.Is(err, codexsdk.ErrClientClosed) {
		panic(fmt.Sprintf("published codexsdk generated facade did not fail closed: %v", err))
	}
	if _, err := client.ThreadRunner().Start(ctx, codexsdk.StartThreadRunRequest{}); !errors.Is(err, codexsdk.ErrClientClosed) {
		panic(fmt.Sprintf("published codexsdk exact lifecycle did not fail closed: %v", err))
	}
}
`, nil
	default:
		return "", fmt.Errorf("module %s has no published consumer seam", moduleID)
	}
}

func publishedEnvironment(gopath, moduleCache, buildCache string, options PublishOptions) map[string]string {
	return map[string]string{
		"GOPATH": gopath, "GOMODCACHE": moduleCache, "GOCACHE": buildCache,
		"GOPROXY": options.Proxy, "GOSUMDB": options.SumDB, "GOWORK": "off", "GOVCS": "*:off",
		"GOPRIVATE": "", "GONOPROXY": "", "GONOSUMDB": "", "GOENV": "off", "GOTOOLCHAIN": "local", "GOFLAGS": "-mod=readonly -modcacherw",
	}
}

func cloneEnvironment(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for name, value := range source {
		result[name] = value
	}
	return result
}

func runPublishedCommand(ctx context.Context, directory string, environment map[string]string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, args[0], args[1:]...)
	command.Dir = directory
	command.Env = overriddenEnvironment(environment)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func finalizePublishedEvidence(evidence *PublishedEvidence) error {
	evidence.EvidenceDigest = ""
	data, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	evidence.EvidenceDigest = "sha256:" + hex.EncodeToString(digest[:])
	return nil
}

func WritePublishedEvidence(path string, evidence PublishedEvidence) error {
	if evidence.FormatVersion != publishedEvidenceFormatVersion || evidence.Subject.Kind != "published_module" || evidence.Subject.PlanDigest == "" || evidence.Subject.AuthorizationDigest == "" || evidence.EvidenceDigest == "" {
		return fmt.Errorf("published evidence is incomplete")
	}
	want := evidence.EvidenceDigest
	copy := evidence
	if err := finalizePublishedEvidence(&copy); err != nil {
		return err
	}
	if copy.EvidenceDigest != want {
		return fmt.Errorf("published evidence digest mismatch")
	}
	return writeJSON(path, evidence)
}
