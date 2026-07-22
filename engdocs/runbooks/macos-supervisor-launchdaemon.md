# macOS supervisor LaunchDaemon hardening

Owner: heimdall  
Request: paul / Mike, 2026-07-22 — decide whether `com.gascity.supervisor` should become a system LaunchDaemon so the fleet spine can start before GUI login.

## Verdict

**GO for a staged implementation, not an immediate live flip.** A system LaunchDaemon is worth building as belt-and-suspenders for headless reboot recovery because the current macOS service lives in the user GUI domain (`~/Library/LaunchAgents/com.gascity.supervisor.plist`) and therefore cannot load until a login creates `gui/<uid>`.

Do **not** copy the current LaunchAgent plist into `/Library/LaunchDaemons` as-is. The current deployment pattern can carry provider credential names in the service `EnvironmentVariables`, and a system plist is a higher-risk place to store long-lived secrets. A LaunchDaemon also does not replace auto-login for GUI, browser, or user-keychain-dependent workflows.

### Cheap ceiling test

Given a perfect implementation, this helps enough to bother: it removes one single-point boot dependency between power restoration and the Gas City control plane. It does not make a headless reboot fully self-healing by itself; it only gives the supervisor and non-GUI control plane a chance to come back before login.

### Kill gates

Kill or hold the cutover if any of these are true:

1. The system plist would include provider keys, OAuth tokens, GitHub tokens, cloud secrets, or other long-lived secrets in `EnvironmentVariables`.
2. The user LaunchAgent and system LaunchDaemon would both be active for the same `GC_HOME`, API port, or supervisor socket.
3. `gc supervisor status`, restart, uninstall, and rollback still assume only the `gui/<uid>/com.gascity.supervisor` launchd target.
4. The reboot test only verifies `launchctl` liveness and does not verify `gc status` / `gc hook` behavior after a no-login boot.
5. The expected recovery depends on the user keychain, browser profiles, or a GUI session. Those remain auto-login territory, not LaunchDaemon territory.

## Current-state assessment

- Existing Gas City launchd lifecycle code installs a user LaunchAgent under `~/Library/LaunchAgents` and manages the `gui/<uid>/<label>` target.
- A redacted audit of the live user LaunchAgent on 2026-07-22 reported two sensitive provider-key names in `EnvironmentVariables`; no secret values were printed, hashed, or persisted.
- The incident class is real: if the host reboots and no one logs in, launchd does not create the GUI domain, so a GUI LaunchAgent does not start.
- Auto-login is still the primary recovery mechanism for the full interactive fleet because it restores the user login session, browser access, and keychain-backed workflows.
- LaunchDaemon conversion is defense-in-depth for the supervisor/control-plane subset only.

## Safe LaunchDaemon contract

The daemon should run as the normal operator user, not as root. The file must be owned by root and must not be group/world-writable. Keep the environment minimal and free of provider credentials.

<!-- launchdaemon-plist-start -->
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.gascity.supervisor</string>
  <key>UserName</key>
  <string>qeetbastudio</string>
  <key>GroupName</key>
  <string>staff</string>
  <key>ProgramArguments</key>
  <array>
    <string>/opt/homebrew/bin/gc</string>
    <string>supervisor</string>
    <string>run</string>
  </array>
  <key>WorkingDirectory</key>
  <string>/Users/qeetbastudio</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>Crashed</key>
    <true/>
    <key>SuccessfulExit</key>
    <false/>
  </dict>
  <key>EnvironmentVariables</key>
  <dict>
    <key>GC_HOME</key>
    <string>/Users/qeetbastudio/.gc</string>
    <key>HOME</key>
    <string>/Users/qeetbastudio</string>
    <key>PATH</key>
    <string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
  </dict>
  <key>StandardOutPath</key>
  <string>/Users/qeetbastudio/.gc/logs/supervisor-launchdaemon.out.log</string>
  <key>StandardErrorPath</key>
  <string>/Users/qeetbastudio/.gc/logs/supervisor-launchdaemon.err.log</string>
</dict>
</plist>
```
<!-- launchdaemon-plist-end -->

This sample is a contract sketch for implementation and rehearsal. It is not a command to install it live.

## Migration rehearsal

Run these checks from a human/operator shell before touching launchd state:

1. Save a redacted inventory of the current user LaunchAgent with `internal/bootstrap/packs/core/assets/scripts/supervisor-env-surface-audit.sh --plist ~/Library/LaunchAgents/com.gascity.supervisor.plist`. Do not dump full plist contents into transcripts.
2. Fail closed if the inventory reports any provider-key or token-shaped names in the service environment.
3. Confirm the current supervisor API port, socket path, and `GC_HOME`; choose one target owner for each. No active duplicate service may own the same tuple.
4. Backup the current LaunchAgent path and current `gc supervisor status` output.
5. Create `~/.gc/logs` and confirm the operator user can write there.
6. Install the LaunchDaemon plist with root ownership and not group/world-writable permissions.
7. Stop and disable the user LaunchAgent before bootstrapping the system daemon.
8. Bootstrap and start the daemon with the system launchd domain.
9. Verify behavior, not just process existence:
   - `gc status` returns usable supervisor/city state.
   - `gc hook` returns without the login-gated failure mode.
   - `launchctl list` shows exactly one `com.gascity.supervisor` instance.
   - No provider-key names are present in the system plist.
10. Perform one planned reboot rehearsal. The meaningful pass condition is: before any GUI login, the supervisor is running and the non-GUI control plane responds. GUI/browser/keychain tasks may still wait for auto-login.

## Rollback

1. Boot out the system daemon.
2. Remove or disable `/Library/LaunchDaemons/com.gascity.supervisor.plist`.
3. Re-enable and load the user LaunchAgent.
4. Verify exactly one supervisor instance and the same `GC_HOME`/port/socket tuple.
5. Record the reason for rollback in the ops bead and this runbook if it changes the decision.

## Source work needed before live cutover

- Add first-class `gc supervisor` lifecycle support for the system launchd domain instead of relying on manual plist surgery.
- Add a preflight that refuses LaunchDaemon install when secret-shaped environment variable names are present.
- Add duplicate-owner detection across user LaunchAgent and system LaunchDaemon modes.
- Teach status/restart/uninstall to report and operate on both `gui/<uid>/<label>` and `system/<label>` targets.
- Keep credential isolation work separate: LaunchDaemon support must not become a way to preserve long-lived provider keys in a more privileged plist.

## Bottom line

Build the LaunchDaemon path, but gate the live flip behind credential removal, duplicate-service prevention, lifecycle support, and a no-login reboot rehearsal. Until those gates pass, auto-login remains the only complete recovery path for the full fleet.
