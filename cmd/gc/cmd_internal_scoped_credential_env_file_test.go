package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/runtime/secretscrub"
)

func TestInternalScopedCredentialEnvFileWritesPrivateFileWithoutLeakingValues(t *testing.T) {
	t.Setenv("SOURCE_OPENAI", "sk-scoped-openai")
	t.Setenv("GITHUB_TOKEN", "ghs-scoped-github")
	path := filepath.Join(t.TempDir(), "creds", "worker.env")
	var stdout, stderr bytes.Buffer
	cmd := newInternalScopedCredentialEnvFileCmd(&stdout, &stderr)
	cmd.SetArgs([]string{
		"--out", path,
		"--from-env", "OPENAI_API_KEY=SOURCE_OPENAI",
		"--from-env", "GITHUB_TOKEN",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v stderr=%s", err, stderr.String())
	}
	for _, leak := range []string{"sk-scoped-openai", "ghs-scoped-github"} {
		if strings.Contains(stdout.String(), leak) || strings.Contains(stderr.String(), leak) {
			t.Fatalf("command leaked %q: stdout=%q stderr=%q", leak, stdout.String(), stderr.String())
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
	env, err := secretscrub.ApplyScopedCredentialEnvFile(map[string]string{secretscrub.ScopedCredentialEnvFileEnv: path})
	if err != nil {
		t.Fatalf("ApplyScopedCredentialEnvFile: %v", err)
	}
	if env["OPENAI_API_KEY"] != "sk-scoped-openai" || env["GITHUB_TOKEN"] != "ghs-scoped-github" {
		t.Fatalf("written env did not round-trip: %#v", env)
	}
}

func TestInternalScopedCredentialEnvFileRejectsMissingEnvWithoutLeakingOtherValues(t *testing.T) {
	t.Setenv("SOURCE_OPENAI", "sk-present-but-not-used")
	path := filepath.Join(t.TempDir(), "worker.env")
	var stdout, stderr bytes.Buffer
	cmd := newInternalScopedCredentialEnvFileCmd(&stdout, &stderr)
	cmd.SetArgs([]string{
		"--out", path,
		"--from-env", "OPENAI_API_KEY=SOURCE_OPENAI",
		"--from-env", "GITHUB_TOKEN=MISSING_GITHUB_TOKEN",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute succeeded, want missing-env error")
	}
	if !strings.Contains(stderr.String(), "MISSING_GITHUB_TOKEN") || !strings.Contains(stderr.String(), "GITHUB_TOKEN") {
		t.Fatalf("stderr = %q, want source and output key names", stderr.String())
	}
	if strings.Contains(stdout.String(), "sk-present") || strings.Contains(stderr.String(), "sk-present") {
		t.Fatalf("command leaked present secret: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("output path exists after failure: statErr=%v", statErr)
	}
}

func TestParseScopedCredentialFromEnvSpec(t *testing.T) {
	key, source, err := parseScopedCredentialFromEnvSpec("OPENAI_API_KEY=SOURCE_OPENAI")
	if err != nil || key != "OPENAI_API_KEY" || source != "SOURCE_OPENAI" {
		t.Fatalf("explicit source = (%q,%q,%v)", key, source, err)
	}
	key, source, err = parseScopedCredentialFromEnvSpec("GITHUB_TOKEN")
	if err != nil || key != "GITHUB_TOKEN" || source != "GITHUB_TOKEN" {
		t.Fatalf("implicit source = (%q,%q,%v)", key, source, err)
	}
	if _, _, err := parseScopedCredentialFromEnvSpec("OPENAI_API_KEY="); err == nil {
		t.Fatal("empty source succeeded, want error")
	}
}
