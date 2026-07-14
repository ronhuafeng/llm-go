package repository

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type ReleaseRecoveryContract struct {
	Repository          string
	WorkflowPath        string
	SourceRunID         int64
	ArtifactID          int64
	ArtifactName        string
	Tag                 string
	Commit              string
	DraftReleaseID      int64
	AuthorizationDigest string
}

type ReleaseRecoveryDisposition struct {
	FormatVersion      int    `json:"format_version"`
	State              string `json:"state"`
	ReleaseID          int64  `json:"release_id"`
	AllowMissingAssets bool   `json:"allow_missing_assets"`
	Publish            bool   `json:"publish"`
}

func LLMKitV060RecoveryContract() ReleaseRecoveryContract {
	return ReleaseRecoveryContract{
		Repository:          "ronhuafeng/llm-go",
		WorkflowPath:        ".github/workflows/release.yml",
		SourceRunID:         29342863026,
		ArtifactID:          8314814782,
		ArtifactName:        "release-preflight",
		Tag:                 "llmkit/v0.6.0",
		Commit:              "14f28b0dd4727f079c02ba3139c326ed249bb86a",
		DraftReleaseID:      353873922,
		AuthorizationDigest: "sha256:9b3a05c02f5b464b2e3bb53789368c6254873b982c3012f2c60075ffadaae0cb",
	}
}

type recoveryWorkflowRun struct {
	ID         int64  `json:"id"`
	Event      string `json:"event"`
	HeadSHA    string `json:"head_sha"`
	HeadBranch string `json:"head_branch"`
	Path       string `json:"path"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

type recoveryArtifacts struct {
	Artifacts []struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Expired     bool   `json:"expired"`
		WorkflowRun struct {
			ID         int64  `json:"id"`
			HeadBranch string `json:"head_branch"`
			HeadSHA    string `json:"head_sha"`
		} `json:"workflow_run"`
	} `json:"artifacts"`
}

func ValidateReleaseRecoverySourceFacts(contract ReleaseRecoveryContract, runData, artifactsData, tagObjectData []byte) error {
	if err := validateReleaseRecoveryContract(contract); err != nil {
		return err
	}

	var run recoveryWorkflowRun
	if err := json.Unmarshal(runData, &run); err != nil {
		return fmt.Errorf("decode source run: %w", err)
	}
	if run.ID != contract.SourceRunID || run.Repository.FullName != contract.Repository {
		return fmt.Errorf("source run does not match recovery contract")
	}
	if run.Event != "workflow_dispatch" || run.Path != contract.WorkflowPath {
		return fmt.Errorf("source workflow does not match recovery contract")
	}
	if run.HeadSHA != contract.Commit || run.HeadBranch != "main" {
		return fmt.Errorf("source run commit or branch does not match recovery contract")
	}
	if run.Status != "completed" || run.Conclusion != "failure" {
		return fmt.Errorf("source run is not the completed failed release run")
	}

	var artifacts recoveryArtifacts
	if err := json.Unmarshal(artifactsData, &artifacts); err != nil {
		return fmt.Errorf("decode source artifacts: %w", err)
	}
	matches := 0
	for _, artifact := range artifacts.Artifacts {
		if artifact.ID != contract.ArtifactID {
			continue
		}
		matches++
		if artifact.Name != contract.ArtifactName || artifact.WorkflowRun.ID != contract.SourceRunID || artifact.WorkflowRun.HeadBranch != "main" || artifact.WorkflowRun.HeadSHA != contract.Commit {
			return fmt.Errorf("source artifact does not match recovery contract")
		}
		if artifact.Expired {
			return fmt.Errorf("source artifact is expired")
		}
	}
	if matches != 1 {
		return fmt.Errorf("source artifact id %d matched %d artifacts", contract.ArtifactID, matches)
	}
	return validateRecoveryTagObject(tagObjectData, contract)
}

func ValidateReleaseRecoveryReleaseState(contract ReleaseRecoveryContract, releaseData []byte, allowPublished bool) (ReleaseRecoveryDisposition, error) {
	if err := validateReleaseRecoveryContract(contract); err != nil {
		return ReleaseRecoveryDisposition{}, err
	}
	var release githubRelease
	if err := json.Unmarshal(releaseData, &release); err != nil {
		return ReleaseRecoveryDisposition{}, fmt.Errorf("decode Draft Release: %w", err)
	}
	if release.ID != contract.DraftReleaseID || release.TagName != contract.Tag {
		return ReleaseRecoveryDisposition{}, fmt.Errorf("Release does not match recovery contract")
	}
	if release.Prerelease {
		return ReleaseRecoveryDisposition{}, fmt.Errorf("Release is an unexpected prerelease")
	}
	if release.TargetCommitish != contract.Commit {
		return ReleaseRecoveryDisposition{}, fmt.Errorf("Release targets %s, want %s", release.TargetCommitish, contract.Commit)
	}
	disposition := ReleaseRecoveryDisposition{FormatVersion: 1, State: "draft", ReleaseID: release.ID, AllowMissingAssets: true, Publish: true}
	if !release.Draft {
		if !allowPublished {
			return ReleaseRecoveryDisposition{}, fmt.Errorf("Draft Release is already published")
		}
		if release.Name != contract.Tag+" (verified)" {
			return ReleaseRecoveryDisposition{}, fmt.Errorf("published Release title does not match verified terminal state")
		}
		disposition.State = "published_verified"
		disposition.AllowMissingAssets = false
		disposition.Publish = false
	}
	return disposition, nil
}

func validateReleaseRecoveryContract(contract ReleaseRecoveryContract) error {
	if contract.Repository == "" || contract.WorkflowPath == "" || contract.SourceRunID <= 0 || contract.ArtifactID <= 0 || contract.ArtifactName == "" || contract.Tag == "" || contract.Commit == "" || contract.DraftReleaseID <= 0 || contract.AuthorizationDigest == "" {
		return fmt.Errorf("release recovery contract is incomplete")
	}
	return nil
}

func WriteReleaseRecoveryDisposition(path string, disposition ReleaseRecoveryDisposition) error {
	if disposition.FormatVersion != 1 || disposition.ReleaseID <= 0 {
		return fmt.Errorf("release recovery disposition is invalid")
	}
	if disposition.State == "draft" {
		if !disposition.AllowMissingAssets || !disposition.Publish {
			return fmt.Errorf("Draft recovery disposition is invalid")
		}
	} else if disposition.State == "published_verified" {
		if disposition.AllowMissingAssets || disposition.Publish {
			return fmt.Errorf("published recovery disposition is invalid")
		}
	} else {
		return fmt.Errorf("release recovery disposition state is invalid")
	}
	return writeJSON(path, disposition)
}

const releaseAssetPlanFormatVersion = 1

type ReleaseAssetPlan struct {
	FormatVersion int                    `json:"format_version"`
	Assets        []ReleaseAssetPlanItem `json:"assets"`
}

type ReleaseAssetPlanItem struct {
	Name       string `json:"name"`
	SourcePath string `json:"source_path"`
	SHA256     string `json:"sha256"`
	State      string `json:"state"`
	AssetID    int64  `json:"asset_id,omitempty"`
}

type githubReleaseAsset struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

func BuildReleaseAssetPlan(data []byte, expected map[string]string, requireComplete bool) (ReleaseAssetPlan, error) {
	wantNames := []string{"published-evidence.json", "release-authorization.json", "release-plan.json"}
	if len(expected) != len(wantNames) {
		return ReleaseAssetPlan{}, fmt.Errorf("release asset expectation set is incomplete")
	}
	items := map[string]ReleaseAssetPlanItem{}
	for _, name := range wantNames {
		path, ok := expected[name]
		if !ok || filepath.Base(path) != name {
			return ReleaseAssetPlan{}, fmt.Errorf("release asset expectation for %s is invalid", name)
		}
		digest, err := fileSHA256(path)
		if err != nil {
			return ReleaseAssetPlan{}, fmt.Errorf("hash release asset %s: %w", name, err)
		}
		items[name] = ReleaseAssetPlanItem{Name: name, SourcePath: path, SHA256: digest, State: "missing"}
	}

	var pages [][]githubReleaseAsset
	if err := json.Unmarshal(data, &pages); err != nil {
		return ReleaseAssetPlan{}, fmt.Errorf("decode GitHub Release assets: %w", err)
	}
	seen := map[string]bool{}
	for _, page := range pages {
		for _, asset := range page {
			item, ok := items[asset.Name]
			if !ok {
				return ReleaseAssetPlan{}, fmt.Errorf("unexpected GitHub Release asset %q", asset.Name)
			}
			if seen[asset.Name] {
				return ReleaseAssetPlan{}, fmt.Errorf("duplicate GitHub Release asset %q", asset.Name)
			}
			seen[asset.Name] = true
			if asset.ID <= 0 || asset.State != "uploaded" {
				return ReleaseAssetPlan{}, fmt.Errorf("GitHub Release asset %q is not completely uploaded", asset.Name)
			}
			item.State = "existing"
			item.AssetID = asset.ID
			items[asset.Name] = item
		}
	}

	plan := ReleaseAssetPlan{FormatVersion: releaseAssetPlanFormatVersion}
	for _, name := range wantNames {
		item := items[name]
		if requireComplete && item.State != "existing" {
			return ReleaseAssetPlan{}, fmt.Errorf("release asset set is incomplete; %s is missing", name)
		}
		plan.Assets = append(plan.Assets, item)
	}
	if err := plan.Validate(); err != nil {
		return ReleaseAssetPlan{}, err
	}
	return plan, nil
}

func (plan ReleaseAssetPlan) Validate() error {
	if plan.FormatVersion != releaseAssetPlanFormatVersion || len(plan.Assets) != 3 {
		return fmt.Errorf("release asset plan is incomplete")
	}
	if !sort.SliceIsSorted(plan.Assets, func(i, j int) bool { return plan.Assets[i].Name < plan.Assets[j].Name }) {
		return fmt.Errorf("release asset plan is not sorted")
	}
	wantNames := []string{"published-evidence.json", "release-authorization.json", "release-plan.json"}
	for index, asset := range plan.Assets {
		if asset.Name != wantNames[index] || filepath.Base(asset.SourcePath) != asset.Name || !isSHA256(asset.SHA256) {
			return fmt.Errorf("release asset plan item is invalid")
		}
		switch asset.State {
		case "existing":
			if asset.AssetID <= 0 {
				return fmt.Errorf("existing release asset has no id")
			}
		case "missing":
			if asset.AssetID != 0 {
				return fmt.Errorf("missing release asset unexpectedly has an id")
			}
		default:
			return fmt.Errorf("release asset plan has invalid state %q", asset.State)
		}
	}
	return nil
}

func WriteReleaseAssetPlan(path string, plan ReleaseAssetPlan) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	return writeJSON(path, plan)
}

func ReadReleaseAssetPlan(path string) (ReleaseAssetPlan, error) {
	var plan ReleaseAssetPlan
	if err := readStrictJSON(path, &plan); err != nil {
		return ReleaseAssetPlan{}, err
	}
	if err := plan.Validate(); err != nil {
		return ReleaseAssetPlan{}, err
	}
	return plan, nil
}

func VerifyReleaseAssetDownloads(plan ReleaseAssetPlan, directory string) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	for _, asset := range plan.Assets {
		sourceDigest, err := fileSHA256(asset.SourcePath)
		if err != nil {
			return fmt.Errorf("read release asset source %s: %w", asset.Name, err)
		}
		if sourceDigest != asset.SHA256 {
			return fmt.Errorf("release asset source %s hash mismatch", asset.Name)
		}
		if asset.State != "existing" {
			continue
		}
		digest, err := fileSHA256(filepath.Join(directory, asset.Name))
		if err != nil {
			return fmt.Errorf("read downloaded release asset %s: %w", asset.Name, err)
		}
		if digest != asset.SHA256 {
			return fmt.Errorf("downloaded release asset %s hash mismatch", asset.Name)
		}
	}
	return nil
}

func ValidateReleaseRecoveryArtifacts(contract ReleaseRecoveryContract, plan ReleasePlan, authorization ReleaseAuthorization, evidenceDirectory string) error {
	if err := ValidateReleaseAuthorizationFiles(plan, authorization, evidenceDirectory); err != nil {
		return err
	}
	if plan.Subject.Tag != contract.Tag || plan.Subject.Commit != contract.Commit || plan.Subject.ModuleID != "llmkit" || plan.Subject.TargetVersion != "v0.6.0" {
		return fmt.Errorf("release plan does not match recovery contract")
	}
	if authorization.AuthorizationDigest != contract.AuthorizationDigest {
		return fmt.Errorf("release authorization digest does not match recovery contract")
	}
	return nil
}

func validateRecoveryTagObject(data []byte, contract ReleaseRecoveryContract) error {
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	parts := strings.SplitN(normalized, "\n\n", 2)
	if len(parts) != 2 {
		return fmt.Errorf("annotated tag object has no message")
	}
	headers := map[string]string{}
	for _, line := range strings.Split(parts[0], "\n") {
		fields := strings.SplitN(line, " ", 2)
		if len(fields) != 2 {
			return fmt.Errorf("annotated tag object has malformed header")
		}
		switch fields[0] {
		case "object", "type", "tag":
			if _, exists := headers[fields[0]]; exists {
				return fmt.Errorf("annotated tag object repeats %s", fields[0])
			}
			headers[fields[0]] = fields[1]
		}
	}
	if headers["object"] != contract.Commit || headers["type"] != "commit" || headers["tag"] != contract.Tag {
		return fmt.Errorf("annotated tag object does not match recovery tag and commit")
	}
	wantMessage := contract.Tag + "\n\nrelease-authorization: " + contract.AuthorizationDigest
	if strings.TrimSpace(parts[1]) != wantMessage {
		return fmt.Errorf("annotated tag authorization does not match recovery contract")
	}
	return nil
}
