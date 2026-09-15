package protocolsync

import "testing"

func TestSyncBranchName(t *testing.T) {
	got := syncBranchName("codex/sync-upstream/run-1-1", "refs/tags/rust-v0.154.0", oldSHA)
	if got != "codex/sync-upstream/run-1-1-rust-v0.154.0-111111111111" {
		t.Fatalf("got %s", got)
	}
}

func TestParseSyncMetadata(t *testing.T) {
	body := `<!-- codexsdk-upstream-sync
upstream_ref: rust-v0.154.0
upstream_ref_kind: stable_rust_tag
upstream_commit: ` + oldSHA + `
sync_commit: ` + newSHA + `
base_branch: main
-->
`
	meta := parseSyncMetadata(body)
	if meta["upstream_ref"] != "rust-v0.154.0" || meta["upstream_commit"] != oldSHA || meta["sync_commit"] != newSHA || meta["base_branch"] != "main" {
		t.Fatalf("%+v", meta)
	}
}

func TestNormalizeBranchRef(t *testing.T) {
	if got := normalizeBranchRef("refs/heads/main", "origin"); got != "main" {
		t.Fatalf("got %s", got)
	}
	if got := normalizeBranchRef("origin/main", "origin"); got != "main" {
		t.Fatalf("got %s", got)
	}
}
