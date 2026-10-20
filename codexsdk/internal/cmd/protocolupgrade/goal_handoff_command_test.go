package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestIntegrationGuardRejectsExpiredOrMissingDeadlineBeforeEffects(t *testing.T) {
	for _, deadline := range []string{"", "not-a-date", "2000-01-01T00:00:00Z"} {
		var out, err bytes.Buffer
		if code := run([]string{"goal-integration-guard", "-deadline", deadline}, &out, &err); code == 0 || !strings.Contains(err.String(), "deadline") {
			t.Fatal("deadline not enforced", code, err.String())
		}
	}
}
func TestReadyCommandRequiresTrustedHandoff(t *testing.T) {
	var out, err bytes.Buffer
	if code := run([]string{"goal-ready", "-controller-root", t.TempDir(), "-handoff-file", t.TempDir() + "/missing"}, &out, &err); code == 0 {
		t.Fatal("missing handoff accepted")
	}
}
