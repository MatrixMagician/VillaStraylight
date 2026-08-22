package openwebui

import (
	"context"
	"fmt"

	"github.com/MatrixMagician/VillaStraylight/internal/recall"
)

// attach.go holds the Model-row attach choreography: wiring a knowledge collection
// into the served model's meta.knowledge so retrieval actually happens.
//
// The whole file exists because a 200 from update or create is NOT proof the merge
// persisted. Open WebUI can reshape or silently drop meta.knowledge, leaving
// retrieval off while an indexer reports success. Every write here is followed by a
// re-read that confirms the collection id actually landed.

// The attachment verdict is recall.AttachmentState, reused rather than redeclared:
// recall's staleness report already owns that vocabulary, and a second identical
// enum here is exactly the duplication this module exists to remove.
//
// Its Unknown is distinct from Missing and must stay so. Missing means the protocol
// confidently observed the attachment absent; Unknown means it could not tell.
// Reporting Unknown as Missing would fabricate a negative from an unevaluable
// signal, and reporting it as Attached would be a false green.

// knowledgeItem is the modern attachment item shape the pinned digest's chat
// middleware injects into every completion's files. The legacy collection_name(s)
// shapes are deliberately never emitted.
func knowledgeItem(kbID, kbName string) map[string]any {
	return map[string]any{"type": "collection", "id": kbID, "name": kbName}
}

// mergeKnowledgeIntoRow merges the collection into an existing row's
// meta.knowledge, deduplicating by id and PRESERVING every other meta key and
// top-level field the operator may have set in the UI. Read-merge-write, never
// clobber: attaching must not erase an operator-set description or capabilities.
func mergeKnowledgeIntoRow(row map[string]any, kbID, kbName string) map[string]any {
	meta, _ := row["meta"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	items, _ := meta["knowledge"].([]any)
	if !hasKnowledgeID(items, kbID) {
		items = append(items, knowledgeItem(kbID, kbName))
	}
	meta["knowledge"] = items
	row["meta"] = meta
	return row
}

// hasKnowledgeID reports whether an untyped meta.knowledge list contains the id. It
// tolerates the untyped shape the API returns and never panics on a mis-shaped meta.
func hasKnowledgeID(items []any, kbID string) bool {
	for _, it := range items {
		if m, ok := it.(map[string]any); ok && m["id"] == kbID {
			return true
		}
	}
	return false
}

// rowHasKnowledgeID is hasKnowledgeID over a whole row.
func rowHasKnowledgeID(row map[string]any, kbID string) bool {
	meta, _ := row["meta"].(map[string]any)
	if meta == nil {
		return false
	}
	items, _ := meta["knowledge"].([]any)
	return hasKnowledgeID(items, kbID)
}

// getModelRow reads one Model row. An HTTP failure is reported as absent rather than
// as an error: the pinned digest answers 401 NOT_FOUND for a row that does not
// exist, so "cannot read" and "not there" are indistinguishable at this endpoint.
// Taking the create path on that is safe, because a blind create against a
// truly-present row fails honestly with MODEL_ID_TAKEN rather than silently.
func (c *Client) getModelRow(ctx context.Context, token, modelID string) (map[string]any, bool) {
	out, err := c.transport(ctx, Request{Path: pathModelRowByID(modelID), Token: token})
	if err != nil {
		return nil, false
	}
	var row map[string]any
	if jerr := jsonUnmarshal(out, &row); jerr != nil || row["id"] == nil {
		return nil, false
	}
	return row, true
}

// AttachKnowledge wires the collection into the served model's row, idempotently.
//
// If the row exists its meta.knowledge is merged, preserving foreign keys. If it
// does not, a fresh row is created with id equal to the served base model id and a
// null base_model_id — the shape the pinned digest merges onto the live base model.
// Never a blind create: stale rows from previous swaps make create fail with
// MODEL_ID_TAKEN.
//
// The write is then VERIFIED by re-reading the row. A verdict of Attached means the
// collection id was observed in meta.knowledge after the write, not merely that the
// write returned 200.
func (c *Client) AttachKnowledge(ctx context.Context, token, servedModelID, kbID, kbName string) (recall.AttachmentState, error) {
	row, exists := c.getModelRow(ctx, token, servedModelID)
	if exists {
		body, err := jsonBody(mergeKnowledgeIntoRow(row, kbID, kbName))
		if err != nil {
			return recall.AttachmentUnknown, err
		}
		if _, err := c.do(ctx, "models/model/update", Request{
			Method: "POST", Path: pathModelUpdateByID(servedModelID), Token: token, Body: body,
		}); err != nil {
			return recall.AttachmentUnknown, err
		}
	} else {
		body, err := jsonBody(map[string]any{
			"id":            servedModelID,
			"base_model_id": nil,
			"name":          servedModelID,
			"params":        map[string]any{},
			"is_active":     true,
			"meta":          map[string]any{"knowledge": []any{knowledgeItem(kbID, kbName)}},
		})
		if err != nil {
			return recall.AttachmentUnknown, err
		}
		if _, err := c.do(ctx, "models/create", Request{
			Method: "POST", Path: pathModelCreate, Token: token, Body: body,
		}); err != nil {
			return recall.AttachmentUnknown, err
		}
	}

	// Verify: a 200 is not proof the merge persisted. Confirm the id actually landed,
	// so the caller fails honestly instead of stamping a false green over retrieval
	// that is silently off.
	verifyRow, verifyExists := c.getModelRow(ctx, token, servedModelID)
	if !verifyExists {
		return recall.AttachmentMissing, fmt.Errorf("attach verify: the served model row %q is absent after update/create — retrieval is NOT wired", servedModelID)
	}
	if !rowHasKnowledgeID(verifyRow, kbID) {
		return recall.AttachmentMissing, fmt.Errorf("attach verify: the recall KB %q did not persist in model %q meta.knowledge (silent detach) — retrieval is NOT wired", kbID, servedModelID)
	}
	return recall.AttachmentAttached, nil
}

// AttachmentStateFor answers the read-only retrieval question: discover the served
// model, read its row, and report whether the collection is attached.
//
// Unknown when discovery or parsing is unevaluable. Missing when the service is
// reachable and the token good but the row or attachment is confidently absent —
// the post-model-swap detach case.
func (c *Client) AttachmentStateFor(ctx context.Context, token, kbID string) recall.AttachmentState {
	served, err := c.DiscoverModel(ctx, token)
	if err != nil {
		return recall.AttachmentUnknown
	}
	out, err := c.transport(ctx, Request{Path: pathModelRowByID(served), Token: token})
	if err != nil {
		// DiscoverModel just proved the service reachable and the token good, so a
		// failed row read here is the digest's NOT_FOUND: confidently absent.
		return recall.AttachmentMissing
	}
	var row struct {
		Meta struct {
			Knowledge []struct {
				ID string `json:"id"`
			} `json:"knowledge"`
		} `json:"meta"`
	}
	if jsonUnmarshal(out, &row) != nil {
		return recall.AttachmentUnknown
	}
	for _, k := range row.Meta.Knowledge {
		if k.ID == kbID {
			return recall.AttachmentAttached
		}
	}
	return recall.AttachmentMissing
}
