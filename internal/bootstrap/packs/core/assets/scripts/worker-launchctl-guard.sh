#!/usr/bin/env bash
# Phase-3 tripwire for launchd inspection forms that can dump service env vars.
# This is not a hard boundary: absolute /bin/launchctl still bypasses PATH wrappers.
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
usage: worker-launchctl-guard.sh --real <path> -- [launchctl args...]

Blocks launchctl forms that print launchd environments into transcripts:
  launchctl print ...
  launchctl getenv ...
  launchctl export ...
  launchctl procinfo <pid>
  launchctl dumpstate

For supervisor status, use value-blind listing instead:
  launchctl list | grep gascity
USAGE
}

real_cmd=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --real) [ "$#" -ge 2 ] || { usage; exit 64; }; real_cmd="$2"; shift 2 ;;
    --) shift; break ;;
    -h|--help) usage; exit 0 ;;
    *) echo "launchctl guard: unknown option" >&2; usage; exit 64 ;;
  esac
done
[ -n "$real_cmd" ] || { echo "launchctl guard: --real is required" >&2; usage; exit 64; }

case "${1:-}" in
  print|getenv|export|procinfo|dumpstate)
    echo "launchctl guard deny: this launchctl subcommand can print launchd environment values into transcripts" >&2
    echo "launchctl guard deny: for supervisor status use: launchctl list | grep gascity; request ops review for env-bearing diagnostics" >&2
    exit 78
    ;;
esac

exec "$real_cmd" "$@"
