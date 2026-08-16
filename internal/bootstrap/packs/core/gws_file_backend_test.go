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

type gwsFileBackendResult struct {
	stdout string
	stderr string
	code   int
}

func writeGWSFileBackendAsset(t *testing.T, assetPath string) string {
	t.Helper()
	data, err := fs.ReadFile(PackFS, assetPath)
	if err != nil {
		t.Fatalf("reading %s: %v", assetPath, err)
	}
	path := filepath.Join(t.TempDir(), filepath.Base(assetPath))
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatalf("writing temp asset: %v", err)
	}
	return path
}

func runGWSFileBackendCommand(t *testing.T, path string, env []string, args ...string) gwsFileBackendResult {
	t.Helper()
	cmd := exec.Command(path, args...)
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
			t.Fatalf("running %s: %v; stderr=%s", path, err, stderr.String())
		}
	}
	return gwsFileBackendResult{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

func writeFakeGWSBackendRecorder(t *testing.T, marker string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gws")
	body := "#!/usr/bin/env bash\n" +
		"set -euo pipefail\n" +
		"printf 'backend=%s args=%s\\n' \"${GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND:-}\" \"$*\" > " + shellQuote(marker) + "\n" +
		"printf 'fake-gws:%s\\n' \"$*\"\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("writing fake gws: %v", err)
	}
	return path
}

func TestGWSFileBackendExecForcesFileBackendAndPreservesArgs(t *testing.T) {
	wrapper := writeGWSFileBackendAsset(t, "assets/scripts/gws-file-backend-exec.sh")
	marker := filepath.Join(t.TempDir(), "called")
	fake := writeFakeGWSBackendRecorder(t, marker)

	got := runGWSFileBackendCommand(t, wrapper, []string{
		"GWS_FILE_BACKEND_REAL_GWS=" + fake,
		"GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND=keyring",
	}, "gmail", "+triage", "--query", "is:unread")
	if got.code != 0 {
		t.Fatalf("wrapper exit=%d want 0 stdout=%q stderr=%q", got.code, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stdout, "fake-gws:gmail +triage --query is:unread") {
		t.Fatalf("wrapper did not preserve gws args on stdout: %q", got.stdout)
	}
	markerData, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("fake gws marker missing: %v", err)
	}
	if !strings.Contains(string(markerData), "backend=file args=gmail +triage --query is:unread") {
		t.Fatalf("wrapper did not force file backend or preserve args: %q", markerData)
	}
}

func TestGWSFileBackendAuditFlagsUnguardedCallSites(t *testing.T) {
	audit := writeGWSFileBackendAsset(t, "assets/scripts/gws-file-backend-audit.sh")
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tools", "unsafe.sh"), []byte("#!/usr/bin/env bash\ngws gmail +triage\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tools", "safe.sh"), []byte("#!/usr/bin/env bash\nGOOGLE_WORKSPACE_CLI_KEYRING_BACKEND=file gws auth status\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := runGWSFileBackendCommand(t, audit, nil, "--root", root, "--launchagents", filepath.Join(root, "missing-launchagents"))
	if got.code != 1 {
		t.Fatalf("audit exit=%d want 1 stdout=%q stderr=%q", got.code, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stderr, "missing file backend guard") || !strings.Contains(got.stderr, "unsafe.sh") {
		t.Fatalf("audit stderr missing unsafe callsite: %q", got.stderr)
	}
	if strings.Contains(got.stderr, "/safe.sh") {
		t.Fatalf("audit flagged protected callsite: %q", got.stderr)
	}
}

func TestGWSFileBackendAuditAcceptsWrapperAndPlistEnv(t *testing.T) {
	audit := writeGWSFileBackendAsset(t, "assets/scripts/gws-file-backend-audit.sh")
	root := t.TempDir()
	for _, dir := range []string{"tools", "orders"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "tools", "wrapped.sh"), []byte("#!/usr/bin/env bash\n/opt/gc/gws-file-backend-exec.sh gmail +send\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>ProgramArguments</key><array><string>/opt/homebrew/bin/gws</string><string>auth</string><string>status</string></array>
<key>EnvironmentVariables</key><dict>
<key>GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND</key><string>file</string>
</dict>
</dict></plist>`
	if err := os.WriteFile(filepath.Join(root, "orders", "safe.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}

	got := runGWSFileBackendCommand(t, audit, nil, "--root", root, "--launchagents", filepath.Join(root, "missing-launchagents"))
	if got.code != 0 {
		t.Fatalf("audit exit=%d want 0 stdout=%q stderr=%q", got.code, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stdout, "2 apparent gws call sites protected") {
		t.Fatalf("audit stdout missing protected count: %q", got.stdout)
	}
}

func TestCorePackIncludesGWSFileBackendHardeningAssets(t *testing.T) {
	for _, path := range []string{
		"assets/scripts/gws-file-backend-exec.sh",
		"assets/scripts/gws-file-backend-audit.sh",
	} {
		data, err := fs.ReadFile(PackFS, path)
		if err != nil {
			t.Fatalf("core pack missing %s: %v", path, err)
		}
		if !strings.Contains(string(data), "GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND") {
			t.Fatalf("%s does not reference the file backend control", path)
		}
	}
}
