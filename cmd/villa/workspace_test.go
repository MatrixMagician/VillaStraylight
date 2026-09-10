package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/workspace"
)

// fakeWorkspaceDeps wires workspaceDeps to an in-memory config and a real
// filesystem fixture under t.TempDir(), so add/list/remove are exercisable
// without touching the user's real XDG config.
func fakeWorkspaceDeps(cfg config.VillaConfig, home, configRoot, dataRoot string) (*workspaceDeps, *config.VillaConfig) {
	saved := cfg
	d := &workspaceDeps{
		load: func() (config.VillaConfig, error) { return saved, nil },
		save: func(c config.VillaConfig) error { saved = c; return nil },
		wd: workspace.Deps{
			Home:         func() (string, error) { return home, nil },
			ConfigRoot:   func() string { return configRoot },
			DataRoot:     func() string { return dataRoot },
			Stat:         os.Stat,
			EvalSymlinks: filepath.EvalSymlinks,
		},
	}
	return d, &saved
}

// TestRunWorkspaceAddSuccess asserts a valid path is registered, persisted, and
// printed.
func TestRunWorkspaceAddSuccess(t *testing.T) {
	home := t.TempDir()
	projects := filepath.Join(home, "projects")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	d, saved := fakeWorkspaceDeps(config.VillaConfig{}, home, filepath.Join(home, ".config", "villa"), filepath.Join(home, ".local", "share", "villa"))
	cmd, out, _ := lifecycleTestCmd()

	code := runWorkspaceAdd(cmd, projects, d)
	if code != exitPass {
		t.Fatalf("exit = %d, want %d", code, exitPass)
	}
	if !strings.Contains(out.String(), "registered") {
		t.Errorf("output = %q, want it to mention registration", out.String())
	}
	if len(saved.Workspace) != 1 || saved.Workspace[0] != projects {
		t.Errorf("saved.Workspace = %v, want [%s]", saved.Workspace, projects)
	}
}

// TestRunWorkspaceAddRefusalPrintsRemediationAndExits1 asserts a refused path
// exits 1 and prints the remediation, without saving anything.
func TestRunWorkspaceAddRefusalPrintsRemediationAndExits1(t *testing.T) {
	home := t.TempDir()
	d, saved := fakeWorkspaceDeps(config.VillaConfig{}, home, filepath.Join(home, ".config", "villa"), filepath.Join(home, ".local", "share", "villa"))
	cmd, _, errOut := lifecycleTestCmd()

	code := runWorkspaceAdd(cmd, "relative/path", d)
	if code != exitBlocked {
		t.Fatalf("exit = %d, want %d", code, exitBlocked)
	}
	if !strings.Contains(errOut.String(), "refused") || !strings.Contains(errOut.String(), "absolute") {
		t.Errorf("stderr = %q, want a remediation mentioning an absolute path", errOut.String())
	}
	if len(saved.Workspace) != 0 {
		t.Errorf("saved.Workspace = %v, want unchanged", saved.Workspace)
	}
}

// TestRunWorkspaceListTable asserts the default table lists one path per line.
func TestRunWorkspaceListTable(t *testing.T) {
	d, _ := fakeWorkspaceDeps(config.VillaConfig{Workspace: []string{"/home/op/projects", "/home/op/notes"}}, "/home/op", "/home/op/.config/villa", "/home/op/.local/share/villa")
	cmd, out, _ := lifecycleTestCmd()

	code := runWorkspaceList(cmd, false, d)
	if code != exitPass {
		t.Fatalf("exit = %d, want %d", code, exitPass)
	}
	for _, want := range []string{"/home/op/projects", "/home/op/notes"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output = %q, want it to contain %q", out.String(), want)
		}
	}
}

// TestRunWorkspaceListEmpty asserts an empty grant list says so rather than
// printing nothing.
func TestRunWorkspaceListEmpty(t *testing.T) {
	d, _ := fakeWorkspaceDeps(config.VillaConfig{}, "/home/op", "/home/op/.config/villa", "/home/op/.local/share/villa")
	cmd, out, _ := lifecycleTestCmd()

	if code := runWorkspaceList(cmd, false, d); code != exitPass {
		t.Fatalf("exit = %d, want %d", code, exitPass)
	}
	if !strings.Contains(out.String(), "no registered workspaces") {
		t.Errorf("output = %q, want the empty-list message", out.String())
	}
}

// TestWorkspaceListJSONGolden asserts `workspace list --json` matches
// cmd/villa/testdata/workspace-list.golden.json byte-for-byte (contract
// stability). Run with -update to regenerate.
func TestWorkspaceListJSONGolden(t *testing.T) {
	d, _ := fakeWorkspaceDeps(config.VillaConfig{Workspace: []string{"/home/op/projects", "/home/op/notes"}}, "/home/op", "/home/op/.config/villa", "/home/op/.local/share/villa")
	cmd, out, _ := lifecycleTestCmd()

	if code := runWorkspaceList(cmd, true, d); code != exitPass {
		t.Fatalf("exit = %d, want %d", code, exitPass)
	}

	golden := filepath.Join("testdata", "workspace-list.golden.json")
	if *update {
		if err := os.WriteFile(golden, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", golden)
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if !bytes.Equal(out.Bytes(), want) {
		t.Errorf("JSON output does not match golden.\n--- got ---\n%s\n--- want ---\n%s", out.String(), want)
	}
}

// TestRunWorkspaceRemoveSuccess asserts a registered path is forgotten and
// persisted, touching no file.
func TestRunWorkspaceRemoveSuccess(t *testing.T) {
	home := t.TempDir()
	projects := filepath.Join(home, "projects")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	d, saved := fakeWorkspaceDeps(config.VillaConfig{Workspace: []string{projects}}, home, filepath.Join(home, ".config", "villa"), filepath.Join(home, ".local", "share", "villa"))
	cmd, out, _ := lifecycleTestCmd()

	code := runWorkspaceRemove(cmd, projects, d)
	if code != exitPass {
		t.Fatalf("exit = %d, want %d", code, exitPass)
	}
	if !strings.Contains(out.String(), "removed") {
		t.Errorf("output = %q, want it to mention removal", out.String())
	}
	if len(saved.Workspace) != 0 {
		t.Errorf("saved.Workspace = %v, want empty", saved.Workspace)
	}
}

// TestRunWorkspaceRemoveUnknownExits1 asserts removing an unregistered path
// exits 1 with a message and saves nothing.
func TestRunWorkspaceRemoveUnknownExits1(t *testing.T) {
	home := t.TempDir()
	other := filepath.Join(home, "other")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	d, saved := fakeWorkspaceDeps(config.VillaConfig{}, home, filepath.Join(home, ".config", "villa"), filepath.Join(home, ".local", "share", "villa"))
	cmd, _, errOut := lifecycleTestCmd()

	code := runWorkspaceRemove(cmd, other, d)
	if code != exitBlocked {
		t.Fatalf("exit = %d, want %d", code, exitBlocked)
	}
	if !strings.Contains(errOut.String(), "not a registered workspace") {
		t.Errorf("stderr = %q, want the not-registered message", errOut.String())
	}
	if len(saved.Workspace) != 0 {
		t.Errorf("saved.Workspace = %v, want unchanged", saved.Workspace)
	}
}
