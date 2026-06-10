package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recall"
)

// install_memory.go holds the v1.3 MEMORY-STACK install wiring the `villa install`
// verb gates on the PERSISTED memory_enabled (D-04/D-07/PRIV-04, INFRA-02):
//
//   - nomicEmbedShard: the pinned nomic-embed-text-v1.5 Q8_0 GGUF pre-stage source.
//     With memory on (and not dry-run), install pulls it into the villa-models volume
//     via the existing internal/download path BEFORE starting villa-embed, and only
//     when the file is absent (idempotent). This is the single sanctioned outbound
//     window — a one-time install-time controlled pull; runtime stays ZERO-download
//     (PRIV-04 / D-07). download.PullModel HEAD-verifies size/etag then streams +
//     SHA256-verifies + size-checks + atomically renames, so a half-written/unverified
//     GGUF is never trusted on disk (T-19-06).
//
//   - the two memory service names started after inference + Open WebUI (Qdrant first
//     so embed/OWUI peers can reach it, embed after its GGUF is staged — Pitfall 4).
//
//   - the embed-model presence/path helpers and the proof seam (in install_memory.go's
//     Task-2 half).
//
// The served `-m` path and this pre-stage filename are ONE source of truth: the embed
// Quadlet Exec uses orchestrate.EmbedGGUFFilename(); nomicEmbedShard.Filename MUST equal
// it (asserted unconditionally by TestEmbedGGUFFilenameSingleSource — no literal
// fallback). Binding both ends to the one exported accessor makes them impossible to
// drift (Pitfall 3).

// nomicEmbedShard is the pinned nomic-embed-text-v1.5 Q8_0 GGUF pre-staged into the
// villa-models volume at install (D-07 pre-stage source; PRIV-04 zero runtime download).
//
// Provenance: HuggingFace HEAD 2026-06-09 — SizeBytes is X-Linked-Size, SHA256 is the
// git-LFS oid (X-Linked-Etag, NOT the CDN/Xet chunk ETag — catalog.Shard doc, Pitfall 2).
// The URL is the canonical resolve/main GGUF; download.PullModel verifies size + SHA256
// before the atomic rename, so the file on disk is exactly this content or absent.
//
// The Filename is the on-disk name villa-embed serves via its `-m /models/<filename>`
// Exec; it MUST equal orchestrate.EmbedGGUFFilename() (the single source of truth bound
// at render time) so the staged file and the served path can never drift (Pitfall 3,
// asserted unconditionally by TestEmbedGGUFFilenameSingleSource).
var nomicEmbedShard = catalog.Shard{
	URL:       "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q8_0.gguf",
	Filename:  "nomic-embed-text-v1.5.Q8_0.gguf",
	SHA256:    "3e24342164b3d94991ba9692fdc0dd08e3fd7362e0aacc396a9a5c54a544c3b7",
	SizeBytes: 146146432,
}

// qdrantServiceName is the systemd service the villa-qdrant .container generates
// (Quadlet maps villa-qdrant.container → villa-qdrant.service). It is started BEFORE
// villa-embed so the embedder/OWUI peers can reach the vector store (Pitfall 4),
// after inference + Open WebUI, only when the persisted memory_enabled is true.
const qdrantServiceName = "villa-qdrant.service"

// embedServiceName is the systemd service the villa-embed .container generates
// (Quadlet maps villa-embed.container → villa-embed.service). It is started AFTER
// villa-qdrant and AFTER its GGUF is pre-staged on disk (Pitfall 4) so the embeddings
// llama-server comes up against a present `-m` file (zero runtime download, PRIV-04).
const embedServiceName = "villa-embed.service"

// embedModelPath is the on-disk path of the pre-staged embed GGUF inside the models
// dir. The filename is nomicEmbedShard.Filename (== orchestrate.EmbedGGUFFilename(),
// the single source of truth — Pitfall 3); join with the resolved models dir.
func embedModelPath(modelsDir string) string {
	return filepath.Join(modelsDir, nomicEmbedShard.Filename)
}

// liveEmbedModelPresent reports whether the pre-staged embed GGUF already exists on
// disk AND is intact (the ensureEmbedModel idempotency guard — a present file is never
// re-pulled, strictly-local). It is the live wiring for the embedModelPresent seam.
//
// IN-03 integrity guard: presence requires the on-disk size to MATCH
// nomicEmbedShard.SizeBytes. A present-but-truncated/tampered file (e.g. a leftover from
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
	return fi.Size() >= 0 && uint64(fi.Size()) == nomicEmbedShard.SizeBytes
}

// liveEnsureEmbedModel pre-stages nomicEmbedShard into modelsDir via the verified
// downloader the `model pull`/`model swap` path uses (the pullFn seam), wrapping the
// shard in a single-shard CatalogModel (D-07). It creates the models dir 0700 first
// (mirroring liveInstallDeps.ensureModel). download.PullModel does the HEAD size/etag
// verify → stream → SHA256 + size check → atomic rename, so a half-written or
// unverified GGUF is never left on disk (T-19-06). It is invoked only when memory is
// on, not dry-run, and the file is absent.
func liveEnsureEmbedModel(modelsDir string) error {
	if mkErr := os.MkdirAll(modelsDir, 0o700); mkErr != nil {
		return mkErr
	}
	m := catalog.CatalogModel{Shards: []catalog.Shard{nomicEmbedShard}}
	return pullFn(context.Background(), m, modelsDir)
}

// liveLoadedConfig returns the PERSISTED config.LoadVilla() so runInstall can SEED cfg
// from the user's on-disk config (preserving their memory/dashboard/chat fields) rather
// than the always-default DefaultVillaConfig() seed (WR-02). A load error fails SOFT to
// typed defaults: an unreadable/absent config yields the same loopback dashboard/chat +
// memory-off defaults the DefaultVillaConfig() seed gave, so a first-install host is
// byte-for-byte unchanged, while a host WITH a persisted config has its customizations
// honored through saveConfig. LoadVilla self-heals zeroed dashboard/chat fields.
func liveLoadedConfig() config.VillaConfig {
	c, err := config.LoadVilla()
	if err != nil {
		return config.DefaultVillaConfig()
	}
	return c
}

// liveLoadedMemoryEnabled returns the PERSISTED config.LoadVilla().MemoryEnabled — the
// AUTHORITATIVE memory gate source threaded into runInstall (NOT the DefaultVillaConfig()
// seed, which is false by construction). A config load error fails SOFT to false so a
// broken config never silently enables the memory stack (an opted-in user must have a
// readable config). This is the exact fix for the silent-failure risk (T-19-16): the gate
// reflects the user's opt-in, not the seed's hard-coded false.
func liveLoadedMemoryEnabled() bool {
	c, err := config.LoadVilla()
	if err != nil {
		return false
	}
	return c.MemoryEnabled
}

// warnRecallEmbeddingSkew is the Phase-23 D-10/D-11 read-only WARN surface for the
// install memory readiness flow (CTRL-05, T-23-18): when the recall-state stamp
// records an embedding identity that CONFIDENTLY diverges from the configured one,
// print a WARN naming both identities, the consequence, and the remediation — and
// do NOTHING else. Never a block (the caller's exit code is untouched), never a
// state write, never a service mutation, never an auto-reindex (the Phase-22
// diagnose-don't-mutate posture). The comparison is the single Plan 23-01 helper
// (recall.EmbeddingSkew) — never re-rolled here. Typed-Unknown discipline: a nil
// seam (test doubles), an unreadable state (real I/O fault), or an empty stamp all
// degrade SILENTLY — no recorded truth means no alarm, and an unevaluable signal
// is never escalated to a fabricated warning.
func warnRecallEmbeddingSkew(errOut io.Writer, cfg config.VillaConfig, readRecallState func() (recall.State, error)) {
	if readRecallState == nil {
		return // seam absent (mirrors the doctor optional-seam pattern) — silent
	}
	st, err := readRecallState()
	if err != nil {
		return // unevaluable read ⇒ typed-Unknown ⇒ silent (never a fabricated alarm, never a block)
	}
	if recall.EmbeddingSkew(st, cfg.EmbeddingModel, cfg.EmbeddingDim) != recall.SkewMismatch {
		return // match or empty stamp (SkewUnknown) — nothing to warn about
	}
	fmt.Fprintf(errOut, "install: WARN: the recall index was built with %s (dim %d) but config now says %s (dim %d) — retrieval from the existing collection is corrupt until re-index; run `villa recall index --rebuild` to re-index, or revert embedding_model/embedding_dim in config.toml.\n",
		st.EmbeddingModel, st.EmbeddingDim, cfg.EmbeddingModel, cfg.EmbeddingDim)
}

// --- Memory-stack readiness proof (Task 2 / D-09, SC#2/SC#3) -----------------
//
// The proof asserts the memory stack is honestly healthy BEFORE install declares
// success: an OFFLINE 768-length /v1/embeddings vector (the embedder serves the
// pre-staged GGUF with no runtime download) AND a Qdrant writable round-trip (PUT +
// DELETE a 768-dim probe collection — /readyz alone is insufficient for SC#2). A FAIL
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
// 768 dim). Values are config-resolved, never shell-interpolated (T-19-10).
type memoryProofInput struct {
	embedAddr    string
	embedPort    int
	embedModel   string
	embeddingDim int
	qdrantAddr   string
	qdrantPort   int
}

// evalMemoryProof is the PURE proof core (unit-testable off-hardware via injected
// probes): it maps the two probe outcomes to a verdict. An embed error or a wrong
// vector length → FAIL("…embeddings endpoint…"); a Qdrant error or a non-writable
// store → FAIL("…Qdrant not writable…"); both ok → PASS. wantDim is the pinned 768.
func evalMemoryProof(_ context.Context, embedProbe func() (gotDim int, err error), qdrantProbe func() (writable bool, err error), wantDim int) memoryProof {
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
	return memoryProof{status: preflight.StatusPass, detail: "768-dim embeddings + Qdrant writable"}
}

// memoryProofNetwork is the podman network the proof reaches the container-DNS-only
// memory services over (villa-embed / villa-qdrant publish NO host port — SC#4/PRIV-01).
// It matches orchestrate's NetworkName (the Quadlet villa.network unit's NetworkName=villa);
// a config-value name, not a backend image/device literal, so it stays seam-clean.
const memoryProofNetwork = "villa"

// villaProbeCollection is the throwaway 768-dim Qdrant collection the writable round-trip
// creates and deletes — proving the named volume is writable by the container UID (SC#2),
// leaving no stray state behind.
const villaProbeCollection = "villa-probe"

// liveMemoryProof is the production proof seam: it reaches the container-DNS-only
// villa-embed / villa-qdrant over villa.network via a one-shot `podman run --rm --network
// villa` curl (no host port is opened — T-19-11), sourcing the helper image from the
// orchestrate accessor (EmbedImage(), which ships curl) rather than a re-typed image
// literal (T-19-10, keeps TestSeamGrepGate green). Every podman/curl arg is FIXED; the
// JSON body is a constant and the model id is config-resolved, never shell-interpolated.
func liveMemoryProof(ctx context.Context, in memoryProofInput) memoryProof {
	helperImage := orchestrate.EmbedImage()

	// embedProbe POSTs the fixed /v1/embeddings body and returns len(data[0].embedding).
	embedProbe := func() (int, error) {
		body, err := json.Marshal(map[string]any{
			"input":           "villa memory readiness probe",
			"model":           in.embedModel,
			"encoding_format": "float",
		})
		if err != nil {
			return 0, err
		}
		url := fmt.Sprintf("http://%s:%d/v1/embeddings", in.embedAddr, in.embedPort)
		out, err := runProbeCurl(ctx, helperImage,
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
	// idempotency ordering is unit-testable off-hardware (WR-03).
	qdrantProbe := func() (bool, error) {
		base := fmt.Sprintf("http://%s:%d", in.qdrantAddr, in.qdrantPort)
		curl := func(args ...string) ([]byte, error) { return runProbeCurl(ctx, helperImage, args...) }
		return qdrantWritableProbe(curl, base, in.embeddingDim)
	}

	return evalMemoryProof(ctx, embedProbe, qdrantProbe, in.embeddingDim)
}

// probeCurlFn is the injectable curl-runner seam qdrantWritableProbe drives: it runs a
// fixed-arg curl (in production, `podman run --rm --network villa <img> curl <args...>`)
// and returns curl's stdout. Tests inject a fake to simulate a leftover probe collection.
type probeCurlFn func(args ...string) ([]byte, error)

// qdrantWritableProbe is the PURE Qdrant writable round-trip (unit-testable via an
// injected probeCurlFn): assert /readyz, then prove the named volume is writable by
// creating the probe collection and deleting it. It is IDEMPOTENT (WR-03): it issues a
// best-effort DELETE of any STALE probe collection BEFORE the PUT-create, so a leftover
// villa-probe collection from an interrupted prior run (whose cleanup DELETE never ran)
// can NOT make the create return a non-2xx and hard-block install on a perfectly writable
// Qdrant. The pre-DELETE result is intentionally ignored (a no-op on a clean store). Every
// curl invocation is fixed-arg (no shell interpolation, T-19-10).
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
	// Idempotency (WR-03): clear any stale leftover collection first (best-effort).
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

// runProbeCurl runs `podman run --rm --network villa <helperImage> curl <args...>` as a
// FIXED-ARG exec (never a shell, T-19-10) and returns curl's stdout. The helper image is
// sourced from the orchestrate accessor (no re-typed image literal). --entrypoint curl
// runs curl from inside the network so the container-DNS-only services are reachable
// WITHOUT opening a host port (T-19-11).
func runProbeCurl(ctx context.Context, helperImage string, curlArgs ...string) ([]byte, error) {
	args := []string{
		"run", "--rm",
		"--network", memoryProofNetwork,
		"--entrypoint", "curl",
		helperImage,
	}
	args = append(args, curlArgs...)
	cmd := exec.CommandContext(ctx, "podman", args...) // fixed args; no shell
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
