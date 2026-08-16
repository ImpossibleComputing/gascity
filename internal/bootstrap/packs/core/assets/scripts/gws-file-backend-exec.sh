#!/usr/bin/env bash
# Execute googleworkspace-cli with the file keyring backend forced on.
#
# This is an explicit wrapper for gws call sites (launchd plists, tools, agent
# configs) that need to avoid the macOS keychain backend in headless/locked
# contexts. It is intentionally not installed as a transparent PATH shim by this
# asset; operators should wire reviewed gws call sites to this wrapper or set the
# same env var at a proven injection point.
set -euo pipefail

usage() {
  cat <<'USAGE'
usage: gws-file-backend-exec.sh [--real-gws PATH] -- <gws args...>
       gws-file-backend-exec.sh [--real-gws PATH] <gws args...>

Forces GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND=file, then execs gws.
Environment:
  GWS_FILE_BACKEND_REAL_GWS   real gws path (default: PATH gws or /opt/homebrew/bin/gws)
USAGE
}

REAL_GWS="${GWS_FILE_BACKEND_REAL_GWS:-}"
case "${1:-}" in
  -h|--help) usage; exit 0 ;;
esac
if [ "${1:-}" = "--real-gws" ]; then
  if [ "$#" -lt 2 ]; then
    echo "--real-gws requires a path" >&2
    usage >&2
    exit 64
  fi
  REAL_GWS="$2"
  shift 2
fi
if [ "${1:-}" = "--" ]; then
  shift
fi
if [ "$#" -eq 0 ]; then
  usage >&2
  exit 64
fi
if [ -z "$REAL_GWS" ]; then
  REAL_GWS="$(command -v gws 2>/dev/null || true)"
fi
if [ -z "$REAL_GWS" ]; then
  REAL_GWS="/opt/homebrew/bin/gws"
fi
if [ ! -x "$REAL_GWS" ]; then
  echo "gws-file-backend-exec: real gws is not executable: $REAL_GWS" >&2
  exit 127
fi

export GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND=file
exec "$REAL_GWS" "$@"
