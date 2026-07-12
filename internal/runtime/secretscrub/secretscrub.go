package secretscrub

import "strings"

// EnableDefaultScrubEnv enables launch-time unsetting of the default shared
// supervisor credential names when set to a truthy value in runtime.Config.Env.
// The control variable is itself scrubbed from the launched process.
const EnableDefaultScrubEnv = "GC_WORKER_SECRET_ENV_SCRUB_DEFAULTS"

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

func Enabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
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
