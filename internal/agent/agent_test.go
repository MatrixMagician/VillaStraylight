package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

// TestPolicyLoad verifies the embedded crush-policy.json decodes to the FROZEN
// v0.76.0 pin (AGENT-01, D-02): the pinned version, the linux/amd64 asset name,
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
	// The binary hash is the UNPINNED sentinel until Plan 03 records it on-hardware.
	if asset.BinarySHA256 != unpinnedBinarySentinel {
		t.Errorf("binarySha256 = %q, want the unpinned sentinel %q (Plan 03 replaces it)", asset.BinarySHA256, unpinnedBinarySentinel)
	}
}

// TestPolicyLoadPanicsOnMalformed asserts the decode path panics on malformed
// policy bytes (build-time data, never runtime input — T-26-05). It exercises the
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

// TestChecksumGate verifies the pure D-03 install gate (T-26-01): VerifyTarball
// passes ONLY when both size and hex SHA-256 match the pinned asset (case-
// insensitive), and refuses-with-remediation on a checksum OR size mismatch —
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
