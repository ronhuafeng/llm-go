package protocolsync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
)

// PublicationAPI is the GitHub JSON boundary used for native publication state.
// A failed read is unknown, never an empty result.
type PublicationAPI interface {
	Request(method, path string, body any, result any) error
}

type githubPublicationAPI struct{ repoRoot string }

func (api githubPublicationAPI) Request(method, path string, body, result any) error {
	args := []string{"api", "--method", method, path}
	var input []byte
	if body != nil {
		var err error
		input, err = json.Marshal(body)
		if err != nil {
			return err
		}
		args = append(args, "--input", "-")
	}
	cmd := exec.Command("gh", args...)
	cmd.Dir = api.repoRoot
	if body != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("GitHub %s %s: %w: %s", method, path, err, stderr.String())
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(stdout.Bytes(), result); err != nil {
		return fmt.Errorf("decode GitHub %s: %w", path, err)
	}
	return nil
}

type publicationUser struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}
type publicationRef struct {
	Ref  string `json:"ref"`
	SHA  string `json:"sha"`
	Repo struct {
		FullName string `json:"full_name"`
	} `json:"repo"`
}
type publicationPR struct {
	Number   int             `json:"number"`
	URL      string          `json:"html_url"`
	State    string          `json:"state"`
	Body     string          `json:"body"`
	MergedAt *string         `json:"merged_at"`
	User     publicationUser `json:"user"`
	Head     publicationRef  `json:"head"`
	Base     publicationRef  `json:"base"`
	MergeSHA string          `json:"merge_commit_sha"`
}

func listPublicationPRs(api PublicationAPI, repository string) ([]publicationPR, error) {
	var all []publicationPR
	for page := 1; ; page++ {
		var batch []publicationPR
		if err := api.Request("GET", fmt.Sprintf("repos/%s/pulls?state=all&per_page=100&page=%d", repository, page), nil, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			return all, nil
		}
	}
}
