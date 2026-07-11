#!/usr/bin/env bash
# Phase-3 tripwire for process listings that can leak inherited env/API keys.
# This is not a hard boundary: absolute /bin/ps still bypasses PATH wrappers.
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
usage: worker-process-listing-guard.sh --real <path> -- [ps args...]

Blocks broad/full-command ps forms for non-privileged worker identities. Use
process-specific, narrow output such as: ps -p <pid> -o pid,comm=
USAGE
}

real_cmd=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --real) [ "$#" -ge 2 ] || { usage; exit 64; }; real_cmd="$2"; shift 2 ;;
    --) shift; break ;;
    -h|--help) usage; exit 0 ;;
    *) echo "process listing guard: unknown option: $1" >&2; usage; exit 64 ;;
  esac
done
[ -n "$real_cmd" ] || { echo "process listing guard: --real is required" >&2; usage; exit 64; }

lc() { printf '%s' "$1" | tr '[:upper:]' '[:lower:]'; }
role="$(lc "${GC_AGENT_ROLE:-}")"
agent="$(lc "${GC_AGENT:-}")"

is_privileged_identity() {
  if [ "${GC_PROCESS_LISTING_GUARD_ALLOW:-}" = "1" ]; then
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

has_process_specific_filter=0
prev=""
for arg in "$@"; do
  if [ "$prev" = "-p" ] || [ "$prev" = "--pid" ]; then
    has_process_specific_filter=1
  fi
  case "$arg" in
    -p*|--pid*) has_process_specific_filter=1 ;;
  esac
  prev="$arg"
done

is_sensitive_ps_form() {
  local arg lower compact
  # No-arg ps is narrow enough for now; it is not the broad env/full-command
  # form that caused the transcript leak.
  [ "$#" -gt 0 ] || return 1
  for arg in "$@"; do
    lower="$(lc "$arg")"
    compact="${lower//-/}"
    compact="${compact// /}"
    case "$lower" in
      aux|ax|axu|axww|auxww|-ef|-eaf|-ely|-a|-a*) return 0 ;;
    esac
    case "$lower" in
      -f|-f*|f) return 0 ;;
    esac
    case "$compact" in
      *ww*|*full*) return 0 ;;
    esac
    case "$compact" in
      *aux*|*axww*|*eww*|*env*) return 0 ;;
    esac
    case "$lower" in
      *args*|*command*|*cmd*|*env*) return 0 ;;
    esac
  done
  # All-process selectors are sensitive unless paired with a specific pid.
  if [ "$has_process_specific_filter" -eq 0 ]; then
    for arg in "$@"; do
      lower="$(lc "$arg")"
      case "$lower" in
        -a|-a*|-e|-e*|-a|-a*|-x|-x*) return 0 ;;
      esac
    done
  fi
  return 1
}

if is_sensitive_ps_form "$@" && ! is_privileged_identity; then
  echo "process listing guard deny: broad/full-command ps listings can leak inherited credentials" >&2
  echo "process listing guard deny: use a process-specific narrow form, e.g. ps -p <pid> -o pid,comm=, or request ops review" >&2
  exit 78
fi

exec "$real_cmd" "$@"
