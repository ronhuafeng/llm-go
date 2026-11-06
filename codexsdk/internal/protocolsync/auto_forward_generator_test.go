package protocolsync

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Use the native Git eligibility/proof/rebase path inside the full controller.
// Repository effects and CI observations stay local fixtures, not live writes.
type generatorForwardFixture struct {
	*forwardFake
	native  *NativeForward
	reviews int
}

func (f *generatorForwardFixture) Ancestor(ctx context.Context, base, head string) (bool, error) {
	return f.native.Ancestor(ctx, base, head)
}

func (f *generatorForwardFixture) Verify(ctx context.Context, main, head string) error {
	return f.native.Verify(ctx, main, head)
}

func (f *generatorForwardFixture) Rebase(ctx context.Context, main, head string) (string, error) {
	f.rebases++
	return f.native.Rebase(ctx, main, head)
}

func (f *generatorForwardFixture) Review(context.Context) error {
	f.reviews++
	return nil
}

func TestAutoForwardNativeGeneratorEvolution(t *testing.T) {
	for _, reconcile := range []bool{false, true} {
		t.Run(map[bool]string{false: "current_base", true: "advanced_main"}[reconcile], func(t *testing.T) {
			root, main, _, base := forwardNativeFixture(t, "clean")
			if !reconcile {
				main = base
			}
			origin := goalTestGit(t, root, "remote", "get-url", "origin")
			files := map[string]string{
				"codexsdk/internal/protocolgen/evolution.go":            "package protocolgen\n",
				"codexsdk/internal/protocolgen/evolution_test.go":       "package protocolgen\n",
				"codexsdk/internal/protocolgen/testdata/evolution.json": `{"type":"object"}`,
			}
			for name, content := range files {
				goalTestWrite(t, origin, name, content)
			}
			goalTestGit(t, origin, "add", ".")
			goalTestGit(t, origin, "commit", "--quiet", "-m", "adapt candidate generator")
			head := goalTestGit(t, origin, "rev-parse", "HEAD")
			goalTestGit(t, root, "fetch", "--quiet", "--no-write-fetch-head", origin, head)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			proofs := 0
			verified := ""
			native := &NativeForward{proofs: map[string]bool{}, req: ForwardRequest{
				RepoRoot: root,
				Target:   Target{PeeledCommitSHA: strings.Repeat("a", 40)},
				VerifyCandidate: func(_ context.Context, gotMain, gotHead string) error {
					proofs++
					verified = gotHead
					if gotMain != main {
						t.Fatal("candidate proof lost current main binding")
					}
					for name, content := range files {
						if got := goalTestGit(t, root, "show", gotHead+":"+name); got != strings.TrimSuffix(content, "\n") {
							t.Fatalf("proof lost generator adaptation %s: %q", name, got)
						}
					}
					return nil
				},
			}}
			fixture := &generatorForwardFixture{forwardFake: newForwardFake(), native: native}
			fixture.state.Main, fixture.state.Head = main, head
			result, err := AutoForward(ctx, head, time.Millisecond, fixture)
			if err != nil || result.Status != "integrated" {
				t.Fatalf("generator adaptation was not eligible: %+v %v", result, err)
			}
			if proofs != 1 || verified != result.Head || fixture.checkCalls < 2 || fixture.reviews != 1 || fixture.forwarded != 1 || fixture.cleaned != 1 {
				t.Fatalf("candidate did not traverse proof, exact-head checks, review and effect: proofs=%d verified=%s result=%+v fixture=%+v reviews=%d", proofs, verified, result, fixture.forwardFake, fixture.reviews)
			}
			wantRebases := 0
			if reconcile {
				wantRebases = 1
			}
			if fixture.rebases != wantRebases || fixture.published != wantRebases || (result.Head != head) != reconcile {
				t.Fatal("generator adaptation bypassed normal reconciliation/publication")
			}
			if goalTestGit(t, root, "rev-parse", "HEAD") != base || AssertClean(root) != nil {
				t.Fatal("candidate generator replaced the accepted controller checkout")
			}
		})
	}
}
