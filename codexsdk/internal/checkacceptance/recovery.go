package checkacceptance

import (
	"fmt"
	"net/url"
	"strings"
)

// RecoverCancelled requests at most a second attempt of failed/cancelled jobs.
// A live job that started and did not succeed is never replayed automatically.
// The current PR identity and run/attempt are read back before this effect. An
// ambiguous POST is returned, never retried; a subsequent invocation observes
// the changed attempt instead of blindly repeating the request.
func RecoverCancelled(api API, repository, head string, pr int, runID int64, attempt int) error {
	if pr <= 0 || runID <= 0 || attempt != 1 {
		return fmt.Errorf("needs-maintainer: recovery is limited to one retry of attempt 1")
	}
	observation, err := Observe(api, repository, head)
	if err != nil {
		return err
	}
	if observation.WorkflowState != Cancelled || observation.WorkflowRunID != runID || observation.WorkflowAttempt != attempt {
		return fmt.Errorf("needs-maintainer: cancellation observation changed")
	}
	for _, check := range observation.Checks {
		if check.State == Failed {
			return fmt.Errorf("needs-maintainer: failed required check %s is not automatically retried", check.Name)
		}
	}
	if err := verifyRecoveryJobs(api, repository, head, runID, attempt); err != nil {
		return err
	}
	var current struct {
		State string `json:"state"`
		Head  struct {
			SHA  string `json:"sha"`
			Ref  string `json:"ref"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
	}
	if err := read(api, fmt.Sprintf("repos/%s/pulls/%d", repository, pr), &current); err != nil {
		return err
	}
	if current.State != "open" || current.Head.SHA != head || current.Head.Repo.FullName != repository {
		return fmt.Errorf("needs-maintainer: PR head changed before recovery")
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if current.Head.Ref == "" {
		return fmt.Errorf("needs-maintainer: PR has no head ref")
	}
	if err := read(api, fmt.Sprintf("repos/%s/git/ref/heads/%s", repository, url.PathEscape(current.Head.Ref)), &ref); err != nil {
		return err
	}
	if ref.Object.SHA != head {
		return fmt.Errorf("needs-maintainer: remote head changed before recovery")
	}
	var currentRun WorkflowRun
	path := fmt.Sprintf("repos/%s/actions/runs/%d", repository, runID)
	if err := read(api, path, &currentRun); err != nil {
		return err
	}
	if currentRun.ID != runID || currentRun.HeadSHA != head || currentRun.Attempt != attempt || workflowState(currentRun) != Cancelled {
		return fmt.Errorf("needs-maintainer: run changed before recovery")
	}
	latest, err := observeWorkflow(api, repository, head, Observation{Head: head})
	if err != nil {
		return err
	}
	if latest.WorkflowRunID != runID || latest.WorkflowAttempt != attempt || latest.WorkflowState != Cancelled {
		return fmt.Errorf("needs-maintainer: latest workflow changed before recovery")
	}
	return api.Request("POST", path+"/rerun-failed-jobs", nil, nil)
}

func verifyRecoveryJobs(api API, repository, head string, runID int64, attempt int) error {
	total, count := -1, 0
	for page := 1; page <= 100; page++ {
		var batch struct {
			Total int `json:"total_count"`
			Jobs  []struct {
				Name       string  `json:"name"`
				HeadSHA    string  `json:"head_sha"`
				Status     string  `json:"status"`
				Conclusion string  `json:"conclusion"`
				StartedAt  *string `json:"started_at"`
				Steps      []struct {
					Status string `json:"status"`
				} `json:"steps"`
			} `json:"jobs"`
		}
		path := fmt.Sprintf("repos/%s/actions/runs/%d/attempts/%d/jobs?per_page=100&page=%d", repository, runID, attempt, page)
		if err := read(api, path, &batch); err != nil {
			return err
		}
		if total < 0 {
			total = batch.Total
		} else if total != batch.Total {
			return fmt.Errorf("needs-maintainer: jobs changed during recovery observation")
		}
		for _, job := range batch.Jobs {
			if job.HeadSHA != head || job.Status != "completed" {
				return fmt.Errorf("needs-maintainer: job identity or state changed")
			}
			if job.Conclusion != "success" && job.Conclusion != "cancelled" && job.Conclusion != "skipped" {
				return fmt.Errorf("needs-maintainer: job failure requires investigation")
			}
			if strings.Contains(job.Name, "Live scenarios") && job.Conclusion != "success" {
				if job.StartedAt != nil {
					return fmt.Errorf("needs-maintainer: cancelled live job already started")
				}
				// A started setup/test step can already have called the model. No inference
				// from cancellation or a missing final result licenses another live call.
				for _, step := range job.Steps {
					if step.Status != "queued" {
						return fmt.Errorf("needs-maintainer: cancelled live execution must not be replayed")
					}
				}
			}
		}
		count += len(batch.Jobs)
		if len(batch.Jobs) < 100 {
			if total != count {
				return fmt.Errorf("needs-maintainer: incomplete jobs listing")
			}
			return nil
		}
	}
	return fmt.Errorf("needs-maintainer: jobs listing exceeds bounded observation")
}
