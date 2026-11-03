package repository

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeGoalPreparesSourceWithoutAcceptedSemanticPlanner(t *testing.T) {
	root, _ := filepath.Abs("../../../..")
	job, _ := workflowJobByID(readWorkflow(t, root, "codexsdk-upstream-protocol-sync.yml"), "sync")
	if !strings.Contains(job, `args=(prepare-source -repo-root "${GITHUB_WORKSPACE}"`) || strings.Contains(job, `args=(diagnose -repo-root "${GITHUB_WORKSPACE}"`) {
		t.Fatal("native goal preparation must not depend on accepted generator planning")
	}
	for _, want := range []string{".Target as $actual", "$actual.PeeledCommitSHA == $expected.PeeledCommitSHA", `-candidate-dir "$(jq -r .CandidateDir "${source}")"`} {
		if !strings.Contains(job, want) {
			t.Fatalf("exact source handoff lost %q", want)
		}
	}
}
