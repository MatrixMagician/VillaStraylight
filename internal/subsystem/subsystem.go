// Package subsystem names every part of the stack, and answers one question about
// each: is this subsystem on?
//
// Four subsystems are OPTIONAL — the memory stack, web search, the coding agent,
// and coding mode. Each is gated by a bool in config, and each bool was read
// directly in dozens of places across more than twenty files — the install path
// alone read the memory flag eleven times. An authoritative decision module existed
// for memory and had two callers, so it failed the deletion test: deleting it would
// have changed almost nothing, because nearly every read site bypassed it.
//
// Two are ALWAYS ON — inference and chat. They carry no config gate because the
// stack is not the stack without them. They are named here anyway so that code
// which needs to key something by subsystem (the pin table, `villa update`) has one
// vocabulary for the concept rather than inventing a second one for the two the
// gates happened not to cover.
//
// This package is where a gate is answered. Understanding one no longer requires a
// tour of eleven files.
//
// # The second question: does this subsystem own persistent state?
//
// Enablement is not the only fact that belongs to a subsystem rather than to a
// caller. `villa update` used to treat every subsystem as though its IMAGE were the
// state being changed — true for the backends, SearXNG and the websafe base, false
// for chat and memory, which own a mutable volume and whose real state is the data.
// A live update migrated Open WebUI's schema forward and the retained image could
// not roll it back.
//
// So OwnsPersistentState / StateVolume live here, beside the gates, for the same
// reason the gates do: it is a property of the subsystem, and a hardcoded pair in
// the update path would go stale the moment a sixth subsystem gained a volume.
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

// Kind names one subsystem. It exists so the gates can be enumerated — status,
// doctor and install all walk the same optional four — rather than each caller
// hard-coding its own list and drifting when a fifth arrives.
//
// Members are iota-numbered, so the always-on pair is APPENDED rather than
// inserted at the head where it reads more naturally. Inserting would silently
// renumber every existing value, and a Kind is a map key in the pin state store.
// Ordering that reads well is not worth a renumber.
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
	// Inference is llama.cpp serving the chat model on the inference backend. It is
	// always on: a stack without it serves nothing.
	Inference
	// Chat is the Open WebUI chat surface. It is always on: `villa install` renders
	// its unit unconditionally.
	Chat
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
	case Inference:
		return "inference"
	case Chat:
		return "chat"
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
	case Inference, Chat:
		// DELIBERATE: an always-on subsystem has no config key, because there is no
		// flag an operator could edit to change the answer. The empty string is the
		// honest return, and callers rendering remediation must treat it as "there is
		// nothing to edit" rather than as a missing case.
		return ""
	}
	return ""
}

// AlwaysOn reports whether this subsystem has no gate at all.
//
// It exists so a caller can tell "on because the operator enabled it" from "on
// because it cannot be off" without re-deriving the list. A reporter that prints
// "enabled" beside inference is technically true and useless.
func (k Kind) AlwaysOn() bool {
	switch k {
	case Inference, Chat:
		return true
	case Memory, WebSearch, Agent, CodingMode:
		return false
	}
	return false
}

// stateVolumes is the podman NAMED volume each stateful subsystem owns.
//
// A subsystem is in this map when the state being changed is its DATA rather than
// its image. That distinction was invisible until an update proved it: a real
// `villa update chat` migrated Open WebUI's SQLite config table forward, and the
// retained image could not roll that back, because the image was never the thing
// that changed.
//
// MUTABLE OWNED STATE ONLY, which is narrower than "mounts a volume". villa-embed
// and villa-llama mount the models volume READ-ONLY and villa-websafe bind-mounts
// the villa binary read-only; none of those is state the subsystem owns, none can
// be migrated forward by an image bump, and model weights are explicitly outside
// what `update` touches. A read-only mount must never appear here.
//
// The names are re-declared rather than read from internal/orchestrate because
// orchestrate imports THIS package — the dependency only runs one way. They are
// bound to the rendered reality by a cross-package drift test in
// internal/orchestrate, the same shape as the one that binds the embed GGUF
// filename, so the declaration cannot quietly disagree with what is mounted.
var stateVolumes = map[Kind]string{
	Chat:   "villa-openwebui",
	Memory: "villa-qdrant",
}

// StateVolume reports the podman named volume this subsystem's persistent state
// lives in, and whether it owns any at all.
//
// The two returns come together on purpose: a caller cannot read a volume name
// without having been told whether there is one, so "snapshot the empty string"
// is not a reachable mistake. It is a property of the subsystem in the same way
// ConfigKey is, so a caller binds a symbol rather than re-typing a volume name.
func (k Kind) StateVolume() (name string, owns bool) {
	v, ok := stateVolumes[k]
	return v, ok
}

// OwnsPersistentState reports whether the state this subsystem changes lives in a
// volume rather than in its image.
//
// It is the question the update lifecycle asks: a subsystem that owns persistent
// state needs its data captured before it is mutated, because restoring the prior
// digest onto migrated data restores something that was never proven.
func (k Kind) OwnsPersistentState() bool {
	_, owns := k.StateVolume()
	return owns
}

// Stateful is every subsystem that owns persistent state, in Every order.
//
// It exists so a caller walks the declaration rather than hardcoding the pair —
// which is precisely what would go stale the moment a sixth subsystem gains a
// volume, silently and with no compile error to catch it.
func Stateful() []Kind {
	var out []Kind
	for _, k := range Every {
		if k.OwnsPersistentState() {
			out = append(out, k)
		}
	}
	return out
}

// All is every OPTIONAL subsystem, in the order the stack reports them. A caller
// that needs to walk the gates ranges over this rather than writing its own list.
//
// DELIBERATE: All was NOT widened to include the always-on pair. Enabled walks it,
// and every existing caller of Enabled asks "which addons did the operator turn
// on?" — a question whose honest answer never includes inference. Widening it would
// have made status, doctor and install each report two subsystems nobody enabled,
// silently, with no compile error to catch it. Every is the all-subsystems list.
var All = []Kind{Memory, WebSearch, Agent, CodingMode}

// Every is every subsystem, optional and always-on alike, in stack order:
// inference first because nothing runs without it, then chat, then the addons.
//
// This is the list a caller walks when it needs to name subsystems rather than to
// ask which are enabled — the pin table keys its entries by this vocabulary. The
// order here is presentation order and is deliberately NOT the iota order, which
// exists only to keep stored values stable.
var Every = []Kind{Inference, Chat, Memory, WebSearch, Agent, CodingMode}

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
	case Inference, Chat:
		// TRUE BY CONSTRUCTION, not a stub. Inference and chat have no config bool
		// because `villa install` renders both units unconditionally: there is no
		// reachable state in which the stack is installed and either is off. If one
		// ever becomes optional it gains a config field like every other gate, and
		// this case moves up to join them.
		return true
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
