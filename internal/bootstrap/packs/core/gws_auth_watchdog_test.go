package core

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type gwsWatchdogResult struct {
	stdout string
	stderr string
	code   int
}

func writeGWSAuthWatchdogAsset(t *testing.T) string {
	t.Helper()
	data, err := fs.ReadFile(PackFS, "assets/scripts/gws-auth-liveness-watchdog.sh")
	if err != nil {
		t.Fatalf("reading gws watchdog asset: %v", err)
	}
	path := filepath.Join(t.TempDir(), "gws-auth-liveness-watchdog.sh")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatalf("writing gws watchdog: %v", err)
	}
	return path
}

func runGWSAuthWatchdog(t *testing.T, script string, env []string, args ...string) gwsWatchdogResult {
	t.Helper()
	cmd := exec.Command(script, args...)
	cmd.Env = append([]string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin"}, env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("running gws watchdog: %v; stderr=%s", err, stderr.String())
		}
	}
	return gwsWatchdogResult{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

func makeGWSConfig(t *testing.T, withKey bool) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "credentials.enc"), []byte("fake-encrypted-token"), 0o600); err != nil {
		t.Fatalf("writing fake credentials.enc: %v", err)
	}
	if withKey {
		if err := os.WriteFile(filepath.Join(dir, ".encryption_key"), []byte("fake-file-backend-key"), 0o600); err != nil {
			t.Fatalf("writing fake .encryption_key: %v", err)
		}
	}
	return dir
}

func writeFakeGWSStatus(t *testing.T, marker string, rc int, stdout string, stderr string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gws")
	body := "#!/usr/bin/env bash\n" +
		"set -euo pipefail\n" +
		"printf 'backend=%s args=%s\\n' \"${GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND:-}\" \"$*\" > " + shellQuote(marker) + "\n" +
		"printf %s " + shellQuote(stdout) + "\n" +
		"printf %s " + shellQuote(stderr) + " >&2\n" +
		"exit " + string(rune('0'+rc)) + "\n"
	if rc < 0 || rc > 9 {
		t.Fatalf("test helper only supports one-digit rc, got %d", rc)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("writing fake gws: %v", err)
	}
	return path
}

func writeFakeGCRecorder(t *testing.T, logPath string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gc")
	body := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> " + shellQuote(logPath) + "\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("writing fake gc: %v", err)
	}
	return path
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func TestGWSAuthWatchdogPassesWithFileBackendOAuth2(t *testing.T) {
	script := writeGWSAuthWatchdogAsset(t)
	config := makeGWSConfig(t, true)
	marker := filepath.Join(t.TempDir(), "gws-called")
	gws := writeFakeGWSStatus(t, marker, 0, `{"auth_method":"oauth2"}`, "")
	gcLog := filepath.Join(t.TempDir(), "gc.log")
	gc := writeFakeGCRecorder(t, gcLog)

	got := runGWSAuthWatchdog(t, script, []string{
		"GWS_AUTH_WATCHDOG_CONFIG_DIR=" + config,
		"GWS_AUTH_WATCHDOG_GWS=" + gws,
		"GWS_AUTH_WATCHDOG_GC=" + gc,
		"GWS_AUTH_WATCHDOG_STATE=" + filepath.Join(t.TempDir(), "state"),
	})
	if got.code != 0 {
		t.Fatalf("watchdog exit=%d want 0 stdout=%q stderr=%q", got.code, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stdout, "auth_method=oauth2") {
		t.Fatalf("watchdog stdout missing oauth2 success: %q", got.stdout)
	}
	markerData, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("fake gws marker missing: %v", err)
	}
	if !strings.Contains(string(markerData), "backend=file args=auth status") {
		t.Fatalf("gws was not invoked with file backend auth status: %q", markerData)
	}
	if _, err := os.Stat(gcLog); !os.IsNotExist(err) {
		t.Fatalf("gc alert log exists on healthy auth; err=%v", err)
	}
}

func TestGWSAuthWatchdogAlertsAndSkipsGWSWhenFileBackendKeyMissing(t *testing.T) {
	script := writeGWSAuthWatchdogAsset(t)
	config := makeGWSConfig(t, false)
	marker := filepath.Join(t.TempDir(), "gws-called")
	gws := writeFakeGWSStatus(t, marker, 0, `{"auth_method":"oauth2"}`, "")
	gcLog := filepath.Join(t.TempDir(), "gc.log")
	gc := writeFakeGCRecorder(t, gcLog)

	got := runGWSAuthWatchdog(t, script, []string{
		"GWS_AUTH_WATCHDOG_CONFIG_DIR=" + config,
		"GWS_AUTH_WATCHDOG_GWS=" + gws,
		"GWS_AUTH_WATCHDOG_GC=" + gc,
		"GWS_AUTH_WATCHDOG_STATE=" + filepath.Join(t.TempDir(), "state"),
		"GWS_AUTH_WATCHDOG_COOLDOWN_SECONDS=0",
	})
	if got.code != 2 {
		t.Fatalf("watchdog exit=%d want 2 stdout=%q stderr=%q", got.code, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stderr, ".encryption_key is missing") || !strings.Contains(got.stderr, "refusing an active gws probe") {
		t.Fatalf("stderr missing safe missing-key failure: %q", got.stderr)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("fake gws was invoked despite missing .encryption_key; err=%v", err)
	}
	logData, err := os.ReadFile(gcLog)
	if err != nil {
		t.Fatalf("fake gc alert log missing: %v", err)
	}
	if !strings.Contains(string(logData), "session nudge mayor") || !strings.Contains(string(logData), "mail send mayor") {
		t.Fatalf("alert did not use both non-gws gc paths: %q", logData)
	}
}

func TestGWSAuthWatchdogAlertsOnNonOAuth2Method(t *testing.T) {
	script := writeGWSAuthWatchdogAsset(t)
	config := makeGWSConfig(t, true)
	marker := filepath.Join(t.TempDir(), "gws-called")
	gws := writeFakeGWSStatus(t, marker, 0, `{"auth_method":"service_account"}`, "")
	gcLog := filepath.Join(t.TempDir(), "gc.log")
	gc := writeFakeGCRecorder(t, gcLog)

	got := runGWSAuthWatchdog(t, script, []string{
		"GWS_AUTH_WATCHDOG_CONFIG_DIR=" + config,
		"GWS_AUTH_WATCHDOG_GWS=" + gws,
		"GWS_AUTH_WATCHDOG_GC=" + gc,
		"GWS_AUTH_WATCHDOG_STATE=" + filepath.Join(t.TempDir(), "state"),
	})
	if got.code != 2 {
		t.Fatalf("watchdog exit=%d want 2 stdout=%q stderr=%q", got.code, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stderr, "auth_method=service_account, want oauth2") {
		t.Fatalf("stderr missing method failure: %q", got.stderr)
	}
	logData, err := os.ReadFile(gcLog)
	if err != nil {
		t.Fatalf("fake gc alert log missing: %v", err)
	}
	if !strings.Contains(string(logData), "gws auth liveness alarm") {
		t.Fatalf("alert subject missing from gc log: %q", logData)
	}
}
