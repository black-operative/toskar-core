package version

import (
	"strings"
	"testing"
)

func TestCorrespondingSource(t *testing.T) {
	origV, origC, origS := Version, Commit, SourceURL
	t.Cleanup(func() {
		Version, Commit, SourceURL = origV, origC, origS
	})

	Version, Commit, SourceURL = "0.1.0-dev", "unknown", ""
	if got := CorrespondingSource(); got != Repository {
		t.Fatalf("dev build source = %q", got)
	}

	Version, Commit = "0.1.0-dev", "abc1234"
	if got := CorrespondingSource(); got != Repository+"/tree/abc1234" {
		t.Fatalf("dev commit source = %q", got)
	}

	Version, Commit = "1.2.0-beta.3", "abc1234"
	if got := CorrespondingSource(); got != Repository+"/tree/v1.2.0-beta.3" {
		t.Fatalf("release source = %q", got)
	}

	Version = "v1.2.0-beta.3"
	if got := CorrespondingSource(); got != Repository+"/tree/v1.2.0-beta.3" {
		t.Fatalf("prefixed tag source = %q", got)
	}

	SourceURL = "https://example.invalid/fork/tree/abc1234"
	if got := CorrespondingSource(); got != SourceURL {
		t.Fatalf("override source = %q", got)
	}
}

func TestOfferText(t *testing.T) {
	origV, origC, origS := Version, Commit, SourceURL
	t.Cleanup(func() {
		Version, Commit, SourceURL = origV, origC, origS
	})
	Version, Commit, SourceURL = "1.2.0-beta.3", "abc1234", ""
	text := CurrentOffer().Text()
	for _, want := range []string{
		"Toskar Core 1.2.0-beta.3",
		"Licensed under AGPL-3.0-or-later",
		"Corresponding source:",
		Repository + "/tree/v1.2.0-beta.3",
		"Commit: abc1234",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("offer text missing %q:\n%s", want, text)
		}
	}
}
