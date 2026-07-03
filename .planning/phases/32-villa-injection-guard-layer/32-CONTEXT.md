# Phase 32: Villa Injection Guard Layer - Context

**Gathered:** 2026-06-19
**Status:** Ready for planning

> Captured via autonomous smart-discuss. All four grey areas resolved with their
> recommended option. Decisions follow the v1.5 roadmap decisions (STATE.md →
> Decisions) and fill the Phase-31 guard seam (`internal/websafe/guard_stubs.go`
> identity pass-throughs). Phase 32 is **research-flagged** — the planner MUST run
> `--research` (adversarial injection corpus + heuristic precision/recall eval;
> bluemonday StrictPolicy; Unicode neutralization; provenance fencing).

<domain>
## Phase Boundary

Replace the Phase-31 identity guard stubs in `internal/websafe` with real policy so
fetched web content reaching the model is **sanitized → Unicode-normalized →
provenance-fenced → screened by a heuristic injection classifier** — honestly
surfaced as **"reduces and flags, does not eliminate."** GUARD-02/03/04.

**In scope:** `sanitize` (bluemonday StrictPolicy on raw HTML), `normalize`
(invisible/zero-width/bidi neutralization + NFKC), `fence` (nonced per-page
provenance wrapper), `classify` (heuristic injection tripwire returning a verdict),
the pipeline-order fix (sanitize→normalize→fence→classify) and wiring the classifier
verdict into the loader response metadata; the adversarial+benign Go test corpus;
the honesty copy + markdown-image-exfil residual doc.

**Out of scope (later phases):** `villa verify search` egress proof + opt-in/PRIV
plumbing (**Phase 33**, PRIV-07/08/09); `status.Report` 4→5 surfacing of
guard-verdict counters, dashboard, doctor, backup (**Phase 34**, SURF-04..07). The
**model-based classifier** (PromptGuard/DeBERTa) is **deferred to GUARD-V2-01** behind
a pre-declared must-WIN precision/recall eval (would add a Python runtime/container).

</domain>

<decisions>
## Implementation Decisions

### Area 1 — Sanitization pipeline & ordering
- **bluemonday `StrictPolicy` runs on the RAW fetched HTML body**, stripping all
  tags/attributes/scripts — it **REPLACES** the naive `extractText` hand-roller (the
  audited stripper supersedes it; note `extractText`/`extractTitle` carried the
  CR-01/CR-02 robustness bugs just fixed — bluemonday is the durable replacement).
- **Pipeline order:** `sanitize (HTML) → normalize (Unicode) → fence → classify`.
  Fix the current `internal/websafe/websafe.go:155-158` stub order (`sanitize(normalize(text))`
  = normalize-first) to sanitize-first; classify runs LAST on the normalized text and
  its verdict is **used** (currently `_ = classify(text)` discards it).
- **Dependency:** add `github.com/microcosm-cc/bluemonday` (pure-Go, CGO-free,
  well-audited) to go.mod. Confirm the static `CGO_ENABLED=0` build still works
  (websafe runs in distroless), `internal/websafe` stays a pure core, and
  `TestSeamGrepGate` stays green (no backend/host literals leak).
- **Title:** keep `extractTitle` for `metadata.title`, but route it through the same
  sanitize+normalize so a malicious title can't carry markup/Unicode tricks.

### Area 2 — Unicode normalization scope
- **Transforms:** strip zero-width/invisible runes (ZWSP/ZWNJ/ZWJ/BOM/word-joiner),
  neutralize **bidirectional control chars** (the "Trojan Source" class: LRO/RLO/LRE/
  RLE/PDF/LRI/RLI/FSI/PDI), and apply **NFKC** (fold fullwidth/compatibility variants).
- **Homoglyph/confusables:** **conservative for v1.5** — invisibles + bidi + NFKC cover
  the dangerous classes; full Unicode TR39 confusables-skeleton folding is documented as
  a later refinement (it is heavy and can corrupt legitimate non-Latin content).
- **Library:** `golang.org/x/text` (`unicode/norm` for NFKC, rangetables/`unicode` for
  the control-char classes) — pure Go, already a likely transitive dep.
- **Readability:** neutralize attack characters WITHOUT mangling legitimate content —
  the goal is to defang, not to destroy text (conservative).

### Area 3 — Provenance fence (GUARD-03)
- **Format:** wrap each page's sanitized+normalized content in a **nonced** fence —
  e.g. `[UNTRUSTED_WEB_CONTENT nonce=<hex>] … [/UNTRUSTED_WEB_CONTENT nonce=<hex>]` —
  preceded by a short preamble stating the enclosed text is untrusted web DATA, not
  instructions.
- **Nonce:** `crypto/rand` per fetch — unguessable, so injected content cannot forge the
  closing fence to "break out."
- **Scope:** **per-page** (each fetched page wrapped independently — page-scoped provenance).
- **Where:** in the `fence()` hook, after sanitize+normalize, before the `page_content`
  is returned to OWUI (so exactly what OWUI embeds/shows the model is fenced).

### Area 4 — Heuristic classifier & outcome surfacing (GUARD-04)
- **Heuristic, NOT a model** (roadmap-locked): a curated, deterministic, pure-Go rule set
  matched on the **normalized** text — imperative override phrases ("ignore previous/above
  instructions", "disregard", "you are now", "system prompt", "developer mode"),
  role/delimiter spoofing (`<|im_start|>`, `###system`, `assistant:`/`system:` turn
  markers), and secret/exfil-probe phrasing. Case- and whitespace-insensitive.
- **Action: flag-not-block** — keep the sanitized+fenced content, **annotate a guard
  verdict** (detected + matched rule names); NEVER silently drop. (No block/quarantine by
  default — that would be a dishonest "we stopped it" posture; the egress bound, Phase 33,
  is the real backstop.)
- **Verdict plumbing:** widen `classify` from `bool` to a verdict value (e.g.
  `detected bool`, `rules []string`) and wire it into the loader response **metadata** so
  Phase 34 can surface guard-verdict counters in `status`/dashboard.
- **Eval + honesty:** ship a Go **adversarial injection corpus** (recall) + a **benign
  sample set** (precision) as tests asserting the heuristic flags injections without
  over-flagging benign content; the package doc + any operator-facing copy state
  **"reduces and flags, does not eliminate"** (grep-ban "injection-safe"); the
  **browser-side markdown-image zero-click exfil channel is documented as a known
  residual** (bypasses container egress — operator's browser renders the image), NOT
  claimed closed (GUARD-04).

### Claude's Discretion
- Exact bluemonday policy tuning (StrictPolicy is the floor; whether to also drop
  comments/CDATA is implementation detail).
- The precise verdict struct shape + how it threads through `fetchOne`/`Loader.Load`/the
  `/load` response metadata.
- The exact heuristic rule corpus contents + thresholds (planner/researcher informs;
  must flag the shipped adversarial corpus).
- NFKC-vs-NFKD choice and the exact invisible/bidi rune set (research-informed).

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets / Seam to fill
- `internal/websafe/guard_stubs.go` — the four identity stubs (`sanitize`, `normalize`,
  `fence`, `classify`) Phase 32 replaces with policy. Single, clean insertion point.
- `internal/websafe/websafe.go:155-158` — the guard-seam call site in `fetchOne`
  (`text = sanitize(normalize(text)); text = fence(text); _ = classify(text)`); fix the
  order + use the classify verdict. `extractText`/`extractTitle` (just CR-fixed) are
  replaced by bluemonday for sanitization.
- `internal/websafe/loader.go` — the `/load` response (page_content + metadata.source/
  title); the guard verdict threads into metadata here for Phase-34 surfacing.
- `internal/websafe` is a **pure core** (network injected as a Deps func) and must stay
  TestSeamGrepGate-clean + statically buildable (distroless) — bluemonday + x/text are
  pure Go, CGO-free.

### Established Patterns
- Pure-core + injectable seam; orchestrate is the only impure module.
- Honesty-by-construction; grep-ban on "injection-safe" copy (the two governing claims
  stay distinct: outbound-bounded is proven, injection-immunity is never claimed).
- Defense-in-depth + Unicode normalization BEFORE fencing; fences are one layer (a hint),
  the egress bound (Phase 33) is the real backstop.

### Integration Points
- The guard runs entirely inside `internal/websafe` (between fetch and the `/load`
  response) — no new container, no OWUI change. The verdict surfaces later via Phase-34
  `status.Report`.

</code_context>

<specifics>
## Specific Ideas

- **Order is load-bearing:** strip (bluemonday) → normalize (defang Unicode) → fence
  (nonced) → classify (flag) — normalize BEFORE fence so attack chars can't smuggle past
  the fence; classify on the normalized text so homoglyph/zero-width tricks don't evade it.
- **`crypto/rand` nonce** makes the fence non-forgeable — the single most important fence
  property.
- **Flag-not-block + honest copy** is the product's posture: it reduces + flags + bounds,
  never claims to eliminate injection.

</specifics>

<deferred>
## Deferred Ideas

- **Model-based classifier** (PromptGuard/DeBERTa sidecar) → **GUARD-V2-01**, behind a
  pre-declared must-WIN precision/recall eval vs this heuristic baseline (adds a Python
  runtime/container — breaks single-static-binary).
- **Full Unicode TR39 confusables-skeleton homoglyph folding** → later refinement
  (heavy; risk of corrupting legitimate non-Latin content).
- **`villa verify search` egress proof + opt-in/PRIV plumbing** → **Phase 33**.
- **Guard-verdict surfacing** (status/dashboard/doctor counters) → **Phase 34**.
- **Closing the markdown-image exfil channel** → out of scope (documented residual; it
  bypasses container egress entirely).

</deferred>

---

*Phase: 32-villa-injection-guard-layer*
*Context gathered: 2026-06-19 (autonomous smart-discuss)*
