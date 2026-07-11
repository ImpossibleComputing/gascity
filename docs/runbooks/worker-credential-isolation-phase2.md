# Phase-2 worker credential isolation

Goal: non-privileged fleet workers must not be able to read mayor/founder credential files even when a prompt is compromised.

## Immediate host-enforced path

For local tmux-backed workers on macOS, set `sandbox_profile` on worker agents to the core profile `assets/security/worker-credential-deny.sb`. Gas City wraps the launched agent command as:

```sh
sandbox-exec -f <profile> -- sh -lc 'exec <agent command>'
```

The sandbox is inherited by child processes, so direct file reads, `gws`, Python, shells, and absolute-path helpers cannot read the denied paths from inside the worker process tree.

The profile denies reads from:

- `~/.config/gws`
- `~/.config/gcloud`
- `~/.aws`
- `<city>/.secrets`
- `~/.secrets`
- `~/.ssh`
- browser profile roots under `~/Library/Application Support`

It intentionally allows ordinary city/repo reads so coding agents keep working.

## Activation checklist

1. Keep suspect/pool workers suspended until this preflight passes.
2. Install/copy the profile to a stable city path, e.g. `.gc/security/worker-credential-deny.sb`.
3. Set `sandbox_profile = "//.gc/security/worker-credential-deny.sb"` on every non-mayor/non-Paul worker template, or via pack/agent overrides.
4. Run the preflight:

```sh
internal/bootstrap/packs/core/assets/scripts/worker-credential-sandbox-preflight.sh \
  --profile .gc/security/worker-credential-deny.sb \
  --city /Users/qeetbastudio/gt \
  --home /Users/qeetbastudio
```

5. Restart/drain worker sessions so every live worker is launched under the sandbox.
6. Re-probe from inside a sandboxed worker: ordinary repo read must pass; `gws`/`.secrets` reads must fail. Do not send external mail as a test.
7. Only after the sandbox and prompt/credential guards hold: rotate mayor@ password/token and enable 2FA.

## Limits

This is stronger than a PATH wrapper: absolute path bypasses and spawned children inherit the sandbox. It is still not a substitute for a future separate Unix-user/container split where feasible, and it does not authorize workers to receive mayor@ credentials through environment variables or broker APIs.
