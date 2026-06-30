package main

// Tests for the hook-claim-continuation nudge enqueue seam.
//
// Invariant: gc hook --claim enqueues the hook-claim-continuation nudge exactly
// when a newly claimed pool graph.v2 workflow root pre-assigns at least one
// continuation sibling, and never otherwise:
//
//   - A newly claimed workflow root that pre-assigns at least one continuation
//     sibling enqueues exactly one queued nudge with source
//     "hook-claim-continuation", targeting the claiming session by name, fenced
//     to the session generation (SessionID/ContinuationEpoch from the claim
//     env), using the canonical propulsion message.
//   - Non-workflow step-bead claims do not enqueue the nudge.
//   - Re-found existing assignments do not enqueue the nudge (idempotence): the
//     session is already active, so a second nudge would only force a redundant
//     hook re-entry.
//   - Workflow root claims with zero continuation siblings do not enqueue: there
//     is nothing to propel into.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
)

// makeWorkflowRootBead returns a bead shaped like a pool graph.v2 workflow root
// ready to be claimed: gc.kind=workflow, gc.run_target pointing at the pool
// template, and gc.root_bead_id / gc.continuation_group so pre-assignment runs.
func makeWorkflowRootBead(id, runTarget, rootID, continuationGroup string) beads.Bead {
	return beads.Bead{
		ID:     id,
		Status: "open",
		Metadata: map[string]string{
			"gc.kind":               "workflow",
			"gc.run_target":         runTarget,
			"gc.root_bead_id":       rootID,
			"gc.continuation_group": continuationGroup,
		},
	}
}

// makeStepBead returns a bead shaped like a formula step (gc.kind=step) with
// the same continuation metadata a workflow root has. Step beads reach the
// work queue via gc.routed_to (set by preassignHookContinuationGroup), not
// via gc.run_target, so routedTo is the claiming session name.
func makeStepBead(id, routedTo, rootID, continuationGroup string) beads.Bead {
	return beads.Bead{
		ID:     id,
		Status: "open",
		Metadata: map[string]string{
			"gc.kind":               "step",
			"gc.routed_to":          routedTo,
			"gc.root_bead_id":       rootID,
			"gc.continuation_group": continuationGroup,
		},
	}
}

// captureNudgeOps returns a hookClaimOps that captures the city path and nudge
// item passed to EnqueueContinuationNudge. The capture is written to *calls.
func captureNudgeOps(t *testing.T, calls *[]queuedNudge, bead beads.Bead, sibling beads.Bead) hookClaimOps {
	t.Helper()
	output, err := json.Marshal([]beads.Bead{bead})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return hookClaimOps{
		Runner: func(string, string) (string, error) { return string(output), nil },
		Claim: func(_ context.Context, _ string, _ []string, _, assignee string) (beads.Bead, bool, error) {
			claimed := bead
			claimed.Assignee = assignee
			claimed.Status = "in_progress"
			return claimed, true, nil
		},
		ListContinuation: func(_ context.Context, _ string, _ []string, _, _ string) ([]beads.Bead, error) {
			if sibling.ID == "" {
				return nil, nil
			}
			return []beads.Bead{sibling}, nil
		},
		AssignContinuation: func(_ context.Context, _ string, _ []string, _, _ string) error {
			return nil
		},
		DrainAck:          func(io.Writer) error { return nil },
		ResolveWorkBranch: func(string) string { return "" },
		StampWorkBranch:   func(_ context.Context, _ string, _ []string, _, _, _ string) error { return nil },
		RecordSessionPointers: func(_ context.Context, _ string, _ []string, _, _, _, _ string) error {
			return nil
		},
		EnqueueContinuationNudge: func(_ string, item queuedNudge) error {
			*calls = append(*calls, item)
			return nil
		},
	}
}

// TestHookClaimWorkflowRootEnqueuesContinuationNudge verifies that claiming a
// new pool graph.v2 workflow root with at least one pre-assigned continuation
// sibling enqueues exactly one queued nudge with the canonical propulsion
// semantics (source, message, agent equal to the claiming session name).
func TestHookClaimWorkflowRootEnqueuesContinuationNudge(t *testing.T) {
	root := makeWorkflowRootBead("root-1", "pool-worker", "root-1", "group-a")
	// Continuation siblings in a graph.v2 formula use gc.kind=workflow and gc.run_target
	// for pre-assignment routing — the same shape as the root, just a different bead ID.
	// preassignHookContinuationGroup uses hookClaimMatchesRoute to filter, which only
	// matches unrouted beads via run_target when gc.kind=workflow.
	sibling := makeWorkflowRootBead("step-1", "pool-worker", "root-1", "group-a")

	var calls []queuedNudge
	ops := captureNudgeOps(t, &calls, root, sibling)

	var stdout, stderr bytes.Buffer
	// The claim env carries the concrete session generation; the enqueued nudge
	// must be fenced to it (SessionID/ContinuationEpoch) so a recycled slot that
	// reuses the runtime session name cannot pick up a stale continuation nudge.
	code := doHookClaim("query", "", hookClaimOptions{
		Assignee:           "pool-worker/slot-0",
		IdentityCandidates: []string{"pool-worker/slot-0"},
		RouteTargets:       []string{"pool-worker"},
		Env:                []string{"GC_SESSION_ID=sess-123", "GC_CONTINUATION_EPOCH=2"},
		JSON:               true,
	}, ops, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}

	if len(calls) != 1 {
		t.Fatalf("EnqueueContinuationNudge called %d times, want exactly 1; stderr=%s", len(calls), stderr.String())
	}
	got := calls[0]
	if got.Agent != "pool-worker/slot-0" {
		t.Errorf("nudge.Agent = %q, want %q", got.Agent, "pool-worker/slot-0")
	}
	if got.Source != hookClaimContinuationNudgeSource {
		t.Errorf("nudge.Source = %q, want %q", got.Source, hookClaimContinuationNudgeSource)
	}
	if got.Message != hookClaimContinuationNudgeMessage {
		t.Errorf("nudge.Message = %q, want %q", got.Message, hookClaimContinuationNudgeMessage)
	}
	if got.SessionID != "sess-123" {
		t.Errorf("nudge.SessionID = %q, want %q (session-generation fence)", got.SessionID, "sess-123")
	}
	if got.ContinuationEpoch != "2" {
		t.Errorf("nudge.ContinuationEpoch = %q, want %q (session-generation fence)", got.ContinuationEpoch, "2")
	}
}

// TestHookClaimStepBeadDoesNotEnqueueContinuationNudge verifies that claiming a
// non-workflow step bead — even one that has continuation metadata and pre-assigns
// siblings — does not enqueue the hook-claim-continuation nudge. Only workflow
// root claims propel the session; step claims happen while the session is already
// active.
func TestHookClaimStepBeadDoesNotEnqueueContinuationNudge(t *testing.T) {
	// Step beads are routed to the concrete session name (set by
	// preassignHookContinuationGroup on the prior root claim), so the route
	// target here is the session name, not the pool template.
	step := makeStepBead("step-1", "pool-worker/slot-0", "root-1", "group-a")
	sibling := beads.Bead{
		ID:     "step-2",
		Status: "open",
		Metadata: map[string]string{
			"gc.kind":               "step",
			"gc.routed_to":          "pool-worker/slot-0",
			"gc.root_bead_id":       "root-1",
			"gc.continuation_group": "group-a",
		},
	}

	var calls []queuedNudge
	ops := captureNudgeOps(t, &calls, step, sibling)

	var stdout, stderr bytes.Buffer
	// A step bead's route target is the concrete session name (pool-worker/slot-0),
	// not the pool template name; include both to mirror real hook invocation.
	code := doHookClaim("query", "", hookClaimOptions{
		Assignee:           "pool-worker/slot-0",
		IdentityCandidates: []string{"pool-worker/slot-0"},
		RouteTargets:       []string{"pool-worker/slot-0", "pool-worker"},
		JSON:               true,
	}, ops, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}

	if len(calls) != 0 {
		t.Errorf("EnqueueContinuationNudge called %d times for step-bead claim, want 0", len(calls))
	}
}

// TestHookClaimExistingAssignmentDoesNotEnqueueContinuationNudge verifies that
// re-finding a workflow root that is already assigned to the claiming session
// (idempotent re-find on retry) does not enqueue an additional continuation
// nudge. The session is already active; a second nudge would cause a redundant
// hook re-entry.
func TestHookClaimExistingAssignmentDoesNotEnqueueContinuationNudge(t *testing.T) {
	root := beads.Bead{
		ID:       "root-1",
		Status:   "in_progress",
		Assignee: "pool-worker/slot-0",
		Metadata: map[string]string{
			"gc.kind":               "workflow",
			"gc.run_target":         "pool-worker",
			"gc.root_bead_id":       "root-1",
			"gc.continuation_group": "group-a",
		},
	}
	output, err := json.Marshal([]beads.Bead{root})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var calls []queuedNudge
	sibling := beads.Bead{
		ID:     "step-1",
		Status: "open",
		Metadata: map[string]string{
			"gc.kind":               "step",
			"gc.run_target":         "pool-worker",
			"gc.root_bead_id":       "root-1",
			"gc.continuation_group": "group-a",
		},
	}
	ops := hookClaimOps{
		Runner: func(string, string) (string, error) { return string(output), nil },
		// Claim is not called for existing assignments; provide a no-op to satisfy applyDefaults.
		Claim: func(_ context.Context, _ string, _ []string, _, _ string) (beads.Bead, bool, error) {
			return beads.Bead{}, false, nil
		},
		ListContinuation: func(_ context.Context, _ string, _ []string, _, _ string) ([]beads.Bead, error) {
			return []beads.Bead{sibling}, nil
		},
		AssignContinuation: func(_ context.Context, _ string, _ []string, _, _ string) error {
			return nil
		},
		DrainAck:          func(io.Writer) error { return nil },
		ResolveWorkBranch: func(string) string { return "" },
		StampWorkBranch:   func(_ context.Context, _ string, _ []string, _, _, _ string) error { return nil },
		RecordSessionPointers: func(_ context.Context, _ string, _ []string, _, _, _, _ string) error {
			return nil
		},
		EnqueueContinuationNudge: func(_ string, item queuedNudge) error {
			calls = append(calls, item)
			return nil
		},
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("query", "", hookClaimOptions{
		Assignee:           "pool-worker/slot-0",
		IdentityCandidates: []string{"pool-worker/slot-0"},
		RouteTargets:       []string{"pool-worker"},
		JSON:               true,
	}, ops, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}

	if len(calls) != 0 {
		t.Errorf("EnqueueContinuationNudge called %d times for re-found assignment, want 0", len(calls))
	}
}

// TestHookClaimZeroContinuationDoesNotEnqueueContinuationNudge verifies that
// claiming a workflow root where no continuation siblings are available — either
// because the formula has no steps or all step beads are already assigned —
// does not enqueue the nudge. There is nothing to propel into.
func TestHookClaimZeroContinuationDoesNotEnqueueContinuationNudge(t *testing.T) {
	root := makeWorkflowRootBead("root-1", "pool-worker", "root-1", "group-a")

	var calls []queuedNudge
	// sibling is empty: ListContinuation returns nothing.
	ops := captureNudgeOps(t, &calls, root, beads.Bead{})

	var stdout, stderr bytes.Buffer
	code := doHookClaim("query", "", hookClaimOptions{
		Assignee:           "pool-worker/slot-0",
		IdentityCandidates: []string{"pool-worker/slot-0"},
		RouteTargets:       []string{"pool-worker"},
		JSON:               true,
	}, ops, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}

	if len(calls) != 0 {
		t.Errorf("EnqueueContinuationNudge called %d times for zero-continuation claim, want 0", len(calls))
	}
}

// concreteSlotNudgeTarget builds the nudgeTarget a running pool slot resolves to
// (mirroring resolveNudgeTargetFromSessionBead): its identity is the pool
// template name and its sessionName is the concrete slot, so queueKeys() carries
// BOTH the template and the concrete name.
func concreteSlotNudgeTarget(sessionID, epoch string) nudgeTarget {
	return nudgeTarget{
		cityPath:          "/city",
		identity:          "pool-worker",        // template name (agent_name/template on the session bead)
		sessionID:         sessionID,            // concrete generation
		continuationEpoch: epoch,                //
		sessionName:       "pool-worker/slot-0", // concrete runtime session
	}
}

// TestConcreteSessionMatchesTemplateSlingNudge documents that the pre-existing
// sling-time nudge — enqueued under the pool TEMPLATE name before the concrete
// slot existed — is NOT "pruned as undeliverable" for the concrete session.
// Because a pool slot's queueKeys() includes the template identity, the slot's
// poller claims the template-named sling nudge just as it claims the
// concrete-named, fenced continuation nudge. This is the mechanism behind the
// reviewer's double-nudge concern on PR #3833.
func TestConcreteSessionMatchesTemplateSlingNudge(t *testing.T) {
	target := concreteSlotNudgeTarget("sess-123", "2")

	// The sling nudge predates the concrete slot, so it carries the template
	// agent name and no session fence.
	slingNudge := queuedNudge{Agent: "pool-worker", Source: "sling", Message: hookClaimContinuationNudgeMessage}
	// The continuation nudge targets the concrete slot and is fenced to it.
	continuationNudge := queuedNudge{
		Agent:             "pool-worker/slot-0",
		Source:            hookClaimContinuationNudgeSource,
		Message:           hookClaimContinuationNudgeMessage,
		SessionID:         "sess-123",
		ContinuationEpoch: "2",
	}

	// Claimable: the concrete slot matches BOTH (so the sling nudge is not pruned).
	if !queuedNudgeClaimableForTarget(target, slingNudge) {
		t.Errorf("concrete slot does NOT claim the template-named sling nudge; it would if 'pruned as undeliverable' were accurate")
	}
	if !queuedNudgeClaimableForTarget(target, continuationNudge) {
		t.Errorf("concrete slot does not claim its own fenced continuation nudge")
	}
	// Delivery fence: both pass for this generation (the unfenced sling nudge is
	// exempt; the continuation nudge matches sess-123/epoch 2).
	if !queuedNudgeMatchesTargetFence(target, slingNudge) {
		t.Errorf("unfenced sling nudge unexpectedly rejected by the generation fence")
	}
	if !queuedNudgeMatchesTargetFence(target, continuationNudge) {
		t.Errorf("fenced continuation nudge rejected by its own generation fence")
	}

	// The two nudges differ in Source and neither carries a Reference, so the
	// (agent, source, reference) supersession at enqueue cannot collapse them.
	if slingNudge.Source == continuationNudge.Source {
		t.Errorf("test premise broken: sources should differ (%q vs %q)", slingNudge.Source, continuationNudge.Source)
	}
	if continuationNudge.Reference != nil {
		t.Errorf("continuation nudge has a Reference %v; it must be nil so the test reflects production", continuationNudge.Reference)
	}

	// A recycled slot (new session id) must NOT pick up the fenced continuation nudge.
	recycled := concreteSlotNudgeTarget("sess-999", "1")
	if queuedNudgeClaimableForTarget(recycled, continuationNudge) {
		t.Errorf("recycled slot (sess-999) wrongly claims a continuation nudge fenced to sess-123")
	}
}

// TestSlingAndContinuationNudgesCoalesceForConcreteSession documents the
// mitigating reality behind the double-nudge concern: when the poller claims
// both the template sling nudge and the concrete continuation nudge in one tick,
// tryDeliverQueuedNudgesByPoller renders them through formatNudgeInjectOutput
// into a SINGLE delivered message (one hook re-entry), not two separate wakes.
// The two identical "Work slung" lines are cosmetic, not a double propulsion.
func TestSlingAndContinuationNudgesCoalesceForConcreteSession(t *testing.T) {
	items := []queuedNudge{
		{Agent: "pool-worker", Source: "sling", Message: hookClaimContinuationNudgeMessage},
		{Agent: "pool-worker/slot-0", Source: hookClaimContinuationNudgeSource, Message: hookClaimContinuationNudgeMessage, SessionID: "sess-123"},
	}
	out := formatNudgeInjectOutput(items)

	if got := strings.Count(out, "</system-reminder>"); got != 1 {
		t.Errorf("formatNudgeInjectOutput emitted %d reminder blocks, want 1 (single coalesced delivery):\n%s", got, out)
	}
	if !strings.Contains(out, "[sling]") {
		t.Errorf("coalesced message missing the sling source tag:\n%s", out)
	}
	if !strings.Contains(out, "["+hookClaimContinuationNudgeSource+"]") {
		t.Errorf("coalesced message missing the continuation source tag:\n%s", out)
	}
	if got := strings.Count(out, hookClaimContinuationNudgeMessage); got != 2 {
		t.Errorf("coalesced message contains the propulsion line %d times, want 2 (both nudges, one delivery):\n%s", got, out)
	}
}
