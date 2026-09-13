---
description: "--json and dashboard contracts are byte-frozen"
globs: ["cmd/villa/**/*.go", "cmd/villa/testdata/**", "internal/status/**", "internal/dashboard/**"]
priority: 60
prompt: ["golden", "--json", "schema", "testdata", "dashboard"]
---
The `--json` output and the dashboard API are byte-frozen by golden tests (`cmd/villa/testdata/*.golden`,
a couple `*.golden.json` — nothing ends `.json.golden`). Evolve them append-only and bump the schema
version in the same change; a removed or renamed field breaks a consumer that cannot be recompiled
alongside it.

Refreeze deliberately with `go test … -update`, and read the resulting diff before committing it. A
golden that changed in a way you cannot explain is a regression that just relabelled itself as an
expectation.

`internal/status` holds the shared read-model that both the CLI and the dashboard fold; it must not
import a store package directly. Projection from a store into a `status` type happens in `cmd/villa`
and is wired through `Deps`, so the gating stays in the core.
