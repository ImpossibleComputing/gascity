package core

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type guardResult struct {
	stdout string
	stderr string
	code   int
}

func writeCoreAsset(t *testing.T, assetPath string) string {
	t.Helper()
	data, err := fs.ReadFile(PackFS, assetPath)
	if err != nil {
		t.Fatalf("reading embedded %s: %v", assetPath, err)
	}
	path := filepath.Join(t.TempDir(), filepath.Base(assetPath))
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatalf("writing temp %s: %v", path, err)
	}
	return path
}

func writeFakeTool(t *testing.T, marker string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-tool")
	body := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
printf 'called:%%s\n' "$*" > %q
printf 'fake-ok:%%s\n' "$*"
`, marker)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("writing fake tool: %v", err)
	}
	return path
}

func runGuard(t *testing.T, guard string, env []string, args ...string) guardResult {
	t.Helper()
	cmd := exec.Command(guard, args...)
	cmd.Env = append([]string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin"}, env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("running guard: %v; stderr=%s", err, stderr.String())
		}
	}
	return guardResult{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

func assertDeniedBeforeFakeTool(t *testing.T, got guardResult, marker string) {
	t.Helper()
	if got.code != 77 {
		t.Fatalf("guard exit = %d, want 77; stdout=%q stderr=%q", got.code, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stderr, "credential guard deny") {
		t.Fatalf("deny stderr missing guard message: %q", got.stderr)
	}
	if strings.Contains(got.stderr, "keith") || strings.Contains(got.stderr, "fake-secret-value") {
		t.Fatalf("deny stderr leaked request detail/secret-looking text: %q", got.stderr)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("fake tool marker exists after denied request; err=%v", err)
	}
}

func TestWorkerSensitiveToolGuardDeniesWorkerFounderMail(t *testing.T) {
	guard := writeCoreAsset(t, "assets/scripts/worker-sensitive-tool-guard.sh")
	workerEnv := []string{
		"GC_AGENT=claude-sonnet-1",
		"GC_SESSION_NAME=gt-wisp-worker-test",
		"GC_AGENT_ROLE=worker",
	}

	for _, tt := range []struct {
		name string
		args []string
	}{
		{name: "gmail send", args: []string{"gmail", "+send", "--to", "keith@example.invalid"}},
		{name: "gmail reply", args: []string{"gmail", "+reply", "--thread", "founder-thread"}},
		{name: "gmail read", args: []string{"gmail", "+read", "--from", "founder"}},
		{name: "gmail triage", args: []string{"gmail", "+triage"}},
		{name: "users messages send", args: []string{"users", "messages", "send", "--user", "mayor"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "called")
			fake := writeFakeTool(t, marker)
			args := append([]string{"--kind", "gws", "--real", fake, "--"}, tt.args...)
			got := runGuard(t, guard, workerEnv, args...)
			assertDeniedBeforeFakeTool(t, got, marker)
		})
	}
}

func TestWorkerSensitiveToolGuardDeniesWorkerSensitiveSecretsAndProfiles(t *testing.T) {
	guard := writeCoreAsset(t, "assets/scripts/worker-sensitive-tool-guard.sh")
	workerEnv := []string{
		"GC_AGENT=worker-pool-7",
		"GC_SESSION_NAME=pool-worker-test",
		"GC_AGENT_ROLE=pool",
	}

	for _, tt := range []struct {
		name string
		args []string
	}{
		{name: "founder comms secret", args: []string{"--kind", "secret", "--secret-class", "founder-comms", "--", "get", "mayor-gmail"}},
		{name: "payment card secret", args: []string{"--kind", "secret", "--secret-class", "payment-card", "--", "get", "vendor-card"}},
		{name: "zai secret", args: []string{"--kind", "secret", "--secret-class", "z.ai", "--", "get", "api-key"}},
		{name: "mistral secret", args: []string{"--kind", "secret", "--secret-class", "mistral", "--", "get", "api-key"}},
		{name: "mayor browser", args: []string{"--kind", "browser", "--profile", "mayor-payment", "--", "open", "console"}},
		{name: "payment browser", args: []string{"--kind", "browser", "--profile", "stripe-card", "--", "open", "billing"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "called")
			fake := writeFakeTool(t, marker)
			args := append([]string{}, tt.args...)
			// Insert the fake real command immediately before the -- separator.
			insert := len(args)
			for i, arg := range args {
				if arg == "--" {
					insert = i
					break
				}
			}
			args = append(args[:insert], append([]string{"--real", fake}, args[insert:]...)...)
			got := runGuard(t, guard, workerEnv, args...)
			assertDeniedBeforeFakeTool(t, got, marker)
		})
	}
}

func TestWorkerSensitiveToolGuardAllowsMayorToFakeTool(t *testing.T) {
	guard := writeCoreAsset(t, "assets/scripts/worker-sensitive-tool-guard.sh")
	marker := filepath.Join(t.TempDir(), "called")
	fake := writeFakeTool(t, marker)
	got := runGuard(t, guard,
		[]string{"GC_AGENT=mayor", "GC_AGENT_ROLE=mayor", "GC_SESSION_NAME=mayor-review-test"},
		"--kind", "gws", "--real", fake, "--", "gmail", "+send", "--to", "keith@example.invalid",
	)
	if got.code != 0 {
		t.Fatalf("guard exit = %d, want 0; stdout=%q stderr=%q", got.code, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stdout, "fake-ok:gmail +send") {
		t.Fatalf("fake tool stdout missing pass-through args: %q", got.stdout)
	}
	markerData, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("reading fake marker: %v", err)
	}
	if !strings.Contains(string(markerData), "called:gmail +send") {
		t.Fatalf("fake marker missing pass-through args: %q", markerData)
	}
}

func TestWorkerSensitiveToolPhase1AssetsEmbedded(t *testing.T) {
	required := []string{
		"assets/scripts/worker-sensitive-tool-guard.sh",
		"assets/scripts/worker-sensitive-tool-path.sh",
		"assets/worker-sensitive-tools/bin/gws",
		"assets/worker-sensitive-tools/bin/secret-peek",
		"assets/worker-sensitive-tools/bin/mayor-browser",
	}
	for _, path := range required {
		data, err := fs.ReadFile(PackFS, path)
		if err != nil {
			t.Fatalf("core pack missing Phase-1 credential guard asset %s: %v", path, err)
		}
		if path != "assets/scripts/worker-sensitive-tool-path.sh" && !strings.Contains(string(data), "worker-sensitive-tool-guard.sh") {
			t.Fatalf("%s does not route through worker-sensitive-tool-guard.sh", path)
		}
	}

	pathHelper, err := fs.ReadFile(PackFS, "assets/scripts/worker-sensitive-tool-path.sh")
	if err != nil {
		t.Fatalf("reading path helper: %v", err)
	}
	if !strings.Contains(string(pathHelper), "worker-sensitive-tools/bin") {
		t.Fatal("path helper must prepend the guarded wrapper bin directory")
	}
}
