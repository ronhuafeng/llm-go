package protocolsync

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/codexexec"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestVerifierRolesDoNotSelfAcceptOrResetBudget(t *testing.T) {
	for _, mode := range []string{"proposal", "empty", "review-reject", "same-session", "scope", "proof-failed", "tamper", "proof-feedback"} {
		t.Run(mode, func(t *testing.T) {
			req := repairFixture(t)
			writeFile(t, filepath.Join(req.RepoRoot, ".gitignore"), "codexsdk/.cache/\n")
			if e := runGit(req.RepoRoot, "add", ".gitignore"); e != nil {
				t.Fatal(e)
			}
			if e := runGit(req.RepoRoot, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture cache ignore"); e != nil {
				t.Fatal(e)
			}
			req.MaxRounds = 2
			if mode == "proof-feedback" {
				req.MaxRounds = 3
			}
			req.Executor = repairExecutorFixture{run: func(context.Context, string, string) (codexexec.Result, error) {
				writeFile(t, filepath.Join(req.RepoRoot, "codexsdk/internal/protocolgen/fixture.go"), "package protocolgen\n")
				return codexexec.Result{Status: "completed", SessionID: "upgrade"}, nil
			}}
			req.Oracle = func(context.Context, ResumeRequest) (SyncResult, []byte, error) {
				return SyncResult{Stage: "verification"}, nil, errors.New("concrete failure")
			}
			proofCalls, reviewCalls := 0, 0
			req.Verifier = &VerifierRepairConfig{OriginRunID: 123, OriginAttempt: 1, OutputDir: filepath.Join(t.TempDir(), "verifier"), Proof: func(_ context.Context, r verifierProofRequest) (verifierProofEvidence, error) {
				proofCalls++
				if mode == "proof-feedback" && proofCalls == 1 {
					return verifierProofEvidence{}, verifierFeedback(errors.New("fix fixture compile"))
				}
				if mode == "proof-failed" {
					return verifierProofEvidence{}, errors.New("fixed proof failed")
				}
				cases, _ := json.Marshal(r.Cases)
				return verifierProofEvidence{Passed: true, CandidateSHA256: r.CandidateSHA256, ProposalSHA256: r.ProposalSHA256, PatchDigest: repairDigest(r.Patch), CasesDigest: repairDigest(cases), SourceDigest: repairDigest([]byte("fixture trusted source")), Lanes: []verifierProofLane{{Case: "fixture", Lane: "new-good", Started: true, Passed: true}}}, nil
			}}
			req.Verifier.Actor = func(_ context.Context, root string, readonly bool) (VerifierActor, func(), error) {
				return repairExecutorFixture{run: func(_ context.Context, prompt, resumed string) (codexexec.Result, error) {
					final := filepath.Join(t.TempDir(), "result.json")
					if !readonly {
						if mode == "proof-feedback" && proofCalls == 1 && (resumed != "author" || !strings.Contains(prompt, "fix fixture compile")) {
							t.Fatal("verifier feedback lost session or concrete cause")
						}
						if mode != "empty" {
							name := "codexsdk/internal/schemaoracle/repair.go"
							if mode == "scope" {
								name = "codexsdk/internal/protocolsync/repair.go"
							}
							writeFile(t, filepath.Join(root, name), "package schemaoracle\n")
							writeFile(t, filepath.Join(root, "codexsdk/.cache/verifier-proof/cases.json"), "[]")
						}
						writeFile(t, final, "Implementation defect remains; no verifier change justified.")
						return codexexec.Result{Status: "completed", SessionID: "author", FinalMessagePath: final}, nil
					}
					reviewCalls++
					digest := regexp.MustCompile(`proof digest ([0-9a-f]{64})`).FindStringSubmatch(prompt)
					if len(digest) != 2 {
						t.Fatal("review lacks exact executed proof")
					}
					b, _ := json.Marshal(map[string]any{"approved": mode != "review-reject", "proof_sha256": digest[1], "reason": "fixture review"})
					writeFile(t, final, string(b))
					if mode == "tamper" {
						writeFile(t, filepath.Join(root, "codexsdk/internal/schemaoracle/repair.go"), "package schemaoracle // changed\n")
					}
					id := "reviewer"
					if mode == "same-session" {
						id = "author"
					}
					return codexexec.Result{Status: "completed", SessionID: id, FinalMessagePath: final}, nil
				}}, func() {}, nil
			}
			result, err := Repair(context.Background(), req)
			cp := readRepairCheckpoint(t, req.CheckpointDir)
			if cp.Round != req.MaxRounds || cp.MaxRounds != req.MaxRounds {
				t.Fatalf("budget reset: %+v", cp)
			}
			if mode == "proposal" || mode == "proof-feedback" {
				if err != nil || result.Outcome != OutcomeVerifierProposal || cp.VerifierHandoffs != 1 || proofCalls != req.MaxRounds-1 || reviewCalls != 1 {
					t.Fatalf("result=%+v cp=%+v err=%v", result, cp, err)
				}
				if _, e := os.Stat(filepath.Join(req.RepoRoot, "codexsdk/internal/schemaoracle/repair.go")); !os.IsNotExist(e) {
					t.Fatal("verifier was hot-loaded into upgrade workspace")
				}
			} else if err == nil || result.Outcome == OutcomeApplied || result.Outcome == OutcomeVerifierProposal {
				t.Fatalf("unsafe acceptance: %+v %v", result, err)
			}
			if mode == "empty" && (proofCalls != 0 || reviewCalls != 0 || cp.Status != "exhausted") {
				t.Fatalf("empty proposal bypassed budget: %+v", cp)
			}
		})
	}
}
func TestVerifierScopeKeepsControlAndUpgradeSeparate(t *testing.T) {
	for _, p := range []string{"codexsdk/internal/schemaoracle/oracle.go", "codexsdk/internal/schemaoracle/oracle_test.go", "codexsdk/internal/schemaoracle/testdata/verifier-proof/good.go"} {
		if e := validatePaths([]string{p}, "verifier"); e != nil {
			t.Fatal(e)
		}
		if e := validatePaths([]string{p}, "agent"); e == nil {
			t.Fatal("upgrade may alter verifier")
		}
		if e := validatePaths([]string{p}, "final"); e == nil {
			t.Fatal("verifier entered ordinary sync publication")
		}
	}
	for _, p := range []string{"codexsdk/internal/protocolsync/repair.go", ".github/workflows/auto-forward.yml", "docs/protocol-sync.md", "go.mod", "codexsdk/internal/schemaoracle/../../protocolsync/publish.go", "codexsdk/internal/schemaoracle/testdata/verifier-proof/go.mod"} {
		if e := validatePaths([]string{p}, "verifier"); e == nil {
			t.Fatal("verifier escaped scope: " + p)
		}
	}
}
func TestVerifierStrictResponseDecode(t *testing.T) {
	for _, input := range []string{`{} {}`, `{"unknown":true}`, `{"approved":true} garbage`} {
		var v struct {
			Approved bool `json:"approved"`
		}
		if verifierDecode([]byte(input), &v) == nil {
			t.Fatal("ambiguous review accepted " + strings.TrimSpace(input))
		}
	}
}

func TestContinuedRepairRechecksBeforeAnyModel(t *testing.T) {
	for _, mode := range []string{"accepted", "rejected", "expired"} {
		t.Run(mode, func(t *testing.T) {
			req := repairFixture(t)
			req.ConsumedRounds = req.MaxRounds
			req.VerifyBeforeModel = true
			req.VerifierHandoffs = 1
			req.DeadlineUTC = time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
			if mode == "expired" {
				req.DeadlineUTC = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
			}
			req.Executor = repairExecutorFixture{preflight: errors.New("must not preflight exhausted continuation"), run: func(context.Context, string, string) (codexexec.Result, error) {
				t.Fatal("model called without remaining budget")
				return codexexec.Result{}, nil
			}}
			calls := 0
			req.Oracle = func(ctx context.Context, _ ResumeRequest) (SyncResult, []byte, error) {
				calls++
				d, ok := ctx.Deadline()
				if !ok || d.After(time.Now().Add(2*time.Minute)) {
					t.Fatal("deadline reset")
				}
				if mode == "accepted" {
					return SyncResult{Outcome: OutcomeApplied}, nil, nil
				}
				return SyncResult{Stage: "verification"}, nil, errors.New("still rejected")
			}
			result, e := Repair(context.Background(), req)
			if mode == "accepted" {
				if e != nil || result.Outcome != OutcomeApplied || calls != 1 {
					t.Fatalf("%+v %v calls=%d", result, e, calls)
				}
			} else if e == nil || result.Outcome == OutcomeApplied {
				t.Fatalf("unsafe continuation %+v %v", result, e)
			}
			if mode == "expired" && calls != 0 {
				t.Fatal("expired continuation ran oracle")
			}
		})
	}
}
