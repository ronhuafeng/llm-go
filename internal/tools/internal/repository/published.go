package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/sumdb/dirhash"
)

const publishedEvidenceFormatVersion = 2

type PublishedEvidence struct {
	FormatVersion  int                  `json:"format_version"`
	Subject        PublishedSubject     `json:"subject"`
	Environment    PublishedEnvironment `json:"environment"`
	Resolved       PublishedResolution  `json:"resolved"`
	Consumer       *PublishedConsumer   `json:"consumer,omitempty"`
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
	Path        string                `json:"path"`
	Version     string                `json:"version"`
	Sum         string                `json:"sum"`
	GoModSum    string                `json:"go_mod_sum"`
	ZipSHA256   string                `json:"zip_sha256"`
	GoModSHA256 string                `json:"go_mod_sha256"`
	Origin      *ModuleOrigin         `json:"origin"`
	Tuple       []PublishedTupleEntry `json:"compatibility_tuple,omitempty"`
}

type PublishedTupleEntry struct {
	Module          string `json:"module"`
	Role            string `json:"role"`
	DeclaredVersion string `json:"declared_version"`
	ResolvedVersion string `json:"resolved_version"`
	Direct          bool   `json:"direct"`
	Sum             string `json:"sum"`
	GoModSum        string `json:"go_mod_sum"`
}

type PublishedConsumer struct {
	Kind                 string `json:"kind,omitempty"`
	TypedValue           string `json:"typed_value,omitempty"`
	ProviderName         string `json:"provider_name,omitempty"`
	EffectiveModel       string `json:"effective_model,omitempty"`
	NeutralInputTokens   int64  `json:"neutral_input_tokens,omitempty"`
	ExactThreadID        string `json:"exact_thread_id,omitempty"`
	ExactTurnID          string `json:"exact_turn_id,omitempty"`
	ExactResultPreserved bool   `json:"exact_result_preserved,omitempty"`
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
	goModData   []byte
}

type listedModule struct {
	Path     string        `json:"Path"`
	Version  string        `json:"Version"`
	Main     bool          `json:"Main"`
	Sum      string        `json:"Sum"`
	GoModSum string        `json:"GoModSum"`
	Replace  *listedModule `json:"Replace"`
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
		if err := validateReleaseTracerScope(plan.Subject.ModuleID, plan.Subject.TargetVersion); err != nil {
			return err
		}
		if err := validateFirstTracerDependencies(plan.Subject.ModuleID, plan.Subject.TargetVersion, plan.Dependencies); err != nil {
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

	var downloaded moduleDownload
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
		downloaded = resolved
		return nil
	}); err != nil {
		return finish(err)
	}
	var declaredTuple map[string]string
	if plan.Subject.ModuleID == "codex-adapter" {
		if err := check("proxy artifact exact declared tuple without overrides", func() error {
			var err error
			declaredTuple, err = validateAdapterArtifactMetadata(downloaded.goModData, plan)
			return err
		}); err != nil {
			return finish(err)
		}
	}
	consumerCheck := "isolated typed consumer"
	if plan.Subject.ModuleID == "codex-adapter" {
		consumerCheck = "isolated typed consumer exact resolved tuple and evidence"
	}
	if err := check(consumerCheck, func() error {
		consumer, tuple, err := runPublishedConsumer(deadlineCtx, plan, declaredTuple, options, runPublishedCommand)
		if err != nil {
			return err
		}
		if consumer.Kind != "" {
			evidence.Consumer = &consumer
		}
		evidence.Resolved.Tuple = tuple
		return nil
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
	download.goModData, err = os.ReadFile(download.GoMod)
	if err != nil {
		return moduleDownload{}, fmt.Errorf("read module go.mod: %w", err)
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

func validateAdapterArtifactMetadata(data []byte, plan ReleasePlan) (map[string]string, error) {
	if plan.Subject.ModuleID != "codex-adapter" || plan.Subject.ModulePath != adapterModulePath || plan.Subject.TargetVersion != "v0.5.0" {
		return nil, fmt.Errorf("adapter artifact verification received an unauthorized release subject")
	}
	parsed, err := modfile.Parse("proxy-artifact/go.mod", data, nil)
	if err != nil {
		return nil, fmt.Errorf("parse proxy artifact go.mod: %w", err)
	}
	if parsed.Module == nil || parsed.Module.Mod.Path != plan.Subject.ModulePath {
		return nil, fmt.Errorf("proxy artifact module path does not match %s", plan.Subject.ModulePath)
	}
	if len(parsed.Replace) != 0 {
		return nil, fmt.Errorf("proxy artifact go.mod contains a replace directive")
	}
	if len(parsed.Exclude) != 0 {
		return nil, fmt.Errorf("proxy artifact go.mod contains an exclude directive")
	}
	planVersions := make(map[string]string, len(plan.Dependencies))
	for _, dependency := range plan.Dependencies {
		planVersions[dependency.Module] = dependency.Version
	}
	required := make(map[string]*modfile.Require, len(parsed.Require))
	for _, requirement := range parsed.Require {
		if !isStableVersion(requirement.Mod.Version) {
			return nil, fmt.Errorf("proxy artifact requires %s at non-stable version %q", requirement.Mod.Path, requirement.Mod.Version)
		}
		if _, duplicate := required[requirement.Mod.Path]; duplicate {
			return nil, fmt.Errorf("proxy artifact contains duplicate requirement %s", requirement.Mod.Path)
		}
		required[requirement.Mod.Path] = requirement
	}
	declared := make(map[string]string, 2)
	for _, modulePath := range []string{codexSDKModulePath, llmkitModulePath} {
		requirement, ok := required[modulePath]
		if !ok {
			return nil, fmt.Errorf("proxy artifact must declare %s", modulePath)
		}
		if requirement.Indirect {
			return nil, fmt.Errorf("proxy artifact must directly require %s", modulePath)
		}
		want := planVersions[modulePath]
		if want == "" || requirement.Mod.Version != want {
			return nil, fmt.Errorf("proxy artifact requires %s at %s, release plan declares %q", modulePath, requirement.Mod.Version, want)
		}
		declared[modulePath] = requirement.Mod.Version
	}
	for modulePath := range required {
		if strings.HasPrefix(modulePath, "github.com/ronhuafeng/llm-go/") && modulePath != codexSDKModulePath && modulePath != llmkitModulePath {
			return nil, fmt.Errorf("proxy artifact declares unexpected repository module %s", modulePath)
		}
	}
	return declared, nil
}

func runPublishedConsumer(ctx context.Context, plan ReleasePlan, declaredTuple map[string]string, options PublishOptions, command publishedCommand) (consumer PublishedConsumer, tuple []PublishedTupleEntry, err error) {
	root, err := os.MkdirTemp("", "llm-go-published-consumer-")
	if err != nil {
		return PublishedConsumer{}, nil, err
	}
	defer func() {
		if cleanupErr := os.RemoveAll(root); cleanupErr != nil && err == nil {
			err = fmt.Errorf("remove consumer cache: %w", cleanupErr)
		}
	}()
	goMod := "module example.test/llm-go-published-consumer\n\ngo 1.23.0\n\nrequire " + plan.Subject.ModulePath + " " + plan.Subject.TargetVersion + "\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(goMod), 0o644); err != nil {
		return PublishedConsumer{}, nil, err
	}
	source, err := publishedConsumerSource(plan.Subject.ModuleID, plan.Subject.ModulePath)
	if err != nil {
		return PublishedConsumer{}, nil, err
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0o644); err != nil {
		return PublishedConsumer{}, nil, err
	}
	environment := publishedEnvironment(filepath.Join(root, "gopath"), filepath.Join(root, "gomodcache"), filepath.Join(root, "gocache"), options)
	tidyEnvironment := cloneEnvironment(environment)
	tidyEnvironment["GOFLAGS"] = "-modcacherw"
	commandCtx, cancel := context.WithTimeout(ctx, options.CommandTimeout)
	_, err = command(commandCtx, root, tidyEnvironment, "go", "mod", "tidy")
	cancel()
	if err != nil {
		return PublishedConsumer{}, nil, err
	}
	if err := validateCleanConsumerGoMod(filepath.Join(root, "go.mod")); err != nil {
		return PublishedConsumer{}, nil, err
	}
	if plan.Subject.ModuleID == "codex-adapter" {
		commandCtx, cancel = context.WithTimeout(ctx, options.CommandTimeout)
		output, downloadErr := command(commandCtx, root, environment, "go", "mod", "download", "-json", "all")
		cancel()
		if downloadErr != nil {
			return PublishedConsumer{}, nil, downloadErr
		}
		downloaded, decodeErr := decodeDownloadedModules(output)
		if decodeErr != nil {
			return PublishedConsumer{}, nil, decodeErr
		}
		commandCtx, cancel = context.WithTimeout(ctx, options.CommandTimeout)
		output, listErr := command(commandCtx, root, environment, "go", "list", "-m", "-json", "all")
		cancel()
		if listErr != nil {
			return PublishedConsumer{}, nil, listErr
		}
		modules, decodeErr := decodeListedModules(output)
		if decodeErr != nil {
			return PublishedConsumer{}, nil, decodeErr
		}
		if err := validateMaterializedGraph(modules, downloaded); err != nil {
			return PublishedConsumer{}, nil, err
		}
		tuple, err = validateAdapterResolvedGraph(modules, plan.Subject.ModulePath, plan.Subject.TargetVersion, declaredTuple)
		if err != nil {
			return PublishedConsumer{}, nil, err
		}
	}
	commandCtx, cancel = context.WithTimeout(ctx, options.CommandTimeout)
	defer cancel()
	output, err := command(commandCtx, root, environment, "go", "run", ".")
	if err != nil {
		return PublishedConsumer{}, nil, err
	}
	if plan.Subject.ModuleID == "codex-adapter" {
		consumer, err = validateAdapterConsumerAttestation(output)
		if err != nil {
			return PublishedConsumer{}, nil, err
		}
	}
	return consumer, tuple, nil
}

func decodeDownloadedModules(data []byte) (map[string]moduleDownload, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	modules := map[string]moduleDownload{}
	for {
		var module moduleDownload
		if err := decoder.Decode(&module); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode full graph download: %w", err)
		}
		if module.Path == "" || !isStableVersion(module.Version) {
			return nil, fmt.Errorf("full graph download contains an invalid module %s@%s", module.Path, module.Version)
		}
		if module.Error != "" {
			return nil, fmt.Errorf("full graph download %s@%s failed: %s", module.Path, module.Version, module.Error)
		}
		if module.Sum == "" || module.GoModSum == "" {
			return nil, fmt.Errorf("full graph download %s@%s is missing an official checksum", module.Path, module.Version)
		}
		if _, duplicate := modules[module.Path]; duplicate {
			return nil, fmt.Errorf("full graph download contains duplicate module %s", module.Path)
		}
		modules[module.Path] = module
	}
	if len(modules) == 0 {
		return nil, fmt.Errorf("full graph download is empty")
	}
	return modules, nil
}

func validateMaterializedGraph(listed []listedModule, downloaded map[string]moduleDownload) error {
	versioned := 0
	for _, module := range listed {
		if module.Main {
			continue
		}
		versioned++
		download, ok := downloaded[module.Path]
		if !ok {
			return fmt.Errorf("resolved module %s was not materialized by the full graph download", module.Path)
		}
		if download.Version != module.Version || download.Sum != module.Sum || download.GoModSum != module.GoModSum {
			return fmt.Errorf("resolved module %s does not match its full graph download checksums", module.Path)
		}
	}
	if len(downloaded) != versioned {
		return fmt.Errorf("full graph download contains %d modules, resolved graph contains %d versioned modules", len(downloaded), versioned)
	}
	return nil
}

func validateCleanConsumerGoMod(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	parsed, err := modfile.Parse(path, data, nil)
	if err != nil {
		return err
	}
	if len(parsed.Replace) != 0 || len(parsed.Exclude) != 0 {
		return fmt.Errorf("clean consumer go.mod contains replace or exclude directives")
	}
	return nil
}

func decodeListedModules(data []byte) ([]listedModule, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	modules := []listedModule{}
	for {
		var module listedModule
		if err := decoder.Decode(&module); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode resolved module graph: %w", err)
		}
		if module.Path == "" {
			return nil, fmt.Errorf("resolved module graph contains an empty path")
		}
		modules = append(modules, module)
	}
	if len(modules) == 0 {
		return nil, fmt.Errorf("resolved module graph is empty")
	}
	return modules, nil
}

func validateAdapterResolvedGraph(modules []listedModule, subjectPath, subjectVersion string, declared map[string]string) ([]PublishedTupleEntry, error) {
	if subjectPath != adapterModulePath || subjectVersion != "v0.5.0" || len(declared) != 2 {
		return nil, fmt.Errorf("adapter resolved graph has an incomplete declared tuple")
	}
	resolved := make(map[string]listedModule, len(modules))
	for _, module := range modules {
		if _, duplicate := resolved[module.Path]; duplicate {
			return nil, fmt.Errorf("resolved module graph contains duplicate %s", module.Path)
		}
		resolved[module.Path] = module
		if module.Main {
			continue
		}
		if module.Replace != nil {
			return nil, fmt.Errorf("resolved module %s contains a replacement", module.Path)
		}
		if !isStableVersion(module.Version) {
			return nil, fmt.Errorf("resolved module %s has non-stable version %q", module.Path, module.Version)
		}
		if module.Sum == "" || module.GoModSum == "" {
			return nil, fmt.Errorf("resolved module %s is missing an official checksum", module.Path)
		}
		if strings.HasPrefix(module.Path, "github.com/ronhuafeng/llm-go/") && module.Path != subjectPath && module.Path != codexSDKModulePath && module.Path != llmkitModulePath {
			return nil, fmt.Errorf("resolved graph contains unexpected repository module %s", module.Path)
		}
	}
	expected := map[string]struct {
		version string
		role    string
		direct  bool
	}{
		subjectPath:        {version: subjectVersion, role: "adapter", direct: false},
		codexSDKModulePath: {version: declared[codexSDKModulePath], role: "sdk", direct: true},
		llmkitModulePath:   {version: declared[llmkitModulePath], role: "toolkit", direct: true},
	}
	tuple := make([]PublishedTupleEntry, 0, len(expected))
	for modulePath, expectation := range expected {
		module, ok := resolved[modulePath]
		if !ok {
			return nil, fmt.Errorf("resolved graph is missing %s", modulePath)
		}
		if module.Version != expectation.version {
			return nil, fmt.Errorf("%s resolved to %s, declared tuple requires %s", modulePath, module.Version, expectation.version)
		}
		tuple = append(tuple, PublishedTupleEntry{
			Module: modulePath, Role: expectation.role, DeclaredVersion: expectation.version,
			ResolvedVersion: module.Version, Direct: expectation.direct, Sum: module.Sum, GoModSum: module.GoModSum,
		})
	}
	sort.Slice(tuple, func(i, j int) bool { return tuple[i].Module < tuple[j].Module })
	return tuple, nil
}

func validateAdapterConsumerAttestation(data []byte) (PublishedConsumer, error) {
	var got PublishedConsumer
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&got); err != nil {
		return PublishedConsumer{}, fmt.Errorf("decode adapter consumer attestation: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return PublishedConsumer{}, fmt.Errorf("decode adapter consumer attestation: trailing data")
	}
	if got.Kind != "typed_three_layer_call" || got.TypedValue != "verified" || got.ProviderName != "codex" || got.EffectiveModel != "proxy-model" || got.NeutralInputTokens != 11 || got.ExactThreadID != "thread-proxy" || got.ExactTurnID != "turn-proxy" || !got.ExactResultPreserved {
		return PublishedConsumer{}, fmt.Errorf("adapter consumer attestation is incomplete: %+v", got)
	}
	return got, nil
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
	case "codex-adapter":
		return `package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"

	"` + codexSDKModulePath + `"
	"` + codexSDKModulePath + `/protocolv2"
	codexcaller "` + modulePath + `"
	"` + llmkitModulePath + `/llmadapter"
)

type runner struct {
	result codexsdk.StartedThreadRun
}

func (r runner) Start(context.Context, codexsdk.StartThreadRunRequest) (codexsdk.StartedThreadRun, error) {
	return r.result, nil
}

func (runner) StartStream(context.Context, codexsdk.StartThreadRunRequest) (*codexsdk.Stream[codexsdk.StartedThreadRun], error) {
	return nil, nil
}

type answer struct {
	Answer string ` + "`json:\"answer\"`" + `
}

func main() {
	want := codexsdk.StartedThreadRun{
		Start: protocolv2.ThreadStartResponse{
			ApprovalPolicy: protocolv2.NewAskForApprovalNever(),
			ApprovalsReviewer: protocolv2.ApprovalsReviewerUser,
			CWD: "/workspace",
			Model: "proxy-model",
			ModelProvider: "openai",
			Sandbox: protocolv2.NewSandboxPolicyReadOnly(protocolv2.SandboxPolicyReadOnly{}),
			Thread: protocolv2.Thread{
				CliVersion: "proxy-consumer", CWD: "/workspace", Ephemeral: true, ID: "thread-proxy",
				ModelProvider: "openai", Preview: "proxy verification", SessionID: "session-proxy",
				Source: protocolv2.NewSessionSourceAppServer(),
				Status: protocolv2.NewThreadStatusIdle(),
				Turns: []protocolv2.Turn{},
			},
		},
		Run: codexsdk.ThreadRunResult{
			Turn: protocolv2.Turn{ID: "turn-proxy", Items: []protocolv2.ThreadItem{}, Status: protocolv2.TurnStatusCompleted},
			Usage: &protocolv2.ThreadTokenUsage{Total: protocolv2.TokenUsageBreakdown{InputTokens: 11, CachedInputTokens: 3, OutputTokens: 7, ReasoningOutputTokens: 2}},
			Notifications: []protocolv2.ServerNotification{},
			FinalResponse: ` + "`{\"answer\":\"verified\"}`" + `,
			InputStats: codexsdk.InputStats{ItemsCount: 1, TextBytes: 13, InputItemsHash: "sha256:input"},
			Diagnostics: []codexsdk.DiagnosticRef{{Kind: "trace", ID: "diagnostic-1", Path: "trace.json", SizeBytes: 17, SHA256: "sha256:diagnostic"}},
		},
	}
	caller, err := codexcaller.New(codexcaller.ReadOnlyEphemeralOptions(runner{result: want}))
	if err != nil {
		panic(err)
	}
	result, err := llmadapter.ValueDetailed[answer](context.Background(), caller, "Return the typed answer.")
	if err != nil {
		panic(err)
	}
	details, ok := result.Response.ProviderDetails.(codexcaller.Details)
	if !ok || !reflect.DeepEqual(details.Run, want) {
		panic(fmt.Sprintf("exact SDK result was not preserved: %#v", result.Response.ProviderDetails))
	}
	if result.Value.Answer != "verified" || result.Response.Execution.ProviderName != "codex" || result.Response.Execution.EffectiveModel != "proxy-model" || result.Response.Execution.Usage == nil || result.Response.Execution.Usage.InputTokens != 11 {
		panic(fmt.Sprintf("neutral typed evidence was not preserved: %#v", result))
	}
	attestation := struct {
		Kind                 string ` + "`json:\"kind\"`" + `
		TypedValue           string ` + "`json:\"typed_value\"`" + `
		ProviderName         string ` + "`json:\"provider_name\"`" + `
		EffectiveModel       string ` + "`json:\"effective_model\"`" + `
		NeutralInputTokens   int64  ` + "`json:\"neutral_input_tokens\"`" + `
		ExactThreadID        string ` + "`json:\"exact_thread_id\"`" + `
		ExactTurnID          string ` + "`json:\"exact_turn_id\"`" + `
		ExactResultPreserved bool   ` + "`json:\"exact_result_preserved\"`" + `
	}{
		Kind: "typed_three_layer_call", TypedValue: result.Value.Answer,
		ProviderName: result.Response.Execution.ProviderName, EffectiveModel: result.Response.Execution.EffectiveModel,
		NeutralInputTokens: result.Response.Execution.Usage.InputTokens,
		ExactThreadID: details.Run.Start.Thread.ID, ExactTurnID: details.Run.Run.Turn.ID,
		ExactResultPreserved: true,
	}
	if err := json.NewEncoder(os.Stdout).Encode(attestation); err != nil {
		panic(err)
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
