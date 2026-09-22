package stacklock

import (
	"path/filepath"
	"testing"
	"time"
)

// TestTryAcquireRefusesWhileHeld guards the dashboard's 409 path: a second,
// non-blocking acquire on a lock another (simulated) process already holds must
// fail with ErrBusy rather than blocking or silently succeeding.
func TestTryAcquireRefusesWhileHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)

	held, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer held.Release()

	if _, err := TryAcquire(path); err != ErrBusy {
		t.Fatalf("TryAcquire while held: err = %v, want ErrBusy", err)
	}
}

// TestReleaseAllowsReacquire guards that Release actually frees the flock, not
// merely closes the handle silently ignored by the kernel.
func TestReleaseAllowsReacquire(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)

	l, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	l2, err := TryAcquire(path)
	if err != nil {
		t.Fatalf("TryAcquire after Release: %v", err)
	}
	defer l2.Release()
}

// TestAcquireBlocksUntilReleased guards the CLI's wait-don't-refuse promise: a
// blocking Acquire against an already-held lock must not return until the holder
// releases it — asserted with a real elapsed-time floor, not just eventual success.
func TestAcquireBlocksUntilReleased(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)

	held, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	const holdFor = 150 * time.Millisecond
	released := make(chan struct{})
	go func() {
		time.Sleep(holdFor)
		held.Release()
		close(released)
	}()

	start := time.Now()
	waiter, err := Acquire(path)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Acquire (waiter): %v", err)
	}
	defer waiter.Release()
	<-released

	if elapsed < holdFor/2 {
		t.Errorf("Acquire returned after %v, want it to have waited roughly %v for the release", elapsed, holdFor)
	}
}

// TestReleaseIsSafeOnNilAndDoubleCall guards the defer-safety contract: a nil
// *Lock (a failed Acquire) and a second Release call must both be no-ops, never a
// panic, so callers can defer unconditionally.
func TestReleaseIsSafeOnNilAndDoubleCall(t *testing.T) {
	var nilLock *Lock
	if err := nilLock.Release(); err != nil {
		t.Errorf("Release on nil = %v, want nil", err)
	}

	path := filepath.Join(t.TempDir(), FileName)
	l, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("first Release: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Errorf("second Release = %v, want nil (no-op)", err)
	}
}
