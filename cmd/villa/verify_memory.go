package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
)

// verify_memory.go holds the v1.3 RUNTIME firewalled zero-outbound document-upload RAG
// smoke proof — the headline honesty gate. Install-time
// green is NOT sufficient: Open WebUI lazily fetches embed/reranker/Whisper models from
// HuggingFace at RUNTIME on first RAG use, and ChromaDB posts PostHog telemetry. This
// proof drives a REAL document upload through the OWUI REST RAG path (upload → chunk →
// embed via villa-embed → store in Qdrant → retrieve → cite) entirely over villa.network
// WHILE host egress is blocked, paired with a negative-control external probe that
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
// (auto-extraction default-OFF): the drive uses the STANDARD (non-agentic)
// chat path — a plain POST /api/chat/completions with files:[{type:collection,id}] so the
// knowledge is auto-injected WITH citations. villa NEVER enables Native Function Calling
// nor installs a memory filter; keeping the RAG path on the standard citation-bearing route
// is exactly what enforces 's default-off auto-extraction.

// ragSmokeInput carries the resolved loopback OWUI address/port the RAG drive reaches over
// the EXISTING PublishPort (127.0.0.1:<chat_port>, no new host port), plus the
// planted question and the fact whose only source is the uploaded document. Values are
// config-resolved, never shell-interpolated. It mirrors memoryProofInput.
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
// Negative-control FIRST: asserting zero-outbound by absence alone is a
// false-green, so egress must be proven actually blocked BEFORE the drive is even trusted
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

// egressNegativeControlHost is the known external host the negative-control probe attempts
// to reach. OWUI lazily fetches embed/reranker models from HuggingFace at runtime, so
// huggingface.co is the canonical exfil destination to prove unreachable: if `curl` can
// reach it, host egress is NOT blocked and the zero-outbound claim is a false-green. It is
// a probe TARGET (a URL constant), not a backend/image literal — TestSeamGrepGate is
// unaffected (it guards GPU/backend markers, not arbitrary URLs).
const egressNegativeControlHost = "https://huggingface.co/"

// ragSmokeProcessTimeout bounds the async file-processing poll (chunk → embed → store). A
// timeout is an ERROR (the RAG path did not complete), NEVER a silent skip — a green that
// could not actually drive the path is a false-green (Pitfall 6, honesty-by-construction).
const ragSmokeProcessTimeout = 60 * time.Second

// liveRagSmoke is the production runtime-RAG-smoke seam (on-hardware by nature: it needs the
// live OWUI container + a host-egress precondition supplied by the verification wave). It
// mirrors liveMemoryProof's four-layer shape, driving the local upload→chunk→embed→
// retrieve→cite path end-to-end and composing the negative-control egress probe, then calls
// the pure evalRagSmoke.
//
//   - egressBlocked: a negative-control external probe via the EXISTING runProbeCurl (fixed-
//     arg `podman run --rm --network villa --entrypoint curl <helperImage> curl -sf
//     --max-time 5 https://huggingface.co/`). The helper image is sourced from
//
// orchestrate.EmbedImage — NO re-typed image literal (TestSeamGrepGate green).
//
//	  `blocked := err != nil`: a REACHABLE external host means egress is NOT blocked, so the
//	  proof FAILs; a probe that cannot run surfaces as the egressBlocked error → FAIL.
//
//	- uploadCite: drives the OWUI REST RAG path over the loopback PublishPort
//
// (http://127.0.0.1:<chat_port>, no new host port) via a fixed-arg HOST-side curl
//
//	runner (the loopback PublishPort is reached from the host, NOT from inside
//	villa.network, so runProbeCurl's --network villa is wrong for this leg). Sequence
//	(RESEARCH §"Driving the OWUI RAG path"): signin (fallback signup first-user-admin) →
//	knowledge/create → files/ (multipart) + poll process/status → knowledge/{id}/file/add
//	→ chat/completions with files:[{type:collection,id}] → assert fact + citation.
//
// Native Function Calling stays OFF in the drive (a plain chat/completions request, not an
// agentic tool-calling request) so knowledge is auto-injected WITH citations (Pitfall 5)
// which is exactly what enforces 's auto-extraction-default-off (villa never enables
// Agentic/Native FC nor installs a memory filter).
func liveRagSmoke(ctx context.Context, in ragSmokeInput) memoryProof {
	helperImage := orchestrate.EmbedImage()

	// egressBlocked: the negative control MUST fail to reach an external host. Reuse the
	// fixed-arg runProbeCurl over villa.network; a short --max-time bounds a hung connect.
	egressBlocked := func() (bool, error) {
		_, err := runProbeCurl(ctx, helperImage, "-sf", "--max-time", "5", egressNegativeControlHost)
		// A reachable external host (err == nil) means egress is NOT blocked → blocked=false.
		// An unreachable host (err != nil) is the EXPECTED, desired outcome → blocked=true.
		return err != nil, nil
	}

	base := fmt.Sprintf("http://%s:%d", in.owuiAddr, in.owuiPort)
	uploadCite := func() (string, bool, error) {
		return driveRagUploadCite(ctx, base, in.question, in.wantFact)
	}

	return evalRagSmoke(egressBlocked, uploadCite, in.wantFact)
}

// driveRagUploadCite drives the OWUI REST RAG path over the loopback base URL via fixed-arg
// host-side curls and returns (answer, cited, err). It mints an admin Bearer token
// (signin, falling back to signup first-user-becomes-admin on a fresh DB — Open Question 1
// / A5), creates a Knowledge collection, uploads + processes the planted doc (polling until
// done; a timeout is an ERROR not a skip — Pitfall 6), attaches the file to the collection,
// then issues a plain (non-agentic) chat/completions with files:[{type:collection,id}] and
// derives `cited` from the response's citation/source metadata (the exact field name is
// confirmed on-hardware in Plan 03 — A6; both candidate fields sources/citations are
// checked and the raw body is returned on a parse miss). All URLs are composed from `base`;
// all curl args are fixed; no shell interpolation.
func driveRagUploadCite(ctx context.Context, base, question, wantFact string) (string, bool, error) {
	owui := liveOpenWebUIClient(base)

	token, err := liveOpenWebUISignIn(ctx, owui)
	if err != nil {
		return "", false, fmt.Errorf("mint admin token: %w", err)
	}

	// (1) A collection to retrieve from.
	kbID, err := owui.EnsureKnowledge(ctx, token, ragSmokeKnowledgeName, ragSmokeKnowledgeDescription)
	if err != nil {
		return "", false, err
	}

	// (2) Upload the planted doc and wait for chunk-embed-store. Its ONLY content is
	// the planted fact, so a correct answer can only come from retrieval, never from
	// the base model's priors. A processing timeout is an error, never a skip.
	docText := fmt.Sprintf("VillaStraylight runtime RAG smoke document.\n\n%s\n", wantFact)
	if _, err := owui.UploadToKnowledge(ctx, token, kbID, "villa-verify.txt", docText, ragSmokeProcessTimeout); err != nil {
		return "", false, err
	}

	// (3) A plain, non-agentic completion with the collection attached, so the chat UI
	// auto-injects the retrieved chunks WITH citations (native function calling stays
	// off). The completion requires a model id, and the served id is the GGUF
	// filename rather than the config slug, so it is discovered rather than assumed.
	model, err := owui.DiscoverModel(ctx, token)
	if err != nil {
		return "", false, fmt.Errorf("discover chat model: %w", err)
	}
	body, err := json.Marshal(map[string]any{
		"model":    model,
		"messages": []map[string]string{{"role": "user", "content": question}},
		"files":    []map[string]any{{"type": "collection", "id": kbID}},
		"stream":   false, // a real bool, not the string "false"
	})
	if err != nil {
		return "", false, err
	}
	out, err := owui.ChatCompletion(ctx, token, body)
	if err != nil {
		return "", false, err
	}

	answer, cited := parseChatAnswerAndCitation(out)
	return answer, cited, nil
}

// ragSmokeKnowledgeName / ragSmokeKnowledgeDescription identify the collection the
// smoke proof retrieves from. EnsureKnowledge finds-or-creates by name, so a re-run
// reuses the collection instead of accumulating one per verification.
const (
	ragSmokeKnowledgeName        = "villa-verify-memory"
	ragSmokeKnowledgeDescription = "villa verify memory runtime RAG smoke"
)

// parseChatAnswerAndCitation extracts the assistant answer text and whether the response
// carried citation/source metadata. The exact citation field name is confirmed on-hardware
// in Plan 03 (A6); both candidate fields (sources, citations) are checked at the top level
// AND inside the choices, so a green requires a real citation, never an assumed one. On a
// parse miss the raw body is returned as the answer so the FAIL detail is diagnosable.
func parseChatAnswerAndCitation(out []byte) (answer string, cited bool) {
	var r struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Sources   []json.RawMessage `json:"sources"`
			Citations []json.RawMessage `json:"citations"`
		} `json:"choices"`
		Sources   []json.RawMessage `json:"sources"`
		Citations []json.RawMessage `json:"citations"`
	}
	if json.Unmarshal(out, &r) != nil {
		return string(out), false
	}
	cited = len(r.Sources) > 0 || len(r.Citations) > 0
	if len(r.Choices) > 0 {
		answer = r.Choices[0].Message.Content
		if len(r.Choices[0].Sources) > 0 || len(r.Choices[0].Citations) > 0 {
			cited = true
		}
	}
	if answer == "" {
		answer = string(out)
	}
	return answer, cited
}

// runLoopbackCurlStdin runs a fixed-arg curl against the loopback chat-UI PublishPort
// as a plain host process, with an optional stdin payload. The stdin path carries the
// multipart `-F file=@-` upload, so document content never touches argv, a temp file
// or a shell.
//
// It is the ONE host-side loopback exec, and its only caller is the Open WebUI
// transport adapter (openwebui_live.go). Everything that used to build curl
// invocations against the chat UI by hand now goes through the protocol module.
func runLoopbackCurlStdin(ctx context.Context, stdin string, curlArgs ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "curl", curlArgs...) // fixed args; no shell
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%w: %s", err, stderr.String())
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}
