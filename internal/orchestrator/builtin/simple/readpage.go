package simple

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

const (
	answerAfterTools = "\n\nAnswer the user in plain text. Do not mention tool names, function-call syntax, or JSON."
	answerFromPage   = "\n\nAnswer the user's question from the page text. State the facts and include the page link. Do not reply with a list of websites. Do not mention tool names, function-call syntax, or JSON."
)

const maxPagesToRead = 3

func followLiveSearch(
	ctx context.Context,
	env pluginapi.ExecutionEnvironment,
	profile contracts.AIProfile,
	prompt string,
	searchArgs map[string]any,
	searchResult map[string]any,
) (map[string]any, bool) {
	if !tools.MessageNeedsLiveWeb(prompt) {
		return nil, false
	}
	return readBestPage(ctx, env, profile, prompt, searchArgs, searchResult)
}

// readBestPage opens the most useful result of a search and returns its text.
func readBestPage(
	ctx context.Context,
	env pluginapi.ExecutionEnvironment,
	profile contracts.AIProfile,
	prompt string,
	searchArgs map[string]any,
	searchResult map[string]any,
) (map[string]any, bool) {
	pages := readBestPages(ctx, env, profile, prompt, searchArgs, searchResult, 1)
	if len(pages) == 0 {
		return nil, false
	}
	return pages[0], true
}

// readBestPages opens up to want useful results of a search, trying a few
// extra links when some turn out to be empty or blocked.
func readBestPages(
	ctx context.Context,
	env pluginapi.ExecutionEnvironment,
	profile contracts.AIProfile,
	prompt string,
	searchArgs map[string]any,
	searchResult map[string]any,
	want int,
) []map[string]any {
	if want <= 0 || !toolEnabled(profile, "internet.open") {
		return nil
	}
	var out []map[string]any
	tried := 0
	for _, rawURL := range livePageURLs(prompt, searchArgs, searchResult) {
		if len(out) >= want || ctx.Err() != nil {
			break
		}
		if !usefulPageURL(rawURL) {
			continue
		}
		tried++
		if tried > maxPagesToRead+want-1 {
			break
		}
		page, err := env.ExecuteTool(ctx, "internet.open", map[string]any{"url": rawURL})
		if err != nil {
			continue
		}
		content, _ := page["content"].(string)
		if !usefulPageText(content) {
			continue
		}
		out = append(out, page)
	}
	return out
}

func toolEnabled(profile contracts.AIProfile, toolID string) bool {
	for _, def := range tools.Enabled(profile, nil) {
		if def.ID == toolID {
			return true
		}
	}
	return false
}

func searchResultURLs(result map[string]any) []string {
	payload, err := json.Marshal(result["results"])
	if err != nil {
		return nil
	}
	var rows []struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(payload, &rows); err != nil {
		return nil
	}
	var urls []string
	for _, row := range rows {
		if strings.TrimSpace(row.URL) != "" {
			urls = append(urls, row.URL)
		}
	}
	return urls
}

func livePageURLs(prompt string, args map[string]any, result map[string]any) []string {
	urls := orderedSearchURLs(prompt, result)
	if observation := weatherObservationURL(prompt, args); observation != "" {
		return append([]string{observation}, urls...)
	}
	return urls
}

func weatherObservationURL(prompt string, args map[string]any) string {
	lower := strings.ToLower(prompt)
	if !strings.Contains(lower, "weather") && !strings.Contains(lower, "forecast") {
		return ""
	}
	query, _ := args["query"].(string)
	place := weatherPlace(query)
	if place == "" {
		place = weatherPlace(prompt)
	}
	if place == "" {
		return ""
	}
	return "https://wttr.in/" + url.PathEscape(place) + "?format=" + url.QueryEscape("%l: %t, %C, humidity %h, wind %w")
}

func weatherPlace(value string) string {
	drop := map[string]bool{
		"weather": true, "forecast": true, "current": true, "today": true, "the": true,
		"for": true, "in": true, "show": true, "me": true, "what": true, "is": true,
		"whats": true, "what's": true, "can": true, "you": true, "please": true,
		"a": true, "right": true, "now": true, "my": true,
	}
	var kept []string
	for _, word := range strings.Fields(strings.ToLower(value)) {
		word = strings.Trim(word, "?.!,\"'")
		if word == "" || drop[word] {
			continue
		}
		kept = append(kept, word)
	}
	place := strings.Join(kept, " ")
	if len(place) < 2 || len(place) > 80 {
		return ""
	}
	return place
}

func usefulPageText(content string) bool {
	text := strings.TrimSpace(content)
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	return !strings.Contains(lower, "unknown location")
}

func orderedSearchURLs(prompt string, result map[string]any) []string {
	urls := searchResultURLs(result)
	lower := strings.ToLower(prompt)
	if !strings.Contains(lower, "weather") && !strings.Contains(lower, "forecast") {
		return urls
	}
	var preferred, rest []string
	for _, rawURL := range urls {
		host := pageHost(rawURL)
		if strings.Contains(host, "weather.gov") || host == "wttr.in" {
			preferred = append(preferred, rawURL)
			continue
		}
		rest = append(rest, rawURL)
	}
	return append(preferred, rest...)
}

func usefulPageURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	switch pageHost(raw) {
	case "duckduckgo.com", "google.com", "bing.com", "youtube.com", "youtu.be",
		"facebook.com", "instagram.com", "tiktok.com", "x.com", "twitter.com":
		return false
	default:
		return true
	}
}

func pageHost(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
}

// enabledIDs lists the tools a profile exposes.
func enabledIDs(profile contracts.AIProfile) []string {
	var out []string
	for _, def := range tools.Enabled(profile, nil) {
		out = append(out, def.ID)
	}
	return out
}

// offerOnly keeps the profile's policies for the offered tools and drops the
// rest for this turn, so they are neither described to the model nor run.
func offerOnly(profile contracts.AIProfile, offered []string) contracts.AIProfile {
	keep := map[string]bool{}
	for _, id := range offered {
		keep[id] = true
	}
	out := profile
	out.Tools = make([]contracts.ToolPolicy, 0, len(offered))
	for _, t := range profile.Tools {
		if keep[tools.Canonical(t.ToolID)] {
			out.Tools = append(out.Tools, t)
		}
	}
	return out
}
