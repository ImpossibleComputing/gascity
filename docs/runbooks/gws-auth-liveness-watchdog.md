# gws auth liveness watchdog

## Why this exists

The mayor Gmail path has failed silently more than once when `gws` auth material
under `~/.config/gws` vanished.  The damaging part is not just the auth outage:
`gmail_watch.py` depends on `gws gmail +triage`, and when auth is broken it can
return no messages, so the same path that should notice new founder mail cannot
reliably report its own credential death.

This watchdog is deliberately **independent of gws for alert delivery**.  It can
probe `gws` auth, but if it finds a problem it alerts with:

1. `gc session nudge mayor ...`
2. `gc mail send mayor ...`

No Gmail/gws send is used for the alarm.

## Root-cause model and current security boundary

Grounded findings from the 2026-07-22/23 repair notes:

- `gws` defaults to the macOS keychain backend unless
  `GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND=file` is present at invocation/provision
time.
- In a headless or locked-keychain context, keychain-backed `gws` can misread the
token as unusable and delete `credentials.enc`.
- The file backend creates a local `.encryption_key` next to `credentials.enc`.
  That on-disk key is the durable signal that the config is not relying on the
  login keychain.
- The worker sandbox denies file operations under the gws config path, so moving
  mayor@ to file-backend gws does **not** by itself reopen the 2026-07-11
  pool-worker credential boundary. From a sandboxed worker, attempts to stat
  `~/.config/gws` should fail; do not weaken that boundary.
- Do **not** restore or reuse
  `~/.config/gws.DISABLED-pool-worker-incident-20260711-105921`.

Sanity check for any mayor@ re-auth: it is consistent with the pool-worker
boundary only if it is mayor@'s own config, behind the sandbox-denied gws path,
least-privilege for the watcher use case, and provisioned with
`GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND=file`. If a re-auth command omitted that
env var, treat file-backend durability as **unproven** until an unsandboxed ops
session verifies `.encryption_key` exists and `gws auth status` works with the
file backend.

## Watchdog behavior

`assets/scripts/gws-auth-liveness-watchdog.sh` checks:

1. config dir exists;
2. `credentials.enc` exists and is non-empty;
3. `.encryption_key` exists and is non-empty;
4. `GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND=file gws auth status` exits zero and
   does not report a 401/no-credentials/invalid-grant style failure;
5. if `auth_method` is exposed, it must be `oauth2`.

The `.encryption_key` check is fail-closed and happens **before** invoking
`gws`. This avoids making the watchdog itself another keychain-backend
self-delete trigger.

Alerts are cooldown-limited by failure signature. The message body is staged in a
temporary file before `gc mail send`; do not inline complex mail bodies in shell
commands around credential tools.

## Example launchd install

Use a calm ops window. This is a source runbook; adapt paths if the core pack is
materialized elsewhere.

```bash
set -euo pipefail
cd ~/gt/rigs/gascity
install -m 0755 internal/bootstrap/packs/core/assets/scripts/gws-auth-liveness-watchdog.sh \
  ~/gt/tools/gws-auth-liveness-watchdog.sh

cat > ~/Library/LaunchAgents/com.impossiblecomputing.gt.gws-auth-watchdog.plist <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.impossiblecomputing.gt.gws-auth-watchdog</string>
  <key>ProgramArguments</key>
  <array>
    <string>/Users/qeetbastudio/gt/tools/gws-auth-liveness-watchdog.sh</string>
  </array>
  <key>StartInterval</key><integer>300</integer>
  <key>RunAtLoad</key><true/>
  <key>WorkingDirectory</key><string>/Users/qeetbastudio/gt</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>HOME</key><string>/Users/qeetbastudio</string>
    <key>PATH</key><string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
    <key>GWS_AUTH_WATCHDOG_CONFIG_DIR</key><string>/Users/qeetbastudio/.config/gws</string>
    <key>GWS_AUTH_WATCHDOG_STATE</key><string>/Users/qeetbastudio/gt/.gc/runtime/gws-auth-watchdog.state</string>
    <key>GWS_AUTH_WATCHDOG_NOTIFY_TO</key><string>mayor</string>
    <key>GWS_AUTH_WATCHDOG_COOLDOWN_SECONDS</key><string>900</string>
  </dict>
  <key>StandardOutPath</key><string>/Users/qeetbastudio/gt/.gc/runtime/gws-auth-watchdog.out</string>
  <key>StandardErrorPath</key><string>/Users/qeetbastudio/gt/.gc/runtime/gws-auth-watchdog.err</string>
</dict>
</plist>
PLIST

plutil -lint ~/Library/LaunchAgents/com.impossiblecomputing.gt.gws-auth-watchdog.plist
~/gt/tools/gws-auth-liveness-watchdog.sh --dry-run
launchctl bootout gui/$(id -u)/com.impossiblecomputing.gt.gws-auth-watchdog 2>/dev/null || true
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.impossiblecomputing.gt.gws-auth-watchdog.plist
launchctl kickstart -k gui/$(id -u)/com.impossiblecomputing.gt.gws-auth-watchdog
```

Expected healthy output:

```text
OK gws auth liveness: auth_method=oauth2
```

If the output says `.encryption_key` is missing, do not reload the Gmail watcher
as "fixed" yet; re-provision or verify the config with an unsandboxed ops session
using `GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND=file` first.
