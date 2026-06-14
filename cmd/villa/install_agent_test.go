package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// install_agent_test.go covers the pure resolution + presence seams of the v1.4
// coding-agent install addon (Task 2): coderShardFor (catalog-resolved, no literal —
// D-02/D-04) and liveCoderModelPresent (size-gated integrity, mirroring the embed
// analog). The download/binary-install/readiness wiring is host-touching and is
// exercised through the install flow tests (install_test.go).

// TestCoderShard asserts coderShardFor resolves the picked coder entry's Shards[0] by id
// (D-02) and returns (zero, false) when no coder fits or the entry/shard is absent — it
// is NOT a hard-coded literal (the explicit anti-pattern the memory analog's nomicEmbedShard
// would be for coder).
func TestCoderShard(t *testing.T) {
	coderShard := catalog.Shard{
		URL:       "https://huggingface.co/example/Qwen3-Coder-GGUF/resolve/main/qwen3-coder.Q4.gguf",
		Filename:  "qwen3-coder.Q4.gguf",
		SHA256:    "abc123",
		SizeBytes: 18_000_000_000,
	}
	cat := catalog.Catalog{Models: []catalog.CatalogModel{
		{ID: "qwen3-chat-30b"}, // a chat entry with no shards — must be skipped
		{ID: "qwen3-coder-30b-a3b", Role: "coder", Shards: []catalog.Shard{coderShard}},
		{ID: "qwen3-coder-no-shard", Role: "coder"}, // coder but no shards — must not resolve
	}}

	t.Run("resolves the picked entry's Shards[0]", func(t *testing.T) {
		rec := recommend.Recommendation{Coder: recommend.CoderFit{Model: "qwen3-coder-30b-a3b"}}
		got, ok := coderShardFor(rec, cat)
		if !ok {
			t.Fatal("coderShardFor returned ok=false for a present coder entry, want true")
		}
		if got != coderShard {
			t.Errorf("coderShardFor = %+v, want the picked entry's Shards[0] %+v", got, coderShard)
		}
	})

	t.Run("no coder fit (empty id) → false", func(t *testing.T) {
		rec := recommend.Recommendation{Coder: recommend.CoderFit{Model: ""}}
		if _, ok := coderShardFor(rec, cat); ok {
			t.Error("coderShardFor returned ok=true for an empty coder model id, want false")
		}
	})

	t.Run("picked id absent from catalog → false", func(t *testing.T) {
		rec := recommend.Recommendation{Coder: recommend.CoderFit{Model: "not-in-catalog"}}
		if _, ok := coderShardFor(rec, cat); ok {
			t.Error("coderShardFor returned ok=true for an id absent from the catalog, want false")
		}
	})

	t.Run("entry present but no shards → false", func(t *testing.T) {
		rec := recommend.Recommendation{Coder: recommend.CoderFit{Model: "qwen3-coder-no-shard"}}
		if _, ok := coderShardFor(rec, cat); ok {
			t.Error("coderShardFor returned ok=true for an entry with no shards, want false")
		}
	})
}

// TestCoderModelPresent asserts liveCoderModelPresent treats the staged GGUF as present
// only when it exists AND its on-disk size matches the resolved shard's SizeBytes — a
// truncated/tampered file is NOT trusted (returns false → re-pull + re-verify), the same
// integrity guard as liveEmbedModelPresent (IN-03).
func TestCoderModelPresent(t *testing.T) {
	sh := catalog.Shard{Filename: "qwen3-coder.Q4.gguf", SizeBytes: 1024}

	t.Run("absent file is not present", func(t *testing.T) {
		dir := t.TempDir()
		if liveCoderModelPresent(dir, sh) {
			t.Error("an absent coder GGUF must not be reported present")
		}
	})

	t.Run("size-matched file is present", func(t *testing.T) {
		dir := t.TempDir()
		writeFileOfSize(t, filepath.Join(dir, sh.Filename), int(sh.SizeBytes))
		if !liveCoderModelPresent(dir, sh) {
			t.Error("a size-matched coder GGUF must be reported present")
		}
	})

	t.Run("truncated file is not present (integrity guard)", func(t *testing.T) {
		dir := t.TempDir()
		writeFileOfSize(t, filepath.Join(dir, sh.Filename), int(sh.SizeBytes)-1)
		if liveCoderModelPresent(dir, sh) {
			t.Error("a truncated coder GGUF must NOT be reported present (re-pull + re-verify)")
		}
	})
}

// writeFileOfSize writes a file of exactly n zero bytes at path (test helper).
func writeFileOfSize(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, n), 0o600); err != nil {
		t.Fatalf("write fixture %q: %v", path, err)
	}
}
