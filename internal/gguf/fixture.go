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
	return FixtureWithArraysForTest(arch, keys, nil, nil)
}

// FixtureWithArraysForTest is FixtureForTest plus per-layer arrays: uintArrays are
// encoded as u32 arrays and boolArrays as bool arrays, the shapes a sliding-window
// architecture carries for head_count_kv and sliding_window_pattern.
func FixtureWithArraysForTest(arch string, keys map[string]uint64, uintArrays map[string][]uint64, boolArrays map[string][]bool) []byte {
	b := []byte{'G', 'G', 'U', 'F'}
	b = binary.LittleEndian.AppendUint32(b, 3)
	b = binary.LittleEndian.AppendUint64(b, 0)
	b = binary.LittleEndian.AppendUint64(b, uint64(len(keys)+len(uintArrays)+len(boolArrays)+1))

	b = appendFixtureString(b, archKey)
	b = binary.LittleEndian.AppendUint32(b, typeString)
	b = appendFixtureString(b, arch)

	for k, v := range keys {
		b = appendFixtureString(b, k)
		b = binary.LittleEndian.AppendUint32(b, typeUint64)
		b = binary.LittleEndian.AppendUint64(b, v)
	}
	for k, vals := range uintArrays {
		b = appendFixtureString(b, k)
		b = binary.LittleEndian.AppendUint32(b, typeArray)
		b = binary.LittleEndian.AppendUint32(b, typeUint32)
		b = binary.LittleEndian.AppendUint64(b, uint64(len(vals)))
		for _, v := range vals {
			b = binary.LittleEndian.AppendUint32(b, uint32(v))
		}
	}
	for k, vals := range boolArrays {
		b = appendFixtureString(b, k)
		b = binary.LittleEndian.AppendUint32(b, typeArray)
		b = binary.LittleEndian.AppendUint32(b, typeBool)
		b = binary.LittleEndian.AppendUint64(b, uint64(len(vals)))
		for _, v := range vals {
			if v {
				b = append(b, 1)
			} else {
				b = append(b, 0)
			}
		}
	}
	return b
}

// appendFixtureString appends the u64-length-prefixed string encoding.
func appendFixtureString(b []byte, s string) []byte {
	b = binary.LittleEndian.AppendUint64(b, uint64(len(s)))
	return append(b, s...)
}
