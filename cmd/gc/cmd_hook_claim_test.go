package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
)

func TestDoHookClaimUsesSelectedStoreContextForMutationAndContinuation(t *testing.T) {
	var claimedDir string
	var claimedEnv []string
	var listedDir string
	var listedEnv []string
	var assignedDir string
	var assignedEnv []string
	var assignedBead string

	storeDir := "rig-store"
	storeEnv := []string{"BEADS_DIR=rig-store", "GC_RIG_ROOT=rig-root"}
	candidates := []beads.Bead{{
		ID:       "bead-1",
		Status:   "open",
		Metadata: map[string]string{"gc.kind": "workflow", "gc.run_target": "route-1", "gc.root_bead_id": "root-1", "gc.continuation_group": "group-a"},
	}}
	output, err := json.Marshal(candidates)
	if err != nil {
		t.Fatalf("marshal candidates: %v", err)
	}

	ops := hookClaimOps{
		Runner: func(string, string) (string, error) { return string(output), nil },
		Claim: func(_ context.Context, dir string, env []string, beadID, assignee string) (beads.Bead, bool, error) {
			claimedDir = dir
			claimedEnv = append([]string(nil), env...)
			return beads.Bead{ID: beadID, Assignee: assignee, Status: "in_progress", Metadata: candidates[0].Metadata}, true, nil
		},
		ListContinuation: func(_ context.Context, dir string, env []string, rootID, group string) ([]beads.Bead, error) {
			listedDir = dir
			listedEnv = append([]string(nil), env...)
			if rootID != "root-1" || group != "group-a" {
				t.Fatalf("continuation lookup = (%q, %q), want (root-1, group-a)", rootID, group)
			}
			return []beads.Bead{{ID: "sib-1", Status: "open", Metadata: candidates[0].Metadata}}, nil
		},
		AssignContinuation: func(_ context.Context, dir string, env []string, beadID, assignee string) error {
			assignedDir = dir
			assignedEnv = append([]string(nil), env...)
			assignedBead = beadID
			if assignee != "worker-1" {
				t.Fatalf("assignee = %q, want worker-1", assignee)
			}
			return nil
		},
		DrainAck: func(io.Writer) error { return nil },
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("query", storeDir, hookClaimOptions{
		Assignee:           "worker-1",
		IdentityCandidates: []string{"worker-1"},
		RouteTargets:       []string{"route-1"},
		Env:                storeEnv,
		JSON:               true,
	}, ops, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}
	if claimedDir != storeDir {
		t.Fatalf("claimedDir = %q, want %q", claimedDir, storeDir)
	}
	if listedDir != storeDir {
		t.Fatalf("listedDir = %q, want %q", listedDir, storeDir)
	}
	if assignedDir != storeDir {
		t.Fatalf("assignedDir = %q, want %q", assignedDir, storeDir)
	}
	if !reflect.DeepEqual(claimedEnv, storeEnv) {
		t.Fatalf("claimedEnv = %#v, want %#v", claimedEnv, storeEnv)
	}
	if !reflect.DeepEqual(listedEnv, storeEnv) {
		t.Fatalf("listedEnv = %#v, want %#v", listedEnv, storeEnv)
	}
	if !reflect.DeepEqual(assignedEnv, storeEnv) {
		t.Fatalf("assignedEnv = %#v, want %#v", assignedEnv, storeEnv)
	}
	if assignedBead != "sib-1" {
		t.Fatalf("assignedBead = %q, want sib-1", assignedBead)
	}
}

// poolGraphV2RootBead returns a minimal pool-routed graph.v2 workflow root bead
// with the given id, assignee, and optional session id stamped in metadata.
func poolGraphV2RootBead(id, routeTarget, sessionID string) beads.Bead {
	return beads.Bead{
		ID:     id,
		Status: "open",
		Metadata: map[string]string{
			beadmeta.KindMetadataKey:              beadmeta.KindWorkflow,
			beadmeta.FormulaContractMetadataKey:   beadmeta.FormulaContractGraphV2,
			beadmeta.ContinuationGroupMetadataKey: beadmeta.PoolWorkflowContinuationGroup,
			beadmeta.RunTargetMetadataKey:         routeTarget,
			beadmeta.SessionIDMetadataKey:         sessionID,
		},
	}
}

// minimalPoolRootOps returns a hookClaimOps that claims successfully and
// exercises no continuation pre-assignment (no root_bead_id on the bead).
func minimalPoolRootOps(bead beads.Bead, nudgeFn hookEnqueuePoolRootNudgeFunc) hookClaimOps {
	return hookClaimOps{
		Runner: func(string, string) (string, error) {
			out, _ := json.Marshal([]beads.Bead{bead})
			return string(out), nil
		},
		Claim: func(_ context.Context, _ string, _ []string, _, assignee string) (beads.Bead, bool, error) {
			b := bead
			b.Assignee = assignee
			b.Status = "in_progress"
			return b, true, nil
		},
		ListContinuation:     func(_ context.Context, _ string, _ []string, _, _ string) ([]beads.Bead, error) { return nil, nil },
		AssignContinuation:   func(_ context.Context, _ string, _ []string, _, _ string) error { return nil },
		EnqueuePoolRootNudge: nudgeFn,
	}
}

// TestHookClaimPoolGraphV2Root_EnqueuesNudge verifies that claiming a pool
// graph.v2 workflow root calls EnqueuePoolRootNudge exactly once, threading the
// real CityPath and Cfg and seeding the session fence from the claim env
// (GC_SESSION_ID / GC_CONTINUATION_EPOCH) — pool roots carry no gc.session_id
// in metadata, so the env is the authoritative fence source.
func TestHookClaimPoolGraphV2Root_EnqueuesNudge(t *testing.T) {
	bead := poolGraphV2RootBead("wf-root-1", "worker-1", "")

	cfg := &config.City{}
	var calls []struct {
		cfg                                         *config.City
		cityPath, sessionName, sessionID, contEpoch string
	}
	ops := minimalPoolRootOps(bead, func(c *config.City, cityPath, sessionName, sessionID, contEpoch string) error {
		calls = append(calls, struct {
			cfg                                         *config.City
			cityPath, sessionName, sessionID, contEpoch string
		}{c, cityPath, sessionName, sessionID, contEpoch})
		return nil
	})

	var stdout, stderr bytes.Buffer
	code := doHookClaim("query", "city-dir", hookClaimOptions{
		Assignee:     "worker-1",
		RouteTargets: []string{"worker-1"},
		CityPath:     "city-root",
		Cfg:          cfg,
		Env:          []string{"GC_SESSION_ID=sess-abc", "GC_CONTINUATION_EPOCH=epoch-7"},
	}, ops, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}
	if len(calls) != 1 {
		t.Fatalf("EnqueuePoolRootNudge called %d times, want 1", len(calls))
	}
	if calls[0].cfg != cfg {
		t.Fatalf("cfg = %p, want threaded %p", calls[0].cfg, cfg)
	}
	if calls[0].cityPath != "city-root" {
		t.Fatalf("cityPath = %q, want city-root (threaded, not per-store dir)", calls[0].cityPath)
	}
	if calls[0].sessionName != "worker-1" {
		t.Fatalf("sessionName = %q, want worker-1", calls[0].sessionName)
	}
	if calls[0].sessionID != "sess-abc" {
		t.Fatalf("sessionID = %q, want sess-abc (from GC_SESSION_ID)", calls[0].sessionID)
	}
	if calls[0].contEpoch != "epoch-7" {
		t.Fatalf("continuationEpoch = %q, want epoch-7 (from GC_CONTINUATION_EPOCH)", calls[0].contEpoch)
	}
}

// TestHookClaimNonGraphV2Bead_NoNudge verifies that claiming a non-graph.v2
// bead does not call EnqueuePoolRootNudge.
func TestHookClaimNonGraphV2Bead_NoNudge(t *testing.T) {
	bead := beads.Bead{
		ID:     "task-1",
		Status: "open",
		Metadata: map[string]string{
			beadmeta.KindMetadataKey:     "task",
			beadmeta.RoutedToMetadataKey: "worker-1",
		},
	}
	var called int
	ops := minimalPoolRootOps(bead, func(_ *config.City, _, _, _, _ string) error { called++; return nil })

	var stdout, stderr bytes.Buffer
	code := doHookClaim("query", "city-dir", hookClaimOptions{
		Assignee:     "worker-1",
		RouteTargets: []string{"worker-1"},
	}, ops, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}
	if called != 0 {
		t.Fatalf("EnqueuePoolRootNudge called %d times, want 0", called)
	}
}

// TestHookClaimPoolStepBead_NoNudge verifies that a pool step bead (same
// continuation group but kind != "workflow") does not call EnqueuePoolRootNudge.
func TestHookClaimPoolStepBead_NoNudge(t *testing.T) {
	bead := beads.Bead{
		ID:     "step-1",
		Status: "open",
		Metadata: map[string]string{
			beadmeta.KindMetadataKey:              "task",
			beadmeta.FormulaContractMetadataKey:   beadmeta.FormulaContractGraphV2,
			beadmeta.ContinuationGroupMetadataKey: beadmeta.PoolWorkflowContinuationGroup,
			beadmeta.RoutedToMetadataKey:          "worker-1",
		},
	}
	var called int
	ops := minimalPoolRootOps(bead, func(_ *config.City, _, _, _, _ string) error { called++; return nil })

	var stdout, stderr bytes.Buffer
	code := doHookClaim("query", "city-dir", hookClaimOptions{
		Assignee:     "worker-1",
		RouteTargets: []string{"worker-1"},
	}, ops, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}
	if called != 0 {
		t.Fatalf("EnqueuePoolRootNudge called %d times, want 0 (kind != workflow)", called)
	}
}

// TestHookClaimNamedSessionBead_NoNudge verifies that a named-session graph.v2
// workflow root (no pool continuation group) does not call EnqueuePoolRootNudge.
func TestHookClaimNamedSessionBead_NoNudge(t *testing.T) {
	bead := beads.Bead{
		ID:     "named-wf-1",
		Status: "open",
		Metadata: map[string]string{
			beadmeta.KindMetadataKey:            beadmeta.KindWorkflow,
			beadmeta.FormulaContractMetadataKey: beadmeta.FormulaContractGraphV2,
			beadmeta.RunTargetMetadataKey:       "worker-1",
			// No ContinuationGroupMetadataKey → not pool-routed.
		},
	}
	var called int
	ops := minimalPoolRootOps(bead, func(_ *config.City, _, _, _, _ string) error { called++; return nil })

	var stdout, stderr bytes.Buffer
	code := doHookClaim("query", "city-dir", hookClaimOptions{
		Assignee:     "worker-1",
		RouteTargets: []string{"worker-1"},
	}, ops, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}
	if called != 0 {
		t.Fatalf("EnqueuePoolRootNudge called %d times, want 0 (no continuation group)", called)
	}
}

// TestHookClaimPoolGraphV2Root_NudgeError verifies that a nudge-enqueue error
// does not cause the claim to fail: exit code is still 0 and the error appears
// on stderr.
func TestHookClaimPoolGraphV2Root_NudgeError(t *testing.T) {
	bead := poolGraphV2RootBead("wf-root-2", "worker-1", "sess-def")
	ops := minimalPoolRootOps(bead, func(_ *config.City, _, _, _, _ string) error {
		return errors.New("nudge queue full")
	})

	var stdout, stderr bytes.Buffer
	code := doHookClaim("query", "city-dir", hookClaimOptions{
		Assignee:     "worker-1",
		RouteTargets: []string{"worker-1"},
	}, ops, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d after nudge error, want 0", code)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("nudge queue full")) {
		t.Fatalf("stderr %q does not contain nudge error", stderr.String())
	}
}

// TestHookClaimPoolGraphV2Root_NilNudgeSeam verifies that a nil
// EnqueuePoolRootNudge seam does not panic and the claim still succeeds.
func TestHookClaimPoolGraphV2Root_NilNudgeSeam(t *testing.T) {
	bead := poolGraphV2RootBead("wf-root-3", "worker-1", "sess-ghi")
	ops := minimalPoolRootOps(bead, nil) // nil seam

	var stdout, stderr bytes.Buffer
	code := doHookClaim("query", "city-dir", hookClaimOptions{
		Assignee:     "worker-1",
		RouteTargets: []string{"worker-1"},
	}, ops, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d with nil nudge seam, want 0; stderr=%s", code, stderr.String())
	}
}

// TestEnqueuePoolRootContinuationNudge_HonorsSupervisorDispatcher exercises the
// production seam (not a fake) to prove the blocker fix: with
// daemon.nudge_dispatcher = "supervisor" the per-session sidecar poller is NOT
// started — the supervisor dispatcher owns queued delivery and a sidecar
// `gc nudge poll` would race it and reintroduce the bd-shellout load it exists
// to eliminate. In legacy mode the poller IS started. The regression this
// guards: nudgeDispatcherIsSupervisor reads target.cfg, so a hand-built target
// with a nil cfg silently defeats the guard and always spawns the sidecar.
func TestEnqueuePoolRootContinuationNudge_HonorsSupervisorDispatcher(t *testing.T) {
	origPoller := startNudgePoller
	origStore := openNudgeBeadStore
	t.Cleanup(func() { startNudgePoller = origPoller; openNudgeBeadStore = origStore })
	// Zero store: the seeded fence below short-circuits withNudgeTargetFence
	// before any store read, so no session beads are needed.
	openNudgeBeadStore = func(string) beads.NudgesStore { return beads.NudgesStore{} }

	for _, tc := range []struct {
		name       string
		dispatcher string
		wantPoller bool
	}{
		{"supervisor skips sidecar poller", "supervisor", false},
		{"legacy starts sidecar poller", "legacy", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var started bool
			startNudgePoller = func(_, _, _ string) error { started = true; return nil }
			cfg := &config.City{Daemon: config.DaemonConfig{NudgeDispatcher: tc.dispatcher}}
			// Seed both fence fields (sessionID + continuationEpoch) so
			// withNudgeTargetFence returns without touching the store: this
			// test isolates the poller decision, not the fence lookup.
			if err := enqueuePoolRootContinuationNudge(cfg, t.TempDir(), "pool-worker/slot-0", "sess-1", "epoch-1"); err != nil {
				t.Fatalf("enqueuePoolRootContinuationNudge() error = %v", err)
			}
			if started != tc.wantPoller {
				t.Fatalf("startNudgePoller called = %v, want %v (dispatcher=%q)", started, tc.wantPoller, tc.dispatcher)
			}
		})
	}
}
