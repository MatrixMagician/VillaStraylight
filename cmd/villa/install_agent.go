package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/agent"
	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// install_agent.go holds the v1.4 CODING-AGENT install addon wiring the `villa install`
// verb gates on the PERSISTED agent_enabled (D-01, INSTALL-03) — the Crush twin of
// install_memory.go. It mirrors the memory addon seam-for-seam with ONE structural delta:
// the coder GGUF is a CATALOG ENTRY resolved from the recommend-picked coder id, NOT a
// hard-coded literal like nomicEmbedShard — so D-02 (pick selects) and D-04 (single source:
// the staged filename and the served -m path derive from the SAME entry) hold by construction.
//
//   - coderShardFor: resolves the picked coder entry's Shards[0] (D-02/D-04) — never a literal.
//   - liveCoderModelPresent / liveEnsureCoderModel: idempotent size-gated presence-skip +
//     the single sanctioned outbound window (pullFn == download.PullModel, D-03).
//   - liveInstallAgentBinary: COMPOSES the Phase-26 agent.Install (checksum-before-extract,
//     D-03) with the policy-resolved pinned asset/URL — never re-implements the verify.
//   - liveRenderCrushConfig: COMPOSES the Task-1 agent.Render (restrictive tools) + writes
//     the rendered crush.json to crushConfigPath() — never a new JSON encoder.
//
// The FSL-1.1-MIT notice, the evalAgentProof readiness verdict, and the host-side tool-call
// probe (Task 3) live below the binary/model seams.

// coderShardFor resolves the GGUF shard to pre-stage for the recommend-picked coder model
// (D-02/D-04): it scans the catalog for the entry whose ID == rec.Coder.Model and returns
// its Shards[0] (coder entries are single-shard — catalog FROZEN, Phase 24). It returns
// (zero, false) when no coder fits (empty id), the id is absent, or the entry carries no
// shards. This is the explicit ANTI-pattern guard: the shard is RESOLVED from the picked
// entry, never a hard-coded coderShard literal — eliminating a drift/forgery surface
// (T-27-03) and making the staged filename single-source with the served -m path.
func coderShardFor(rec recommend.Recommendation, cat catalog.Catalog) (catalog.Shard, bool) {
	if rec.Coder.Model == "" {
		return catalog.Shard{}, false // no coder fit (shared residency) — nothing to stage
	}
	for _, m := range cat.Models {
		if m.ID == rec.Coder.Model && len(m.Shards) > 0 {
			return m.Shards[0], true
		}
	}
	return catalog.Shard{}, false // picked id absent / entry has no shards → refuse-with-remediation
}

// liveCoderModelPresent reports whether the pre-staged coder GGUF already exists on disk
// AND is intact (the ensureCoderModel idempotency guard — a present file is never re-pulled).
// It clones liveEmbedModelPresent's IN-03 integrity guard parameterized by the resolved
// shard: presence requires the on-disk size to MATCH sh.SizeBytes, so a truncated/tampered
// file is treated as absent and re-pulled (then re-verified by download.PullModel) rather
// than trusting a corrupt weight. A cheap stat-only guard; it does NOT re-hash on every install.
func liveCoderModelPresent(modelsDir string, sh catalog.Shard) bool {
	fi, err := os.Stat(filepath.Join(modelsDir, sh.Filename))
	if err != nil {
		return false
	}
	// A size mismatch means a truncated/tampered file → treat as absent (re-pull + re-verify).
	// fi.Size() is non-negative for a regular file, so the uint64 conversion is safe.
	return fi.Size() >= 0 && uint64(fi.Size()) == sh.SizeBytes
}

// liveEnsureCoderModel pre-stages the resolved coder shard into modelsDir via the verified
// downloader the `model pull`/`model swap` path uses (the pullFn seam == download.PullModel),
// wrapping the shard in a single-shard CatalogModel (D-03). It creates the models dir 0700
// first (mirroring liveEnsureEmbedModel). download.PullModel does the HEAD size/etag verify
// → stream → SHA256 + size check → atomic rename, so a half-written or unverified GGUF is
// never left on disk (T-27-01). This is the single sanctioned outbound window for the coder
// weight: a one-time install-time controlled pull; runtime stays ZERO-download (D-03).
func liveEnsureCoderModel(modelsDir string, sh catalog.Shard) error {
	if mkErr := os.MkdirAll(modelsDir, 0o700); mkErr != nil {
		return mkErr
	}
	m := catalog.CatalogModel{Shards: []catalog.Shard{sh}}
	return pullFn(context.Background(), m, modelsDir)
}

// liveInstallAgentBinary COMPOSES the Phase-26 checksum-before-extract install seam
// (agent.Install) — it NEVER re-implements the verify/extract (D-03). It resolves the
// pinned linux/amd64 asset + release URL from the EXPORTED policy loader (agent.LoadCrushPolicy,
// the single source of the pin), GETs the tarball over a bounded HTTP request, then hands
// the response body to agent.Install, which buffers it bounded by asset.Size, asserts size
// THEN SHA-256 via VerifyTarball BEFORE any extraction, and places ONLY the `crush` binary
// at agentBinDir()/crush (T-27-02). An unverified/mismatched tarball is never extracted.
//
// The asset/URL resolution is kept HERE (testable in cmd/villa) rather than inside the
// agent package so a future asset rename or URL template change is exercised by the install
// flow tests; the verify/extract stays behind the agent seam.
func liveInstallAgentBinary(ctx context.Context) (string, error) {
	policy := agent.LoadCrushPolicy()
	asset, ok := policy.Assets["linux/amd64"]
	if !ok {
		return "", fmt.Errorf("install: crush pin policy has no linux/amd64 asset — cannot install the coding agent")
	}
	url := strings.ReplaceAll(policy.URLTmpl, "{asset}", asset.Name)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("install: build Crush download request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("install: download Crush %s: %w", asset.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("install: download Crush %s: unexpected HTTP status %s", asset.Name, resp.Status)
	}
	// agent.Install bounds the read by asset.Size+1, verifies size THEN SHA-256 (D-03),
	// and extracts ONLY the crush binary — checksum-BEFORE-extract, never a partial place.
	binPath, err := agent.Install(asset, resp.Body, agentBinDir())
	if err != nil {
		return "", err // already a refuse-with-remediation from the verify gate
	}
	return binPath, nil
}

// liveRenderCrushConfig COMPOSES the Task-1 agent.Render (the restrictive-tools crush.json)
// with the WriteConfig discipline, persisting the rendered config to crushConfigPath()
// (~/.config/crush/crush.json — the global config Crush itself reads). It does NOT write a
// new JSON encoder: Render owns the deterministic byte-output (the source of truth derived
// from config.toml, D-06), and the write mirrors liveAgentDeps.WriteConfig (MkdirAll 0700 +
// WriteFile 0600, traversal-guarded). LSP probes resolve the host PATH (references only,
// never auto-installs, D-10); a missing server is a WARN-and-omit, never a blocking error.
func liveRenderCrushConfig(cfg config.VillaConfig) error {
	b, _, err := agent.Render(cfg, liveLSPProbes())
	if err != nil {
		return fmt.Errorf("install: render crush.json: %w", err)
	}
	path, err := crushConfigPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := assertWithinDir(path, dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("install: create crush config dir: %w", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("install: write crush.json: %w", err)
	}
	return nil
}

// liveLSPProbes resolves the fixed set of supported LSP servers on the host PATH for the
// crush.json render (D-10). It references PATH only — it NEVER auto-installs (the Render
// contract: a not-found server is a WARN and an omitted lsp entry). The probe set mirrors
// the languages the coding agent commonly drives; absence degrades gracefully.
func liveLSPProbes() []agent.LSPProbe {
	want := []struct{ key, cmd string }{
		{"go", "gopls"},
		{"python", "pyright"},
		{"rust", "rust-analyzer"},
		{"typescript", "typescript-language-server"},
	}
	probes := make([]agent.LSPProbe, 0, len(want))
	for _, w := range want {
		_, found := lookPathFn(w.cmd)
		probes = append(probes, agent.LSPProbe{Key: w.key, Command: w.cmd, Found: found})
	}
	return probes
}

// lookPathFn is the host-PATH lookup seam (defaults to exec.LookPath, wrapped to a
// (path, found) bool result like liveAgentDeps.LookPath). Kept as a package var so the
// install-flow tests can inject a deterministic probe result without a live host PATH.
var lookPathFn = func(bin string) (string, bool) {
	p, err := exec.LookPath(bin)
	return p, err == nil
}

// liveLoadedAgentEnabled returns the PERSISTED config.LoadVilla().AgentEnabled — the
// AUTHORITATIVE coding-agent gate source threaded into runInstall (mirrors
// liveLoadedMemoryEnabled). A config load error fails SOFT to false so a broken config never
// silently enables the agent addon (an opted-in user must have a readable config). A bare
// `villa install` therefore gates on the persisted value, while `--coding-agent` overrides
// it to true before the gate (D-01).
func liveLoadedAgentEnabled() bool {
	c, err := config.LoadVilla()
	if err != nil {
		return false
	}
	return c.AgentEnabled
}

// --- FSL-1.1-MIT consent notice (install-addon obligation) -------------------

// agentLicenseNotice returns the one-line FSL-1.1-MIT notice surfaced before the Crush
// binary is staged (27-RESEARCH § FSL consent text). It is INFORMATIONAL, not a click-
// through EULA — the user already invoked --coding-agent. It mirrors how villa already
// surfaces honest notices (the no-telemetry line, the SELinux note). The load-bearing
// content is the license fact: FSL-1.1-MIT, non-compete, each version → MIT two years
// after release; villa installs a pinned, checksum-verified release and renders config locally.
func agentLicenseNotice() string {
	return "The coding agent (Crush, charmbracelet) is distributed under the Functional Source " +
		"License v1.1 (FSL-1.1-MIT): you may use, modify, and redistribute it for any purpose " +
		"except offering a competing commercial service; each version becomes MIT-licensed two " +
		"years after release. villa installs a pinned, checksum-verified release and renders its " +
		"config locally."
}

// --- Install readiness: tool-call round-trip verdict (D-05) ------------------
//
// The proof asserts the coding agent is HONESTLY ready BEFORE install declares success:
// a REAL `crush run` tool-call round-trip (read a planted file → edit it → result), not a
// health-200. A health-200 / no-edit is a false-green and FAILS (D-05, T-27-05). A FAIL
// refuses-with-remediation (the caller returns exitBlocked), never a silent skip.

// agentProof is the install-readiness verdict for the coding agent (the agentProof twin of
// memoryProof): PASS only on a real tool-call edit, FAIL with a remediation detail otherwise.
// There is no WARN — an agent that cannot complete a planted read→edit round-trip is a
// confident known-bad the user opted into (honesty-by-construction).
type agentProof struct {
	status preflight.Status
	detail string
}

// evalAgentProof is the PURE readiness core (unit-testable off-hardware via the injected
// toolCall): it maps the SINGLE tool-call outcome to a verdict (D-05). A health-200 is NEVER
// an input — the only signal is whether the agent performed the planted edit:
//   - err            → FAIL (the round-trip could not run; re-run `villa install --coding-agent`)
//   - !edited        → FAIL (the agent ran but did not edit — verify the coding-mode --jinja unit)
//   - edited, no err → PASS (read→edit→result completed against the local endpoint)
func evalAgentProof(toolCall func() (edited bool, err error)) agentProof {
	edited, err := toolCall()
	if err != nil {
		return agentProof{
			status: preflight.StatusFail,
			detail: fmt.Sprintf("the coding agent could not complete a tool-call round-trip (%v) — check `systemctl --user status %s` and its journal, then re-run `villa install --coding-agent`", err, installServiceName),
		}
	}
	if !edited {
		return agentProof{
			status: preflight.StatusFail,
			detail: fmt.Sprintf("the coding agent ran but did not perform the tool-call edit — the served coding-mode (--jinja) unit may be misconfigured; check `systemctl --user status %s`, then re-run `villa install --coding-agent`", installServiceName),
		}
	}
	return agentProof{status: preflight.StatusPass, detail: "tool-call round-trip (read→edit→result) completed against the local endpoint"}
}

// agentProbePrompt / agentProbeToken{A,B} are the planted read→edit round-trip payload the
// readiness driver uses: a temp file is planted containing TOKEN_A, the agent is asked to
// replace TOKEN_A with TOKEN_B, and success is the file now containing TOKEN_B (a REAL edit,
// D-05). The tokens are fixed, metachar-free constants (no shell interpolation, T-27-06).
// The on-hardware payload (exact prompt phrasing) is confirmed in Plan 04; these are the
// pinned defaults the live driver uses.
const (
	agentProbeTokenA = "VILLA_PROBE_TOKEN_A"
	agentProbeTokenB = "VILLA_PROBE_TOKEN_B"
	agentProbePrompt = "Open the file villa-readiness-probe.txt, replace the text " +
		agentProbeTokenA + " with " + agentProbeTokenB + ", and save it. Reply DONE when finished."
)

// liveAgentToolCallProbe is the host-side `crush run` driver for the readiness verdict
// (D-05): it plants a file containing TOKEN_A in a fresh temp working dir, execs the
// EXPLICIT villa-owned crush binary (agentBinPath()) fixed-arg with the constant prompt,
// and reports edited == the file now containing TOKEN_B. It is fixed-arg (no shell, no image
// literal — host binary exec only, so TestSeamGrepGate stays green) and bounded by the
// passed context (a timeout → err → FAIL, never a hang masquerading as success). The
// returned func is the toolCall input evalAgentProof consumes.
func liveAgentToolCallProbe(ctx context.Context) func() (bool, error) {
	return func() (bool, error) {
		workDir, err := os.MkdirTemp("", "villa-agent-probe-")
		if err != nil {
			return false, fmt.Errorf("create probe working dir: %w", err)
		}
		defer os.RemoveAll(workDir)

		probeFile := filepath.Join(workDir, "villa-readiness-probe.txt")
		if err := os.WriteFile(probeFile, []byte(agentProbeTokenA+"\n"), 0o600); err != nil {
			return false, fmt.Errorf("plant probe file: %w", err)
		}

		// Fixed-arg exec of the villa-owned binary in the probe working dir: `crush run
		// "<prompt>"`. NEVER a PATH lookup (a user-installed crush cannot be hijacked,
		// T-26-09) and NEVER a shell. A non-zero exit is surfaced as the round-trip error.
		cmd := exec.CommandContext(ctx, agentBinPath(), "run", agentProbePrompt) //nolint:gosec // fixed-arg, villa-owned binary, constant prompt
		cmd.Dir = workDir
		if runErr := cmd.Run(); runErr != nil {
			return false, fmt.Errorf("crush run: %w", runErr)
		}

		edited, err := os.ReadFile(probeFile) //nolint:gosec // path is the planted probe file under a villa temp dir
		if err != nil {
			return false, fmt.Errorf("read probe file after run: %w", err)
		}
		return strings.Contains(string(edited), agentProbeTokenB), nil
	}
}
