package protocolsync

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/checkacceptance"
)

// collectGoalCIDiagnostics reads data from the already authenticated exact-head
// workflow. It never executes artifacts, downloads repository code, or retries CI.
func collectGoalCIDiagnostics(ctx context.Context, req GoalPublicationRequest, observation checkacceptance.Observation, log *goalDiagnostics) error {
	if observation.WorkflowRunID <= 0 || observation.WorkflowAttempt <= 0 || observation.Head == "" {
		return errors.New("CI diagnostic identity unavailable")
	}
	var listing struct {
		Total     int `json:"total_count"`
		Artifacts []struct {
			ID      int64  `json:"id"`
			Name    string `json:"name"`
			Expired bool   `json:"expired"`
			Size    int64  `json:"size_in_bytes"`
		} `json:"artifacts"`
	}
	path := fmt.Sprintf("repos/%s/actions/runs/%d/artifacts?per_page=100", req.Repository, observation.WorkflowRunID)
	if err := req.API.Request("GET", path, nil, &listing); err != nil {
		return errors.New("CI diagnostics listing unavailable")
	}
	if listing.Total > 100 {
		return errors.New("CI diagnostic artifact listing exceeds bound")
	}
	gates := []string{"source", "generated", "provenance", "live"}
	for _, gate := range gates {
		name := fmt.Sprintf("protocol-goal-%s-%s-%d", gate, observation.Head, observation.WorkflowAttempt)
		var id int64
		for _, a := range listing.Artifacts {
			if a.Name == name {
				if id != 0 || a.Expired || a.Size > goalDiagnosticLimit+(1<<20) {
					return errors.New("CI diagnostic artifact ambiguous or unavailable")
				}
				id = a.ID
			}
		}
		if id == 0 {
			return errors.New("complete CI diagnostics unavailable")
		}
		cmd := exec.CommandContext(ctx, "gh", "api", fmt.Sprintf("repos/%s/actions/artifacts/%d/zip", req.Repository, id))
		cmd.Dir = req.RepoRoot
		cmd.Env = goalReadEnvironment()
		var archive boundedGoalArchive
		cmd.Stdout = &archive
		if err := cmd.Run(); err != nil || archive.tooLarge {
			return errors.New("CI diagnostic artifact download failed")
		}
		if err := appendGoalCIArchive(log, archive.Bytes(), gate, observation.Head); err != nil {
			return err
		}
	}
	return nil
}

type boundedGoalArchive struct {
	bytes.Buffer
	tooLarge bool
}

func (w *boundedGoalArchive) Write(p []byte) (int, error) {
	if w.Len()+len(p) > goalDiagnosticLimit+(1<<20) {
		w.tooLarge = true
		return 0, errors.New("archive exceeds bound")
	}
	return w.Buffer.Write(p)
}

func appendGoalCIArchive(log *goalDiagnostics, data []byte, gate, head string) error {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return errors.New("invalid CI diagnostic archive")
	}
	// Each gate uploads exactly its one JSONL file. No filesystem extraction means
	// archive paths and links cannot overwrite candidate or controller state.
	if len(archive.File) != 1 {
		return errors.New("unexpected CI diagnostic artifact contents")
	}
	file := archive.File[0]
	if file.FileInfo().IsDir() || !strings.HasSuffix(file.Name, ".jsonl") || file.UncompressedSize64 > goalDiagnosticLimit {
		return errors.New("invalid CI diagnostic artifact member")
	}
	reader, err := file.Open()
	if err != nil {
		return errors.New("CI diagnostic artifact unreadable")
	}
	defer reader.Close()
	decoder := json.NewDecoder(io.LimitReader(reader, goalDiagnosticLimit+1))
 decoder.UseNumber()
	bound := false
	finished := false
	for {
		var record map[string]any
		err = decoder.Decode(&record)
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.New("CI diagnostic JSONL incomplete")
		}
        if finished || (!bound && record["kind"]!="ci_identity") {return errors.New("CI diagnostic framing invalid")}
		// The gate writer binds its header and footer; outputs are data under their
		// own labelled records, never an alternate controller verdict.
		if record["kind"] == "ci_identity" {
			if bound || record["head"] != head || record["gate"] != gate {
				return errors.New("CI diagnostic identity mismatch")
			}
			bound = true
		}
		if record["kind"] == "ci_result" {
			if !bound || record["head"] != head || record["gate"]!=gate || record["complete"] != true {
				return errors.New("CI diagnostic result incomplete")
			}
			finished = true
		}
		log.record("diagnostic_record", gate, map[string]any{"source": "ci_checker", "record": record})
		if log.err != nil {
			return errors.New("combined CI diagnostics exceed bound")
		}
	}
	if !bound || !finished {
		return errors.New("CI diagnostic evidence incomplete")
	}
	return nil
}
