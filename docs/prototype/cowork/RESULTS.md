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

## Addendum: the sandbox as a libkrun microVM (same day)

Question: does `podman --runtime=krun` give Cowork's VM boundary while staying a Quadlet
container? Probes in `krun.sh` (crun baseline vs krun) and `krun-task.sh` (task T2 run
end-to-end inside the VM). Log in `results/krun.log` and `results/krun-T2.log`, the report the
model wrote in `results/outputs/krun-reconciliation.md.txt`. Host packages added:
`crun-krun 1.28`, `libkrun 1.19.0`, `libkrunfw 5.5.0` (Fedora 44 updates).

| Probe | crun | krun |
|---|---|---|
| start + `uname -r` | 0.16 s, host kernel 7.1.13 | 0.50 s, **guest kernel 6.12.91** |
| `/dev/kfd`, `/dev/dri` | absent | absent |
| rw folder grant (`Volume=…:Z`) | ok | ok |
| llama-server over the `villa` network by name | ok | ok |
| egress on the `villa` network | **open** | **open** |
| egress on a `--internal` network | not run | **blocked**, llama still reachable |
| Claude Code binary bind-mounted, `--version` | ok | ok |
| T2 reconcile, headless, inside | not run | **correct, 88 s** (fastest T2 of the day) |

**Verdict: yes.** The same image, volume grant and network work under krun unchanged, with a
separate guest kernel and a half-second start. That closes the VM question at parity with
Cowork's topology rather than at "weaker".

Three things the run taught, all of which a real unit must encode:

1. **`--user` is ignored under krun.** The guest process is always uid 0; with
   `--userns=keep-id` its files then appear as uid 1000 inside the guest, which trips Claude
   Code's temp-dir ownership check. Without keep-id ownership is consistent (0 inside, the
   user on the host). `setpriv`, `su` and `runuser` are not in the base image and the
   setuid `sudo` is denied, so privilege cannot be dropped inside. Root-in-VM is acceptable
   because the VM is the boundary, but it is a fact to document, not to discover.
2. **Claude Code's root guard needs `IS_SANDBOX=1`.** Otherwise `--dangerously-skip-permissions`
   refuses as root. Also give it `CLAUDE_CODE_TMPDIR` under a fresh directory.
3. **The `villa` network is not internal.** A sandbox unit needs its own `--internal`
   network with the inference container joined to it, or every task has egress.

Not measured: memory and CPU headroom under `--memory 4g --cpus 4` (it sufficed), and the
cost of a VM per task versus a long-lived one.

## Addendum: the chosen harness on the same tasks (same day, later)

Question ([the chosen-harness prototype](https://github.com/MatrixMagician/VillaStraylight/issues/166)):
does Crush v0.76.0, villa's pinned coding agent, do the five tasks as well as the Claude Code
stand-in did, inside the sandbox? Setup in `crush-task.sh`: the villa-owned `crush` binary
bind-mounted into `localhost/villa-proto-sandbox:office` (the office-formats research's image,
1.54 GB, built locally), `--runtime=krun`, an `--internal` network carrying the :8081 server,
one rw folder grant, `crush run -q` (non-interactive Crush auto-approves every tool call; the
approval gate is the bridge's job and was not exercised here), `crushcfg/crush.json` with the
web-reaching tools disabled. Transcripts in `results/crush-T*.log`, outputs in
`results/outputs/crush/`, token deltas from the server's `/metrics`.

| Task | Wall (stand-in) | Outcome | Misses |
|------|-----:|---------|--------|
| T1 organise + index | 77 s (171 s) | 13 files into 5 folders, ISO renames, `INDEX.md` complete, nothing deleted; kept the rules files at top level | `Sync-June-1.md` under `notes/`, not `meetings/`, so not renamed (the stand-in made the same call) |
| T2 reconcile | 55 s (208 s) | All 7 statuses right, £50.00 short payment caught, draft flagged | **Totals row wrong**: total paid £9,221.75 (is £8,761.25), so outstanding £1,425.00 (is £1,885.50 with the draft, £910.50 without); "14 days overdue" assumes terms the sources do not state |
| T3 action items | 55 s (156 s) | 8 actions, the net-30 reversal, statuses inferred from later notes, sources cited | Same unresolved `INV-1045?` cross-reference as the stand-in |
| T4 memo | 22 s (125 s) | Requested shape; **no invented facts**: every benefit and risk traces to a note | Mild inference ("higher internal support costs", a two-user pilot) inside the recommendation the task asked for |
| T5 duplicates, report-only | 50 s (137 s) | All three byte-identical pairs (including the two rules files the seed left), keep rationale, diff shows only the report | Same-minute photos not considered near-duplicates (as before) |

**Verdict: parity or better on four tasks, and faster on all five.** Crush is three to six times
quicker than the stand-in on the same model and server, and its fixed prompt cost is small
(the smoke test's whole two-turn exchange was 9.5k prompt tokens; Claude Code's system prompt
alone was 47k). The 64k context floor was a stand-in artefact; 32k is plausible for Crush and
the tools-mode ticket should take the floor from the catalog entry, measured, not from this note.
The one regression is T2's arithmetic in the totals row, which the stand-in also got partly
wrong (its "39 days"). That is the grounding check's job, and it caught it (below).

### The grounding audit, measured

`grounding-audit.sh` is the second-pass claim audit [the grounding decision](https://github.com/MatrixMagician/VillaStraylight/issues/162)
describes, run by hand: one chat completion per document, the sources being the files the task
read, `temperature 0`, thinking disabled (with thinking on, Qwen3.6 spent the whole 4096-token
budget reasoning and returned no content; the audit call must set `enable_thinking: false`).
Outputs in `results/audit-*.txt`.

| Document | Claims | Unsupported | Wall | Read |
|---|--:|--:|--:|---|
| Stand-in's T4 memo (the one that invented facts) | 19 | **12** | 13 s | Every named invention caught: per-token cost, lock-in, rate limits, "a generation or two behind", remote access |
| Crush's T4 memo | 12 | 0 | 13 s | "The auditor found none", not "grounded" |
| Crush's T2 reconciliation | 23 | 4 | 42 s | **2 true**: total paid (it computed the right £8,761.25) and the outstanding derived from it. **2 strict**: "Q2 2026 (April – June)" as the period, "14 days overdue" resting on unstated terms. The "generated 2026-06-30" date also flagged |
| Crush's T3 action items | 17 | 1 | 25 s | One flag, arguably right: the report asserted the `INV-1045?` ambiguity belongs to the Ono-Sendai quote, which the note does not say |

So the audit catches inventions and arithmetic slips at a cost of one call per document, well
under a minute each, with a strict-reading false-positive rate of roughly one to three flags
per correct document. Exit `2` on any flag is the right default; the flags are short and a
human clears them in seconds. The classifier is the same model; a clean audit is evidence,
not proof.

### The xlsx task (the office-formats prototype)

[The xlsx prototype](https://github.com/MatrixMagician/VillaStraylight/issues/169) asked
whether the model can write a correct openpyxl script, recalculate with LibreOffice, and read
its own result back. `tasks/T6-xlsx.txt`, run last in the same workspace.

**Result: the workbook was written correctly in shape, the model's own recalculation route hung
the task, and its formula would have produced the wrong answer.** Three findings:

1. **openpyxl authoring works.** `_build_reconciliation.py` built two sheets, `SUMIF` per
   invoice, `=D2-E2` outstanding, a `SUM` totals row. Structure exactly as asked.
2. **Model-written recalculation is the failure mode the research predicted.** The model
   skipped the one-line `soffice --convert-to` route, wrote a UNO script that spawns
   `soffice --accept=socket…` as a listener, then waited on that background job with
   `job_output wait=true`; the task sat there until the 1200 s timeout (`exit=124`). The
   research's prescribed command, run afterwards by hand under krun with no network
   (`soffice -env:UserInstallation=file:///tmp/lo --headless --calc --convert-to xlsx`),
   recalculated the same file in 3 s and rendered the PDF in the same run. The spec must ship
   the recalc and render as villa-provided scripts the model invokes, never as code it writes.
3. **The formula is wrong for the data, and only a read-back would have shown it.**
   `SUMIF(Bank!B:B,"*"&A2,Bank!C:C)` matches `INV-1041` against bank descriptions that carry
   `INV1041` (no hyphen) or no reference at all (`SENSE NET PAYMENT`, `MAAS BIOLABS`), so every
   `paid_gbp` recalculates to 0 and the totals row reads £10,646.75 outstanding. The Markdown
   reconciliation (T2) got the same matching right by reasoning; a formula cannot. The
   verify-by-reading-back step the task asked for is what catches this, and the model never
   reached it. Not exercised: reading the PDF render (the :8081 server ran without the vision
   projector).

Also observed: `which` is not in the image (the model coped); `.crush/` (Crush's per-project
SQLite) lands in the workspace, so the bridge must point Crush's data dir at the tmpfs, not the
grant.

### What this settles for the spec

- Harness: Crush inside the sandbox holds up on document work; faster and cheaper in context
  than the stand-in, same judgment misses, one arithmetic slip the audit caught.
- Grounding: the second-pass audit works on this model, with thinking off, and its two numbers
  are 12/12 named inventions caught and about one to three strict flags per correct document.
- Office formats: author with openpyxl, recalc and render with villa-shipped scripts, and make
  the read-back mandatory: an xlsx task is not done until its recalculated values were read.
