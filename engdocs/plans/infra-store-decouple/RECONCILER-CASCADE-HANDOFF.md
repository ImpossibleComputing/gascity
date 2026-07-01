# Handoff — finish the non-work field-door cleanup: reconciler spine → P5 → P6 → ready

**PR #3839** (DRAFT, base `main`), branch `upstream/object-front-doors-cleanup`,
worktree `/data/projects/gascity/.claude/worktrees/object-front-doors`,
**HEAD `1dbf692e7`** (pushed; the recovery-loop slice + a pre-existing unparam
fix landed on top of `7a4014955`). This is the authoritative "finish it out"
guide.
For the completed history read `P4-CASCADE-HANDOFF.md`; the architecture is in
`NONWORK-BEAD-FIELDDOOR-PLAN.md`; the per-site swap rules are in
`P4-CONVERSION-CONTRACT.md`.

> **Provenance.** The reconciler-spine map, the flip cluster, the foundation
> tiers, and the 21-site census below were produced by an adversarially
> cross-checked verification workflow (`wf_7f806124-bcd`, 2026-07-01) that
> re-derived every load-bearing claim directly from the code at this HEAD. One
> of its census agents ran against a **stale out-of-tree checkout**
> (`8cacec199`) and wrongly reported that the `*Info` siblings and `OpenInfos()`
> "do not exist" — that agent's output was discarded. Everything below is the
> critic-verified truth. If you re-run a mapping agent, pin it to
> `git rev-parse HEAD` first.

**Hard rule (owner directive):** a direct read of metadata/bead FIELDS on any
NON-WORK object (session/nudge/mail/order/graph) is illegal — only generic WORK
beads read raw. This is the precondition for a per-class backend swap.

## Confirm a green baseline first

```
go build ./cmd/gc/ ./internal/session/
go test ./cmd/gc/ -run 'TestSessionClassifierInfoEquivalence|TestSessionSnapshotInfoEquivalence|TestNudgeTargetInfoEquivalence|TestSnapshotInfoOnlyFilesStayOnInfoAccessors|TestFrontDoorStoreFreeFilesStayStoreFree|TestSweepUndesiredPoolSessionBeads' -count=1
go test ./internal/session/ -run 'TestNamedSessionInfoEquivalence' -count=1
git checkout go.sum   # churns spuriously on go test
```

## Where the migration stands (verified at HEAD `7a4014955`)

Raw-accessor surface is **20** non-test sites (was 21; the recovery loop at
`build_desired_state.go:2079` was converted this session — see below). The Info codec
(`internal/session/info_store.go:InfoFromPersistedBead`, lines 23–109) is RICH:
it already projects `state`→`MetadataState`, `sleep_reason`, `pool_slot`,
`pool_managed`, `session_origin`, `dependency_only`, `manual_session`, `Labels`,
`Title`, `pending_create_claim`, `pending_create_started_at`, the named-session
cluster, the trigger/pack cluster, the health cluster, `last_woke_at`,
`state_reason`, `creation_complete_at`, `wake_attempts`, `quarantined_until`, and
the fidelity-trap raw mirrors (`MetadataState`, `SessionNameMetadata`,
`ManualSessionMetadata`, `TransportMetadata`, `Type`/`ContinuityEligible`). ~30
`*Info` classifier siblings exist. Files fully accessor-free and guard-pinned in
`snapshotInfoOnlyFiles`: `template_resolve.go`, `session_name_lookup.go`,
`cmd_citystatus.go`, `session_reconciler_trace_cycle.go`, `providers.go`,
`nudge_dispatcher.go`, `named_sessions.go`.

**DONE previously + this session:** foundation P1–P3, the pool-demand cascade,
five small cascades, and (this session, commits `d8c606fd8`, `79c375147`,
`897f660ea`):
- The `*ForAgent` classifier family Info forms —
  `isLegacyManualSessionInfoForAgent` (`session_origin.go:146`),
  `isManualSessionInfoForAgent` (`session_origin.go:167`),
  `sessionAgentMetricIdentityInfo`/`pooledFallbackIdentityInfo`
  (`session_name_lookup.go:511`). (`isEphemeralSessionInfoForAgent`,
  `existingPoolSlotInfo` already existed.)
- The pending-create lease Info-helper family — `pendingCreateStartInFlightInfo`,
  `pendingCreateNeverStartedLeaseExpiredInfo`, `pendingCreateAttemptStaleInfo`,
  `pendingCreateLeaseActiveInfo` (`session_reconciler.go:680`),
  `pendingCreateClaimStillLeasedForSweepInfo` (`city_runtime.go:2800`) — plus 3
  fidelity fields `Info.LastWokeAt`/`StateReason`/`CreationCompleteAt`
  (`manager.go:207–214`, codec `info_store.go:97–99`).
- **The pool sweep loop** (`sweepUndesiredPoolSessionBeads`, `city_runtime.go:2658`)
  fully flipped to `OpenInfos()`, candidates recovered via `FindByID(info.ID)`
  (`city_runtime.go:2752`); all 20 `TestSweepUndesiredPoolSessionBeads_*` branch
  tests pass unchanged.
- **The pool recovery loop** (`discoverSessionBeadsWithRoots`,
  `build_desired_state.go:2079`) fully flipped to `OpenInfos()` (commit
  `1dbf692e7`); raw bead recovered via `FindByID(info.ID)` only for the identity
  chain. Added the two siblings it needed — `scaleCheckPartialSessionPreservableInfo`
  and `staleNonExpandingPoolSessionBeadInfo` (the latter equivalence-cased with a
  canonical-singleton agent fixture to hit its true branches). Adversarially
  cross-verified byte-identical. `build_desired_state.go` is NOT accessor-free
  (rule-3 store ops + the identity chain remain), so it is NOT in
  `snapshotInfoOnlyFiles`.

**The 20 remaining sites, categorized (this is the work map):**

| Category | Sites | Disposition |
| --- | --- | --- |
| **reconciler-spine-blocked** (7) | `city_runtime.go:1159`, `:2158`, `:2246`, `:3085`; `cmd_start.go:904`, `:918`; `session_lifecycle_parallel.go:809` | Unblock only after the spine flip / once the reconcile entry + drain/orphan-release ops take Info. |
| **rule3-store-op — stay raw** (7) | `build_desired_state.go:3341`, `:3570`, `:3816`, `:4165`; `city_runtime.go:2752` (SANCTIONED sweep recovery); `session_beads.go:57`, `:2033` | Thread the bead into a store/close op or a raw `[]beads.Bead` helper. Leave raw (contract rule 3). |
| **other-blocked** (2) | `cmd_wait.go:1164` (wait-nudge helper family); `soft_reload.go:103` (needs a `template_overrides`/raw-metadata accessor on Info) | Own small foundation each. |
| **raw-by-design — do NOT convert** (3) | `city_status_snapshot.go:411`, `city_runtime.go:2153`, `city_runtime.go:3246` | See "RAW-BY-DESIGN" below. |
| **codec-edge — EXEMPT** (1) | `session_bead_snapshot.go:301` | The one codec edge; always exempt. |
| **DONE this session** (1) | `build_desired_state.go:2079` (recovery loop) | Flipped to `OpenInfos()` in `1dbf692e7`. |

---

## The reconciler spine flip (THE primary unlock)

> **SESSION UPDATE 2026-07-01 (CONT-5) — DESIGN DECIDED + SCOPE VERIFIED + Tier-0 reset fields landed (`69ccc13c6`).**
> The owner chose **Fork B (working-copy wrapper)** over the full-typed Fork A
> below, and chose to drive it in-session. Deep reconnaissance at HEAD then
> established the true scope, which materially reshapes the plan:
>
> - **The flip is NOT a mechanical field-swap and NOT a single atomic commit.**
>   The spine has **two whole-metadata-map consumers** that read the raw
>   `map[string]string`, not typed fields: `healStatePatchWithRollback` →
>   `sessionpkg.ProjectLifecycle(LifecycleInput{Metadata: …})`
>   (`session_reconcile.go:1058`), and the circuit breaker
>   `restoreFromMetadata`/`observeResetGenerationFromMetadata`
>   (`session_circuit_breaker.go:320/508`), fed `ordered[i].Metadata` at
>   `session_reconciler.go:1146/1155`. `session.Info` has no `Metadata` map, so
>   these **cannot** be fed from `Info` without a fragile field→map
>   reconstruction.
> - **Fork B keeps ProjectLifecycle + circuit breaker + write-back lockstep on
>   the raw bead** (accepted). The raw bead therefore stays the single source of
>   truth and the Phase-1↔Phase-2 aliasing/lockstep is **untouched** — so there
>   is **no atomic-flip requirement and no state-split risk**. The "working-copy
>   wrapper" is realized as a per-iteration `info := sessionpkg.InfoFromPersistedBead(*session)`
>   derived alongside the raw working copy; the tick's **classifier DECISION
>   reads** go through `info`, and `info` is re-derived after a mutation. Each
>   decision-read cluster converts **independently and incrementally** (verify
>   each against the reconcile/pool E2E suites — the byte-identical oracle).
> - **Consequence:** the reconciler files will NOT become accessor-free (the
>   ProjectLifecycle/CB/write-back raw reads remain, by design), so they are NOT
>   added to `snapshotInfoOnlyFiles`, and the 7 "reconciler-spine-blocked"
>   sites that thread raw `[]beads.Bead` into the reconcile entry are
>   **reclassified as rule-3-sanctioned** (the entry legitimately needs raw beads
>   for ProjectLifecycle/CB), not converted.
> - **Verified scope:** 194 raw `.Metadata[` reads (reconciler 123 / reconcile
>   50 / wake 21) + 6 `.Status`. Most are inside the raw machinery that stays.
> - **Field-gaps (decision-reads only — machinery-only keys stay raw):**
>   `reset_committed_at` + `continuation_reset_pending` → **DONE** (`Info.ResetCommittedAt`
>   / `Info.ContinuationResetPending` + `resetPendingCommittedAtInfo`, `69ccc13c6`).
>   `generation` → needs **`Info.Generation string`** (RAW mirror, NOT int — fidelity
>   trap: read both as `strconv.Atoi` AND `strings.TrimSpace`, `session_wake.go:41/173/283/331/350/461`).
>   `started_config_hash` → has BOTH decision reads (drift detection, `session_reconciler.go:2026/2278/3571/3733`)
>   and write-back; classify per site, add `Info.StartedConfigHash` (raw) for the
>   decision reads. `held_until`/`pin_awake`/`wake_request` → ProjectLifecycle/machinery
>   (stay raw) except `pin_awake` at `session_reconciler.go:2501` (a decision — needs a mirror).
>   `churn_count`/`core_hash_breakdown` → write-back/drift-logging machinery (stay raw).
> - **Recommended incremental order (each its own verified commit):** (1) add
>   `Info.Generation` + convert Phase 2 `advanceSessionDrainsWithSessionsTraced`
>   decision reads (bounded, self-contained, 428–668); (2) the Phase-1 driver
>   decision-read clusters, cluster by cluster, deriving `info` per iteration;
>   (3) leave the apply/write-back cluster + ProjectLifecycle + CB on raw.
>   This is genuinely a **3–5 session effort**; do not rush it.
>
> The Fork-A material below (full-typed Info, re-projection primitive, Tier-2
> classifier siblings, atomic flip of the maps + Phase 2) is **superseded** by
> Fork B and retained only as background.

### What it actually is (verified)

The reconcile tick does NOT hold a raw bead as a read-leak — it holds
`session := &ordered[i]` (a `*beads.Bead`) at **`session_reconciler.go:1227`**, a
**mutable per-tick working copy** aliasing into `ordered []beads.Bead` (from
`topoOrder`, `:1121`). The canonical mutation pattern (`healStateWithRollback`,
`session_reconcile.go:1025–1051`):

```go
batch := healStatePatchWithRollback(*session, alive, clk, …)  // PURE: reads fields → map[string]string
sessFront.ApplyPatch(session.ID, batch)                       // WRITE: InfoStore front door (already correct)
for k, v := range batch { session.Metadata[k] = v }           // LOCKSTEP: mutate the working copy in-memory
```

**Two load-bearing facts the flip must honor:**

1. **Two maps alias the same backing array.** `circuitSessionByIdentity
   map[string]*beads.Bead` (built `:1139/:1145`, `&ordered[i]`) and `beadByID
   map[string]*beads.Bead` (built `:1183–1186`, `&ordered[i]`). The code comment
   at `:1180–1182` is explicit: the pointers "intentionally alias into the
   ordered slice so that mutations in Phase 1 (healState, clearWakeFailures,
   etc.) are visible to Phase 2's advanceSessionDrains via this map." A lockstep
   `session.Metadata[k]=v` (or `session.Status="closed"`) is thereby visible to
   every later consumer this tick.
2. **Phase 2 consumes the same array two ways.**
   `advanceSessionDrainsWithSessionsTraced` (`session_wake.go:428`) takes BOTH a
   `sessionLookup func(id) *beads.Bead` (backed by `beadByID`,
   `session_reconciler.go:2792–2795`) AND `sessions []beads.Bead` (= `ordered`).
   **Any flip that does not migrate Phase 2 in lockstep splits state — a
   correctness break, not a cosmetic one.** This is the hard scope floor.

Besides `healStateWithRollback`, the tick also does **direct** working-copy
writes that need Info analogs: `session.Status="closed"` at
`session_reconciler.go:1350` and `:1574`; `session.Metadata["restart_requested"]="true"`
at `:1858`; and a restart-handoff batch lockstep `for key,value := range batch {
… session.Metadata[key]=value }` at `:1908–1920` (with the `ResetCommittedAtKey`
skip at `:1916–1918`).

### The single hardest blocker — there is no in-memory re-projection primitive

`session.Info` (`internal/session/manager.go:74`) is a struct of individually
typed fields — it has **NO `Metadata` map** and **no `applyMetadataPatch` / re-
project helper anywhere**. `InfoStore.ApplyPatch` (`internal/session/store.go:35`)
writes by ID to the store only; it does not mutate an in-memory `Info`. So the
lockstep step (3) above **has no `Info` analog today**. Building that primitive is
Tier 1 of the foundation and gates the entire flip.

### The recommended flip cluster (critic-endorsed)

Make the working copy a mutable `session.Info`; each mutating helper takes the
working `*session.Info` + `sessFront`, computes the same `batch` from an Info-form
patch computer, writes via `sessFront.ApplyPatch(info.ID, batch)` (unchanged),
then re-projects/lockstep-applies the batch onto the working Info.

**Functions in the flip cluster** (all take `*beads.Bead` today; none have Info
forms): `healState`, `healStateWithRollback`, `checkStability`,
`checkRateLimitStability`, `checkChurn`, `markProviderTerminalError`,
`recordWakeFailure`, `clearWakeFailures`, `recordChurn`, `clearChurn`,
`recordRateLimitQuarantine`, `clearLastWokeAt`, `healExpiredTimers`,
`markDrainAckStopPending`, `recoverPendingIdleSleep`, `reconcileDetachedAt`,
`persistSessionCircuitBreakerMetadata` — plus the inline direct writes at
`session_reconciler.go:1350`, `:1574`, `:1858`, `:1908–1920`.

**Callers that flip in the same commit:**
- `reconcileSessionBeadsTracedWithNamedDemand` (`session_reconciler.go:1005`) —
  the sole Phase 0/0.5/1/2 driver. Its compatibility wrappers
  (`reconcileSessionBeadsAtPath`/`…WithNamedDemand`/`…Traced`, `:800–971`) only
  forward `sessions []beads.Bead` and need **no signature change** — the working-
  copy type is internal to the driver (`ordered`/`beadByID`/`circuitSessionByIdentity`
  are all derived inside it).
- **Phase 2 `advanceSessionDrainsWithSessionsTraced`** (`session_wake.go:428`) —
  MUST migrate in lockstep (fact 2 above). Re-type `beadByID` +
  `circuitSessionByIdentity` to `*session.Info` in this same commit.

**Sequence (each its own reviewed, test-green commit; do NOT fan agents at this
connected component):**
1. **Tier 0 + Tier 1 foundation** (below) — additive, no caller flips.
2. **Tier 2 pure-classifier Info siblings** (below) — additive, equivalence-cased.
3. **The flip** — the cluster + the two aliasing maps + Phase 2, all together.

### Foundation, ordered by dependency (land BEFORE the flip)

**Tier 0 — missing codec fields (blocks the flip; `resetPendingCommittedAt`,
`session_reconciler.go:103`, reads both at `:1230`):**
- `Info.ResetCommittedAt` (mirrors `reset_committed_at`,
  `sessionpkg.ResetCommittedAtKey`) + `Info.ContinuationResetPending` (mirrors
  `continuation_reset_pending`) → add to the struct + populate in
  `InfoFromPersistedBead` + equivalence case. Verified absent today.
- If the flipped helpers read them (they do): `Info.ChurnCount`,
  `Info.SessionKey`, `Info.StartedConfigHash`, `Info.CoreHashBreakdown` (raw-only
  today; `checkChurn`/`recordChurn`/`silentRebaselineSessionHashes`/
  `rebaselineLaunchDriftHashes` read `churn_count`/`session_key`/
  `started_config_hash`/`core_hash_breakdown`).

**Tier 1 — the re-projection primitive (the hardest blocker):**
- Add either `Info.applyMetadataPatch(batch)` that re-derives every affected
  typed field with the SAME parse/trim/normalize rules `InfoFromPersistedBead`
  uses, or a re-project-via-`InfoFromPersistedBead` path. Key→field mappings that
  MUST match: `state`→`MetadataState` (verbatim) AND `State` (via
  `normalizeInfoState`); `sleep_reason`→`SleepReason`;
  `wake_attempts`→`WakeAttempts` (Atoi); `pending_create_claim`→`PendingCreateClaim`
  (=="true"); `pending_create_started_at`→`PendingCreateStartedAt`;
  `quarantined_until`→`QuarantinedUntil`; `last_woke_at`→`LastWokeAt`;
  `state_reason`→`StateReason`; `session_health`→`HealthState`;
  `provider_terminal_error`→`ProviderTerminalError`;
  `session_drainable`→`Drainable` (=="true"); plus the Tier-0 fields.
- **Prove it with a recording test:** apply batch to a bead + re-project via
  `InfoFromPersistedBead` == apply the same batch to `Info` directly. Byte-equal
  for every key the spine writes. This is the equivalence oracle for the flip.
- Add a mutable `Info.Closed`/status path so the direct `session.Status="closed"`
  writes at `:1350`/`:1574` have an Info analog.

**Tier 2 — pure-classifier Info siblings on the spine's read set (all verified
missing; mirror each `beads.Bead` form byte-for-byte + cover in
`TestSessionClassifierInfoEquivalence`):** `healStatePatchInfo` (mirror
`healStatePatchWithRollback`, `session_reconcile.go:1058`), `sessionExitFactsInfo`,
`productiveLongEnoughInfo`, `stableLongEnoughInfo`, `sessionStartRequestedInfo`,
`resetPendingCommittedAtInfo` (needs Tier-0 field), `pendingCreateLeaseExpiredForRollbackInfo`,
`pendingCreateSessionStillLeasedInfo`, `shouldRollbackPendingCreateInfo`,
`resolveSessionSleepPolicyInfo`, `isPoolExcessInfo`, `sessionWithinDesiredConfigInfo`.
(Already present and reusable: `isKnownStateInfo`, `sessionMetadataStateInfo`,
`sessionWakeAttemptsInfo`, `sessionIsQuarantinedInfo`, `sessionHasProviderTerminalErrorInfo`,
the whole pending-create lease family, the `*ForAgent` family.)

---

## The rest of the sites (after / independent of the spine flip)

Convert as their blocking helper gains an Info form; add each newly accessor-free
file to `snapshotInfoOnlyFiles`.

- **`build_desired_state.go:2079` (recovery loop, `discoverSessionBeadsWithRoots`)
  — DONE in `1dbf692e7`.** Flipped to `OpenInfos()`; the two foundation siblings
  (`scaleCheckPartialSessionPreservableInfo`, `staleNonExpandingPoolSessionBeadInfo`)
  landed + equivalence-cased; raw `b` recovered via `FindByID(info.ID)` only for
  the identity chain (`sessionBeadQualifiedName`, `canonicalSessionIdentityWithConfig`,
  `resolveTemplateForSessionBead` — rule 3). Left as the reference shape for the
  remaining loops.
- `city_runtime.go:3085` (`filterSessionBeadsByName`) — its caller feeds
  `newSessionBeadSnapshot(open)` + the raw-bead reconciler; converts only once
  `reconcileSessionBeadsAtPathWithNamedDemand` takes Info (part of the spine).
- `soft_reload.go:103` — needs a `template_overrides` (or raw-metadata-map)
  accessor on `Info` for `sessionCoreConfigForHash`→`applyTemplateOverridesToConfig`
  (`sessionpkg.ParseTemplateOverrides(session.Metadata)`), plus Info forms of the
  drain helpers (`clearSoftReloadConfigDriftDrainAck`/
  `cancelSoftReloadConfigDriftDrain`→`cancelSessionConfigDriftDrain`).
- `cmd_wait.go:1164` (ready-wait-nudge loop) — the wait-nudge helper family
  (`cachedSessionCanReceiveWaitNudge`, `waitNudgeAgent`, `sessionProviderFamily`,
  `waitNudgePollerKey`) needs Info forms; then convert the loop. Its store ops
  (`startNudgePoller`, `enqueueQueuedNudge`) stay.
- `session_beads.go:2033` — loop reads `pending_create_claim` + `isNamedSessionBead`
  over raw; convert if pure else leave (rule 3). `session_beads.go:57` returns
  `Open()` as `[]beads.Bead` to raw callers — a return-type cascade; do with its
  callers or leave.
- `city_runtime.go:1159`/`:2158`/`:2246`, `cmd_start.go:904`/`:918`,
  `session_lifecycle_parallel.go:809` — thread the raw `open []beads.Bead` into
  drain/orphan-release/reconcile/`resolvePreservedConfiguredNamedSessionTemplate`
  store ops; stay raw (rule 3) until those ops take Info (largely the spine flip).

### RAW-BY-DESIGN — do NOT convert (not leaks)

- `city_status_snapshot.go:411` `countCitySessionsFromSnapshot` — needs
  `IsSessionBeadOrRepairableInfo` (exists) but its fidelity hinges on the
  snapshot-only-holds-session-beads invariant; prove that invariant first.
- `city_runtime.go:2153` `emitDueComputeFacts` — usage-bookkeeping metadata
  (`awake_started_at`/`slept_at`/`usage_compute_emitted_at`), not session identity.
- `city_runtime.go:3246` `sessionBeadSnapshotFingerprint` — hashes
  ID/Status/Assignee/ALL raw metadata; a whole-bead change fingerprint.
- `session_bead_snapshot.go:301` — the codec edge (`newSessionBeadSnapshot`);
  always EXEMPT.

## P5 — the `closeBead` cross-class split (LANDMINE — isolated, last)

`closeBead(store, id, reason, now, stderr)` in `session_beads.go` decomposes into
SESSION close (`InfoStore.Close`, `internal/session/store.go:222` — bundles skip-
if-closed idempotence + ClosePatch + CloseWithoutReason; deliberately OMITS work-
release), EXTMSG (`cancelStateAssignedToRetiredSessionBead` = `session.CancelWaits`
+ `extmsg.CloseSessionBindings`), and WORK release (the `workAssignment` façade).
Order is **close-THEN-release**; **preserve skip-if-already-closed idempotence**
(it prevents the bead.updated storm across the 3 reconciler close paths). Prove
the exact op sequence with a recording-fake store. Also tidy `createPoolSessionBead`
to thread `sessFront` (`CreateSession`/`CreateSpec` exist). (closeBead body/caller
line offsets not re-verified in the last workflow pass — re-open before editing.)

## P6 — close it out + enforce

1. As each consumer file becomes accessor-free, add it to `snapshotInfoOnlyFiles`.
2. Once every caller uses the Info forms, delete the now-dead bead classifiers /
   `Open()` / `FindSessionBeadBy*` — the snapshot codec edge
   (`newSessionBeadSnapshot`) legitimately keeps raw classifiers; it is EXEMPT.
3. Extend the guard to also forbid `.Store().Store` in the fully-converted files.

## Method (proven this stack)

Keep each original classifier UNTOUCHED + ADD the typed sibling + ADD an
equivalence case (byte-identical oracle), THEN flip the signature with ALL its
callers in the SAME commit. `snapshot.OpenInfos()[i]` is the precomputed
projection of `Open()[i]`, so raw and Info slices coexist during partial migration
— a full-component atomic flip is not required (EXCEPT the reconciler mutation
spine, which must flip together with Phase 2 and the two aliasing maps). For
foundation gaps, add the Info field + codec population + equivalence case BEFORE
the site that needs it. Test call sites project fixtures via
`sessionInfosFromBeads([]beads.Bead) []session.Info`.

## Build / commit / gate hygiene

- `git checkout go.sum` after builds; commit AND push with `--no-verify` (stale
  hooksPath + the pre-push hook runs the full suite and times out — run gates
  manually). Trailer:
  `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- Never `tmux kill-server`; never `go clean -cache` (`-testcache` ok; cold build
  `GOCACHE=$(mktemp -d)`); gascity Dolt is LOCAL-ONLY (no `bd dolt push`).
- Gates: `go build ./...` · `go vet ./...` ·
  `golangci-lint run ./cmd/gc/... ./internal/session/...` (0) · the equivalence +
  guard tests · targeted subject suites (reconcile/pool/wait). The build host is
  oversubscribed — run targeted `-run` locally; CI on dedicated runners is the
  byte-identical gate. **`make dashboard-check` not needed** (`Info` additions
  stay internal — empty openapi/docs-schema/generated-TS diff).

## Finish (only when #3839 CI is verified GREEN — no premature ready)

- `gh pr checks 3839 --watch`
- ready (gh pr ready aborts on projectCards — use the API):
  `gh api graphql -f query='mutation($id:ID!){markPullRequestReadyForReview(input:{pullRequestId:$id}){pullRequest{isDraft}}}' -f id=$(gh api repos/gastownhall/gascity/pulls/3839 --jq .node_id)`
- label:
  `gh api --method POST repos/gastownhall/gascity/issues/3839/labels -f 'labels[]=status/needs-review-auto'`

**Done =** every non-work consumer reads via `session.Info` (grep-clean of raw
snapshot accessors + `.Store().Store`), the guard forbids regression, full gates
+ #3839 CI green, #3839 ready + labeled. Update `memory/infra-beads-decoupling-plan.md`.
