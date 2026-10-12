package protocolsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// NewGoalAPI uses the host's existing gh identity. No credentials are copied into
// the candidate or supplied to Codex. Every request inherits the goal deadline.
func NewGoalAPI(ctx context.Context, repoRoot string) PublicationAPI {
	return goalGitHubAPI{ctx, repoRoot}
}

type goalGitHubAPI struct {
	ctx  context.Context
	root string
}

func (api goalGitHubAPI) Request(method, path string, body, result any) error {
	args := []string{"api", "--method", method, path}
	var input []byte
	if body != nil {
		var e error
		input, e = json.Marshal(body)
		if e != nil {
			return errors.New("invalid GitHub request")
		}
		args = append(args, "--input", "-")
	}
	cmd := exec.CommandContext(api.ctx, "gh", args...)
	cmd.Dir = api.root
	if method == "GET" {
		cmd.Env = goalReadEnvironment()
	}
	if body != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	var output boundedGoalArchive
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil || output.tooLarge {
		return errors.New("GitHub operation failed")
	}
	if result != nil && json.Unmarshal(output.Bytes(), result) != nil {
		return errors.New("GitHub response invalid")
	}
	return nil
}

// ValidateGoalBinding is a read-only launch precondition. The publication path
// repeats it at the effect boundary because model work and CI may take time.
func ValidateGoalBinding(ctx context.Context, req GoalPublicationRequest) error {
	if err := goalBinding(req); err != nil {
		return err
	}
	return goalCurrent(ctx, req, req.ExpectedHead)
}

func goalReadEnvironment() []string {
	env := os.Environ()
	read := os.Getenv("GH_READ_TOKEN")
	if read == "" {
		return env
	}
	var out []string
	for _, v := range env {
		if !strings.HasPrefix(v, "GH_TOKEN=") {
			out = append(out, v)
		}
	}
	return append(out, "GH_TOKEN="+read)
}

// Only goal-owned network observations get this deadline-bound adapter.
func goalLookupContext(ctx context.Context, existing RemoteLookuper) RemoteLookuper {
	if existing != nil {
		return existing
	}
	return goalContextLookuper{ctx}
}

type goalContextLookuper struct{ ctx context.Context }

func (l goalContextLookuper) LSRemote(remote string, patterns ...string) (string, error) {
	cmd := exec.CommandContext(l.ctx, "git", append([]string{"ls-remote", remote}, patterns...)...)
	cmd.WaitDelay = 250 * time.Millisecond
	var output boundedGoalArchive
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil || output.tooLarge {
		return "", errors.New("deadline-bound upstream lookup failed")
	}
	return output.String(), nil
}
