package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/stacklock"
)

// TestConfigSetWaitsForTheStackLock guards #267: `villa config set` is a load→save
// of the whole config.toml, and a swap's rollback restores the whole captured file,
// so a set that lands inside a swap's prove window is silently reverted. It must
// wait for the lock and read the config only after it has it.
func TestConfigSetWaitsForTheStackLock(t *testing.T) {
	requireWaitsForStackLock(t, func() {
		cmd, _, _ := lifecycleTestCmd()
		runConfigSet(cmd, "ctx=8192", liveConfigDeps())
	})
	cfg, err := config.LoadVilla()
	if err != nil || cfg.Ctx != 8192 {
		t.Errorf("the set must land once the lock is free, got ctx %d, err %v", cfg.Ctx, err)
	}
}

// TestWorkspaceAddRemoveWaitForTheStackLock guards #267: the grant list is written
// into config.toml, so add and remove are config writers a swap rollback can revert.
func TestWorkspaceAddRemoveWaitForTheStackLock(t *testing.T) {
	home := t.TempDir()
	projects := filepath.Join(home, "projects")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatal(err)
	}
	roots := func() (string, string) {
		return filepath.Join(home, ".config", "villa"), filepath.Join(home, ".local", "share", "villa")
	}
	t.Run("add", func(t *testing.T) {
		cfgRoot, dataRoot := roots()
		d, _ := fakeWorkspaceDeps(config.VillaConfig{}, home, cfgRoot, dataRoot)
		requireWaitsForStackLock(t, func() {
			cmd, _, _ := lifecycleTestCmd()
			runWorkspaceAdd(cmd, projects, d)
		})
	})
	t.Run("remove", func(t *testing.T) {
		cfgRoot, dataRoot := roots()
		d, _ := fakeWorkspaceDeps(config.VillaConfig{Workspace: []string{projects}}, home, cfgRoot, dataRoot)
		requireWaitsForStackLock(t, func() {
			cmd, _, _ := lifecycleTestCmd()
			runWorkspaceRemove(cmd, projects, d)
		})
	})
}

// TestRecommendSaveWaitsForTheStackLock guards #267: `recommend --save` loads the
// config on disk and writes it back, the same revertable write as `config set`.
func TestRecommendSaveWaitsForTheStackLock(t *testing.T) {
	requireWaitsForStackLock(t, func() {
		_ = saveRecommendation(io.Discard, fixtureRecommendation(), "")
	})
}

// TestVerifyAgentWaitsForTheStackLock guards #267: `verify agent` stops villa-llama
// and starts it again, which fails a concurrent swap's proof for a reason the swap
// did not cause. The proof (and its stop) must not begin while the lock is held.
func TestVerifyAgentWaitsForTheStackLock(t *testing.T) {
	deps := verifyAgentDeps{
		loadedAgentEnabled: func() bool { return true },
		verifyFn: func(context.Context, verifyAgentDeps) memoryProof {
			return memoryProof{status: preflight.StatusPass}
		},
	}
	requireWaitsForStackLock(t, func() {
		cmd, _, _ := lifecycleTestCmd()
		runVerifyAgent(cmd, nil, deps)
	})
}

// TestBackupWaitsForTheStackLock guards #267: `backup` stops Open WebUI and Qdrant
// to export a clean volume, which a concurrent swap's proof would see as an outage.
func TestBackupWaitsForTheStackLock(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	outPath := filepath.Join(t.TempDir(), "b.tar")
	requireWaitsForStackLock(t, func() {
		cmd, _, _ := newBackupTestCmd()
		runBackup(cmd, outPath, fakeRunDeps(t, map[string][]byte{}))
	})
}

// requireWaitsForStackLock is the behavioural half of ADR-0010's coverage: while
// another stack mutation holds the REAL blocking flock (a temp XDG dir, the shape
// TestBenchABSwitchWaitsForTheStackLock uses), run must not return; once the lock is
// released it must proceed. A verb that takes no lock, or takes it after its first
// config read or service stop, returns while the lock is held and fails here.
//
// run executes on its own goroutine, so it must not call t.Fatal.
func requireWaitsForStackLock(t *testing.T, run func()) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "villa"), 0o700); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(dir, "villa", stacklock.FileName)
	prev := acquireStackLock
	acquireStackLock = func() (*stacklock.Lock, error) { return stacklock.Acquire(lockPath) }
	t.Cleanup(func() { acquireStackLock = prev })

	held, err := stacklock.Acquire(lockPath)
	if err != nil {
		t.Fatalf("hold the stack lock: %v", err)
	}
	done := make(chan struct{})
	go func() {
		run()
		close(done)
	}()

	select {
	case <-done:
		_ = held.Release()
		t.Fatal("the verb ran while another stack mutation held the lock")
	case <-time.After(300 * time.Millisecond):
	}
	if err := held.Release(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the verb never proceeded after the lock was released")
	}
}
