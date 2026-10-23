package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/checkacceptance"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolsync"
)

// The summary is an offline view of retained controller results. It cannot
// acquire checks, reinterpret acceptance, or perform repository effects.
func runGoalSummary(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("goal-summary", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("evidence-dir", "", "retained goal source, binding and result directory")
	repo := fs.String("repository", "", "GitHub repository for evidence links")
	outcome := fs.String("goal-outcome", "", "native goal step outcome supplied by Actions")
	runID := fs.Int64("run-id", 0, "upstream workflow run ID")
	artifactID := fs.Int64("artifact-id", 0, "uploaded evidence artifact ID")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *dir == "" || !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(*repo) {
		fmt.Fprintln(stderr, "goal-summary: evidence directory and repository required")
		return 2
	}
	var source struct{ Target protocolsync.Target }
	var binding struct {
		Number        int
		ControllerSHA string `json:"controller_sha"`
		Base          string
		Confirmed     bool
	}
	var result protocolsync.GoalFlowResult
	inputs := []struct {
		name string
		read bool
	}{
		{"goal-source.json", readGoalSummaryInput(filepath.Join(*dir, "goal-source.json"), &source)},
		{"goal-binding.json", readGoalSummaryInput(filepath.Join(*dir, "goal-binding.json"), &binding)},
		{"goal-result.json", readGoalSummaryInput(filepath.Join(*dir, "goal-result.json"), &result)},
	}
	var missing []string
	for _, input := range inputs {
		if !input.read {
			missing = append(missing, input.name)
		}
	}
	var b strings.Builder
	fmt.Fprint(&b, "## Native upstream goal\n\n")
	label := map[string]string{"success": "succeeded", "failure": "failed", "cancelled": "cancelled", "skipped": "not executed"}[*outcome]
	if label == "" {
		label = "outcome unavailable"
	}
	fmt.Fprintf(&b, "**Goal command: %s.**\n\n", label)
	if len(missing) > 0 {
		fmt.Fprintf(&b, "Evidence unavailable or incomplete: %s. No result is inferred from missing files.\n\n", strings.Join(missing, ", "))
	}
	baseURL := "https://github.com/" + *repo
	fmt.Fprintf(&b, "- Target: %s · upstream commit %s\n", goalSummaryText(source.Target.RefName), goalSummaryCommit("https://github.com/openai/codex", source.Target.PeeledCommitSHA))
	controller := binding.ControllerSHA
	if controller == "" {
		controller = binding.Base
	} // legacy diagnostic evidence only
	fmt.Fprintf(&b, "- Control commit: %s\n", goalSummaryCommit(baseURL, controller))
	fmt.Fprintf(&b, "- Original compatibility base: %s\n", goalSummaryCommit(baseURL, binding.Base))
	if binding.Number > 0 {
		state := "binding unconfirmed"
		if binding.Confirmed {
			state = "Draft binding confirmed"
		}
		fmt.Fprintf(&b, "- PR: [#%d](%s/pull/%d) (%s)\n", binding.Number, baseURL, binding.Number, state)
	} else {
		fmt.Fprintln(&b, "- PR: no retained locator")
	}
	fmt.Fprintf(&b, "- Last Git-confirmed publication: %s (publication confirmation and acceptance are separate)\n", goalSummaryCommit(baseURL, result.Head))
	fmt.Fprintf(&b, "- Snapshot: %s\n- Native status: %s\n- Last controller stage: %s\n", goalSummaryText(result.SnapshotDigest), goalSummaryText(result.NativeStatus), goalSummaryText(result.Stage))
	if result.Failure != "" {
		fmt.Fprintf(&b, "- Stop reason: %s\n", goalSummaryText(result.Failure))
	}
	if result.DiagnosticPath != "" {
		fmt.Fprintf(&b, "- Last verification file in the evidence artifact: %s\n", goalSummaryText(filepath.Base(result.DiagnosticPath)))
	}
	fmt.Fprint(&b, "\n### Retained CI observation\n\n")
	matching := summarySHA.MatchString(result.Head) && result.Checks.Head == result.Head
	if !matching {
		fmt.Fprint(&b, "No retained CI observation for the published head. Earlier-head results are not shown as its checks.\n\n")
	} else {
		fmt.Fprintf(&b, "Head: %s. Aggregate: %s. Workflow state: %s.\n\n", goalSummaryCommit(baseURL, result.Checks.Head), goalSummaryText(string(result.Checks.State)), goalSummaryText(string(result.Checks.WorkflowState)))
		if result.Checks.WorkflowRunID > 0 {
			fmt.Fprintf(&b, "[PR verification run](%s/actions/runs/%d), attempt %d.\n\n", baseURL, result.Checks.WorkflowRunID, result.Checks.WorkflowAttempt)
		}
	}
	fmt.Fprintln(&b, "| Required check | Last observed state | Evidence |\n| --- | --- | --- |")
	for _, name := range checkacceptance.RequiredNames() {
		state, link := "unavailable", "—"
		if matching {
			for _, c := range result.Checks.Checks {
				if c.Name == name {
					state = string(c.State)
					if c.RunID > 0 {
						link = fmt.Sprintf("[Check](%s/runs/%d)", baseURL, c.RunID)
					}
					break
				}
			}
		}
		fmt.Fprintf(&b, "| %s | %s | %s |\n", name, goalSummaryText(state), link)
	}
	fmt.Fprintln(&b)
	if *runID > 0 {
		fmt.Fprintf(&b, "[Upstream run and logs](%s/actions/runs/%d)", baseURL, *runID)
		if *artifactID > 0 {
			fmt.Fprintf(&b, " · [Retained verification evidence](%s/actions/runs/%d/artifacts/%d)", baseURL, *runID, *artifactID)
		} else {
			fmt.Fprint(&b, " · Evidence artifact link unavailable")
		}
		fmt.Fprint(&b, "\n\n")
	}
	fmt.Fprintln(&b, "These are observations retained by this run; follow the links for current PR/CI state. Native goal completion alone is not acceptance. This summary grants no publication, Ready or integration authority.")
	if _, err := io.WriteString(stdout, b.String()); err != nil {
		fmt.Fprintln(stderr, "goal-summary: cannot write summary")
		return 1
	}
	return 0
}

var summarySHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

func goalSummaryCommit(baseURL, sha string) string {
	if !summarySHA.MatchString(sha) {
		return "unavailable"
	}
	return fmt.Sprintf("[%s](%s/commit/%s)", sha, baseURL, sha)
}

func goalSummaryText(value string) string {
	if value == "" {
		return "unavailable"
	}
	if len(value) > 1024 {
		value = value[:1024] + "… (display shortened)"
	}
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	value = html.EscapeString(value)
	return strings.NewReplacer("|", "&#124;", "`", "&#96;", "[", "&#91;", "]", "&#93;", "*", "&#42;", "_", "&#95;", "\\", "&#92;").Replace(value)
}

func readGoalSummaryInput[T any](path string, value *T) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	const limit = 1 << 20
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || len(b) > limit || !strings.HasPrefix(strings.TrimSpace(string(b)), "{") {
		return false
	}
	var decoded T
	if json.Unmarshal(b, &decoded) != nil {
		return false
	}
	*value = decoded
	return true
}
