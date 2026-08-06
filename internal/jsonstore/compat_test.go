package jsonstore_test

// compat_test.go is the migration's evidence: a file written by the pre-shared-store
// code must still load, and the bytes a save produces must be unchanged.
//
// The three stores each kept their own schema version and their own filename, so the
// risk of consolidating them was never the happy path (their existing tests cover
// that) — it was that a shared layer might reorder JSON fields, drop a key, or make
// one store's version readable by another. Each of those is checked here against a
// literal captured from the previous implementation.

import (
	"encoding/json"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/recall"
	"github.com/MatrixMagician/VillaStraylight/internal/usage"
	"github.com/MatrixMagician/VillaStraylight/internal/verifystate"
)

// bufferDeps is a byte-backed seam: what the store writes is what the next read sees.
func bufferDeps(initial []byte) (*[]byte, verifystate.Deps) {
	buf := initial
	return &buf, verifystate.Deps{
		WriteAll: func(data []byte) error { buf = append([]byte(nil), data...); return nil },
		ReadAll:  func() ([]byte, error) { return buf, nil },
	}
}

// TestVerifyStateBytesUnchanged pins the serialised form against a literal captured
// from the previous implementation, so a field reorder or a renamed key fails here
// rather than silently changing what is on disk.
func TestVerifyStateBytesUnchanged(t *testing.T) {
	buf, deps := bufferDeps(nil)

	if err := verifystate.Save(deps, verifystate.State{Verdict: "PASS", CheckedAt: "2026-08-06T09:00:00Z"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	const want = `{"schema_version":1,"verdict":"PASS","checked_at":"2026-08-06T09:00:00Z"}`
	if string(*buf) != want {
		t.Errorf("serialised bytes changed:\n got %s\nwant %s", *buf, want)
	}
}

// TestVerifyStateLoadsAFileWrittenBefore proves a document already on disk still
// loads: the literal is exactly what the previous code wrote.
func TestVerifyStateLoadsAFileWrittenBefore(t *testing.T) {
	const onDisk = `{"schema_version":1,"verdict":"PASS","checked_at":"2026-08-05T21:00:00Z"}`
	_, deps := bufferDeps([]byte(onDisk))

	got, err := verifystate.Load(deps)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Verdict != "PASS" || got.CheckedAt != "2026-08-05T21:00:00Z" {
		t.Errorf("loaded %+v, want the verdict and timestamp from disk", got)
	}
}

// TestFailClosedSemanticsPreserved is the load-bearing behaviour the issue named:
// an absent, corrupt or future-schema store must read as EMPTY, never as a recorded
// result. An empty verify state means "no verified result", and reading a fabricated
// PASS out of a corrupt file would be the worst possible failure of this store.
func TestFailClosedSemanticsPreserved(t *testing.T) {
	cases := map[string]string{
		"absent":        "",
		"corrupt":       `{not json at all`,
		"future schema": `{"schema_version":99,"verdict":"PASS","checked_at":"2026-08-06T09:00:00Z"}`,
		"past schema":   `{"schema_version":0,"verdict":"PASS","checked_at":"2026-08-06T09:00:00Z"}`,
	}

	for name, onDisk := range cases {
		t.Run(name, func(t *testing.T) {
			_, deps := bufferDeps([]byte(onDisk))
			got, err := verifystate.Load(deps)
			if err != nil {
				t.Fatalf("Load returned an error rather than failing closed: %v", err)
			}
			if got != (verifystate.State{}) {
				t.Errorf("loaded %+v, want the empty state — a %s store must never read as a recorded PASS", got, name)
			}
		})
	}
}

// TestSchemaVersionIsAlwaysStamped guards the tampering rule: a caller-supplied
// version is never trusted, so a document claiming another version is rewritten to
// this store's own on save.
func TestSchemaVersionIsAlwaysStamped(t *testing.T) {
	buf, deps := bufferDeps(nil)

	if err := verifystate.Save(deps, verifystate.State{SchemaVersion: 99, Verdict: "FAIL"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var got verifystate.State
	if err := json.Unmarshal(*buf, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.SchemaVersion != verifystate.SchemaVersion() {
		t.Errorf("schema_version = %d, want %d — a caller-supplied version must not survive",
			got.SchemaVersion, verifystate.SchemaVersion())
	}
}

// TestStoresKeepSeparateVersionsAndPaths is the "not interchangeable" check: the
// three stores share a persistence layer, not a schema or a filename. A payload from
// one must not be readable as another.
func TestStoresKeepSeparateVersionsAndPaths(t *testing.T) {
	paths := map[string]string{
		"verifystate": verifystate.VerifyStatePath(),
		"recall":      recall.RecallStatePath(),
		"usage":       usage.UsagePath(),
	}
	seen := map[string]string{}
	for name, p := range paths {
		if other, clash := seen[p]; clash {
			t.Errorf("%s and %s resolve to the same path %q — the stores would overwrite each other", name, other, p)
		}
		seen[p] = name
	}

	// A recall document must not load as a verify state, even though both now go
	// through the same layer.
	recallDoc := `{"schema_version":1,"knowledge_id":"kb-1","knowledge_name":"recall"}`
	_, deps := bufferDeps([]byte(recallDoc))
	got, err := verifystate.Load(deps)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Verdict != "" {
		t.Errorf("a recall document loaded as a verify state carrying verdict %q", got.Verdict)
	}
}

// TestUsageBytesUnchanged pins the usage document's serialised form too, since it
// carries the counter-folding state that must survive a round trip untouched.
func TestUsageBytesUnchanged(t *testing.T) {
	var buf []byte
	deps := usage.Deps{
		WriteAll: func(data []byte) error { buf = append([]byte(nil), data...); return nil },
		ReadAll:  func() ([]byte, error) { return buf, nil },
	}

	totals := usage.UsageTotals{
		Models: map[string]usage.ModelUsage{
			"qwen3": {
				Prompt:    usage.CounterState{Cumulative: 100, LastSeenRaw: 40},
				Predicted: usage.CounterState{Cumulative: 200, LastSeenRaw: 80},
				LastSeen:  "2026-08-06T09:00:00Z",
			},
		},
	}
	if err := usage.Save(deps, totals); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// The literal is the previous implementation's output, field for field. The
	// struct tags are unchanged by the migration (the diff touches no `json:` tag),
	// so this is what was already on disk.
	const want = `{"models":{"qwen3":{"model":"","prompt_tokens":{"cumulative":100,"last_seen_raw":40},` +
		`"generated_tokens":{"cumulative":200,"last_seen_raw":80},"last_seen":"2026-08-06T09:00:00Z"}},` +
		`"schema_version":1}`
	if string(buf) != want {
		t.Errorf("usage serialised bytes changed:\n got %s\nwant %s", buf, want)
	}

	back, err := usage.Load(deps)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if back.Models["qwen3"].Prompt.Cumulative != 100 || back.Models["qwen3"].Predicted.LastSeenRaw != 80 {
		t.Errorf("round trip lost counter state: %+v", back.Models["qwen3"])
	}
}
