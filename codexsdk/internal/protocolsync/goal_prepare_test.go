package protocolsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

type goalPrepareFixture struct {
	*goalPublicationFixture
	prs          []goalDraftPR
	creates      int
	loseResponse bool
	rejectCreate bool
	mutate       func(*goalDraftPR)
}

func (f *goalPrepareFixture) Request(method, path string, body, result any) error {
	var value any
	switch {
	case method == "GET" && path == "repos/owner/repo":
		value = map[string]string{"full_name": "owner/repo", "default_branch": "main"}
	case method == "GET" && strings.Contains(path, "pulls?"):
		parsed, _ := url.Parse(path)
		branch := strings.TrimPrefix(parsed.Query().Get("head"), "owner:")
		matches := []goalDraftPR{}
		for _, pr := range f.prs {
			if pr.Head.Ref == branch {
				matches = append(matches, pr)
			}
		}
		value = matches
	case method == "POST" && path == "repos/owner/repo/pulls":
		f.creates++
		data := body.(map[string]any)
		if data["draft"] != true {
			f.t.Fatal("created ready PR")
		}
		if f.rejectCreate {
			return errors.New("native create rejected")
		}
		branch := data["head"].(string)
		head, _ := goalPrepareRemote(context.Background(), GoalPublicationRequest{RepoRoot: f.request.RepoRoot, Branch: branch})
		pr := f.pr
		pr.Head.Ref = branch
		pr.Head.SHA = head
		pr.URL = "https://github.com/owner/repo/pull/19"
		pr.Number = 19
		if f.mutate != nil {
			f.mutate(&pr)
		}
		f.prs = append(f.prs, pr)
		if f.loseResponse {
			return errors.New("lost create response")
		}
		value = pr
	case method == "GET" && strings.Contains(path, "/pulls/"):
		if len(f.prs) != 1 {
			return errors.New("no unique PR")
		}
		value = f.prs[0]
	case method == "GET" && strings.Contains(path, "/activity?"):
		parsed, _ := url.Parse(path)
		ref := parsed.Query().Get("ref")
		head, _ := goalPrepareRemote(context.Background(), GoalPublicationRequest{RepoRoot: f.request.RepoRoot, Branch: strings.TrimPrefix(ref, "refs/heads/")})
		value = []any{map[string]any{"ref": ref, "after": head, "actor": f.actor}}
	default:
		return fmt.Errorf("unexpected request %s %s", method, path)
	}
	if result == nil {
		return nil
	}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, result)
}
func newGoalPrepareFixture(t *testing.T) (*goalPrepareFixture, GoalPublicationRequest) {
	f := &goalPrepareFixture{goalPublicationFixture: newGoalPublicationFixture(t)}
	req := f.request
	req.API = f
	req.Branch = ""
	req.ExpectedHead = ""
	req.Number = 0
	req.CandidateDir = ""
	req.SnapshotDigest = ""
	return f, req
}
func TestPrepareGoalDraftCreatesEmptySeedAndRecoversLostCreate(t *testing.T) {
	f, req := newGoalPrepareFixture(t)
	f.loseResponse = true
	got, err := PrepareGoalDraft(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://github.com/owner/repo/pull/19" || got.Binding.Number != 19 || f.creates != 1 {
		t.Fatalf("%+v creates=%d", got, f.creates)
	}
	if goalTestGit(t, req.RepoRoot, "show", "-s", "--format=%P", got.Binding.ExpectedHead) != req.BaseSHA {
		t.Fatal("seed is not single-parent")
	}
	if goalTestGit(t, req.RepoRoot, "rev-parse", got.Binding.ExpectedHead+"^{tree}") != goalTestGit(t, req.RepoRoot, "rev-parse", req.BaseSHA+"^{tree}") {
		t.Fatal("seed contains SDK answer")
	}
	if got.Binding.Branch == f.request.Branch {
		t.Fatal("reused prior draft")
	}
	again, err := PrepareGoalDraft(context.Background(), got.Binding)
	if err != nil || again.URL != got.URL || f.creates != 1 {
		t.Fatalf("reconcile %+v %v creates=%d", again, err, f.creates)
	}
}
func TestPrepareGoalDraftRetainsLocatorAfterUnconfirmedCreate(t *testing.T) {
	f, req := newGoalPrepareFixture(t)
	f.rejectCreate = true
	got, err := PrepareGoalDraft(context.Background(), req)
	if err == nil || got.Binding.Branch == "" || got.Binding.ExpectedHead == "" || got.URL != "" {
		t.Fatalf("%+v %v", got, err)
	}
	if f.creates != 1 {
		t.Fatal("blind create replay")
	}
	head, e := goalPrepareRemote(context.Background(), got.Binding)
	if e != nil || head != got.Binding.ExpectedHead {
		t.Fatal("lost native locator")
	}
	f.rejectCreate = false
	resumed, err := PrepareGoalDraft(context.Background(), got.Binding)
	if err != nil || resumed.Binding.Branch != got.Binding.Branch || resumed.Binding.ExpectedHead != got.Binding.ExpectedHead {
		t.Fatalf("reconcile %+v %v", resumed, err)
	}
}
func TestPrepareGoalDraftRejectsIdentityChanges(t *testing.T) {
	for _, name := range []string{"main", "target", "actor", "ready", "creator", "foreign repo", "closed"} {
		t.Run(name, func(t *testing.T) {
			f, req := newGoalPrepareFixture(t)
			switch name {
			case "main":
				req.BaseSHA = strings.Repeat("c", 40)
			case "target":
				req.Target.PeeledCommitSHA = strings.Repeat("c", 40)
			case "actor":
				f.actor.ID++
			case "ready":
				f.mutate = func(p *goalDraftPR) { p.Draft = false }
			case "creator":
				f.mutate = func(p *goalDraftPR) { p.User.ID++ }
			case "foreign repo":
				f.mutate = func(p *goalDraftPR) { p.Head.Repo.FullName = "foreign/repo" }
			case "closed":
				f.mutate = func(p *goalDraftPR) { p.State = "closed" }
			}
			if _, err := PrepareGoalDraft(context.Background(), req); err == nil {
				t.Fatal("changed binding accepted")
			}
			if (name == "main" || name == "target" || name == "actor") && f.creates != 0 {
				t.Fatal("stale preparation created PR")
			}
		})
	}
}
func TestPrepareGoalDraftRejectsChangedSeedHistory(t *testing.T) {
	f, req := newGoalPrepareFixture(t)
	got, err := PrepareGoalDraft(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	tree := goalTestGit(t, req.RepoRoot, "rev-parse", req.BaseSHA+"^{tree}")
	// A second parent is forbidden even if its tree contains no file changes.
	head := goalTestGit(t, req.RepoRoot, "commit-tree", tree, "-p", got.Binding.ExpectedHead, "-m", "not the retained seed")
	got.Binding.ExpectedHead = head
	if _, err := PrepareGoalDraft(context.Background(), got.Binding); err == nil {
		t.Fatal("non-seed history accepted")
	}
	if f.creates != 1 {
		t.Fatal("created replacement PR")
	}
}

func TestPrepareGoalDraftRejectsSeedWithFileChanges(t *testing.T) {
	f, req := newGoalPrepareFixture(t)
	got, err := PrepareGoalDraft(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	goalTestGit(t, req.RepoRoot, "fetch", "--no-tags", f.request.CandidateDir, "HEAD")
	tree := goalTestGit(t, f.request.CandidateDir, "rev-parse", "HEAD^{tree}")
	got.Binding.ExpectedHead = goalTestGit(t, req.RepoRoot, "commit-tree", tree, "-p", req.BaseSHA, "-m", "SDK answer disguised as seed")
	if _, err := PrepareGoalDraft(context.Background(), got.Binding); err == nil {
		t.Fatal("seed with SDK answer accepted")
	}
	if f.creates != 1 {
		t.Fatal("created a replacement PR")
	}
}
