// Package ratings is community model ratings (#37): a person's own 1–5
// star rating of each model, shared with the ratings service only when they
// choose to, and the public summary of everyone's ratings, grouped by
// hardware like theirs. The contract is yeixio/toskar-ratings' schema
// version 1.
package ratings

import (
	"errors"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Identity is which model a rating is about. Ratings of different formats,
// quantizations, runtimes, or backends are never combined.
type Identity struct {
	// ID is the base model as a lower-case slug, such as
	// qwen2.5-coder-7b-instruct: the same whoever converted it.
	ID           string `json:"id"`
	Format       string `json:"format"`
	Quantization string `json:"quantization"`
	Runtime      string `json:"runtime"`
	Backend      string `json:"backend"`
}

// ErrUnknownModel is a model whose identity cannot be told, such as one
// installed from a URL outside Hugging Face.
var ErrUnknownModel = errors.New("which model this is cannot be told from where it was downloaded, so it cannot be compared with others' ratings")

var (
	slugRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,99}$`)
	quantRe = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_.-]{0,23}$`)
	// fileQuantRe finds a GGUF quantization in a file name.
	fileQuantRe = regexp.MustCompile(`(?i)(?:^|[-_.])((?:IQ|Q)\d(?:_[0-9A-Z]+)*|BF16|F16|F32)(?:[-_.]|$)`)
	nonSlug     = regexp.MustCompile(`[^a-z0-9._-]+`)
	// versionRe is a version the ratings service accepts.
	versionRe = regexp.MustCompile(`^[0-9A-Za-z.+-]{1,32}$`)
)

// Identify says which model m is, run by runtime on backend.
func Identify(m contracts.Model, backend string) (Identity, error) {
	u, err := url.Parse(m.Source.URL)
	if err != nil || !strings.HasSuffix(strings.ToLower(u.Hostname()), "huggingface.co") {
		return Identity{}, ErrUnknownModel
	}
	// /{owner}/{repo}/resolve/{revision}/{file}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return Identity{}, ErrUnknownModel
	}
	id := slugOf(parts[1])
	if !slugRe.MatchString(id) {
		return Identity{}, ErrUnknownModel
	}
	format := strings.ToLower(m.Source.Format)
	if format == "" && strings.HasSuffix(strings.ToLower(u.Path), ".gguf") {
		format = "gguf"
	}
	if !slices.Contains([]string{"gguf", "mlx", "safetensors", "onnx"}, format) {
		return Identity{}, ErrUnknownModel
	}
	quant := strings.ToUpper(strings.TrimSpace(m.Variant))
	if !quantRe.MatchString(quant) || !fileQuantRe.MatchString(quant) {
		quant = ""
		if f := fileQuantRe.FindStringSubmatch(path.Base(u.Path)); f != nil {
			quant = strings.ToUpper(f[1])
		}
	}
	if quant == "" {
		return Identity{}, ErrUnknownModel
	}
	runtime := "llamacpp"
	if len(m.Runtime) > 0 && m.Runtime[0] != "" {
		runtime = strings.ReplaceAll(strings.ToLower(m.Runtime[0]), ".", "")
	}
	if !slices.Contains([]string{"llamacpp", "mlx", "vllm", "ollama", "external"}, runtime) {
		return Identity{}, ErrUnknownModel
	}
	return Identity{ID: id, Format: format, Quantization: quant, Runtime: runtime, Backend: backend}, nil
}

// slugOf is a repository name as a model id: lower case, without the
// "-GGUF" conversions add.
func slugOf(repo string) string {
	s := strings.ToLower(repo)
	for _, suffix := range []string{"-gguf", "_gguf", ".gguf", "-mlx"} {
		s = strings.TrimSuffix(s, suffix)
	}
	s = nonSlug.ReplaceAllString(s, "-")
	return strings.Trim(s, "-._")
}
