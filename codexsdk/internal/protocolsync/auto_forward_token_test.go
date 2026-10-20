package protocolsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/checkacceptance"
)

type forwardTokenAPI struct {
	forwardNativeReadAPI
	missingCheck string
}

func (api forwardTokenAPI) Request(method, path string, body, result any) error {
	var data any
	switch {
	case strings.Contains(path, "/check-runs?"):
		var runs []checkacceptance.Run
		for i, name := range checkacceptance.RequiredNames() {
			if name == api.missingCheck {
				continue
			}
			run := checkacceptance.Run{ID: int64(i + 1), Name: name, HeadSHA: api.head, Status: "completed", Conclusion: "success"}
			run.App.Slug = "github-actions"
			runs = append(runs, run)
		}
		data = map[string]any{"total_count": len(runs), "check_runs": runs}
	case strings.Contains(path, "/actions/workflows/pr-verification.yml/runs?"):
		data = map[string]any{"total_count": 1, "workflow_runs": []checkacceptance.WorkflowRun{{ID: 1, HeadSHA: api.head, Attempt: 1, Status: "completed", Conclusion: "success"}}}
	default:
		return api.forwardNativeReadAPI.Request(method, path, body, result)
	}
	if method != "GET" || body != nil {
		api.t.Fatal("acceptance fixture received a write")
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, result)
}

func forwardTokenFixture(t *testing.T) (root, remote, main, head string, op *NativeForward) {
	t.Helper()
	root = t.TempDir()
	goalTestGit(t, root, "init", "-q", "-b", "main")
	goalTestGit(t, root, "config", "user.name", "Fixture")
	goalTestGit(t, root, "config", "user.email", "fixture@example.invalid")
	goalTestWrite(t, root, ".github/workflows/fixture.yml", "name: before\n")
	goalTestGit(t, root, "add", ".")
	goalTestGit(t, root, "commit", "-qm", "M")
	main = goalTestGit(t, root, "rev-parse", "HEAD")
	goalTestGit(t, root, "checkout", "-qb", "proposal")
	goalTestWrite(t, root, ".github/workflows/fixture.yml", "name: after\n")
	goalTestGit(t, root, "add", ".")
	goalTestGit(t, root, "commit", "-qm", "workflow change")
	goalTestGit(t, root, "commit", "--allow-empty", "-qm", "published H")
	head = goalTestGit(t, root, "rev-parse", "HEAD")
	remote = filepath.Join(t.TempDir(), "remote.git")
	goalTestGit(t, root, "clone", "--bare", root, remote)
	goalTestGit(t, root, "remote", "add", "origin", remote)
	var err error
	op, err = NewNativeForward(ForwardRequest{
		RepoRoot: root, Repository: "fixture/project", BaseBranch: "main", OriginalBase: main, VerificationBase: main, ExpectedHead: head, Number: 1,
		API:    forwardTokenAPI{forwardNativeReadAPI: forwardNativeReadAPI{t: t, head: head, branch: "proposal"}},
		Review: func(context.Context) error { return nil },
		Token: func(context.Context, bool) (string, func(), error) {
			t.Fatal("unexpected token preparation")
			return "", nil, errors.New("unexpected token preparation")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return
}

func TestForwardExistingWorkflowHeadUsesContentsOnly(t *testing.T) {
	root, remote, main, head, op := forwardTokenFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if changed, err := op.needsWorkflows(ctx, main, head); err != nil || !changed {
		t.Fatal("fixture has no workflow delta")
	}
	issued, revoked, reviewed := 0, 0, 0
	op.req.Review = func(context.Context) error { reviewed++; return nil }
	op.req.Token = func(_ context.Context, workflows bool) (string, func(), error) {
		if workflows || reviewed != 1 {
			t.Fatal("existing-H FF requested workflow authority or skipped review")
		}
		issued++
		return "fixture-token", func() { revoked++ }, nil
	}
	if err := op.Forward(ctx, head, main); err != nil {
		t.Fatal(err)
	}
	if issued != 1 || revoked != 1 || goalTestGit(t, remote, "rev-parse", "refs/heads/main") != head || goalTestGit(t, root, "rev-parse", "HEAD") != head {
		t.Fatal("normal FF or token lifecycle not observed")
	}
}

func TestForwardPreconditionsPreventTokenAndWrite(t *testing.T) {
	scenarios := []string{"head moved", "main moved", "review rejected", "proof stale"}
	for _, name := range checkacceptance.RequiredNames() {
		scenarios = append(scenarios, "missing "+name)
	}
	for _, scenario := range scenarios {
		t.Run(scenario, func(t *testing.T) {
			root, remote, main, head, op := forwardTokenFixture(t)
			currentMain, currentHead := main, head
			switch scenario {
			case "head moved":
				currentHead = goalTestGit(t, root, "commit-tree", head+"^{tree}", "-p", head, "-m", "external edit")
				goalTestGit(t, remote, "fetch", root, currentHead)
				goalTestGit(t, remote, "update-ref", "refs/heads/proposal", currentHead)
			case "main moved":
				currentMain = head
				goalTestGit(t, remote, "update-ref", "refs/heads/main", currentMain)
			case "review rejected":
				op.req.Review = func(context.Context) error { return errors.New("review required") }
			case "proof stale":
				delete(op.proofs, main+":"+head)
			default:
				api := op.req.API.(forwardTokenAPI)
				api.missingCheck = strings.TrimPrefix(scenario, "missing ")
				op.req.API = api
			}
			if err := op.Forward(context.Background(), head, main); err == nil {
				t.Fatal("unaccepted candidate advanced")
			}
			if goalTestGit(t, remote, "rev-parse", "refs/heads/main") != currentMain || goalTestGit(t, remote, "rev-parse", "refs/heads/proposal") != currentHead {
				t.Fatal("blocked integration changed remote refs")
			}
		})
	}
}

func TestForwardMainMovesAfterTokenStillFailsClosed(t *testing.T) {
	root, remote, main, head, op := forwardTokenFixture(t)
	// An ordinary FF could accept this intermediate ancestor. The trusted
	// pre-push hook must still reject its unproved main advertisement.
	moved := goalTestGit(t, root, "rev-parse", head+"^")
	released := false
	op.req.Token = func(_ context.Context, workflows bool) (string, func(), error) {
		if workflows {
			t.Fatal("FF requested workflow authority")
		}
		goalTestGit(t, remote, "update-ref", "refs/heads/main", moved)
		return "fixture-token", func() { released = true }, nil
	}
	if err := op.Forward(context.Background(), head, main); err == nil || !released || goalTestGit(t, remote, "rev-parse", "refs/heads/main") != moved {
		t.Fatal("main CAS guard or token cleanup lost")
	}
}

func TestPublishRewrittenWorkflowDiffStillRequestsWorkflows(t *testing.T) {
	root, remote, base, old, op := forwardTokenFixture(t)
	// Main accepted a workflow change; the rewritten proposal retains that
	// exact control content and only adds ordinary documentation.
	goalTestGit(t, root, "checkout", "-q", "main")
	goalTestWrite(t, root, ".github/workflows/fixture.yml", "name: accepted current main\n")
	goalTestGit(t, root, "add", ".")
	goalTestGit(t, root, "commit", "-qm", "accepted workflow")
	main := goalTestGit(t, root, "rev-parse", "HEAD")
	goalTestWrite(t, root, "docs/proposal.md", "ordinary proposal\n")
	goalTestGit(t, root, "add", ".")
	goalTestGit(t, root, "commit", "-qm", "rewritten proposal")
	next := goalTestGit(t, root, "rev-parse", "HEAD")
	goalTestGit(t, remote, "fetch", root, main)
	goalTestGit(t, remote, "update-ref", "refs/heads/main", main)
	if base == main || next == old {
		t.Fatal("fixture did not create new main and candidate")
	}
	proved, issued, revoked := 0, 0, 0
	op.req.VerifyCandidate = func(_ context.Context, m, h string) error {
		if m != main || h != next {
			t.Fatal("publication verified a different candidate")
		}
		proved++
		return nil
	}
	op.req.Token = func(_ context.Context, workflows bool) (string, func(), error) {
		if !workflows || proved != 1 {
			t.Fatal("rewritten workflow publication lost authority or proof requirement")
		}
		issued++
		return "fixture-token", func() { revoked++ }, nil
	}
	if err := op.Publish(context.Background(), old, next, main); err != nil {
		t.Fatal(err)
	}
	if issued != 1 || revoked != 1 || goalTestGit(t, remote, "rev-parse", "refs/heads/proposal") != next || goalTestGit(t, remote, "rev-parse", "refs/heads/main") != main {
		t.Fatal("leased publication or token lifecycle not observed")
	}
}

func TestForwardTokenErrorsKeepOnlySafeTypedFacts(t *testing.T) {
	const marker = "private-transport-body-header-token-marker"
	typed := &AppTokenError{Failure: AppTokenHTTPRejected, HTTPStatus: 403}
	for _, input := range []error{errors.New(marker), fmt.Errorf("%s: %w", marker, typed)} {
		_, remote, main, head, op := forwardTokenFixture(t)
		op.req.Token = func(context.Context, bool) (string, func(), error) { return "", nil, input }
		err := op.Forward(context.Background(), head, main)
		if err == nil || strings.Contains(err.Error(), marker) || strings.Contains(err.Error(), "lacks") || goalTestGit(t, remote, "rev-parse", "refs/heads/main") != main {
			t.Fatal("token failure leaked, invented missing grant, or wrote main")
		}
		var got *AppTokenError
		if errors.As(input, &got) {
			if !errors.As(err, &got) || got.Failure != AppTokenHTTPRejected || got.HTTPStatus != 403 {
				t.Fatal("safe token diagnostic was discarded")
			}
		} else if err.Error() != "integration App token preparation failed" {
			t.Fatal("arbitrary token error was not made generic")
		}
	}
}
