package main

import (
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	sessionpkg "github.com/gastownhall/gascity/internal/session"
)

// Read-after-write harness (front-door migration Step 6d).
//
// The 6d cutover replaces the reconciler's raw-bead snapshot refresh
// (refreshSessionInfo re-projecting *beadByID[id]) with write-returns-Info
// (infoByID[id] = infoByID[id].ApplyPatch(batch) / markClosed), and then drops
// the session.Metadata[k]=v lockstep and the raw working set. The byte-identical
// write oracle (a recording fake store) is BLIND to same-tick stale reads
// (RECONCILER-FRONT-DOOR-SPEC §2 governing principle): a converted write that
// fails to refresh the infoByID snapshot is invisible until a LATER same-tick
// read consumes the stale value and flips a decision. So every lockstep drop
// needs a multi-session / read-after-write same-tick test — these.
//
// The harness exploits a determinism guarantee to place a write before a read in
// one tick: topoOrder returns a single-template working set in slice order
// (session_reconcile.go:1289 — empty deps returns `sessions` unchanged, and
// same-template sessions keep input order otherwise). So when every seeded
// session shares one template, a session earlier in the []beads.Bead slice is
// visited (and its mutation refreshed onto the snapshot) before a later
// session whose decision reads that mutation off the snapshot. Each test asserts
// an OBSERVABLE outcome (a recycle / restart_requested / running state) that
// flips iff the earlier write reached the later read through the snapshot, so it
// fails loudly if a 6d conversion leaves the snapshot stale.

// TestReconcileSessionBeads_MinFloorCountReflectsMidTickClose guards the
// cross-session min-floor read: the progress-stall recycler exempts a stalled
// pool worker when its pool is at its configured floor, and it measures the pool
// via openPoolSessionCountForTemplate (session_reconciler.go ~2090), which reads
// !Info.Closed off the infoByID snapshot. A pool worker CLOSED earlier in the
// same tick must drop that open count so a stalled worker visited later is
// exempt.
//
// Scenario: floor 1, max 2. A stale failed-create companion (no live runtime, no
// assigned work) is first in the slice, so the reconciler closes it and refreshes
// its snapshot Info BEFORE the stalled worker's min-floor decision runs. With the
// companion closed the pool is at floor (open == 1 == min), so the stalled worker
// must NOT be recycled. If the close's snapshot refresh regresses (the 6d hazard),
// the count stays at 2 > floor and the stalled worker is wrongly recycled — the
// assertions below catch that.
//
// This is the mid-tick-close integration test Step 4D deferred as "impractical —
// topoOrder hides processing order"; single-template ordering makes it
// deterministic.
func TestReconcileSessionBeads_MinFloorCountReflectsMidTickClose(t *testing.T) {
	env, session, sessionName := newProgressStallTestEnv(t)
	env.cfg.Agents[0].MinActiveSessions = restartRequestTestIntPtr(1)
	env.cfg.Agents[0].MaxActiveSessions = restartRequestTestIntPtr(2)

	// A second worker, open at tick start (lifting open == 2 > floor 1), but a
	// stale failed-create with no live runtime and no assigned work, so the
	// reconciler closes it this tick. Placed FIRST so its close lands on the
	// snapshot before the stalled worker's min-floor read.
	closing := env.createSessionBead("worker-closing-companion")
	env.setSessionMetadata(&closing, map[string]string{"state": string(sessionpkg.StateFailedCreate)})

	env.reconcileAtPath(t.TempDir(), []beads.Bead{closing, session})

	// Precondition: the companion actually closed this tick. If it did not, the
	// count never dropped and the rest of the scenario proves nothing.
	gotClosing, err := env.store.Get(closing.ID)
	if err != nil {
		t.Fatalf("store.Get(%s): %v", closing.ID, err)
	}
	if gotClosing.Status != "closed" {
		t.Fatalf("companion status = %q, want closed — a failed-create worker with no live runtime must close mid-tick for this scenario to exercise the read-after-write", gotClosing.Status)
	}

	// The read-after-write assertion: after the same-tick close, open == 1 == floor,
	// so the stalled worker is a min-floor idle worker and must be left running.
	if !env.sp.IsRunning(sessionName) {
		t.Fatalf("session %q was recycled; after the same-tick companion close the pool is at floor (open == 1 == min), so the stalled worker must be min-floor exempt — the min-floor count did not reflect the same-tick close (stale snapshot)", sessionName)
	}
	got, err := env.store.Get(session.ID)
	if err != nil {
		t.Fatalf("store.Get(%s): %v", session.ID, err)
	}
	if got.Metadata["restart_requested"] != "" {
		t.Fatalf("restart_requested = %q, want empty — the stalled worker must be min-floor exempt after the same-tick close", got.Metadata["restart_requested"])
	}
	if strings.Contains(env.stderr.String(), "progress-stalled") {
		t.Fatalf("stderr = %q, want no progress-stalled diagnostic for the exempt floor worker", env.stderr.String())
	}
}

// TestReconcileSessionBeads_MinFloorCountReflectsMidTickCloseDrainAck is the
// sibling of the store-only-close tests above for the DRAIN-ACK finalize close
// path (reconcileDrainAckStopPending → finalizeDrainAckStoppedSession, site 1
// ~1510). Unlike the failed-create/orphan store-only closes (MarkClosed only),
// finalizeDrainAckStoppedSession's unassigned-close path mirrors a ClosePatch
// onto the raw bead, so its write-returns-Info snapshot fold is
// ApplyPatch(closePatch)+MarkClosed. This test guards that third close site the
// same way — a companion closed earlier in the tick must drop the pool's open
// count so a stalled worker visited later is min-floor exempt.
//
// Scenario: floor 1, max 2. A drain-ack-stop-pending companion (open, parked in
// state=draining/reason=drain-ack-stop-pending, not in the desired set, no live
// runtime) is first in the slice, so reconcileDrainAckStopPending finalizes and
// closes it via the unassigned-close path and folds that close onto its snapshot
// Info BEFORE the stalled worker's min-floor decision runs. With the companion
// closed the pool is at floor (open == 1 == min), so the stalled worker must NOT
// be recycled. If the drain-ack close's snapshot fold regresses (the 6d hazard),
// the count stays at 2 > floor and the stalled worker is wrongly recycled.
func TestReconcileSessionBeads_MinFloorCountReflectsMidTickCloseDrainAck(t *testing.T) {
	env, session, sessionName := newProgressStallTestEnv(t)
	env.cfg.Agents[0].MinActiveSessions = restartRequestTestIntPtr(1)
	env.cfg.Agents[0].MaxActiveSessions = restartRequestTestIntPtr(2)

	// A second worker, open at tick start (lifting open == 2 > floor 1), parked in
	// drain-ack stop-pending state (state=draining, state_reason=drain-ack-stop-pending
	// → isDrainAckStopPendingInfo true) with no live runtime and not in the desired
	// set. reconcileDrainAckStopPending observes it not running and calls
	// finalizeDrainAckStoppedSession with closeIfUnassigned=true (!desired), which
	// closes it via the unassigned-close path (Path A, mirroring a ClosePatch), and
	// the site folds that close onto the snapshot. Placed FIRST so its close lands
	// on the snapshot before the stalled worker's min-floor read.
	draining := env.createSessionBead("worker-drainack-companion")
	env.setSessionMetadata(&draining, map[string]string{
		"state":        string(sessionpkg.StateDraining),
		"state_reason": sessionpkg.DrainAckStopPendingReason,
	})

	env.reconcileAtPath(t.TempDir(), []beads.Bead{draining, session})

	// Precondition: the companion actually closed this tick via the drain-ack
	// finalize path. If it did not, the count never dropped and the rest of the
	// scenario proves nothing (and the teeth-check against the site-1 fold would be
	// vacuous).
	gotDraining, err := env.store.Get(draining.ID)
	if err != nil {
		t.Fatalf("store.Get(%s): %v", draining.ID, err)
	}
	if gotDraining.Status != "closed" {
		t.Fatalf("drain-ack companion status = %q, want closed — a drain-ack-stop-pending worker with no live runtime and no assigned work must close mid-tick via finalizeDrainAckStoppedSession for this scenario to exercise the read-after-write", gotDraining.Status)
	}

	// The read-after-write assertion: after the same-tick drain-ack close, open ==
	// 1 == floor, so the stalled worker is a min-floor idle worker and must be left
	// running.
	if !env.sp.IsRunning(sessionName) {
		t.Fatalf("session %q was recycled; after the same-tick drain-ack close the pool is at floor (open == 1 == min), so the stalled worker must be min-floor exempt — the min-floor count did not reflect the same-tick close (stale snapshot)", sessionName)
	}
	got, err := env.store.Get(session.ID)
	if err != nil {
		t.Fatalf("store.Get(%s): %v", session.ID, err)
	}
	if got.Metadata["restart_requested"] != "" {
		t.Fatalf("restart_requested = %q, want empty — the stalled worker must be min-floor exempt after the same-tick drain-ack close", got.Metadata["restart_requested"])
	}
	if strings.Contains(env.stderr.String(), "progress-stalled") {
		t.Fatalf("stderr = %q, want no progress-stalled diagnostic for the exempt floor worker", env.stderr.String())
	}
}

// TestReconcileSessionBeads_MinFloorCountReflectsMidTickCloseDrainAckOrphan is
// the per-site guard for the SECOND drain-ack finalize call site
// (session_reconciler.go ~1802, the post-heal "default" orphan drain-ack close),
// distinct from the site-1 guard above (the drain-ack-stop-pending fast path).
// This site is reached only when a controller drain-ack is set (dops.isDrainAcked)
// for an undesired, not-running worker, which the site-1 fast path does not
// intercept (its state is asleep, not draining/drain-ack-stop-pending). Its fold
// (result.applyTo, ApplyPatch(ClosePatch)+MarkClosed on Path A) must drop the
// closed companion from the pool open count exactly like site 1; without a
// per-site read-after-write test a coherence regression here ships silently
// (STEP6-DESIGN §8: the write oracle is blind to same-tick stale reads).
//
// Scenario: floor 1, max 2. A drain-acked orphan companion (asleep, not desired,
// no live runtime, controller drain-ack set) is first in the slice, so the
// reconciler heals it, takes the post-heal default drain-ack branch, and closes
// it via finalizeDrainAckStoppedSession (closeIfUnassigned=true) BEFORE the
// stalled worker's min-floor decision. With it closed the pool is at floor
// (open == 1 == min), so the stalled worker must NOT be recycled.
func TestReconcileSessionBeads_MinFloorCountReflectsMidTickCloseDrainAckOrphan(t *testing.T) {
	env, session, sessionName := newProgressStallTestEnv(t)
	env.cfg.Agents[0].MinActiveSessions = restartRequestTestIntPtr(1)
	env.cfg.Agents[0].MaxActiveSessions = restartRequestTestIntPtr(2)

	// A second worker, open at tick start (lifting open == 2 > floor 1). Asleep
	// (default state) so the site-1 drain-ack-stop-pending fast path does NOT
	// intercept it; not in the desired set and never started in the fake provider
	// (so !providerAlive), with a controller drain-ack set so the post-heal default
	// branch closes it via finalizeDrainAckStoppedSession this tick. Placed FIRST so
	// its close lands on the snapshot before the stalled worker's min-floor read.
	orphan := env.createSessionBead("worker-drainack-orphan-companion")
	dops := newFakeDrainOps()
	if err := dops.setDrainAck("worker-drainack-orphan-companion"); err != nil {
		t.Fatalf("setDrainAck: %v", err)
	}

	env.reconcileAtPathWithDrainOps(t.TempDir(), []beads.Bead{orphan, session}, dops)

	// Precondition: the companion actually closed this tick via the orphan
	// drain-ack finalize path (site 2). If it did not, the count never dropped and
	// the teeth-check against the ~1802 fold would be vacuous.
	gotOrphan, err := env.store.Get(orphan.ID)
	if err != nil {
		t.Fatalf("store.Get(%s): %v", orphan.ID, err)
	}
	if gotOrphan.Status != "closed" {
		t.Fatalf("drain-ack orphan status = %q, want closed — an undesired, not-running, drain-acked worker must close mid-tick via the post-heal drain-ack finalize (site 2) for this scenario to exercise the read-after-write", gotOrphan.Status)
	}

	// The read-after-write assertion: after the same-tick close, open == 1 ==
	// floor, so the stalled worker is a min-floor idle worker and must be left
	// running.
	if !env.sp.IsRunning(sessionName) {
		t.Fatalf("session %q was recycled; after the same-tick drain-ack orphan close the pool is at floor (open == 1 == min), so the stalled worker must be min-floor exempt — the min-floor count did not reflect the same-tick close (stale snapshot at site 2)", sessionName)
	}
	got, err := env.store.Get(session.ID)
	if err != nil {
		t.Fatalf("store.Get(%s): %v", session.ID, err)
	}
	if got.Metadata["restart_requested"] != "" {
		t.Fatalf("restart_requested = %q, want empty — the stalled worker must be min-floor exempt after the same-tick site-2 close", got.Metadata["restart_requested"])
	}
	if strings.Contains(env.stderr.String(), "progress-stalled") {
		t.Fatalf("stderr = %q, want no progress-stalled diagnostic for the exempt floor worker", env.stderr.String())
	}
}

// TestReconcileSessionBeads_MinFloorCountReflectsMidTickCloseDrainAckReconciler
// is the per-site guard for the THIRD drain-ack finalize call site
// (session_reconciler.go ~2123, the post-zombie reconciler-owned drain-ack
// close). Unlike site 2 (undesired orphan, inside the `if !desired` block), this
// site is reached only by a DESIRED session that falls through to the common
// post-zombie drain-ack block; its close gate is
// closeIfUnassigned=isPoolManagedSessionBead. Its fold is the same
// result.applyTo used at sites 1 and 2, so this test guards the site-3 call site
// against a coherence regression the write oracle cannot see.
//
// Scenario: floor 1, max 2. A desired, pool-managed, drain-acked companion with
// no live runtime and no assigned work is first in the slice, so the reconciler
// closes it via finalizeDrainAckStoppedSession at site 3 BEFORE the stalled
// worker's min-floor decision. With it closed the pool is at floor (open == 1 ==
// min), so the stalled worker must NOT be recycled.
func TestReconcileSessionBeads_MinFloorCountReflectsMidTickCloseDrainAckReconciler(t *testing.T) {
	env, session, sessionName := newProgressStallTestEnv(t)
	env.cfg.Agents[0].MinActiveSessions = restartRequestTestIntPtr(1)
	env.cfg.Agents[0].MaxActiveSessions = restartRequestTestIntPtr(2)

	// A second worker, open at tick start (lifting open == 2 > floor 1). Desired
	// (so it skips the `if !desired` block that owns site 2 and falls through to
	// the common post-zombie drain-ack block), pool-managed (so
	// closeIfUnassigned=isPoolManagedSessionBead is true), never started in the
	// fake provider (so !alive), with a controller drain-ack set and no assigned
	// work, so finalizeDrainAckStoppedSession closes it via site 3 this tick.
	// Placed FIRST so its close lands on the snapshot before the stalled worker's
	// min-floor read.
	const companionName = "worker-drainack-desired-companion"
	companion := env.createSessionBead(companionName)
	env.setSessionMetadata(&companion, map[string]string{
		"state":        "active",
		"pool_managed": "true",
	})
	env.desiredState[companionName] = TemplateParams{
		Command:      "true",
		SessionName:  companionName,
		TemplateName: "worker",
		ResolvedProvider: &config.ResolvedProvider{
			Name:          "zai",
			SessionIDFlag: "--session-id",
		},
	}
	dops := newFakeDrainOps()
	if err := dops.setDrainAck(companionName); err != nil {
		t.Fatalf("setDrainAck: %v", err)
	}

	env.reconcileAtPathWithDrainOps(t.TempDir(), []beads.Bead{companion, session}, dops)

	// Precondition: the companion actually closed this tick via the reconciler
	// drain-ack finalize path (site 3). If it did not, the count never dropped and
	// the teeth-check against the ~2123 fold would be vacuous.
	gotCompanion, err := env.store.Get(companion.ID)
	if err != nil {
		t.Fatalf("store.Get(%s): %v", companion.ID, err)
	}
	if gotCompanion.Status != "closed" {
		t.Fatalf("desired drain-ack companion status = %q, want closed — a desired, pool-managed, not-running, drain-acked worker with no assigned work must close mid-tick via the reconciler drain-ack finalize (site 3) for this scenario to exercise the read-after-write", gotCompanion.Status)
	}

	// The read-after-write assertion: after the same-tick close, open == 1 ==
	// floor, so the stalled worker is a min-floor idle worker and must be left
	// running.
	if !env.sp.IsRunning(sessionName) {
		t.Fatalf("session %q was recycled; after the same-tick site-3 close the pool is at floor (open == 1 == min), so the stalled worker must be min-floor exempt — the min-floor count did not reflect the same-tick close (stale snapshot at site 3)", sessionName)
	}
	got, err := env.store.Get(session.ID)
	if err != nil {
		t.Fatalf("store.Get(%s): %v", session.ID, err)
	}
	if got.Metadata["restart_requested"] != "" {
		t.Fatalf("restart_requested = %q, want empty — the stalled worker must be min-floor exempt after the same-tick site-3 close", got.Metadata["restart_requested"])
	}
	if strings.Contains(env.stderr.String(), "progress-stalled") {
		t.Fatalf("stderr = %q, want no progress-stalled diagnostic for the exempt floor worker", env.stderr.String())
	}
}

// TestReconcileSessionBeads_MinFloorCountReflectsMidTickCloseOrphan is the
// sibling of the failed-create test above for the ORPHAN close path
// (session_reconciler.go ~1834): a not-desired, not-running pool worker that the
// reconciler closes via closeSessionBeadIfReachableStoreUnassigned after heal.
// Where the failed-create close runs pre-heal, the orphan close runs in the
// post-heal switch default; its snapshot refresh must be byte-identical to the
// heal-refreshed pre-close Info folded with MarkClosed. This test guards that
// second store-only close site the same way — a companion closed earlier in the
// tick must drop the pool's open count so a stalled worker visited later is
// min-floor exempt.
//
// Scenario: floor 1, max 2. An orphan companion (open, asleep, not in the
// desired set, no live runtime) is first in the slice, so the reconciler closes
// it via the orphan path and refreshes its snapshot Info BEFORE the stalled
// worker's min-floor decision runs. With the companion closed the pool is at
// floor (open == 1 == min), so the stalled worker must NOT be recycled. If the
// orphan close's snapshot refresh regresses (the 6d hazard), the count stays at
// 2 > floor and the stalled worker is wrongly recycled.
func TestReconcileSessionBeads_MinFloorCountReflectsMidTickCloseOrphan(t *testing.T) {
	env, session, sessionName := newProgressStallTestEnv(t)
	env.cfg.Agents[0].MinActiveSessions = restartRequestTestIntPtr(1)
	env.cfg.Agents[0].MaxActiveSessions = restartRequestTestIntPtr(2)

	// A second worker, open at tick start (lifting open == 2 > floor 1). It is an
	// orphan: never added to desiredState and never started in the fake provider,
	// with the default asleep state (not failed-create), so the reconciler heals
	// it, then closes it via the not-desired/not-running orphan path this tick.
	// Placed FIRST so its close lands on the snapshot before the stalled worker's
	// min-floor read.
	orphan := env.createSessionBead("worker-orphan-companion")

	env.reconcileAtPath(t.TempDir(), []beads.Bead{orphan, session})

	// Precondition: the orphan actually closed this tick via the orphan path. If
	// it did not, the count never dropped and the rest of the scenario proves
	// nothing (and the teeth-check against the ~1834 site would be vacuous).
	gotOrphan, err := env.store.Get(orphan.ID)
	if err != nil {
		t.Fatalf("store.Get(%s): %v", orphan.ID, err)
	}
	if gotOrphan.Status != "closed" {
		t.Fatalf("orphan status = %q, want closed — a not-desired asleep worker with no live runtime must close mid-tick via the orphan path for this scenario to exercise the read-after-write", gotOrphan.Status)
	}

	// The read-after-write assertion: after the same-tick orphan close, open == 1
	// == floor, so the stalled worker is a min-floor idle worker and must be left
	// running.
	if !env.sp.IsRunning(sessionName) {
		t.Fatalf("session %q was recycled; after the same-tick orphan close the pool is at floor (open == 1 == min), so the stalled worker must be min-floor exempt — the min-floor count did not reflect the same-tick close (stale snapshot)", sessionName)
	}
	got, err := env.store.Get(session.ID)
	if err != nil {
		t.Fatalf("store.Get(%s): %v", session.ID, err)
	}
	if got.Metadata["restart_requested"] != "" {
		t.Fatalf("restart_requested = %q, want empty — the stalled worker must be min-floor exempt after the same-tick orphan close", got.Metadata["restart_requested"])
	}
	if strings.Contains(env.stderr.String(), "progress-stalled") {
		t.Fatalf("stderr = %q, want no progress-stalled diagnostic for the exempt floor worker", env.stderr.String())
	}
}
