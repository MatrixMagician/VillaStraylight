package openwebui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/recall"
)

// attach_test.go covers the read-merge-write attach choreography. These tests moved
// here from the command tier along with the code: they used to reach a function
// through three injected row operations, and now drive the real thing through the
// one transport seam, which means the endpoint paths and request bodies are under
// test too.

const testKB = "kb1"
const testKBName = "Villa Recall"
const testServed = "served.gguf"

// rowServer is a fake that stores ONE model row and reports what happened to it.
type rowServer struct {
	// row is the stored row, or nil for "no such row".
	row map[string]any
	// dropWrites makes update/create return 200 while silently not persisting —
	// the silent-detach case the verify re-read exists to catch.
	dropWrites bool
	// freshReads returns a fresh copy per read, as a real server does, so an
	// in-place merge of one read cannot pollute the verify re-read.
	freshReads bool

	updates int
	creates int
	// lastWrite is the body of the most recent update or create.
	lastWrite map[string]any
}

func (s *rowServer) transport() Transport {
	return func(_ context.Context, req Request) ([]byte, error) {
		switch {
		case req.Path == pathModels:
			return json.Marshal(map[string]any{"data": []map[string]string{{"id": testServed}}})

		case strings.HasPrefix(req.Path, pathModelUpdate):
			s.updates++
			var body map[string]any
			_ = json.Unmarshal(req.Body, &body)
			s.lastWrite = body
			if !s.dropWrites {
				s.row = body
			}
			return []byte(`{}`), nil

		case req.Path == pathModelCreate:
			s.creates++
			var body map[string]any
			_ = json.Unmarshal(req.Body, &body)
			s.lastWrite = body
			if !s.dropWrites {
				s.row = body
			}
			return []byte(`{}`), nil

		case strings.HasPrefix(req.Path, pathModelRow):
			if s.row == nil {
				// The pinned digest answers NOT_FOUND for an absent row.
				return nil, fmt.Errorf("NOT_FOUND")
			}
			out, err := json.Marshal(s.row)
			if err != nil {
				return nil, err
			}
			if s.freshReads {
				return out, nil
			}
			return out, nil
		}
		return nil, fmt.Errorf("unrouted %q", req.Path)
	}
}

// TestAttachPreservesForeignKeys is the read-merge-write invariant: attaching must
// never clobber a description, capability or parameter the operator set in the UI.
func TestAttachPreservesForeignKeys(t *testing.T) {
	s := &rowServer{row: map[string]any{
		"id": testServed,
		"meta": map[string]any{
			"description": "keep me",
			"knowledge": []any{
				map[string]any{"type": "collection", "id": "other-kb", "name": "Other"},
			},
		},
		"params": map[string]any{"temperature": 0.5},
	}}

	state, err := New(s.transport()).AttachKnowledge(context.Background(), "tok", testServed, testKB, testKBName)
	if err != nil || state != recall.AttachmentAttached {
		t.Fatalf("attach = (%v, %v), want (attached, nil)", state, err)
	}
	if s.creates != 0 {
		t.Error("create must not run when the row exists — a blind create fails with MODEL_ID_TAKEN")
	}

	meta, _ := s.lastWrite["meta"].(map[string]any)
	if meta == nil || meta["description"] != "keep me" {
		t.Errorf("foreign meta keys must survive the merge; meta = %v", meta)
	}
	items, _ := meta["knowledge"].([]any)
	if len(items) != 2 {
		t.Fatalf("knowledge must carry the prior item AND the new one; items = %v", items)
	}
	if !hasKnowledgeID(items, testKB) {
		t.Errorf("the collection must be merged into meta.knowledge; items = %v", items)
	}
	if p, _ := s.lastWrite["params"].(map[string]any); p == nil || p["temperature"] != 0.5 {
		t.Errorf("foreign top-level fields must survive; params = %v", s.lastWrite["params"])
	}
}

// TestAttachIsIdempotent: re-attaching the same collection must not duplicate it.
func TestAttachIsIdempotent(t *testing.T) {
	s := &rowServer{row: map[string]any{
		"id": testServed,
		"meta": map[string]any{
			"knowledge": []any{map[string]any{"type": "collection", "id": testKB, "name": testKBName}},
		},
	}}

	if _, err := New(s.transport()).AttachKnowledge(context.Background(), "tok", testServed, testKB, testKBName); err != nil {
		t.Fatalf("re-attach errored: %v", err)
	}
	meta, _ := s.lastWrite["meta"].(map[string]any)
	items, _ := meta["knowledge"].([]any)
	if len(items) != 1 {
		t.Errorf("re-attach duplicated the collection; items = %v", items)
	}
}

// TestAttachCreatesTheOverrideRowWhenAbsent: an absent row takes the create path,
// with the override shape the server merges onto the live base model.
func TestAttachCreatesTheOverrideRowWhenAbsent(t *testing.T) {
	s := &rowServer{} // no row

	state, err := New(s.transport()).AttachKnowledge(context.Background(), "tok", testServed, testKB, testKBName)
	if err != nil || state != recall.AttachmentAttached {
		t.Fatalf("attach = (%v, %v), want (attached, nil)", state, err)
	}
	if s.updates != 0 {
		t.Error("update must not run when the row is absent")
	}
	if s.lastWrite["id"] != testServed || s.lastWrite["base_model_id"] != nil {
		t.Errorf("the created row must override the SERVED model (id == served, base_model_id null); row = %v", s.lastWrite)
	}
	meta, _ := s.lastWrite["meta"].(map[string]any)
	items, _ := meta["knowledge"].([]any)
	if len(items) != 1 {
		t.Errorf("the created row must carry the collection; items = %v", items)
	}
}

// TestSilentDetachIsNotReportedAttached is the reason the verify re-read exists. The
// write returns 200 but the server dropped meta.knowledge, so retrieval is off. That
// must surface as an error and a not-Attached verdict, never as a false green.
func TestSilentDetachIsNotReportedAttached(t *testing.T) {
	s := &rowServer{
		row:        map[string]any{"id": testServed, "meta": map[string]any{"knowledge": []any{}}},
		dropWrites: true,
	}

	state, err := New(s.transport()).AttachKnowledge(context.Background(), "tok", testServed, testKB, testKBName)
	if err == nil {
		t.Fatalf("a silent detach must return an error; got state=%v err=nil", state)
	}
	if state == recall.AttachmentAttached {
		t.Errorf("a silent detach must NOT be reported attached; state = %v", state)
	}
	if !strings.Contains(err.Error(), "retrieval is NOT wired") {
		t.Errorf("the error must say retrieval is not wired, got %q", err)
	}
}

// TestAttachmentStateDistinguishesUnknownFromMissing is the honesty rule the status
// report depends on. Missing is a confident observation that retrieval is off;
// Unknown means it could not be evaluated. Collapsing them would either fabricate a
// negative or hide one.
func TestAttachmentStateDistinguishesUnknownFromMissing(t *testing.T) {
	t.Run("attached", func(t *testing.T) {
		s := &rowServer{row: map[string]any{
			"id":   testServed,
			"meta": map[string]any{"knowledge": []any{map[string]any{"id": testKB}}},
		}}
		if got := New(s.transport()).AttachmentStateFor(context.Background(), "tok", testKB); got != recall.AttachmentAttached {
			t.Errorf("state = %v, want attached", got)
		}
	})

	t.Run("row present without the collection is confidently Missing", func(t *testing.T) {
		s := &rowServer{row: map[string]any{"id": testServed, "meta": map[string]any{}}}
		if got := New(s.transport()).AttachmentStateFor(context.Background(), "tok", testKB); got != recall.AttachmentMissing {
			t.Errorf("state = %v, want missing — the post-model-swap detach case", got)
		}
	})

	t.Run("row absent, service reachable, is confidently Missing", func(t *testing.T) {
		s := &rowServer{}
		if got := New(s.transport()).AttachmentStateFor(context.Background(), "tok", testKB); got != recall.AttachmentMissing {
			t.Errorf("state = %v, want missing", got)
		}
	})

	t.Run("model discovery failure is Unknown, never Missing", func(t *testing.T) {
		down := func(_ context.Context, _ Request) ([]byte, error) {
			return nil, fmt.Errorf("connection refused")
		}
		if got := New(down).AttachmentStateFor(context.Background(), "tok", testKB); got != recall.AttachmentUnknown {
			t.Errorf("state = %v, want unknown — an unreachable service is not evidence of a detach", got)
		}
	})
}
