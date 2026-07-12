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

func TestInternalScopedCredentialEnvFileReadsPrivateSourceEnvFileWithoutLeakingValues(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.env")
	if err := os.WriteFile(source, []byte("SCOPED_OPENAI=sk-source-openai\nSCOPED_GITHUB=ghs-source-github\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	path := filepath.Join(t.TempDir(), "creds", "worker.env")
	var stdout, stderr bytes.Buffer
	cmd := newInternalScopedCredentialEnvFileCmd(&stdout, &stderr)
	cmd.SetArgs([]string{
		"--out", path,
		"--source-env-file", source,
		"--from-env-file", "OPENAI_API_KEY=SCOPED_OPENAI",
		"--from-env-file", "GITHUB_TOKEN=SCOPED_GITHUB",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v stderr=%s", err, stderr.String())
	}
	for _, leak := range []string{"sk-source-openai", "ghs-source-github"} {
		if strings.Contains(stdout.String(), leak) || strings.Contains(stderr.String(), leak) {
			t.Fatalf("command leaked %q: stdout=%q stderr=%q", leak, stdout.String(), stderr.String())
		}
	}
	env, err := secretscrub.ApplyScopedCredentialEnvFile(map[string]string{secretscrub.ScopedCredentialEnvFileEnv: path})
	if err != nil {
		t.Fatalf("ApplyScopedCredentialEnvFile: %v", err)
	}
	if env["OPENAI_API_KEY"] != "sk-source-openai" || env["GITHUB_TOKEN"] != "ghs-source-github" {
		t.Fatalf("written env did not round-trip: %#v", env)
	}
}

func TestInternalScopedCredentialEnvFileRejectsUnsafeSourceEnvFileWithoutLeakingValues(t *testing.T) {
	for _, tt := range []struct {
		name    string
		content string
		mode    os.FileMode
		want    string
	}{
		{name: "loose", content: "SCOPED_OPENAI=sk-loose\n", mode: 0o644, want: "must not be group/world accessible"},
		{name: "malformed", content: "sk-malformed-without-equals\n", mode: 0o600, want: "invalid dotenv syntax"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "source.env")
			if err := os.WriteFile(source, []byte(tt.content), tt.mode); err != nil {
				t.Fatalf("write source: %v", err)
			}
			path := filepath.Join(t.TempDir(), "worker.env")
			var stdout, stderr bytes.Buffer
			cmd := newInternalScopedCredentialEnvFileCmd(&stdout, &stderr)
			cmd.SetArgs([]string{
				"--out", path,
				"--source-env-file", source,
				"--from-env-file", "OPENAI_API_KEY=SCOPED_OPENAI",
			})

			err := cmd.Execute()
			if err == nil {
				t.Fatal("Execute succeeded, want source env-file error")
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), tt.want)
			}
			for _, leak := range []string{"sk-loose", "sk-malformed"} {
				if strings.Contains(stdout.String(), leak) || strings.Contains(stderr.String(), leak) {
					t.Fatalf("command leaked %q: stdout=%q stderr=%q", leak, stdout.String(), stderr.String())
				}
			}
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Fatalf("output path exists after failure: statErr=%v", statErr)
			}
		})
	}
}

func TestInternalScopedCredentialEnvFileRejectsDuplicateOutputAcrossSources(t *testing.T) {
	t.Setenv("SOURCE_OPENAI", "sk-env")
	source := filepath.Join(t.TempDir(), "source.env")
	if err := os.WriteFile(source, []byte("SCOPED_OPENAI=sk-file\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	path := filepath.Join(t.TempDir(), "worker.env")
	var stdout, stderr bytes.Buffer
	cmd := newInternalScopedCredentialEnvFileCmd(&stdout, &stderr)
	cmd.SetArgs([]string{
		"--out", path,
		"--from-env", "OPENAI_API_KEY=SOURCE_OPENAI",
		"--source-env-file", source,
		"--from-env-file", "OPENAI_API_KEY=SCOPED_OPENAI",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute succeeded, want duplicate output key error")
	}
	if !strings.Contains(stderr.String(), "duplicate output key OPENAI_API_KEY") {
		t.Fatalf("stderr = %q, want duplicate output key", stderr.String())
	}
	for _, leak := range []string{"sk-env", "sk-file"} {
		if strings.Contains(stdout.String(), leak) || strings.Contains(stderr.String(), leak) {
			t.Fatalf("command leaked %q: stdout=%q stderr=%q", leak, stdout.String(), stderr.String())
		}
	}
}
