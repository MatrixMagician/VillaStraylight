package main

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
)

// TestEveryRenderedContainerHasAStatusRow: status and doctor report a managed
// service's health only through a row in liveStatusServices, so a .container unit
// the stack renders without a row is a service nobody watches. The fixture turns
// the extractor on beside memory and web search (ADR-0029).
func TestEveryRenderedContainerHasAStatusRow(t *testing.T) {
	rows := map[string]bool{}
	for _, s := range liveStatusServices() {
		rows[s.Unit] = true
	}
	for _, u := range renderWithState(t, pinstate.State{}) {
		if !strings.HasSuffix(u.Name, ".container") {
			continue
		}
		if svc := unitServiceName(u.Name); !rows[svc] {
			t.Errorf("rendered unit %s has no status row for %s", u.Name, svc)
		}
	}
}
