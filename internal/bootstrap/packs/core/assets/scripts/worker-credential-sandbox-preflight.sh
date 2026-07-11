#!/usr/bin/env bash
# Preflight a sandbox-exec credential isolation profile without printing secrets.
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
usage: worker-credential-sandbox-preflight.sh --profile <profile.sb> --city <city-root> [--home <home-dir>] [--https-push-remote <remote>] [--https-push-ref <ref>]

Checks that the profile permits ordinary city reads, denies credential path reads,
denies sandboxed writes/creates under credential directories, and optionally
checks that GitHub publication still works over HTTPS+gh-token with ~/.ssh denied.
USAGE
}

profile=""
city=""
home_dir="${HOME:-}"
https_push_remote=""
https_push_ref=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --profile) [ "$#" -ge 2 ] || { usage; exit 64; }; profile="$2"; shift 2 ;;
    --city) [ "$#" -ge 2 ] || { usage; exit 64; }; city="$2"; shift 2 ;;
    --home) [ "$#" -ge 2 ] || { usage; exit 64; }; home_dir="$2"; shift 2 ;;
    --https-push-remote) [ "$#" -ge 2 ] || { usage; exit 64; }; https_push_remote="$2"; shift 2 ;;
    --https-push-ref) [ "$#" -ge 2 ] || { usage; exit 64; }; https_push_ref="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown arg: $1" >&2; usage; exit 64 ;;
  esac
done
[ -n "$profile" ] || { echo "--profile required" >&2; exit 64; }
[ -n "$city" ] || { echo "--city required" >&2; exit 64; }
[ -n "$home_dir" ] || { echo "--home or HOME required" >&2; exit 64; }
[ -f "$profile" ] || { echo "profile not found: $profile" >&2; exit 66; }
[ -d "$city" ] || { echo "city not found: $city" >&2; exit 66; }
if { [ -n "$https_push_remote" ] && [ -z "$https_push_ref" ]; } || { [ -z "$https_push_remote" ] && [ -n "$https_push_ref" ]; }; then
  echo "--https-push-remote and --https-push-ref must be provided together" >&2
  exit 64
fi

run_sandbox() {
  sandbox-exec \
    -D "GWS_CONFIG=$home_dir/.config/gws" \
    -D "GCLOUD_CONFIG=$home_dir/.config/gcloud" \
    -D "AWS_CONFIG=$home_dir/.aws" \
    -D "CITY_SECRETS=$city/.secrets" \
    -D "HOME_SECRETS=$home_dir/.secrets" \
    -D "HOME_SSH=$home_dir/.ssh" \
    -D "BROWSER_PROFILES=$home_dir/Library/Application Support" \
    -D "GC_SECURITY=$city/.gc/security" \
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
    local parent
    parent="$(dirname "$path")"
    if [ ! -d "$parent" ]; then
      echo "SKIP deny $label create (missing parent: $parent)"
      return 0
    fi
    if run_sandbox sh -c 'mkdir "$1"' _ "$path"; then
      # The path should not be creatable from inside the sandbox. Remove only the
      # empty directory we just created, then fail loudly.
      rmdir "$path" 2>/dev/null || true
      echo "FAIL deny $label: sandbox allowed create ($path)" >&2
      exit 1
    fi
    echo "PASS deny $label create"
    return 0
  fi
  if run_sandbox sh -c 'test -r "$1"' _ "$path"; then
    echo "FAIL deny $label: sandbox allowed read ($path)" >&2
    exit 1
  fi
  echo "PASS deny $label read"
  if [ -d "$path" ]; then
    must_deny_write "$label" "$path"
  fi
}

must_deny_write() {
  local label="$1" path="$2"
  if [ ! -d "$path" ]; then
    echo "SKIP deny $label write (missing path: $path)"
    return 0
  fi
  local probe="$path/.worker-sandbox-write-probe.$$"
  if run_sandbox sh -c 'printf probe > "$1"' _ "$probe"; then
    rm -f "$probe" 2>/dev/null || true
    echo "FAIL deny $label: sandbox allowed write ($probe)" >&2
    exit 1
  fi
  echo "PASS deny $label write"
}

must_allow_https_push() {
  if [ -z "$https_push_remote" ]; then
    echo "SKIP allow https git push (no --https-push-remote/--https-push-ref provided)"
    return 0
  fi
  case "$https_push_remote" in
    https://github.com/*|git@github.com:*) ;;
    *) echo "FAIL allow https git push: remote must target github.com ($https_push_remote)" >&2; exit 64 ;;
  esac
  if ! run_sandbox git -C "$city" push --dry-run "$https_push_remote" "HEAD:$https_push_ref" >/dev/null; then
    echo "FAIL allow https git push: sandboxed dry-run push failed ($https_push_remote HEAD:$https_push_ref)" >&2
    exit 1
  fi
  echo "PASS allow https git push"
}

must_allow "city board" "$city/teams/rd-board.md"
must_deny "gws config" "$home_dir/.config/gws"
must_deny "gcloud config" "$home_dir/.config/gcloud"
must_deny "aws config" "$home_dir/.aws"
must_deny "city secrets" "$city/.secrets"
must_deny "home secrets" "$home_dir/.secrets"
must_deny "ssh keys" "$home_dir/.ssh"
must_deny_write "gc security" "$city/.gc/security"
# Browser profile root is intentionally broad; missing is fine on headless hosts.
must_deny "browser profiles" "$home_dir/Library/Application Support"
must_allow_https_push

echo "PASS worker credential sandbox preflight"
