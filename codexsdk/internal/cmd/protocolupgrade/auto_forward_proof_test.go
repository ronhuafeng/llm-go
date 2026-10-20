package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestForwardSemanticRepairRequiresCompleteAssertionEvidence(t *testing.T) {
	exit := exec.Command("sh", "-c", "exit 1").Run()
	for _, tt := range []struct {
		name, body      string
		err             error
		cancelled, want bool
	}{
		{"assertion", "{\"kind\":\"output\",\"output\":\"--- FAIL: TestAcceptedContract (0.01s)\"}\n{\"kind\":\"footer\",\"complete\":true,\"passed\":false}\n", exit, false, true},
		{"accepted runtime assertion", "{\"kind\":\"output\",\"output\":\"--- FAIL: TestAcceptedContract\"}\n{\"kind\":\"footer\",\"complete\":true,\"passed\":false}\n", errors.New("baseline runtime assertions: process=exit status 1"), false, true},
		{"sandbox", "{\"kind\":\"output\",\"output\":\"bwrap: namespace unavailable\"}\n{\"kind\":\"footer\",\"complete\":true,\"passed\":false}\n", exit, false, false},
		{"incomplete", "{\"kind\":\"output\",\"output\":\"--- FAIL: TestAcceptedContract\"}\n", exit, false, false},
		{"nonexit", "{\"kind\":\"output\",\"output\":\"--- FAIL: TestAcceptedContract\"}\n{\"kind\":\"footer\",\"complete\":true,\"passed\":false}\n", errors.New("transport unavailable"), false, false},
		{"cancelled", "{\"kind\":\"output\",\"output\":\"--- FAIL: TestAcceptedContract\"}\n{\"kind\":\"footer\",\"complete\":true,\"passed\":false}\n", exit, true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "proof.jsonl")
			if e := os.WriteFile(path, []byte(tt.body), 0600); e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancelled {
				cancel()
			}
			if got := semanticForwardFailure(ctx, path, tt.err); got != tt.want {
				t.Fatalf("eligible=%v want %v", got, tt.want)
			}
		})
	}
}
