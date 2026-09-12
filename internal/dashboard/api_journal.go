package dashboard

// api_journal.go is the Journal panel's read-model, handler, and the pure
// reduction of `journalctl -o json` text into that read-model. ParseJournalJSON is
// exported so the live wiring (cmd/villa) can compose it with the
// orchestrate.Systemd.JournalTail seam without a second implementation of the
// reduction — the handler itself does no parsing, only folding the injected
// Journal seam (the same split handlePins and handleModels already hold).

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// JournalView is the Journal panel read-model. Available is false when
// journalctl is missing, produced nothing, or produced nothing this reduction
// could parse — the panel then renders "unavailable", never an empty console
// pretending the stack is quiet.
type JournalView struct {
	Available bool          `json:"available"`
	Lines     []JournalLine `json:"lines"`
}

// JournalLine is one journal record, already reduced to what the console renders.
type JournalLine struct {
	At      string `json:"at"`
	Unit    string `json:"unit"`
	Message string `json:"message"`
}

// journalRecord is one line of `journalctl -o json` output, narrowed to the three
// fields the panel needs. Message is left as raw JSON rather than a string: journald
// encodes a non-UTF-8 MESSAGE as an array of byte numbers instead of a string, and
// that shape must be detected before it is unmarshaled, not after.
type journalRecord struct {
	RealTime string          `json:"__REALTIME_TIMESTAMP"`
	Unit     string          `json:"_SYSTEMD_USER_UNIT"`
	Message  json.RawMessage `json:"MESSAGE"`
}

// ParseJournalJSON reduces `journalctl -o json` output (one JSON object per line)
// to the Journal panel's read-model: oldest first, capped at max lines. A line that
// fails to parse, or whose MESSAGE is not a string (journald's non-UTF-8 encoding),
// is dropped rather than failing the whole view — one bad record must not blank the
// panel. An input with no parseable line yields the zero (unavailable) view.
func ParseJournalJSON(text string, limit int) JournalView {
	var lines []JournalLine
	for _, raw := range strings.Split(text, "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		var rec journalRecord
		if err := json.Unmarshal([]byte(raw), &rec); err != nil {
			continue
		}
		var msg string
		if err := json.Unmarshal(rec.Message, &msg); err != nil {
			continue // byte-array MESSAGE (non-UTF-8) or otherwise malformed
		}
		lines = append(lines, JournalLine{
			At:      formatJournalTime(rec.RealTime),
			Unit:    strings.TrimSuffix(rec.Unit, ".service"),
			Message: msg,
		})
	}
	if len(lines) == 0 {
		return JournalView{}
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:] // keep the most recent limit, oldest-first order intact
	}
	return JournalView{Available: true, Lines: lines}
}

// formatJournalTime converts journald's __REALTIME_TIMESTAMP (microseconds since
// the epoch, encoded as a string) to the local "HH:MM:SS" the panel renders. An
// unparseable timestamp yields "" rather than dropping the record: the unit and
// message are still worth showing.
func formatJournalTime(s string) string {
	us, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return ""
	}
	return time.UnixMicro(us).Local().Format("15:04:05")
}

// handleJournal serves GET /api/journal by folding the injected Journal seam,
// which already carries the full JournalTail + ParseJournalJSON reduction done by
// the live wiring.
func (s *Server) handleJournal(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.journal())
}
