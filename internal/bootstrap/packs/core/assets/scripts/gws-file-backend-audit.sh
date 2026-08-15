#!/usr/bin/env bash
# Static audit for gws call sites that are missing the file keyring backend.
#
# Scope is deliberately narrow and value-blind: scan reviewed invocation loci
# (tools scripts, orders plists, agent.toml files, and LaunchAgents plists) for
# files that appear to call gws, then require either the explicit backend env or
# the reviewed gws-file-backend wrapper in the same file. It does not read gws
# credential files and it does not prove live OAuth/account health.
set -euo pipefail

DEFAULT_HOME="${HOME:-}"
ROOT="${GWS_FILE_BACKEND_AUDIT_ROOT:-${DEFAULT_HOME:+$DEFAULT_HOME/gt}}"
LAUNCHAGENTS="${GWS_FILE_BACKEND_AUDIT_LAUNCHAGENTS:-${DEFAULT_HOME:+$DEFAULT_HOME/Library/LaunchAgents}}"
ROOT="${ROOT:-.}"
LAUNCHAGENTS="${LAUNCHAGENTS:-/nonexistent}"

usage() {
  cat <<'USAGE'
usage: gws-file-backend-audit.sh [--root DIR] [--launchagents DIR]

Scans known gws invocation loci and exits non-zero if any apparent gws caller is
missing GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND=file (or gws-file-backend-exec.sh).
No credential values or gws config files are read.
USAGE
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --root) [ "$#" -ge 2 ] || { echo "--root requires a directory" >&2; exit 64; }; ROOT="$2"; shift 2 ;;
    --launchagents) [ "$#" -ge 2 ] || { echo "--launchagents requires a directory" >&2; exit 64; }; LAUNCHAGENTS="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 64 ;;
  esac
done

candidate_files() {
  if [ -d "$ROOT/tools" ]; then
    find "$ROOT/tools" -maxdepth 1 -type f \( -name '*.sh' -o -name '*.py' \) -print
  fi
  if [ -d "$ROOT/orders" ]; then
    find "$ROOT/orders" -maxdepth 1 -type f -name '*.plist' -print
  fi
  if [ -d "$ROOT/agents" ]; then
    find "$ROOT/agents" -maxdepth 2 -type f -name 'agent.toml' -print
  fi
  if [ -d "$LAUNCHAGENTS" ]; then
    find "$LAUNCHAGENTS" -maxdepth 1 -type f -name '*.plist' -print
  fi
}

appears_to_call_gws() {
  local file="$1"
  python3 - "$file" <<'PY'
import pathlib, re, sys
text = pathlib.Path(sys.argv[1]).read_text(encoding='utf-8', errors='replace')
# Comments often document gws recovery without being an invocation locus. Strip
# XML comments and full-line shell/Python comments before matching.
text = re.sub(r'<!--.*?-->', '', text, flags=re.S)
text = '\n'.join(line for line in text.splitlines() if not line.lstrip().startswith('#'))
patterns = [
    r'(?<![A-Za-z0-9_./-])gws(?![A-Za-z0-9_-])',
    r'/opt/homebrew/bin/gws(?![A-Za-z0-9_-])',
    r'googleworkspace-cli',
    r'gws-file-backend-exec\.sh',
]
raise SystemExit(0 if any(re.search(p, text) for p in patterns) else 1)
PY
}

has_file_backend_guard() {
  local file="$1"
  python3 - "$file" <<'PY'
import pathlib, re, sys
text = pathlib.Path(sys.argv[1]).read_text(encoding='utf-8', errors='replace')
if 'gws-file-backend-exec.sh' in text:
    raise SystemExit(0)
# Shell/Python/env/plist/TOML forms. Require the variable and a nearby literal file.
for m in re.finditer(r'GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND', text, flags=re.I):
    window = text[m.start():m.start()+240]
    if re.search(r'(?:=|<string>|:)\s*["\']?file["\']?', window, flags=re.I):
        raise SystemExit(0)
raise SystemExit(1)
PY
}

unsafe=0
checked=0
while IFS= read -r file; do
  [ -f "$file" ] || continue
  if appears_to_call_gws "$file"; then
    checked=$((checked + 1))
    if has_file_backend_guard "$file"; then
      printf 'OK gws-file-backend-audit: %s\n' "$file"
    else
      printf 'FAIL gws-file-backend-audit: missing file backend guard: %s\n' "$file" >&2
      unsafe=$((unsafe + 1))
    fi
  fi
done < <(candidate_files | sort -u)

if [ "$unsafe" -gt 0 ]; then
  printf 'FAIL gws-file-backend-audit: %d/%d apparent gws call sites missing file backend guard\n' "$unsafe" "$checked" >&2
  exit 1
fi
printf 'OK gws-file-backend-audit: %d apparent gws call sites protected\n' "$checked"
