// recall_live.go is the live REST drive surface for `villa recall` (RECALL-01/02/03):
// thin fixed-arg curl wrappers over the EXISTING loopback Open WebUI PublishPort,
// reusing the Phase-20 primitives in verify_memory.go — mintAdminToken,
// runLoopbackCurl(Stdin), pollFileProcessed, discoverChatModel — never copying them
// (D-01). No new host port is opened; all traffic is 127.0.0.1:<chat_port>.
//
// Honesty invariants carried from the Phase-20 seam (T-20-09 / T-21-06/-08/-11):
//
//   - Every request is a fixed-arg exec.CommandContext curl — no shell, ever.
//     JSON bodies are built via json.Marshal only; URLs are composed from the
//     config-resolved base + API-returned ids; transcript content travels via
//     stdin multipart (`-F file=@-`), never argv or a temp file.
//   - Every error is wrapped with the endpoint name; an empty id in a 200 body is
//     an ERROR carrying the (truncated) raw body for diagnosis — never a silent skip.
//   - Indexing writes go ONLY through OWUI's knowledge/files REST pipeline (D-02);
//     villa never writes Qdrant directly. Clean-replace uses
//     `knowledge/{id}/file/remove?delete_file=true` (vectors deleted by file_id AND
//     hash); --rebuild uses the id-preserving `knowledge/{id}/reset` — NEVER
//     `DELETE /knowledge/{id}/delete`, which strips the KB from every model's
//     meta.knowledge (D-04).
//   - The attach step (D-03, RECALL-02) is an idempotent read-merge-write of the
//     served model's Model row: GET → merge the recall KB into meta.knowledge
//     preserving every other meta key the operator may have set → update-or-create.
//   - Identity is the existing villa-verify@localhost admin service account (D-09);
//     the JWT is held in memory only, never persisted.
//
// NO image/backend literals live here (loopback URLs and OWUI endpoint paths are not
// gate-relevant — verify_memory.go precedent); TestSeamGrepGate stays green.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/recall"
)

// recallKnowledgeName is the villa-managed Knowledge collection's name — the
// find-or-create key owuiEnsureKnowledge matches on, and the `name` field of the
// meta.knowledge attachment item (D-02).
const recallKnowledgeName = "Villa Recall — Past Conversations"

// recallKnowledgeDescription is the villa-managed collection's description (set
// once at create; OWUI embeds it as KB metadata, which is why villa-embed must be
// reachable at create time — Pitfall 7).
const recallKnowledgeDescription = "villa-managed semantic index of past Open WebUI conversations (villa recall)"

// recallServiceAccountEmail is the ONE identity the indexer deterministically
// excludes from the chat universe (D-09): villa's own verify/admin service account
// (the same fixed credential mintAdminToken signs in with). All remaining human
// users on this single-operator box are the operator.
const recallServiceAccountEmail = "villa-verify@localhost"

// owuiChatsPageSize is the admin chats-list page size hard-coded by the pinned
// OWUI digest (routers/chats.py): a page with fewer items is the LAST page.
const owuiChatsPageSize = 60

// recallUploadBaseTimeout is the base per-file processing allowance; see
// recallUploadTimeout for the size-aware extension (RESEARCH A2 / Pitfall 5: long
// transcripts are chunked at ~1000 chars and embedded one chunk per request).
const recallUploadBaseTimeout = 60 * time.Second

// recallUploadTimeout returns the size-aware processing timeout for one transcript:
// 60s base + 1s per 2 KiB of content. A timeout remains an ERROR (the chat was not
// indexed), never a silent skip — the generosity only avoids FALSE timeouts on long
// chats, it never converts a real failure into a pass.
func recallUploadTimeout(content string) time.Duration {
	return recallUploadBaseTimeout + time.Duration(len(content)/2048)*time.Second
}

// truncateBody bounds a raw response body embedded in an error detail so a parse
// miss on a large (potentially content-bearing) chat body stays diagnosable without
// dumping hundreds of KiB to stderr.
func truncateBody(out []byte) string {
	const max = 512
	if len(out) <= max {
		return string(out)
	}
	return string(out[:max]) + "…(truncated)"
}

// bearerHeader composes the Authorization header value from a minted JWT. The token
// is an API-returned value held in memory only (D-09).
func bearerHeader(token string) string { return "Authorization: Bearer " + token }

// owuiUser is one item of the admin users list (GET /api/v1/users/): the id the
// chats-list endpoint is keyed by, the email the service-account exclusion matches
// on (D-09), and the role (informational).
type owuiUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// owuiListUsers enumerates ALL users via the admin endpoint `GET /api/v1/users/?page=N`
// (D-09). Pages are fetched until one is empty or contributes no new ids (the
// dedupe guard also terminates against a server that ignores `page`). Any failure
// is an error — an index run cannot proceed on a partial user universe.
func owuiListUsers(ctx context.Context, base, token string) ([]owuiUser, error) {
	auth := bearerHeader(token)
	var users []owuiUser
	seen := map[string]bool{}
	for page := 1; ; page++ {
		out, err := runLoopbackCurl(ctx, "-sf", "-H", auth,
			base+"/api/v1/users/?page="+strconv.Itoa(page))
		if err != nil {
			return nil, fmt.Errorf("users/ page %d: %w", page, err)
		}
		var resp struct {
			Users []owuiUser `json:"users"`
		}
		if jerr := json.Unmarshal(out, &resp); jerr != nil {
			return nil, fmt.Errorf("parse users/ page %d (%v): %s", page, jerr, truncateBody(out))
		}
		added := 0
		for _, u := range resp.Users {
			if u.ID == "" || seen[u.ID] {
				continue
			}
			seen[u.ID] = true
			users = append(users, u)
			added++
		}
		if len(resp.Users) == 0 || added == 0 {
			return users, nil
		}
	}
}

// owuiListUserChats lists ONE user's complete chat universe via the ADMIN endpoint
// `GET /api/v1/chats/list/user/{user_id}?page=N` — include_archived is hard-coded
// True and no folder/pinned filtering applies (Pitfall 1; the self-list
// `GET /api/v1/chats/` silently under-indexes and is NEVER used). 60 items/page;
// a short page terminates. Items map to recall.ChatRef (id + updated_at epoch
// seconds) — no titles, no content (T-21-01).
func owuiListUserChats(ctx context.Context, base, token, userID string) ([]recall.ChatRef, error) {
	auth := bearerHeader(token)
	var refs []recall.ChatRef
	for page := 1; ; page++ {
		out, err := runLoopbackCurl(ctx, "-sf", "-H", auth,
			base+"/api/v1/chats/list/user/"+userID+"?page="+strconv.Itoa(page))
		if err != nil {
			return nil, fmt.Errorf("chats/list/user page %d: %w", page, err)
		}
		var items []struct {
			ID        string `json:"id"`
			UpdatedAt int64  `json:"updated_at"`
		}
		if jerr := json.Unmarshal(out, &items); jerr != nil {
			return nil, fmt.Errorf("parse chats/list/user page %d (%v): %s", page, jerr, truncateBody(out))
		}
		for _, it := range items {
			if it.ID == "" {
				continue
			}
			refs = append(refs, recall.ChatRef{ID: it.ID, UserID: userID, UpdatedAt: it.UpdatedAt})
		}
		if len(items) < owuiChatsPageSize {
			return refs, nil
		}
	}
}

// owuiGetChat fetches one full chat document (`GET /api/v1/chats/{id}`) and maps it
// to recall.ChatDoc: the top-level id/title/created_at plus chat.history — the flat
// chat.messages list is NEVER read (a stale frontend branch view; the renderer
// walks history.currentId → parentId, exactly OWUI's get_message_list).
func owuiGetChat(ctx context.Context, base, token, chatID string) (recall.ChatDoc, error) {
	auth := bearerHeader(token)
	out, err := runLoopbackCurl(ctx, "-sf", "-H", auth, base+"/api/v1/chats/"+chatID)
	if err != nil {
		return recall.ChatDoc{}, fmt.Errorf("chats/{id} get: %w", err)
	}
	var resp struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		CreatedAt int64  `json:"created_at"`
		Chat      struct {
			History recall.ChatHistory `json:"history"`
		} `json:"chat"`
	}
	if jerr := json.Unmarshal(out, &resp); jerr != nil || resp.ID == "" {
		return recall.ChatDoc{}, fmt.Errorf("chats/{id} get returned no parseable chat (%v): %s", jerr, truncateBody(out))
	}
	return recall.ChatDoc{
		ID:        resp.ID,
		Title:     resp.Title,
		CreatedAt: resp.CreatedAt,
		History:   resp.Chat.History,
	}, nil
}

// owuiEnsureKnowledge finds-or-creates the villa-managed Knowledge collection
// (D-02): list existing KBs (`GET /api/v1/knowledge/`) and return the id of the one
// matching name; else `POST /api/v1/knowledge/create`. Find-or-create keeps re-runs
// AND state-file-loss recovery idempotent — a lost recall-state.json never spawns a
// second collection.
func owuiEnsureKnowledge(ctx context.Context, base, token, name, description string) (string, error) {
	auth := bearerHeader(token)
	// The pinned digest serves `GET /api/v1/knowledge/` as a PAGINATED envelope
	// `{"items":[…],"total":N}` (live-verified on gfx1151, 21-03); older shapes
	// returned a bare array. Parse both, and walk pages until an empty page so a
	// large KB list can never hide the recall collection (which would spawn a
	// duplicate on every run).
	type kbRow struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	seen := map[string]bool{}
	for page := 1; ; page++ {
		lOut, err := runLoopbackCurl(ctx, "-sf", "-H", auth,
			fmt.Sprintf("%s/api/v1/knowledge/?page=%d", base, page))
		if err != nil {
			return "", fmt.Errorf("knowledge/ list: %w", err)
		}
		var kbs []kbRow
		if jerr := json.Unmarshal(lOut, &kbs); jerr != nil {
			var envelope struct {
				Items []kbRow `json:"items"`
			}
			if eerr := json.Unmarshal(lOut, &envelope); eerr != nil {
				return "", fmt.Errorf("parse knowledge/ list (%v): %s", jerr, truncateBody(lOut))
			}
			kbs = envelope.Items
		}
		newIDs := false
		for _, kb := range kbs {
			if kb.Name == name && kb.ID != "" {
				return kb.ID, nil
			}
			if kb.ID != "" && !seen[kb.ID] {
				seen[kb.ID] = true
				newIDs = true
			}
		}
		// Terminate on an empty page OR a page contributing no new ids (the
		// owuiListUsers dedupe guard — robust against a server ignoring ?page).
		if len(kbs) == 0 || !newIDs {
			break
		}
	}

	body, err := json.Marshal(map[string]any{"name": name, "description": description})
	if err != nil {
		return "", err
	}
	cOut, err := runLoopbackCurl(ctx,
		"-sf", "-X", "POST", base+"/api/v1/knowledge/create",
		"-H", "Content-Type: application/json", "-H", auth,
		"-d", string(body),
	)
	if err != nil {
		return "", fmt.Errorf("knowledge/create: %w", err)
	}
	var created struct {
		ID string `json:"id"`
	}
	if jerr := json.Unmarshal(cOut, &created); jerr != nil || created.ID == "" {
		return "", fmt.Errorf("knowledge/create returned no id (%v): %s", jerr, truncateBody(cOut))
	}
	return created.ID, nil
}

// owuiUploadTranscript pushes one rendered transcript into the recall KB via the
// proven three-step pipeline (D-02): multipart upload to `POST /api/v1/files/` with
// the content on STDIN (`-F file=@-` — never argv, never a temp file, T-21-06),
// poll `GET /api/v1/files/{id}/process/status` until chunk→embed→store completes
// (a timeout is an ERROR, never a skip — Pitfall 6), then
// `POST /api/v1/knowledge/{kbID}/file/add`. Returns the OWUI file id the
// clean-replace flow keys on.
func owuiUploadTranscript(ctx context.Context, base, token, kbID, filename, content string, timeout time.Duration) (string, error) {
	auth := bearerHeader(token)
	fOut, err := runLoopbackCurlStdin(ctx, content,
		"-sf", "-X", "POST", base+"/api/v1/files/",
		"-H", auth,
		"-F", "file=@-;filename="+filename+";type=text/plain",
	)
	if err != nil {
		return "", fmt.Errorf("files/ upload: %w", err)
	}
	var fResp struct {
		ID string `json:"id"`
	}
	if jerr := json.Unmarshal(fOut, &fResp); jerr != nil || fResp.ID == "" {
		return "", fmt.Errorf("files/ upload returned no id (%v): %s", jerr, truncateBody(fOut))
	}

	if perr := pollFileProcessed(ctx, base, auth, fResp.ID, timeout); perr != nil {
		return "", fmt.Errorf("file processing: %w", perr)
	}

	aBody, err := json.Marshal(map[string]any{"file_id": fResp.ID})
	if err != nil {
		return "", err
	}
	if _, err := runLoopbackCurl(ctx,
		"-sf", "-X", "POST", base+"/api/v1/knowledge/"+kbID+"/file/add",
		"-H", "Content-Type: application/json", "-H", auth,
		"-d", string(aBody),
	); err != nil {
		return "", fmt.Errorf("knowledge/file/add: %w", err)
	}
	return fResp.ID, nil
}

// owuiRemoveKnowledgeFile is the clean-replace/delete primitive (D-04):
// `POST /api/v1/knowledge/{kbID}/file/remove?delete_file=true` — confirmed
// leak-free on the pinned digest (vectors deleted by file_id AND content hash, the
// per-file vector collection dropped, the file row deleted).
func owuiRemoveKnowledgeFile(ctx context.Context, base, token, kbID, fileID string) error {
	auth := bearerHeader(token)
	body, err := json.Marshal(map[string]any{"file_id": fileID})
	if err != nil {
		return err
	}
	if _, err := runLoopbackCurl(ctx,
		"-sf", "-X", "POST", base+"/api/v1/knowledge/"+kbID+"/file/remove?delete_file=true",
		"-H", "Content-Type: application/json", "-H", auth,
		"-d", string(body),
	); err != nil {
		return fmt.Errorf("knowledge/file/remove: %w", err)
	}
	return nil
}

// owuiResetKnowledge is the --rebuild primitive (D-04):
// `POST /api/v1/knowledge/{kbID}/reset` drops the KB's vector collection and clears
// its file list while KEEPING the KB id — so the served model's meta.knowledge
// attachment survives. NEVER `DELETE /knowledge/{id}/delete` (it strips the KB from
// every model's meta.knowledge and changes the id).
func owuiResetKnowledge(ctx context.Context, base, token, kbID string) error {
	auth := bearerHeader(token)
	if _, err := runLoopbackCurl(ctx,
		"-sf", "-X", "POST", base+"/api/v1/knowledge/"+kbID+"/reset",
		"-H", auth,
	); err != nil {
		return fmt.Errorf("knowledge/reset: %w", err)
	}
	return nil
}

// knowledgeItem is the modern meta.knowledge attachment item shape the pinned
// digest's chat middleware injects into every completion's files (RECALL-02). The
// legacy collection_name(s) shapes are deliberately never emitted.
func knowledgeItem(kbID, kbName string) map[string]any {
	return map[string]any{"type": "collection", "id": kbID, "name": kbName}
}

// mergeKnowledgeIntoRow merges the recall KB attachment item into an existing Model
// row's meta.knowledge, deduplicating by KB id and PRESERVING every other meta key
// (and every other top-level row field) the operator may have set in the UI —
// read-merge-write, never clobber (T-21-10 / Pitfall: attach must not erase an
// operator-set description/capabilities). It returns the same row, updated.
func mergeKnowledgeIntoRow(row map[string]any, kbID, kbName string) map[string]any {
	meta, _ := row["meta"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	items, _ := meta["knowledge"].([]any)
	present := false
	for _, it := range items {
		if m, ok := it.(map[string]any); ok && m["id"] == kbID {
			present = true
			break
		}
	}
	if !present {
		items = append(items, knowledgeItem(kbID, kbName))
	}
	meta["knowledge"] = items
	row["meta"] = meta
	return row
}

// attachKnowledgeRow is the idempotent read-merge-write attach choreography (D-03,
// RECALL-02) over injected row operations, unit-testable off-hardware
// (TestRecallAttach): if the served model's Model row exists, merge the recall KB
// into its meta.knowledge (preserving foreign keys) and update; if absent, create a
// fresh row with id == the SERVED base model id and base_model_id null — the shape
// the pinned digest's utils/models.py merges onto the live base model. Never a
// blind create (Pitfall 3: stale rows from previous swaps make create 401
// MODEL_ID_TAKEN).
func attachKnowledgeRow(
	getRow func() (map[string]any, bool, error),
	updateRow func(map[string]any) error,
	createRow func(map[string]any) error,
	servedModelID, kbID, kbName string,
) (recall.AttachmentState, error) {
	row, exists, err := getRow()
	if err != nil {
		return recall.AttachmentUnknown, fmt.Errorf("models/model get: %w", err)
	}
	if exists {
		merged := mergeKnowledgeIntoRow(row, kbID, kbName)
		if err := updateRow(merged); err != nil {
			return recall.AttachmentUnknown, fmt.Errorf("models/model/update: %w", err)
		}
		return recall.AttachmentAttached, nil
	}
	fresh := map[string]any{
		"id":            servedModelID,
		"base_model_id": nil,
		"name":          servedModelID,
		"params":        map[string]any{},
		"is_active":     true,
		"meta":          map[string]any{"knowledge": []any{knowledgeItem(kbID, kbName)}},
	}
	if err := createRow(fresh); err != nil {
		return recall.AttachmentUnknown, fmt.Errorf("models/create: %w", err)
	}
	return recall.AttachmentAttached, nil
}

// owuiAttachKnowledge wires attachKnowledgeRow to the live Model-row endpoints:
// `GET /api/v1/models/model?id=<served>` (a non-2xx — the digest 401s NOT_FOUND for
// an absent row — selects the create path), `POST /api/v1/models/model/update?id=…`
// with the FULL merged row (foreign meta/params keys preserved), or
// `POST /api/v1/models/create`. The served model id is the GGUF filename and is
// query-escaped; it is an API-returned value, never user input.
func owuiAttachKnowledge(ctx context.Context, base, token, servedModelID, kbID, kbName string) (recall.AttachmentState, error) {
	auth := bearerHeader(token)
	escapedID := url.QueryEscape(servedModelID)

	getRow := func() (map[string]any, bool, error) {
		out, err := runLoopbackCurl(ctx, "-sf", "-H", auth,
			base+"/api/v1/models/model?id="+escapedID)
		if err != nil {
			// The pinned digest answers 401 NOT_FOUND for an absent row; with -sf any
			// HTTP failure lands here → take the create path (a blind create on a
			// truly-present row fails honestly with MODEL_ID_TAKEN, never silently).
			return nil, false, nil
		}
		var row map[string]any
		if jerr := json.Unmarshal(out, &row); jerr != nil || row["id"] == nil {
			return nil, false, nil
		}
		return row, true, nil
	}
	updateRow := func(row map[string]any) error {
		body, err := json.Marshal(row)
		if err != nil {
			return err
		}
		_, err = runLoopbackCurl(ctx,
			"-sf", "-X", "POST", base+"/api/v1/models/model/update?id="+escapedID,
			"-H", "Content-Type: application/json", "-H", auth,
			"-d", string(body),
		)
		return err
	}
	createRow := func(row map[string]any) error {
		body, err := json.Marshal(row)
		if err != nil {
			return err
		}
		_, err = runLoopbackCurl(ctx,
			"-sf", "-X", "POST", base+"/api/v1/models/create",
			"-H", "Content-Type: application/json", "-H", auth,
			"-d", string(body),
		)
		return err
	}

	return attachKnowledgeRow(getRow, updateRow, createRow, servedModelID, kbID, kbName)
}

// owuiAttachmentState answers `recall status`'s retrieval question with the typed
// AttachmentState verdict (D-06, Pitfall 2): discover the SERVED model (reuse
// discoverChatModel), GET its Model row, and report Attached when the recall KB id
// is in meta.knowledge, Missing when OWUI is reachable but the row/attachment is
// confidently absent (the post-model-swap detach case), and Unknown when discovery
// or parsing is unevaluable — Unknown is DISTINCT from Missing, never a guess.
func owuiAttachmentState(ctx context.Context, base, token, kbID string) recall.AttachmentState {
	auth := bearerHeader(token)
	served, err := discoverChatModel(ctx, base, auth)
	if err != nil {
		return recall.AttachmentUnknown
	}
	out, err := runLoopbackCurl(ctx, "-sf", "-H", auth,
		base+"/api/v1/models/model?id="+url.QueryEscape(served))
	if err != nil {
		// discoverChatModel just proved OWUI reachable + the token good — a failed
		// row GET here is the digest's NOT_FOUND: confidently absent, retrieval OFF.
		return recall.AttachmentMissing
	}
	var row struct {
		Meta struct {
			Knowledge []struct {
				ID string `json:"id"`
			} `json:"knowledge"`
		} `json:"meta"`
	}
	if json.Unmarshal(out, &row) != nil {
		return recall.AttachmentUnknown
	}
	for _, k := range row.Meta.Knowledge {
		if k.ID == kbID {
			return recall.AttachmentAttached
		}
	}
	return recall.AttachmentMissing
}

// owuiHealthy is the cheap pre-mutation reachability gate (`GET /health` over the
// loopback PublishPort, bounded by --max-time). It runs BEFORE any mutating step so
// a down OWUI (or its villa-embed dependency — knowledge/create embeds KB metadata,
// Pitfall 7) surfaces as a refuse-with-remediation naming the service, not a
// confusing mid-create failure.
func owuiHealthy(ctx context.Context, base string) error {
	if _, err := runLoopbackCurl(ctx, "-sf", "--max-time", "5", base+"/health"); err != nil {
		return fmt.Errorf("health: %w", err)
	}
	return nil
}
