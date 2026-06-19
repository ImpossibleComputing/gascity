package main

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// waitForSocket polls until the unix socket file exists or the deadline passes.
func waitForSocket(t *testing.T, path string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// sendWakeSignal dials socketPath and sends one byte, simulating what the
// controller does after making work ready (Track A / pingNudgeWakeSocket).
func sendWakeSignal(t *testing.T, socketPath string) {
	t.Helper()
	conn, err := net.DialTimeout("unix", socketPath, 200*time.Millisecond)
	if err != nil {
		t.Logf("sendWakeSignal: dial: %v", err)
		return
	}
	defer conn.Close() //nolint:errcheck
	_ = conn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
	_, _ = conn.Write([]byte{1})
}

// --- doHookWait tests ---

// Criterion 1: work found on the first query — no blocking, exit 0.
func TestDoHookWait_ImmediateWork(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "wake.sock")
	runner := func(_, _ string) (string, error) {
		return `[{"id":"b1","title":"work item"}]`, nil
	}
	var stdout, stderr bytes.Buffer
	code := doHookWait("bd ready", "", 5*time.Second, socketPath, runner, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookWait(immediate work) = %d, want 0; stderr=%s", code, stderr.String())
	}
	if stderr.String() != "" {
		t.Errorf("unexpected stderr: %s", stderr.String())
	}
	if stdout.String() == "" {
		t.Error("expected work output on stdout, got empty")
	}
}

// Criterion 1 variant: immediate work must not leave a socket file behind.
func TestDoHookWait_ImmediateWork_NoSocketLeftBehind(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "wake.sock")
	runner := func(_, _ string) (string, error) {
		return `[{"id":"b1","title":"work"}]`, nil
	}
	var stdout, stderr bytes.Buffer
	doHookWait("bd ready", "", 5*time.Second, socketPath, runner, &stdout, &stderr)
	if _, err := os.Stat(socketPath); err == nil {
		t.Error("socket file must not exist after immediate-work return")
	}
}

// Criterion 2: no work found; blocks until timeout, then returns 1.
func TestDoHookWait_TimeoutNoWork(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "wake.sock")
	runner := func(_, _ string) (string, error) {
		return "", nil
	}
	var stdout, stderr bytes.Buffer
	start := time.Now()
	code := doHookWait("bd ready", "", 60*time.Millisecond, socketPath, runner, &stdout, &stderr)
	elapsed := time.Since(start)
	if code != 1 {
		t.Errorf("doHookWait(timeout no work) = %d, want 1", code)
	}
	if elapsed < 40*time.Millisecond {
		t.Errorf("returned too fast: %v (want ≥40ms block)", elapsed)
	}
}

// Criterion 2 variant: socket must be cleaned up after timeout.
func TestDoHookWait_TimeoutNoWork_SocketCleanedUp(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "wake.sock")
	runner := func(_, _ string) (string, error) { return "", nil }
	var stdout, stderr bytes.Buffer
	doHookWait("bd ready", "", 30*time.Millisecond, socketPath, runner, &stdout, &stderr)
	if _, err := os.Stat(socketPath); err == nil {
		t.Error("socket file must be removed after wait completes")
	}
}

// Criterion 3: wake signal arrives before timeout; re-check finds work → exit 0.
func TestDoHookWait_WakeBeforeTimeout(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "wake.sock")
	calls := 0
	runner := func(_, _ string) (string, error) {
		calls++
		if calls >= 2 {
			return `[{"id":"b2","title":"work arrived"}]`, nil
		}
		return "", nil // first call: no work yet
	}
	var stdout, stderr bytes.Buffer

	// Signal the socket after it appears.
	go func() {
		if !waitForSocket(t, socketPath, 2*time.Second) {
			return
		}
		sendWakeSignal(t, socketPath)
	}()

	code := doHookWait("bd ready", "", 5*time.Second, socketPath, runner, &stdout, &stderr)
	if code != 0 {
		t.Errorf("doHookWait(wake before timeout) = %d, want 0; stderr=%s", code, stderr.String())
	}
	if calls < 2 {
		t.Errorf("expected at least 2 runner calls (initial + post-wake), got %d", calls)
	}
}

// Criterion 3 variant: wake arrives but re-check still finds no work → exit 1.
func TestDoHookWait_WakeNoWorkFound(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "wake.sock")
	runner := func(_, _ string) (string, error) { return "", nil }
	var stdout, stderr bytes.Buffer

	go func() {
		if !waitForSocket(t, socketPath, 2*time.Second) {
			return
		}
		sendWakeSignal(t, socketPath)
	}()

	code := doHookWait("bd ready", "", 5*time.Second, socketPath, runner, &stdout, &stderr)
	if code != 1 {
		t.Errorf("doHookWait(wake no work) = %d, want 1", code)
	}
}

// Criterion 4: socket already in use (supervisor running) — falls back to
// the initial query result without blocking for the full wait duration.
func TestDoHookWait_SocketInUse_FallbackImmediate(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "wake.sock")
	// Pre-occupy the socket, simulating a running supervisor.
	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("pre-occupying socket: %v", err)
	}
	defer lis.Close() //nolint:errcheck

	runner := func(_, _ string) (string, error) { return "", nil }
	var stdout, stderr bytes.Buffer
	start := time.Now()
	code := doHookWait("bd ready", "", 5*time.Second, socketPath, runner, &stdout, &stderr)
	elapsed := time.Since(start)
	if code != 1 {
		t.Errorf("doHookWait(socket in use) = %d, want 1", code)
	}
	// Must not have blocked for the full 5s wait duration.
	if elapsed > 500*time.Millisecond {
		t.Errorf("doHookWait(socket in use) blocked %v, want <500ms fallback", elapsed)
	}
}

// Criterion 4 variant: socket in use, initial query found work → still exit 0 immediately.
func TestDoHookWait_SocketInUse_ImmediateWorkWins(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "wake.sock")
	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("pre-occupying socket: %v", err)
	}
	defer lis.Close() //nolint:errcheck

	runner := func(_, _ string) (string, error) {
		return `[{"id":"b3","title":"existing work"}]`, nil
	}
	var stdout, stderr bytes.Buffer
	code := doHookWait("bd ready", "", 5*time.Second, socketPath, runner, &stdout, &stderr)
	if code != 0 {
		t.Errorf("doHookWait(socket in use, has work) = %d, want 0", code)
	}
}

// Criterion 6: no --wait flag — existing cmdHookWithOptions behavior unchanged.
func TestCmdHookWithOptions_NoWait_BackwardCompat(t *testing.T) {
	runner := func(_, _ string) (string, error) {
		return `[{"id":"b4","title":"task"}]`, nil
	}
	// doHook is the direct path when WaitDuration == 0.
	var stdout, stderr bytes.Buffer
	code := doHook("bd ready", "", false, runner, &stdout, &stderr)
	if code != 0 {
		t.Errorf("doHook(has work) = %d, want 0", code)
	}
}

// Invalid --wait duration is rejected by cobra flag parsing; test via newHookCmd.
func TestHookCmd_InvalidWaitDuration(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cmd := newHookCmd(&stdout, &stderr)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--wait", "not-a-duration", "agent"})
	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for invalid --wait duration, got nil")
	}
}
