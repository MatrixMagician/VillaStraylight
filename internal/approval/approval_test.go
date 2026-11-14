package approval

import "testing"

// TestDecideTable guards every Action x Mode cell against spec sec3.3's table:
// ask mode allows read/list and asks write/execute; auto mode allows
// read/list/write and asks execute (AutoAllowed/safe-list cases are covered
// separately); fetch/download deny in both modes.
func TestDecideTable(t *testing.T) {
	cases := []struct {
		action Action
		mode   Mode
		want   Decision
	}{
		{Read, ModeAsk, Allow},
		{List, ModeAsk, Allow},
		{Write, ModeAsk, Ask},
		{Execute, ModeAsk, Ask},
		{Fetch, ModeAsk, Deny},
		{Download, ModeAsk, Deny},

		{Read, ModeAuto, Allow},
		{List, ModeAuto, Allow},
		{Write, ModeAuto, Allow},
		{Execute, ModeAuto, Ask},
		{Fetch, ModeAuto, Deny},
		{Download, ModeAuto, Deny},
	}

	for _, c := range cases {
		got := Decide(c.mode, Untainted, Request{Action: c.action})
		if got != c.want {
			t.Errorf("Decide(%s, %s) = %v, want %v", c.mode, c.action, got, c.want)
		}
	}
}

// TestDecideExecuteAutoAllowlist checks the auto-mode execute path resolves
// through AutoAllowed/the safe list before falling back to Ask.
func TestDecideExecuteAutoAllowlist(t *testing.T) {
	cases := []struct {
		name string
		req  Request
		want Decision
	}{
		{"villa allowlist command inside workspace", Request{Action: Execute, Command: "python3 script.py", Workspace: "/workspace"}, Allow},
		{"crush safe command", Request{Action: Execute, Command: "git status", Workspace: "/workspace"}, Allow},
		{"unlisted command", Request{Action: Execute, Command: "curl https://example.com", Workspace: "/workspace"}, Ask},
		{"deletion always asks even if it looks safe otherwise", Request{Action: Execute, Command: "rm -rf /workspace/x", Workspace: "/workspace"}, Ask},
	}

	for _, c := range cases {
		got := Decide(ModeAuto, Untainted, c.req)
		if got != c.want {
			t.Errorf("%s: Decide(auto, %+v) = %v, want %v", c.name, c.req, got, c.want)
		}
	}
}

// TestDecideDeletionAsksInBothModes guards the load-bearing exception: a
// deletion command asks even under auto mode, which otherwise allows execute
// freely for allowlisted/safe commands.
func TestDecideDeletionAsksInBothModes(t *testing.T) {
	req := Request{Action: Execute, Command: "rm -rf /workspace/x", Workspace: "/workspace"}
	if got := Decide(ModeAsk, Untainted, req); got != Ask {
		t.Errorf("Decide(ask, deletion) = %v, want Ask", got)
	}
	if got := Decide(ModeAuto, Untainted, req); got != Ask {
		t.Errorf("Decide(auto, deletion) = %v, want Ask", got)
	}
}

func TestIsDeletionMatches(t *testing.T) {
	commands := []string{
		"rm -rf x",
		"find . -name '*.tmp' -delete",
		"git clean -fdx",
		"ls && rm x",
		"cat a | xargs rm",
		"sudo rm x",
		"trash x",
		"timeout 60 rm -rf x",
		"nice rm -rf x",
		"nohup rm -rf x",
		"ls -la\nrm -rf x",
		"ls & rm -rf x",
	}
	for _, cmd := range commands {
		if !IsDeletion(cmd) {
			t.Errorf("IsDeletion(%q) = false, want true", cmd)
		}
	}
}

func TestIsDeletionDoesNotMatch(t *testing.T) {
	commands := []string{
		"rmdir_helper.py",
		"echo rm",
		"grep rm file",
		"python3 remove_dupes.py",
	}
	for _, cmd := range commands {
		if IsDeletion(cmd) {
			t.Errorf("IsDeletion(%q) = true, want false", cmd)
		}
	}
}

func TestAutoAllowed(t *testing.T) {
	cases := []struct {
		command   string
		workspace string
		want      bool
	}{
		{"mv /workspace/a /workspace/b", "/workspace", true},
		{"mv /workspace/a /tmp/b", "/workspace", false},
		{"cp ../x y", "/workspace", false},
		{"python3 script.py", "/workspace", true},
		{"soffice --convert-to pdf /workspace/x.docx", "/workspace", true},
		{"curl https://example.com", "/workspace", false},
		// GHSA-mmp6: python3 -c takes inline code, not a path. The old check
		// treated the code string's first non-dash word as a workspace-
		// relative path, which trivially "resolved" inside the workspace and
		// auto-allowed arbitrary code (including deletions the pattern-based
		// IsDeletion check cannot see, e.g. shutil.rmtree).
		{`python3 -c "import shutil;shutil.rmtree('src')"`, "/workspace", false},
	}

	for _, c := range cases {
		got := AutoAllowed(c.command, c.workspace)
		if got != c.want {
			t.Errorf("AutoAllowed(%q, %q) = %v, want %v", c.command, c.workspace, got, c.want)
		}
	}
}

// TestDecideUnknownActionAsks guards GHSA-fpjc: Decision's zero value must
// not be Allow, and a lookup miss on either table level must fall back to
// Ask rather than the map's zero value. A new or renamed Crush tool, an MCP
// tool, or a case change must never be silently auto-allowed.
func TestDecideUnknownActionAsks(t *testing.T) {
	unknownActions := []Action{"", "edit", "mcp", "Write"}
	for _, mode := range []Mode{ModeAsk, ModeAuto} {
		for _, action := range unknownActions {
			got := Decide(mode, Untainted, Request{Action: action})
			if got != Ask {
				t.Errorf("Decide(%s, %q) = %v, want Ask", mode, action, got)
			}
		}
	}
}

// TestDecideAutoModeNeverBypassesDeletion turns every bypass command
// GHSA-mmp6 listed into a table case: a safe-list wrapper (timeout, nice,
// nohup), a newline- or single-&-chained command, and inline code that
// AutoAllowed mistook for a path must all still ask in auto mode.
func TestDecideAutoModeNeverBypassesDeletion(t *testing.T) {
	commands := []string{
		"timeout 60 rm -rf src",
		"nice rm -rf src",
		"nohup rm -rf src",
		"ls -la\nrm -rf src",
		"ls & rm -rf src",
		`python3 -c "import shutil;shutil.rmtree('src')"`,
	}
	for _, cmd := range commands {
		got := Decide(ModeAuto, Untainted, Request{Action: Execute, Command: cmd, Workspace: "/workspace"})
		if got != Ask {
			t.Errorf("Decide(auto, %q) = %v, want Ask", cmd, got)
		}
	}
}

// allCells is every request shape that reaches a distinct branch of Decide
// (each Action, the allowlist, the safe list, a deletion).
func allCells() []Request {
	return []Request{
		{Action: Read}, {Action: List}, {Action: Write}, {Action: Fetch}, {Action: Download},
		{Action: Action("unknown")},
		{Action: Execute, Command: "unlisted-tool"},
		{Action: Execute, Command: "python3 script.py", Workspace: "/workspace"},
		{Action: Execute, Command: "git status", Workspace: "/workspace"},
		{Action: Execute, Command: "rm -rf /workspace/x", Workspace: "/workspace"},
	}
}

// TestTaintedNeverMorePermissive guards the one-way property of the taint: for
// every cell and every taint, the tainted answer is never more permissive than
// the untainted one (Allow < Ask < Deny in restrictiveness).
func TestTaintedNeverMorePermissive(t *testing.T) {
	rank := map[Decision]int{Allow: 0, Ask: 1, Deny: 2}
	for _, mode := range []Mode{ModeAsk, ModeAuto} {
		for _, req := range allCells() {
			base := Decide(mode, Untainted, req)
			for _, taint := range []Taint{TaintWeb, Taint("unrecognised")} {
				got := Decide(mode, taint, req)
				if rank[got] < rank[base] {
					t.Errorf("Decide(%s, %q, %+v) = %v, more permissive than untainted %v", mode, taint, req, got, base)
				}
			}
		}
	}
}

// TestTaintedWriteAndExecuteAsk guards the decision of ADR-0040: a tainted
// task's write and execute actions ask, under --auto and for an allowlisted or
// safe-listed command too; read and list stay allowed, and deny stays deny.
func TestTaintedWriteAndExecuteAsk(t *testing.T) {
	cases := []struct {
		name string
		mode Mode
		req  Request
		want Decision
	}{
		{"write under auto", ModeAuto, Request{Action: Write}, Ask},
		{"write under ask", ModeAsk, Request{Action: Write}, Ask},
		{"allowlisted execute under auto", ModeAuto, Request{Action: Execute, Command: "python3 script.py", Workspace: "/workspace"}, Ask},
		{"safe-listed execute under auto", ModeAuto, Request{Action: Execute, Command: "git status", Workspace: "/workspace"}, Ask},
		{"unlisted execute under auto", ModeAuto, Request{Action: Execute, Command: "unlisted-tool"}, Ask},
		{"deletion", ModeAuto, Request{Action: Execute, Command: "rm x", Workspace: "/workspace"}, Ask},
		{"read stays allowed", ModeAuto, Request{Action: Read}, Allow},
		{"list stays allowed", ModeAsk, Request{Action: List}, Allow},
		{"fetch stays denied", ModeAuto, Request{Action: Fetch}, Deny},
		{"download stays denied", ModeAsk, Request{Action: Download}, Deny},
	}
	for _, c := range cases {
		if got := Decide(c.mode, TaintWeb, c.req); got != c.want {
			t.Errorf("%s: Decide(%s, web, %+v) = %v, want %v", c.name, c.mode, c.req, got, c.want)
		}
	}
}

// TestUnrecognisedTaintFailsClosed guards the stored-record case: a taint this
// build does not know (a hand edit, a newer build) is treated as tainted.
func TestUnrecognisedTaintFailsClosed(t *testing.T) {
	if got := Decide(ModeAuto, Taint("email"), Request{Action: Write}); got != Ask {
		t.Errorf("Decide(auto, unknown taint, write) = %v, want Ask", got)
	}
}

// TestTaintValid guards the closed enumeration: web is the only source, and
// the zero value is not a source a setter may record.
func TestTaintValid(t *testing.T) {
	if !TaintWeb.Valid() {
		t.Error("TaintWeb must be valid")
	}
	for _, bad := range []Taint{Untainted, "email", "WEB", " web"} {
		if bad.Valid() {
			t.Errorf("Taint(%q).Valid() = true, want false", bad)
		}
	}
	if Untainted.Tainted() || !TaintWeb.Tainted() || !Taint("x").Tainted() {
		t.Error("Tainted must be false only for the zero value")
	}
}
