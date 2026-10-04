package hfclient

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/yeixio/toskar-core/internal/cache"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

var preferredQuants = []string{"q4_k_m", "q5_k_m", "q4_k_s", "q8_0", "q4_0"}

var paramRe = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*[bB]\b`)

// Client searches Hugging Face Hub for GGUF models Yggdrasil can run.
type Client struct {
	HTTP    *http.Client
	BaseURL string

	// Cache keeps recent search results (§36).
	Cache *cache.Cache[[]contracts.BrowseModel]
}

// CachePolicy is the Hub search cache's policy.
var CachePolicy = cache.Policy{
	Name: "model_search", Label: "Hugging Face model search", Key: "search text and result limit",
	TTL: 5 * time.Minute, Invalidation: "age", Scope: "this computer", Privacy: cache.Public, MaxEntries: 100,
}

// New creates a Hub client with a short in-memory cache.
func New() *Client {
	return &Client{
		HTTP:    &http.Client{Timeout: 20 * time.Second},
		BaseURL: "https://huggingface.co/api",
		Cache:   cache.New[[]contracts.BrowseModel](CachePolicy),
	}
}

// Search returns GGUF-filtered models matching query.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]contracts.BrowseModel, error) {
	if limit <= 0 || limit > 50 {
		limit = 24
	}
	q := strings.TrimSpace(query)
	cacheKey := strings.ToLower(q) + "|" + fmt.Sprint(limit)
	if cached, ok := c.Cache.Get(cacheKey); ok {
		return append([]contracts.BrowseModel(nil), cached...), nil
	}

	u, _ := url.Parse(c.BaseURL + "/models")
	vals := u.Query()
	vals.Set("filter", "gguf")
	vals.Set("sort", "downloads")
	vals.Set("direction", "-1")
	vals.Set("limit", fmt.Sprint(limit*2))
	if q != "" {
		vals.Set("search", q)
	}
	u.RawQuery = vals.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Toskar/0.1")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("huggingface api status %d", resp.StatusCode)
	}

	var raw []hfModel
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	var out []contracts.BrowseModel
	for _, m := range raw {
		b := mapRepo(m)
		if b.SourceURL == "" {
			continue
		}
		out = append(out, b)
		if len(out) >= limit {
			break
		}
	}

	c.Cache.Put(cacheKey, append([]contracts.BrowseModel(nil), out...))
	return out, nil
}

type hfModel struct {
	ID          string   `json:"id"`
	ModelID     string   `json:"modelId"`
	Downloads   int      `json:"downloads"`
	Tags        []string `json:"tags"`
	PipelineTag string   `json:"pipeline_tag"`
	Siblings    []hfFile `json:"siblings"`
}

type hfFile struct {
	RFilename string `json:"rfilename"`
	Size      int64  `json:"size"`
}

func mapRepo(m hfModel) contracts.BrowseModel {
	repo := m.ID
	if repo == "" {
		repo = m.ModelID
	}
	file, size, variant := pickGGUF(m.Siblings)
	if file == "" {
		// Sibling list often omitted from search; invent a resolve path if name looks like GGUF repo.
		if !strings.Contains(strings.ToLower(repo), "gguf") {
			return contracts.BrowseModel{}
		}
		file = guessFilename(repo)
		variant = "Q4_K_M"
	}
	name := friendlyName(repo, file)
	params := extractParams(repo + " " + file)
	tags := []string{}
	low := strings.ToLower(repo + " " + strings.Join(m.Tags, " "))
	if strings.Contains(low, "code") || strings.Contains(low, "coder") {
		tags = append(tags, "coding")
	}
	if strings.Contains(low, "vision") || strings.Contains(low, "llava") {
		tags = append(tags, "vision")
	}
	if strings.Contains(low, "reason") || strings.Contains(low, "r1") || strings.Contains(low, "think") {
		tags = append(tags, "reasoning")
	}
	if len(tags) == 0 {
		tags = append(tags, "general")
	}
	id := sanitizeID(repo + "-" + variant)
	return contracts.BrowseModel{
		ID:          id,
		DisplayName: name,
		Summary:     "From Hugging Face · " + repo,
		RepoID:      repo,
		Filename:    file,
		SourceURL:   fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", repo, file),
		SizeBytes:   uint64(size),
		Parameters:  params,
		Variant:     variant,
		Downloads:   m.Downloads,
		Tags:        tags,
	}
}

func pickGGUF(files []hfFile) (name string, size int64, variant string) {
	type cand struct {
		f     hfFile
		rank  int
		quant string
	}
	var cands []cand
	for _, f := range files {
		low := strings.ToLower(f.RFilename)
		if !strings.HasSuffix(low, ".gguf") {
			continue
		}
		if strings.Contains(low, "mmproj") || strings.Contains(low, "encoder") {
			continue
		}
		quant := detectQuant(low)
		rank := 100
		for i, q := range preferredQuants {
			if quant == q {
				rank = i
				break
			}
		}
		cands = append(cands, cand{f: f, rank: rank, quant: quant})
	}
	if len(cands) == 0 {
		return "", 0, ""
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].rank != cands[j].rank {
			return cands[i].rank < cands[j].rank
		}
		return cands[i].f.Size < cands[j].f.Size
	})
	best := cands[0]
	v := strings.ToUpper(best.quant)
	if v == "" {
		v = "GGUF"
	}
	return best.f.RFilename, best.f.Size, v
}

func detectQuant(name string) string {
	for _, q := range preferredQuants {
		if strings.Contains(name, q) {
			return q
		}
	}
	return ""
}

func guessFilename(repo string) string {
	base := repo
	if i := strings.LastIndex(repo, "/"); i >= 0 {
		base = repo[i+1:]
	}
	base = strings.TrimSuffix(base, "-GGUF")
	base = strings.TrimSuffix(base, "-gguf")
	return base + "-Q4_K_M.gguf"
}

func friendlyName(repo, file string) string {
	base := repo
	if i := strings.LastIndex(repo, "/"); i >= 0 {
		base = repo[i+1:]
	}
	base = strings.ReplaceAll(base, "-GGUF", "")
	base = strings.ReplaceAll(base, "-gguf", "")
	base = strings.ReplaceAll(base, "-Instruct", "")
	base = strings.ReplaceAll(base, "-instruct", "")
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.TrimSpace(base)
	if base == "" {
		base = file
	}
	return base
}

func extractParams(s string) string {
	m := paramRe.FindStringSubmatch(s)
	if len(m) >= 2 {
		return m[1] + "B"
	}
	return ""
}

func sanitizeID(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '/' || r == '-' || r == '_' || r == '.':
			b.WriteByte('-')
		}
	}
	out := b.String()
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	return strings.Trim(out, "-")
}
