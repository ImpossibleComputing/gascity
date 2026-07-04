# Reconciler front-door — the LOCKSTEP DROP (in progress; Steps 1–2 done, 3 next)

**PR #3839** (DRAFT, base `main`), branch `upstream/object-front-doors-cleanup`,
worktree `.claude/worktrees/object-front-doors`, **HEAD `1d2ea2028`** (re-grep
`git rev-parse HEAD`; line numbers below drift as you edit — always re-grep).

## Progress

- [x] **Step 1 — circuit persist store-authoritative + drop `circuitSessionByIdentity`**
      (`ec6127ead`). CORRECTION to the plan's stale line-anchored model: the Phase-0.5
      restore READS already project `ordered[i].Metadata` via `CircuitStateFromMetadata`
      (Step 5); the only surviving raw consumer was the progress-sig persist lookup.
      `persistSessionCircuitBreakerMetadata`/`recordSessionCircuitBreakerRestart` now take
      `id string`, equality via `sessFront.CircuitState(id)`, raw mirror dropped, dead
      `sessionCircuitMetadataEqual` removed. `circuitSessionByIdentity` → `circuitIDByIdentity`
      (`map[string]string`). Byte-identical under a healthy store; fable review wf_803d0b26
      (0 defects beyond one ACCEPTED LOW: the equality-skip now does a store Get on the
      previously-free path — details in `raw/lockstep-drop-step1-circuit.md`).
- [x] **Step 2a — `completeDrain` store-only** (`4bcec563b`). Drop the bead mirror; take
      `sessions.Info` (id + raw wake_mode). Byte-identical (mirror had no in-tick consumer;
      tests assert on the store).
- [x] **Step 2b — `advanceSessionDrains` off the raw bead + retire `beadByID`/`sessionLookup`**.
      Traced core takes `infoLookup func(id)(Info,bool)`; loop reads Info only; `verifiedStop`
      + drain-cancel Info siblings; reconciler feeds it `infoByID`. Fable review wf_381c5866:
      2 lenses clean, F2/F4 byte-identical, 1 refuted, 1 CONFIRMED-then-FIXED: the
      `buildPreparedStart` `instance_token` residue (now threaded into recoverRunningPendingCreate's
      returned fold batch + teeth test). Details in `raw/lockstep-drop-step2-drains.md`.
- [ ] Steps 3–6 below.

## Where things stand

The reconciler's decision reads are all on the typed `session.Info` snapshot
(`infoByID`), and every snapshot refresh is write-returns-`Info` — **no code
re-derives `Info` from the raw working bead anywhere on the decision or refresh
path.** The blanket pre-pass, both aggregating refreshes, and `refreshSessionInfo`
are deleted (see `RECONCILER-FRONT-DOOR-STEP6-PREPASS-AUDIT.md`). Verified by the
comprehensive reconciler suite (211-212s green) + a 4-lens capstone fable review
(0 defects).

**Already removed (Steps 1–2b):** `circuitSessionByIdentity`, `beadByID`, and
`sessionLookup` are GONE. The circuit persist (`persist`/`recordSessionCircuitBreakerRestart`),
`completeDrain`, and the whole Phase-2 drain scan (`advanceSessionDrainsWithSessionsTraced`,
`verifiedStop`, the drain-cancel helpers) are off the raw bead and their mirrors dropped.

**What's still physically present but READ-DEAD for decisions:** the raw
`ordered []beads.Bead` working set, and the remaining `session.Metadata[k]=v` lockstep
mirror writes in the forward pass (re-grep `session\.Metadata\[.*\] *=` in
session_reconciler.go — ~11 left after Steps 1–2b). The `wakeTargets` loop still carries
raw `target.session` beads (a **separate** source from `ordered`; addressed in Steps 3–5).
The remaining lockstep drop removes all of it.

## The governing safety principle (unchanged)

> Never remove a raw read/mirror until its typed replacement is in place and
> byte-identical. Convert each consumer, verify, THEN delete.

Two hard invariants the CI enforces and the awake scan depends on:
- **`buildAwakeInputFromReconciler` slice order is load-bearing.** It appends to
  `input.SessionBeads` in `ordered` slice order and `ComputeAwakeSet` does
  `SessionName`-keyed **last-write-wins** + first-match `resolveNamedSessionBeadName`
  over it. `SessionName` is NON-unique (a retired-duplicate + winner share it). So
  the iteration domain must stay **ORDER-PRESERVING** — replace `ordered` with an
  `[]Info`/`[]string` in the SAME order, **never** `range infoByID` (map iteration
  reorders and can flip an outcome). `openPoolSessionCountForTemplate` MAY
  domain-switch to `infoByID` (unique IDs proven, order-independent count).
- **The tick-start snapshot is store-equivalent already.** `infoByID` is built at
  tick entry as `InfoFromPersistedBead(ordered[i])`, and `ordered` is the
  store-loaded bead set the reconciler was handed. So "cut to store `Get`/`List`"
  is mostly: keep building the tick-start snapshot from the loaded beads, then stop
  keeping the raw beads around — it is NOT a per-refresh `Get` (the reverted
  #2345/#2574 hazard). Per-refresh `Get` was tried and rejected (STEP6-DESIGN §2).

## The remaining raw consumers (re-grep — these are what to convert)

1. ~~**`advanceSessionDrainsWithSessionsTraced`**~~ **DONE (2b, `1d2ea2028`).** Takes
   `infoLookup func(id)(Info,bool)`; drain scan reads Info only; `verifiedStop` + the
   drain-cancel helpers have Info siblings; `beadByID`/`sessionLookup` removed. The
   `sessionBeads []beads.Bead` param SURVIVES (dead in the prod call — `wakeEvals` non-nil
   — but non-prod callers pass `wakeEvals==nil` for the `computeWakeEvaluations` fallback);
   drop it only when `ordered` goes (Step 5).
2. ~~**The Phase-0.5 circuit-breaker block**~~ **DONE (1, `ec6127ead`).** `circuitSessionByIdentity`
   (`map[string]*beads.Bead`) → `circuitIDByIdentity` (`map[string]string`); circuit persist
   is store-authoritative by id.
3. **`buildAwakeInputFromReconciler`** (`compute_awake_bridge.go`, reconciler call ~3007) —
   **NEXT (Step 3).** DECISION READS are already on Info (4C/4D): the loop reads
   `sessionInfoByID[b.ID]` (falls back to `InfoFromPersistedBead(*b)` for nil-snapshot unit
   tests). What remains is the **DOMAIN**: it iterates `sessionBeads []beads.Bead` (= `ordered`).
   Replace the param with an order-preserving `[]session.Info` (build it in the reconciler as
   `sessionInfos[i] = infoByID[ordered[i].ID]`, SAME order as `ordered`) and drop the
   `sessionInfoByID` map + the fallback. **NEVER `range infoByID`** (slice order is load-bearing —
   see the invariant above). 15 test call sites + 1 reconciler site.
4. **The `wakeTargets` / `sleep_intent` sub-thread** (`session_reconciler.go` ~3185-3222, ~4362;
   and the `wakeTargets` loop in `buildAwakeInputFromReconciler` reading
   `target.session.Metadata["session_name"]`) — `target.session` is a **raw bead carried on
   `wakeTarget`** (a different source than `ordered`, deemed out-of-scope in 4C). The post-loop
   `sleep_intent` read/clear (`SetMarker` + `target.session.Metadata["sleep_intent"] = ""`) is a
   raw read+mirror. `Info.SleepIntent` exists (`b.Metadata["sleep_intent"]`, raw). Convert these
   reads to `Info`/store and drop the mirror. Can be its own step (3.5) or folded into Step 3.
5. **`newSessionBeadSnapshot` / `resolvePreservedConfiguredNamedSessionTemplate`** (bucket-D,
   STEP6-PREPASS-AUDIT / §7) — the whole-bead template subsystem still reads raw beads;
   feed it from a store source. HARDEST — may need a store `List`.
6. **The remaining raw `session.Metadata[k]=v` mirrors + `ordered []beads.Bead`** (re-grep
   `session\.Metadata\[.*\] *=` in session_reconciler.go — ~11 left; each has a fold beside it
   now). Delete them ONLY after 3-5, in the same commit as removing `ordered` (nothing reads the
   raw bead by then). This also drops the now-dead `sessionBeads` param on `advanceSessionDrains`.

## The Get-cutover exposure set (mostly already handled — verify, don't re-solve)

The raw refresh preserved deliberate intra-tick raw/store divergences. Confirm each is
handled before cutting the tick-start build to a store `List`:
- **`reset_committed_at`** (#2345 force-wake): persisted by RestartRequestPatch this tick
  but kept OFF this tick's snapshot. Already handled — `restartFold` EXCLUDES
  `ResetCommittedAtKey` (Commit 4), so the fold never adds it this tick; a tick-start
  `List` correctly reads the PRIOR tick's durable value. **No new work if the build stays
  at tick entry.**
- **`started_live_hash`**: persisted without a mirror; `Info.StartedLiveHash` has ZERO
  decision readers (verified). Harmless.
- **`buildPreparedStart` residue** (`recoverRunningPendingCreate`): the `instance_token`
  mint is now THREADED into the returned fold batch (Step 2b, `pendingCreateInstanceTokenFold`)
  because `verifiedStop` reads `info.InstanceToken`. The OTHER residue keys (a stale-resume
  clear of `session_key`/`started_config_hash`/`continuation_reset_pending`) are still NOT
  threaded — verified inert (no divergent same-tick Info reader) by the pre-pass capstone
  (wf_e8507262). **CAUTION for Step 3:** the awake scan already reads
  `info.ContinuationResetPending`/`ConfigDrift`-adjacent fields from the snapshot; re-confirm
  (fable review) that the un-threaded residue keys stay inert when you touch the awake path,
  and thread them if a divergence surfaces.

## 6e — the CI guard (last)

Extend `snapshotInfoOnlyFiles` (`frontdoor_di_guard_test.go:83`) to ALSO forbid raw
`session.Metadata[` reads/writes on the reconciler decision path (today it only forbids
the four raw snapshot accessors), then add the reconciler files once raw-free. Keep the
documented raw-by-design exceptions (witness full-resync, work-bead reads).

## Suggested commit sequence

1. ~~**CB block → `Store.CircuitState`**~~ DONE (`ec6127ead`).
2. ~~**`advanceSessionDrains` off the raw bead**~~ DONE (2a `4bcec563b`, 2b `1d2ea2028`).
3. **`buildAwakeInputFromReconciler` domain → order-preserving `[]session.Info`** (NOT
   map iteration; slice-order invariant lives here). ← NEXT.
   - **3.5** (optional split): the `wakeTargets` / `sleep_intent` raw reads+mirror
     (consumer #4). Can land separately or with Step 3.
4. **`newSessionBeadSnapshot` off a store source** (bucket-D, hardest — may need `List`).
5. **Drop the remaining lockstep mirrors + `ordered []beads.Bead` + the dead `sessionBeads`
   param + cut the tick-start build to the store**. Nothing reads the raw bead by now.
6. **6e guard.**

Each step: build · vet · golangci-lint 0 · gofmt · the reconciler suite (`go test ./cmd/gc/
-run 'Reconcile|Awake|Wake|Sleep|Pool|DrainAck|Recycle|Zombie|Heal|Drift|Churn|Stability|
RateLimit|Named|Restart|Progress|Rollback|PendingCreate|MinFloor|Idle|MaxAge|Detach|Rebaseline|
Relaunch|Quarantine|Circuit|Lifecycle|Session' -timeout 25m` — the full `cmd/gc` package times
out at 600s, use this subset or TESTING.md shards). The suite is the byte-identity gate: a raw
consumer converted wrong flips an awake/drain decision and fails. Run a fable adversarial review
per non-trivial step (owner prefers fable). Commit + push `--no-verify`. Trailer:
`Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. #3839 stays DRAFT.

## Beyond the lockstep drop (the wider backlog)

This completes the reconciler front-door (Phase 5 reads). Then, per
`infra-beads-decoupling-plan.md` / OBJECT-MODEL-FRONT-DOOR-DESIGN §7:
- The cross-class **WORK/assignment split** (design §5 / Phase 6).
- The tier fix (Phase C).
- The owner-gated **cold migration** (`maintainer-city` dolt→postgres) — stop-first,
  owner-approved, the live-system landmine. NOT a code change; do last.
