# Phase-3 worker credential authority architecture (proposal)

Status: **proposal for mayor + Paul review**. This document intentionally stops
short of live cutover. It defines the scoped/revocable credential authority that
must exist before `GC_SUPERVISOR_OMIT_PROVIDER_CREDS=1` is used for model/Codex
workers.

## Why this exists

PRs #20-#27 landed the launch/config/materialization/preflight/audit stack:

- supervisor provider-key plist audit (`supervisor-provider-creds`);
- `scoped_credential_env_file` agent config and launch-time default secret scrub;
- private scoped env-file writer with source-file input, no symlink race, and
  value-blind materialization audit;
- `scoped-worker-credential-files` doctor checks for file validity and mixed
  explicit-env cutover footguns.

That stack is **not** credential isolation by itself. It safely carries scoped
credentials if they already exist. The missing Phase-3 component is the authority
that issues, records, renews, and revokes those credentials per worker/session.

## Design goals

1. A worker receives only credentials for its agent/session scope.
2. Credentials are short-lived where the upstream supports it; otherwise they are
   selected from narrow, rotatable pools with explicit blast-radius labels.
3. Issuance is value-blind in logs: audit records key names, principal, scope,
   target path, lease id, and expiry; never credential values, hashes, prefixes,
   or lengths.
4. Revoking one worker does not require rotating every fleet credential.
5. The supervisor LaunchAgent/systemd unit can omit long-lived provider keys.
6. Compute access never re-allows `~/.ssh`; it uses short-lived SSH certificates
   and the existing `worker-compute-ssh.sh` launcher contract.
7. The system fails closed: missing/expired/revoked credentials block worker
   launch or renewal rather than falling back to ambient supervisor env.

## Non-goals

- Do not implement a universal secrets manager inside Gas City.
- Do not promise provider APIs can mint true sub-keys where they cannot. For
  those providers, this architecture uses short TTL wrappers or narrow pool
  credentials and names the residual risk explicitly.
- Do not perform live supervisor restart/reinstall. That belongs to Paul/ops in a
  maintenance window after review.
- Do not weaken the worker file sandbox or re-enable `~/.ssh`.

## Core objects

### Worker identity

A credential lease is bound to the resolved worker identity:

- city and rig name;
- qualified agent name;
- provider/session id when available;
- work bead id when assigned;
- target capability set: `llm`, `github`, `compute-ssh`;
- requested TTL and renewal policy.

The identity must be derived by controller/supervisor code, not by
worker-supplied environment variables. Worker-provided identity may be recorded
as a hint, but not trusted for policy.

### Lease record

Each issuance writes a value-blind lease record under a private city/supervisor
state directory, for example:

```json
{
  "lease_id": "lc_...",
  "issued_at": "2026-07-12T12:00:00Z",
  "expires_at": "2026-07-12T14:00:00Z",
  "identity": {
    "city": "gt",
    "agent": "norse/codex",
    "session": "gt-wisp-...",
    "work": "gt-y2gvg"
  },
  "capabilities": ["llm", "github", "compute-ssh"],
  "outputs": {
    "env_file": "/.../.gc/worker-credentials/norse-codex.env",
    "audit_log": "/.../.gc/scoped-credential-materialization.jsonl"
  },
  "keys": ["OPENAI_API_KEY", "GITHUB_TOKEN"],
  "compute_principals": ["ic-workstation:norse-codex"],
  "status": "active"
}
```

Credential values live only in the provider-specific issuer or private source
files long enough to materialize the per-worker env file. Lease records and audit
logs never include values.

### Revocation state

Revocation is a state transition on the lease, plus provider-specific cleanup:

- mark lease `revoked` with time/reason/operator;
- delete or truncate the materialized env file;
- revoke upstream token/cert when the upstream supports direct revocation;
- deny renewal for that identity/lease;
- optionally terminate or recycle the running session that held the credential.

Short TTL is not a substitute for explicit revocation, but it is the backstop for
providers with weak revocation APIs.

## Authority service shape

Implement as a supervisor/controller-owned authority, not as a worker command.
The worker may request work; it must not mint its own credentials.

Minimal interface:

```text
Issue(identity, capabilities, ttl) -> lease_id, materialized_env_file, expiry
Renew(lease_id) -> new_expiry or denied
Revoke(lease_id, reason) -> revoked
Inspect(lease_id) -> value-blind lease status
SweepExpired(now) -> revoked/deleted files
```

The existing hidden writer remains the final materialization primitive, but it is
called only by the authority or an operator-run cutover tool. The authority owns
policy, source selection, audit correlation, and revocation state.

## Capability-specific issuance

### LLM credentials

Preferred order:

1. Provider-native scoped token/key with expiry, per-agent metadata, spending
   limit, and revocation API.
2. Organization/project-scoped key dedicated to a small agent class, wrapped in a
   short Gas City lease and rate limit.
3. Last-resort shared provider key copied into a per-worker file only during a
   time-boxed migration. This is **not** final isolation and must remain labeled
   as a stopgap in the lease/audit record.

Mapping policy:

| Agent class | Allowed LLM keys | Default TTL | Notes |
| --- | --- | --- | --- |
| non-LLM maintenance | none | n/a | should launch with default scrub and no scoped file |
| Codex/LLM worker | provider-specific API key or broker token | 1-4h | no broad supervisor env fallback |
| serving eval worker | model provider + optional base URL | run duration + buffer | record eval/run id in lease metadata |

The scoped env file may expose the standard provider key name the harness expects
(e.g. `OPENAI_API_KEY`) but the value must be worker-scoped or lease-wrapped.

### GitHub credentials

Preferred end-state: repo-scoped, short-lived GitHub credential per worker or
per narrow agent class.

Options in order:

1. GitHub App installation token scoped to required repositories and permissions.
2. Fine-grained PAT with repo subset and no broad admin scopes, rotated and
   brokered with a short lease.
3. `GC_GIT_CREDENTIAL_COMMAND` helper that retrieves/refreshes an installation
   token for the worker identity.

Policy requirements:

- No `admin:public_key` for worker publication.
- No all-repo token for generic workers.
- Git publication uses HTTPS credential helper/token, not user `~/.ssh`.
- Lease record names repositories and permission class, not token value.

### Compute SSH credentials

End-state follows the SSH-CA pattern already endorsed:

1. Worker or authority generates an ephemeral keypair under a sanctioned runtime
   path, not under `~/.ssh`.
2. Authority signs the public key with a compute SSH CA for specific host,
   principal, command/scope if supported, and TTL.
3. Worker uses `worker-compute-ssh.sh` with explicit `IdentityFile`,
   `CertificateFile`, and pinned `UserKnownHostsFile`; `IdentitiesOnly=yes`, no
   ambient ssh config, no agent forwarding.
4. Revocation uses short TTL plus CA revocation list/denylist where supported.

Lease metadata records host, principal, cert serial/key id, TTL, and allowed
purpose. It never stores the private key.

## Materialization flow

1. Controller resolves agent config and sees `scoped_credential_env_file` or a
   policy that requires one.
2. Authority issues or renews capability leases for the resolved worker identity.
3. Authority writes provider-specific source values into a private source dotenv
   or holds them in-process.
4. Authority calls `gc internal scoped-credential-env-file` with:

```bash
gc internal scoped-credential-env-file \
  --out "$GC_CITY_RUNTIME_DIR/worker-creds/$LEASE_ID.env" \
  --source-env-file "$GC_HOME/worker-credential-sources/$LEASE_ID.env" \
  --from-env-file OPENAI_API_KEY=LEASE_OPENAI_API_KEY \
  --from-env-file GITHUB_TOKEN=LEASE_GITHUB_TOKEN \
  --audit-log "$GC_HOME/scoped-credential-materialization.jsonl"
```

5. Authority atomically updates the lease record to active and points the agent's
   resolved config at the materialized env file.
6. Worker launch consumes the env file, automatically enables default secret
   scrub, and removes control variables from the worker environment.
7. Inside-worker `worker-secret-env-preflight.sh` proves shared supervisor keys
   are absent. LLM workers may explicitly allow only the scoped key names that
   they need.

## Renewal and expiry

- A lease cannot renew after its identity changes.
- Renewal must re-run policy and write a fresh materialized file.
- Expired leases are swept: materialized file removed, lease marked expired, and
  long-lived sessions recycled before their credentials become invalid.
- Existing `max_session_age` remains useful for providers whose SDKs cache
  credentials and cannot hot-reload renewed files.

## Cutover sequence

Daytime/Paul-maintenance-window sequence:

1. Review/approve this authority design and provider-specific policy.
2. Provision source authorities:
   - LLM provider scopes or interim narrow key pools;
   - GitHub App/fine-grained token path;
   - compute SSH CA and host principal policy.
3. Configure model/Codex agents with `scoped_credential_env_file` paths.
4. Run `gc doctor` and require:
   - `scoped-worker-credential-files` OK for configured agents;
   - no mixed explicit credential env survivors;
   - materialization audit log is private.
5. Launch a small canary worker with scoped env file.
6. Inside canary, run `worker-secret-env-preflight.sh`; it must fail if shared
   supervisor keys are present and pass with only scoped allowlist.
7. Reinstall supervisor with `GC_SUPERVISOR_OMIT_PROVIDER_CREDS=1`.
8. Restart/reconcile under Paul/ops control.
9. Re-run:
   - `gc doctor supervisor-provider-creds` (no provider keys in LaunchAgent);
   - inside-worker no-shared-env proof;
   - GitHub publish smoke using scoped token/helper;
   - compute SSH smoke using cert lease and `worker-compute-ssh.sh`.
10. Roll out to the rest of model/Codex workers; keep maintenance agents scrubbed
    and credentialless.

Rollback:

- restore prior supervisor service env only if scoped authority failure blocks
  critical operations;
- keep audit log and lease records for postmortem;
- do not remove sandbox/`~/.ssh` denial to recover throughput.

## Acceptance criteria

Phase-3 is not complete until all are true:

1. Live supervisor service env contains no long-lived provider credential keys.
2. Generic workers have no shared credential env names.
3. LLM/Codex workers receive scoped/revocable credentials through scoped env
   files or credential helper commands only.
4. GitHub publication uses repo-scoped or installation credentials without broad
   admin scopes.
5. Compute SSH uses short-lived cert leases and explicit launcher paths, never
   `~/.ssh`.
6. Issuance, renewal, revocation, expiry, and materialization are value-blind
   audited.
7. Revoking one worker lease prevents renewal and removes that worker's
   materialized credential file without rotating every fleet key.

## Open decisions for mayor + Paul

1. Which LLM providers can mint true scoped/expiring sub-keys today, and which
   require narrow pooled keys as an interim?
2. Should GitHub use a GitHub App installation token as the primary path? If yes,
   which app/repository permissions are approved?
3. Who owns the compute SSH CA private key, where is it stored, and what host
   principals/scopes are valid for workers?
4. What is the default lease TTL by capability and agent class?
5. Which command/API surface should operators use for canary issuance and
   revocation (`gc credential lease ...`, supervisor-internal API, or a pack
   command)?
6. What live maintenance window and rollback threshold should Paul use for the
   first `GC_SUPERVISOR_OMIT_PROVIDER_CREDS=1` reinstall?
