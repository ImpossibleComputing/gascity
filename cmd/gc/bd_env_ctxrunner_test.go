package main

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// TestBdCtxCommandRunnerHonorsDeadline proves the context-aware managed-retry
// runner cancels a stuck child promptly when the caller's context deadline
// fires, instead of blocking on the per-command bd timeout. BdStore installs
// this runner on the transient read/write path so a contended bd subprocess
// cannot wedge the boot reconcile (#3288). A non-"bd" command name keeps the
// invocation off the managed-Dolt recovery branch so the test exercises only
// the context plumbing.
func TestBdCtxCommandRunnerHonorsDeadline(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep unavailable")
	}

	runner := bdCtxCommandRunnerWithManagedRetryErr(t.TempDir(), func(_ string) (map[string]string, error) {
		return map[string]string{}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := runner(ctx, t.TempDir(), "sleep", "30")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("ctx-bound runner unexpectedly succeeded; the deadline was ignored")
	}
	if elapsed > 10*time.Second {
		t.Fatalf("ctx-bound runner blocked %s; the 200ms deadline was not honored", elapsed)
	}
}

// TestBdCtxCommandRunnerPropagatesEnvError proves the ctx runner surfaces an
// env-resolution failure without spawning a child, matching the non-ctx runner.
func TestBdCtxCommandRunnerPropagatesEnvError(t *testing.T) {
	wantErr := context.Canceled
	runner := bdCtxCommandRunnerWithManagedRetryErr(t.TempDir(), func(_ string) (map[string]string, error) {
		return nil, wantErr
	})
	if _, err := runner(context.Background(), t.TempDir(), "bd", "list"); err != wantErr {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}
