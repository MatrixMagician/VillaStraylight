// fixture.go builds well-formed GGUF headers for the tests of OTHER packages,
// which cannot reach this package's test-local encoder.
//
// INVARIANT: it emits only VALID headers. The deliberately malformed bytes (a bad
// magic, a truncation, an oversized declared length) are built by hand inside
// gguf_test.go, because a builder that can produce them would have to expose the
// whole wire format to do it. Nothing in the production path calls this.
package gguf

import "encoding/binary"

// FixtureForTest encodes a GGUF v3 header whose metadata is general.architecture
// plus one u64 entry per key. Keys are full metadata names, so a caller writes
// "qwen35moe.block_count" rather than assembling the namespace itself.
func FixtureForTest(arch string, keys map[string]uint64) []byte {
	b := []byte{'G', 'G', 'U', 'F'}
	b = binary.LittleEndian.AppendUint32(b, 3)
	b = binary.LittleEndian.AppendUint64(b, 0)
	b = binary.LittleEndian.AppendUint64(b, uint64(len(keys)+1))

	b = appendFixtureString(b, archKey)
	b = binary.LittleEndian.AppendUint32(b, typeString)
	b = appendFixtureString(b, arch)

	for k, v := range keys {
		b = appendFixtureString(b, k)
		b = binary.LittleEndian.AppendUint32(b, typeUint64)
		b = binary.LittleEndian.AppendUint64(b, v)
	}
	return b
}

// appendFixtureString appends the u64-length-prefixed string encoding.
func appendFixtureString(b []byte, s string) []byte {
	b = binary.LittleEndian.AppendUint64(b, uint64(len(s)))
	return append(b, s...)
}
