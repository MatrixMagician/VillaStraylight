package gguf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"strings"
	"testing"
)

// fixtureTensorCount is the tensor_count every fixture advertises. It is non-zero
// so a reader that never decodes the field cannot pass by returning the Go zero.
const fixtureTensorCount = 7

// kvPair is one already-encoded metadata entry: the key, its wire type, and the
// raw little-endian payload the value occupies. Building the payload by hand is
// what lets a test emit a nested array or an oversized length the encoders would
// refuse to produce.
type kvPair struct {
	key     string
	typ     uint32
	payload []byte
}

// appendString appends the GGUF string encoding (u64 length + raw bytes) to b.
func appendString(b []byte, s string) []byte {
	b = binary.LittleEndian.AppendUint64(b, uint64(len(s)))
	return append(b, s...)
}

func kvStr(key, v string) kvPair {
	return kvPair{key: key, typ: typeString, payload: appendString(nil, v)}
}

func kvU32(key string, v uint32) kvPair {
	return kvPair{key: key, typ: typeUint32, payload: binary.LittleEndian.AppendUint32(nil, v)}
}

func kvU64(key string, v uint64) kvPair {
	return kvPair{key: key, typ: typeUint64, payload: binary.LittleEndian.AppendUint64(nil, v)}
}

// kvArray encodes an array value from an already-encoded element blob, so a test
// can nest arrays or mix element types the helpers do not cover.
func kvArray(key string, elemType uint32, count int, elems []byte) kvPair {
	p := binary.LittleEndian.AppendUint32(nil, elemType)
	p = binary.LittleEndian.AppendUint64(p, uint64(count))
	return kvPair{key: key, typ: typeArray, payload: append(p, elems...)}
}

// encodeArray returns the payload of an array value (element type, count, elements)
// without the surrounding key/type, for building a nested array.
func encodeArray(elemType uint32, count int, elems []byte) []byte {
	p := binary.LittleEndian.AppendUint32(nil, elemType)
	p = binary.LittleEndian.AppendUint64(p, uint64(count))
	return append(p, elems...)
}

// fixture builds a complete GGUF v3 header + KV section from kv.
func fixture(kv ...kvPair) []byte { return fixtureVersion(3, kv...) }

// fixtureVersion is fixture at an explicit format version.
func fixtureVersion(version uint32, kv ...kvPair) []byte {
	b := []byte{'G', 'G', 'U', 'F'}
	b = binary.LittleEndian.AppendUint32(b, version)
	b = binary.LittleEndian.AppendUint64(b, fixtureTensorCount)
	b = binary.LittleEndian.AppendUint64(b, uint64(len(kv)))
	for _, p := range kv {
		b = appendString(b, p.key)
		b = binary.LittleEndian.AppendUint32(b, p.typ)
		b = append(b, p.payload...)
	}
	return b
}

// TestGeometryHybridCountsAttentionLayers guards the promise that a hybrid
// architecture reports its KV-BEARING layer count, not its block count: only every
// full_attention_interval-th block holds a per-token KV cache, and counting blocks
// would overstate the KV term by that factor.
func TestGeometryHybridCountsAttentionLayers(t *testing.T) {
	kv := []kvPair{
		kvStr("general.architecture", "qwen35moe"),
		kvU32("qwen35moe.block_count", 40),
		kvU32("qwen35moe.full_attention_interval", 4),
		kvU32("qwen35moe.attention.head_count_kv", 2),
		kvU32("qwen35moe.attention.key_length", 256),
	}
	h, err := ReadHeader(bytes.NewReader(fixture(kv...)))
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	g, err := h.Geometry()
	if err != nil {
		t.Fatalf("Geometry: %v", err)
	}
	want := Geometry{KVLayers: 10, HeadCountKV: 2, KeyLength: 256}
	if g != want {
		t.Errorf("Geometry() = %+v, want %+v (40 blocks / interval 4)", g, want)
	}
}

// TestGeometryZeroIntervalCountsEveryBlock guards the promise that a zero
// full_attention_interval means every block is attention-bearing, rather than
// dividing by zero.
func TestGeometryZeroIntervalCountsEveryBlock(t *testing.T) {
	kv := append(llamaKV(), kvU32("llama.full_attention_interval", 0))
	h, err := ReadHeader(bytes.NewReader(fixture(kv...)))
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	g, err := h.Geometry()
	if err != nil {
		t.Fatalf("Geometry: %v", err)
	}
	if g.KVLayers != 48 {
		t.Errorf("KVLayers = %d, want 48", g.KVLayers)
	}
}

// llamaKV is the minimal qualifying metadata set: a dense architecture (every
// block attention-bearing) plus the three geometry keys the fit consumes.
func llamaKV() []kvPair {
	return []kvPair{
		kvStr("general.architecture", "llama"),
		kvU32("llama.block_count", 48),
		kvU32("llama.attention.head_count_kv", 4),
		kvU32("llama.attention.key_length", 128),
	}
}

// TestReadHeaderGeometry guards the promise that a well-formed header yields the
// architecture, the tensor count, and the three geometry values verbatim.
func TestReadHeaderGeometry(t *testing.T) {
	h, err := ReadHeader(bytes.NewReader(fixture(llamaKV()...)))
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if h.Version != 3 {
		t.Errorf("Version = %d, want 3", h.Version)
	}
	if h.TensorCount != fixtureTensorCount {
		t.Errorf("TensorCount = %d, want %d", h.TensorCount, fixtureTensorCount)
	}
	if h.Arch() != "llama" {
		t.Errorf("Arch() = %q, want %q", h.Arch(), "llama")
	}
	g, err := h.Geometry()
	if err != nil {
		t.Fatalf("Geometry: %v", err)
	}
	want := Geometry{KVLayers: 48, HeadCountKV: 4, KeyLength: 128}
	if g != want {
		t.Errorf("Geometry() = %+v, want %+v", g, want)
	}
}

// TestGeometryKeyLengthFallback guards the promise that an entry without an
// explicit key_length derives it from embedding_length / head_count rather than
// reporting a missing key.
func TestGeometryKeyLengthFallback(t *testing.T) {
	kv := []kvPair{
		kvStr("general.architecture", "llama"),
		kvU32("llama.block_count", 48),
		kvU32("llama.attention.head_count_kv", 4),
		kvU32("llama.embedding_length", 4096),
		kvU32("llama.attention.head_count", 32),
	}
	h, err := ReadHeader(bytes.NewReader(fixture(kv...)))
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	g, err := h.Geometry()
	if err != nil {
		t.Fatalf("Geometry: %v", err)
	}
	if g.KeyLength != 128 {
		t.Errorf("KeyLength = %d, want 128 (4096/32)", g.KeyLength)
	}
}

// TestGeometryMissingKeyNamesIt guards the promise that an absent geometry key is
// an error naming the key, which is what the caller degrades to a typed-Unknown
// WARN rather than a confident mismatch.
func TestGeometryMissingKeyNamesIt(t *testing.T) {
	kv := []kvPair{
		kvStr("general.architecture", "llama"),
		kvU32("llama.attention.head_count_kv", 4),
		kvU32("llama.attention.key_length", 128),
	}
	h, err := ReadHeader(bytes.NewReader(fixture(kv...)))
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if _, gErr := h.Geometry(); gErr == nil {
		t.Fatal("expected an error for a missing block_count, got nil")
	} else if !strings.Contains(gErr.Error(), "llama.block_count") {
		t.Errorf("error %q does not name the missing key", gErr)
	}
}

// kvU32Array encodes an array of u32 elements.
func kvU32Array(key string, vals ...uint32) kvPair {
	var elems []byte
	for _, v := range vals {
		elems = binary.LittleEndian.AppendUint32(elems, v)
	}
	return kvArray(key, typeUint32, len(vals), elems)
}

// kvBoolArray encodes an array of bool elements.
func kvBoolArray(key string, vals ...bool) kvPair {
	elems := make([]byte, len(vals))
	for i, v := range vals {
		if v {
			elems[i] = 1
		}
	}
	return kvArray(key, typeBool, len(vals), elems)
}

// interleave repeats group n times: the shape of a sliding-window pattern or a
// per-layer KV head count.
func interleave[T any](n int, group ...T) []T {
	var out []T
	for range n {
		out = append(out, group...)
	}
	return out
}

func gemma4KV() []kvPair {
	heads := interleave[uint32](10, 16, 16, 16, 16, 16, 4)
	pattern := interleave(10, true, true, true, true, true, false)
	return []kvPair{
		kvStr("general.architecture", "gemma4"),
		kvU32("gemma4.block_count", 60),
		kvU32("gemma4.attention.head_count", 32),
		kvU32Array("gemma4.attention.head_count_kv", heads...),
		kvU32("gemma4.attention.key_length", 512),
		kvU32("gemma4.attention.key_length_swa", 256),
		kvU32("gemma4.attention.sliding_window", 1024),
		kvBoolArray("gemma4.attention.sliding_window_pattern", pattern...),
	}
}

func museKV() []kvPair {
	return []kvPair{
		kvStr("general.architecture", "muse-glimmer"),
		kvU32("muse-glimmer.block_count", 52),
		kvU32("muse-glimmer.attention.head_count_kv", 2),
		kvU32("muse-glimmer.attention.key_length", 128),
		kvU32("muse-glimmer.attention.sliding_window", 2048),
		kvBoolArray("muse-glimmer.attention.sliding_window_pattern", interleave(13, true, true, true, false)...),
	}
}

// replaceKV returns kv with the entry named key swapped for p.
func replaceKV(kv []kvPair, p kvPair) []kvPair {
	out := make([]kvPair, len(kv))
	copy(out, kv)
	for i := range out {
		if out[i].key == p.key {
			out[i] = p
		}
	}
	return out
}

// dropKV returns kv without the entry named key.
func dropKV(kv []kvPair, key string) []kvPair {
	var out []kvPair
	for _, p := range kv {
		if p.key != key {
			out = append(out, p)
		}
	}
	return out
}

// TestGeometrySlidingWindow guards the promise that a sliding-window layer is not
// a KV-bearing layer, because llama.cpp bounds its cache at the window: only the
// layers the pattern marks false grow with the context, and a per-layer
// head_count_kv is read at exactly those layers.
func TestGeometrySlidingWindow(t *testing.T) {
	cases := []struct {
		name string
		kv   []kvPair
		want Geometry
	}{
		{
			"gemma4 per-layer head_count_kv",
			gemma4KV(),
			Geometry{
				KVLayers: 10, HeadCountKV: 4, KeyLength: 512,
				SWALayers: 50, SWAHeadCountKV: 16, SWAKeyLength: 256, SWAWindow: 1024,
			},
		},
		{
			"muse-glimmer scalar head_count_kv, key_length_swa absent",
			museKV(),
			Geometry{
				KVLayers: 13, HeadCountKV: 2, KeyLength: 128,
				SWALayers: 39, SWAHeadCountKV: 2, SWAKeyLength: 128, SWAWindow: 2048,
			},
		},
		{"dense scalar, no pattern", llamaKV(), Geometry{KVLayers: 48, HeadCountKV: 4, KeyLength: 128}},
		{
			"all sliding with scalar head_count_kv",
			replaceKV(museKV(), kvBoolArray("muse-glimmer.attention.sliding_window_pattern", interleave(52, true)...)),
			Geometry{
				KVLayers: 0, HeadCountKV: 2, KeyLength: 128,
				SWALayers: 52, SWAHeadCountKV: 2, SWAKeyLength: 128, SWAWindow: 2048,
			},
		},
		{
			"all sliding with array head_count_kv has nothing to read at the global set",
			replaceKV(
				replaceKV(gemma4KV(), kvBoolArray("gemma4.attention.sliding_window_pattern", interleave(60, true)...)),
				kvU32Array("gemma4.attention.head_count_kv", interleave[uint32](60, 16)...)),
			Geometry{
				KVLayers: 0, HeadCountKV: 0, KeyLength: 512,
				SWALayers: 60, SWAHeadCountKV: 16, SWAKeyLength: 256, SWAWindow: 1024,
			},
		},
		{
			"interval with array head_count_kv uniform over all blocks",
			[]kvPair{
				kvStr("general.architecture", "hyb"),
				kvU32("hyb.block_count", 8),
				kvU32("hyb.full_attention_interval", 4),
				kvU32Array("hyb.attention.head_count_kv", interleave[uint32](8, 2)...),
				kvU32("hyb.attention.key_length", 64),
			},
			Geometry{KVLayers: 2, HeadCountKV: 2, KeyLength: 64},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, err := ReadHeader(bytes.NewReader(fixture(tc.kv...)))
			if err != nil {
				t.Fatalf("ReadHeader: %v", err)
			}
			g, err := h.Geometry()
			if err != nil {
				t.Fatalf("Geometry: %v", err)
			}
			if g != tc.want {
				t.Errorf("Geometry() = %+v, want %+v", g, tc.want)
			}
		})
	}
}

// TestGeometrySlidingWindowRefusals guards the promise that a pattern or a
// per-layer head_count_kv that does not line up with the block count, or whose
// KV-bearing layers disagree, is an error naming what is wrong rather than a
// guessed geometry.
func TestGeometrySlidingWindowRefusals(t *testing.T) {
	gemmaDisagree := interleave[uint32](10, 16, 16, 16, 16, 16, 4)
	gemmaDisagree[11] = 8
	gemmaSlidingDisagree := interleave[uint32](10, 16, 16, 16, 16, 16, 4)
	gemmaSlidingDisagree[0] = 8
	cases := []struct {
		name string
		kv   []kvPair
		want []string
	}{
		{
			"pattern length differs from block_count",
			replaceKV(museKV(), kvBoolArray("muse-glimmer.attention.sliding_window_pattern", interleave(10, true, false)...)),
			[]string{"muse-glimmer.attention.sliding_window_pattern", "20", "52"},
		},
		{
			"global layers disagree on head_count_kv",
			replaceKV(gemma4KV(), kvU32Array("gemma4.attention.head_count_kv", gemmaDisagree...)),
			[]string{"disagree", "[4 8]"},
		},
		{
			"sliding layers disagree on head_count_kv",
			replaceKV(gemma4KV(), kvU32Array("gemma4.attention.head_count_kv", gemmaSlidingDisagree...)),
			[]string{"disagree", "[8 16]"},
		},
		{
			"a pattern without sliding_window cannot bound the cache",
			dropKV(gemma4KV(), "gemma4.attention.sliding_window"),
			[]string{"gemma4.attention.sliding_window"},
		},
		{
			"array head_count_kv length differs from block_count",
			replaceKV(gemma4KV(), kvU32Array("gemma4.attention.head_count_kv", 4, 8)),
			[]string{"gemma4.attention.head_count_kv", "2", "60"},
		},
		{
			"array head_count_kv without a pattern is non-uniform",
			[]kvPair{
				kvStr("general.architecture", "llama"),
				kvU32("llama.block_count", 4),
				kvU32Array("llama.attention.head_count_kv", 4, 8, 4, 8),
				kvU32("llama.attention.key_length", 128),
			},
			[]string{"disagree", "[4 8]"},
		},
		{
			"other keys still refuse a per-layer array",
			replaceKV(llamaKV(), kvU32Array("llama.block_count", 48)),
			[]string{"llama.block_count", "unsupported"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, err := ReadHeader(bytes.NewReader(fixture(tc.kv...)))
			if err != nil {
				t.Fatalf("ReadHeader: %v", err)
			}
			_, gErr := h.Geometry()
			if gErr == nil {
				t.Fatal("expected an error, got nil")
			}
			for _, w := range tc.want {
				if !strings.Contains(gErr.Error(), w) {
					t.Errorf("error %q does not contain %q", gErr, w)
				}
			}
		})
	}
}

// TestReadHeaderRetainsSmallLayerArrays guards the promise that an integer or
// bool array up to maxLayerArray is retained for Uints and Bools, while a larger
// one, a negative-element one and a string one are discarded and the keys after
// them still decode.
func TestReadHeaderRetainsSmallLayerArrays(t *testing.T) {
	big := make([]byte, (maxLayerArray+1)*4)
	neg := binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, 1), ^uint32(0))
	strs := appendString(appendString(nil, "a"), "b")
	kv := append([]kvPair{
		kvU32Array("n.ints", 3, 5, 7),
		kvBoolArray("n.bools", true, false),
		kvArray("n.big", typeUint32, maxLayerArray+1, big),
		kvArray("n.neg", typeInt32, 2, neg),
		kvArray("n.strs", typeString, 2, strs),
	}, llamaKV()...)
	h, err := ReadHeader(bytes.NewReader(fixture(kv...)))
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if got, ok := h.Uints("n.ints"); !ok || len(got) != 3 || got[0] != 3 || got[2] != 7 {
		t.Errorf("Uints(n.ints) = %v, %v; want [3 5 7], true", got, ok)
	}
	if got, ok := h.Bools("n.bools"); !ok || len(got) != 2 || !got[0] || got[1] {
		t.Errorf("Bools(n.bools) = %v, %v; want [true false], true", got, ok)
	}
	for _, key := range []string{"n.big", "n.neg", "n.strs"} {
		if _, ok := h.Uints(key); ok {
			t.Errorf("Uints(%s) retained an array it must discard", key)
		}
		if _, ok := h.Bools(key); ok {
			t.Errorf("Bools(%s) retained an array it must discard", key)
		}
	}
	if _, ok := h.Uints("n.bools"); ok {
		t.Error("Uints returned a bool array")
	}
	if g, gErr := h.Geometry(); gErr != nil || g.KVLayers != 48 {
		t.Errorf("Geometry after arrays = %+v, %v; want 48 KV layers", g, gErr)
	}
}

// TestReadHeaderBadMagic guards the promise that a non-GGUF file is refused with
// the ErrBadMagic sentinel rather than parsed as garbage.
func TestReadHeaderBadMagic(t *testing.T) {
	b := fixture(llamaKV()...)
	b[0] = 'X'
	_, err := ReadHeader(bytes.NewReader(b))
	if !errors.Is(err, ErrBadMagic) {
		t.Fatalf("ReadHeader err = %v, want ErrBadMagic", err)
	}
}

// TestReadHeaderVersionWindow guards the promise that only GGUF v2 and v3 are
// accepted: an older or newer container is refused with ErrUnsupportedVersion, so
// a format villa has not read is never guessed at.
func TestReadHeaderVersionWindow(t *testing.T) {
	for _, v := range []uint32{1, 4} {
		_, err := ReadHeader(bytes.NewReader(fixtureVersion(v, llamaKV()...)))
		if !errors.Is(err, ErrUnsupportedVersion) {
			t.Errorf("version %d: err = %v, want ErrUnsupportedVersion", v, err)
		}
	}
	for _, v := range []uint32{2, 3} {
		if _, err := ReadHeader(bytes.NewReader(fixtureVersion(v, llamaKV()...))); err != nil {
			t.Errorf("version %d: unexpected error %v", v, err)
		}
	}
}

// TestReadHeaderTruncated guards the promise that a file that ends inside the KV
// section is an error wrapping io.ErrUnexpectedEOF, never a partially populated
// Header the caller could mistake for a complete one.
func TestReadHeaderTruncated(t *testing.T) {
	b := []byte{'G', 'G', 'U', 'F'}
	b = binary.LittleEndian.AppendUint32(b, 3)
	b = binary.LittleEndian.AppendUint64(b, fixtureTensorCount)
	b = binary.LittleEndian.AppendUint64(b, 1)
	_, err := ReadHeader(bytes.NewReader(b))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("ReadHeader err = %v, want it to wrap io.ErrUnexpectedEOF", err)
	}
}

// TestReadHeaderSkipsArrays guards the promise that array values (including the
// token vocabulary and a nested array) are parsed for their length and discarded,
// so the keys after them still decode and no array contents are retained.
func TestReadHeaderSkipsArrays(t *testing.T) {
	strs := appendString(nil, "alpha")
	strs = appendString(strs, "beta")
	nested := encodeArray(typeUint32, 2, binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, 1), 2))
	nested = append(nested, encodeArray(typeUint32, 1, binary.LittleEndian.AppendUint32(nil, 3))...)

	kv := append([]kvPair{
		kvArray("tokenizer.ggml.tokens", typeString, 2, strs),
		kvArray("llama.nested", typeArray, 2, nested),
	}, llamaKV()...)

	h, err := ReadHeader(bytes.NewReader(fixture(kv...)))
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if g, gErr := h.Geometry(); gErr != nil || g.KVLayers != 48 {
		t.Fatalf("Geometry after arrays = %+v, %v; want 48 KV layers and no error", g, gErr)
	}
	if _, ok := h.String("tokenizer.ggml.tokens"); ok {
		t.Error("array contents were retained; the reader must discard them")
	}
}

// TestReadHeaderStringCap guards the promise that a declared string length beyond
// the sanity cap is refused by name, so a corrupt length cannot drive an
// allocation the size of the file.
func TestReadHeaderStringCap(t *testing.T) {
	b := []byte{'G', 'G', 'U', 'F'}
	b = binary.LittleEndian.AppendUint32(b, 3)
	b = binary.LittleEndian.AppendUint64(b, fixtureTensorCount)
	b = binary.LittleEndian.AppendUint64(b, 1)
	b = binary.LittleEndian.AppendUint64(b, maxStringLen+1)
	_, err := ReadHeader(bytes.NewReader(b))
	if err == nil {
		t.Fatal("expected a cap error for an oversized string length, got nil")
	}
	if !strings.Contains(err.Error(), "cap") {
		t.Errorf("error %q does not name the cap it enforced", err)
	}
}

// TestHeaderUintAcceptsEveryIntegerWidth guards the promise that Uint reads any
// GGUF integer type, since the same logical key is encoded as u32 by one
// converter and u64 by another.
func TestHeaderUintAcceptsEveryIntegerWidth(t *testing.T) {
	i32 := kvPair{key: "n.signed", typ: typeInt32, payload: binary.LittleEndian.AppendUint32(nil, 17)}
	neg := kvPair{key: "n.negative", typ: typeInt32, payload: binary.LittleEndian.AppendUint32(nil, ^uint32(0))}
	h, err := ReadHeader(bytes.NewReader(fixture(kvU32("n.u32", 5), kvU64("n.u64", 6), i32, neg)))
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	for _, tc := range []struct {
		key  string
		want uint64
	}{{"n.u32", 5}, {"n.u64", 6}, {"n.signed", 17}} {
		got, ok := h.Uint(tc.key)
		if !ok || got != tc.want {
			t.Errorf("Uint(%q) = %d, %v; want %d, true", tc.key, got, ok, tc.want)
		}
	}
	if _, ok := h.Uint("n.negative"); ok {
		t.Error("Uint accepted a negative signed value; it must not")
	}
}

// TestReadValueBranches exercises readValue's switch directly for the wire
// types no ReadHeader-level test reaches: bool, float32, float64, a truncated
// read failing mid-decode inside the signed-integer group, and an unknown
// type code. Coverage was 42.9% before this test (untrusted model-file bytes
// choose typ), so every branch below was previously unexercised.
func TestReadValueBranches(t *testing.T) {
	f32 := binary.LittleEndian.AppendUint32(nil, math.Float32bits(3.5))
	f64 := binary.LittleEndian.AppendUint64(nil, math.Float64bits(-2.25))

	cases := []struct {
		name    string
		typ     uint32
		payload []byte
		want    any
		wantErr bool
	}{
		{"bool true", typeBool, []byte{1}, true, false},
		{"bool false", typeBool, []byte{0}, false, false},
		{"bool truncated", typeBool, []byte{}, nil, true},
		{"float32", typeFloat32, f32, float64(float32(3.5)), false},
		{"float32 truncated", typeFloat32, f32[:2], nil, true},
		{"float64", typeFloat64, f64, -2.25, false},
		{"float64 truncated", typeFloat64, f64[:4], nil, true},
		{"signed integer truncated", typeInt8, []byte{}, nil, true},
		{"unknown wire type", 99, nil, nil, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readValue(bytes.NewReader(tc.payload), tc.typ)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("readValue(type=%d) = %v, nil; want an error", tc.typ, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("readValue(type=%d): unexpected error %v", tc.typ, err)
			}
			if got != tc.want {
				t.Errorf("readValue(type=%d) = %v, want %v", tc.typ, got, tc.want)
			}
		})
	}
}
