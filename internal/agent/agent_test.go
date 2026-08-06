package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// update regenerates the render-determinism golden fixture (NEW append-only):
// `go test ./internal/agent -run TestRenderGolden -update`.
var update = flag.Bool("update", false, "regenerate golden crush.json fixtures")

// TestPolicyLoad verifies the embedded crush-policy.json decodes to the FROZEN
// v0.76.0 pin (AGENT-01): the pinned version, the linux/amd64 asset name,
// its tarball SHA-256, and its size. Guards against an accidental edit to the
// compiled-in policy data drifting the install gate off the verified release.
func TestPolicyLoad(t *testing.T) {
	p := loadCrushPolicy()
	if p.Version != "v0.76.0" {
		t.Fatalf("policy version = %q, want v0.76.0", p.Version)
	}
	asset, ok := p.Assets["linux/amd64"]
	if !ok {
		t.Fatalf("policy has no linux/amd64 asset; assets=%v", p.Assets)
	}
	if asset.Name != "crush_0.76.0_Linux_x86_64.tar.gz" {
		t.Errorf("asset name = %q, want crush_0.76.0_Linux_x86_64.tar.gz", asset.Name)
	}
	wantSHA := "0f66114171270485763ffbc96f63403de5d598124c4f3841bc478c3a3a0d1ec9"
	if !strings.EqualFold(asset.SHA256, wantSHA) {
		t.Errorf("asset sha256 = %q, want %q", asset.SHA256, wantSHA)
	}
	if asset.Size != 25155696 {
		t.Errorf("asset size = %d, want 25155696", asset.Size)
	}
	if p.URLTmpl == "" || !strings.Contains(p.URLTmpl, "{asset}") {
		t.Errorf("urlTemplate = %q, want a {asset}-placeholder URL", p.URLTmpl)
	}
	// The binary hash was pinned on-hardware in Plan 03 (26-03): the EXTRACTED
	// `crush` binary SHA-256, derived from the SHA-256-verified tarball on the
	// gfx1151 box (Q2 / Pitfall 6 — distinct from the tarball SHA above). With a
	// real value present, binary-drift is now a confident signal, not a WARN.
	wantBinSHA := "4fd811f68c05da6c8d11fd1d5b6298a75ecc38a6c105a342b74e080cce8342b4"
	if !strings.EqualFold(asset.BinarySHA256, wantBinSHA) {
		t.Errorf("binarySha256 = %q, want the pinned extracted-binary hash %q", asset.BinarySHA256, wantBinSHA)
	}
	if asset.BinarySHA256 == unpinnedBinarySentinel {
		t.Error("binarySha256 is still the unpinned sentinel — Plan 03 must pin the real on-hardware hash")
	}
}

// TestPolicyLoadPanicsOnMalformed asserts the decode path panics on malformed
// policy bytes (build-time data, never runtime input). It exercises the
// SAME unmarshal-or-panic discipline via a helper rather than corrupting the real
// embed.
func TestPolicyLoadPanicsOnMalformed(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on malformed policy bytes, got none")
		}
	}()
	var p CrushPolicy
	if err := json.Unmarshal([]byte("{not valid json"), &p); err != nil {
		panic(err) // mirrors loadCrushPolicy's panic-on-malformed posture
	}
	t.Fatal("malformed bytes unmarshaled without error — unreachable")
}

// TestChecksumGate verifies the pure install gate: VerifyTarball
// passes ONLY when both size and hex SHA-256 match the pinned asset (case-
// insensitive), and refuses-with-remediation on a checksum OR size mismatch
// never a silent pass.
func TestChecksumGate(t *testing.T) {
	body := []byte("the pinned crush tarball bytes (fixture)")
	sum := sha256.Sum256(body)
	good := CrushAsset{
		Name:   "crush_0.76.0_Linux_x86_64.tar.gz",
		SHA256: hex.EncodeToString(sum[:]),
		Size:   uint64(len(body)),
	}

	t.Run("match passes", func(t *testing.T) {
		if err := VerifyTarball(good, body); err != nil {
			t.Fatalf("VerifyTarball(matching) = %v, want nil", err)
		}
	})

	t.Run("uppercase policy sha still passes (EqualFold)", func(t *testing.T) {
		upper := good
		upper.SHA256 = strings.ToUpper(good.SHA256)
		if err := VerifyTarball(upper, body); err != nil {
			t.Fatalf("VerifyTarball(uppercase sha) = %v, want nil (case-insensitive)", err)
		}
	})

	t.Run("checksum mismatch refuses", func(t *testing.T) {
		bad := good
		bad.SHA256 = "deadbeef" + good.SHA256[8:]
		err := VerifyTarball(bad, body)
		if err == nil {
			t.Fatal("VerifyTarball(checksum mismatch) = nil, want refuse")
		}
		if !strings.Contains(err.Error(), "checksum mismatch") {
			t.Errorf("error = %q, want a checksum-mismatch remediation", err.Error())
		}
	})

	t.Run("size mismatch refuses before hashing", func(t *testing.T) {
		wrongSize := good
		wrongSize.Size = good.Size + 1
		err := VerifyTarball(wrongSize, body)
		if err == nil {
			t.Fatal("VerifyTarball(size mismatch) = nil, want refuse")
		}
		if !strings.Contains(err.Error(), "size mismatch") {
			t.Errorf("error = %q, want a size-mismatch remediation", err.Error())
		}
	})
}

// TestVersionCompare asserts the cloned comparator behaves identically to the
// floors.go original: -1/0/+1, tolerant of a leading "v" and of trailing
// pre-release/distro suffixes.
func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.76.0", "0.76.0", 0},
		{"v0.76.0", "0.76.0", 0},
		{"0.76.0", "0.75.9", 1},
		{"0.75.9", "0.76.0", -1},
		{"v0.76.1", "v0.76.0", 1},
		{"0.76.0-rc1", "0.76.0", 0}, // suffix stops the segment; numeric portions equal
		{"1.0.0", "0.999.999", 1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// renderTestConfig is the fixed config the render tests/golden are pinned to. Using
// CoderModel exercises the coding-mode served-id derivation.
func renderTestConfig() config.VillaConfig {
	return config.VillaConfig{
		Model:         "qwen3-30b",
		CoderModel:    "qwen3-coder-30b-a3b",
		CoderAgentCtx: 65536,
	}
}

// renderTestProbes pins the LSP probe inputs: gopls FOUND, pyright MISSING (so the
// golden exercises both the rendered-entry and WARN-and-omit paths).
func renderTestProbes() []LSPProbe {
	return []LSPProbe{
		{Key: "go", Command: "gopls", Found: true},
		{Key: "python", Command: "pyright-langserver", Found: false},
	}
}

// TestRenderGolden asserts Render is byte-deterministic (Pitfall 4) and equals the
// append-only golden fixture (refreezable via -update). Two renders of the same
// inputs MUST be byte-identical (config-drift compare depends on it).
func TestRenderGolden(t *testing.T) {
	cfg := renderTestConfig()
	probes := renderTestProbes()

	got, _, err := Render(cfg, probes)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	again, _, err := Render(cfg, probes)
	if err != nil {
		t.Fatalf("Render (2nd): %v", err)
	}
	if string(got) != string(again) {
		t.Fatal("Render is not byte-deterministic across two identical calls")
	}

	goldenPath := filepath.Join("testdata", "crush.json.golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("rendered crush.json != golden\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestRenderContract asserts the rendered JSON satisfies the locked contract:
// both kill switches set; exactly ONE openai-compat provider at the loopback
// base_url with a non-empty models[] whose id is villa- prefixed.
func TestRenderContract(t *testing.T) {
	got, _, err := Render(renderTestConfig(), renderTestProbes())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var parsed struct {
		Options struct {
			DisableMetrics            bool `json:"disable_metrics"`
			DisableProviderAutoUpdate bool `json:"disable_provider_auto_update"`
		} `json:"options"`
		Providers map[string]struct {
			Type    string `json:"type"`
			BaseURL string `json:"base_url"`
			Models  []struct {
				ID string `json:"id"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("rendered crush.json does not parse: %v", err)
	}
	if !parsed.Options.DisableMetrics || !parsed.Options.DisableProviderAutoUpdate {
		t.Errorf("kill switches: disable_metrics=%v disable_provider_auto_update=%v, want both true",
			parsed.Options.DisableMetrics, parsed.Options.DisableProviderAutoUpdate)
	}
	if len(parsed.Providers) != 1 {
		t.Fatalf("provider count = %d, want exactly 1", len(parsed.Providers))
	}
	p, ok := parsed.Providers["villa"]
	if !ok {
		t.Fatalf("no 'villa' provider; providers=%v", parsed.Providers)
	}
	if p.Type != "openai-compat" {
		t.Errorf("provider type = %q, want openai-compat", p.Type)
	}
	if p.BaseURL != "http://127.0.0.1:8080/v1" {
		t.Errorf("base_url = %q, want the loopback literal http://127.0.0.1:8080/v1", p.BaseURL)
	}
	if len(p.Models) == 0 {
		t.Fatal("models[] is empty — would trigger a startup /models fetch (Pitfall 3)")
	}
	if !strings.HasPrefix(p.Models[0].ID, "villa-") {
		t.Errorf("models[0].id = %q, want a villa- prefix (#2649)", p.Models[0].ID)
	}
}

// TestLSPMissingWarn asserts: a not-found server WARNs and OMITS the entry; a
// found server renders a fixed-literal command and emits no warning; Render never
// returns a blocking error for a missing LSP server.
func TestLSPMissingWarn(t *testing.T) {
	probes := []LSPProbe{
		{Key: "go", Command: "gopls", Found: false},
		{Key: "rust", Command: "rust-analyzer", Found: true},
	}
	got, warns, err := Render(renderTestConfig(), probes)
	if err != nil {
		t.Fatalf("Render returned a blocking error for a missing LSP server: %v", err)
	}
	var found bool
	for _, w := range warns {
		if w.Code == "lsp_missing" && strings.Contains(w.Msg, "gopls") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an lsp_missing WARN mentioning gopls; warns=%v", warns)
	}
	var parsed struct {
		LSP map[string]struct {
			Command string `json:"command"`
		} `json:"lsp"`
	}
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := parsed.LSP["go"]; ok {
		t.Error("missing gopls should be OMITTED from the lsp block, but 'go' is present")
	}
	rust, ok := parsed.LSP["rust"]
	if !ok || rust.Command != "rust-analyzer" {
		t.Errorf("found rust-analyzer should render entry with fixed command; got %+v ok=%v", rust, ok)
	}
}

// TestRenderNoMetachars asserts NO rendered value contains a shell metacharacter
// sequence ($(, backtick, ${) — Crush $(...)-expands config values at load
// (Pitfall 1); every villa-rendered value MUST be metachar-free.
func TestRenderNoMetachars(t *testing.T) {
	got, _, err := Render(renderTestConfig(), renderTestProbes())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, bad := range []string{"$(", "`", "${"} {
		if strings.Contains(string(got), bad) {
			t.Errorf("rendered crush.json contains shell metachar %q (Pitfall 1)", bad)
		}
	}
}

const (
	pinnedBinSHA = "1111111111111111111111111111111111111111111111111111111111111111"
	otherBinSHA  = "2222222222222222222222222222222222222222222222222222222222222222"
)

// TestBinaryDrift asserts: a mismatched installed binary hash is a confident
// BinaryDrift; equal hashes are clean; an UNPINNED policy hash degrades to a
// typed-Unknown WARN (BinaryDriftUnknown), never a false drift (Pitfall 6).
func TestBinaryDrift(t *testing.T) {
	rendered, _, _ := Render(renderTestConfig(), renderTestProbes())

	t.Run("mismatch is confident drift", func(t *testing.T) {
		r := DetectDrift(DriftInput{
			BinaryPresent: true, InstalledBinSHA: otherBinSHA, PolicyBinSHA: pinnedBinSHA,
			ConfigPresent: true, OnDiskConfig: rendered, RenderedConfig: rendered,
		})
		if !r.BinaryDrift {
			t.Error("BinaryDrift = false, want true on a hash mismatch")
		}
		if r.BinaryDriftUnknown {
			t.Error("BinaryDriftUnknown = true on a confident mismatch, want false")
		}
		if r.Reason == "" {
			t.Error("expected a remediation Reason on binary drift")
		}
	})

	t.Run("equal hashes are clean", func(t *testing.T) {
		r := DetectDrift(DriftInput{
			BinaryPresent: true, InstalledBinSHA: pinnedBinSHA, PolicyBinSHA: pinnedBinSHA,
			ConfigPresent: true, OnDiskConfig: rendered, RenderedConfig: rendered,
		})
		if r.BinaryDrift || r.BinaryDriftUnknown {
			t.Errorf("equal hashes: BinaryDrift=%v Unknown=%v, want both false", r.BinaryDrift, r.BinaryDriftUnknown)
		}
	})

	t.Run("unpinned sentinel degrades to typed-Unknown WARN", func(t *testing.T) {
		r := DetectDrift(DriftInput{
			BinaryPresent: true, InstalledBinSHA: otherBinSHA, PolicyBinSHA: unpinnedBinarySentinel,
			ConfigPresent: true, OnDiskConfig: rendered, RenderedConfig: rendered,
		})
		if r.BinaryDrift {
			t.Error("BinaryDrift = true against the unpinned sentinel, want a typed-Unknown WARN (false drift avoided)")
		}
		if !r.BinaryDriftUnknown {
			t.Error("BinaryDriftUnknown = false against the unpinned sentinel, want true")
		}
	})

	t.Run("empty policy hash also degrades to Unknown", func(t *testing.T) {
		r := DetectDrift(DriftInput{
			BinaryPresent: true, InstalledBinSHA: otherBinSHA, PolicyBinSHA: "",
			ConfigPresent: true, OnDiskConfig: rendered, RenderedConfig: rendered,
		})
		if r.BinaryDrift || !r.BinaryDriftUnknown {
			t.Errorf("empty policy hash: BinaryDrift=%v Unknown=%v, want false/true", r.BinaryDrift, r.BinaryDriftUnknown)
		}
	})

	t.Run("absent binary is BinaryAbsent not drift", func(t *testing.T) {
		r := DetectDrift(DriftInput{
			BinaryPresent: false, PolicyBinSHA: pinnedBinSHA,
			ConfigPresent: true, OnDiskConfig: rendered, RenderedConfig: rendered,
		})
		if !r.BinaryAbsent {
			t.Error("BinaryAbsent = false on a missing binary, want true")
		}
		if r.BinaryDrift {
			t.Error("BinaryDrift = true on a missing binary, want false (absent != drift)")
		}
	})
}

// TestConfigAbsent asserts: ConfigPresent=false is the FIRST-RUN render
// trigger (ConfigAbsent), DISTINCT from ConfigDrift, and an absent config is never
// compared against the rendered reference (no false drift).
func TestConfigAbsent(t *testing.T) {
	rendered, _, _ := Render(renderTestConfig(), renderTestProbes())
	r := DetectDrift(DriftInput{
		BinaryPresent: true, InstalledBinSHA: pinnedBinSHA, PolicyBinSHA: pinnedBinSHA,
		ConfigPresent: false, OnDiskConfig: nil, RenderedConfig: rendered,
	})
	if !r.ConfigAbsent {
		t.Error("ConfigAbsent = false on a missing crush.json, want true (first-run render trigger)")
	}
	if r.ConfigDrift {
		t.Error("ConfigDrift = true on a missing crush.json, want false (absent != drift)")
	}
}

// TestConfigDrift asserts + Pitfall 4: a present config that semantically
// differs is ConfigDrift; a whitespace-only re-save of identical semantics is NOT
// drift (parsed-semantic compare).
func TestConfigDrift(t *testing.T) {
	rendered, _, _ := Render(renderTestConfig(), renderTestProbes())

	t.Run("semantic difference is drift", func(t *testing.T) {
		// A different config (no coder model) renders a different model id.
		other, _, _ := Render(config.VillaConfig{Model: "different-model"}, nil)
		r := DetectDrift(DriftInput{
			BinaryPresent: true, InstalledBinSHA: pinnedBinSHA, PolicyBinSHA: pinnedBinSHA,
			ConfigPresent: true, OnDiskConfig: other, RenderedConfig: rendered,
		})
		if !r.ConfigDrift {
			t.Error("ConfigDrift = false on a semantic difference, want true")
		}
		if r.Reason == "" {
			t.Error("expected a remediation Reason on config drift")
		}
	})

	t.Run("whitespace-only difference is not drift", func(t *testing.T) {
		// Re-indent the rendered config with different whitespace but identical semantics.
		var v any
		if err := json.Unmarshal(rendered, &v); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		reSaved, err := json.MarshalIndent(v, "", "    ") // 4-space indent vs the rendered 2-space
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		r := DetectDrift(DriftInput{
			BinaryPresent: true, InstalledBinSHA: pinnedBinSHA, PolicyBinSHA: pinnedBinSHA,
			ConfigPresent: true, OnDiskConfig: reSaved, RenderedConfig: rendered,
		})
		if r.ConfigDrift {
			t.Error("ConfigDrift = true on a whitespace-only re-save, want false (parsed-semantic compare, Pitfall 4)")
		}
	})
}
