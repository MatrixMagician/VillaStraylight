package preflight

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

func TestCheckLinger(t *testing.T) {
	on := readFixture(t, "linger-on.txt")
	off := readFixture(t, "linger-off.txt")

	t.Run("linger on passes (WARN tier)", func(t *testing.T) {
		d := lingerDeps{username: "oliverh", lingerOutput: func(string) (string, bool, bool) { return on, true, true }}
		got := checkLinger(d)
		if got.Tier != TierWarn {
			t.Errorf("linger must be WARN tier, got %v", got.Tier)
		}
		if got.Status != StatusPass {
			t.Errorf("linger on should pass, got %v", got.Status)
		}
	})

	t.Run("linger off warns with enable-linger hint", func(t *testing.T) {
		d := lingerDeps{username: "oliverh", lingerOutput: func(string) (string, bool, bool) { return off, true, true }}
		got := checkLinger(d)
		if got.Tier != TierWarn || got.Status != StatusWarn {
			t.Fatalf("linger off should be WARN/WARN, got tier=%v status=%v", got.Tier, got.Status)
		}
		if !strings.Contains(got.Remediation, "enable-linger") {
			t.Errorf("remediation should mention loginctl enable-linger, got %q", got.Remediation)
		}
	})

	t.Run("loginctl missing warns (D-15) and stays WARN tier", func(t *testing.T) {
		d := lingerDeps{username: "oliverh", lingerOutput: func(string) (string, bool, bool) { return "", false, false }}
		got := checkLinger(d)
		if got.Tier != TierWarn || got.Status != StatusWarn {
			t.Fatalf("loginctl missing should be WARN/WARN, got tier=%v status=%v", got.Tier, got.Status)
		}
	})

	t.Run("unparseable output warns", func(t *testing.T) {
		d := lingerDeps{username: "oliverh", lingerOutput: func(string) (string, bool, bool) { return "garbage no equals", true, true }}
		got := checkLinger(d)
		if got.Status != StatusWarn {
			t.Errorf("unparseable linger should WARN, got %v", got.Status)
		}
	})
}
