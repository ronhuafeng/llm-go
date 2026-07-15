package repository

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/mod/sumdb/dirhash"
)

func TestReleasePlanDigestRejectsMutation(t *testing.T) {
	plan := validReleasePlan(t)
	if err := plan.Validate(); err != nil {
		t.Fatal(err)
	}
	plan.Subject.Tag = "llmkit/v0.6.1"
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "target version") && !strings.Contains(err.Error(), "digest") {
		t.Fatalf("mutated plan error = %v", err)
	}
}

func TestReleasePlanReadRejectsUnknownFields(t *testing.T) {
	plan := validReleasePlan(t)
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data[:len(data)-1], []byte(`,"unexpected":true}`)...)
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadReleasePlan(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("ReadReleasePlan error = %v", err)
	}
}

func TestPreV1BreakingFragmentRequiresMinor(t *testing.T) {
	root := t.TempDir()
	candidate := module{ID: "llmkit", Dir: "llmkit"}
	writeFile(t, root, "llmkit/.changes/releases/v0.5.1/breaking.json", `{
  "format_version": 1,
  "module": "llmkit",
  "impact": "patch",
  "breaking": true,
  "summary": "Break the contract.",
  "issue": 1,
  "migration": "docs/migration.md"
}`)
	_, _, _, _, err := loadReleaseFragments(root, candidate, "v0.5.1")
	if err == nil || !strings.Contains(err.Error(), "at least a minor") {
		t.Fatalf("loadReleaseFragments error = %v", err)
	}
}

func TestReleaseFragmentsMustBeArchivedAndStrict(t *testing.T) {
	candidate := module{ID: "llmkit", Dir: "llmkit"}
	t.Run("unconsumed", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "llmkit/.changes/10.json", `{}`)
		_, _, _, _, err := loadReleaseFragments(root, candidate, "v0.6.0")
		if err == nil || !strings.Contains(err.Error(), "unconsumed") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("unknown field", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "llmkit/.changes/releases/v0.6.0/10.json", `{
  "format_version":1,"module":"llmkit","impact":"minor","breaking":true,
  "summary":"Move path.","issue":10,"migration":"docs/migration.md","extra":true
}`)
		_, _, _, _, err := loadReleaseFragments(root, candidate, "v0.6.0")
		if err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("deterministic", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "llmkit/.changes/releases/v0.6.0/b.json", `{
  "format_version":1,"module":"llmkit","impact":"patch","breaking":false,
  "summary":"Second.","issue":11,"migration":"none"
}`)
		writeFile(t, root, "llmkit/.changes/releases/v0.6.0/a.json", `{
  "format_version":1,"module":"llmkit","impact":"minor","breaking":true,
  "summary":"First.","issue":10,"migration":"docs/migration.md"
}`)
		fragments, inputs, impact, breaking, err := loadReleaseFragments(root, candidate, "v0.6.0")
		if err != nil {
			t.Fatal(err)
		}
		if impact != "minor" || !breaking || !strings.HasSuffix(fragments[0].Path, "/a.json") || !reflect.DeepEqual(inputs, []string{fragments[0].Path, fragments[1].Path}) {
			t.Fatalf("fragments=%+v inputs=%v impact=%s breaking=%t", fragments, inputs, impact, breaking)
		}
	})
}

func TestVersionImpactAndStability(t *testing.T) {
	tests := []struct {
		previous string
		target   string
		impact   string
		valid    bool
	}{
		{previous: "v0.5.0", target: "v0.5.1", impact: "patch", valid: true},
		{previous: "v0.5.1", target: "v0.6.0", impact: "minor", valid: true},
		{previous: "v0.9.0", target: "v1.0.0", impact: "major", valid: true},
		{previous: "v0.5.0", target: "v0.6.0-rc.1", valid: false},
		{previous: "v0.5.0", target: "v0.6.0+build", valid: false},
	}
	for _, test := range tests {
		err := validateTargetVersion(test.previous, test.target)
		if (err == nil) != test.valid {
			t.Fatalf("validateTargetVersion(%s,%s) = %v", test.previous, test.target, err)
		}
		if test.valid && semverImpact(test.previous, test.target) != test.impact {
			t.Fatalf("impact(%s,%s) = %s", test.previous, test.target, semverImpact(test.previous, test.target))
		}
	}
	if got := nextVersion("v0.5.0", "minor"); got != "v0.6.0" {
		t.Fatalf("next minor = %s", got)
	}
	if got := nextVersion("v1.2.3", "major"); got != "v2.0.0" {
		t.Fatalf("next major = %s", got)
	}
}

func TestReleaseCommitApprovalExpiresWhenMainAdvances(t *testing.T) {
	approved := strings.Repeat("a", 40)
	advanced := strings.Repeat("b", 40)
	if err := validateReleaseCommit(approved, approved, "origin/main", approved); err != nil {
		t.Fatal(err)
	}
	if err := validateReleaseCommit(approved, approved, "origin/main", advanced); err == nil || !strings.Contains(err.Error(), "no longer current") {
		t.Fatalf("main advance error = %v", err)
	}
}

func TestLegacyAPIInventoryMappingIsExact(t *testing.T) {
	legacy := []byte("pkg github.com/ronhuafeng/llmkit-go/llmstep, type Result\npkg github.com/ronhuafeng/llmkit-go/settle, func Run()\n")
	want := []byte("pkg github.com/ronhuafeng/llm-go/llmkit/llmstep, type Result\npkg github.com/ronhuafeng/llm-go/llmkit/settle, func Run()\n")
	got := mapAPIInventory(legacy, "github.com/ronhuafeng/llmkit-go", "github.com/ronhuafeng/llm-go/llmkit")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mapped inventory = %q, want %q", got, want)
	}
	mutated := append([]byte(nil), want...)
	mutated = append(mutated, []byte("pkg unexpected, type Added\n")...)
	if _, err := mappedAPIInventoryDigest(legacy, mutated, "github.com/ronhuafeng/llmkit-go", "github.com/ronhuafeng/llm-go/llmkit"); err == nil || !strings.Contains(err.Error(), "not equivalent") {
		t.Fatalf("mismatched inventory error = %v", err)
	}
}

func TestCodexSDKLegacyAPIInventoryMappingIncludesFlattenedRoot(t *testing.T) {
	legacy := []byte("type github.com/ronhuafeng/codexsdk-go/codexsdk.Client struct{}\n" +
		"type github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2.Turn struct{}\n")
	want := []byte("type github.com/ronhuafeng/llm-go/codexsdk.Client struct{}\n" +
		"type github.com/ronhuafeng/llm-go/codexsdk/protocolv2.Turn struct{}\n")
	got := mapAPIInventory(legacy, "github.com/ronhuafeng/codexsdk-go/codexsdk", "github.com/ronhuafeng/llm-go/codexsdk")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mapped SDK inventory = %q, want %q", got, want)
	}
}

func TestReleaseWorkflowWiringSmoke(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	for _, required := range []string{
		"options: [llmkit, codexsdk]",
		"options: [v0.6.0]",
		"permissions:\n  contents: read",
		"if: github.ref != 'refs/heads/main'",
		"release dispatch must originate from refs/heads/main",
		"environment:\n      name: production-release\n    permissions:\n      contents: read",
		"RELEASE_DEPLOY_KEY: ${{ secrets.RELEASE_DEPLOY_KEY }}",
		"ssh-key: ${{ secrets.RELEASE_DEPLOY_KEY }}",
		"ssh-strict: true",
		"persist-credentials: true",
		"git remote set-url origin \"git@github.com:${GITHUB_REPOSITORY}.git\"",
		"test \"$(git remote get-url origin)\" = \"git@github.com:${GITHUB_REPOSITORY}.git\"",
		"\"$RUNNER_TEMP/repoctl\" authorize-tag",
		"-authorization \"$AUTHORIZATION\" -evidence-dir \"$EVIDENCE_DIR\"",
		"-digest \"$DIGEST\" -commit \"$COMMIT\" -tag \"$TAG\" -main refs/remotes/origin/main",
		"finalize-release -plan",
		".authorization_digest",
		"release-authorization.json",
		"module_dir=$(jq -r '.subject.module_dir' \"$RUNNER_TEMP/release-plan.json\")",
		"go-version-file: ${{ steps.plan.outputs.module_dir }}/go.mod",
		"git fetch --no-tags origin \"+refs/heads/main:refs/remotes/origin/main\"",
		"git ls-remote origin refs/heads/main",
		"git tag -a \"$TAG\" \"$COMMIT\"",
		"git push --atomic --force-with-lease=\"refs/tags/$TAG:\" origin \"refs/tags/$TAG\"",
		"--verify-tag --target \"$COMMIT\" --draft",
		"needs: [preflight, draft-release]",
		"needs: [preflight, post-tag]",
		"published-evidence.json",
		"gh release edit \"$TAG\"",
		"gh api --paginate --slurp \"repos/$GITHUB_REPOSITORY/releases?per_page=100\"",
		"select-draft-release -input \"$releases_json\" -tag \"$TAG\" -target \"$COMMIT\"",
		"validate-tag-ref -input \"$tag_refs\" -tag \"$TAG\" -commit \"$COMMIT\"",
		"3)",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("release workflow missing wiring %q", required)
		}
	}
	if got := strings.Count(workflow, "contents: write"); got != 2 {
		t.Errorf("contents: write occurrences = %d, want only Draft Release and verified Release jobs", got)
	}
	if got := strings.Count(workflow, "timeout-minutes:"); got != 5 {
		t.Errorf("release job timeouts = %d, want all five jobs bounded", got)
	}
	if got := strings.Count(workflow, "name: production-release"); got != 1 {
		t.Errorf("protected environment occurrences = %d, want tag job only", got)
	}
	if got := strings.Count(workflow, "git fetch --no-tags origin \"+refs/heads/main:refs/remotes/origin/main\""); got != 2 {
		t.Errorf("fresh main fetches in tag step = %d, want one before and one after authorization", got)
	}
	buildIndex := strings.Index(workflow, "Build release tool before the final main observation")
	authorizeIndex := strings.Index(workflow, "Reauthorize against fresh main and create exactly one tag")
	if buildIndex < 0 || authorizeIndex <= buildIndex {
		t.Error("tag job must compile tooling before its final main observations")
	}
	if strings.Contains(workflow, "$COMMIT:refs/heads/main") {
		t.Error("tag transaction must not attempt to rewrite the protected main branch")
	}
	draftIndex := strings.Index(workflow, "  draft-release:")
	postIndex := strings.Index(workflow, "  post-tag:")
	publishIndex := strings.Index(workflow, "  publish-release:")
	if draftIndex < 0 || postIndex <= draftIndex || publishIndex <= postIndex {
		t.Errorf("release jobs are not ordered Draft -> post-tag -> publish")
	}
	if strings.Contains(workflow, "inputs.proxy") || strings.Contains(workflow, "inputs.sumdb") || strings.Contains(workflow, "proxy,direct") {
		t.Error("release workflow exposes or weakens the exclusive public proxy policy")
	}
	if strings.Contains(workflow, "options: [llmkit, codexsdk, codex-adapter]") || strings.Contains(workflow, "go-version-file: llmkit/go.mod") {
		t.Error("release workflow must authorize only the first toolkit and SDK tags and derive the selected module directory from the typed plan")
	}
	if strings.Contains(workflow, "/releases/tags/") {
		t.Error("Draft lookup must use the authenticated releases list because get-by-tag hides Draft Releases")
	}
	if got := strings.Count(workflow, "secrets.RELEASE_DEPLOY_KEY"); got != 2 {
		t.Errorf("release Deploy Key secret references = %d, want only presence validation and checkout SSH auth", got)
	}
	for _, forbidden := range []string{
		"secrets.GH_PAT",
		"secrets.PAT",
		"personal_access_token",
		"persist-credentials: false",
		"ssh-strict: false",
		"git remote set-url origin https://",
	} {
		if strings.Contains(workflow, forbidden) {
			t.Errorf("release workflow contains forbidden release credential pattern %q", forbidden)
		}
	}
}

func TestReleaseWorkflowBoundsDraftVisibilityRetryAfterCreation(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	create := `gh release create "$TAG"`
	retry := `for attempt in $(seq 1 "$draft_visibility_attempts"); do`
	createIndex := strings.Index(workflow, create)
	retryIndex := strings.Index(workflow, retry)
	if createIndex < 0 || retryIndex <= createIndex {
		t.Fatal("Draft visibility retry must run only after successful Draft creation")
	}
	if got := strings.Count(workflow, create); got != 1 {
		t.Fatalf("Draft creation occurrences = %d, want exactly one", got)
	}
	for _, required := range []string{
		"draft_visibility_attempts=6",
		"draft_visibility_delay_seconds=2",
	} {
		settingIndex := strings.Index(workflow, required)
		if settingIndex <= createIndex || settingIndex >= retryIndex {
			t.Errorf("Draft visibility retry setting %q must be initialized between creation and retry", required)
		}
	}
	retryEndOffset := strings.Index(workflow[retryIndex:], "              done")
	if retryEndOffset < 0 {
		t.Fatal("Draft visibility retry must have a bounded loop terminator")
	}
	retryBlock := workflow[retryIndex : retryIndex+retryEndOffset]
	for _, required := range []string{
		"list_releases",
		`"$REPOCTL" select-draft-release -input "$releases_json" -tag "$TAG" -target "$COMMIT"`,
		`case "$lookup_exit" in`,
		"0)",
		"3)",
		`if [ "$attempt" -eq "$draft_visibility_attempts" ]; then`,
		`exit "$lookup_exit"`,
		`sleep "$draft_visibility_delay_seconds"`,
	} {
		if !strings.Contains(retryBlock, required) {
			t.Errorf("Draft visibility retry missing %q", required)
		}
	}
	if strings.Contains(retryBlock, "gh release create") {
		t.Error("Draft visibility retry must never attempt a second Draft creation")
	}
}

func TestPublishedDownloadBindsArtifactAndOriginToPlan(t *testing.T) {
	plan := validReleasePlan(t)
	root := t.TempDir()
	zipPath := filepath.Join(root, "module.zip")
	modPath := filepath.Join(root, "module.mod")
	if err := os.WriteFile(zipPath, []byte("zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modPath, []byte("module example.com/kit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	download := moduleDownload{
		Path: plan.Subject.ModulePath, Version: plan.Subject.TargetVersion, Sum: "h1:canonical", GoModSum: "h1:gomod",
		Zip: zipPath, GoMod: modPath,
		Origin: &ModuleOrigin{VCS: "git", URL: plan.Subject.RepositoryURL, Hash: plan.Subject.Commit, Ref: "refs/tags/" + plan.Subject.Tag},
	}
	var err error
	download.zipSHA256, err = fileSHA256(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	download.goModSHA256, err = fileSHA256(modPath)
	if err != nil {
		t.Fatal(err)
	}
	download.contentSum = download.Sum
	plan.ArchiveSum = download.Sum
	plan.Inputs[0].SHA256 = download.goModSHA256
	plan.PlanDigest, err = releasePlanDigest(plan)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := validateModuleDownload(download, plan)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.ZipSHA256 == "" || resolution.GoModSHA256 == "" || resolution.Sum == "" || resolution.GoModSum == "" {
		t.Fatalf("resolution = %+v", resolution)
	}
	bad := download
	bad.Origin = &ModuleOrigin{VCS: "git", Hash: strings.Repeat("f", 40), Ref: "refs/tags/" + plan.Subject.Tag}
	if _, err := validateModuleDownload(bad, plan); err == nil || !strings.Contains(err.Error(), "origin") {
		t.Fatalf("wrong origin error = %v", err)
	}
	bad = download
	bad.GoModSum = ""
	if _, err := validateModuleDownload(bad, plan); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("missing checksum error = %v", err)
	}
	bad = download
	bad.zipSHA256 = strings.Repeat("1", 64)
	if resolution, err := validateModuleDownload(bad, plan); err != nil || resolution.ZipSHA256 != bad.zipSHA256 {
		t.Fatalf("raw zip SHA must remain observational: resolution=%+v err=%v", resolution, err)
	}
	bad = download
	bad.contentSum = "h1:different"
	if _, err := validateModuleDownload(bad, plan); err == nil || !strings.Contains(err.Error(), "canonical") {
		t.Fatalf("wrong canonical content error = %v", err)
	}
	bad = download
	bad.goModSHA256 = strings.Repeat("2", 64)
	if _, err := validateModuleDownload(bad, plan); err == nil || !strings.Contains(err.Error(), "planned go.mod") {
		t.Fatalf("wrong go.mod digest error = %v", err)
	}
}

func TestReleaseArchiveDigestBindsTargetVersion(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/kit\n\ngo 1.23.0\n")
	writeFile(t, root, "kit.go", "package kit\n")
	candidate := module{ID: "llmkit", Dir: "llmkit", path: "example.com/kit"}
	registered := registry{Modules: []module{candidate}}
	identityA, err := moduleArchiveIdentityForVersion(root, candidate, registered, "v0.6.0")
	if err != nil {
		t.Fatal(err)
	}
	identityB, err := moduleArchiveIdentityForVersion(root, candidate, registered, "v0.6.1")
	if err != nil {
		t.Fatal(err)
	}
	if identityA.Sum == identityB.Sum {
		t.Fatal("module zip digest must bind the target version in archive paths")
	}
}

func TestCanonicalZipSumIgnoresCompressionButDetectsContent(t *testing.T) {
	root := t.TempDir()
	stored := filepath.Join(root, "stored.zip")
	deflated := filepath.Join(root, "deflated.zip")
	changed := filepath.Join(root, "changed.zip")
	writeModuleZip(t, stored, zip.Store, "same")
	writeModuleZip(t, deflated, zip.Deflate, "same")
	writeModuleZip(t, changed, zip.Deflate, "different")
	storedSum, err := dirhash.HashZip(stored, dirhash.Hash1)
	if err != nil {
		t.Fatal(err)
	}
	deflatedSum, err := dirhash.HashZip(deflated, dirhash.Hash1)
	if err != nil {
		t.Fatal(err)
	}
	changedSum, err := dirhash.HashZip(changed, dirhash.Hash1)
	if err != nil {
		t.Fatal(err)
	}
	storedSHA, _ := fileSHA256(stored)
	deflatedSHA, _ := fileSHA256(deflated)
	if storedSum != deflatedSum || storedSHA == deflatedSHA {
		t.Fatalf("same content: sums %s/%s raw %s/%s", storedSum, deflatedSum, storedSHA, deflatedSHA)
	}
	if changedSum == storedSum {
		t.Fatal("changed module content retained the canonical sum")
	}
}

func TestProxyPropagationUsesFreshCachePerAttempt(t *testing.T) {
	options := PublishOptions{Proxy: "https://proxy.golang.org", SumDB: "sum.golang.org", RetryInterval: time.Millisecond, CommandTimeout: time.Second}
	var caches []string
	attempt := 0
	command := func(_ context.Context, _ string, environment map[string]string, _ ...string) ([]byte, error) {
		attempt++
		caches = append(caches, environment["GOMODCACHE"])
		if environment["GOPROXY"] != options.Proxy || environment["GOVCS"] != "*:off" || environment["GOWORK"] != "off" || environment["GOSUMDB"] != options.SumDB {
			t.Fatalf("unsafe proxy environment: %+v", environment)
		}
		if attempt == 1 {
			return nil, errors.New("not propagated")
		}
		return []byte(`{"Path":"example.com/kit","Version":"v0.6.0"}`), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := waitForProxy(ctx, "example.com/kit@v0.6.0", options, command); err != nil {
		t.Fatal(err)
	}
	if len(caches) != 2 || caches[0] == caches[1] {
		t.Fatalf("probe caches = %v, want two distinct caches", caches)
	}
	for _, cache := range caches {
		if _, err := os.Stat(cache); !os.IsNotExist(err) {
			t.Fatalf("probe cache %s survived retry: %v", cache, err)
		}
	}
}

func TestArtifactValidationCacheIsRemovedAfterDigestsAreCaptured(t *testing.T) {
	options := PublishOptions{Proxy: "https://proxy.golang.org", SumDB: "sum.golang.org", CommandTimeout: time.Second}
	var temporaryRoot string
	command := func(_ context.Context, directory string, _ map[string]string, _ ...string) ([]byte, error) {
		temporaryRoot = directory
		zipPath := filepath.Join(directory, "cache", "module.zip")
		modPath := filepath.Join(directory, "cache", "module.mod")
		if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
			return nil, err
		}
		writeModuleZip(t, zipPath, zip.Deflate, "content")
		if err := os.WriteFile(modPath, []byte("mod"), 0o444); err != nil {
			return nil, err
		}
		payload := moduleDownload{Path: "example.com/kit", Version: "v0.6.0", Zip: zipPath, GoMod: modPath, Sum: "h1:zip", GoModSum: "h1:mod"}
		return json.Marshal(payload)
	}
	download, err := downloadFromFreshCache(context.Background(), "example.com/kit@v0.6.0", options, command)
	if err != nil {
		t.Fatal(err)
	}
	if download.zipSHA256 == "" || download.goModSHA256 == "" {
		t.Fatalf("download digests = %+v", download)
	}
	if _, err := os.Stat(temporaryRoot); !os.IsNotExist(err) {
		t.Fatalf("validation cache survived: %v", err)
	}
}

func TestArtifactValidationReportsCleanupFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can remove deliberately read-only fixture directories")
	}
	options := PublishOptions{Proxy: "https://proxy.golang.org", SumDB: "sum.golang.org", CommandTimeout: time.Second}
	var lockedDirectory string
	command := func(_ context.Context, directory string, _ map[string]string, _ ...string) ([]byte, error) {
		lockedDirectory = filepath.Join(directory, "locked")
		if err := os.MkdirAll(lockedDirectory, 0o755); err != nil {
			return nil, err
		}
		zipPath := filepath.Join(lockedDirectory, "module.zip")
		modPath := filepath.Join(lockedDirectory, "module.mod")
		writeModuleZip(t, zipPath, zip.Deflate, "content")
		if err := os.WriteFile(modPath, []byte("mod"), 0o444); err != nil {
			return nil, err
		}
		if err := os.Chmod(lockedDirectory, 0o500); err != nil {
			return nil, err
		}
		payload := moduleDownload{Path: "example.com/kit", Version: "v0.6.0", Zip: zipPath, GoMod: modPath, Sum: "h1:zip", GoModSum: "h1:mod"}
		return json.Marshal(payload)
	}
	_, err := downloadFromFreshCache(context.Background(), "example.com/kit@v0.6.0", options, command)
	if err == nil || !strings.Contains(err.Error(), "remove validation cache") {
		t.Fatalf("cleanup error = %v", err)
	}
	if lockedDirectory != "" {
		_ = os.Chmod(lockedDirectory, 0o700)
		_ = os.RemoveAll(filepath.Dir(lockedDirectory))
	}
}

func TestPublishedEnvironmentDisablesWorkspaceVCSAndPrivateBypass(t *testing.T) {
	got := publishedEnvironment("/gopath", "/mod", "/build", PublishOptions{Proxy: "https://proxy.golang.org", SumDB: "sum.golang.org"})
	want := map[string]string{
		"GOWORK": "off", "GOVCS": "*:off", "GOPROXY": "https://proxy.golang.org", "GOSUMDB": "sum.golang.org",
		"GOPRIVATE": "", "GONOPROXY": "", "GONOSUMDB": "", "GOENV": "off", "GOFLAGS": "-mod=readonly -modcacherw",
	}
	for name, value := range want {
		if got[name] != value {
			t.Fatalf("%s = %q, want %q", name, got[name], value)
		}
	}
}

func TestReleaseTracerScopesOnlyFirstToolkitAndSDKTags(t *testing.T) {
	for _, test := range []struct {
		moduleID string
		version  string
		allowed  bool
	}{
		{moduleID: "llmkit", version: "v0.6.0", allowed: true},
		{moduleID: "codexsdk", version: "v0.6.0", allowed: true},
		{moduleID: "codex-adapter", version: "v0.5.0", allowed: false},
		{moduleID: "llmkit", version: "v0.6.1", allowed: false},
		{moduleID: "codexsdk", version: "v0.6.1", allowed: false},
	} {
		err := validateReleaseTracerScope(test.moduleID, test.version)
		if (err == nil) != test.allowed {
			t.Fatalf("validateReleaseTracerScope(%s, %s) = %v, allowed=%t", test.moduleID, test.version, err, test.allowed)
		}
	}
}

func TestCodexSDKPublishedConsumerTemplateRunsAgainstCheckoutPublicSeams(t *testing.T) {
	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	modulePath := "github.com/ronhuafeng/llm-go/codexsdk"
	source, err := publishedConsumerSource("codexsdk", modulePath)
	if err != nil {
		t.Fatal(err)
	}
	consumerRoot := t.TempDir()
	goMod := "module example.test/codexsdk-release-consumer\n\ngo 1.23.0\n\nrequire " + modulePath + " v0.0.0\n\nreplace " + modulePath + " => " + filepath.ToSlash(filepath.Join(repositoryRoot, "codexsdk")) + "\n"
	if err := os.WriteFile(filepath.Join(consumerRoot, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(consumerRoot, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = consumerRoot
	command.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("run Codex SDK release consumer: %v: %s", err, output)
	}
}

func TestSelectDraftReleaseSupportsExactRerunOnly(t *testing.T) {
	tag := "llmkit/v0.6.0"
	commit := "14f28b0dd4727f079c02ba3139c326ed249bb86a"
	exact := []byte(`[[{"id":42,"tag_name":"other/v1.0.0","target_commitish":"main","draft":false},{"id":353873922,"tag_name":"llmkit/v0.6.0","target_commitish":"` + commit + `","draft":true,"prerelease":false,"unknown":"ignored"}],[]]`)
	id, err := SelectDraftRelease(exact, tag, commit)
	if err != nil {
		t.Fatalf("reuse exact Draft Release: %v", err)
	}
	if id != 353873922 {
		t.Fatalf("selected release id = %d, want 353873922", id)
	}
	tests := []struct {
		name string
		json string
		want string
	}{
		{name: "published", json: `[[{"id":42,"tag_name":"llmkit/v0.6.0","target_commitish":"` + commit + `","draft":false}]]`, want: "already published"},
		{name: "prerelease", json: `[[{"id":42,"tag_name":"llmkit/v0.6.0","target_commitish":"` + commit + `","draft":true,"prerelease":true}]]`, want: "unexpected prerelease"},
		{name: "wrong target", json: `[[{"id":42,"tag_name":"llmkit/v0.6.0","target_commitish":"main","draft":true}]]`, want: "targets"},
		{name: "missing id", json: `[[{"id":0,"tag_name":"llmkit/v0.6.0","target_commitish":"` + commit + `","draft":true}]]`, want: "invalid id"},
		{name: "duplicate", json: `[[{"id":42,"tag_name":"llmkit/v0.6.0","target_commitish":"` + commit + `","draft":true}],[{"id":43,"tag_name":"llmkit/v0.6.0","target_commitish":"` + commit + `","draft":true}]]`, want: "multiple"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := SelectDraftRelease([]byte(test.json), tag, commit); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("SelectDraftRelease error = %v, want %q", err, test.want)
			}
		})
	}
	for name, data := range map[string][]byte{
		"empty pages":     []byte(`[]`),
		"only other tags": []byte(`[[{"id":42,"tag_name":"llmkit/v0.6.1","target_commitish":"` + commit + `","draft":true}]]`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := SelectDraftRelease(data, tag, commit); !errors.Is(err, ErrDraftReleaseNotFound) {
				t.Fatalf("SelectDraftRelease error = %v, want ErrDraftReleaseNotFound", err)
			}
		})
	}
	refs := []byte(strings.Repeat("c", 40) + "\trefs/tags/" + tag + "\n" + commit + "\trefs/tags/" + tag + "^{}\n")
	if err := ValidateRemoteTagRefs(refs, tag, commit); err != nil {
		t.Fatalf("validate annotated tag refs: %v", err)
	}
	wrongRefs := []byte(strings.Repeat("c", 40) + "\trefs/tags/" + tag + "\n" + strings.Repeat("b", 40) + "\trefs/tags/" + tag + "^{}\n")
	if err := ValidateRemoteTagRefs(wrongRefs, tag, commit); err == nil || !strings.Contains(err.Error(), "peels") {
		t.Fatalf("wrong peeled commit error = %v", err)
	}
}

func TestReleaseAuthorizationBindsEveryPreflightEvidence(t *testing.T) {
	root := t.TempDir()
	plan := validReleasePlan(t)
	planPath := filepath.Join(root, "release-plan.json")
	if err := WriteReleasePlan(planPath, plan); err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{}
	for _, stage := range []string{"minimum", "current", "race"} {
		path := filepath.Join(root, "evidence-"+stage+".json")
		evidence := Evidence{
			FormatVersion: evidenceFormatVersion,
			Subject:       EvidenceSubject{Kind: "module_source", Commit: plan.Subject.Commit, Tree: plan.Subject.Tree, Module: plan.Subject.ModuleID, Stage: stage},
			Checks:        []EvidenceCheck{{Name: stage + " checks", Status: "passed"}},
		}
		if err := WriteEvidence(path, evidence); err != nil {
			t.Fatal(err)
		}
		paths[stage] = path
	}
	checkoutPath := filepath.Join(root, "evidence-checkout.json")
	if err := WriteEvidence(checkoutPath, Evidence{
		FormatVersion: evidenceFormatVersion,
		Subject:       EvidenceSubject{Kind: "checkout_source", Commit: plan.Subject.Commit, Tree: plan.Subject.Tree},
		Checks:        []EvidenceCheck{{Name: "checkout checks", Status: "passed"}},
	}); err != nil {
		t.Fatal(err)
	}
	authorization, err := BuildReleaseAuthorization(plan, planPath, paths["minimum"], paths["current"], paths["race"], checkoutPath)
	if err != nil {
		t.Fatal(err)
	}
	if authorization.AuthorizationDigest == "" || authorization.Plan.PlanDigest != plan.PlanDigest {
		t.Fatalf("authorization = %+v", authorization)
	}
	if err := ValidateReleaseAuthorizationFiles(plan, authorization, root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths["race"], []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateReleaseAuthorizationFiles(plan, authorization, root); err == nil || !strings.Contains(err.Error(), "race evidence artifact hash mismatch") {
		t.Fatalf("tampered evidence error = %v", err)
	}
}

func validReleasePlan(t *testing.T) ReleasePlan {
	t.Helper()
	plan := ReleasePlan{
		FormatVersion: releasePlanFormatVersion,
		Subject: ReleasePlanSubject{
			Commit: strings.Repeat("a", 40), Tree: strings.Repeat("b", 40), ModuleID: "llmkit", ModuleDir: "llmkit",
			ModulePath: "github.com/ronhuafeng/llm-go/llmkit", RepositoryURL: "https://github.com/ronhuafeng/llm-go", PreviousVersion: "v0.5.0", TargetVersion: "v0.6.0",
			TagPrefix: "llmkit/", Tag: "llmkit/v0.6.0",
		},
		Impact: ReleaseImpact{
			Declared: "minor", Breaking: true, APIInventoryPath: "llmkit/internal/architecture/testdata/handwritten-api.txt",
			APIInventorySHA256: strings.Repeat("c", 64), Baseline: "fixture",
			Fragments: []ReleaseFragment{{Path: "llmkit/.changes/releases/v0.6.0/10.json", Impact: "minor", Breaking: true, Summary: "Move path.", Issue: 10, Migration: "docs/migration/v0.6.0.md"}},
		},
		Operations: []ReleaseOperation{{Order: 1, ModuleID: "llmkit", Tag: "llmkit/v0.6.0"}},
		Inputs:     []ReleaseInput{{Path: "llmkit/go.mod", SHA256: strings.Repeat("d", 64)}},
		ArchiveSum: "h1:canonical",
	}
	var err error
	plan.PlanDigest, err = releasePlanDigest(plan)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func writeModuleZip(t *testing.T, path string, method uint16, content string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	header := &zip.FileHeader{Name: "example.com/kit@v0.6.0/kit.go", Method: method}
	header.SetModTime(time.Unix(0, 0))
	writer, err := archive.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
