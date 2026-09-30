---
status: accepted
---

# Status takes one reading per run and gates on the config it loaded

`villa-dashboard.service` polls `/api/status` every 2.5 seconds and holds the
`status.Deps` it wired at startup for the life of the process. A read at
`2fd9314` found three faults in that read-model. `liveStatusDeps` wired the three
agent seams only when `subsystem.AgentOn` was true at wiring, and closed the
inference client, with its api key, into `Props`, `GenTokensPerSec`, `AgentCache`
and the inference and chat health probes. The CLI builds these per run and was
correct; the dashboard kept the agent rows empty after `villa install
--workspace-agent`, and kept sending a missing or old key, until it was
restarted (#253). `status.Run` also checks `AgentOn` itself, so the gate was
answered twice from two configs. Second, a run probed the host once for ROCm
readiness, once for the weight footprint and once more for the agent's
residency, and doctor added its own probe and one per residency proof: up to
seven `detect.Probe` calls. Third, the web-search freshness rule was written
twice, in `status.webSearchInfo` and doctor's `liveSearchEgressProof`. This ADR
records the fix for #257.

## Decision

- **Every seam is wired whatever the config says, and none closes over a
  config.** A seam that needs one takes the config the run loaded: `Props`,
  `GenTokensPerSec`, `AgentCache` and `Service.Probe` build the inference client
  (ADR-0014) from it on each call. `liveStatusDeps` binds `AgentPinMatch`,
  `AgentResidency` and `AgentCache` unconditionally, and `status.Run` answers the
  agent gate once, from its own load. The config `liveStatusDeps` loads at
  wiring now does one thing: refuse a backend that does not resolve.

- **A status run takes one host profile.** `status.Deps.Probe` replaces the
  `ROCmReadiness` seam. `Run` calls it once, folds readiness from the profile,
  and hands the same profile to `WeightBytes(cfg, host)` and
  `AgentResidency(cfg, host)`. `liveWeightBytes(cfg)` stays, as `weightBytes`
  on a fresh probe, for `backend` and `bench`, which run outside a status run.

- **Doctor reuses that reading.** `liveDoctorDeps` wraps the status seam in
  `sync.OnceValue` and hands the same function to `doctor.Deps.Probe`, so the
  host-condition checks, the status report and the three residency proofs'
  weight footprints read one profile. Only doctor memoizes: it is one-shot,
  where the dashboard needs a fresh reading each poll.

- **Doctor reads the egress answer from the status report.** The `search-egress`
  finding maps `Report.WebSearch.OutboundBounded` (bounded → PASS, not-bounded →
  BLOCK FAIL, anything else → WARN); `doctor.Deps.SearchEgressProof` and
  `liveSearchEgressProof` are deleted, and `status.VerifyFreshnessWindow` is the
  only freshness rule.

- **One helper reads a store file.** `storeReader(path)` returns the file's
  bytes, or `(nil, nil)` when it does not exist, and is the `ReadAll` of the
  usage, recall, verify, pin and bench stores. It replaces seven closures. It
  lives in `cmd/villa`, beside the projections into `status` types (the last
  task still comes from `taskstore` there), so `internal/status` gains no
  import.

- **One table decides what no answer means for the three direct-HTTP health
  probes.** `unansweredHealth` maps the inference unit and the dashboard to
  down, because each is asked on its own loopback port, and the chat row to a
  typed Unknown, because it is asked through llama-server's model list and no
  answer says nothing about Open WebUI. A key the client refuses before sending
  counts as no answer, never as success. The in-network probes keep their
  mapping in `internal/inprobe`.

- **`dashboardDeps` is deleted.** It was a field-for-field copy of
  `dashboard.Config`; `liveDashboardDeps` returns the Config and `runDashboard`
  takes it with the serve function. The `/api/metrics` reads build their client
  per scrape for the same reason as the status seams. `status.Service.Rendered`
  and `doctor.Deps.LoadConfig`, which nothing read, are deleted too.

## Rejected

**Rebuild the status deps on every dashboard poll.** Handing the dashboard a
`func() status.Deps` over `liveStatusDeps` would refresh the gate and the key,
but it keeps two config loads per run, still answers the gate at wiring as well
as in `Run`, and adds a second error path for a backend that stops resolving,
which `Run` already reports as an errored report.

**Carry the host profile on `status.Report` for doctor.** An untagged field would
keep the JSON unchanged, but doctor's residency proofs run after the report and
take `*status.Deps`, not the report, so they would still need the memo or new
signatures. The memo alone covers every doctor reader.

**Give `status.Deps` one `Client(cfg) inference.Client` seam.** `Run` would then
call routes on a concrete HTTP client, and the core's tests would need an HTTP
server where a stubbed func does today.

**Keep doctor's egress seam and point it at the status core's function.** It
would remove the second freshness rule, but keep a seam that answers the web
search gate at wiring, beside the status report that already carries the answer
from the config the run loaded.

## Consequences

The `villa status --json`, `villa doctor --json` and dashboard `/api/status`
goldens are unchanged. The doctor `search-egress` wording changed in two
non-PASS cases: a failed verify no longer quotes its verdict string, since the
report carries only the tri-state, and the unknown case no longer tells an
unreadable store apart from a stale one. No golden covers either.

An errored status report now yields no `search-egress` finding. Doctor already
reports it as one `stack` WARN, and a recent failed verify that the finding
would have carried leaves doctor at WARN instead of FAIL until the report can
be read. That case needs a config whose model or render fails while web search
is on.

The dashboard reloads `config.toml` on each status run and each metrics scrape,
as `ModelID` already did. The task runner still takes its client and the
sandbox gate at startup; moving it is left to a change that owns its wiring.

Doctor still decides which subsystem seams to bind from its own config load;
moving that gating into the doctor module is #258.
