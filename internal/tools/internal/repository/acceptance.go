package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const migrationAcceptanceFormatVersion = 1

var migrationAcceptanceCategoryIDs = []string{
	"provenance",
	"api_behavior_equivalence",
	"architecture_boundaries",
	"independent_consumption",
	"published_dependency_chain",
	"cutover_readiness",
}

type MigrationAcceptanceReport struct {
	FormatVersion int                           `json:"format_version"`
	Subject       MigrationAcceptanceSubject    `json:"subject"`
	Complete      bool                          `json:"complete"`
	Artifacts     []MigrationAcceptanceArtifact `json:"artifacts"`
	Categories    []MigrationAcceptanceCategory `json:"categories"`
	ReportDigest  string                        `json:"report_digest"`
}

type MigrationAcceptanceSubject struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
	Tree       string `json:"tree"`
}

type MigrationAcceptanceArtifact struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Locator string `json:"locator"`
	SHA256  string `json:"sha256,omitempty"`
	Sum     string `json:"sum,omitempty"`
}

type MigrationAcceptanceCategory struct {
	ID     string                     `json:"id"`
	Status string                     `json:"status"`
	Checks []MigrationAcceptanceCheck `json:"checks"`
}

type MigrationAcceptanceCheck struct {
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	Artifacts []string `json:"artifacts"`
	Error     string   `json:"error,omitempty"`
}

type MigrationAcceptanceOptions struct {
	SourceEvidenceDirectory string
	GitHubToken             string
	GitHubAPI               string
	HTTPClient              *http.Client
	Publish                 PublishOptions
}

type migrationAuditBuilder struct {
	report           MigrationAcceptanceReport
	byID             map[string]int
	artifactFailures map[string][]migrationArtifactFailure
}

type migrationArtifactFailure struct {
	artifact string
	err      string
}

func newMigrationAuditBuilder(commit, tree string) *migrationAuditBuilder {
	report := MigrationAcceptanceReport{
		FormatVersion: migrationAcceptanceFormatVersion,
		Subject: MigrationAcceptanceSubject{
			Repository: "https://github.com/ronhuafeng/llm-go", Commit: commit, Tree: tree,
		},
	}
	byID := make(map[string]int, len(migrationAcceptanceCategoryIDs))
	for _, id := range migrationAcceptanceCategoryIDs {
		byID[id] = len(report.Categories)
		report.Categories = append(report.Categories, MigrationAcceptanceCategory{ID: id, Status: "complete"})
	}
	return &migrationAuditBuilder{report: report, byID: byID, artifactFailures: map[string][]migrationArtifactFailure{}}
}

func (builder *migrationAuditBuilder) artifact(kind, locator, digest, sum string) string {
	id := "artifact-" + fmt.Sprint(len(builder.report.Artifacts)+1)
	builder.report.Artifacts = append(builder.report.Artifacts, MigrationAcceptanceArtifact{
		ID: id, Kind: kind, Locator: locator, SHA256: digest, Sum: sum,
	})
	return id
}

func (builder *migrationAuditBuilder) check(category, name string, artifacts []string, run func() error) {
	index := builder.byID[category]
	check := MigrationAcceptanceCheck{Name: name, Status: "passed", Artifacts: artifacts}
	if err := run(); err != nil {
		check.Status = "failed"
		check.Error = err.Error()
		builder.report.Categories[index].Status = "incomplete"
	}
	builder.report.Categories[index].Checks = append(builder.report.Categories[index].Checks, check)
}

func BuildMigrationAcceptanceReport(ctx context.Context, root string, options MigrationAcceptanceOptions) (MigrationAcceptanceReport, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return MigrationAcceptanceReport{}, fmt.Errorf("resolve repository root: %w", err)
	}
	commit, err := resolveCommit(root, "HEAD")
	if err != nil {
		return MigrationAcceptanceReport{}, err
	}
	tree, err := resolveTree(root, commit)
	if err != nil {
		return MigrationAcceptanceReport{}, err
	}
	builder := newMigrationAuditBuilder(commit, tree)

	manifestPath := filepath.Join(root, provenanceFilename)
	manifestID := builder.fileArtifact(root, "migration provenance manifest", manifestPath, "provenance")
	registryPath := filepath.Join(root, registryFilename)
	registryID := builder.fileArtifact(root, "module registry", registryPath, "architecture_boundaries")

	registered, registryErr := loadPopulatedRegistry(root)
	if registryErr != nil {
		registered = registry{}
	}
	provenanceArtifacts := append([]string{manifestID}, auditProvenanceArtifacts(root, builder)...)
	builder.check("provenance", "immutable imported Git histories and relocation DAG", provenanceArtifacts, func() error {
		if registryErr != nil {
			return registryErr
		}
		violations := verifyProvenance(root, registered, commandGit{root: root})
		if len(violations) != 0 {
			return fmt.Errorf("%s", strings.Join(violations, "; "))
		}
		return nil
	})

	mappingArtifacts, mappingErr := auditAPIInventoryMappings(root, registered, builder)
	builder.check("api_behavior_equivalence", "mapped legacy-to-current canonical API inventories", mappingArtifacts, func() error { return mappingErr })

	sourceEvidence, sourceErrs := auditSourceEvidence(root, options.SourceEvidenceDirectory, builder)
	equivalenceArtifacts := append([]string(nil), sourceEvidence.currentAndRace...)
	builder.check("api_behavior_equivalence", "public behavior, generated output, and race suites", equivalenceArtifacts, func() error {
		return sourceErrs.forStages("current", "race")
	})

	architectureArtifacts := []string{registryID, builder.fileArtifact(root, "workspace registry", filepath.Join(root, "go.work"), "architecture_boundaries")}
	for _, candidate := range registered.Modules {
		architectureArtifacts = append(architectureArtifacts, builder.fileArtifact(root, "registered module metadata", filepath.Join(root, filepath.FromSlash(candidate.Dir), "go.mod"), "architecture_boundaries"))
	}
	builder.check("architecture_boundaries", "registered modules, workspace, and permitted dependency edges", architectureArtifacts, func() error {
		return Verify(root)
	})
	builder.check("architecture_boundaries", "exactly three public modules and one non-published tool module", []string{registryID}, func() error {
		if registryErr != nil {
			return registryErr
		}
		published := 0
		for _, candidate := range registered.Modules {
			if candidate.Published {
				published++
			}
		}
		if len(registered.Modules) != 4 || published != 3 {
			return fmt.Errorf("registry has %d modules and %d public modules", len(registered.Modules), published)
		}
		return nil
	})

	builder.check("independent_consumption", "minimum-Go, current-Go, race, archives, and standalone consumers", sourceEvidence.all, func() error {
		return sourceErrs.all()
	})

	if options.GitHubAPI == "" {
		options.GitHubAPI = "https://api.github.com"
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{Timeout: 2 * time.Minute}
	}
	if options.Publish.Proxy == "" {
		options.Publish = PublishOptions{
			Proxy: "https://proxy.golang.org", SumDB: "sum.golang.org", Timeout: 5 * time.Minute,
			RetryInterval: 10 * time.Second, CommandTimeout: 5 * time.Minute,
		}
	}
	releaseArtifacts, plans, releaseErr := auditReleaseAssets(ctx, root, options, builder)
	builder.check("published_dependency_chain", "published Release plans, authorizations, and evidence assets", releaseArtifacts, func() error { return releaseErr })

	proxyArtifacts, proxyErr := auditFreshPublishedChain(ctx, plans, options.Publish, builder)
	builder.check("published_dependency_chain", "fresh public-Proxy tags and exact adapter tuple typed consumer", proxyArtifacts, func() error { return proxyErr })

	legacyArtifacts, legacyErr := auditLegacyProxyArtifacts(ctx, root, options.Publish, builder)
	builder.check("cutover_readiness", "final legacy migration releases remain public-Proxy available", legacyArtifacts, func() error { return legacyErr })
	activeArtifacts, activeErr := auditActiveDependencies(root, registered, builder)
	builder.check("cutover_readiness", "active CI, release tooling, docs, and gates are independent of legacy repositories and historical proposals", activeArtifacts, func() error { return activeErr })

	builder.failUnreadableArtifacts()
	for index := range builder.report.Categories {
		if len(builder.report.Categories[index].Checks) == 0 || builder.report.Categories[index].Status != "complete" {
			builder.report.Complete = false
			goto finalized
		}
	}
	builder.report.Complete = true

finalized:
	if err := finalizeMigrationAcceptanceReport(&builder.report); err != nil {
		return builder.report, err
	}
	if err := builder.report.Validate(); err != nil {
		return builder.report, err
	}
	if !builder.report.Complete {
		return builder.report, fmt.Errorf("migration acceptance is incomplete")
	}
	return builder.report, nil
}

func (builder *migrationAuditBuilder) failUnreadableArtifacts() {
	if len(builder.artifactFailures) == 0 {
		return
	}
	for category, failures := range builder.artifactFailures {
		artifactIDs := make([]string, 0, len(failures))
		messages := make([]string, 0, len(failures))
		for _, failure := range failures {
			artifactIDs = append(artifactIDs, failure.artifact)
			messages = append(messages, failure.err)
		}
		builder.check(category, "reported filesystem artifacts for this category are readable and hashable", artifactIDs, func() error {
			return fmt.Errorf("%s", strings.Join(messages, "; "))
		})
	}
}

func auditProvenanceArtifacts(root string, builder *migrationAuditBuilder) []string {
	provenance, err := loadProvenance(root)
	if err != nil {
		return []string{builder.artifact("unavailable migration provenance", provenanceFilename, "unavailable:"+err.Error(), "")}
	}
	var artifacts []string
	for _, imported := range provenance.Imports {
		artifacts = append(artifacts,
			builder.fileArtifact(root, "legacy annotated tag object", filepath.Join(root, filepath.FromSlash(imported.Source.TagEvidence)), "provenance"),
			builder.artifact("legacy source Git commit", "git:"+imported.Source.Commit+"^{commit}", "", "git-object:"+imported.Source.Commit+";tree:"+imported.Source.Tree),
			builder.artifact("pure relocation Git commit", "git:"+imported.Relocation.Commit+"^{commit}", "", "git-object:"+imported.Relocation.Commit+";subtree:"+imported.Relocation.Subtree),
			builder.artifact("independent history merge Git commit", "git:"+imported.Merge.Commit+"^{commit}", "", "git-object:"+imported.Merge.Commit+";parents:"+imported.Merge.FirstParent+","+imported.Merge.SecondParent),
		)
	}
	return artifacts
}

func (builder *migrationAuditBuilder) fileArtifact(root, kind, path string, categories ...string) string {
	locator, err := filepath.Rel(root, path)
	if err != nil {
		locator = path
	}
	digest, err := fileSHA256(path)
	id := ""
	if err != nil {
		digest = "unavailable:" + err.Error()
	}
	id = builder.artifact(kind, filepath.ToSlash(locator), digest, "")
	if err != nil {
		for _, category := range categories {
			builder.artifactFailures[category] = append(builder.artifactFailures[category], migrationArtifactFailure{artifact: id, err: filepath.ToSlash(locator) + ": " + err.Error()})
		}
	}
	return id
}

type auditSourceArtifactSet struct {
	all            []string
	currentAndRace []string
}

type auditSourceErrors map[string][]error

func (errorsByStage auditSourceErrors) forStages(stages ...string) error {
	var messages []string
	for _, stage := range stages {
		for _, err := range errorsByStage[stage] {
			messages = append(messages, err.Error())
		}
	}
	if len(messages) != 0 {
		return fmt.Errorf("%s", strings.Join(messages, "; "))
	}
	return nil
}

func (errorsByStage auditSourceErrors) all() error {
	return errorsByStage.forStages("minimum", "current", "race", "checkout")
}

func auditSourceEvidence(root, directory string, builder *migrationAuditBuilder) (auditSourceArtifactSet, auditSourceErrors) {
	result := auditSourceArtifactSet{}
	errorsByStage := auditSourceErrors{}
	commit, _ := resolveCommit(root, "HEAD")
	tree, _ := resolveTree(root, commit)
	for _, moduleID := range []string{"llmkit", "codexsdk", "codex-adapter"} {
		for _, stage := range []string{"minimum", "current", "race"} {
			name := fmt.Sprintf("evidence-%s-%s.json", moduleID, stage)
			path := filepath.Join(directory, name)
			categories := []string{"independent_consumption"}
			if stage != "minimum" {
				categories = append(categories, "api_behavior_equivalence")
			}
			artifact := builder.fileArtifact(root, "module source evidence", path, categories...)
			result.all = append(result.all, artifact)
			if stage != "minimum" {
				result.currentAndRace = append(result.currentAndRace, artifact)
			}
			var evidence Evidence
			err := readStrictJSON(path, &evidence)
			if err == nil {
				err = validateAcceptanceSourceEvidence(evidence, commit, tree, "module_source", moduleID, stage)
			}
			if err != nil {
				errorsByStage[stage] = append(errorsByStage[stage], fmt.Errorf("%s: %w", name, err))
			}
		}
	}
	path := filepath.Join(directory, "evidence-checkout.json")
	artifact := builder.fileArtifact(root, "checkout source evidence", path, "independent_consumption")
	result.all = append(result.all, artifact)
	var evidence Evidence
	err := readStrictJSON(path, &evidence)
	if err == nil {
		err = validateAcceptanceSourceEvidence(evidence, commit, tree, "checkout_source", "", "")
	}
	if err != nil {
		errorsByStage["checkout"] = append(errorsByStage["checkout"], fmt.Errorf("evidence-checkout.json: %w", err))
	}
	return result, errorsByStage
}

func validateAcceptanceSourceEvidence(evidence Evidence, commit, tree, kind, moduleID, stage string) error {
	if evidence.FormatVersion != evidenceFormatVersion || evidence.Subject.Commit != commit || evidence.Subject.Tree != tree || evidence.Subject.Kind != kind || evidence.Subject.Module != moduleID || evidence.Subject.Stage != stage {
		return fmt.Errorf("source evidence subject does not match audit checkout")
	}
	if len(evidence.Checks) == 0 {
		return fmt.Errorf("source evidence contains no checks")
	}
	want := acceptanceSourceCheckNames(kind, moduleID, stage)
	if len(evidence.Checks) != len(want) {
		return fmt.Errorf("source evidence has %d checks, want exact %d-check contract", len(evidence.Checks), len(want))
	}
	for index, check := range evidence.Checks {
		if check.Name != want[index] {
			return fmt.Errorf("source check %d is %q, want %q", index, check.Name, want[index])
		}
		if check.Status != "passed" || check.Error != "" {
			return fmt.Errorf("source check %q is not passed", check.Name)
		}
	}
	return nil
}

func acceptanceSourceCheckNames(kind, moduleID, stage string) []string {
	if kind == "checkout_source" {
		return []string{
			"source identity", "repository contract", "repository tool tests", "workspace three-layer canary",
			"ephemeral source consumer: codex-adapter", "ephemeral source consumer: codexsdk", "ephemeral source consumer: llmkit", "final source identity",
		}
	}
	switch stage {
	case "minimum":
		return []string{"source identity", "minimum Go tests", "final source identity"}
	case "race":
		return []string{"source identity", "race tests", "final source identity"}
	case "current":
		checks := []string{"source identity", "Go formatting", "module checksums", "module metadata", "Go vet", "ordinary tests", "public API inventory", "module archive boundaries"}
		if moduleID == "codexsdk" {
			checks = append(checks, "module-owned SDK generator validation")
		}
		return append(checks, "final source identity")
	default:
		return nil
	}
}

func auditAPIInventoryMappings(root string, registered registry, builder *migrationAuditBuilder) ([]string, error) {
	provenance, err := loadProvenance(root)
	if err != nil {
		return nil, err
	}
	var artifacts []string
	var failures []string
	for _, imported := range provenance.Imports {
		candidate, ok := findModule(registered, imported.ID)
		if !ok {
			failures = append(failures, "missing registered module "+imported.ID)
			continue
		}
		inventoryPath, err := apiInventoryPath(candidate.ID)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		legacyPath := inventoryPath
		if candidate.ID == "codexsdk" {
			legacyPath = "codexsdk/" + inventoryPath
		}
		legacy, legacyErr := gitBytes(root, "show", imported.Source.Commit+":"+legacyPath)
		if legacyErr == nil {
			digest := sha256.Sum256(legacy)
			artifacts = append(artifacts, builder.artifact("legacy canonical API inventory", "git:"+imported.Source.Commit+":"+legacyPath, hex.EncodeToString(digest[:]), ""))
		}
		artifacts = append(artifacts, builder.fileArtifact(root, "current canonical API inventory", filepath.Join(root, filepath.FromSlash(candidate.Dir), filepath.FromSlash(inventoryPath)), "api_behavior_equivalence"))
		if _, mapErr := validateMigrationAPIInventory(root, candidate, inventoryPath); mapErr != nil {
			failures = append(failures, candidate.ID+": "+mapErr.Error())
		}
	}
	if len(failures) != 0 {
		return artifacts, fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return artifacts, nil
}

func validateMigrationAPIInventory(root string, candidate module, inventoryPath string) (string, error) {
	provenance, err := loadProvenance(root)
	if err != nil {
		return "", err
	}
	var sourceCommit string
	for _, imported := range provenance.Imports {
		if imported.ID == candidate.ID {
			sourceCommit = imported.Source.Commit
			break
		}
	}
	if sourceCommit == "" {
		return "", fmt.Errorf("module %s has no source commit for API baseline", candidate.ID)
	}
	legacyPath := inventoryPath
	if candidate.ID == "codexsdk" {
		legacyPath = "codexsdk/" + inventoryPath
	}
	legacy, err := gitBytes(root, "show", sourceCommit+":"+legacyPath)
	if err != nil {
		return "", fmt.Errorf("read legacy API inventory: %w", err)
	}
	current, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(candidate.Dir), filepath.FromSlash(inventoryPath)))
	if err != nil {
		return "", fmt.Errorf("read current API inventory: %w", err)
	}
	type mapping struct{ old, new string }
	mappings := make([]mapping, 0, len(provenance.Imports))
	for _, imported := range provenance.Imports {
		old, err := legacyModulePath(root, imported)
		if err != nil {
			return "", fmt.Errorf("read legacy module identity for %s: %w", imported.ID, err)
		}
		switch imported.ID {
		case "codexsdk":
			old += "/codexsdk"
		case "codex-adapter":
			old += "/llmcaller/codex"
		}
		mappings = append(mappings, mapping{old: old, new: imported.Destination.Module})
	}
	sort.Slice(mappings, func(i, j int) bool { return len(mappings[i].old) > len(mappings[j].old) })
	mapped := append([]byte(nil), legacy...)
	for _, mapping := range mappings {
		mapped = bytes.ReplaceAll(mapped, []byte(mapping.old), []byte(mapping.new))
	}
	if !bytes.Equal(mapped, current) {
		return "", fmt.Errorf("current API inventory is not equivalent to the legacy inventory after the declared module-path mapping")
	}
	digest := sha256.Sum256(mapped)
	return hex.EncodeToString(digest[:]), nil
}

type acceptanceGitHubRelease struct {
	TagName         string                         `json:"tag_name"`
	TargetCommitish string                         `json:"target_commitish"`
	Draft           bool                           `json:"draft"`
	Prerelease      bool                           `json:"prerelease"`
	Assets          []acceptanceGitHubReleaseAsset `json:"assets"`
}

type acceptanceGitHubReleaseAsset struct {
	Name               string `json:"name"`
	Digest             string `json:"digest"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func auditReleaseAssets(ctx context.Context, root string, options MigrationAcceptanceOptions, builder *migrationAuditBuilder) ([]string, map[string]ReleasePlan, error) {
	plans := map[string]ReleasePlan{}
	var artifacts []string
	var failures []string
	provenance, err := loadProvenance(root)
	if err != nil {
		return artifacts, plans, err
	}
	for _, imported := range provenance.Imports {
		moduleID := imported.ID
		tag := imported.Destination.FirstTag
		endpoint := strings.TrimRight(options.GitHubAPI, "/") + "/repos/ronhuafeng/llm-go/releases/tags/" + url.PathEscape(tag)
		var release acceptanceGitHubRelease
		if err := getJSON(ctx, options.HTTPClient, options.GitHubToken, endpoint, &release); err != nil {
			failures = append(failures, moduleID+": "+err.Error())
			continue
		}
		if release.TagName != tag || release.Draft || release.Prerelease {
			failures = append(failures, moduleID+": GitHub Release is not the exact published stable tag")
			continue
		}
		assets := map[string][]byte{}
		for _, name := range []string{"release-plan.json", "release-authorization.json", "published-evidence.json"} {
			asset, ok := findGitHubAsset(release.Assets, name)
			if !ok {
				failures = append(failures, moduleID+": missing Release asset "+name)
				continue
			}
			data, err := getBytes(ctx, options.HTTPClient, options.GitHubToken, asset.BrowserDownloadURL)
			if err != nil {
				failures = append(failures, moduleID+"/"+name+": "+err.Error())
				continue
			}
			digest := sha256.Sum256(data)
			got := "sha256:" + hex.EncodeToString(digest[:])
			if asset.Digest == "" || got != asset.Digest {
				failures = append(failures, moduleID+"/"+name+": Release asset digest mismatch")
				continue
			}
			artifacts = append(artifacts, builder.artifact("GitHub Release evidence asset", asset.BrowserDownloadURL, strings.TrimPrefix(got, "sha256:"), ""))
			assets[name] = data
		}
		var plan ReleasePlan
		var authorization ReleaseAuthorization
		var published PublishedEvidence
		if err := strictJSONBytes(assets["release-plan.json"], &plan); err != nil {
			failures = append(failures, moduleID+": decode release plan: "+err.Error())
			continue
		}
		if err := plan.Validate(); err != nil {
			failures = append(failures, moduleID+": "+err.Error())
			continue
		}
		if err := strictJSONBytes(assets["release-authorization.json"], &authorization); err != nil {
			failures = append(failures, moduleID+": decode release authorization: "+err.Error())
			continue
		}
		if err := authorization.Validate(); err != nil {
			failures = append(failures, moduleID+": "+err.Error())
			continue
		}
		if err := strictJSONBytes(assets["published-evidence.json"], &published); err != nil {
			failures = append(failures, moduleID+": decode published evidence: "+err.Error())
			continue
		}
		planHash := sha256.Sum256(assets["release-plan.json"])
		if plan.Subject.ModuleID != moduleID || plan.Subject.Tag != tag || release.TargetCommitish != plan.Subject.Commit || authorization.Subject.Commit != plan.Subject.Commit || authorization.Subject.ModuleID != moduleID || authorization.Subject.Tag != tag || authorization.Plan.PlanDigest != plan.PlanDigest || authorization.Plan.SHA256 != hex.EncodeToString(planHash[:]) {
			failures = append(failures, moduleID+": Release artifacts do not bind one exact tag, commit, plan, and authorization")
			continue
		}
		if err := validatePublishedEvidenceAsset(published); err != nil {
			failures = append(failures, moduleID+": "+err.Error())
			continue
		}
		if published.Subject.Module != plan.Subject.ModulePath || published.Subject.Version != plan.Subject.TargetVersion || published.Subject.Tag != tag || published.Subject.TagCommit != plan.Subject.Commit || published.Subject.PlanDigest != plan.PlanDigest || published.Subject.AuthorizationDigest != authorization.AuthorizationDigest {
			failures = append(failures, moduleID+": published evidence subject does not match release artifacts")
			continue
		}
		plans[moduleID] = plan
	}
	if len(failures) != 0 {
		return artifacts, plans, fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return artifacts, plans, nil
}

func validatePublishedEvidenceAsset(evidence PublishedEvidence) error {
	// The first llmkit and codexsdk releases carry the immutable v1 evidence
	// schema; the adapter carries v2. Both are owned published artifacts and
	// remain verifiable with the common fields below.
	if (evidence.FormatVersion != 1 && evidence.FormatVersion != publishedEvidenceFormatVersion) || evidence.Subject.Kind != "published_module" || evidence.Subject.PlanDigest == "" || evidence.Subject.AuthorizationDigest == "" || evidence.EvidenceDigest == "" {
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
	if evidence.Environment.GOPROXY != "https://proxy.golang.org" || evidence.Environment.GOSUMDB != "sum.golang.org" || evidence.Environment.GOWORK != "off" || evidence.Environment.GOVCS != "*:off" || !evidence.Environment.FreshCaches || !evidence.Environment.SeparateCaches {
		return fmt.Errorf("published evidence environment is not exclusive and fresh")
	}
	if len(evidence.Checks) == 0 || evidence.Resolved.Path == "" || evidence.Resolved.Version == "" || evidence.Resolved.Sum == "" || evidence.Resolved.GoModSum == "" || evidence.Resolved.Origin == nil {
		return fmt.Errorf("published evidence has incomplete resolution")
	}
	for _, check := range evidence.Checks {
		if check.Status != "passed" || check.Error != "" {
			return fmt.Errorf("published check %q is not passed", check.Name)
		}
	}
	return nil
}

func auditFreshPublishedChain(ctx context.Context, plans map[string]ReleasePlan, options PublishOptions, builder *migrationAuditBuilder) ([]string, error) {
	var artifacts []string
	var failures []string
	for _, moduleID := range []string{"llmkit", "codexsdk", "codex-adapter"} {
		plan, ok := plans[moduleID]
		if !ok {
			failures = append(failures, "missing validated release plan for "+moduleID)
			continue
		}
		moduleVersion := plan.Subject.ModulePath + "@" + plan.Subject.TargetVersion
		deadline, cancel := context.WithTimeout(ctx, options.Timeout)
		if err := waitForProxy(deadline, moduleVersion, options, runPublishedCommand); err != nil {
			cancel()
			failures = append(failures, moduleID+": "+err.Error())
			continue
		}
		download, err := downloadFromFreshCache(deadline, moduleVersion, options, runPublishedCommand)
		if err != nil {
			cancel()
			failures = append(failures, moduleID+": "+err.Error())
			continue
		}
		resolution, err := validateModuleDownload(download, plan)
		if err != nil {
			cancel()
			failures = append(failures, moduleID+": "+err.Error())
			continue
		}
		artifacts = append(artifacts, builder.artifact("fresh public Proxy module", moduleVersion, resolution.ZipSHA256, resolution.Sum))
		if moduleID == "codex-adapter" {
			declared, tupleErr := validateAdapterArtifactMetadata(download.goModData, plan)
			if tupleErr == nil {
				var consumer PublishedConsumer
				consumer, resolution.Tuple, tupleErr = runPublishedConsumer(deadline, plan, declared, options, runPublishedCommand)
				if tupleErr == nil && (consumer.Kind == "" || !consumer.ExactResultPreserved) {
					tupleErr = fmt.Errorf("adapter typed consumer did not preserve exact evidence")
				}
			}
			if tupleErr != nil {
				failures = append(failures, moduleID+": "+tupleErr.Error())
			}
		}
		cancel()
	}
	if len(failures) != 0 {
		return artifacts, fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return artifacts, nil
}

func auditLegacyProxyArtifacts(ctx context.Context, root string, options PublishOptions, builder *migrationAuditBuilder) ([]string, error) {
	provenance, err := loadProvenance(root)
	if err != nil {
		return nil, err
	}
	var artifacts []string
	var failures []string
	for _, imported := range provenance.Imports {
		modulePath, err := legacyModulePath(root, imported)
		if err != nil {
			failures = append(failures, imported.ID+": "+err.Error())
			continue
		}
		moduleVersion := modulePath + "@" + imported.Source.Tag
		deadline, cancel := context.WithTimeout(ctx, options.Timeout)
		download, err := downloadFromFreshCache(deadline, moduleVersion, options, runPublishedCommand)
		cancel()
		if err != nil {
			failures = append(failures, imported.ID+": "+err.Error())
			continue
		}
		if download.Path != modulePath || download.Version != imported.Source.Tag || download.Sum == "" || download.GoModSum == "" || download.Origin == nil || download.Origin.VCS != "git" || canonicalRepositoryURL(download.Origin.URL) != canonicalRepositoryURL(imported.Source.Repository) || download.Origin.Hash != imported.Source.Commit || download.Origin.Ref != "refs/tags/"+imported.Source.Tag {
			failures = append(failures, imported.ID+": legacy Proxy origin does not match provenance")
			continue
		}
		artifacts = append(artifacts, builder.artifact("fresh legacy public Proxy module", moduleVersion, download.zipSHA256, download.Sum))
	}
	if len(failures) != 0 {
		return artifacts, fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return artifacts, nil
}

func auditActiveDependencies(root string, registered registry, builder *migrationAuditBuilder) ([]string, error) {
	paths := []string{filepath.Join(root, ".github"), filepath.Join(root, "internal", "tools"), filepath.Join(root, "docs", "releasing.md")}
	for _, candidate := range registered.Modules {
		paths = append(paths, filepath.Join(root, filepath.FromSlash(candidate.Dir), "go.mod"))
		architectureRoot := filepath.Join(root, filepath.FromSlash(candidate.Dir), "internal", "architecture")
		if _, err := os.Stat(architectureRoot); err == nil {
			paths = append(paths, architectureRoot)
		}
	}
	paths = append(paths, filepath.Join(root, "codexsdk", "public_api_test.go"))
	legacyPrefix := "github.com/ronhuafeng/"
	legacy := []string{legacyPrefix + "llmkit-go", legacyPrefix + "codexsdk-go", legacyPrefix + "llmcaller-codex-go"}
	var artifacts []string
	var violations []string
	for _, start := range paths {
		info, err := os.Stat(start)
		if err != nil {
			violations = append(violations, err.Error())
			continue
		}
		visit := func(path string) {
			relative, _ := filepath.Rel(root, path)
			relative = filepath.ToSlash(relative)
			artifacts = append(artifacts, builder.fileArtifact(root, "active dependency scan input", path, "cutover_readiness"))
			data, err := os.ReadFile(path)
			if err != nil {
				violations = append(violations, err.Error())
				return
			}
			violations = append(violations, activeLegacyReferenceViolations(relative, data, legacy)...)
		}
		if info.IsDir() {
			walkErr := filepath.WalkDir(start, func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() {
					return nil
				}
				visit(path)
				return nil
			})
			if walkErr != nil {
				violations = append(violations, walkErr.Error())
			}
		} else {
			visit(start)
		}
	}
	for _, locator := range []string{".github/workflows", ".github/scripts", "internal/tools", "docs/releasing.md", registryFilename} {
		artifacts = append(artifacts, builder.artifact("active gate scan scope", locator, "", ""))
	}
	if registryErr := verifyHistoricalProposalIsolation(root, registered); len(registryErr) != 0 {
		violations = append(violations, registryErr...)
	}
	provenance, provenanceErr := loadProvenance(root)
	if provenanceErr != nil {
		violations = append(violations, provenanceErr.Error())
	}
	requiredDocs := []string{"README.md", "CONTEXT-MAP.md", "docs/verification.md", "docs/releasing.md", "docs/migration/README.md"}
	docAllowances := legacyDocumentationAllowances{migration: map[string]bool{}, changelogOwner: map[string]string{}, readmeVersion: map[string]string{}}
	for _, imported := range provenance.Imports {
		version, versionErr := migrationVersion(imported.Destination.FirstTag)
		if versionErr != nil {
			violations = append(violations, imported.ID+" has invalid first-tag documentation identity")
			continue
		}
		readme := filepath.ToSlash(filepath.Join(imported.Destination.Directory, "README.md"))
		changelog := filepath.ToSlash(filepath.Join(imported.Destination.Directory, "CHANGELOG.md"))
		migration := filepath.ToSlash(filepath.Join(imported.Destination.Directory, "docs", "migration", version+".md"))
		requiredDocs = append(requiredDocs, readme, changelog, migration)
		legacyModule, moduleErr := legacyModulePath(root, imported)
		if moduleErr != nil {
			violations = append(violations, imported.ID+": "+moduleErr.Error())
			continue
		}
		docAllowances.migration[migration] = true
		docAllowances.changelogOwner[changelog] = legacyModule
		docAllowances.readmeVersion[readme] = legacyModule + "@" + imported.Source.Tag
	}
	for _, required := range requiredDocs {
		path := filepath.Join(root, filepath.FromSlash(required))
		data, err := os.ReadFile(path)
		if err != nil {
			violations = append(violations, "missing migration documentation "+required)
		} else {
			artifacts = append(artifacts, builder.fileArtifact(root, "migration documentation", path, "cutover_readiness"))
			if strings.Contains(string(data), "v0.2-"+"refactor-plan.md") {
				violations = append(violations, required+" depends on a historical refactor proposal")
			}
			violations = append(violations, documentationLegacyReferenceViolations(required, data, legacy, docAllowances)...)
		}
	}
	if len(violations) != 0 {
		sort.Strings(violations)
		return artifacts, fmt.Errorf("%s", strings.Join(violations, "; "))
	}
	return artifacts, nil
}

func activeLegacyReferenceViolations(relative string, data []byte, legacy []string) []string {
	var violations []string
	for lineNumber, line := range strings.Split(string(data), "\n") {
		for _, needle := range legacy {
			if strings.Contains(line, needle) && !allowedLegacyFixtureReference(relative, line, needle) {
				violations = append(violations, fmt.Sprintf("%s:%d has an active legacy-repository dependency on %s", relative, lineNumber+1, needle))
			}
		}
	}
	return violations
}

func allowedLegacyFixtureReference(relative, line, legacyPath string) bool {
	trimmed := strings.TrimSpace(line)
	switch relative {
	case "internal/tools/internal/repository/release_test.go":
		prefix := "github.com/ronhuafeng/"
		if legacyPath != prefix+"llmkit-go" && legacyPath != prefix+"codexsdk-go" && legacyPath != prefix+"llmcaller-codex-go" {
			return false
		}
		for _, marker := range []string{"legacy := []byte(", "mapAPIInventory(", "mappedAPIInventoryDigest(", "{Old:"} {
			if strings.Contains(line, marker) {
				return true
			}
		}
		return strings.HasPrefix(trimmed, `"`) && strings.Contains(trimmed, `\n"`)
	case "llmkit/internal/architecture/toolkit_boundary_test.go":
		return trimmed == `"`+legacyPath+`",`
	}
	return false
}

type legacyDocumentationAllowances struct {
	migration      map[string]bool
	changelogOwner map[string]string
	readmeVersion  map[string]string
}

func documentationLegacyReferenceViolations(relative string, data []byte, legacy []string, allowances legacyDocumentationAllowances) []string {
	var violations []string
	for lineNumber, line := range strings.Split(string(data), "\n") {
		for _, needle := range legacy {
			if !strings.Contains(line, needle) {
				continue
			}
			allowed := allowances.migration[relative] || allowances.changelogOwner[relative] == needle
			if expected := allowances.readmeVersion[relative]; strings.Contains(line, expected) && (strings.Contains(line, "Consumers of") || strings.Contains(line, "should follow") || strings.Contains(line, "Final legacy release:")) {
				allowed = true
			}
			if !allowed {
				violations = append(violations, fmt.Sprintf("%s:%d has non-migration guidance for legacy module %s", relative, lineNumber+1, needle))
			}
		}
	}
	return violations
}

func getJSON(ctx context.Context, client *http.Client, token, endpoint string, target any) error {
	data, err := getBytes(ctx, client, token, endpoint)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode GitHub response: %w", err)
	}
	return nil
}

func getBytes(ctx context.Context, client *http.Client, token, endpoint string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return nil, fmt.Errorf("GET %s: %s: %s", endpoint, response.Status, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(response.Body)
}

func strictJSONBytes(data []byte, target any) error {
	if len(data) == 0 {
		return fmt.Errorf("empty JSON artifact")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return err
	}
	return nil
}

func findGitHubAsset(assets []acceptanceGitHubReleaseAsset, name string) (acceptanceGitHubReleaseAsset, bool) {
	var found acceptanceGitHubReleaseAsset
	count := 0
	for _, asset := range assets {
		if asset.Name == name {
			found = asset
			count++
		}
	}
	return found, count == 1
}

func finalizeMigrationAcceptanceReport(report *MigrationAcceptanceReport) error {
	report.ReportDigest = ""
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	report.ReportDigest = "sha256:" + hex.EncodeToString(digest[:])
	return nil
}

func (report MigrationAcceptanceReport) Validate() error {
	if report.FormatVersion != migrationAcceptanceFormatVersion || report.Subject.Repository != "https://github.com/ronhuafeng/llm-go" || len(report.Subject.Commit) != 40 || len(report.Subject.Tree) != 40 {
		return fmt.Errorf("migration acceptance report subject is incomplete")
	}
	if len(report.Categories) != len(migrationAcceptanceCategoryIDs) {
		return fmt.Errorf("migration acceptance report does not contain all mandatory categories")
	}
	artifactIDs := make(map[string]bool, len(report.Artifacts))
	for _, artifact := range report.Artifacts {
		if artifact.ID == "" || artifact.Kind == "" || artifact.Locator == "" || artifactIDs[artifact.ID] {
			return fmt.Errorf("migration acceptance report contains an invalid or duplicate artifact")
		}
		artifactIDs[artifact.ID] = true
	}
	allComplete := true
	for index, category := range report.Categories {
		if category.ID != migrationAcceptanceCategoryIDs[index] || (category.Status != "complete" && category.Status != "incomplete") || len(category.Checks) == 0 {
			return fmt.Errorf("migration acceptance report mandatory categories are incomplete or out of order")
		}
		categoryComplete := true
		for _, check := range category.Checks {
			if check.Name == "" || (check.Status != "passed" && check.Status != "failed") {
				return fmt.Errorf("category %s contains invalid check", category.ID)
			}
			if len(check.Artifacts) == 0 {
				return fmt.Errorf("category %s check %q has no inspected artifacts", category.ID, check.Name)
			}
			for _, artifact := range check.Artifacts {
				if !artifactIDs[artifact] {
					return fmt.Errorf("category %s check %q references unknown artifact %q", category.ID, check.Name, artifact)
				}
			}
			if check.Status != "passed" {
				categoryComplete = false
				if check.Error == "" {
					return fmt.Errorf("failed check %q has no error", check.Name)
				}
			} else if check.Error != "" {
				return fmt.Errorf("passed check %q carries an error", check.Name)
			}
		}
		if (category.Status == "complete") != categoryComplete {
			return fmt.Errorf("category %s cannot be complete when a check failed", category.ID)
		}
		allComplete = allComplete && categoryComplete
	}
	if report.Complete != allComplete {
		return fmt.Errorf("report complete field disagrees with mandatory category status")
	}
	if report.ReportDigest != "" {
		want := report.ReportDigest
		copy := report
		if err := finalizeMigrationAcceptanceReport(&copy); err != nil {
			return err
		}
		if copy.ReportDigest != want {
			return fmt.Errorf("migration acceptance report digest mismatch")
		}
	}
	return nil
}

func WriteMigrationAcceptanceReport(path string, report MigrationAcceptanceReport) error {
	if err := report.Validate(); err != nil {
		return err
	}
	if report.ReportDigest == "" {
		return fmt.Errorf("migration acceptance report digest is missing")
	}
	return writeJSON(path, report)
}
