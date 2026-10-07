package automations

import (
	"strings"
	"testing"
)

// A page counts as changed when its text did, and the run is told which
// lines came and went (#204).
func TestCheckPage(t *testing.T) {
	found, state, err := CheckPage("Jobs\nEngineer\nDesigner", nil)
	if err != nil || found.Changed || len(state) == 0 {
		t.Fatalf("first check = %+v, %v", found, err)
	}
	// Spacing alone isn't a change.
	if found, state, _ = CheckPage("  Jobs \n\nEngineer\n   Designer  ", state); found.Changed {
		t.Fatal("reflowed text counted as a change")
	}
	found, _, _ = CheckPage("Jobs\nEngineer\nProduct manager", state)
	if !found.Changed || !strings.Contains(found.Summary, "+ Product manager") || !strings.Contains(found.Summary, "- Designer") {
		t.Fatalf("change = %+v", found)
	}
}

const rss = `<?xml version="1.0"?><rss version="2.0"><channel><title>Blog</title>
<item><title>Second post</title><link>https://example.com/2</link><guid>2</guid></item>
<item><title>First post</title><link>https://example.com/1</link><guid>1</guid></item>
</channel></rss>`

const atom = `<?xml version="1.0" encoding="utf-8"?><feed xmlns="http://www.w3.org/2005/Atom"><title>Releases</title>
<entry><id>tag:v2</id><title>v2.0</title><link rel="alternate" href="https://example.com/v2"/></entry>
</feed>`

// A feed counts as changed when it has posts it hasn't seen (#204).
func TestCheckFeed(t *testing.T) {
	found, state, err := CheckFeed([]byte(rss), nil)
	if err != nil || found.Changed {
		t.Fatalf("first check = %+v, %v", found, err)
	}
	newer := strings.Replace(rss, "<item><title>Second", `<item><title>Third post</title><link>https://example.com/3</link><guid>3</guid></item><item><title>Second`, 1)
	found, state, _ = CheckFeed([]byte(newer), state)
	if !found.Changed || !strings.Contains(found.Summary, "Third post (https://example.com/3)") || strings.Contains(found.Summary, "Second post") {
		t.Fatalf("new post = %+v", found)
	}
	// A post that drops off the feed and comes back isn't new.
	dropped := strings.Replace(newer, `<item><title>First post</title><link>https://example.com/1</link><guid>1</guid></item>`, "", 1)
	_, state, _ = CheckFeed([]byte(dropped), state)
	if found, _, _ = CheckFeed([]byte(newer), state); found.Changed {
		t.Fatalf("returning post counted as new: %+v", found)
	}

	_, astate, err := CheckFeed([]byte(atom), nil)
	if err != nil {
		t.Fatal(err)
	}
	more := strings.Replace(atom, "<entry>", `<entry><id>tag:v3</id><title>v3.0</title><link href="https://example.com/v3"/></entry><entry>`, 1)
	if found, _, _ = CheckFeed([]byte(more), astate); !found.Changed || !strings.Contains(found.Summary, "v3.0") {
		t.Fatalf("atom = %+v", found)
	}
	if _, _, err := CheckFeed([]byte("<html><body>not a feed</body></html>"), nil); err == nil {
		t.Fatal("read a page as a feed")
	}
}

func TestTriggerValidate(t *testing.T) {
	for _, bad := range []*Trigger{{Kind: "page", URL: "file:///etc/passwd"}, {Kind: "page"}, {Kind: "folder", URL: "https://x"}} {
		if bad.Validate() == nil {
			t.Errorf("%+v was accepted", bad)
		}
	}
	if (&Trigger{Kind: "feed", URL: "https://example.com/feed.xml"}).Validate() != nil {
		t.Error("a feed link was refused")
	}
}
