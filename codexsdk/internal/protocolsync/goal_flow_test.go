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
	req := GoalFlowRequest{Objective: "one goal", Local: GoalVerifyRequest{DiagnosticDir: t.TempDir()}, Publication: fixture.request}
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
	if bytes.Contains(data, []byte(runtimeErr.Error())) {
		t.Fatal("private runtime error retained")
	}
}
func TestNativeGoalFlowRequiresActualAcceptance(t *testing.T) {
	for _, scenario := range []string{"passes", "never_calls", "local_failure", "ci_failure", "mutates_after", "late_failed_tool", "blocked"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newGoalPublicationFixture(t)
			diag := t.TempDir()
			localPath := filepath.Join(diag, "local.jsonl")
			if err := os.WriteFile(localPath, []byte("{\"kind\":\"result\",\"passed\":true}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			req := GoalFlowRequest{Objective: "one goal", Local: GoalVerifyRequest{RepoRoot: fixture.request.RepoRoot, DiagnosticDir: diag}, Publication: fixture.request}
			count := 0
			ops := goalFlowOperations{
				local: func(context.Context, GoalVerifyRequest) (GoalVerifyResult, error) {
					count++
					r := GoalVerifyResult{SnapshotRoot: fixture.request.CandidateDir, SnapshotDigest: fixture.request.SnapshotDigest, DiagnosticPath: localPath}
					if scenario == "local_failure" || scenario == "late_failed_tool" && count > 1 {
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
			if (err == nil) != (scenario == "passes") {
				t.Fatalf("scenario %s got %v", scenario, err)
			}
			if scenario == "passes" && (result.Head == "" || result.Checks.State != checkacceptance.Succeeded) {
				t.Fatalf("missing acceptance %+v", result)
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
