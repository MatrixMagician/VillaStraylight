# Pitfalls Research

**Domain:** Operability features (doctor / backup-restore / saved-bench / usage-tracking / TUI install / alt-ROCm) for a rootless-Podman, single-static-binary, zero-telemetry local AI control plane (VillaStraylight v1.2)
**Researched:** 2026-06-07
**Confidence:** HIGH for this-system invariants (grounded in source: `seam_test.go`, `openwebui.go`, `backend_rocm.go`, `rocm-policy.json`, `bench.go`, `metrics/llamacpp.go`, `config/villaconfig.go`); MEDIUM for external Podman volume mechanics (WebSearch-verified, not Context7).

These are pitfalls specific to ADDING these six features to THIS system without weakening its shipped invariants: single static CGO-free binary, strictly-local/zero-telemetry, config-is-single-source-of-truth, `--json`/dashboard outputs byte-frozen by golden tests, offload-asserting (no false-green), loopback-only binds, no shell interpolation, backend literals seam-locked (`TestSeamGrepGate`).

## Critical Pitfalls

### Pitfall 1: Backup/restore mangles the Open WebUI volume via rootless UID-mapping (BAK-01)

**What goes wrong:**
A naive backup (`tar` over `~/.local/share/containers/storage/volumes/villa-openwebui/_data`, or `podman volume export` run while the stack is up) captures files owned by the in-namespace UID. On restore — especially onto a different machine, a different `subuid` range, or after a Podman reset — the files land with the wrong host ownership and Open WebUI cannot read/write `/app/backend/data` (SQLite DB + uploads). Chats appear lost or the container crash-loops on a read-only DB.

**Why it happens:**
Rootless Podman maps the host user to an in-container UID via user namespaces; the on-disk owner is `subuid`-offset, not the host UID. `subuid`/`subgid` ranges differ per host. Capturing raw bytes preserves numeric ownership that is meaningless on the target. The Open WebUI volume is mounted `:Z` (PRIVATE SELinux label) — a raw restore also loses/!corrupts the SELinux context.

**How to avoid:**
- Use `podman volume export villa-openwebui > archive.tar` / `podman volume import` (Podman normalizes ownership through the namespace) rather than host-path `tar`, OR perform host-path tar inside `podman unshare` so ownership is namespace-relative on both ends.
- On restore, re-apply the SELinux private label (recreate the volume via the existing Quadlet `:Z` mount path rather than writing into a pre-existing context). Reuse `internal/orchestrate`'s volume render — do not hand-create the volume.
- After import, `podman unshare chown -R` is the documented repair if ownership is still off; surface this as remediation, never silently.
- Treat the SQLite DB as the integrity unit: quiesce writes (stop `villa-openwebui` first) before capture.

**Warning signs:**
Restore "succeeds" but Open WebUI shows an empty chat list, logs `attempt to write a readonly database` / `permission denied` on `/app/backend/data/webui.db`, or `ls -n` inside `podman unshare` shows files owned by `65534`/an unexpected high UID.

**Phase to address:** Backup/restore phase (BAK-01).

---

### Pitfall 2: Restoring into a running stack corrupts the live SQLite DB (BAK-01)

**What goes wrong:**
Restore overwrites `/app/backend/data` while `villa-openwebui` is running. SQLite has an open WAL/journal; replacing the DB file out from under a live process yields a corrupt database, lost recent chats, or a half-old/half-new state. The reverse — backing up a live, mid-write DB — captures a torn snapshot that restores as corrupt.

**Why it happens:**
Operators expect volume restore to be atomic like a file copy. It is not, when a containerized process holds the file open. This mirrors the v1.0/v1.1 transactional-discipline lesson (`backendswap` capture→prove→cutover→rollback): mutating a live service must be ordered.

**How to avoid:**
- Order restore the way `internal/backendswap` orders a backend switch: **stop** `villa-openwebui` (and ideally the network) → swap the volume → **restart** → **prove** health (Open WebUI `/health` 200 AND DB readable) → only then declare success; on any failure, restore the prior volume verbatim (keep the pre-restore archive as the rollback frame).
- Same ordering for backup: stop the service, capture, restart — or document that a live backup is best-effort and re-capture after stopping for a guaranteed-consistent copy.
- Reuse `orchestrate`'s systemd seam (`systemctl --user stop/start`) — do not shell out anew; the impure surface must stay in `internal/orchestrate`.

**Warning signs:**
`SQLITE_CORRUPT` / `database disk image is malformed` after restore; a `-wal`/`-shm` sidecar present in the archive; restore that doesn't stop the container at all.

**Phase to address:** Backup/restore phase (BAK-01).

---

### Pitfall 3: Backup silently swallows (or silently drops) multi-GB model weights (BAK-01)

**What goes wrong:**
"Back up everything" sweeps in the `villa-models.volume` (GGUF weights — tens of GB). Either the archive balloons to 30–60 GB and the operation appears to hang / fills the disk, or the dev quietly excludes models and a "full restore" later comes up with no model and a broken stack — a false sense of recoverability.

**Why it happens:**
`villa-models.volume` is a **bind volume** (`Type=none`/`Device=`/`Options=bind` in `volume.tmpl`) pointing at host model storage, distinct from the Open WebUI named volume. It's tempting to treat both volumes uniformly. Models are large, reproducible (re-pullable from the catalog), and version-pinned — fundamentally different backup economics from chats/settings.

**How to avoid:**
- Scope BAK-01 explicitly to **config + Open WebUI data only** (recovery of irreplaceable state: chats, settings, prompts). Record the model identity (name/quant/digest from config) in the backup **manifest**, not the bytes — restore re-pulls weights via the existing `download`/`catalog` path.
- Make exclusion of model bytes an explicit, documented decision in the archive manifest, not an accident.
- Refuse-with-remediation if free disk < archive estimate before starting (consistent with preflight discipline).

**Warning signs:**
Backup archive size in tens of GB; backup duration in minutes; restore that produces a configured-but-modelless stack; no manifest distinguishing "captured" vs "re-pull on restore".

**Phase to address:** Backup/restore phase (BAK-01).

---

### Pitfall 4: Restore across version skew silently breaks (BAK-01)

**What goes wrong:**
A backup taken under one Open WebUI image digest is restored under a newer one (or vice-versa). The DB schema migrated forward; restoring an old DB into a newer image either auto-migrates unexpectedly or fails to start. Worse: restoring config that pins an old model/quant/backend onto a host whose firmware/kernel no longer satisfies the ROCm floors yields a config that no longer passes preflight.

**Why it happens:**
Backups are assumed portable across time. They carry both data (DB) and a `config.toml` that encodes hardware-and-image-specific decisions. The Open WebUI image is digest-pinned (`openWebUIImage`), and config encodes `backend`/`model`/`quant` that were valid for a specific host+policy snapshot.

**How to avoid:**
- Stamp the backup **manifest** with: villa version, Open WebUI image digest, model/quant/backend, host fingerprint (kernel, firmware, gfx), and a backup schema version.
- On restore, compare manifest vs current; **WARN-with-remediation** on skew (image digest changed, firmware now below `rocm-policy.json` floor, etc.) instead of restoring blind. Restoring config should re-run preflight, not assume validity.
- Restore config through the existing `config.SaveVilla` path (0600, traversal-guarded), never by dropping a raw file — and re-validate against `recommend`/`preflight`.

**Warning signs:**
Open WebUI fails to start after restore with migration errors; restored config selects `rocm` on a host whose firmware is now `firmwareDeny`-listed; no version/digest in the archive.

**Phase to address:** Backup/restore phase (BAK-01).

---

### Pitfall 5: Usage tracking accidentally becomes telemetry / leaks data off-box (USAGE-01)

**What goes wrong:**
Cumulative usage tracking is built in a way that (a) phones home for "anonymous stats," (b) writes prompt/response content to a usage store, or (c) the dashboard exposes a usage endpoint that isn't loopback-only — any of which violates the project's stated core value (strictly local, zero telemetry, STRIDE-secured loopback) and would fail the existing PRIV gates.

**Why it happens:**
"Usage analytics" is a near-universal SaaS pattern; the muscle memory is to ship counts somewhere. Token counts feel innocuous, but content-adjacent fields (prompts, model responses, per-chat detail) leak the very privacy the product sells.

**How to avoid:**
- Persist **counts only** (cumulative prompt/eval tokens, tok/s aggregates, run timestamps) — never prompt or response text, never per-conversation content.
- Source the numbers from the existing loopback `/metrics` scrape (`internal/metrics`), which is already bounded (`io.LimitReader`, 2s timeout) and local-only.
- Any new dashboard surface binds `127.0.0.1` via `net.JoinHostPort` (PRIV-01) and sits behind the `requireSameOrigin` `/api` guard — assert with a loopback test.
- Add a no-outbound assertion in `status`-style checks: usage tracking must add zero new outbound destinations (the only permitted egress remains image/model pulls).

**Warning signs:**
Any `http.Post`/new network dial in the usage path; prompt text or chat IDs in the usage store; a usage endpoint bound to `:port`/`0.0.0.0`; a new env var like `ANALYTICS_URL`.

**Phase to address:** Usage-tracking phase (USAGE-01). Verify in any security-review checkpoint.

---

### Pitfall 6: Usage store grows unbounded / races on concurrent writes (USAGE-01)

**What goes wrong:**
Usage is appended every scrape/interval to a file that grows forever (append-only log → multi-hundred-MB after months), and the dashboard poll loop + CLI + a background tracker all write the same file, producing interleaved/corrupt records or lost updates.

**Why it happens:**
Append-on-every-tick is the easiest first implementation. Concurrency is non-obvious because the dashboard server is long-lived and the CLI runs fresh from `./villa` — two processes, one store. The codebase already guards its one cached dashboard value with a mutex, but a cross-process store needs file-level discipline a mutex doesn't give.

**How to avoid:**
- Store **aggregates with bounded cardinality** (rolling daily/period buckets, fixed-size ring, or a single cumulative row updated in place) rather than an unbounded event log; cap retention with an explicit rotation policy.
- Single-writer ownership: designate the long-lived `villa-dashboard.service` (or one tracker) as the writer; CLI reads. If multiple writers are unavoidable, use atomic write (write-temp-then-rename) + `flock` advisory lock; never partial-overwrite in place.
- Store under XDG (`$XDG_DATA_HOME/villa/`), 0600, traversal-guarded — mirror the `config` save discipline; do not co-mingle with `config.toml` (config stays the single source of *configuration* truth, not a metrics sink).

**Warning signs:**
Usage file size grows linearly with uptime; truncated/garbled trailing records; two villa processes writing concurrently in `lsof`; dashboard restart loses or doubles counts.

**Phase to address:** Usage-tracking phase (USAGE-01).

---

### Pitfall 7: `villa doctor` reports false-green (DOCTOR-01)

**What goes wrong:**
`doctor` checks `systemctl is-active` and an Open WebUI `/health` 200 and prints "all healthy" — while inference is silently running on CPU (offload failed), the model bound is missing, or the configured backend doesn't match what's actually loaded. This is exactly the false-green the whole system is architected against (D-11, offload-asserting). A green `doctor` that hides a CPU fallback is worse than no doctor.

**Why it happens:**
Health checks default to liveness (process up, port answers). The codebase explicitly distinguishes liveness from *offload-assertion*: `metrics` notes tok/s gauges are last-window snapshots, NOT an "is generating?" signal (Pitfall 3 in `metrics/llamacpp.go`); `bench`/`backendswap` gate on a real generation probe + `ResidencyProof`. `doctor` must inherit that rigor, not regress to is-active.

**How to avoid:**
- Compose `doctor` from the **existing honest cores**: re-run `internal/preflight` against the running install, fold `internal/status` (which already reflects the configured backend, not a hardcoded Vulkan), and reuse the residency/offload assertion path — a confident CPU-fallback is a FAIL, an unevaluable signal is a WARN, never a silent PASS (the typed-Unknown → WARN discipline).
- Three-state output (PASS/WARN/FAIL) with refuse-with-remediation on every non-PASS, mirroring preflight `CheckResult`.
- Detect **drift**: on-disk Quadlet units vs what config would render now (reuse `orchestrate.Reconcile`); backend in config vs backend actually resident; model in config vs model present on the volume.
- Do NOT re-type backend marker literals to "check the backend" — route through `internal/inference`/`status`, or `TestSeamGrepGate` fails the build.

**Warning signs:**
`doctor` passes while `bench`/`status` show resident==false; a `doctor` check that greps `is-active` and stops; backend marker strings (`ROCm0`, `Vulkan0`, `HSA_OVERRIDE…`, image tags) appearing in a new `doctor` file outside the seam.

**Phase to address:** Doctor phase (DOCTOR-01).

---

### Pitfall 8: Saved bench reports drift from / break the byte-frozen golden contract (BENCH-03)

**What goes wrong:**
A persisted bench report is serialized as a new JSON/`--json` shape that isn't golden-frozen (so it silently mutates across versions and breaks `--compare` of old vs new), OR the dev edits an existing frozen `--json`/dashboard schema in place (reorders fields, blends pp/tg) to fit saved reports — breaking the append-only contract guarded by `cmd/villa/testdata/*.golden*` and the dashboard goldens.

**Why it happens:**
Saved-report formats feel like "just another struct." But the comparison feature's whole value is cross-version stability, and the system's hard rule is that machine-readable output evolves **append-only + schema-bump**, re-frozen intentionally once (the Phase-10 discipline). It's easy to forget the saved-report format IS a frozen contract the moment a second version reads it.

**How to avoid:**
- Define the saved-report schema with an explicit `schema_version` from day one; freeze it with a golden the same way `--json` outputs are frozen; evolve only by append + bump, refreeze with `go test … -update` deliberately.
- Keep pp and tg **separate** in the persisted shape (never a blended number) — the `bench` core already carries them separately end-to-end; the report must not collapse them.
- `--compare` reads via the versioned schema; older reports compare on the common (lower) schema, never silently misalign fields.
- If saved reports surface in the dashboard, that addition is append-only to the existing dashboard golden, schema-bumped, refrozen once.

**Warning signs:**
A persisted report with no version field; a golden diff that reorders/removes fields rather than appends; pp and tg merged; `--compare` between two villa versions throwing a parse error or mismatching columns.

**Phase to address:** Saved-bench phase (BENCH-03).

---

### Pitfall 9: `bench --compare` compares non-comparable runs (BENCH-03)

**What goes wrong:**
`--compare` shows a tok/s delta between two saved runs that used different models, quants, prompts, `n_predict`, seeds, host firmware, or backends — presenting a meaningless number as a real regression/win. This betrays the v1.1 honesty constraint (the whole point of the Phase-9 A/B core was that a printed delta is *trustworthy*).

**Why it happens:**
Saved reports accumulate over time across config changes. The comparison UI invites "compare any two." But throughput is only comparable when the `BenchSpec` (prompt, NPredict, Seed, Temp, Reps/Warmup) AND the environment (model/quant, host, backend) match — exactly the reproducibility fields `BenchSpec` already pins for a single A/B.

**How to avoid:**
- Persist the **full `BenchSpec` + environment fingerprint** (model, quant, backend, image digest, host kernel/firmware/gfx, villa version) with every saved run.
- `--compare` refuses-or-WARNs when the comparison axis differs on anything but the one dimension being compared (e.g. backend-vs-backend requires identical model/quant/prompt/host); a delta across mismatched specs is labeled "not comparable," never printed as a clean number.
- Carry the residency/void state forward: a saved run that was VoidExhausted (offload not proven) must not be silently compared as a valid band.

**Warning signs:**
A clean delta between runs whose models/quants differ; saved reports lacking environment fingerprint; `--compare` with no comparability guard; a regression that's really a prompt/n_predict change.

**Phase to address:** Saved-bench phase (BENCH-03).

---

### Pitfall 10: The TUI breaks the single-static-binary / CGO-free / non-TTY contract (INSTALL-01)

**What goes wrong:**
A TUI library pulls in CGO (breaking the static, easy-distribution binary), bloats the binary, or the TUI is hard-wired so `villa install` is unusable over SSH-without-TTY, in CI, or piped — regressing the flag-driven path that v1.0 shipped. Worst case: the interactive flow becomes the *only* path and scripted/headless install dies.

**Why it happens:**
TUI frameworks are tempting and some assume an interactive terminal unconditionally. The constraint "single static binary, CGO-free preferred" is easy to violate with the wrong dependency. The existing dashboard UI is deliberately no-build/`go:embed` — the project already avoids toolchain creep.

**How to avoid:**
- Choose a **pure-Go, CGO-free** TUI (e.g. Bubble Tea / tview class — verify no cgo at build with `CGO_ENABLED=0 go build`) and confirm the binary stays single/static.
- TUI is a **front-end over the existing flag-driven commands**, gated on `isatty(stdout)`: non-TTY / `--no-tui` / CI → fall straight through to the current non-interactive `install` path. The TUI must never be the only way in.
- Add a `CGO_ENABLED=0` static-build check to `make check`/CI so a cgo-pulling dependency fails fast.

**Warning signs:**
`go build` now requires a C toolchain; `CGO_ENABLED=0` build fails; `villa install` hangs or panics with no TTY; binary size jumps sharply; install unusable in a script.

**Phase to address:** TUI-install phase (INSTALL-01).

---

### Pitfall 11: The TUI duplicates (and drifts from) the detect→recommend→preflight→install logic (INSTALL-01)

**What goes wrong:**
The TUI re-implements hardware fit math, the preflight gate, or backend selection inline (to drive its screens), so there are now two sources of truth. The TUI's copy drifts — it shows a model that doesn't fit, skips a BLOCK check, or selects a backend the resolver would reject — and "just works after install" silently regresses for TUI users only.

**Why it happens:**
TUIs want data shaped for rendering and it's quick to recompute locally. But the system's discipline is composition-over-reimplementation (bench composes backendswap, dashboard composes status) and a single polymorphism point (`BackendFor`). A second recommend/preflight implementation violates both.

**How to avoid:**
- The TUI **calls the existing pure cores** for every decision: `detect` → `recommend.Pick` → `preflight` → `orchestrate`, and selects the backend only via `BackendFor`. It renders their typed results; it computes nothing about fit/eligibility itself.
- Treat the TUI as the command-tier's thin presentation seam (like `live*Deps` wiring) over the same cores the flag path uses — identical decisions, two renderings.
- Reuse `config.SaveVilla` for persistence (0600, traversal-guarded); the TUI does not write config by a side path.

**Warning signs:**
Fit/quant math or preflight tiers reimplemented in the TUI package; the TUI recommending a model the CLI wouldn't; a backend string handled outside `BackendFor`; TUI and CLI disagreeing on the same host.

**Phase to address:** TUI-install phase (INSTALL-01).

---

### Pitfall 12: Adding `rocm-6.4.4` without a digest pin / tripping the seam gate or policy floors (ROCM-ALT-01)

**What goes wrong:**
The alternate ROCm image is added as a bare tag (`:rocm-6.4.4`) without a `@sha256:` digest — losing reproducibility (the tag is silently rebuilt). Or its literal is placed in a caller/new file outside `internal/inference`, failing `TestSeamGrepGate` (the gate explicitly matches `rocm-7.2.4`/`rocm7-nightlies` and `kyuz0`/`docker.io/` — a new ROCm literal in the wrong file fails CI). Or `rocm-6.4.4` is selected for a host that doesn't pass the `rocm-policy.json` floors (kernel `6.18.4`, firmware `20260110`, `firmwareDeny` `20251125`, required HSA `11.5.1`), reintroducing the fragility v1.1 fenced off.

**Why it happens:**
Adding "one more image" looks trivial. But this system pins every image by digest (`openWebUIImage`, `rocmImage`), seam-locks every backend literal, and gates ROCm behind a policy file — three invariants a casual addition violates at once. There's also a behavioral trap: 6.4.4 is being added for TG-heavy models because 7.2.4 regressed tg (live Δtg −11.15), so it must not be silently auto-selected (Vulkan stays default; ROCm advises, never auto-switches).

**How to avoid:**
- Pin the digest: resolve `docker.io/kyuz0/…:rocm-6.4.4` via `skopeo inspect` and store `@sha256:…`, exactly as `rocmImage` documents its resolution.
- Keep the literal **inside `internal/inference`** (sibling to `backend_rocm.go`), behind the existing `Backend` seam and `BackendFor`; add positive-presence + the existing grep-gate coverage. Extend the gate's known-tag set to include `rocm-6.4.4` so a future leak still fails.
- Run it through the **same `rocm-policy.json` gate**: verify 6.4.4's kernel/firmware/HSA requirements against the floors; if 6.4.4 needs different floors, model that in the policy file (append, re-audit) rather than bypassing it. Confirm 6.4.4 is not the nightly cap-bug image.
- Selection stays **opt-in and offload-asserting**: bring it up transactionally via `backendswap` (capture→prove→cutover→rollback) with a residency proof; never auto-switch from Vulkan. Frame it as "alt option for TG-heavy," surfaced honestly, ideally validated by `bench --compare`.

**Warning signs:**
A `:rocm-6.4.4` reference with no `@sha256:`; `TestSeamGrepGate` red after the change; a new file under `internal/preflight`/`cmd/` containing the ROCm tag; 6.4.4 selectable on a host below the floors; the alt image chosen automatically without `villa backend set`.

**Phase to address:** Alt-ROCm phase (ROCM-ALT-01).

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Host-path `tar` of the volume dir instead of `podman volume export`/`unshare` | Fewer moving parts | Wrong ownership/SELinux on cross-host restore; corrupt SQLite if live | Never for the supported restore path; OK only as an internal debug dump |
| Append-only usage event log, no rotation | Trivial to write | Unbounded growth; harder concurrent writes | Only behind an explicit retention cap shipped same-phase |
| Saved bench report with no `schema_version` | Ship faster | `--compare` breaks across versions; in-place schema edits violate golden contract | Never — version from the first commit |
| Recompute fit/preflight inside the TUI | TUI ships independently | Two sources of truth, silent drift from CLI | Never — call the cores |
| Bare `:rocm-6.4.4` tag (no digest) | One fewer lookup | Non-reproducible builds; tag rebuilt under you | Never |
| `doctor` = `is-active` + `/health` 200 | Looks done in a demo | False-green over CPU fallback; betrays D-11 | Never as the verdict; OK only as one input among offload assertions |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| Open WebUI named volume (`villa-openwebui`, `:Z`) | Raw byte tar; restore while running | `podman volume export/import` or `podman unshare` tar; stop→swap→restart→prove; recreate via Quadlet `:Z` |
| `villa-models.volume` (bind, `Type=none`) | Sweep multi-GB weights into the backup | Capture model identity in manifest; re-pull weights on restore via catalog/download |
| llama-server `/metrics` (`internal/metrics`) | Treat last-window tok/s as a live signal in usage totals | Gate on `IsGenerating`; accumulate only real generation windows; degrade to typed-Unknown, never fabricate zeros |
| `systemctl --user` lifecycle | TUI/backup/doctor shell out to systemd directly | Route all systemd/exec through `internal/orchestrate` (the only impure module) |
| ROCm images (kyuz0 toolboxes) | Bare tag; literal outside the seam | Digest-pin inside `internal/inference`; gate via `rocm-policy.json` |
| Dashboard `/api` (chi, loopback) | New usage/doctor endpoint bound off-loopback or without same-origin | `net.JoinHostPort("127.0.0.1", …)`; behind `requireSameOrigin`; loopback test |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Backup pulling in model weights | Backup runs for minutes; archive tens of GB; disk fills | Scope to config + WebUI data; manifest model identity only | First backup on a host with large GGUFs (immediately) |
| Unbounded usage log | Usage file grows linearly with uptime; slow reads in dashboard | Bounded buckets / fixed retention + rotation | Weeks–months of uptime |
| Per-scrape usage write (chatty I/O) | Disk writes every poll tick; SSD wear paranoia | Aggregate in memory, flush on interval/shutdown; single writer | High-frequency dashboard polling |
| Comparing many saved bench runs | `--compare` slow / confusing as reports accumulate | Index by environment fingerprint; cap or paginate; comparability filter | Dozens of saved runs |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Usage tracking phones home for "anon stats" | Violates zero-telemetry core value; data off-box | Counts-only, local store, no new outbound; assert no new egress |
| Usage store holds prompt/response text | Privacy leak of the exact thing the product protects | Persist token counts/timestamps only; never content/chat IDs |
| New usage/doctor HTTP surface bound `0.0.0.0`/`:port` | LAN/remote exposure (PRIV-01 regression) | Loopback-only `net.JoinHostPort`; same-origin `/api` guard; loopback test |
| Backup archive written world-readable | Chats/settings (sensitive) leak via file perms | 0600 file / 0700 dir; XDG path; traversal guard (mirror `config`) |
| Restore writes config by raw file drop | Bypasses traversal guard / fail-closed parsing | Restore via `config.SaveVilla`; re-validate with preflight |
| TUI/backup shelling user-supplied strings | Shell injection | Fixed-arg `exec.Command`; catalog-resolved names; no interpolation |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| Restore says "done" but stack unhealthy | User trusts a broken recovery | Prove health post-restore; FAIL + rollback if not proven |
| `doctor` green while inference on CPU | False confidence; slow inference unnoticed | Offload-assert; FAIL on confident CPU fallback, WARN on unevaluable |
| TUI-only install, no headless path | SSH/CI/scripted installs impossible | TTY-gated; `--no-tui` falls through to flag path |
| `bench --compare` prints delta across mismatched specs | User chases a phantom regression | Comparability guard; label "not comparable" |
| Backup omits models without saying so | "Full restore" comes up modelless | Manifest states what's captured vs re-pulled |
| `doctor` lists faults with no fix | User stuck | Refuse-with-remediation on every non-PASS (preflight idiom) |

## "Looks Done But Isn't" Checklist

- [ ] **Backup/restore:** Often missing the *cross-host UID/SELinux* case — verify restore on a second machine (or after `podman system reset`) reopens chats, not just same-host round-trip.
- [ ] **Backup/restore:** Often missing *quiesce ordering* — verify the service is stopped before capture/restore and health is proven after (rollback on fail).
- [ ] **Usage tracking:** Often missing the *no-new-outbound* proof — verify zero new network destinations and counts-only (no content) in the store.
- [ ] **Usage tracking:** Often missing *concurrent-write* safety — verify CLI + long-lived dashboard don't corrupt/double-count the store.
- [ ] **Saved bench:** Often missing *schema version + golden freeze* — verify a persisted report has a version field and a golden test guards its bytes.
- [ ] **`--compare`:** Often missing the *comparability guard* — verify mismatched model/quant/host is flagged, not silently delta'd.
- [ ] **doctor:** Often missing *offload assertion* — verify a forced CPU fallback yields FAIL, not green.
- [ ] **TUI:** Often missing *non-TTY fallthrough + CGO-free* — verify `CGO_ENABLED=0` build and headless install both work.
- [ ] **rocm-6.4.4:** Often missing *digest pin + seam containment + policy gate* — verify `@sha256:`, `TestSeamGrepGate` green, and floors enforced.

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Restore corrupted the live DB | HIGH | Restore from the pre-restore rollback archive (kept as the transaction frame); re-stop service, re-import, re-prove |
| Wrong ownership after cross-host restore | MEDIUM | `podman unshare chown -R` to namespace UID; or recreate volume via Quadlet and re-import through `podman volume import` |
| Usage log grew unbounded | LOW | Rotate/truncate to bounded buckets; ship retention cap; aggregate historical |
| Saved-report schema edited in place (golden broke) | MEDIUM | Revert to append-only, restore prior schema, add new fields by append + bump, refreeze once |
| `doctor` shipped false-green | MEDIUM | Re-route verdict through offload-assert/`status`; downgrade is-active to one input; add forced-CPU-fallback test |
| `rocm-6.4.4` literal leaked / un-pinned | LOW | Move literal into `internal/inference`; add `@sha256:`; extend gate tag set; re-run `TestSeamGrepGate` |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| 1 UID/SELinux mangle on restore | BAK-01 (backup/restore) | Cross-host (or post-reset) restore reopens chats; ownership correct in `podman unshare` |
| 2 Restore into running stack corrupts DB | BAK-01 | Restore stops→swaps→restarts→proves; rollback archive on fail |
| 3 Backup sweeps model weights | BAK-01 | Manifest distinguishes captured vs re-pull; disk-estimate refuse |
| 4 Version-skew restore | BAK-01 | Manifest carries villa version + WebUI digest + host fingerprint; WARN on skew; config re-validated |
| 5 Usage becomes telemetry/leaks | USAGE-01 (+ security checkpoint) | No new outbound; counts-only store; loopback test on any new surface |
| 6 Unbounded growth / write race | USAGE-01 | Bounded retention; single-writer or flock+atomic write; XDG 0600 |
| 7 doctor false-green | DOCTOR-01 | Forced CPU fallback → FAIL; composes preflight+status+residency; seam gate green |
| 8 Saved report breaks golden contract | BENCH-03 (saved bench) | `schema_version` present; golden-frozen; append-only diff; pp/tg separate |
| 9 `--compare` non-comparable runs | BENCH-03 | Environment fingerprint persisted; comparability guard; void runs excluded |
| 10 TUI breaks static/CGO/non-TTY | INSTALL-01 (TUI install) | `CGO_ENABLED=0` build passes; non-TTY/`--no-tui` falls through |
| 11 TUI duplicates decision logic | INSTALL-01 | TUI calls detect/recommend/preflight/BackendFor; no fit/preflight reimpl |
| 12 rocm-6.4.4 un-pinned / leaks / floor bypass | ROCM-ALT-01 (alt ROCm) | `@sha256:` pin; literal in `internal/inference`; `rocm-policy.json` gate; `TestSeamGrepGate` green; opt-in only |

## Sources

- This codebase (HIGH): `internal/inference/seam_test.go` (TestSeamGrepGate scope + matched tags), `internal/inference/backend_rocm.go` (digest-pin discipline, nightlies cap-bug, HSA/kfd), `internal/orchestrate/openwebui.go` + `quadlet/*.volume.tmpl` (named vs bind volume, `:Z` label, digest-pinned image), `internal/preflight/rocm-policy.json` + `floors.go`/`checks_rocm.go` (kernel/firmware/HSA floors, imageDeny), `internal/bench/bench.go` (separate pp/tg, residency-void gating, reproducible BenchSpec), `internal/metrics/llamacpp.go` (last-window vs live-signal trap), `internal/config/villaconfig.go` (XDG 0600/0700, traversal guard, fail-closed).
- `.planning/PROJECT.md` + `CLAUDE.md` (HIGH): zero-telemetry/loopback PRIV gates, offload-asserting D-11, byte-frozen golden contracts, composition-over-reimplementation, single `BackendFor` resolver, transactional backendswap, dashboard-restart-after-rebuild gotcha, live Δtg −11.15 (ROCm tg regression motivating 6.4.4).
- Podman rootless volume backup/restore mechanics (MEDIUM, WebSearch-verified): `podman volume export`/`import`, rootless storage path `~/.local/share/containers/storage/volumes/`, `--userns=keep-id`, `:U`, `podman unshare chown` — https://www.tutorialworks.com/podman-rootless-volumes/ , https://github.com/containers/podman/issues/10669

---
*Pitfalls research for: VillaStraylight v1.2 Operability features*
*Researched: 2026-06-07*
