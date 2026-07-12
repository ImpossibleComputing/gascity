package main

import (
	"bytes"
	"encoding/json"
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

func TestInternalScopedCredentialEnvFileRejectsSourceEnvFileSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink no-follow contract is Unix-only")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target.env")
	if err := os.WriteFile(target, []byte("SCOPED_OPENAI=sk-symlink-target\n"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(dir, "source.env")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	path := filepath.Join(t.TempDir(), "worker.env")
	var stdout, stderr bytes.Buffer
	cmd := newInternalScopedCredentialEnvFileCmd(&stdout, &stderr)
	cmd.SetArgs([]string{
		"--out", path,
		"--source-env-file", link,
		"--from-env-file", "OPENAI_API_KEY=SCOPED_OPENAI",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute succeeded, want symlink rejection")
	}
	if !strings.Contains(stderr.String(), "must not be a symlink") {
		t.Fatalf("stderr = %q, want symlink rejection", stderr.String())
	}
	if strings.Contains(stdout.String(), "sk-symlink-target") || strings.Contains(stderr.String(), "sk-symlink-target") {
		t.Fatalf("command leaked symlink target secret: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("output path exists after failure: statErr=%v", statErr)
	}
}

func TestInternalScopedCredentialEnvFileWritesAuditLogWithoutLeakingValues(t *testing.T) {
	t.Setenv("SOURCE_OPENAI", "sk-audit-openai")
	source := filepath.Join(t.TempDir(), "source.env")
	if err := os.WriteFile(source, []byte("SCOPED_GITHUB=ghs-audit-github\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	path := filepath.Join(t.TempDir(), "creds", "worker.env")
	auditPath := filepath.Join(t.TempDir(), "audit", "scoped-creds.jsonl")
	var stdout, stderr bytes.Buffer
	cmd := newInternalScopedCredentialEnvFileCmd(&stdout, &stderr)
	cmd.SetArgs([]string{
		"--out", path,
		"--from-env", "OPENAI_API_KEY=SOURCE_OPENAI",
		"--source-env-file", source,
		"--from-env-file", "GITHUB_TOKEN=SCOPED_GITHUB",
		"--audit-log", auditPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v stderr=%s", err, stderr.String())
	}
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	for _, leak := range []string{"sk-audit-openai", "ghs-audit-github"} {
		if strings.Contains(stdout.String(), leak) || strings.Contains(stderr.String(), leak) || strings.Contains(string(data), leak) {
			t.Fatalf("leaked %q: stdout=%q stderr=%q audit=%q", leak, stdout.String(), stderr.String(), string(data))
		}
	}
	info, err := os.Stat(auditPath)
	if err != nil {
		t.Fatalf("stat audit log: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("audit log mode = %o, want 600", info.Mode().Perm())
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("audit log lines = %d, want 1: %q", len(lines), string(data))
	}
	var event scopedCredentialAuditEvent
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatalf("unmarshal audit event: %v", err)
	}
	if event.Action != "write-scoped-credential-env-file" || event.Out != path {
		t.Fatalf("audit event action/out = %q/%q", event.Action, event.Out)
	}
	if got, want := strings.Join(event.Keys, ","), "GITHUB_TOKEN,OPENAI_API_KEY"; got != want {
		t.Fatalf("event keys = %q, want %q", got, want)
	}
	if len(event.Sources) != 2 {
		t.Fatalf("event sources = %#v, want 2", event.Sources)
	}
	if event.Sources[0] != (scopedCredentialAuditSource{Key: "GITHUB_TOKEN", Kind: "env-file", SourceKey: "SCOPED_GITHUB"}) ||
		event.Sources[1] != (scopedCredentialAuditSource{Key: "OPENAI_API_KEY", Kind: "env", SourceKey: "SOURCE_OPENAI"}) {
		t.Fatalf("event sources = %#v", event.Sources)
	}
}

func TestScopedCredentialAuditLogRejectsUnsafePaths(t *testing.T) {
	if err := appendScopedCredentialAuditLog("relative.jsonl", "/tmp/out.env", map[string]string{"OPENAI_API_KEY": "sk"}, nil); err == nil {
		t.Fatal("relative audit log path succeeded, want error")
	}
	if runtime.GOOS == "windows" {
		return
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target.jsonl")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(dir, "audit.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	err := appendScopedCredentialAuditLog(link, "/tmp/out.env", map[string]string{"OPENAI_API_KEY": "sk"}, nil)
	if err == nil {
		t.Fatal("symlink audit log path succeeded, want error")
	}
	if !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("error = %v, want symlink rejection", err)
	}
}
