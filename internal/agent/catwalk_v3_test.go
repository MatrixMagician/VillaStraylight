package agent

import (
	"encoding/json"
	"testing"
)

// catwalk_v3_test.go guards the Catwalk v2 -> v3 model-schema move: Crush v0.99+
// rewrites a v2 crush.json in place the first time it loads it, so villa renders
// v3 and reads a v2 file as the same document.

var legacyModelKeys = []string{
	"cost_per_1m_in", "cost_per_1m_out", "cost_per_1m_in_cached", "cost_per_1m_out_cached",
	"can_reason", "supports_attachments",
}

// firstModel returns providers.villa.models[0] of a decoded crush.json.
func firstModel(t *testing.T, doc []byte) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(doc, &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	models := v["providers"].(map[string]any)[providerKey].(map[string]any)["models"].([]any)
	return models[0].(map[string]any)
}

// legacyShape rewrites a v3 render into the shape villa rendered before the
// bump: the six legacy keys in place of pricing, capabilities and reasoning.
func legacyShape(t *testing.T, v3 []byte) []byte {
	t.Helper()
	return editDoc(t, v3, func(v map[string]any) {
		m := v["providers"].(map[string]any)[providerKey].(map[string]any)["models"].([]any)[0].(map[string]any)
		delete(m, "pricing")
		delete(m, "capabilities")
		delete(m, "reasoning")
		m["cost_per_1m_in"] = 0.0
		m["cost_per_1m_out"] = 0.0
		m["cost_per_1m_in_cached"] = 0.0
		m["cost_per_1m_out_cached"] = 0.0
		m["can_reason"] = false
		m["supports_attachments"] = false
	})
}

// crushWrote rewrites a render into the file Crush v0.101.0 wrote back after it
// migrated villa's v2 render: the model object is exactly that shape.
func crushWrote(t *testing.T, doc []byte) []byte {
	t.Helper()
	return editDoc(t, doc, func(v map[string]any) {
		m := v["providers"].(map[string]any)[providerKey].(map[string]any)["models"].([]any)[0].(map[string]any)
		var migrated map[string]any
		if err := json.Unmarshal([]byte(`{"capabilities":{"vision":false},"pricing":{"cache_create":0,"cache_hit":0,"input":0,"output":0},"reasoning":{"thinking":"never"}}`), &migrated); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		for _, k := range legacyModelKeys {
			delete(m, k)
		}
		for k, val := range migrated {
			m[k] = val
		}
	})
}

func driftOf(onDisk, rendered []byte) DriftReport {
	return DetectDrift(DriftInput{
		BinaryPresent: true, InstalledBinSHA: pinnedBinSHA, PolicyBinSHA: pinnedBinSHA,
		ConfigPresent: true, OnDiskConfig: onDisk, RenderedConfig: rendered,
	})
}

// TestRenderModelIsCatwalkV3 guards that the rendered model carries pricing,
// capabilities and reasoning and none of the six legacy keys, for both the
// coding-agent and the workspace-task render.
func TestRenderModelIsCatwalkV3(t *testing.T) {
	agentDoc, _, err := Render(renderTestConfig(), renderTestProbes())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	sandboxDoc, err := RenderSandbox(renderTestConfig(), "http://x/v1")
	if err != nil {
		t.Fatalf("RenderSandbox: %v", err)
	}
	for name, doc := range map[string][]byte{"Render": agentDoc, "RenderSandbox": sandboxDoc} {
		m := firstModel(t, doc)
		for _, k := range legacyModelKeys {
			if _, ok := m[k]; ok {
				t.Errorf("%s: model carries legacy key %q", name, k)
			}
		}
		pricing, _ := m["pricing"].(map[string]any)
		for _, k := range []string{"input", "output", "cache_create", "cache_hit"} {
			if v, ok := pricing[k]; !ok || v != 0.0 {
				t.Errorf("%s: pricing.%s = %v (present %v), want 0", name, k, v, ok)
			}
		}
		if c, _ := m["capabilities"].(map[string]any); c["vision"] != false {
			t.Errorf("%s: capabilities = %v, want vision false", name, m["capabilities"])
		}
		if r, _ := m["reasoning"].(map[string]any); r["thinking"] != "never" {
			t.Errorf("%s: reasoning = %v, want thinking never", name, m["reasoning"])
		}
	}
}

// TestLegacyRenderIsNotDrift guards that villa's pre-bump v2 render of the same
// config reads as unchanged: Crush migrates it on the next load, after which it
// equals the v3 render.
func TestLegacyRenderIsNotDrift(t *testing.T) {
	rendered, _, _ := Render(renderTestConfig(), renderTestProbes())
	legacy := legacyShape(t, rendered)
	if r := driftOf(legacy, rendered); r.ConfigDrift || r.ConfigVillaOwned {
		t.Errorf("legacy render: ConfigDrift=%v ConfigVillaOwned=%v, want clean", r.ConfigDrift, r.ConfigVillaOwned)
	}
}

// TestCrushMigratedFileIsNotDrift guards the blocker: the file Crush v0.101.0
// writes back after its first load must not be reported as drift by either
// DetectDrift or VillaOwnedDrift.
func TestCrushMigratedFileIsNotDrift(t *testing.T) {
	_, rendered := keyDriftDocs(t)
	migrated := crushWrote(t, rendered)
	if r := driftOf(migrated, rendered); r.ConfigDrift || r.ConfigVillaOwned {
		t.Errorf("migrated file: ConfigDrift=%v ConfigVillaOwned=%v, want clean", r.ConfigDrift, r.ConfigVillaOwned)
	}
	if VillaOwnedDrift(migrated, rendered) {
		t.Error("VillaOwnedDrift = true on the file Crush wrote, want false")
	}
}

// TestLegacyRenderWithStaleKeyIsVillaOwned guards ADR-0019 across the schema
// move: a v2 render whose only other difference is the villa-owned api_key is
// still healed, not refused.
func TestLegacyRenderWithStaleKeyIsVillaOwned(t *testing.T) {
	stale, rendered := keyDriftDocs(t)
	onDisk := legacyShape(t, stale)
	r := driftOf(onDisk, rendered)
	if !r.ConfigDrift || !r.ConfigVillaOwned {
		t.Errorf("legacy + stale key: ConfigDrift=%v ConfigVillaOwned=%v, want both true", r.ConfigDrift, r.ConfigVillaOwned)
	}
	if !VillaOwnedDrift(onDisk, rendered) {
		t.Error("VillaOwnedDrift = false on a legacy render with a stale key, want true")
	}
}

// TestOperatorEditIsStillDriftInEitherSchema guards AGENT-04: a hand edit of a
// v2 or a v3 file is ConfigDrift that is not villa-owned, so it is refused.
func TestOperatorEditIsStillDriftInEitherSchema(t *testing.T) {
	_, rendered := keyDriftDocs(t)
	changeCtx := func(v map[string]any) {
		m := v["providers"].(map[string]any)[providerKey].(map[string]any)["models"].([]any)[0].(map[string]any)
		m["context_window"] = 1234.0
	}
	addModel := func(v map[string]any) {
		p := v["providers"].(map[string]any)[providerKey].(map[string]any)
		p["models"] = append(p["models"].([]any), map[string]any{"id": "extra", "name": "extra", "supports_attachments": true})
	}
	cases := map[string][]byte{
		"v2 changed context_window": editDoc(t, legacyShape(t, rendered), changeCtx),
		"v3 changed context_window": editDoc(t, rendered, changeCtx),
		"v2 extra model":            editDoc(t, legacyShape(t, rendered), addModel),
		"v3 extra model":            editDoc(t, crushWrote(t, rendered), addModel),
	}
	for name, onDisk := range cases {
		t.Run(name, func(t *testing.T) {
			r := driftOf(onDisk, rendered)
			if !r.ConfigDrift || r.ConfigVillaOwned {
				t.Errorf("ConfigDrift=%v ConfigVillaOwned=%v, want drift that is not villa-owned", r.ConfigDrift, r.ConfigVillaOwned)
			}
			if VillaOwnedDrift(onDisk, rendered) {
				t.Error("VillaOwnedDrift = true on an operator edit, want false")
			}
		})
	}
}

// TestMigrateLegacyModels pins Crush's own v2 -> v3 mapping, case by case, so
// the comparison reads a v2 file the way Crush will.
func TestMigrateLegacyModels(t *testing.T) {
	run := func(model string) map[string]any {
		var doc any
		in := `{"providers":{"p":{"models":[` + model + `]}}}`
		if err := json.Unmarshal([]byte(in), &doc); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		migrateLegacyModels(doc)
		return doc.(map[string]any)["providers"].(map[string]any)["p"].(map[string]any)["models"].([]any)[0].(map[string]any)
	}
	canon := func(v any) string { b, _ := json.Marshal(v); return string(b) }

	t.Run("cost, attachments and can_reason false", func(t *testing.T) {
		m := run(`{"id":"a","cost_per_1m_in":1,"cost_per_1m_out":2,"cost_per_1m_in_cached":3,"cost_per_1m_out_cached":4,"supports_attachments":true,"can_reason":false}`)
		want := `{"capabilities":{"vision":true},"id":"a","pricing":{"cache_create":3,"cache_hit":4,"input":1,"output":2},"reasoning":{"thinking":"never"}}`
		if got := canon(m); got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})
	t.Run("can_reason true is toggleable", func(t *testing.T) {
		m := run(`{"id":"a","can_reason":true}`)
		if got := canon(m["reasoning"]); got != `{"thinking":"toggleable"}` {
			t.Errorf("reasoning = %s", got)
		}
	})
	t.Run("an existing pricing and vision win; legacy keys still go", func(t *testing.T) {
		m := run(`{"id":"a","pricing":{"input":9},"cost_per_1m_in":1,"capabilities":{"vision":false},"supports_attachments":true}`)
		want := `{"capabilities":{"vision":false},"id":"a","pricing":{"input":9}}`
		if got := canon(m); got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})
	t.Run("a v3 model is untouched", func(t *testing.T) {
		in := `{"capabilities":{"vision":true},"id":"a","pricing":{"input":1},"reasoning":{"thinking":"never"}}`
		if got := canon(run(in)); got != in {
			t.Errorf("got %s, want %s", got, in)
		}
	})
	t.Run("no reasoning key and no legacy reasoning keys adds none", func(t *testing.T) {
		m := run(`{"id":"a"}`)
		if _, ok := m["reasoning"]; ok {
			t.Errorf("reasoning added to %v", m)
		}
	})
}
