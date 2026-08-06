package main

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// agentCheckByID is a small helper: find the first CheckResult with the given id.
func agentCheckByID(t *testing.T, checks []preflight.CheckResult, id string) preflight.CheckResult {
	t.Helper()
	for _, c := range checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no check with id %q in %v", id, checks)
	return preflight.CheckResult{}
}

// fitCoder builds a recommendation whose coder block fits (Fits=true) at the given
// totals, so the envelope check passes and the disk check has a staged size.
func fitCoder() recommend.Recommendation {
	return recommend.Recommendation{
		Coder: recommend.CoderFit{
			Model: "qwen3-coder-test", Quant: "Q4_K_M", AgentCtx: 16384,
			WeightBytes: 3 << 30, KVCacheBytes: 1 << 30, HeadroomBytes: 1 << 30,
			TotalBytes: 5 << 30, Fits: true, Residency: "swap",
		},
	}
}

// TestAgentPreflightDiskBlock proves the disk BLOCK tier: when free disk is
// confidently below the staged GGUF + binary footprint → TierBlock/StatusFail
// (refuse-with-remediation), never a WARN.
func TestAgentPreflightDiskBlock(t *testing.T) {
	rec := fitCoder()
	in := agentCheckInput{
		stagedBytes: 4 << 30, // coder shard + binary
		dataDir:     "/var/lib/villa/models",
		statfs:      func(string) (uint64, bool) { return 1 << 30, true }, // only 1 GiB free
		lookupEnv:   func(string) (string, bool) { return "", false },
	}
	got := runAgentChecks(detect.HostProfile{}, rec, in)
	disk := agentCheckByID(t, got, agentDiskCheckID)
	if disk.Tier != preflight.TierBlock || disk.Status != preflight.StatusFail {
		t.Fatalf("insufficient disk: want TierBlock/StatusFail, got %s/%s", disk.Tier, disk.Status)
	}
	if disk.Remediation == "" {
		t.Errorf("disk BLOCK must carry a remediation; got empty")
	}
}

// TestAgentPreflightDiskPass proves the disk check PASSES when free space clears
// the staged footprint.
func TestAgentPreflightDiskPass(t *testing.T) {
	rec := fitCoder()
	in := agentCheckInput{
		stagedBytes: 4 << 30,
		dataDir:     "/var/lib/villa/models",
		statfs:      func(string) (uint64, bool) { return 100 << 30, true }, // 100 GiB free
		lookupEnv:   func(string) (string, bool) { return "", false },
	}
	disk := agentCheckByID(t, runAgentChecks(detect.HostProfile{}, rec, in), agentDiskCheckID)
	if disk.Status != preflight.StatusPass {
		t.Fatalf("ample disk: want StatusPass, got %s (%s)", disk.Status, disk.Detail)
	}
}

// TestAgentPreflightDiskUnprobeableWARN proves the typed-Unknown rule: an
// unprobeable free-disk signal degrades to a WARN, NEVER a false BLOCK/FAIL.
func TestAgentPreflightDiskUnprobeableWARN(t *testing.T) {
	rec := fitCoder()
	in := agentCheckInput{
		stagedBytes: 4 << 30,
		dataDir:     "/var/lib/villa/models",
		statfs:      func(string) (uint64, bool) { return 0, false }, // statfs failed → unprobeable
		lookupEnv:   func(string) (string, bool) { return "", false },
	}
	disk := agentCheckByID(t, runAgentChecks(detect.HostProfile{}, rec, in), agentDiskCheckID)
	if disk.Status != preflight.StatusWarn {
		t.Fatalf("unprobeable disk: want StatusWarn (typed-Unknown), got %s", disk.Status)
	}
	if disk.Status == preflight.StatusFail {
		t.Fatalf("unprobeable disk must NEVER be a confident FAIL/BLOCK")
	}
}

// TestAgentPreflightEnvelopeBlock proves the post-coder envelope BLOCK: when
// rec.Coder.Fits == false the host cannot fit the coder at agent ctx → TierBlock/
// StatusFail, with the detail driven by rec.Coder (never re-derived).
func TestAgentPreflightEnvelopeBlock(t *testing.T) {
	rec := recommend.Recommendation{
		Coder: recommend.CoderFit{
			Model: "", Fits: false, Residency: "shared",
			TotalBytes: 9 << 30,
		},
	}
	in := agentCheckInput{
		stagedBytes: 4 << 30,
		dataDir:     "/var/lib/villa/models",
		statfs:      func(string) (uint64, bool) { return 100 << 30, true },
		lookupEnv:   func(string) (string, bool) { return "", false },
	}
	env := agentCheckByID(t, runAgentChecks(detect.HostProfile{}, rec, in), agentEnvelopeCheckID)
	if env.Tier != preflight.TierBlock || env.Status != preflight.StatusFail {
		t.Fatalf("no coder fit: want TierBlock/StatusFail, got %s/%s", env.Tier, env.Status)
	}
	if env.Remediation == "" {
		t.Errorf("envelope BLOCK must carry a remediation")
	}
}

// TestAgentPreflightEnvelopePass proves rec.Coder.Fits == true → StatusPass.
func TestAgentPreflightEnvelopePass(t *testing.T) {
	in := agentCheckInput{
		stagedBytes: 4 << 30,
		dataDir:     "/var/lib/villa/models",
		statfs:      func(string) (uint64, bool) { return 100 << 30, true },
		lookupEnv:   func(string) (string, bool) { return "", false },
	}
	env := agentCheckByID(t, runAgentChecks(detect.HostProfile{}, fitCoder(), in), agentEnvelopeCheckID)
	if env.Status != preflight.StatusPass {
		t.Fatalf("coder fits: want StatusPass, got %s (%s)", env.Status, env.Detail)
	}
}

// TestAgentPreflightCloudCredWARN proves a present cloud-LLM credential is a WARN
// naming the var(s) + the neutralization — NEVER a BLOCK.
func TestAgentPreflightCloudCredWARN(t *testing.T) {
	rec := fitCoder()
	present := map[string]string{"ANTHROPIC_API_KEY": "sk-ant-xxxx"}
	in := agentCheckInput{
		stagedBytes: 4 << 30,
		dataDir:     "/var/lib/villa/models",
		statfs:      func(string) (uint64, bool) { return 100 << 30, true },
		lookupEnv:   func(k string) (string, bool) { v, ok := present[k]; return v, ok },
	}
	cred := agentCheckByID(t, runAgentChecks(detect.HostProfile{}, rec, in), agentCloudCredCheckID)
	if cred.Tier != preflight.TierWarn || cred.Status != preflight.StatusWarn {
		t.Fatalf("present credential: want TierWarn/StatusWarn, got %s/%s", cred.Tier, cred.Status)
	}
	if cred.Tier == preflight.TierBlock {
		t.Fatalf("a credential must NEVER be a BLOCK")
	}
	if !strings.Contains(cred.Detail, "ANTHROPIC_API_KEY") {
		t.Errorf("cloud-cred WARN must name the found var; detail = %q", cred.Detail)
	}
}

// TestAgentPreflightCloudCredAbsentNoBlock proves that with no credential present
// the cloud-cred check is a PASS (or omitted) — never a BLOCK and never a WARN.
func TestAgentPreflightCloudCredAbsentNoBlock(t *testing.T) {
	rec := fitCoder()
	in := agentCheckInput{
		stagedBytes: 4 << 30,
		dataDir:     "/var/lib/villa/models",
		statfs:      func(string) (uint64, bool) { return 100 << 30, true },
		lookupEnv:   func(string) (string, bool) { return "", false },
	}
	cred := agentCheckByID(t, runAgentChecks(detect.HostProfile{}, rec, in), agentCloudCredCheckID)
	if cred.Status != preflight.StatusPass {
		t.Fatalf("no credential: want StatusPass, got %s", cred.Status)
	}
	if cred.Tier == preflight.TierBlock {
		t.Fatalf("the cloud-cred check must never be a BLOCK")
	}
}

// TestAgentPreflightCloudCredMultiple proves multiple present credentials are all
// named in the single WARN.
func TestAgentPreflightCloudCredMultiple(t *testing.T) {
	rec := fitCoder()
	present := map[string]string{"OPENAI_API_KEY": "x", "GROQ_API_KEY": "y"}
	in := agentCheckInput{
		stagedBytes: 4 << 30,
		dataDir:     "/var/lib/villa/models",
		statfs:      func(string) (uint64, bool) { return 100 << 30, true },
		lookupEnv:   func(k string) (string, bool) { v, ok := present[k]; return v, ok },
	}
	cred := agentCheckByID(t, runAgentChecks(detect.HostProfile{}, rec, in), agentCloudCredCheckID)
	if !strings.Contains(cred.Detail, "OPENAI_API_KEY") || !strings.Contains(cred.Detail, "GROQ_API_KEY") {
		t.Errorf("multi-cred WARN must name every found var; detail = %q", cred.Detail)
	}
}

// TestAgentCloudCredAllowlistComplete pins the researched allowlist (A1): every
// provider key from 27-RESEARCH is scanned, so a stray cloud key is always surfaced.
func TestAgentCloudCredAllowlistComplete(t *testing.T) {
	want := []string{
		"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "OPENROUTER_API_KEY", "GEMINI_API_KEY",
		"GOOGLE_GENERATIVE_AI_API_KEY", "GROQ_API_KEY", "XAI_API_KEY", "MISTRAL_API_KEY",
		"DEEPSEEK_API_KEY", "AZURE_OPENAI_API_KEY", "CRUSH_API_KEY",
	}
	have := map[string]bool{}
	for _, k := range cloudCredentialAllowlist {
		have[k] = true
	}
	for _, k := range want {
		if !have[k] {
			t.Errorf("cloudCredentialAllowlist is missing %q (27-RESEARCH A1)", k)
		}
	}
}
