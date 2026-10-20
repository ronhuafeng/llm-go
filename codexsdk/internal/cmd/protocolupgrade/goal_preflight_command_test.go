package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestGoalPreflightCommandRejectsIncompleteBinding(t *testing.T) {
	var out, errout bytes.Buffer
	if code := runGoalPreflight(nil, &out, &errout); code != 1 {
		t.Fatalf("exit=%d", code)
	}
	var result struct{ Outcome, Reason string }
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "blocked" || result.Reason == "" || !strings.Contains(errout.String(), result.Reason) {
		t.Fatalf("result=%+v stderr=%s", result, errout.String())
	}
}
func TestGoalPreflightCommandRejectsArguments(t *testing.T) {
	for _, args := range [][]string{{"--unknown"}, {"extra"}, {"-app-bot-id", "not-an-id"}} {
		var out, errout bytes.Buffer
		if code := runGoalPreflight(args, &out, &errout); code != 2 || out.Len() != 0 {
			t.Fatalf("args=%v exit=%d out=%s", args, code, out.String())
		}
	}
}
