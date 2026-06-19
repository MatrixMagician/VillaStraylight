package orchestrate

// openwebui.go holds the Open WebUI MANAGED-SERVICE constants and view builder
// (Phase-4 D-01). Open WebUI is NOT an inference Backend: it is a fixed OSS managed
// service, so its image, env block, ports, and volume mount are orchestrate-level
// managed-service constants — a DIFFERENT category from the GPU/Vulkan backend
// literals that internal/inference's TestSeamGrepGate guards. Living here keeps the
// inference seam gate green while giving Render() a dedicated, parseContainerArgs-free
// render path (Pitfall 4: Open WebUI has no GPU device passthrough, no supplemental
// group/seccomp args, and no custom Exec — it runs the image entrypoint).
//
// Phase-20 (INFRA-03, D-01..D-09): buildOpenWebUIView is parameterized by the
// resolved memory render-view + a memoryEnabled flag. When memory is OFF the env
// block is byte-identical to the v1.2 render (Phase-18 SC#1 continuity, D-04);
// when memory is ON a SINGLE ordered group of D-09 RAG/Qdrant/memory keys is
// APPENDED after the existing entries (D-02, append-only). The appended group is
// byte-frozen by villa-openwebui.container.memory.golden + the memory-aware
// TestRenderOpenWebUITelemetryFrozen (D-05). All endpoint values are composed from
// the memory.MemoryRenderInput pieces (QdrantAddr/QdrantPort/EmbedAddr/EmbedPort/
// EmbeddingModel) with fmt — NO re-typed villa-qdrant/villa-embed/port host literals,
// so TestSeamGrepGate stays green (these are config-sourced values, not GPU/image
// tokens).
//
// Phase-30 (SRCH-02/SRCH-03, D-01..D-04): buildOpenWebUIView is additionally
// parameterized by the resolved web-search inputs (webSearchEnabled flag +
// searxngAddr/searxngPort/webSearchResultCount, all config-threaded). When web
// search is ON a SECOND ordered group of OWUI native web-search keys is APPENDED
// (independent of the memory group, append-only): the enable key, the engine key
// (searxng), the SearXNG query-URL key, and the result-count key. The query URL is
// composed via fmt.Sprintf from the config-threaded searxngAddr/searxngPort — NO
// re-typed host literal (WR-01, so TestSeamGrepGate stays green). The <query> token
// is OWUI's literal substitution placeholder, kept verbatim.
//
// The MANDATORY load-bearing key is ENABLE_PERSISTENT_CONFIG=False (D-04): it is
// emitted exactly ONCE, LAST, gated on memoryEnabled || webSearchEnabled. ALL of
// the appended memory keys AND the appended web-search keys are DB-backed
// PersistentConfig ConfigVars — without this trailing gate they seed the OWUI DB
// once and the env is silently ignored after first boot, so "config is the single
// source of truth" (INFRA-03) would NOT hold; its absence (or duplication, or being
// dropped when web search is on but memory is off) is a phase failure (T-20-01,
// extended to the web-search ConfigVars).

import (
	"fmt"
	"strconv"

	"github.com/MatrixMagician/VillaStraylight/internal/memory"
)

// openWebUIImage is the digest-pinned Open WebUI chat-UI image (CLAUDE.md prescribed:
// ghcr.io/open-webui/open-webui:main, pin a digest). Resolved on the dev box
// 2026-06-05 via `podman pull ghcr.io/open-webui/open-webui:main &&
// podman image inspect ghcr.io/open-webui/open-webui:main --format '{{index .RepoDigests 0}}'`.
// The :main tag is silently rebuilt; the digest is not (D-07 reproducibility).
// PRIV-02 re-audit-on-bump is enforced structurally: TestRenderOpenWebUITelemetryFrozen
// + the container golden FAIL on any env-block change, forcing a deliberate re-audit
// of the telemetry-kill set whenever this digest is bumped.
const openWebUIImage = "ghcr.io/open-webui/open-webui:main@sha256:7f1b0a1a50cfbac23da3b16f96bc968fd757b26dc9e54e93813d61768ea9184e"

// OpenWebUIImage returns the digest-pinned Open WebUI image so callers (the
// Phase-16 backup manifest, D-10) can record it WITHOUT re-typing the literal.
// The literal stays behind the orchestrate seam — Open WebUI is a managed-service
// constant, NOT an inference-backend token (so it is outside TestSeamGrepGate's
// inference-seam scope), but routing all reads through this accessor keeps the
// no-re-typed-image-literal discipline uniform and future-proof.
func OpenWebUIImage() string { return openWebUIImage }

// OpenWebUIVolumeName returns the podman NAMED-volume identity for the Open WebUI
// data volume (the same name the Quadlet volume unit registers). The Phase-16
// backup/restore flow (D-02) needs the resolved volume name to drive the cmd-tier
// fixed-arg `podman volume export <name>` seam; routing the read through this
// accessor keeps the volume-name a single source of truth behind the orchestrate
// seam (config is the single source of truth — never a re-typed literal in cmd).
func OpenWebUIVolumeName() string { return openWebUIVolumeName }

// Open WebUI stable Quadlet identities (this project's unit-name/volume contract,
// asserted by the goldens — they leak no GPU/image assumption).
const (
	openWebUIContainerUnitName = "villa-openwebui.container"
	openWebUIVolumeUnitName    = "villa-openwebui.volume"

	openWebUIContainerName = "villa-openwebui"
	openWebUIVolumeName    = "villa-openwebui"

	// openWebUIPublishPort is loopback-only (D-04, PRIV-01 continuity): the host
	// reaches the UI at 127.0.0.1:3000; the container-internal port is 8080. Nothing
	// binds 0.0.0.0. TestRenderOpenWebUILoopbackOnly asserts this.
	openWebUIPublishPort = "127.0.0.1:3000:8080"

	// openWebUIVolumeMount is the durable named-volume mount (D-11, CHAT-03): a
	// dedicated read-write data volume at /app/backend/data with the :Z PRIVATE
	// SELinux label (never a shared :z, never a host/system path — Pitfall 5/7).
	openWebUIVolumeMount = openWebUIVolumeName + ".volume:/app/backend/data:Z"
)

// envPair is one ordered Environment= entry. Open WebUI's env block is an ORDERED
// SLICE (not a map[string]string) on purpose (Pattern 2): map iteration order is
// non-deterministic in Go and would break the byte-for-byte golden + the frozen
// telemetry-env test.
type envPair struct {
	Key   string
	Value string
}

// openWebUIView is the data the openwebui.container.tmpl renders. It deliberately
// mirrors containerView's field naming (ContainerName, Image, Network, PublishPort,
// Volume) but carries an ordered Env slice and OMITS AddDevice/GroupAdd/PodmanArgs/
// Exec — Open WebUI runs the image entrypoint with no GPU passthrough, so it must NOT
// flow through parseContainerArgs (whose defensive all-fields-non-empty check would
// reject it).
type openWebUIView struct {
	ContainerName string
	Image         string
	Network       string
	PublishPort   string
	Volume        string
	Env           []envPair
}

// openWebUIVolumeView is the data the openwebui.volume.tmpl renders: a plain
// podman-managed NAMED volume (VolumeName + Driver=local), with NO Type=none/Device=/
// Options=bind bind-mount fields (D-11; Open Question #2 resolution — a small
// dedicated template keeps both goldens clean).
type openWebUIVolumeView struct {
	VolumeName string
}

// buildOpenWebUIView assembles the Open WebUI container view from the managed-service
// constants. The Env order is FIXED and load-bearing (frozen by the golden + the
// telemetry test). OPENAI_API_BASE_URL sources its host from the render.go
// containerName constant ("villa-llama") so the chat target can NEVER drift from the
// inference unit's ContainerName= (Pitfall 3 DNS lockstep, T-4-01) — it is built from
// the constant, never re-typed as a separate host literal. Network is set to the
// existing networkAttach ("villa.network") so Open WebUI joins the Phase-3 network
// unchanged. WEBUI_AUTH stays True (D-10): the first visit creates a local admin
// account persisted in the durable volume — do NOT set it False.
func buildOpenWebUIView(mv memory.MemoryRenderInput, memoryEnabled bool, webSearchEnabled bool, searxngAddr string, searxngPort int, webSearchResultCount int) openWebUIView {
	env := []envPair{
		// Connection: reach inference over villa.network by container DNS
		// (NOT localhost / host.containers.internal), at its internal port 8080.
		{Key: "OPENAI_API_BASE_URL", Value: "http://" + containerName + ":8080/v1"},
		{Key: "ENABLE_OPENAI_API", Value: "True"},
		{Key: "ENABLE_OLLAMA_API", Value: "False"},
		// Required-but-ignored placeholder (WR-03): llama.cpp's OpenAI-compatible
		// endpoint performs NO auth, but Open WebUI needs a non-empty key field to
		// register the connection. This is NOT a secret — it is a fixed sentinel,
		// frozen by the container golden + the telemetry test. (The sk- shape can
		// trip secret scanners; the value is deliberately the well-known no-auth
		// placeholder, not a credential.)
		{Key: "OPENAI_API_KEY", Value: "sk-no-key-required"},
		// Telemetry kill-set (PRIV-02, D-06) — frozen by the telemetry test.
		{Key: "ANONYMIZED_TELEMETRY", Value: "False"},
		{Key: "DO_NOT_TRACK", Value: "True"},
		{Key: "SCARF_NO_ANALYTICS", Value: "True"},
		{Key: "OFFLINE_MODE", Value: "True"},
		{Key: "ENABLE_VERSION_UPDATE_CHECK", Value: "False"},
		{Key: "HF_HUB_OFFLINE", Value: "1"},
		// Local admin auth (D-10) — account persisted in the durable volume.
		{Key: "WEBUI_AUTH", Value: "True"},
	}

	if memoryEnabled {
		// D-09 RAG/Qdrant/memory group (Phase-20), appended as ONE ordered block
		// after the existing entries (D-02). Re-verified against OWUI config.py
		// (20-RESEARCH "OWUI Env Contract — Re-verified Against Source"). Endpoint
		// URLs are composed from the resolved render-view (mv) with fmt — NO
		// re-typed villa-qdrant/villa-embed/port literals (D-04, WR-01); the values
		// flow from config, so the seam gate stays green.
		env = append(env,
			// Point OWUI's vector subsystem at the Phase-19 Qdrant service (D-08).
			// VECTOR_DB is plain env (honored regardless of ENABLE_PERSISTENT_CONFIG);
			// QDRANT_URI is composed from mv.
			envPair{Key: "VECTOR_DB", Value: "qdrant"},
			envPair{Key: "QDRANT_URI", Value: fmt.Sprintf("http://%s:%d", mv.QdrantAddr, mv.QdrantPort)},
			// D-01: locked True NOW, before any vector exists — one shared,
			// tenant-partitioned collection (OWUI's source default; Qdrant's
			// recommended layout). Byte-frozen the moment the first document is
			// embedded; flipping it later silently disconnects collections. Plain
			// env, always honored.
			envPair{Key: "ENABLE_QDRANT_MULTITENANCY_MODE", Value: "True"},
			envPair{Key: "QDRANT_COLLECTION_PREFIX", Value: "open-webui"},
			// D-08: route chunk/embed/retrieve through the local villa-embed
			// OpenAI-compatible endpoint (no cloud API, no HF runtime download).
			// RAG_OPENAI_API_BASE_URL is composed from mv.
			envPair{Key: "RAG_EMBEDDING_ENGINE", Value: "openai"},
			envPair{Key: "RAG_OPENAI_API_BASE_URL", Value: fmt.Sprintf("http://%s:%d/v1", mv.EmbedAddr, mv.EmbedPort)},
			// Required-but-ignored placeholder (same rationale as OPENAI_API_KEY
			// above): llama-server's /v1/embeddings performs NO auth, but OWUI's RAG
			// OpenAI client needs a non-empty key field. This is NOT a secret — it is
			// the well-known no-auth sentinel, frozen by the memory golden + the
			// telemetry test.
			envPair{Key: "RAG_OPENAI_API_KEY", Value: "sk-no-key-required"},
			// The pinned embedding model id served by villa-embed (D-08); sourced
			// from mv (config is the single source of truth, never re-typed).
			envPair{Key: "RAG_EMBEDDING_MODEL", Value: mv.EmbeddingModel},
			// D-09 nomic task-instruction prefixes (plain env, honored): improve
			// retrieval for nomic-embed-text-v1.5. llama-server takes the prefix
			// inline in `input`, so RAG_EMBEDDING_PREFIX_FIELD_NAME is omitted.
			envPair{Key: "RAG_EMBEDDING_QUERY_PREFIX", Value: "search_query:"},
			envPair{Key: "RAG_EMBEDDING_CONTENT_PREFIX", Value: "search_document:"},
			// T-20-03: no runtime embedding-model auto-download (HF egress).
			envPair{Key: "RAG_EMBEDDING_MODEL_AUTO_UPDATE", Value: "False"},
			// D-06: native personalized memory store + cross-chat injection.
			envPair{Key: "ENABLE_MEMORIES", Value: "True"},
			// QDRANT_API_KEY intentionally omitted: empty default is accepted on the
			// private villa.network (D-discretion, A4).
			//
			// NOTE (Phase-30 D-04): the load-bearing ENABLE_PERSISTENT_CONFIG=False
			// switch NO LONGER lives inside this memory block — it is now emitted once,
			// last, by the trailing memoryEnabled || webSearchEnabled gate below.
		)
	}

	if webSearchEnabled {
		// Phase-30 OWUI native web-search group (SRCH-02/SRCH-03, D-01..D-03), appended
		// as ONE ordered block AFTER the base env, INDEPENDENT of the memory group
		// (append-only). The exact key names are VERIFIED against OWUI config.py at the
		// pinned digest rev 02dc3e68 (30-RESEARCH "OWUI Env Contract"): the older
		// ENABLE_RAG_WEB_SEARCH / RAG_WEB_SEARCH_* family is GONE at this revision (no
		// os.environ fallback), so the current names are used verbatim.
		env = append(env,
			// Turn OWUI's native web search on (DB-backed ConfigVar → authoritative only
			// with the ENABLE_PERSISTENT_CONFIG=False trailing gate below).
			envPair{Key: "ENABLE_WEB_SEARCH", Value: "True"},
			// Select the SearXNG provider.
			envPair{Key: "WEB_SEARCH_ENGINE", Value: "searxng"},
			// Compose the SearXNG query URL from the config-threaded host:port (WR-01) —
			// NEVER a re-typed villa-searxng / 8080 literal. The <query> token is OWUI's
			// literal substitution placeholder (kept verbatim). The &format=json suffix
			// is frozen for SC#1 literal compliance and future-robustness; at this digest
			// OWUI's SearXNG provider strips the URL query string and supplies format=json
			// itself, so the suffix is a no-op here (do not rely on it — Pitfall 1).
			envPair{Key: "SEARXNG_QUERY_URL",
				Value: fmt.Sprintf("http://%s:%d/search?q=<query>&format=json", searxngAddr, searxngPort)},
			// D-05 operator-tunable result count (config is the single source of truth;
			// default 3 resolved upstream in config). Rendered via strconv.Itoa.
			envPair{Key: "WEB_SEARCH_RESULT_COUNT", Value: strconv.Itoa(webSearchResultCount)},
			// D-06 (Phase-30 on-hardware UAT fix, SRCH-03 SC#2): inject fetched page
			// content DIRECTLY into the model context instead of OWUI's
			// embed→retrieve path. On-hardware UAT (OWUI 0.9.6) proved that with the
			// default embed→retrieve path (BYPASS=False) OWUI embeds web results into the
			// ephemeral open-webui_web-search collection but never QUERIES that collection
			// at retrieval time — only the model-attached durable knowledge/memory
			// collection is queried — so SearXNG results never reach the model and the
			// answer silently falls back to stale internal knowledge (SC#2 "grounded
			// answer" FAILS). Direct injection makes grounding reliable. Per-page /
			// result-count context bounding is deferred to Phase-31 (GROUND-03).
			envPair{Key: "BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL", Value: "True"},
		)
	}

	if memoryEnabled || webSearchEnabled {
		// D-04 (MANDATORY, load-bearing — T-20-01, extended to the web-search ConfigVars):
		// force OWUI to always read the appended ConfigVar keys (memory AND/OR web-search)
		// from env, ignoring the DB. Without it those keys are silently ignored after
		// first boot and config is NOT the single source of truth — its absence is a phase
		// failure. Emitted exactly ONCE and LAST, regardless of which group(s) are on
		// (never duplicated per-group, never dropped when web search is on but memory off).
		env = append(env, envPair{Key: "ENABLE_PERSISTENT_CONFIG", Value: "False"})
	}

	return openWebUIView{
		ContainerName: openWebUIContainerName,
		Image:         openWebUIImage,
		Network:       networkAttach,
		PublishPort:   openWebUIPublishPort,
		Volume:        openWebUIVolumeMount,
		Env:           env,
	}
}

// buildOpenWebUIVolumeView assembles the named-volume view (D-11).
func buildOpenWebUIVolumeView() openWebUIVolumeView {
	return openWebUIVolumeView{VolumeName: openWebUIVolumeName}
}
