package protocolsync

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

type fixturePublicationAPI struct {
	prs        []publicationPR
	fail       bool
	pushActor  publicationUser
	checks     []map[string]string
	editOnRead bool
}

func (api *fixturePublicationAPI) Request(method, path string, body, result any) error {
	if api.fail {
		return fmt.Errorf("fixture unavailable")
	}
	var value any
	switch {
	case strings.Contains(path, "pulls?"):
		value = api.prs
	case strings.Contains(path, "/pulls/"):
		pr := api.prs[0]
		if api.editOnRead {
			pr.Body += "\nMaintainer note."
		}
		value = pr
	case strings.Contains(path, "/activity?"):
		value = []any{map[string]any{"ref": "refs/heads/" + api.prs[0].Head.Ref, "after": api.prs[0].Head.SHA, "actor": api.pushActor}}
	case strings.Contains(path, "/check-runs"):
		value = map[string]any{"check_runs": api.checks}
	default:
		return fmt.Errorf("unexpected API request %s %s", method, path)
	}
	data, _ := json.Marshal(value)
	return json.Unmarshal(data, result)
}

func pendingFixture(t *testing.T) (PendingRequest, *fixturePublicationAPI) {
	t.Helper()
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	base := strings.TrimSpace(gitMust(t, repo, "rev-parse", "HEAD"))
	metadata := filepath.Join(repo, "codexsdk", filepath.FromSlash(defaultBaselineRel), "baseline_metadata.json")
	writeFile(t, metadata, fmt.Sprintf(`{"source_commit":%q,"source_ref_name":"rust-v0.154.0","source_ref_kind":"stable_rust_tag"}`, newSHA))
	runGitInitCommit(t, repo, "bot candidate")
	head := strings.TrimSpace(gitMust(t, repo, "rev-parse", "HEAD"))
	gitMust(t, repo, "checkout", "--detach", head)
	gitMust(t, repo, "branch", "-f", "main", base)
	pr := publicationPR{Number: 7, URL: "https://github.com/owner/repo/pull/7", State: "open", User: publicationUser{ID: 42, Login: "sync[bot]", Type: "Bot"}, Title: publicationTitle("rust-v0.154.0"), Body: publicationBody("main", "rust-v0.154.0", KindStableTag, newSHA, head)}
	pr.Head.Ref = "codex/sync-upstream-target"
	pr.Head.SHA = head
	pr.Head.Repo.FullName = "owner/repo"
	pr.Base.Ref = "main"
	pr.Base.SHA = base
	api := &fixturePublicationAPI{prs: []publicationPR{pr}, pushActor: pr.User}
	req := PendingRequest{RepoRoot: repo, Repository: "owner/repo", AppBotID: 42, BaseBranch: "main", BaseSHA: base, Remote: repo, Target: Target{RefName: "rust-v0.154.0", RefKind: KindStableTag, PeeledCommitSHA: newSHA}, API: api}
	return req, api
}

func TestInspectPendingUsesNativeHeadAndRejectsHumanChange(t *testing.T) {
	req, api := pendingFixture(t)
	repo := req.RepoRoot
	head := api.prs[0].Head.SHA
	pr := api.prs[0]
	observed, err := InspectPending(req)
	if err != nil {
		t.Fatal(err)
	}
	if !observed.Reusable || observed.Head != head || observed.URL != pr.URL {
		t.Fatalf("pending = %+v", observed)
	}
	writeFile(t, filepath.Join(repo, "codexsdk", "manual.go"), "package codexsdk\n")
	runGitInitCommit(t, repo, "human change")
	api.prs[0].Head.SHA = strings.TrimSpace(gitMust(t, repo, "rev-parse", "HEAD"))
	if _, err := InspectPending(req); err == nil {
		t.Fatal("human head change was adopted")
	}
	api.fail = true
	if _, err := InspectPending(req); err == nil {
		t.Fatal("failed read treated as absence")
	}
}

func TestInspectPendingRequiresConfiguredBotID(t *testing.T) {
	req, _ := pendingFixture(t)
	req.AppBotID = 0
	_, err := InspectPending(req)
	if err == nil || failureCategory(err) != FailurePolicy || !strings.Contains(err.Error(), "PROTOCOL_SYNC_APP_BOT_ID") {
		t.Fatalf("missing bot ID = %v", err)
	}
}

func TestInspectPendingChangesAndOwnership(t *testing.T) {
	for _, kind := range []string{"new target", "new base", "closed", "multiple", "wrong app", "retargeted tag", "recover metadata", "human replacement", "other bot push"} {
		t.Run(kind, func(t *testing.T) {
			req, api := pendingFixture(t)
			wantError := true
			switch kind {
			case "new target":
				req.Target.RefName = "rust-v0.155.0"
				req.Target.PeeledCommitSHA = strings.Repeat("3", 40)
				wantError = false
			case "new base":
				gitMust(t, req.RepoRoot, "checkout", "--detach", req.BaseSHA)
				writeFile(t, filepath.Join(req.RepoRoot, "README.md"), "new accepted base")
				runGitInitCommit(t, req.RepoRoot, "advance accepted base")
				req.BaseSHA = strings.TrimSpace(gitMust(t, req.RepoRoot, "rev-parse", "HEAD"))
				wantError = false
			case "closed":
				api.prs[0].State = "closed"
			case "multiple":
				api.prs = append(api.prs, api.prs[0])
				api.prs[1].Number = 8
			case "wrong app":
				req.AppBotID = 43
			case "retargeted tag":
				req.Target.PeeledCommitSHA = strings.Repeat("3", 40)
			case "recover metadata":
				api.prs[0].Body = publicationBody("main", "rust-v0.154.0", KindStableTag, newSHA, oldSHA)
				api.pushActor = api.prs[0].User
				wantError = false
			case "human replacement":
				api.prs[0].Body = publicationBody("main", "rust-v0.154.0", KindStableTag, newSHA, oldSHA)
				api.pushActor = publicationUser{Login: "maintainer", Type: "User"}
			case "other bot push":
				api.pushActor = publicationUser{ID: 43, Login: "other[bot]", Type: "Bot"}
			}
			observed, err := InspectPending(req)
			if (err != nil) != wantError {
				t.Fatalf("observed=%+v err=%v", observed, err)
			}
			if !wantError && observed.Reusable {
				t.Fatal("changed input inherited old proof")
			}
		})
	}
}

func TestSyncReusesPendingBeforeGeneration(t *testing.T) {
	req, _ := pendingFixture(t)
	gitMust(t, req.RepoRoot, "checkout", "--detach", req.BaseSHA)
	result, err := Sync(SyncRequest{
		RepoRoot: req.RepoRoot, Publication: &req, UpstreamRef: req.Target.RefName,
		Lookuper: fakeLookuper{byPattern: map[string]string{
			"refs/tags/rust-v0.154.0":    newSHA + "\trefs/tags/rust-v0.154.0",
			"refs/tags/rust-v0.154.0^{}": newSHA + "\trefs/tags/rust-v0.154.0^{}",
		}},
		Generate: func(GenerateRequest) (Candidate, error) {
			t.Fatal("pending retry generated again")
			return Candidate{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomePRPending || result.Publication == nil || result.Publication.Number != 7 {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Reason, "no generation, Agent pass, or fresh proof") {
		t.Fatalf("reason=%s", result.Reason)
	}
}

func TestSyncRejectsPendingAfterRemoteBaseAdvances(t *testing.T) {
	req, _ := pendingFixture(t)
	gitMust(t, req.RepoRoot, "checkout", "--detach", req.BaseSHA)
	writeFile(t, filepath.Join(req.RepoRoot, "README.md"), "new main base")
	runGitInitCommit(t, req.RepoRoot, "advance main")
	newBase := strings.TrimSpace(gitMust(t, req.RepoRoot, "rev-parse", "HEAD"))
	gitMust(t, req.RepoRoot, "branch", "-f", "main", newBase)
	gitMust(t, req.RepoRoot, "checkout", "--detach", req.BaseSHA)
	result, err := Sync(SyncRequest{
		RepoRoot: req.RepoRoot, Publication: &req, UpstreamRef: req.Target.RefName,
		Lookuper: fakeLookuper{byPattern: map[string]string{
			"refs/tags/rust-v0.154.0":    newSHA + "\trefs/tags/rust-v0.154.0",
			"refs/tags/rust-v0.154.0^{}": newSHA + "\trefs/tags/rust-v0.154.0^{}",
		}},
		Generate: func(GenerateRequest) (Candidate, error) {
			t.Fatal("stale base generated again")
			return Candidate{}, nil
		},
	})
	if err == nil || failureCategory(err) != FailurePublication || result.Outcome != OutcomeFailed {
		t.Fatalf("stale main was reported as reusable: result=%+v err=%v", result, err)
	}
}

func TestSyncMergedAcceptedTargetSkipsPublication(t *testing.T) {
	repo := initSyncRepo(t, newSHA, "rust-v0.154.0", KindStableTag)
	api := &fixturePublicationAPI{fail: true}
	result, err := Sync(SyncRequest{RepoRoot: repo, UpstreamRef: "rust-v0.154.0", Publication: &PendingRequest{API: api}, Lookuper: fakeLookuper{byPattern: map[string]string{
		"refs/tags/rust-v0.154.0":    newSHA + "\trefs/tags/rust-v0.154.0",
		"refs/tags/rust-v0.154.0^{}": newSHA + "\trefs/tags/rust-v0.154.0^{}",
	}}, Generate: func(GenerateRequest) (Candidate, error) {
		t.Fatal("merged target regenerated")
		return Candidate{}, nil
	}})
	if err != nil || result.Outcome != OutcomeBaselineMatches {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestInspectPendingPreservesHumanDescription(t *testing.T) {
	req, api := pendingFixture(t)
	api.prs[0].Body += "\nMaintainer investigation notes.\n"
	if _, err := InspectPending(req); err == nil {
		t.Fatal("human description would be overwritten by automatic update")
	}
}

func TestInspectPendingClosedCandidateDoesNotRequireLiveBranch(t *testing.T) {
	for _, variant := range []string{"same", "newer", "alias"} {
		t.Run(variant, func(t *testing.T) {
			req, api := pendingFixture(t)
			req.Remote = req.RepoRoot
			api.prs[0].State = "closed"
			api.pushActor = publicationUser{Login: "maintainer", Type: "User"} // branch deleted by maintainer
			if variant != "same" {
				req.Target.RefName = "rust-v0.155.0"
				if variant == "newer" {
					req.Target.PeeledCommitSHA = strings.Repeat("3", 40)
				}
			}
			observed, err := InspectPending(req)
			if variant != "same" {
				if err != nil || observed.Head != "absent" {
					t.Fatalf("old closed candidate blocked new version: %+v %v", observed, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "was closed") {
				t.Fatalf("same closed candidate was not paused: %+v %v", observed, err)
			}
		})
	}
}

func TestInspectPendingHeadChecksDoNotCertifyMergeCandidate(t *testing.T) {
	for _, conclusion := range []string{"success", "failure", ""} {
		t.Run(conclusion, func(t *testing.T) {
			req, api := pendingFixture(t)
			req.ReadChecks = true
			api.prs[0].MergeSHA = strings.Repeat("4", 40)
			api.checks = []map[string]string{
				{"name": "Root source verification", "status": "completed", "conclusion": conclusion},
				{"name": "Codex generated reproducibility / Generated reproducibility", "status": "completed", "conclusion": conclusion},
			}
			observed, err := InspectPending(req)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(observed.Checks, "PR head") || !strings.Contains(observed.Checks, "merge candidate unverified") || strings.Contains(observed.Checks, "awaiting review") {
				t.Fatalf("head checks overstated proof: %s", observed.Checks)
			}
		})
	}
}

func TestInspectPendingRejectsDescriptionEditedDuringRead(t *testing.T) {
	req, api := pendingFixture(t)
	api.editOnRead = true
	if _, err := InspectPending(req); err == nil {
		t.Fatal("concurrent description edit reused stale publication")
	}
}
