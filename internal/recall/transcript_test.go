// Transcript renderer tests guard the D-04 contract: the canonical thread is the
// history.currentId → parentId chain walk (NEVER the stale flat chat.messages
// view), with a visited-set cycle guard; reasoning <details> blocks are stripped
// from assistant content (Pitfall 5); a chat with no reconstructable
// user/assistant messages signals skip via ok=false (never silently dropped);
// and the header + role-labeled turn format is deterministic.
package recall

import (
	"strings"
	"testing"
)

// testDoc builds a three-turn chat whose messages map is keyed out-of-order so a
// naive map iteration could never produce the right sequence — only the
// currentId→parentId walk does.
func testDoc() ChatDoc {
	return ChatDoc{
		ID:        "chat-1",
		Title:     "Quantum gravity chat",
		CreatedAt: 1781040998, // 2026-06-10T11:36:38Z
		History: ChatHistory{
			CurrentID: "m3",
			Messages: map[string]ChatMsg{
				"m3": {ID: "m3", ParentID: "m2", Role: "assistant", Content: "It is an open problem."},
				"m1": {ID: "m1", ParentID: "", Role: "user", Content: "Explain quantum gravity."},
				"m2": {ID: "m2", ParentID: "m1", Role: "assistant", Content: "Which aspect?"},
			},
		},
	}
}

// TestRenderTranscriptChainWalk proves the renderer reconstructs chronological
// order by walking currentId back through parentId and reversing (D-04 — mirrors
// OWUI's own get_message_list), and renders the title + ISO-date header followed
// by role-labeled turns.
func TestRenderTranscriptChainWalk(t *testing.T) {
	text, ok := RenderTranscript(testDoc())
	if !ok {
		t.Fatal("RenderTranscript(ok chat) ok = false, want true")
	}
	want := "# Quantum gravity chat\n" +
		"# 2026-06-10T11:36:38Z — Open WebUI chat chat-1\n" +
		"\n" +
		"user: Explain quantum gravity.\n" +
		"\n" +
		"assistant: Which aspect?\n" +
		"\n" +
		"assistant: It is an open problem.\n"
	if text != want {
		t.Errorf("RenderTranscript =\n%q\nwant\n%q", text, want)
	}
}

// TestRenderTranscriptCycleGuard proves a parentId cycle terminates (visited-set
// guard, T-21-05) and renders each message exactly once in walk-then-reverse
// order — malicious or corrupt chat JSON can never hang the renderer.
func TestRenderTranscriptCycleGuard(t *testing.T) {
	doc := ChatDoc{
		ID:        "chat-cycle",
		Title:     "cycle",
		CreatedAt: 0,
		History: ChatHistory{
			CurrentID: "m2",
			Messages: map[string]ChatMsg{
				"m1": {ID: "m1", ParentID: "m2", Role: "user", Content: "first"},
				"m2": {ID: "m2", ParentID: "m1", Role: "assistant", Content: "second"},
			},
		},
	}
	text, ok := RenderTranscript(doc)
	if !ok {
		t.Fatal("cycle chat with real messages must still render, ok = false")
	}
	if strings.Count(text, "first") != 1 || strings.Count(text, "second") != 1 {
		t.Errorf("cycle rendered messages more than once:\n%s", text)
	}
	// Walk m2→m1 then reverse ⇒ user turn before assistant turn.
	if strings.Index(text, "user: first") > strings.Index(text, "assistant: second") {
		t.Errorf("cycle order wrong (want user before assistant):\n%s", text)
	}
}

// TestRenderTranscriptStripsReasoning proves <details type="reasoning"…>…</details>
// blocks are removed from assistant content — single, multiple, and UNCLOSED
// (strip to end) — so chain-of-thought never bloats the index (Pitfall 5, D-04).
func TestRenderTranscriptStripsReasoning(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string // exact rendered assistant line content
		absent  []string
	}{
		{
			name:    "single reasoning block stripped",
			content: `<details type="reasoning" done="true">thinking hard</details>The answer is 42.`,
			want:    "assistant: The answer is 42.",
			absent:  []string{"thinking hard", "<details"},
		},
		{
			name:    "multiple reasoning blocks stripped",
			content: `<details type="reasoning">step one</details>Part A. <details type="reasoning">step two</details>Part B.`,
			want:    "assistant: Part A. Part B.",
			absent:  []string{"step one", "step two", "<details"},
		},
		{
			name:    "unclosed reasoning block strips to end",
			content: `Visible prefix. <details type="reasoning" done="false">never closed thinking`,
			want:    "assistant: Visible prefix.",
			absent:  []string{"never closed thinking", "<details"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := ChatDoc{
				ID:    "c1",
				Title: "t",
				History: ChatHistory{
					CurrentID: "m2",
					Messages: map[string]ChatMsg{
						"m1": {ID: "m1", Role: "user", Content: "q"},
						"m2": {ID: "m2", ParentID: "m1", Role: "assistant", Content: tc.content},
					},
				},
			}
			text, ok := RenderTranscript(doc)
			if !ok {
				t.Fatal("ok = false, want true")
			}
			if !strings.Contains(text, tc.want) {
				t.Errorf("transcript missing %q:\n%s", tc.want, text)
			}
			for _, a := range tc.absent {
				if strings.Contains(text, a) {
					t.Errorf("transcript still contains stripped token %q:\n%s", a, text)
				}
			}
		})
	}
}

// TestRenderTranscriptSkips proves ok=false (the SKIP signal the caller must
// record — never a silent drop, D-04) for every unreconstructable shape: missing
// currentId, currentId absent from the map, an empty messages map, and a thread
// containing no user/assistant role at all.
func TestRenderTranscriptSkips(t *testing.T) {
	cases := []struct {
		name string
		doc  ChatDoc
	}{
		{
			name: "missing currentId",
			doc: ChatDoc{ID: "c", History: ChatHistory{
				CurrentID: "",
				Messages:  map[string]ChatMsg{"m1": {ID: "m1", Role: "user", Content: "x"}},
			}},
		},
		{
			name: "currentId not in messages map",
			doc: ChatDoc{ID: "c", History: ChatHistory{
				CurrentID: "ghost",
				Messages:  map[string]ChatMsg{"m1": {ID: "m1", Role: "user", Content: "x"}},
			}},
		},
		{
			name: "empty messages map",
			doc:  ChatDoc{ID: "c", History: ChatHistory{CurrentID: "m1"}},
		},
		{
			name: "no user/assistant roles in the chain",
			doc: ChatDoc{ID: "c", History: ChatHistory{
				CurrentID: "m2",
				Messages: map[string]ChatMsg{
					"m1": {ID: "m1", Role: "system", Content: "sys"},
					"m2": {ID: "m2", ParentID: "m1", Role: "tool", Content: "tool out"},
				},
			}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if text, ok := RenderTranscript(tc.doc); ok {
				t.Errorf("ok = true, want false (skip); rendered:\n%s", text)
			}
		})
	}
}

// TestRenderTranscriptSkipsNonChatRoles proves non-user/assistant roles inside an
// otherwise-valid thread are omitted from the rendered turns while the
// user/assistant turns around them survive (D-04).
func TestRenderTranscriptSkipsNonChatRoles(t *testing.T) {
	doc := ChatDoc{
		ID:    "c1",
		Title: "t",
		History: ChatHistory{
			CurrentID: "m3",
			Messages: map[string]ChatMsg{
				"m1": {ID: "m1", Role: "user", Content: "question"},
				"m2": {ID: "m2", ParentID: "m1", Role: "system", Content: "internal system note"},
				"m3": {ID: "m3", ParentID: "m2", Role: "assistant", Content: "answer"},
			},
		},
	}
	text, ok := RenderTranscript(doc)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if strings.Contains(text, "internal system note") || strings.Contains(text, "system:") {
		t.Errorf("non-user/assistant role leaked into the transcript:\n%s", text)
	}
	if !strings.Contains(text, "user: question") || !strings.Contains(text, "assistant: answer") {
		t.Errorf("user/assistant turns missing:\n%s", text)
	}
}

// TestTranscriptFilename proves the deterministic per-chat filename contract
// (D-04: villa-recall-<chat-id>.txt) the clean-replace flow keys on.
func TestTranscriptFilename(t *testing.T) {
	if got, want := TranscriptFilename("abc-123"), "villa-recall-abc-123.txt"; got != want {
		t.Errorf("TranscriptFilename = %q, want %q", got, want)
	}
}
