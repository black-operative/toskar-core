// Package gguf reads the few header values Yggdrasil needs from a GGUF model
// file, such as how many layers and attention heads it has, without reading
// its weights.
package gguf

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
)

// Info is a model's shape, as its GGUF header states it.
type Info struct {
	Architecture string
	// Layers is {arch}.block_count.
	Layers uint64
	// HeadCount and HeadCountKV are the attention heads, and the key/value
	// heads (fewer with grouped-query attention). A per-layer list counts its
	// largest value.
	HeadCount   uint64
	HeadCountKV uint64
	// Embedding is {arch}.embedding_length.
	Embedding uint64
	// KeyLength and ValueLength are the per-head sizes, when the header
	// states them; otherwise Embedding / HeadCount.
	KeyLength   uint64
	ValueLength uint64
}

// KVCacheBytes is about how much memory llama.cpp reserves for a window of
// this many tokens: keys and values for every layer and key/value head, at
// bytesPerElement each (2 for the default f16 cache). It is 0 when the
// header lacks what it needs. Models with sliding-window or compressed
// attention use less, so it is an upper estimate.
func (i Info) KVCacheBytes(window int, bytesPerElement int) uint64 {
	if window <= 0 || bytesPerElement <= 0 || i.Layers == 0 || i.HeadCount == 0 {
		return 0
	}
	kvHeads := i.HeadCountKV
	if kvHeads == 0 {
		kvHeads = i.HeadCount
	}
	key := i.KeyLength
	if key == 0 {
		if i.Embedding == 0 {
			return 0
		}
		key = i.Embedding / i.HeadCount
	}
	value := i.ValueLength
	if value == 0 {
		value = key
	}
	return i.Layers * uint64(window) * kvHeads * (key + value) * uint64(bytesPerElement)
}

// GGUF metadata value types.
const (
	typeUint8 = iota
	typeInt8
	typeUint16
	typeInt16
	typeUint32
	typeInt32
	typeFloat32
	typeBool
	typeString
	typeArray
	typeUint64
	typeInt64
	typeFloat64
)

// ReadInfo reads a GGUF file's header and returns its shape.
func ReadInfo(path string) (Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()
	return readInfo(bufio.NewReaderSize(f, 1<<16))
}

func readInfo(r *bufio.Reader) (Info, error) {
	d := decoder{r: r}
	if magic := d.bytes(4); string(magic) != "GGUF" {
		if d.err != nil {
			return Info{}, d.err
		}
		return Info{}, errors.New("not a GGUF file")
	}
	if version := d.u32(); version < 2 {
		return Info{}, fmt.Errorf("GGUF version %d is not supported", version)
	}
	d.u64() // tensor count
	count := d.u64()
	if d.err != nil {
		return Info{}, d.err
	}
	ints := map[string]uint64{}
	var info Info
	for n := uint64(0); n < count && d.err == nil; n++ {
		key := d.str()
		typ := d.u32()
		switch {
		case key == "general.architecture" && typ == typeString:
			info.Architecture = d.str()
		case wanted(key):
			if v, ok := d.number(typ); ok {
				ints[key] = v
			}
		default:
			d.skip(typ, 0)
		}
	}
	if d.err != nil {
		return Info{}, d.err
	}
	field := func(name string) uint64 { return ints[info.Architecture+"."+name] }
	info.Layers = field("block_count")
	info.HeadCount = field("attention.head_count")
	info.HeadCountKV = field("attention.head_count_kv")
	info.Embedding = field("embedding_length")
	info.KeyLength = field("attention.key_length")
	info.ValueLength = field("attention.value_length")
	return info, nil
}

// wanted is a key whose number ReadInfo keeps, for any architecture.
func wanted(key string) bool {
	for _, suffix := range []string{".block_count", ".attention.head_count", ".attention.head_count_kv", ".embedding_length", ".attention.key_length", ".attention.value_length"} {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return false
}

type decoder struct {
	r   *bufio.Reader
	err error
}

func (d *decoder) bytes(n int) []byte {
	if d.err != nil {
		return nil
	}
	b := make([]byte, n)
	_, d.err = io.ReadFull(d.r, b)
	return b
}

func (d *decoder) u32() uint32 {
	b := d.bytes(4)
	if d.err != nil {
		return 0
	}
	return binary.LittleEndian.Uint32(b)
}

func (d *decoder) u64() uint64 {
	b := d.bytes(8)
	if d.err != nil {
		return 0
	}
	return binary.LittleEndian.Uint64(b)
}

func (d *decoder) discard(n uint64) {
	if d.err != nil {
		return
	}
	for n > 0 {
		step := n
		if step > math.MaxInt32 {
			step = math.MaxInt32
		}
		var got int
		got, d.err = d.r.Discard(int(step))
		if d.err != nil {
			return
		}
		n -= uint64(got)
	}
}

func (d *decoder) str() string {
	n := d.u64()
	if d.err != nil {
		return ""
	}
	if n > 1<<20 {
		d.err = errors.New("GGUF string too long")
		return ""
	}
	return string(d.bytes(int(n)))
}

// number reads an integer value, or the largest of an integer array (some
// models list key/value heads per layer).
func (d *decoder) number(typ uint32) (uint64, bool) {
	switch typ {
	case typeUint8, typeInt8:
		b := d.bytes(1)
		if d.err != nil {
			return 0, false
		}
		return uint64(b[0]), true
	case typeUint16, typeInt16:
		b := d.bytes(2)
		if d.err != nil {
			return 0, false
		}
		return uint64(binary.LittleEndian.Uint16(b)), true
	case typeUint32, typeInt32:
		return uint64(d.u32()), d.err == nil
	case typeUint64, typeInt64:
		return d.u64(), d.err == nil
	case typeArray:
		elem := d.u32()
		n := d.u64()
		var largest uint64
		for i := uint64(0); i < n && d.err == nil; i++ {
			v, ok := d.number(elem)
			if !ok {
				return 0, false
			}
			largest = max(largest, v)
		}
		return largest, d.err == nil
	default:
		d.skip(typ, 0)
		return 0, false
	}
}

// skip passes over a value of the given type.
func (d *decoder) skip(typ uint32, depth int) {
	if d.err != nil {
		return
	}
	switch typ {
	case typeUint8, typeInt8, typeBool:
		d.discard(1)
	case typeUint16, typeInt16:
		d.discard(2)
	case typeUint32, typeInt32, typeFloat32:
		d.discard(4)
	case typeUint64, typeInt64, typeFloat64:
		d.discard(8)
	case typeString:
		d.discard(d.u64())
	case typeArray:
		if depth > 2 {
			d.err = errors.New("GGUF arrays nested too deeply")
			return
		}
		elem := d.u32()
		n := d.u64()
		if size := fixedSize(elem); size > 0 {
			d.discard(n * size)
			return
		}
		for i := uint64(0); i < n && d.err == nil; i++ {
			d.skip(elem, depth+1)
		}
	default:
		d.err = fmt.Errorf("unknown GGUF value type %d", typ)
	}
}

func fixedSize(typ uint32) uint64 {
	switch typ {
	case typeUint8, typeInt8, typeBool:
		return 1
	case typeUint16, typeInt16:
		return 2
	case typeUint32, typeInt32, typeFloat32:
		return 4
	case typeUint64, typeInt64, typeFloat64:
		return 8
	}
	return 0
}
