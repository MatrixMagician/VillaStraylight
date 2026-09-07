package gguf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
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

// TestGeometryPerLayerArrayUnsupported guards the promise that a per-layer
// head_count_kv array is refused as unsupported rather than silently read as
// missing (which would degrade to WARN and hide a real geometry the fit cannot use).
func TestGeometryPerLayerArrayUnsupported(t *testing.T) {
	elems := binary.LittleEndian.AppendUint32(nil, 4)
	elems = binary.LittleEndian.AppendUint32(elems, 8)
	kv := []kvPair{
		kvStr("general.architecture", "llama"),
		kvU32("llama.block_count", 48),
		kvArray("llama.attention.head_count_kv", typeUint32, 2, elems),
		kvU32("llama.attention.key_length", 128),
	}
	h, err := ReadHeader(bytes.NewReader(fixture(kv...)))
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	_, gErr := h.Geometry()
	if gErr == nil {
		t.Fatal("expected an error for a per-layer head_count_kv array, got nil")
	}
	if !strings.Contains(gErr.Error(), "unsupported") {
		t.Errorf("error %q does not say the shape is unsupported", gErr)
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
