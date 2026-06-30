package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
)

// TestDoHookClaimJSONSurfacesGraphVerifyFields pins the worker-side analog of the
// graph-resident demand fix: the claim JSON must carry root_bead_id and
// continuation_group so a worker can verify a graph-resident (gcg-) bead straight
// from the graph-aware `gc hook --claim --json` output, with NO second
// `bd show "$WORK_ID"`. The bare bd show was graph-aware only via the PATH shim
// and fell through to the Dolt bd ("no issue found") for a stale session, looping
// CLAIM_REJECTED forever and spinning the adopt-PR review-loop. The claim already
// reads these metadata keys (preassignHookContinuationGroup); this asserts they
// are now surfaced in the JSON the template consumes.
func TestDoHookClaimJSONSurfacesGraphVerifyFields(t *testing.T) {
	spy := &recordRunIDSpy{}
	ops := hookClaimOps{
		Runner: func(string, string) (string, error) {
			return `[{"id":"gcg-2024","status":"open","metadata":{"gc.routed_to":"worker"}}]`, nil
		},
		Claim: func(_ context.Context, _ string, _ []string, id, assignee string) (beads.Bead, bool, error) {
			return beads.Bead{ID: id, Status: "in_progress", Assignee: assignee, Metadata: map[string]string{
				"gc.routed_to":          "worker",
				"gc.root_bead_id":       "gcg-1900",
				"gc.continuation_group": "main",
			}}, true, nil
		},
		ListContinuation:      func(context.Context, string, []string, string, string) ([]beads.Bead, error) { return nil, nil },
		ResolveWorkBranch:     func(string) string { return "" }, // suppress work_branch stamp
		RecordSessionPointers: spy.fn,
	}
	opts := hookClaimOptions{
		Assignee:           "worker-1",
		IdentityCandidates: []string{"worker-1"},
		RouteTargets:       []string{"worker"},
		Env:                []string{"GC_SESSION_ID=sess-1"},
		JSON:               true,
	}

	var stdout, stderr bytes.Buffer
	if code := doHookClaim("bd ready --json", "/tmp/work", opts, ops, &stdout, &stderr); code != 0 {
		t.Fatalf("doHookClaim = %d, want 0; stderr=%s", code, stderr.String())
	}

	var res hookClaimJSONResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal claim JSON: %v; stdout=%s", err, stdout.String())
	}
	if res.BeadID != "gcg-2024" {
		t.Fatalf("bead_id = %q, want gcg-2024", res.BeadID)
	}
	if res.RootBeadID != "gcg-1900" {
		t.Fatalf("root_bead_id = %q, want gcg-1900 (worker must verify graph beads from the claim JSON, not bd show)", res.RootBeadID)
	}
	if res.ContinuationGroup != "main" {
		t.Fatalf("continuation_group = %q, want main", res.ContinuationGroup)
	}
}

// TestDoHookClaimJSONOmitsGraphFieldsWhenAbsent keeps the JSON byte-stable for a
// standalone bead with no root/group metadata (omitempty), so non-graph claims
// are unchanged.
func TestDoHookClaimJSONOmitsGraphFieldsWhenAbsent(t *testing.T) {
	spy := &recordRunIDSpy{}
	ops := hookClaimOps{
		Runner: func(string, string) (string, error) {
			return `[{"id":"hw-standalone","status":"open","metadata":{"gc.routed_to":"worker"}}]`, nil
		},
		Claim: func(_ context.Context, _ string, _ []string, id, assignee string) (beads.Bead, bool, error) {
			return beads.Bead{ID: id, Status: "in_progress", Assignee: assignee, Metadata: map[string]string{
				"gc.routed_to": "worker",
			}}, true, nil
		},
		ResolveWorkBranch:     func(string) string { return "" },
		RecordSessionPointers: spy.fn,
	}
	opts := hookClaimOptions{
		Assignee:           "worker-1",
		IdentityCandidates: []string{"worker-1"},
		RouteTargets:       []string{"worker"},
		Env:                []string{"GC_SESSION_ID=sess-1"},
		JSON:               true,
	}

	var stdout, stderr bytes.Buffer
	if code := doHookClaim("bd ready --json", "/tmp/work", opts, ops, &stdout, &stderr); code != 0 {
		t.Fatalf("doHookClaim = %d, want 0; stderr=%s", code, stderr.String())
	}
	if bytes.Contains(stdout.Bytes(), []byte("root_bead_id")) || bytes.Contains(stdout.Bytes(), []byte("continuation_group")) {
		t.Fatalf("claim JSON must omit graph-verify fields when absent; got %s", stdout.String())
	}
}
