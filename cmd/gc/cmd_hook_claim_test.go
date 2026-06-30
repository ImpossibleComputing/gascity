package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
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
func minimalPoolRootOps(bead beads.Bead, nudgeFn func(string, string, string, string) error) hookClaimOps {
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
// graph.v2 workflow root calls EnqueuePoolRootNudge exactly once with the
// correct assignee, sessionID, and sessionName.
func TestHookClaimPoolGraphV2Root_EnqueuesNudge(t *testing.T) {
	bead := poolGraphV2RootBead("wf-root-1", "worker-1", "sess-abc")

	var calls []struct{ cityPath, assignee, sessionID, sessionName string }
	ops := minimalPoolRootOps(bead, func(cityPath, assignee, sessionID, sessionName string) error {
		calls = append(calls, struct{ cityPath, assignee, sessionID, sessionName string }{cityPath, assignee, sessionID, sessionName})
		return nil
	})

	var stdout, stderr bytes.Buffer
	code := doHookClaim("query", "city-dir", hookClaimOptions{
		Assignee:     "worker-1",
		RouteTargets: []string{"worker-1"},
	}, ops, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}
	if len(calls) != 1 {
		t.Fatalf("EnqueuePoolRootNudge called %d times, want 1", len(calls))
	}
	if calls[0].assignee != "worker-1" {
		t.Fatalf("assignee = %q, want worker-1", calls[0].assignee)
	}
	if calls[0].sessionID != "sess-abc" {
		t.Fatalf("sessionID = %q, want sess-abc", calls[0].sessionID)
	}
	if calls[0].sessionName != "worker-1" {
		t.Fatalf("sessionName = %q, want worker-1", calls[0].sessionName)
	}
	if calls[0].cityPath != "city-dir" {
		t.Fatalf("cityPath = %q, want city-dir", calls[0].cityPath)
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
	ops := minimalPoolRootOps(bead, func(_, _, _, _ string) error { called++; return nil })

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
	ops := minimalPoolRootOps(bead, func(_, _, _, _ string) error { called++; return nil })

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
	ops := minimalPoolRootOps(bead, func(_, _, _, _ string) error { called++; return nil })

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
	ops := minimalPoolRootOps(bead, func(_, _, _, _ string) error {
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

// poolGraphV2RootBeadWithRoot returns a pool-routed graph.v2 workflow root
// bead that also carries gc.root_bead_id so preassignHookContinuationGroup
// can locate and assign continuation siblings.
func poolGraphV2RootBeadWithRoot(id, routeTarget, sessionID, rootID string) beads.Bead {
	return beads.Bead{
		ID:     id,
		Status: "open",
		Metadata: map[string]string{
			beadmeta.KindMetadataKey:              beadmeta.KindWorkflow,
			beadmeta.FormulaContractMetadataKey:   beadmeta.FormulaContractGraphV2,
			beadmeta.ContinuationGroupMetadataKey: beadmeta.PoolWorkflowContinuationGroup,
			beadmeta.RunTargetMetadataKey:         routeTarget,
			beadmeta.SessionIDMetadataKey:         sessionID,
			beadmeta.RootBeadIDMetadataKey:        rootID,
		},
	}
}

// opsForNudgeContentTest returns hookClaimOps for tests that inspect the
// actual nudge content written to the filesystem. EnqueuePoolRootNudge is
// intentionally left nil so applyDefaults wires the real
// enqueuePoolRootContinuationNudge.
func opsForNudgeContentTest(bead beads.Bead, siblings []beads.Bead) hookClaimOps {
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
		ListContinuation: func(_ context.Context, _ string, _ []string, _, _ string) ([]beads.Bead, error) {
			return siblings, nil
		},
		AssignContinuation: func(_ context.Context, _ string, _ []string, _, _ string) error { return nil },
		// EnqueuePoolRootNudge: nil — applyDefaults sets enqueuePoolRootContinuationNudge
	}
}

// stubNudgeSideEffects replaces the package-level vars that
// enqueuePoolRootContinuationNudge uses to spawn background processes, so
// the real nudge-enqueue path can run safely against a t.TempDir() cityPath.
// Tests using this helper must not call t.Parallel because the stubs are
// global.
func stubNudgeSideEffects(t *testing.T) {
	t.Helper()
	prevPoller := startNudgePoller
	startNudgePoller = func(_, _, _ string) error { return nil }
	t.Cleanup(func() { startNudgePoller = prevPoller })

	prevStore := openNudgeBeadStore
	openNudgeBeadStore = func(string) beads.Store { return nil }
	t.Cleanup(func() { openNudgeBeadStore = prevStore })
}

// pendingNudgesInState reads the nudge queue persisted at cityPath and
// returns all items in state.Pending regardless of agent or session.
func pendingNudgesInState(t *testing.T, cityPath string) []queuedNudge {
	t.Helper()
	var pending []queuedNudge
	if err := withNudgeQueueState(cityPath, func(state *nudgeQueueState) error {
		pending = append(pending, state.Pending...)
		return nil
	}); err != nil {
		t.Fatalf("reading nudge queue state: %v", err)
	}
	return pending
}

// TestHookClaimPoolGraphV2Root_NudgeMessageAndSource verifies that a fresh
// claim of a pool graph.v2 workflow root with at least one assigned
// continuation sibling enqueues exactly one nudge with Source
// "hook-claim-continuation", Message "Work slung. Check your hook.", and
// Agent equal to the claiming session name (not the session ID stored in
// bead metadata).
//
// This test is intended to FAIL against the current production
// implementation (Source "hook-claim", wrong message, Agent = sessionID)
// and pass after ga-7n7vth.2 fixes those fields.
func TestHookClaimPoolGraphV2Root_NudgeMessageAndSource(t *testing.T) {
	cityPath := t.TempDir()
	stubNudgeSideEffects(t)

	root := poolGraphV2RootBeadWithRoot("wf-root-msg", "worker-1", "sess-msg", "root-msg")
	sibling := beads.Bead{
		ID:     "step-msg-a",
		Status: "open",
		Metadata: map[string]string{
			beadmeta.KindMetadataKey:              "task",
			beadmeta.FormulaContractMetadataKey:   beadmeta.FormulaContractGraphV2,
			beadmeta.ContinuationGroupMetadataKey: beadmeta.PoolWorkflowContinuationGroup,
			beadmeta.RootBeadIDMetadataKey:        "root-msg",
			beadmeta.RoutedToMetadataKey:          "worker-1",
		},
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("query", cityPath, hookClaimOptions{
		Assignee:     "worker-1",
		RouteTargets: []string{"worker-1"},
	}, opsForNudgeContentTest(root, []beads.Bead{sibling}), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}

	pending := pendingNudgesInState(t, cityPath)
	if len(pending) != 1 {
		t.Fatalf("pending nudge count = %d, want 1", len(pending))
	}
	n := pending[0]

	const wantSource = "hook-claim-continuation"
	const wantMsg = "Work slung. Check your hook."
	const wantAgent = "worker-1" // claiming session name, not the session ID in bead metadata
	if n.Source != wantSource {
		t.Errorf("nudge Source = %q, want %q", n.Source, wantSource)
	}
	if n.Message != wantMsg {
		t.Errorf("nudge Message = %q, want %q", n.Message, wantMsg)
	}
	if n.Agent != wantAgent {
		t.Errorf("nudge Agent = %q, want %q (should be session name, not session ID)", n.Agent, wantAgent)
	}
	_ = time.Now() // anchor for future DeliverAfter assertions
}

// TestHookClaimPoolGraphV2Root_ReFindNoDoubleNudge verifies that the
// existing_assignment (re-find) path does NOT enqueue a second continuation
// nudge. The first fresh-claim call is permitted to enqueue one; a subsequent
// call that resolves to an already-assigned root must not add another.
//
// This test is intended to FAIL against the current production implementation
// because writeHookClaimWorkResultForBead is invoked on both the fresh-claim
// and existing_assignment paths, causing two nudges to be enqueued. The fix
// in ga-7n7vth.2 gates enqueueing on result reason == "claimed" only.
func TestHookClaimPoolGraphV2Root_ReFindNoDoubleNudge(t *testing.T) {
	cityPath := t.TempDir()
	stubNudgeSideEffects(t)

	root := poolGraphV2RootBeadWithRoot("wf-root-idem", "worker-1", "sess-idem", "root-idem")
	sibling := beads.Bead{
		ID:     "step-idem-a",
		Status: "open",
		Metadata: map[string]string{
			beadmeta.KindMetadataKey:              "task",
			beadmeta.FormulaContractMetadataKey:   beadmeta.FormulaContractGraphV2,
			beadmeta.ContinuationGroupMetadataKey: beadmeta.PoolWorkflowContinuationGroup,
			beadmeta.RootBeadIDMetadataKey:        "root-idem",
			beadmeta.RoutedToMetadataKey:          "worker-1",
		},
	}

	var callCount int
	ops := hookClaimOps{
		Runner: func(string, string) (string, error) {
			callCount++
			if callCount == 1 {
				// First call: bead is open and unassigned.
				out, _ := json.Marshal([]beads.Bead{root})
				return string(out), nil
			}
			// Second call: bead is already in-progress (existing_assignment path).
			claimed := root
			claimed.Status = "in_progress"
			claimed.Assignee = "worker-1"
			out, _ := json.Marshal([]beads.Bead{claimed})
			return string(out), nil
		},
		Claim: func(_ context.Context, _ string, _ []string, _, assignee string) (beads.Bead, bool, error) {
			b := root
			b.Assignee = assignee
			b.Status = "in_progress"
			return b, true, nil
		},
		ListContinuation: func(_ context.Context, _ string, _ []string, _, _ string) ([]beads.Bead, error) {
			return []beads.Bead{sibling}, nil
		},
		AssignContinuation: func(_ context.Context, _ string, _ []string, _, _ string) error { return nil },
	}
	opts := hookClaimOptions{
		Assignee:           "worker-1",
		IdentityCandidates: []string{"worker-1"},
		RouteTargets:       []string{"worker-1"},
	}
	var stdout, stderr bytes.Buffer

	// First call: fresh claim enqueues the nudge.
	if code := doHookClaim("query", cityPath, opts, ops, &stdout, &stderr); code != 0 {
		t.Fatalf("first doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	// Second call: re-find must not add a second nudge.
	if code := doHookClaim("query", cityPath, opts, ops, &stdout, &stderr); code != 0 {
		t.Fatalf("second doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}

	pending := pendingNudgesInState(t, cityPath)
	if len(pending) != 1 {
		t.Fatalf("pending nudge count after re-find = %d, want 1 (re-find must not add a second nudge)", len(pending))
	}
}

// TestHookClaimPoolStepBead_WithContinuationAssigned_NoContinuationNudge
// verifies that claiming a non-workflow step bead (kind != "workflow") never
// enqueues a hook-claim-continuation nudge, even when continuation siblings
// are assigned during the claim.
func TestHookClaimPoolStepBead_WithContinuationAssigned_NoContinuationNudge(t *testing.T) {
	cityPath := t.TempDir()
	stubNudgeSideEffects(t)

	// Step bead: kind "task", has root_bead_id + continuation_group but is not
	// the pool workflow root, so isPoolGraphV2WorkflowRoot must return false.
	step := beads.Bead{
		ID:     "step-claim-6",
		Status: "open",
		Metadata: map[string]string{
			beadmeta.KindMetadataKey:              "task",
			beadmeta.FormulaContractMetadataKey:   beadmeta.FormulaContractGraphV2,
			beadmeta.ContinuationGroupMetadataKey: beadmeta.PoolWorkflowContinuationGroup,
			beadmeta.RootBeadIDMetadataKey:        "root-6",
			beadmeta.RoutedToMetadataKey:          "worker-1",
		},
	}
	sibling := beads.Bead{
		ID:     "step-claim-6b",
		Status: "open",
		Metadata: map[string]string{
			beadmeta.KindMetadataKey:              "task",
			beadmeta.FormulaContractMetadataKey:   beadmeta.FormulaContractGraphV2,
			beadmeta.ContinuationGroupMetadataKey: beadmeta.PoolWorkflowContinuationGroup,
			beadmeta.RootBeadIDMetadataKey:        "root-6",
			beadmeta.RoutedToMetadataKey:          "worker-1",
		},
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("query", cityPath, hookClaimOptions{
		Assignee:     "worker-1",
		RouteTargets: []string{"worker-1"},
	}, opsForNudgeContentTest(step, []beads.Bead{sibling}), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}

	pending := pendingNudgesInState(t, cityPath)
	for _, n := range pending {
		if n.Source == "hook-claim-continuation" {
			t.Fatalf("step bead claim enqueued hook-claim-continuation nudge, want none; nudge=%+v", n)
		}
	}
}
