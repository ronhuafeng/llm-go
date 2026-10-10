package checkacceptance

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

const testHead = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func successfulRuns() []Run {
	var runs []Run
	for i, name := range RequiredNames() {
		r := Run{ID: int64(i + 1), Name: name, HeadSHA: testHead, Status: "completed", Conclusion: "success"}
		r.App.Slug = "github-actions"
		runs = append(runs, r)
	}
	return runs
}
func TestExactHeadLatestActionsPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, status, conclusion, app, head string
		missing                             bool
		want                                State
	}{
		{name: "success", status: "completed", conclusion: "success", want: Succeeded},
		{name: "missing live", missing: true, want: Missing},
		{name: "live pending", status: "in_progress", want: Pending},
		{name: "live failed", status: "completed", conclusion: "failure", want: Failed},
		{name: "live cancelled", status: "completed", conclusion: "cancelled", want: Cancelled},
		{name: "skipped is not not-applicable", status: "completed", conclusion: "skipped", want: Failed},
		{name: "neutral not proof", status: "completed", conclusion: "neutral", want: Failed},
		{name: "another app", status: "completed", conclusion: "success", app: "other", want: Missing},
		{name: "another head", status: "completed", conclusion: "success", head: strings.Repeat("b", 40), want: Missing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runs := successfulRuns()
			live := runs[3]
			runs = runs[:3]
			if !tc.missing {
				live.Status = tc.status
				live.Conclusion = tc.conclusion
				if tc.app != "" {
					live.App.Slug = tc.app
				}
				if tc.head != "" {
					live.HeadSHA = tc.head
				}
				runs = append(runs, live)
			}
			if got := Evaluate(testHead, runs); got.State != tc.want {
				t.Fatalf("got %+v, want %s", got, tc.want)
			}
		})
	}
	for _, conclusion := range []string{"failure", "cancelled", ""} {
		runs := successfulRuns()
		newer := runs[3]
		newer.ID = 90
		newer.Conclusion = conclusion
		runs = append([]Run{newer}, runs...)
		if Evaluate(testHead, runs).State == Succeeded {
			t.Fatal("old success hid newer unsuccessful attempt")
		}
	}
}

type fakeAPI struct {
	runs      []Run
	workflows []WorkflowRun
	failPage  bool
	posts     int
	jobs      any
	changed   bool
}

func (f *fakeAPI) Request(method, path string, body, result any) error {
	if method == "POST" {
		f.posts++
		return nil
	}
	var value any
	switch {
	case strings.Contains(path, "/check-runs?"):
		page := 1
		if strings.Contains(path, "page=2") {
			page = 2
		}
		if page == 2 && f.failPage {
			return fmt.Errorf("transient read")
		}
		start := (page - 1) * 100
		end := min(start+100, len(f.runs))
		value = map[string]any{"total_count": len(f.runs), "check_runs": f.runs[start:end]}
	case strings.Contains(path, "/actions/workflows/"):
		value = map[string]any{"total_count": len(f.workflows), "workflow_runs": f.workflows}
	case strings.Contains(path, "/attempts/"):
		value = f.jobs
	case strings.Contains(path, "/pulls/"):
		head := testHead
		if f.changed {
			head = strings.Repeat("b", 40)
		}
		value = map[string]any{"state": "open", "head": map[string]any{"sha": head, "ref": "candidate", "repo": map[string]string{"full_name": "owner/repo"}}}
	case strings.Contains(path, "/git/ref/"):
		value = map[string]any{"object": map[string]string{"sha": testHead}}
	case strings.Contains(path, "/actions/runs/"):
		value = f.workflows[len(f.workflows)-1]
	default:
		return fmt.Errorf("unexpected %s", path)
	}
	raw, _ := json.Marshal(value)
	return json.Unmarshal(raw, result)
}
func goodAPI() *fakeAPI {
	return &fakeAPI{runs: successfulRuns(), workflows: []WorkflowRun{{ID: 1, HeadSHA: testHead, Attempt: 1, Status: "completed", Conclusion: "success"}}}
}
func TestPaginationAndWorkflowAttemptBarrier(t *testing.T) {
	f := goodAPI()
	for i := int64(5); i <= 101; i++ {
		f.runs = append(f.runs, Run{ID: i, Name: "extra"})
	}
	newer := f.runs[3]
	newer.ID = 102
	newer.Conclusion = "failure"
	f.runs = append(f.runs, newer)
	got, err := Observe(f, "owner/repo", testHead)
	if err != nil || got.State != Failed {
		t.Fatalf("later page ignored: %+v %v", got, err)
	}
	f.failPage = true
	if _, err := Observe(f, "owner/repo", testHead); err == nil {
		t.Fatal("failed page accepted")
	}
	f = goodAPI()
	f.workflows[0].Attempt = 2
	f.workflows[0].Status = "queued"
	f.workflows[0].Conclusion = ""
	got, err = Observe(f, "owner/repo", testHead)
	if err != nil || got.State != Pending {
		t.Fatalf("old checks masked queued attempt: %+v %v", got, err)
	}
	f.workflows[0].Status = "completed"
	f.workflows[0].Conclusion = "cancelled"
	got, err = Observe(f, "owner/repo", testHead)
	if err != nil || got.State != Cancelled {
		t.Fatalf("old checks masked cancelled attempt: %+v %v", got, err)
	}
	f = goodAPI()
	f.runs[3].ID = f.runs[0].ID
	if _, err := Observe(f, "owner/repo", testHead); err == nil {
		t.Fatal("duplicate listing accepted")
	}
}
func TestCancellationRecoveryBoundAndLiveNoReplay(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		attempt                      int
		liveStarted, failed, changed bool
		want                         bool
	}{
		{name: "one safe retry", attempt: 1, want: true},
		{name: "budget exhausted", attempt: 2},
		{name: "started live never replayed", attempt: 1, liveStarted: true},
		{name: "failed proof", attempt: 1, failed: true},
		{name: "head moved", attempt: 1, changed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := goodAPI()
			f.workflows[0].Conclusion = "cancelled"
			f.workflows[0].Attempt = tc.attempt
			f.changed = tc.changed
			if tc.failed {
				f.runs[3].Conclusion = "failure"
			}
			steps := []any{}
			if tc.liveStarted {
				steps = append(steps, map[string]string{"status": "completed"})
			}
			f.jobs = map[string]any{"total_count": 1, "jobs": []any{map[string]any{"name": "Live Codex integration / Live scenarios", "head_sha": testHead, "status": "completed", "conclusion": "cancelled", "steps": steps}}}
			err := RecoverCancelled(f, "owner/repo", testHead, 1, 1, tc.attempt)
			if (err == nil) != tc.want || f.posts != map[bool]int{true: 1, false: 0}[tc.want] {
				t.Fatalf("err=%v posts=%d", err, f.posts)
			}
		})
	}
}

type transientAPI struct {
	API
	failures, reads int
}

func (f *transientAPI) Request(method, path string, body, result any) error {
	f.reads++
	if f.failures > 0 {
		f.failures--
		return fmt.Errorf("temporary API error")
	}
	return f.API.Request(method, path, body, result)
}
func TestReadRecoveryIsFinite(t *testing.T) {
	f := &transientAPI{API: goodAPI(), failures: 1}
	observation, err := Observe(f, "owner/repo", testHead)
	if err != nil || observation.State != Succeeded || f.reads != 3 {
		t.Fatalf("observation=%+v err=%v reads=%d", observation, err, f.reads)
	}
	f = &transientAPI{API: goodAPI(), failures: 20}
	if _, err := Observe(f, "owner/repo", testHead); err == nil || f.reads != 2 {
		t.Fatalf("read budget: err=%v reads=%d", err, f.reads)
	}
}

func TestOlderWorkflowRerunIsLatestAttempt(t *testing.T) {
	f := goodAPI()
	f.workflows = []WorkflowRun{
		{ID: 20, HeadSHA: testHead, Attempt: 1, Status: "completed", Conclusion: "success", StartedAt: time.Unix(100, 0)},
		{ID: 10, HeadSHA: testHead, Attempt: 2, Status: "completed", Conclusion: "cancelled", StartedAt: time.Unix(200, 0)},
	}
	result, err := Observe(f, "owner/repo", testHead)
	if err != nil || result.State != Cancelled || result.WorkflowRunID != 10 || result.WorkflowAttempt != 2 {
		t.Fatalf("older run's newer attempt ignored: %+v %v", result, err)
	}
}
