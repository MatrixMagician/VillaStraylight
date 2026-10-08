// Tests for the reservation registry (ADR-0027): which rows the gates produce,
// how each row is sized, that Pick subtracts every row it is handed, and the
// append-only shape of the schema 9 contract.
package recommend

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// TestReservationsForFollowsTheGates guards ADR-0027: the registry holds one row
// per service whose gate is on, in registry order, and nothing for a service that
// is off. An off service that still reserved would shrink every fit for nothing.
func TestReservationsForFollowsTheGates(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.VillaConfig
		want []string
	}{
		{"everything off", config.VillaConfig{}, nil},
		{"memory on", config.VillaConfig{MemoryEnabled: true, EmbeddingModel: "nomic-embed-text-v1.5"}, []string{"embedding"}},
		{"web search on", config.VillaConfig{WebSearchEnabled: true}, []string{"web_search"}},
		{"both on, embedding first", config.VillaConfig{MemoryEnabled: true, WebSearchEnabled: true}, []string{"embedding", "web_search"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, r := range ReservationsFor(tc.cfg) {
				got = append(got, r.Name)
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("rows = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestReservationRowsCarryTheirNameAndSize guards that each row is sized
// by its own source: the embedding row by the pinned footprint, the web-search
// row by the injection budget at the configured result count.
func TestReservationRowsCarryTheirNameAndSize(t *testing.T) {
	res := ReservationsFor(config.VillaConfig{
		MemoryEnabled: true, EmbeddingModel: "nomic-embed-text-v1.5",
		WebSearchEnabled: true, WebSearchResultCount: 3,
	})
	if len(res) != 2 {
		t.Fatalf("got %d rows, want 2", len(res))
	}
	if res[0].Name != "embedding" || res[0].Bytes != 536870912 {
		t.Errorf("embedding row = %+v, want embedding / 536870912", res[0])
	}
	wantWeb, _ := webSearchReservation(webSearchInputs{ResultCount: 3})
	if res[1].Name != "web_search" || res[1].Bytes != wantWeb {
		t.Errorf("web-search row = %+v, want web_search / %d", res[1], wantWeb)
	}
}

// TestPickSubtractsEveryReservation is #307's acceptance test: a row the
// registry has never seen shrinks the chat-model envelope by exactly its bytes,
// with no change to Pick. That is what lets a new service land as one row.
func TestPickSubtractsEveryReservation(t *testing.T) {
	const env = uint64(64 << 30)
	cat := testCatalog()
	base := Pick(profileWithEnvelope(env), cat, Overrides{}, nil)

	extra := Reservation{Name: "test", Bytes: 3 << 30, Notes: []string{"RESERVED for test"}}
	res := append(ReservationsFor(config.VillaConfig{MemoryEnabled: true, EmbeddingModel: "nomic-embed-text-v1.5"}), extra)
	rec := Pick(profileWithEnvelope(env), cat, Overrides{}, res)

	if want := base.UsableEnvelopeBytes - 536870912 - 3<<30; rec.UsableEnvelopeBytes != want {
		t.Errorf("UsableEnvelopeBytes = %d, want %d (every row subtracted)", rec.UsableEnvelopeBytes, want)
	}
	if !hasNote(rec.Notes, "RESERVED for test") {
		t.Errorf("a row's notes must reach the pick, got %v", rec.Notes)
	}
	if rec.EmbeddingReservationBytes != 536870912 || !rec.MemoryConsidered {
		t.Errorf("legacy embedding fields = %d / %v, want 536870912 / true", rec.EmbeddingReservationBytes, rec.MemoryConsidered)
	}
	if rec.WebSearchReservationBytes != 0 {
		t.Errorf("WebSearchReservationBytes = %d, want 0 with no web-search row", rec.WebSearchReservationBytes)
	}
}

// TestReservationsJSONIsAppendOnly guards the v9 contract: the reservations
// array is always present, carries name and bytes only, and the schema is 9.
func TestReservationsJSONIsAppendOnly(t *testing.T) {
	cat := testCatalog()
	off := Pick(profileWithEnvelope(64<<30), cat, Overrides{}, nil)
	on := Pick(profileWithEnvelope(64<<30), cat, Overrides{}, ReservationsFor(config.VillaConfig{WebSearchEnabled: true}))

	offJSON, _ := json.Marshal(off)
	if !strings.Contains(string(offJSON), `"reservations":[]`) {
		t.Errorf("an empty registry must marshal as [], got %s", offJSON)
	}
	if off.SchemaVersion != 9 {
		t.Errorf("SchemaVersion = %d, want 9 (the reservations array bump)", off.SchemaVersion)
	}
	onJSON, _ := json.Marshal(on.Reservations)
	want := `[{"name":"web_search","bytes":` + jsonUint(on.WebSearchReservationBytes) + `}]`
	if string(onJSON) != want {
		t.Errorf("reservations = %s, want %s", onJSON, want)
	}
}

func jsonUint(v uint64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// memOn is the registry's rows with memory on and nothing else.
func memOn(model string) []Reservation {
	return ReservationsFor(config.VillaConfig{MemoryEnabled: true, EmbeddingModel: model})
}

// webOn is a web-search row sized from explicit tuning, which the config cannot
// carry (TopK and ChunkSizeChars have no config field).
func webOn(web webSearchInputs) []Reservation {
	bytes, notes := webSearchReservation(web)
	return []Reservation{{Name: reservationWebSearch, Bytes: bytes, Notes: notes}}
}

// TestReservedBytesSaturates guards the total every consumer reads: rows that sum
// past 2^64 give the maximum, never a wrapped small number that would let the
// install floor pass a stack Pick refused.
func TestReservedBytesSaturates(t *testing.T) {
	rec := Recommendation{Reservations: []Reservation{{Bytes: math.MaxUint64 - 1}, {Bytes: 2}}}
	if got := rec.ReservedBytes(); got != math.MaxUint64 {
		t.Errorf("ReservedBytes = %d, want MaxUint64", got)
	}
	rec.Reservations = []Reservation{{Bytes: 3}, {Bytes: 4}}
	if got := rec.ReservedBytes(); got != 7 {
		t.Errorf("ReservedBytes = %d, want 7", got)
	}
}
