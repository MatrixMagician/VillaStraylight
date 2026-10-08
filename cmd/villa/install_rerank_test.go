package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/install"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
)

// TestRerankGGUFFilenameSingleSource: the staged reranker file and the served `-m`
// path bind one symbol, so they cannot drift.
func TestRerankGGUFFilenameSingleSource(t *testing.T) {
	if install.RerankShard.Filename != orchestrate.RerankGGUFFilename() {
		t.Fatalf("rerank GGUF filename drift: RerankShard.Filename = %q, orchestrate.RerankGGUFFilename() = %q",
			install.RerankShard.Filename, orchestrate.RerankGGUFFilename())
	}
}

// TestRerankShardValues pins the verified integrity values of the reranker shard
// (hashed on the dev host, 2026-10-08): a typo would let an unverified GGUF through.
func TestRerankShardValues(t *testing.T) {
	if install.RerankShard.SizeBytes != 635676416 {
		t.Errorf("RerankShard.SizeBytes = %d, want 635676416", install.RerankShard.SizeBytes)
	}
	if install.RerankShard.SHA256 != "a43c7c9b11a4c1517e5bf95151960e1621d1b72f7a493364b01e386cf1aaa1d3" {
		t.Errorf("RerankShard.SHA256 = %q, want a43c7c9b11a4c1517e5bf95151960e1621d1b72f7a493364b01e386cf1aaa1d3", install.RerankShard.SHA256)
	}
	if !strings.HasPrefix(install.RerankShard.URL, "https://huggingface.co/gpustack/bge-reranker-v2-m3-GGUF/") {
		t.Errorf("RerankShard.URL = %q, want the gpustack GGUF repository", install.RerankShard.URL)
	}
}

// TestLiveRerankModelPresentSizeGuard: the reranker's presence check trusts the
// file only at the pinned size, like the embedder's.
func TestLiveRerankModelPresentSizeGuard(t *testing.T) {
	t.Run("absent file is not present", func(t *testing.T) {
		if liveRerankModelPresent(t.TempDir()) {
			t.Error("an absent reranker GGUF must not be reported present")
		}
	})
	t.Run("truncated file is not present", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, install.RerankShard.Filename), []byte("short"), 0o600); err != nil {
			t.Fatal(err)
		}
		if liveRerankModelPresent(dir) {
			t.Error("a truncated reranker GGUF must not be reported present")
		}
	})
	t.Run("file at the pinned size is present", func(t *testing.T) {
		dir := t.TempDir()
		f, err := os.Create(filepath.Join(dir, install.RerankShard.Filename))
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(int64(install.RerankShard.SizeBytes)); err != nil {
			t.Fatal(err)
		}
		f.Close()
		if !liveRerankModelPresent(dir) {
			t.Error("a reranker GGUF at the pinned size must be reported present")
		}
	})
}

// TestEvalMemoryProofCoversTheReranker: with a reranker probe the memory proof
// passes only when the reranker ranks the on-topic document first; an error or a
// wrong ranking fails with a remediation naming the unit; with no probe (reranker
// off) the verdict and detail are what they were.
func TestEvalMemoryProofCoversTheReranker(t *testing.T) {
	embedProbe := func() (int, error) { return 768, nil }
	qdrantProbe := func() (bool, error) { return true, nil }

	cases := []struct {
		name       string
		rerank     func() (int, error)
		wantStatus preflight.Status
		wantDetail string
	}{
		{"reranker off", nil, preflight.StatusPass, "768-dim embeddings + Qdrant writable"},
		{"reranker ranks the on-topic document first", func() (int, error) { return 0, nil }, preflight.StatusPass, "768-dim embeddings + Qdrant writable + reranker ranking"},
		{"reranker ranks the off-topic document first", func() (int, error) { return 1, nil }, preflight.StatusFail, "villa-rerank.service"},
		{"reranker does not answer", func() (int, error) { return 0, errors.New("connection refused") }, preflight.StatusFail, "villa-rerank.service"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evalMemoryProof(t.Context(), embedProbe, qdrantProbe, tc.rerank, nil, 768)
			if got.status != tc.wantStatus {
				t.Errorf("status = %v, want %v (detail %q)", got.status, tc.wantStatus, got.detail)
			}
			if !strings.Contains(got.detail, tc.wantDetail) {
				t.Errorf("detail = %q, want it to contain %q", got.detail, tc.wantDetail)
			}
		})
	}
}

// TestRerankProbeReadsTheTopIndex: the live probe picks the index with the highest
// relevance score, which llama-server reports as a raw logit that may be negative,
// so "highest" is not "positive"; an answer with no results is an error, never
// index 0.
func TestRerankProbeReadsTheTopIndex(t *testing.T) {
	body := []byte(`{"model":"bge-reranker-v2-m3","results":[{"index":1,"relevance_score":-7.5},{"index":0,"relevance_score":-1.2}]}`)
	scores, err := parseRerankScores(body, 2)
	if err != nil {
		t.Fatalf("parseRerankScores: %v", err)
	}
	if top := topScore(scores); top != 0 {
		t.Errorf("topScore(%v) = %d, want 0", scores, top)
	}
	if _, err := parseRerankScores([]byte(`{"results":[]}`), 2); err == nil {
		t.Error("an empty results list must be an error, never index 0")
	}
}
