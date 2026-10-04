package mimir

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"unicode/utf8"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Semantic search (AI experience spec §61). When an embedding model is
// installed, every passage also gets a vector, and a search combines keyword
// (BM25) and meaning (cosine similarity) matches. Without one, search is
// keyword-only, exactly as before.

// Embedder turns passages and questions into vectors.
type Embedder interface {
	// ModelID names the embedding model. Vectors from different models are
	// never compared; changing it re-embeds every passage.
	ModelID() string
	// EmbedDocuments returns one vector per passage, in order.
	EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error)
	// EmbedQuery returns the vector for a question.
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
}

// Reranker scores how well each passage answers a question.
type Reranker interface {
	// Rerank returns one score per passage, in order. Higher is better.
	Rerank(ctx context.Context, query string, passages []string) ([]float64, error)
}

// Models finds the supporting models Mimir can use right now. Either method
// returns nil, nil when no such model is installed, and ErrNotNow when one is
// installed but should not be loaded at the moment (for example while
// training holds the computer).
type Models interface {
	Embedder(ctx context.Context) (Embedder, error)
	Reranker(ctx context.Context) (Reranker, error)
}

// ErrNotNow means a supporting model is installed but cannot run now. Search
// falls back to keywords, and background embedding tries again later.
var ErrNotNow = contracts.NewError("SUPPORT_MODEL_BUSY", nil, errors.New("the supporting model cannot run right now"))

// SetModels lets Mimir use installed embedding and reranker models. Call it
// before StartIndexing. Passing nil keeps search keyword-only.
func (s *Store) SetModels(m Models) {
	s.semMu.Lock()
	s.models = m
	s.semMu.Unlock()
}

func (s *Store) supportingModels() Models {
	s.semMu.Lock()
	defer s.semMu.Unlock()
	return s.models
}

// Limits on semantic indexing.
const (
	// maxEmbeddedChunks: a source with more passages than this is searched by
	// keyword only, which keeps a query's vector scan quick.
	maxEmbeddedChunks = 20000
	// embedTextRunes caps the text embedded per passage so it fits the
	// embedding model's window. Long table rows are cut.
	embedTextRunes = 1500
)

// embedText is what is embedded for a passage: its title (file, heading, or
// row) and body.
func embedText(title, body string) string {
	t := title + "\n" + body
	if utf8.RuneCountInString(t) > embedTextRunes {
		t = string([]rune(t)[:embedTextRunes])
	}
	return t
}

func textHash(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:16])
}

// Vectors are stored normalized and quantized to int8 with one float32 scale,
// which is a quarter of the space of float32 and keeps cosine similarity
// within about 1% for typical embedding sizes.

// encodeVector normalizes v and packs it as a little-endian float32 scale
// followed by one int8 per dimension. It returns nil for a zero vector.
func encodeVector(v []float32) []byte {
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		return nil
	}
	norm = math.Sqrt(norm)
	var maxAbs float64
	for _, x := range v {
		maxAbs = math.Max(maxAbs, math.Abs(float64(x)/norm))
	}
	scale := maxAbs / 127
	out := make([]byte, 4+len(v))
	binary.LittleEndian.PutUint32(out, math.Float32bits(float32(scale)))
	for i, x := range v {
		out[4+i] = byte(int8(math.Round(float64(x) / norm / scale)))
	}
	return out
}

// quantized is a decoded vector.
type quantized struct {
	scale float32
	q     []int8
}

func decodeVector(b []byte) (quantized, bool) {
	if len(b) < 5 {
		return quantized{}, false
	}
	q := make([]int8, len(b)-4)
	for i, c := range b[4:] {
		q[i] = int8(c)
	}
	return quantized{scale: math.Float32frombits(binary.LittleEndian.Uint32(b)), q: q}, true
}

// cosine of two stored vectors. Both are normalized, so this is their dot
// product. Vectors of different sizes do not match.
func (a quantized) cosine(b quantized) float64 {
	if len(a.q) != len(b.q) {
		return 0
	}
	var dot int64
	for i, x := range a.q {
		dot += int64(x) * int64(b.q[i])
	}
	return float64(dot) * float64(a.scale) * float64(b.scale)
}

// storedVector is a passage's vector, reused across a reindex when its text
// is unchanged.
type storedVector struct {
	model string
	vec   []byte
}

// keepVectors returns the vectors of a source's passages by text hash, so a
// reindex can keep them. It runs inside the reindex transaction.
func keepVectors(ctx context.Context, tx *sql.Tx, sourceID string) (map[string]storedVector, error) {
	rows, err := tx.QueryContext(ctx, `SELECT hash, model, vec FROM knowledge_vectors WHERE source_id = ?`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]storedVector{}
	for rows.Next() {
		var hash string
		var v storedVector
		if err := rows.Scan(&hash, &v.model, &v.vec); err != nil {
			return nil, err
		}
		out[hash] = v
	}
	return out, rows.Err()
}
