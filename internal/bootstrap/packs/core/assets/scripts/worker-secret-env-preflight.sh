#!/usr/bin/env bash
# Fail-closed preflight for inherited worker secret env names.
# Prints names only; never prints values, hashes, lengths, or prefixes.
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
usage: worker-secret-env-preflight.sh [--forbid <ENV_KEY>]... [--allow <ENV_KEY>]...

Checks the current process environment for forbidden inherited credential names.
Defaults cover supervisor-level LLM/GitHub token names. Output is redacted: names
only, no values/hashes/prefixes/lengths.
USAGE
}

forbid=(
  AMP_API_KEY
  ANTHROPIC_API_KEY
  ANTHROPIC_AUTH_TOKEN
  AWS_ACCESS_KEY_ID
  AWS_BEARER_TOKEN_BEDROCK
  AWS_CONTAINER_AUTHORIZATION_TOKEN
  AWS_SECRET_ACCESS_KEY
  AWS_SESSION_TOKEN
  AZURE_OPENAI_API_KEY
  CEREBRAS_API_KEY
  CLAUDE_CODE_OAUTH_TOKEN
  COHERE_API_KEY
  COPILOT_GITHUB_TOKEN
  COPILOT_PROVIDER_API_KEY
  CURSOR_API_KEY
  DEEPSEEK_API_KEY
  FIREWORKS_API_KEY
  GEMINI_API_KEY
  GH_TOKEN
  GITHUB_TOKEN
  GOOGLE_API_KEY
  GOOGLE_APPLICATION_CREDENTIALS
  GOOGLE_GENERATIVE_AI_API_KEY
  GROQ_API_KEY
  KIMI_API_KEY
  KIRO_API_KEY
  MISTRAL_API_KEY
  OPENAI_API_KEY
  OPENROUTER_API_KEY
  TOGETHER_API_KEY
  XAI_API_KEY
  XIAOMI_API_KEY
  ZAI_API_KEY
)
allow=()

while [ "$#" -gt 0 ]; do
  case "$1" in
    --forbid) [ "$#" -ge 2 ] || { usage; exit 64; }; forbid+=("$2"); shift 2 ;;
    --allow) [ "$#" -ge 2 ] || { usage; exit 64; }; allow+=("$2"); shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown arg: $1" >&2; usage; exit 64 ;;
  esac
done

is_allowed() {
  local key="$1" allowed
  if [ "${#allow[@]}" -eq 0 ]; then
    return 1
  fi
  for allowed in "${allow[@]}"; do
    if [ "$key" = "$allowed" ]; then
      return 0
    fi
  done
  return 1
}

seen=""
violations=0
printf 'worker_secret_env_preflight schema=1\n'
for key in "${forbid[@]}"; do
  case "\n$seen\n" in
    *"\n$key\n"*) continue ;;
  esac
  seen="$seen
$key"
  if is_allowed "$key"; then
    printf 'SKIP allowed_env_name=%s value=REDACTED\n' "$key"
    continue
  fi
  if [ "${!key+x}" = "x" ]; then
    printf 'FAIL forbidden_env_name=%s value=REDACTED\n' "$key"
    violations=$((violations + 1))
  else
    printf 'PASS absent_env_name=%s\n' "$key"
  fi
done

printf 'forbidden_present_count=%d\n' "$violations"
printf 'note=values_not_printed_hashed_or_persisted\n'
if [ "$violations" -gt 0 ]; then
  exit 1
fi
printf 'PASS worker secret env preflight\n'
