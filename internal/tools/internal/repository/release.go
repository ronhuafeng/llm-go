package repository

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
)

const releasePlanFormatVersion = 2

// ReleasePlan is the immutable authorization input for one public-module tag.
// It intentionally contains only deterministic facts from one source commit.
type ReleasePlan struct {
	FormatVersion int                 `json:"format_version"`
	Subject       ReleasePlanSubject  `json:"subject"`
	Impact        ReleaseImpact       `json:"impact"`
	Dependencies  []ReleaseDependency `json:"dependencies"`
	Operations    []ReleaseOperation  `json:"operations"`
	Inputs        []ReleaseInput      `json:"inputs"`
	ArchiveSum    string              `json:"archive_sum"`
	PlanDigest    string              `json:"plan_digest"`
}

type ReleasePlanSubject struct {
	Commit          string `json:"commit"`
	Tree            string `json:"tree"`
	ModuleID        string `json:"module_id"`
	ModuleDir       string `json:"module_dir"`
	ModulePath      string `json:"module_path"`
	RepositoryURL   string `json:"repository_url"`
	PreviousVersion string `json:"previous_version"`
	TargetVersion   string `json:"target_version"`
	TagPrefix       string `json:"tag_prefix"`
	Tag             string `json:"tag"`
}

type ReleaseImpact struct {
	Declared     string                      `json:"declared"`
	Breaking     bool                        `json:"breaking"`
	APIInventory ReleaseAPIInventoryEvidence `json:"api_inventory"`
	Fragments    []ReleaseFragment           `json:"fragments"`
}

type ReleaseAPIInventoryEvidence struct {
	Path              string                       `json:"path"`
	BaselineTag       string                       `json:"baseline_tag"`
	BaselineSHA256    string                       `json:"baseline_sha256"`
	CurrentSHA256     string                       `json:"current_sha256"`
	HandwrittenImpact apiInventoryImpact           `json:"handwritten_impact"`
	MechanicalImpact  apiInventoryImpact           `json:"mechanical_impact"`
	Generated         *ReleaseGeneratedAPIEvidence `json:"generated,omitempty"`
}

type ReleaseGeneratedAPIEvidence struct {
	Path           string             `json:"path"`
	BaselineSHA256 string             `json:"baseline_sha256"`
	CurrentSHA256  string             `json:"current_sha256"`
	Impact         apiInventoryImpact `json:"impact"`
}

type ReleaseFragment struct {
	Path      string `json:"path"`
	Impact    string `json:"impact"`
	Breaking  bool   `json:"breaking"`
	Summary   string `json:"summary"`
	Issue     int    `json:"issue"`
	Migration string `json:"migration"`
}

type ReleaseDependency struct {
	Module  string `json:"module"`
	Version string `json:"version"`
}

type ReleaseOperation struct {
	Order    int    `json:"order"`
	ModuleID string `json:"module_id"`
	Tag      string `json:"tag"`
}

type ReleaseInput struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type changeFragment struct {
	FormatVersion int    `json:"format_version"`
	Module        string `json:"module"`
	Impact        string `json:"impact"`
	Breaking      bool   `json:"breaking"`
	Summary       string `json:"summary"`
	Issue         int    `json:"issue"`
	Migration     string `json:"migration"`
}

type githubRelease struct {
	ID              int64  `json:"id"`
	TagName         string `json:"tag_name"`
	TargetCommitish string `json:"target_commitish"`
	Name            string `json:"name"`
	Draft           bool   `json:"draft"`
	Prerelease      bool   `json:"prerelease"`
}

var ErrDraftReleaseNotFound = errors.New("matching Draft Release not found")

const (
	adapterModulePath  = "github.com/ronhuafeng/llm-go/llmcaller/codex"
	codexSDKModulePath = "github.com/ronhuafeng/llm-go/codexsdk"
	llmkitModulePath   = "github.com/ronhuafeng/llm-go/llmkit"
)

// BuildReleasePlan verifies the source facts that authorize exactly one tag.
func BuildReleasePlan(root, moduleID, targetVersion, requiredCommit, mainRef string) (ReleasePlan, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return ReleasePlan{}, fmt.Errorf("resolve repository root: %w", err)
	}
	if err := verifySourceIdentity(root, nil); err != nil {
		return ReleasePlan{}, fmt.Errorf("release source identity: %w", err)
	}
	if err := Verify(root); err != nil {
		return ReleasePlan{}, err
	}
	registered, err := loadPopulatedRegistry(root)
	if err != nil {
		return ReleasePlan{}, err
	}
	candidate, ok := findModule(registered, moduleID)
	if !ok || !candidate.Published {
		return ReleasePlan{}, fmt.Errorf("module %q is not a registered public module", moduleID)
	}
	commit, err := resolveCommit(root, "HEAD")
	if err != nil {
		return ReleasePlan{}, err
	}
	if mainRef == "" {
		return ReleasePlan{}, fmt.Errorf("main ref is required")
	}
	mainCommit, err := resolveCommit(root, mainRef)
	if err != nil {
		return ReleasePlan{}, fmt.Errorf("resolve main ref %s: %w", mainRef, err)
	}
	if err := validateReleaseCommit(commit, requiredCommit, mainRef, mainCommit); err != nil {
		return ReleasePlan{}, err
	}
	tree, err := resolveTree(root, commit)
	if err != nil {
		return ReleasePlan{}, err
	}
	repositoryURL, err := gitOutput(root, "remote", "get-url", "origin")
	if err != nil {
		return ReleasePlan{}, fmt.Errorf("resolve origin URL: %w", err)
	}
	repositoryURL = canonicalRepositoryURL(strings.TrimSpace(repositoryURL))
	if repositoryURL != "https://github.com/ronhuafeng/llm-go" {
		return ReleasePlan{}, fmt.Errorf("release origin %q is not the canonical repository", repositoryURL)
	}
	previousVersion, err := previousReleaseVersion(root, candidate)
	if err != nil {
		return ReleasePlan{}, err
	}
	tag := candidate.Dir + "/" + targetVersion
	if err := validateTargetVersion(previousVersion, targetVersion); err != nil {
		return ReleasePlan{}, err
	}
	if output, err := gitOutput(root, "tag", "--list", tag); err != nil {
		return ReleasePlan{}, err
	} else if strings.TrimSpace(output) != "" {
		return ReleasePlan{}, fmt.Errorf("tag %s already exists; published identities are immutable", tag)
	}

	fragments, fragmentInputs, declaredImpact, breaking, err := loadReleaseFragments(root, candidate, targetVersion)
	if err != nil {
		return ReleasePlan{}, err
	}
	if got := semverImpact(previousVersion, targetVersion); got != declaredImpact {
		return ReleasePlan{}, fmt.Errorf("target %s is a %s release from %s, but archived fragments require %s", targetVersion, got, previousVersion, declaredImpact)
	}
	if want := nextVersion(previousVersion, declaredImpact); targetVersion != want {
		return ReleasePlan{}, fmt.Errorf("target %s skips the next %s version %s after %s", targetVersion, declaredImpact, want, previousVersion)
	}
	if err := validateReleaseDocumentation(root, candidate, targetVersion, fragments); err != nil {
		return ReleasePlan{}, err
	}

	inventoryPath, err := apiInventoryPath(candidate.ID)
	if err != nil {
		return ReleasePlan{}, err
	}
	moduleRoot := filepath.Join(root, filepath.FromSlash(candidate.Dir))
	recorder := newEvidence(EvidenceSubject{Kind: "release_preflight", Commit: commit, Tree: tree, Module: moduleID})
	runner := commandRunner{directory: moduleRoot, environment: sourceCleanEnvironment()}
	if err := verifyAPISurface(recorder, runner, candidate.ID); err != nil {
		return ReleasePlan{}, fmt.Errorf("verify canonical API inventory: %w", err)
	}
	apiInventory, err := validateAPIInventoryBaseline(root, candidate, inventoryPath, previousVersion, declaredImpact, breaking)
	if err != nil {
		return ReleasePlan{}, err
	}
	archiveIdentity, err := moduleArchiveIdentityForVersion(moduleRoot, candidate, registered, targetVersion)
	if err != nil {
		return ReleasePlan{}, fmt.Errorf("verify module archive: %w", err)
	}

	inputPaths := []string{
		registryFilename,
		filepath.ToSlash(filepath.Join(candidate.Dir, "go.mod")),
		filepath.ToSlash(filepath.Join(candidate.Dir, "go.sum")),
		filepath.ToSlash(filepath.Join(candidate.Dir, "CHANGELOG.md")),
		filepath.ToSlash(filepath.Join(candidate.Dir, inventoryPath)),
	}
	if candidate.ID == "codexsdk" {
		inputPaths = append(inputPaths, filepath.ToSlash(filepath.Join(candidate.Dir, codexSDKGeneratedManifestPath)))
	}
	inputPaths = append(inputPaths, fragmentInputs...)
	for _, fragment := range fragments {
		if fragment.Migration != "none" {
			inputPaths = append(inputPaths, filepath.ToSlash(filepath.Join(candidate.Dir, fragment.Migration)))
		}
	}
	inputs, err := digestInputs(root, inputPaths)
	if err != nil {
		return ReleasePlan{}, err
	}
	inventoryDigest := ""
	for _, input := range inputs {
		if input.Path == filepath.ToSlash(filepath.Join(candidate.Dir, inventoryPath)) {
			inventoryDigest = input.SHA256
		}
	}
	if inventoryDigest == "" {
		return ReleasePlan{}, fmt.Errorf("canonical API inventory digest is missing")
	}
	if inventoryDigest != apiInventory.CurrentSHA256 {
		return ReleasePlan{}, fmt.Errorf("canonical API inventory input digest does not match the module-owned report")
	}

	dependencies := make([]ReleaseDependency, 0, len(candidate.requireVersions))
	for path, version := range candidate.requireVersions {
		if path == "" || version == "" {
			return ReleasePlan{}, fmt.Errorf("module %s has incomplete dependency requirement", candidate.ID)
		}
		dependencies = append(dependencies, ReleaseDependency{Module: path, Version: version})
	}
	sort.Slice(dependencies, func(i, j int) bool { return dependencies[i].Module < dependencies[j].Module })

	plan := ReleasePlan{
		FormatVersion: releasePlanFormatVersion,
		Subject: ReleasePlanSubject{
			Commit: commit, Tree: tree, ModuleID: candidate.ID, ModuleDir: candidate.Dir,
			ModulePath: candidate.path, RepositoryURL: repositoryURL, PreviousVersion: previousVersion, TargetVersion: targetVersion,
			TagPrefix: candidate.Dir + "/", Tag: tag,
		},
		Impact: ReleaseImpact{
			Declared: declaredImpact, Breaking: breaking, APIInventory: apiInventory, Fragments: fragments,
		},
		Dependencies: dependencies,
		Operations:   []ReleaseOperation{{Order: 1, ModuleID: candidate.ID, Tag: tag}},
		Inputs:       inputs,
		ArchiveSum:   archiveIdentity.Sum,
	}
	plan.PlanDigest, err = releasePlanDigest(plan)
	if err != nil {
		return ReleasePlan{}, err
	}
	if err := plan.Validate(); err != nil {
		return ReleasePlan{}, err
	}
	return plan, nil
}

func validateReleaseCommit(checkoutCommit, requiredCommit, mainRef, mainCommit string) error {
	if requiredCommit == "" || checkoutCommit != requiredCommit {
		return fmt.Errorf("checkout commit %s does not match required commit %s", checkoutCommit, requiredCommit)
	}
	if mainCommit != checkoutCommit {
		return fmt.Errorf("required commit %s is no longer current %s at %s", checkoutCommit, mainRef, mainCommit)
	}
	return nil
}

type apiInventoryImpact string

type moduleAPIInventoryReport struct {
	FormatVersion  int                `json:"format_version"`
	BaselineSHA256 string             `json:"baseline_sha256"`
	TargetSHA256   string             `json:"target_sha256"`
	Impact         apiInventoryImpact `json:"impact"`
}

type generatedAPIReleaseReport struct {
	ReleaseImpact apiInventoryImpact `json:"release_impact"`
}

const codexSDKGeneratedManifestPath = "internal/protocolschema/appserver/v2/manifest.json"

const (
	apiInventoryMetadataOnly apiInventoryImpact = "metadata-only"
	apiInventoryAdditive     apiInventoryImpact = "additive"
	apiInventoryBreaking     apiInventoryImpact = "breaking"
)

func validateAPIInventoryBaseline(root string, candidate module, inventoryPath, previousVersion, declaredImpact string, declaredBreaking bool) (ReleaseAPIInventoryEvidence, error) {
	previousTag := candidate.Dir + "/" + previousVersion
	inventoryRepoPath := filepath.ToSlash(filepath.Join(candidate.Dir, inventoryPath))
	previous, err := gitBytes(root, "show", previousTag+":"+inventoryRepoPath)
	if err != nil {
		return ReleaseAPIInventoryEvidence{}, fmt.Errorf("read canonical API inventory from %s: %w", previousTag, err)
	}
	current, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(inventoryRepoPath)))
	if err != nil {
		return ReleaseAPIInventoryEvidence{}, fmt.Errorf("read current canonical API inventory: %w", err)
	}
	report, impact, err := moduleAPIInventoryImpact(root, candidate, inventoryPath, previous, current)
	if err != nil {
		return ReleaseAPIInventoryEvidence{}, err
	}
	evidence := ReleaseAPIInventoryEvidence{
		Path:              inventoryRepoPath,
		BaselineTag:       previousTag,
		BaselineSHA256:    report.BaselineSHA256,
		CurrentSHA256:     report.TargetSHA256,
		HandwrittenImpact: impact,
	}
	if candidate.ID == "codexsdk" {
		generated, generatedImpact, err := codexSDKGeneratedAPIImpact(root, candidate, previousTag)
		if err != nil {
			return ReleaseAPIInventoryEvidence{}, err
		}
		evidence.Generated = &generated
		impact = strongerAPIImpact(impact, generatedImpact)
	}
	if err := validateAPIImpactFloor(previousTag, previousVersion, impact, declaredImpact, declaredBreaking); err != nil {
		return ReleaseAPIInventoryEvidence{}, err
	}
	evidence.MechanicalImpact = impact
	return evidence, nil
}

func moduleAPIInventoryImpact(root string, candidate module, inventoryPath string, previous, current []byte) (moduleAPIInventoryReport, apiInventoryImpact, error) {
	temporary, err := os.CreateTemp("", "llm-go-api-inventory-*.txt")
	if err != nil {
		return moduleAPIInventoryReport{}, "", fmt.Errorf("create API inventory baseline: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(previous); err != nil {
		temporary.Close()
		return moduleAPIInventoryReport{}, "", fmt.Errorf("write API inventory baseline: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return moduleAPIInventoryReport{}, "", fmt.Errorf("close API inventory baseline: %w", err)
	}
	moduleRoot := filepath.Join(root, filepath.FromSlash(candidate.Dir))
	command := exec.Command("go", "run", "./internal/cmd/apiinventoryreport", "-baseline", temporaryPath, "-target", inventoryPath)
	command.Dir = moduleRoot
	command.Env = overriddenEnvironment(sourceCleanEnvironment())
	output, err := command.CombinedOutput()
	if err != nil {
		return moduleAPIInventoryReport{}, "", fmt.Errorf("module-owned API inventory report: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var report moduleAPIInventoryReport
	if err := json.Unmarshal(output, &report); err != nil {
		return moduleAPIInventoryReport{}, "", fmt.Errorf("decode module-owned API inventory report: %w", err)
	}
	if report.FormatVersion != 1 {
		return moduleAPIInventoryReport{}, "", fmt.Errorf("module-owned API inventory report has unsupported format_version %d", report.FormatVersion)
	}
	if report.BaselineSHA256 != sha256Hex(previous) || report.TargetSHA256 != sha256Hex(current) {
		return moduleAPIInventoryReport{}, "", fmt.Errorf("module-owned API inventory report digests do not match its inputs")
	}
	impact, err := parseAPIInventoryImpact(report.Impact)
	if err != nil {
		return moduleAPIInventoryReport{}, "", fmt.Errorf("module-owned API inventory report: %w", err)
	}
	return report, impact, nil
}

func validateAPIImpactFloor(previousTag, previousVersion string, impact apiInventoryImpact, declaredImpact string, declaredBreaking bool) error {
	minimum := minimumReleaseImpact(previousVersion, impact)
	if releaseImpactRank(declaredImpact) < releaseImpactRank(minimum) {
		return fmt.Errorf("canonical API inventory is %s since %s and requires at least a %s release, got %s", impact, previousTag, minimum, declaredImpact)
	}
	if impact == apiInventoryBreaking && !declaredBreaking {
		return fmt.Errorf("canonical API inventory is breaking since %s but no change fragment declares breaking=true", previousTag)
	}
	return nil
}

func codexSDKGeneratedAPIImpact(root string, candidate module, previousTag string) (ReleaseGeneratedAPIEvidence, apiInventoryImpact, error) {
	manifestPath := filepath.ToSlash(filepath.Join(candidate.Dir, codexSDKGeneratedManifestPath))
	previous, err := gitBytes(root, "show", previousTag+":"+manifestPath)
	if err != nil {
		return ReleaseGeneratedAPIEvidence{}, "", fmt.Errorf("read generated API manifest from %s: %w", previousTag, err)
	}
	currentPath := filepath.Join(root, filepath.FromSlash(manifestPath))
	current, err := os.ReadFile(currentPath)
	if err != nil {
		return ReleaseGeneratedAPIEvidence{}, "", fmt.Errorf("read current generated API manifest: %w", err)
	}
	temporary, err := os.CreateTemp("", "llm-go-codexsdk-manifest-*.json")
	if err != nil {
		return ReleaseGeneratedAPIEvidence{}, "", fmt.Errorf("create generated API baseline: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(previous); err != nil {
		temporary.Close()
		return ReleaseGeneratedAPIEvidence{}, "", fmt.Errorf("write generated API baseline: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return ReleaseGeneratedAPIEvidence{}, "", fmt.Errorf("close generated API baseline: %w", err)
	}
	moduleRoot := filepath.Join(root, filepath.FromSlash(candidate.Dir))
	command := exec.Command("python3", "scripts/codexsdk_release_report.py", "--base-manifest", temporaryPath, "--target-manifest", codexSDKGeneratedManifestPath)
	command.Dir = moduleRoot
	environment := sourceCleanEnvironment()
	environment["PYTHONNOUSERSITE"] = "1"
	environment["PYTHONPATH"] = ""
	command.Env = overriddenEnvironment(environment)
	output, runErr := command.CombinedOutput()
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) || exitErr.ExitCode() != 1 {
			return ReleaseGeneratedAPIEvidence{}, "", fmt.Errorf("classify generated API compatibility: %w: %s", runErr, strings.TrimSpace(string(output)))
		}
	}
	var report generatedAPIReleaseReport
	if err := json.Unmarshal(output, &report); err != nil {
		return ReleaseGeneratedAPIEvidence{}, "", fmt.Errorf("decode generated API compatibility report: %w", err)
	}
	impact, err := parseAPIInventoryImpact(report.ReleaseImpact)
	if err != nil {
		return ReleaseGeneratedAPIEvidence{}, "", fmt.Errorf("generated API compatibility report: %w", err)
	}
	return ReleaseGeneratedAPIEvidence{
		Path:           manifestPath,
		BaselineSHA256: sha256Hex(previous),
		CurrentSHA256:  sha256Hex(current),
		Impact:         impact,
	}, impact, nil
}

func sourceCleanEnvironment() map[string]string {
	return map[string]string{
		"GOWORK":                  "off",
		"GOTOOLCHAIN":             "local",
		"PYTHONDONTWRITEBYTECODE": "1",
	}
}

func parseAPIInventoryImpact(impact apiInventoryImpact) (apiInventoryImpact, error) {
	switch impact {
	case apiInventoryMetadataOnly, apiInventoryAdditive, apiInventoryBreaking:
		return impact, nil
	default:
		return "", fmt.Errorf("unknown API inventory impact %q", impact)
	}
}

func strongerAPIImpact(left, right apiInventoryImpact) apiInventoryImpact {
	rank := map[apiInventoryImpact]int{apiInventoryMetadataOnly: 1, apiInventoryAdditive: 2, apiInventoryBreaking: 3}
	if rank[right] > rank[left] {
		return right
	}
	return left
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func minimumReleaseImpact(previousVersion string, impact apiInventoryImpact) string {
	switch impact {
	case apiInventoryAdditive:
		return "minor"
	case apiInventoryBreaking:
		if semver.Major(previousVersion) == "v0" {
			return "minor"
		}
		return "major"
	default:
		return "patch"
	}
}

func releaseImpactRank(impact string) int {
	return map[string]int{"patch": 1, "minor": 2, "major": 3}[impact]
}

func canonicalRepositoryURL(value string) string {
	value = strings.TrimSuffix(value, ".git")
	if value == "git@github.com:ronhuafeng/llm-go" || value == "ssh://git@github.com/ronhuafeng/llm-go" {
		return "https://github.com/ronhuafeng/llm-go"
	}
	return value
}

func previousReleaseVersion(root string, candidate module) (string, error) {
	output, err := gitOutput(root, "tag", "--list", candidate.Dir+"/v*", "--sort=-version:refname")
	if err != nil {
		return "", err
	}
	for _, tag := range strings.Fields(output) {
		version := strings.TrimPrefix(tag, candidate.Dir+"/")
		if isStableVersion(version) {
			return version, nil
		}
	}
	return "", fmt.Errorf("module %s has no published stable tag to use as its API baseline", candidate.ID)
}

func validateTargetVersion(previous, target string) error {
	if !isStableVersion(previous) || !isStableVersion(target) {
		return fmt.Errorf("release versions must be canonical stable SemVer: previous=%q target=%q", previous, target)
	}
	if semver.Compare(target, previous) <= 0 {
		return fmt.Errorf("target version %s must be greater than %s", target, previous)
	}
	return nil
}

func isStableVersion(version string) bool {
	return semver.IsValid(version) && semver.Canonical(version) == version && semver.Prerelease(version) == "" && semver.Build(version) == ""
}

func semverImpact(previous, target string) string {
	previousParts := semverParts(previous)
	targetParts := semverParts(target)
	if targetParts[0] != previousParts[0] {
		return "major"
	}
	if targetParts[1] != previousParts[1] {
		return "minor"
	}
	return "patch"
}

func semverParts(version string) [3]int {
	trimmed := strings.TrimPrefix(semver.Canonical(version), "v")
	trimmed = strings.SplitN(trimmed, "-", 2)[0]
	fields := strings.Split(trimmed, ".")
	var result [3]int
	for index := range result {
		if index < len(fields) {
			result[index], _ = strconv.Atoi(fields[index])
		}
	}
	return result
}

func nextVersion(previous, impact string) string {
	parts := semverParts(previous)
	switch impact {
	case "major":
		parts[0]++
		parts[1], parts[2] = 0, 0
	case "minor":
		parts[1]++
		parts[2] = 0
	case "patch":
		parts[2]++
	default:
		return ""
	}
	return fmt.Sprintf("v%d.%d.%d", parts[0], parts[1], parts[2])
}

func loadReleaseFragments(root string, candidate module, targetVersion string) ([]ReleaseFragment, []string, string, bool, error) {
	changesRoot := filepath.Join(root, filepath.FromSlash(candidate.Dir), ".changes")
	loose, err := filepath.Glob(filepath.Join(changesRoot, "*.json"))
	if err != nil {
		return nil, nil, "", false, err
	}
	if len(loose) != 0 {
		return nil, nil, "", false, fmt.Errorf("unconsumed change fragments remain in %s; archive them in the release commit", filepath.ToSlash(filepath.Join(candidate.Dir, ".changes")))
	}
	releaseRoot := filepath.Join(changesRoot, "releases", targetVersion)
	paths, err := filepath.Glob(filepath.Join(releaseRoot, "*.json"))
	if err != nil {
		return nil, nil, "", false, err
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, nil, "", false, fmt.Errorf("release %s has no archived change fragments", targetVersion)
	}
	declared := "patch"
	breaking := false
	fragments := make([]ReleaseFragment, 0, len(paths))
	inputs := make([]string, 0, len(paths))
	for _, path := range paths {
		var fragment changeFragment
		if err := readStrictJSON(path, &fragment); err != nil {
			return nil, nil, "", false, fmt.Errorf("read change fragment %s: %w", filepath.Base(path), err)
		}
		if fragment.FormatVersion != 1 || fragment.Module != candidate.ID || releaseImpactRank(fragment.Impact) == 0 || strings.TrimSpace(fragment.Summary) == "" || fragment.Issue <= 0 {
			return nil, nil, "", false, fmt.Errorf("change fragment %s is incomplete or invalid", filepath.Base(path))
		}
		if fragment.Breaking && (fragment.Migration == "" || fragment.Migration == "none") {
			return nil, nil, "", false, fmt.Errorf("breaking change fragment %s must name module-local migration guidance", filepath.Base(path))
		}
		if fragment.Breaking && fragment.Impact == "patch" {
			return nil, nil, "", false, fmt.Errorf("breaking pre-v1 change fragment %s must require at least a minor release", filepath.Base(path))
		}
		if releaseImpactRank(fragment.Impact) > releaseImpactRank(declared) {
			declared = fragment.Impact
		}
		breaking = breaking || fragment.Breaking
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return nil, nil, "", false, err
		}
		relative = filepath.ToSlash(relative)
		fragments = append(fragments, ReleaseFragment{
			Path: relative, Impact: fragment.Impact, Breaking: fragment.Breaking, Summary: fragment.Summary,
			Issue: fragment.Issue, Migration: fragment.Migration,
		})
		inputs = append(inputs, relative)
	}
	return fragments, inputs, declared, breaking, nil
}

func validateReleaseDocumentation(root string, candidate module, targetVersion string, fragments []ReleaseFragment) error {
	changelogPath := filepath.Join(root, filepath.FromSlash(candidate.Dir), "CHANGELOG.md")
	changelog, err := os.ReadFile(changelogPath)
	if err != nil {
		return fmt.Errorf("read changelog: %w", err)
	}
	if !bytes.Contains(changelog, []byte("## ["+strings.TrimPrefix(targetVersion, "v")+"]")) {
		return fmt.Errorf("CHANGELOG.md has no release section for %s", targetVersion)
	}
	for _, fragment := range fragments {
		if fragment.Migration == "none" {
			continue
		}
		clean := filepath.ToSlash(filepath.Clean(fragment.Migration))
		if clean != fragment.Migration || filepath.IsAbs(fragment.Migration) || clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("fragment %s has invalid migration path %q", fragment.Path, fragment.Migration)
		}
		path := filepath.Join(root, filepath.FromSlash(candidate.Dir), filepath.FromSlash(clean))
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			return fmt.Errorf("fragment %s migration document %s is unavailable", fragment.Path, fragment.Migration)
		}
	}
	return nil
}

func apiInventoryPath(moduleID string) (string, error) {
	paths := map[string]string{
		"llmkit":        "internal/architecture/testdata/handwritten-api.txt",
		"codexsdk":      "testdata/handwritten-api.txt",
		"codex-adapter": "internal/architecture/testdata/handwritten-api.txt",
	}
	path, ok := paths[moduleID]
	if !ok {
		return "", fmt.Errorf("module %s has no canonical API inventory", moduleID)
	}
	return path, nil
}

func digestInputs(root string, paths []string) ([]ReleaseInput, error) {
	unique := map[string]bool{}
	for _, path := range paths {
		path = filepath.ToSlash(filepath.Clean(path))
		if path == "." || filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, "../") {
			return nil, fmt.Errorf("invalid release input path %q", path)
		}
		unique[path] = true
	}
	paths = paths[:0]
	for path := range unique {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	inputs := make([]ReleaseInput, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return nil, fmt.Errorf("read release input %s: %w", path, err)
		}
		digest := sha256.Sum256(data)
		inputs = append(inputs, ReleaseInput{Path: path, SHA256: hex.EncodeToString(digest[:])})
	}
	return inputs, nil
}

func releasePlanDigest(plan ReleasePlan) (string, error) {
	plan.PlanDigest = ""
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (plan ReleasePlan) Validate() error {
	if plan.FormatVersion != releasePlanFormatVersion {
		return fmt.Errorf("unsupported release plan format_version %d", plan.FormatVersion)
	}
	if plan.Subject.Commit == "" || plan.Subject.Tree == "" || plan.Subject.ModuleID == "" || plan.Subject.ModulePath == "" || plan.Subject.RepositoryURL == "" || plan.Subject.TargetVersion == "" || plan.Subject.Tag == "" {
		return fmt.Errorf("release plan subject is incomplete")
	}
	if plan.Subject.TagPrefix != plan.Subject.ModuleDir+"/" || plan.Subject.Tag != plan.Subject.TagPrefix+plan.Subject.TargetVersion {
		return fmt.Errorf("release plan tag does not match its module directory and target version")
	}
	if canonicalRepositoryURL(plan.Subject.RepositoryURL) != "https://github.com/ronhuafeng/llm-go" {
		return fmt.Errorf("release plan repository URL is not canonical")
	}
	if !isStableVersion(plan.Subject.PreviousVersion) || !isStableVersion(plan.Subject.TargetVersion) {
		return fmt.Errorf("release plan contains invalid versions")
	}
	if plan.Subject.TargetVersion != nextVersion(plan.Subject.PreviousVersion, plan.Impact.Declared) {
		return fmt.Errorf("release plan target does not match its declared impact")
	}
	if plan.Impact.Breaking && plan.Impact.Declared == "patch" {
		return fmt.Errorf("release plan declares a breaking patch release")
	}
	if len(plan.Impact.Fragments) == 0 || plan.Impact.Declared == "" || plan.ArchiveSum == "" {
		return fmt.Errorf("release plan impact or archive evidence is incomplete")
	}
	inventoryPath, err := apiInventoryPath(plan.Subject.ModuleID)
	if err != nil {
		return err
	}
	wantInventoryPath := filepath.ToSlash(filepath.Join(plan.Subject.ModuleDir, inventoryPath))
	inventory := plan.Impact.APIInventory
	if inventory.Path != wantInventoryPath || inventory.BaselineTag != plan.Subject.ModuleDir+"/"+plan.Subject.PreviousVersion || !isSHA256(inventory.BaselineSHA256) || !isSHA256(inventory.CurrentSHA256) {
		return fmt.Errorf("release plan API inventory evidence is incomplete or inconsistent")
	}
	handwrittenImpact, err := parseAPIInventoryImpact(inventory.HandwrittenImpact)
	if err != nil {
		return err
	}
	mechanicalImpact := handwrittenImpact
	if plan.Subject.ModuleID == "codexsdk" {
		if inventory.Generated == nil || inventory.Generated.Path != filepath.ToSlash(filepath.Join(plan.Subject.ModuleDir, codexSDKGeneratedManifestPath)) || !isSHA256(inventory.Generated.BaselineSHA256) || !isSHA256(inventory.Generated.CurrentSHA256) {
			return fmt.Errorf("release plan generated API evidence is incomplete or inconsistent")
		}
		generatedImpact, err := parseAPIInventoryImpact(inventory.Generated.Impact)
		if err != nil {
			return err
		}
		mechanicalImpact = strongerAPIImpact(mechanicalImpact, generatedImpact)
	} else if inventory.Generated != nil {
		return fmt.Errorf("release plan contains generated API evidence for a non-SDK module")
	}
	if inventory.MechanicalImpact != mechanicalImpact {
		return fmt.Errorf("release plan mechanical API impact does not match owner reports")
	}
	if err := validateAPIImpactFloor(inventory.BaselineTag, plan.Subject.PreviousVersion, mechanicalImpact, plan.Impact.Declared, plan.Impact.Breaking); err != nil {
		return err
	}
	if !strings.HasPrefix(plan.ArchiveSum, "h1:") {
		return fmt.Errorf("release plan contains an invalid canonical archive sum")
	}
	if len(plan.Operations) != 1 || plan.Operations[0].Order != 1 || plan.Operations[0].ModuleID != plan.Subject.ModuleID || plan.Operations[0].Tag != plan.Subject.Tag {
		return fmt.Errorf("release plan operations do not authorize exactly its subject tag")
	}
	if !sort.SliceIsSorted(plan.Dependencies, func(i, j int) bool { return plan.Dependencies[i].Module < plan.Dependencies[j].Module }) {
		return fmt.Errorf("release plan dependencies are not sorted")
	}
	lastDependency := ""
	for _, dependency := range plan.Dependencies {
		if dependency.Module == "" || dependency.Module == lastDependency || !isStableVersion(dependency.Version) {
			return fmt.Errorf("release plan contains an invalid or duplicate dependency")
		}
		lastDependency = dependency.Module
	}
	if !sort.SliceIsSorted(plan.Inputs, func(i, j int) bool { return plan.Inputs[i].Path < plan.Inputs[j].Path }) {
		return fmt.Errorf("release plan inputs are not sorted")
	}
	lastInput := ""
	for _, input := range plan.Inputs {
		if input.Path == "" || input.Path == lastInput || !isSHA256(input.SHA256) {
			return fmt.Errorf("release plan contains an invalid or duplicate input")
		}
		lastInput = input.Path
	}
	if releaseInputSHA256(plan.Inputs, inventory.Path) != inventory.CurrentSHA256 {
		return fmt.Errorf("release plan API inventory evidence does not match its release input")
	}
	if inventory.Generated != nil && releaseInputSHA256(plan.Inputs, inventory.Generated.Path) != inventory.Generated.CurrentSHA256 {
		return fmt.Errorf("release plan generated API evidence does not match its release input")
	}
	if !sort.SliceIsSorted(plan.Impact.Fragments, func(i, j int) bool { return plan.Impact.Fragments[i].Path < plan.Impact.Fragments[j].Path }) {
		return fmt.Errorf("release plan fragments are not sorted")
	}
	want, err := releasePlanDigest(plan)
	if err != nil {
		return err
	}
	if plan.PlanDigest == "" || plan.PlanDigest != want {
		return fmt.Errorf("release plan digest mismatch: got %q want %q", plan.PlanDigest, want)
	}
	return nil
}

func releaseInputSHA256(inputs []ReleaseInput, path string) string {
	for _, input := range inputs {
		if input.Path == path {
			return input.SHA256
		}
	}
	return ""
}

func isSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func ReadReleasePlan(path string) (ReleasePlan, error) {
	var plan ReleasePlan
	if err := readStrictJSON(path, &plan); err != nil {
		return ReleasePlan{}, fmt.Errorf("read release plan: %w", err)
	}
	if err := plan.Validate(); err != nil {
		return ReleasePlan{}, err
	}
	return plan, nil
}

func WriteReleasePlan(path string, plan ReleasePlan) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	return writeJSON(path, plan)
}

// AuthorizeTag validates the evidence-bound authorization and re-derives its
// plan at current main. Any main advance, source mutation, plan mutation,
// evidence mutation, or tag mismatch invalidates the approval.
func AuthorizeTag(root string, approved ReleasePlan, authorization ReleaseAuthorization, evidenceDirectory, expectedDigest, requiredCommit, tag, mainRef string) error {
	if err := approved.Validate(); err != nil {
		return err
	}
	if err := ValidateReleaseAuthorizationFiles(approved, authorization, evidenceDirectory); err != nil {
		return err
	}
	if expectedDigest == "" || authorization.AuthorizationDigest != expectedDigest {
		return fmt.Errorf("approved authorization digest %q does not match required digest %q", authorization.AuthorizationDigest, expectedDigest)
	}
	if approved.Subject.Commit != requiredCommit || approved.Subject.Tag != tag {
		return fmt.Errorf("authorization does not cover commit %s and tag %s", requiredCommit, tag)
	}
	recomputed, err := BuildReleasePlan(root, approved.Subject.ModuleID, approved.Subject.TargetVersion, requiredCommit, mainRef)
	if err != nil {
		return err
	}
	if recomputed.PlanDigest != approved.PlanDigest {
		return fmt.Errorf("authorized plan %s no longer matches current preflight %s", approved.PlanDigest, recomputed.PlanDigest)
	}
	return nil
}

func ReleaseNotes(plan ReleasePlan) (string, error) {
	if err := plan.Validate(); err != nil {
		return "", err
	}
	var notes strings.Builder
	fmt.Fprintf(&notes, "# %s\n\n", plan.Subject.Tag)
	for _, fragment := range plan.Impact.Fragments {
		fmt.Fprintf(&notes, "- %s ([#%d](https://github.com/ronhuafeng/llm-go/issues/%d))\n", fragment.Summary, fragment.Issue, fragment.Issue)
	}
	fmt.Fprintf(&notes, "\nRelease plan: `%s`\n", plan.PlanDigest)
	return notes.String(), nil
}

// SelectDraftRelease finds exactly one release for tag in the authenticated,
// paginated GitHub Releases list. GitHub's get-release-by-tag endpoint hides
// Draft Releases, so it cannot own rerun discovery. The remote peeled tag still
// independently owns immutable commit identity.
func SelectDraftRelease(data []byte, tag, target string) (int64, error) {
	if tag == "" || target == "" {
		return 0, fmt.Errorf("Draft Release tag and target are required")
	}
	var pages [][]githubRelease
	if err := json.Unmarshal(data, &pages); err != nil {
		return 0, fmt.Errorf("decode paginated GitHub Releases: %w", err)
	}
	matches := make([]githubRelease, 0, 1)
	for _, page := range pages {
		for _, release := range page {
			if release.TagName == tag {
				matches = append(matches, release)
			}
		}
	}
	if len(matches) == 0 {
		return 0, fmt.Errorf("%w for tag %s", ErrDraftReleaseNotFound, tag)
	}
	if len(matches) != 1 {
		return 0, fmt.Errorf("multiple GitHub Releases match tag %s", tag)
	}
	release := matches[0]
	if release.ID <= 0 {
		return 0, fmt.Errorf("GitHub Release for %s has invalid id", tag)
	}
	if !release.Draft {
		return 0, fmt.Errorf("GitHub Release for %s is already published; forward-only release automation will not modify it", tag)
	}
	if release.Prerelease {
		return 0, fmt.Errorf("GitHub Release for %s is an unexpected prerelease", tag)
	}
	if release.TargetCommitish != target {
		return 0, fmt.Errorf("GitHub Release for %s targets %s, want %s", tag, release.TargetCommitish, target)
	}
	return release.ID, nil
}

func ValidateRemoteTagRefs(data []byte, tag, commit string) error {
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	wantDirect := "refs/tags/" + tag
	wantPeeled := wantDirect + "^{}"
	seenDirect := false
	seenPeeled := false
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return fmt.Errorf("invalid remote tag-ref evidence")
		}
		switch fields[1] {
		case wantDirect:
			seenDirect = true
		case wantPeeled:
			seenPeeled = true
			if fields[0] != commit {
				return fmt.Errorf("remote tag %s peels to %s, want %s", tag, fields[0], commit)
			}
		default:
			return fmt.Errorf("unexpected remote tag ref %s", fields[1])
		}
	}
	if !seenDirect || !seenPeeled {
		return fmt.Errorf("remote tag %s is missing its annotated or peeled ref", tag)
	}
	return nil
}
