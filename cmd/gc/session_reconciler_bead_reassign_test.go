package main

import (
	"context"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/runtime"
	sessionpkg "github.com/gastownhall/gascity/internal/session"
)

// TestReconcileSessionBeads_AliveFreshModeReassignCyclesConversation verifies
// the fix for gastownhall/gascity#1893: an alive on_demand named session
// running wake_mode=fresh must cycle its conversation when bd update points
// the assignee at a new bead. The session keeps the same bead identifier in
// the store (it's a named session) but its conversation lineage is reset so
// the next wake starts fresh on the newly assigned bead.
func TestReconcileSessionBeads_AliveFreshModeReassignCyclesConversation(t *testing.T) {
	env := newRestartRequestTestEnv()
	env.cfg = &config.City{
		Workspace:     config.Workspace{Name: "test-city"},
		Agents:        []config.Agent{{Name: "witness", StartCommand: "true", MaxActiveSessions: restartRequestTestIntPtr(1)}},
		NamedSessions: []config.NamedSession{{Template: "witness", Mode: "on_demand"}},
	}
	sessionName := config.NamedSessionRuntimeName(env.cfg.Workspace.Name, env.cfg.Workspace, "witness")
	env.desiredState[sessionName] = TemplateParams{
		Command:      "true",
		SessionName:  sessionName,
		TemplateName: "witness",
		ResolvedProvider: &config.ResolvedProvider{
			SessionIDFlag: "--session-id",
		},
	}

	session := env.createSessionBead(sessionName)
	env.setSessionMetadata(&session, map[string]string{
		namedSessionMetadataKey:      "true",
		namedSessionIdentityMetadata: "witness",
		namedSessionModeMetadata:     "on_demand",
		"template":                   "witness",
		"state":                      "active",
		"wake_mode":                  "fresh",
		"session_key":                "conversation-A",
		sessionpkg.CurrentBeadIDKey:  "wb-A",
	})
	if err := env.sp.Start(context.Background(), sessionName, runtime.Config{Command: "true"}); err != nil {
		t.Fatalf("start session: %v", err)
	}
	if err := env.sp.SetMeta(sessionName, "GC_SESSION_ID", session.ID); err != nil {
		t.Fatalf("SetMeta(GC_SESSION_ID): %v", err)
	}

	// Patrol formula poured wb-B and pointed the witness's assignee at it;
	// wb-A is gone (closed/burned), so the reconciler only sees wb-B.
	workBead := beads.Bead{ID: "wb-B", Title: "next witness wisp", Type: "task", Status: "in_progress", Assignee: "witness"}

	reconcileSessionBeadsWithAssignedWork(env, []beads.Bead{session}, []beads.Bead{workBead})

	if env.sp.IsRunning(sessionName) {
		t.Fatal("session should have been killed by fresh-cycle")
	}
	got, _ := env.store.Get(session.ID)
	if got.Metadata[sessionpkg.CurrentBeadIDKey] != "wb-B" {
		t.Fatalf("%s = %q, want wb-B", sessionpkg.CurrentBeadIDKey, got.Metadata[sessionpkg.CurrentBeadIDKey])
	}
	if got.Metadata["started_config_hash"] != "" {
		t.Fatalf("started_config_hash = %q, want empty so the next wake takes the first-start path", got.Metadata["started_config_hash"])
	}
	if got.Metadata["continuation_reset_pending"] != "true" {
		t.Fatalf("continuation_reset_pending = %q, want true", got.Metadata["continuation_reset_pending"])
	}
	if got.Metadata["session_key"] == "" || got.Metadata["session_key"] == "conversation-A" {
		t.Fatalf("session_key = %q, want rotated key", got.Metadata["session_key"])
	}
}

// TestReconcileSessionBeads_AliveResumeModeReassignKeepsConversation verifies
// that wake_mode=resume sessions DO NOT cycle on bead reassign — the
// existing conversation is preserved and the agent picks up the new bead
// from its work query at its next prompt boundary.
func TestReconcileSessionBeads_AliveResumeModeReassignKeepsConversation(t *testing.T) {
	env := newRestartRequestTestEnv()
	env.cfg = &config.City{
		Workspace:     config.Workspace{Name: "test-city"},
		Agents:        []config.Agent{{Name: "witness", StartCommand: "true", MaxActiveSessions: restartRequestTestIntPtr(1)}},
		NamedSessions: []config.NamedSession{{Template: "witness", Mode: "on_demand"}},
	}
	sessionName := config.NamedSessionRuntimeName(env.cfg.Workspace.Name, env.cfg.Workspace, "witness")
	env.desiredState[sessionName] = TemplateParams{
		Command:      "true",
		SessionName:  sessionName,
		TemplateName: "witness",
		ResolvedProvider: &config.ResolvedProvider{
			SessionIDFlag: "--session-id",
		},
	}

	session := env.createSessionBead(sessionName)
	env.setSessionMetadata(&session, map[string]string{
		namedSessionMetadataKey:      "true",
		namedSessionIdentityMetadata: "witness",
		namedSessionModeMetadata:     "on_demand",
		"template":                   "witness",
		"state":                      "active",
		// wake_mode unset (default = resume)
		"session_key":               "conversation-A",
		sessionpkg.CurrentBeadIDKey: "wb-A",
	})
	if err := env.sp.Start(context.Background(), sessionName, runtime.Config{Command: "true"}); err != nil {
		t.Fatalf("start session: %v", err)
	}
	if err := env.sp.SetMeta(sessionName, "GC_SESSION_ID", session.ID); err != nil {
		t.Fatalf("SetMeta(GC_SESSION_ID): %v", err)
	}

	workBead := beads.Bead{ID: "wb-B", Title: "next witness wisp", Type: "task", Status: "in_progress", Assignee: "witness"}

	reconcileSessionBeadsWithAssignedWork(env, []beads.Bead{session}, []beads.Bead{workBead})

	if !env.sp.IsRunning(sessionName) {
		t.Fatal("resume-mode session should still be running — divergence must not cycle non-fresh sessions")
	}
	got, _ := env.store.Get(session.ID)
	if got.Metadata["session_key"] != "conversation-A" {
		t.Fatalf("session_key = %q, want conversation-A preserved", got.Metadata["session_key"])
	}
	if got.Metadata["continuation_reset_pending"] == "true" {
		t.Fatalf("continuation_reset_pending = true, want unset for resume mode (no cycle should have run)")
	}
}

// TestReconcileSessionBeads_AsleepWakeRecordsCurrentBead pins the recording
// half of the contract: when an asleep session is woken because of an
// assigned bead, the reconciler must stamp currently_processing_bead_id
// onto the session bead. Without this, the next reassign cycle would have
// no recorded current bead to compare against.
func TestReconcileSessionBeads_AsleepWakeRecordsCurrentBead(t *testing.T) {
	env := newRestartRequestTestEnv()
	env.cfg = &config.City{
		Workspace:     config.Workspace{Name: "test-city"},
		Agents:        []config.Agent{{Name: "witness", StartCommand: "true", MaxActiveSessions: restartRequestTestIntPtr(1)}},
		NamedSessions: []config.NamedSession{{Template: "witness", Mode: "on_demand"}},
	}
	sessionName := config.NamedSessionRuntimeName(env.cfg.Workspace.Name, env.cfg.Workspace, "witness")
	env.desiredState[sessionName] = TemplateParams{
		Command:      "true",
		SessionName:  sessionName,
		TemplateName: "witness",
		ResolvedProvider: &config.ResolvedProvider{
			SessionIDFlag: "--session-id",
		},
	}

	session := env.createSessionBead(sessionName)
	env.setSessionMetadata(&session, map[string]string{
		namedSessionMetadataKey:      "true",
		namedSessionIdentityMetadata: "witness",
		namedSessionModeMetadata:     "on_demand",
		"template":                   "witness",
		"state":                      "asleep",
		"wake_mode":                  "fresh",
	})

	workBead := beads.Bead{ID: "wb-77", Title: "witness wisp", Type: "task", Status: "in_progress", Assignee: "witness"}

	reconcileSessionBeadsWithAssignedWork(env, []beads.Bead{session}, []beads.Bead{workBead})

	got, _ := env.store.Get(session.ID)
	if got.Metadata[sessionpkg.CurrentBeadIDKey] != "wb-77" {
		t.Fatalf("%s = %q, want wb-77 recorded at wake", sessionpkg.CurrentBeadIDKey, got.Metadata[sessionpkg.CurrentBeadIDKey])
	}
}

// TestReconcileSessionBeads_RecoveryPrefersRecordedBead pins crash-recovery
// behavior: when a session is asleep with a recorded current bead AND
// multiple beads are assigned, the reconciler must anchor on the recorded
// bead so the agent resumes the work it was last actively processing.
func TestReconcileSessionBeads_RecoveryPrefersRecordedBead(t *testing.T) {
	env := newRestartRequestTestEnv()
	env.cfg = &config.City{
		Workspace:     config.Workspace{Name: "test-city"},
		Agents:        []config.Agent{{Name: "witness", StartCommand: "true", MaxActiveSessions: restartRequestTestIntPtr(1)}},
		NamedSessions: []config.NamedSession{{Template: "witness", Mode: "on_demand"}},
	}
	sessionName := config.NamedSessionRuntimeName(env.cfg.Workspace.Name, env.cfg.Workspace, "witness")
	env.desiredState[sessionName] = TemplateParams{
		Command:      "true",
		SessionName:  sessionName,
		TemplateName: "witness",
		ResolvedProvider: &config.ResolvedProvider{
			SessionIDFlag: "--session-id",
		},
	}

	session := env.createSessionBead(sessionName)
	env.setSessionMetadata(&session, map[string]string{
		namedSessionMetadataKey:      "true",
		namedSessionIdentityMetadata: "witness",
		namedSessionModeMetadata:     "on_demand",
		"template":                   "witness",
		"state":                      "asleep",
		"wake_mode":                  "fresh",
		sessionpkg.CurrentBeadIDKey:  "wb-current",
	})

	other := beads.Bead{ID: "wb-other", Title: "other wisp", Type: "task", Status: "open", Assignee: "witness"}
	current := beads.Bead{ID: "wb-current", Title: "current wisp", Type: "task", Status: "in_progress", Assignee: "witness"}

	reconcileSessionBeadsWithAssignedWork(env, []beads.Bead{session}, []beads.Bead{other, current})

	got, _ := env.store.Get(session.ID)
	if got.Metadata[sessionpkg.CurrentBeadIDKey] != "wb-current" {
		t.Fatalf("%s = %q, want wb-current preserved across restart", sessionpkg.CurrentBeadIDKey, got.Metadata[sessionpkg.CurrentBeadIDKey])
	}
}

// TestReconcileSessionBeads_BootSuppressesAliveCurrentBeadStamp pins the #3288
// boot fix: an alive named session whose assignee already points at its
// recorded bead would, in steady state, be re-stamped (a no-op write today, but
// the same write site that fires for every alive session at boot). During the
// boot reconcile (withBootReconcile) the alive-branch stamp must be skipped so
// the per-session write wave does not contend with the boot sweep's reads. The
// first steady-state tick then performs the stamp.
func TestReconcileSessionBeads_BootSuppressesAliveCurrentBeadStamp(t *testing.T) {
	newEnv := func(t *testing.T) (*restartRequestTestEnv, beads.Bead, string) {
		t.Helper()
		env := newRestartRequestTestEnv()
		env.cfg = &config.City{
			Workspace:     config.Workspace{Name: "test-city"},
			Agents:        []config.Agent{{Name: "witness", StartCommand: "true", MaxActiveSessions: restartRequestTestIntPtr(1)}},
			NamedSessions: []config.NamedSession{{Template: "witness", Mode: "on_demand"}},
		}
		sessionName := config.NamedSessionRuntimeName(env.cfg.Workspace.Name, env.cfg.Workspace, "witness")
		env.desiredState[sessionName] = TemplateParams{
			Command:      "true",
			SessionName:  sessionName,
			TemplateName: "witness",
			ResolvedProvider: &config.ResolvedProvider{
				SessionIDFlag: "--session-id",
			},
		}
		session := env.createSessionBead(sessionName)
		// Alive resume-mode session with no recorded current bead yet — the
		// exact shape that takes the alive-branch stamp for every session on the
		// first boot after #3288 shipped the new metadata key.
		env.setSessionMetadata(&session, map[string]string{
			namedSessionMetadataKey:      "true",
			namedSessionIdentityMetadata: "witness",
			namedSessionModeMetadata:     "on_demand",
			"template":                   "witness",
			"state":                      "active",
		})
		if err := env.sp.Start(context.Background(), sessionName, runtime.Config{Command: "true"}); err != nil {
			t.Fatalf("start session: %v", err)
		}
		if err := env.sp.SetMeta(sessionName, "GC_SESSION_ID", session.ID); err != nil {
			t.Fatalf("SetMeta(GC_SESSION_ID): %v", err)
		}
		return env, session, sessionName
	}

	work := beads.Bead{ID: "wb-1", Title: "witness wisp", Type: "task", Status: "in_progress", Assignee: "witness"}

	// Boot pass: the alive-branch stamp must be suppressed.
	bootEnv, bootSession, _ := newEnv(t)
	reconcileSessionBeadsBootWithAssignedWork(bootEnv, []beads.Bead{bootSession}, []beads.Bead{work})
	gotBoot, _ := bootEnv.store.Get(bootSession.ID)
	if v := gotBoot.Metadata[sessionpkg.CurrentBeadIDKey]; v != "" {
		t.Fatalf("boot reconcile stamped %s=%q; want it deferred to steady state (#3288)", sessionpkg.CurrentBeadIDKey, v)
	}

	// Steady-state pass over the same shape must perform the stamp.
	steadyEnv, steadySession, _ := newEnv(t)
	reconcileSessionBeadsWithAssignedWork(steadyEnv, []beads.Bead{steadySession}, []beads.Bead{work})
	gotSteady, _ := steadyEnv.store.Get(steadySession.ID)
	if v := gotSteady.Metadata[sessionpkg.CurrentBeadIDKey]; v != "wb-1" {
		t.Fatalf("steady-state %s = %q, want wb-1 (the deferred stamp must land off the boot path)", sessionpkg.CurrentBeadIDKey, v)
	}
}

// TestReconcileSessionBeads_BootSuppressesFreshReassignCycle pins that the
// heavier fresh-reassign cycle write (which kills and re-handshakes the session)
// is also suppressed during boot. Letting every diverged fresh session cycle at
// boot multiplies the write wave and the session teardowns that wedged
// readiness (#3288); the cycle is deferred to the first steady-state tick.
func TestReconcileSessionBeads_BootSuppressesFreshReassignCycle(t *testing.T) {
	env := newRestartRequestTestEnv()
	env.cfg = &config.City{
		Workspace:     config.Workspace{Name: "test-city"},
		Agents:        []config.Agent{{Name: "witness", StartCommand: "true", MaxActiveSessions: restartRequestTestIntPtr(1)}},
		NamedSessions: []config.NamedSession{{Template: "witness", Mode: "on_demand"}},
	}
	sessionName := config.NamedSessionRuntimeName(env.cfg.Workspace.Name, env.cfg.Workspace, "witness")
	env.desiredState[sessionName] = TemplateParams{
		Command:      "true",
		SessionName:  sessionName,
		TemplateName: "witness",
		ResolvedProvider: &config.ResolvedProvider{
			SessionIDFlag: "--session-id",
		},
	}
	session := env.createSessionBead(sessionName)
	env.setSessionMetadata(&session, map[string]string{
		namedSessionMetadataKey:      "true",
		namedSessionIdentityMetadata: "witness",
		namedSessionModeMetadata:     "on_demand",
		"template":                   "witness",
		"state":                      "active",
		"wake_mode":                  "fresh",
		"session_key":                "conversation-A",
		sessionpkg.CurrentBeadIDKey:  "wb-A",
	})
	if err := env.sp.Start(context.Background(), sessionName, runtime.Config{Command: "true"}); err != nil {
		t.Fatalf("start session: %v", err)
	}
	if err := env.sp.SetMeta(sessionName, "GC_SESSION_ID", session.ID); err != nil {
		t.Fatalf("SetMeta(GC_SESSION_ID): %v", err)
	}

	// Assignee now points at wb-B (a divergence that would normally fresh-cycle).
	workBead := beads.Bead{ID: "wb-B", Title: "next witness wisp", Type: "task", Status: "in_progress", Assignee: "witness"}

	reconcileSessionBeadsBootWithAssignedWork(env, []beads.Bead{session}, []beads.Bead{workBead})

	if !env.sp.IsRunning(sessionName) {
		t.Fatal("boot reconcile fresh-cycled the session; the cycle write must be deferred to steady state (#3288)")
	}
	got, _ := env.store.Get(session.ID)
	if got.Metadata[sessionpkg.CurrentBeadIDKey] != "wb-A" {
		t.Fatalf("%s = %q, want wb-A unchanged (no cycle write at boot)", sessionpkg.CurrentBeadIDKey, got.Metadata[sessionpkg.CurrentBeadIDKey])
	}
	if got.Metadata["continuation_reset_pending"] == "true" {
		t.Fatal("continuation_reset_pending=true; boot must not run the fresh-reassign cycle")
	}
}

// reconcileSessionBeadsWithAssignedWork is a test-only wrapper that mirrors
// restartRequestTestEnv.reconcile but threads assignedWorkBeads through so
// ComputeAwakeSet sees the work demand. Tests for assigned-work-driven
// behavior need this hook; the existing helper in
// session_reconciler_restart_request_test.go intentionally passes nil.
func reconcileSessionBeadsWithAssignedWork(env *restartRequestTestEnv, sessions []beads.Bead, assignedWork []beads.Bead) {
	poolDesired := make(map[string]int)
	for _, tp := range env.desiredState {
		if tp.TemplateName != "" {
			poolDesired[tp.TemplateName]++
		}
	}
	cfgNames := configuredSessionNames(env.cfg, "", env.store)
	_ = reconcileSessionBeads(
		context.Background(),
		sessions,
		env.desiredState,
		cfgNames,
		env.cfg,
		env.sp,
		env.store,
		nil,
		assignedWork,
		nil,
		env.dt,
		poolDesired,
		false,
		nil,
		"",
		nil,
		env.clk,
		env.rec,
		0,
		0,
		&env.stdout,
		&env.stderr,
	)
}

// reconcileSessionBeadsBootWithAssignedWork mirrors
// reconcileSessionBeadsWithAssignedWork but marks the pass as the one-shot boot
// reconcile (withBootReconcile), so it exercises the #3288 boot-time
// write-wave suppression on the alive-session branch.
func reconcileSessionBeadsBootWithAssignedWork(env *restartRequestTestEnv, sessions []beads.Bead, assignedWork []beads.Bead) {
	poolDesired := make(map[string]int)
	for _, tp := range env.desiredState {
		if tp.TemplateName != "" {
			poolDesired[tp.TemplateName]++
		}
	}
	cfgNames := configuredSessionNames(env.cfg, "", env.store)
	_ = reconcileSessionBeadsTracedWithNamedDemand(
		context.Background(),
		"",
		sessions,
		env.desiredState,
		cfgNames,
		env.cfg,
		env.sp,
		env.store,
		nil,
		assignedWork,
		nil,
		nil,
		env.dt,
		nil,
		poolDesired,
		nil,
		false,
		nil,
		"",
		nil,
		env.clk,
		env.rec,
		0,
		0,
		&env.stdout,
		&env.stderr,
		nil,
		withBootReconcile(),
	)
}
