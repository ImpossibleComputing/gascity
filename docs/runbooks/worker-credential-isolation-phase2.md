# Phase-2 worker credential isolation

Goal: non-privileged fleet workers must not be able to read, create, or tamper with mayor/founder credential files even when a prompt is compromised, while preserving a sanctioned code-publication path.

## Immediate host-enforced path

For local tmux-backed workers on macOS, set `sandbox_profile` on worker agents to the core profile `assets/security/worker-credential-deny.sb`. Gas City wraps the launched agent command as:

```sh
sandbox-exec -D ... -f <profile> -- sh -lc 'exec <agent command>'
```

The sandbox is inherited by child processes, so direct file reads, `gws`, Python, shells, and absolute-path helpers cannot read the denied paths from inside the worker process tree.

The profile denies all file operations for credential paths:

- `~/.config/gws`
- `~/.config/gcloud`
- `~/.aws`
- `<city>/.secrets`
- `~/.secrets`
- `~/.ssh`

The profile also read-denies browser profile roots under `~/Library/Application Support`, but does not blanket write-deny that broad cache root. It write-denies the city security-profile directory after launch so workers cannot weaken future sandbox restarts from inside their own process tree.

It intentionally allows ordinary city/repo reads so coding agents keep working. Code publication should use HTTPS plus the existing GitHub CLI token/credential helper; SSH private keys stay fully denied. Do not deny `~/.config/gh`, `~/.gitconfig`, or `~/.config/git`: those are the sanctioned HTTPS publication mechanism.

## Activation checklist

1. Keep suspect/pool workers suspended until this preflight passes.
2. Install/copy the profile to a stable city path, e.g. `.gc/security/worker-credential-deny.sb`.
3. Set `sandbox_profile = "//.gc/security/worker-credential-deny.sb"` on every non-mayor/non-Paul worker template, or via pack/agent overrides.
4. Run the preflight:

```sh
internal/bootstrap/packs/core/assets/scripts/worker-credential-sandbox-preflight.sh \
  --profile .gc/security/worker-credential-deny.sb \
  --city /Users/qeetbastudio/gt \
  --home /Users/qeetbastudio \
  --https-push-remote git@github.com:ImpossibleComputing/gascity.git \
  --https-push-ref refs/heads/<disposable-preflight-ref>
```

5. Restart/drain worker sessions so every live worker is launched under the sandbox.
6. Re-probe from inside a sandboxed worker:
   - ordinary repo read, `gc mail inbox`, and `git status` in a rig checkout must pass;
   - credential path reads, writes, and creates must fail for `gws`, gcloud, AWS, `.secrets`, and `~/.ssh`;
   - `.gc/security` writes must fail from inside the worker;
   - Git publication must work over HTTPS using the GitHub CLI token/credential helper, not SSH (the preflight dry-runs a push to the supplied disposable ref; Paul configures `url.https://github.com/.insteadOf git@github.com:` before the reflip).
   Do not send external mail as a test.
7. Only after the sandbox, HTTPS publication, and prompt/credential guards hold: rotate mayor@ password/token and enable 2FA.

## Environment-variable credential surface

The file-read sandbox does **not** protect credentials that are already present in
the worker process environment. A worker launched by the supervisor inherits the
supervisor environment unless the launcher scrubs or replaces it before spawning
the worker. That means a launchd plist such as
`~/Library/LaunchAgents/com.gascity.supervisor.plist` can be a plaintext
credential surface if it stores API keys under `EnvironmentVariables`, and those
variables can bypass file-deny sandboxing entirely.

Use the redacted audit helper instead of dumping a full plist:

```sh
internal/bootstrap/packs/core/assets/scripts/supervisor-env-surface-audit.sh \
  --plist ~/Library/LaunchAgents/com.gascity.supervisor.plist
```

The helper prints variable names and plist metadata only; it does not print,
hash, prefix, or persist values. Treat any secret-bearing name such as
`OPENAI_API_KEY`, `GEMINI_API_KEY`, `*_TOKEN`, or `*_SECRET` in supervisor
`EnvironmentVariables` as part of the assume-breach inventory if the plist or a
full plist dump entered a transcript.

This finding does not by itself block the PR#11/PR#12 pool-resume gate: the
incident hole was worker access to mayor@ file credentials plus authority
impersonation. Environment variables instead expose the LLM/API credentials
placed there, which is still a real abuse/cost surface and must be handled as a
follow-on.

## Phase-3 broker / per-worker LLM credential scope

Target end-state: workers do not inherit shared supervisor API keys. The launcher
should start workers with a scrubbed environment and provide only the credentials
that worker is authorized to use, ideally through a broker that can mint or
select per-worker scoped credentials, log issuance, revoke a single worker, and
rate-limit by agent/session.

Acceptance probes for that follow-on:

1. A generic non-LLM worker launched by the supervisor has no `OPENAI_API_KEY`,
   `GEMINI_API_KEY`, or other secret-bearing environment names.
2. A Codex/LLM worker that needs model access receives only its scoped worker key
   or broker token, not the shared supervisor key.
3. Revoking one worker credential does not require rotating every fleet key.
4. A full launchd plist audit contains no plaintext long-lived API keys.
5. File sandbox probes from this runbook still pass after env scrubbing/broker
   integration.

## Limits

This is stronger than a PATH wrapper: absolute path bypasses and spawned children inherit the sandbox. It is still not a substitute for a future separate Unix-user/container split where feasible, and it does not authorize workers to receive mayor@ credentials, shared API keys, or other secrets through environment variables or broker APIs.
