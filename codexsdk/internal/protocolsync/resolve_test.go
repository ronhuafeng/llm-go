package protocolsync

import (
	"strings"
	"testing"
)

type fakeLookuper struct {
	byPattern map[string]string
}

func (f fakeLookuper) LSRemote(remote string, patterns ...string) (string, error) {
	var b strings.Builder
	for _, pattern := range patterns {
		if text, ok := f.byPattern[pattern]; ok && text != "" {
			b.WriteString(text)
			if !strings.HasSuffix(text, "\n") {
				b.WriteByte('\n')
			}
		}
	}
	return b.String(), nil
}

func TestLatestStableTagUsesSemverOrder(t *testing.T) {
	output := strings.Join([]string{
		oldSHA + "\trefs/tags/rust-v0.99.0",
		newSHA + "\trefs/tags/rust-v0.100.0",
		"3333333333333333333333333333333333333333\trefs/tags/not-rust-v9.0.0",
	}, "\n")
	got, err := latestStableRustTag(output)
	if err != nil {
		t.Fatal(err)
	}
	if got != "rust-v0.100.0" {
		t.Fatalf("got %s", got)
	}
}

func TestLatestStableTagIgnoresPrerelease(t *testing.T) {
	output := strings.Join([]string{
		oldSHA + "\trefs/tags/rust-v0.99.0",
		newSHA + "\trefs/tags/rust-v0.100.0-alpha.1",
		"3333333333333333333333333333333333333333\trefs/tags/rust-v0.100.0-alpha.1^{}",
		"4444444444444444444444444444444444444444\trefs/tags/rust-v0.100.0^{}",
	}, "\n")
	got, err := latestStableRustTag(output)
	if err != nil {
		t.Fatal(err)
	}
	if got != "rust-v0.99.0" {
		t.Fatalf("got %s", got)
	}
}

func TestResolveExplicitSHADoesNotLookup(t *testing.T) {
	target, err := ResolveUpstream(ResolveRequest{
		Remote:      "unused",
		UpstreamRef: oldSHA,
		Lookuper:    fakeLookuper{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if target.RefName != oldSHA || target.RefKind != KindManualCommit || target.PeeledCommitSHA != oldSHA || !target.TargetExplicit {
		t.Fatalf("%+v", target)
	}
}

func TestResolveExplicitTagRecordsPeeledSHA(t *testing.T) {
	lookuper := fakeLookuper{byPattern: map[string]string{
		"refs/tags/rust-v0.100.0":    oldSHA + "\trefs/tags/rust-v0.100.0",
		"refs/tags/rust-v0.100.0^{}": newSHA + "\trefs/tags/rust-v0.100.0^{}",
	}}
	target, err := ResolveUpstream(ResolveRequest{
		Remote:      "fake",
		UpstreamRef: "rust-v0.100.0",
		Lookuper:    lookuper,
	})
	if err != nil {
		t.Fatal(err)
	}
	if target.TagSHA != oldSHA || target.PeeledCommitSHA != newSHA || target.RefKind != KindStableTag || !target.TargetExplicit {
		t.Fatalf("%+v", target)
	}
}

func TestResolveLatestStableIsNotExplicit(t *testing.T) {
	lookuper := fakeLookuper{byPattern: map[string]string{
		"refs/tags/rust-v*":          oldSHA + "\trefs/tags/rust-v0.99.0\n" + newSHA + "\trefs/tags/rust-v0.100.0\n",
		"refs/tags/rust-v0.100.0":    newSHA + "\trefs/tags/rust-v0.100.0",
		"refs/tags/rust-v0.100.0^{}": newSHA + "\trefs/tags/rust-v0.100.0^{}",
	}}
	target, err := ResolveUpstream(ResolveRequest{
		Remote:       "fake",
		LatestStable: true,
		Lookuper:     lookuper,
	})
	if err != nil {
		t.Fatal(err)
	}
	if target.RefName != "rust-v0.100.0" || target.TargetExplicit {
		t.Fatalf("%+v", target)
	}
}
