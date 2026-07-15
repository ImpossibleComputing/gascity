// Package secretscrub validates and applies worker credential environment scrub rules.
package secretscrub

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/gastownhall/gascity/internal/processenv"
)

// EnableDefaultScrubEnv enables launch-time unsetting of the default shared
// supervisor credential names when set to a truthy value in runtime.Config.Env.
// The control variable is itself scrubbed from the launched process.
const EnableDefaultScrubEnv = "GC_WORKER_SECRET_ENV_SCRUB_DEFAULTS"

// ScopedCredentialEnvFileEnv points at a chmod-0600 dotenv file containing
// broker-issued per-worker credentials. The file is consumed by the supervisor
// at session launch and this control variable is not passed to the worker.
const ScopedCredentialEnvFileEnv = "GC_WORKER_SCOPED_CREDENTIAL_ENV_FILE"

// ScopedGitCredentialCommandEnv is intentionally allowed in scoped credential
// env files: it lets a broker provide a per-worker git credential helper without
// exposing a broad supervisor GH_TOKEN/GITHUB_TOKEN to the launched worker.
const ScopedGitCredentialCommandEnv = "GC_GIT_CREDENTIAL_COMMAND"

// ZDOTDIREnv controls where zsh reads per-user startup files from.
const ZDOTDIREnv = "ZDOTDIR"

// IsolatedZDOTDIR points zsh at a non-user startup directory for credential-
// scrubbed worker launches. This prevents ~/.zshenv from re-exporting shared
// provider keys immediately after gc unsets them. The path does not need to
// exist; if it is absent, zsh simply finds no per-user startup files there.
const IsolatedZDOTDIR = "/var/empty"

// DefaultWorkerSecretEnvKeys names shared supervisor credential variables that
// a scrubbed worker launch should not inherit implicitly. Keep this list in sync
// with the core pack's worker-secret-env-preflight.sh default forbid list.
var DefaultWorkerSecretEnvKeys = []string{
	"AMP_API_KEY",
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_AUTH_TOKEN",
	"AWS_ACCESS_KEY_ID",
	"AWS_BEARER_TOKEN_BEDROCK",
	"AWS_CONTAINER_AUTHORIZATION_TOKEN",
	"AWS_SECRET_ACCESS_KEY",
	"AWS_SESSION_TOKEN",
	"AZURE_OPENAI_API_KEY",
	"CEREBRAS_API_KEY",
	"CLAUDE_CODE_OAUTH_TOKEN",
	"COHERE_API_KEY",
	"COPILOT_GITHUB_TOKEN",
	"COPILOT_PROVIDER_API_KEY",
	"CURSOR_API_KEY",
	"DEEPSEEK_API_KEY",
	"FIREWORKS_API_KEY",
	"GEMINI_API_KEY",
	"GH_TOKEN",
	"GITHUB_TOKEN",
	"GOOGLE_API_KEY",
	"GOOGLE_APPLICATION_CREDENTIALS",
	"GOOGLE_GENERATIVE_AI_API_KEY",
	"GROQ_API_KEY",
	"KIMI_API_KEY",
	"KIRO_API_KEY",
	"MISTRAL_API_KEY",
	"OPENAI_API_KEY",
	"OPENROUTER_API_KEY",
	"TOGETHER_API_KEY",
	"XAI_API_KEY",
	"XIAOMI_API_KEY",
	"ZAI_API_KEY",
}

// Enabled reports whether a launch control value enables default secret scrubbing.
func Enabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// ApplyScopedCredentialEnvFile loads broker-issued credential env values from
// ScopedCredentialEnvFileEnv, merges them into env, enables default shared-secret
// scrubbing, and removes the file-path control variable from the launched env.
//
// The env file is deliberately narrow: chmod 0600, absolute path, credential-key
// allowlist only, no empty values, and no conflict with an already non-empty
// configured value. That prevents a scoped broker file from becoming a generic
// launch-env override channel or silently coexisting with broad supervisor creds.
func ApplyScopedCredentialEnvFile(env map[string]string) (map[string]string, error) {
	path := strings.TrimSpace(env[ScopedCredentialEnvFileEnv])
	if path == "" {
		return env, nil
	}
	scoped, err := loadScopedCredentialEnvFile(path)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(env)+len(scoped)+2)
	for k, v := range env {
		out[k] = v
	}
	out[ScopedCredentialEnvFileEnv] = ""
	out[EnableDefaultScrubEnv] = "1"

	keys := make([]string, 0, len(scoped))
	for k := range scoped {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if existing, ok := out[k]; ok && strings.TrimSpace(existing) != "" {
			return nil, fmt.Errorf("scoped credential env file conflicts with preconfigured %s", k)
		}
		out[k] = scoped[k]
	}
	return out, nil
}

// ValidateScopedCredentialEnvFile checks that path satisfies the broker-issued
// worker credential env-file contract without returning any credential values.
func ValidateScopedCredentialEnvFile(path string) error {
	_, err := loadScopedCredentialEnvFile(path)
	return err
}

// ScopedCredentialEnvFileKeys returns the sorted key names in a valid scoped
// credential env file without exposing any values.
func ScopedCredentialEnvFileKeys(path string) ([]string, error) {
	entries, err := loadScopedCredentialEnvFile(path)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

// WriteScopedCredentialEnvFile atomically writes broker-issued scoped
// credentials to path using the same contract enforced at worker launch. The
// caller supplies values out-of-band (for example from a broker or environment
// lookup); returned errors mention keys/paths only and never include values.
func WriteScopedCredentialEnvFile(path string, entries map[string]string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%s output path must be an absolute path", ScopedCredentialEnvFileEnv)
	}
	if len(entries) == 0 {
		return fmt.Errorf("scoped credential env file requires at least one credential key")
	}
	if err := validateScopedCredentialEntries(entries); err != nil {
		return err
	}
	data, err := formatScopedCredentialEnvFile(entries)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create scoped credential env dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create scoped credential env temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod scoped credential env temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write scoped credential env temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync scoped credential env temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close scoped credential env temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("chmod scoped credential env temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("install scoped credential env file: %w", err)
	}
	cleanup = false
	if err := ValidateScopedCredentialEnvFile(path); err != nil {
		return fmt.Errorf("validate written scoped credential env file: %w", err)
	}
	return nil
}

func loadScopedCredentialEnvFile(path string) (map[string]string, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%s must be an absolute path", ScopedCredentialEnvFileEnv)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat scoped credential env file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("scoped credential env file %q is a directory", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("scoped credential env file %q must not be group/world accessible", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read scoped credential env file: %w", err)
	}
	parsed, err := processenv.ParseEnvFile(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse scoped credential env file: invalid dotenv syntax")
	}
	if err := validateScopedCredentialEntries(parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func validateScopedCredentialEntries(parsed map[string]string) error {
	for k, v := range parsed {
		if !validEnvKey(k) {
			return fmt.Errorf("scoped credential env file contains invalid env key %q", k)
		}
		if !allowedScopedCredentialKey(k) {
			return fmt.Errorf("scoped credential env file key %s is not an allowed credential key", k)
		}
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("scoped credential env file key %s has an empty value", k)
		}
	}
	return nil
}

func formatScopedCredentialEnvFile(entries map[string]string) ([]byte, error) {
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	for _, k := range keys {
		v := entries[k]
		formatted, err := formatScopedCredentialEnvValue(k, v)
		if err != nil {
			return nil, err
		}
		buf.WriteString(k)
		buf.WriteByte('=')
		buf.WriteString(formatted)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

func formatScopedCredentialEnvValue(key, value string) (string, error) {
	if strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("scoped credential env file key %s contains an unsupported newline", key)
	}
	if strings.TrimSpace(value) == value {
		return value, nil
	}
	if !strings.Contains(value, `"`) {
		return `"` + value + `"`, nil
	}
	if !strings.Contains(value, `'`) {
		return `'` + value + `'`, nil
	}
	return "", fmt.Errorf("scoped credential env file key %s contains unsupported surrounding whitespace plus quotes", key)
}

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validEnvKey(key string) bool {
	return envKeyPattern.MatchString(key)
}

func allowedScopedCredentialKey(key string) bool {
	return IsWorkerCredentialEnvKey(key)
}

// IsWorkerCredentialEnvKey reports whether key is a credential/provider key
// covered by the scoped worker credential and default scrub contracts.
func IsWorkerCredentialEnvKey(key string) bool {
	if key == ScopedGitCredentialCommandEnv {
		return true
	}
	for _, allowed := range DefaultWorkerSecretEnvKeys {
		if key == allowed {
			return true
		}
	}
	return processenv.IsProviderCredentialEnv(key)
}

// ApplyDefaultUnsets returns env with default shared credential keys set to an
// empty value when EnableDefaultScrubEnv is truthy. Explicit non-empty values in
// env win, so a future broker can still provide a scoped credential for an
// allowed key. The returned map is always a copy when scrubbing is enabled.
func ApplyDefaultUnsets(env map[string]string) map[string]string {
	if !Enabled(env[EnableDefaultScrubEnv]) {
		return env
	}
	out := make(map[string]string, len(env)+len(DefaultWorkerSecretEnvKeys)+1)
	for k, v := range env {
		out[k] = v
	}
	for _, key := range DefaultWorkerSecretEnvKeys {
		if _, exists := out[key]; !exists {
			out[key] = ""
		}
	}
	out[EnableDefaultScrubEnv] = ""
	return out
}

// ApplyShellStartupIsolation forces zsh startup lookup away from the user's
// dotfiles whenever a worker launch is scrubbing shared credential env vars.
// Without this, ~/.zshenv can re-export a provider key after env -u removed it.
func ApplyShellStartupIsolation(env map[string]string) map[string]string {
	if !needsShellStartupIsolation(env) {
		return env
	}
	out := make(map[string]string, len(env)+1)
	for k, v := range env {
		out[k] = v
	}
	out[ZDOTDIREnv] = IsolatedZDOTDIR
	return out
}

func needsShellStartupIsolation(env map[string]string) bool {
	if Enabled(env[EnableDefaultScrubEnv]) {
		return true
	}
	for k, v := range env {
		if v == "" && IsWorkerCredentialEnvKey(k) {
			return true
		}
	}
	return false
}
