package agent

// catwalk_v3.go is the read side of Crush's Catwalk v2 -> v3 model migration.
// Crush v0.99+ rewrites a crush.json written in the v2 model schema the first
// time it loads it (upstream internal/config/load.go, migrateCatwalkV2Model), so
// a file villa rendered before that bump stops matching villa's render after one
// `crush run`. Villa now renders v3; the drift comparison applies Crush's own
// mapping to the on-disk file so a v2 render reads as unchanged while an operator
// edit still reads as drift. The invariant: this runs on the comparison path only
// and never shapes what villa writes.

// legacyPricingFields maps a v2 cost key to its v3 pricing key.
var legacyPricingFields = map[string]string{
	"cost_per_1m_in":         "input",
	"cost_per_1m_out":        "output",
	"cost_per_1m_in_cached":  "cache_create",
	"cost_per_1m_out_cached": "cache_hit",
}

// migrateLegacyModels rewrites, in place, every providers.*.models[] object of
// a decoded crush.json from the v2 model fields to v3, with Crush's semantics:
//
//   - the cost keys fold into pricing only when pricing is absent, and are
//     deleted either way;
//   - supports_attachments becomes capabilities.vision unless that is set, and
//     is deleted;
//   - can_reason, reasoning_levels and default_reasoning_effort are deleted, and
//     when reasoning is absent and any was present, can_reason=false gives
//     {"thinking":"never"} and anything else {"thinking":"toggleable"}.
//
// Crush also builds effort_levels from a reasoning_levels list. Villa never
// renders one, so a file that has it differs from the render either way and is
// left reading as an operator edit. A model already in v3 is untouched.
func migrateLegacyModels(doc any) {
	root, _ := doc.(map[string]any)
	providers, _ := root["providers"].(map[string]any)
	for _, raw := range providers {
		provider, _ := raw.(map[string]any)
		for _, key := range []string{"models", "chatgpt_models"} {
			models, _ := provider[key].([]any)
			for _, rawModel := range models {
				if model, ok := rawModel.(map[string]any); ok {
					migrateLegacyModel(model)
				}
			}
		}
	}
}

func migrateLegacyModel(model map[string]any) {
	_, hasPricing := model["pricing"]
	pricing := map[string]any{}
	for legacy, field := range legacyPricingFields {
		if v, ok := model[legacy]; ok {
			pricing[field] = v
			delete(model, legacy)
		}
	}
	if !hasPricing && len(pricing) > 0 {
		model["pricing"] = pricing
	}

	if v, ok := model["supports_attachments"]; ok {
		delete(model, "supports_attachments")
		caps, ok := model["capabilities"].(map[string]any)
		if !ok {
			caps = map[string]any{}
			model["capabilities"] = caps
		}
		if _, set := caps["vision"]; !set {
			caps["vision"] = v
		}
	}

	canReason, hasCan := model["can_reason"]
	_, hasLevels := model["reasoning_levels"]
	_, hasEffort := model["default_reasoning_effort"]
	delete(model, "can_reason")
	delete(model, "reasoning_levels")
	delete(model, "default_reasoning_effort")
	if _, ok := model["reasoning"]; !ok && (hasCan || hasLevels || hasEffort) {
		thinking := "toggleable"
		if can, isBool := canReason.(bool); isBool && !can {
			thinking = thinkingNever
		}
		model["reasoning"] = map[string]any{"thinking": thinking}
	}
}
