package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/runtime/secretscrub"
)

func writeScopedWorkerCredentialTestFile(t *testing.T, body string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "worker.env")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestScopedWorkerCredentialFilesCheckOKWhenValid(t *testing.T) {
	path := writeScopedWorkerCredentialTestFile(t, "OPENAI_API_KEY=scoped\nGC_GIT_CREDENTIAL_COMMAND=/broker/gitcred\n", 0o600)
	cfg := &config.City{Agents: []config.Agent{{Name: "codex", Env: map[string]string{secretscrub.ScopedCredentialEnvFileEnv: path}}}}

	result := NewScopedWorkerCredentialFilesCheck(cfg).Run(&CheckContext{})
	if result.Status != StatusOK {
		t.Fatalf("Status = %v, want OK (message %q details %#v)", result.Status, result.Message, result.Details)
	}
	if !strings.Contains(result.Message, "1 scoped worker credential env file") {
		t.Fatalf("Message = %q", result.Message)
	}
}

func TestScopedWorkerCredentialFilesCheckWarnsWithoutLeakingValues(t *testing.T) {
	badKey := writeScopedWorkerCredentialTestFile(t, "PATH=/tmp/bin\n", 0o600)
	badSyntax := writeScopedWorkerCredentialTestFile(t, "sk-secret-without-equals\n", 0o600)
	cfg := &config.City{Agents: []config.Agent{
		{Name: "bad-key", Env: map[string]string{secretscrub.ScopedCredentialEnvFileEnv: badKey}},
		{Name: "bad-syntax", Env: map[string]string{secretscrub.ScopedCredentialEnvFileEnv: badSyntax}},
	}}

	result := NewScopedWorkerCredentialFilesCheck(cfg).Run(&CheckContext{})
	if result.Status != StatusWarning {
		t.Fatalf("Status = %v, want Warning (message %q)", result.Status, result.Message)
	}
	if result.Severity != SeverityAdvisory {
		t.Fatalf("Severity = %v, want advisory", result.Severity)
	}
	joined := strings.Join(result.Details, "\n")
	for _, want := range []string{"bad-key", "bad-syntax", "PATH", "invalid dotenv syntax"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("details missing %q:\n%s", want, joined)
		}
	}
	for _, leak := range []string{"/tmp/bin", "sk-secret-without-equals"} {
		if strings.Contains(joined, leak) || strings.Contains(result.Message, leak) || strings.Contains(result.FixHint, leak) {
			t.Fatalf("result leaked %q: message=%q details=%q fix=%q", leak, result.Message, joined, result.FixHint)
		}
	}
}

func TestScopedWorkerCredentialFilesCheckWarnsOnRelativeMissingAndLooseMode(t *testing.T) {
	loose := writeScopedWorkerCredentialTestFile(t, "OPENAI_API_KEY=scoped\n", 0o644)
	missing := filepath.Join(t.TempDir(), "missing.env")
	cfg := &config.City{Agents: []config.Agent{
		{Name: "relative", Env: map[string]string{secretscrub.ScopedCredentialEnvFileEnv: "relative.env"}},
		{Name: "missing", Env: map[string]string{secretscrub.ScopedCredentialEnvFileEnv: missing}},
		{Name: "loose", Env: map[string]string{secretscrub.ScopedCredentialEnvFileEnv: loose}},
	}}

	result := NewScopedWorkerCredentialFilesCheck(cfg).Run(&CheckContext{})
	if result.Status != StatusWarning {
		t.Fatalf("Status = %v, want Warning (message %q)", result.Status, result.Message)
	}
	joined := strings.Join(result.Details, "\n")
	for _, want := range []string{"relative", "absolute path", "missing", "stat scoped credential env file", "loose", "group/world"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("details missing %q:\n%s", want, joined)
		}
	}
}

func TestScopedWorkerCredentialFilesCheckOKWhenNoneConfigured(t *testing.T) {
	cfg := &config.City{Agents: []config.Agent{{Name: "worker"}}}
	result := NewScopedWorkerCredentialFilesCheck(cfg).Run(&CheckContext{})
	if result.Status != StatusOK {
		t.Fatalf("Status = %v, want OK", result.Status)
	}
}
