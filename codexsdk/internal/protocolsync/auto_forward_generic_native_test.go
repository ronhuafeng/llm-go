package protocolsync

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func forwardGenericFixtureController(t *testing.T, root, base, head string) *NativeForward {
	t.Helper()
	op, err := NewNativeForward(ForwardRequest{RepoRoot: root, Repository: "fixture/project", BaseBranch: "main", OriginalBase: base, VerificationBase: base, ExpectedHead: head, Number: 1,
		API: forwardNativeReadAPI{t: t, head: head}, Review: func(context.Context) error { return nil },
		Token: func(context.Context, bool) (string, func(), error) {
			t.Fatal("local ordinary-proposal fixture requested write authority")
			return "", nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return op
}

// Ordinary automatic proof covers the accepted SDK, llmkit/llmcaller Go roots and documentation.
func forwardGenericFixture(t *testing.T, scenario string) (root, main, head, base string) {
	t.Helper()
	origin := t.TempDir()
	goalTestGit(t, origin, "init", "--quiet", "-b", "main")
	goalTestGit(t, origin, "config", "user.name", "fixture")
	goalTestGit(t, origin, "config", "user.email", "fixture@localhost")
	goalTestWrite(t, origin, "codexsdk/shared.go", "package fixture\nconst Shared = 1\n")
	goalTestGit(t, origin, "add", ".")
	goalTestGit(t, origin, "commit", "--quiet", "-m", "base")
	base = goalTestGit(t, origin, "rev-parse", "HEAD")
	root = filepath.Join(t.TempDir(), "controller")
	goalTestGit(t, t.TempDir(), "clone", "--quiet", "--no-local", origin, root)
	goalTestWrite(t, origin, "codexsdk/main.go", "package fixture\nconst Main = 2\n")
	if scenario == "conflict" {
		goalTestWrite(t, origin, "codexsdk/shared.go", "package fixture\nconst Shared = 2\n")
	}
	goalTestGit(t, origin, "add", ".")
	goalTestGit(t, origin, "commit", "--quiet", "-m", "main")
	main = goalTestGit(t, origin, "rev-parse", "HEAD")
	goalTestGit(t, origin, "checkout", "--quiet", "-b", "proposal", base)
	goalTestWrite(t, origin, "codexsdk/example.go", "package fixture\nconst Proposal = 3\n")
	if scenario == "conflict" {
		goalTestWrite(t, origin, "codexsdk/shared.go", "package fixture\nconst Shared = 3\n")
	}
	if scenario == "unsupported" {
		goalTestWrite(t, origin, "tools/custom.go", "package custom\nconst Untested = 4\n")
	}
	if scenario == "supported_llmkit" {
		goalTestWrite(t, origin, "llmkit/example.go", "package llmkit\nconst Implementation = 4\n")
	}
	goalTestGit(t, origin, "add", ".")
	goalTestGit(t, origin, "commit", "--quiet", "-m", "proposal")
	head = goalTestGit(t, origin, "rev-parse", "HEAD")
	goalTestGit(t, root, "fetch", "--quiet", "--no-write-fetch-head", origin, main, head)
	return
}

func TestForwardGenericRebaseProvesExactNewCandidateWithoutModelRepair(t *testing.T) {
	for _, scenario := range []string{"pass", "supported_llmkit", "conflict", "semantic", "infrastructure", "unsupported"} {
		t.Run(scenario, func(t *testing.T) {
			root, main, head, base := forwardGenericFixture(t, scenario)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			deadline, _ := ctx.Deadline()
			op := forwardGenericFixtureController(t, root, base, head)
			calls := 0
			op.req.VerifyCandidate = func(call context.Context, gotMain, gotHead string) error {
				calls++
				if gotMain != main || gotHead == head || gotHead == main || op.req.OriginalBase != base {
					t.Fatal("ordinary reconciliation lost exact original/current baseline binding")
				}
				if got, ok := call.Deadline(); !ok || !got.Equal(deadline) {
					t.Fatal("ordinary reconciliation reset the original deadline")
				}
				if scenario == "semantic" {
					return ErrForwardSemanticProof
				}
				if scenario == "infrastructure" {
					return errors.New("sandbox unavailable")
				}
				return nil
			}
			op.req.RepairCandidate = func(context.Context, string, string, string) (GoalVerifyResult, error) {
				t.Fatal("ordinary proposal launched model repair without fixed U")
				return GoalVerifyResult{}, nil
			}
			next, err := op.Rebase(ctx, main, head)
			if scenario == "pass" || scenario == "supported_llmkit" {
				if err != nil || calls != 1 || !op.proofs[main+":"+next] {
					t.Fatalf("ordinary mechanical rebase lacks immutable proof: %s %v calls=%d", next, err, calls)
				}
				if err := op.Verify(ctx, main, next); err != nil || calls != 1 {
					t.Fatal("ordinary exact M/H proof was not reused")
				}
				if scenario == "supported_llmkit" && goalTestGit(t, root, "show", next+":llmkit/example.go") == "" {
					t.Fatal("ordinary rebase dropped supported llmkit implementation")
				}
				for _, name := range []string{"codexsdk/main.go", "codexsdk/example.go", "codexsdk/shared.go"} {
					if goalTestGit(t, root, "show", next+":"+name) == "" {
						t.Fatal("ordinary rebase dropped accepted or proposal content")
					}
				}
			} else if err == nil || next != "" || len(op.proofs) != 1 {
				t.Fatalf("unproved ordinary candidate escaped explicit blocking: %s %v proofs=%v", next, err, op.proofs)
			}
			if scenario == "conflict" && (!errors.Is(err, ErrForwardConflict) || calls != 0) {
				t.Fatal("ordinary conflict did not stop before verification or model execution")
			}
			if scenario == "unsupported" && (calls != 0 || !strings.Contains(err.Error(), "outside existing immutable runtime proof")) {
				t.Fatal("unsupported tools/custom.go change reached automatic executable verification")
			}
			if scenario == "semantic" && !errors.Is(err, ErrForwardSemanticProof) {
				t.Fatal("ordinary semantic failure lost its explicit blocker")
			}
		})
	}
}

func TestForwardGenericManualProofExceptionCannotCrossCurrentMain(t *testing.T) {
	root, main, _, base := forwardGenericFixture(t, "pass")
	origin := goalTestGit(t, root, "remote", "get-url", "origin")
	goalTestWrite(t, origin, ".github/workflows/reviewed.yml", "explicitly reviewed control change\n")
	goalTestGit(t, origin, "add", ".")
	goalTestGit(t, origin, "commit", "--quiet", "-m", "reviewed control change")
	head := goalTestGit(t, origin, "rev-parse", "HEAD")
	goalTestGit(t, root, "fetch", "--quiet", "--no-write-fetch-head", origin, head)
	op := forwardGenericFixtureController(t, root, base, head)
	op.req.VerifyCandidate = func(context.Context, string, string) error {
		t.Fatal("control changes reached automatic executable verification")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := op.Verify(ctx, base, head); err != nil {
		t.Fatalf("original explicit reviewed manual FF route was blocked: %v", err)
	}
	if err := op.Verify(ctx, main, head); err == nil {
		t.Fatalf("manual initial proof bypass crossed current main: %v", err)
	}
}

func TestForwardNativeAndGenericBindingsRemainDistinct(t *testing.T) {
	root, _, head, base := forwardNativeFixture(t, "clean")
	valid := forwardGenericFixtureController(t, root, base, head).req
	for _, scenario := range []string{"missing_original", "missing_verified", "partial_target", "missing_actor"} {
		t.Run(scenario, func(t *testing.T) {
			req := valid
			switch scenario {
			case "missing_original":
				req.OriginalBase = ""
			case "missing_verified":
				req.VerificationBase = ""
			case "partial_target":
				req.Target.RefName = "rust-v0.1.0"
			case "missing_actor":
				req.Target = Target{RefName: "rust-v0.1.0", RefKind: KindStableTag, PeeledCommitSHA: strings.Repeat("a", 40)}
			}
			if _, err := NewNativeForward(req); err == nil {
				t.Fatal("incomplete original/current baseline or native target/App binding accepted")
			}
		})
	}
}
