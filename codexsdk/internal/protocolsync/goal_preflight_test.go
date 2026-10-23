package protocolsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type preflightAPI struct {
	prs   []publicationPR
	actor publicationUser
	head  string
	fail  string
	calls int
}

func (f *preflightAPI) Request(method, path string, body, result any) error {
	f.calls++
	if method != "GET" || body != nil {
		return errors.New("unexpected mutation")
	}
	if f.fail != "" && strings.Contains(path, f.fail) {
		return errors.New("fixture API failure")
	}
	var value any
	switch {
	case strings.Contains(path, "/pulls?"):
		u, _ := url.Parse(path)
		page, _ := strconv.Atoi(u.Query().Get("page"))
		start := (page - 1) * 100
		if start > len(f.prs) {
			return errors.New("bad page")
		}
		end := start + 100
		if end > len(f.prs) {
			end = len(f.prs)
		}
		value = append([]publicationPR{}, f.prs[start:end]...)
	case strings.Contains(path, "/git/ref/heads/"):
		branch := strings.SplitN(path, "/git/ref/heads/", 2)[1]
		value = map[string]any{"ref": "refs/heads/" + branch, "object": map[string]string{"type": "commit", "sha": f.head}}
	case strings.Contains(path, "/activity?"):
		u, _ := url.Parse(path)
		value = []any{map[string]any{"ref": u.Query().Get("ref"), "after": f.head, "actor": f.actor}}
	default:
		return fmt.Errorf("unexpected read %s", path)
	}
	b, _ := json.Marshal(value)
	return json.Unmarshal(b, result)
}
func preflightFixture(t *testing.T) (GoalPreflightRequest, *preflightAPI) {
	t.Helper()
	root := t.TempDir()
	goalTestGit(t, root, "init", "-q")
	writeFile(t, filepath.Join(root, "codexsdk", defaultBaselineRel, "baseline_metadata.json"), `{"source_commit":"`+oldSHA+`","source_ref_name":"rust-v0.100.0","source_ref_kind":"stable_rust_tag"}`)
	goalTestGit(t, root, "add", ".")
	goalTestGit(t, root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.com", "commit", "-qm", "base")
	base := goalTestGit(t, root, "rev-parse", "HEAD")
	f := &preflightAPI{prs: []publicationPR{}, actor: publicationUser{ID: 42, Login: "sync[bot]", Type: "Bot"}, head: newSHA}
	lookup := fakeLookuper{byPattern: map[string]string{"refs/tags/rust-v*": newSHA + " refs/tags/rust-v0.101.0", "refs/tags/rust-v0.101.0": newSHA + " refs/tags/rust-v0.101.0"}}
	return GoalPreflightRequest{RepoRoot: root, Repository: "owner/repo", BaseBranch: "main", BaseSHA: base, AppBotID: 42, API: f, Lookuper: lookup}, f
}
func preflightPR(req GoalPreflightRequest, f *preflightAPI, n int) publicationPR {
	p := publicationPR{Number: n, URL: fmt.Sprintf("https://github.com/owner/repo/pull/%d", n), State: "open", User: f.actor}
	p.Base.Ref = "main"
	p.Base.SHA = req.BaseSHA
	p.Base.Repo.FullName = req.Repository
	p.Head.Ref = syncBranchName("rust-v0.101.0", newSHA, req.BaseSHA) + "-goal-0123456789abcdef"
	p.Head.SHA = newSHA
	p.Head.Repo.FullName = req.Repository
	return p
}
func TestGoalPreflightNewTarget(t *testing.T) {
	req, f := preflightFixture(t)
	got, err := GoalPreflight(context.Background(), req)
	if err != nil || got.Outcome != "run" || got.Target.RefName != "rust-v0.101.0" || got.Target.PeeledCommitSHA != newSHA || got.Target.TargetExplicit || f.calls != 1 {
		t.Fatalf("%+v %v calls=%d", got, err, f.calls)
	}
	req.UpstreamRef = newSHA
	got, err = GoalPreflight(context.Background(), req)
	if err != nil || got.Outcome != "run" || got.Target.RefKind != KindManualCommit || !got.Target.TargetExplicit {
		t.Fatalf("explicit: %+v %v", got, err)
	}
}
func TestGoalPreflightSameBaselineSkipsWithoutPRReads(t *testing.T) {
	req, f := preflightFixture(t)
	req.UpstreamRef = oldSHA
	got, err := GoalPreflight(context.Background(), req)
	if err != nil || got.Outcome != "skip" || f.calls != 0 {
		t.Fatalf("%+v %v calls=%d", got, err, f.calls)
	}
}
func TestGoalPreflightTrustedPendingNeedsExplicitRecoveryAcrossBaseAndTarget(t *testing.T) {
	req, f := preflightFixture(t)
	p := preflightPR(req, f, 1)
	p.Head.Ref = syncBranchName("rust-v0.100.1", oldSHA, oldSHA) + "-goal-0123456789abcdef"
	p.Head.SHA = oldSHA // PR projection may lag the authoritative ref.
	f.prs = []publicationPR{p}
	got, err := GoalPreflight(context.Background(), req)
	if err != nil || got.Outcome != "needs_recovery" || got.ExistingPRNumber != p.Number || got.ExistingHead != f.head || got.ExistingBranch != p.Head.Ref || got.ExistingPRURL != p.URL || f.calls != 3 {
		t.Fatalf("%+v %v calls=%d", got, err, f.calls)
	}
}
func TestGoalPreflightRejectsUnknownIdentityAndReads(t *testing.T) {
	cases := map[string]func(*GoalPreflightRequest, *preflightAPI){
		"missing bot":    func(r *GoalPreflightRequest, f *preflightAPI) { r.AppBotID = 0 },
		"wrong checkout": func(r *GoalPreflightRequest, f *preflightAPI) { r.BaseSHA = newSHA },
		"wrong base":     func(r *GoalPreflightRequest, f *preflightAPI) { f.prs[0].Base.Repo.FullName = "foreign/repo" },
		"foreign fork":   func(r *GoalPreflightRequest, f *preflightAPI) { f.prs[0].Head.Repo.FullName = "foreign/repo" },
		"spoofed author": func(r *GoalPreflightRequest, f *preflightAPI) { f.prs[0].User.ID = 99 },
		"human author":   func(r *GoalPreflightRequest, f *preflightAPI) { f.prs[0].User.Type = "User" },
		"head actor":     func(r *GoalPreflightRequest, f *preflightAPI) { f.actor.ID = 99 },
		"legacy branch": func(r *GoalPreflightRequest, f *preflightAPI) {
			f.prs[0].Head.Ref = syncBranchName("rust-v0.101.0", newSHA, r.BaseSHA)
		},
		"bad URL":      func(r *GoalPreflightRequest, f *preflightAPI) { f.prs[0].URL = "https://example.com" },
		"multiple":     func(r *GoalPreflightRequest, f *preflightAPI) { f.prs = append(f.prs, preflightPR(*r, f, 2)) },
		"duplicate":    func(r *GoalPreflightRequest, f *preflightAPI) { f.prs = append(f.prs, f.prs[0]) },
		"list error":   func(r *GoalPreflightRequest, f *preflightAPI) { f.fail = "/pulls?" },
		"ref error":    func(r *GoalPreflightRequest, f *preflightAPI) { f.fail = "/git/ref/" },
		"actor error":  func(r *GoalPreflightRequest, f *preflightAPI) { f.fail = "/activity?" },
		"lookup error": func(r *GoalPreflightRequest, f *preflightAPI) { r.Lookuper = fakeLookuper{} },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			req, f := preflightFixture(t)
			f.prs = []publicationPR{preflightPR(req, f, 1)}
			change(&req, f)
			got, err := GoalPreflight(context.Background(), req)
			if err == nil || got.Outcome != "blocked" {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}
func TestGoalPreflightReadsAllPages(t *testing.T) {
	req, f := preflightFixture(t)
	for i := 1; i <= 100; i++ {
		p := preflightPR(req, f, i)
		p.Head.Ref = fmt.Sprintf("dependency-%d", i)
		f.prs = append(f.prs, p)
	}
	f.prs = append(f.prs, preflightPR(req, f, 101))
	got, err := GoalPreflight(context.Background(), req)
	if err != nil || got.Outcome != "needs_recovery" || f.calls != 4 {
		t.Fatalf("%+v %v calls=%d", got, err, f.calls)
	}
}
