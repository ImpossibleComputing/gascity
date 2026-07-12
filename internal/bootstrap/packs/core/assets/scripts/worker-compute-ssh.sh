#!/usr/bin/env bash
# Phase-3 compute SSH launcher contract.
#
# This is a broker/stopgap helper, not a credential broker by itself. It makes
# sanctioned compute SSH explicit: never consult ~/.ssh, use an explicit
# runtime-provided identity/certificate and pinned known_hosts file, and force
# IdentitiesOnly so OpenSSH does not fall back to ambient user keys.
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
usage: worker-compute-ssh.sh --real <ssh> --identity <path> --known-hosts <path> [--certificate <path>] -- [ssh args...]

Launch ssh for sandboxed compute access without touching ~/.ssh. Identity,
certificate, and known_hosts paths must be absolute, explicit, and outside
~/.ssh. The wrapper rejects user-supplied ssh config/identity/known-host
override flags.
USAGE
}

real_cmd=""
identity=""
certificate=""
known_hosts=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --real) [ "$#" -ge 2 ] || { usage; exit 64; }; real_cmd="$2"; shift 2 ;;
    --identity) [ "$#" -ge 2 ] || { usage; exit 64; }; identity="$2"; shift 2 ;;
    --certificate) [ "$#" -ge 2 ] || { usage; exit 64; }; certificate="$2"; shift 2 ;;
    --known-hosts) [ "$#" -ge 2 ] || { usage; exit 64; }; known_hosts="$2"; shift 2 ;;
    --) shift; break ;;
    -h|--help) usage; exit 0 ;;
    *) echo "compute ssh guard: unknown option: $1" >&2; usage; exit 64 ;;
  esac
done

[ -n "$real_cmd" ] || { echo "compute ssh guard: --real is required" >&2; usage; exit 64; }
[ -n "$identity" ] || { echo "compute ssh guard: --identity is required" >&2; usage; exit 64; }
[ -n "$known_hosts" ] || { echo "compute ssh guard: --known-hosts is required" >&2; usage; exit 64; }
[ "$#" -gt 0 ] || { echo "compute ssh guard: ssh destination/args are required" >&2; usage; exit 64; }

home_ssh="${HOME:-}/.ssh"

is_home_ssh_path() {
  local path="$1"
  [ -n "$path" ] || return 1
  case "$path" in
    ~/.ssh|~/.ssh/*) return 0 ;;
  esac
  if [ -n "$home_ssh" ]; then
    case "$path" in
      "$home_ssh"|"$home_ssh"/*) return 0 ;;
    esac
  fi
  return 1
}

require_absolute_path() {
  local label="$1" path="$2"
  case "$path" in
    /*) return 0 ;;
    *) echo "compute ssh guard deny: $label must be an absolute path" >&2; exit 78 ;;
  esac
}

require_not_home_ssh() {
  local label="$1" path="$2"
  if is_home_ssh_path "$path"; then
    echo "compute ssh guard deny: $label must not be under ~/.ssh" >&2
    exit 78
  fi
}

require_absolute_path "identity" "$identity"
require_absolute_path "known_hosts" "$known_hosts"
require_not_home_ssh "identity" "$identity"
require_not_home_ssh "known_hosts" "$known_hosts"
if [ -n "$certificate" ]; then
  require_absolute_path "certificate" "$certificate"
  require_not_home_ssh "certificate" "$certificate"
fi

# Deny caller-supplied options that could re-enable ambient ~/.ssh material,
# bypass pinned host keys, forward local credentials, or execute proxy/local
# helpers outside this contract.
prev=""
for arg in "$@"; do
  lower="$(printf '%s' "$arg" | tr '[:upper:]' '[:lower:]')"
  if [ "$prev" = "-f" ] || [ "$prev" = "-i" ] || [ "$prev" = "-o" ] || [ "$prev" = "-j" ] || [ "$prev" = "-w" ]; then
    echo "compute ssh guard deny: caller may not override ssh config/identity/options" >&2
    exit 78
  fi
  case "$lower" in
    -f|-i|-i*|-f*|-o|-a|-a*|-j|-j*|-w|-w*) echo "compute ssh guard deny: caller may not override ssh config/identity/options" >&2; exit 78 ;;
    -oidentityfile=*|-ocertificatefile=*|-ouserknownhostsfile=*|-oglobalknownhostsfile=*|-oidentitiesonly=*|-oproxycommand=*|-oproxyjump=*|-olocalcommand=*|-opermitlocalcommand=*|-oforwardagent=*|-opubkeyauthentication=*|-opasswordauthentication=*|-opreferredauthentications=*)
      echo "compute ssh guard deny: caller may not override ssh config/identity/options" >&2
      exit 78
      ;;
  esac
  prev="$lower"
done

args=(
  -F /dev/null
  -o IdentitiesOnly=yes
  -o IdentityFile="$identity"
  -o UserKnownHostsFile="$known_hosts"
  -o GlobalKnownHostsFile=/dev/null
  -o PasswordAuthentication=no
  -o PreferredAuthentications=publickey
  -o ForwardAgent=no
)
if [ -n "$certificate" ]; then
  args+=( -o CertificateFile="$certificate" )
fi

exec "$real_cmd" "${args[@]}" "$@"
