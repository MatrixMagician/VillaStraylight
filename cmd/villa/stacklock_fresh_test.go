package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLockConfigDirOnAFreshHost guards #450: on a host whose villa config directory
// does not exist yet (every host before its first install), taking the stack lock
// creates that directory at 0700 instead of failing, so `villa install` and
// `villa catalog add` can run at all.
func TestLockConfigDirOnAFreshHost(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg", "villa")

	lock, err := lockConfigDir(dir)
	if err != nil {
		t.Fatalf("lockConfigDir on a missing directory: %v", err)
	}
	defer func() { _ = lock.Release() }()

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("config dir not created: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("config dir mode = %o, want 700", got)
	}
}
