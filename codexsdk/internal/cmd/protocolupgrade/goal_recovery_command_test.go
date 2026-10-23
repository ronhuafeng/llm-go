package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestGoalRecoverCommandRejectsIncompleteBinding(t *testing.T) {
	var out, errout bytes.Buffer
	if code := runGoalRecover(nil, &out, &errout); code != 1 {
		t.Fatalf("exit=%d", code)
	}
	var got struct{ Outcome, Reason string }
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Outcome != "blocked" || got.Reason == "" || !strings.Contains(errout.String(), got.Reason) {
		t.Fatalf("%+v stderr=%s", got, errout.String())
	}
}
func TestGoalRecoverCommandRequiresExplicitPairedExecutionAuthorization(t *testing.T) {
	for _, args := range [][]string{{"--unknown"}, {"extra"}, {"-prior-run-id", "bad"}, {"-new-execution-deadline", "bad"}, {"-new-execution-deadline", "2026-10-24T01:00:00Z"}, {"-authorize-new-bounded-execution"}} {
		var out, errout bytes.Buffer
		if code := runGoalRecover(args, &out, &errout); code != 2 || out.Len() != 0 {
			t.Fatalf("args=%v code=%d out=%s", args, code, out.String())
		}
	}
}
