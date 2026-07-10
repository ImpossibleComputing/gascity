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
Privileged identities are intentionally narrow (`GC_AGENT_ROLE=mayor`, `ops`,
`credential-admin`, `payment-admin`, or `GC_AGENT=mayor|paul`). Any broader
allowlist must be reviewed before live activation.

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
6. Worker/pool identity cannot launch mayor/payment browser profiles through the
   guarded browser shim.
7. Mayor/ops identity can pass through to a fake tool in tests.

## Remaining hard-wall requirements

Phase 2 must remove the underlying credentials/profiles from worker-readable
space. Acceptance for the actual hard wall requires direct absolute-path bypass
attempts and forged `GC_AGENT=mayor` attempts to fail because the worker process
has no OS/vault authority and no usable credential material.
