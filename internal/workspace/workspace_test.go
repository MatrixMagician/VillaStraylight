package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// realDeps wires Deps to the real filesystem so most rows below need no
// hand-rolled FileInfo fake: a t.TempDir() and a genuine symlink loop exercise
// Stat/EvalSymlinks exactly as production will.
func realDeps(home, configRoot, dataRoot string) Deps {
	return Deps{
		Home:         func() (string, error) { return home, nil },
		ConfigRoot:   func() string { return configRoot },
		DataRoot:     func() string { return dataRoot },
		Stat:         os.Stat,
		EvalSymlinks: filepath.EvalSymlinks,
	}
}

// TestRegisterRefusals asserts one row per spec §3.1 refusal, plus a success
// row and an idempotent re-add row.
func TestRegisterRefusals(t *testing.T) {
	home := t.TempDir()
	configRoot := filepath.Join(home, ".config", "villa")
	dataRoot := filepath.Join(home, ".local", "share", "villa")
	for _, d := range []string{configRoot, dataRoot} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("MkdirAll %s: %v", d, err)
		}
	}
	projects := filepath.Join(home, "projects")
	sub := filepath.Join(projects, "sub")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatalf("MkdirAll %s: %v", sub, err)
	}
	outside := t.TempDir()
	file := filepath.Join(home, "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	loop := filepath.Join(home, "loop")
	if err := os.Symlink("loop", loop); err != nil {
		t.Fatalf("Symlink loop: %v", err)
	}
	configDescendant := filepath.Join(configRoot, "tasks")
	if err := os.MkdirAll(configDescendant, 0o700); err != nil {
		t.Fatalf("MkdirAll %s: %v", configDescendant, err)
	}

	deps := realDeps(home, configRoot, dataRoot)

	cases := []struct {
		name     string
		cfg      config.VillaConfig
		path     string
		wantKind RefusalKind
	}{
		{"Relative", config.VillaConfig{}, "relative/path", Relative},
		{"NotResolved", config.VillaConfig{}, loop, NotResolved},
		{"Missing", config.VillaConfig{}, filepath.Join(home, "does-not-exist"), Missing},
		{"NotDir", config.VillaConfig{}, file, NotDir},
		{"Home", config.VillaConfig{}, home, Home},
		{"OutsideHome", config.VillaConfig{}, outside, OutsideHome},
		{"VillaRoot/config descendant", config.VillaConfig{}, configDescendant, VillaRoot},
		{"VillaRoot/data ancestor", config.VillaConfig{}, filepath.Dir(dataRoot), VillaRoot},
		{
			"Nested/parent-of-existing",
			config.VillaConfig{Workspace: []string{sub}},
			projects,
			Nested,
		},
		{
			"Nested/child-of-existing",
			config.VillaConfig{Workspace: []string{projects}},
			sub,
			Nested,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Register(tc.cfg, tc.path, deps)
			if err == nil {
				t.Fatalf("Register(%q) = nil error, want Refusal(%s)", tc.path, tc.wantKind)
			}
			refusal, ok := err.(Refusal)
			if !ok {
				t.Fatalf("Register(%q) error = %T(%v), want Refusal", tc.path, err, err)
			}
			if refusal.Kind != tc.wantKind {
				t.Errorf("Register(%q) kind = %s, want %s", tc.path, refusal.Kind, tc.wantKind)
			}
			if refusal.Remediation == "" {
				t.Errorf("Register(%q) remediation is empty", tc.path)
			}
		})
	}

	t.Run("success", func(t *testing.T) {
		got, err := Register(config.VillaConfig{}, projects, deps)
		if err != nil {
			t.Fatalf("Register(%q): %v", projects, err)
		}
		if len(got.Workspace) != 1 || got.Workspace[0] != projects {
			t.Errorf("Workspace = %v, want [%s]", got.Workspace, projects)
		}
	})

	t.Run("idempotent re-add", func(t *testing.T) {
		cfg := config.VillaConfig{Workspace: []string{projects}}
		got, err := Register(cfg, projects, deps)
		if err != nil {
			t.Fatalf("Register(%q): %v", projects, err)
		}
		if len(got.Workspace) != 1 {
			t.Errorf("Workspace = %v, want no duplicate", got.Workspace)
		}
	})
}

// TestRegisterRefusesSensitiveDirectories asserts Register refuses known
// exec/autostart locations and dot-directories SELinux labels specially, not
// just an overlap with villa's own config/data roots (GHSA-3q4q-7cmw-m22m,
// GHSA-45r5-q556-rrgc).
func TestRegisterRefusesSensitiveDirectories(t *testing.T) {
	home := t.TempDir()
	deps := realDeps(home, filepath.Join(home, ".config", "villa"), filepath.Join(home, ".local", "share", "villa"))

	for _, rel := range []string{
		"bin",
		filepath.Join(".local", "bin"),
		filepath.Join(".config", "systemd"),
		filepath.Join(".config", "autostart"),
		".ssh",
		".gnupg",
	} {
		dir := filepath.Join(home, rel)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll %s: %v", dir, err)
		}
		t.Run(rel, func(t *testing.T) {
			_, err := Register(config.VillaConfig{}, dir, deps)
			refusal, ok := err.(Refusal)
			if !ok {
				t.Fatalf("Register(%q) error = %T(%v), want Refusal", dir, err, err)
			}
			if refusal.Kind != Sensitive {
				t.Errorf("Register(%q) kind = %s, want %s", dir, refusal.Kind, Sensitive)
			}
		})
	}
}

// TestRegisterRefusesContainingTheRunningExecutable asserts Register refuses
// a folder that contains os.Executable() after EvalSymlinks: that binary is
// bind-mounted into every task, so granting its folder would let the in-VM
// agent overwrite the file the dashboard unit runs next (GHSA-3q4q-7cmw-m22m).
// A sibling folder that does not contain it still registers.
func TestRegisterRefusesContainingTheRunningExecutable(t *testing.T) {
	home := t.TempDir()
	codeDir := filepath.Join(home, "code")
	if err := os.MkdirAll(codeDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	exe := filepath.Join(codeDir, "villa")
	if err := os.WriteFile(exe, []byte("bin"), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	deps := realDeps(home, filepath.Join(home, ".config", "villa"), filepath.Join(home, ".local", "share", "villa"))
	deps.Executable = func() (string, error) { return exe, nil }

	_, err := Register(config.VillaConfig{}, codeDir, deps)
	refusal, ok := err.(Refusal)
	if !ok {
		t.Fatalf("Register(%q) error = %T(%v), want Refusal", codeDir, err, err)
	}
	if refusal.Kind != ContainsExecutable {
		t.Errorf("Register(%q) kind = %s, want %s", codeDir, refusal.Kind, ContainsExecutable)
	}

	other := filepath.Join(home, "other")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if _, err := Register(config.VillaConfig{}, other, deps); err != nil {
		t.Errorf("Register(%q): %v, want success", other, err)
	}
}

// TestCheckGrantRefusesAnAlreadyGrantedPath asserts CheckGrant refuses a
// resolved path directly, independent of Register: this is the re-check the
// task-launch path re-runs, so a workspace granted before this check existed
// is still refused when a task actually launches (GHSA-3q4q-7cmw-m22m).
func TestCheckGrantRefusesAnAlreadyGrantedPath(t *testing.T) {
	home := t.TempDir()
	ssh := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(ssh, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	safe := filepath.Join(home, "projects")
	if err := os.MkdirAll(safe, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	deps := realDeps(home, "", "")

	if err := CheckGrant(ssh, deps); err == nil {
		t.Error("CheckGrant(.ssh) = nil error, want a refusal")
	}
	if err := CheckGrant(safe, deps); err != nil {
		t.Errorf("CheckGrant(%q) = %v, want nil", safe, err)
	}
}

// TestRegistered asserts Registered resolves a path and finds its grant, and
// reports false for an unknown path with no error.
func TestRegistered(t *testing.T) {
	home := t.TempDir()
	projects := filepath.Join(home, "projects")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	deps := realDeps(home, filepath.Join(home, ".config", "villa"), filepath.Join(home, ".local", "share", "villa"))
	cfg := config.VillaConfig{Workspace: []string{projects}}

	if got, ok := Registered(cfg, projects, deps); !ok || got != projects {
		t.Errorf("Registered(%q) = (%q, %v), want (%q, true)", projects, got, ok, projects)
	}
	other := filepath.Join(home, "other")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if _, ok := Registered(cfg, other, deps); ok {
		t.Errorf("Registered(%q) = true, want false (not granted)", other)
	}
}

// TestRemove asserts Remove forgets an exact-match grant and touches no file,
// and refuses an unknown path.
func TestRemove(t *testing.T) {
	cfg := config.VillaConfig{Workspace: []string{"/home/op/a", "/home/op/b"}}

	got, err := Remove(cfg, "/home/op/a")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(got.Workspace) != 1 || got.Workspace[0] != "/home/op/b" {
		t.Errorf("Workspace after remove = %v, want [/home/op/b]", got.Workspace)
	}

	if _, err := Remove(cfg, "/home/op/unknown"); err == nil {
		t.Error("Remove(unknown) = nil error, want a refusal")
	}
}
