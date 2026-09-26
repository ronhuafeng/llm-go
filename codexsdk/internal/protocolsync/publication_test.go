package protocolsync

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type publicationFixture struct {
	t                      *testing.T
	repo, remote, branch   string
	prs                    []publicationPR
	creates, updates       int
	loseCreate, loseUpdate bool
	rejectUpdate           bool
	beforePush             func()
	afterWrite             func()
	actor                  publicationUser
}

func (f *publicationFixture) ref(name string) string {
	f.t.Helper()
	out, err := exec.Command("git", "--git-dir", f.remote, "rev-parse", "refs/heads/"+name).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
func (f *publicationFixture) snapshot() []publicationPR {
	prs := append([]publicationPR(nil), f.prs...)
	for i := range prs {
		prs[i].Head.SHA = f.ref(prs[i].Head.Ref)
		prs[i].Base.SHA = f.ref("main")
	}
	return prs
}
func (f *publicationFixture) Request(method, path string, body, result any) error {
	var value any
	switch {
	case strings.Contains(path, "/activity?"):
		parsed, err := url.Parse(path)
		if err != nil {
			return err
		}
		ref := parsed.Query().Get("ref")
		value = []any{map[string]any{"ref": ref, "after": f.ref(strings.TrimPrefix(ref, "refs/heads/")), "actor": f.actor}}
	case strings.Contains(path, "/check-runs"):
		value = map[string]any{"check_runs": []any{}}
	case method == "GET" && strings.Contains(path, "pulls?"):
		value = f.snapshot()
	case method == "GET" && strings.Contains(path, "/pulls/"):
		if len(f.prs) != 1 {
			return fmt.Errorf("ambiguous fixture PR")
		}
		value = f.snapshot()[0]
		if f.beforePush != nil {
			hook := f.beforePush
			f.beforePush = nil
			defer hook()
		}
	case method == "POST" && strings.HasSuffix(path, "/pulls"):
		args := body.(map[string]string)
		f.creates++
		pr := publicationPR{Number: 7, URL: "https://github.com/owner/repo/pull/7", State: "open", User: publicationUser{ID: 42, Login: "sync[bot]", Type: "Bot"}, Title: args["title"], Body: args["body"]}
		pr.Head.Ref = args["head"]
		pr.Head.Repo.FullName = "owner/repo"
		pr.Base.Ref = args["base"]
		f.prs = append(f.prs, pr)
		value = f.snapshot()[0]
		if f.afterWrite != nil {
			f.afterWrite()
			f.afterWrite = nil
		}
		if f.loseCreate {
			return fmt.Errorf("fixture creation response lost")
		}
	case method == "PATCH" && strings.Contains(path, "/pulls/"):
		f.updates++
		if f.rejectUpdate {
			return fmt.Errorf("fixture update did not take effect")
		}
		f.prs[0].Body = body.(map[string]string)["body"]
		f.prs[0].Title = body.(map[string]string)["title"]
		value = f.snapshot()[0]
		if f.afterWrite != nil {
			f.afterWrite()
			f.afterWrite = nil
		}
		if f.loseUpdate {
			return fmt.Errorf("fixture update response lost")
		}
	default:
		return fmt.Errorf("unexpected API %s %s", method, path)
	}
	if result == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, result)
}
func newPublicationFixture(t *testing.T) (*publicationFixture, PublishRequest, string) {
	t.Helper()
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	gitMust(t, repo, "config", "user.name", "Fixture")
	gitMust(t, repo, "config", "user.email", "fixture@example.com")
	base := strings.TrimSpace(gitMust(t, repo, "rev-parse", "HEAD"))
	remote := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "clone", "--bare", repo, remote).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}
	gitMust(t, repo, "remote", "add", "origin", remote)
	branch := syncBranchName("rust-v0.154.0", newSHA)
	f := &publicationFixture{t: t, repo: repo, remote: remote, branch: branch, actor: publicationUser{ID: 42, Login: "sync[bot]", Type: "Bot"}}
	f.candidate(base, "rust-v0.154.0", newSHA)
	req := PublishRequest{RepoRoot: repo, BaseBranch: "main", Repository: "owner/repo", AppBotID: 42, ExpectedHead: "absent", ExpectedBranch: branch, TargetRef: "rust-v0.154.0", TargetKind: KindStableTag, TargetSHA: newSHA, API: f, Lookuper: fakeLookuper{byPattern: map[string]string{"refs/tags/rust-v0.154.0": newSHA + "\trefs/tags/rust-v0.154.0", "refs/tags/rust-v0.154.0^{}": newSHA + "\trefs/tags/rust-v0.154.0^{}"}}}
	return f, req, base
}
func (f *publicationFixture) candidate(base, ref, sha string) string {
	f.t.Helper()
	gitMust(f.t, f.repo, "checkout", "--detach", base)
	writeFile(f.t, filepath.Join(f.repo, "codexsdk", filepath.FromSlash(defaultBaselineRel), "baseline_metadata.json"), fmt.Sprintf(`{"source_commit":%q,"source_ref_name":%q,"source_ref_kind":"stable_rust_tag"}`, sha, ref))
	runGitInitCommit(f.t, f.repo, "candidate "+ref)
	return strings.TrimSpace(gitMust(f.t, f.repo, "rev-parse", "HEAD"))
}

func TestPublishCreatesAndRecoversLostResponse(t *testing.T) {
	f, req, _ := newPublicationFixture(t)
	f.loseCreate = true
	actual, err := Publish(req)
	if err != nil {
		t.Fatal(err)
	}
	if actual != "https://github.com/owner/repo/pull/7" || f.creates != 1 {
		t.Fatalf("url=%s creates=%d", actual, f.creates)
	}
	// Same prepared bytes, different commit metadata: reuse actual remote head.
	gitMust(t, f.repo, "-c", "user.name=Another timestamp", "commit", "--amend", "--no-edit")
	actual, err = Publish(req)
	if err != nil {
		t.Fatal(err)
	}
	if f.creates != 1 || f.updates != 0 {
		t.Fatal("retry mutated existing publication")
	}
}

func TestPublishUpdatesAfterBaseAdvance(t *testing.T) {
	f, req, base := newPublicationFixture(t)
	if _, err := Publish(req); err != nil {
		t.Fatal(err)
	}
	oldHead := f.ref(f.branch)
	gitMust(t, f.repo, "checkout", "--detach", base)
	writeFile(t, filepath.Join(f.repo, "README.md"), "new main base")
	runGitInitCommit(t, f.repo, "advance main")
	newBase := strings.TrimSpace(gitMust(t, f.repo, "rev-parse", "HEAD"))
	gitMust(t, f.repo, "push", "origin", newBase+":refs/heads/main")
	newHead := f.candidate(newBase, req.TargetRef, req.TargetSHA)
	req.ExpectedHead = oldHead
	f.loseUpdate = true
	if _, err := Publish(req); err != nil {
		t.Fatal(err)
	}
	if f.creates != 1 || f.updates != 1 || f.ref(f.branch) != newHead {
		t.Fatalf("incorrect update: creates=%d updates=%d head=%s", f.creates, f.updates, f.ref(f.branch))
	}
}

func TestPublishLeasePreservesConcurrentHumanCommit(t *testing.T) {
	f, req, base := newPublicationFixture(t)
	if _, err := Publish(req); err != nil {
		t.Fatal(err)
	}
	oldHead := f.ref(f.branch)
	// A different owned candidate on the same base reaches the conditional push.
	head := f.candidate(base, req.TargetRef, req.TargetSHA)
	writeFile(t, filepath.Join(f.repo, "codexsdk", "change.go"), "package codexsdk\n")
	gitMust(t, f.repo, "add", ".")
	gitMust(t, f.repo, "commit", "--amend", "--no-edit")
	candidate := strings.TrimSpace(gitMust(t, f.repo, "rev-parse", "HEAD"))
	gitMust(t, f.repo, "checkout", "--detach", head)
	writeFile(t, filepath.Join(f.repo, "codexsdk", "human.go"), "package codexsdk\n")
	runGitInitCommit(t, f.repo, "human edit")
	human := strings.TrimSpace(gitMust(t, f.repo, "rev-parse", "HEAD"))
	gitMust(t, f.repo, "checkout", "--detach", candidate)
	f.beforePush = func() { gitMust(t, f.repo, "push", "--force", "origin", human+":refs/heads/"+f.branch) }
	req.ExpectedHead = oldHead
	if _, err := Publish(req); err == nil {
		t.Fatal("concurrent human push accepted")
	}
	if f.ref(f.branch) != human {
		t.Fatal("human commit overwritten")
	}
}

func TestPublishRecoversOwnedOrphanWithoutDuplicateBranch(t *testing.T) {
	f, req, _ := newPublicationFixture(t)
	head := strings.TrimSpace(gitMust(t, f.repo, "rev-parse", "HEAD"))
	gitMust(t, f.repo, "push", "origin", head+":refs/heads/"+f.branch)
	// Simulate a process that lost the push response before creating its PR.
	actual, err := Publish(req)
	if err != nil {
		t.Fatal(err)
	}
	if f.creates != 1 || actual == "" || f.ref(f.branch) != head {
		t.Fatal("orphan was not recovered")
	}
}

func TestPublishRebuildsForNewTarget(t *testing.T) {
	f, req, base := newPublicationFixture(t)
	if _, err := Publish(req); err != nil {
		t.Fatal(err)
	}
	req.ExpectedHead = f.ref(f.branch)
	req.TargetRef = "rust-v0.155.0"
	req.TargetSHA = strings.Repeat("3", 40)
	req.Lookuper = fakeLookuper{byPattern: map[string]string{
		"refs/tags/rust-v0.155.0":    req.TargetSHA + "\trefs/tags/rust-v0.155.0",
		"refs/tags/rust-v0.155.0^{}": req.TargetSHA + "\trefs/tags/rust-v0.155.0^{}",
	}}
	head := f.candidate(base, req.TargetRef, req.TargetSHA)
	if _, err := Publish(req); err != nil {
		t.Fatal(err)
	}
	if f.creates != 1 || f.updates != 1 || f.ref(f.branch) != head {
		t.Fatal("new target was not updated in the existing PR")
	}
}

func TestPublishReportsPostWriteBaseMovement(t *testing.T) {
	f, req, base := newPublicationFixture(t)
	candidate := strings.TrimSpace(gitMust(t, f.repo, "rev-parse", "HEAD"))
	gitMust(t, f.repo, "checkout", "--detach", base)
	writeFile(t, filepath.Join(f.repo, "README.md"), "concurrent main")
	runGitInitCommit(t, f.repo, "concurrent main advance")
	otherBase := strings.TrimSpace(gitMust(t, f.repo, "rev-parse", "HEAD"))
	gitMust(t, f.repo, "checkout", "--detach", candidate)
	f.afterWrite = func() { gitMust(t, f.repo, "push", "origin", otherBase+":refs/heads/main") }
	if _, err := Publish(req); err == nil {
		t.Fatal("base movement was reported as current")
	}
	if f.creates != 1 || f.ref(f.branch) != candidate {
		t.Fatal("residual publication was destructively rolled back")
	}
}

func TestPublishCompletesMetadataAfterUpdateDidNotTakeEffect(t *testing.T) {
	f, req, base := newPublicationFixture(t)
	if _, err := Publish(req); err != nil {
		t.Fatal(err)
	}
	req.ExpectedHead = f.ref(f.branch)
	req.TargetRef = "rust-v0.155.0"
	req.TargetSHA = strings.Repeat("3", 40)
	req.Lookuper = fakeLookuper{byPattern: map[string]string{
		"refs/tags/rust-v0.155.0":    req.TargetSHA + "\trefs/tags/rust-v0.155.0",
		"refs/tags/rust-v0.155.0^{}": req.TargetSHA + "\trefs/tags/rust-v0.155.0^{}",
	}}
	head := f.candidate(base, req.TargetRef, req.TargetSHA)
	f.rejectUpdate = true
	if _, err := Publish(req); err == nil {
		t.Fatal("failed metadata update was accepted")
	}
	pendingRequest := PendingRequest{RepoRoot: f.repo, Repository: req.Repository, AppBotID: req.AppBotID, BaseBranch: "main", BaseSHA: base, Target: Target{RefName: req.TargetRef, RefKind: req.TargetKind, PeeledCommitSHA: req.TargetSHA}, API: f}
	observed, err := InspectPending(pendingRequest)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Reusable {
		t.Fatal("partial publication skipped metadata recovery")
	}
	f.rejectUpdate = false
	req.ExpectedHead = observed.Head
	if _, err := Publish(req); err != nil {
		t.Fatal(err)
	}
	observed, err = InspectPending(pendingRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !observed.Reusable || observed.Head != head || f.creates != 1 || f.updates != 2 {
		t.Fatalf("publication not completed: %+v creates=%d updates=%d", observed, f.creates, f.updates)
	}
}

func TestPublishReportsDescriptionChangedDuringCreation(t *testing.T) {
	f, req, _ := newPublicationFixture(t)
	f.afterWrite = func() { f.prs[0].Body += "\nMaintainer investigation." }
	if _, err := Publish(req); err == nil {
		t.Fatal("creation readback accepted concurrently changed description")
	}
	if f.creates != 1 || !strings.Contains(f.prs[0].Body, "Maintainer investigation.") {
		t.Fatal("maintainer note was lost")
	}
}
