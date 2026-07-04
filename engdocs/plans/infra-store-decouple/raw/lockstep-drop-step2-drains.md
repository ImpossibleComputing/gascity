# Lockstep drop — Step 2: advanceSessionDrains off the raw bead (retire beadByID/sessionLookup)

## Goal
The reconciler builds `beadByID` (:1406) + `sessionLookup` (:3335) SOLELY to feed
`advanceSessionDrainsWithSessionsTraced` (:3338). Retire both by feeding drain processing
from the typed `infoByID` snapshot instead of raw `*beads.Bead`.

## advanceSessionDrainsWithSessionsTraced raw-bead consumers (session_wake.go :428-618)
Decision reads already project `info := sessions.InfoFromPersistedBead(*session)` (:464). The
remaining raw uses, ALL Info-convertible (Info has ID, Generation, SessionNameMetadata, WakeMode,
InstanceToken, Template — all RAW verbatim per info_store.go):
- `completeDrain(session, sessFront, ds, clk)` (:492, :606) — reads wake_mode+ID, writes store,
  MIRRORS bead. → `completeDrain(info, ...)` store-only. [STEP 2a]
- `cancelSessionDrainForPending(*session, sp, dt)` (:507), `cancelSessionDrainForAssignedWork(*session)` (:519)
  — read id/generation/session_name only (no mutation of bead). → Info siblings. [STEP 2b]
- `verifiedStop(*session, store, sp, cfg)` (:583) — reads session_name/instance_token/id.
  → `verifiedStopInfo(info, ...)`. Only prod caller is :583; 3 test callers. [STEP 2b]
- `workerSessionTargetRunningWithConfig(..., session.ID)` (:486,:601), `wakeEvals[session.ID]`
  (:504,:515) → info.ID. [STEP 2b]
- `sessionLookup(id)` → replace param `sessionLookup func(id) *beads.Bead` with
  `infoLookup func(id) (sessions.Info, bool)` (keeps the closure indirection the 12 tests use). [STEP 2b]

## sessionBeads param
`sessionBeads []beads.Bead` stays: it feeds `computeWakeEvaluations` (:443) ONLY when wakeEvals==nil.
In the reconciler call wakeEvals is always non-nil (DEAD there, §7 6c-audit) but non-prod callers pass
nil, so the param can't be dropped. Non-reconciler (tests) still pass beads for that fallback.

## completeDrain (Step 2a) — byte-identity
- info.WakeMode == b.Metadata["wake_mode"] (info_store.go:115, RAW). info.ID == b.ID.
- All 4 completeDrain tests assert on store.Get(b.ID), NOT the local bead → mirror is dead.
- Prod mirror lands on ordered[i] AFTER the awake scan (advanceSessionDrains :3338 > awake scan :3040-3198)
  and is always followed by dt.remove+continue → no in-tick consumer.
- nil-store: OLD skipped ApplyPatch then mirrored (unobservable — callers continue); NEW no-ops. Safe.
- Aliases: session_wake.go = `sessions`; session_wake_test.go = `sessionpkg`.

## Call sites
completeDrain: prod :492/:606 (info in scope); tests :1360/:1391/:1425/:1453 (`&b` -> `sessionpkg.InfoFromPersistedBead(b)`).
advanceSessionDrains family test sites (2b): session_wake_test.go x12, session_sleep_test.go:1272,
trace_integration_test.go:382 — build infoLookup from their beads.

## Step 2b implementation + fable review (wf_381c5866)
Landed: advanceSessionDrainsWithSessionsTraced takes `infoLookup func(id)(Info,bool)`;
loop reads Info only (info.ID/Generation/SessionNameMetadata/WakeMode/InstanceToken);
cancelSessionDrainIfInfo core + Pending/AssignedWork Info siblings; verifiedStop→Info;
infoLookupFromBeadLookup adapts the bead-based wrappers (10/12 test sites untouched);
reconciler passes infoByID-backed infoLookup, DROPS beadByID + sessionLookup.

Review verdict: 2 lenses CLEAN (typed-field-fidelity, callsite-completeness); F2/F4 CONFIRMED
byte-identical (presence 1:1, adapter correct); 1 finding REFUTED (start-candidate preWakeCommit
mutates a REBOUND deep-copy via prepareStartCandidateForCity store.Get, not ordered[i] — so OLD
and NEW both read stale; no divergence).

**1 CONFIRMED (LOW→FIXED): the buildPreparedStart instance_token residue.** recoverRunningPendingCreate's
buildPreparedStart mints instance_token onto ordered[i]+store when empty, OUTSIDE CommitStartedPatch,
so infoByID kept "". Pre-2b this was snapshot-inert (no Info reader). 2b's verifiedStop(info.InstanceToken)
made it a live same-tick Info reader → in a compound-rare state (pending_create_claim + empty tick-start
token + live runtime + uncancelable drain past deadline) NEW read "" and skipped the token check, killing
a runtime OLD spared. FIX: recoverRunningPendingCreate now threads the persisted mint into its returned
fold batch on every return path (pendingCreateInstanceTokenFold on the abort paths; metadata["instance_token"]=tok
on success — store write unchanged, augments only the fold). Teeth test
TestRecoverRunningPendingCreate_ReturnsMintedInstanceTokenForSnapshotFold (fails without the fold).
The OTHER residue keys (session_key/started_config_hash/continuation_reset_pending stale-resume clears)
stay unthreaded — still no Info reader; thread when Step 3 moves the awake scan onto Info.
