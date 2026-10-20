package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolsync"
	"io"
	"os"
	"strings"
)

func runGoalReady(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("goal-ready", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("controller-root", "", "trusted accepted checkout")
	path := fs.String("handoff-file", "", "success-only trusted controller handoff")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *root == "" || *path == "" {
		return 2
	}
	data, err := os.ReadFile(*path)
	if err != nil || len(data) > 16384 {
		fmt.Fprintln(stderr, "completed handoff unavailable")
		return 1
	}
	var handoff protocolsync.GoalHandoff
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&handoff) != nil || decoder.Decode(new(any)) != io.EOF {
		fmt.Fprintln(stderr, "completed handoff invalid")
		return 1
	}
	ctx, cancel := context.WithDeadline(context.Background(), handoff.Deadline)
	defer cancel()
	if err = protocolsync.ReadyGoal(ctx, *root, handoff, protocolsync.NewGoalAPI(ctx, *root), nil); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if json.NewEncoder(stdout).Encode(handoff) != nil {
		return 1
	}
	return 0
}
