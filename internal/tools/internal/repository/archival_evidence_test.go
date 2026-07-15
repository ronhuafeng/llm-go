package repository

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommittedArchivalEvidenceFailsClosed(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var complete archivalEvidence
	if err := readStrictJSON(filepath.Join(root, filepath.FromSlash(archivalEvidenceFilename)), &complete); err != nil {
		t.Fatal(err)
	}
	provenance, err := loadProvenance(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := complete.validate(root, provenance); err != nil {
		t.Fatalf("committed evidence: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*archivalEvidence)
		want   string
	}{
		{
			name: "uppercase Git object identity",
			mutate: func(evidence *archivalEvidence) {
				evidence.Subject.Commit = strings.ToUpper(evidence.Subject.Commit)
			},
			want: "subject is incomplete",
		},
		{
			name: "missing prerequisite category",
			mutate: func(evidence *archivalEvidence) {
				evidence.Prerequisite.Categories = evidence.Prerequisite.Categories[:5]
			},
			want: "mandatory category",
		},
		{
			name: "legacy repository not archived",
			mutate: func(evidence *archivalEvidence) {
				evidence.LegacyRepositories[0].Archived = false
			},
			want: "archival state is incomplete",
		},
		{
			name: "archive predates acceptance",
			mutate: func(evidence *archivalEvidence) {
				evidence.LegacyRepositories[0].ArchiveObservedAt = "2026-07-15T04:19:59Z"
			},
			want: "archival state is incomplete",
		},
		{
			name: "legacy actions still enabled",
			mutate: func(evidence *archivalEvidence) {
				evidence.LegacyRepositories[0].ActionsEnabled = true
			},
			want: "archival state is incomplete",
		},
		{
			name: "security handoff missing",
			mutate: func(evidence *archivalEvidence) {
				evidence.SecurityHandoff.SuccessorEnabledBeforeLegacyDisable = false
			},
			want: "handoff is incomplete",
		},
		{
			name: "post archive PVR claimed observable",
			mutate: func(evidence *archivalEvidence) {
				evidence.LegacyRepositories[0].PostArchivePVRStatus = "disabled"
			},
			want: "archival state is incomplete",
		},
		{
			name: "proxy checksum omitted",
			mutate: func(evidence *archivalEvidence) {
				evidence.ProxyModules[0].GoModSum = ""
			},
			want: "public-Proxy module",
		},
		{
			name: "typed exact evidence lost",
			mutate: func(evidence *archivalEvidence) {
				evidence.AdapterTuple.TypedConsumer.ExactResultPreserved = false
			},
			want: "typed consumer evidence is incomplete",
		},
		{
			name: "report claims incomplete",
			mutate: func(evidence *archivalEvidence) {
				evidence.Complete = false
			},
			want: "cannot be incomplete",
		},
		{
			name: "digest does not bind evidence",
			mutate: func(evidence *archivalEvidence) {
				evidence.ReportDigest = "sha256:" + strings.Repeat("0", 64)
			},
			want: "digest mismatch",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneArchivalEvidence(t, complete)
			test.mutate(&candidate)
			if err := candidate.validate(root, provenance); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validate() = %v, want %q", err, test.want)
			}
		})
	}
}

func TestArchivalEvidenceUsesCanonicalIdentityRules(t *testing.T) {
	if objectIDPattern.MatchString(strings.Repeat("A", 40)) {
		t.Fatal("canonical Git object rule accepted uppercase hex")
	}
	for _, tag := range []string{"llmkit/V0.6.0", "llmkit/v0.6", "llmkit/main"} {
		if _, err := migrationVersion(tag); err == nil {
			t.Fatalf("canonical migration version rule accepted %q", tag)
		}
	}
}

func cloneArchivalEvidence(t *testing.T, source archivalEvidence) archivalEvidence {
	t.Helper()
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var cloned archivalEvidence
	if err := json.Unmarshal(data, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}
