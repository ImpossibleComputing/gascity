package dolt_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeCompactEnv returns the minimal env for running compact/run.sh without
// a real Dolt server. GC_DOLT_MANAGED_LOCAL is left unset so the script skips
// the managed-port lookup and uses the explicit port directly.
func writeCompactEnv(t *testing.T, binDir, cityPath, dataDir, port string, extra ...string) []string {
	t.Helper()
	root := repoRoot(t)
	env := append(filteredEnv(
		"GC_PACK_DIR",
		"GC_CITY_PATH",
		"GC_DOLT_DATA_DIR",
		"GC_DOLT_PORT",
		"GC_DOLT_HOST",
		"GC_DOLT_USER",
		"GC_DOLT_PASSWORD",
		"GC_DOLT_MANAGED_LOCAL",
		"GC_DOLT_COMPACT_WARN_SECS",
		"GC_ESCALATE_SCRIPT",
		"DOLT_ESCALATE_SCRIPT",
		"PATH",
	),
		"GC_PACK_DIR="+root,
		"GC_CITY_PATH="+cityPath,
		"GC_DOLT_DATA_DIR="+dataDir,
		"GC_DOLT_PORT="+port,
		"GC_DOLT_HOST=127.0.0.1",
		"GC_DOLT_USER=root",
		"GC_DOLT_PASSWORD=",
		// GC_DOLT_MANAGED_LOCAL=0 bypasses the managed-port runtime check so the
		// script reaches parameter validation without a real Dolt server.
		"GC_DOLT_MANAGED_LOCAL=0",
		"PATH="+binDir+":"+os.Getenv("PATH"),
	)
	return append(env, extra...)
}

// TestCompactDurationAlert_InvalidWarnSecs verifies that compact exits 2 when
// GC_DOLT_COMPACT_WARN_SECS is set to an invalid value (0 or non-integer).
func TestCompactDurationAlert_InvalidWarnSecs(t *testing.T) {
	root := repoRoot(t)
	binDir := t.TempDir()
	cityPath := t.TempDir()
	dataDir := filepath.Join(cityPath, ".beads", "dolt")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// fake dolt binary — never reached for this code path
	writeExecutable(t, filepath.Join(binDir, "dolt"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(binDir, "gc"), "#!/bin/sh\nexit 0\n")

	cases := []struct {
		name  string
		value string
	}{
		{"zero", "0"},
		{"negative", "-1"},
		{"non-integer", "notanumber"},
		{"float", "1.5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("sh", filepath.Join(root, "commands", "compact", "run.sh"))
			cmd.Env = writeCompactEnv(t, binDir, cityPath, dataDir, "3306",
				"GC_DOLT_COMPACT_WARN_SECS="+tc.value)
			out, err := cmd.CombinedOutput()
			var exitErr *exec.ExitError
			if err == nil {
				t.Fatalf("expected exit 2 for WARN_SECS=%s, got success\n%s", tc.value, out)
			}
			ee := &exec.ExitError{}
			if errors.As(err, &ee) {
				exitErr = ee
			}
			if exitErr == nil || exitErr.ExitCode() != 2 {
				t.Fatalf("expected exit 2 for WARN_SECS=%s, got: %v\n%s", tc.value, err, out)
			}
			if !strings.Contains(string(out), "GC_DOLT_COMPACT_WARN_SECS") {
				t.Errorf("expected WARN_SECS in error output, got: %s", out)
			}
		})
	}
}

// TestCompactDurationAlert_DefaultValid verifies the default (300) is accepted
// without error during early validation.
func TestCompactDurationAlert_DefaultValid(t *testing.T) {
	root := repoRoot(t)
	binDir := t.TempDir()
	cityPath := t.TempDir()
	dataDir := filepath.Join(cityPath, ".beads", "dolt")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// fake gc and dolt: dolt exits non-zero so the script bails early with
	// a recognizable error — but we only care that it doesn't exit 2 (bad config).
	writeExecutable(t, filepath.Join(binDir, "dolt"), "#!/bin/sh\nexit 1\n")
	writeExecutable(t, filepath.Join(binDir, "gc"), "#!/bin/sh\nprintf 'gc %s\\n' \"$*\"; exit 1\n")

	cmd := exec.Command("sh", filepath.Join(root, "commands", "compact", "run.sh"))
	cmd.Env = writeCompactEnv(t, binDir, cityPath, dataDir, "3306")
	// no GC_DOLT_COMPACT_WARN_SECS set — should use 300 default and not exit 2
	out, err := cmd.CombinedOutput()
	if err != nil {
		exitErr := &exec.ExitError{}
		ok := errors.As(err, &exitErr)
		if ok && exitErr.ExitCode() == 2 {
			t.Fatalf("exit 2 (invalid config) with default WARN_SECS unset:\n%s", out)
		}
		// any other non-zero exit is expected (no real Dolt server)
		return
	}
	// success is fine too if the script short-circuits before Dolt calls
}

// TestCompactDurationAlertShell runs the standalone shell test for compact
// duration alert behavior. The shell test exercises the alert boundary
// (fires on elapsed >= warn_secs, silent below) using a mock escalate.sh.
func TestCompactDurationAlertShell(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skipf("bash not found: %v", err)
	}
	root := repoRoot(t)
	testScript := filepath.Join(root, "compact_duration_alert_test.sh")
	if _, err := os.Stat(testScript); err != nil {
		t.Skipf("shell test not found at %s: %v", testScript, err)
	}
	cmd := exec.Command("bash", testScript)
	cmd.Env = append(os.Environ(), "PACK_DIR="+root)
	out, err := cmd.CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatalf("shell test failed: %v\n%s", err, out)
	}
}
