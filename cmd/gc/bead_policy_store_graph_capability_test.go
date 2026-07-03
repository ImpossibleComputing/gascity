package main

import (
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/coordrouter"
)

// TestBeadPolicyStoreForwardsGraphOnlyCapabilities pins the production store
// shape: the controller runs wrapStoreWithBeadPolicies(Router) (via
// routedPolicyStore), and the reconciler/orphan-heal/dispatcher fast paths gate
// on beads.GraphOnlyReadyFor AND beads.GraphOnlyListFor. A wrapper that forwards
// Ready but drops List silently disables the orphan heal (08cdd75f3), the
// close/recycle graph scope (2a83e20bd), liveListForRoot (df3f274ce), and the
// reconciler's in-progress drain guard — every one of those probed the wrong,
// federated leg in production while every test used a fake that implemented the
// handle directly. This table goes through the real wrapper, so a dropped
// forwarder fails CI.
func TestBeadPolicyStoreForwardsGraphOnlyCapabilities(t *testing.T) {
	cases := []struct {
		name       string
		build      func(t *testing.T) beads.Store
		wantReady  bool
		wantList   bool
		wantPrefix string
	}{
		{
			// The PRODUCTION graph_store=sqlite shape: policy(Router(mem work + SQLite graph)).
			name: "sqlite graph store wrapper",
			build: func(t *testing.T) beads.Store {
				return routedPolicyStore(beads.NewMemStore(), graphSQLiteCfg(), t.TempDir())
			},
			wantReady: true, wantList: true, wantPrefix: graphStoreIDPrefix,
		},
		{
			// Identity-phase Router: a Router with no distinct ClassGraph backend.
			// The capability is present (Router implements the handles directly and
			// falls back to the federated read); the prefix is "".
			name: "identity-phase router wrapper",
			build: func(t *testing.T) beads.Store {
				return wrapStoreWithBeadPolicies(coordrouter.New(beads.NewMemStore()), &config.City{})
			},
			wantReady: true, wantList: true, wantPrefix: "",
		},
		{
			// Default city, no Router: both capabilities absent — byte-identical to
			// the pre-split behavior, so consumers keep their federated paths.
			name: "default city no router",
			build: func(t *testing.T) beads.Store {
				return routedPolicyStore(beads.NewMemStore(), &config.City{}, t.TempDir())
			},
			wantReady: false, wantList: false, wantPrefix: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := tc.build(t)
			t.Cleanup(func() { _ = closeBeadStoreHandle(store) })

			if _, ok := beads.GraphOnlyReadyFor(store); ok != tc.wantReady {
				t.Fatalf("GraphOnlyReadyFor through the policy wrapper: ok=%v, want %v", ok, tc.wantReady)
			}
			gol, ok := beads.GraphOnlyListFor(store)
			if ok != tc.wantList {
				t.Fatalf("GraphOnlyListFor through the policy wrapper: ok=%v, want %v — a dropped List handle silently disables the orphan heal, close/recycle graph scope, liveListForRoot, and the reconciler assigned-work scope", ok, tc.wantList)
			}
			if ok {
				if got := gol.GraphIDPrefix(); got != tc.wantPrefix {
					t.Fatalf("GraphIDPrefix through the policy wrapper = %q, want %q", got, tc.wantPrefix)
				}
			}
		})
	}
}

// tierRecordingGraphLister is a graph-only-list store that records the TierMode
// of the query it receives, so we can prove the policy forwarder applies the
// same read-tier expansion the rest of the policy read surface does. Graph steps
// are no-history tier, so an unexpanded (TierIssues) query would miss them — the
// handle would be present but return nothing, a subtler version of the drop bug.
type tierRecordingGraphLister struct {
	*beads.MemStore
	gotTier beads.TierMode
	called  bool
}

func (r *tierRecordingGraphLister) ListGraphOnly(q beads.ListQuery) ([]beads.Bead, error) {
	r.gotTier = q.TierMode
	r.called = true
	return nil, nil
}

func (r *tierRecordingGraphLister) GraphIDPrefix() string { return graphStoreIDPrefix }

// TestBeadPolicyStoreGraphOnlyListExpandsReadTier proves the forwarded handle is
// not a bare pass-through: it expands the read tier (TierIssues -> TierBoth) just
// like beadPolicyStore.List, so no-history graph steps are visible.
func TestBeadPolicyStoreGraphOnlyListExpandsReadTier(t *testing.T) {
	inner := &tierRecordingGraphLister{MemStore: beads.NewMemStore()}
	store := wrapStoreWithBeadPolicies(inner, &config.City{})

	gol, ok := beads.GraphOnlyListFor(store)
	if !ok {
		t.Fatal("policy wrapper must forward the GraphOnlyList capability")
	}
	if _, err := gol.ListGraphOnly(beads.ListQuery{}); err != nil {
		t.Fatalf("ListGraphOnly: %v", err)
	}
	if !inner.called {
		t.Fatal("inner ListGraphOnly was never called through the policy wrapper")
	}
	if inner.gotTier != beads.TierBoth {
		t.Fatalf("policy read-tier expansion missing: forwarded query TierMode = %v, want TierBoth (no-history graph steps would be invisible)", inner.gotTier)
	}
}
