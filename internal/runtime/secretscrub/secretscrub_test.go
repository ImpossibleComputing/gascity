package secretscrub

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestApplyDefaultUnsetsRequiresTruthyControl(t *testing.T) {
	env := map[string]string{"GH_TOKEN": "shared"}
	got := ApplyDefaultUnsets(env)
	if got["GH_TOKEN"] != "shared" {
		t.Fatalf("GH_TOKEN = %q, want unchanged when scrub control is absent", got["GH_TOKEN"])
	}
}

func TestApplyDefaultUnsetsScrubsAbsentDefaultsAndPreservesExplicitScopedValues(t *testing.T) {
	env := map[string]string{
		EnableDefaultScrubEnv: "true",
		"OPENAI_API_KEY":      "scoped-worker-token",
		"LANG":                "en_US.UTF-8",
	}
	got := ApplyDefaultUnsets(env)
	got["LANG"] = "mutated"
	if env["LANG"] != "en_US.UTF-8" {
		t.Fatalf("ApplyDefaultUnsets mutated original map; LANG=%q", env["LANG"])
	}
	if got["OPENAI_API_KEY"] != "scoped-worker-token" {
		t.Fatalf("OPENAI_API_KEY = %q, want explicit scoped value preserved", got["OPENAI_API_KEY"])
	}
	if got["GH_TOKEN"] != "" || got["GEMINI_API_KEY"] != "" {
		t.Fatalf("default secrets not scrubbed: GH_TOKEN=%q GEMINI_API_KEY=%q", got["GH_TOKEN"], got["GEMINI_API_KEY"])
	}
	if got[EnableDefaultScrubEnv] != "" {
		t.Fatalf("%s = %q, want scrub control unset in launched env", EnableDefaultScrubEnv, got[EnableDefaultScrubEnv])
	}
	if _, ok := env["GH_TOKEN"]; ok {
		t.Fatalf("original env mutated: %#v", env)
	}
}

func TestApplyShellStartupIsolationWhenScrubbingSecrets(t *testing.T) {
	unchanged := map[string]string{"LANG": "en_US.UTF-8"}
	if got := ApplyShellStartupIsolation(unchanged); got[ZDOTDIREnv] != "" || got["LANG"] != "en_US.UTF-8" {
		t.Fatalf("ApplyShellStartupIsolation changed env without scrub signal: %#v", got)
	}

	defaultScrub := ApplyDefaultUnsets(map[string]string{
		EnableDefaultScrubEnv: "1",
		"ZDOTDIR":             "/Users/example",
	})
	got := ApplyShellStartupIsolation(defaultScrub)
	if got[ZDOTDIREnv] != IsolatedZDOTDIR {
		t.Fatalf("%s = %q, want isolated %q", ZDOTDIREnv, got[ZDOTDIREnv], IsolatedZDOTDIR)
	}

	explicitUnset := ApplyShellStartupIsolation(map[string]string{
		"GEMINI_API_KEY": "",
		"ZDOTDIR":        "/Users/example",
	})
	if explicitUnset[ZDOTDIREnv] != IsolatedZDOTDIR {
		t.Fatalf("explicit credential unset did not isolate %s: %#v", ZDOTDIREnv, explicitUnset)
	}
}

func TestIsolatedZDOTDIRPreventsZshenvReexport(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not installed")
	}
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".zshenv"), []byte("export GC_ZSHENV_REEXPORT=from-dotfile\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	readVar := func(env []string) string {
		t.Helper()
		cmd := exec.Command(zsh, "-c", `printf '%s' "${GC_ZSHENV_REEXPORT-unset}"`)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("zsh probe failed: %v: %s", err, out)
		}
		return string(out)
	}
	baseEnv := []string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}
	if got := readVar(baseEnv); got != "from-dotfile" {
		t.Fatalf("positive control .zshenv value = %q, want from-dotfile", got)
	}
	isolatedEnv := append(append([]string{}, baseEnv...), ZDOTDIREnv+"="+IsolatedZDOTDIR)
	if got := readVar(isolatedEnv); got != "unset" {
		t.Fatalf("isolated %s still sourced user .zshenv: got %q", ZDOTDIREnv, got)
	}
}

func TestApplyScopedCredentialEnvFileMergesScopedCredsAndEnablesScrub(t *testing.T) {
	path := writeScopedEnvFile(t, "OPENAI_API_KEY=scoped-openai\nGITHUB_TOKEN=scoped-github\nGC_GIT_CREDENTIAL_COMMAND=/broker/gitcred\n")
	env := map[string]string{
		ScopedCredentialEnvFileEnv: path,
		"LANG":                     "en_US.UTF-8",
	}
	got, err := ApplyScopedCredentialEnvFile(env)
	if err != nil {
		t.Fatalf("ApplyScopedCredentialEnvFile: %v", err)
	}
	if got["OPENAI_API_KEY"] != "scoped-openai" || got["GITHUB_TOKEN"] != "scoped-github" {
		t.Fatalf("scoped credentials not merged: %#v", got)
	}
	if got[ScopedGitCredentialCommandEnv] != "/broker/gitcred" {
		t.Fatalf("%s = %q", ScopedGitCredentialCommandEnv, got[ScopedGitCredentialCommandEnv])
	}
	if got[EnableDefaultScrubEnv] != "1" {
		t.Fatalf("%s = %q, want 1", EnableDefaultScrubEnv, got[EnableDefaultScrubEnv])
	}
	if got[ScopedCredentialEnvFileEnv] != "" {
		t.Fatalf("%s = %q, want scrubbed control", ScopedCredentialEnvFileEnv, got[ScopedCredentialEnvFileEnv])
	}
	if _, ok := env["OPENAI_API_KEY"]; ok {
		t.Fatalf("original env mutated: %#v", env)
	}
}

func TestApplyScopedCredentialEnvFileRejectsRelativeInsecureUnknownAndConflict(t *testing.T) {
	if _, err := ApplyScopedCredentialEnvFile(map[string]string{ScopedCredentialEnvFileEnv: "relative.env"}); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("relative path error = %v, want absolute-path rejection", err)
	}

	unknown := writeScopedEnvFile(t, "PATH=/tmp/bin\n")
	if _, err := ApplyScopedCredentialEnvFile(map[string]string{ScopedCredentialEnvFileEnv: unknown}); err == nil || !strings.Contains(err.Error(), "not an allowed credential key") {
		t.Fatalf("unknown key error = %v, want allowlist rejection", err)
	}

	empty := writeScopedEnvFile(t, "OPENAI_API_KEY=\n")
	if _, err := ApplyScopedCredentialEnvFile(map[string]string{ScopedCredentialEnvFileEnv: empty}); err == nil || !strings.Contains(err.Error(), "empty value") {
		t.Fatalf("empty value error = %v, want empty-value rejection", err)
	}

	conflict := writeScopedEnvFile(t, "OPENAI_API_KEY=scoped\n")
	if _, err := ApplyScopedCredentialEnvFile(map[string]string{ScopedCredentialEnvFileEnv: conflict, "OPENAI_API_KEY": "already-set"}); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("conflict error = %v, want conflict rejection", err)
	}

	if runtime.GOOS != "windows" {
		insecure := filepath.Join(t.TempDir(), "scoped.env")
		if err := os.WriteFile(insecure, []byte("OPENAI_API_KEY=scoped\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(insecure, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ApplyScopedCredentialEnvFile(map[string]string{ScopedCredentialEnvFileEnv: insecure}); err == nil || !strings.Contains(err.Error(), "group/world") {
			t.Fatalf("insecure mode error = %v, want mode rejection", err)
		}
	}
}

func TestWriteScopedCredentialEnvFileAtomicallyWritesPrivateSortedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "worker.env")
	err := WriteScopedCredentialEnvFile(path, map[string]string{
		"GITHUB_TOKEN":                      "scoped-github",
		"OPENAI_API_KEY":                    "scoped-openai",
		ScopedGitCredentialCommandEnv:       "/broker/gitcred",
		"ANTHROPIC_AUTH_TOKEN":              " scoped-anthropic ",
		"GOOGLE_APPLICATION_CREDENTIALS":    "/runtime/google.json",
		"AWS_CONTAINER_AUTHORIZATION_TOKEN": "aws-bearer",
	})
	if err != nil {
		t.Fatalf("WriteScopedCredentialEnvFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat written file: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	text := string(data)
	if strings.Index(text, "ANTHROPIC_AUTH_TOKEN=") > strings.Index(text, "OPENAI_API_KEY=") {
		t.Fatalf("expected sorted output, got:\n%s", text)
	}
	env := map[string]string{ScopedCredentialEnvFileEnv: path}
	got, err := ApplyScopedCredentialEnvFile(env)
	if err != nil {
		t.Fatalf("ApplyScopedCredentialEnvFile(written): %v\n%s", err, text)
	}
	if got["OPENAI_API_KEY"] != "scoped-openai" || got["ANTHROPIC_AUTH_TOKEN"] != " scoped-anthropic " {
		t.Fatalf("written values did not round-trip through parser: %#v", got)
	}
}

func TestWriteScopedCredentialEnvFileRejectsBadInputsWithoutLeakingValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worker.env")
	for name, entries := range map[string]map[string]string{
		"bad-key":     {"PATH": "/tmp/bin"},
		"empty-value": {"OPENAI_API_KEY": ""},
		"newline":     {"OPENAI_API_KEY": "sk-secret\nnext-line"},
	} {
		t.Run(name, func(t *testing.T) {
			err := WriteScopedCredentialEnvFile(path, entries)
			if err == nil {
				t.Fatal("WriteScopedCredentialEnvFile succeeded, want error")
			}
			for _, leak := range []string{"/tmp/bin", "sk-secret", "next-line"} {
				if strings.Contains(err.Error(), leak) {
					t.Fatalf("error leaked %q: %v", leak, err)
				}
			}
		})
	}
}

func TestScopedCredentialEnvFileKeysReturnsSortedNamesOnly(t *testing.T) {
	path := writeScopedEnvFile(t, "OPENAI_API_KEY=sk-secret\nGITHUB_TOKEN=ghs-secret\n")
	keys, err := ScopedCredentialEnvFileKeys(path)
	if err != nil {
		t.Fatalf("ScopedCredentialEnvFileKeys: %v", err)
	}
	got := strings.Join(keys, ",")
	if got != "GITHUB_TOKEN,OPENAI_API_KEY" {
		t.Fatalf("keys = %q", got)
	}
	for _, leak := range []string{"sk-secret", "ghs-secret"} {
		if strings.Contains(got, leak) {
			t.Fatalf("keys leaked %q: %q", leak, got)
		}
	}
}

func writeScopedEnvFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scoped.env")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
