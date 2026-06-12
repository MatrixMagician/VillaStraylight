package main

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// fixtureRecommendation is a deterministic Recommendation (NOT live hardware) so
// the golden JSON is stable in CI. It locks the full --json / dashboard contract
// (D-05), including all four fit terms.
func fixtureRecommendation() recommend.Recommendation {
	return recommend.Recommendation{
		Model:               "qwen3-35b-a3b-moe-64",
		Quant:               "UD-Q4_K_M",
		ContextLen:          131072,
		Backend:             "vulkan",
		WeightBytes:         22000000000,
		KVCacheBytes:        25769803776,
		HeadroomBytes:       8053063680,
		TotalBytes:          55822867456,
		UsableEnvelopeBytes: 67149381632,
		Fits:                true,
		Degraded:            false,
		Notes:               []string{},
		// The coder block surfaces unconditionally (D-07, no omitempty) and is
		// POPULATED here so the frozen bytes exercise the full schema-3 shape:
		// a fitting agent-profile pick with residency "swap".
		Coder: recommend.CoderFit{
			Model:         "qwen3-coder-30b-a3b",
			Quant:         "UD-Q4_K_XL",
			AgentCtx:      65536,
			WeightBytes:   17665334432,
			KVCacheBytes:  6442450944,
			HeadroomBytes: 8057925795,
			TotalBytes:    32165711171,
			Fits:          true,
			Residency:     "swap",
		},
		// SchemaVersion surfaces unconditionally in --json (D-06/D-07). The fixture
		// builds the struct directly (it does not call Pick), so it pins the contract
		// version explicitly; advice fields stay empty (no readiness fixture) and so
		// remain absent under omitempty. Schema 2 (Phase 22, D-03): the append-only
		// embedding_reservation_bytes + memory_considered keys surface as zero/false
		// here — the memory-off contract shape. Schema 3 (Phase 24, D-07): the
		// append-only coder block lands directly above schema_version.
		SchemaVersion: 3,
	}
}

// TestRecommendJSONGolden asserts `villa recommend --json` over the injected
// fixture matches cmd/villa/testdata/recommend.golden.json byte-for-byte (D-05).
// Run with -update to regenerate.
func TestRecommendJSONGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := renderRecommend(&buf, fixtureRecommendation(), nil, true /*json*/, false); err != nil {
		t.Fatalf("renderRecommend: %v", err)
	}

	golden := filepath.Join("testdata", "recommend.golden.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", golden)
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("JSON output does not match golden.\n--- got ---\n%s\n--- want ---\n%s", buf.String(), want)
	}
}

// TestRecommendTableShowsFitMath asserts the default table surfaces all four fit
// terms and the ≤ comparison (D-06 — math is SHOWN, not just applied), plus the
// coder (agent profile) section: model id, agent ctx, fits glyph and residency
// when a coder entry fits.
func TestRecommendTableShowsFitMath(t *testing.T) {
	var buf bytes.Buffer
	if err := renderRecommend(&buf, fixtureRecommendation(), nil, false /*table*/, false); err != nil {
		t.Fatalf("renderRecommend: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"model_bytes", "KV-cache", "headroom", "total", "usable envelope",
		"Coder (agent profile)", "qwen3-coder-30b-a3b", "agent ctx 65536", "residency: swap", "≤",
	} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Errorf("fit table missing %q:\n%s", want, out)
		}
	}
}

// TestRecommendTableCoderNoFitLine asserts the compact honest rendering when no
// coder entry fits: a single line stating no coder model fits and residency is
// "shared" — the JSON block is always stamped (D-07) but the table stays terse.
func TestRecommendTableCoderNoFitLine(t *testing.T) {
	rec := fixtureRecommendation()
	rec.Coder = recommend.CoderFit{Fits: false, Residency: "shared"}
	var buf bytes.Buffer
	if err := renderRecommend(&buf, rec, nil, false /*table*/, false); err != nil {
		t.Fatalf("renderRecommend: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Coder (agent profile)", "no coder model fits", "shared"} {
		if !strings.Contains(out, want) {
			t.Errorf("no-fit coder line missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "residency: swap") {
		t.Errorf("no-fit coder rendering must not claim swap residency:\n%s", out)
	}
}

// TestRecommendSaveWritesOnlyWithFlag drives the real command and asserts that a
// plain `recommend` writes NO config file while `recommend --save` writes one
// (D-20). XDG_CONFIG_HOME is redirected to a temp dir so the user's real config
// is never touched.
func TestRecommendSaveWritesOnlyWithFlag(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	cfgPath := filepath.Join(tmp, "villa", "config.toml")

	// Plain recommend: must NOT write config.
	runRecommend(t, []string{"recommend", "--json"})
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Errorf("plain recommend wrote config (or stat err %v) — must be read-only (D-20)", err)
	}

	// recommend --save: must write config under XDG.
	runRecommend(t, []string{"recommend", "--save", "--json"})
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("recommend --save did not write config: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("saved config perms = %o, want 600", perm)
	}
}

// TestSaveRecommendationPreservesDashboardPorts asserts that `recommend --save`
// writes a config.toml whose BYTES carry dashboard_port=8888 / chat_port=3000
// (never the dashboard-breaking zeros), proving the writer fix is independent of
// load-time normalization (Task 1). It then round-trips via LoadVillaFrom and
// asserts the loaded ports are 8888/3000 and the bind stays loopback (PRIV-01).
func TestSaveRecommendationPreservesDashboardPorts(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	cfgDir := filepath.Join(tmp, "villa")
	cfgPath := filepath.Join(cfgDir, "config.toml")

	runRecommend(t, []string{"recommend", "--save", "--json"})

	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read written config: %v", err)
	}
	bytesStr := string(raw)
	// Assert the persisted bytes themselves, NOT the loaded view — this proves the
	// writer no longer emits zeros, so the fix holds even without normalization.
	if !strings.Contains(bytesStr, "dashboard_port = 8888") {
		t.Errorf("config bytes missing 'dashboard_port = 8888':\n%s", bytesStr)
	}
	if !strings.Contains(bytesStr, "chat_port = 3000") {
		t.Errorf("config bytes missing 'chat_port = 3000':\n%s", bytesStr)
	}
	if strings.Contains(bytesStr, "dashboard_port = 0") || strings.Contains(bytesStr, "chat_port = 0") {
		t.Errorf("config bytes still carry zeroed ports:\n%s", bytesStr)
	}

	// Full round-trip: the loaded config must resolve to the loopback defaults.
	got, err := config.LoadVillaFrom(cfgDir)
	if err != nil {
		t.Fatalf("LoadVillaFrom: %v", err)
	}
	if got.DashboardPort != 8888 || got.ChatPort != 3000 {
		t.Errorf("round-trip ports = {dash %d, chat %d}, want {8888, 3000}", got.DashboardPort, got.ChatPort)
	}
	if got.DashboardAddr != "127.0.0.1" {
		t.Errorf("round-trip DashboardAddr = %q, want 127.0.0.1 (loopback-only, PRIV-01)", got.DashboardAddr)
	}
	// The saved selection fields must still be present.
	if got.Model == "" || got.Backend == "" {
		t.Errorf("round-trip dropped the recommendation: %+v", got)
	}
}

// TestDashboardURLNonZeroFromZeroedConfig locks the end-to-end user-visible
// symptom of gap test:1b at the cleanly-testable layer: a config.toml on disk
// carrying dashboard_port=0 / chat_port=0 / dashboard_addr="" (the user's
// already-broken file), loaded through the dashboard command's config path
// (config.LoadVillaFrom), must resolve to the usable loopback URL "127.0.0.1:8888"
// — never the unreachable "127.0.0.1:0" the dashboard previously printed. No live
// socket is bound, so this runs in CI without hardware.
func TestDashboardURLNonZeroFromZeroedConfig(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "villa")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	zeroed := `model = "qwen3-35b-a3b-moe-64"
quant = "UD-Q4_K_M"
ctx = 131072
backend = "vulkan"
dashboard_addr = ""
dashboard_port = 0
chat_port = 0
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(zeroed), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.LoadVillaFrom(dir)
	if err != nil {
		t.Fatalf("LoadVillaFrom: %v", err)
	}
	if cfg.DashboardPort == 0 {
		t.Fatalf("resolved DashboardPort is still 0 — dashboard would bind :0")
	}
	// The exact string the dashboard command prints (minus scheme).
	addr := net.JoinHostPort(cfg.DashboardAddr, strconv.Itoa(cfg.DashboardPort))
	if addr != "127.0.0.1:8888" {
		t.Errorf("resolved dashboard address = %q, want 127.0.0.1:8888 (never :0)", addr)
	}
}

// runRecommend executes the villa root command with args, capturing output.
func runRecommend(t *testing.T, args []string) {
	t.Helper()
	root := newRoot()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("villa %v: %v\noutput:\n%s", args, err, buf.String())
	}
}
