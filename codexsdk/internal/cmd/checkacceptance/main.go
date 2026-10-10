// checkacceptance observes the shared exact-head acceptance policy. It is
// read-only: missing or unsuccessful checks never trigger a workflow rerun.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/checkacceptance"
)

type githubAPI struct{}

func (githubAPI) Request(method, path string, body, result any) error {
	command := exec.Command("gh", "api", "--method", method, path)
	command.Stderr = os.Stderr
	raw, err := command.Output()
	if err != nil {
		return fmt.Errorf("read GitHub checks: %w", err)
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(raw, result)
}

func main() {
	repository := flag.String("repository", "", "owner/repository")
	head := flag.String("head", "", "exact candidate head SHA")
	recover := flag.Bool("recover-cancelled", false, "retry only unstarted/safe cancelled jobs, at most once")
	pr := flag.Int("pr", 0, "current same-repository PR")
	runID := flag.Int64("run", 0, "observed workflow run ID")
	attempt := flag.Int("attempt", 0, "observed workflow attempt")
	flag.Parse()
	if *recover {
		if err := checkacceptance.RecoverCancelled(githubAPI{}, *repository, *head, *pr, *runID, *attempt); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("Requested one cancellation recovery; fresh acceptance remains pending.")
		return
	}
	observation, err := checkacceptance.Observe(githubAPI{}, *repository, *head)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(observation); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if observation.State != checkacceptance.Succeeded {
		os.Exit(1)
	}
}
