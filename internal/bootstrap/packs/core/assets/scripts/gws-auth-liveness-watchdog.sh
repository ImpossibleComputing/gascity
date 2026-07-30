#!/usr/bin/env bash
# Independent mayor gws auth liveness watchdog.
#
# This probes file presence and a file-backend `gws auth status`, then alerts via
# gc session nudge + gc mail.  The alert path deliberately does NOT use gws so a
# broken Gmail credential cannot make its own outage silent.
set -euo pipefail

CONFIG_DIR="${GWS_AUTH_WATCHDOG_CONFIG_DIR:-$HOME/.config/gws}"
GWS_BIN="${GWS_AUTH_WATCHDOG_GWS:-$(command -v gws 2>/dev/null || printf '/opt/homebrew/bin/gws')}"
GC_BIN="${GWS_AUTH_WATCHDOG_GC:-$(command -v gc 2>/dev/null || printf '/opt/homebrew/bin/gc')}"
STATE_FILE="${GWS_AUTH_WATCHDOG_STATE:-$HOME/gt/.gc/runtime/gws-auth-watchdog.state}"
NOTIFY_TO="${GWS_AUTH_WATCHDOG_NOTIFY_TO:-mayor}"
COOLDOWN_SECONDS="${GWS_AUTH_WATCHDOG_COOLDOWN_SECONDS:-900}"
DRY_RUN=0

usage() {
  cat <<'USAGE'
usage: gws-auth-liveness-watchdog.sh [--dry-run]

Checks mayor gws auth without depending on gws for alert delivery. Environment:
  GWS_AUTH_WATCHDOG_CONFIG_DIR       gws config dir (default: $HOME/.config/gws)
  GWS_AUTH_WATCHDOG_GWS              gws binary (default: PATH gws or /opt/homebrew/bin/gws)
  GWS_AUTH_WATCHDOG_GC               gc binary (default: PATH gc or /opt/homebrew/bin/gc)
  GWS_AUTH_WATCHDOG_STATE            cooldown state file
  GWS_AUTH_WATCHDOG_NOTIFY_TO        gc alias to nudge/mail (default: mayor)
  GWS_AUTH_WATCHDOG_COOLDOWN_SECONDS alert cooldown per failure signature (default: 900)
USAGE
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 64 ;;
  esac
done

now_epoch() { date +%s; }

signature_for() {
  # cksum is available on macOS and Linux; no secret material is included.
  printf '%s' "$1" | cksum | awk '{print $1}'
}

cooldown_allows() {
  local reason="$1" now last_epoch last_sig sig
  now="$(now_epoch)"
  sig="$(signature_for "$reason")"
  if [ -f "$STATE_FILE" ]; then
    # shellcheck disable=SC2162
    read last_epoch last_sig <"$STATE_FILE" || true
    if [ "${last_sig:-}" = "$sig" ] && [ $((now - ${last_epoch:-0})) -lt "$COOLDOWN_SECONDS" ]; then
      echo "alert suppressed by cooldown for signature=$sig age=$((now - ${last_epoch:-0}))s"
      return 1
    fi
  fi
  mkdir -p "$(dirname "$STATE_FILE")"
  printf '%s %s\n' "$now" "$sig" >"$STATE_FILE"
  return 0
}

notify_failure() {
  local reason="$1"
  local body
  body="GWS AUTH LIVENESS ALARM for ${CONFIG_DIR}

${reason}

This alert was sent via gc session nudge + gc mail, not via gws/Gmail. It does not include credential contents. Recovery should verify the file backend posture before reloading gmail_watch.py."

  if ! cooldown_allows "$reason"; then
    return 0
  fi

  if [ "$DRY_RUN" = 1 ]; then
    printf 'DRY-RUN would alert %s:\n%s\n' "$NOTIFY_TO" "$body"
    return 0
  fi

  # `gc mail send -m` with complex inline bodies has caused quoting accidents in
  # live ops. Keep the body as data in a temporary file.
  local tmp
  tmp="$(mktemp -t gws-auth-watchdog.XXXXXX)"
  printf '%s\n' "$body" >"$tmp"

  "$GC_BIN" session nudge "$NOTIFY_TO" "$body" >/dev/null 2>&1 || true
  "$GC_BIN" mail send "$NOTIFY_TO" -s "gws auth liveness alarm" -m "$(cat "$tmp")" >/dev/null 2>&1 || true
  rm -f "$tmp"
}

fail() {
  local reason="$1"
  echo "FAIL gws auth liveness: $reason" >&2
  notify_failure "$reason"
  exit 2
}

if [ ! -d "$CONFIG_DIR" ]; then
  fail "config dir is missing: $CONFIG_DIR"
fi
if [ ! -s "$CONFIG_DIR/credentials.enc" ]; then
  fail "credentials.enc is missing or empty under $CONFIG_DIR"
fi
if [ ! -s "$CONFIG_DIR/.encryption_key" ]; then
  # This is intentionally a hard failure before invoking gws. The destructive
  # class we are guarding is keychain-backed gws in a headless/locked-keychain
  # context deleting credentials.enc. A missing file-backend key means the safe
  # posture is absent or unproven.
  fail ".encryption_key is missing or empty under $CONFIG_DIR; refusing an active gws probe because file-backend protection is not proven"
fi

status_out=""
status_err=""
status_rc=0
status_tmp="$(mktemp -t gws-auth-status.XXXXXX)"
err_tmp="$(mktemp -t gws-auth-status.err.XXXXXX)"
trap 'rm -f "$status_tmp" "$err_tmp"' EXIT
set +e
GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND=file "$GWS_BIN" auth status >"$status_tmp" 2>"$err_tmp"
status_rc=$?
set -e
status_out="$(cat "$status_tmp")"
status_err="$(cat "$err_tmp")"

if [ "$status_rc" -ne 0 ]; then
  fail "gws auth status failed rc=$status_rc under GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND=file; stderr tail: $(printf '%s' "$status_err" | tail -c 300)"
fi
if printf '%s\n%s' "$status_out" "$status_err" | grep -Eiq '(^|[^0-9])401([^0-9]|$)|No credentials found|invalid_grant|unauthorized'; then
  fail "gws auth status reported an auth failure under file backend"
fi

auth_method="$(python3 - "$status_tmp" <<'PY' 2>/dev/null || true
import json, re, sys
text = open(sys.argv[1], encoding='utf-8', errors='replace').read()
try:
    obj = json.loads(text)
except Exception:
    obj = None
if isinstance(obj, dict):
    for key in ('auth_method', 'authMethod', 'method'):
        val = obj.get(key)
        if isinstance(val, str):
            print(val.strip())
            raise SystemExit(0)
for pat in (r'auth_method\s*[:=]\s*([A-Za-z0-9_.-]+)', r'auth method\s*[:=]\s*([A-Za-z0-9_.-]+)'):
    m = re.search(pat, text, flags=re.I)
    if m:
        print(m.group(1).strip())
        raise SystemExit(0)
PY
)"

if [ -n "$auth_method" ] && [ "$auth_method" != "oauth2" ]; then
  fail "gws auth status auth_method=$auth_method, want oauth2"
fi

# If the installed gws does not expose auth_method, do not false-page: the file
# posture and zero-401 command success are still a liveness signal. Keep this
# visible so ops can tighten parsing once the binary exposes a stable schema.
if [ -z "$auth_method" ]; then
  echo "OK gws auth liveness: credentials.enc + .encryption_key present; auth status rc=0/no 401; auth_method not exposed"
else
  echo "OK gws auth liveness: auth_method=$auth_method"
fi
