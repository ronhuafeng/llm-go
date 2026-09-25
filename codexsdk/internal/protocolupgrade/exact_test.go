package protocolupgrade

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyExactRebuildsAndRejectsStaleFacts(t *testing.T) {
	root := copyModuleForCheck(t)
	baseline := filepath.Join(root, filepath.FromSlash(defaultBaselineRel))
	candidate := t.TempDir()
	if err := copyTree(baseline, candidate); err != nil {
		t.Fatal(err)
	}
	commonRS := filepath.Join(root, "common.rs")
	writeBaselineMappingFixture(t, baseline, commonRS)
	sha := strings.Repeat("b", 40)
	req := ApplyRequest{
		Baseline: baseline, Candidate: candidate, StableCandidate: candidate,
		CommonRS: commonRS, CommonRSSourceSHA: sha, TargetRef: "rust-v1.2.3",
		TargetKind: "stable_rust_tag", TargetSHA: sha, ModuleRoot: root,
	}
	if _, err := Apply(req); err != nil {
		t.Fatal(err)
	}
	before, err := snapshotHashes(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyExact(req); err != nil {
		t.Fatalf("fresh exact reconstruction failed: %v", err)
	}
	after, err := snapshotHashes(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if err := sameSnapshot(before, after, "accepted baseline during exact validation"); err != nil {
		t.Fatal(err)
	}
	rebuiltRoot := t.TempDir()
	rebuiltBaseline := filepath.Join(rebuiltRoot, filepath.FromSlash(defaultBaselineRel))
	if err := copyTree(baseline, rebuiltBaseline); err != nil {
		t.Fatal(err)
	}
	for _, rel := range generatedProtocolArtifacts {
		from := filepath.Join(root, filepath.FromSlash(rel))
		to := filepath.Join(rebuiltRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := copyFile(from, to); err != nil {
			t.Fatal(err)
		}
	}

	coveragePath := filepath.Join(baseline, "coverage_matrix.json")
	manifestPath := filepath.Join(baseline, "manifest.json")
	coverageBytes, err := os.ReadFile(coveragePath)
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, artifact string
		mutate         func(t *testing.T)
	}{
		{name: "requiredness", artifact: "coverage_matrix.json", mutate: func(t *testing.T) {
			var coverage coverageFile
			if err := loadJSON(coveragePath, &coverage); err != nil {
				t.Fatal(err)
			}
			coverage.Fields[0]["required"] = coverage.Fields[0]["required"] != true
			if err := writeJSON(coveragePath, coverage); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "method stability", artifact: "manifest.json", mutate: func(t *testing.T) {
			var manifest manifestFile
			if err := loadJSON(manifestPath, &manifest); err != nil {
				t.Fatal(err)
			}
			if manifest.Entries[0].Stability == "stable" {
				manifest.Entries[0].Stability = "experimental"
			} else {
				manifest.Entries[0].Stability = "stable"
			}
			if err := writeJSON(manifestPath, manifest); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "response mapping", artifact: "manifest.json", mutate: func(t *testing.T) {
			var manifest manifestFile
			if err := loadJSON(manifestPath, &manifest); err != nil {
				t.Fatal(err)
			}
			for index := range manifest.Entries {
				if manifest.Entries[index].Kind == "request" {
					manifest.Entries[index].SourceRef["response_mapping"] = "stale accepted mapping"
					break
				}
			}
			if err := writeJSON(manifestPath, manifest); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(coveragePath, coverageBytes, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil {
				t.Fatal(err)
			}
			test.mutate(t)
			// VerifyExact reaches this comparison after isolated reconstruction.
			// Metadata is checked independently of the generated Go files.
			err := compareExactBaseline(baseline, rebuiltBaseline, root, rebuiltRoot)
			if err == nil || !strings.Contains(err.Error(), test.artifact) {
				t.Fatalf("got %v, want %s mismatch", err, test.artifact)
			}
		})
	}
	if err := os.WriteFile(coveragePath, coverageBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*ApplyRequest)
		want   string
	}{
		{name: "wrong source SHA", change: func(req *ApplyRequest) { req.CommonRSSourceSHA = strings.Repeat("c", 40) }, want: "does not match target"},
		{name: "missing stable schema", change: func(req *ApplyRequest) { req.StableCandidate = filepath.Join(root, "missing-stable") }, want: "load stable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			broken := req
			test.change(&broken)
			_, err := VerifyExact(broken)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want %q", err, test.want)
			}
		})
	}
}
