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
  OPENAI_API_KEY
  GEMINI_API_KEY
  ANTHROPIC_API_KEY
  MISTRAL_API_KEY
  ZAI_API_KEY
  GH_TOKEN
  GITHUB_TOKEN
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
