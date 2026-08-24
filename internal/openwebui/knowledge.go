package openwebui

import (
	"context"
	"fmt"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/recall"
)

// knowledge.go holds the knowledge-collection and file-pipeline operations: the
// indexing half of the protocol.

// User is one item of the admin users list: the id the chats list is keyed by, the
// email a service-account exclusion matches on, and the role (informational).
type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// ListUsers enumerates ALL users. Pages are fetched until one is empty or
// contributes no new ids; the dedupe guard also terminates against a server that
// ignores the page parameter. Any failure is an error — an index run cannot proceed
// on a partial user universe.
func (c *Client) ListUsers(ctx context.Context, token string) ([]User, error) {
	var users []User
	seen := map[string]bool{}
	for page := 1; ; page++ {
		out, err := c.do(ctx, fmt.Sprintf("users/ page %d", page), Request{
			Path:  pathUsersPage(page),
			Token: token,
		})
		if err != nil {
			return nil, err
		}
		var resp struct {
			Users []User `json:"users"`
		}
		if derr := decode(fmt.Sprintf("users/ page %d", page), out, &resp); derr != nil {
			return nil, derr
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

// ListUserChats lists ONE user's complete chat universe, paging until a short page.
func (c *Client) ListUserChats(ctx context.Context, token, userID string) ([]recall.ChatRef, error) {
	var refs []recall.ChatRef
	for page := 1; ; page++ {
		name := fmt.Sprintf("chats/list/user page %d", page)
		out, err := c.do(ctx, name, Request{Path: pathUserChatsPage(userID, page), Token: token})
		if err != nil {
			return nil, err
		}
		var items []struct {
			ID        string `json:"id"`
			UpdatedAt int64  `json:"updated_at"`
		}
		if derr := decode(name, out, &items); derr != nil {
			return nil, derr
		}
		for _, it := range items {
			if it.ID == "" {
				continue
			}
			refs = append(refs, recall.ChatRef{ID: it.ID, UserID: userID, UpdatedAt: it.UpdatedAt})
		}
		if len(items) < ChatsPageSize {
			return refs, nil
		}
	}
}

// GetChat fetches one full chat document, mapped to recall.ChatDoc — the shape the
// transcript renderer already owns, reused rather than redeclared.
//
// Only chat.history is read. The flat chat.messages list the API also returns is a
// stale frontend branch view; the renderer walks currentId back through parentId,
// exactly as the server's own get_message_list does.
func (c *Client) GetChat(ctx context.Context, token, chatID string) (recall.ChatDoc, error) {
	out, err := c.do(ctx, "chats/{id} get", Request{Path: pathChat(chatID), Token: token})
	if err != nil {
		return recall.ChatDoc{}, err
	}
	var resp struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		CreatedAt int64  `json:"created_at"`
		Chat      struct {
			History recall.ChatHistory `json:"history"`
		} `json:"chat"`
	}
	if jerr := jsonUnmarshal(out, &resp); jerr != nil || resp.ID == "" {
		return recall.ChatDoc{}, fmt.Errorf("chats/{id} get returned no parseable chat (%v): %s", jerr, bodyDetail(out))
	}
	return recall.ChatDoc{
		ID:        resp.ID,
		Title:     resp.Title,
		CreatedAt: resp.CreatedAt,
		History:   resp.Chat.History,
	}, nil
}

// EnsureKnowledge finds-or-creates the villa-managed collection by name. Find before
// create keeps both re-runs AND state-file-loss recovery idempotent: a lost state
// file never spawns a second collection.
//
// It parses both the paginated envelope the pinned digest serves and the bare array
// older versions returned, and walks pages until one is empty or contributes no new
// ids, so a large collection list can never hide the recall collection (which would
// spawn a duplicate on every run).
func (c *Client) EnsureKnowledge(ctx context.Context, token, name, description string) (string, error) {
	type row struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	seen := map[string]bool{}
	for page := 1; ; page++ {
		out, err := c.do(ctx, "knowledge/ list", Request{Path: pathKnowledgePage(page), Token: token})
		if err != nil {
			return "", err
		}
		var rows []row
		if jerr := jsonUnmarshal(out, &rows); jerr != nil {
			var envelope struct {
				Items []row `json:"items"`
			}
			if derr := decode("knowledge/ list", out, &envelope); derr != nil {
				return "", derr
			}
			rows = envelope.Items
		}
		newIDs := false
		for _, kb := range rows {
			if kb.Name == name && kb.ID != "" {
				return kb.ID, nil
			}
			if kb.ID != "" && !seen[kb.ID] {
				seen[kb.ID] = true
				newIDs = true
			}
		}
		if len(rows) == 0 || !newIDs {
			break
		}
	}

	body, err := jsonBody(map[string]any{"name": name, "description": description})
	if err != nil {
		return "", err
	}
	out, err := c.do(ctx, "knowledge/create", Request{
		Method: "POST", Path: pathKnowledgeCreate, Token: token, Body: body,
	})
	if err != nil {
		return "", err
	}
	var created struct {
		ID string `json:"id"`
	}
	if jerr := jsonUnmarshal(out, &created); jerr != nil || created.ID == "" {
		return "", fmt.Errorf("knowledge/create returned no id (%v): %s", jerr, bodyDetail(out))
	}
	return created.ID, nil
}

// UploadFile pushes content into the file pipeline and waits for chunk-embed-store
// to complete. It returns the file id.
//
// A processing timeout is an ERROR, never a silent skip: the content was not
// indexed, and reporting otherwise would be a false green.
func (c *Client) UploadFile(ctx context.Context, token, filename, content string, timeout time.Duration) (string, error) {
	out, err := c.do(ctx, "files/ upload", Request{
		Method: "POST",
		Path:   pathFiles,
		Token:  token,
		Upload: &Upload{Filename: filename, Content: content},
	})
	if err != nil {
		return "", err
	}
	var resp struct {
		ID string `json:"id"`
	}
	if jerr := jsonUnmarshal(out, &resp); jerr != nil || resp.ID == "" {
		return "", fmt.Errorf("files/ upload returned no id (%v): %s", jerr, bodyDetail(out))
	}
	if perr := c.PollProcessed(ctx, token, resp.ID, timeout); perr != nil {
		return "", fmt.Errorf("file processing: %w", perr)
	}
	return resp.ID, nil
}

// PollProcessed polls one file's processing status until it completes or the timeout
// elapses. A timeout is an error; so is a status the server reports as failed.
func (c *Client) PollProcessed(ctx context.Context, token, fileID string, timeout time.Duration) error {
	deadline := c.now().Add(timeout)
	for {
		out, err := c.transport(ctx, Request{Path: pathFileStatus(fileID), Token: token})
		if err == nil {
			var r struct {
				Status string `json:"status"`
			}
			if jsonUnmarshal(out, &r) == nil {
				if isCompleteStatus(r.Status) {
					return nil
				}
				if isFailedStatus(r.Status) {
					return fmt.Errorf("processing reported status %q", r.Status)
				}
			}
		}
		if c.now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for file %s to process", timeout, fileID)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.after(pollInterval):
		}
	}
}

// pollInterval is how often PollProcessed re-reads the processing status.
const pollInterval = time.Second

// AddFileToKnowledge joins an uploaded file to a collection.
//
// On failure it best-effort deletes the file. Without that, a file that was uploaded
// and embedded but never joined the collection is unreachable by the clean-replace
// path (which keys off recorded collection file ids), so it and its vectors would
// orphan-accumulate on every retry. The cleanup never masks the real add failure.
func (c *Client) AddFileToKnowledge(ctx context.Context, token, kbID, fileID string) error {
	body, err := jsonBody(map[string]any{"file_id": fileID})
	if err != nil {
		return err
	}
	if _, err := c.do(ctx, "knowledge/file/add", Request{
		Method: "POST", Path: pathKnowledgeFileAdd(kbID), Token: token, Body: body,
	}); err != nil {
		_ = c.DeleteFile(ctx, token, fileID)
		return err
	}
	return nil
}

// UploadToKnowledge is the whole three-step indexing pipeline for one document:
// upload, wait for processing, join the collection. It returns the file id the
// clean-replace flow keys on.
func (c *Client) UploadToKnowledge(ctx context.Context, token, kbID, filename, content string, timeout time.Duration) (string, error) {
	fileID, err := c.UploadFile(ctx, token, filename, content, timeout)
	if err != nil {
		return "", err
	}
	if err := c.AddFileToKnowledge(ctx, token, kbID, fileID); err != nil {
		return "", err
	}
	return fileID, nil
}

// DeleteFile removes a stand-alone uploaded file and its vectors.
func (c *Client) DeleteFile(ctx context.Context, token, fileID string) error {
	_, err := c.do(ctx, "files/{id} delete", Request{
		Method: "DELETE", Path: pathFileDelete(fileID), Token: token,
	})
	return err
}

// RemoveKnowledgeFile is the clean-replace primitive: it removes a file from the
// collection AND deletes its vectors by file id and content hash.
func (c *Client) RemoveKnowledgeFile(ctx context.Context, token, kbID, fileID string) error {
	body, err := jsonBody(map[string]any{"file_id": fileID})
	if err != nil {
		return err
	}
	_, err = c.do(ctx, "knowledge/file/remove", Request{
		Method: "POST", Path: pathKnowledgeFileRemove(kbID), Token: token, Body: body,
	})
	return err
}

// ResetKnowledge is the rebuild primitive: it empties the collection while keeping
// its id, so the served model's attachment survives.
func (c *Client) ResetKnowledge(ctx context.Context, token, kbID string) error {
	_, err := c.do(ctx, "knowledge/reset", Request{
		Method: "POST", Path: pathKnowledgeReset(kbID), Token: token,
	})
	return err
}
