// Package webfixtures answers the web tools from a file of fixed pages
// instead of the internet, so a quality run (tests/quality/web.json) gives
// every model the same pages and an answer that matches them came from the
// page, not from the model's memory. The daemon uses it only when
// TOSKAR_WEB_FIXTURES names a file; it is never on for real use.
package webfixtures

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/yeixio/toskar-core/internal/tools"
)

// Site is a topic: the searches it answers, their results, and its pages.
type Site struct {
	Match   string `json:"match"`
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Snippet string `json:"snippet"`
	} `json:"results"`
	Pages map[string]string `json:"pages"`

	match *regexp.Regexp
}

// Fixtures is a web.json file. The iPhone app's quality run serves the same
// file.
type Fixtures struct {
	Sites []Site `json:"sites"`
}

// Load reads and checks a fixtures file.
func Load(path string) (*Fixtures, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f Fixtures
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for i := range f.Sites {
		re, err := regexp.Compile(f.Sites[i].Match)
		if err != nil {
			return nil, fmt.Errorf("%s: site %d: %w", path, i, err)
		}
		f.Sites[i].match = re
	}
	return &f, nil
}

// Search returns the results of the first site whose pattern matches the
// query, or one made-up result for anything else.
func (f *Fixtures) Search(query string) map[string]any {
	for _, site := range f.Sites {
		if site.match.MatchString(query) {
			var results []any
			for _, r := range site.Results {
				results = append(results, map[string]any{"title": r.Title, "url": r.URL, "snippet": r.Snippet})
			}
			return map[string]any{"results": results}
		}
	}
	slug := strings.ReplaceAll(strings.ToLower(query), " ", "-")
	return map[string]any{"results": []any{
		map[string]any{"title": query + " - Example", "url": "https://example.com/" + slug, "snippet": "Facts about " + query + "."},
	}}
}

// Places returns the results of the first site matching the query and
// where, as places, so a run never reaches OpenStreetMap.
func (f *Fixtures) Places(query, near string) map[string]any {
	var places []any
	for _, site := range f.Sites {
		if site.match.MatchString(query + " " + near) {
			for _, r := range site.Results {
				places = append(places, map[string]any{"name": r.Title, "address": r.Snippet, "website": r.URL})
			}
			break
		}
	}
	return map[string]any{"places": places}
}

// Open returns the page with the longest URL prefix of any site.
func (f *Fixtures) Open(url string) map[string]any {
	best, content := "", ""
	for _, site := range f.Sites {
		for prefix, page := range site.Pages {
			if strings.HasPrefix(url, prefix) && len(prefix) > len(best) {
				best, content = prefix, page
			}
		}
	}
	if content == "" {
		content = "This page has no more about that."
	}
	return map[string]any{"title": "Page", "url": url, "content": content}
}

// Register replaces the tools that reach the internet with ones answered
// from f. Those without fixtures, such as routes, say there is nothing.
func Register(r *tools.Registry, f *Fixtures) {
	str := func(args map[string]any, key string) string { s, _ := args[key].(string); return s }
	r.Register(tool{id: "internet.search", run: func(args map[string]any) map[string]any { return f.Search(str(args, "query")) }})
	r.Register(tool{id: "places.search", run: func(args map[string]any) map[string]any { return f.Places(str(args, "query"), str(args, "near")) }})
	r.Register(tool{id: "internet.open", run: func(args map[string]any) map[string]any { return f.Open(str(args, "url")) }})
	for _, id := range []string{"places.details", "maps.route", "maps.distance"} {
		r.Register(tool{id: id, run: func(map[string]any) map[string]any {
			return map[string]any{"error": "not available in this test's pages"}
		}})
	}
}

// tool is a web tool answered from fixtures.
type tool struct {
	id  string
	run func(args map[string]any) map[string]any
}

func (t tool) ID() string          { return t.id }
func (t tool) DisplayName() string { return t.id }
func (t tool) Description() string { return t.id }
func (t tool) Execute(_ context.Context, args map[string]any) (map[string]any, error) {
	return t.run(args), nil
}
