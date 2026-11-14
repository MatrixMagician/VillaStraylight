package catalogadd

import (
	"errors"
	"strings"
	"testing"
)

const commit = "4c22107061e656fb2a87a3ec2491bb61975eb581"

// TestParseSourceAccepts pins the ONE accepted shape (ADR-0041) and what it
// canonicalises to: the commit-pinned Hugging Face resolve URL of a single GGUF.
func TestParseSourceAccepts(t *testing.T) {
	canonical := "https://huggingface.co/Youssofal/Heretic-GGUF/resolve/" + commit + "/Q6_K/Heretic-Q6_K.gguf"
	tests := []struct {
		name, raw string
		want      Source
	}{
		{
			"nested path", canonical,
			Source{URL: canonical, Repo: "Youssofal/Heretic-GGUF", Commit: commit, Path: "Q6_K/Heretic-Q6_K.gguf", Filename: "Heretic-Q6_K.gguf"},
		},
		{
			"top-level file, host in capitals, surrounding space",
			"  https://HuggingFace.co/org/repo/resolve/" + commit + "/m.gguf\n",
			Source{URL: "https://huggingface.co/org/repo/resolve/" + commit + "/m.gguf", Repo: "org/repo", Commit: commit, Path: "m.gguf", Filename: "m.gguf"},
		},
		{
			"plus and dots in the names",
			"https://huggingface.co/a.b/c_d/resolve/" + commit + "/x+y.v1.gguf",
			Source{URL: "https://huggingface.co/a.b/c_d/resolve/" + commit + "/x+y.v1.gguf", Repo: "a.b/c_d", Commit: commit, Path: "x+y.v1.gguf", Filename: "x+y.v1.gguf"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSource(tt.raw)
			if err != nil {
				t.Fatalf("ParseSource: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ParseSource = %+v\nwant          %+v", got, tt.want)
			}
		})
	}
}

// TestParseSourceRefusesAMovingTarget is the headline refusal: a URL not pinned to a
// commit names whatever the branch points at today, so the recorded sha256 would be
// of a file no other host can reproduce. Every ref that is not 40 lowercase hex
// refuses, and the reason says what to do.
func TestParseSourceRefusesAMovingTarget(t *testing.T) {
	for _, ref := range []string{"main", "master", "v1.0", "pr", "4c22107", strings.ToUpper(commit), commit + "0", commit[:39], "HEAD"} {
		t.Run(ref, func(t *testing.T) {
			_, err := ParseSource("https://huggingface.co/org/repo/resolve/" + ref + "/m.gguf")
			if !errors.Is(err, ErrNotPinned) {
				t.Fatalf("ParseSource(ref %q) = %v, want ErrNotPinned", ref, err)
			}
		})
	}
	_, err := ParseSource("https://huggingface.co/org/repo/resolve/main/m.gguf")
	if err == nil || !strings.Contains(err.Error(), "commit") {
		t.Fatalf("the refusal must tell the operator to use a commit id: %v", err)
	}
}

// TestParseSourceFailsClosed: everything outside the one accepted shape refuses, each
// for its own reason, and never by "fixing" the input.
func TestParseSourceFailsClosed(t *testing.T) {
	good := "huggingface.co/org/repo/resolve/" + commit + "/m.gguf"
	tests := []struct {
		name, raw string
		wantSub   string
	}{
		{"empty", "", "empty"},
		{"http", "http://" + good, "https"},
		{"other host", "https://hf-mirror.com/org/repo/resolve/" + commit + "/m.gguf", "huggingface.co"},
		{"lookalike host", "https://huggingface.co.evil.example/org/repo/resolve/" + commit + "/m.gguf", "huggingface.co"},
		{"subdomain", "https://cdn.huggingface.co/org/repo/resolve/" + commit + "/m.gguf", "huggingface.co"},
		{"userinfo", "https://user:pw@" + good, "userinfo"},
		{"port", "https://huggingface.co:8443/org/repo/resolve/" + commit + "/m.gguf", "port"},
		{"query", "https://" + good + "?download=true", "query"},
		{"fragment", "https://" + good + "#x", "fragment"},
		{"blob view", "https://huggingface.co/org/repo/blob/" + commit + "/m.gguf", "resolve"},
		{"raw", "https://huggingface.co/org/repo/raw/" + commit + "/m.gguf", "resolve"},
		{"no file", "https://huggingface.co/org/repo/resolve/" + commit, "path"},
		{"repo root only", "https://huggingface.co/org/repo", "path"},
		{"not a gguf", "https://huggingface.co/org/repo/resolve/" + commit + "/model.safetensors", ".gguf"},
		{"gguf in the middle", "https://huggingface.co/org/repo/resolve/" + commit + "/m.gguf/x", ".gguf"},
		{"percent escape", "https://huggingface.co/org/repo/resolve/" + commit + "/a%20b.gguf", "escape"},
		{"encoded slash", "https://huggingface.co/org/repo/resolve/" + commit + "/a%2Fb.gguf", "escape"},
		{"dot segment", "https://huggingface.co/org/repo/resolve/" + commit + "/../m.gguf", "segment"},
		{"empty segment", "https://huggingface.co/org/repo/resolve/" + commit + "//m.gguf", "segment"},
		{"backslash", "https://huggingface.co/org/repo/resolve/" + commit + `/a\b.gguf`, "segment"},
		{"space inside", "https://huggingface.co/org/repo/resolve/" + commit + "/a b.gguf", "character"},
		{"control byte", "https://huggingface.co/org/repo/resolve/" + commit + "/a\x00b.gguf", "character"},
		{"unicode name", "https://huggingface.co/org/repo/resolve/" + commit + "/modèle.gguf", "segment"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSource(tt.raw)
			if err == nil {
				t.Fatalf("ParseSource accepted %q as %+v", tt.raw, got)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.wantSub)) {
				t.Fatalf("refusal %q does not mention %q", err, tt.wantSub)
			}
		})
	}
}

// TestParseSourceRefusesAShardedFile: a split model needs one manifest entry per
// shard and this verb takes one file, so a numbered shard refuses with that reason
// rather than adding an entry that can never load.
func TestParseSourceRefusesAShardedFile(t *testing.T) {
	_, err := ParseSource("https://huggingface.co/org/repo/resolve/" + commit + "/Big-Q4_K_M-00001-of-00004.gguf")
	if !errors.Is(err, ErrSharded) {
		t.Fatalf("ParseSource(shard) = %v, want ErrSharded", err)
	}
}
