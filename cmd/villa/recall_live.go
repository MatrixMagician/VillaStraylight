// recall_live.go holds what remains of the recall verb's live wiring after the Open
// WebUI protocol moved to internal/openwebui: the collection's identity and the
// size-aware upload allowance.
//
// The twelve REST drives that used to live here are gone. They were injected into
// the recall command one by one, each seam a rename of the function beside it; they
// are now methods on one protocol client seamed at the transport
// (cmd/villa/openwebui_live.go binds it).
package main

import "time"

// recallKnowledgeName is the villa-managed Knowledge collection's name — the
// find-or-create key owuiEnsureKnowledge matches on, and the `name` field of the
// meta.knowledge attachment item.
const recallKnowledgeName = "Villa Recall — Past Conversations"

// recallKnowledgeDescription is the villa-managed collection's description (set
// once at create; OWUI embeds it as KB metadata, which is why villa-embed must be
// reachable at create time — Pitfall 7).
const recallKnowledgeDescription = "villa-managed semantic index of past Open WebUI conversations (villa recall)"

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
