// Package stacklock is the ONE cross-process exclusion every stack-mutating core
// takes around its mutate+prove window: a single advisory flock on a file in the
// villa config directory. `swapMu` (internal/dashboard) already serialised the
// dashboard's own model-switch handler against ITSELF, but a CLI verb (`backend
// set`, `speculation set`, `tools-mode enter|exit`, `coding-mode enter|exit`,
// `model swap`) runs in a SEPARATE process and could never see that mutex — its
// rollback could silently revert a switch the dashboard made during the CLI's own
// prove window. ADR-0010 records the decision; this package is what it implements.
//
// Linux/Unix only (syscall.Flock) — the project targets Fedora exclusively (v1).
package stacklock

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// FileName is the lock file's name inside the villa config directory. It is
// deliberately NOT config.toml: no core takes this lock to protect its OWN reads
// or writes of that file, only to exclude every OTHER stack-mutating core for the
// duration of its mutate+prove window.
const FileName = ".stacklock"

// ErrBusy is returned by TryAcquire when another process already holds the lock.
var ErrBusy = errors.New("stacklock: another stack mutation is already in progress")

// Lock is a held advisory flock on the file at path. The zero value is not valid;
// obtain one through Acquire or TryAcquire.
type Lock struct {
	f *os.File
}

// Acquire takes the lock, BLOCKING until it is free. CLI verbs use this: a stack
// mutation's mutate+prove window is seconds, so waiting for a concurrent one to
// finish is preferable to a random refusal the operator would just retry anyway.
//
// ponytail: no timeout — a holder that never releases (a crashed process leaves
// the flock released automatically on fd close by the kernel, but a hung one does
// not) blocks every other stack mutation until it does. Add a bounded wait if a
// stuck holder proves painful in practice; syscall.Flock has no native timeout.
func Acquire(path string) (*Lock, error) {
	return acquire(path, true)
}

// TryAcquire takes the lock without blocking, returning ErrBusy immediately when
// another process holds it. The dashboard's HTTP handler uses this — mirroring its
// existing swapMu.TryLock()-then-409 shape — so a request never blocks on a lock a
// client-side timeout may have already given up on.
func TryAcquire(path string) (*Lock, error) {
	return acquire(path, false)
}

func acquire(path string, block bool) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("stacklock: open %q: %w", path, err)
	}
	how := syscall.LOCK_EX
	if !block {
		how |= syscall.LOCK_NB
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		_ = f.Close()
		if !block && errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrBusy
		}
		return nil, fmt.Errorf("stacklock: flock %q: %w", path, err)
	}
	return &Lock{f: f}, nil
}

// Release unlocks and closes the file. Safe to call once on a non-nil *Lock; a nil
// receiver is a no-op so a deferred Release after a failed Acquire is always safe.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	closeErr := l.f.Close()
	l.f = nil
	if unlockErr != nil {
		return fmt.Errorf("stacklock: unlock: %w", unlockErr)
	}
	return closeErr
}
