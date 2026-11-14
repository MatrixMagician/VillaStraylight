package toolserver

import "testing"

// TestSetAccessorsFollowTheTable: the proof of the tool server reads each set's route, its
// operation id (the name its request log carries) and its Open WebUI connection id from here,
// so none of the three is a second spelling in another package.
func TestSetAccessorsFollowTheTable(t *testing.T) {
	cases := []struct {
		set                     Set
		path, operation, connID string
	}{
		{SetWeb, "/web/search", "villa_web_search", "villa-web"},
		{SetRecall, "/recall/search", "villa_recall_search", "villa-recall"},
	}
	for _, c := range cases {
		if got := SearchPath(c.set); got != c.path {
			t.Errorf("SearchPath(%s) = %q, want %q", c.set, got, c.path)
		}
		if got := OperationID(c.set); got != c.operation {
			t.Errorf("OperationID(%s) = %q, want %q", c.set, got, c.operation)
		}
		if got := ConnectionID(c.set); got != c.connID {
			t.Errorf("ConnectionID(%s) = %q, want %q", c.set, got, c.connID)
		}
		tools := toolsFor(c.set)
		if len(tools) != 1 || tools[0].Path != c.path || tools[0].OperationID != c.operation {
			t.Errorf("the table disagrees with the accessors for %s: %+v", c.set, tools)
		}
	}
	if SearchPath("nope") != "" || OperationID("nope") != "" || ConnectionID("nope") != "" {
		t.Error("an unknown set has no route, operation or connection")
	}
}
