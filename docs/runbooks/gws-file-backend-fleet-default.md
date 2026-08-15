# gws file-backend fleet-default hardening

`gws` uses the macOS keychain backend unless `GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND=file` is present at invocation/provision time. In headless or locked-keychain contexts that default has deleted usable `credentials.enc` state. The safe fleet default is therefore: every reviewed `gws` call site either sets the env var itself or invokes an explicit wrapper that does.

This runbook is source support only. It does **not** repair a missing account token, perform OAuth, edit live LaunchAgents, or prove account ownership. Re-provisioning an account such as `calvin@` still needs the operator/GUI step with the same env var set.

## Assets

- `assets/scripts/gws-file-backend-exec.sh` — explicit wrapper that forces `GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND=file` and then execs the real `gws` binary while preserving argv/stdin/stdout/stderr behavior.
- `assets/scripts/gws-file-backend-audit.sh` — value-blind static audit for known local invocation loci: `tools/*.sh`, `tools/*.py`, `orders/*.plist`, `agents/*/agent.toml`, and installed LaunchAgents. It reports apparent `gws` callers that lack either the env var or the wrapper in the same file.

## Review / staging flow

```bash
# Source-tree review from a gascity checkout.
internal/bootstrap/packs/core/assets/scripts/gws-file-backend-audit.sh \
  --root ~/gt \
  --launchagents ~/Library/LaunchAgents
```

Expected result during staging may be non-zero: each `FAIL ... missing file backend guard` line is an invocation site to inspect and patch at its proven injection point. Prefer the smallest reliable local fix:

1. LaunchAgent plist: add `EnvironmentVariables` with `GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND=file`, or make its program call `gws-file-backend-exec.sh` if it directly invokes `gws`.
2. Script/tool: export `GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND=file` before any `gws` subprocess, or call `gws-file-backend-exec.sh`.
3. Agent config: add `[env] GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND = "file"` only for agents that are approved to invoke `gws`.

After patching, rerun the audit and then exercise the changed call site against a non-destructive command such as `gws auth status` for the intended config. The audit reads source/config text only; it does not read `~/.config/gws*` credential material.

## Boundaries

- File-backend env is necessary at provisioning time; it does not recreate an already-missing `credentials.enc`.
- This is not a credential isolation claim. Same-UID and account-scoping risks remain governed by the worker credential isolation runbooks.
- Do not install a transparent PATH shim as an unreviewed global mutation. This wrapper is an explicit call-site primitive so each caller remains reviewable and reversible.
