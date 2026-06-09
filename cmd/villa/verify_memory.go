package main

import (
	"fmt"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
)

// verify_memory.go holds the v1.3 RUNTIME firewalled zero-outbound document-upload RAG
// smoke proof — the headline PRIV-05 / SC#4 honesty gate (D-08/D-10/D-11). Install-time
// green is NOT sufficient: Open WebUI lazily fetches embed/reranker/Whisper models from
// HuggingFace at RUNTIME on first RAG use, and ChromaDB posts PostHog telemetry. This
// proof drives a REAL document upload through the OWUI REST RAG path (upload → chunk →
// embed via villa-embed → store in Qdrant → retrieve → cite) entirely over villa.network
// (D-08) WHILE host egress is blocked, paired with a negative-control external probe that
// MUST fail. Asserting zero-outbound by ABSENCE alone is a false-green; the negative
// control proves egress is actually blocked, not merely unused (honesty-by-construction).
//
// It mirrors the Phase-19 memory-stack readiness proof seam (install_memory.go) four-layer
// shape EXACTLY:
//
//  1. Verdict type — reuses memoryProof ({status preflight.Status; detail string}); PASS/
//     FAIL only, no WARN — an unevaluable result is a FAIL, never a silent skip.
//  2. Pure core — evalRagSmoke maps the (egressBlocked, uploadCite) probe outcomes to a
//     verdict, asserting the negative control FIRST (unit-testable off-hardware).
//  3. Live seam — liveRagSmoke (Task 2) composes the egress negative-control probe + the
//     loopback REST RAG drive and calls evalRagSmoke.
//  4. Fixed-arg podman/curl exec — reuses runProbeCurl (install_memory.go) for the
//     negative control; the loopback drive uses a fixed-arg host-side curl. No shell.
//
// MEM-03 / D-07 (auto-extraction default-OFF): the drive uses the STANDARD (non-agentic)
// chat path — a plain POST /api/chat/completions with files:[{type:collection,id}] so the
// knowledge is auto-injected WITH citations. villa NEVER enables Native Function Calling
// nor installs a memory filter; keeping the RAG path on the standard citation-bearing route
// is exactly what enforces MEM-03's default-off auto-extraction.

// ragSmokeInput carries the resolved loopback OWUI address/port the RAG drive reaches over
// the EXISTING PublishPort (127.0.0.1:<chat_port>, no new host port — D-11), plus the
// planted question and the fact whose only source is the uploaded document. Values are
// config-resolved, never shell-interpolated (T-20-09). It mirrors memoryProofInput.
type ragSmokeInput struct {
	owuiAddr string
	owuiPort int
	question string
	wantFact string
}

// evalRagSmoke is the PURE runtime-RAG-smoke core (unit-testable off-hardware via injected
// probes): it maps the negative-control egress outcome and the upload-and-cite outcome to a
// PASS/FAIL verdict. There is NO WARN and NO skip path — an unevaluable result is a FAIL
// (honesty-by-construction, mirrors evalMemoryProof).
//
// Negative-control FIRST (PRIV-05 / T-20-07): asserting zero-outbound by absence alone is a
// false-green, so egress must be proven actually blocked BEFORE the drive is even trusted —
// the upload drive is not invoked until the negative control passes. A probe that could not
// RUN (err) → FAIL refusing to declare zero-outbound; an external host that WAS reachable
// (blocked == false) → FAIL that egress is not blocked.
//
// Only after egress is proven blocked is uploadCite invoked: an error → FAIL the RAG path
// did not complete; the answer not Contains(wantFact) OR !cited → FAIL no-citation. All ok
// + fact present + cited → PASS. Every FAIL carries a refuse-with-remediation detail naming
// the service to check and to re-run `villa verify memory`.
func evalRagSmoke(egressBlocked func() (bool, error), uploadCite func() (answer string, cited bool, err error), wantFact string) memoryProof {
	// 1) Negative control FIRST — egress must be proven blocked before the drive is trusted.
	blocked, err := egressBlocked()
	if err != nil {
		return memoryProof{
			status: preflight.StatusFail,
			detail: fmt.Sprintf("could not run the egress negative-control probe (%v) — refusing to declare zero-outbound; verify the %q network and a reachable helper image, then re-run `villa verify memory`", err, memoryProofNetwork),
		}
	}
	if !blocked {
		return memoryProof{
			status: preflight.StatusFail,
			detail: "egress is NOT blocked: an external host was reachable during the test — block the host's outbound to the public internet for the duration, then re-run `villa verify memory`",
		}
	}

	// 2) Real RAG path — only reached once egress is proven blocked.
	answer, cited, err := uploadCite()
	if err != nil {
		return memoryProof{
			status: preflight.StatusFail,
			detail: fmt.Sprintf("the document-upload RAG path did not complete (%v) — check `systemctl --user status villa-openwebui.service` and the villa-embed/villa-qdrant services, then re-run `villa verify memory`", err),
		}
	}
	if !strings.Contains(answer, wantFact) || !cited {
		return memoryProof{
			status: preflight.StatusFail,
			detail: fmt.Sprintf("the answer did not use the uploaded content / returned no citation (fact-present=%t, cited=%t) — check that villa-embed answered 768-dim embeddings and the doc was stored in Qdrant, then re-run `villa verify memory`", strings.Contains(answer, wantFact), cited),
		}
	}

	return memoryProof{status: preflight.StatusPass, detail: "document upload retrieved + cited with zero outbound"}
}
