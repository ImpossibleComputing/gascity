# Reconciler front-door LOCKSTEP DROP — finish line (Steps 5b–5e + 6e)

> **STATUS UPDATE (2026-07-04, HEAD `b98725a35`): Step 5b is DONE + a systemic blocker was found & resolved.**
> A trace-decouple **precursor** (`245a86b4a`) landed first: the raw mirror WRITES are NOT read-dead — the
> post-reconcile gc-trace read (`recordReconcileTraceResults`) reads `open[i].Metadata` via maps shared with the
> reconciler's `ordered`, so deleting a mirror stales the trace, and there's no byte-identical replacement. Owner
> chose to source that read from the authoritative post-reconcile store snapshot (accuracy improvement, trace-only)
> — which UNBLOCKS the mirror deletions. Step 5b (`97fd6fbc6`) then took the drain-ack family off raw reads +
> deleted its mirrors, KEEPING `finalizeDrainAckStoppedSession`'s `*beads.Bead` param (the "drop the param"
> instruction below is INFEASIBLE — raw-by-design whole-bead helpers). **RESUME AT 5c.** Full detail + the 5c
> unblock are in `RECONCILER-FRONT-DOOR-LOCKSTEP-DROP.md` progress + memory CONT-31. The 5b/5c framing below is
> historical; read it for the census discipline, not the exact param mechanics.

**PR #3839** (DRAFT, base `main`), branch `upstream/object-front-doors-cleanup`, worktree
`.claude/worktrees/object-front-doors`, **HEAD `b98725a35`** (re-grep `git rev-parse HEAD`; every
line number below drifts as you edit — ALWAYS re-grep before touching).

## Where things stand

The **entire decision-path READ conversion is DONE** (Steps 1–5a). Every raw `session.Metadata[…]`
**decision read** on the reconciler path now reads the typed `session.Info` snapshot (`infoByID`) or a
store front door. What remains is the **delete-the-now-read-dead-scaffolding** phase: remove the raw
lockstep mirror *writes*, drop a dead param, demote the raw `ordered` working set, and add the CI guard.

**Authoritative per-site design:** `RECONCILER-FRONT-DOOR-REMAINING-PLAN.md` §Step 5 / §Step 6e — a
fable audit+synthesis (wf_b8f8125a) with two settled boundary corrections:
- **Step 5 DEMOTES `ordered`, does NOT delete it.** `ordered` survives as the load-time slice that (i)
  builds the tick-start `infoByID` snapshot and (ii) carries raw beads into the documented raw-by-design
  / action consumers. Physical deletion needs `startCandidate.session`/`buildPreparedStart` (start
  EXECUTION) converted — that is **consumer #7, out of scope** (a separate future initiative).
- Step 5's real deliverable: **zero raw decision reads (done) + zero lockstep mirror WRITES on the
  decision path**, and the 6e guard enforcing it.

Progress + commit hashes: `RECONCILER-FRONT-DOOR-LOCKSTEP-DROP.md`. Session log: memory
`infra-beads-decoupling-plan.md` CONT-25→30.

## What's shipped (all pushed, each fable-reviewed 0-findings unless noted)

| Step | Commit | What |
|---|---|---|
| 1/2a/2b | `ec6127ead`/`4bcec563b`/`1d2ea2028` | circuit persist, completeDrain, advanceSessionDrains off raw; drop `circuitSessionByIdentity`/`beadByID`/`sessionLookup` |
| 3 | `0d694acee` | awake scan domain → order-preserving `[]session.Info` |
| 3.5a/b/c | `2d387146c`/`60e231cb2`/`a06980fd0` | wakeTargets apply loop + awake bridge off raw; 4 mutating helpers write-returns-Info |
| 4 | `656d322c5` | preserve-template feed off raw `ordered` (live `infoByID`, `newSessionBeadSnapshotFromInfos`) |
| 5a | `6e31df0dc` | 20 forward-pass decision reads → `infoByID[session.ID]`; **fable review caught + fixed a real MEDIUM** (started_config_hash stale-resume residue → `pendingCreateResidueFold`) |

## THE 5a LESSON (read before 5b–5c)

Flipping a forward-pass read onto `infoByID` can make it a **same-tick reader of an un-folded
`buildPreparedStart` residue**. `clearStaleResumeKeyMetadata` (`session_lifecycle_parallel.go:1829`)
writes `session_key=""`, `started_config_hash=""`, `continuation_reset_pending="true"` to the raw bead +
store OUTSIDE any folded batch. The residue set + status:
- `instance_token` — threaded (2b, `pendingCreateResidueFold`).
- `started_config_hash` — threaded (5a, `pendingCreateResidueFold`).
- `session_key` — no same-tick Info reader (unthreaded, fine).
- `continuation_reset_pending` — the awake scan reads `Info.ContinuationResetPending`. `reset_committed_at`
  is DURABLE (sole writer `RestartRequestPatch`, no clearer), so the residue CAN cause a **one-tick,
  self-healing awake-scan deferral** — a **pre-existing Step-3/6d gap, #2345-class**, NOT introduced by 5a.
  Left as-is (threading it would change awake behaviour vs the current snapshot); tracked here for a
  separate cleanup, not a lockstep-drop step.

**Rule:** before flipping/deleting anything touching `session_key`/`started_config_hash`/
`continuation_reset_pending`/`instance_token`, check `buildPreparedStart` + `clearStaleResumeKeyMetadata`.

## The remaining steps (per REMAINING-PLAN §Step 5; re-grep all anchors)

### 5b — drain-ack finalize family off the raw bead
`finalizeDrainAckStoppedSession` (`session_reconciler.go:358`) and `markDrainAckStopPending` (`:82`) drop
their `*beads.Bead` param and read `Info.WakeMode`/`RestartRequested` (verbatim). The NDI witness arm
(`:450` `drainAckFinalizeResult{witnessInfo}`) builds `InfoFromPersistedBead(latest)` directly (source is
the store bead `latest`, not `ordered`; the wholesale `session.Status/Metadata = latest.*` swap dies with
the param). Mirror loops `:102`/`:418`/`:485` die with the params; callers keep folding the returned
`drainAckFinalizeResult`/reconstructed batch exactly as today. **Gate:** the non-reconciler caller
`finalizeDrainAckStopPendingSessions` (via `city_runtime.go`) keeps its accepted per-bead
`InfoFromPersistedBead` boundary projection (same pattern as the advanceSessionDrains wrappers).

### 5c — DELETE the raw lockstep mirror WRITE loops (THE riskiest sub-commit)
Re-grep `session\.Metadata\[.*\] *=[^=]` in `session_reconciler.go` (currently ~:2263, :2344, :2902,
:2983, :3683, :4345, :4739, :4853 + the loop-2 `target.session.Metadata["sleep_intent"]=""` clear, minus
the 5b ones). Each is a lockstep mirror whose fold already carries the same keys onto `infoByID`. Delete
each ONLY after a **per-key census**:
- **START-EXECUTION COUPLING — mirrors whose keys the start path reads SURVIVE.** `buildPreparedStart`
  reads `session_key`/`instance_token`/`last_woke_at`/`currently_processing_bead_id` off the RAW bead
  handed via `startCandidate.session` (`session_lifecycle_parallel.go` ~:916/:1005/:1539). At minimum
  `recordCurrentBeadIDOnWake`'s `CurrentBeadIDKey` mirror (session_bead_cycle.go, folded but the raw
  mirror is kept — it feeds `startCandidates` at the append `:3166`-ish) SURVIVES. Document each survivor
  as "start-execution coupling" here + in LOCKSTEP-DROP.md.
- **The reconciler suite MAY NOT catch a wrong deletion** — start execution is downstream of the decision
  reads the suite asserts. So the census is grep-by-key (does any THIS-TICK reader — incl.
  `buildPreparedStart` via `startCandidate` — read the key off the raw bead?), NOT loop-by-loop.
- `#2345`: the restart-handoff loop at `:2344` and the `restartFold` share one loop with the
  `ResetCommittedAtKey` skip; deleting the raw half must keep the fold loop's exclusion verbatim.
- `cycleAliveSessionForFreshReassign`'s mirror is droppable (its branch `continue`s; the bead never
  enters `startCandidates`).

### 5d — drop the dead `sessionBeads` param
`advanceSessionDrainsWithSessionsTraced` (`session_wake.go:484`, param `sessionBeads []beads.Bead:489`):
prod-dead (the reconciler call `session_reconciler.go:3376` passes `wakeEvals` non-nil, so the
`wakeEvals==nil` `computeWakeEvaluations` fallback is unreachable there). Move that fallback into the
test-only wrappers (`session_wake.go:467` `advanceSessionDrainsWithSessions`), which keep their own
`[]beads.Bead` + per-bead Info projection at the boundary. Do NOT delete
`computeWakeEvaluations`/`evaluateWakeReasons` (STEP6-DESIGN §6 keeps them for the CLI wake column).

### 5e — demote `ordered`
Introduce `orderedIDs []string` (or keep `ordered[i].ID`) for the two order-sensitive rebuilds: the
`sessionInfos` build (~:3013-3015) and the Step-4 preserve feed (~:1587). **Never `range infoByID`** for
either (SessionName last-write-wins). `openPoolSessionCountForTemplate` MAY domain-switch to
`range infoByID` (order-independent count, unique IDs — plan-blessed). After 5a–5d, grep `ordered` +
`session.Metadata\[` in session_reconciler.go and record the FINAL raw census in LOCKSTEP-DROP.md: the
only survivors must be (i) the tick-start `infoByID`/topoOrder/Phase-0.5 builds (:1338/:1356-1386/:1412-
1414 — pre-snapshot typed projections, blessed), (ii) `&ordered[i]` handoffs into documented raw-by-design
helpers + `startCandidate`, (iii) the surviving start-coupled mirrors from 5c.

### 6e — extend the CI guard (STRICTLY LAST)
`frontdoor_di_guard_test.go` (`:83` `snapshotInfoOnlyFiles`, `:97` `forbiddenRawSnapshotAccessors`,
`:109` `TestSnapshotInfoOnlyFilesStayOnInfoAccessors`). Today it forbids the raw snapshot ACCESSORS
(`.Open()`/`.FindByID(`/…) in listed files. Extend it to ALSO forbid raw `session.Metadata[` on the
reconciler decision path:
- Needle: `"session.Metadata["` — matches `session.Metadata[` and `target.session.Metadata[` but NOT the
  work-bead reads (`item.bead.Metadata[`, `b.Metadata[`, `bead.Metadata[` — grep-verify) NOR helper
  `session beads.Bead` params in other files.
- Add `session_reconciler.go`, `compute_awake_bridge.go` (raw-free after 5c), and `session_reconcile.go`
  if raw-free. Do NOT add `session_sleep.go`/`session_wake.go`/`session_lifecycle_parallel.go`/
  `session_bead_snapshot.go` (raw-by-design: sleep-policy helpers, start execution, the bead constructor).
- **CAVEAT:** if a surviving 5c start-coupled mirror keeps a `session.Metadata[` WRITE in
  session_reconciler.go, either relocate that write into a named helper in an unguarded file, OR hold
  session_reconciler.go out of the *write* needle only (a read-only needle) — record whichever in
  LOCKSTEP-DROP.md. Do NOT silently widen exceptions.
- Verify with a revert-canary (add a raw read locally, confirm the guard fails, remove).

## Definition of done (the whole finish)

`grep -n 'session\.Metadata\[' cmd/gc/session_reconciler.go cmd/gc/compute_awake_bridge.go` returns only
the documented raw-by-design census in LOCKSTEP-DROP.md (start-coupled survivor mirrors, if any);
`grep -n '\*beads\.Bead\|target\.session\.Metadata' cmd/gc/session_reconciler.go` shows raw beads flowing
ONLY into the census'd raw-by-design helpers, `startCandidate`, and the tick-start build; the extended
`TestSnapshotInfoOnlyFilesStayOnInfoAccessors` enforces it; the comprehensive reconciler suite + the three
injected-error fail-safe tests (`session_reconciler_test.go` ProgressStallDoesNotRecycle /
attachment_check_error_fails_safe + `session_reconciler_progress_test.go`) + `make test` + `go vet ./...`
are green; and a final fable review over the 5c diff reports zero confirmed byte-identity defects.

**Deferred / raw-by-design forever** (do NOT convert): `startCandidate`/`executePlannedStartsTraced`/
`buildPreparedStart` (start execution, consumer #7); `resolveSessionSleepPolicy`/`configWakeSuppressed`/
`persistSleepPolicyMetadata` (whole-bead + runtime/config); `sessionHasOpenAssignedWorkForReachableStore`/
`collectSessionAssignedWork` (read-only store queries); `pruneAgentHomeWorktreeIfSafe`; the
`evaluateWakeReasons`/`computeWakeEvaluations` family + raw classifier oracle siblings; the CRP one-tick
deferral (#2345-class, separate).

## Discipline (unchanged — this is the byte-identity bar)

- **Byte-identity is the gate.** Convert/delete each thing, VERIFY, THEN move on. A wrong conversion flips
  an awake/sleep/drain/close decision and fails the reconciler suite — EXCEPT start-execution reads (5c),
  which the suite may not cover, so 5c leans on the per-key census + the fable review.
- **Per non-trivial commit: build · `go vet` · `golangci-lint run ./cmd/gc/... ./internal/session/...` (0) ·
  `gofmt -l` (empty) · the reconciler subset** (`go test ./cmd/gc/ -timeout 25m -run
  'Reconcile|Awake|Wake|Sleep|Pool|DrainAck|Recycle|Zombie|Heal|Drift|Churn|Stability|RateLimit|Named|Restart|Progress|Rollback|PendingCreate|MinFloor|Idle|MaxAge|Detach|Rebaseline|Relaunch|Quarantine|Circuit|Lifecycle|Session' -count=1`, ~210s;
  the full cmd/gc package times out at 600s — use the subset). `git checkout go.sum` after (isolated-cache runs dirty it).
- **A fable adversarial review workflow before EACH commit** (owner prefers fable, not opus). Reuse the
  Step-5a script shape at `.claude/.../workflows/scripts/step5a-forward-reads-review-*.js` (find/verify
  pipeline, `model:'fable'`, `effort:'high'`; NO backticks inside the template-literal prompts — they break
  the JS parser; build strings with array `.join('\n')`). The 5a review caught a real MEDIUM — do not skip it,
  especially for 5c.
- Commit AND `git push --no-verify` each step. Trailer:
  `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. #3839 stays DRAFT.
- Never `tmux kill-server` / `go clean -cache` (`-testcache` ok). gascity Dolt is LOCAL-ONLY (no `bd dolt push`).
- Update LOCKSTEP-DROP.md progress + memory (`infra-beads-decoupling-plan.md`, CONT-31…) as each lands.
