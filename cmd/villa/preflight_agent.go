package main

import (
	"fmt"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// preflight_agent.go is the v1.4 CODING-AGENT preflight gate:
// runAgentChecks appends honest agent-specific CheckResults to the existing install
// gate, but ONLY when the addon is enabled (the caller — install.go — gates the fold
// on loadedAgentEnabled, so an agent-off preflight stays byte-identical). It is the
// Crush twin of internal/preflight/checks_memory.go's RunMemory, kept at the cmd tier
// because the preflight `pass`/`warn`/`fail` builders are package-private and the
// agent gate needs the recommendation (rec.Coder) + the staged-footprint size that the
// install flow already resolves. It builds CheckResult literals directly, exactly as
// the agent gate's honest-gating decision requires:
//
//   - disk BLOCK         — free bytes < (staged coder GGUF + pinned binary) → TierBlock/
//     StatusFail (refuse-with-remediation). An unprobeable free-disk signal degrades to
// a typed-Unknown WARN, NEVER a false BLOCK (the honesty rule).
//   - envelope BLOCK     — rec.Coder.Fits == false → TierBlock/StatusFail at agent ctx;
//     the basis is READ from rec.Coder (TotalBytes/Residency), NEVER re-derived (the
//     anti-pattern guard — the gate cannot drift from the Phase-24 pickCoder output).
//   - cloud-credential   — a present cloud-LLM provider key in the env → TierWarn naming
// the var(s) + the neutralization; absence → a single PASS. NEVER a BLOCK:
//     the rendered loopback-only provider + disable_default_providers + the egress proof
//     already neutralize a stray key structurally; preflight only SURFACES the risk.
//
// This package never reads the env directly in the pure core — env access is the
// injected lookupEnv seam (os.LookupEnv live) so the tier tests are deterministic.

// Agent preflight check IDs (stable, mirroring the PRE-/MEM-PRE- id discipline).
const (
	agentDiskCheckID      = "AGENT-PRE-disk"
	agentEnvelopeCheckID  = "AGENT-PRE-envelope"
	agentCloudCredCheckID = "AGENT-PRE-cloud-cred"
)

// cloudCredentialAllowlist is the fixed set of cloud-LLM provider env vars scanned by
// the cloud-credential WARN (27-RESEARCH § "Cloud-credential WARN allowlist", A1).
// Presence of ANY of these → a WARN (never a BLOCK). The list is the practical
// silent-fallback signal on the Strix Halo target: Crush reads provider keys from the
// environment (A2). Crush ALSO keeps an on-disk auth store; the env scan is the
// load-bearing one, so the on-disk path is covered by the same lookupEnv-driven WARN
// surface (the live wiring may extend the scan, but env is the canonical signal here).
var cloudCredentialAllowlist = []string{
	"ANTHROPIC_API_KEY",
	"OPENAI_API_KEY",
	"OPENROUTER_API_KEY",
	"GEMINI_API_KEY",
	"GOOGLE_GENERATIVE_AI_API_KEY",
	"GROQ_API_KEY",
	"XAI_API_KEY",
	"MISTRAL_API_KEY",
	"DEEPSEEK_API_KEY",
	"AZURE_OPENAI_API_KEY",
	"CRUSH_API_KEY",
}

// agentCheckInput carries the resolved facts + injected probe seams runAgentChecks
// gates on, mirroring preflight.ResourceReq + the statfs-injection idiom so the tier
// tests assert each verdict without a real undersized filesystem or a live env.
type agentCheckInput struct {
	// stagedBytes is the on-disk footprint the addon will add: the resolved coder
	// GGUF shard SizeBytes + the pinned Crush binary asset.Size. Computed by the
	// caller from the SAME catalog entry + policy pin the install flow stages, so the
	// disk gate can never drift from what is actually written.
	stagedBytes uint64
	// dataDir is the models dir the GGUF lands in (the statfs target).
	dataDir string
	// statfs reports free bytes at a path and whether the probe succeeded (nil-failure
	// degrades to a typed-Unknown WARN). Injected so a too-small / unprobeable disk is
	// testable without a real filesystem.
	statfs func(path string) (freeBytes uint64, ok bool)
	// lookupEnv reads an env var (value, present) — the injected env seam (os.LookupEnv
	// live) so the cloud-credential scan is deterministic in tests.
	lookupEnv func(key string) (string, bool)
}

// runAgentChecks is the PURE honest-gating surface: it maps the resolved agent
// facts to a stable-ordered []preflight.CheckResult (disk, envelope, cloud-credential).
// It is unit-testable off-hardware via the injected seams and reads rec.Coder WITHOUT
// re-deriving the fit. The caller (install.go) appends the result to the install gate
// ONLY when the addon is enabled, so the checks flow through the SAME gateInstall and
// inherit refuse-with-remediation while agent-off preflight stays byte-identical.
func runAgentChecks(profile detect.HostProfile, rec recommend.Recommendation, in agentCheckInput) []preflight.CheckResult {
	return []preflight.CheckResult{
		agentDiskCheck(in),
		agentEnvelopeCheck(rec),
		agentCloudCredCheck(in),
	}
}

// agentDiskCheck is AGENT-PRE-disk (BLOCK): free disk at the models dir must clear the
// staged coder GGUF + pinned binary footprint, or the pre-stage fills the disk. A
// confident shortage is a FAIL (refuse-with-remediation); an unprobeable free-disk
// signal (statfs failed) degrades to a typed-Unknown WARN — NEVER a false BLOCK.
func agentDiskCheck(in agentCheckInput) preflight.CheckResult {
	const name = "Coding-agent staging disk space"
	freeDisk, ok := in.statfs(in.dataDir)
	if !ok {
		return preflight.CheckResult{
			ID:          agentDiskCheckID,
			Name:        name,
			Tier:        preflight.TierBlock,
			Status:      preflight.StatusWarn,
			Detail:      fmt.Sprintf("could not statfs %q to verify free disk for the coding-agent staging", in.dataDir),
			Remediation: "Verify the models dir exists and is readable, then re-run `villa install --coding-agent`; or omit --coding-agent.",
			Provenance:  "syscall.Statfs",
		}
	}
	if freeDisk < in.stagedBytes {
		return preflight.CheckResult{
			ID:     agentDiskCheckID,
			Name:   name,
			Tier:   preflight.TierBlock,
			Status: preflight.StatusFail,
			Detail: fmt.Sprintf("free disk %s at %q < required %s for the coder model + agent binary",
				gib(freeDisk), in.dataDir, gib(in.stagedBytes)),
			Remediation: fmt.Sprintf("Free up disk under %q (the coder GGUF + the agent binary need %s), then re-run `villa install --coding-agent`. (The chat stack is unaffected.)",
				in.dataDir, gib(in.stagedBytes)),
			Provenance: "syscall.Statfs:" + in.dataDir,
		}
	}
	return preflight.CheckResult{
		ID:         agentDiskCheckID,
		Name:       name,
		Tier:       preflight.TierBlock,
		Status:     preflight.StatusPass,
		Detail:     fmt.Sprintf("free disk %s ≥ %s at %q", gib(freeDisk), gib(in.stagedBytes), in.dataDir),
		Provenance: "syscall.Statfs:" + in.dataDir,
	}
}

// agentEnvelopeCheck is AGENT-PRE-envelope (BLOCK): the host must fit the coder at its
// agent-profile context. The verdict is READ from rec.Coder (the Phase-24 pickCoder
// output) — NEVER re-derived (the explicit anti-pattern). rec.Coder.Fits == false
// (the no-fit / "shared" residency) is the BLOCK basis; Fits == true is a PASS, with the
// detail citing rec.Coder.TotalBytes + the residency so the basis is single-sourced.
func agentEnvelopeCheck(rec recommend.Recommendation) preflight.CheckResult {
	const name = "Coding-agent memory envelope"
	cf := rec.Coder
	if !cf.Fits {
		return preflight.CheckResult{
			ID:     agentEnvelopeCheckID,
			Name:   name,
			Tier:   preflight.TierBlock,
			Status: preflight.StatusFail,
			Detail: fmt.Sprintf("no coder model fits the detected memory envelope at agent ctx (residency %q; best coder footprint %s) — the host cannot stand up a standalone coding agent",
				cf.Residency, gib(cf.TotalBytes)),
			Remediation: "Free memory or use a larger-envelope host, then re-run `villa install --coding-agent`. (The chat stack is unaffected.)",
			Provenance:  "recommend.Pick:rec.Coder",
		}
	}
	return preflight.CheckResult{
		ID:     agentEnvelopeCheckID,
		Name:   name,
		Tier:   preflight.TierBlock,
		Status: preflight.StatusPass,
		Detail: fmt.Sprintf("coder %s fits at agent ctx %d (footprint %s, residency %q)",
			cf.Model, cf.AgentCtx, gib(cf.TotalBytes), cf.Residency),
		Provenance: "recommend.Pick:rec.Coder",
	}
}

// agentCloudCredCheck is AGENT-PRE-cloud-cred (WARN): a present cloud-LLM provider key
// in the environment is the silent-fallback risk surface (Pitfall 2). Presence →
// a WARN naming the found var(s) + the neutralization; absence → a single PASS. It is
// NEVER a BLOCK — the rendered loopback-only provider + disable_default_providers + the
// egress proof already neutralize a stray key structurally; preflight only surfaces it.
func agentCloudCredCheck(in agentCheckInput) preflight.CheckResult {
	const name = "Cloud-LLM credential in environment"
	var found []string
	for _, key := range cloudCredentialAllowlist {
		if _, present := in.lookupEnv(key); present {
			found = append(found, key)
		}
	}
	if len(found) == 0 {
		return preflight.CheckResult{
			ID:         agentCloudCredCheckID,
			Name:       name,
			Tier:       preflight.TierWarn,
			Status:     preflight.StatusPass,
			Detail:     "no cloud-LLM provider credential found in the environment",
			Provenance: "os.LookupEnv",
		}
	}
	return preflight.CheckResult{
		ID:     agentCloudCredCheckID,
		Name:   name,
		Tier:   preflight.TierWarn,
		Status: preflight.StatusWarn,
		Detail: fmt.Sprintf("cloud-LLM credential(s) present in the environment: %s — surfaced so you know the silent-fallback risk; the rendered crush.json has exactly one loopback provider with disable_default_providers, and `villa verify agent` catches any leak structurally",
			strings.Join(found, ", ")),
		Remediation: "This does NOT block the install: the agent is locked to the local loopback endpoint. To remove the risk entirely, unset the named variable(s) before running the agent.",
		Provenance:  "os.LookupEnv",
	}
}
