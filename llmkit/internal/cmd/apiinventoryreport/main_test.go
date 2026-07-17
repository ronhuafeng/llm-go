package main

import "testing"

func TestBuildReportClassifiesCanonicalInventoryChanges(t *testing.T) {
	baseline := []byte("func example.com/pkg.A()\nfunc example.com/pkg.B()\n")
	for _, test := range []struct {
		name   string
		target []byte
		impact inventoryImpact
	}{
		{name: "metadata only", target: append([]byte(nil), baseline...), impact: impactMetadataOnly},
		{name: "additive", target: append(append([]byte(nil), baseline...), []byte("func example.com/pkg.C()\n")...), impact: impactAdditive},
		{name: "breaking", target: []byte("func example.com/pkg.A()\n"), impact: impactBreaking},
	} {
		t.Run(test.name, func(t *testing.T) {
			report := buildReport(baseline, test.target)
			if report.FormatVersion != reportFormatVersion {
				t.Fatalf("format version = %d", report.FormatVersion)
			}
			if report.Impact != test.impact {
				t.Fatalf("impact = %s, want %s", report.Impact, test.impact)
			}
			if report.BaselineSHA256 != sha256Hex(baseline) || report.TargetSHA256 != sha256Hex(test.target) {
				t.Fatalf("report digests do not bind both inventories: %#v", report)
			}
		})
	}
}
