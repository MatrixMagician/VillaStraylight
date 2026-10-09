package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/inprobe"
	"github.com/MatrixMagician/VillaStraylight/internal/install"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// install_memory.go holds the v1.3 MEMORY-STACK install wiring the `villa install`
// verb gates on the PERSISTED memory_enabled (INFRA-02):
//
//   - install.NomicEmbedShard: the pinned nomic-embed-text-v1.5 Q8_0 GGUF pre-stage source.
//     With memory on (and not dry-run), install pulls it into the villa-models volume
//     via the existing internal/download path BEFORE starting villa-embed, and only
//     when the file is absent (idempotent). This is the single sanctioned outbound
//     window — a one-time install-time controlled pull; runtime stays ZERO-download
// download.PullModel HEAD-verifies size/etag then streams +
//     SHA256-verifies + size-checks + atomically renames, so a half-written/unverified
// GGUF is never trusted on disk.
//
//   - the two memory service names started after inference + Open WebUI (Qdrant first
//     so embed/OWUI peers can reach it, embed after its GGUF is staged — Pitfall 4).
//
//   - the embed-model presence/path helpers and the proof seam (in install_memory.go's
//     Task-2 half).
//
// The served `-m` path and this pre-stage filename are ONE source of truth: the embed
// Quadlet Exec uses orchestrate.EmbedGGUFFilename(); install.NomicEmbedShard.Filename MUST equal
// it (asserted unconditionally by TestEmbedGGUFFilenameSingleSource — no literal
// fallback). Binding both ends to the one exported accessor makes them impossible to
// drift (Pitfall 3).

// qdrantServiceName is the systemd service the villa-qdrant .container generates
// (Quadlet maps villa-qdrant.container → villa-qdrant.service). It is started BEFORE
// villa-embed so the embedder/OWUI peers can reach the vector store (Pitfall 4),
// after inference + Open WebUI, only when the persisted memory_enabled is true.
const qdrantServiceName = "villa-qdrant.service"

// embedServiceName is the systemd service the villa-embed .container generates
// (Quadlet maps villa-embed.container → villa-embed.service). It is started AFTER
// villa-qdrant and AFTER its GGUF is pre-staged on disk (Pitfall 4) so the embeddings
// llama-server comes up against a present `-m` file (zero runtime download).
const embedServiceName = "villa-embed.service"

// embedModelPath is the on-disk path of the pre-staged embed GGUF inside the models
// dir. The filename is install.NomicEmbedShard.Filename (== orchestrate.EmbedGGUFFilename(),
// the single source of truth — Pitfall 3); join with the resolved models dir.
func embedModelPath(modelsDir string) string {
	return filepath.Join(modelsDir, install.NomicEmbedShard.Filename)
}

// liveEmbedModelPresent reports whether the pre-staged embed GGUF already exists on
// disk AND is intact (the ensureEmbedModel idempotency guard — a present file is never
// re-pulled, strictly-local). It is the live wiring for the embedModelPresent seam.
//
// integrity guard: presence requires the on-disk size to MATCH
// install.NomicEmbedShard.SizeBytes. A present-but-truncated/tampered file (e.g. a leftover from
// a kill between rename steps, or manual tampering) would otherwise be treated as
// "present, never re-pulled" and villa-embed would crash-loop on the bad weight. A size
// mismatch → not present → the verified download path re-pulls (download.PullModel then
// re-verifies size + SHA256 + atomic-renames). This is a cheap stat-only guard; it does
// NOT re-hash on every install (a full re-hash would be wasteful and is not warranted).
func liveEmbedModelPresent(modelsDir string) bool {
	fi, err := os.Stat(embedModelPath(modelsDir))
	if err != nil {
		return false
	}
	// A size mismatch means a truncated/tampered file → treat as absent so it is re-pulled
	// and re-verified, rather than trusting a corrupt weight. fi.Size() is non-negative for
	// a regular file, so the uint64 conversion is safe.
	return fi.Size() >= 0 && uint64(fi.Size()) == install.NomicEmbedShard.SizeBytes
}

// liveEnsureEmbedModel pre-stages install.NomicEmbedShard into modelsDir via the verified
// downloader the `model pull`/`model swap` path uses (the pullFn seam), wrapping the
// shard in a single-shard catalog.Model. It creates the models dir 0700 first
// (mirroring liveInstallDeps.ensureModel). download.PullModel does the HEAD size/etag
// verify → stream → SHA256 + size check → atomic rename, so a half-written or
// unverified GGUF is never left on disk. It is invoked only when memory is
// on, not dry-run, and the file is absent.
// ctx is the command's cancellation context, so Ctrl-C interrupts the transfer;
// the partial ".part" file survives and resumes via HTTP Range.
func liveEnsureEmbedModel(ctx context.Context, modelsDir string) error {
	if mkErr := os.MkdirAll(modelsDir, 0o700); mkErr != nil {
		return mkErr
	}
	m := catalog.Model{Shards: []catalog.Shard{install.NomicEmbedShard}}
	return pullFn(ctx, m, modelsDir)
}

// rerankModelPath is the on-disk path of the pre-staged reranker GGUF.
func rerankModelPath(modelsDir string) string {
	return filepath.Join(modelsDir, install.RerankShard.Filename)
}

// liveRerankModelPresent is liveEmbedModelPresent for the reranker shard: present
// only at the pinned size, so a truncated file is re-pulled and re-verified.
func liveRerankModelPresent(modelsDir string) bool {
	fi, err := os.Stat(rerankModelPath(modelsDir))
	if err != nil {
		return false
	}
	return fi.Size() >= 0 && uint64(fi.Size()) == install.RerankShard.SizeBytes
}

// liveEnsureRerankModel pre-stages install.RerankShard through the same verified
// downloader as the embedder's shard.
func liveEnsureRerankModel(ctx context.Context, modelsDir string) error {
	if mkErr := os.MkdirAll(modelsDir, 0o700); mkErr != nil {
		return mkErr
	}
	m := catalog.Model{Shards: []catalog.Shard{install.RerankShard}}
	return pullFn(ctx, m, modelsDir)
}

// liveLoadedConfig returns the PERSISTED config.LoadVilla() so runInstall can SEED cfg
// from the user's on-disk config (preserving their memory/dashboard/chat fields) rather
// than the always-default DefaultVillaConfig seed. LoadVilla self-heals zeroed
// dashboard/chat fields, and an ABSENT config is not an error there — it returns the
// typed defaults — so a first-install host is byte-for-byte unchanged.
//
// A load error PROPAGATES. It used to fail soft to typed defaults, which meant a
// hand-edited or unreadable config.toml installed from defaults instead of refusing:
// install would render and write Quadlet units, and persist a config, from input it had
// just failed to read. The repo convention for untrusted input is to fail closed —
// error, never a silent default — and this is the seam where a silent default is most
// expensive.
func liveLoadedConfig() (config.VillaConfig, error) {
	return config.LoadVilla()
}

// liveLoadedMemoryEnabled returns the PERSISTED config.LoadVilla().MemoryEnabled — the
// AUTHORITATIVE memory gate source threaded into runInstall (NOT the DefaultVillaConfig()
// seed, which is false by construction). A config load error fails SOFT to false so a
// broken config never silently enables the memory stack (an opted-in user must have a
// readable config). This is the exact fix for the silent-failure risk: the gate
// reflects the user's opt-in, not the seed's hard-coded false.
func liveLoadedMemoryEnabled() bool {
	c, err := config.LoadVilla()
	if err != nil {
		return false
	}
	return subsystem.MemoryOn(c)
}

// --- Memory-stack readiness proof (Task 2) -----------------
//
// The proof asserts the memory stack is honestly healthy BEFORE install declares
// success: an OFFLINE 768-length /v1/embeddings vector (the embedder serves the
// pre-staged GGUF with no runtime download) AND a Qdrant writable round-trip (PUT +
// DELETE a 768-dim probe collection — /readyz alone is insufficient for). A FAIL
// refuses-with-remediation (the caller returns exitBlocked), NEVER a silent skip or a
// false-green (honesty-by-construction). It mirrors the installReadiness verdict shape.

// memoryProof is the memory-stack readiness verdict (mirrors installReadiness): PASS
// when both the embed vector length and the Qdrant write succeed, FAIL with a
// remediation detail otherwise. There is no WARN — a memory stack that cannot answer
// 768-dim embeddings or accept a write is a confident known-bad the user opted into.
type memoryProof struct {
	status preflight.Status
	detail string
}

// memoryProofInput carries the resolved memory addresses/ports/model/dim the proof
// probes (from the persisted config — container-DNS names on villa.network + the pinned
// 768 dim). Values are config-resolved, never shell-interpolated.
type memoryProofInput struct {
	embedAddr    string
	embedPort    int
	embedModel   string
	embeddingDim int
	qdrantAddr   string
	qdrantPort   int
	// rerank is subsystem.RerankOn: only a rendered reranker is probed.
	rerank     bool
	rerankAddr string
	rerankPort int
	// extract is subsystem.ExtractOn: only a rendered extractor is probed.
	extract     bool
	extractAddr string
	extractPort int
}

// rerankServiceName is the systemd service the villa-rerank .container generates.
const rerankServiceName = "villa-rerank.service"

// rerankProbeDocuments is the fixed pair the readiness probe asks the reranker to
// order against rerankProbeQuery: the first is on topic, the second is not, so a
// reranker that answers but cannot tell them apart fails the proof.
var rerankProbeDocuments = []string{
	"The dashboard listens on port 8888.",
	"Bake the loaf for forty minutes at 180 degrees.",
}

const rerankProbeQuery = "which port does the dashboard bind"

// extractServiceName is the systemd service the villa-extract .container generates.
const extractServiceName = "villa-extract.service"

// extractProbeBody is the plain-text document the readiness probe hands the
// extractor; the proof passes only when the extracted text carries it.
const extractProbeBody = "villa extractor readiness probe"

// evalMemoryProof is the PURE proof core (unit-testable off-hardware via injected
// probes): it maps the probe outcomes to a verdict. An embed error or a wrong
// vector length → FAIL("…embeddings endpoint…"); a Qdrant error or a non-writable
// store → FAIL("…Qdrant not writable…"); a reranker error or a reranker that does
// not put the on-topic document first → FAIL naming villa-rerank.service; an
// extractor error or an extraction without the probe sentence → FAIL naming
// villa-extract.service; all ok → PASS. rerankProbe and extractProbe are nil when
// their unit is not rendered, and then not probed. wantDim is the pinned 768.
func evalMemoryProof(_ context.Context, embedProbe func() (gotDim int, err error), qdrantProbe func() (writable bool, err error), rerankProbe func() (top int, err error), extractProbe func() (text string, err error), wantDim int) memoryProof {
	gotDim, err := embedProbe()
	if err != nil {
		return memoryProof{
			status: preflight.StatusFail,
			detail: fmt.Sprintf("the embeddings endpoint did not answer (%v) — check `systemctl --user status %s` and its journal, then re-run `villa install`", err, embedServiceName),
		}
	}
	if gotDim != wantDim {
		return memoryProof{
			status: preflight.StatusFail,
			detail: fmt.Sprintf("the embeddings endpoint returned a %d-dim vector, expected %d — the embedder is misconfigured (pooling/model mismatch); check `systemctl --user status %s`, then re-run `villa install`", gotDim, wantDim, embedServiceName),
		}
	}
	writable, err := qdrantProbe()
	if err != nil {
		return memoryProof{
			status: preflight.StatusFail,
			detail: fmt.Sprintf("Qdrant did not answer (%v) — check `systemctl --user status %s` and its journal, then re-run `villa install`", err, qdrantServiceName),
		}
	}
	if !writable {
		return memoryProof{
			status: preflight.StatusFail,
			detail: fmt.Sprintf("Qdrant is not writable (the probe collection round-trip failed) — check the volume permissions and `systemctl --user status %s`, then re-run `villa install`", qdrantServiceName),
		}
	}
	detail := "768-dim embeddings + Qdrant writable"
	if rerankProbe != nil {
		top, err := rerankProbe()
		if err != nil {
			return memoryProof{
				status: preflight.StatusFail,
				detail: fmt.Sprintf("the reranker did not answer (%v) — check `systemctl --user status %s` and its journal, then re-run `villa install`", err, rerankServiceName),
			}
		}
		if top != 0 {
			return memoryProof{
				status: preflight.StatusFail,
				detail: fmt.Sprintf("the reranker ranked the off-topic probe document first (index %d) — the served model is not a reranker; check `systemctl --user status %s`, then re-run `villa install`", top, rerankServiceName),
			}
		}
		detail += " + reranker ranking"
	}
	if extractProbe != nil {
		text, err := extractProbe()
		if err != nil {
			return memoryProof{
				status: preflight.StatusFail,
				detail: fmt.Sprintf("the extractor did not answer (%v) — check `systemctl --user status %s` and its journal, then re-run `villa install`", err, extractServiceName),
			}
		}
		if !strings.Contains(text, extractProbeBody) {
			return memoryProof{
				status: preflight.StatusFail,
				detail: fmt.Sprintf("the extractor returned no text for the probe document — check `systemctl --user status %s`, then re-run `villa install`", extractServiceName),
			}
		}
		detail += " + extractor text"
	}
	return memoryProof{status: preflight.StatusPass, detail: detail}
}

// topScore is the index of the highest score. Scores are raw logits, so the
// highest may be negative.
func topScore(scores []float64) int {
	top := 0
	for i, s := range scores {
		if s > scores[top] {
			top = i
		}
	}
	return top
}

// memoryProofNetwork is the podman network the proof reaches the container-DNS-only
// memory services over (villa-embed / villa-qdrant publish NO host port).
// It matches orchestrate's NetworkName (the Quadlet villa.network unit's NetworkName=villa);
// a config-value name, not a backend image/device literal, so it stays seam-clean.
const memoryProofNetwork = "villa"

// villaProbeCollection is the throwaway 768-dim Qdrant collection the writable round-trip
// creates and deletes — proving the named volume is writable by the container UID,
// leaving no stray state behind.
const villaProbeCollection = "villa-probe"

// memoryProofDeps is the in-network seam the memory proof drives: the curl exec
// (live: a `podman run --rm --network villa` curl), the helper image it runs, and
// the bound on waiting for a llama-server to finish loading its model.
type memoryProofDeps struct {
	exec     inprobe.Exec
	image    string
	timeout  time.Duration
	interval time.Duration
}

// liveMemoryProof is the production proof seam: it reaches the container-DNS-only
// villa-embed / villa-qdrant over villa.network via a one-shot `podman run --rm --network
// villa` curl (no host port is opened), sourcing the helper image from the
// orchestrate accessor (EmbedImage(), which ships curl) rather than a re-typed image
// literal (keeps TestSeamGrepGate green). Every podman/curl arg is FIXED; the
// JSON body is a constant and the model id is config-resolved, never shell-interpolated.
func liveMemoryProof(ctx context.Context, in memoryProofInput) memoryProof {
	return memoryProofWith(ctx, memoryProofDeps{
		exec:     runProbeCurlCode,
		image:    orchestrate.EmbedImage(),
		timeout:  readinessTimeout,
		interval: readinessInterval,
	}, in)
}

// memoryProofWith drives the memory proof through d; liveMemoryProof binds the
// host, tests a fake network.
func memoryProofWith(ctx context.Context, d memoryProofDeps, in memoryProofInput) memoryProof {
	curl := func(args ...string) ([]byte, error) {
		out, _, err := d.exec(ctx, d.image, args...)
		return out, err
	}

	// awaitHealth polls a llama-server's /health until it answers 200, bounded like
	// the inference readiness poll. llama-server answers 503 while its model loads
	// and refuses the connection for the first instants after its container starts;
	// a request sent in that window fails, so the proof waits before its first one.
	awaitHealth := func(addr string, port int) error {
		url := fmt.Sprintf("http://%s:%d/health", addr, port)
		probe := func() (int, error) {
			out, _, err := d.exec(ctx, d.image, "-s", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "5", url)
			if err != nil {
				return 0, err
			}
			return strconv.Atoi(strings.TrimSpace(string(out)))
		}
		if ready := pollReadiness(ctx, probe, d.timeout, d.interval); ready.Status != preflight.StatusPass {
			return fmt.Errorf("not ready after %s: %s", d.timeout, ready.Detail)
		}
		return nil
	}

	// embedProbe POSTs the fixed /v1/embeddings body and returns len(data[0].embedding).
	embedProbe := func() (int, error) {
		if err := awaitHealth(in.embedAddr, in.embedPort); err != nil {
			return 0, err
		}
		body, err := json.Marshal(map[string]any{
			"input":           "villa memory readiness probe",
			"model":           in.embedModel,
			"encoding_format": "float",
		})
		if err != nil {
			return 0, err
		}
		url := fmt.Sprintf("http://%s:%d/v1/embeddings", in.embedAddr, in.embedPort)
		out, err := curl(
			"-sf", "-X", "POST", url,
			"-H", "Content-Type: application/json",
			"-d", string(body),
		)
		if err != nil {
			return 0, err
		}
		var resp struct {
			Data []struct {
				Embedding []float64 `json:"embedding"`
			} `json:"data"`
		}
		if jerr := json.Unmarshal(out, &resp); jerr != nil {
			return 0, fmt.Errorf("decode embeddings response: %w", jerr)
		}
		if len(resp.Data) == 0 {
			return 0, fmt.Errorf("embeddings response carried no data[]")
		}
		return len(resp.Data[0].Embedding), nil
	}

	// qdrantProbe asserts /readyz then (DELETE-)PUT + DELETE the probe collection,
	// delegating the writable round-trip to the pure qdrantWritableProbe so the
	// idempotency ordering is unit-testable off-hardware.
	qdrantProbe := func() (bool, error) {
		base := fmt.Sprintf("http://%s:%d", in.qdrantAddr, in.qdrantPort)
		return qdrantWritableProbe(curl, base, in.embeddingDim)
	}

	// rerankProbe scores the fixed two-document pair and returns the top index.
	var rerankProbe func() (int, error)
	if in.rerank {
		rerankProbe = func() (int, error) {
			if err := awaitHealth(in.rerankAddr, in.rerankPort); err != nil {
				return 0, err
			}
			scores, err := postRerank(ctx, d.exec, d.image, in.rerankAddr, in.rerankPort, rerankProbeQuery, rerankProbeDocuments)
			if err != nil {
				return 0, err
			}
			return topScore(scores), nil
		}
	}

	// extractProbe hands the extractor the fixed plain-text document and returns
	// the text it extracted.
	var extractProbe func() (string, error)
	if in.extract {
		extractProbe = func() (string, error) {
			return postExtract(ctx, helperImage, in.extractAddr, in.extractPort, "text/plain", []byte(extractProbeBody))
		}
	}

	return evalMemoryProof(ctx, embedProbe, qdrantProbe, rerankProbe, extractProbe, in.embeddingDim)
}

// probeCurlFn is the injectable curl-runner seam qdrantWritableProbe drives: it runs a
// fixed-arg curl (in production, `podman run --rm --network villa <img> curl <args...>`)
// and returns curl's stdout. Tests inject a fake to simulate a leftover probe collection.
type probeCurlFn func(args ...string) ([]byte, error)

// qdrantWritableProbe is the PURE Qdrant writable round-trip (unit-testable via an
// injected probeCurlFn): assert /readyz, then prove the named volume is writable by
// creating the probe collection and deleting it. It is IDEMPOTENT: it issues a
// best-effort DELETE of any STALE probe collection BEFORE the PUT-create, so a leftover
// villa-probe collection from an interrupted prior run (whose cleanup DELETE never ran)
// can NOT make the create return a non-2xx and hard-block install on a perfectly writable
// Qdrant. The pre-DELETE result is intentionally ignored (a no-op on a clean store). Every
// curl invocation is fixed-arg (no shell interpolation).
func qdrantWritableProbe(curl probeCurlFn, base string, embeddingDim int) (bool, error) {
	if _, err := curl("-sf", base+"/readyz"); err != nil {
		return false, fmt.Errorf("/readyz: %w", err)
	}
	coll := base + "/collections/" + villaProbeCollection
	putBody, err := json.Marshal(map[string]any{
		"vectors": map[string]any{
			"size":     embeddingDim,
			"distance": "Cosine",
		},
	})
	if err != nil {
		return false, err
	}
	// Idempotency: clear any stale leftover collection first (best-effort).
	_, _ = curl("-sf", "-X", "DELETE", coll)
	if _, err := curl(
		"-sf", "-X", "PUT", coll,
		"-H", "Content-Type: application/json",
		"-d", string(putBody),
	); err != nil {
		return false, fmt.Errorf("create probe collection: %w", err)
	}
	// Best-effort cleanup so no stray state remains; a delete failure does not
	// negate the proven write (the create already proved writability).
	_, _ = curl("-sf", "-X", "DELETE", coll)
	return true, nil
}

// runProbeCurlCode runs `podman run --rm --network villa --entrypoint curl
// <helperImage> curl <args...>` as a FIXED-ARG exec (never a shell) and returns
// curl's stdout together with the process exit code.
//
// It is one of three thin doors onto probeCurl, the ONE in-network probe strategy.
// There used to be three strategies: this one, a return-only-stdout twin that was
// otherwise byte-identical, and a third in the status path that called the twin and
// then re-derived the exit code the twin had discarded. runProbeCurl below is a thin
// convenience for the callers that genuinely do not care about the code, and
// runProbeCurlIn the door for a request the inference client built.
func runProbeCurlCode(ctx context.Context, helperImage string, curlArgs ...string) (stdout []byte, exitCode int, err error) {
	return probeCurl(ctx, helperImage, nil, curlArgs)
}

// runProbeCurlIn runs one llama-server request the inference client built
// (ADR-0014), after the caller's own curl flags. The request's Stdin — the
// Authorization header curl reads through `-H @-` — reaches curl over a pipe
// (`podman run -i`), so the api key never lands on podman's or curl's command line,
// where any local user could read it from /proc (#252).
func runProbeCurlIn(ctx context.Context, helperImage string, req inference.CurlRequest, flags ...string) ([]byte, error) {
	if req.Err != nil {
		return nil, req.Err
	}
	out, _, err := probeCurl(ctx, helperImage, req.Stdin, slices.Concat(flags, req.Args))
	return out, err
}

// probeContainerName is a fresh name per drive, so a cancelled drive stops its own
// container and never one belonging to a drive running beside it.
func probeContainerName() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "villa-probe-" + hex.EncodeToString(b[:])
}

// removeProbeContainer force-removes a probe container after its context was
// cancelled. Killing the `podman run` client does not stop the container, and its
// curl would keep llama-server decoding a round nothing waits for (#329). The removal
// runs on a fresh bounded context, since the caller's is already done; a failure is
// ignored because the container may have exited and --rm taken it.
func removeProbeContainer(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, "podman", "rm", "-f", name).Run() // fixed args; no shell
}

// probeCurl is the in-network probe itself. A nil stdin runs the helper exactly as
// it always ran; a non-nil one adds `-i` and pipes it to curl.
//
// The helper image is sourced from the orchestrate accessor (no re-typed image
// literal), and --entrypoint curl runs curl from INSIDE villa.network, so the
// container-DNS-only services are reachable without opening a host port.
//
// The exit code is what makes the egress negative control honest. podman propagates
// the container process's (curl's) exit code, so a curl CONNECTION/TIMEOUT (6/7/28)
// reads as a genuine block, while a container that never started reports -1 and must
// be read as infrastructure — "the probe could not run", never "the host was
// blocked".
func probeCurl(ctx context.Context, helperImage string, stdin []byte, curlArgs []string) (stdout []byte, exitCode int, err error) {
	name := probeContainerName()
	args := []string{"run", "--rm", "--name", name}
	if stdin != nil {
		args = append(args, "-i")
	}
	args = append(args,
		"--network", memoryProofNetwork,
		"--entrypoint", "curl",
		helperImage,
	)
	if dl, ok := ctx.Deadline(); ok {
		args = append(args, "--max-time", strconv.Itoa(max(1, int(math.Ceil(time.Until(dl).Seconds())))))
	}
	args = append(args, curlArgs...)
	cmd := exec.CommandContext(ctx, "podman", args...) // fixed args; no shell
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if runErr == nil {
		return out.Bytes(), 0, nil
	}
	if ctx.Err() != nil {
		removeProbeContainer(name)
	}
	code := inprobe.ExitCode(runErr)
	if stderr.Len() > 0 {
		return out.Bytes(), code, fmt.Errorf("%w: %s", runErr, stderr.String())
	}
	return out.Bytes(), code, runErr
}

// runProbeCurl is runProbeCurlCode for the callers that only need stdout and an
// error. It exists so those call sites stay readable, not as a second strategy.
func runProbeCurl(ctx context.Context, helperImage string, curlArgs ...string) ([]byte, error) {
	out, _, err := runProbeCurlCode(ctx, helperImage, curlArgs...)
	return out, err
}
