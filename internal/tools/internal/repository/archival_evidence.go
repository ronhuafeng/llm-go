package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const archivalEvidenceFilename = "docs/migration/archive-evidence.json"

var archivalCategoryIDs = []string{
	"provenance",
	"api_behavior_equivalence",
	"architecture_boundaries",
	"independent_consumption",
	"published_dependency_chain",
	"cutover_readiness",
}

var archivalRepositoryIDs = map[string]int64{
	"codex-adapter": 1265769870,
	"codexsdk":      1265769872,
	"llmkit":        1265769868,
}

type archivalEvidence struct {
	FormatVersion         int                         `json:"format_version"`
	Subject               archivalSubject             `json:"subject"`
	Prerequisite          archivalAcceptanceReference `json:"prerequisite_acceptance"`
	Successor             archivalRepositoryState     `json:"successor"`
	SecurityHandoff       archivalSecurityHandoff     `json:"security_handoff"`
	LegacyRepositories    []archivalLegacyRepository  `json:"legacy_repositories"`
	IssueDispositions     []archivalIssueDisposition  `json:"issue_dispositions"`
	PostArchiveAcceptance archivalAcceptanceReference `json:"post_archive_acceptance"`
	ProxyEnvironment      archivalProxyEnvironment    `json:"proxy_environment"`
	ProxyModules          []archivalProxyModule       `json:"proxy_modules"`
	AdapterTuple          archivalAdapterTuple        `json:"adapter_tuple"`
	ObservedAt            string                      `json:"observed_at"`
	Complete              bool                        `json:"complete"`
	ReportDigest          string                      `json:"report_digest"`
}

type archivalSubject struct {
	Repository string `json:"repository"`
	Issue      int    `json:"issue"`
	Commit     string `json:"commit"`
	Tree       string `json:"tree"`
}

type archivalAcceptanceReference struct {
	RunID            int64                    `json:"run_id"`
	RunAttempt       int                      `json:"run_attempt"`
	RunURL           string                   `json:"run_url"`
	CreatedAt        string                   `json:"created_at"`
	CompletedAt      string                   `json:"completed_at"`
	ArtifactID       int64                    `json:"artifact_id"`
	ArtifactName     string                   `json:"artifact_name"`
	ArtifactSHA256   string                   `json:"artifact_sha256"`
	ReportFileSHA256 string                   `json:"report_file_sha256"`
	ReportDigest     string                   `json:"report_digest"`
	Commit           string                   `json:"commit"`
	Tree             string                   `json:"tree"`
	Complete         bool                     `json:"complete"`
	Categories       []archivalCategoryStatus `json:"categories"`
}

type archivalCategoryStatus struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type archivalRepositoryState struct {
	RepositoryID                  int64  `json:"repository_id"`
	FullName                      string `json:"full_name"`
	URL                           string `json:"url"`
	Archived                      bool   `json:"archived"`
	DefaultBranch                 string `json:"default_branch"`
	HeadCommit                    string `json:"head_commit"`
	Description                   string `json:"description"`
	Homepage                      string `json:"homepage"`
	IssuesEnabled                 bool   `json:"issues_enabled"`
	ActionsEnabled                bool   `json:"actions_enabled"`
	PrivateVulnerabilityReporting bool   `json:"private_vulnerability_reporting"`
}

type archivalSecurityHandoff struct {
	SuccessorEnabledBeforeLegacyDisable bool `json:"successor_enabled_before_legacy_disable"`
	LegacyDisabledBeforeArchive         bool `json:"legacy_disabled_before_archive"`
}

type archivalLegacyRepository struct {
	ID                    string                  `json:"id"`
	RepositoryID          int64                   `json:"repository_id"`
	FullName              string                  `json:"full_name"`
	URL                   string                  `json:"url"`
	LegacyModule          string                  `json:"legacy_module"`
	ReplacementModule     string                  `json:"replacement_module"`
	FinalTag              string                  `json:"final_tag"`
	FinalCommit           string                  `json:"final_commit"`
	Archived              bool                    `json:"archived"`
	ArchiveObservedAt     string                  `json:"archive_observed_at"`
	DefaultBranch         string                  `json:"default_branch"`
	HeadCommit            string                  `json:"head_commit"`
	Description           string                  `json:"description"`
	Homepage              string                  `json:"homepage"`
	OpenIssues            int                     `json:"open_issues"`
	OpenPullRequests      int                     `json:"open_pull_requests"`
	ActionsEnabled        bool                    `json:"actions_enabled"`
	LiveRuns              int                     `json:"live_runs"`
	PreArchivePVRDisabled bool                    `json:"pre_archive_pvr_disabled"`
	PostArchivePVRStatus  string                  `json:"post_archive_pvr_status"`
	TrackedWorkflows      []archivalWorkflowState `json:"tracked_workflows"`
}

type archivalWorkflowState struct {
	ID    int64  `json:"id"`
	Path  string `json:"path"`
	State string `json:"state"`
}

type archivalIssueDisposition struct {
	SourceURL string `json:"source_url"`
	Outcome   string `json:"outcome"`
	TargetURL string `json:"target_url,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type archivalProxyEnvironment struct {
	GOPROXY       string `json:"goproxy"`
	GOSUMDB       string `json:"gosumdb"`
	GOWORK        string `json:"gowork"`
	GOVCS         string `json:"govcs"`
	FreshCaches   bool   `json:"fresh_caches"`
	SeparateCache bool   `json:"separate_caches"`
}

type archivalProxyModule struct {
	Path        string               `json:"path"`
	Version     string               `json:"version"`
	Sum         string               `json:"sum"`
	GoModSum    string               `json:"go_mod_sum"`
	ZipSHA256   string               `json:"zip_sha256"`
	GoModSHA256 string               `json:"go_mod_sha256"`
	Origin      archivalModuleOrigin `json:"origin"`
}

type archivalModuleOrigin struct {
	VCS    string `json:"vcs"`
	URL    string `json:"url"`
	Subdir string `json:"subdir"`
	Hash   string `json:"hash"`
	Ref    string `json:"ref"`
}

type archivalAdapterTuple struct {
	Caller        archivalModuleVersion   `json:"caller"`
	Dependencies  []archivalModuleVersion `json:"dependencies"`
	NoReplace     bool                    `json:"no_replace"`
	NoExclude     bool                    `json:"no_exclude"`
	NoWorkspace   bool                    `json:"no_workspace"`
	TypedConsumer archivalTypedConsumer   `json:"typed_consumer"`
}

type archivalModuleVersion struct {
	Module  string `json:"module"`
	Version string `json:"version"`
}

type archivalTypedConsumer struct {
	Kind                 string `json:"kind"`
	TypedValue           string `json:"typed_value"`
	ProviderName         string `json:"provider_name"`
	EffectiveModel       string `json:"effective_model"`
	NeutralInputTokens   int64  `json:"neutral_input_tokens"`
	ExactThreadID        string `json:"exact_thread_id"`
	ExactTurnID          string `json:"exact_turn_id"`
	ExactResultPreserved bool   `json:"exact_result_preserved"`
}

func verifyArchivalEvidence(root string) []string {
	path := filepath.Join(root, filepath.FromSlash(archivalEvidenceFilename))
	var evidence archivalEvidence
	if err := readStrictJSON(path, &evidence); err != nil {
		return []string{fmt.Sprintf("read archival evidence: %v", err)}
	}
	provenance, err := loadProvenance(root)
	if err != nil {
		return []string{err.Error()}
	}
	if err := evidence.validate(root, provenance); err != nil {
		return []string{err.Error()}
	}
	return nil
}

func (evidence archivalEvidence) validate(root string, provenance provenanceManifest) error {
	if evidence.FormatVersion != 1 || evidence.Subject.Repository != "https://github.com/ronhuafeng/llm-go" || evidence.Subject.Issue != 17 || !objectIDPattern.MatchString(evidence.Subject.Commit) || !objectIDPattern.MatchString(evidence.Subject.Tree) {
		return fmt.Errorf("archival evidence subject is incomplete")
	}
	if !evidence.Complete {
		return fmt.Errorf("archival evidence cannot be incomplete")
	}
	if _, err := time.Parse(time.RFC3339, evidence.ObservedAt); err != nil {
		return fmt.Errorf("archival evidence observed_at is invalid: %w", err)
	}
	commit, err := resolveCommit(root, evidence.Subject.Commit)
	if err != nil || commit != evidence.Subject.Commit {
		return fmt.Errorf("archival evidence subject commit is not available")
	}
	tree, err := resolveTree(root, commit)
	if err != nil || tree != evidence.Subject.Tree {
		return fmt.Errorf("archival evidence subject tree does not match its commit")
	}
	if err := validateAcceptanceReference(evidence.Prerequisite, evidence.Subject); err != nil {
		return fmt.Errorf("prerequisite acceptance: %w", err)
	}
	if err := validateAcceptanceReference(evidence.PostArchiveAcceptance, evidence.Subject); err != nil {
		return fmt.Errorf("post-archive acceptance: %w", err)
	}
	prerequisiteCompleted, _ := time.Parse(time.RFC3339, evidence.Prerequisite.CompletedAt)
	postArchiveCreated, _ := time.Parse(time.RFC3339, evidence.PostArchiveAcceptance.CreatedAt)
	postArchiveCompleted, _ := time.Parse(time.RFC3339, evidence.PostArchiveAcceptance.CompletedAt)
	observedAt, _ := time.Parse(time.RFC3339, evidence.ObservedAt)
	if !postArchiveCreated.After(prerequisiteCompleted) {
		return fmt.Errorf("post-archive acceptance does not follow the prerequisite report")
	}
	if !observedAt.After(postArchiveCompleted) {
		return fmt.Errorf("archival evidence observation does not follow the post-archive report")
	}
	if err := validateSuccessorState(evidence.Successor, evidence.Subject); err != nil {
		return err
	}
	if !evidence.SecurityHandoff.SuccessorEnabledBeforeLegacyDisable || !evidence.SecurityHandoff.LegacyDisabledBeforeArchive {
		return fmt.Errorf("private vulnerability reporting handoff is incomplete")
	}
	if err := validateLegacyRepositoryStates(root, evidence.LegacyRepositories, provenance, prerequisiteCompleted, postArchiveCreated); err != nil {
		return err
	}
	if err := validateIssueDispositions(evidence.IssueDispositions, provenance); err != nil {
		return err
	}
	if evidence.ProxyEnvironment != (archivalProxyEnvironment{
		GOPROXY: "https://proxy.golang.org", GOSUMDB: "sum.golang.org", GOWORK: "off", GOVCS: "*:off", FreshCaches: true, SeparateCache: true,
	}) {
		return fmt.Errorf("archival public-Proxy environment is not exclusive and fresh")
	}
	if err := validateArchivalProxyModules(root, evidence.ProxyModules, provenance); err != nil {
		return err
	}
	if err := validateArchivalAdapterTuple(evidence.AdapterTuple, provenance); err != nil {
		return err
	}
	want, err := archivalEvidenceDigest(evidence)
	if err != nil {
		return err
	}
	if evidence.ReportDigest != want {
		return fmt.Errorf("archival evidence digest mismatch: got %q want %q", evidence.ReportDigest, want)
	}
	return nil
}

func validateAcceptanceReference(reference archivalAcceptanceReference, subject archivalSubject) error {
	if reference.RunID <= 0 || reference.RunAttempt != 1 || reference.ArtifactID <= 0 || reference.RunURL != fmt.Sprintf("%s/actions/runs/%d", subject.Repository, reference.RunID) || reference.ArtifactName != "migration-acceptance-"+subject.Commit || !reference.Complete || reference.Commit != subject.Commit || reference.Tree != subject.Tree {
		return fmt.Errorf("report identity is incomplete or does not match the archival subject")
	}
	createdAt, err := time.Parse(time.RFC3339, reference.CreatedAt)
	if err != nil {
		return fmt.Errorf("created_at is invalid")
	}
	completedAt, err := time.Parse(time.RFC3339, reference.CompletedAt)
	if err != nil {
		return fmt.Errorf("completed_at is invalid")
	}
	if !completedAt.After(createdAt) {
		return fmt.Errorf("report completion does not follow its creation")
	}
	if !isSHA256(reference.ArtifactSHA256) || !isSHA256(reference.ReportFileSHA256) || !isPrefixedSHA256(reference.ReportDigest) {
		return fmt.Errorf("report digests are incomplete")
	}
	if len(reference.Categories) != len(archivalCategoryIDs) {
		return fmt.Errorf("report does not contain every mandatory category")
	}
	for index, category := range reference.Categories {
		if category.ID != archivalCategoryIDs[index] || category.Status != "complete" {
			return fmt.Errorf("report category %d is incomplete or out of order", index)
		}
	}
	return nil
}

func validateSuccessorState(state archivalRepositoryState, subject archivalSubject) error {
	if state.RepositoryID <= 0 || state.FullName != "ronhuafeng/llm-go" || state.URL != subject.Repository || state.Archived || state.DefaultBranch != "main" || state.HeadCommit != subject.Commit || state.Description == "" || state.Homepage != "" || !state.IssuesEnabled || !state.ActionsEnabled || !state.PrivateVulnerabilityReporting {
		return fmt.Errorf("successor repository is not the sole active source, tracker, automation owner, and security intake")
	}
	return nil
}

func validateLegacyRepositoryStates(root string, states []archivalLegacyRepository, provenance provenanceManifest, prerequisiteCompleted, postArchiveCreated time.Time) error {
	if len(states) != len(provenance.Imports) {
		return fmt.Errorf("archival evidence has %d legacy repositories, want %d", len(states), len(provenance.Imports))
	}
	imports := append([]provenanceImport(nil), provenance.Imports...)
	sort.Slice(imports, func(i, j int) bool { return imports[i].ID < imports[j].ID })
	for index, imported := range imports {
		state := states[index]
		legacyModule, err := legacyModulePath(root, imported)
		if err != nil {
			return err
		}
		wantName := strings.TrimPrefix(imported.Source.Repository, "https://github.com/")
		archiveObservedAt, timeErr := time.Parse(time.RFC3339, state.ArchiveObservedAt)
		wantDescription := fmt.Sprintf("Archived: use %s; legacy %s is immutable", imported.Destination.Module, imported.Source.Tag)
		if state.ID != imported.ID || state.RepositoryID != archivalRepositoryIDs[imported.ID] || state.FullName != wantName || state.URL != imported.Source.Repository || state.LegacyModule != legacyModule || state.ReplacementModule != imported.Destination.Module || state.FinalTag != imported.Source.Tag || state.FinalCommit != imported.Source.Commit || !state.Archived || timeErr != nil || !archiveObservedAt.After(prerequisiteCompleted) || !archiveObservedAt.Before(postArchiveCreated) || state.DefaultBranch != "main" || state.HeadCommit != state.FinalCommit || state.Description != wantDescription || state.OpenIssues != 0 || state.OpenPullRequests != 0 || state.ActionsEnabled || state.LiveRuns != 0 || !state.PreArchivePVRDisabled || state.PostArchivePVRStatus != "ineligible_archived_http_422" || state.Homepage != "https://github.com/ronhuafeng/llm-go" {
			return fmt.Errorf("legacy repository %s archival state is incomplete", imported.ID)
		}
		workflowOutput, err := gitOutput(root, "ls-tree", "-r", "--name-only", imported.Source.Commit, "--", ".github/workflows")
		if err != nil {
			return err
		}
		wantWorkflows := strings.Fields(workflowOutput)
		sort.Strings(wantWorkflows)
		if len(state.TrackedWorkflows) != len(wantWorkflows) || len(state.TrackedWorkflows) == 0 {
			return fmt.Errorf("legacy repository %s reports an incomplete tracked workflow set", imported.ID)
		}
		lastPath := ""
		for workflowIndex, workflow := range state.TrackedWorkflows {
			if workflow.ID <= 0 || workflow.Path != wantWorkflows[workflowIndex] || workflow.Path <= lastPath || workflow.State != "disabled_manually" {
				return fmt.Errorf("legacy repository %s contains invalid workflow evidence", imported.ID)
			}
			lastPath = workflow.Path
		}
	}
	return nil
}

func validateIssueDispositions(dispositions []archivalIssueDisposition, provenance provenanceManifest) error {
	if len(dispositions) != 2 {
		return fmt.Errorf("archival evidence must account for exactly two unfinished legacy issues")
	}
	repositories := map[string]string{}
	for _, imported := range provenance.Imports {
		repositories[imported.ID] = imported.Source.Repository
	}
	if dispositions[0].Outcome != "transferred" || dispositions[0].SourceURL != repositories["llmkit"]+"/issues/12" || dispositions[0].TargetURL != "https://github.com/ronhuafeng/llm-go/issues/41" || dispositions[0].Reason != "" {
		return fmt.Errorf("transferred legacy issue evidence is incomplete")
	}
	if dispositions[1].Outcome != "closed" || dispositions[1].SourceURL != repositories["codex-adapter"]+"/issues/11" || dispositions[1].TargetURL != "" || dispositions[1].Reason != "not_planned_superseded" {
		return fmt.Errorf("closed legacy roadmap evidence is incomplete")
	}
	return nil
}

func validateArchivalProxyModules(root string, modules []archivalProxyModule, provenance provenanceManifest) error {
	if len(modules) != len(provenance.Imports)*2 {
		return fmt.Errorf("archival evidence has %d public-Proxy modules, want %d", len(modules), len(provenance.Imports)*2)
	}
	want := map[string]struct {
		version string
		url     string
		subdir  string
		hash    string
		ref     string
	}{}
	for _, imported := range provenance.Imports {
		legacyModule, err := legacyModulePath(root, imported)
		if err != nil {
			return err
		}
		want[legacyModule] = struct{ version, url, subdir, hash, ref string }{imported.Source.Tag, imported.Source.Repository, "", imported.Source.Commit, "refs/tags/" + imported.Source.Tag}
		destinationVersion, err := migrationVersion(imported.Destination.FirstTag)
		if err != nil {
			return err
		}
		tagCommit, err := resolveCommit(root, imported.Destination.FirstTag)
		if err != nil {
			return err
		}
		want[imported.Destination.Module] = struct{ version, url, subdir, hash, ref string }{version: destinationVersion, url: "https://github.com/ronhuafeng/llm-go", subdir: imported.Destination.Directory, hash: tagCommit, ref: "refs/tags/" + imported.Destination.FirstTag}
	}
	lastPath := ""
	for _, module := range modules {
		expected, ok := want[module.Path]
		if !ok || module.Path <= lastPath || module.Version != expected.version || module.Sum == "" || module.GoModSum == "" || !isSHA256(module.ZipSHA256) || !isSHA256(module.GoModSHA256) || module.Origin.VCS != "git" || canonicalRepositoryURL(module.Origin.URL) != canonicalRepositoryURL(expected.url) || module.Origin.Subdir != expected.subdir || !objectIDPattern.MatchString(module.Origin.Hash) || (expected.hash != "" && module.Origin.Hash != expected.hash) || module.Origin.Ref != expected.ref {
			return fmt.Errorf("public-Proxy module %s evidence is incomplete", module.Path)
		}
		lastPath = module.Path
	}
	return nil
}

func validateArchivalAdapterTuple(tuple archivalAdapterTuple, provenance provenanceManifest) error {
	versions := map[string]string{}
	for _, imported := range provenance.Imports {
		version, err := migrationVersion(imported.Destination.FirstTag)
		if err != nil {
			return err
		}
		versions[imported.Destination.Module] = version
	}
	if tuple.Caller != (archivalModuleVersion{Module: adapterModulePath, Version: versions[adapterModulePath]}) || len(tuple.Dependencies) != 2 || tuple.Dependencies[0] != (archivalModuleVersion{Module: codexSDKModulePath, Version: versions[codexSDKModulePath]}) || tuple.Dependencies[1] != (archivalModuleVersion{Module: llmkitModulePath, Version: versions[llmkitModulePath]}) || !tuple.NoReplace || !tuple.NoExclude || !tuple.NoWorkspace {
		return fmt.Errorf("archival adapter tuple is not the exact released dependency join")
	}
	consumer := tuple.TypedConsumer
	if consumer.Kind != "typed_three_layer_call" || consumer.TypedValue != "verified" || consumer.ProviderName != "codex" || consumer.EffectiveModel != "proxy-model" || consumer.NeutralInputTokens != 11 || consumer.ExactThreadID != "thread-proxy" || consumer.ExactTurnID != "turn-proxy" || !consumer.ExactResultPreserved {
		return fmt.Errorf("archival adapter typed consumer evidence is incomplete")
	}
	return nil
}

func archivalEvidenceDigest(evidence archivalEvidence) (string, error) {
	evidence.ReportDigest = ""
	data, err := json.Marshal(evidence)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func isPrefixedSHA256(value string) bool {
	return strings.HasPrefix(value, "sha256:") && isSHA256(strings.TrimPrefix(value, "sha256:"))
}
