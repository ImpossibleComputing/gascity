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

	"github.com/BurntSushi/toml"
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

func TestWorkerSensitiveToolGuardDeniesWorkerUnclassifiedSecretsAndProfiles(t *testing.T) {
	guard := writeCoreAsset(t, "assets/scripts/worker-sensitive-tool-guard.sh")
	workerEnv := []string{
		"GC_AGENT=worker-pool-7",
		"GC_SESSION_NAME=pool-worker-test",
		"GC_AGENT_ROLE=worker",
	}

	for _, tt := range []struct {
		name string
		args []string
	}{
		{name: "unclassified sanctioned secret", args: []string{"--kind", "secret", "--secret-class", "project-alpha", "--", "get", "build-token"}},
		{name: "unclassified shared browser profile", args: []string{"--kind", "browser", "--profile", "research-profile", "--", "open", "dashboard"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "called")
			fake := writeFakeTool(t, marker)
			args := append([]string{}, tt.args...)
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
		"assets/scripts/worker-process-listing-guard.sh",
		"assets/scripts/worker-secret-env-preflight.sh",
		"assets/worker-sensitive-tools/bin/gws",
		"assets/worker-sensitive-tools/bin/secret-peek",
		"assets/worker-sensitive-tools/bin/mayor-browser",
		"assets/worker-sensitive-tools/bin/ps",
	}
	for _, path := range required {
		data, err := fs.ReadFile(PackFS, path)
		if err != nil {
			t.Fatalf("core pack missing Phase-1 credential guard asset %s: %v", path, err)
		}
		if path == "assets/worker-sensitive-tools/bin/ps" {
			if !strings.Contains(string(data), "worker-process-listing-guard.sh") {
				t.Fatalf("%s does not route through worker-process-listing-guard.sh", path)
			}
			continue
		}
		if path != "assets/scripts/worker-sensitive-tool-path.sh" &&
			path != "assets/scripts/worker-process-listing-guard.sh" &&
			path != "assets/scripts/worker-secret-env-preflight.sh" &&
			!strings.Contains(string(data), "worker-sensitive-tool-guard.sh") {
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

func TestSupervisorEnvSurfaceAuditRedactsValues(t *testing.T) {
	audit := writeCoreAsset(t, "assets/scripts/supervisor-env-surface-audit.sh")
	plist := filepath.Join(t.TempDir(), "com.gascity.supervisor.plist")
	secretOpenAI := "sk-proj-fake-secret-value-must-not-appear"
	secretGemini := "AIza-fake-secret-value-must-not-appear"
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.gascity.supervisor</string>
  <key>ProgramArguments</key><array><string>/opt/homebrew/bin/gc</string><string>supervisor</string></array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>OPENAI_API_KEY</key><string>%s</string>
    <key>GEMINI_API_KEY</key><string>%s</string>
    <key>PATH</key><string>/usr/bin:/bin</string>
  </dict>
</dict>
</plist>
`, secretOpenAI, secretGemini)
	if err := os.WriteFile(plist, []byte(body), 0o600); err != nil {
		t.Fatalf("writing plist: %v", err)
	}

	cmd := exec.Command(audit, "--plist", plist)
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("audit failed: %v; stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"OPENAI_API_KEY", "GEMINI_API_KEY", "PATH", "value=REDACTED", "values_not_printed_hashed_or_persisted"} {
		if !strings.Contains(out, want) {
			t.Fatalf("audit output missing %q: %q", want, out)
		}
	}
	for _, leak := range []string{secretOpenAI, secretGemini, "sk-proj", "AIza-fake"} {
		if strings.Contains(out, leak) || strings.Contains(stderr.String(), leak) {
			t.Fatalf("audit leaked secret fragment %q; stdout=%q stderr=%q", leak, out, stderr.String())
		}
	}
}

func TestCorePackIncludesWorkerCredentialSandboxAssets(t *testing.T) {
	for _, path := range []string{
		"assets/security/worker-credential-deny.sb",
		"assets/scripts/worker-credential-sandbox-preflight.sh",
		"assets/scripts/supervisor-env-surface-audit.sh",
		"assets/scripts/worker-process-listing-guard.sh",
		"assets/scripts/worker-secret-env-preflight.sh",
		"assets/worker-sensitive-tools/bin/ps",
	} {
		if _, err := fs.Stat(PackFS, path); err != nil {
			t.Fatalf("core pack missing Phase-2 worker credential sandbox asset %s: %v", path, err)
		}
	}
}

func TestWorkerCredentialIsolationCoversMaintenanceAgents(t *testing.T) {
	for _, tt := range []struct {
		name string
		path string
	}{
		{name: "bd dog", path: filepath.Join("..", "..", "..", "..", "examples", "bd", "dolt", "agents", "dog", "agent.toml")},
		{name: "control dispatcher", path: filepath.Join("agents", "control-dispatcher", "agent.toml")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("reading %s: %v", tt.path, err)
			}
			if !strings.Contains(string(data), `sandbox_profile = "//.gc/security/worker-credential-deny.sb"`) {
				t.Fatalf("%s must not be silently unsandboxed; content:\n%s", tt.name, data)
			}
			var parsed struct {
				Env map[string]string `toml:"env"`
			}
			if _, err := toml.Decode(string(data), &parsed); err != nil {
				t.Fatalf("Decode(%s): %v", tt.path, err)
			}
			assertNonLLMMaintenanceSecretEnvScrubbed(t, parsed.Env)
		})
	}
}

func TestWorkerCredentialSandboxProfileDeniesCredentialFileOps(t *testing.T) {
	data, err := fs.ReadFile(PackFS, "assets/security/worker-credential-deny.sb")
	if err != nil {
		t.Fatalf("reading worker credential sandbox profile: %v", err)
	}
	profile := string(data)
	for _, param := range []string{
		"GWS_CONFIG",
		"GCLOUD_CONFIG",
		"AWS_CONFIG",
		"CITY_SECRETS",
		"HOME_SECRETS",
		"HOME_SSH",
	} {
		want := fmt.Sprintf(`(deny file* (subpath (param "%s")))`, param)
		if !strings.Contains(profile, want) {
			t.Fatalf("sandbox profile missing %q in:\n%s", want, profile)
		}
	}
	if want := `(deny file-read* (subpath (param "BROWSER_PROFILES")))`; !strings.Contains(profile, want) {
		t.Fatalf("sandbox profile missing browser read deny %q in:\n%s", want, profile)
	}
	if want := `(deny file-write* (subpath (param "GC_SECURITY")))`; !strings.Contains(profile, want) {
		t.Fatalf("sandbox profile missing security-profile write deny %q in:\n%s", want, profile)
	}
	for _, forbidden := range []string{"GH_CONFIG", "GIT_CONFIG", ".config/gh", ".config/git", ".gitconfig"} {
		if strings.Contains(profile, forbidden) {
			t.Fatalf("sandbox profile must not deny HTTPS publication config %q in:\n%s", forbidden, profile)
		}
	}
}

func TestWorkerSecretEnvPreflightDeniesForbiddenNamesWithoutLeakingValues(t *testing.T) {
	preflight := writeCoreAsset(t, "assets/scripts/worker-secret-env-preflight.sh")
	secretOpenAI := "sk-proj-fake-secret-value-must-not-appear"
	secretGemini := "AIza-fake-secret-value-must-not-appear"
	secretClaude := "claude-oauth-fake-secret-value-must-not-appear"
	secretGoogle := "google-fake-secret-value-must-not-appear"
	secretAWS := "aws-fake-secret-value-must-not-appear"
	got := runGuard(t, preflight, []string{
		"OPENAI_API_KEY=" + secretOpenAI,
		"GEMINI_API_KEY=" + secretGemini,
		"ANTHROPIC_AUTH_TOKEN=" + secretClaude,
		"CLAUDE_CODE_OAUTH_TOKEN=" + secretClaude,
		"GOOGLE_API_KEY=" + secretGoogle,
		"AWS_SECRET_ACCESS_KEY=" + secretAWS,
		"GC_INSTANCE_TOKEN=not-in-default-forbid-list",
	})
	if got.code != 1 {
		t.Fatalf("preflight exit = %d, want 1; stdout=%q stderr=%q", got.code, got.stdout, got.stderr)
	}
	for _, want := range []string{
		"FAIL forbidden_env_name=OPENAI_API_KEY value=REDACTED",
		"FAIL forbidden_env_name=GEMINI_API_KEY value=REDACTED",
		"FAIL forbidden_env_name=ANTHROPIC_AUTH_TOKEN value=REDACTED",
		"FAIL forbidden_env_name=CLAUDE_CODE_OAUTH_TOKEN value=REDACTED",
		"FAIL forbidden_env_name=GOOGLE_API_KEY value=REDACTED",
		"FAIL forbidden_env_name=AWS_SECRET_ACCESS_KEY value=REDACTED",
		"PASS absent_env_name=GITHUB_TOKEN",
		"forbidden_present_count=6",
		"values_not_printed_hashed_or_persisted",
	} {
		if !strings.Contains(got.stdout, want) {
			t.Fatalf("preflight stdout missing %q: %q", want, got.stdout)
		}
	}
	for _, leak := range []string{secretOpenAI, secretGemini, secretClaude, secretGoogle, secretAWS, "sk-proj", "AIza-fake", "claude-oauth", "google-fake", "aws-fake"} {
		if strings.Contains(got.stdout, leak) || strings.Contains(got.stderr, leak) {
			t.Fatalf("preflight leaked secret fragment %q; stdout=%q stderr=%q", leak, got.stdout, got.stderr)
		}
	}
	if strings.Contains(got.stdout, "GC_INSTANCE_TOKEN") {
		t.Fatalf("preflight should not flag GC_INSTANCE_TOKEN by default: %q", got.stdout)
	}
}

func TestWorkerSecretEnvPreflightAllowsExplicitBrokerTokenName(t *testing.T) {
	preflight := writeCoreAsset(t, "assets/scripts/worker-secret-env-preflight.sh")
	got := runGuard(t, preflight,
		[]string{"WORKER_LLM_BROKER_TOKEN=fake-worker-scoped-token"},
		"--forbid", "WORKER_LLM_BROKER_TOKEN",
		"--allow", "WORKER_LLM_BROKER_TOKEN",
	)
	if got.code != 0 {
		t.Fatalf("preflight exit = %d, want 0; stdout=%q stderr=%q", got.code, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stdout, "SKIP allowed_env_name=WORKER_LLM_BROKER_TOKEN value=REDACTED") {
		t.Fatalf("preflight stdout missing allowed broker token line: %q", got.stdout)
	}
	if strings.Contains(got.stdout, "fake-worker-scoped-token") || strings.Contains(got.stderr, "fake-worker-scoped-token") {
		t.Fatalf("preflight leaked allowed token value; stdout=%q stderr=%q", got.stdout, got.stderr)
	}
}

func TestWorkerCredentialSandboxPreflightProbesMissingPathCreate(t *testing.T) {
	data, err := fs.ReadFile(PackFS, "assets/scripts/worker-credential-sandbox-preflight.sh")
	if err != nil {
		t.Fatalf("reading worker credential sandbox preflight: %v", err)
	}
	script := string(data)
	for _, want := range []string{
		`if [ ! -e "$path" ]; then`,
		`run_sandbox sh -c 'mkdir "$1"' _ "$path"`,
		`FAIL deny $label: sandbox allowed create`,
		`rmdir "$path"`,
		`must_deny_write()`,
		`must_deny_write "$label" "$path"`,
		`must_deny_write "gc security" "$city/.gc/security"`,
		`must_allow_https_push()`,
		`git -C "$city" push --dry-run "$https_push_remote" "HEAD:$https_push_ref"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("preflight missing %q in:\n%s", want, script)
		}
	}
}

func TestWorkerCredentialIsolationRunbookDocumentsHTTPSPublication(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "docs", "runbooks", "worker-credential-isolation-phase2.md"))
	if err != nil {
		t.Fatalf("reading worker credential isolation runbook: %v", err)
	}
	runbook := string(data)
	for _, want := range []string{
		"HTTPS plus the existing GitHub CLI token",
		"SSH private keys stay fully denied",
		"Git publication must work over HTTPS",
		"~/.config/gh",
		"~/.gitconfig",
		"~/.config/git",
		"credential path reads, writes, and creates must fail",
	} {
		if !strings.Contains(runbook, want) {
			t.Fatalf("runbook missing %q in:\n%s", want, runbook)
		}
	}
}

func TestWorkerProcessListingGuardDeniesBroadAndFullCommandForms(t *testing.T) {
	guard := writeCoreAsset(t, "assets/scripts/worker-process-listing-guard.sh")
	workerEnv := []string{
		"GC_AGENT=claude-sonnet-1",
		"GC_SESSION_NAME=gt-wisp-worker-test",
		"GC_AGENT_ROLE=worker",
	}

	for _, tt := range []struct {
		name string
		args []string
	}{
		{name: "classic aux", args: []string{"aux"}},
		{name: "full process table", args: []string{"-ef"}},
		{name: "env wide", args: []string{"axeww"}},
		{name: "args output", args: []string{"-Ao", "pid,args"}},
		{name: "command output", args: []string{"-p", "123", "-o", "pid,command"}},
		{name: "process specific full format", args: []string{"-p", "123", "-f"}},
		{name: "wide process specific", args: []string{"-ww", "-p", "123"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "called")
			fake := writeFakeTool(t, marker)
			args := append([]string{"--real", fake, "--"}, tt.args...)
			got := runGuard(t, guard, workerEnv, args...)
			if got.code != 78 {
				t.Fatalf("guard exit = %d, want 78; stdout=%q stderr=%q", got.code, got.stdout, got.stderr)
			}
			if !strings.Contains(got.stderr, "process listing guard deny") {
				t.Fatalf("deny stderr missing guard message: %q", got.stderr)
			}
			if strings.Contains(got.stderr, "OPENAI") || strings.Contains(got.stderr, "GEMINI") || strings.Contains(got.stderr, "fake-secret-value") {
				t.Fatalf("deny stderr leaked secret-looking text: %q", got.stderr)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("fake ps marker exists after denied request; err=%v", err)
			}
		})
	}
}

func TestWorkerProcessListingGuardAllowsNarrowPidCommForm(t *testing.T) {
	guard := writeCoreAsset(t, "assets/scripts/worker-process-listing-guard.sh")
	marker := filepath.Join(t.TempDir(), "called")
	fake := writeFakeTool(t, marker)
	got := runGuard(t, guard,
		[]string{"GC_AGENT=claude-sonnet-1", "GC_AGENT_ROLE=worker", "GC_SESSION_NAME=gt-wisp-worker-test"},
		"--real", fake, "--", "-p", "123", "-o", "pid,comm=",
	)
	if got.code != 0 {
		t.Fatalf("guard exit = %d, want 0; stdout=%q stderr=%q", got.code, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stdout, "fake-ok:-p 123 -o pid,comm=") {
		t.Fatalf("fake ps stdout missing pass-through args: %q", got.stdout)
	}
	markerData, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("reading fake marker: %v", err)
	}
	if !strings.Contains(string(markerData), "called:-p 123 -o pid,comm=") {
		t.Fatalf("fake marker missing pass-through args: %q", markerData)
	}
}

func TestWorkerProcessListingGuardAllowsPrivilegedBroadListing(t *testing.T) {
	guard := writeCoreAsset(t, "assets/scripts/worker-process-listing-guard.sh")
	marker := filepath.Join(t.TempDir(), "called")
	fake := writeFakeTool(t, marker)
	got := runGuard(t, guard,
		[]string{"GC_AGENT=paul", "GC_AGENT_ROLE=ops", "GC_SESSION_NAME=paul-review-test"},
		"--real", fake, "--", "aux",
	)
	if got.code != 0 {
		t.Fatalf("guard exit = %d, want 0; stdout=%q stderr=%q", got.code, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stdout, "fake-ok:aux") {
		t.Fatalf("fake ps stdout missing pass-through args: %q", got.stdout)
	}
}
