package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// TestServerReachableReflectsDoltExit verifies that server_reachable — the
// guard introduced so a transiently-unreachable managed Dolt server is not
// mistaken for a missing/unregistered schema (which would trigger a
// DESTRUCTIVE --force reinit and abort city init) — succeeds exactly when
// the dolt client can reach the server and fails when it cannot.
//
// Without this guard, a momentary blip (port drift, an exclusive lock held
// by a stale dolt process, a slow server start) makes bd_runtime_schema_ready
// / ensure_database_registered return false, and the init path force-reinits
// a healthy store.
func TestServerReachableReflectsDoltExit(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available; skipping shell-function test")
	}

	root := repoRootForLint(t)
	scriptPath := filepath.Join(root, "examples", "bd", "assets", "scripts", "gc-beads-bd.sh")
	scriptBytes, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	src := string(scriptBytes)
	serverSQL := extractShellFunction(t, src, "server_sql")
	serverReachable := extractShellFunction(t, src, "server_reachable")

	cases := []struct {
		name     string
		doltExit int
		wantOK   bool
	}{
		{"server_up_exit0_reachable", 0, true},
		{"server_down_exit1_unreachable", 1, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			binDir := t.TempDir()
			writeFakeExitDolt(t, binDir, tc.doltExit)

			// connect_host is overridden so the test exercises only the
			// reachability decision, not host resolution.
			script := "connect_host() { printf '127.0.0.1'; }\n" +
				serverSQL + "\n" +
				serverReachable + "\n" +
				"server_reachable\n"

			cmd := exec.Command("bash", "-c", script)
			cmd.Env = append(os.Environ(),
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"DOLT_PORT=42188",
				"DOLT_USER=root",
				"DOLT_PASSWORD=",
			)
			runErr := cmd.Run()
			gotOK := runErr == nil
			if gotOK != tc.wantOK {
				t.Fatalf("server_reachable ok=%v, want %v (fake dolt exit %d)", gotOK, tc.wantOK, tc.doltExit)
			}
		})
	}
}

func writeFakeExitDolt(t *testing.T, dir string, exitCode int) {
	t.Helper()
	p := filepath.Join(dir, "dolt")
	body := "#!/bin/sh\nexit " + strconv.Itoa(exitCode) + "\n"
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake dolt: %v", err)
	}
}

func TestServerSQLUsesNeutralWorkingDirectory(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available; skipping shell-function test")
	}

	root := repoRootForLint(t)
	scriptPath := filepath.Join(root, "examples", "bd", "assets", "scripts", "gc-beads-bd.sh")
	scriptBytes, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	serverSQL := extractShellFunction(t, string(scriptBytes), "server_sql")

	binDir := t.TempDir()
	pwdFile := filepath.Join(t.TempDir(), "pwd.txt")
	fakeDolt := filepath.Join(binDir, "dolt")
	body := "#!/bin/sh\npwd > " + strconv.Quote(pwdFile) + "\n"
	if err := os.WriteFile(fakeDolt, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake dolt: %v", err)
	}

	poisonCWD := t.TempDir()
	script := "connect_host() { printf '127.0.0.1'; }\n" +
		serverSQL + "\n" +
		"server_sql 'SELECT 1'\n"
	cmd := exec.Command("bash", "-c", script)
	cmd.Dir = poisonCWD
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"DOLT_PORT=42188",
		"DOLT_USER=root",
		"DOLT_PASSWORD=",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("server_sql failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(pwdFile)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := filepath.Clean(string(bytes.TrimSpace(data))), filepath.Clean(os.TempDir()); !samePath(got, want) {
		t.Fatalf("server_sql cwd = %q, want neutral temp dir %q", got, want)
	}
}
