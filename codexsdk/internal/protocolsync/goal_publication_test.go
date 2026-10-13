package protocolsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/checkacceptance"
)

type goalPublicationFixture struct {
	t               *testing.T
	request         GoalPublicationRequest
	pr              goalDraftPR
	actor           publicationUser
	state           checkacceptance.State
	pendingAttempt  bool
	apiCalls        int
	staleProjection string
	failPRRead      bool
	onRead          func()
}

func TestGoalPushObtainsFreshTokenForEachPublication(t *testing.T) {
	bin := t.TempDir()
	// An earlier launch token has expired. Git must receive the token minted for
	// this publication, including a second publication after a long repair.
	t.Setenv("GH_TOKEN", "expired-launch-token")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := "#!/bin/sh\ntest \"$GH_TOKEN\" = \"$EXPECTED_PUSH_TOKEN\" && test -z \"$PUSH_FAIL\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	issued, revoked := 0, 0
	r := GoalPublicationRequest{RepoRoot: t.TempDir(), Branch: "codex/sync-upstream-fixture"}
	r.PublicationToken = func(context.Context) (string, func(), error) {
		issued++
		return fmt.Sprintf("fresh-publication-%d", issued), func() { revoked++ }, nil
	}
	for i := 1; i <= 2; i++ {
		t.Setenv("EXPECTED_PUSH_TOKEN", fmt.Sprintf("fresh-publication-%d", i))
		if err := pushGoalDraft(context.Background(), r, strings.Repeat("a", 40)); err != nil {
			t.Fatalf("publication %d reused an expired token: %v", i, err)
		}
		if issued != i || revoked != i || os.Getenv("GH_TOKEN") != "expired-launch-token" {
			t.Fatal("publication must obtain and release its own token without changing the parent environment")
		}
	}
	t.Setenv("PUSH_FAIL", "1")
	t.Setenv("EXPECTED_PUSH_TOKEN", "fresh-publication-3")
	if err := pushGoalDraft(context.Background(), r, strings.Repeat("a", 40)); err == nil || issued != 3 || revoked != 3 {
		t.Fatal("failed push must release its token without replaying the write")
	}
}

func goalTestGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}
func goalTestWrite(t *testing.T, root, name, text string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

type goalLookup struct{ target Target }

func (l goalLookup) LSRemote(repository string, patterns ...string) (string, error) {
	var out []string
	for _, p := range patterns {
		out = append(out, l.target.PeeledCommitSHA+"\t"+p)
	}
	return strings.Join(out, "\n"), nil
}
func (f *goalPublicationFixture) Request(method, path string, body, result any) error {
	if method != "GET" {
		f.t.Fatalf("unexpected effect API %s %s", method, path)
	}
	f.apiCalls++
	head, _ := goalRemoteHead(context.Background(), f.request, f.request.Branch)
	var data any
	switch {
	case strings.Contains(path, "/pulls/"):
		if f.failPRRead {
			return errors.New("PR read unavailable")
		}
		pr := f.pr
		pr.Head.SHA = head
		if f.staleProjection != "" {
			pr.Head.SHA = f.staleProjection
		}
		data = pr
	case strings.Contains(path, "/activity?"):
		data = []any{map[string]any{"ref": "refs/heads/" + f.request.Branch, "after": head, "actor": f.actor}}
	case strings.Contains(path, "/check-runs"):
		var runs []checkacceptance.Run
		if f.state != checkacceptance.Missing {
			for i, name := range checkacceptance.RequiredNames() {
				r := checkacceptance.Run{ID: int64(i + 1), Name: name, HeadSHA: head, Status: "completed", Conclusion: "success"}
				r.App.Slug = "github-actions"
				if f.state == checkacceptance.Failed {
					r.Conclusion = "failure"
				}
				if f.state == checkacceptance.Cancelled {
					r.Conclusion = "cancelled"
				}
				runs = append(runs, r)
			}
		}
		data = map[string]any{"total_count": len(runs), "check_runs": runs}
	case strings.Contains(path, "/actions/workflows/"):
		var runs []checkacceptance.WorkflowRun
		if f.state != checkacceptance.Missing {
			r := checkacceptance.WorkflowRun{ID: 9, HeadSHA: head, Attempt: 1, Status: "completed", Conclusion: "success"}
			if f.state == checkacceptance.Failed {
				r.Conclusion = "failure"
			}
			if f.state == checkacceptance.Cancelled {
				r.Conclusion = "cancelled"
			}
			if f.pendingAttempt {
				r.Attempt = 2
				r.Status = "in_progress"
				r.Conclusion = ""
			}
			runs = append(runs, r)
		}
		data = map[string]any{"total_count": len(runs), "workflow_runs": runs}
	default:
		return fmt.Errorf("unexpected path %s", path)
	}
	if f.onRead != nil {
		hook := f.onRead
		f.onRead = nil
		hook()
	}
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, result)
}
func newGoalPublicationFixture(t *testing.T) *goalPublicationFixture {
	t.Helper()
	root := t.TempDir()
	goalTestGit(t, root, "init", "-b", "main")
	goalTestGit(t, root, "config", "user.name", "Fixture")
	goalTestGit(t, root, "config", "user.email", "fixture@localhost")
	target := Target{RefName: "rust-v0.141.0", RefKind: KindStableTag, PeeledCommitSHA: strings.Repeat("b", 40)}
	goalTestWrite(t, root, "codexsdk/internal/protocolschema/appserver/v2/baseline_metadata.json", fmt.Sprintf(`{"source_commit":%q,"source_ref_name":%q,"source_ref_kind":%q}`, target.PeeledCommitSHA, target.RefName, target.RefKind))
	goalTestWrite(t, root, "README.md", "accepted contract\n")
	goalTestGit(t, root, "add", "-A")
	goalTestGit(t, root, "commit", "-m", "base")
	base := goalTestGit(t, root, "rev-parse", "HEAD")
	remote := filepath.Join(t.TempDir(), "remote.git")
	goalTestGit(t, root, "clone", "--bare", root, remote)
	goalTestGit(t, root, "remote", "add", "origin", remote)
	branch := "codex/sync-upstream-fixture"
	goalTestGit(t, root, "push", "origin", base+":refs/heads/"+branch)
	goalTestWrite(t, root, "codexsdk/internal/protocolgen/fix.go", "package protocolgen\n")
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	digest, err := goalSnapshot(context.Background(), root, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	f := &goalPublicationFixture{t: t, state: checkacceptance.Succeeded, actor: publicationUser{ID: 42, Login: "sync[bot]", Type: "Bot"}}
	f.request = GoalPublicationRequest{RepoRoot: root, Repository: "owner/repo", Number: 7, AppBotID: 42, BaseBranch: "main", Branch: branch, BaseSHA: base, ExpectedHead: base, Target: target, CandidateDir: snapshot, SnapshotDigest: digest, API: f, Lookuper: goalLookup{target}}
	f.pr = goalDraftPR{publicationPR: publicationPR{Number: 7, State: "open", User: f.actor}, Draft: true}
	f.pr.Head.Ref = branch
	f.pr.Head.Repo.FullName = "owner/repo"
	f.pr.Base.Ref = "main"
	f.pr.Base.SHA = base
	f.pr.Base.Repo.FullName = "owner/repo"
	return f
}
func TestGoalDraftAppendsFrozenTreeAndReusesIdenticalHead(t *testing.T) {
	f := newGoalPublicationFixture(t)
	goalTestWrite(t, f.request.RepoRoot, "README.md", "mutable original ignored\n")
	result, err := AppendGoalDraft(context.Background(), f.request)
	if err != nil {
		t.Fatal(err)
	}
	if parent := goalTestGit(t, f.request.RepoRoot, "show", "-s", "--format=%P", result.Head); parent != f.request.ExpectedHead {
		t.Fatalf("parent %s", parent)
	}
	if got := goalTestGit(t, f.request.RepoRoot, "show", result.Head+":README.md"); got != "accepted contract" {
		t.Fatalf("published dirty original: %q", got)
	}
	if got := goalTestGit(t, f.request.RepoRoot, "rev-parse", "HEAD"); got != f.request.BaseSHA {
		t.Fatal("mutated original checkout")
	}
	f.request.ExpectedHead = result.Head
	repeat, err := appendGoalDraft(context.Background(), f.request, func(context.Context, GoalPublicationRequest, string) error {
		t.Fatal("same tree pushed again")
		return nil
	})
	if err != nil || repeat.Head != result.Head {
		t.Fatalf("repeat=%+v err=%v", repeat, err)
	}
	goalTestWrite(t, f.request.CandidateDir, "codexsdk/internal/schemaoracle/fix_test.go", "package schemaoracle\n")
	goalTestGit(t, f.request.CandidateDir, "add", "-A")
	goalTestGit(t, f.request.CandidateDir, "-c", "user.name=Fixture", "-c", "user.email=fixture@localhost", "commit", "-m", "next candidate")
	f.request.SnapshotDigest, _ = goalSnapshot(context.Background(), f.request.CandidateDir, "")
	next, err := AppendGoalDraft(context.Background(), f.request)
	if err != nil {
		t.Fatal(err)
	}
	if goalTestGit(t, f.request.RepoRoot, "show", "-s", "--format=%P", next.Head) != result.Head {
		t.Fatal("second append lost previous parent")
	}
}
func TestGoalDraftRejectsChangedBindingBeforePush(t *testing.T) {
	cases := map[string]func(*goalPublicationFixture){
		"ready": func(f *goalPublicationFixture) { f.pr.Draft = false }, "closed": func(f *goalPublicationFixture) { f.pr.State = "closed" }, "foreign repo": func(f *goalPublicationFixture) { f.pr.Head.Repo.FullName = "other/repo" }, "foreign creator": func(f *goalPublicationFixture) { f.pr.User.ID++ }, "foreign actor": func(f *goalPublicationFixture) { f.actor.ID++ }, "wrong base": func(f *goalPublicationFixture) { f.pr.Base.SHA = strings.Repeat("c", 40) }, "namespace": func(f *goalPublicationFixture) { f.request.Branch = "feature/anything" }, "zero PR": func(f *goalPublicationFixture) { f.request.Number = 0 }, "digest": func(f *goalPublicationFixture) { f.request.SnapshotDigest = strings.Repeat("0", 64) },
		"source": func(f *goalPublicationFixture) { f.request.Target.PeeledCommitSHA = strings.Repeat("c", 40) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newGoalPublicationFixture(t)
			mutate(f)
			_, err := appendGoalDraft(context.Background(), f.request, func(context.Context, GoalPublicationRequest, string) error {
				t.Fatal("invalid binding pushed")
				return nil
			})
			if err == nil {
				t.Fatal("accepted invalid publication")
			}
		})
	}
}
func TestGoalDraftUnknownPushReadsBackWithoutReplay(t *testing.T) {
	for _, didPush := range []bool{false, true} {
		t.Run(fmt.Sprint(didPush), func(t *testing.T) {
			f := newGoalPublicationFixture(t)
			calls := 0
			result, err := appendGoalDraft(context.Background(), f.request, func(ctx context.Context, r GoalPublicationRequest, head string) error {
				calls++
				if didPush {
					goalTestGit(t, r.RepoRoot, "push", "origin", head+":refs/heads/"+r.Branch)
				}
				return errors.New("lost response")
			})
			if calls != 1 {
				t.Fatalf("push replayed %d", calls)
			}
			if (err == nil) != didPush {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}
func TestGoalCIExistingLatestAttemptAndTerminalStates(t *testing.T) {
	for _, state := range []checkacceptance.State{checkacceptance.Succeeded, checkacceptance.Failed, checkacceptance.Cancelled} {
		t.Run(string(state), func(t *testing.T) {
			f := newGoalPublicationFixture(t)
			f.state = state
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			got, err := ObserveGoalCI(ctx, f.request, f.request.ExpectedHead, time.Millisecond)
			if err != nil || got.State != state {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
	for _, state := range []checkacceptance.State{checkacceptance.Missing, checkacceptance.Pending, checkacceptance.Failed} {
		t.Run(string(state), func(t *testing.T) {
			f := newGoalPublicationFixture(t)
			f.state = state
			f.pendingAttempt = true
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			got, err := ObserveGoalCI(ctx, f.request, f.request.ExpectedHead, time.Millisecond)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("%+v %v", got, err)
			}
			if got.State == checkacceptance.Succeeded {
				t.Fatal("old green accepted during newer attempt")
			}
		})
	}
}
func TestGoalCINeedsDeadlineAndRejectsHeadMovement(t *testing.T) {
	f := newGoalPublicationFixture(t)
	if _, err := ObserveGoalCI(context.Background(), f.request, f.request.ExpectedHead, time.Millisecond); err == nil {
		t.Fatal("unbounded poll accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	f.onRead = func() { f.pr.Draft = false }
	if _, err := ObserveGoalCI(ctx, f.request, f.request.ExpectedHead, time.Millisecond); err == nil {
		t.Fatal("PR change accepted")
	}
}

func TestGoalDraftMainMovementBlocksBeforeAndAfterPush(t *testing.T) {
	for _, afterPush := range []bool{false, true} {
		t.Run(fmt.Sprint(afterPush), func(t *testing.T) {
			f := newGoalPublicationFixture(t)
			moveMain := func() {
				tree := goalTestGit(t, f.request.RepoRoot, "rev-parse", f.request.BaseSHA+"^{tree}")
				next := goalTestGit(t, f.request.RepoRoot, "commit-tree", tree, "-p", f.request.BaseSHA, "-m", "unrelated main movement")
				goalTestGit(t, f.request.RepoRoot, "push", "origin", next+":refs/heads/main")
			}
			if !afterPush {
				f.onRead = moveMain
			}
			pushes := 0
			_, err := appendGoalDraft(context.Background(), f.request, func(ctx context.Context, r GoalPublicationRequest, h string) error {
				pushes++
				goalTestGit(t, r.RepoRoot, "push", "origin", h+":refs/heads/"+r.Branch)
				moveMain()
				return nil
			})
			if err == nil {
				t.Fatal("main movement accepted")
			}
			if !afterPush && pushes != 0 {
				t.Fatal("stale main was published")
			}
			if afterPush && pushes != 1 {
				t.Fatal("push was replayed")
			}
		})
	}
}

func TestGoalDraftRejectsFrozenSourceIdentityChanges(t *testing.T) {
	for _, body := range []string{"invalid identity", fmt.Sprintf(`{"source_commit":%q,"source_ref_name":"rust-v0.141.0","source_ref_kind":"stable_rust_tag"}`, strings.Repeat("c", 40))} {
		t.Run(body, func(t *testing.T) {
			f := newGoalPublicationFixture(t)
			goalTestWrite(t, f.request.CandidateDir, "codexsdk/internal/protocolschema/appserver/v2/baseline_metadata.json", body)
			goalTestGit(t, f.request.CandidateDir, "add", "-A")
			goalTestGit(t, f.request.CandidateDir, "-c", "user.name=Fixture", "-c", "user.email=fixture@localhost", "commit", "-m", "changed frozen candidate")
			f.request.SnapshotDigest, _ = goalSnapshot(context.Background(), f.request.CandidateDir, "")
			if _, err := appendGoalDraft(context.Background(), f.request, func(context.Context, GoalPublicationRequest, string) error {
				t.Fatal("invalid frozen source published")
				return nil
			}); err == nil {
				t.Fatal("invalid frozen source accepted")
			}
		})
	}
}

func TestGoalDraftRetainsConfirmedHeadWhenPRReadbackLags(t *testing.T) {
	for _, mode := range []string{"stale projection", "read failure"} {
		t.Run(mode, func(t *testing.T) {
			f := newGoalPublicationFixture(t)
			pushes := 0
			got, err := appendGoalDraft(context.Background(), f.request, func(ctx context.Context, r GoalPublicationRequest, head string) error {
				pushes++
				goalTestGit(t, r.RepoRoot, "push", "origin", head+":refs/heads/"+r.Branch)
				if mode == "stale projection" {
					f.staleProjection = r.ExpectedHead
				} else {
					f.failPRRead = true
				}
				return nil
			})
			if mode == "stale projection" && err != nil {
				t.Fatalf("lagging PR projection rejected confirmed Git head: %v", err)
			}
			if mode == "read failure" && err == nil {
				t.Fatal("unavailable PR identity became acceptance")
			}
			actual, readErr := goalRemoteHead(context.Background(), f.request, f.request.Branch)
			if readErr != nil || got.Head == "" || got.Head != actual || got.PreviousHead != f.request.ExpectedHead || got.Number != f.request.Number || got.SnapshotDigest != f.request.SnapshotDigest {
				t.Fatalf("confirmed effect lost: %+v %v", got, readErr)
			}
			f.request.ExpectedHead = got.Head
			f.failPRRead = false
			if mode == "stale projection" {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				observation, err := ObserveGoalCI(ctx, f.request, got.Head, time.Millisecond)
				if err != nil || observation.State != checkacceptance.Succeeded || observation.Head != got.Head {
					t.Fatalf("confirmed exact-head CI blocked by stale PR projection: %+v %v", observation, err)
				}
			}
			again, err := appendGoalDraft(context.Background(), f.request, func(context.Context, GoalPublicationRequest, string) error {
				pushes++
				t.Fatal("confirmed identical snapshot pushed twice")
				return nil
			})
			if err != nil || again.Head != got.Head || pushes != 1 {
				t.Fatalf("reconciliation %+v %v pushes=%d", again, err, pushes)
			}
		})
	}
}

func TestGoalDraftNeverAdoptsForeignPushReadback(t *testing.T) {
	f := newGoalPublicationFixture(t)
	got, err := appendGoalDraft(context.Background(), f.request, func(ctx context.Context, r GoalPublicationRequest, head string) error {
		tree := goalTestGit(t, r.RepoRoot, "rev-parse", r.BaseSHA+"^{tree}")
		foreign := goalTestGit(t, r.RepoRoot, "commit-tree", tree, "-p", r.ExpectedHead, "-m", "another actor")
		goalTestGit(t, r.RepoRoot, "push", "origin", foreign+":refs/heads/"+r.Branch)
		return errors.New("our push failed")
	})
	if err == nil || got.Head != "" {
		t.Fatalf("foreign effect adopted: %+v %v", got, err)
	}
}
