package protocolsync

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGoalRemoteLookupHonorsCancellation(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\n/bin/sleep 2 &\nwait\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := goalLookupContext(ctx, nil).LSRemote("https://github.com/openai/codex.git", "refs/tags/example")
	if err == nil || ctx.Err() == nil || time.Since(start) > time.Second {
		t.Fatalf("lookup did not honor cancellation: %v", err)
	}
}
