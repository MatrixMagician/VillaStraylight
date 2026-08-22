package subsystem

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// TestGatesAnswerTheConfigFlags pins each gate to its config field. These are the
// mappings every caller now depends on, so they are asserted once here rather than
// re-derived at each read site.
func TestGatesAnswerTheConfigFlags(t *testing.T) {
	cases := []struct {
		kind Kind
		set  func(*config.VillaConfig)
		key  string
	}{
		{Memory, func(c *config.VillaConfig) { c.MemoryEnabled = true }, "memory_enabled"},
		{WebSearch, func(c *config.VillaConfig) { c.WebSearchEnabled = true }, "web_search_enabled"},
		{Agent, func(c *config.VillaConfig) { c.AgentEnabled = true }, "agent_enabled"},
		{CodingMode, func(c *config.VillaConfig) { c.CodingMode = true }, "coding_mode"},
	}
	for _, tc := range cases {
		t.Run(tc.kind.String(), func(t *testing.T) {
			off := config.VillaConfig{}
			if On(off, tc.kind) {
				t.Errorf("%v reads on for a zero config; every optional subsystem defaults OFF", tc.kind)
			}
			on := config.VillaConfig{}
			tc.set(&on)
			if !On(on, tc.kind) {
				t.Errorf("%v reads off despite its flag being set", tc.kind)
			}
			if tc.kind.ConfigKey() != tc.key {
				t.Errorf("ConfigKey = %q, want %q — remediation text tells the operator what to edit", tc.kind.ConfigKey(), tc.key)
			}
		})
	}
}

// TestGatesAreIndependent: turning one subsystem on must not turn another on. A
// shared-flag bug here would enable a whole stack the operator never opted into,
// which is exactly the failure the scattered reads made hard to see.
func TestGatesAreIndependent(t *testing.T) {
	for _, k := range All {
		cfg := config.VillaConfig{}
		switch k {
		case Memory:
			cfg.MemoryEnabled = true
		case WebSearch:
			cfg.WebSearchEnabled = true
		case Agent:
			cfg.AgentEnabled = true
		case CodingMode:
			cfg.CodingMode = true
		}
		enabled := Enabled(cfg)
		if len(enabled) != 1 || enabled[0] != k {
			t.Errorf("enabling %v turned on %v; the gates must be independent", k, enabled)
		}
	}
}

// TestNamedAccessorsAgreeWithOn: the four convenience accessors and the enumerable
// form must never disagree, or a caller's choice of spelling would change the answer.
func TestNamedAccessorsAgreeWithOn(t *testing.T) {
	cfg := config.VillaConfig{
		MemoryEnabled:    true,
		WebSearchEnabled: false,
		AgentEnabled:     true,
		CodingMode:       false,
	}
	pairs := []struct {
		kind  Kind
		named bool
	}{
		{Memory, MemoryOn(cfg)},
		{WebSearch, WebSearchOn(cfg)},
		{Agent, AgentOn(cfg)},
		{CodingMode, CodingModeOn(cfg)},
	}
	for _, p := range pairs {
		if got := On(cfg, p.kind); got != p.named {
			t.Errorf("%v: On = %v but the named accessor = %v", p.kind, got, p.named)
		}
	}
}

// TestEnabledPreservesAllOrder: reporters render subsystems in a stable order, so a
// map-backed implementation (which would randomise it) must not creep in.
func TestEnabledPreservesAllOrder(t *testing.T) {
	cfg := config.VillaConfig{MemoryEnabled: true, WebSearchEnabled: true, AgentEnabled: true, CodingMode: true}
	got := Enabled(cfg)
	if len(got) != len(All) {
		t.Fatalf("Enabled returned %d of %d subsystems", len(got), len(All))
	}
	for i, k := range All {
		if got[i] != k {
			t.Errorf("Enabled[%d] = %v, want %v — the order must match All", i, got[i], k)
		}
	}
}

// TestModuleIsLoadBearing is the deletion test the memory module used to fail.
//
// The point of routing gates through here is that deleting this package would break
// callers. This asserts the property directly: the flags are read through the gate
// across the tiers, not around it. A direct predicate read of a gating flag outside
// this package (and outside config, which defines the fields) means the module has
// started sliding back toward decorative.
func TestModuleIsLoadBearing(t *testing.T) {
	// Predicate reads only: an `if cfg.MemoryEnabled`, a `&&`, a `return c.AgentEnabled`.
	// Assignments (`cfg.MemoryEnabled = ...`) are how a gate gets SET, which is a
	// different operation and legitimately touches the field.
	flags := regexp.MustCompile(`\b(cfg|c)\.(MemoryEnabled|WebSearchEnabled|AgentEnabled|CodingMode)\b\s*(?:[^=]|$)`)
	predicate := regexp.MustCompile(`\bif\b|&&|\|\||\breturn\b`)

	repoRoot := filepath.Join("..", "..")
	var bypasses []string

	for _, dir := range []string{"cmd/villa", "internal"} {
		root := filepath.Join(repoRoot, dir)
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, rerr := filepath.Rel(repoRoot, path)
			if rerr != nil {
				return rerr
			}
			rel = filepath.ToSlash(rel)
			// This package defines the gates; config defines the fields.
			if strings.HasPrefix(rel, "internal/subsystem/") || strings.HasPrefix(rel, "internal/config/") {
				return nil
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			for i, line := range strings.Split(string(data), "\n") {
				code := line
				if idx := strings.Index(code, "//"); idx >= 0 {
					code = code[:idx] // a comment may name a field freely
				}
				if flags.MatchString(code) && predicate.MatchString(code) {
					bypasses = append(bypasses, rel+":"+itoa(i+1))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}

	if len(bypasses) > 0 {
		t.Errorf("subsystem gates were read directly instead of through this module:\n  %s\n"+
			"Use subsystem.MemoryOn / WebSearchOn / AgentOn / CodingModeOn so the gate has one answer.",
			strings.Join(bypasses, "\n  "))
	}
}

// itoa avoids pulling strconv in for one call in a test.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
