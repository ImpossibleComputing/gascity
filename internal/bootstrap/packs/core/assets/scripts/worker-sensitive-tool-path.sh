#!/usr/bin/env bash
# Author-only Phase-1 PATH routing helper.
# Do not source from live sessions until Paul ops-review + mayor review + rollback plan.
set -euo pipefail
this_dir=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]:-$0}")" && pwd)
asset_root=$(CDPATH= cd -- "$this_dir/.." && pwd)
wrapper_bin="$asset_root/worker-sensitive-tools/bin"
case ":${PATH:-}:" in
  *":$wrapper_bin:"*) ;;
  *) export PATH="$wrapper_bin:${PATH:-}" ;;
esac
export GC_REAL_GWS="${GC_REAL_GWS:-/opt/homebrew/bin/gws}"
export GC_REAL_PS="${GC_REAL_PS:-/bin/ps}"
export GC_REAL_LAUNCHCTL="${GC_REAL_LAUNCHCTL:-/bin/launchctl}"
export GC_CREDENTIAL_GUARD_ACTIVE="${GC_CREDENTIAL_GUARD_ACTIVE:-phase1-authored-not-live}"
