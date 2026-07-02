# Step 1 — exhaustive Info-mirror key inventory (regenerated at HEAD 42e61a57e)

Regenerated per spec §5.1 ("do NOT trust the handoff's list; grep every
`Metadata[...]` read reachable from the reconciler decision paths"). Method:
`grep -nE '\.Metadata\[' ` across the five decision-path files
(`session_reconciler.go`, `session_reconcile.go`, `session_wake.go`,
`compute_awake_bridge.go`, `internal/session/lifecycle_projection.go`), then
classified each key read on the **session bead** as decision-read / write-only /
trace / already-mirrored, resolving all symbolic-constant keys and confirming
receiver types.

## Divergence from the handoff's list of 6

- Handoff listed 6 "known-missing": `held_until`, `wait_hold`, `restart_requested`,
  `churn_count`, `wake_mode`, `session_name_explicit`.
- **`session_name_explicit` is a PHANTOM** — `grep -rn session_name_explicit`
  over the whole repo returns nothing. Do NOT add it.
- **`restart_requested` is special (§5.2)** — intra-tick in-memory marker, NOT a
  codec mirror. Deferred to Step 3 (add the intra-tick `Info` field when its
  read/write sites are wired, so the codec never reads it from the bead).
- The real decision-read set is **12 core keys + 5 drift/stranded-bookkeeping
  keys = 17**, not 6.

## A. Raw-string Info mirrors to ADD in Step 1 (session-bead decision reads, absent from Info)

Core lifecycle decision keys (12):

| key | read sites | drives |
| --- | --- | --- |
| `held_until` | reconcile:95,401,613 | wake suppression / heal-expired-timers |
| `wait_hold` | awake_bridge:128 (LifecycleInput.WaitHold), reconcile:109 | lifecycle blocker |
| `churn_count` | reconcile:913,948 (read via Atoi + `==""`/`=="0"`) | death-spiral quarantine |
| `wake_mode` | wake:59,622; reconciler:383,385,2833 (`=="fresh"`) | fresh-wake / drain finalize |
| `sleep_intent` | reconciler:2709,2847,4015 (`!=""`, `=="idle-stop-pending"`) | sleep-intent branch |
| `instance_token` | wake:646,663 | wake token match |
| `detached_at` | awake_bridge:137 (LifecycleInput),195 | detach gate (RFC3339) |
| `currently_processing_bead_id` (`session.CurrentBeadIDKey`) | awake_bridge:132 | LifecycleInput.CurrentlyProcessingBeadID |
| `core_hash_breakdown` | reconciler:2235,2236,2489 | config-drift trace payload (raw-string mirror is byte-identical) |
| `started_provision_hash` | reconciler:2247 | launch-only-drift decision |
| `started_launch_hash` | reconciler:2248 | launch-only-drift decision |
| `started_live_hash` | reconciler:2395 | config-drift decision |

Config-drift-deferral + stranded bookkeeping (5 — read+write in deep helpers,
writes already route through `sessFront`; the READS still crack raw):

| key (symbolic const) | literal | read sites |
| --- | --- | --- |
| `namedSessionConfigDriftDeferredAtMetadata` | `config_drift_deferred_at` | reconciler:3635,3669 |
| `namedSessionConfigDriftDeferredKeyMetadata` | `config_drift_deferred_key` | reconciler:3629,3670 |
| `sessionAttachedConfigDriftDeferredAtMetadata` | `attached_config_drift_deferred_at` | reconciler:3671,3699,3717 |
| `sessionAttachedConfigDriftDeferredKeyMetadata` | `attached_config_drift_deferred_key` | reconciler:3671,3698,3714 |
| `strandedEventEmittedKey` | `stranded_event_emitted_at` | reconciler:3271 |

## B. Special-cased (NOT a Step-1 codec mirror)

- `restart_requested` — §5.2 intra-tick field; add in Step 3.

## C. OUT OF SCOPE (not a session bead — do NOT add to session.Info)

- `detachedProbeMetadataKey` (`beadmeta.DetachedMetadataKey`) — read at
  reconciler:3313 on `strandedAssignedWork.bead`, an **assigned-work** bead, not
  a session bead.

## D. PHANTOM (do NOT add)

- `session_name_explicit` — nonexistent anywhere in the repo.

## E. Already mirrored (verified present in Info — no work)

`session_name`/`SessionNameMetadata`, `state`/`MetadataState`, `template`,
`pending_create_claim`, `last_woke_at`, `started_config_hash`, `sleep_reason`,
`pending_create_started_at`, `wake_attempts`, `quarantined_until`, `generation`,
`session_key`, `work_dir`, `dependency_only`, `continuation_reset_pending`,
`reset_committed_at`, `state_reason`, `creation_complete_at`, `continuation_epoch`,
provider-terminal/health/drainable cluster, transport, pin_awake, MCP cluster,
trigger/brain cluster, alias/alias_history, pool_slot/pool_managed/common_name/
configured_named_*.

## F. Circuit-breaker cluster → Step 5 (dedicated `CircuitState` accessor, NOT Info)

`session_circuit_*` (progress_signature/restarts/last_restart/last_progress/
last_observed/opened_at/open_restart_count/state/reset_generation). Per spec §5.3.

## G. Write-only / loop-copy (no mirror)

- `for k,v := range batch { session.Metadata[k]=v }` lockstep sites (the raw
  write path retired in Step 6): wake:75,631; reconcile:617,627,748,784,835,842,
  870,925,933,1066; reconciler:77,348,404,2091,2570,2644,3786,3845.
