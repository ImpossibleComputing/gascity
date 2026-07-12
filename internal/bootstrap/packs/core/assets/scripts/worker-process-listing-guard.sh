#!/usr/bin/env bash
# Phase-3 tripwire for process listings that can leak inherited env/API keys.
# This is not a hard boundary: absolute /bin/ps still bypasses PATH wrappers.
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
usage: worker-process-listing-guard.sh --real <path> -- [ps args...]

Blocks broad/full-command ps forms. Use the process-specific narrow output
allowlist: ps -p <pid> -o pid,comm=
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

is_pid_list() {
  case "$1" in
    ''|*[!0-9,]*) return 1 ;;
    *) return 0 ;;
  esac
}

is_narrow_output() {
  case "$(lc "$1")" in
    pid,comm|pid,comm=|pid=,comm|pid=,comm=) return 0 ;;
    *) return 1 ;;
  esac
}

is_allowed_narrow_ps_form() {
  # No-arg ps is narrow enough for now; it is not the broad env/full-command
  # form that caused the transcript leak.
  [ "$#" -gt 0 ] || return 0

  case "$#" in
    4)
      if { [ "$1" = "-p" ] || [ "$1" = "--pid" ]; } && is_pid_list "$2" && [ "$3" = "-o" ] && is_narrow_output "$4"; then
        return 0
      fi
      if [ "$1" = "-o" ] && is_narrow_output "$2" && { [ "$3" = "-p" ] || [ "$3" = "--pid" ]; } && is_pid_list "$4"; then
        return 0
      fi
      ;;
    3)
      case "$1" in
        -p[0-9]*|--pid=[0-9]*)
          pid="${1#-p}"
          pid="${pid#--pid=}"
          if is_pid_list "$pid" && [ "$2" = "-o" ] && is_narrow_output "$3"; then
            return 0
          fi
          ;;
      esac
      ;;
  esac
  return 1
}

if ! is_allowed_narrow_ps_form "$@"; then
  echo "process listing guard deny: broad/full-command ps listings can leak inherited credentials" >&2
  echo "process listing guard deny: use a process-specific narrow form, e.g. ps -p <pid> -o pid,comm=, or request ops review" >&2
  exit 78
fi

exec "$real_cmd" "$@"
