package beads

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestRunBDTransientReadBoundsEachAttempt proves a stuck bd read attempt is
// killed at bdTransientCommandTimeout (via the context-aware runner) instead
// of blocking on the long-lived city context. The boot sweep's wisp probe runs
// on the synchronous readiness path, so an unbounded read wedge freezes the
// whole city until the startup watchdog (#3288 boot hang).
func TestRunBDTransientReadBoundsEachAttempt(t *testing.T) {
	oldTimeout := bdTransientCommandTimeout
	bdTransientCommandTimeout = 50 * time.Millisecond
	t.Cleanup(func() { bdTransientCommandTimeout = oldTimeout })

	var sawDeadline bool
	ctxRunner := func(ctx context.Context, _, _ string, _ ...string) ([]byte, error) {
		select {
		case <-ctx.Done():
			sawDeadline = true
			return nil, fmt.Errorf("bd list: %w", ctx.Err())
		case <-time.After(5 * time.Second):
			return []byte("late"), nil
		}
	}
	s := NewBdStore(t.TempDir(), failRunner(t), WithBdStoreContextRunner(ctxRunner))

	start := time.Now()
	_, err := s.runBDTransientRead("list", "--json")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("runBDTransientRead unexpectedly succeeded; the stuck attempt was not bounded")
	}
	if !sawDeadline {
		t.Fatal("context-aware runner never observed the per-attempt deadline")
	}
	// Three retry attempts, each ~50ms, must finish well under the city budget.
	if elapsed > 5*time.Second {
		t.Fatalf("runBDTransientRead blocked %s; the per-attempt deadline was ignored", elapsed)
	}
}

// TestRunBDTransientWriteBoundsAndRetriesOnTimeout proves the per-attempt
// deadline error is treated as retryable so a transiently-stuck write fails
// fast and retries instead of parking the boot reconcile. This is the
// load-bearing half of the #3288 fix: the per-session currently_processing_bead_id
// write wave must not be able to wedge a contended bd subprocess for minutes.
func TestRunBDTransientWriteBoundsAndRetriesOnTimeout(t *testing.T) {
	oldTimeout := bdTransientCommandTimeout
	bdTransientCommandTimeout = 50 * time.Millisecond
	t.Cleanup(func() { bdTransientCommandTimeout = oldTimeout })

	attempts := 0
	ctxRunner := func(ctx context.Context, _, _ string, _ ...string) ([]byte, error) {
		attempts++
		if attempts < bdTransientWriteAttempts {
			// Simulate a contended subprocess that blows the deadline.
			<-ctx.Done()
			return nil, fmt.Errorf("bd update: %w", ctx.Err())
		}
		return []byte(`{"id":"bd-x"}`), nil
	}
	s := NewBdStore(t.TempDir(), failRunner(t), WithBdStoreContextRunner(ctxRunner))

	out, err := s.runBDTransientWriteOutput("update", "bd-x", "--set-metadata", "k=v")
	if err != nil {
		t.Fatalf("runBDTransientWriteOutput returned %v, want success after retry", err)
	}
	if attempts != bdTransientWriteAttempts {
		t.Fatalf("attempts = %d, want %d (deadline error must be retryable)", attempts, bdTransientWriteAttempts)
	}
	if string(out) != `{"id":"bd-x"}` {
		t.Fatalf("out = %q, want the successful retry payload", out)
	}
}

// TestTransientTimeoutErrorsAreRetryable pins that context-deadline errors map
// onto the existing ambiguous/transient retry classifiers, so threading a short
// deadline does not silently make timeouts terminal.
func TestTransientTimeoutErrorsAreRetryable(t *testing.T) {
	deadlineErr := fmt.Errorf("bd update: %w", context.DeadlineExceeded)
	if !isBdAmbiguousWriteError(deadlineErr) {
		t.Fatalf("context.DeadlineExceeded not classified as an ambiguous (retryable) write error")
	}
	if !isBdTransientWriteError(deadlineErr) {
		t.Fatalf("context.DeadlineExceeded not classified as a transient (retryable) write error")
	}
	if !errors.Is(deadlineErr, context.DeadlineExceeded) {
		t.Fatalf("deadline error lost its wrapped sentinel")
	}
}

// TestRunBDTransientReadFallsBackToPlainRunner proves stores constructed without
// a context-aware runner (the common test/non-exec path) keep their existing
// unbounded behavior, so the new seam is strictly additive.
func TestRunBDTransientReadFallsBackToPlainRunner(t *testing.T) {
	calls := 0
	runner := func(_, _ string, _ ...string) ([]byte, error) {
		calls++
		return []byte("ok"), nil
	}
	s := NewBdStore(t.TempDir(), runner)
	out, err := s.runBDTransientRead("list", "--json")
	if err != nil {
		t.Fatalf("runBDTransientRead = %v, want success via plain runner", err)
	}
	if calls != 1 || string(out) != "ok" {
		t.Fatalf("calls=%d out=%q, want one plain-runner call returning ok", calls, out)
	}
}

// failRunner returns a CommandRunner that fails the test if invoked; the
// context-aware runner path must not fall through to the plain runner.
func failRunner(t *testing.T) CommandRunner {
	t.Helper()
	return func(_, _ string, args ...string) ([]byte, error) {
		t.Fatalf("plain runner invoked unexpectedly with args %v; context runner should have handled the call", args)
		return nil, nil
	}
}
