#!/bin/bash
# Unit test for the compact-duration alert logic added to compact/run.sh.
#
# Tests the alert boundary (fires on elapsed >= warn_secs, silent below)
# using a mock escalate.sh, without requiring a real Dolt server.
#
# Run: bash examples/bd/dolt/compact_duration_alert_test.sh
set -u

HERE=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
PACK_DIR="${PACK_DIR:-$HERE}"
NOTIFY_LIB="$PACK_DIR/assets/scripts/_notify.sh"

if [ ! -f "$NOTIFY_LIB" ]; then
  echo "FAIL: _notify.sh not found at $NOTIFY_LIB"
  exit 1
fi

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

# Mock escalate.sh: records subject+message to a file, returns 0.
# Use double-quoted heredoc so $ESCALATE_LOG and $WORK expand now,
# but \$2 and \$4 stay literal for the script's positional args.
ESCALATE_LOG="$WORK/escalations.log"
MOCK_ESCALATE="$WORK/escalate.sh"
cat > "$MOCK_ESCALATE" <<EOF
#!/bin/sh
printf 'ESCALATION: subject=%s message=%s\n' "\$2" "\$4" >> "$ESCALATE_LOG"
exit 0
EOF
chmod +x "$MOCK_ESCALATE"

# Failing escalate.sh: always returns 1.
FAIL_ESCALATE="$WORK/escalate_fail.sh"
cat > "$FAIL_ESCALATE" <<'EOF'
#!/bin/sh
exit 1
EOF
chmod +x "$FAIL_ESCALATE"

fail=0
pass() { echo "PASS: $1"; }
bad()  { echo "FAIL: $1"; fail=1; }

export GC_ESCALATE_SCRIPT="$MOCK_ESCALATE"
export GC_CITY_PATH="$WORK/city"
# shellcheck disable=SC1090
. "$NOTIFY_LIB"

# Helper: run the alert logic with controlled elapsed time.
# Mirrors the timing wrapper added to compact/run.sh.
run_compact_alert() {
  local db="$1"
  local elapsed="$2"
  local warn="$3"
  local compact_result="${4:-0}"
  local failed_count=0
  compact_warn_secs="$warn"
  _db_compact_elapsed="$elapsed"
  if [ "$compact_result" -ne 0 ]; then
    failed_count=$((failed_count + 1))
  fi
  if [ "$_db_compact_elapsed" -ge "$compact_warn_secs" ]; then
    dolt_escalate \
      "Dolt compact duration alert [MEDIUM] — db=${db}" \
      "compact for db=${db} took ${_db_compact_elapsed}s (warn_secs=${compact_warn_secs}). A slow compact may hold the DB write lock and stall agent/convoy writes. city=${GC_CITY_PATH}" \
      || printf 'compact: db=%s duration alert escalation failed (elapsed=%ss)\n' "$db" "$_db_compact_elapsed" >&2
  fi
  return $failed_count
}

# Test 1: Fast compact — no escalation fired.
rm -f "$ESCALATE_LOG"
run_compact_alert "beads" 0 300
if [ -f "$ESCALATE_LOG" ] && grep -q "ESCALATION" "$ESCALATE_LOG" 2>/dev/null; then
  bad "fast compact (0s < 300s) should not escalate"
else
  pass "fast compact (0s < 300s) — no escalation"
fi

# Test 2: Slow compact — escalation fires with correct fields.
rm -f "$ESCALATE_LOG"
run_compact_alert "beads" 2 1
if grep -q "ESCALATION" "$ESCALATE_LOG" 2>/dev/null; then
  pass "slow compact (2s >= 1s) — escalation fires"
else
  bad "slow compact (2s >= 1s) should escalate"
fi
if grep -q "db=beads" "$ESCALATE_LOG" 2>/dev/null && grep -q "took 2s" "$ESCALATE_LOG" 2>/dev/null; then
  pass "escalation message contains db and elapsed"
else
  bad "escalation message missing db or elapsed: $(cat "$ESCALATE_LOG" 2>/dev/null)"
fi

# Test 3: Threshold boundary — elapsed == warn_secs fires (>= semantics).
rm -f "$ESCALATE_LOG"
run_compact_alert "beads" 2 2
if grep -q "ESCALATION" "$ESCALATE_LOG" 2>/dev/null; then
  pass "boundary (2s >= 2s) — escalation fires"
else
  bad "boundary (2s >= 2s) should escalate (>= semantics)"
fi

# Test 4: Escalation failure — compact exits 0, warning on stderr.
# Directly override DOLT_ESCALATE_SCRIPT (already set from the first source).
DOLT_ESCALATE_SCRIPT="$FAIL_ESCALATE"
stderr_out="$WORK/stderr.txt"
run_compact_alert "beads" 5 1 2>"$stderr_out"
result=$?
if [ "$result" -eq 0 ]; then
  pass "escalation failure — compact exits 0 (not failed)"
else
  bad "escalation failure should not mark compact as failed (got exit $result)"
fi
if grep -q "duration alert escalation failed" "$stderr_out" 2>/dev/null; then
  pass "escalation failure — warning on stderr"
else
  bad "escalation failure should warn on stderr: $(cat "$stderr_out" 2>/dev/null)"
fi

echo "----"
if [ "$fail" -eq 0 ]; then echo "ALL PASS"; else echo "FAILURES PRESENT"; fi
exit "$fail"
