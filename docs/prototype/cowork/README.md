# Prototype: can the local model do Cowork-class document work?

**PROTOTYPE — throwaway.** Lives on branch `prototype/cowork`; nothing here ships.

## Question

Anthropic's Claude Cowork is an agent given a folder and asked to do knowledge work
(organise files, reconcile spreadsheets, turn notes into reports, draft memos). VillaStraylight
already has `villa code --agent claude`, which launches Claude Code against the local
llama-server. Does the local model (Qwen3.6-35B-A3B UD-Q4_K_M on ROCm) do Cowork-class
work through that path well enough to build a product surface on?

## Shape

Not a UI or state-machine prototype: an on-hardware capability experiment. The live stack is
untouched (the read-only protocol: a second `llama-server` on :8081 with the unit's own args
plus `--jinja`). Claude Code runs headlessly (`claude -p`) with the exact env
`internal/agent/claude.go` sets, port aside.

```
./seed.sh          # (re)create workspace/ — a messy Downloads-like folder. WIPE ME.
./serve.sh start   # second llama-server on :8081, 128k ctx, --jinja
./run.sh [T1..T5]  # run tasks; transcripts + file-list diffs land in results/
./serve.sh stop
```

Tasks in `tasks/`: T1 organise + index, T2 invoice/bank reconciliation, T3 action items
across three meetings, T4 memo from notes, T5 duplicate report (report-only: tests that
"don't touch anything" is obeyed).

## Findings

See `RESULTS.md`.
