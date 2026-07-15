package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationAcceptanceReportFailsClosed(t *testing.T) {
	complete := completeAcceptanceFixture()
	if err := complete.Validate(); err != nil {
		t.Fatalf("complete report: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*MigrationAcceptanceReport)
		want   string
	}{
		{
			name: "missing category",
			mutate: func(report *MigrationAcceptanceReport) {
				report.Categories = report.Categories[:len(report.Categories)-1]
			},
			want: "mandatory categories",
		},
		{
			name: "failed check claims completion",
			mutate: func(report *MigrationAcceptanceReport) {
				report.Categories[0].Checks[0].Status = "failed"
				report.Categories[0].Checks[0].Error = "broken"
			},
			want: "cannot be complete",
		},
		{
			name: "check omits inspected artifacts",
			mutate: func(report *MigrationAcceptanceReport) {
				report.Categories[0].Checks[0].Artifacts = nil
			},
			want: "no inspected artifacts",
		},
		{
			name: "incomplete report claims completion",
			mutate: func(report *MigrationAcceptanceReport) {
				report.Complete = false
			},
			want: "complete field disagrees",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := completeAcceptanceFixture()
			test.mutate(&report)
			if err := report.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() = %v, want %q", err, test.want)
			}
		})
	}
}

func TestMigrationAcceptanceReportAllowsExplicitIncompleteEvidence(t *testing.T) {
	report := completeAcceptanceFixture()
	report.Categories[2].Status = "incomplete"
	report.Categories[2].Checks[0].Status = "failed"
	report.Categories[2].Checks[0].Error = "module registry missing"
	report.Complete = false
	if err := report.Validate(); err != nil {
		t.Fatalf("explicit incomplete report: %v", err)
	}
}

func TestValidatePublishedEvidenceRejectsIncompleteAsset(t *testing.T) {
	evidence := PublishedEvidence{
		FormatVersion: publishedEvidenceFormatVersion,
		Subject: PublishedSubject{
			Kind: "published_module", Module: "example.com/module", Version: "v1.0.0",
			Tag: "module/v1.0.0", TagCommit: strings.Repeat("a", 40),
			PlanDigest: "sha256:" + strings.Repeat("b", 64), AuthorizationDigest: "sha256:" + strings.Repeat("c", 64),
		},
		Environment: PublishedEnvironment{
			GOPROXY: "https://proxy.golang.org", GOSUMDB: "sum.golang.org", GOWORK: "off", GOVCS: "*:off",
			FreshCaches: true, SeparateCaches: true,
		},
		Resolved: PublishedResolution{
			Path: "example.com/module", Version: "v1.0.0", Sum: "h1:x", GoModSum: "h1:y",
			Origin: &ModuleOrigin{VCS: "git", URL: "https://example.com/module", Hash: strings.Repeat("a", 40), Ref: "refs/tags/v1.0.0"},
		},
		Checks: []PublishedCheck{{Name: "proxy", Status: "failed", Error: "unavailable"}},
	}
	if err := finalizePublishedEvidence(&evidence); err != nil {
		t.Fatal(err)
	}
	if err := validatePublishedEvidenceAsset(evidence); err == nil || !strings.Contains(err.Error(), "is not passed") {
		t.Fatalf("validatePublishedEvidenceAsset() = %v", err)
	}
}

func TestValidatePublishedEvidenceKeepsFirstReleaseSchemaVerifiable(t *testing.T) {
	evidence := PublishedEvidence{
		FormatVersion: 1,
		Subject: PublishedSubject{
			Kind: "published_module", Module: "example.com/module", Version: "v1.0.0",
			Tag: "module/v1.0.0", TagCommit: strings.Repeat("a", 40),
			PlanDigest: "sha256:" + strings.Repeat("b", 64), AuthorizationDigest: "sha256:" + strings.Repeat("c", 64),
		},
		Environment: PublishedEnvironment{
			GOPROXY: "https://proxy.golang.org", GOSUMDB: "sum.golang.org", GOWORK: "off", GOVCS: "*:off",
			FreshCaches: true, SeparateCaches: true,
		},
		Resolved: PublishedResolution{
			Path: "example.com/module", Version: "v1.0.0", Sum: "h1:x", GoModSum: "h1:y",
			Origin: &ModuleOrigin{VCS: "git", URL: "https://example.com/module", Hash: strings.Repeat("a", 40), Ref: "refs/tags/v1.0.0"},
		},
		Checks: []PublishedCheck{{Name: "proxy", Status: "passed"}},
	}
	if err := finalizePublishedEvidence(&evidence); err != nil {
		t.Fatal(err)
	}
	if err := validatePublishedEvidenceAsset(evidence); err != nil {
		t.Fatalf("validate immutable v1 evidence: %v", err)
	}
}

func TestAcceptanceSourceEvidenceRequiresExactStageContract(t *testing.T) {
	evidence := Evidence{
		FormatVersion: evidenceFormatVersion,
		Subject: EvidenceSubject{
			Kind: "module_source", Commit: strings.Repeat("a", 40), Tree: strings.Repeat("b", 40), Module: "codexsdk", Stage: "current",
		},
		Checks: []EvidenceCheck{{Name: "anything", Status: "passed"}},
	}
	err := validateAcceptanceSourceEvidence(evidence, evidence.Subject.Commit, evidence.Subject.Tree, "module_source", "codexsdk", "current")
	if err == nil || !strings.Contains(err.Error(), "exact") {
		t.Fatalf("validateAcceptanceSourceEvidence() = %v, want exact-contract rejection", err)
	}

	evidence.Checks = nil
	for _, name := range acceptanceSourceCheckNames("module_source", "codexsdk", "current") {
		evidence.Checks = append(evidence.Checks, EvidenceCheck{Name: name, Status: "passed"})
	}
	if err := validateAcceptanceSourceEvidence(evidence, evidence.Subject.Commit, evidence.Subject.Tree, "module_source", "codexsdk", "current"); err != nil {
		t.Fatalf("validate exact codexsdk current evidence: %v", err)
	}
}

func TestMigrationAuditRecordsUnreadableArtifactAsFailure(t *testing.T) {
	builder := newMigrationAuditBuilder(strings.Repeat("a", 40), strings.Repeat("b", 40))
	builder.fileArtifact(t.TempDir(), "missing", filepath.Join(t.TempDir(), "absent"), "cutover_readiness")
	failures := builder.artifactFailures["cutover_readiness"]
	if len(builder.artifactFailures) != 1 || len(failures) != 1 || !strings.Contains(failures[0].err, "no such file") {
		t.Fatalf("artifact failures = %v, want missing-file failure", builder.artifactFailures)
	}
	builder.failUnreadableArtifacts()
	for _, category := range builder.report.Categories {
		want := "complete"
		if category.ID == "cutover_readiness" {
			want = "incomplete"
		}
		if category.Status != want {
			t.Fatalf("category %s = %s, want exact owner status %s", category.ID, category.Status, want)
		}
	}
}

func TestLegacyReferenceAllowlistIsExact(t *testing.T) {
	prefix := "github.com/ronhuafeng/"
	legacy := prefix + "llmkit-go"
	if !allowedLegacyFixtureReference("internal/tools/internal/repository/release_test.go", `legacy := []byte("pkg `+legacy+`/llmstep")`, legacy) {
		t.Fatal("known API-mapping negative fixture must be allowed")
	}
	if allowedLegacyFixtureReference("internal/tools/internal/repository/release_test.go", `import _ "`+legacy+`/llmstep"`, legacy) {
		t.Fatal("active import in an allowlisted fixture file must not be allowed")
	}
	if allowedLegacyFixtureReference("internal/tools/internal/repository/other_test.go", `legacy := []byte("`+legacy+`")`, legacy) {
		t.Fatal("unknown test fixture must not be blanket-allowed")
	}
	if allowedLegacyFixtureReference("internal/tools/internal/repository/release_test.go", `legacy := []byte("`+legacy+`")`, prefix+"unknown-go") {
		t.Fatal("unknown legacy path must not be allowed")
	}
}

func TestLegacyDocumentationAllowancesAreNarrow(t *testing.T) {
	legacy := "github.com/ronhuafeng/" + "llmkit-go"
	allowances := legacyDocumentationAllowances{
		migration:      map[string]bool{"llmkit/docs/migration/v0.6.0.md": true},
		changelogOwner: map[string]string{"llmkit/CHANGELOG.md": legacy},
		readmeVersion:  map[string]string{"llmkit/README.md": legacy + "@v0.5.0"},
	}
	if got := documentationLegacyReferenceViolations("llmkit/README.md", []byte("go get "+legacy+"@v0.5.0"), []string{legacy}, allowances); len(got) != 1 {
		t.Fatalf("active README install guidance violations = %v, want one", got)
	}
	if got := documentationLegacyReferenceViolations("llmkit/README.md", []byte("Consumers of `"+legacy+"@v0.5.0` should follow the migration guide"), []string{legacy}, allowances); len(got) != 0 {
		t.Fatalf("versioned README migration reference violations = %v", got)
	}
	if got := documentationLegacyReferenceViolations("llmkit/docs/migration/v0.6.0.md", []byte(legacy), []string{legacy}, allowances); len(got) != 0 {
		t.Fatalf("migration mapping violations = %v", got)
	}
}

func TestDerivedMigrationDocumentVersionUsesLastTagSegment(t *testing.T) {
	tag := "llmcaller/codex/v0.5.0"
	version, err := migrationVersion(tag)
	if err != nil || version != "v0.5.0" {
		t.Fatalf("derived version from %q = %q, %v", tag, version, err)
	}
	if _, err := os.Stat(filepath.Join("..", "..", "..", "..", "llmcaller", "codex", "docs", "migration", version+".md")); err != nil {
		t.Fatalf("derived migration document: %v", err)
	}
}

func TestCurrentActiveDependenciesAreCutOver(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	registered, err := loadPopulatedRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	builder := newMigrationAuditBuilder(strings.Repeat("a", 40), strings.Repeat("b", 40))
	artifacts, err := auditActiveDependencies(root, registered, builder)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) < 10 {
		t.Fatalf("active scan recorded only %d artifacts, want exact file-level inputs", len(artifacts))
	}
}

func completeAcceptanceFixture() MigrationAcceptanceReport {
	artifact := MigrationAcceptanceArtifact{ID: "artifact", Kind: "test", Locator: "testdata/artifact", SHA256: strings.Repeat("a", 64)}
	categories := make([]MigrationAcceptanceCategory, 0, len(migrationAcceptanceCategoryIDs))
	for _, id := range migrationAcceptanceCategoryIDs {
		categories = append(categories, MigrationAcceptanceCategory{
			ID: id, Status: "complete",
			Checks: []MigrationAcceptanceCheck{{Name: "proof", Status: "passed", Artifacts: []string{artifact.ID}}},
		})
	}
	return MigrationAcceptanceReport{
		FormatVersion: migrationAcceptanceFormatVersion,
		Subject: MigrationAcceptanceSubject{
			Repository: "https://github.com/ronhuafeng/llm-go", Commit: strings.Repeat("a", 40), Tree: strings.Repeat("b", 40),
		},
		Complete: true, Artifacts: []MigrationAcceptanceArtifact{artifact}, Categories: categories,
	}
}
