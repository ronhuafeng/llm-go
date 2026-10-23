package protocolsync

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type recoveryFixture struct {
	t         *testing.T
	req       GoalRecoveryRequest
	now       time.Time
	run       goalRecoveryRun
	artifact  goalRecoveryArtifact
	binding   GoalRecoveryBinding
	files     map[string][]byte
	archive   []byte
	pr        goalDraftPR
	actor     publicationUser
	requests  int
	mutateRun func(*goalRecoveryRun)
}

func (f *recoveryFixture) Request(method, path string, body, result any) error {
	if method != "GET" || body != nil {
		f.t.Fatalf("recovery attempted remote effect: %s %s", method, path)
	}
	f.requests++
	var value any
	root := "repos/" + f.req.Repository
	switch {
	case path == root:
		value = map[string]any{"full_name": f.req.Repository, "default_branch": "main"}
	case path == fmt.Sprintf("%s/actions/runs/%d", root, f.req.PriorRunID):
		run := f.run
		if f.mutateRun != nil {
			f.mutateRun(&run)
		}
		value = run
	case path == root+"/actions/workflows/codexsdk-upstream-protocol-sync.yml":
		value = map[string]any{"id": int64(200), "path": goalRecoveryWorkflow}
	case strings.Contains(path, "/artifacts?"):
		value = map[string]any{"total_count": 1, "artifacts": []goalRecoveryArtifact{f.artifact}}
	case path == fmt.Sprintf("%s/pulls/%d", root, f.req.Number):
		value = f.pr
	case strings.Contains(path, "/git/ref/heads/"):
		branch := strings.SplitN(path, "/git/ref/heads/", 2)[1]
		head := f.req.ExpectedHead
		if branch == "main" {
			head = f.req.CurrentBaseSHA
		}
		value = map[string]any{"ref": "refs/heads/" + branch, "object": map[string]string{"type": "commit", "sha": head}}
	case strings.Contains(path, "/activity?"):
		value = []any{map[string]any{"ref": "refs/heads/" + f.binding.Branch, "after": f.req.ExpectedHead, "actor": f.actor}}
	default:
		return errors.New("unexpected recovery read: " + path)
	}
	b, _ := json.Marshal(value)
	return json.Unmarshal(b, result)
}
func (f *recoveryFixture) download(ctx context.Context, req GoalRecoveryRequest, id int64) ([]byte, error) {
	if id != f.artifact.ID {
		f.t.Fatal("download unbound artifact")
	}
	return f.archive, nil
}
func recoveryJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func (f *recoveryFixture) pack() {
	f.files["goal-binding.json"] = recoveryJSON(f.t, f.binding)
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range f.files {
		entry, e := writer.Create(name)
		if e != nil {
			f.t.Fatal(e)
		}
		if _, e = entry.Write(content); e != nil {
			f.t.Fatal(e)
		}
	}
	if e := writer.Close(); e != nil {
		f.t.Fatal(e)
	}
	f.archive = buffer.Bytes()
	f.artifact.Size = int64(len(f.archive))
	f.artifact.Digest = fmt.Sprintf("sha256:%x", sha256.Sum256(f.archive))
}
func newRecoveryFixture(t *testing.T) *recoveryFixture {
	t.Helper()
	root := t.TempDir()
	goalTestGit(t, root, "init", "-q", "-b", "main")
	goalTestGit(t, root, "config", "user.name", "Fixture")
	goalTestGit(t, root, "config", "user.email", "fixture@example.com")
	goalTestWrite(t, root, mechanicalPrefix+"/baseline_metadata.json", `{"source_commit":"`+oldSHA+`","source_ref_name":"rust-v0.100.0","source_ref_kind":"stable_rust_tag"}`)
	goalTestWrite(t, root, "README.md", "original accepted contract\n")
	goalTestGit(t, root, "add", ".")
	goalTestGit(t, root, "commit", "-qm", "base")
	base := goalTestGit(t, root, "rev-parse", "HEAD")
	target := Target{RefName: "rust-v0.101.0", RefKind: KindStableTag, PeeledCommitSHA: newSHA}
	branch := syncBranchName(target.RefName, target.PeeledCommitSHA, base) + "-goal-0123456789abcdef"
	tree := goalTestGit(t, root, "rev-parse", "HEAD^{tree}")
	seed := goalTestGit(t, root, "commit-tree", tree, "-p", base, "-m", "empty seed")
	now := time.Date(2026, 10, 23, 23, 0, 0, 0, time.UTC)
	f := &recoveryFixture{t: t, now: now, actor: publicationUser{ID: 42, Login: "sync[bot]", Type: "Bot"}, files: map[string][]byte{}}
	f.req = GoalRecoveryRequest{RepoRoot: root, OutputDir: filepath.Join(t.TempDir(), "recovery"), Repository: "owner/repo", CurrentBaseSHA: base, ExpectedHead: seed, Number: 7, AppBotID: 42, PriorRunID: 100, RegenerateEmptySeed: true, API: f, Lookuper: goalLookup{target}}
	f.binding = GoalRecoveryBinding{Schema: 1, Repository: f.req.Repository, RunID: 100, RunAttempt: 1, ControllerSHA: base, Base: base, Branch: branch, Number: 7, Head: seed, AppBotID: 42, TargetRef: target.RefName, TargetKind: target.RefKind, TargetSHA: target.PeeledCommitSHA, Deadline: now.Add(time.Hour), URL: "https://github.com/owner/repo/pull/7", Confirmed: true}
	f.run = goalRecoveryRun{ID: 100, Attempt: 1, WorkflowID: 200, Path: goalRecoveryWorkflow, Event: "workflow_dispatch", Status: "completed", Conclusion: "failure", HeadBranch: "main", HeadSHA: base}
	f.run.Repository.FullName = f.req.Repository
	f.run.HeadRepository.FullName = f.req.Repository
	f.artifact = goalRecoveryArtifact{ID: 300, Name: "protocol-native-goal-1"}
	f.artifact.WorkflowRun.ID = 100
	f.artifact.WorkflowRun.HeadSHA = base
	f.artifact.WorkflowRun.HeadBranch = "main"
	f.pr = goalDraftPR{publicationPR: publicationPR{Number: 7, URL: f.binding.URL, State: "open", User: f.actor}, Draft: true}
	f.pr.Head.Ref = branch
	f.pr.Head.SHA = seed
	f.pr.Head.Repo.FullName = f.req.Repository
	f.pr.Base.Ref = "main"
	f.pr.Base.SHA = base
	f.pr.Base.Repo.FullName = f.req.Repository
	f.files["goal-source.json"] = recoveryJSON(t, struct{ Target Target }{target})
	f.files["goal-result.json"] = recoveryJSON(t, GoalFlowResult{NativeStatus: "failed", Stage: "Local verification", Failure: "fixture stopped"})
	f.pack()
	return f
}
func (f *recoveryFixture) recover() (GoalRecoveryResult, error) {
	return recoverGoalDraft(context.Background(), f.req, f.download, f.now)
}
func (f *recoveryFixture) candidate() {
	t := f.t
	root := f.req.RepoRoot
	target := Target{RefName: f.binding.TargetRef, RefKind: f.binding.TargetKind, PeeledCommitSHA: f.binding.TargetSHA}
	goalTestWrite(t, root, mechanicalPrefix+"/baseline_metadata.json", fmt.Sprintf(`{"source_commit":%q,"source_ref_name":%q,"source_ref_kind":%q}`, target.PeeledCommitSHA, target.RefName, target.RefKind))
	goalTestWrite(t, root, "README.md", "retained candidate\n")
	goalTestWrite(t, root, ".gitattributes", "README.md filter=hostile\n")
	candidate := filepath.Join(t.TempDir(), "snapshot")
	digest, e := goalSnapshot(context.Background(), root, candidate)
	if e != nil {
		t.Fatal(e)
	}
	goalTestGit(t, root, "reset", "--hard", f.binding.Base)
	if err := os.Remove(filepath.Join(root, ".gitattributes")); err != nil {
		t.Fatal(err)
	}
	goalTestGit(t, root, "fetch", "--no-tags", candidate, "HEAD")
	tree := goalTestGit(t, candidate, "rev-parse", "HEAD^{tree}")
	head := goalTestGit(t, root, "commit-tree", tree, "-p", f.binding.Head, "-m", "publication")
	state := filepath.Join(t.TempDir(), "state")
	req := GoalPublicationRequest{RepoRoot: root, Repository: f.req.Repository, BaseBranch: "main", Branch: f.binding.Branch, BaseSHA: f.binding.Base, ExpectedHead: f.binding.Head, Number: f.req.Number, AppBotID: f.req.AppBotID, Target: target, CandidateDir: candidate, SnapshotDigest: digest, StateDir: state, Deadline: time.Now().Add(time.Hour)}
	// The fixture's deterministic clock and the publication writer's real clock
	// are independent. Retain one matching old deadline after making the bundle.
	intent, e := writeGoalPublicationIntent(context.Background(), req, candidate, head, tree)
	if e != nil {
		t.Fatal(e)
	}
	intent.Deadline = f.binding.Deadline
	f.files["goal-publication/"+goalIntentName] = recoveryJSON(t, intent)
	bundle, e := os.ReadFile(filepath.Join(state, goalBundleName))
	if e != nil {
		t.Fatal(e)
	}
	f.files["goal-publication/"+goalBundleName] = bundle
	f.req.ExpectedHead = head
	f.pr.Head.SHA = head
	f.pack()
}

func TestGoalRecoveryExplicitEmptySeedRegeneratesSamePR(t *testing.T) {
	f := newRecoveryFixture(t)
	got, e := f.recover()
	if e != nil || got.Outcome != "admitted" || got.Classification != "regenerate_empty_seed" || got.Binding.Number != 7 || got.ExpectedHead != f.req.ExpectedHead || !got.ExecutionDeadline.Equal(f.binding.Deadline) {
		t.Fatalf("%+v %v", got, e)
	}
	if head := goalTestGit(t, got.CandidateDir, "rev-parse", "HEAD"); head != f.req.ExpectedHead {
		t.Fatal("candidate not exact empty seed")
	}
	if !strings.Contains(got.Reason, "fresh native goal") {
		t.Fatal("must not claim private thread resume")
	}
}
func TestGoalRecoveryAdoptsOnlyOwnRetainedCandidate(t *testing.T) {
	f := newRecoveryFixture(t)
	f.candidate()
	f.req.RegenerateEmptySeed = false
	got, e := f.recover()
	if e != nil || got.Outcome != "admitted" || got.Classification != "retained_candidate" {
		t.Fatalf("%+v %v", got, e)
	}
	b, e := os.ReadFile(filepath.Join(got.CandidateDir, "README.md"))
	if e != nil || string(b) != "retained candidate\n" {
		t.Fatal("full candidate was not preserved")
	}
	if f.requests < 12 {
		t.Fatal("must revalidate provenance and live identities")
	}
}
func TestGoalRecoveryDeadlineCannotRenewImplicitly(t *testing.T) {
	for _, tc := range []struct {
		name        string
		authorize   bool
		newDeadline time.Duration
		want        string
	}{{"expired", false, 0, "explicit new bounded"}, {"timestamp alone", false, time.Hour, "requires explicit"}, {"unbounded", true, 7 * time.Hour, "six hours"}, {"new authorized execution", true, time.Hour, ""}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRecoveryFixture(t)
			f.binding.Deadline = f.now.Add(-time.Hour)
			f.req.AuthorizeNewBoundedExecution = tc.authorize
			if tc.newDeadline != 0 {
				f.req.NewExecutionDeadline = f.now.Add(tc.newDeadline)
			}
			f.pack()
			got, e := f.recover()
			if tc.want == "" {
				if e != nil || !got.PriorDeadline.Equal(f.binding.Deadline) || !got.ExecutionDeadline.Equal(f.req.NewExecutionDeadline) {
					t.Fatalf("%+v %v", got, e)
				}
			} else if e == nil || !strings.Contains(e.Error(), tc.want) {
				t.Fatalf("%+v %v", got, e)
			}
		})
	}
}
func TestGoalRecoveryRejectsDivergentSeedWithoutMutation(t *testing.T) {
	f := newRecoveryFixture(t)
	goalTestWrite(t, f.req.RepoRoot, "main-progress.txt", "new accepted work\n")
	goalTestGit(t, f.req.RepoRoot, "add", ".")
	goalTestGit(t, f.req.RepoRoot, "commit", "-qm", "main advanced")
	f.req.CurrentBaseSHA = goalTestGit(t, f.req.RepoRoot, "rev-parse", "HEAD")
	got, e := f.recover()
	if e == nil || got.Classification != "divergent_empty_seed" || !strings.Contains(e.Error(), "separately authorized exact-lease rewrite or replacement") {
		t.Fatalf("%+v %v", got, e)
	}
	if head := goalTestGit(t, f.req.RepoRoot, "rev-parse", "HEAD"); head != f.req.CurrentBaseSHA {
		t.Fatal("changed current checkout")
	}
}
func TestGoalRecoveryRejectsUnauthenticatedOrActiveEvidence(t *testing.T) {
	cases := map[string]func(*recoveryFixture){
		"active run":                func(f *recoveryFixture) { f.run.Status = "in_progress"; f.run.Conclusion = "" },
		"successful run":            func(f *recoveryFixture) { f.run.Conclusion = "success" },
		"wrong run":                 func(f *recoveryFixture) { f.run.ID++ },
		"wrong workflow":            func(f *recoveryFixture) { f.run.WorkflowID++ },
		"wrong repository":          func(f *recoveryFixture) { f.run.HeadRepository.FullName = "foreign/repo" },
		"wrong actor":               func(f *recoveryFixture) { f.actor.ID++ },
		"foreign creator":           func(f *recoveryFixture) { f.pr.User.ID++ },
		"wrong PR":                  func(f *recoveryFixture) { f.pr.Number++ },
		"wrong branch":              func(f *recoveryFixture) { f.pr.Head.Ref = "codex/sync-upstream-foreign" },
		"not draft":                 func(f *recoveryFixture) { f.pr.Draft = false },
		"binding run mismatch":      func(f *recoveryFixture) { f.binding.RunID++; f.pack() },
		"binding actor mismatch":    func(f *recoveryFixture) { f.binding.AppBotID++; f.pack() },
		"binding head forged":       func(f *recoveryFixture) { f.binding.Head = oldSHA; f.pack() },
		"missing original deadline": func(f *recoveryFixture) { f.binding.Deadline = time.Time{}; f.pack() },
		"source target mismatch": func(f *recoveryFixture) {
			f.files["goal-source.json"] = []byte(`{"Target":{"RefName":"rust-v0.101.0","RefKind":"stable_rust_tag","PeeledCommitSHA":"` + oldSHA + `"}}`)
			f.pack()
		},
		"null result":                   func(f *recoveryFixture) { f.files["goal-result.json"] = []byte("null"); f.pack() },
		"empty result":                  func(f *recoveryFixture) { f.files["goal-result.json"] = []byte("{}"); f.pack() },
		"missing result":                func(f *recoveryFixture) { delete(f.files, "goal-result.json"); f.pack() },
		"artifact missing":              func(f *recoveryFixture) { f.artifact.Name = "other" },
		"artifact expired":              func(f *recoveryFixture) { f.artifact.Expired = true },
		"artifact foreign run":          func(f *recoveryFixture) { f.artifact.WorkflowRun.ID++ },
		"artifact forged bytes":         func(f *recoveryFixture) { f.archive = append(f.archive, 'x') },
		"artifact missing digest":       func(f *recoveryFixture) { f.artifact.Digest = "" },
		"unapproved empty regeneration": func(f *recoveryFixture) { f.req.RegenerateEmptySeed = false },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newRecoveryFixture(t)
			mutate(f)
			got, e := f.recover()
			if e == nil || got.Outcome != "blocked" {
				t.Fatalf("%+v %v", got, e)
			}
			if _, err := os.Stat(f.req.OutputDir); !os.IsNotExist(err) {
				t.Fatal("failed admission left reusable source directory")
			}
		})
	}
}
func TestGoalRecoveryNeverReplaysUnresolvedPublication(t *testing.T) {
	for _, name := range []string{"previous head", "missing intent", "missing candidate", "corrupt candidate", "foreign intended head"} {
		t.Run(name, func(t *testing.T) {
			f := newRecoveryFixture(t)
			f.candidate()
			switch name {
			case "previous head":
				f.req.ExpectedHead = f.binding.Head
			case "missing intent":
				delete(f.files, "goal-publication/"+goalIntentName)
			case "missing candidate":
				delete(f.files, "goal-publication/"+goalBundleName)
			case "corrupt candidate":
				f.files["goal-publication/"+goalBundleName] = []byte("bad")
			case "foreign intended head":
				var in GoalPublicationIntent
				_ = json.Unmarshal(f.files["goal-publication/"+goalIntentName], &in)
				in.IntendedHead = oldSHA
				f.files["goal-publication/"+goalIntentName] = recoveryJSON(t, in)
			}
			f.pack()
			got, e := f.recover()
			if e == nil || got.Outcome != "blocked" {
				t.Fatalf("%+v %v", got, e)
			}
		})
	}
}
func TestGoalRecoveryRejectsPriorRunRestartDuringAdmission(t *testing.T) {
	f := newRecoveryFixture(t)
	calls := 0
	f.mutateRun = func(r *goalRecoveryRun) {
		calls++
		if calls > 1 {
			r.Attempt++
			r.Status = "in_progress"
			r.Conclusion = ""
		}
	}
	got, e := f.recover()
	if e == nil || got.Outcome != "blocked" {
		t.Fatalf("%+v %v", got, e)
	}
}
func TestGoalRecoveryArchiveRejectsDuplicateAndTraversal(t *testing.T) {
	for _, names := range [][]string{{"goal-binding.json", "goal-binding.json"}, {"../goal-binding.json"}, {"/goal-binding.json"}, {"goal-publication/../goal-binding.json"}} {
		var b bytes.Buffer
		z := zip.NewWriter(&b)
		for _, name := range names {
			w, _ := z.Create(name)
			_, _ = w.Write([]byte("{}"))
		}
		_ = z.Close()
		if _, e := readGoalRecoveryArchive(b.Bytes()); e == nil {
			t.Fatalf("accepted unsafe members %v", names)
		}
	}
}

func TestGoalRecoveryMaterializationNeverExecutesHostFilters(t *testing.T) {
	f := newRecoveryFixture(t)
	f.candidate()
	marker := filepath.Join(t.TempDir(), "filter-executed")
	config := filepath.Join(t.TempDir(), "gitconfig")
	// This candidate-controlled attribute must not invoke ambient host commands.
	content := fmt.Sprintf("[filter \"hostile\"]\n\tsmudge = touch %s; cat\n\tclean = cat\n\trequired = true\n", marker)
	if e := os.WriteFile(config, []byte(content), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	got, e := f.recover()
	if e != nil || got.Outcome != "admitted" {
		t.Fatalf("%+v %v", got, e)
	}
	if _, e := os.Stat(marker); !os.IsNotExist(e) {
		t.Fatal("candidate invoked ambient host filter during read-only recovery")
	}
	if b, e := os.ReadFile(filepath.Join(got.CandidateDir, "README.md")); e != nil || string(b) != "retained candidate\n" {
		t.Fatal("materialized bytes changed")
	}
}

func TestGoalRecoveryCannotDiscardHistoricallyVerifiedMain(t *testing.T) {
	f := newRecoveryFixture(t)
	f.candidate()
	// Controller launched at B, then verified against M1. Main is now back at B.
	// Even a separately authorized deadline must not erase M1's obligations.
	root := f.req.RepoRoot
	goalTestWrite(t, root, "accepted-new-contract.txt", "M1 obligation\n")
	goalTestGit(t, root, "add", ".")
	tree := goalTestGit(t, root, "write-tree")
	m1 := goalTestGit(t, root, "commit-tree", tree, "-p", f.binding.Base, "-m", "accepted M1")
	goalTestGit(t, root, "reset", "--hard", f.binding.Base)
	var intent GoalPublicationIntent
	if err := json.Unmarshal(f.files["goal-publication/"+goalIntentName], &intent); err != nil {
		t.Fatal(err)
	}
	intent.VerificationBaseSHA = m1
	f.files["goal-publication/"+goalIntentName] = recoveryJSON(t, intent)
	f.req.AuthorizeNewBoundedExecution = true
	f.req.NewExecutionDeadline = f.now.Add(2 * time.Hour)
	f.pack()
	got, e := f.recover()
	if e == nil || got.Outcome != "blocked" || !strings.Contains(e.Error(), "historically verified accepted baseline obligations") {
		t.Fatalf("%+v %v", got, e)
	}
}

func TestGoalRecoveryPreservesAdoptedSourceAcrossFreshFailedExecutions(t *testing.T) {
	f := newRecoveryFixture(t)
	f.candidate()
	originalReceipt := append([]byte(nil), f.files["goal-publication/"+goalIntentName]...)
	originalBundle := append([]byte(nil), f.files["goal-publication/"+goalBundleName]...)
	originalDeadline := f.binding.Deadline
	f.now = f.now.Add(2 * time.Hour)
	f.req.AuthorizeNewBoundedExecution = true
	f.req.NewExecutionDeadline = f.now.Add(time.Hour)
	first, e := f.recover()
	if e != nil || first.AdoptedEvidenceDir == "" {
		t.Fatalf("%+v %v", first, e)
	}
	for _, name := range []string{goalIntentName, goalBundleName} {
		b, e := os.ReadFile(filepath.Join(first.AdoptedEvidenceDir, name))
		if e != nil {
			t.Fatal(e)
		}
		f.files["goal-adopted-publication/"+name] = b
		delete(f.files, "goal-publication/"+name)
	}
	f.binding.Head = f.req.ExpectedHead
	f.binding.Deadline = first.ExecutionDeadline
	f.req.AuthorizeNewBoundedExecution = false
	f.req.NewExecutionDeadline = time.Time{}
	// Each subsequent trusted launch failed before any new publication. The
	// latest artifact alone still carries the exact adopted source, without a
	// predecessor-chain lookup or renewal of its original intent deadline.
	for i := 0; i < 2; i++ {
		f.req.PriorRunID++
		f.run.ID = f.req.PriorRunID
		f.binding.RunID = f.req.PriorRunID
		f.artifact.WorkflowRun.ID = f.req.PriorRunID
		f.req.OutputDir = filepath.Join(t.TempDir(), "recovery")
		f.now = f.now.Add(15 * time.Minute)
		f.files["goal-result.json"] = recoveryJSON(t, GoalFlowResult{Stage: "Goal preparation", Failure: "Native goal not started"})
		f.pack()
		got, e := f.recover()
		if e != nil || got.Classification != "retained_candidate" || !got.PriorDeadline.Equal(first.ExecutionDeadline) || !got.ExecutionDeadline.Equal(first.ExecutionDeadline) {
			t.Fatalf("iteration %d: %+v %v", i, got, e)
		}
		receipt, e := os.ReadFile(filepath.Join(got.AdoptedEvidenceDir, goalIntentName))
		if e != nil || !bytes.Equal(receipt, originalReceipt) {
			t.Fatal("historical receipt was rewritten")
		}
		bundle, e := os.ReadFile(filepath.Join(got.AdoptedEvidenceDir, goalBundleName))
		if e != nil || !bytes.Equal(bundle, originalBundle) {
			t.Fatal("adopted candidate bytes changed")
		}
		var intent GoalPublicationIntent
		if e = json.Unmarshal(receipt, &intent); e != nil || !intent.Deadline.Equal(originalDeadline) {
			t.Fatal("original publication deadline was renewed")
		}
	}
}

func TestGoalRecoveryActiveIntentNeverFallsBackToAdoptedSource(t *testing.T) {
	for _, name := range []string{"malformed active", "incomplete active", "unresolved active"} {
		t.Run(name, func(t *testing.T) {
			f := newRecoveryFixture(t)
			f.candidate()
			f.binding.Head = f.req.ExpectedHead
			for _, file := range []string{goalIntentName, goalBundleName} {
				f.files["goal-adopted-publication/"+file] = f.files["goal-publication/"+file]
			}
			switch name {
			case "malformed active":
				f.files["goal-publication/"+goalIntentName] = []byte("{")
			case "incomplete active":
				delete(f.files, "goal-publication/"+goalIntentName)
			case "unresolved active":
				var intent GoalPublicationIntent
				_ = json.Unmarshal(f.files["goal-publication/"+goalIntentName], &intent)
				intent.PreviousHead = f.req.ExpectedHead
				intent.IntendedHead = oldSHA
				f.files["goal-publication/"+goalIntentName] = recoveryJSON(t, intent)
			}
			f.pack()
			got, e := f.recover()
			if e == nil || got.Outcome != "blocked" {
				t.Fatalf("%+v %v", got, e)
			}
		})
	}
}

func TestGoalRecoveryCannotReturnAnExpiredAdmission(t *testing.T) {
	f := newRecoveryFixture(t)
	f.binding.Deadline = f.now.Add(time.Nanosecond)
	f.pack()
	got, e := f.recover()
	if e == nil || got.Outcome != "blocked" || !strings.Contains(e.Error(), "expired during recovery admission") {
		t.Fatalf("%+v %v", got, e)
	}
}

func TestGoalRecoveryKnownPublicationIntentCannotDisappear(t *testing.T) {
	for _, name := range []string{"missing both with adopted source", "missing both at empty seed", "stale active pair", "invalid marker"} {
		t.Run(name, func(t *testing.T) {
			f := newRecoveryFixture(t)
			marker := oldSHA
			switch name {
			case "missing both with adopted source":
				f.candidate()
				f.binding.Head = f.req.ExpectedHead
				for _, file := range []string{goalIntentName, goalBundleName} {
					f.files["goal-adopted-publication/"+file] = f.files["goal-publication/"+file]
					delete(f.files, "goal-publication/"+file)
				}
			case "stale active pair":
				f.candidate()
			case "invalid marker":
				marker = "not-an-exact-head"
			}
			f.files["goal-result.json"] = recoveryJSON(t, GoalFlowResult{Stage: "Draft publication", Failure: "publication incomplete", PublicationIntentHead: marker})
			f.pack()
			got, e := f.recover()
			if e == nil || got.Outcome != "blocked" || !strings.Contains(e.Error(), "intent") {
				t.Fatalf("%+v %v", got, e)
			}
		})
	}
}

func TestGoalRecoveryAcceptsMatchingRetainedIntentMarker(t *testing.T) {
	f := newRecoveryFixture(t)
	f.candidate()
	f.files["goal-result.json"] = recoveryJSON(t, GoalFlowResult{Stage: "Draft publication", Failure: "confirmation lost", PublicationIntentHead: f.req.ExpectedHead})
	f.pack()
	got, e := f.recover()
	if e != nil || got.Classification != "retained_candidate" {
		t.Fatalf("%+v %v", got, e)
	}
}
