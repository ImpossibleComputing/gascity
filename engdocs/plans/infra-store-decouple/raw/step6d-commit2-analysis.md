# 6d Commit 2 — drain-ack finalize closes: analysis (scratch)

HEAD before: a66a888f9. Sites re-grepped after Commit 1 (+~11 line shift).

## finalizeDrainAckStoppedSession (def @314) has THREE exit shapes, not one

The prompt's `ApplyPatch(closeBatch).MarkClosed()` only covers Path A. The
function actually exits via:

- **Path A — close-success (@367-384, `return`@383):** `session.Status="closed"`
  + mirrors `ClosePatch(now,"drained")` onto `session.Metadata`. Snapshot fold =
  `info.ApplyPatch(closePatch).MarkClosed()`. ClosePatch keys: state (Info),
  close_reason/closed_at/synced_at (NON-Info → ApplyPatch ignores → byte-ident).
- **Path B — NDI witness (@385-397, `return`@396):** store already shows closed;
  `session.Status=latest.Status`; `session.Metadata=latest.Metadata` (WHOLESALE
  swap, not a patch). Snapshot fold = full reproject `InfoFromPersistedBead(*session)`
  (post-swap) — byte-identical to the old refresh, the one residual raw read (rare
  path, reworked with the lockstep drop later).
- **Path C — drain-ack, NOT a close (@407-441):** `AcknowledgeDrainPatch` /
  `CompleteDrainPatch` (+ `restart_requested=""` clear @419), applied to store then
  mirrored. Snapshot fold = `info.ApplyPatch(batch)` (NO MarkClosed). Error sub-path
  (@421-423 store write fails, no mirror) → no mutation → zero result.
- Early return (@328 nil/empty) → no mutation → zero result.

Reachable at all 3 sites: site 3's `closeIfUnassigned=isPoolManagedSessionBead`
can be false → Path C definitely reachable. So all shapes must be handled.

## Mechanism: `drainAckFinalizeResult{batch, closed, witnessInfo}` + `applyTo`

Return-only (no new param) → the 4 test callers + the `finalizeDrainAckStopPendingSessions`
@512 call stay as statement-calls (Go discards the return) — zero ripple.
`applyTo(info)`: witnessInfo wins outright; else `info.ApplyPatch(batch)` then
`MarkClosed()` if closed. Zero value = no-op (unchanged info) for async/early/error.

## Byte-identity rests on pre-call snapshot COHERENCE (infoByID[id] == InfoFromPersistedBead(*session))

ApplyPatch/MarkClosed oracles prove the fold == reprojection GIVEN coherence.
Verified per site (no un-refreshed `*session` mutation reaches the finalize call):

- **Site 1 (@1448, via `reconcileDrainAckStopPending`, close via Path A):**
  top-of-loop `info:=infoByID[id]`@1435 is coherent (comment @1428-1434); the
  finalize branch observes runtime only (read-only) before finalize. Async branch
  (`queueDrainAckAsyncStop`, takes ID not `*session`) mutates nothing → zero result
  → applyTo no-op == old refresh of unmutated bead.
- **Site 2 (@1737):** post-heal refresh @1636 syncs infoByID; path to 1737 requires
  `!providerAlive` (skips the @1724 markDrainAck under `if providerAlive`); the
  cancel* calls take `*session` by value. No mutation → coherent.
- **Site 3 (@2054):** post-zombie refresh @1903 (comment @1893-1902 asserts the
  fast-path stays byte-identical); path to 2054 requires `!alive` (skips @2041
  markDrainAck) and is the fall-through past config-drift branches that all
  `continue`; every call takes `*session` by value or mutates sp/dt/store-copy. No
  mutation → coherent.

`reconcileDrainAckStopPending` (only caller = site 1) returns `(bool, result)`.

## Tests — one teeth-verified per-site read-after-write test per call site (§8 discipline)

Single-template deterministic harness (topoOrder returns slice order). Companion at
slice idx 0 closes mid-tick via the target site → min-floor open count drops to
1==floor → stalled worker (idx 1) exempt. Each teeth-verified: disable ONLY that
site's fold → ONLY that site's test fails (others pass), proving both the guard AND
the routing.

- **Site 1** (`…MidTickCloseDrainAck`): companion `state="draining"`,
  `state_reason="drain-ack-stop-pending"` (→ isDrainAckStopPendingInfo) → site-1
  Path A close. dops=nil (site 1 needs no dops).
- **Site 2** (`…MidTickCloseDrainAckOrphan`): companion asleep, NOT desired,
  `dops.setDrainAck`, not started (→ !providerAlive) → post-heal default drain-ack →
  Path A close. Needs the new `reconcileAtPathWithDrainOps` harness helper.
- **Site 3** (`…MidTickCloseDrainAckReconciler`): companion `state="active"`,
  `pool_managed="true"`, DESIRED (skips the `if !desired` block that owns site 2 —
  1542..1925), `dops.setDrainAck`, not started (→ !alive), no assigned work →
  post-zombie reconciler drain-ack → Path A close (closeIfUnassigned=isPoolManaged).

## Adversarial review (6-lens fable panel + adversarial verify, workflow wf_3d1f12c0)

0 byte-identity / coherence defects. All lenses independently confirmed: applyTo is
byte-identical for every exit path (Path A ApplyPatch+MarkClosed via oracle; Path B
witness reproject by construction; Path C drain-ack batch incl. restart_requested
clear; async/error/no-op), and infoByID[id] is coherent at all three sites (every
intervening helper takes the bead by value or `continue`s). The one CONFIRMED finding
— sites 2/3 lacked per-site teeth tests — is now CLOSED (both added + teeth-verified).
A "flaky test" finding was REFUTED (traced to the reviewers' own concurrent
fold-disable experiments in the shared worktree; ~950 runs green).
