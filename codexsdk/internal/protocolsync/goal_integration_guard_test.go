package protocolsync

import (
	"context"
	"strings"
	"testing"
)

func TestIntegrationHandoffGuardBindsSourceAndAcceptedAncestors(t *testing.T) {
	f := newGoalPublicationFixture(t)
	base := f.request.BaseSHA
	target := Target{RefName: "rust-v0.142.0", RefKind: KindStableTag, PeeledCommitSHA: strings.Repeat("c", 40)}
	goalTestWrite(t, f.request.RepoRoot, "codexsdk/internal/protocolschema/appserver/v2/baseline_metadata.json", `{"source_commit":"`+target.PeeledCommitSHA+`","source_ref_name":"rust-v0.142.0","source_ref_kind":"stable_rust_tag"}`)
	goalTestGit(t, f.request.RepoRoot, "add", "-A")
	goalTestGit(t, f.request.RepoRoot, "commit", "-m", "fixed target")
	head := goalTestGit(t, f.request.RepoRoot, "rev-parse", "HEAD")
	if err := validateGoalIntegrationHandoff(context.Background(), f.request.RepoRoot, base, base, base, head, target, goalLookup{target}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"wrong_head", "wrong_source", "moved_tag", "unaccepted_base", "rollback_verified"} {
		t.Run(name, func(t *testing.T) {
			h, b, m, v := head, base, base, base
			u := target
			lookup := goalLookup{target}
			switch name {
			case "wrong_head":
				h = base
			case "wrong_source":
				u.RefName = "rust-v0.143.0"
			case "moved_tag":
				lookup.target.PeeledCommitSHA = strings.Repeat("d", 40)
			case "unaccepted_base":
				b = head
			case "rollback_verified":
				v = head
			}
			if err := validateGoalIntegrationHandoff(context.Background(), f.request.RepoRoot, b, v, m, h, u, lookup); err == nil {
				t.Fatal("invalid automatic integration allowed")
			}
		})
	}
}
