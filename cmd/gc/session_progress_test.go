package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
)

func TestSessionProgressStalled(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	stale := now.Add(-time.Hour)    // well past any sane threshold
	recent := now.Add(-time.Second) // within threshold
	const threshold = 30 * time.Minute

	tests := []struct {
		name            string
		threshold       time.Duration
		holdsClaim      bool
		providerHealthy bool
		exempt          bool
		lastProgress    time.Time
		want            bool
	}{
		{"stalled: alive, no claim, healthy, not exempt, old progress", threshold, false, true, false, stale, true},
		{"disabled when threshold is zero", 0, false, true, false, stale, false},
		{"not stalled when progress is recent", threshold, false, true, false, recent, false},
		{"holds a claim -> reaper's job, not recycled", threshold, true, true, false, stale, false},
		{"provider unhealthy -> never recycle into a dead provider", threshold, false, false, false, stale, false},
		{"exempt (attached/interactive/startup) -> left alone", threshold, false, true, true, stale, false},
		{"unknown progress (zero) -> conservative, not recycled", threshold, false, true, false, time.Time{}, false},
		{"exactly at threshold is not yet stalled", threshold, false, true, false, now.Add(-threshold), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sessionProgressStalled(tc.threshold, tc.holdsClaim, tc.providerHealthy, tc.exempt, tc.lastProgress, now)
			if got != tc.want {
				t.Errorf("sessionProgressStalled = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestProgressStall_MinFloorIdleWorker_NotRecycled verifies that a pool worker
// sitting below the min_active_sessions floor is exempt from the stall recycler.
func TestProgressStall_MinFloorIdleWorker_NotRecycled(t *testing.T) {
	tests := []struct {
		name       string
		min        int
		open       int
		wantExempt bool
	}{
		// pool with min=1, exactly 1 open session → at floor, exempt
		{"at floor: open == min", 1, 1, true},
		// pool with min=2, 1 open session → below floor, exempt
		{"below floor: open < min", 2, 1, true},
		// pool with min=1, 2 open sessions → above floor, not exempt
		{"above floor: open > min", 1, 2, false},
		// pool with min=0 (no floor) → not exempt regardless of open count
		{"no floor: min == 0", 0, 1, false},
		// pool with min=0, open=0 → also not exempt
		{"no floor, empty pool", 0, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isMinFloorIdleWorker(tc.min, tc.open)
			if got != tc.wantExempt {
				t.Errorf("isMinFloorIdleWorker(%d, %d) = %v, want %v", tc.min, tc.open, got, tc.wantExempt)
			}
		})
	}
}

// TestProgressStall_DemandWorkerLostClaim_IsRecycled verifies that a demand
// worker (pool with no floor, or pool above its floor) that holds no claim
// and has stale progress IS recycled by sessionProgressStalled.
func TestProgressStall_DemandWorkerLostClaim_IsRecycled(t *testing.T) {
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	stale := now.Add(-time.Hour)
	const threshold = 30 * time.Minute

	tests := []struct {
		name        string
		min         int
		open        int
		wantRecycle bool
	}{
		// min=0: no floor at all, demand worker is recycled
		{"demand pool: min=0, open=1", 0, 1, true},
		// min=1 but 2 open sessions: above floor, demand worker is recycled
		{"above floor: min=1, open=2", 1, 2, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			floorExempt := isMinFloorIdleWorker(tc.min, tc.open)
			recycled := sessionProgressStalled(threshold, false, true, floorExempt, stale, now)
			if recycled != tc.wantRecycle {
				t.Errorf("demand worker: isMinFloorIdleWorker(%d,%d)=%v; sessionProgressStalled=%v, want %v",
					tc.min, tc.open, floorExempt, recycled, tc.wantRecycle)
			}
		})
	}
}

// TestPoolGraphV2AutoAdvance_NudgeEnqueuedAtClaim verifies the full
// doHookClaim path for a pool-routed graph.v2 workflow root: claiming the root
// automatically enqueues a continuation nudge so the pool session advances
// through its formula steps without requiring a manual nudge. This is the
// mechanism that enables pool graph.v2 auto-advance.
func TestPoolGraphV2AutoAdvance_NudgeEnqueuedAtClaim(t *testing.T) {
	root := beads.Bead{
		ID:     "wf-auto-1",
		Status: "open",
		Metadata: map[string]string{
			beadmeta.KindMetadataKey:              beadmeta.KindWorkflow,
			beadmeta.FormulaContractMetadataKey:   beadmeta.FormulaContractGraphV2,
			beadmeta.ContinuationGroupMetadataKey: beadmeta.PoolWorkflowContinuationGroup,
			beadmeta.RunTargetMetadataKey:         "pool-worker",
			beadmeta.SessionIDMetadataKey:         "sess-pool-1",
		},
	}
	out, _ := json.Marshal([]beads.Bead{root})

	var nudgeEnqueued bool
	ops := hookClaimOps{
		Runner: func(string, string) (string, error) { return string(out), nil },
		Claim: func(_ context.Context, _ string, _ []string, _, assignee string) (beads.Bead, bool, error) {
			b := root
			b.Assignee = assignee
			b.Status = "in_progress"
			return b, true, nil
		},
		ListContinuation:   func(_ context.Context, _ string, _ []string, _, _ string) ([]beads.Bead, error) { return nil, nil },
		AssignContinuation: func(_ context.Context, _ string, _ []string, _, _ string) error { return nil },
		EnqueuePoolRootNudge: func(_, assignee, sessionID, _ string) error {
			if assignee != "pool-worker" {
				t.Errorf("nudge assignee = %q, want pool-worker", assignee)
			}
			if sessionID != "sess-pool-1" {
				t.Errorf("nudge sessionID = %q, want sess-pool-1", sessionID)
			}
			nudgeEnqueued = true
			return nil
		},
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("query", "city-path", hookClaimOptions{
		Assignee:     "pool-worker",
		RouteTargets: []string{"pool-worker"},
	}, ops, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !nudgeEnqueued {
		t.Fatal("continuation nudge was not enqueued after claiming pool graph.v2 root; pool session will not auto-advance")
	}
}
