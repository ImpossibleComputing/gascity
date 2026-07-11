#!/usr/bin/env bash
# Redacted audit of launchd supervisor EnvironmentVariables.
# Prints variable names and metadata only; never prints, hashes, or persists values.
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
usage: supervisor-env-surface-audit.sh [--plist <path>]

Safely inventories a launchd plist EnvironmentVariables surface without printing
secret values. Defaults to ~/Library/LaunchAgents/com.gascity.supervisor.plist.
USAGE
}

plist="${HOME:-}/Library/LaunchAgents/com.gascity.supervisor.plist"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --plist) [ "$#" -ge 2 ] || { usage; exit 64; }; plist="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown arg: $1" >&2; usage; exit 64 ;;
  esac
done

PLIST_PATH="$plist" python3 - <<'PY'
from pathlib import Path
import os
import plistlib
import stat
import sys

path = Path(os.environ["PLIST_PATH"]).expanduser()
print("supervisor_env_surface_audit schema=1")
print(f"plist={path}")
if not path.exists():
    print("status=missing")
    sys.exit(66)
try:
    st = path.stat()
    print(f"mode={stat.filemode(st.st_mode)} uid={st.st_uid} gid={st.st_gid} size_bytes={st.st_size}")
    with path.open("rb") as f:
        plist = plistlib.load(f)
except Exception as exc:
    print(f"status=error error={type(exc).__name__}", file=sys.stderr)
    sys.exit(65)

env = plist.get("EnvironmentVariables") or {}
if not isinstance(env, dict):
    print("status=error error=EnvironmentVariables_not_dict", file=sys.stderr)
    sys.exit(65)

print(f"status=ok env_key_count={len(env)}")
sensitive_names = []
for key in sorted(str(k) for k in env.keys()):
    upper = key.upper()
    sensitive = any(token in upper for token in ("KEY", "TOKEN", "SECRET", "PASSWORD", "CREDENTIAL"))
    if sensitive:
        sensitive_names.append(key)
    # Deliberately do not inspect/print lengths, hashes, prefixes, or values.
    label = "sensitive-name" if sensitive else "non-sensitive-name"
    print(f"env {key} {label} value=REDACTED")

print(f"sensitive_name_count={len(sensitive_names)}")
if sensitive_names:
    print("sensitive_names=" + ",".join(sensitive_names))
print("note=values_not_printed_hashed_or_persisted")
PY
