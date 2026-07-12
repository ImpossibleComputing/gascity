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


## Pack-defined and maintenance-agent residual coverage

The named-agent rollout is not the whole fleet. Pack-defined agents must carry
the same profile when they are prompt/code workers or when the profile does not
conflict with their infra role.

Apply the profile to:

- `jacq-worktree-pool` agents (`claude`, `claude-sonnet`, `codex`): these are
  secondary coding/pool workers and share the same risk class as the primary
  `claude-sonnet` pool.
- `bd.dog`: this is a maintenance worker, but its Dolt/bead work is under the
  city store; the credential-deny profile blocks only credential roots,
  browser-profile reads, and `.gc/security` writes. It should also explicitly
  unset inherited LLM/GitHub secret env vars because it is not an LLM/code
  publication worker.
- `core.control-dispatcher` and rig-scoped `*/control-dispatcher`: these are
  deterministic system agents with `prompt_mode = "none"`; the same profile
  still allows `.gc/runtime` trace writes and `gc convoy control --serve`, while
  blocking credential roots. They should explicitly unset inherited LLM/GitHub
  secret env vars for the same reason.

Only exempt a system agent with a concrete functional reason and document that
reason in the runbook/bead. Do not leave a pack-defined prompt worker silently
unsandboxed.

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

For non-LLM maintenance/system agents, use the launch scrub switch plus the
existing empty-env convention to remove inherited secrets at pane launch. Keep
this list aligned with `worker-secret-env-preflight.sh`; excerpt:

```toml
[env]
GC_WORKER_SECRET_ENV_SCRUB_DEFAULTS = "1"
OPENAI_API_KEY = ""
GEMINI_API_KEY = ""
ANTHROPIC_API_KEY = ""
MISTRAL_API_KEY = ""
ZAI_API_KEY = ""
GH_TOKEN = ""
GITHUB_TOKEN = ""
```

The tmux launch path converts empty configured values into `env -u <KEY>` for
the agent command. `GC_WORKER_SECRET_ENV_SCRUB_DEFAULTS=1` also asks the runtime
to unset the default shared-secret names before launch, then drop that control
variable itself; this gives the non-LLM maintenance agents (`bd.dog` and
`core.control-dispatcher`) a drift-resistant first rollout target. This is not
enough for Codex/LLM workers that actually need model access; those need the
Phase-3 broker/per-worker credential path below before any broad/default flip.

To verify a worker's current environment without exposing values, run:

```sh
internal/bootstrap/packs/core/assets/scripts/worker-secret-env-preflight.sh
```

The preflight prints forbidden env names only, with `value=REDACTED`, and fails
if default supervisor-level LLM, cloud, or GitHub credential names are present
(`OPENAI_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `CLAUDE_CODE_OAUTH_TOKEN`,
`GEMINI_API_KEY`, `GOOGLE_API_KEY`, `AWS_SECRET_ACCESS_KEY`, `GH_TOKEN`,
`GITHUB_TOKEN`, and peer provider keys). For a future brokered worker, allow
only the scoped broker token name explicitly, e.g. `--forbid
WORKER_LLM_BROKER_TOKEN --allow WORKER_LLM_BROKER_TOKEN`; do not allow the
shared supervisor keys.


### Process-listing transcript leak guard

A second environment-variable leak vector is broad process inspection. Commands
such as `ps aux`, `ps -ef`, or env/full-command variants can copy inherited
API-key environment variables or secret-bearing command lines into durable agent
transcripts. Workers should not run broad or full-command process
listings through the guarded PATH wrapper. Prefer process-specific narrow forms such as:

```sh
ps -p <pid> -o pid,comm=
```

The core pack includes a PATH-level tripwire for this class:
`assets/worker-sensitive-tools/bin/ps` routes through
`assets/scripts/worker-process-listing-guard.sh`, denying broad/full-command ps
forms with an allowlist that passes only no-arg `ps` and narrow `-p ... -o
pid,comm=` diagnostics. The guard does not trust worker-settable identity
environment variables (`GC_AGENT`, `GC_AGENT_ROLE`, or override flags) for
authorization; privileged operators who need broader inspection must use an
out-of-band ops path. Full-format, wide-output, undashed broad BSD forms, and
BSD env-output variants (`-f`, `-ww`, `axo ...`, `-E`) are denied even when
paired with a specific pid because those can still surface secret-bearing
command lines or inherited environments. This guard is intentionally weaker than the macOS file
sandbox: absolute `/bin/ps` can bypass a PATH wrapper, so Phase-3 should still
scrub inherited environments and remove long-lived secrets from process envs.

### Compute SSH without reopening `~/.ssh`

The worker file sandbox intentionally denies `~/.ssh`: re-allowing that path
would reopen the credential-theft class the sandbox closed. Sandboxed worker
access to compute hosts (`ic-workstation`, raw-SSH RunPod-style targets, etc.)
therefore needs an explicit compute credential path, not ambient user SSH state.

The durable Phase-3 target is a compute broker using the standard SSH-CA
pattern: a worker generates an ephemeral key under its sandbox runtime; the
broker signs the public key for a specific host, principal, scope, and TTL; the
worker invokes ssh with explicit `IdentityFile`, `CertificateFile`, pinned
`UserKnownHostsFile`, and `IdentitiesOnly=yes`. Static deploy keys in a
sandbox-allowed path are an interim throughput fallback only: they can be
scoped and rotated, but a compromised worker can still read them while they are
allowed.

The core pack includes `assets/scripts/worker-compute-ssh.sh` as the source
contract for both brokered certs and any time-boxed static-key fallback. It
refuses relative paths and identity, certificate, or known-hosts paths under `~/.ssh`, disables
ambient ssh config with `-F /dev/null`, sets only the explicit `IdentityFile`,
forces `IdentitiesOnly=yes` and `ForwardAgent=no`, pins the known-hosts file,
and rejects caller-supplied ssh options that could override identity,
certificate, host-key, proxy, agent-forwarding, or local-command behavior. The helper is
not a broker by itself; it is the launcher boundary the broker or stopgap should
use.

## Phase-3 broker / per-worker LLM credential scope

The launch/config/preflight/materialization pieces below are plumbing. The
credential authority that decides issuance, revocation, audit, and cutover is
drafted separately in [Phase-3 worker credential authority architecture](worker-credential-authority-phase3.md).
Do not treat the plumbing as final isolation until that authority is approved and
live workers use scoped/revocable credentials.

### Launch-time shared secret scrub switch

The tmux runtime supports the launch-env control
`GC_WORKER_SECRET_ENV_SCRUB_DEFAULTS=1`. When present in an agent's configured
launch environment, the runtime prefixes the pane command with `env -u` for the
default shared supervisor credential names used by
`worker-secret-env-preflight.sh`, then unsets the control variable itself.
Explicit non-empty values in the configured agent env win, so a future broker can
still inject a scoped per-worker token intentionally while blocking accidental
inheritance from the tmux/supervisor environment.

This switch is a launch scrub, not a credential broker. It should be enabled for
workers once their needed model/GitHub/compute credentials come from explicit
scoped broker material rather than inherited supervisor env vars.

### Scoped credential env-file launch contract

The tmux runtime can also consume a broker-issued credential env file before
starting the worker:

```toml
scoped_credential_env_file = ".gc/worker-credentials/{{.Agent}}.env"
```

The file uses the same simple `KEY=VALUE` dotenv subset as other Gas City env
files, but it is intentionally narrow because this is a credential channel, not
a general launch-env override:

- `scoped_credential_env_file` may be absolute or relative to the city root and
  accepts the same template placeholders as `work_dir`; the resolved path is
  projected to `GC_WORKER_SCOPED_CREDENTIAL_ENV_FILE` at launch;
- if you set the legacy `env.GC_WORKER_SCOPED_CREDENTIAL_ENV_FILE` escape hatch
  directly, it must be absolute;
- do not set both `scoped_credential_env_file` and
  `env.GC_WORKER_SCOPED_CREDENTIAL_ENV_FILE` for the same agent;
- on Unix, the file must be mode `0600` or stricter;
- keys must be credential keys only (for example `OPENAI_API_KEY`,
  `ANTHROPIC_AUTH_TOKEN`, `GITHUB_TOKEN`, or `GC_GIT_CREDENTIAL_COMMAND`);
- values must be non-empty;
- a key already configured with a non-empty value in the agent env is a hard
  conflict, not silently overwritten;
- `GC_WORKER_SCOPED_CREDENTIAL_ENV_FILE` is scrubbed from the launched worker;
- loading this file automatically enables the default shared-secret scrub, so
  absent supervisor credential names are still prefixed with `env -u`.

This gives the future broker a concrete launch boundary: mint/write a per-worker
0600 env file under a private city/runtime location, point the agent's
`scoped_credential_env_file` at it, and let the runtime inject only those scoped
credentials while unsetting shared supervisor keys. For GitHub, prefer a scoped
`GITHUB_TOKEN` or a `GC_GIT_CREDENTIAL_COMMAND` helper from the broker; do not
rely on the supervisor's broad ambient `GH_TOKEN`.

Broker/controller code can use the hidden internal writer to materialize the
file from already-resolved process environment variables without ever passing
credential values on argv or printing them:

```bash
gc internal scoped-credential-env-file \
  --out "$GC_CITY_RUNTIME_DIR/worker-creds/codex.env" \
  --from-env OPENAI_API_KEY=SCOPED_CODEX_OPENAI_API_KEY \
  --from-env GITHUB_TOKEN=SCOPED_CODEX_GITHUB_TOKEN
```

For the no-provider-creds supervisor cutover, prefer keeping broker-issued or
operator-provisioned scoped source values in a private dotenv file rather than
persisting them in LaunchAgents or process env. The same writer can read those
source keys without printing values:

```bash
gc internal scoped-credential-env-file \
  --out "$GC_CITY_RUNTIME_DIR/worker-creds/codex.env" \
  --source-env-file "$GC_HOME/worker-credential-sources.env" \
  --from-env-file OPENAI_API_KEY=CODEX_OPENAI_API_KEY \
  --from-env-file GITHUB_TOKEN=CODEX_GITHUB_TOKEN \
  --audit-log "$GC_HOME/scoped-credential-materialization.jsonl"
```

`--source-env-file` must be an absolute private dotenv file (mode `0600` or
stricter on Unix). Parse errors are reported as invalid syntax only, so a
malformed secret-like source line is not echoed to stderr. `--audit-log` is
optional and appends value-blind JSONL events (destination path, output key
names, and source key names only) to an absolute private file; it never records
credential values and rejects symlinked audit logs on Unix.

The writer creates parent directories as private directories, writes a sorted
dotenv file atomically at mode `0600`, applies the same credential-key allowlist
as launch-time loading, and reports only key names/paths on errors. It is a
materialization primitive, not a token issuer: the credential source still needs
to be scoped, revocable, and audited by the broker.

`gc doctor` also runs the advisory `scoped-worker-credential-files` check for
any configured `scoped_credential_env_file` or legacy
`GC_WORKER_SCOPED_CREDENTIAL_ENV_FILE`. It validates the same contract before
launch — absolute/resolved path, private mode, credential-key allowlist,
non-empty values, sanitized parse errors, and no explicit non-empty credential
env values on the same agent that would either conflict at launch or survive
default secret scrubbing — without printing credential values.

### Supervisor launchd plaintext credential audit

`gc doctor` includes the advisory `supervisor-provider-creds` check. It inspects
only the installed supervisor launchd plist's `EnvironmentVariables` keys and
reports provider credential/config key names when they are persisted there; it
never prints values, hashes, prefixes, or lengths. A warning means long-lived
provider credentials are still in the service file and should be moved out of
LaunchAgents as part of the Phase-3 cutover.

The safe end-state is not just moving the same shared key to another ambient
environment source. Once model/GitHub workers have scoped broker-issued
credentials, reinstall the supervisor with `GC_SUPERVISOR_OMIT_PROVIDER_CREDS=1`
so provider keys are omitted from the supervisor service env, and keep any
machine-local broker inputs in a private, auditable 0600 location rather than in
the launchd plist.

Target end-state: workers do not inherit shared supervisor API keys. The launcher
should start workers with a scrubbed environment and provide only the credentials
that worker is authorized to use, ideally through a broker that can mint or
select per-worker scoped credentials, log issuance, revoke a single worker, and
rate-limit by agent/session.

Acceptance probes for that follow-on:

1. A generic non-LLM worker launched by the supervisor has no `OPENAI_API_KEY`,
   `GEMINI_API_KEY`, or other shared credential environment names. The redacted
   `worker-secret-env-preflight.sh` should pass inside that worker.
2. A Codex/LLM worker that needs model access receives only its scoped worker key
   or broker token, not the shared supervisor key. The same preflight may allow
   only that scoped token name and must still fail on shared supervisor keys.
3. Revoking one worker credential does not require rotating every fleet key.
4. A full launchd plist audit contains no plaintext long-lived API keys.
5. File sandbox probes from this runbook still pass after env scrubbing/broker
   integration.

## Limits

This is stronger than a PATH wrapper: absolute path bypasses and spawned children inherit the sandbox. It is still not a substitute for a future separate Unix-user/container split where feasible, and it does not authorize workers to receive mayor@ credentials, shared API keys, or other secrets through environment variables or broker APIs.
