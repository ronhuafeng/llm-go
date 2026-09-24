package protocolupgrade

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPlanReadyDoesNotMutateAcceptedBaseline(t *testing.T) {
	fix := writeApplyFixture(t)
	req := ApplyRequest{
		Baseline:          fix.baseline,
		Candidate:         fix.candidate,
		StableCandidate:   fix.stable,
		CommonRS:          fix.commonRS,
		CommonRSSourceSHA: fix.sha,
		Reports:           fix.reports,
		TargetRef:         "rust-v1.2.3",
		TargetKind:        "stable_rust_tag",
		TargetSHA:         fix.sha,
		SkipCodegen:       true,
		skipSurface:       true,
		Now:               func() time.Time { return time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC) },
	}
	before, err := snapshotHashes(fix.baseline)
	if err != nil {
		t.Fatal(err)
	}
	planned, err := Plan(req)
	if err != nil {
		t.Fatal(err)
	}
	if planned.Status != PlanReady {
		t.Fatalf("plan = %+v", planned)
	}
	after, err := snapshotHashes(fix.baseline)
	if err != nil {
		t.Fatal(err)
	}
	if err := sameSnapshot(before, after, "accepted baseline"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(fix.baseline, "AddedType.json")); !os.IsNotExist(err) {
		t.Fatalf("plan wrote candidate schema into accepted baseline: %v", err)
	}
}

func TestPlanReturnsStructuredSemanticIncompatibility(t *testing.T) {
	fix := writeApplyFixture(t)
	if err := os.WriteFile(fix.commonRS, []byte("client_request_definitions! { broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	planned, err := Plan(ApplyRequest{
		Baseline:          fix.baseline,
		Candidate:         fix.candidate,
		StableCandidate:   fix.stable,
		CommonRS:          fix.commonRS,
		CommonRSSourceSHA: fix.sha,
		Reports:           fix.reports,
		TargetRef:         "rust-v1.2.3",
		TargetKind:        "stable_rust_tag",
		TargetSHA:         fix.sha,
		SkipCodegen:       true,
		skipSurface:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if planned.Status != PlanSemanticUnresolved || planned.Issue == nil || planned.Issue.Stage != "manifest" {
		t.Fatalf("plan = %+v", planned)
	}
}

func TestIncompatibilityPathExtractsSchemaPointer(t *testing.T) {
	message := "field v2/ThreadAttachmentAddParams.json#/properties/payload has unreviewed true schema"
	if got := incompatibilityPath(message); got != "v2/ThreadAttachmentAddParams.json#/properties/payload" {
		t.Fatalf("path = %q", got)
	}
	if got := incompatibilityPath("plain failure"); got != "" {
		t.Fatalf("unexpected path %q", got)
	}
}

func TestPlanDoesNotWriteConfiguredReportsDirectory(t *testing.T) {
	fix := writeApplyFixture(t)
	reports := filepath.Join(fix.module, "external-reports")
	_, err := Plan(ApplyRequest{
		Baseline:          fix.baseline,
		Candidate:         fix.candidate,
		StableCandidate:   fix.stable,
		CommonRS:          fix.commonRS,
		CommonRSSourceSHA: fix.sha,
		Reports:           reports,
		TargetRef:         "rust-v1.2.3",
		TargetKind:        "stable_rust_tag",
		TargetSHA:         fix.sha,
		SkipCodegen:       true,
		skipSurface:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(reports); !os.IsNotExist(err) {
		t.Fatalf("plan wrote caller reports directory: %v", err)
	}
	if strings.TrimSpace(reports) == "" {
		t.Fatal("fixture reports path unexpectedly empty")
	}
}
