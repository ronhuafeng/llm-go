// Package checkacceptance owns required exact-head GitHub check observations.
// Observing a success acquires no fresh proof and never reruns any workflow.
package checkacceptance

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

// RequiredNames returns a copy of the repository's single required-check policy.
func RequiredNames() []string {
	return []string{
		"Root source verification",
		"Codex generated reproducibility / Generated reproducibility",
		"Codex protocol provenance",
		"Live Codex integration / Live scenarios",
	}
}

type State string

const (
	Missing   State = "missing"
	Pending   State = "pending"
	Failed    State = "failed"
	Cancelled State = "cancelled"
	Succeeded State = "succeeded"
)

type Run struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	HeadSHA    string `json:"head_sha"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	App        struct {
		Slug string `json:"slug"`
	} `json:"app"`
}

type Check struct {
	Name       string `json:"name"`
	State      State  `json:"state"`
	RunID      int64  `json:"run_id,omitempty"`
	Conclusion string `json:"conclusion,omitempty"`
}

type Observation struct {
	Head            string  `json:"head"`
	State           State   `json:"state"`
	Checks          []Check `json:"checks"`
	WorkflowRunID   int64   `json:"workflow_run_id,omitempty"`
	WorkflowAttempt int     `json:"workflow_attempt,omitempty"`
	WorkflowState   State   `json:"workflow_state"`
}

// Evaluate selects the latest GitHub Actions attempt by check-run ID for each
// required name on H. Neither another App nor a result from another H is proof.
func Evaluate(head string, runs []Run) Observation {
	result := Observation{Head: head, State: Succeeded}
	for _, name := range RequiredNames() {
		var latest Run
		for _, run := range runs {
			if run.Name == name && run.HeadSHA == head && run.App.Slug == "github-actions" && run.ID > latest.ID {
				latest = run
			}
		}
		check := Check{Name: name, State: Missing, RunID: latest.ID, Conclusion: latest.Conclusion}
		if latest.ID > 0 {
			check.State = Pending
			if latest.Status == "completed" {
				switch latest.Conclusion {
				case "success":
					check.State = Succeeded
				case "cancelled":
					check.State = Cancelled
				case "":
					check.State = Pending
				default:
					check.State = Failed
				}
			}
		}
		result.Checks = append(result.Checks, check)
	}
	// Terminal failures take precedence, but per-check facts retain every state.
	for _, state := range []State{Failed, Cancelled, Missing, Pending} {
		for _, check := range result.Checks {
			if check.State == state {
				result.State = state
				return result
			}
		}
	}
	return result
}

// API matches the existing native publication API. Reads fail closed.
type API interface {
	Request(method, path string, body, result any) error
}

var headPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Observe reads every page, including old attempts, before choosing the latest.
// A changing or oversized listing is unknown, not a partial successful result.
// No workflow dispatch or retry is performed, particularly for live scenarios.
func Observe(api API, repository, head string) (Observation, error) {
	if !headPattern.MatchString(head) || !repositoryPattern.MatchString(repository) {
		return Observation{}, fmt.Errorf("repository and exact 40-character head SHA are required")
	}
	var runs []Run
	seen := map[int64]bool{}
	total := -1
	for page := 1; page <= 100; page++ {
		var batch struct {
			Total int   `json:"total_count"`
			Runs  []Run `json:"check_runs"`
		}
		path := fmt.Sprintf("repos/%s/commits/%s/check-runs?filter=all&per_page=100&page=%d", repository, head, page)
		if err := read(api, path, &batch); err != nil {
			return Observation{}, err
		}
		if total < 0 {
			total = batch.Total
		} else if total != batch.Total {
			return Observation{}, fmt.Errorf("check listing changed while observing H %s", head)
		}
		for _, run := range batch.Runs {
			if run.ID <= 0 || seen[run.ID] {
				return Observation{}, fmt.Errorf("invalid or repeated check-run ID while observing H %s", head)
			}
			seen[run.ID] = true
		}
		runs = append(runs, batch.Runs...)
		if len(batch.Runs) < 100 {
			if len(runs) != total {
				return Observation{}, fmt.Errorf("incomplete check listing for H %s: got %d of %d", head, len(runs), total)
			}
			return observeWorkflow(api, repository, head, Evaluate(head, runs))
		}
	}
	return Observation{}, fmt.Errorf("check listing exceeds bounded observation for H %s", head)
}

// All required contexts are produced by PR verification. Its current attempt is
// a barrier: an attempt that has not created jobs yet cannot inherit old green
// check runs. The full check listing remains authoritative for each named proof.
type WorkflowRun struct {
	StartedAt  time.Time `json:"run_started_at"`
	ID         int64     `json:"id"`
	HeadSHA    string    `json:"head_sha"`
	Attempt    int       `json:"run_attempt"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
}

func workflowState(run WorkflowRun) State {
	if run.ID == 0 {
		return Missing
	}
	if run.Status != "completed" || run.Conclusion == "" {
		return Pending
	}
	switch run.Conclusion {
	case "success":
		return Succeeded
	case "cancelled":
		return Cancelled
	default:
		return Failed
	}
}

func observeWorkflow(api API, repository, head string, result Observation) (Observation, error) {
	var latest WorkflowRun
	var active bool
	count, total := 0, -1
	seen := map[int64]bool{}
	for page := 1; page <= 100; page++ {
		var batch struct {
			Total int           `json:"total_count"`
			Runs  []WorkflowRun `json:"workflow_runs"`
		}
		path := fmt.Sprintf("repos/%s/actions/workflows/pr-verification.yml/runs?head_sha=%s&per_page=100&page=%d", repository, head, page)
		if err := read(api, path, &batch); err != nil {
			return Observation{}, err
		}
		if total < 0 {
			total = batch.Total
		} else if total != batch.Total {
			return Observation{}, fmt.Errorf("workflow listing changed while observing H %s", head)
		}
		for _, run := range batch.Runs {
			if run.ID <= 0 || seen[run.ID] || run.HeadSHA != head || run.Attempt < 1 {
				return Observation{}, fmt.Errorf("invalid workflow observation for H %s", head)
			}
			seen[run.ID] = true
			if run.StartedAt.After(latest.StartedAt) || (run.StartedAt.Equal(latest.StartedAt) && run.ID > latest.ID) {
				latest = run
			}
			// An older run can itself be rerun. While any exact-head attempt is active,
			// wait rather than accepting before that attempt creates its new check jobs.
			active = active || workflowState(run) == Pending
		}
		count += len(batch.Runs)
		if len(batch.Runs) == 100 {
			continue
		}
		if count != total {
			return Observation{}, fmt.Errorf("incomplete workflow listing for H %s", head)
		}
		result.WorkflowRunID, result.WorkflowAttempt = latest.ID, latest.Attempt
		result.WorkflowState = workflowState(latest)
		if active {
			result.WorkflowState = Pending
		}
		if result.WorkflowState != Succeeded {
			result.State = result.WorkflowState
		}
		return result, nil
	}
	return Observation{}, fmt.Errorf("workflow listing exceeds bounded observation for H %s", head)
}

// Only read failures are retried, once. A fresh raw response prevents a partial
// failed decode from becoming evidence on retry. This never repeats an effect.
func read(api API, path string, result any) error {
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		var raw json.RawMessage
		err = api.Request("GET", path, nil, &raw)
		if err == nil {
			return json.Unmarshal(raw, result)
		}
		if attempt == 0 {
			time.Sleep(100 * time.Millisecond)
		}
	}
	return err
}
