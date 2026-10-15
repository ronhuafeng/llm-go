package protocolsync

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/checkacceptance"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/codexexec"
)

type goalRunnerFunc func(context.Context, string, func(context.Context) (string, error)) (codexexec.GoalResult, error)

func (f goalRunnerFunc) RunGoal(c context.Context, s string, v func(context.Context) (string, error)) (codexexec.GoalResult, error) {
	return f(c, s, v)
}
func TestGoalFlowRetainsNativeFailureStatusWithoutPrivateError(t *testing.T) {
	fixture := newGoalPublicationFixture(t)
	var progress bytes.Buffer
	req := GoalFlowRequest{Objective: "one goal", Local: GoalVerifyRequest{DiagnosticDir: t.TempDir()}, Publication: fixture.request, Progress: &GoalProgress{Output: &progress, Actions: true}}
	runtimeErr := errors.New("private provider error detail")
	runner := goalRunnerFunc(func(context.Context, string, func(context.Context) (string, error)) (codexexec.GoalResult, error) {
		return codexexec.GoalResult{ThreadID: "native-thread", Status: "goal_turn_failed"}, runtimeErr
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := runUpstreamGoal(ctx, req, runner, goalFlowOperations{})
	if !errors.Is(err, runtimeErr) || result.ThreadID != "native-thread" || result.Head != "" {
		t.Fatal("native failure changed or acquired acceptance", result, err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var retained map[string]any
	if err = json.Unmarshal(data, &retained); err != nil {
		t.Fatal(err)
	}
	if retained["NativeStatus"] != "goal_turn_failed" {
		t.Fatal("native failure status omitted from retained JSON", string(data))
	}
	if bytes.Contains(data, []byte(runtimeErr.Error())) || strings.Contains(progress.String(), runtimeErr.Error()) || result.Failure == "" {
		t.Fatal("private runtime error retained")
	}
}
func TestNativeGoalFlowRequiresActualAcceptance(t *testing.T) {
	for _, scenario := range []string{"passes", "new_paths", "never_calls", "local_failure", "ci_failure", "mutates_after", "late_failed_tool", "blocked", "recovered_local_failure", "final_ci_failure"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newGoalPublicationFixture(t)
			if scenario == "new_paths" {
				goalTestWrite(t, fixture.request.RepoRoot, "codexsdk/internal/protocolgen/testdata/union.json", `{"type":"string"}`)
				goalTestWrite(t, fixture.request.RepoRoot, "codexsdk/internal/protocolsync/oracle_schema_helpers.go", "package protocolsync\n")
				goalTestWrite(t, fixture.request.RepoRoot, "codexsdk/internal/protocolsync/goal_publication.go", "package protocolsync\n")
				goalTestWrite(t, fixture.request.RepoRoot, ".github/workflows/pr-verification.yml", "name: proposed workflow\n")
				fixture.request.CandidateDir = filepath.Join(t.TempDir(), "candidate")
				var err error
				fixture.request.SnapshotDigest, err = goalSnapshot(context.Background(), fixture.request.RepoRoot, fixture.request.CandidateDir)
				if err != nil {
					t.Fatal(err)
				}
			}
			diag := t.TempDir()
			localPath := filepath.Join(diag, "local.jsonl")
			if err := os.WriteFile(localPath, []byte("{\"kind\":\"result\",\"passed\":true}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			var progress bytes.Buffer
			req := GoalFlowRequest{Objective: "one goal", Local: GoalVerifyRequest{RepoRoot: fixture.request.RepoRoot, DiagnosticDir: diag}, Publication: fixture.request, Progress: &GoalProgress{Output: &progress, Actions: true}}
			count := 0
			ops := goalFlowOperations{
				local: func(context.Context, GoalVerifyRequest) (GoalVerifyResult, error) {
					count++
					r := GoalVerifyResult{SnapshotRoot: fixture.request.CandidateDir, SnapshotDigest: fixture.request.SnapshotDigest, DiagnosticPath: localPath}
					if scenario == "local_failure" || scenario == "late_failed_tool" && count > 1 || scenario == "recovered_local_failure" && count == 1 {
						return r, errors.New("failed")
					}
					return r, nil
				},
				publish: AppendGoalDraft, observe: ObserveGoalCI,
				diagnostics: func(context.Context, GoalPublicationRequest, checkacceptance.Observation, *goalDiagnostics) error {
					return nil
				},
			}
			if scenario == "ci_failure" {
				fixture.state = checkacceptance.Failed
			}
			runner := goalRunnerFunc(func(ctx context.Context, _ string, verify func(context.Context) (string, error)) (codexexec.GoalResult, error) {
				if scenario != "never_calls" {
					path, _ := verify(ctx)
					if path == "" {
						t.Error("missing diagnostic path")
					}
				}
				if scenario == "late_failed_tool" {
					_, _ = verify(ctx)
				}
				if scenario == "recovered_local_failure" {
					if _, err := verify(ctx); err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "final_ci_failure" {
					fixture.state = checkacceptance.Cancelled
				}
				if scenario == "mutates_after" {
					goalTestWrite(t, req.Local.RepoRoot, "README.md", "changed after CI")
				}
				status := "complete"
				if scenario == "blocked" {
					status = "blocked"
				}
				return codexexec.GoalResult{ThreadID: "same-native-thread", Status: status}, nil
			})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			result, err := runUpstreamGoal(ctx, req, runner, ops)
			wantPass := scenario == "passes" || scenario == "new_paths" || scenario == "recovered_local_failure"
			if (err == nil) != wantPass {
				t.Fatalf("scenario %s got %v", scenario, err)
			}
			if wantPass && (result.Head == "" || result.Checks.State != checkacceptance.Succeeded) {
				t.Fatalf("missing acceptance %+v", result)
			}
			if (result.Failure == "") != wantPass || strings.Count(progress.String(), "::group::") != strings.Count(progress.String(), "::endgroup::") || strings.Contains(progress.String(), "::error") {
				t.Fatalf("misleading progress: %+v\n%s", result, progress.String())
			}
			if scenario == "final_ci_failure" && (result.Checks.State != checkacceptance.Cancelled || result.Stage != "Final acceptance") {
				t.Fatal("final failure retained earlier green", result)
			}
			if scenario == "new_paths" {
				data, err := os.ReadFile(result.DiagnosticPath)
				if err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"codexsdk/internal/protocolgen/testdata/union.json", "codexsdk/internal/protocolsync/oracle_schema_helpers.go", "codexsdk/internal/protocolsync/goal_publication.go", ".github/workflows/pr-verification.yml"} {
					if !strings.Contains(strings.Join(result.ChangedPaths, "\n"), name) || !bytes.Contains(data, []byte(name)) {
						t.Fatalf("published path missing from review evidence: %s", name)
					}
					if got := goalTestGit(t, fixture.request.RepoRoot, "show", result.Head+":"+name); got == "" {
						t.Fatalf("candidate content omitted: %s", name)
					}
				}
			}
		})
	}
}
func goalArchiveFixture(t *testing.T, content string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	f, e := w.Create("diagnostics.jsonl")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Write([]byte(content)); e != nil {
		t.Fatal(e)
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func TestGoalCIArchiveRequiresExactCompleteIdentity(t *testing.T) {
	head := strings.Repeat("a", 40)
	valid := fmt.Sprintf("{\"kind\":\"ci_identity\",\"gate\":\"source\",\"head\":%q}\n{\"kind\":\"ci_result\",\"gate\":\"source\",\"head\":%q,\"complete\":true}\n", head, head)
	for _, tc := range []struct {
		name, data string
		ok         bool
	}{{"valid", valid, true}, {"wrong_head", strings.ReplaceAll(valid, head, strings.Repeat("b", 40)), false}, {"missing_footer", strings.Split(valid, "\n")[0] + "\n", false}, {"incomplete", strings.Replace(valid, "true", "false", 1), false}, {"malformed", valid + "{", false}} {
		t.Run(tc.name, func(t *testing.T) {
			log, e := newGoalDiagnostics(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			defer log.file.Close()
			e = appendGoalCIArchive(log, goalArchiveFixture(t, tc.data), "source", head)
			if (e == nil) != tc.ok {
				t.Fatalf("got %v", e)
			}
		})
	}
}

func TestGoalDiagnosticImportsPreserveJSONNumbers(t *testing.T) {
	head := strings.Repeat("a", 40)
	witness := `{"large":9007199254740993,"huge":1e400}`
	local := filepath.Join(t.TempDir(), "local.jsonl")
	if err := os.WriteFile(local, []byte(witness+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	log, err := newGoalDiagnostics(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = appendGoalDiagnosticFile(log, local, "local_checker"); err != nil {
		t.Fatal(err)
	}
	text := fmt.Sprintf("{\"kind\":\"ci_identity\",\"gate\":\"source\",\"head\":%q}\n%s\n{\"kind\":\"ci_result\",\"gate\":\"source\",\"head\":%q,\"complete\":true}\n", head, witness, head)
	if err = appendGoalCIArchive(log, goalArchiveFixture(t, text), "source", head); err != nil {
		t.Fatal(err)
	}
	log.file.Close()
	data, err := os.ReadFile(log.file.Name())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "9007199254740993") != 2 || strings.Count(string(data), "1e400") != 2 {
		t.Fatalf("witness changed: %s", data)
	}
	for _, bad := range []string{witness + "\n" + text, text + witness + "\n", text + text, strings.Replace(text, `"kind":"ci_result","gate":"source"`, `"kind":"ci_result","gate":"live"`, 1)} {
		output, e := newGoalDiagnostics(t.TempDir())
		if e != nil {
			t.Fatal(e)
		}
		e = appendGoalCIArchive(output, goalArchiveFixture(t, bad), "source", head)
		output.file.Close()
		if e == nil {
			t.Fatal("accepted malformed CI record framing")
		}
	}
}
