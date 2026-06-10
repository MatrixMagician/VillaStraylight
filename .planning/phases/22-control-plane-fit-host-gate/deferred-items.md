# deferred from 22-03
- gofmt/goimports drift: cmd/villa/bench_compare.go (comment list marker), cmd/villa/verify_memory_test.go (struct field alignment) — pre-existing, out of plan scope

# deferred from 22-04 (on-hardware UAT)
- health:villa-qdrant.service / health:villa-embed.service rows probe the CHAT model endpoint: internal/status/status.go:376 passes the single villa-llama `endpoint` to d.Health() for EVERY non-OWUI service row, so a stopped villa-embed still renders "health PASS /health is ready (200)" in status/doctor (observed live during the 22-04 negative control). Honesty is preserved at the doctor verdict level (MEM-DOC-residency's D-10 is-active gate catches the down unit -> overall WARN), but the per-row health label is a false-green. Pre-existing since Phase 19 (memory services joined serviceUnits); fix belongs to the Phase 23 status memory-rows work (schema 2->3, per-service health endpoints / N/A pattern alongside the offload N/A fix already carried there).
