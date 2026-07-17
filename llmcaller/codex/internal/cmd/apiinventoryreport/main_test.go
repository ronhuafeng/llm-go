package main

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestBuildReportClassifiesCanonicalInventoryChanges(t *testing.T) {
	baseline := []byte("func example.com/pkg.A()\nfunc example.com/pkg.B()\n")
	for _, test := range []struct {
		name   string
		target []byte
		impact inventoryImpact
	}{
		{name: "metadata only", target: append([]byte(nil), baseline...), impact: inventoryImpact("metadata-only")},
		{name: "reordering is metadata only", target: []byte("func example.com/pkg.B()\nfunc example.com/pkg.A()\n"), impact: inventoryImpact("metadata-only")},
		{name: "additive", target: append(append([]byte(nil), baseline...), []byte("func example.com/pkg.C()\n")...), impact: inventoryImpact("additive")},
		{name: "breaking", target: []byte("func example.com/pkg.A()\n"), impact: inventoryImpact("breaking")},
	} {
		t.Run(test.name, func(t *testing.T) {
			report := buildReport(baseline, test.target)
			if report.FormatVersion != 1 {
				t.Fatalf("format version = %d", report.FormatVersion)
			}
			if report.Impact != test.impact {
				t.Fatalf("impact = %s, want %s", report.Impact, test.impact)
			}
			if report.BaselineSHA256 != fmt.Sprintf("%x", sha256.Sum256(baseline)) || report.TargetSHA256 != fmt.Sprintf("%x", sha256.Sum256(test.target)) {
				t.Fatalf("report digests do not bind both inventories: %#v", report)
			}
		})
	}
}
