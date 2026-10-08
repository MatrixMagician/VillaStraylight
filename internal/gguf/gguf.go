// Package gguf reads the header and metadata (KV) section of a GGUF file, and
// nothing else. It stops at the end of the KV section: it never reads the tensor
// index, never reads tensor data, and never mmaps the file, so the cost of asking
// a 20 GB model for its geometry is a few kilobytes.
//
// INVARIANT: the catalog stays the source of truth. The values this package
// returns are a WITNESS, never a replacement — nothing here feeds the recommend
// fit inequality, and nothing here rewrites a catalog entry. A disagreement
// between the vetted entry and the file on disk is a FINDING for the operator, so
// an unvetted or re-quantized file can never silently move the fit.
//
// A layer that uses sliding-window attention is not a KV-bearing layer: llama.cpp
// bounds its cache at the window, so only the global layers grow with the context
// and only they enter the KV term of the fit.
//
// The package is pure and stdlib-only. It takes an io.Reader; opening the file is
// the caller's seam, which is what keeps `os` out of this package and out of
// internal/preflight.
package gguf

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
)

// Sentinel errors callers match on with errors.Is. Every other failure is a
// descriptive fmt.Errorf naming the key, the cap, or the offending type.
var (
	// ErrBadMagic means the first four bytes are not "GGUF": this is not a GGUF file.
	ErrBadMagic = errors.New("gguf: not a GGUF file")
	// ErrUnsupportedVersion means the container version is outside the v2/v3 window
	// this reader has been written against. A format villa has not read is refused,
	// never guessed at.
	ErrUnsupportedVersion = errors.New("gguf: unsupported format version")
)

// GGUF metadata value types, as encoded in the u32 that follows each key.
const (
	typeUint8 uint32 = iota
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

// Sanity caps on the three lengths a corrupt or hostile file controls. Each one
// bounds an allocation or a loop before it is made, and exceeding one is an error
// naming the cap rather than an out-of-memory kill.
const (
	maxKVCount    = 1 << 20
	maxStringLen  = 1 << 24
	maxArrayCount = 1 << 28
	// maxLayerArray bounds the integer and bool arrays kept in memory. No model has
	// this many blocks, and a vocabulary array has far more elements, so a
	// per-layer array is retained and a vocabulary never is.
	maxLayerArray = 4096
)

// Geometry is the three per-model dimensions the recommend fit inequality
// consumes. It is the exact subset the catalog hand-carries, so the two are
// directly comparable.
type Geometry struct {
	// KVLayers is the number of layers whose per-token KV cache grows with the
	// context, which on a dense architecture is the block count but otherwise is
	// NOT. A hybrid (Gated-DeltaNet / SSM) model gives most of its blocks a
	// fixed-size recurrent state and only every full_attention_interval-th block a
	// growing KV cache. A sliding-window layer is not KV-bearing either, because
	// llama.cpp bounds its cache at the window. Counting blocks would overstate the
	// KV term by those factors. The name says what it counts because the two are
	// equal often enough to hide the bug.
	KVLayers int
	// HeadCountKV is the grouped-query KV head count, not the attention head count.
	HeadCountKV int
	// KeyLength is the per-head key dimension.
	KeyLength int
}

// Header is the decoded GGUF header plus its metadata section. The kv map holds
// scalar and string values, plus integer and bool arrays of at most maxLayerArray
// elements (the per-layer arrays). Every other array is parsed for its length and
// discarded, leaving behind a marker so a key that IS an array is distinguishable
// from one that is absent.
type Header struct {
	Version     uint32
	TensorCount uint64

	kv map[string]any
}

// arrayVal marks a key whose value was an array that was not retained. The
// marker is what lets uintKey refuse an array as unsupported instead of reporting
// the key missing.
type arrayVal struct{}

// Arch returns general.architecture, the prefix every geometry key is namespaced
// under. It is empty when the key is absent or is not a string.
func (h Header) Arch() string {
	s, _ := h.String(archKey)
	return s
}

// String returns a string-typed metadata value and whether it was present.
func (h Header) String(key string) (string, bool) {
	s, ok := h.kv[key].(string)
	return s, ok
}

// Uint returns an integer-typed metadata value widened to uint64, accepting every
// GGUF integer type (u8..u64, and i8..i64 when non-negative). The same logical key
// is emitted as u32 by one converter and u64 by another, so a caller must not have
// to know which.
func (h Header) Uint(key string) (uint64, bool) {
	switch v := h.kv[key].(type) {
	case uint64:
		return v, true
	case int64:
		if v < 0 {
			return 0, false
		}
		return uint64(v), true
	default:
		return 0, false
	}
}

// Uints returns an integer array retained at read time, widened to uint64. It is
// false for an absent key, a non-array, an array longer than maxLayerArray, or one
// with a negative element.
func (h Header) Uints(key string) ([]uint64, bool) {
	v, ok := h.kv[key].([]uint64)
	return v, ok
}

// Bools returns a bool array retained at read time, with the limits of Uints.
func (h Header) Bools(key string) ([]bool, bool) {
	v, ok := h.kv[key].([]bool)
	return v, ok
}

// archKey is the one metadata key that is not architecture-namespaced.
const archKey = "general.architecture"

// Geometry extracts the fit dimensions from the metadata. key_length is optional
// in practice, so an absent one is derived from embedding_length / head_count
// rather than refused.
//
// A missing key is an ERROR naming the key, never a zero value: the caller turns
// that into a typed-Unknown WARN, and a fabricated zero would instead read as a
// confident mismatch against the catalog.
func (h Header) Geometry() (Geometry, error) {
	arch := h.Arch()
	if arch == "" {
		return Geometry{}, fmt.Errorf("gguf: missing key %s", archKey)
	}
	blocks, err := h.uintKey(arch + ".block_count")
	if err != nil {
		return Geometry{}, err
	}
	kvLayers, bearing, err := h.kvLayerSet(arch, int(blocks))
	if err != nil {
		return Geometry{}, err
	}
	kvHeads, err := h.kvHeadCount(arch+".attention.head_count_kv", int(blocks), bearing)
	if err != nil {
		return Geometry{}, err
	}
	keyLen, ok := h.Uint(arch + ".attention.key_length")
	if !ok {
		embed, embedErr := h.uintKey(arch + ".embedding_length")
		if embedErr != nil {
			return Geometry{}, embedErr
		}
		heads, headErr := h.uintKey(arch + ".attention.head_count")
		if headErr != nil {
			return Geometry{}, headErr
		}
		if heads == 0 {
			return Geometry{}, fmt.Errorf("gguf: %s.attention.head_count is zero, so key_length cannot be derived", arch)
		}
		keyLen = embed / heads
	}
	return Geometry{KVLayers: kvLayers, HeadCountKV: int(kvHeads), KeyLength: int(keyLen)}, nil
}

// kvLayerSet counts the KV-bearing layers and returns the predicate saying which
// block indexes they are. A sliding_window_pattern marks the sliding layers true,
// so the bearing ones are the false entries. A hybrid architecture instead gives
// only every full_attention_interval-th block a KV cache; which blocks those are
// is not needed, so its predicate admits every block. A declared zero interval is
// read as absent rather than dividing by zero.
func (h Header) kvLayerSet(arch string, blocks int) (int, func(int) bool, error) {
	patKey := arch + ".attention.sliding_window_pattern"
	if pattern, ok := h.Bools(patKey); ok {
		if len(pattern) != blocks {
			return 0, nil, fmt.Errorf("gguf: key %s has %d entries but %s.block_count is %d", patKey, len(pattern), arch, blocks)
		}
		n := 0
		for _, sliding := range pattern {
			if !sliding {
				n++
			}
		}
		return n, func(i int) bool { return !pattern[i] }, nil
	}
	all := func(int) bool { return true }
	if interval, ok := h.Uint(arch + ".full_attention_interval"); ok && interval > 0 {
		return blocks / int(interval), all, nil
	}
	return blocks, all, nil
}

// kvHeadCount reads a KV head count that is either a scalar or a per-layer array.
// An array is read at the KV-bearing layers, which must agree; with no such layer
// there is nothing to read and the count is zero, not an error.
func (h Header) kvHeadCount(key string, blocks int, bearing func(int) bool) (uint64, error) {
	per, ok := h.Uints(key)
	if !ok {
		return h.uintKey(key)
	}
	if len(per) != blocks {
		return 0, fmt.Errorf("gguf: key %s has %d entries but block_count is %d", key, len(per), blocks)
	}
	seen := map[uint64]struct{}{}
	for i, v := range per {
		if bearing(i) {
			seen[v] = struct{}{}
		}
	}
	vals := slices.Sorted(maps.Keys(seen))
	switch len(vals) {
	case 0:
		return 0, nil
	case 1:
		return vals[0], nil
	default:
		return 0, fmt.Errorf("gguf: KV-bearing layers disagree on %s: %v", key, vals)
	}
}

// uintKey is Uint with the three failure modes separated: absent, an unsupported
// per-layer array, or a value that is not a non-negative integer.
func (h Header) uintKey(key string) (uint64, error) {
	v, present := h.kv[key]
	if !present {
		return 0, fmt.Errorf("gguf: missing key %s", key)
	}
	switch v.(type) {
	case arrayVal, []uint64, []bool:
		return 0, fmt.Errorf("gguf: key %s is a per-layer array, which is unsupported", key)
	}
	n, ok := h.Uint(key)
	if !ok {
		return 0, fmt.Errorf("gguf: key %s is not a non-negative integer", key)
	}
	return n, nil
}

// ReadHeader decodes the magic, version, tensor count and the whole metadata
// section from r, then stops. It reads no tensor index and no tensor data.
func ReadHeader(r io.Reader) (Header, error) {
	br := bufio.NewReader(r)

	var magic [4]byte
	if _, err := io.ReadFull(br, magic[:]); err != nil {
		return Header{}, wrapEOF(err)
	}
	if string(magic[:]) != "GGUF" {
		return Header{}, fmt.Errorf("%w: magic is %q", ErrBadMagic, magic[:])
	}

	version, err := readU32(br)
	if err != nil {
		return Header{}, err
	}
	if version != 2 && version != 3 {
		return Header{}, fmt.Errorf("%w: %d (this reader is written against v2 and v3)", ErrUnsupportedVersion, version)
	}

	tensorCount, err := readU64(br)
	if err != nil {
		return Header{}, err
	}
	kvCount, err := readU64(br)
	if err != nil {
		return Header{}, err
	}
	if kvCount > maxKVCount {
		return Header{}, fmt.Errorf("gguf: metadata count %d exceeds the %d-entry cap", kvCount, maxKVCount)
	}

	h := Header{Version: version, TensorCount: tensorCount, kv: make(map[string]any, kvCount)}
	for i := uint64(0); i < kvCount; i++ {
		key, keyErr := readString(br)
		if keyErr != nil {
			return Header{}, keyErr
		}
		typ, typErr := readU32(br)
		if typErr != nil {
			return Header{}, typErr
		}
		v, valErr := readValue(br, typ)
		if valErr != nil {
			return Header{}, fmt.Errorf("gguf: metadata key %s: %w", key, valErr)
		}
		h.kv[key] = v
	}
	return h, nil
}

// readValue decodes one metadata value of the given wire type. Arrays are walked
// element by element and, unless they are a small integer or bool array, discarded,
// which is what keeps a million-token vocabulary out of memory while still leaving
// the reader positioned at the next key.
func readValue(r io.Reader, typ uint32) (any, error) {
	switch typ {
	case typeUint8, typeUint16, typeUint32, typeUint64:
		return readLE(r, scalarWidth(typ))
	case typeBool:
		u, err := readLE(r, 1)
		return u != 0, err
	case typeInt8, typeInt16, typeInt32, typeInt64:
		w := scalarWidth(typ)
		u, err := readLE(r, w)
		if err != nil {
			return nil, err
		}
		shift := uint(64 - 8*w)
		return int64(u<<shift) >> shift, nil
	case typeFloat32:
		u, err := readLE(r, 4)
		if err != nil {
			return nil, err
		}
		return float64(math.Float32frombits(uint32(u))), nil
	case typeFloat64:
		u, err := readLE(r, 8)
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(u), nil
	case typeString:
		return readString(r)
	case typeArray:
		return readArray(r)
	default:
		return nil, fmt.Errorf("gguf: unknown metadata value type %d", typ)
	}
}

// scalarWidth is the byte width of a fixed-size integer wire type.
func scalarWidth(typ uint32) int {
	switch typ {
	case typeUint8, typeInt8:
		return 1
	case typeUint16, typeInt16:
		return 2
	case typeUint32, typeInt32:
		return 4
	default:
		return 8
	}
}

// readArray consumes an array value (element type, count, then every element).
// An integer or bool array of at most maxLayerArray elements is returned as
// []uint64 or []bool; a negative element, any other element type and any larger
// count are consumed and discarded behind an arrayVal marker. Nested arrays
// recurse through readValue.
func readArray(r io.Reader) (any, error) {
	elemType, err := readU32(r)
	if err != nil {
		return nil, err
	}
	count, err := readU64(r)
	if err != nil {
		return nil, err
	}
	if count > maxArrayCount {
		return nil, fmt.Errorf("gguf: array length %d exceeds the %d-element cap", count, maxArrayCount)
	}
	isBool := elemType == typeBool
	keep := count <= maxLayerArray && (isBool || scalarInt(elemType))
	var uints []uint64
	var bools []bool
	if keep && isBool {
		bools = make([]bool, 0, count)
	} else if keep {
		uints = make([]uint64, 0, count)
	}
	for i := uint64(0); i < count; i++ {
		v, elemErr := readValue(r, elemType)
		if elemErr != nil {
			return nil, elemErr
		}
		switch e := v.(type) {
		case bool:
			if keep {
				bools = append(bools, e)
			}
		case uint64:
			if keep {
				uints = append(uints, e)
			}
		case int64:
			if e < 0 {
				keep = false
			} else if keep {
				uints = append(uints, uint64(e))
			}
		}
	}
	switch {
	case !keep:
		return arrayVal{}, nil
	case isBool:
		return bools, nil
	default:
		return uints, nil
	}
}

// scalarInt reports whether typ is one of the fixed-width integer wire types.
func scalarInt(typ uint32) bool {
	switch typ {
	case typeUint8, typeInt8, typeUint16, typeInt16, typeUint32, typeInt32, typeUint64, typeInt64:
		return true
	default:
		return false
	}
}

// readString decodes a u64-prefixed byte string, refusing a declared length beyond
// the cap BEFORE allocating it.
func readString(r io.Reader) (string, error) {
	n, err := readU64(r)
	if err != nil {
		return "", err
	}
	if n > maxStringLen {
		return "", fmt.Errorf("gguf: string length %d exceeds the %d-byte cap", n, maxStringLen)
	}
	b := make([]byte, n)
	if _, readErr := io.ReadFull(r, b); readErr != nil {
		return "", wrapEOF(readErr)
	}
	return string(b), nil
}

// readLE reads width bytes little-endian into a uint64.
func readLE(r io.Reader, width int) (uint64, error) {
	var buf [8]byte
	if _, err := io.ReadFull(r, buf[:width]); err != nil {
		return 0, wrapEOF(err)
	}
	return binary.LittleEndian.Uint64(buf[:]), nil
}

func readU32(r io.Reader) (uint32, error) {
	u, err := readLE(r, 4)
	return uint32(u), err
}

func readU64(r io.Reader) (uint64, error) { return readLE(r, 8) }

// wrapEOF normalizes any end-of-input inside the header to io.ErrUnexpectedEOF: a
// GGUF file that stops mid-header is truncated, and a bare io.EOF would read to a
// caller as a clean finish.
func wrapEOF(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("gguf: header ends early: %w", io.ErrUnexpectedEOF)
	}
	return fmt.Errorf("gguf: read: %w", err)
}
