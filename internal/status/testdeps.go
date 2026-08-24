// Package statustest is the ONE stubbed Deps builder.
//
// There were three, around forty lines each: one in the status tests, one in the
// dashboard tests, one in the command tier. They drifted — the dashboard's returned
// an Unknown GTT reading and a zero weight where the status one returned a healthy
// pair — so the same core was covered against three subtly different worlds and no
// single test file could tell you what "healthy" meant.
//
// It lives in a NON-test file so all three tiers can use it, and it deliberately
// does NOT import testing: a testing import here would be linked into the shipped
// villa binary, which registers test flags and adds weight for nothing. The caller
// passes a temp dir and reports its own failure instead.
//
// Everything it builds is a stub; it touches no host.
package status

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// FixtureWeight is the model weight the stubbed deps report. It is under the
// fixture GTT reading, so the default world is a stack whose offload proves.
const FixtureWeight uint64 = 21504 * 1024 * 1024 // matches the Vulkan0 residency fixture

// fixtureGTTUsed is the GTT reading the stub serves: above FixtureWeight, so the
// residency floor is cleared.
const fixtureGTTUsed = "23068672000\n"

// StubServices are the service units the stubbed deps report, matching the units
// StubDeps renders. Exposed so a test can assert against the same names.
const (
	StubInferenceService = "villa-llama.service"
	StubOWUIService      = "villa-openwebui.service"
	StubDashboardService = "villa-dashboard.service"
)

// StubDeps builds fully-stubbed Deps describing a healthy loopback stack: every
// service active and ready, a residency journal proving offload, a matching /props,
// and a GTT reading that clears the floor.
//
// A test that wants an unhealthy world mutates the returned value rather than
// building its own, so "healthy" is defined exactly once.
func StubDeps(tempDir string, units []orchestrate.Unit) (Deps, error) {
	if err := os.WriteFile(filepath.Join(tempDir, "mem_info_gtt_used"), []byte(fixtureGTTUsed), 0o600); err != nil {
		return Deps{}, fmt.Errorf("seed GTT fixture: %w", err)
	}
	return Deps{
		LoadConfig: func() (config.VillaConfig, error) {
			return config.VillaConfig{Model: "qwen3", Quant: "Q4", Ctx: 131072, Backend: "vulkan"}, nil
		},
		ModelFile: func(config.VillaConfig) (string, error) { return "qwen3.gguf", nil },
		ResidentUnits: func(config.VillaConfig) ([]orchestrate.ResidentUnit, error) {
			return nil, nil
		},
		ModelsDir: func() string { return "/home/villa/.local/share/villa/models" },
		Render:    func(orchestrate.RenderInput) ([]orchestrate.Unit, error) { return units, nil },
		IsActive:  func(string) (string, error) { return "active", nil },
		JournalText: func(string) (string, bool) {
			return "load_tensors:      Vulkan0 model buffer size = 21504.49 MiB\n", true
		},
		Props: func(string) *inference.PropsInfo {
			return &inference.PropsInfo{ModelPath: "/models/qwen3.gguf", NCtx: 131072}
		},
		GTTUsed:     func() detect.Bytes { return detect.GTTUsedBytesForTest(tempDir) },
		WeightBytes: func(config.VillaConfig) uint64 { return FixtureWeight },
		Endpoint:    func() string { return "http://127.0.0.1:8080" },
		Services:    StubServiceList(HealthReady),
	}, nil
}

// StubServiceList builds the stub service list, every service reporting the given
// health. It mirrors the live list's shape: one inference service whose offload
// folds, and managed services whose offload is N/A.
func StubServiceList(h HealthState) []Service {
	probe := func() HealthState { return h }
	return []Service{
		{Unit: StubInferenceService, Kind: Inference, Probe: probe},
		{Unit: StubOWUIService, Kind: Managed, Probe: probe},
		{Unit: StubDashboardService, Kind: Managed, AlwaysRow: true, Probe: probe},
	}
}

// WithServiceHealth returns a copy of services where the named unit reports h. It is
// how a test says "this one service is down" without rebuilding the list.
func WithServiceHealth(services []Service, unit string, h HealthState) []Service {
	out := make([]Service, len(services))
	copy(out, services)
	for i := range out {
		if out[i].Unit == unit {
			out[i].Probe = func() HealthState { return h }
		}
	}
	return out
}

// WithoutProbe returns a copy of services where the named units have NO probe. It
// drives the case the old nil-guards covered: a service whose health cannot be
// evaluated reports Unknown rather than a fabricated ready or a borrowed verdict.
func WithoutProbe(services []Service, units ...string) []Service {
	out := make([]Service, len(services))
	copy(out, services)
	for i := range out {
		for _, u := range units {
			if out[i].Unit == u {
				out[i].Probe = nil
			}
		}
	}
	return out
}
