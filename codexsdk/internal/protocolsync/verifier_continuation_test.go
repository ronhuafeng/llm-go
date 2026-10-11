package protocolsync

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/codexexec"
)

// Retain a genuine handwritten patch, rather than the arbitrary-byte publication
// fixture. Its paths are unchanged by the separate verifier-only commit.
func verifierContinuationFixture(t *testing.T) (*verifierPublicationFixture, VerifierContinuationRequest) {
	t.Helper()
	f := newVerifierPublicationFixture(t)
	writeFile(t, filepath.Join(f.req.RepoRoot, "codexsdk/internal/protocolgen/continued.go"), "package protocolgen\nconst continuationFixture = true\n")
	patch, err := repairPatch(f.req.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	gitMust(t, f.req.RepoRoot, "reset", "--hard", "HEAD")
	f.handoff.ProposalSHA256 = repairDigest(patch)
	f.checkpoint["proposal.patch"] = patch
	var cp map[string]any
	if err = json.Unmarshal(f.checkpoint["checkpoint.json"], &cp); err != nil {
		t.Fatal(err)
	}
	cp["proposal_sha256"] = f.handoff.ProposalSHA256
	f.checkpoint["checkpoint.json"] = verifierFixtureJSON(t, cp)
	f.sealEvidence()
	pub := f.publish()
	req := f.merge(pub)
	return f, req
}
func preparedVerifierContinuationFixture(t *testing.T) (*verifierPublicationFixture, RepairRequest, string, VerifierContinuation) {
	t.Helper()
	f, request := verifierContinuationFixture(t)
	c, result, err := PrepareVerifierContinuation(request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeSemanticUnresolved || result.CandidateDir == "" || result.Publication == nil || result.Publication.Head != "absent" {
		t.Fatalf("continued data was mislabeled as proof: %+v", result)
	}
	repair := RepairRequest{ResumeRequest: ResumeRequest{RepoRoot: f.req.RepoRoot, ModuleRoot: filepath.Join(f.req.RepoRoot, "codexsdk"), CandidateDir: c.CandidateDir, CandidateSHA256: c.Publication.Handoff.CandidateSHA256, TargetRef: c.Publication.Handoff.Target.RefName, TargetKind: c.Publication.Handoff.Target.RefKind, TargetSHA: c.Publication.Handoff.Target.PeeledCommitSHA}, CheckpointDir: filepath.Join(t.TempDir(), "next-checkpoint"), MaxRounds: 12, ConsumedRounds: 0, Verifier: &VerifierRepairConfig{OriginRunID: 999}}
	return f, repair, filepath.Join(request.OutputDir, "continuation.json"), c
}

func TestBindVerifierContinuationCarriesBudgetAndRequiresFreshProof(t *testing.T) {
	_, req, receipt, c := preparedVerifierContinuationFixture(t)
	if err := BindVerifierContinuation(&req, receipt); err != nil {
		t.Fatal(err)
	}
	h := c.Publication.Handoff
	if req.MaxRounds != h.MaxRounds || req.ConsumedRounds != h.RoundsUsed || req.VerifierHandoffs != 1 || !req.VerifyBeforeModel || req.Verifier != nil || req.DeadlineUTC != h.DeadlineUTC {
		t.Fatalf("continued request reset authority or limits: %+v", req)
	}
	if baseline, err := gitBaselineIdentity(req.RepoRoot, c.MainSHA); err != nil || baseline.SourceCommit != oldSHA {
		t.Fatalf("verifier continuation changed accepted upstream: %+v %v", baseline, err)
	}
}

func TestBindVerifierContinuationRejectsChangedIdentityAndBytes(t *testing.T) {
	for _, mode := range []string{"head", "target-ref", "target-kind", "target-sha", "candidate-digest", "candidate-bytes", "proposal", "proposal-scope", "exhausted-overrun", "expired", "malformed-deadline", "duplicate", "handoff-count"} {
		t.Run(mode, func(t *testing.T) {
			_, req, receipt, c := preparedVerifierContinuationFixture(t)
			rewrite := false
			switch mode {
			case "head":
				runGitInitCommit(t, req.RepoRoot, "unexpected proposal commit")
			case "target-ref":
				req.TargetRef = "rust-v0.999.0"
			case "target-kind":
				req.TargetKind = KindManualRef
			case "target-sha":
				req.TargetSHA = oldSHA
			case "candidate-digest":
				req.CandidateSHA256 = strings.Repeat("a", 64)
			case "candidate-bytes":
				writeFile(t, filepath.Join(req.CandidateDir, "common.rs"), "mutated candidate")
			case "proposal":
				writeFile(t, filepath.Join(req.RepoRoot, "codexsdk/internal/protocolgen/continued.go"), "package protocolgen\nconst continuationFixture = false\n")
			case "proposal-scope":
				writeFile(t, filepath.Join(req.RepoRoot, "README.md"), "unapproved accepted-contract edit")
				patch, err := repairPatch(req.RepoRoot)
				if err != nil {
					t.Fatal(err)
				}
				c.Publication.Handoff.ProposalSHA256 = repairDigest(patch)
				rewrite = true
			case "exhausted-overrun":
				c.Publication.Handoff.RoundsUsed = c.Publication.Handoff.MaxRounds + 1
				rewrite = true
			case "expired":
				c.Publication.Handoff.DeadlineUTC = "2000-01-01T00:00:00Z"
				rewrite = true
			case "malformed-deadline":
				c.Publication.Handoff.DeadlineUTC = "not-a-deadline"
				rewrite = true
			case "duplicate":
				c.Duplicate = true
				rewrite = true
			case "handoff-count":
				c.Publication.Handoff.Handoffs = 2
				rewrite = true
			}
			if rewrite {
				if err := verifierJSON(receipt, c); err != nil {
					t.Fatal(err)
				}
			}
			before := req
			if err := BindVerifierContinuation(&req, receipt); err == nil {
				t.Fatal("changed continuation accepted")
			}
			if !reflect.DeepEqual(req, before) {
				t.Fatal("rejected continuation changed repair authority or budget")
			}
		})
	}
}

func TestBindVerifierContinuationRejectsWorkspaceReceiptAndParentSymlink(t *testing.T) {
	for _, mode := range []string{"inside", "parent-symlink", "file-symlink"} {
		t.Run(mode, func(t *testing.T) {
			_, req, receipt, _ := preparedVerifierContinuationFixture(t)
			raw, err := os.ReadFile(receipt)
			if err != nil {
				t.Fatal(err)
			}
			ignored := filepath.Join(req.RepoRoot, ".local-receipts")
			if err = os.Mkdir(ignored, 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(req.RepoRoot, ".git/info/exclude"), []byte(".local-receipts/\n"), 0600); err != nil {
				t.Fatal(err)
			}
			inside := filepath.Join(ignored, "continuation.json")
			if err = os.WriteFile(inside, raw, 0600); err != nil {
				t.Fatal(err)
			}
			candidate := inside
			if mode == "parent-symlink" {
				alias := filepath.Join(t.TempDir(), "outside-alias")
				if err = os.Symlink(ignored, alias); err != nil {
					t.Fatal(err)
				}
				candidate = filepath.Join(alias, "continuation.json")
			}
			if mode == "file-symlink" {
				candidate = filepath.Join(t.TempDir(), "receipt.json")
				if err = os.Symlink(inside, candidate); err != nil {
					t.Fatal(err)
				}
			}
			before := req
			if err = BindVerifierContinuation(&req, candidate); err == nil {
				t.Fatal("model-writable receipt accepted as external authority")
			}
			if !reflect.DeepEqual(req, before) {
				t.Fatal("receipt-scope rejection changed inherited request")
			}
		})
	}
}

func TestPrepareVerifierContinuationRejectsWrongCheckoutBeforeApplying(t *testing.T) {
	for _, mode := range []string{"old-base", "dirty", "no-output", "expired"} {
		t.Run(mode, func(t *testing.T) {
			f, req := verifierContinuationFixture(t)
			switch mode {
			case "old-base":
				gitMust(t, f.req.RepoRoot, "checkout", "--detach", f.handoff.BaseSHA)
			case "dirty":
				writeFile(t, filepath.Join(f.req.RepoRoot, "README.md"), "unrelated local work")
			case "no-output":
				req.OutputDir = ""
			case "expired":
				req.Now = func() time.Time { return time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC) }
			}
			before := strings.TrimSpace(gitMust(t, f.req.RepoRoot, "rev-parse", "HEAD"))
			if _, _, err := PrepareVerifierContinuation(req); err == nil {
				t.Fatal("invalid prepare was accepted")
			}
			if after := strings.TrimSpace(gitMust(t, f.req.RepoRoot, "rev-parse", "HEAD")); after != before {
				t.Fatal("rejected prepare changed Git identity")
			}
			if _, err := os.Stat(filepath.Join(f.req.RepoRoot, "codexsdk/internal/protocolgen/continued.go")); !os.IsNotExist(err) {
				t.Fatal("invalid prepare materialized proposal")
			}
		})
	}
}

func TestPrepareVerifierContinuationUsesOnlyReadAuthority(t *testing.T) {
	f, req := verifierContinuationFixture(t)
	effects := &verifierEffectOnlyFixture{target: f}
	req.ReadAPI = f
	req.API = effects
	if _, result, err := PrepareVerifierContinuation(req); err != nil || result.Outcome != OutcomeSemanticUnresolved {
		t.Fatalf("read-only continuation used effect API: %+v %v", result, err)
	}
	if effects.writes != 0 {
		t.Fatal("continuation performed an external write")
	}
}

func TestPrepareVerifierContinuationDuplicateDoesNotRestoreOrApply(t *testing.T) {
	f, req := verifierContinuationFixture(t)
	prior := f.runs[303]
	prior.ID = 302
	f.continuations = append(f.continuations, prior)
	c, result, err := PrepareVerifierContinuation(req)
	if err != nil || !c.Duplicate || result.Outcome != "continuation_duplicate" {
		t.Fatalf("duplicate=%+v result=%+v err=%v", c, result, err)
	}
	if _, err = os.Stat(req.OutputDir); !os.IsNotExist(err) {
		t.Fatal("duplicate wrote a receipt or checkpoint")
	}
	if err = AssertClean(f.req.RepoRoot); err != nil {
		t.Fatal("duplicate changed proposal workspace")
	}
}

type continuationNoModelExecutor struct{ t *testing.T }

func (f continuationNoModelExecutor) Preflight(context.Context) error {
	f.t.Fatal("exhausted continuation checked model prerequisites")
	return nil
}
func (f continuationNoModelExecutor) Run(context.Context, string, string) (codexexec.Result, error) {
	f.t.Fatal("exhausted continuation invoked model")
	return codexexec.Result{}, nil
}

func TestBoundFinalRoundContinuationRunsFreshOracleWithoutModel(t *testing.T) {
	for _, success := range []bool{true, false} {
		t.Run(map[bool]string{true: "fresh-proof", false: "still-rejected"}[success], func(t *testing.T) {
			_, req, receipt, c := preparedVerifierContinuationFixture(t)
			c.Publication.Handoff.RoundsUsed = c.Publication.Handoff.MaxRounds
			c.RemainingRounds = 0
			if err := verifierJSON(receipt, c); err != nil {
				t.Fatal(err)
			}
			if err := BindVerifierContinuation(&req, receipt); err != nil {
				t.Fatal(err)
			}
			req.Executor = continuationNoModelExecutor{t: t}
			proofs := 0
			req.Oracle = func(context.Context, ResumeRequest) (SyncResult, []byte, error) {
				proofs++
				if success {
					return SyncResult{Outcome: OutcomeApplied}, nil, nil
				}
				return SyncResult{Stage: "verification"}, nil, errors.New("fresh verifier rejects retained implementation")
			}
			result, err := Repair(context.Background(), req)
			if proofs != 1 {
				t.Fatalf("fresh proof runs=%d", proofs)
			}
			if success {
				if err != nil || result.Outcome != OutcomeApplied {
					t.Fatalf("fresh proof rejected: %+v %v", result, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "budget exhausted") {
				t.Fatalf("exhausted rejected proposal reset budget: %+v %v", result, err)
			}
			if cp := readRepairCheckpoint(t, req.CheckpointDir); cp.Round != c.Publication.Handoff.MaxRounds || cp.VerifierHandoffs != 1 {
				t.Fatalf("checkpoint lost original consumption: %+v", cp)
			}
		})
	}
}

type continuationTombstoneAPI struct {
	fixture *verifierPublicationFixture
	closed  publicationPR
}

func (api continuationTombstoneAPI) Request(method, path string, body, result any) error {
	if method == "GET" && strings.Contains(path, "/pulls?") {
		return json.Unmarshal(verifierFixtureJSON(api.fixture.t, append([]publicationPR{api.fixture.publication.prs[0]}, api.closed)), result)
	}
	return api.fixture.Request(method, path, body, result)
}
func TestPrepareVerifierContinuationPreservesClosedSyncTombstone(t *testing.T) {
	f, req := verifierContinuationFixture(t)
	main := req.ExpectedHead
	metadata := filepath.Join(f.req.RepoRoot, "codexsdk", filepath.FromSlash(defaultBaselineRel), "baseline_metadata.json")
	writeFile(t, metadata, string(verifierFixtureJSON(t, map[string]string{"source_commit": newSHA, "source_ref_name": "rust-v0.141.0", "source_ref_kind": KindStableTag})))
	runGitInitCommit(t, f.req.RepoRoot, "previous closed ordinary sync candidate")
	head := strings.TrimSpace(gitMust(t, f.req.RepoRoot, "rev-parse", "HEAD"))
	gitMust(t, f.req.RepoRoot, "checkout", "--detach", main)
	closed := publicationPR{Number: 8, State: "closed", User: publicationUser{ID: 42, Type: "Bot"}}
	closed.Head.Ref = "codex/sync-upstream-closed"
	closed.Head.SHA = head
	closed.Head.Repo.FullName = f.req.Repository
	closed.Base.Ref = "main"
	closed.Base.SHA = main
	req.API = continuationTombstoneAPI{fixture: f, closed: closed}
	req.ReadAPI = req.API
	if _, _, err := PrepareVerifierContinuation(req); err == nil || !strings.Contains(err.Error(), "was closed") {
		t.Fatalf("continuation bypassed ordinary sync tombstone: %v", err)
	}
	if err := AssertClean(f.req.RepoRoot); err != nil {
		t.Fatal("closed sync candidate caused proposal replay")
	}
}
