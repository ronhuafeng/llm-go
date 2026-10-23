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
)

func TestGoalPublicationIntentSurvivesLostReadbackWithoutReplay(t *testing.T) {
	f := newGoalPublicationFixture(t)
	remote := goalTestGit(t, f.request.RepoRoot, "remote", "get-url", "origin")
	pushes := 0
	got, err := appendGoalDraft(context.Background(), f.request, func(ctx context.Context, req GoalPublicationRequest, head string) error {
		pushes++
		intent, e := readGoalPublicationIntent(ctx, req)
		if e != nil || intent == nil || intent.IntendedHead != head || intent.PreviousHead != req.ExpectedHead || !intent.Deadline.Equal(req.Deadline) {
			t.Fatalf("push preceded durable intent: %+v %v", intent, e)
		}
		if e = pushGoalDraft(ctx, req, head); e != nil {
			t.Fatal(e)
		}
		if e = os.Rename(remote, remote+".unavailable"); e != nil {
			t.Fatal(e)
		}
		return errors.New("SECRET transport response")
	})
	if err == nil || strings.Contains(err.Error(), "SECRET") || got.Head != "" || got.Intent == nil || got.EffectState != GoalPublicationUnconfirmed {
		t.Fatalf("unknown effect lost or leaked: %+v %v", got, err)
	}
	if err = os.Rename(remote+".unavailable", remote); err != nil {
		t.Fatal(err)
	}
	recovered, err := ReconcileGoalPublication(context.Background(), f.request)
	if err != nil || recovered.Head != got.Intent.IntendedHead || recovered.EffectState != GoalPublicationObserved {
		t.Fatalf("read-only effect reconciliation: %+v %v", recovered, err)
	}
	again, err := appendGoalDraft(context.Background(), f.request, func(context.Context, GoalPublicationRequest, string) error {
		pushes++
		t.Fatal("uncertain push was replayed")
		return nil
	})
	if err != nil || again.Head != recovered.Head || pushes != 1 {
		t.Fatalf("replay: %+v %v pushes=%d", again, err, pushes)
	}
	// Locator discovery alone did not check CI or perform local verification.
	if recovered.SnapshotDigest != f.request.SnapshotDigest {
		t.Fatal("recovered a different candidate")
	}
}

func TestGoalPublicationIntentOldHeadCannotAuthorizeReplay(t *testing.T) {
	f := newGoalPublicationFixture(t)
	pushes := 0
	got, err := appendGoalDraft(context.Background(), f.request, func(context.Context, GoalPublicationRequest, string) error {
		pushes++
		return errors.New("write acknowledgement unavailable")
	})
	if err == nil || got.Intent == nil || got.Head != "" {
		t.Fatalf("unexpected outcome: %+v %v", got, err)
	}
	for range 2 {
		_, err = appendGoalDraft(context.Background(), f.request, func(context.Context, GoalPublicationRequest, string) error {
			pushes++
			t.Fatal("previous head was treated as proof of no effect")
			return nil
		})
		if err == nil || !strings.Contains(err.Error(), "cannot be replayed") {
			t.Fatalf("missing unresolved stop: %v", err)
		}
	}
	if pushes != 1 {
		t.Fatalf("writes=%d", pushes)
	}
}

func TestGoalPublicationReconciliationUsesHistoricalMOnlyAsEvidence(t *testing.T) {
	f := newGoalPublicationFixture(t)
	got, err := AppendGoalDraft(context.Background(), f.request)
	if err != nil {
		t.Fatal(err)
	}
	tree := goalTestGit(t, f.request.RepoRoot, "rev-parse", f.request.BaseSHA+"^{tree}")
	main := goalTestGit(t, f.request.RepoRoot, "commit-tree", tree, "-p", f.request.BaseSHA, "-m", "current accepted main")
	goalTestGit(t, f.request.RepoRoot, "push", "origin", main+":refs/heads/main")
	f.request.VerificationBaseSHA = main
	recovered, err := ReconcileGoalPublication(context.Background(), f.request)
	if err != nil || recovered.Head != got.Head || recovered.Intent.VerificationBaseSHA != f.request.BaseSHA {
		t.Fatalf("historical M blocked locator: %+v %v", recovered, err)
	}
}

func TestGoalPublicationReconciliationRejectsChangedAuthority(t *testing.T) {
	cases := map[string]func(*goalPublicationFixture){
		"ready":      func(f *goalPublicationFixture) { f.pr.Draft = false },
		"closed":     func(f *goalPublicationFixture) { f.pr.State = "closed" },
		"number":     func(f *goalPublicationFixture) { f.pr.Number++ },
		"merged":     func(f *goalPublicationFixture) { value := "now"; f.pr.MergedAt = &value },
		"head ref":   func(f *goalPublicationFixture) { f.pr.Head.Ref += "-other" },
		"head repo":  func(f *goalPublicationFixture) { f.pr.Head.Repo.FullName = "foreign/repo" },
		"base ref":   func(f *goalPublicationFixture) { f.pr.Base.Ref = "other" },
		"base repo":  func(f *goalPublicationFixture) { f.pr.Base.Repo.FullName = "foreign/repo" },
		"creator":    func(f *goalPublicationFixture) { f.pr.User.ID++ },
		"actor":      func(f *goalPublicationFixture) { f.actor.ID++ },
		"actor type": func(f *goalPublicationFixture) { f.actor.Type = "User" },
		"target": func(f *goalPublicationFixture) {
			target := f.request.Target
			target.PeeledCommitSHA = strings.Repeat("c", 40)
			f.request.Lookuper = goalLookup{target}
		},
		"original deadline": func(f *goalPublicationFixture) { f.request.Deadline = f.request.Deadline.Add(time.Hour) },
		"original base":     func(f *goalPublicationFixture) { f.request.BaseSHA = strings.Repeat("c", 40) },
		"foreign head": func(f *goalPublicationFixture) {
			tree := goalTestGit(f.t, f.request.RepoRoot, "rev-parse", "HEAD^{tree}")
			previous := goalTestGit(f.t, f.request.RepoRoot, "ls-remote", "origin", "refs/heads/"+f.request.Branch)
			foreign := goalTestGit(f.t, f.request.RepoRoot, "commit-tree", tree, "-p", strings.Fields(previous)[0], "-m", "foreign")
			goalTestGit(f.t, f.request.RepoRoot, "push", "origin", foreign+":refs/heads/"+f.request.Branch)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newGoalPublicationFixture(t)
			if _, err := AppendGoalDraft(context.Background(), f.request); err != nil {
				t.Fatal(err)
			}
			mutate(f)
			got, err := ReconcileGoalPublication(context.Background(), f.request)
			if err == nil || got.Head != "" || got.EffectState == GoalPublicationObserved {
				t.Fatalf("changed authority adopted: %+v %v", got, err)
			}
		})
	}
}

func TestGoalPublicationIntentRequiresCompleteUntamperedArtifacts(t *testing.T) {
	cases := map[string]func(*testing.T, string){
		"missing intent": func(t *testing.T, state string) {
			if err := os.Remove(filepath.Join(state, goalIntentName)); err != nil {
				t.Fatal(err)
			}
		},
		"missing bundle": func(t *testing.T, state string) {
			if err := os.Remove(filepath.Join(state, goalBundleName)); err != nil {
				t.Fatal(err)
			}
		},
		"truncated intent": func(t *testing.T, state string) {
			if err := os.WriteFile(filepath.Join(state, goalIntentName), []byte(`{"version":1}`), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"changed bundle": func(t *testing.T, state string) {
			if err := os.WriteFile(filepath.Join(state, goalBundleName), []byte("not the verified bytes"), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"bundle symlink": func(t *testing.T, state string) {
			path := filepath.Join(state, goalBundleName)
			if err := os.Rename(path, path+".real"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(path+".real", path); err != nil {
				t.Fatal(err)
			}
		},
		"changed intended tree": func(t *testing.T, state string) {
			path := filepath.Join(state, goalIntentName)
			data, _ := os.ReadFile(path)
			var intent GoalPublicationIntent
			if err := json.Unmarshal(data, &intent); err != nil {
				t.Fatal(err)
			}
			intent.SnapshotTree = strings.Repeat("a", 40)
			data, _ = json.Marshal(intent)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newGoalPublicationFixture(t)
			if _, err := AppendGoalDraft(context.Background(), f.request); err != nil {
				t.Fatal(err)
			}
			mutate(t, f.request.StateDir)
			got, err := ReconcileGoalPublication(context.Background(), f.request)
			if err == nil || got.Head != "" {
				t.Fatalf("incomplete evidence adopted: %+v %v", got, err)
			}
		})
	}
}

func TestGoalPublicationRetainsOnlyVerifiedCodeNotPrivateRuntime(t *testing.T) {
	f := newGoalPublicationFixture(t)
	marker := "SECRET-MARKER-MUST-NOT-BE-RETAINED"
	t.Setenv("GH_TOKEN", marker)
	goalTestWrite(t, f.request.CandidateDir, ".git/private-runtime", marker)
	goalTestWrite(t, f.request.CandidateDir, ".git/hooks/pre-push", marker)
	if _, err := AppendGoalDraft(context.Background(), f.request); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(f.request.StateDir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("retained files: %v %v", entries, err)
	}
	work := t.TempDir()
	goalTestGit(t, work, "init", "-q")
	goalTestGit(t, work, "fetch", filepath.Join(f.request.StateDir, goalBundleName), "HEAD")
	goalTestGit(t, work, "checkout", "--detach", "FETCH_HEAD")
	if _, err = os.Stat(filepath.Join(work, ".git/private-runtime")); !os.IsNotExist(err) {
		t.Fatal("private runtime retained")
	}
	objects := goalTestGit(t, work, "rev-list", "--objects", "--all", "HEAD")
	for _, line := range strings.Split(objects, "\n") {
		fields := strings.Fields(line)
		body := goalTestGit(t, work, "cat-file", "-p", fields[0])
		if strings.Contains(body, marker) {
			t.Fatal("private secret marker retained in bundle")
		}
	}
	data, err := os.ReadFile(filepath.Join(f.request.StateDir, goalIntentName))
	if err != nil || strings.Contains(string(data), marker) {
		t.Fatalf("private token retained: %v", err)
	}
}

func TestGoalPublicationStateMustRemainOutsideCandidate(t *testing.T) {
	f := newGoalPublicationFixture(t)
	f.request.StateDir = filepath.Join(f.request.CandidateDir, "state")
	if _, err := appendGoalDraft(context.Background(), f.request, func(context.Context, GoalPublicationRequest, string) error {
		t.Fatal("candidate-owned state authorized push")
		return nil
	}); err == nil {
		t.Fatal("candidate-owned state accepted")
	}
}

func TestGoalPublicationPreservesSafeTokenAndGitFailureCategories(t *testing.T) {
	f := newGoalPublicationFixture(t)
	tokenErr := &AppTokenError{Failure: AppTokenHTTPRejected, HTTPStatus: 403}
	f.request.PublicationToken = func(context.Context) (string, func(), error) { return "", nil, tokenErr }
	_, err := AppendGoalDraft(context.Background(), f.request)
	var token *AppTokenError
	var publication *GoalPublicationError
	if !errors.As(err, &token) || token.HTTPStatus != 403 || !errors.As(err, &publication) || !strings.Contains(err.Error(), "token_unavailable") {
		t.Fatalf("safe token category discarded: %v", err)
	}
	command := exec.Command("sh", "-c", "echo SECRET >&2; exit 17")
	failure := safeGoalPublicationError("push", command.Run())
	if !errors.As(failure, &publication) || publication.ExitCode != 17 || publication.Stage != "push" || strings.Contains(failure.Error(), "SECRET") {
		t.Fatalf("unsafe/lost Git failure: %v", failure)
	}
}

// This bounded transition check runs the production write and reconciliation
// functions, not a parallel specification. Each case starts with a lost initial
// readback. It then changes only external delivery/ref/actor/time observations.
// The trace names the source transitions so a failure remains reproducible.
func TestGoalPublicationBoundedEffectReconciliationTransitions(t *testing.T) {
	for _, delivered := range []bool{false, true} {
		for _, observed := range []string{"unavailable", "previous", "intended", "foreign"} {
			for _, validActor := range []bool{false, true} {
				for _, expired := range []bool{false, true} {
					name := fmt.Sprintf("delivered=%t/ref=%s/actor=%t/expired=%t", delivered, observed, validActor, expired)
					t.Run(name, func(t *testing.T) {
						f := newGoalPublicationFixture(t)
						remote := goalTestGit(t, f.request.RepoRoot, "remote", "get-url", "origin")
						writes := 0
						first, err := appendGoalDraft(context.Background(), f.request, func(ctx context.Context, req GoalPublicationRequest, head string) error {
							writes++
							if delivered {
								if e := pushGoalDraft(ctx, req, head); e != nil {
									t.Fatal(e)
								}
							}
							if e := os.Rename(remote, remote+".lost"); e != nil {
								t.Fatal(e)
							}
							return errors.New("response lost")
						})
						trace := "appendGoalDraft -> writeGoalPublicationIntent -> push#1 -> unavailable readback -> reconcileGoalPublication"
						if err == nil || first.Intent == nil || first.Head != "" || writes != 1 {
							t.Fatalf("%s: first=%+v writes=%d err=%v", trace, first, writes, err)
						}
						if err = os.Rename(remote+".lost", remote); err != nil {
							t.Fatal(err)
						}
						switch observed {
						case "unavailable":
							if err = os.Rename(remote, remote+".lost"); err != nil {
								t.Fatal(err)
							}
						case "previous":
							// A later rollback is observationally indistinguishable from
							// non-delivery; neither authorizes repeating the old push.
							goalTestGit(t, remote, "update-ref", "refs/heads/"+f.request.Branch, first.PreviousHead)
						case "intended":
							if !delivered {
								// Delayed receive-pack completion after the first timeout.
								goalTestGit(t, f.request.RepoRoot, "push", "origin", first.Intent.IntendedHead+":refs/heads/"+f.request.Branch)
							}
						case "foreign":
							current := goalTestGit(t, remote, "rev-parse", "refs/heads/"+f.request.Branch)
							foreign := goalTestGit(t, f.request.RepoRoot, "commit-tree", first.Intent.SnapshotTree, "-p", current, "-m", "independent foreign head")
							goalTestGit(t, f.request.RepoRoot, "push", "origin", foreign+":refs/heads/"+f.request.Branch)
						}
						if !validActor {
							f.actor.ID++
						}
						now := time.Now()
						if expired {
							now = f.request.Deadline.Add(time.Nanosecond)
						}
						got, err := reconcileGoalPublication(context.Background(), f.request, now)
						wantLocator := observed == "intended" && validActor && !expired
						if wantLocator {
							if err != nil || got.Head != first.Intent.IntendedHead || got.EffectState != GoalPublicationObserved {
								t.Fatalf("%s: intended effect not found: %+v %v", trace, got, err)
							}
						} else if err == nil || got.Head != "" || got.EffectState == GoalPublicationObserved {
							t.Fatalf("%s: invalid locator promoted: %+v %v", trace, got, err)
						}
						_, _ = appendGoalDraft(context.Background(), f.request, func(context.Context, GoalPublicationRequest, string) error {
							writes++
							return errors.New("unexpected replay")
						})
						if writes != 1 || f.ciCalls != 0 {
							t.Fatalf("%s: writes=%d CI reads=%d; locator is not acceptance", trace, writes, f.ciCalls)
						}
						t.Logf("%s: one write, locator=%t, no acceptance/CI observation", trace, wantLocator)
					})
				}
			}
		}
	}
}

func goalTrackPushCommands(t *testing.T) func() int {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "pushes")
	script := "#!/bin/sh\nfor arg do\n  if test \"$arg\" = push; then printf 'push\\n' >> \"$GOAL_PUSH_LOG\"; fi\ndone\nexec \"$GOAL_REAL_GIT\" \"$@\"\n"
	if err = os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOAL_REAL_GIT", real)
	t.Setenv("GOAL_PUSH_LOG", log)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() int {
		data, err := os.ReadFile(log)
		if os.IsNotExist(err) {
			return 0
		}
		if err != nil {
			t.Fatal(err)
		}
		return strings.Count(string(data), "push\n")
	}
}

func TestGoalPublicationTokenPreparationFailureDoesNotConsumeIntent(t *testing.T) {
	f := newGoalPublicationFixture(t)
	pushes := goalTrackPushCommands(t)
	issued, released := 0, 0
	f.request.PublicationToken = func(context.Context) (string, func(), error) {
		issued++
		if issued == 1 {
			return "", nil, &AppTokenError{Failure: AppTokenHTTPRejected, HTTPStatus: 403}
		}
		return "fresh-private-token", func() { released++ }, nil
	}
	first, err := AppendGoalDraft(context.Background(), f.request)
	var token *AppTokenError
	if !errors.As(err, &token) || first.Intent != nil || pushes() != 0 {
		t.Fatalf("preparation consumed write: %+v pushes=%d %v", first, pushes(), err)
	}
	if _, err = os.Stat(filepath.Join(f.request.StateDir, goalIntentName)); !os.IsNotExist(err) {
		t.Fatal("failed token preparation retained an intent")
	}
	second, err := AppendGoalDraft(context.Background(), f.request)
	if err != nil || second.Head == "" || second.Intent == nil || pushes() != 1 || issued != 2 || released != 1 {
		t.Fatalf("fresh prepared attempt could not publish: %+v pushes=%d issued=%d released=%d %v", second, pushes(), issued, released, err)
	}
}

func TestGoalPublicationPersistenceFailureReleasesPreparedTokenWithoutPush(t *testing.T) {
	f := newGoalPublicationFixture(t)
	pushes := goalTrackPushCommands(t)
	released := 0
	f.request.PublicationToken = func(context.Context) (string, func(), error) {
		// A host state I/O failure after successful token preparation, but before
		// durable intent and therefore before the execute closure can be called.
		if err := os.WriteFile(f.request.StateDir, []byte("not a state directory"), 0600); err != nil {
			t.Fatal(err)
		}
		return "private-prepared-token", func() { released++ }, nil
	}
	got, err := AppendGoalDraft(context.Background(), f.request)
	if err == nil || got.Intent != nil || pushes() != 0 || released != 1 {
		t.Fatalf("persistence failure executed or leaked token: %+v pushes=%d released=%d %v", got, pushes(), released, err)
	}
}
