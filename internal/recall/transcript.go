// transcript.go is the pure per-chat transcript renderer (D-04, RECALL-01): it
// turns one Open WebUI chat document into the role-labeled text file the indexer
// uploads to the recall knowledge base. The canonical thread is reconstructed by
// walking history.currentId back through parentId with a visited-set cycle guard
// — EXACTLY OWUI's own get_message_list semantics — never the flat chat.messages
// list (a stale frontend branch view). Reasoning <details> blocks are stripped
// from assistant content before rendering (Pitfall 5: chain-of-thought bloats
// embeddings and pollutes retrieval). A chat with no reconstructable
// user/assistant messages is SKIPPED via ok=false so the caller can RECORD the
// skip — never a silent drop.
//
// PURE: string transformation only — no I/O, no os/exec, no eval (T-21-05); the
// curl bytes are parsed by the cmd tier into these JSON-tagged types (D-08).
package recall

import (
	"fmt"
	"strings"
	"time"
)

// ChatDoc is the renderer's input, parsed by the CALLER from the chat GET body:
// the REST response's top-level id/title/created_at plus chat.history. CreatedAt
// is epoch seconds, as the API returns.
type ChatDoc struct {
	ID        string      `json:"id"`
	Title     string      `json:"title"`
	CreatedAt int64       `json:"created_at"`
	History   ChatHistory `json:"history"`
}

// ChatHistory is OWUI's chat.history shape: the id of the newest message on the
// canonical branch (currentId) and the full message map keyed by message id.
type ChatHistory struct {
	CurrentID string             `json:"currentId"`
	Messages  map[string]ChatMsg `json:"messages"`
}

// ChatMsg is one message node in the history graph: its id, the parentId link the
// chain walk follows, the role ("user"/"assistant"/other), the content text, and
// the message timestamp in epoch seconds.
type ChatMsg struct {
	ID        string `json:"id"`
	ParentID  string `json:"parentId"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}

// linearThread reconstructs the canonical linear conversation by walking from
// currentID back through each message's ParentID (visited-set cycle guard, so
// corrupt or malicious chat JSON can never hang the walk — T-21-05), then
// reversing to chronological order. Mirrors OWUI's get_message_list verbatim.
func linearThread(messages map[string]ChatMsg, currentID string) []ChatMsg {
	var out []ChatMsg
	seen := map[string]bool{}
	for id := currentID; id != "" && !seen[id]; {
		m, ok := messages[id]
		if !ok {
			break
		}
		seen[id] = true
		out = append(out, m)
		id = m.ParentID
	}
	// reverse → chronological
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// reasoningOpen / detailsClose delimit the reasoning blocks OWUI embeds in
// assistant content on this box: <details type="reasoning" …>…</details>.
const (
	reasoningOpen = `<details type="reasoning"`
	detailsClose  = `</details>`
)

// stripReasoning removes every <details type="reasoning"…>…</details> span from
// s, including multiple blocks; an UNCLOSED block is stripped to the end of the
// string (fail toward dropping thought text, never toward indexing it —
// Pitfall 5). The result is whitespace-trimmed.
func stripReasoning(s string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, reasoningOpen)
		if i < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])
		rest := s[i:]
		j := strings.Index(rest, detailsClose)
		if j < 0 {
			break // unclosed block → strip to end
		}
		s = rest[j+len(detailsClose):]
	}
	return strings.TrimSpace(b.String())
}

// RenderTranscript renders one chat as the D-04 transcript document: a header of
// "# <title>" and "# <ISO-8601 of created_at> — Open WebUI chat <chat-id>", then
// the chronological user/assistant turns as "user: …" / "assistant: …" with
// reasoning blocks stripped from assistant content and non-chat roles omitted.
// ok=false signals an unreconstructable chat (missing currentId, empty/orphaned
// message map, or no user/assistant turn at all) — the caller must RECORD the
// skip; a skip is never a silent drop (D-04).
func RenderTranscript(c ChatDoc) (string, bool) {
	thread := linearThread(c.History.Messages, c.History.CurrentID)

	var turns []string
	for _, m := range thread {
		switch m.Role {
		case "user":
			turns = append(turns, "user: "+m.Content)
		case "assistant":
			turns = append(turns, "assistant: "+stripReasoning(m.Content))
		}
	}
	if len(turns) == 0 {
		return "", false // unreconstructable — caller records the skip
	}

	created := time.Unix(c.CreatedAt, 0).UTC().Format(time.RFC3339)
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", c.Title)
	fmt.Fprintf(&b, "# %s — Open WebUI chat %s\n", created, c.ID)
	for _, turn := range turns {
		b.WriteString("\n")
		b.WriteString(turn)
		b.WriteString("\n")
	}
	return b.String(), true
}

// TranscriptFilename is the deterministic per-chat upload filename
// (villa-recall-<chat-id>.txt, D-04) the clean-replace flow keys on; chat ids are
// API-returned values, never user input.
func TranscriptFilename(chatID string) string {
	return "villa-recall-" + chatID + ".txt"
}
