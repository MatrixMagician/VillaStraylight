package config

// villaconfig.go is the Phase-1 TOML configuration store for the `villa` CLI
// (D-19/D-20). It is a NEW file, deliberately separate from the legacy
// env-var-based config.go (which is reference-only). The legacy Load/validate
// discipline is reused; the SOURCE is swapped from environment variables to a
// TOML file at $XDG_CONFIG_HOME/villa/config.toml.
//
// Phase 1 is read-only by default: Load returns typed defaults when the file is
// absent, and Save is invoked ONLY by `recommend --save` (D-20). Save writes
// strictly under the XDG config dir with 0600 perms (V12 / T-02-02..04).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// configFileMode is the restrictive file mode for the written config — readable
// and writable only by the owner (T-02-04 info-disclosure mitigation).
const configFileMode os.FileMode = 0o600

// configDirMode is the mode for the created villa config directory.
const configDirMode os.FileMode = 0o700

// VillaConfig is the persisted recommend selection that later phases (Phase 3
// install) derive Quadlet units from. Fields are TOML-tagged and typed.
type VillaConfig struct {
	// Model is the chosen catalog model id.
	Model string `toml:"model"`
	// Quant is the chosen quantization (e.g. UD-Q4_K_M).
	Quant string `toml:"quant"`
	// Ctx is the chosen context length in tokens.
	Ctx int `toml:"ctx"`
	// Backend is the inference backend (vulkan by default for gfx1151, REC-04).
	Backend string `toml:"backend"`
	// CatalogPath optionally points at an external catalog override.
	CatalogPath string `toml:"catalog_path"`
	// DashboardAddr is the loopback-only bind address for the control dashboard
	// (D-13). Default "127.0.0.1"; NEVER bind all interfaces (PRIV-01).
	DashboardAddr string `toml:"dashboard_addr"`
	// DashboardPort is the host port the control dashboard listens on (D-13).
	// Default 8888.
	DashboardPort int `toml:"dashboard_port"`
	// ChatPort is the host port Open WebUI is published on (D-12) — the dashboard's
	// chat link target, read from config rather than hard-coded. Default 3000.
	ChatPort int `toml:"chat_port"`

	// --- Memory stack fields (v1.3, INFRA-04 / D-04..D-08) ---
	// These follow the existing flat, self-healing pattern (dashboard_*/chat_*),
	// default to a coherent OFF state, and are NOT emitted to disk for a
	// non-opted-in install (byte-identical guarantee, SC#1/D-05). The endpoint
	// addr fields are container-DNS names on villa.network ONLY — never a routable
	// host bind (PRIV-01 / D-06); normalizeVilla never widens them.

	// MemoryEnabled gates the whole v1.3 memory stack. Default false (D-04): an
	// existing v1.2 install stays memory-off until the user opts in.
	MemoryEnabled bool `toml:"memory_enabled"`
	// EmbeddingModel is the pinned embedding model id served by villa-embed
	// (D-08). Default "nomic-embed-text-v1.5".
	EmbeddingModel string `toml:"embedding_model"`
	// EmbeddingDim is the pinned, LOAD-BEARING embedding dimension (D-03/D-08).
	// Default 768. Changing it corrupts existing Qdrant vectors (no auto-reindex);
	// it is recorded here as the anchor for the Phase-23 memory-aware swap guard.
	EmbeddingDim int `toml:"embedding_dim"`
	// QdrantAddr is the container-DNS name of the Qdrant vector store on
	// villa.network (D-06). Default "villa-qdrant"; NEVER a routable host bind
	// (PRIV-01) — Qdrant publishes no host port.
	QdrantAddr string `toml:"qdrant_addr"`
	// QdrantPort is the in-network Qdrant REST port (D-06). Default 6333.
	QdrantPort int `toml:"qdrant_port"`
	// EmbedAddr is the container-DNS name of the dedicated villa-embed
	// llama-server on villa.network (D-06/D-07). Default "villa-embed"; NEVER a
	// routable host bind (PRIV-01).
	EmbedAddr string `toml:"embed_addr"`
	// EmbedPort is the in-network villa-embed OpenAI /v1 port (D-06/D-07).
	// Default 8080.
	EmbedPort int `toml:"embed_port"`
}

// defaultConfig is the typed default returned when no config file exists. An absent
// dashboard/chat field therefore defaults to loopback:8888 / chat 3000 (D-13/D-12).
func defaultConfig() VillaConfig {
	return VillaConfig{
		Backend:       "vulkan",
		DashboardAddr: "127.0.0.1",
		DashboardPort: 8888,
		ChatPort:      3000,
		// Memory stack defaults — the SINGLE home of these literals (D-05). The
		// stack is OFF by default (D-04); the rest are inert until opt-in. The
		// addr fields are container-DNS names on villa.network only (D-06/PRIV-01).
		MemoryEnabled:  false,
		EmbeddingModel: "nomic-embed-text-v1.5",
		EmbeddingDim:   768,
		QdrantAddr:     "villa-qdrant",
		QdrantPort:     6333,
		EmbedAddr:      "villa-embed",
		EmbedPort:      8080,
	}
}

// normalizeVilla treats the dashboard/chat service fields' type-zero values
// (DashboardPort==0, ChatPort==0, DashboardAddr=="") as "unset → default" and
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
// address — it NEVER widens the bind to a routable interface (PRIV-01).
//
// The same self-heal extends to the v1.3 memory fields (D-04/D-05): a type-zero
// embedding_dim / qdrant_port / embed_port or an empty embedding_model /
// qdrant_addr / embed_addr is treated as "unset -> default" and filled from the
// SAME defaultConfig() source (never a re-hard-coded literal -- a duplicate would
// be a drift bug). For the endpoint addr fields this only ever fills the
// container-DNS default name (villa-qdrant / villa-embed) -- it NEVER substitutes
// a routable/widened bind (PRIV-01 / T-18-02). MemoryEnabled is a deliberate bool
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
	if cfg.DashboardAddr == "" {
		cfg.DashboardAddr = d.DashboardAddr
	}
	if cfg.EmbeddingModel == "" {
		cfg.EmbeddingModel = d.EmbeddingModel
	}
	if cfg.EmbeddingDim == 0 {
		cfg.EmbeddingDim = d.EmbeddingDim
	}
	if cfg.QdrantAddr == "" {
		cfg.QdrantAddr = d.QdrantAddr
	}
	if cfg.QdrantPort == 0 {
		cfg.QdrantPort = d.QdrantPort
	}
	if cfg.EmbedAddr == "" {
		cfg.EmbedAddr = d.EmbedAddr
	}
	if cfg.EmbedPort == 0 {
		cfg.EmbedPort = d.EmbedPort
	}
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
// absent (read-only by default, D-20). A present-but-malformed file is a real
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
	// Self-heal a config whose dashboard/chat fields were persisted as zeros by
	// an older partial writer (gap test:1b) — never widens the bind (PRIV-01).
	return normalizeVilla(cfg), nil
}

// SaveVilla writes the config as TOML under the XDG config dir with 0600 perms.
// It marshals via BurntSushi/toml (no string interpolation, T-02-03) and refuses
// to write outside the villa config dir (path-traversal guard, T-02-02/V12).
func SaveVilla(c VillaConfig) error {
	dir, err := villaConfigDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "config.toml")

	if err := assertInsideDir(path, dir); err != nil {
		return err
	}

	if err := os.MkdirAll(dir, configDirMode); err != nil {
		return fmt.Errorf("config: create config dir %q: %w", dir, err)
	}

	data, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}

	if err := os.WriteFile(path, data, configFileMode); err != nil {
		return fmt.Errorf("config: write %q: %w", path, err)
	}
	// Tighten perms even if the file pre-existed with a looser mode.
	if err := os.Chmod(path, configFileMode); err != nil {
		return fmt.Errorf("config: chmod %q: %w", path, err)
	}
	return nil
}

// SaveVillaTo is the testable core of SaveVilla: it writes c to a config.toml
// inside dir, enforcing that the resolved path stays within dir. Production code
// calls SaveVilla; tests pass a temp dir to exercise the traversal guard without
// touching the user's real XDG config.
func SaveVillaTo(dir string, c VillaConfig) error {
	path := filepath.Join(dir, "config.toml")
	if err := assertInsideDir(path, dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, configDirMode); err != nil {
		return fmt.Errorf("config: create config dir %q: %w", dir, err)
	}
	data, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	if err := os.WriteFile(path, data, configFileMode); err != nil {
		return fmt.Errorf("config: write %q: %w", path, err)
	}
	return os.Chmod(path, configFileMode)
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
	// Self-heal zeroed dashboard/chat fields on load (gap test:1b); loopback-only.
	return normalizeVilla(cfg), nil
}

// Parse unmarshals config.toml BYTES into a VillaConfig, seeding the typed
// defaults FIRST and self-healing zeroed dashboard/chat fields exactly as
// LoadVilla does (loopback-only, never widening the bind — PRIV-01). It is the
// in-memory counterpart of LoadVilla, used by `villa restore` to turn the archive's
// config.toml entry into the source-of-truth VillaConfig the Quadlet recreate
// renders from (config is the single source of truth — D-07). A malformed payload
// is a real error.
func Parse(data []byte) (VillaConfig, error) {
	cfg := defaultConfig()
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return VillaConfig{}, fmt.Errorf("config: parse bytes: %w", err)
	}
	return normalizeVilla(cfg), nil
}

// assertInsideDir verifies path resolves within dir, rejecting traversal escapes
// (V12). Both are cleaned and compared as absolute paths.
func assertInsideDir(path, dir string) error {
	absDir, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return err
	}
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("config: refusing to write %q outside config dir %q", absPath, absDir)
	}
	return nil
}
