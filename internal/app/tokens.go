package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/cache"
	"github.com/yeixio/toskar-core/internal/contextusage"
	"github.com/yeixio/toskar-core/internal/gguf"
	"github.com/yeixio/toskar-core/internal/runtimes/llamacpp"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// tokenCountPolicy keeps counts from the model's tokenizer, so a long
// conversation is not re-counted every turn (§66). The key is a hash, but
// it still says which texts were seen, so it is personal.
var tokenCountPolicy = cache.Policy{
	Name: "token_counts", Label: "Token counts", Key: "the model and a hash of the text",
	TTL: time.Hour, Invalidation: "age; cleared with run records", Scope: "this computer",
	Privacy: cache.Personal, MaxEntries: 5000,
}

// tokenizeTimeout bounds one count; a slow tokenizer falls back to the estimate.
const tokenizeTimeout = 5 * time.Second

// tokenCounter counts with the tokenizer of modelID while it runs on this
// computer, and estimates otherwise (AI experience spec §66). It never
// loads a model just to count.
func (a *App) tokenCounter(ctx context.Context, modelID string) contextusage.Counter {
	if a.stubInference || a.Runtimes == nil || modelID == "" {
		return nil
	}
	tokenize := a.tokenize
	if tokenize == nil {
		tokenize = llamacpp.Tokenize
	}
	var mu sync.Mutex
	endpoint, failed := "", false
	return func(text string) (int, bool) {
		sum := sha256.Sum256([]byte(text))
		key := modelID + "/" + hex.EncodeToString(sum[:])
		if a.tokenCounts != nil {
			if n, ok := a.tokenCounts.Get(key); ok {
				return n, true
			}
		}
		mu.Lock()
		if endpoint == "" && !failed {
			// The model may load between calls, so a miss is not kept.
			endpoint = a.runningEndpoint(ctx, modelID)
		}
		ep, skip := endpoint, failed
		mu.Unlock()
		if ep == "" || skip {
			return contextusage.Estimate(text), false
		}
		cctx, cancel := context.WithTimeout(ctx, tokenizeTimeout)
		defer cancel()
		n, err := tokenize(cctx, ep, text)
		if err != nil {
			// One failure is enough to stop asking for the rest of the turn.
			if a.Logger != nil {
				a.Logger.Debug("count tokens", "model_id", modelID, "error", err)
			}
			mu.Lock()
			failed = true
			mu.Unlock()
			return contextusage.Estimate(text), false
		}
		if a.tokenCounts != nil {
			a.tokenCounts.Put(key, n)
		}
		return n, true
	}
}

// runningEndpoint is the endpoint of a chat instance of modelID running on
// this computer, or "".
func (a *App) runningEndpoint(ctx context.Context, modelID string) string {
	running, err := a.Runtimes.ListRunning(ctx, "llamacpp")
	if err != nil {
		return ""
	}
	for _, r := range running {
		if r.ModelID == modelID && r.Status == "running" && r.Endpoint != "" &&
			r.Mode != pluginapi.ModeEmbedding && r.Mode != pluginapi.ModeReranking {
			return r.Endpoint
		}
	}
	return ""
}

// kvBytesPerElement is the size of one key or value entry in llama.cpp's
// default f16 cache.
const kvBytesPerElement = 2

// localContext is the window a model running on this computer actually has,
// and about how much memory that window reserves (its KV cache), for the
// context gauge (#230). ok is false when the model isn't running here, and
// memory is 0 when its file's header can't be read.
func (a *App) localContext(ctx context.Context, modelID string) (window int, memory uint64, ok bool) {
	if a.stubInference || a.Runtimes == nil || modelID == "" {
		return 0, 0, false
	}
	endpoint := a.runningEndpoint(ctx, modelID)
	if endpoint == "" {
		return 0, 0, false
	}
	if v, hit := a.windows.Load(endpoint); hit {
		window = v.(int)
	} else {
		read := a.window
		if read == nil {
			read = llamacpp.Window
		}
		wctx, cancel := context.WithTimeout(ctx, tokenizeTimeout)
		n, err := read(wctx, endpoint)
		cancel()
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		window = n
		a.windows.Store(endpoint, n)
	}
	if a.Models != nil {
		if path, err := a.Models.Path(ctx, modelID); err == nil && path != "" {
			var info gguf.Info
			if v, hit := a.shapes.Load(path); hit {
				info = v.(gguf.Info)
			} else if read, err := gguf.ReadInfo(path); err == nil {
				info = read
				a.shapes.Store(path, info)
			}
			memory = info.KVCacheBytes(window, kvBytesPerElement)
		}
	}
	return window, memory, true
}
