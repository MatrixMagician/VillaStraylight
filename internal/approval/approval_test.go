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
		got := Decide(c.mode, Request{Action: c.action})
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
		got := Decide(ModeAuto, c.req)
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
	if got := Decide(ModeAsk, req); got != Ask {
		t.Errorf("Decide(ask, deletion) = %v, want Ask", got)
	}
	if got := Decide(ModeAuto, req); got != Ask {
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
	}

	for _, c := range cases {
		got := AutoAllowed(c.command, c.workspace)
		if got != c.want {
			t.Errorf("AutoAllowed(%q, %q) = %v, want %v", c.command, c.workspace, got, c.want)
		}
	}
}
