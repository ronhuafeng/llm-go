package protocolsync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCandidateFixture(t *testing.T, dir, sourceSHA string) {
	t.Helper()
	for name, value := range map[string]string{
		"schema/ClientRequest.json":        `{"title":"ClientRequest"}`,
		"stable-schema/ClientRequest.json": `{"title":"ClientRequest"}`,
		"reports/drift_summary.json":       fmt.Sprintf(`{"status":"review-required","target":{"source_ref_name":"rust-v0.141.0","source_ref_kind":"stable_rust_tag","source_commit":%q}}`, sourceSHA),
		"common.rs":                        "client_request_definitions! {}\n",
		"common.rs.source_sha":             sourceSHA + "\n",
	} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCandidateDigestCoversIgnoredCompleteSetAndSnapshot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "candidate")
	writeCandidateFixture(t, dir, newSHA)
	want, err := candidateDigest(dir)
	if err != nil {
		t.Fatal(err)
	}
	copy, cleanup, err := copyVerifiedCandidate(dir, want, "rust-v0.141.0", KindStableTag, newSHA)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if copy == dir {
		t.Fatal("resume must use an isolated candidate copy")
	}
	if got, err := candidateDigest(copy); err != nil || got != want {
		t.Fatalf("copied digest %s, err %v; want %s", got, err, want)
	}
	if err := os.WriteFile(filepath.Join(dir, "schema/ClientRequest.json"), []byte(`{"changed":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := candidateDigest(copy); err != nil || got != want {
		t.Fatalf("isolated copy changed with cache: digest %s, err %v", got, err)
	}
	if _, _, err := copyVerifiedCandidate(dir, want, "rust-v0.141.0", KindStableTag, newSHA); err == nil || !strings.Contains(err.Error(), "changed after initial Plan") {
		t.Fatalf("mutated ignored cache accepted: %v", err)
	}
}

func TestCandidateDigestRejectsMissingChangedAndRedirectedInputs(t *testing.T) {
	for _, test := range []struct {
		name, path string
		mutate     func(t *testing.T, path string)
		want       string
	}{
		{name: "missing stable schema", path: "stable-schema/ClientRequest.json", mutate: func(t *testing.T, path string) {
			t.Helper()
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}, want: "changed after initial Plan"},
		{name: "changed mapping", path: "common.rs", mutate: func(t *testing.T, path string) {
			t.Helper()
			if err := os.WriteFile(path, []byte("changed"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, want: "changed after initial Plan"},
		{name: "extra report", path: "reports/extra.json", mutate: func(t *testing.T, path string) {
			t.Helper()
			if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, want: "changed after initial Plan"},
		{name: "redirected schema", path: "schema/ClientRequest.json", mutate: func(t *testing.T, path string) {
			t.Helper()
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(filepath.Dir(path), "../stable-schema/ClientRequest.json"), path); err != nil {
				t.Fatal(err)
			}
		}, want: "not a regular file"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "candidate")
			writeCandidateFixture(t, dir, newSHA)
			want, err := candidateDigest(dir)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, filepath.FromSlash(test.path))
			test.mutate(t, path)
			if _, _, err := copyVerifiedCandidate(dir, want, "rust-v0.141.0", KindStableTag, newSHA); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want %q", err, test.want)
			}
		})
	}
}
