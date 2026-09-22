package gguf

import (
	"bytes"
	"testing"
)

// FuzzReadHeader feeds arbitrary bytes to ReadHeader, the entry point that
// reads an untrusted, possibly-hostile downloaded model file. The invariant
// is not "parses correctly" (most fuzzed input is not a GGUF file at all) but
// "never panics, and a successful parse is safe to query further": every
// input either returns an error or a Header whose Geometry() call also
// completes without panicking, since Geometry is the first thing a real
// caller (internal/detect's catalog cross-check) does with the result.
func FuzzReadHeader(f *testing.F) {
	f.Add(fixture(llamaKV()...))
	f.Add(fixtureVersion(2, llamaKV()...))

	badMagic := fixture(llamaKV()...)
	badMagic[0] = 'X'
	f.Add(badMagic)

	truncated := fixture(llamaKV()...)
	f.Add(truncated[:len(truncated)/2])

	strs := appendString(nil, "alpha")
	strs = appendString(strs, "beta")
	f.Add(fixture(append([]kvPair{
		kvArray("tokenizer.ggml.tokens", typeString, 2, strs),
	}, llamaKV()...)...))

	f.Fuzz(func(t *testing.T, data []byte) {
		h, err := ReadHeader(bytes.NewReader(data))
		if err != nil {
			return
		}
		// Reached only on a successful parse: every subsequent accessor must
		// stay panic-free regardless of what the fuzzer put in the KV map.
		_, _ = h.Geometry()
		_ = h.Arch()
	})
}
