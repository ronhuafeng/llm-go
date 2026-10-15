package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/checkacceptance"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolsync"
)

func summaryFixture(t *testing.T) (string, protocolsync.GoalFlowResult) {
	t.Helper()
	dir := t.TempDir()
	writeSummaryJSON(t, dir, "goal-source.json", map[string]any{"Target": protocolsync.Target{RefName: "rust-v1.2.3", PeeledCommitSHA: strings.Repeat("a", 40)}})
	writeSummaryJSON(t, dir, "goal-binding.json", map[string]any{"number": 7, "base": strings.Repeat("b", 40), "confirmed": true})
	r := protocolsync.GoalFlowResult{Head: strings.Repeat("c", 40), SnapshotDigest: strings.Repeat("d", 64), NativeStatus: "complete", Stage: "Final acceptance", DiagnosticPath: "/private/runner/verification-123.jsonl"}
	r.Checks = checkacceptance.Observation{Head: r.Head, State: checkacceptance.Succeeded, WorkflowState: checkacceptance.Succeeded, WorkflowRunID: 99, WorkflowAttempt: 2}
	for i, name := range checkacceptance.RequiredNames() {
		r.Checks.Checks = append(r.Checks.Checks, checkacceptance.Check{Name: name, State: checkacceptance.Succeeded, RunID: int64(i + 10)})
	}
	return dir, r
}

func writeSummaryJSON(t *testing.T, dir, name string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
		t.Fatal(err)
	}
}

func renderSummary(t *testing.T, dir, outcome string) string {
	t.Helper()
	var out, err bytes.Buffer
	if code := run([]string{"goal-summary", "-evidence-dir", dir, "-repository", "owner/repo", "-goal-outcome", outcome, "-run-id", "88", "-artifact-id", "77"}, &out, &err); code != 0 || err.Len() != 0 {
		t.Fatalf("summary: %d %s", code, &err)
	}
	return out.String()
}

func TestGoalSummaryBindsLinksAndChecksToRetainedHead(t *testing.T) {
	dir, r := summaryFixture(t)
	writeSummaryJSON(t, dir, "goal-result.json", r)
	out := renderSummary(t, dir, "success")
	for _, want := range []string{"Goal command: succeeded", "owner/repo/pull/7", "owner/repo/commit/" + r.Head, "owner/repo/actions/runs/99", "attempt 2", "owner/repo/runs/10", "owner/repo/actions/runs/88/artifacts/77", "verification-123.jsonl", "Native status: complete", "Final acceptance"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, "/private/runner") || strings.Count(out, "| succeeded |") != len(checkacceptance.RequiredNames()) {
		t.Fatalf("unexpected CI evidence or local path: %s", out)
	}
	r.Checks.Head = strings.Repeat("e", 40)
	writeSummaryJSON(t, dir, "goal-result.json", r)
	out = renderSummary(t, dir, "failure")
	if strings.Contains(out, "| succeeded |") || strings.Contains(out, "/actions/runs/99") || !strings.Contains(out, "Earlier-head results") {
		t.Fatalf("old green attached to new head: %s", out)
	}
}

func TestGoalSummaryDoesNotPromoteLifecycleOrPartialPublication(t *testing.T) {
	for _, tc := range []struct {
		outcome, native, stage, reason string
		state                          checkacceptance.State
	}{
		{"failure", "complete", "Final acceptance", "Candidate changed after accepted verification", checkacceptance.Succeeded},
		{"failure", "blocked", "Draft publication", "Draft publication could not be confirmed", checkacceptance.Missing},
		{"cancelled", "cancelled", "CI observation", "Native goal reached its execution deadline", checkacceptance.Pending},
		{"failure", "complete", "CI observation", "Exact-head CI has not passed: cancelled", checkacceptance.Cancelled},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			dir, r := summaryFixture(t)
			r.NativeStatus, r.Stage, r.Failure = tc.native, tc.stage, tc.reason
			r.Checks.State, r.Checks.WorkflowState = tc.state, tc.state
			for i := range r.Checks.Checks {
				r.Checks.Checks[i].State = tc.state
			}
			writeSummaryJSON(t, dir, "goal-result.json", r)
			out := renderSummary(t, dir, tc.outcome)
			if strings.Contains(out, "Goal command: succeeded") || !strings.Contains(out, tc.reason) || !strings.Contains(out, "Native status: "+tc.native) {
				t.Fatal(out)
			}
		})
	}
}

func TestGoalSummaryMissingMalformedAndOversizedEvidence(t *testing.T) {
	for _, content := range []string{"", "null", `{"Head":`, `{"Head":"` + strings.Repeat("c", 40) + `","NativeStatus":42}`, strings.Repeat(" ", 1<<20) + "{}"} {
		dir, _ := summaryFixture(t)
		if content != "" {
			if err := os.WriteFile(filepath.Join(dir, "goal-result.json"), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
		}
		out := renderSummary(t, dir, "failure")
		if !strings.Contains(out, "Evidence unavailable or incomplete: goal-result.json") || strings.Contains(out, "| succeeded |") || !strings.Contains(out, "Last Git-confirmed publication: unavailable") {
			t.Fatal(out)
		}
	}
	out := renderSummary(t, t.TempDir(), "skipped")
	if !strings.Contains(out, "Goal command: not executed") || !strings.Contains(out, "PR: no retained locator") {
		t.Fatal(out)
	}
}

func TestGoalSummaryEscapesDisplayAndDoesNotReadPrivatePayloads(t *testing.T) {
	dir, r := summaryFixture(t)
	writeSummaryJSON(t, dir, "goal-source.json", map[string]any{"Target": map[string]string{"RefName": "<script>\n::error::bad [link](https://example.org)|`"}, "Private": "private-provider-detail"})
	r.Failure = "x\r\n::error::bad | [link](url) <script>"
	writeSummaryJSON(t, dir, "goal-result.json", r)
	out := renderSummary(t, dir, "failure")
	for _, bad := range []string{"<script>", "\n::error", "[link]", "private-provider-detail", "bad |"} {
		if strings.Contains(out, bad) {
			t.Fatalf("unescaped or private value %q: %s", bad, out)
		}
	}
}

func TestNativeGoalPreflightAnnotationKeepsStdoutClean(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	var out, err bytes.Buffer
	if code := run([]string{"goal"}, &out, &err); code == 0 || out.Len() != 0 || !strings.Contains(err.String(), "::error title=Upstream goal incomplete::") {
		t.Fatalf("stdout=%q stderr=%q", &out, &err)
	}
}

func TestNativeGoalLaunchFailureRetainsSafeSummaryResult(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("PROTOCOL_SYNC_APP_CLIENT_ID", "")
	var out, err bytes.Buffer
	dir := t.TempDir()
	code := run([]string{"goal", "-controller-root", dir, "-workspace", dir, "-state-dir", dir, "-diagnostic-dir", dir, "-candidate-dir", dir}, &out, &err)
	var result protocolsync.GoalFlowResult
	if code != 1 || json.Unmarshal(out.Bytes(), &result) != nil || result.Stage != "Goal preparation" || result.Failure != "Publication App credentials unavailable" || !strings.Contains(err.String(), "::error title=") {
		t.Fatalf("stdout=%q stderr=%q", &out, &err)
	}
}
