package install

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// install_test.go covers install's decisions WITHOUT constructing the dependency
// set. That is the point of the extraction: these questions previously could not be
// asked without standing up 51 fake fields, so in practice they were not asked.

// fit is a recommendation that installs cleanly: a real model, a positive context,
// a non-zero weight.
func fit() recommend.Recommendation {
	return recommend.Recommendation{
		Model:       "qwen3-30b-a3b",
		Quant:       "Q4_K_M",
		ContextLen:  8192,
		Backend:     "rocm",
		WeightBytes: 21 << 30,
	}
}

// coderFit adds a swap-residency coder, which is what lets the agent addon enter
// coding mode.
func coderFit() recommend.Recommendation {
	r := fit()
	r.Coder = recommend.CoderFit{
		Model:    "qwen3-coder-30b-a3b",
		Quant:    "Q4_K_M",
		AgentCtx: 32768,
	}
	return r
}

// chosen is the backend-preserved predicate the command tier supplies.
func chosen(name string) bool { return name == "vulkan" || name == "rocm" }

// TestFlagsTurnSubsystemsOnNeverOff is the asymmetry that makes opt-in safe. A flag
// enables a subsystem for this run and persists the choice; nothing on the command
// line turns one off, so a bare install after an opt-in never silently tears down a
// subsystem the operator is relying on. Disabling is an explicit config edit.
func TestFlagsTurnSubsystemsOnNeverOff(t *testing.T) {
	t.Run("a flag turns a subsystem on", func(t *testing.T) {
		g := ResolveGates(config.VillaConfig{}, Opts{CodingAgent: true, WebSearch: true}, fit())
		if !g.Agent {
			t.Error("--coding-agent must enable the agent for this run")
		}
		if !g.WebSearch {
			t.Error("--web-search must enable web search for this run")
		}
	})

	t.Run("an absent flag never turns a persisted subsystem off", func(t *testing.T) {
		on := config.VillaConfig{MemoryEnabled: true, WebSearchEnabled: true, AgentEnabled: true}
		g := ResolveGates(on, Opts{}, fit())
		for _, k := range []subsystem.Kind{subsystem.Memory, subsystem.WebSearch, subsystem.Agent} {
			if !g.On(k) {
				t.Errorf("a bare install turned %v OFF; disabling must be an explicit config edit", k)
			}
		}
	})

	t.Run("memory has no flag and follows the persisted config", func(t *testing.T) {
		if ResolveGates(config.VillaConfig{}, Opts{CodingAgent: true, WebSearch: true}, fit()).Memory {
			t.Error("no flag enables memory; it follows the persisted config alone")
		}
	})
}

// TestAgentGateCoversAFirstTimeOptIn is a real failure this decision prevents.
// Gating the agent's disk and envelope preflight on the PERSISTED flag alone would
// let a first `villa install --coding-agent` stage the agent on a host that was
// never checked for the space or the memory envelope to run it.
func TestAgentGateCoversAFirstTimeOptIn(t *testing.T) {
	firstTime := ResolveGates(config.VillaConfig{}, Opts{CodingAgent: true}, coderFit())
	if !GateAgentChecks(firstTime) {
		t.Error("a first-time --coding-agent run must still be gated: disk and envelope checked BEFORE staging")
	}

	persisted := ResolveGates(config.VillaConfig{AgentEnabled: true}, Opts{}, coderFit())
	if !GateAgentChecks(persisted) {
		t.Error("a persisted agent install must be gated")
	}

	off := ResolveGates(config.VillaConfig{}, Opts{}, fit())
	if GateAgentChecks(off) {
		t.Error("an agent-off install must not run the agent gates — that path stays byte-identical")
	}
}

// TestCodingModeRequiresACoderToServe: the addon serves the coder it stages, so
// entering coding mode without a swap-residency coder fit would render a unit
// serving an empty model. A shared-residency fit leaves coding mode off, and the
// coder-fit refusal handles it later.
func TestCodingModeRequiresACoderToServe(t *testing.T) {
	t.Run("a swap-residency coder fit enters coding mode", func(t *testing.T) {
		g := ResolveGates(config.VillaConfig{}, Opts{CodingAgent: true}, coderFit())
		if !g.CodingMode {
			t.Error("--coding-agent with a real coder fit must enter coding mode")
		}
	})

	t.Run("no coder fit leaves coding mode off", func(t *testing.T) {
		g := ResolveGates(config.VillaConfig{}, Opts{CodingAgent: true}, fit())
		if g.CodingMode {
			t.Error("entering coding mode with no coder would render a unit serving an empty model")
		}
	})

	t.Run("the agent gate is still on without a coder fit", func(t *testing.T) {
		g := ResolveGates(config.VillaConfig{}, Opts{CodingAgent: true}, fit())
		if !g.Agent {
			t.Error("the addon is enabled even when the coder does not fit; the fit refusal reports why")
		}
	})
}

// TestPlanPreservesPersistedCustomisation is why the plan seeds from the persisted
// config rather than from defaults. A user's memory, dashboard and chat settings
// must survive an install; resetting them to seed values would be silent data loss.
func TestPlanPreservesPersistedCustomisation(t *testing.T) {
	persisted := config.VillaConfig{
		Backend:        "vulkan",
		EmbeddingModel: "custom-embed",
		EmbeddingDim:   1024,
		DashboardPort:  9999,
		ChatPort:       4000,
		MemoryEnabled:  true,
	}

	plan := AssemblePlan(persisted, ResolveGates(persisted, Opts{}, fit()), fit(), chosen)

	if plan.Config.EmbeddingModel != "custom-embed" || plan.Config.EmbeddingDim != 1024 {
		t.Errorf("install reset the persisted embedding identity: %+v", plan.Config)
	}
	if plan.Config.DashboardPort != 9999 || plan.Config.ChatPort != 4000 {
		t.Errorf("install reset the persisted dashboard/chat ports: %+v", plan.Config)
	}
}

// TestPlanTakesTheRecommendedSelection: the model, quant and context always come
// from the recommendation, since that is what install just fitted to the host.
func TestPlanTakesTheRecommendedSelection(t *testing.T) {
	old := config.VillaConfig{Model: "old", Quant: "old", Ctx: 1}
	plan := AssemblePlan(old, ResolveGates(old, Opts{}, fit()), fit(), chosen)
	rec := fit()
	if plan.Config.Model != rec.Model || plan.Config.Quant != rec.Quant || plan.Config.Ctx != rec.ContextLen {
		t.Errorf("plan must carry the recommended selection, got %+v", plan.Config)
	}
}

// TestBackendChoiceIsPreservedNotReverted guards a real regression. The recommender
// always returns the default backend and carries no backend override, so assigning
// it unconditionally would silently revert a deliberately-chosen backend on every
// re-install — including a re-install triggered only by adding an addon — and
// re-render the inference unit to the default image.
func TestBackendChoiceIsPreservedNotReverted(t *testing.T) {
	t.Run("a deliberately-chosen backend survives a re-install", func(t *testing.T) {
		plan := AssemblePlan(config.VillaConfig{Backend: "vulkan"}, ResolveGates(config.VillaConfig{Backend: "vulkan"}, Opts{}, fit()), fit(), chosen)
		if plan.Config.Backend != "vulkan" {
			t.Errorf("Backend = %q, want the persisted vulkan preserved", plan.Config.Backend)
		}
		if !plan.BackendPreserved {
			t.Error("BackendPreserved must report that the choice was kept")
		}
	})

	t.Run("adding an addon does not revert the backend", func(t *testing.T) {
		plan := AssemblePlan(config.VillaConfig{Backend: "vulkan"}, ResolveGates(config.VillaConfig{Backend: "vulkan"}, Opts{CodingAgent: true}, coderFit()), coderFit(), chosen)
		if plan.Config.Backend != "vulkan" {
			t.Errorf("`install --coding-agent` reverted the backend to %q", plan.Config.Backend)
		}
	})

	t.Run("an unset backend takes the recommendation", func(t *testing.T) {
		plan := AssemblePlan(config.VillaConfig{}, ResolveGates(config.VillaConfig{}, Opts{}, fit()), fit(), chosen)
		if plan.Config.Backend != fit().Backend {
			t.Errorf("Backend = %q, want the recommended %q", plan.Config.Backend, fit().Backend)
		}
		if plan.BackendPreserved {
			t.Error("nothing was preserved; the recommendation was taken")
		}
	})

	t.Run("an unrecognised persisted backend falls through to the recommendation", func(t *testing.T) {
		plan := AssemblePlan(config.VillaConfig{Backend: "bogus"}, ResolveGates(config.VillaConfig{Backend: "bogus"}, Opts{}, fit()), fit(), chosen)
		if plan.Config.Backend != fit().Backend {
			t.Errorf("Backend = %q, want the recommendation for an unrecognised value", plan.Config.Backend)
		}
	})
}

// TestPlanCarriesTheCoderIdentity: the unit, the agent config and the readiness
// proof must all agree on which coder is served, so they come from the same
// recommendation the disk and envelope gates were computed from.
func TestPlanCarriesTheCoderIdentity(t *testing.T) {
	rec := coderFit()
	plan := AssemblePlan(config.VillaConfig{}, ResolveGates(config.VillaConfig{}, Opts{CodingAgent: true}, rec), rec, chosen)

	if !plan.Config.CodingMode {
		t.Fatal("the addon with a coder fit must enter coding mode")
	}
	if plan.Config.CoderModel != rec.Coder.Model || plan.Config.CoderQuant != rec.Coder.Quant {
		t.Errorf("coder identity = %q/%q, want the gated recommendation's %q/%q",
			plan.Config.CoderModel, plan.Config.CoderQuant, rec.Coder.Model, rec.Coder.Quant)
	}
	if plan.Config.CoderAgentCtx != rec.Coder.AgentCtx {
		t.Errorf("CoderAgentCtx = %d, want %d — the rendered single -c must match the gated fit",
			plan.Config.CoderAgentCtx, rec.Coder.AgentCtx)
	}
}

// TestPlanGatesMatchTheConfigItWrites: the resolved gates and the config the plan
// persists must agree, or a later step reading one would disagree with a step
// reading the other.
func TestPlanGatesMatchTheConfigItWrites(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.VillaConfig
		opts Opts
		rec  recommend.Recommendation
	}{
		{"all off", config.VillaConfig{}, Opts{}, fit()},
		{"memory persisted", config.VillaConfig{MemoryEnabled: true}, Opts{}, fit()},
		{"agent opt-in", config.VillaConfig{}, Opts{CodingAgent: true}, coderFit()},
		{"web opt-in", config.VillaConfig{}, Opts{WebSearch: true}, fit()},
		{"everything", config.VillaConfig{MemoryEnabled: true}, Opts{CodingAgent: true, WebSearch: true}, coderFit()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := AssemblePlan(tc.cfg, ResolveGates(tc.cfg, tc.opts, tc.rec), tc.rec, chosen)
			if got := subsystem.MemoryOn(plan.Config); got != plan.Gates.Memory {
				t.Errorf("memory: config says %v, gates say %v", got, plan.Gates.Memory)
			}
			if got := subsystem.WebSearchOn(plan.Config); got != plan.Gates.WebSearch {
				t.Errorf("web search: config says %v, gates say %v", got, plan.Gates.WebSearch)
			}
			if got := subsystem.AgentOn(plan.Config); got != plan.Gates.Agent {
				t.Errorf("agent: config says %v, gates say %v", got, plan.Gates.Agent)
			}
			if got := subsystem.CodingModeOn(plan.Config); got != plan.Gates.CodingMode {
				t.Errorf("coding mode: config says %v, gates say %v", got, plan.Gates.CodingMode)
			}
		})
	}
}

// TestUnusableRecommendationIsRefused: an empty model, a non-positive context or a
// zero weight all mean no catalog model fit the envelope. Starting an inference
// server with no fit, or with a zero context, is never valid.
func TestUnusableRecommendationIsRefused(t *testing.T) {
	cases := []struct {
		name string
		rec  recommend.Recommendation
		want bool
	}{
		{"a real fit installs", fit(), true},
		{"no model", recommend.Recommendation{ContextLen: 8192, WeightBytes: 1}, false},
		{"zero context", recommend.Recommendation{Model: "m", ContextLen: 0, WeightBytes: 1}, false},
		{"negative context", recommend.Recommendation{Model: "m", ContextLen: -1, WeightBytes: 1}, false},
		{"zero weight", recommend.Recommendation{Model: "m", ContextLen: 8192, WeightBytes: 0}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RecommendationUsable(tc.rec); got != tc.want {
				t.Errorf("RecommendationUsable = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestResourceFitIncludesTheEmbeddingReservation: the memory floor must reflect what
// will actually be resident. Omitting the embedding reservation would gate a
// memory-on install against a floor smaller than the stack it is about to start.
func TestResourceFitIncludesTheEmbeddingReservation(t *testing.T) {
	rec := fit()
	rec.KVCacheBytes = 2 << 30
	rec.HeadroomBytes = 1 << 30
	rec.EmbeddingReservationBytes = 512 << 20

	got := ResourceFit(rec)
	want := rec.WeightBytes + rec.KVCacheBytes + rec.HeadroomBytes + rec.EmbeddingReservationBytes
	if got.MinMemBytes != want {
		t.Errorf("MinMemBytes = %d, want %d (weights + KV + headroom + embedding)", got.MinMemBytes, want)
	}
	if got.MinDiskBytes != rec.WeightBytes {
		t.Errorf("MinDiskBytes = %d, want the weight footprint %d", got.MinDiskBytes, rec.WeightBytes)
	}

	// Memory off: the reservation is zero, so the memory-off gate is unchanged.
	rec.EmbeddingReservationBytes = 0
	off := ResourceFit(rec)
	if off.MinMemBytes != rec.WeightBytes+rec.KVCacheBytes+rec.HeadroomBytes {
		t.Errorf("a memory-off fit must not reserve embedding bytes, got %d", off.MinMemBytes)
	}
}

// TestAssemblePlanCarriesSpeculation asserts the install persists the speculation
// mode the recommendation resolved, the same way it persists the backend, so a
// first install renders the unit the recommendation described.
func TestAssemblePlanCarriesSpeculation(t *testing.T) {
	rec := recommend.Recommendation{Model: "m", Quant: "q", ContextLen: 4096, Backend: "rocm", Speculation: "ngram"}
	plan := AssemblePlan(config.VillaConfig{}, Gates{}, rec, nil)
	if plan.Config.Speculation != "ngram" {
		t.Errorf("plan speculation = %q, want ngram", plan.Config.Speculation)
	}
}

// TestAssemblePlanCarriesVision asserts the install persists the vision decision
// the recommendation resolved, so the projector the install pulled and the unit it
// renders agree about whether this stack has vision.
func TestAssemblePlanCarriesVision(t *testing.T) {
	rec := recommend.Recommendation{Model: "m", Quant: "q", ContextLen: 4096, Backend: "rocm", Vision: true}
	plan := AssemblePlan(config.VillaConfig{}, Gates{}, rec, nil)
	if !plan.Config.Vision {
		t.Errorf("plan vision = false, want true")
	}
}
