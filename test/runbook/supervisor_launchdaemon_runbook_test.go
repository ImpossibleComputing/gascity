package runbook_test

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSupervisorLaunchDaemonRunbookSample(t *testing.T) {
	path := filepath.Join("..", "..", "engdocs", "runbooks", "macos-supervisor-launchdaemon.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read runbook: %v", err)
	}
	body := string(data)

	sample := between(t, body, "<!-- launchdaemon-plist-start -->", "<!-- launchdaemon-plist-end -->")
	sample = strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(sample, "```"), "```xml"))
	if err := xml.Unmarshal([]byte(sample), new(any)); err != nil {
		t.Fatalf("sample LaunchDaemon plist is not well-formed XML: %v", err)
	}

	required := []string{
		"<key>Label</key>",
		"<string>com.gascity.supervisor</string>",
		"<key>UserName</key>",
		"<string>qeetbastudio</string>",
		"<key>GroupName</key>",
		"<string>staff</string>",
		"<string>/opt/homebrew/bin/gc</string>",
		"<string>supervisor</string>",
		"<string>run</string>",
		"<key>GC_HOME</key>",
		"<string>/Users/qeetbastudio/.gc</string>",
	}
	for _, want := range required {
		if !strings.Contains(sample, want) {
			t.Fatalf("sample LaunchDaemon plist missing %q", want)
		}
	}

	forbidden := []string{
		"OPENAI_API_KEY",
		"ANTHROPIC_API_KEY",
		"ANTHROPIC_AUTH_TOKEN",
		"CLAUDE_CODE_OAUTH_TOKEN",
		"GEMINI_API_KEY",
		"GOOGLE_API_KEY",
		"MISTRAL_API_KEY",
		"ZAI_API_KEY",
		"AWS_SECRET_ACCESS_KEY",
		"GH_TOKEN",
		"GITHUB_TOKEN",
	}
	for _, name := range forbidden {
		if strings.Contains(sample, name) {
			t.Fatalf("sample LaunchDaemon plist must not include secret-shaped env name %q", name)
		}
	}

	if strings.Contains(sample, "gui/") || strings.Contains(sample, "~/Library/LaunchAgents") {
		t.Fatalf("sample LaunchDaemon plist must not reference the user LaunchAgent domain/path")
	}
}

func between(t *testing.T, s, start, end string) string {
	t.Helper()
	startIdx := strings.Index(s, start)
	if startIdx < 0 {
		t.Fatalf("missing start marker %q", start)
	}
	startIdx += len(start)
	endIdx := strings.Index(s[startIdx:], end)
	if endIdx < 0 {
		t.Fatalf("missing end marker %q", end)
	}
	return s[startIdx : startIdx+endIdx]
}
