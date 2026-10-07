package automations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// A trigger runs an automation when something changes instead of on every
// occurrence of its schedule (#204). The schedule then says how often to
// check. A check is cheap, a fetch and a comparison with the last one, and
// the model runs only when something changed, told what it was.
type Trigger struct {
	// Kind is page (a web page's text changed) or feed (an RSS or Atom
	// feed has new posts).
	Kind string `json:"kind"`
	URL  string `json:"url,omitempty"`
}

// Trigger kinds.
const (
	TriggerPage = "page"
	TriggerFeed = "feed"
)

// Validate checks a trigger can be watched.
func (t *Trigger) Validate() error {
	if t == nil {
		return nil
	}
	switch t.Kind {
	case TriggerPage, TriggerFeed:
		u, err := url.Parse(strings.TrimSpace(t.URL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("trigger url must be an http or https link")
		}
	default:
		return fmt.Errorf("unsupported trigger kind %q", t.Kind)
	}
	return nil
}

// Watcher checks a trigger: whether what it watches changed since state,
// what changed, and the state to keep. The first check has no state, keeps
// what it found, and isn't a change.
type Watcher interface {
	Check(ctx context.Context, trigger Trigger, state []byte) (Found, []byte, error)
}

// Found is what a check found.
type Found struct {
	Changed bool
	// Summary is what changed, for the run: lines added and removed on a
	// page, or a feed's new posts.
	Summary string
}

type changeKey struct{}

// WithChange gives the run what its trigger found.
func WithChange(ctx context.Context, c Found) context.Context {
	return context.WithValue(ctx, changeKey{}, c)
}

// ChangeNote is the text added to a run's prompt about what its trigger
// found, or "" when it wasn't started by one.
func ChangeNote(ctx context.Context) string {
	c, ok := ctx.Value(changeKey{}).(Found)
	if !ok || !c.Changed || strings.TrimSpace(c.Summary) == "" {
		return ""
	}
	return "This run was started because something changed since the last check:\n\n" + c.Summary
}

// maxSummaryLines keeps what changed short enough for a small model.
const maxSummaryLines = 40

type pageState struct {
	Hash  string   `json:"hash"`
	Lines []string `json:"lines"`
}

// CheckPage compares a page's readable text with the last check's.
func CheckPage(text string, state []byte) (Found, []byte, error) {
	lines := pageLines(text)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	next := pageState{Hash: hex.EncodeToString(sum[:]), Lines: lines}
	raw, err := json.Marshal(next)
	if err != nil {
		return Found{}, nil, err
	}
	var prev pageState
	if len(state) == 0 || json.Unmarshal(state, &prev) != nil || prev.Hash == "" {
		return Found{}, raw, nil
	}
	if prev.Hash == next.Hash {
		return Found{}, raw, nil
	}
	return Found{Changed: true, Summary: lineDiff(prev.Lines, lines)}, raw, nil
}

func pageLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// lineDiff lists lines added and removed, as a set: pages reflow, so line
// order matters less than what's new.
func lineDiff(before, after []string) string {
	had := map[string]int{}
	for _, l := range before {
		had[l]++
	}
	has := map[string]int{}
	for _, l := range after {
		has[l]++
	}
	var b strings.Builder
	n := 0
	for _, l := range after {
		if had[l] > 0 {
			had[l]--
			continue
		}
		if n < maxSummaryLines {
			b.WriteString("+ " + l + "\n")
		}
		n++
	}
	for _, l := range before {
		if has[l] > 0 {
			has[l]--
			continue
		}
		if n < maxSummaryLines {
			b.WriteString("- " + l + "\n")
		}
		n++
	}
	if n > maxSummaryLines {
		fmt.Fprintf(&b, "(and %d more lines)\n", n-maxSummaryLines)
	}
	return strings.TrimSpace(b.String())
}

type feedState struct {
	Seen []string `json:"seen"`
}

// maxSeen is how many post ids a feed remembers.
const maxSeen = 500

type feedItem struct {
	ID, Title, Link string
}

// CheckFeed finds posts in an RSS or Atom feed it hasn't seen before.
func CheckFeed(body []byte, state []byte) (Found, []byte, error) {
	items, err := parseFeed(body)
	if err != nil {
		return Found{}, nil, err
	}
	var prev feedState
	first := len(state) == 0 || json.Unmarshal(state, &prev) != nil
	seen := map[string]bool{}
	for _, id := range prev.Seen {
		seen[id] = true
	}
	var fresh []feedItem
	next := feedState{}
	for _, it := range items {
		if !seen[it.ID] {
			fresh = append(fresh, it)
		}
		next.Seen = append(next.Seen, it.ID)
	}
	// Keep older ids too, so a post that drops off and comes back isn't new.
	for _, id := range prev.Seen {
		if len(next.Seen) >= maxSeen {
			break
		}
		if !containsString(next.Seen, id) {
			next.Seen = append(next.Seen, id)
		}
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return Found{}, nil, err
	}
	if first || len(fresh) == 0 {
		return Found{}, raw, nil
	}
	var b strings.Builder
	for i, it := range fresh {
		if i == maxSummaryLines {
			fmt.Fprintf(&b, "(and %d more posts)\n", len(fresh)-i)
			break
		}
		title := it.Title
		if title == "" {
			title = it.Link
		}
		b.WriteString("- " + title)
		if it.Link != "" && it.Link != title {
			b.WriteString(" (" + it.Link + ")")
		}
		b.WriteString("\n")
	}
	return Found{Changed: true, Summary: "New posts:\n" + strings.TrimSpace(b.String())}, raw, nil
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// parseFeed reads RSS 2.0 or Atom.
func parseFeed(body []byte) ([]feedItem, error) {
	var doc struct {
		XMLName xml.Name
		Channel struct {
			Items []struct {
				GUID  string `xml:"guid"`
				Title string `xml:"title"`
				Link  string `xml:"link"`
			} `xml:"item"`
		} `xml:"channel"`
		Entries []struct {
			ID    string `xml:"id"`
			Title string `xml:"title"`
			Links []struct {
				Href string `xml:"href,attr"`
				Rel  string `xml:"rel,attr"`
			} `xml:"link"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, errors.New("not an RSS or Atom feed")
	}
	var out []feedItem
	switch strings.ToLower(doc.XMLName.Local) {
	case "rss":
		for _, it := range doc.Channel.Items {
			id := firstNonBlank(it.GUID, it.Link, it.Title)
			if id != "" {
				out = append(out, feedItem{ID: id, Title: strings.TrimSpace(it.Title), Link: strings.TrimSpace(it.Link)})
			}
		}
	case "feed":
		for _, e := range doc.Entries {
			link := ""
			for _, l := range e.Links {
				if l.Rel == "" || l.Rel == "alternate" {
					link = l.Href
					break
				}
			}
			id := firstNonBlank(e.ID, link, e.Title)
			if id != "" {
				out = append(out, feedItem{ID: id, Title: strings.TrimSpace(e.Title), Link: strings.TrimSpace(link)})
			}
		}
	default:
		return nil, errors.New("not an RSS or Atom feed")
	}
	return out, nil
}

func firstNonBlank(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}
