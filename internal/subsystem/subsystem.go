// Package subsystem answers one question for each optional part of the stack: is
// this subsystem on?
//
// The four optional subsystems are the memory stack, web search, the coding agent,
// and coding mode. Each is gated by a bool in config, and each bool was read
// directly in dozens of places across more than twenty files — the install path
// alone read the memory flag eleven times. An authoritative decision module existed
// for memory and had two callers, so it failed the deletion test: deleting it would
// have changed almost nothing, because nearly every read site bypassed it.
//
// This package is where a gate is answered. Understanding one no longer requires a
// tour of eleven files.
//
// # What a gate is, and is not
//
// A gate answers "is this subsystem on", nothing more. It is NOT the validity check
// (memory.Decide still owns whether the memory fields are coherent), and it is NOT
// the readiness check (a subsystem can be enabled and not running). Keeping those
// separate is what lets a caller say "enabled but misconfigured" or "enabled but
// down" rather than collapsing three different situations into one boolean.
//
// # Why enablement is a value, not a probe
//
// Every gate here is a pure function of a config that the caller already loaded.
// That is deliberate: the gates used to be answered both from a loaded cfg AND from
// seams that re-read config.toml on the spot, so a single command could observe two
// different answers for the same subsystem within one run. One loaded config, read
// through these gates, cannot disagree with itself.
//
// PURE: no I/O, no os/exec, no container-image literal, so the seam grep gate stays
// green over this package.
package subsystem

import "github.com/MatrixMagician/VillaStraylight/internal/config"

// Kind names one optional subsystem. It exists so the gates can be enumerated —
// status, doctor and install all walk the same four — rather than each caller
// hard-coding its own list and drifting when a fifth arrives.
type Kind int

const (
	// Memory is the vector-memory stack: Qdrant plus the embedder.
	Memory Kind = iota
	// WebSearch is the SearXNG metasearch service plus the SSRF-guarded loader.
	WebSearch
	// Agent is the local coding-agent addon.
	Agent
	// CodingMode is the running stack flipped to a tool-calling configuration.
	CodingMode
)

// String renders the subsystem's name for messages and logs.
func (k Kind) String() string {
	switch k {
	case Memory:
		return "memory"
	case WebSearch:
		return "web search"
	case Agent:
		return "coding agent"
	case CodingMode:
		return "coding mode"
	}
	return "unknown"
}

// ConfigKey is the config.toml key that gates this subsystem, for remediation text
// that tells the operator exactly what to edit.
func (k Kind) ConfigKey() string {
	switch k {
	case Memory:
		return "memory_enabled"
	case WebSearch:
		return "web_search_enabled"
	case Agent:
		return "agent_enabled"
	case CodingMode:
		return "coding_mode"
	}
	return ""
}

// All is every optional subsystem, in the order the stack reports them. A caller
// that needs to walk the gates ranges over this rather than writing its own list.
var All = []Kind{Memory, WebSearch, Agent, CodingMode}

// On reports whether the named subsystem is enabled in this config.
//
// This is THE read. A caller that reaches for the config field directly is asserting
// it knows better than the gate, which is how the flag ended up read in more than
// twenty files.
func On(cfg config.VillaConfig, k Kind) bool {
	switch k {
	case Memory:
		return cfg.MemoryEnabled
	case WebSearch:
		return cfg.WebSearchEnabled
	case Agent:
		return cfg.AgentEnabled
	case CodingMode:
		return cfg.CodingMode
	}
	return false
}

// MemoryOn reports whether the memory stack is enabled.
//
// The four named accessors exist alongside On because most call sites ask about one
// specific subsystem, and `subsystem.MemoryOn(cfg)` reads better at a branch than
// `subsystem.On(cfg, subsystem.Memory)`. They are the same answer.
func MemoryOn(cfg config.VillaConfig) bool { return On(cfg, Memory) }

// WebSearchOn reports whether the web-search stack is enabled.
func WebSearchOn(cfg config.VillaConfig) bool { return On(cfg, WebSearch) }

// AgentOn reports whether the coding-agent addon is enabled.
func AgentOn(cfg config.VillaConfig) bool { return On(cfg, Agent) }

// CodingModeOn reports whether coding mode is engaged.
func CodingModeOn(cfg config.VillaConfig) bool { return On(cfg, CodingMode) }

// Enabled returns every subsystem that is on, in All order. It is what a reporter
// walks instead of writing four branches.
func Enabled(cfg config.VillaConfig) []Kind {
	var on []Kind
	for _, k := range All {
		if On(cfg, k) {
			on = append(on, k)
		}
	}
	return on
}
