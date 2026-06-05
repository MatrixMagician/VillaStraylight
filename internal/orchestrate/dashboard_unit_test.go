package orchestrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testBinaryPath is the stable, space-free path the render/golden tests use so the
// golden fixture is deterministic (the install caller resolves the real path at
// runtime; the renderer only renders whatever path it is handed).
const testBinaryPath = "/opt/villa/bin/villa"

// TestRenderDashboardUnit asserts the rendered native villa-dashboard.service body
// matches the captured golden fixture byte-for-byte. This is a DELIBERATE NEW golden
// (the dashboard .service unit), distinct from the frozen status --json golden in
// cmd/villa — flagged in the plan's <output> as an intentional fixture addition.
func TestRenderDashboardUnit(t *testing.T) {
	got, err := RenderDashboardUnit(testBinaryPath)
	if err != nil {
		t.Fatalf("RenderDashboardUnit: %v", err)
	}

	golden := filepath.Join("testdata", "villa-dashboard.service.golden")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("updated %s", golden)
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with UPDATE_GOLDEN=1 to create): %v", err)
	}
	if got != string(want) {
		t.Errorf("rendered dashboard unit does not match golden.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestRenderDashboardUnitContents asserts the rendered unit carries the contract
// tokens the plan's acceptance criteria require: a native [Unit]/[Service]/[Install]
// with Type=simple, an ExecStart running the caller-supplied binary path + ` dashboard`,
// Restart=on-failure, and WantedBy=default.target (so [Install] enables boot-survival).
// It also asserts the fixed `%h/.local/bin/villa` install path is GONE (UAT Test 5
// fix: the install flow never deployed the binary to that path → 203/EXEC at boot).
func TestRenderDashboardUnitContents(t *testing.T) {
	got, err := RenderDashboardUnit(testBinaryPath)
	if err != nil {
		t.Fatalf("RenderDashboardUnit: %v", err)
	}
	for _, want := range []string{
		"[Unit]",
		"[Service]",
		"[Install]",
		"Type=simple",
		"Restart=on-failure",
		"WantedBy=default.target",
		"ExecStart=" + testBinaryPath + " dashboard",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered dashboard unit missing %q\n%s", want, got)
		}
	}
	// NEGATIVE: the old fixed install path must NOT appear (root cause of UAT Test 5).
	if strings.Contains(got, "%h/.local/bin/villa") {
		t.Errorf("rendered dashboard unit still contains the fixed %%h/.local/bin/villa path\n%s", got)
	}
}

// TestRenderDashboardUnitQuotesPathWithSpaces asserts a binary path containing a
// space is emitted systemd-quoted so ExecStart parses as a single executable token
// plus the `dashboard` arg (rather than splitting on the space into a bogus command).
func TestRenderDashboardUnitQuotesPathWithSpaces(t *testing.T) {
	got, err := RenderDashboardUnit("/home/u/My Apps/villa")
	if err != nil {
		t.Fatalf("RenderDashboardUnit: %v", err)
	}
	want := `ExecStart="/home/u/My Apps/villa" dashboard`
	if !strings.Contains(got, want) {
		t.Errorf("rendered ExecStart not systemd-quoted for a spaced path; want %q\n%s", want, got)
	}
}

// TestRenderDashboardUnitEscapesPercent asserts a binary path containing a literal
// `%` is emitted with the `%` doubled (`%%`). systemd expands `%`-specifiers (e.g.
// %h, %c) in ExecStart REGARDLESS of quoting, so an un-doubled `%` in a legal POSIX
// path byte mangles the executable token → 203/EXEC at boot — the exact failure class
// this change set exists to fix (CR-01). `%` is space-free so the path stays unquoted.
func TestRenderDashboardUnitEscapesPercent(t *testing.T) {
	got, err := RenderDashboardUnit("/home/u/100%cool/villa")
	if err != nil {
		t.Fatalf("RenderDashboardUnit: %v", err)
	}
	want := "ExecStart=/home/u/100%%cool/villa dashboard"
	if !strings.Contains(got, want) {
		t.Errorf("rendered ExecStart did not escape %% as %%%%; want %q\n%s", want, got)
	}
}

// TestRenderDashboardUnitEscapesQuotesInQuotedPath asserts that when a path is wrapped
// in systemd double-quotes (because it contains whitespace), any embedded `"` or `\`
// is backslash-escaped so the quoted token survives systemd's unquoting intact (WR-01).
// Without escaping, an embedded quote would prematurely close the token and corrupt argv.
func TestRenderDashboardUnitEscapesQuotesInQuotedPath(t *testing.T) {
	got, err := RenderDashboardUnit(`/home/u/a "b"/villa`)
	if err != nil {
		t.Fatalf("RenderDashboardUnit: %v", err)
	}
	want := `ExecStart="/home/u/a \"b\"/villa" dashboard`
	if !strings.Contains(got, want) {
		t.Errorf("rendered ExecStart did not escape embedded quotes in a quoted path; want %q\n%s", want, got)
	}
}

// TestRenderDashboardUnitQuotesBackslashPath asserts a path containing a backslash (a
// legal filename byte that systemd treats as a C-style escape both inside and outside
// quotes) is quoted and its backslash doubled, so the literal byte survives (WR-01).
func TestRenderDashboardUnitQuotesBackslashPath(t *testing.T) {
	got, err := RenderDashboardUnit(`/home/u/a\b/villa`)
	if err != nil {
		t.Fatalf("RenderDashboardUnit: %v", err)
	}
	want := `ExecStart="/home/u/a\\b/villa" dashboard`
	if !strings.Contains(got, want) {
		t.Errorf("rendered ExecStart did not quote+escape a backslash path; want %q\n%s", want, got)
	}
}

// TestUserUnitDir asserts userUnitDir resolves under <UserConfigDir>/systemd/user
// (honoring $XDG_CONFIG_HOME) and NEVER the Quadlet containers/systemd dir (Pitfall 5).
func TestUserUnitDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir, err := userUnitDir()
	if err != nil {
		t.Fatalf("userUnitDir: %v", err)
	}
	want := filepath.Join(tmp, "systemd", "user")
	if dir != want {
		t.Fatalf("userUnitDir = %q, want %q", dir, want)
	}
	if strings.Contains(dir, filepath.Join("containers", "systemd")) {
		t.Fatalf("userUnitDir resolved into the Quadlet dir %q (Pitfall 5)", dir)
	}
	// The dir is created so the first install writes cleanly.
	if fi, statErr := os.Stat(dir); statErr != nil || !fi.IsDir() {
		t.Fatalf("userUnitDir not created as a directory: %v", statErr)
	}
}

// TestWriteDashboardUnitAtomic asserts WriteDashboardUnit writes the rendered unit
// atomically into the user-unit dir, reusing the traversal-guarded atomicWrite.
func TestWriteDashboardUnitAtomic(t *testing.T) {
	dir := t.TempDir()
	if err := WriteDashboardUnit(dir, testBinaryPath); err != nil {
		t.Fatalf("WriteDashboardUnit: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, DashboardServiceName))
	if err != nil {
		t.Fatalf("read written unit: %v", err)
	}
	want, err := RenderDashboardUnit(testBinaryPath)
	if err != nil {
		t.Fatalf("RenderDashboardUnit: %v", err)
	}
	if string(got) != want {
		t.Fatalf("written unit != rendered unit\n--- written ---\n%s\n--- rendered ---\n%s", got, want)
	}
	// No leftover *.tmp sibling.
	if _, statErr := os.Stat(filepath.Join(dir, DashboardServiceName+".tmp")); !os.IsNotExist(statErr) {
		t.Fatalf("a *.tmp sibling was left behind: %v", statErr)
	}
}

// TestWriteDashboardUnitRefusesTraversal asserts the write is traversal-guarded:
// a unit dir that the service name would escape is refused before any write (reuses
// assertInsideDir, T-05-15).
func TestWriteDashboardUnitRefusesTraversal(t *testing.T) {
	// A target whose name escapes the dir must be refused. We exercise the guard
	// directly via writeUnitFile since DashboardServiceName is a fixed safe name.
	dir := t.TempDir()
	if err := writeUnitFile(dir, "../escape.service", "x"); err == nil {
		t.Fatalf("writeUnitFile accepted a traversal name; want refusal")
	}
	// The safe name still writes.
	if err := writeUnitFile(dir, DashboardServiceName, "ok"); err != nil {
		t.Fatalf("writeUnitFile rejected the safe name: %v", err)
	}
}
