// Package weblink turns the address a page was read from into the one a
// person should see: what answers cite and the Sources list links to.
package weblink

import (
	"net/url"
	"strings"
)

// Readable returns the page a person would open for raw. wttr.in is read in
// its one-line format ("?format=%l: %t, …"), which reads as a percent-encoded
// query in an answer; the forecast page at the same path is what to link.
// Other addresses come back unchanged.
func Readable(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return raw
	}
	if strings.EqualFold(strings.TrimPrefix(u.Hostname(), "www."), "wttr.in") && u.Query().Has("format") {
		u.RawQuery = ""
		u.Fragment = ""
		return u.String()
	}
	return raw
}
