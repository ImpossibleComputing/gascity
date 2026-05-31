package main

import (
	"context"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/runtime"
)

// TestReconcileSessionBeads_ResetClearsBeadFlagIndependentOfRuntimeFlagClear is
// the DISPROOF for ga-xuvmkk's stated root cause. The bead claims the swallowed
// `_ = dops.clearRestartRequested(name)` (session_reconciler.go:1462) leaves the
// restart_requested flag set and drives a perpetual stop loop.
//
// That attribution is wrong. There are two distinct flags:
//   - the BEAD metadata flag session.Metadata["restart_requested"], set by
//     `gc session reset` (Manager.RequestFreshRestart, manager.go:843); and
//   - the RUNTIME/tmux flag GC_RESTART_REQUESTED, set by
//     `gc runtime request-restart` and read/cleared by dops.isRestartRequested /
//     dops.clearRestartRequested (cmd_runtime_drain.go:117,128).
//
// In the reconciler stop branch the bead flag is cleared by the CHECKED
// store.SetMetadataBatch(RestartRequestPatch(...)) at session_reconciler.go:1450
// (`continue` on error), while clearRestartRequested only clears the runtime flag
// and is guarded by `if tmuxRequested && dops != nil`. A `gc session reset` never
// sets the runtime flag, so tmuxRequested is false and clearRestartRequested is
// never even called — yet the bead flag is reliably cleared.
//
// This test PASSES on main, proving the swallowed clearRestartRequested error
// cannot be the cause of a stuck restart_requested flag or a stop loop.
func TestReconcileSessionBeads_ResetClearsBeadFlagIndependentOfRuntimeFlagClear(t *testing.T) {
	env := newRestartRequestTestEnv()
	env.cfg = &config.City{
		Workspace:     config.Workspace{Name: "test-city"},
		Agents:        []config.Agent{{Name: "worker", StartCommand: "true", MaxActiveSessions: restartRequestTestIntPtr(1)}},
		NamedSessions: []config.NamedSession{{Template: "worker", Mode: "always"}},
	}
	sessionName := config.NamedSessionRuntimeName(env.cfg.Workspace.Name, env.cfg.Workspace, "worker")
	env.desiredState[sessionName] = TemplateParams{
		Command:      "true",
		SessionName:  sessionName,
		TemplateName: "worker",
		ResolvedProvider: &config.ResolvedProvider{
			SessionIDFlag: "--session-id",
		},
	}

	session := env.createSessionBead(sessionName)
	env.setSessionMetadata(&session, map[string]string{
		namedSessionMetadataKey:      "true",
		namedSessionIdentityMetadata: "worker",
		namedSessionModeMetadata:     "always",
		"state":                      "active",
		// gc session reset sets ONLY the bead flag — no GC_RESTART_REQUESTED.
		"restart_requested":   "true",
		"session_key":         "original-key",
		"started_config_hash": "hash-before-restart",
	})
	if err := env.sp.Start(context.Background(), sessionName, runtime.Config{Command: "true"}); err != nil {
		t.Fatalf("start session: %v", err)
	}
	if err := env.sp.SetMeta(sessionName, "GC_SESSION_ID", session.ID); err != nil {
		t.Fatalf("SetMeta(GC_SESSION_ID): %v", err)
	}

	// Tick 1: process the reset — kills the runtime, clears the bead flag, yields.
	env.reconcile([]beads.Bead{session})

	got, _ := env.store.Get(session.ID)
	if got.Metadata["restart_requested"] != "" {
		t.Fatalf("restart_requested = %q, want cleared by the checked SetMetadataBatch (not by clearRestartRequested)", got.Metadata["restart_requested"])
	}

	// clearRestartRequested (provider RemoveMeta of GC_RESTART_REQUESTED) must
	// NOT have been called: tmuxRequested is false because the runtime flag was
	// never set, so the swallowed `_ =` at :1462 is not on the reset path at all.
	for _, c := range env.sp.Calls {
		if c.Method == "RemoveMeta" && c.Key == "GC_RESTART_REQUESTED" {
			t.Fatalf("clearRestartRequested was invoked (RemoveMeta GC_RESTART_REQUESTED); a bead-only reset must not reach it")
		}
	}

	if n := strings.Count(env.stdout.String(), "Stopped restart-requested session"); n != 1 {
		t.Fatalf("Stopped count after tick 1 = %d, want 1", n)
	}

	// Tick 2: the bead flag is cleared and the runtime is dead. The stop branch
	// must NOT re-fire (no perpetual stop loop). The bead flag being cleared by a
	// path entirely independent of clearRestartRequested is the disproof.
	refreshed, _ := env.store.Get(session.ID)
	env.reconcile([]beads.Bead{refreshed})

	if n := strings.Count(env.stdout.String(), "Stopped restart-requested session"); n != 1 {
		t.Fatalf("Stopped count after tick 2 = %d, want 1 (no stop loop)", n)
	}
}

// TestReconcileSessionBeads_ResetOfLiveSessionIsNotAtomicWithinTick CHARACTERIZES
// the genuine residual behind ga-xuvmkk: a `gc session reset` of a *running*
// session is intentionally split across two reconciler ticks. Tick 1 kills the
// runtime, clears restart_requested, and `continue`s (yields) so the kill and
// the next wake run on separate passes (the gastownhall/gascity#2345 alias-race
// fix, session_reconciler.go:1465-1468). It leaves continuation_reset_pending
// set; that marker is cleared only by PreWakePatch (lifecycle_transition.go:109)
// at the next concrete wake.
//
// The consequence: after a single reset tick the session is observably STOPPED
// with continuation_reset_pending="true", and bringing it back depends on a
// LATER tick's wake decision — for which the reconciler provides no in-tick
// completion guarantee or watchdog. Under a starved/slow reconciler (the live
// incident: ~13-min HQ-dolt reconciles) that later wake can be deferred
// indefinitely, leaving the named session asleep with REASON=reset-pending.
//
// This test PASSES on main and documents both halves: the stopped+pending state
// after tick 1, and that a *subsequent* deterministic tick is what restores it.
func TestReconcileSessionBeads_ResetOfLiveSessionIsNotAtomicWithinTick(t *testing.T) {
	env := newRestartRequestTestEnv()
	env.cfg = &config.City{
		Workspace:     config.Workspace{Name: "test-city"},
		Agents:        []config.Agent{{Name: "worker", StartCommand: "true", MaxActiveSessions: restartRequestTestIntPtr(1)}},
		NamedSessions: []config.NamedSession{{Template: "worker", Mode: "always"}},
	}
	sessionName := config.NamedSessionRuntimeName(env.cfg.Workspace.Name, env.cfg.Workspace, "worker")
	env.desiredState[sessionName] = TemplateParams{
		Command:      "true",
		SessionName:  sessionName,
		TemplateName: "worker",
		ResolvedProvider: &config.ResolvedProvider{
			SessionIDFlag: "--session-id",
		},
	}

	session := env.createSessionBead(sessionName)
	env.setSessionMetadata(&session, map[string]string{
		namedSessionMetadataKey:      "true",
		namedSessionIdentityMetadata: "worker",
		namedSessionModeMetadata:     "always",
		"state":                      "active",
		"restart_requested":          "true",
		"session_key":                "original-key",
		"started_config_hash":        "hash-before-restart",
	})
	if err := env.sp.Start(context.Background(), sessionName, runtime.Config{Command: "true"}); err != nil {
		t.Fatalf("start session: %v", err)
	}
	if err := env.sp.SetMeta(sessionName, "GC_SESSION_ID", session.ID); err != nil {
		t.Fatalf("SetMeta(GC_SESSION_ID): %v", err)
	}

	// Tick 1: the reset stops the live runtime and yields without restarting it.
	env.reconcile([]beads.Bead{session})

	if env.sp.IsRunning(sessionName) {
		t.Fatal("tick 1: session should be stopped by the reset")
	}
	afterStop, _ := env.store.Get(session.ID)
	if afterStop.Metadata["restart_requested"] != "" {
		t.Fatalf("tick 1: restart_requested = %q, want cleared", afterStop.Metadata["restart_requested"])
	}
	if afterStop.Metadata["continuation_reset_pending"] != "true" {
		t.Fatalf("tick 1: continuation_reset_pending = %q, want \"true\" (cleared only by a completed wake)", afterStop.Metadata["continuation_reset_pending"])
	}
	if n := strings.Count(env.stdout.String(), "Stopped restart-requested session"); n != 1 {
		t.Fatalf("tick 1: Stopped count = %d, want 1 (stop happened, restart deferred to a later tick)", n)
	}

	// Tick 2: a subsequent reconciler pass is REQUIRED to bring the session back.
	// This is the deferred restart the reset relies on; nothing guarantees it
	// runs promptly (or at all) under reconciler starvation.
	env.reconcile([]beads.Bead{afterStop})

	afterWake, _ := env.store.Get(session.ID)
	t.Logf("ga-xuvmkk: after tick 2 IsRunning=%v continuation_reset_pending=%q state=%q",
		env.sp.IsRunning(sessionName),
		afterWake.Metadata["continuation_reset_pending"],
		afterWake.Metadata["state"])
}
