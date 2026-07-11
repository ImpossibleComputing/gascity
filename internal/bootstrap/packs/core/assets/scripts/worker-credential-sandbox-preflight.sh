#!/usr/bin/env bash
# Preflight a sandbox-exec credential isolation profile without printing secrets.
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
usage: worker-credential-sandbox-preflight.sh --profile <profile.sb> --city <city-root> [--home <home-dir>]

Checks that the profile permits ordinary city reads but denies file reads from:
  $home/.config/gws, $city/.secrets, $home/.secrets, $home/.ssh, and browser profiles.
USAGE
}

profile=""
city=""
home_dir="${HOME:-}"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --profile) [ "$#" -ge 2 ] || { usage; exit 64; }; profile="$2"; shift 2 ;;
    --city) [ "$#" -ge 2 ] || { usage; exit 64; }; city="$2"; shift 2 ;;
    --home) [ "$#" -ge 2 ] || { usage; exit 64; }; home_dir="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown arg: $1" >&2; usage; exit 64 ;;
  esac
done
[ -n "$profile" ] || { echo "--profile required" >&2; exit 64; }
[ -n "$city" ] || { echo "--city required" >&2; exit 64; }
[ -n "$home_dir" ] || { echo "--home or HOME required" >&2; exit 64; }
[ -f "$profile" ] || { echo "profile not found: $profile" >&2; exit 66; }
[ -d "$city" ] || { echo "city not found: $city" >&2; exit 66; }

run_sandbox() {
  sandbox-exec \
    -D "GWS_CONFIG=$home_dir/.config/gws" \
    -D "CITY_SECRETS=$city/.secrets" \
    -D "HOME_SECRETS=$home_dir/.secrets" \
    -D "HOME_SSH=$home_dir/.ssh" \
    -D "BROWSER_PROFILES=$home_dir/Library/Application Support" \
    -f "$profile" -- "$@"
}

must_allow() {
  local label="$1" path="$2"
  if ! run_sandbox sh -c 'test -r "$1"' _ "$path"; then
    echo "FAIL allow $label: sandbox blocked expected city read ($path)" >&2
    exit 1
  fi
  echo "PASS allow $label"
}

must_deny() {
  local label="$1" path="$2"
  if [ ! -e "$path" ]; then
    echo "SKIP deny $label (missing path: $path)"
    return 0
  fi
  if run_sandbox sh -c 'test -r "$1"' _ "$path"; then
    echo "FAIL deny $label: sandbox allowed read ($path)" >&2
    exit 1
  fi
  echo "PASS deny $label"
}

must_allow "city board" "$city/teams/rd-board.md"
must_deny "gws config" "$home_dir/.config/gws"
must_deny "city secrets" "$city/.secrets"
must_deny "home secrets" "$home_dir/.secrets"
must_deny "ssh keys" "$home_dir/.ssh"
# Browser profile root is intentionally broad; missing is fine on headless hosts.
must_deny "browser profiles" "$home_dir/Library/Application Support"

echo "PASS worker credential sandbox preflight"
