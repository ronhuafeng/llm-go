package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestForwardContextRejectsRenewedOrExpiredBudget(t *testing.T) {
	for _, deadline := range []string{"", "not-time", time.Now().Add(-time.Second).Format(time.RFC3339), time.Now().Add(7 * time.Hour).Format(time.RFC3339)} {
		if _, cancel, e := forwardContext(forwardFlags{deadline: deadline}); e == nil {
			cancel()
			t.Fatalf("accepted invalid original deadline %q", deadline)
		}
	}
	want := time.Now().Add(time.Minute).UTC().Truncate(time.Second)
	ctx, cancel, e := forwardContext(forwardFlags{deadline: want.Format(time.RFC3339)})
	if e != nil {
		t.Fatal(e)
	}
	defer cancel()
	got, ok := ctx.Deadline()
	if !ok || !got.Equal(want) {
		t.Fatal("original absolute deadline reset")
	}
}
func TestForwardReadSubprocessHasNoAppOrProviderCredential(t *testing.T) {
	for _, key := range []string{"AUTO_FORWARD_APP_PRIVATE_KEY", "AZURE_OPENAI_API_KEY", "GITHUB_TOKEN", "GH_TOKEN"} {
		t.Setenv(key, "private-fixture-value")
	}
	t.Setenv("GH_READ_TOKEN", "read-only-fixture")
	for _, entry := range forwardReadEnv() {
		if strings.Contains(entry, "private-fixture-value") {
			t.Fatalf("private credential reached read process: %s", strings.Split(entry, "=")[0])
		}
	}
}
func TestForwardManualVerifierNeedsNoProviderOrTargetInputs(t *testing.T) {
	t.Setenv("AUTO_FORWARD_SOURCE", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("AZURE_OPENAI_API_KEY", "")
	if _, e := newForwardVerifier(t.Context(), forwardFlags{}, nil, os.Stderr); e != nil {
		t.Fatal(e)
	}
}
