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
