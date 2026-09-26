package protocolsync

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolupgrade"
)

func TestDecideAfterPolicy(t *testing.T) {
	if got := decideAfterPolicy(DecisionSkip, false); got != "current" {
		t.Fatalf("got %s", got)
	}
	if got := decideAfterPolicy(DecisionSkip, true); got != "generate" {
		t.Fatalf("got %s", got)
	}
	if got := decideAfterPolicy(DecisionAllow, false); got != "generate" {
		t.Fatalf("got %s", got)
	}
	if got := decideAfterPolicy(DecisionBlock, false); got != "blocked" {
		t.Fatalf("got %s", got)
	}
}

func TestSyncCurrentSkipsGenerate(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	generated := false
	result, err := Sync(SyncRequest{
		RepoRoot:     repo,
		ModuleRoot:   filepath.Join(repo, "codexsdk"),
		UpstreamRepo: "fake",
		UpstreamRef:  "rust-v0.140.0",
		Lookuper: fakeLookuper{byPattern: map[string]string{
			"refs/tags/rust-v0.140.0":    oldSHA + "\trefs/tags/rust-v0.140.0",
			"refs/tags/rust-v0.140.0^{}": oldSHA + "\trefs/tags/rust-v0.140.0^{}",
		}},
		Generate: func(GenerateRequest) (Candidate, error) {
			generated = true
			return Candidate{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeBaselineMatches {
		t.Fatalf("outcome = %s", result.Outcome)
	}
	if generated {
		t.Fatal("current baseline must not generate")
	}
}

func TestSyncForceCompareCurrentStillGenerates(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	result, err := Sync(SyncRequest{
		RepoRoot:     repo,
		ModuleRoot:   filepath.Join(repo, "codexsdk"),
		UpstreamRepo: "fake",
		UpstreamRef:  "rust-v0.140.0",
		ForceCompare: true,
		Publication:  &PendingRequest{API: &fixturePublicationAPI{fail: true}},
		Lookuper: fakeLookuper{byPattern: map[string]string{
			"refs/tags/rust-v0.140.0":    oldSHA + "\trefs/tags/rust-v0.140.0",
			"refs/tags/rust-v0.140.0^{}": oldSHA + "\trefs/tags/rust-v0.140.0^{}",
		}},
		Generate: func(GenerateRequest) (Candidate, error) {
			return Candidate{SchemaDir: "/tmp/schema", SourceCommit: oldSHA, DriftStatus: "clean"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeSchemasMatch || result.Candidate != "/tmp/schema" {
		t.Fatalf("%+v", result)
	}
}

func TestSyncValidationOnlyVerifiesFreshExactCandidateWithoutEffects(t *testing.T) {
	for _, test := range []struct {
		name  string
		proof protocolupgrade.PlanResult
		cause error
		fail  bool
	}{
		{name: "matching", proof: protocolupgrade.PlanResult{Status: protocolupgrade.PlanReady}},
		{name: "unresolved", proof: protocolupgrade.PlanResult{Status: protocolupgrade.PlanSemanticUnresolved, Issue: &protocolupgrade.PlanIssue{Stage: "manifest", Reason: "stale requiredness"}}, fail: true},
		{name: "typed incompatibility", cause: &protocolupgrade.IncompatibilityError{Stage: "surface", Err: errors.New("stale requiredness")}, fail: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
			generated, verified, planned, applied := false, false, false, false
			result, err := Sync(SyncRequest{
				RepoRoot: repo, ModuleRoot: filepath.Join(repo, "codexsdk"),
				UpstreamRepo: "fake", UpstreamRef: "rust-v0.140.0",
				ForceCompare: true, ValidationOnly: true,
				Lookuper: fakeLookuper{byPattern: map[string]string{
					"refs/tags/rust-v0.140.0":    oldSHA + "\trefs/tags/rust-v0.140.0",
					"refs/tags/rust-v0.140.0^{}": oldSHA + "\trefs/tags/rust-v0.140.0^{}",
				}},
				Generate: func(GenerateRequest) (Candidate, error) {
					generated = true
					return Candidate{SchemaDir: "/tmp/schema", SourceCommit: oldSHA, DriftStatus: "clean"}, nil
				},
				VerifyExact: func(req protocolupgrade.ApplyRequest) (protocolupgrade.PlanResult, error) {
					verified = true
					if req.TargetSHA != oldSHA || req.Candidate != "/tmp/schema" {
						t.Fatalf("exact verification input: %+v", req)
					}
					return test.proof, test.cause
				},
				Plan: func(protocolupgrade.ApplyRequest) (protocolupgrade.PlanResult, error) {
					planned = true
					return protocolupgrade.PlanResult{}, nil
				},
				Apply: func(protocolupgrade.ApplyRequest) (protocolupgrade.ApplyResult, error) {
					applied = true
					return protocolupgrade.ApplyResult{}, nil
				},
			})
			if generated != true || verified != true || planned || applied {
				t.Fatalf("generated=%v verified=%v planned=%v applied=%v", generated, verified, planned, applied)
			}
			if test.fail {
				if err == nil || result.FailureCategory != FailureUnsupported || !strings.Contains(err.Error(), "stale requiredness") {
					t.Fatalf("unresolved exact verification: %v", err)
				}
			} else if err != nil || result.Outcome != OutcomeExactVerified {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if err := AssertClean(repo); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSyncValidationOnlyRequiresExactComparisonMode(t *testing.T) {
	for _, req := range []SyncRequest{
		{ValidationOnly: true},
		{ValidationOnly: true, ForceCompare: true, Diagnostic: true},
	} {
		if _, err := Sync(req); err == nil {
			t.Fatalf("invalid validation mode accepted: %+v", req)
		}
	}
}

func TestSyncDiagnosticPlansCurrentBaselineWithoutApply(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	generated, planned, applied := false, false, false
	result, err := Sync(SyncRequest{
		RepoRoot:     repo,
		ModuleRoot:   filepath.Join(repo, "codexsdk"),
		UpstreamRepo: "fake",
		UpstreamRef:  "rust-v0.140.0",
		Diagnostic:   true,
		Lookuper: fakeLookuper{byPattern: map[string]string{
			"refs/tags/rust-v0.140.0":    oldSHA + "\trefs/tags/rust-v0.140.0",
			"refs/tags/rust-v0.140.0^{}": oldSHA + "\trefs/tags/rust-v0.140.0^{}",
		}},
		Generate: func(GenerateRequest) (Candidate, error) {
			generated = true
			return Candidate{Dir: "/tmp/candidate", SchemaDir: "/tmp/candidate/schema", SourceCommit: oldSHA, DriftStatus: "clean"}, nil
		},
		Plan: func(protocolupgrade.ApplyRequest) (protocolupgrade.PlanResult, error) {
			planned = true
			return protocolupgrade.PlanResult{Status: protocolupgrade.PlanReady}, nil
		},
		Apply: func(protocolupgrade.ApplyRequest) (protocolupgrade.ApplyResult, error) {
			applied = true
			return protocolupgrade.ApplyResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !generated || !planned || applied || result.Outcome != OutcomePlanReady {
		t.Fatalf("generated=%v planned=%v applied=%v result=%+v", generated, planned, applied, result)
	}
	if err := AssertClean(repo); err != nil {
		t.Fatalf("diagnostic changed accepted worktree: %v", err)
	}
}

func TestSyncBlockedDowngradeFailsClosed(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.141.0", KindStableTag)
	_, err := Sync(SyncRequest{
		RepoRoot:     repo,
		ModuleRoot:   filepath.Join(repo, "codexsdk"),
		UpstreamRepo: "fake",
		UpstreamRef:  "rust-v0.140.0",
		Lookuper: fakeLookuper{byPattern: map[string]string{
			"refs/tags/rust-v0.140.0":    newSHA + "\trefs/tags/rust-v0.140.0",
			"refs/tags/rust-v0.140.0^{}": newSHA + "\trefs/tags/rust-v0.140.0^{}",
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "older than the current baseline") {
		t.Fatalf("err = %v", err)
	}
}

func TestSyncAppliesRealDrift(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	applied := false
	result, err := Sync(SyncRequest{
		RepoRoot:     repo,
		ModuleRoot:   filepath.Join(repo, "codexsdk"),
		UpstreamRepo: "fake",
		UpstreamRef:  "rust-v0.141.0",
		Lookuper: fakeLookuper{byPattern: map[string]string{
			"refs/tags/rust-v0.141.0":    newSHA + "\trefs/tags/rust-v0.141.0",
			"refs/tags/rust-v0.141.0^{}": newSHA + "\trefs/tags/rust-v0.141.0^{}",
		}},
		Generate: func(GenerateRequest) (Candidate, error) {
			return Candidate{SchemaDir: "/tmp/schema", SourceCommit: newSHA, DriftStatus: "review-required"}, nil
		},
		Plan: func(protocolupgrade.ApplyRequest) (protocolupgrade.PlanResult, error) {
			return protocolupgrade.PlanResult{Status: protocolupgrade.PlanReady}, nil
		},
		Apply: func(protocolupgrade.ApplyRequest) (protocolupgrade.ApplyResult, error) {
			applied = true
			path := filepath.Join(repo, "codexsdk", "sdk_surface.gen.go")
			if err := os.WriteFile(path, []byte("package codexsdk\n"), 0o644); err != nil {
				return protocolupgrade.ApplyResult{}, err
			}
			return protocolupgrade.ApplyResult{Status: "ok"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !applied || result.Outcome != OutcomeApplied {
		t.Fatalf("applied=%v result=%+v", applied, result)
	}
}

func TestSyncForceCompareDirtyFailsWithoutApply(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	applied := false
	_, err := Sync(SyncRequest{
		RepoRoot:     repo,
		ModuleRoot:   filepath.Join(repo, "codexsdk"),
		UpstreamRepo: "fake",
		UpstreamRef:  "rust-v0.141.0",
		ForceCompare: true,
		Lookuper: fakeLookuper{byPattern: map[string]string{
			"refs/tags/rust-v0.141.0":    newSHA + "\trefs/tags/rust-v0.141.0",
			"refs/tags/rust-v0.141.0^{}": newSHA + "\trefs/tags/rust-v0.141.0^{}",
		}},
		Generate: func(GenerateRequest) (Candidate, error) {
			return Candidate{SchemaDir: "/tmp/schema", SourceCommit: newSHA, DriftStatus: "review-required"}, nil
		},
		Apply: func(protocolupgrade.ApplyRequest) (protocolupgrade.ApplyResult, error) {
			applied = true
			return protocolupgrade.ApplyResult{}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "comparison never applies") {
		t.Fatalf("err = %v", err)
	}
	if applied {
		t.Fatal("force-compare dirty must not apply")
	}
}

func TestSyncSemanticUnresolvedDoesNotApply(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	candidateDir := filepath.Join(t.TempDir(), "candidate")
	writeCandidateFixture(t, candidateDir, newSHA)
	applied := false
	result, err := Sync(SyncRequest{
		RepoRoot:     repo,
		ModuleRoot:   filepath.Join(repo, "codexsdk"),
		UpstreamRepo: "fake",
		UpstreamRef:  "rust-v0.141.0",
		Lookuper: fakeLookuper{byPattern: map[string]string{
			"refs/tags/rust-v0.141.0":    newSHA + "\trefs/tags/rust-v0.141.0",
			"refs/tags/rust-v0.141.0^{}": newSHA + "\trefs/tags/rust-v0.141.0^{}",
		}},
		Generate: func(GenerateRequest) (Candidate, error) {
			return Candidate{Dir: candidateDir, SchemaDir: filepath.Join(candidateDir, "schema"), SourceCommit: newSHA, DriftStatus: "review-required"}, nil
		},
		Plan: func(protocolupgrade.ApplyRequest) (protocolupgrade.PlanResult, error) {
			return protocolupgrade.PlanResult{
				Status: protocolupgrade.PlanSemanticUnresolved,
				Issue:  &protocolupgrade.PlanIssue{Stage: "surface", Path: "v2/Example.json#/properties/value", Reason: "unsupported schema"},
			}, nil
		},
		Apply: func(protocolupgrade.ApplyRequest) (protocolupgrade.ApplyResult, error) {
			applied = true
			return protocolupgrade.ApplyResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("semantic-unresolved candidate must not apply")
	}
	if result.Outcome != OutcomeSemanticUnresolved || result.Issue == nil || result.Issue.Stage != "surface" || result.CandidateSHA256 == "" {
		t.Fatalf("result = %+v", result)
	}
	if err := AssertClean(repo); err != nil {
		t.Fatalf("planning mutated accepted worktree: %v", err)
	}
}

func TestSyncOrdinaryPlanFailureNeverRequestsAgentOrApplies(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	planCalls, applyCalls := 0, 0
	result, err := Sync(SyncRequest{
		RepoRoot: repo, ModuleRoot: filepath.Join(repo, "codexsdk"),
		UpstreamRepo: "fake", UpstreamRef: "rust-v0.141.0",
		Lookuper: fakeLookuper{byPattern: map[string]string{
			"refs/tags/rust-v0.141.0":    newSHA + "\trefs/tags/rust-v0.141.0",
			"refs/tags/rust-v0.141.0^{}": newSHA + "\trefs/tags/rust-v0.141.0^{}",
		}},
		Generate: func(GenerateRequest) (Candidate, error) {
			return Candidate{SchemaDir: "/tmp/schema", SourceCommit: newSHA, DriftStatus: "review-required"}, nil
		},
		Plan: func(protocolupgrade.ApplyRequest) (protocolupgrade.PlanResult, error) {
			planCalls++
			return protocolupgrade.PlanResult{}, os.ErrNotExist
		},
		Apply: func(protocolupgrade.ApplyRequest) (protocolupgrade.ApplyResult, error) {
			applyCalls++
			return protocolupgrade.ApplyResult{}, nil
		},
	})
	if !errors.Is(err, os.ErrNotExist) || result.Outcome != OutcomeFailed || result.FailureCategory != FailureExecution || result.Stage != "plan" || planCalls != 1 || applyCalls != 0 {
		t.Fatalf("result=%+v err=%v planCalls=%d applyCalls=%d", result, err, planCalls, applyCalls)
	}
}

func TestSyncCleanNewTargetAppliesProvenanceOnly(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	applied := false
	result, err := Sync(SyncRequest{
		RepoRoot:     repo,
		ModuleRoot:   filepath.Join(repo, "codexsdk"),
		UpstreamRepo: "fake",
		UpstreamRef:  "rust-v0.141.0",
		Lookuper: fakeLookuper{byPattern: map[string]string{
			"refs/tags/rust-v0.141.0":    newSHA + "\trefs/tags/rust-v0.141.0",
			"refs/tags/rust-v0.141.0^{}": newSHA + "\trefs/tags/rust-v0.141.0^{}",
		}},
		Generate: func(GenerateRequest) (Candidate, error) {
			return Candidate{Dir: "/tmp/candidate", SchemaDir: "/tmp/candidate/schema", SourceCommit: newSHA, DriftStatus: "clean"}, nil
		},
		Plan: func(protocolupgrade.ApplyRequest) (protocolupgrade.PlanResult, error) {
			return protocolupgrade.PlanResult{
				Status:  protocolupgrade.PlanReady,
				Preview: protocolupgrade.ApplyResult{GeneratedReleaseImpact: "metadata-only"},
			}, nil
		},
		Apply: func(protocolupgrade.ApplyRequest) (protocolupgrade.ApplyResult, error) {
			applied = true
			path := filepath.Join(repo, "codexsdk", "internal", "protocolschema", "appserver", "v2", "baseline_metadata.json")
			if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
				return protocolupgrade.ApplyResult{}, err
			}
			return protocolupgrade.ApplyResult{Status: "ok"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !applied || result.Outcome != OutcomeApplied || !strings.Contains(result.Reason, "provenance-only") {
		t.Fatalf("applied=%v result=%+v", applied, result)
	}
}

func TestSyncCleanSchemaWithGeneratedSurfaceChangeIsMechanical(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	result, err := Sync(SyncRequest{
		RepoRoot:     repo,
		ModuleRoot:   filepath.Join(repo, "codexsdk"),
		UpstreamRepo: "fake",
		UpstreamRef:  "rust-v0.141.0",
		Lookuper: fakeLookuper{byPattern: map[string]string{
			"refs/tags/rust-v0.141.0":    newSHA + "\trefs/tags/rust-v0.141.0",
			"refs/tags/rust-v0.141.0^{}": newSHA + "\trefs/tags/rust-v0.141.0^{}",
		}},
		Generate: func(GenerateRequest) (Candidate, error) {
			return Candidate{Dir: "/tmp/candidate", SchemaDir: "/tmp/candidate/schema", SourceCommit: newSHA, DriftStatus: "clean"}, nil
		},
		Plan: func(protocolupgrade.ApplyRequest) (protocolupgrade.PlanResult, error) {
			return protocolupgrade.PlanResult{
				Status:  protocolupgrade.PlanReady,
				Preview: protocolupgrade.ApplyResult{GeneratedReleaseImpact: "additive"},
			}, nil
		},
		Apply: func(protocolupgrade.ApplyRequest) (protocolupgrade.ApplyResult, error) {
			path := filepath.Join(repo, "codexsdk", "sdk_surface.gen.go")
			if err := os.WriteFile(path, []byte("package codexsdk\n"), 0o644); err != nil {
				return protocolupgrade.ApplyResult{}, err
			}
			return protocolupgrade.ApplyResult{Status: "ok"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeApplied || !strings.Contains(result.Reason, "mechanical") {
		t.Fatalf("result = %+v", result)
	}
}

func TestResumeRejectsMechanicalAgentChanges(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	if err := os.WriteFile(filepath.Join(repo, "codexsdk", "sdk_surface.gen.go"), []byte("package codexsdk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Resume(ResumeRequest{
		RepoRoot:     repo,
		ModuleRoot:   filepath.Join(repo, "codexsdk"),
		CandidateDir: "/tmp/candidate",
		TargetRef:    "rust-v0.141.0",
		TargetKind:   KindStableTag,
		TargetSHA:    newSHA,
	})
	if err == nil || !strings.Contains(err.Error(), "handwritten") {
		t.Fatalf("err = %v", err)
	}
}

func TestResumeBindsAgentProposalToExactCandidateAndProtectedControl(t *testing.T) {
	for _, test := range []struct {
		name, want string
		mutate     func(t *testing.T, repo, candidate string, req *ResumeRequest)
		unresolved bool
	}{
		{name: "allowed generator correction applies"},
		{name: "second unresolved stops", want: "remains unresolved", unresolved: true},
		{name: "control change stops before Plan", want: "handwritten codexsdk scope", mutate: func(t *testing.T, repo, _ string, _ *ResumeRequest) {
			writeFile(t, filepath.Join(repo, "codexsdk/internal/protocolsync/sync.go"), "package protocolsync\n")
		}},
		{name: "ignored stable schema changed", want: "changed after initial Plan", mutate: func(t *testing.T, _, candidate string, _ *ResumeRequest) {
			writeFile(t, filepath.Join(candidate, "stable-schema/ClientRequest.json"), `{"changed":true}`)
		}},
		{name: "ignored mapping missing", want: "candidate common.rs", mutate: func(t *testing.T, _, candidate string, _ *ResumeRequest) {
			if err := os.Remove(filepath.Join(candidate, "common.rs")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "target identity changed", want: "does not match target", mutate: func(_ *testing.T, _, _ string, req *ResumeRequest) {
			req.TargetSHA = oldSHA
		}},
		{name: "target ref changed", want: "does not match selected", mutate: func(_ *testing.T, _, _ string, req *ResumeRequest) {
			req.TargetRef = "rust-v0.142.0"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
			writeFile(t, filepath.Join(repo, ".gitignore"), "codexsdk/.cache/\n")
			runGitInitCommit(t, repo, "ignore candidate cache")
			candidate := filepath.Join(repo, "codexsdk/.cache/candidate")
			writeCandidateFixture(t, candidate, newSHA)
			fingerprint, err := candidateDigest(candidate)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(repo, "codexsdk/internal/protocolgen/type_plan.go"), "package protocolgen // targeted correction\n")
			if err := AssertClean(repo); err == nil {
				t.Fatal("proposal should be visible while ignored candidate remains invisible")
			}
			planned, applied := false, false
			req := ResumeRequest{
				RepoRoot: repo, ModuleRoot: filepath.Join(repo, "codexsdk"),
				CandidateDir: candidate, CandidateSHA256: fingerprint,
				TargetRef: "rust-v0.141.0", TargetKind: KindStableTag, TargetSHA: newSHA,
				Plan: func(req protocolupgrade.ApplyRequest) (protocolupgrade.PlanResult, error) {
					planned = true
					if req.Candidate == filepath.Join(candidate, "schema") {
						t.Fatal("Plan used mutable ignored candidate instead of verified copy")
					}
					if string(mustRead(t, filepath.Join(req.Candidate, "ClientRequest.json"))) != `{"title":"ClientRequest"}` {
						t.Fatal("Plan did not receive original candidate bytes")
					}
					if test.unresolved {
						return protocolupgrade.PlanResult{Status: protocolupgrade.PlanSemanticUnresolved, Issue: &protocolupgrade.PlanIssue{Stage: "codegen", Reason: "still unsupported"}}, nil
					}
					return protocolupgrade.PlanResult{Status: protocolupgrade.PlanReady}, nil
				},
				Apply: func(req protocolupgrade.ApplyRequest) (protocolupgrade.ApplyResult, error) {
					applied = true
					writeFile(t, filepath.Join(repo, "codexsdk/sdk_surface.gen.go"), "package codexsdk\n")
					return protocolupgrade.ApplyResult{Status: "ok"}, nil
				},
			}
			if test.mutate != nil {
				test.mutate(t, repo, candidate, &req)
			}
			result, err := Resume(req)
			if test.want == "" {
				if err != nil || result.Outcome != OutcomeApplied || !planned || !applied {
					t.Fatalf("result=%+v err=%v planned=%v applied=%v", result, err, planned, applied)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.want) || applied {
				t.Fatalf("result=%+v err=%v planned=%v applied=%v, want %q before apply", result, err, planned, applied, test.want)
			}
		})
	}
}

func TestIsMechanicalPath(t *testing.T) {
	if !isMechanicalPath("codexsdk/internal/protocolschema/appserver/v2/manifest.json") {
		t.Fatal("baseline path should be mechanical")
	}
	if !isMechanicalPath("codexsdk/sdk_surface.gen.go") {
		t.Fatal("sdk surface should be mechanical")
	}
	if !isMechanicalPath("codexsdk/protocolv2/protocol_types.gen.go") {
		t.Fatal("generated protocol types should be mechanical")
	}
	if isMechanicalPath("codexsdk/client.go") {
		t.Fatal("handwritten source is not mechanical")
	}
}

func TestWriteGitHubOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out")
	err := WriteGitHubOutput(path, SyncResult{
		Outcome:         OutcomeApplied,
		Reason:          "applied",
		Candidate:       "/exact/schema",
		CandidateSHA256: strings.Repeat("a", 64),
		Target:          Target{RefName: "rust-v0.154.0", RefKind: KindStableTag, PeeledCommitSHA: oldSHA},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(mustRead(t, path))
	for _, want := range []string{"outcome=applied", "applied=true", "candidate=/exact/schema", "candidate_sha256=" + strings.Repeat("a", 64), "target_ref=rust-v0.154.0"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
}

func initSyncRepo(t *testing.T, commit, ref, kind string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "codexsdk", "internal", "protocolschema", "appserver", "v2", "baseline_metadata.json"), mustJSON(t, map[string]string{
		"source_commit":   commit,
		"source_ref_name": ref,
		"source_ref_kind": kind,
	}))
	writeFile(t, filepath.Join(root, "README.md"), "# fixture\n")
	runGitInit(t, root)
	return root
}

func runGitInit(t *testing.T, root string) {
	t.Helper()
	if out, err := execGit(t, root, "init", "-b", "main"); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	runGitInitCommit(t, root, "fixture")
}

func runGitInitCommit(t *testing.T, root, message string) {
	t.Helper()
	if out, err := execGit(t, root, "add", "-A"); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	if out, err := execGit(t, root, "commit", "-m", message); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

func execGit(t *testing.T, root string, args ...string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command("git", append([]string{
		"-C", root,
		"-c", "user.name=protocolsync-test",
		"-c", "user.email=protocolsync-test@example.com",
		"-c", "commit.gpgsign=false",
	}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	return cmd.CombinedOutput()
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(append(raw, '\n'))
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestSyncFailureKeepsOwnerAttributionAndCause(t *testing.T) {
	for _, test := range []struct {
		name, category string
		cause          error
	}{
		{"unknown", FailureUnknown, errors.New("schema path missing: do not infer a repair")},
		{"source", FailureSource, &Failure{Category: FailureSource, Err: errors.New("source mismatch")}},
		{"environment", FailureExecution, os.ErrPermission},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
			result, err := Sync(SyncRequest{
				RepoRoot: repo, UpstreamRef: "rust-v0.141.0",
				Lookuper: fakeLookuper{byPattern: map[string]string{
					"refs/tags/rust-v0.141.0":    newSHA + "\trefs/tags/rust-v0.141.0",
					"refs/tags/rust-v0.141.0^{}": newSHA + "\trefs/tags/rust-v0.141.0^{}",
				}},
				Generate: func(GenerateRequest) (Candidate, error) { return Candidate{}, test.cause },
				Plan: func(protocolupgrade.ApplyRequest) (protocolupgrade.PlanResult, error) {
					t.Fatal("failed generation must not plan or authorize Agent")
					return protocolupgrade.PlanResult{}, nil
				},
				Apply: func(protocolupgrade.ApplyRequest) (protocolupgrade.ApplyResult, error) {
					t.Fatal("failed generation must not apply")
					return protocolupgrade.ApplyResult{}, nil
				},
			})
			if !errors.Is(err, test.cause) || result.Outcome != OutcomeFailed || result.Stage != "generate" || result.FailureCategory != test.category || result.Target.PeeledCommitSHA != newSHA {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestCachedUpstreamOriginMismatchIsSourceFailure(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	writeFile(t, filepath.Join(repo, ".git", "info", "exclude"), "codexsdk/.cache/\n")
	cache := filepath.Join(repo, "codexsdk", ".cache", "openai-codex")
	if err := prepareUpstreamRepo(cache, "https://example.invalid/old.git"); err != nil {
		t.Fatal(err)
	}
	result, err := Sync(SyncRequest{
		RepoRoot: repo, UpstreamRepo: "https://example.invalid/new.git", UpstreamRef: "rust-v0.141.0",
		Lookuper: fakeLookuper{byPattern: map[string]string{
			"refs/tags/rust-v0.141.0":    newSHA + "\trefs/tags/rust-v0.141.0",
			"refs/tags/rust-v0.141.0^{}": newSHA + "\trefs/tags/rust-v0.141.0^{}",
		}},
	})
	if err == nil || result.Stage != "generate" || result.FailureCategory != FailureSource || result.Outcome != OutcomeFailed {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
