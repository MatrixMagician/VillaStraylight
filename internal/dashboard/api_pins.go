package dashboard

// api_pins.go is the Update·Pins panel's read-model and handler. The handler adds
// no pin logic of its own — it folds the injected Pins seam exactly as
// handleModels folds Models — because pinresolve.Resolver.All() and the
// pinstate.Load I/O behind it belong to the live wiring (cmd/villa), never to this
// package (the pure-core/injectable-seam split this repo holds everywhere else).

import "net/http"

// PinsView is the Update·Pins read-model: every pinned component's vetted-vs-
// effective answer, plus the compiled-in manifest serial.
type PinsView struct {
	Serial     uint64   `json:"serial"`
	Components []PinRow `json:"components"`
}

// PinRow is one component's vetted-vs-effective answer (mirrors
// pinresolve.Resolved, narrowed to what the panel renders).
type PinRow struct {
	Component string `json:"component"`
	Subsystem string `json:"subsystem"`
	Vetted    string `json:"vetted"`
	Effective string `json:"effective"`
	FromStore bool   `json:"from_store"`
	Diverged  bool   `json:"diverged"`
}

// handlePins serves GET /api/pins by folding the injected Pins seam. There is no
// availability flag on PinsView: the compiled-in table can never be absent, so an
// unreadable pin-state store still resolves every row to its vetted pin with
// FromStore false — the live wiring's honest answer, not a failure this handler
// needs to detect.
func (s *Server) handlePins(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.pins())
}
