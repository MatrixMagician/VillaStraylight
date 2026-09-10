# Results: Cowork-class document work on the local model

Run 2026-09-10 on the gfx1151 dev host. Model Qwen3.6-35B-A3B UD-Q4_K_M on the pinned
rocm-7.2.4 image, second `llama-server` on :8081 with `--jinja --spec-type ngram-mod`,
ctx 131072. Claude Code 2.1.267 headless (`claude -p --dangerously-skip-permissions
--max-turns 40`), the env from `internal/agent/claude.go`. The live stack was never touched.
Transcripts in `results/T*.log`, file-list diffs in `results/T*.before|after`, the documents
the model wrote in `results/outputs/` (suffixed `.txt` so the docs grep-gate skips them).

## Verdict

**Yes, with one caveat that shapes the product.** The model completed all five tasks
autonomously, wrote correct documents for the four factual tasks, and obeyed "report only".
Its one real failure is invention under a "do not invent" instruction (T4). Speed is
Cowork-comparable: two to three and a half minutes per task.

| Task | Wall | Outcome | Misses |
|------|-----:|---------|--------|
| T1 organise + index | 171 s | All 13 files sorted into 5 folders, meeting `.txt` converted and renamed to ISO, `INDEX.md` complete, nothing deleted | Filed `CLAUDE.md` under `misc/`; filed `Sync-June-1.md` under `misc/`, not `meetings/`, so it was not renamed |
| T2 reconcile invoices vs bank | 208 s | All 7 statuses right, £50.00 short payment caught, outstanding £910.50 correct, draft excluded and explained | Prose slips: one flag names Sense/Net where it means Maas; "39 days overdue" is wrong arithmetic (it is 100) |
| T3 action items across meetings | 156 s | 7 actions, 2 decisions, the net-30 reversal, statuses inferred from later notes, found the June sync in `misc/` | Carried the notes' "INV-1045? might be Maas" uncertainty verbatim instead of resolving it against the invoice CSV |
| T4 memo from notes | 125 s | Requested shape, readable, sensible next step | **Invented facts** the notes do not contain: no per-token cost, no rate limits, no vendor lock-in, loopback-only, "a generation or two behind" |
| T5 duplicate report, report-only | 137 s | Both byte-identical pairs found with a keep rationale; the diff shows only `reports/duplicates.md` added | Did not consider the two same-minute photos near-duplicates |

Throughput from the server's `/metrics` over the five tasks: 252k prompt tokens at ~575 t/s,
12.8k generated at ~37 t/s. Claude Code re-sends its ~47k-token system prompt and tool
schemas every turn; the KV cache absorbs that, which is why a 32k context was rejected outright
(first T1 attempt, `results/T1.log` before the restart is not kept; the error was
`request (47191 tokens) exceeds the available context size (32768 tokens)`).

## What this settles

1. **The model is capable enough to build on.** Multi-step file work, cross-file reasoning,
   and arithmetic over CSVs all held. This is not the bottleneck.
2. **Hallucination under constraint is the product problem, not capability.** T4 shows the
   model will pad a document with plausible claims. A Cowork equivalent needs a grounding
   check (cite-the-source, or a second pass that strikes unsupported claims), not a bigger model.
3. **The harness works as-is.** `villa code --agent claude` needs only three things it does not
   have today: a chat-model unit rendered with `--jinja` (today only coding mode renders it),
   a context floor of ~64k (Claude Code's fixed overhead is 47k), and a folder argument. The
   research file's "widen coding mode to a tools mode" decision follows directly.
4. **Headless mode has no permission model.** `-p` with `--dangerously-skip-permissions` is what
   made autonomy possible, so this run says nothing about Cowork's ask-before-write behaviour.
   That has to come from the tool layer (the read/write split the research file describes),
   not from Claude Code's interactive prompts.
5. **Judgement misses are minor and instructable.** T1's two misfilings and T3's unresolved
   cross-reference are the kind of thing a workspace `CLAUDE.md` line fixes.

## What it does not settle

- Interactive approval UX, background or parallel tasks, and scheduling were not exercised.
- Only Markdown and CSV outputs. `.docx`/`.xlsx` generation (LibreOffice is on the host) is untested.
- One model, one quant, one run per task. No variance measured.
