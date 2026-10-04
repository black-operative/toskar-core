package huginn

import (
	"slices"
	"testing"
)

func TestLinks(t *testing.T) {
	answer := "See [the guide](https://go.dev/doc/install). Or https://pkg.go.dev/net/http, and https://go.dev/doc/install again.\n" +
		"```sh\ncurl https://api.example.org/v1/items\n```\nRun `curl https://code.example/x` too."
	got := Links(answer)
	want := []string{"https://go.dev/doc/install", "https://pkg.go.dev/net/http"}
	if !slices.Equal(got, want) {
		t.Fatalf("links = %q, want %q", got, want)
	}
}

func TestUngroundedLinks(t *testing.T) {
	evidence := "Source: https://www.go.dev/doc/install/\nGo 1.25 is out."
	answer := "Install it from [go.dev](https://go.dev/doc/install#download), read https://go.dev/blog/made-up-post, " +
		"or visit https://go.dev. The docs at http://localhost:8080/docs and http://192.168.1.4/admin and https://example.com/path work too. " +
		"Your link https://github.com/yeixio/toskar-core/issues/12 is fine."
	got := UngroundedLinks(answer, evidence, "What about https://github.com/yeixio/toskar-core/issues/12?")
	if want := []string{"https://go.dev/blog/made-up-post"}; !slices.Equal(got, want) {
		t.Fatalf("ungrounded = %q, want %q", got, want)
	}
	if got := UngroundedLinks("No links here.", evidence); len(got) != 0 {
		t.Fatalf("no links: %q", got)
	}
}
