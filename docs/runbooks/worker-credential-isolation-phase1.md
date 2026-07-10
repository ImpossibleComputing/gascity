# Worker credential isolation — Phase 1 review slice

Status: **authored for review only**. Do not flip live PATH routing, move live
secrets, or change tool behavior until Paul completes ops review, the mayor
reviews the PR, and a rollback plan is written.

## What this phase does and does not prove

Phase 1 is a blast-radius reduction: guarded wrappers fail closed for standard
GC worker/pool sessions before they reach founder-comms, payment, or sanctioned
secret-helper paths. It is not the final hard wall, because a process running as
the same Unix user can still bypass shell wrappers or forge environment
variables until Phase 2 moves secrets/profiles behind an OS/vault boundary.

## Phase 0 inventory artifact (names/classes only)

No secret values belong in this file, tests, logs, PR body, or transcripts.
Sequence highest-value first:

| Priority | Surface class | Examples by name/class only | Phase-1 policy | Phase-2 target |
| --- | --- | --- | --- | --- |
| 1 | Gmail / founder comms | mayor Gmail OAuth material, `gws` auth/keyring, Gmail watcher state | deny all non-privileged GC sessions through guarded `gws` wrapper | move credentials to mayor/ops identity or vault; worker direct `gws` has no usable credential |
| 2 | Payment / card-bearing sessions | payment-console browser profile, vendor billing/admin profile, card-bearing z.ai console session | deny non-privileged browser/payment wrapper use | separate payment-admin account/profile outside worker-readable home/session |
| 3 | z.ai credentials | z.ai API keys and payment-linked console auth classes | deny sanctioned secret-helper/browser access | scoped service account, no auto-recharge, spend cap, vault-backed grants |
| 4 | Mistral credentials | Mistral API keys / hosted model billing classes | deny sanctioned secret-helper access | scoped service account, spend cap, vault-backed grants |
| 5 | Mayor/ops watcher tokens | Slack/docs/Gmail watcher token classes and launchd consumers | deny workers; hold live moves until Paul maps dependencies | ops-owned identity/broker with audit log |

## Authored wrapper assets

The PR adds these core-pack assets:

- `assets/scripts/worker-sensitive-tool-guard.sh` — generic fail-closed guard.
- `assets/worker-sensitive-tools/bin/gws` — guarded `gws` shim.
- `assets/worker-sensitive-tools/bin/secret-peek` — guarded sanctioned secret-helper shim.
- `assets/worker-sensitive-tools/bin/mayor-browser` — guarded browser/payment-profile shim.
- `assets/scripts/worker-sensitive-tool-path.sh` — PATH prepend helper.

The guard treats non-privileged GC sessions as denied for sensitive surfaces.
For Phase 1, the `gws`, `secret`, and `browser` wrapper kinds are all
fail-closed for non-privileged identities. The `secret` and `browser` wrappers do
not rely on keyword matching to decide whether a class/profile is sensitive; an
unkeyworded sanctioned secret class or shared browser profile is still denied
until Phase 2 moves underlying credential authority behind an OS/vault/account
boundary. Privileged identities are intentionally narrow (`GC_AGENT_ROLE=mayor`,
`ops`, `credential-admin`, `payment-admin`, or `GC_AGENT=mayor|paul`). Any
broader allowlist must be reviewed before live activation.

## Authored PATH routing (not live)

After review, a GC-managed session launcher can source the helper to put the
wrapper directory before system tools:

```sh
# Example only; do not enable before the review gates.
. "$GC_CORE_ASSETS/assets/scripts/worker-sensitive-tool-path.sh"
```

Rollback is removing that PATH prepend (or un-sourcing the helper) and restoring
the previous session environment. Phase 1 must be reversible because launchd
watchers and founder-comms tooling depend on these surfaces.

## Regression guarantees in this slice

The fake-tool tests prove the wrapper path fails closed without touching live
Gmail, browser profiles, or real secrets:

1. Worker/pool identity cannot run guarded `gws gmail +send`.
2. Worker/pool identity cannot run guarded `gws gmail +reply`.
3. Worker/pool identity cannot run guarded `gws users messages send`.
4. Worker/pool identity cannot run guarded Gmail read/triage paths.
5. Worker/pool identity cannot access founder/payment/z.ai/Mistral secret IDs
   through the sanctioned helper.
6. Worker/pool identity cannot bypass the helper with unkeyworded sanctioned
   secret classes.
7. Worker/pool identity cannot launch mayor/payment browser profiles through the
   guarded browser shim.
8. Worker/pool identity cannot bypass the browser shim with unkeyworded shared
   profile names.
9. Mayor/ops identity can pass through to a fake tool in tests.


## Activation review and rollback plan (not approved yet)

This section is the review artifact for a future live activation. It is not an
authorization to flip PATH routing or move credentials.

### Pre-activation gates

All of these must be true before any GC-managed session PATH changes:

1. Paul completes ops review of launchd/watchers and signs off on which flows
   legitimately need `gws`, sanctioned secret-helper, or mayor/payment browser
   access.
2. Mayor completes activation review and confirms the privileged allowlist is
   least-privilege for the current fleet.
3. The activation operator records the exact core-pack revision, wrapper asset
   paths, prior session PATH shape, and rollback commands in the activation log.
4. A fake-tool dry run is repeated on the target host using worker, mayor, and
   ops identities; the worker identity must fail closed and mayor/ops identities
   must pass through only to fake tools.
5. No live secret values are printed, copied into transcripts, or committed.
6. Phase-2 owner and design are named, because Phase 1 remains forgeable and is
   not the hard wall.

### Least-privilege allowlist to review

Start from deny-all. Do not add convenience access for general workers.

| Identity / role | Intended Phase-1 access | Review requirement |
| --- | --- | --- |
| `GC_AGENT=mayor` / `GC_AGENT_ROLE=mayor` | Founder-comms/admin path, when mayor is the caller | Mayor confirms required |
| `GC_AGENT=paul` / `GC_AGENT_ROLE=ops` | Ops watcher/repair paths only | Paul confirms each watcher/launchd dependency |
| `GC_AGENT_ROLE=credential-admin` | Temporary activation/debug only | Mayor + Paul time-box and log explicitly |
| `GC_AGENT_ROLE=payment-admin` | Payment-console/admin only | Mayor approves; prefer separate account/profile in Phase 2 |
| Worker/pool roles | No `gws`, sanctioned secret-helper, or shared browser wrapper access | Must remain denied |

`GC_CREDENTIAL_GUARD_ALLOW=1` is test/manual breakglass only. Do not use it as a
routine live allowlist mechanism, and remove or harden it in Phase 2.

### Activation sequence

1. Announce the activation window on the team board and bead.
2. Snapshot the current relevant environment for rollback without printing
   secrets:
   - current launcher/core-pack revision
   - previous PATH prefix shape
   - current wrapper asset paths
   - current allowlist roles
3. Source or prepend `assets/scripts/worker-sensitive-tool-path.sh` only for the
   intended GC-managed session launch path. Do not edit unrelated shells,
   launchd jobs, or user-global startup files as part of Phase 1.
4. Start one worker-role canary session and verify guarded `gws`, `secret-peek`,
   and `mayor-browser` calls fail closed against fake tools or harmless dry-run
   commands.
5. Start or check one mayor/ops canary and verify pass-through only for the
   intended privileged flow. Prefer fake tools for this check; do not send live
   mail or read live secrets as a test.
6. Watch launchd/watchers that Paul identified as dependency-sensitive.
7. Record all observations on the bead and mail mayor+Paul.

### Rollback sequence

Rollback is removing the guarded wrapper PATH prepend from the GC-managed session
launch path and restarting only the affected sessions.

1. Stop launching new sessions with the Phase-1 PATH helper.
2. Restore the previous PATH prefix / launcher config captured in the activation
   log.
3. Restart only canary or affected worker sessions. Do not restart mayor unless
   mayor explicitly approves or founder-comms are already broken.
4. Verify `command -v gws`, `command -v secret-peek`, and `command -v
   mayor-browser` resolve as they did before activation for affected sessions.
5. Confirm launchd/watchers are healthy or explicitly restored.
6. Post rollback result to the bead and board; mail mayor+Paul with exact old/new
   resolution paths and affected session IDs.

### Rollback triggers

Rollback immediately if any of these occur:

- Mayor/founder-comms flow breaks or cannot be verified quickly.
- Paul-identified watcher/launchd flow breaks.
- A worker can reach a real founder-comms/payment/secret surface through the
  wrapper path.
- A privileged flow needs a broader allowlist than reviewed.
- The activation operator cannot tell which launcher/PATH state is live.

## Remaining hard-wall requirements

Phase 2 must remove the underlying credentials/profiles from worker-readable
space. Acceptance for the actual hard wall requires direct absolute-path bypass
attempts and forged `GC_AGENT=mayor` attempts to fail because the worker process
has no OS/vault authority and no usable credential material.
