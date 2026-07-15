package repository

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestValidateReleaseRecoverySourceFactsBindIncident(t *testing.T) {
	contract := LLMKitV060RecoveryContract()
	run := []byte(`{"id":29342863026,"event":"workflow_dispatch","head_sha":"14f28b0dd4727f079c02ba3139c326ed249bb86a","head_branch":"main","path":".github/workflows/release.yml","status":"completed","conclusion":"failure","repository":{"full_name":"ronhuafeng/llm-go"}}`)
	artifacts := []byte(`{"total_count":1,"artifacts":[{"id":8314814782,"name":"release-preflight","expired":false,"workflow_run":{"id":29342863026,"head_branch":"main","head_sha":"14f28b0dd4727f079c02ba3139c326ed249bb86a"}}]}`)
	tagObject := []byte("object 14f28b0dd4727f079c02ba3139c326ed249bb86a\ntype commit\ntag llmkit/v0.6.0\ntagger github-actions[bot] <41898282+github-actions[bot]@users.noreply.github.com> 0 +0000\n\nllmkit/v0.6.0\n\nrelease-authorization: sha256:9b3a05c02f5b464b2e3bb53789368c6254873b982c3012f2c60075ffadaae0cb\n")

	if err := ValidateReleaseRecoverySourceFacts(contract, run, artifacts, tagObject); err != nil {
		t.Fatalf("validate exact recovery incident: %v", err)
	}

	tests := map[string]struct {
		run       []byte
		artifacts []byte
		tagObject []byte
		want      string
	}{
		"wrong source run":        {run: []byte(strings.Replace(string(run), "29342863026", "29342863027", 1)), artifacts: artifacts, tagObject: tagObject, want: "source run"},
		"wrong workflow":          {run: []byte(strings.Replace(string(run), ".github/workflows/release.yml", ".github/workflows/other.yml", 1)), artifacts: artifacts, tagObject: tagObject, want: "workflow"},
		"wrong commit":            {run: []byte(strings.Replace(string(run), contract.Commit, strings.Repeat("a", 40), 1)), artifacts: artifacts, tagObject: tagObject, want: "commit"},
		"wrong artifact":          {run: run, artifacts: []byte(strings.Replace(string(artifacts), "8314814782", "8314814783", 1)), tagObject: tagObject, want: "artifact"},
		"expired artifact":        {run: run, artifacts: []byte(strings.Replace(string(artifacts), `"expired":false`, `"expired":true`, 1)), tagObject: tagObject, want: "expired"},
		"wrong tag authorization": {run: run, artifacts: artifacts, tagObject: []byte(strings.Replace(string(tagObject), contract.AuthorizationDigest, "sha256:"+strings.Repeat("c", 64), 1)), want: "authorization"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if err := ValidateReleaseRecoverySourceFacts(contract, test.run, test.artifacts, test.tagObject); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateReleaseRecoverySourceFacts error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateAdapterReleaseRecoverySourceFactsBindIncident(t *testing.T) {
	contract := CodexAdapterV050RecoveryContract()
	run := []byte(`{"id":29383675440,"event":"workflow_dispatch","head_sha":"5fd612b358292ee587c558dbd8041c5a75aea0d7","head_branch":"main","path":".github/workflows/release.yml","status":"completed","conclusion":"failure","repository":{"full_name":"ronhuafeng/llm-go"}}`)
	artifacts := []byte(`{"total_count":2,"artifacts":[{"id":8330765998,"name":"published-evidence","expired":false,"workflow_run":{"id":29383675440,"head_branch":"main","head_sha":"5fd612b358292ee587c558dbd8041c5a75aea0d7"}},{"id":8330664749,"name":"release-preflight","expired":false,"workflow_run":{"id":29383675440,"head_branch":"main","head_sha":"5fd612b358292ee587c558dbd8041c5a75aea0d7"}}]}`)
	tagObject := []byte("object 5fd612b358292ee587c558dbd8041c5a75aea0d7\ntype commit\ntag llmcaller/codex/v0.5.0\ntagger github-actions[bot] <41898282+github-actions[bot]@users.noreply.github.com> 0 +0000\n\nllmcaller/codex/v0.5.0\n\nrelease-authorization: sha256:1c838c2d505275a20a54e0040a8049bda4ad3b19329865985201e24928385172\n")
	if err := ValidateReleaseRecoverySourceFacts(contract, run, artifacts, tagObject); err != nil {
		t.Fatalf("validate adapter recovery incident: %v", err)
	}
	if contract.ModuleID != "codex-adapter" || contract.TargetVersion != "v0.5.0" || contract.DraftReleaseID != 354170731 {
		t.Fatalf("adapter recovery contract = %+v", contract)
	}
}

func TestRecoveryPlanIdentityComesFromExactContract(t *testing.T) {
	adapter := CodexAdapterV050RecoveryContract()
	plan := validAdapterReleasePlan(t)
	plan.Subject.Commit = adapter.Commit
	plan.PlanDigest = ""
	var err error
	plan.PlanDigest, err = releasePlanDigest(plan)
	if err != nil {
		t.Fatal(err)
	}
	authorization := ReleaseAuthorization{AuthorizationDigest: adapter.AuthorizationDigest}
	if err := validateReleaseRecoveryPlanIdentity(adapter, plan, authorization); err != nil {
		t.Fatal(err)
	}
	plan.Subject.ModuleID = "llmkit"
	if err := validateReleaseRecoveryPlanIdentity(adapter, plan, authorization); err == nil || !strings.Contains(err.Error(), "contract") {
		t.Fatalf("wrong recovery plan error = %v", err)
	}
}

func TestValidateReleaseRecoveryReleaseStateOwnsDisposition(t *testing.T) {
	contract := LLMKitV060RecoveryContract()
	draft := []byte(`{"id":353873922,"tag_name":"llmkit/v0.6.0","target_commitish":"14f28b0dd4727f079c02ba3139c326ed249bb86a","draft":true,"prerelease":false}`)
	disposition, err := ValidateReleaseRecoveryReleaseState(contract, draft, false)
	if err != nil {
		t.Fatalf("validate exact Draft: %v", err)
	}
	if disposition.State != "draft" || !disposition.AllowMissingAssets || !disposition.Publish {
		t.Fatalf("Draft disposition = %+v", disposition)
	}
	for name, test := range map[string]struct {
		release []byte
		want    string
	}{
		"wrong release id": {release: []byte(strings.Replace(string(draft), "353873922", "353873923", 1)), want: "Release"},
		"wrong target":     {release: []byte(strings.Replace(string(draft), contract.Commit, strings.Repeat("b", 40), 1)), want: "targets"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateReleaseRecoveryReleaseState(contract, test.release, false); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateReleaseRecoveryReleaseState error = %v, want %q", err, test.want)
			}
		})
	}
	published := []byte(`{"id":353873922,"tag_name":"llmkit/v0.6.0","target_commitish":"14f28b0dd4727f079c02ba3139c326ed249bb86a","name":"llmkit/v0.6.0 (verified)","draft":false,"prerelease":false}`)
	publishedDisposition, err := ValidateReleaseRecoveryReleaseState(contract, published, true)
	if err != nil {
		t.Fatalf("accept exact already-published terminal state: %v", err)
	}
	if publishedDisposition.State != "published_verified" || publishedDisposition.AllowMissingAssets || publishedDisposition.Publish {
		t.Fatalf("published disposition = %+v", publishedDisposition)
	}
	if _, err := ValidateReleaseRecoveryReleaseState(contract, published, false); err == nil || !strings.Contains(err.Error(), "already published") {
		t.Fatalf("Draft-only validation error = %v", err)
	}
}

func TestReleaseAssetReconciliationIsForwardOnlyAndContentBound(t *testing.T) {
	root := t.TempDir()
	expected := map[string]string{
		"release-plan.json":          writeRecoveryAsset(t, root, "release-plan.json", "plan"),
		"release-authorization.json": writeRecoveryAsset(t, root, "release-authorization.json", "authorization"),
		"published-evidence.json":    writeRecoveryAsset(t, root, "published-evidence.json", "evidence"),
	}

	partial := []byte(`[[{"id":11,"name":"release-plan.json","state":"uploaded"}],[]]`)
	plan, err := BuildReleaseAssetPlan(partial, expected, false)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, asset := range plan.Assets {
		states[asset.Name] = asset.State
	}
	if want := map[string]string{"release-plan.json": "existing", "release-authorization.json": "missing", "published-evidence.json": "missing"}; !reflect.DeepEqual(states, want) {
		t.Fatalf("asset states = %#v, want %#v", states, want)
	}

	downloads := t.TempDir()
	writeRecoveryAsset(t, downloads, "release-plan.json", "plan")
	if err := VerifyReleaseAssetDownloads(plan, downloads); err != nil {
		t.Fatalf("verify matching existing asset: %v", err)
	}
	writeRecoveryAsset(t, downloads, "release-plan.json", "tampered")
	if err := VerifyReleaseAssetDownloads(plan, downloads); err == nil || !strings.Contains(err.Error(), "hash") {
		t.Fatalf("tampered asset error = %v", err)
	}

	if _, err := BuildReleaseAssetPlan(partial, expected, true); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("incomplete asset plan error = %v", err)
	}
	for name, data := range map[string][]byte{
		"unexpected": []byte(`[[{"id":11,"name":"unexpected.json","state":"uploaded"}]]`),
		"duplicate":  []byte(`[[{"id":11,"name":"release-plan.json","state":"uploaded"}],[{"id":12,"name":"release-plan.json","state":"uploaded"}]]`),
		"bad state":  []byte(`[[{"id":11,"name":"release-plan.json","state":"new"}]]`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := BuildReleaseAssetPlan(data, expected, false); err == nil {
				t.Fatal("invalid release assets unexpectedly accepted")
			}
		})
	}
}

func writeRecoveryAsset(t *testing.T, directory, name, contents string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReleaseRecoveryWorkflowWiringSmoke(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "recover-llmkit-v0.6.0.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	for _, required := range []string{
		"options: ['29342863026']",
		"actions: read",
		"contents: read",
		"SOURCE_ARTIFACT_ID: '8314814782'",
		"TAG: llmkit/v0.6.0",
		"COMMIT: 14f28b0dd4727f079c02ba3139c326ed249bb86a",
		"DRAFT_ID: '353873922'",
		"actions/artifacts/$SOURCE_ARTIFACT_ID/zip",
		"ref: 14f28b0dd4727f079c02ba3139c326ed249bb86a",
		"validate-release-recovery-source",
		"validate-release-recovery-release",
		"-allow-published",
		"plan-release-assets",
		"verify-release-assets",
		"gh api --paginate --slurp \"repos/$GITHUB_REPOSITORY/releases/$DRAFT_ID/assets?per_page=100\"",
		"draft-current-disposition.json",
		"published-disposition.json",
		"asset-plan-published.json\" true",
		"\"$RUNNER_TEMP/repoctl-authorized\" verify-tag",
		"-root \"$AUTHORIZED_SOURCE\"",
		"if: always()",
		"name: llmkit-v0.6.0-release-publish-diagnostics",
		"contents: write",
		"encoded_name=$(jq -rn --arg value \"$name\" '$value | @uri')",
		"https://uploads.github.com/repos/$GITHUB_REPOSITORY/releases/$DRAFT_ID/assets?name=$encoded_name",
		"Authorization: Bearer $GH_TOKEN",
		"--silent --show-error --fail-with-body",
		"--retry 3",
		"releases/$DRAFT_ID",
		"draft=false",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("recovery workflow missing wiring %q", required)
		}
	}
	if got := strings.Count(workflow, "contents: write"); got != 1 {
		t.Errorf("recovery contents:write occurrences = %d, want publish job only", got)
	}
	if got := strings.Count(workflow, "if: always()"); got != 2 {
		t.Errorf("recovery always-diagnostic uploads = %d, want verify and publish jobs", got)
	}
	for _, forbidden := range []string{"RELEASE_DEPLOY_KEY", "ssh-key:", "git tag ", "git push ", "git tag -d", "gh release delete", "--method DELETE", "gh api --method POST --hostname uploads.github.com", "length == 0", "release-plan -root", "finalize-release", "authorize-tag"} {
		if strings.Contains(workflow, forbidden) {
			t.Errorf("recovery workflow contains forbidden mutation/authorization pattern %q", forbidden)
		}
	}
}

func TestAdapterReleaseRecoveryWorkflowWiringSmoke(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "recover-codex-adapter-v0.5.0.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	for _, required := range []string{
		"options: ['29383675440']",
		"SOURCE_ARTIFACT_ID: '8330664749'",
		"TAG: llmcaller/codex/v0.5.0",
		"COMMIT: 5fd612b358292ee587c558dbd8041c5a75aea0d7",
		"AUTHORIZATION_DIGEST: sha256:1c838c2d505275a20a54e0040a8049bda4ad3b19329865985201e24928385172",
		"DRAFT_ID: '354170731'",
		"-contract codex-adapter-v0.5.0",
		"validate-release-recovery-source",
		"validate-release-recovery-release",
		"\"$RUNNER_TEMP/repoctl-recovery\" verify-tag",
		"-root \"$AUTHORIZED_SOURCE\"",
		"-command-timeout 5m",
		"name: codex-adapter-v0.5.0-release-recovery",
		"name: codex-adapter-v0.5.0-release-publish-diagnostics",
		"contents: write",
		"if: always()",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("adapter recovery workflow missing wiring %q", required)
		}
	}
	if got := strings.Count(workflow, "contents: write"); got != 1 {
		t.Errorf("adapter recovery contents:write occurrences = %d, want publish job only", got)
	}
	if got := strings.Count(workflow, "if: always()"); got != 2 {
		t.Errorf("adapter recovery always-diagnostic uploads = %d, want verify and publish jobs", got)
	}
	for _, forbidden := range []string{
		"RELEASE_DEPLOY_KEY", "ssh-key:", "git tag ", "git push ", "git tag -d", "gh release delete", "--method DELETE",
		"release-plan -root", "finalize-release", "authorize-tag", "repoctl-authorized",
	} {
		if strings.Contains(workflow, forbidden) {
			t.Errorf("adapter recovery workflow contains forbidden mutation/authorization pattern %q", forbidden)
		}
	}
}
