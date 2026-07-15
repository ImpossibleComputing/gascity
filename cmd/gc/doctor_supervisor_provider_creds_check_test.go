package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/doctor"
)

func writeSupervisorProviderCredsPlist(t *testing.T, body string) {
	t.Helper()
	path := supervisorLaunchdPlistPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir LaunchAgents: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write plist: %v", err)
	}
}

func TestSupervisorProviderCredsCheckWarnsWithNamesOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GC_HOME", filepath.Join(home, ".gc"))

	writeSupervisorProviderCredsPlist(t, `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
  <key>EnvironmentVariables</key>
  <dict>
    <key>GC_HOME</key><string>/tmp/gc-home</string>
    <key>OPENAI_API_KEY</key><string>sk-openai-secret</string>
    <key>GEMINI_API_KEY</key><string>gemini-secret</string>
    <key>CLAUDE_CODE_OAUTH_TOKEN</key><string>claude-oauth-secret</string>
    <key>CLAUDE_CONFIG_DIR</key><string>/tmp/claude-config</string>
    <key>UNRELATED_SECRET</key><string>do-not-report</string>
  </dict>
</dict></plist>`)

	result := newSupervisorProviderCredsCheck().Run(&doctor.CheckContext{})
	if result.Status != doctor.StatusWarning {
		t.Fatalf("Status = %v, want warning (message %q)", result.Status, result.Message)
	}
	if result.Severity != doctor.SeverityAdvisory {
		t.Fatalf("Severity = %v, want advisory", result.Severity)
	}
	joined := strings.Join(result.Details, "\n")
	for _, want := range []string{"CLAUDE_CODE_OAUTH_TOKEN", "GEMINI_API_KEY", "OPENAI_API_KEY"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("details missing %s: %q", want, joined)
		}
	}
	for _, leak := range []string{"sk-openai-secret", "gemini-secret", "claude-oauth-secret", "UNRELATED_SECRET", "do-not-report"} {
		if strings.Contains(joined, leak) || strings.Contains(result.Message, leak) || strings.Contains(result.FixHint, leak) {
			t.Fatalf("result leaked %q: message=%q details=%q fix=%q", leak, result.Message, joined, result.FixHint)
		}
	}
	if strings.Contains(joined, "CLAUDE_CONFIG_DIR") {
		t.Fatalf("details included non-credential CLAUDE_CONFIG_DIR: %q", joined)
	}
	if !strings.Contains(result.FixHint, supervisorOmitProviderCredsEnv) || !strings.Contains(result.FixHint, supervisorSecretsEnvFileName) {
		t.Fatalf("FixHint = %q, want omit env and secrets file", result.FixHint)
	}
}

func TestSupervisorProviderCredsCheckOKWithoutProviderKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GC_HOME", filepath.Join(home, ".gc"))
	writeSupervisorProviderCredsPlist(t, `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
  <key>EnvironmentVariables</key>
  <dict>
    <key>GC_HOME</key><string>/tmp/gc-home</string>
    <key>PATH</key><string>/usr/bin:/bin</string>
  </dict>
</dict></plist>`)

	result := newSupervisorProviderCredsCheck().Run(&doctor.CheckContext{})
	if result.Status != doctor.StatusOK {
		t.Fatalf("Status = %v, want ok (message %q, details %#v)", result.Status, result.Message, result.Details)
	}
}

func TestSupervisorProviderCredsCheckOKWhenPlistAbsent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GC_HOME", filepath.Join(home, ".gc"))

	result := newSupervisorProviderCredsCheck().Run(&doctor.CheckContext{})
	if result.Status != doctor.StatusOK {
		t.Fatalf("Status = %v, want ok (message %q)", result.Status, result.Message)
	}
}

func TestSupervisorProviderCredsCheckRegisteredInDoctor(t *testing.T) {
	city := t.TempDir()
	cfg := &config.City{Workspace: config.Workspace{Name: "demo"}}
	checks := buildDoctorChecks(city, cfg, nil, buildDoctorChecksOpts{SkipCityDoltCheck: true, SkipManagedDoltCheck: true})
	for _, check := range checks {
		if check.Name() == "supervisor-provider-creds" {
			return
		}
	}
	t.Fatalf("supervisor-provider-creds check not registered")
}
