package config

// villaconfig.go is the Phase-1 TOML configuration store for the `villa` CLI
// It is a NEW file, deliberately separate from the legacy
// env-var-based config.go (which is reference-only). The legacy Load/validate
// discipline is reused; the SOURCE is swapped from environment variables to a
// TOML file at $XDG_CONFIG_HOME/villa/config.toml.
//
// Phase 1 is read-only by default: Load returns typed defaults when the file is
// absent, and Save is invoked ONLY by `recommend --save`. Save writes
// strictly under the XDG config dir with 0600 perms (V12).

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
)

// configFileMode is the restrictive file mode for the written config — readable
// and writable only by the owner (info-disclosure mitigation).
const configFileMode os.FileMode = 0o600

// configDirMode is the mode for the created villa config directory.
const configDirMode os.FileMode = 0o700

// Service identity on the private container network, and the dashboard's bind
// address. These were persisted config fields until it became clear none could
// vary: `config set` never accepted them, the load-time normaliser healed any
// hand-edited value straight back to these constants, and the in-network services
// publish no host port, so the numbers were invisible from outside villa.network
// anyway.
//
// They are constants rather than settings because widening them is a privacy
// violation, not a preference:
//
//   - the addresses are container-DNS names on the private network, never routable
//     host binds;
//   - the ports are in-network only;
//   - the dashboard binds loopback by construction and is documented as never
//     widenable.
//
// This is the single home of each literal. A config file still
// carrying the old keys loads fine — unknown keys are ignored — and the values it
// carried were, by construction, these same ones.
const (
	// DashboardAddr is the loopback-only bind address for the control dashboard
	// NEVER bind all interfaces.
	DashboardAddr = "127.0.0.1"

	// QdrantAddr is the container-DNS name of the Qdrant vector store.
	QdrantAddr = "villa-qdrant"
	// QdrantPort is the in-network Qdrant REST port.
	QdrantPort = 6333
	// EmbedAddr is the container-DNS name of the dedicated villa-embed
	// llama-server.
	EmbedAddr = "villa-embed"
	// EmbedPort is the in-network villa-embed OpenAI /v1 port.
	EmbedPort = 8080

	// SearxngAddr is the container-DNS name of the SearXNG metasearch service.
	SearxngAddr = "villa-searxng"
	// SearxngPort is the in-network SearXNG port the readiness proof probes.
	SearxngPort = 8080

	// WebsafeAddr is the container-DNS name of the villa-websafe loader.
	WebsafeAddr = "villa-websafe"
	// WebsafePort is the in-network port the villa-websafe loader listens on.
	WebsafePort = 8090
)

// VillaConfig is the persisted recommend selection that later phases (Phase 3
// install) derive Quadlet units from. Fields are TOML-tagged and typed.
type VillaConfig struct {
	// Model is the chosen catalog model id.
	Model string `toml:"model"`
	// Quant is the chosen quantization (e.g. UD-Q4_K_M).
	Quant string `toml:"quant"`
	// Ctx is the chosen context length in tokens.
	Ctx int `toml:"ctx"`
	// Backend is the inference backend (rocm by default for gfx1151,;
	// vulkan is the explicit opt-in fallback).
	Backend string `toml:"backend"`
	// CatalogPath optionally points at an external catalog override.
	CatalogPath string `toml:"catalog_path"`
	// DashboardPort is the host port the control dashboard listens on.
	// Default 8888.
	DashboardPort int `toml:"dashboard_port"`
	// ChatPort is the host port Open WebUI is published on — the dashboard's
	// chat link target, read from config rather than hard-coded. Default 3000.
	ChatPort int `toml:"chat_port"`

	// --- Memory stack fields (v1.3, INFRA-04 /..) ---
	// These follow the existing flat, self-healing pattern (dashboard_*/chat_*),
	// default to a coherent OFF state, and (via marshalVilla's omit-when-disabled
	// path + ,omitempty tags) are NOT emitted to disk for a non-opted-in install on
	// ANY save-bearing command (byte-identical guarantee). The endpoint
	// addr fields are container-DNS names on villa.network ONLY — never a routable
	// host bind; normalizeVilla never widens them.

	// MemoryEnabled gates the whole v1.3 memory stack. Default false: an
	// existing v1.2 install stays memory-off until the user opts in.
	MemoryEnabled bool `toml:"memory_enabled,omitempty"`
	// EmbeddingModel is the pinned embedding model id served by villa-embed
	// Default "nomic-embed-text-v1.5".
	EmbeddingModel string `toml:"embedding_model,omitempty"`
	// EmbeddingDim is the pinned, LOAD-BEARING embedding dimension.
	// Default 768. Changing it corrupts existing Qdrant vectors (no auto-reindex);
	// it is recorded here as the anchor for the Phase-23 memory-aware swap guard.
	EmbeddingDim int `toml:"embedding_dim,omitzero"`

	// --- Coding-mode fields ---
	// These follow the v1.3 memory-stack precedent EXACTLY: append-only, all
	// ,omitempty, and (via marshalVilla's omit-when-off path) NOT emitted to disk for
	// a non-coding install on ANY save-bearing command — so an existing v1.3 install is
	// byte-identical on disk until the user enters coding mode. Units
	// regenerate from these fields (orchestrate.Render derives the coding-mode
	// render descriptor from them), so the mode survives restart/reboot. The mode
	// changes ONLY via the explicit `villa coding-mode enter|exit` verb (Phase 25
	// Plan 02) — never auto-flipped, never a hand-edited unit. The coder_* fields hold
	// the model/quant/agent_ctx RESOLVED AT ENTER, never re-picked later.

	// CodingMode gates the whole coding-mode delta. Default false: an existing
	// v1.3 install stays chat-only until the user enters coding mode. A deliberate bool
	// toggle (mirrors MemoryEnabled) — false is a meaningful explicit choice, so it is
	// NOT self-healed in normalizeVilla.
	CodingMode bool `toml:"coding_mode,omitempty"`
	// CoderModel is the catalog id of the active coder model, resolved AT ENTER from the
	// Phase-24 coder catalog. Empty/dropped when coding mode is off.
	CoderModel string `toml:"coder_model,omitempty"`
	// CoderQuant is the active coder quantization, resolved AT ENTER. Empty/dropped
	// when coding mode is off.
	CoderQuant string `toml:"coder_quant,omitempty"`
	// CoderAgentCtx is the resolved agent-profile context window the coder unit is
	// rendered with (the single `-c`, Pitfall 1) — sourced from the catalog entry's
	// agent_ctx AT ENTER. Zero/dropped when coding mode is off. Tagged
	// `omitzero` (NOT `omitempty`) to match the v1.3 memory-stack int precedent
	// (embedding_dim/qdrant_port/embed_port): BurntSushi/toml's omitempty does NOT drop
	// a zero int, only omitzero does — required for the byte-identical-off guarantee.
	CoderAgentCtx int `toml:"coder_agent_ctx,omitzero"`

	// --- Coding-agent addon gate ---
	// AgentEnabled gates the whole v1.4 coding-agent (Crush) install addon. Default
	// false: an existing install stays agent-off until the user opts in via
	// `villa install --coding-agent`. A deliberate bool toggle (mirrors MemoryEnabled /
	// CodingMode) — false is a meaningful explicit choice, so it is NOT self-healed in
	// normalizeVilla. ,omitempty so an agent-off install is byte-identical on disk: a
	// plain bool with omitempty drops the key on a default-false marshal (no marshalVilla
	// zeroing is needed — unlike the memory/coder blocks whose non-bool fields require it).
	AgentEnabled bool `toml:"agent_enabled,omitempty"`

	// --- Web-search stack fields ---
	// These follow the v1.3 memory-stack precedent EXACTLY: append-only, all
	// ,omitempty/,omitzero, and (via marshalVilla's omit-when-off path) NOT emitted to
	// disk for a non-opted-in install on ANY save-bearing command — so an existing v1.4
	// install is byte-identical on disk until the user opts into web search (
	// The addr field is a container-DNS name on villa.network ONLY — never a
	// routable host bind; normalizeVilla never widens it.

	// WebSearchEnabled gates the whole v1.5 web-search (SearXNG) stack. Default false: an
	// existing v1.4 install stays web-search-off until the user opts in. A deliberate bool
	// toggle (mirrors MemoryEnabled / CodingMode / AgentEnabled) — false is a meaningful
	// explicit choice, so it is NOT self-healed in normalizeVilla.
	WebSearchEnabled bool `toml:"web_search_enabled,omitempty"`
	// SearxngSecret is the SearXNG secret_key (its session/CSRF crypto seed, V3/V6),
	// generated ONCE via crypto/rand at first opt-in and persisted at 0600 (config.toml).
	// It is NEVER rendered into any 0644 file (unit/settings.yml) — it reaches the
	// container via a Quadlet EnvironmentFile=<0600 path> directive. Empty until
	// opt-in; NOT self-healed (a generated secret has no meaningful default).
	SearxngSecret string `toml:"searxng_secret,omitempty"`
	// WebSearchResultCount is the operator-tunable number of search results Open WebUI
	// requests per query — it maps directly to OWUI's WEB_SEARCH_RESULT_COUNT env var
	// (threaded into the OWUI render by Plan 02). Default 3 keeps the context budget
	// conservative ahead of Phase 31's ctx-budget reservation. Tagged ,omitzero (NOT
	// ,omitempty) to match the v1.3 memory int precedent (qdrant_port/embed_port) and the
	// SearxngPort precedent above: BurntSushi/toml only drops a zero int with omitzero,
	// which the byte-identical-off guarantee depends on (the key is dropped from disk when
	// web search is off via marshalVilla zeroing).
	WebSearchResultCount int `toml:"web_search_result_count,omitzero"`

	// --- villa-websafe loader fields ---
	// The villa-websafe external-loader service identity + the OWUI bearer secret + the
	// host binary path for the bind-mount. Same omit-when-off + self-heal discipline as the
	// SearXNG fields: addr/port self-heal (container-DNS only, — never a host bind);
	// the secret + host path are NOT self-healed (a generated secret / a captured path has no
	// meaningful default). marshalVilla zeroes all four when web search is off so an existing
	// install is byte-identical on disk until opt-in.

	// WebLoaderSecret is the EXTERNAL_WEB_LOADER_API_KEY bearer shared between OWUI and
	// villa-websafe, generated ONCE via crypto/rand at first opt-in and persisted at
	// 0600. It is NEVER rendered into any 0644 file — it reaches the containers via a 0600
	// EnvironmentFile (mirrors SearxngSecret). Empty until opt-in; NOT self-healed (a generated
	// secret has no meaningful default) and never logged.
	WebLoaderSecret string `toml:"web_loader_secret,omitempty"`
	// HostVillaPath is the absolute host path of the villa binary (os.Executable() captured AT
	// opt-in), bind-mounted into the villa-websafe container so the same single static binary
	// serves the loader (Area 1). It is NEVER shell-interpolated. Empty until opt-in; NOT
	// self-healed (a captured host path has no meaningful default).
	HostVillaPath string `toml:"host_villa_path,omitempty"`

	// Speculation is the persisted speculative-decoding mode of the inference unit
	// (ADR-0006). Empty means the recommendation has not resolved it yet and renders
	// off, which is what an install predating this field carries.
	Speculation string `toml:"speculation,omitempty"`

	// Vision is the persisted vision decision: true only when an install or a
	// `recommend --save` resolved a projector that fits the envelope AND pulled it.
	// It is persisted rather than derived from the catalog for the reason ADR-0006
	// gives for speculation, plus one of its own: the projector file must already be
	// on disk before a unit references it, and a v1.8 install never pulled one.
	Vision bool `toml:"vision,omitempty"`

	// --- Resident set fields ---
	// Tail-appended and append-only, like every optional block above it: nothing
	// declared earlier moves, and a nil/empty Resident is dropped by ,omitempty so an
	// existing install gains NO resident key on disk until the user adds one.

	// Resident lists the secondary models held resident alongside Model. Holding a
	// second model loaded costs memory but avoids a cold load on every switch, which
	// is the whole reason this list exists rather than a swap verb. Whether a
	// proposed set actually FITS the memory envelope is decided by
	// internal/residentset, never by this struct.
	Resident []ResidentModel `toml:"resident,omitempty"`
}

// ResidentModel is one secondary model held resident alongside VillaConfig.Model.
// It is a TOML array-of-tables entry ([[resident]]), so the fields carry the same
// omitempty/omitzero discipline as the flat optional blocks: BurntSushi/toml drops a
// zero int only under ,omitzero, never ,omitempty.
type ResidentModel struct {
	// Model is the catalog model id for this slot.
	Model string `toml:"model"`
	// Quant is the chosen quantization for this slot.
	Quant string `toml:"quant,omitempty"`
	// Ctx is this slot's own context length in tokens.
	Ctx int `toml:"ctx,omitzero"`
	// Port is the HOST loopback port this slot publishes on. It is stated
	// explicitly rather than derived from list position: a derived port would
	// renumber every later slot when a middle entry is removed, rewriting and
	// restarting units that did not change.
	Port int `toml:"port,omitzero"`
}

// Speculation modes. The vocabulary is closed: `draft` is deliberately absent
// (ADR-0006 measured it slower on every catalog entry), so a config asking for it
// is refused rather than accepted as a value that does nothing.
const (
	// SpeculationOff renders no speculation flag at all.
	SpeculationOff = "off"
	// SpeculationNgram is llama-server's ngram-mod speculative decoder.
	SpeculationNgram = "ngram"
)

// ValidSpeculation reports whether s is a speculation mode villa implements. The
// empty string is valid and means unresolved.
func ValidSpeculation(s string) bool {
	return s == "" || s == SpeculationOff || s == SpeculationNgram
}

// validateVilla rejects a parsed config carrying a value villa cannot render, so
// an unknown mode is a refusal at the boundary rather than a silent downgrade to
// off. It runs on every parse path, before normalizeVilla.
func validateVilla(cfg VillaConfig) error {
	if !ValidSpeculation(cfg.Speculation) {
		return fmt.Errorf("config: speculation %q is not a known mode (off, ngram, or unset)", cfg.Speculation)
	}
	return nil
}

// defaultConfig is the typed default returned when no config file exists. An absent
// dashboard/chat field therefore defaults to loopback:8888 / chat 3000.
func defaultConfig() VillaConfig {
	return VillaConfig{
		Backend:       "rocm",
		DashboardPort: 8888,
		ChatPort:      3000,
		// Memory stack defaults — the SINGLE home of these literals. The
		// stack is OFF by default; the rest are inert until opt-in. The
		// addr fields are container-DNS names on villa.network only.
		MemoryEnabled:  false,
		EmbeddingModel: "nomic-embed-text-v1.5",
		EmbeddingDim:   768,
		// Web-search stack defaults — the SINGLE home of these literals.
		// The stack is OFF by default; the addr is a container-DNS name on villa.network
		// only. The secret has no default (generated at opt-in).
		WebSearchEnabled: false,
		// Result-count default: conservative 3 ahead of Phase 31's ctx-budget
		// reservation. The SINGLE home of this literal; the other three sites derive it.
		WebSearchResultCount: 3,
		// villa-websafe loader defaults (v1.5) — the SINGLE home of these literals. The addr
		// is a container-DNS name on villa.network only; the port is the in-network
		// loader port. The bearer secret + host binary path have NO default (generated /
		// captured at opt-in).
	}
}

// normalizeVilla treats the dashboard/chat service fields' type-zero values
// (DashboardPort==0, ChatPort==0) as "unset → default" and
// fills them from defaultConfig(). This self-heals an already-broken on-disk
// config on the next load (gap test:1b): BurntSushi/toml sets a key present in
// the file even when its value is the type zero, so a partial writer that emitted
// dashboard_port=0 / chat_port=0 / dashboard_addr="" would otherwise override the
// seeded defaults and leave the dashboard binding the unreachable :0.
//
// 0/"" is safe to treat as unset for these three fields specifically: a port 0 is
// never a valid intended value for a long-running dashboard/chat service (it asks
// the kernel for an ephemeral, undiscoverable port), and an empty bind address is
// never an intended choice — both can only arrive via the partial-write bug this
// plan also fixes. defaultConfig() is the SINGLE source of the three default
// literals (8888 / 3000 / 127.0.0.1); normalizeVilla derives from it rather than
// re-hard-coding them. It only ever fills the loopback "127.0.0.1" for an empty
// address — it NEVER widens the bind to a routable interface.
//
// The same self-heal extends to the v1.3 memory fields: a type-zero
// embedding_dim / qdrant_port / embed_port or an empty embedding_model /
// qdrant_addr / embed_addr is treated as "unset -> default" and filled from the
// SAME defaultConfig() source (never a re-hard-coded literal -- a duplicate would
// be a drift bug). For the endpoint addr fields this only ever fills the
// container-DNS default name (villa-qdrant / villa-embed) -- it NEVER substitutes
// a routable/widened bind. MemoryEnabled is a deliberate bool
// toggle and is NOT self-healed: false is its valid default and a meaningful
// explicit choice, so it is left exactly as parsed.
func normalizeVilla(cfg VillaConfig) VillaConfig {
	d := defaultConfig()
	if cfg.DashboardPort == 0 {
		cfg.DashboardPort = d.DashboardPort
	}
	if cfg.ChatPort == 0 {
		cfg.ChatPort = d.ChatPort
	}
	if cfg.EmbeddingModel == "" {
		cfg.EmbeddingModel = d.EmbeddingModel
	}
	if cfg.EmbeddingDim == 0 {
		cfg.EmbeddingDim = d.EmbeddingDim
	}
	// Result-count self-heal: a zero WebSearchResultCount is treated as
	// "unset -> default" and filled from the SAME defaultConfig() source (never a
	// re-hard-coded literal). Mirrors the SearxngPort==0 heal above.
	if cfg.WebSearchResultCount == 0 {
		cfg.WebSearchResultCount = d.WebSearchResultCount
	}
	// WebLoaderSecret (a generated bearer) and HostVillaPath (a captured host path) are
	// NOT self-healed — neither has a meaningful default.
	return cfg
}

// DefaultVillaConfig is the exported accessor for the typed defaults, so callers
// (e.g. cmd/villa writers) can seed a config from the single source of the
// dashboard/chat default literals without duplicating 8888 / 3000 / 127.0.0.1.
func DefaultVillaConfig() VillaConfig {
	return defaultConfig()
}

// villaConfigDir returns the directory holding the villa config file,
// $XDG_CONFIG_HOME/villa (os.UserConfigDir honors XDG safely, V12).
func villaConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: cannot resolve user config dir: %w", err)
	}
	return filepath.Join(base, "villa"), nil
}

// Path returns the absolute path to the villa config file,
// $XDG_CONFIG_HOME/villa/config.toml.
func Path() (string, error) {
	dir, err := villaConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// LoadVilla reads the TOML config, returning typed defaults when the file is
// absent (read-only by default). A present-but-malformed file is a real
// error the caller should surface.
func LoadVilla() (VillaConfig, error) {
	path, err := Path()
	if err != nil {
		return VillaConfig{}, err
	}

	data, err := os.ReadFile(path) //nolint:gosec // path derived from os.UserConfigDir
	if err != nil {
		if os.IsNotExist(err) {
			return defaultConfig(), nil
		}
		return VillaConfig{}, fmt.Errorf("config: read %q: %w", path, err)
	}

	cfg := defaultConfig()
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return VillaConfig{}, fmt.Errorf("config: parse %q: %w", path, err)
	}
	if err := validateVilla(cfg); err != nil {
		return VillaConfig{}, err
	}
	// Self-heal a config whose dashboard/chat fields were persisted as zeros by
	// an older partial writer (gap test:1b) — never widens the bind.
	return normalizeVilla(cfg), nil
}

// marshalVilla serializes c to TOML for persistence. It is the SINGLE marshal
// path shared by SaveVilla and SaveVillaTo so the byte-identical guarantee
// holds uniformly on every save-bearing command (recommend --save, model swap,
// backend set, restore). When the memory stack is disabled (MemoryEnabled ==
// false) the memory_* fields are zeroed on this by-value copy so the ,omitempty
// tags drop all seven keys from the output — an existing v1.2 install therefore
// gains NO memory keys until the user opts in. The in-memory defaults
// are re-applied by normalizeVilla on the next load, so dropping them on disk is
// lossless. No string interpolation (BurntSushi/toml).
func marshalVilla(c VillaConfig) ([]byte, error) {
	if !c.MemoryEnabled {
		c.EmbeddingModel = ""
		c.EmbeddingDim = 0
	}
	// Coding-mode omit-when-off: when coding mode is disabled the
	// resolved coder_* fields are zeroed on this by-value copy so the ,omitempty tags
	// drop all four coding keys — an existing v1.3 install therefore gains NO coding
	// keys on disk until the user enters coding mode (byte-identical, same discipline
	// as the memory block above). The fields are re-written by the enter path.
	if !c.CodingMode {
		c.CoderModel = ""
		c.CoderQuant = ""
		c.CoderAgentCtx = 0
	}
	// Web-search omit-when-off: when web search is disabled the
	// searxng fields are zeroed on this by-value copy so the ,omitempty/,omitzero tags
	// drop all four web-search keys — an existing v1.4 install therefore gains NO
	// web-search keys on disk until the user opts in (byte-identical, same discipline as
	// the memory/coder blocks above). The fields are re-applied by normalizeVilla on the
	// next load (addr/port) or re-written by the opt-in path (the generated secret).
	if !c.WebSearchEnabled {
		c.SearxngSecret = ""
		c.WebSearchResultCount = 0
		// villa-websafe loader fields: zeroed on this by-value copy so
		// the ,omitempty/,omitzero tags drop all four websafe keys — an off install gains NO
		// websafe keys on disk (byte-identical-off). addr/port are re-applied by normalizeVilla
		// on the next load; the secret/path are re-written by the opt-in path.
		c.WebLoaderSecret = ""
		c.HostVillaPath = ""
	}
	return toml.Marshal(c)
}

// GenerateSearxngSecret returns a fresh, high-entropy SearXNG secret_key sourced from
// crypto/rand (V6) — the repo's FIRST runtime secret generator. It reads 32 random
// bytes (256 bits) and hex-encodes them to a 64-char ASCII string safe to carry in an
// env file. NEVER use math/rand here; never log the returned value. The secret is
// generated ONCE at first web-search opt-in, persisted at 0600 in config.toml, and
// reaches the container only via a 0600 EnvironmentFile (never a 0644 file).
func GenerateSearxngSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("config: generate searxng secret: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// GenerateWebLoaderSecret returns a fresh, high-entropy EXTERNAL_WEB_LOADER_API_KEY bearer
// token sourced from crypto/rand (V6) — a 1:1 clone of GenerateSearxngSecret. It
// reads 32 random bytes (256 bits) and hex-encodes them to a 64-char ASCII string safe to
// carry in an env file. NEVER use math/rand here; never log the returned value. The bearer is
// generated ONCE at first web-search opt-in, persisted at 0600 in config.toml, and reaches the
// villa-websafe + villa-openwebui containers only via a 0600 EnvironmentFile.
func GenerateWebLoaderSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("config: generate web loader secret: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// SaveVilla writes the config as TOML under the XDG config dir with 0600 perms.
// It marshals via marshalVilla (no string interpolation) and refuses
// to write outside the villa config dir (path-traversal guard, /V12).
//
// The write is atomic: config.toml is the single source of truth and holds
// values that exist nowhere else — the SearXNG secret and the web-loader bearer
// are generated once and never re-derivable — so an interrupted write must leave
// the previous config intact rather than a truncated file the fail-closed loader
// would refuse.
func SaveVilla(c VillaConfig) error {
	dir, err := villaConfigDir()
	if err != nil {
		return err
	}
	return saveVillaTo(dir, c)
}

// SaveVillaTo is the testable core of SaveVilla: it writes c to a config.toml
// inside dir, enforcing that the resolved path stays within dir. Production code
// calls SaveVilla; tests pass a temp dir to exercise the traversal guard without
// touching the user's real XDG config.
func SaveVillaTo(dir string, c VillaConfig) error {
	return saveVillaTo(dir, c)
}

// saveVillaTo is the single write path both exported savers share, so neither can
// drift from the other's guarantees: traversal-guarded, 0700 directory, 0600 file,
// and atomic.
//
// dir is made absolute first because the shared writer requires an absolute
// containment root — a relative root cannot bound anything, and refusing it is
// what stops a relative XDG value from reaching a write.
func saveVillaTo(dir string, c VillaConfig) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("config: resolve config dir %q: %w", dir, err)
	}
	path := filepath.Join(absDir, "config.toml")

	// Kept as a separate up-front check, ahead of the writer's own containment
	// guard, because it is what produces the "outside config dir" wording the
	// traversal test asserts verbatim.
	if err := assertInsideDir(path, absDir); err != nil {
		return err
	}

	if err := os.MkdirAll(absDir, configDirMode); err != nil {
		return fmt.Errorf("config: create config dir %q: %w", absDir, err)
	}

	data, err := marshalVilla(c)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}

	if err := pathsafe.WriteFileAtomic(absDir, path, data, configFileMode); err != nil {
		return fmt.Errorf("config: write %q: %w", path, err)
	}
	return nil
}

// LoadVillaFrom reads config.toml from dir (the testable counterpart to
// LoadVilla), returning typed defaults when absent.
func LoadVillaFrom(dir string) (VillaConfig, error) {
	path := filepath.Join(dir, "config.toml")
	data, err := os.ReadFile(path) //nolint:gosec // dir supplied by caller/test
	if err != nil {
		if os.IsNotExist(err) {
			return defaultConfig(), nil
		}
		return VillaConfig{}, fmt.Errorf("config: read %q: %w", path, err)
	}
	cfg := defaultConfig()
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return VillaConfig{}, fmt.Errorf("config: parse %q: %w", path, err)
	}
	if err := validateVilla(cfg); err != nil {
		return VillaConfig{}, err
	}
	// Self-heal zeroed dashboard/chat fields on load (gap test:1b); loopback-only.
	return normalizeVilla(cfg), nil
}

// Parse unmarshals config.toml BYTES into a VillaConfig, seeding the typed
// defaults FIRST and self-healing zeroed dashboard/chat fields exactly as
// LoadVilla does (loopback-only, never widening the bind). It is the
// in-memory counterpart of LoadVilla, used by `villa restore` to turn the archive's
// config.toml entry into the source-of-truth VillaConfig the Quadlet recreate
// renders from (config is the single source of truth). A malformed payload
// is a real error.
func Parse(data []byte) (VillaConfig, error) {
	cfg := defaultConfig()
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return VillaConfig{}, fmt.Errorf("config: parse bytes: %w", err)
	}
	if err := validateVilla(cfg); err != nil {
		return VillaConfig{}, err
	}
	return normalizeVilla(cfg), nil
}

// assertInsideDir verifies path resolves within dir, rejecting traversal escapes
// (V12). Both are cleaned and compared as absolute paths.
func assertInsideDir(path, dir string) error {
	if err := pathsafe.Inside(path, dir); err != nil {
		// Wording note: "outside config dir" is asserted verbatim by
		// TestSaveRefusesTraversal — keep the substring if you reword this.
		return fmt.Errorf("config: refusing to write outside config dir %q: %w", dir, err)
	}
	return nil
}
