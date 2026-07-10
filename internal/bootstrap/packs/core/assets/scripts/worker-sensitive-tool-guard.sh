#!/usr/bin/env bash
# Fail-closed Phase-1 guard for founder-comms/payment credential paths.
# This is a blast-radius tripwire, not the final OS/vault hard wall.
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
usage: worker-sensitive-tool-guard.sh --kind <gws|secret|browser> [--secret-class <class>] [--profile <profile>] --real <path> -- [args...]
USAGE
}

kind=""
secret_class=""
profile=""
real_cmd=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --kind)
      [ "$#" -ge 2 ] || { usage; exit 64; }
      kind="$2"; shift 2 ;;
    --secret-class)
      [ "$#" -ge 2 ] || { usage; exit 64; }
      secret_class="$2"; shift 2 ;;
    --profile)
      [ "$#" -ge 2 ] || { usage; exit 64; }
      profile="$2"; shift 2 ;;
    --real)
      [ "$#" -ge 2 ] || { usage; exit 64; }
      real_cmd="$2"; shift 2 ;;
    --)
      shift; break ;;
    -h|--help)
      usage; exit 0 ;;
    *)
      echo "credential guard: unknown option: $1" >&2
      usage
      exit 64 ;;
  esac
done

[ -n "$kind" ] || { echo "credential guard: --kind is required" >&2; usage; exit 64; }
[ -n "$real_cmd" ] || { echo "credential guard: --real is required" >&2; usage; exit 64; }

lc() { printf '%s' "$1" | tr '[:upper:]' '[:lower:]'; }
contains_sensitive_word() {
  case "$(lc "$1")" in
    *founder*|*gmail*|*gws*|*mayor*|*payment*|*card*|*stripe*|*z.ai*|*zai*|*mistral*|*.secrets*|*secret*) return 0 ;;
    *) return 1 ;;
  esac
}

identity_text="${GC_AGENT:-} ${GC_SESSION_NAME:-} ${GC_SESSION_ID:-} ${GC_POOL_NAME:-} ${GC_AGENT_ROLE:-}"
role="$(lc "${GC_AGENT_ROLE:-}")"
agent="$(lc "${GC_AGENT:-}")"

is_privileged_identity() {
  # Test-only/manual breakglass. Live activation docs require mayor+Paul review
  # before using this in any production PATH.
  if [ "${GC_CREDENTIAL_GUARD_ALLOW:-}" = "1" ]; then
    return 0
  fi
  case "$role" in
    mayor|ops|credential-admin|payment-admin) return 0 ;;
  esac
  case "$agent" in
    mayor|paul) return 0 ;;
  esac
  return 1
}

is_sensitive_request() {
  case "$(lc "$kind")" in
    gws)
      # In the current fleet, gws is the founder-comms/Gmail surface. Deny all
      # non-privileged gws calls rather than trying to infer safety from verbs.
      return 0 ;;
    secret)
      # Before Phase-2 OS/vault isolation, sanctioned secret-helper access is
      # credential-classed by the wrapper, not by user-provided keywords. Deny
      # all non-privileged calls so an unkeyworded secret class/profile cannot
      # bypass the guard.
      return 0 ;;
    browser)
      # Shared browser/profile access can carry founder-comms or payment
      # authority even when the requested profile name is unkeyworded. Deny all
      # non-privileged calls through this wrapper until Phase-2 account/profile
      # isolation exists.
      return 0 ;;
    *)
      echo "credential guard: unsupported kind: $kind" >&2
      exit 64 ;;
  esac
}

if is_sensitive_request "$@"; then
  if ! is_privileged_identity; then
    echo "credential guard deny: non-privileged gc session cannot access founder-comms/payment credential surface kind=$kind" >&2
    echo "credential guard deny: activation is Phase-1 fail-closed tripwire; request mayor/ops review for an allowlisted path" >&2
    exit 77
  fi
fi

exec "$real_cmd" "$@"
