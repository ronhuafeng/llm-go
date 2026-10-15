package protocolsync

import (
	"fmt"
	"io"
	"strings"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/checkacceptance"
)

// GoalProgress projects controller observations into the caller's log. It does
// not decide acceptance or print model/command output. Calls are serialized by
// the existing verification callback and CI observer.
type GoalProgress struct {
	Output  io.Writer
	Actions bool
	grouped bool
}

func (p *GoalProgress) line(message string) {
	if p != nil && p.Output != nil {
		// Only this controller emits workflow command framing.
		fmt.Fprintln(p.Output, "goal: "+strings.NewReplacer("\r", " ", "\n", " ").Replace(message))
	}
}

func (p *GoalProgress) group(name string) {
	p.end()
	if p != nil && p.Actions && p.Output != nil {
		fmt.Fprintln(p.Output, "::group::"+goalCommandText(name))
		p.grouped = true
	} else {
		p.line(name)
	}
}

func (p *GoalProgress) end() {
	if p != nil && p.grouped {
		fmt.Fprintln(p.Output, "::endgroup::")
		p.grouped = false
	}
}

// Failure is only for terminal command failures. Recoverable verification
// failures stay in their log group. message must be a controller-owned
// explanation, never a private provider error.
func (p *GoalProgress) Failure(message string) {
	p.end()
	if p != nil && p.Actions && p.Output != nil {
		fmt.Fprintln(p.Output, "::error title=Upstream goal incomplete::"+goalCommandText(message))
	} else {
		p.line(message)
	}
}

func goalCommandText(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

// Only fixed policy names/states and validated effect locators enter the log.
func goalCIProgress(repository string, o checkacceptance.Observation) string {
	if !goalRepositoryPattern.MatchString(repository) || !shaRE.MatchString(o.Head) {
		return "CI observation unavailable"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "CI for %s: %s", o.Head, goalCheckState(o.State))
	if o.WorkflowRunID > 0 {
		fmt.Fprintf(&b, "; https://github.com/%s/actions/runs/%d (attempt %d)", repository, o.WorkflowRunID, o.WorkflowAttempt)
	}
	fmt.Fprintf(&b, "; workflow: %s", goalCheckState(o.WorkflowState))
	for _, name := range checkacceptance.RequiredNames() {
		for _, c := range o.Checks {
			if c.Name == name {
				fmt.Fprintf(&b, "; %s: %s", name, goalCheckState(c.State))
				break
			}
		}
	}
	return b.String()
}

func goalCheckState(s checkacceptance.State) string {
	switch s {
	case checkacceptance.Missing, checkacceptance.Pending, checkacceptance.Failed, checkacceptance.Cancelled, checkacceptance.Succeeded:
		return string(s)
	default:
		return "unavailable"
	}
}
