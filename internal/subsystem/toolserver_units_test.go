package subsystem

import (
	"reflect"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// TestToolServerUnitsFollowTheirDerivedGates: the registry declares both units in
// order, and Units answers each unit's own gate so a stack apply stops and removes
// exactly the one whose prerequisite went off (ADR-0035).
func TestToolServerUnitsFollowTheirDerivedGates(t *testing.T) {
	every, svcs := ToolServer.EveryUnit()
	if want := []string{"villa-tools-web.container", "villa-tools-recall.container"}; !reflect.DeepEqual(every, want) {
		t.Errorf("ToolServer.EveryUnit() = %v, want %v", every, want)
	}
	if want := []string{"villa-tools-web.service", "villa-tools-recall.service"}; !reflect.DeepEqual(svcs, want) {
		t.Errorf("services = %v, want %v", svcs, want)
	}
	cases := []struct {
		name string
		cfg  config.VillaConfig
		want []string
	}{
		{"gate off", config.VillaConfig{WebSearchEnabled: true, MemoryEnabled: true}, nil},
		{"web only", config.VillaConfig{ToolServer: true, WebSearchEnabled: true}, []string{"villa-tools-web.container"}},
		{"recall only", config.VillaConfig{ToolServer: true, MemoryEnabled: true}, []string{"villa-tools-recall.container"}},
		{"both", config.VillaConfig{ToolServer: true, WebSearchEnabled: true, MemoryEnabled: true}, every},
	}
	for _, tc := range cases {
		got, _ := ToolServer.Units(tc.cfg)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: Units = %v, want %v", tc.name, got, tc.want)
		}
	}
	if ToolServer.AlwaysOn() || ToolServer.String() != "tool server" {
		t.Errorf("ToolServer is a gated addon named %q", ToolServer.String())
	}
	if ToolServer.OwnsPersistentState() {
		t.Error("the tool server owns no state volume; its scope file is derived from recall-state.json")
	}
}
