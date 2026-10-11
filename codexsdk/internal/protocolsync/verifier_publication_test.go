package protocolsync

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

type verifierPublicationFixture struct {
	t                 *testing.T
	publication       *publicationFixture
	req               VerifierPublicationRequest
	handoff           VerifierHandoff
	files, checkpoint map[string][]byte
	archives          map[int64][]byte
	artifacts         []verifierArtifact
	runs              map[int64]verifierNativeRun
	continuations     []verifierNativeRun
	pages             int
	failRead          bool
}

func verifierFixtureJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func verifierZIP(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	var names []string
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		f, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func newVerifierPublicationFixture(t *testing.T) *verifierPublicationFixture {
	t.Helper()
	root := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	writeFile(t, filepath.Join(root, "codexsdk/internal/schemaoracle/oracle.go"), "package schemaoracle\nconst accepted = false\n")
	runGitInitCommit(t, root, "accepted verifier")
	base := strings.TrimSpace(gitMust(t, root, "rev-parse", "HEAD"))
	remote := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "clone", "--bare", root, remote).CombinedOutput(); err != nil {
		t.Fatalf("clone %v %s", err, out)
	}
	gitMust(t, root, "remote", "add", "origin", remote)
	writeFile(t, filepath.Join(root, "codexsdk/internal/schemaoracle/oracle.go"), "package schemaoracle\nconst accepted = true\n")
	writeFile(t, filepath.Join(root, "codexsdk/internal/schemaoracle/repaired_test.go"), "package schemaoracle\n")
	runGitInitCommit(t, root, "verifier proposal")
	head := strings.TrimSpace(gitMust(t, root, "rev-parse", "HEAD"))
	tree := strings.TrimSpace(gitMust(t, root, "rev-parse", "HEAD^{tree}"))
	patch, err := verifierGitPatch(root, base, head)
	if err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(t.TempDir(), "candidate")
	writeCandidateFixture(t, candidate, newSHA)
	digest, err := candidateDigest(candidate)
	if err != nil {
		t.Fatal(err)
	}
	h := VerifierHandoff{Version: 1, DeadlineUTC: "2099-01-01T00:00:00Z", OriginRunID: 101, OriginAttempt: 1, BaseSHA: base, Target: Target{RefName: "rust-v0.141.0", RefKind: KindStableTag, PeeledCommitSHA: newSHA}, CandidateSHA256: digest, ProposalSHA256: repairDigest([]byte("retained upgrade proposal")), VerifierPatchSHA256: repairDigest(patch), ReviewedTree: tree, RoundsUsed: 2, MaxRounds: 6, Handoffs: 1}
	f := &verifierPublicationFixture{t: t, handoff: h, files: map[string][]byte{"verifier.patch": patch, "cases.json": []byte(`[{"name":"fixture"}]`)}, checkpoint: map[string][]byte{}, archives: map[int64][]byte{}, runs: map[int64]verifierNativeRun{}}
	f.publication = &publicationFixture{t: t, repo: root, remote: remote, branch: verifierBranch(h), actor: publicationUser{ID: 42, Type: "Bot", Login: "sync[bot]"}}
	f.req = VerifierPublicationRequest{RepoRoot: root, Repository: "owner/repo", BaseBranch: "main", Remote: "origin", AppBotID: 42, OriginRunID: 101, OriginAttempt: 1, API: f, LoadArtifact: func(id int64) ([]byte, error) {
		raw, ok := f.archives[id]
		if !ok {
			return nil, fmt.Errorf("missing fixture artifact")
		}
		return raw, nil
	}, Lookuper: fakeLookuper{byPattern: map[string]string{"refs/tags/rust-v0.141.0": newSHA + "\trefs/tags/rust-v0.141.0", "refs/tags/rust-v0.141.0^{}": newSHA + "\trefs/tags/rust-v0.141.0^{}"}}}
	origin := verifierNativeRun{ID: 101, RunAttempt: 1, Path: verifierWorkflow, Event: "workflow_dispatch", HeadSHA: base, HeadBranch: "main", Status: "completed", Conclusion: "success"}
	origin.Repository.FullName = "owner/repo"
	origin.HeadRepository.FullName = "owner/repo"
	f.runs[101] = origin
	f.checkpoint["checkpoint.json"] = verifierFixtureJSON(t, map[string]any{"version": 1, "status": "verifier_proposed", "deadline_utc": h.DeadlineUTC, "base": base, "target": h.Target, "candidate_sha256": h.CandidateSHA256, "proposal_sha256": h.ProposalSHA256, "round": 2, "max_rounds": 6, "verifier_handoffs": 1})
	f.checkpoint["proposal.patch"] = []byte("retained upgrade proposal")
	if err = filepath.WalkDir(candidate, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(candidate, name)
		raw, e := os.ReadFile(name)
		if e != nil {
			return e
		}
		f.checkpoint["candidate/"+filepath.ToSlash(rel)] = raw
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.sealEvidence()
	return f
}
func (f *verifierPublicationFixture) sealEvidence() {
	h := &f.handoff
	proof := map[string]any{"passed": true, "candidate_sha256": h.CandidateSHA256, "proposal_sha256": h.ProposalSHA256, "verifier_patch_sha256": h.VerifierPatchSHA256, "cases_sha256": repairDigest(f.files["cases.json"]), "source_sha256": strings.Repeat("c", 64), "lanes": []any{map[string]any{"case": "fixture", "passed": true}}}
	f.files["proof.json"] = verifierFixtureJSON(f.t, proof)
	h.ProofSHA256 = repairDigest(f.files["proof.json"])
	review := VerifierReviewEvidence{Approved: true, AuthorSessionID: "author", ReviewerSessionID: "reviewer", CandidateSHA256: h.CandidateSHA256, ProposalSHA256: h.ProposalSHA256, VerifierPatchSHA256: h.VerifierPatchSHA256, ProofSHA256: h.ProofSHA256, ReviewedTree: h.ReviewedTree, Reason: "Bounded source and negative fixtures were independently reviewed."}
	f.files["review.json"] = verifierFixtureJSON(f.t, review)
	h.ReviewSHA256 = repairDigest(f.files["review.json"])
	f.rearchive()
}
func (f *verifierPublicationFixture) rearchive() {
	f.files["handoff.json"] = verifierFixtureJSON(f.t, f.handoff)
	f.archives[201] = verifierZIP(f.t, f.files)
	f.archives[202] = verifierZIP(f.t, f.checkpoint)
	f.artifacts = nil
	for _, entry := range []struct {
		id   int64
		name string
	}{{201, "protocol-sync-verifier-proposal-1"}, {202, "protocol-sync-repair-checkpoint-1"}} {
		a := verifierArtifact{ID: entry.id, Name: entry.name, Digest: "sha256:" + repairDigest(f.archives[entry.id])}
		a.WorkflowRun.ID = 101
		a.WorkflowRun.HeadSHA = f.handoff.BaseSHA
		f.artifacts = append(f.artifacts, a)
	}
}
func (f *verifierPublicationFixture) Request(method, path string, body, result any) error {
	if f.failRead && method == "GET" {
		return fmt.Errorf("native read unavailable")
	}
	var value any
	switch {
	case strings.Contains(path, "/artifacts?"):
		value = map[string]any{"total_count": len(f.artifacts), "artifacts": f.artifacts}
	case strings.Contains(path, "/actions/workflows/"):
		f.pages++
		page := 1
		if strings.Contains(path, "page=2") {
			page = 2
		}
		start := (page - 1) * 100
		end := min(start+100, len(f.continuations))
		if start > len(f.continuations) {
			return fmt.Errorf("bad page")
		}
		value = map[string]any{"total_count": len(f.continuations), "workflow_runs": f.continuations[start:end]}
	case strings.Contains(path, "/actions/runs/"):
		id, err := strconv.ParseInt(path[strings.LastIndex(path, "/")+1:], 10, 64)
		if err != nil {
			return err
		}
		run, ok := f.runs[id]
		if !ok {
			return fmt.Errorf("run missing")
		}
		value = run
	case strings.Contains(path, "/pulls/") && method == "GET" && len(f.publication.prs) == 1 && f.publication.prs[0].MergedAt != nil:
		value = f.publication.prs[0]
	default:
		return f.publication.Request(method, path, body, result)
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(verifierFixtureJSON(f.t, value), result)
}
func (f *verifierPublicationFixture) publish() VerifierPublication {
	f.t.Helper()
	result, err := PublishVerifier(f.req)
	if err != nil {
		f.t.Fatal(err)
	}
	return result
}
func (f *verifierPublicationFixture) merge(pub VerifierPublication) VerifierContinuationRequest {
	f.t.Helper()
	gitMust(f.t, f.req.RepoRoot, "push", "origin", pub.Head+":refs/heads/main")
	stamp := "2026-10-11T07:00:00Z"
	pr := &f.publication.prs[0]
	pr.State = "closed"
	pr.MergedAt = &stamp
	pr.MergeSHA = pub.Head
	pr.Head.SHA = pub.Head
	run := f.runs[101]
	run.ID = 303
	run.Event = "pull_request"
	run.HeadSHA = pub.Head
	run.Status = "in_progress"
	run.Conclusion = ""
	run.DisplayTitle = VerifierContinuationTitle(pub.Number, pub.Head)
	if err := json.Unmarshal(verifierFixtureJSON(f.t, []any{map[string]any{"number": pub.Number, "head": map[string]string{"sha": pub.Head}}}), &run.PullRequests); err != nil {
		f.t.Fatal(err)
	}
	f.runs[303] = run
	f.continuations = []verifierNativeRun{run}
	return VerifierContinuationRequest{VerifierResolveRequest: VerifierResolveRequest{VerifierPublicationRequest: f.req, Number: pub.Number, ExpectedHead: pub.Head}, ContinuationRunID: 303, OutputDir: filepath.Join(f.t.TempDir(), "restored")}
}

func TestVerifierPublicationPreservesSyncBaselineAndRecoversLostCreate(t *testing.T) {
	f := newVerifierPublicationFixture(t)
	f.publication.loseCreate = true
	pub := f.publish()
	if f.publication.creates != 1 || !strings.HasPrefix(pub.Branch, verifierBranchPrefix) {
		t.Fatal(pub)
	}
	identity, err := gitBaselineIdentity(f.req.RepoRoot, pub.Head)
	if err != nil || identity.SourceCommit != oldSHA {
		t.Fatalf("ordinary accepted baseline changed: %+v %v", identity, err)
	}
	again := f.publish()
	if again.Head != pub.Head || f.publication.creates != 1 || f.publication.updates != 0 {
		t.Fatal("verifier retry mutated publication")
	}
	f.publication.prs[0].Body += "\nOperator notes are not authority."
	if _, err = ResolveVerifier(VerifierResolveRequest{VerifierPublicationRequest: f.req, Number: pub.Number, ExpectedHead: pub.Head}); err != nil {
		t.Fatal(err)
	}
}

func TestVerifierPublicationRejectsUntrustedAndChangedEvidence(t *testing.T) {
	for _, mode := range []string{"origin-pr", "origin-branch", "origin-repository", "origin-attempt", "cancelled", "archive-digest", "review-refused", "self-review", "review-tree", "proof-failed", "cases-changed", "budget", "handoff-repeat", "extra-file", "scope", "head-changed", "source-moved", "base-moved"} {
		t.Run(mode, func(t *testing.T) {
			f := newVerifierPublicationFixture(t)
			run := f.runs[101]
			switch mode {
			case "origin-pr":
				run.Event = "pull_request"
			case "origin-branch":
				run.HeadBranch = "feature"
			case "origin-repository":
				run.HeadRepository.FullName = "other/repo"
			case "origin-attempt":
				run.RunAttempt = 2
			case "cancelled":
				run.Conclusion = "cancelled"
			case "archive-digest":
				f.archives[201] = append(f.archives[201], 0)
			case "review-refused", "self-review", "review-tree":
				var review VerifierReviewEvidence
				_ = json.Unmarshal(f.files["review.json"], &review)
				if mode == "review-refused" {
					review.Approved = false
				}
				if mode == "self-review" {
					review.ReviewerSessionID = review.AuthorSessionID
				}
				if mode == "review-tree" {
					review.ReviewedTree = strings.Repeat("b", 40)
				}
				f.files["review.json"] = verifierFixtureJSON(t, review)
				f.handoff.ReviewSHA256 = repairDigest(f.files["review.json"])
				f.rearchive()
			case "proof-failed":
				var proof map[string]any
				_ = json.Unmarshal(f.files["proof.json"], &proof)
				proof["passed"] = false
				f.files["proof.json"] = verifierFixtureJSON(t, proof)
				f.handoff.ProofSHA256 = repairDigest(f.files["proof.json"])
				f.rearchive()
			case "cases-changed":
				f.files["cases.json"] = []byte(`[]`)
				f.rearchive()
			case "budget":
				f.handoff.RoundsUsed = f.handoff.MaxRounds + 1
				f.rearchive()
			case "handoff-repeat":
				f.handoff.Handoffs = 2
				f.rearchive()
			case "extra-file":
				f.files["controller.sh"] = []byte("evil")
				f.rearchive()
			case "scope":
				writeFile(t, filepath.Join(f.req.RepoRoot, "README.md"), "changed contract")
				gitMust(t, f.req.RepoRoot, "add", "README.md")
				gitMust(t, f.req.RepoRoot, "-c", "user.name=Fixture", "-c", "user.email=f@example.com", "commit", "--amend", "--no-edit")
				head := strings.TrimSpace(gitMust(t, f.req.RepoRoot, "rev-parse", "HEAD"))
				patch, _ := verifierGitPatch(f.req.RepoRoot, f.handoff.BaseSHA, head)
				f.files["verifier.patch"] = patch
				f.handoff.VerifierPatchSHA256 = repairDigest(patch)
				f.handoff.ReviewedTree = strings.TrimSpace(gitMust(t, f.req.RepoRoot, "rev-parse", "HEAD^{tree}"))
				f.sealEvidence()
			case "head-changed":
				writeFile(t, filepath.Join(f.req.RepoRoot, "codexsdk/internal/schemaoracle/other.go"), "package schemaoracle\n")
				runGitInitCommit(t, f.req.RepoRoot, "new head")
			case "source-moved":
				f.req.Lookuper = fakeLookuper{byPattern: map[string]string{"refs/tags/rust-v0.141.0": oldSHA + "\trefs/tags/rust-v0.141.0", "refs/tags/rust-v0.141.0^{}": oldSHA + "\trefs/tags/rust-v0.141.0^{}"}}
			case "base-moved":
				head := strings.TrimSpace(gitMust(t, f.req.RepoRoot, "rev-parse", "HEAD"))
				gitMust(t, f.req.RepoRoot, "push", "origin", head+":refs/heads/main")
			}
			f.runs[101] = run
			if _, err := PublishVerifier(f.req); err == nil {
				t.Fatal("unsafe verifier publication accepted")
			}
			if f.publication.creates != 0 {
				t.Fatal("unsafe evidence reached PR creation")
			}
		})
	}
}

func TestVerifierMergedContinuationPreservesBudgetAndRejectsDuplicate(t *testing.T) {
	f := newVerifierPublicationFixture(t)
	pub := f.publish()
	req := f.merge(pub)
	got, err := ContinueVerifier(req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Duplicate || got.RemainingRounds != 4 || got.MainSHA != pub.Head || got.CandidateDir == "" {
		t.Fatal(got)
	}
	if err = VerifyCandidate(got.CandidateDir, f.handoff.CandidateSHA256, f.handoff.Target.RefName, f.handoff.Target.RefKind, f.handoff.Target.PeeledCommitSHA); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(got.ProposalPatchPath)
	if err != nil || repairDigest(raw) != f.handoff.ProposalSHA256 {
		t.Fatal("proposal bytes changed")
	}
	// A prior attempt is sufficient to suppress a second start, irrespective of
	// its final status. The current native run itself must not suppress first use.
	prior := f.runs[303]
	prior.ID = 302
	prior.Status = "completed"
	prior.Conclusion = "cancelled"
	for i := 0; i < 100; i++ {
		other := prior
		other.ID = int64(400 + i)
		other.DisplayTitle = "unrelated"
		f.continuations = append(f.continuations, other)
	}
	f.continuations = append(f.continuations, prior)
	req.OutputDir = filepath.Join(t.TempDir(), "must-not-exist")
	got, err = ContinueVerifier(req)
	if err != nil || !got.Duplicate || f.pages < 3 {
		t.Fatalf("duplicate=%+v err=%v pages=%d", got, err, f.pages)
	}
	if _, err = os.Stat(req.OutputDir); !os.IsNotExist(err) {
		t.Fatal("duplicate continuation materialized bytes")
	}
}

func TestVerifierContinuationRejectsStaleNativeAndCheckpointState(t *testing.T) {
	for _, mode := range []string{"creator", "not-merged", "merge-commit", "main-moved", "target-moved", "checkpoint-budget", "checkpoint-proposal", "candidate-mutated", "run-unbound", "read-failure", "existing-output"} {
		t.Run(mode, func(t *testing.T) {
			f := newVerifierPublicationFixture(t)
			pub := f.publish()
			req := f.merge(pub)
			switch mode {
			case "creator":
				f.publication.prs[0].User.ID = 99
			case "not-merged":
				f.publication.prs[0].MergedAt = nil
			case "merge-commit":
				f.publication.prs[0].MergeSHA = f.handoff.BaseSHA
			case "main-moved":
				writeFile(t, filepath.Join(f.req.RepoRoot, "README.md"), "later main")
				runGitInitCommit(t, f.req.RepoRoot, "advance main")
				gitMust(t, f.req.RepoRoot, "push", "origin", "HEAD:refs/heads/main")
			case "target-moved":
				req.Lookuper = fakeLookuper{}
			case "checkpoint-budget":
				var cp map[string]any
				_ = json.Unmarshal(f.checkpoint["checkpoint.json"], &cp)
				cp["round"] = 0
				f.checkpoint["checkpoint.json"] = verifierFixtureJSON(t, cp)
				f.rearchive()
			case "checkpoint-proposal":
				f.checkpoint["proposal.patch"] = []byte("replaced")
				f.rearchive()
			case "candidate-mutated":
				f.checkpoint["candidate/common.rs"] = []byte("changed")
				f.rearchive()
			case "run-unbound":
				run := f.runs[303]
				run.PullRequests = nil
				f.runs[303] = run
			case "read-failure":
				f.failRead = true
			case "existing-output":
				if err := os.Mkdir(req.OutputDir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := ContinueVerifier(req); err == nil {
				t.Fatal("invalid continuation accepted")
			}
		})
	}
}

func TestVerifierArchiveRejectsTraversalAndSymlink(t *testing.T) {
	for _, mode := range []string{"traversal", "symlink", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			f := newVerifierPublicationFixture(t)
			var data bytes.Buffer
			writer := zip.NewWriter(&data)
			name := "../outside"
			if mode != "traversal" {
				name = "link"
			}
			header := &zip.FileHeader{Name: name}
			if mode == "symlink" {
				header.SetMode(os.ModeSymlink | 0777)
			}
			w, err := writer.CreateHeader(header)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte("target"))
			if mode == "duplicate" {
				_, _ = writer.Create(name)
			}
			_ = writer.Close()
			f.archives[201] = data.Bytes()
			a := f.artifacts[0]
			a.Digest = "sha256:" + repairDigest(data.Bytes())
			if _, err = loadVerifierArchive(f.req, a); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

func TestVerifierContinuationIgnoresFutureQueuedEventButNotCurrentRerun(t *testing.T) {
	f := newVerifierPublicationFixture(t)
	pub := f.publish()
	req := f.merge(pub)
	future := f.runs[303]
	future.ID = 304
	future.Status = "queued"
	f.continuations = append(f.continuations, future)
	result, err := ContinueVerifier(req)
	if err != nil || result.Duplicate {
		t.Fatalf("future event suppressed first start: %+v %v", result, err)
	}
	current := f.runs[303]
	current.RunAttempt = 2
	f.runs[303] = current
	req.OutputDir = filepath.Join(t.TempDir(), "not-created")
	result, err = ContinueVerifier(req)
	if err != nil || !result.Duplicate {
		t.Fatalf("rerun reset continuation consumption: %+v %v", result, err)
	}
	if _, err = os.Stat(req.OutputDir); !os.IsNotExist(err) {
		t.Fatal("rerun restored candidate")
	}
}

func TestVerifierPublicationDoesNotReplaceForeignBranchOrReopenTombstone(t *testing.T) {
	for _, mode := range []string{"foreign-actor", "closed"} {
		t.Run(mode, func(t *testing.T) {
			f := newVerifierPublicationFixture(t)
			pub := f.publish()
			if mode == "foreign-actor" {
				f.publication.actor = publicationUser{ID: 77, Type: "User"}
			} else {
				f.publication.prs[0].State = "closed"
			}
			if _, err := PublishVerifier(f.req); err == nil {
				t.Fatal("adopted foreign or closed verifier state")
			}
			if f.publication.ref(pub.Branch) != pub.Head || f.publication.creates != 1 || f.publication.updates != 0 {
				t.Fatal("rejection mutated existing publication")
			}
		})
	}
}

func TestVerifierCompletedRunNativeFilter(t *testing.T) {
	for _, mode := range []string{"verifier", "ordinary", "wrong-path", "failed", "wrong-repo", "ambiguous", "changed-head", "fake-prefix"} {
		t.Run(mode, func(t *testing.T) {
			f := newVerifierPublicationFixture(t)
			pub := f.publish()
			run := f.runs[101]
			run.ID = 500
			run.Path = ".github/workflows/pr-verification.yml"
			run.Event = "pull_request"
			run.HeadBranch = pub.Branch
			run.HeadSHA = pub.Head
			_ = json.Unmarshal(verifierFixtureJSON(t, []any{map[string]any{"number": pub.Number, "head": map[string]string{"sha": pub.Head}}}), &run.PullRequests)
			switch mode {
			case "ordinary":
				f.req.AppBotID = 0 // unrelated runs do not need verifier setup
				run.HeadBranch = "ordinary-feature"
				run.Path = "other.yml"
				run.Conclusion = "failure"
			case "wrong-path":
				run.Path = ".github/workflows/other.yml"
			case "failed":
				run.Conclusion = "failure"
			case "wrong-repo":
				run.HeadRepository.FullName = "fork/repo"
			case "ambiguous":
				run.PullRequests = append(run.PullRequests, run.PullRequests[0])
			case "changed-head":
				run.HeadSHA = f.handoff.BaseSHA
				run.PullRequests[0].Head.SHA = run.HeadSHA
			case "fake-prefix":
				f.publication.prs[0].User.ID = 88
			}
			f.runs[500] = run
			got, found, err := ResolveVerifierCompletedRun(f.req, 500)
			if mode == "ordinary" {
				if found || err != nil {
					t.Fatalf("ordinary run failed filtering: found=%v err=%v", found, err)
				}
				return
			}
			if mode == "verifier" {
				if !found || err != nil || got.Head != pub.Head {
					t.Fatalf("valid verifier not resolved: %+v %v %v", got, found, err)
				}
				return
			}
			if !found || err == nil {
				t.Fatalf("unsafe verifier completion: found=%v err=%v", found, err)
			}
		})
	}
}

func TestVerifierFinalRoundAllowsRecheckWithoutNewModelBudget(t *testing.T) {
	f := newVerifierPublicationFixture(t)
	f.handoff.RoundsUsed = 6
	var cp map[string]any
	_ = json.Unmarshal(f.checkpoint["checkpoint.json"], &cp)
	cp["round"] = 6
	f.checkpoint["checkpoint.json"] = verifierFixtureJSON(t, cp)
	f.rearchive()
	pub := f.publish()
	req := f.merge(pub)
	result, err := ContinueVerifier(req)
	if err != nil || result.RemainingRounds != 0 {
		t.Fatalf("last verifier round lost deterministic continuation: %+v %v", result, err)
	}
}

// Native fixture patches deliberately use full indexes; the production
// publisher verifies resulting tree identity, not diff serialization.
func verifierGitPatch(root, base, head string) ([]byte, error) {
	command := exec.Command("git", "-C", root, "-c", "core.quotePath=true", "diff", "--binary", "--full-index", "--no-ext-diff", "--no-textconv", "--no-renames", base, head, "--")
	raw, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("read verifier patch: %w", err)
	}
	return raw, nil
}

func TestVerifierContinuationCannotResetOriginalDeadline(t *testing.T) {
	f := newVerifierPublicationFixture(t)
	pub := f.publish()
	req := f.merge(pub)
	req.Now = func() time.Time { return time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC) }
	if _, err := ContinueVerifier(req); err == nil || !strings.Contains(err.Error(), "deadline expired") {
		t.Fatalf("expired original window accepted: %v", err)
	}
	if _, err := os.Stat(req.OutputDir); !os.IsNotExist(err) {
		t.Fatal("expired chain materialized recovery bytes")
	}
}

type verifierEffectOnlyFixture struct {
	target PublicationAPI
	writes int
}

func (f *verifierEffectOnlyFixture) Request(method, path string, body, result any) error {
	if method == "GET" {
		return fmt.Errorf("App was incorrectly used for reading")
	}
	f.writes++
	return f.target.Request(method, path, body, result)
}
func TestVerifierPublicationSeparatesReadAndEffectAuthority(t *testing.T) {
	f := newVerifierPublicationFixture(t)
	effects := &verifierEffectOnlyFixture{target: f}
	f.req.ReadAPI = f
	f.req.API = effects
	if _, err := PublishVerifier(f.req); err != nil {
		t.Fatal(err)
	}
	if effects.writes != 1 {
		t.Fatalf("effects=%d", effects.writes)
	}
	t.Setenv("GH_TOKEN", "fixture-write-token")
	t.Setenv("GH_READ_TOKEN", "fixture-read-token")
	env := verifierReadEnvironment()
	found := false
	for _, entry := range env {
		if entry == "GH_TOKEN=fixture-read-token" {
			found = true
		}
		if entry == "GH_TOKEN=fixture-write-token" || strings.HasPrefix(entry, "GH_READ_TOKEN=") {
			t.Fatal("read child inherited write/extra token")
		}
	}
	if !found {
		t.Fatal("read child did not receive read identity")
	}
	if err := (verifierReadAPI{}).Request("POST", "ignored", nil, nil); err == nil {
		t.Fatal("read-only API allowed effect")
	}
}

func TestVerifierProducerValidatesBeforeEffectAuthority(t *testing.T) {
	f := newVerifierPublicationFixture(t)
	gitMust(t, f.req.RepoRoot, "checkout", "--detach", f.handoff.BaseSHA)
	got, err := ValidateVerifierProducer(f.req)
	if err != nil || got.ReviewedTree != f.handoff.ReviewedTree {
		t.Fatalf("producer validation: %+v %v", got, err)
	}
	if f.publication.creates != 0 || f.publication.ref(verifierBranch(f.handoff)) != "" {
		t.Fatal("read-only preflight changed remote state")
	}
	if head := strings.TrimSpace(gitMust(t, f.req.RepoRoot, "rev-parse", "HEAD")); head != f.handoff.BaseSHA {
		t.Fatal("preflight changed accepted checkout")
	}
	if err = AssertClean(f.req.RepoRoot); err != nil {
		t.Fatal(err)
	}
	f.files["verifier.patch"] = bytes.Replace(f.files["verifier.patch"], []byte("const accepted = true"), []byte("const accepted = false"), 1)
	f.handoff.VerifierPatchSHA256 = repairDigest(f.files["verifier.patch"])
	f.sealEvidence()
	if _, err = ValidateVerifierProducer(f.req); err == nil {
		t.Fatal("invalid artifact passed credential preflight")
	}
}

func TestVerifierPrepareStagesOnlyReviewedTreeBeforeCredentialEffect(t *testing.T) {
	f := newVerifierPublicationFixture(t)
	gitMust(t, f.req.RepoRoot, "checkout", "--detach", f.handoff.BaseSHA)
	h, err := PrepareVerifierPublication(f.req)
	if err != nil {
		t.Fatal(err)
	}
	if tree := strings.TrimSpace(gitMust(t, f.req.RepoRoot, "write-tree")); tree != h.ReviewedTree {
		t.Fatal("prepare staged an unreviewed tree")
	}
	if f.publication.creates != 0 || f.publication.ref(verifierBranch(h)) != "" {
		t.Fatal("preparation touched remote state")
	}
	gitMust(t, f.req.RepoRoot, "-c", "user.name=Fixture", "-c", "user.email=f@example.com", "commit", "-m", "prepared verifier")
	result, err := PublishVerifier(f.req)
	if err != nil || result.Handoff.ReviewedTree != h.ReviewedTree {
		t.Fatalf("prepared candidate not independently publishable: %+v %v", result, err)
	}
}

func TestVerifierNativeQualifiedWorkflowPath(t *testing.T) {
	f := newVerifierPublicationFixture(t)
	run := f.runs[101]
	run.Path = verifierWorkflow + "@main"
	f.runs[101] = run
	if _, err := PublishVerifier(f.req); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{verifierWorkflow + "@", verifierWorkflow + ".other@main", "other/" + verifierWorkflow + "@main"} {
		if verifierWorkflowPath(bad, verifierWorkflow) {
			t.Fatalf("accepted wrong native workflow file %q", bad)
		}
	}
}
