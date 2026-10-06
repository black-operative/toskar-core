package weblink

import "testing"

func TestReadable(t *testing.T) {
	cases := map[string]string{
		"https://wttr.in/juneau?format=%25l%3A+%25t": "https://wttr.in/juneau",
		"https://wttr.in/new%20york?format=3":        "https://wttr.in/new%20york",
		"https://wttr.in/juneau":                     "https://wttr.in/juneau",
		"https://example.com/a?format=3":             "https://example.com/a?format=3",
		"not a url":                                  "not a url",
	}
	for in, want := range cases {
		if got := Readable(in); got != want {
			t.Errorf("Readable(%q) = %q, want %q", in, got, want)
		}
	}
}
