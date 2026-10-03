package gguf

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// header writes a GGUF v3 header with the given metadata, in order.
type header struct{ buf bytes.Buffer }

func (h *header) u32(v uint32) { _ = binary.Write(&h.buf, binary.LittleEndian, v) }
func (h *header) u64(v uint64) { _ = binary.Write(&h.buf, binary.LittleEndian, v) }
func (h *header) str(s string) { h.u64(uint64(len(s))); h.buf.WriteString(s) }

func writeGGUF(t *testing.T, kvs func(h *header) int) string {
	t.Helper()
	var body header
	n := kvs(&body)
	var h header
	h.buf.WriteString("GGUF")
	h.u32(3)
	h.u64(0) // tensors
	h.u64(uint64(n))
	h.buf.Write(body.buf.Bytes())
	path := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(path, h.buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadInfoAndKVCache(t *testing.T) {
	// Shaped like Llama 3 8B: 32 layers, 32 heads, 8 key/value heads, 4096 wide.
	path := writeGGUF(t, func(h *header) int {
		h.str("general.architecture")
		h.u32(typeString)
		h.str("llama")
		// A large string array before the values, as the tokenizer is.
		h.str("tokenizer.ggml.tokens")
		h.u32(typeArray)
		h.u32(typeString)
		h.u64(3)
		for _, tok := range []string{"<s>", "hello", "world"} {
			h.str(tok)
		}
		h.str("tokenizer.ggml.scores")
		h.u32(typeArray)
		h.u32(typeFloat32)
		h.u64(3)
		h.buf.Write(make([]byte, 12))
		for _, kv := range []struct {
			key string
			v   uint32
		}{{"llama.block_count", 32}, {"llama.attention.head_count", 32}, {"llama.attention.head_count_kv", 8}, {"llama.embedding_length", 4096}} {
			h.str(kv.key)
			h.u32(typeUint32)
			h.u32(kv.v)
		}
		return 7
	})
	info, err := ReadInfo(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Info{Architecture: "llama", Layers: 32, HeadCount: 32, HeadCountKV: 8, Embedding: 4096}
	if info != want {
		t.Fatalf("info = %+v, want %+v", info, want)
	}
	// 32 layers × 8192 tokens × 8 heads × (128 + 128) × 2 bytes = 1 GiB.
	if got := info.KVCacheBytes(8192, 2); got != 1<<30 {
		t.Fatalf("KV cache = %d, want %d", got, 1<<30)
	}
	if info.KVCacheBytes(0, 2) != 0 || (Info{}).KVCacheBytes(8192, 2) != 0 {
		t.Fatal("a missing window or shape should give 0")
	}
}

func TestPerLayerHeadsAndExplicitLengths(t *testing.T) {
	path := writeGGUF(t, func(h *header) int {
		h.str("general.architecture")
		h.u32(typeString)
		h.str("gemma3")
		h.str("gemma3.block_count")
		h.u32(typeUint64)
		h.u64(2)
		h.str("gemma3.attention.head_count")
		h.u32(typeUint32)
		h.u32(8)
		// Per-layer key/value heads: the largest counts.
		h.str("gemma3.attention.head_count_kv")
		h.u32(typeArray)
		h.u32(typeInt32)
		h.u64(2)
		h.u32(2)
		h.u32(4)
		h.str("gemma3.attention.key_length")
		h.u32(typeUint32)
		h.u32(256)
		return 5
	})
	info, err := ReadInfo(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.HeadCountKV != 4 || info.KeyLength != 256 || info.ValueLength != 0 {
		t.Fatalf("info = %+v", info)
	}
	// Value length defaults to the key length: 2 × 100 × 4 × 512 × 2.
	if got := info.KVCacheBytes(100, 2); got != 2*100*4*512*2 {
		t.Fatalf("KV cache = %d", got)
	}
}

func TestNotGGUF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.bin")
	_ = os.WriteFile(path, []byte("nope, not a model"), 0o600)
	if _, err := ReadInfo(path); err == nil {
		t.Fatal("want an error for a file that isn't GGUF")
	}
}
