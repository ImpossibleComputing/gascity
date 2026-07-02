package main

import (
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
)

// graphDemandReadyReader is a CachedReader/LiveReader whose Ready returns a
// fixed slice. In these tests it stands in for the FULL FEDERATED ready set
// under graph_store=sqlite: the per-tick Limit has already evicted a
// genuinely-ready graph wisp because the Dolt work leg filled the window.
type graphDemandReadyReader struct {
	ready []beads.Bead
}

func (r graphDemandReadyReader) Get(string) (beads.Bead, error)              { return beads.Bead{}, nil }
func (r graphDemandReadyReader) List(beads.ListQuery) ([]beads.Bead, error)  { return nil, nil }
func (r graphDemandReadyReader) DepList(string, string) ([]beads.Dep, error) { return nil, nil }
func (r graphDemandReadyReader) Ready(...beads.ReadyQuery) ([]beads.Bead, error) {
	return append([]beads.Bead(nil), r.ready...), nil
}

// graphDemandGraphOnlyReader returns the ClassGraph ready slice alone, where the
// assigned wisp survives the per-tick limit because the Dolt work leg is excluded.
type graphDemandGraphOnlyReader struct {
	ready []beads.Bead
}

func (r graphDemandGraphOnlyReader) ReadyGraphOnly(...beads.ReadyQuery) ([]beads.Bead, error) {
	return append([]beads.Bead(nil), r.ready...), nil
}

// graphDemandStore models a graph_store=sqlite Router store: the federated
// Cached/Live Ready returns a work-leg backlog that has truncated a
// genuinely-ready graph wisp out of the per-tick limit window, while the
// graph-only capability returns the wisp. The controller-demand readiness
// probes must prefer the graph-only slice (mirroring readyStoreSet) so an
// assigned, deps-satisfied wisp is recognized as ready. When hasGraph is false
// (identity phase, no distinct ClassGraph backend) the capability is absent and
// the federated path must be used byte-identically.
type graphDemandStore struct {
	beads.Store
	federated []beads.Bead
	graphOnly []beads.Bead
	hasGraph  bool
}

func (s *graphDemandStore) Handles() beads.StoreHandles {
	r := graphDemandReadyReader{ready: s.federated}
	return beads.StoreHandles{Cached: r, Live: r}
}

func (s *graphDemandStore) ReadyGraphOnlyHandle() (beads.GraphOnlyReadyStore, bool) {
	if !s.hasGraph {
		return nil, false
	}
	return graphDemandGraphOnlyReader{ready: s.graphOnly}, true
}

func graphDemandContains(rows []beads.Bead, id string) bool {
	for _, b := range rows {
		if b.ID == id {
			return true
		}
	}
	return false
}

// The assigned cleanup wisp, ready (its sole blocks-dep closed), but evicted
// from the federated per-tick limit window by older work-leg beads.
func graphDemandWisp() beads.Bead {
	return beads.Bead{ID: "gcg-1590", Status: "open", Assignee: "mc-test"}
}

func TestLiveReadyForControllerDemandPrefersGraphOnly(t *testing.T) {
	store := &graphDemandStore{
		federated: []beads.Bead{{ID: "work-1", Status: "open"}, {ID: "work-2", Status: "open"}},
		graphOnly: []beads.Bead{graphDemandWisp()},
		hasGraph:  true,
	}
	got, err := liveReadyForControllerDemandQuery(store, beads.ReadyQuery{Assignee: "mc-test", Limit: 5})
	if err != nil {
		t.Fatalf("liveReadyForControllerDemandQuery: %v", err)
	}
	if !graphDemandContains(got, "gcg-1590") {
		t.Fatalf("expected graph-only ready slice to contain the assigned wisp gcg-1590, got %v", got)
	}
}

func TestControllerDemandReadyFallsBackWithoutGraphCapability(t *testing.T) {
	// Identity phase: no distinct ClassGraph backend -> GraphOnlyReadyFor=false
	// -> the federated Live.Ready set is returned byte-identically, so default
	// (Dolt-only) cities are unaffected by the fix.
	store := &graphDemandStore{
		federated: []beads.Bead{{ID: "work-1", Status: "open"}},
		graphOnly: []beads.Bead{graphDemandWisp()},
		hasGraph:  false,
	}
	got, err := liveReadyForControllerDemandQuery(store, beads.ReadyQuery{Limit: 5})
	if err != nil {
		t.Fatalf("liveReadyForControllerDemandQuery: %v", err)
	}
	if len(got) != 1 || got[0].ID != "work-1" {
		t.Fatalf("expected federated fallback [work-1], got %v", got)
	}
	if graphDemandContains(got, "gcg-1590") {
		t.Fatalf("graph-only wisp must NOT appear when the graph capability is absent")
	}
}

// TestDefaultScaleCheckCountsSeesGraphResidentRoutedWork is the routed-UNASSIGNED
// pool-demand regression for the wake-from-sleep bug: on a graph_store=sqlite city
// the federated Ready read unions the Dolt work-leg backlog and truncates the
// genuinely-ready graph-resident (gcg-) routed-unassigned bead out of the per-tick
// window, so defaultScaleCheckCounts saw 0 demand and the asleep pool worker was
// never woken to self-claim. The probe must read the graph-only ready slice
// (mirroring the assigned-work path) so the routed bead drives pool demand. The
// pre-fix probe read the federated slice; this asserts counts==1, so it fails
// until readyForControllerDemand prefers the graph-only ready slice.
func TestDefaultScaleCheckCountsSeesGraphResidentRoutedWork(t *testing.T) {
	const template = "gascity/gc.run-operator"
	store := &graphDemandStore{
		// Federated read: only the Dolt work-leg backlog survived the per-tick
		// window; the routed graph bead was evicted.
		federated: []beads.Bead{{ID: "work-1", Status: "open"}, {ID: "work-2", Status: "open"}},
		// Graph-only read: the routed-unassigned graph bead is present.
		graphOnly: []beads.Bead{{
			ID:       "gcg-2024",
			Type:     "task",
			Status:   "open",
			Metadata: map[string]string{"gc.routed_to": template},
		}},
		hasGraph: true,
	}

	counts, _, errs := defaultScaleCheckCounts([]defaultScaleCheckTarget{{
		template: template,
		storeKey: "rig:gascity",
		store:    store,
	}})
	if len(errs) != 0 {
		t.Fatalf("defaultScaleCheckCounts errs = %v", errs)
	}
	if got := counts[template]; got != 1 {
		t.Fatalf("defaultScaleCheckCounts[%q] = %d, want 1 (graph-resident routed-unassigned work must drive pool demand)", template, got)
	}
}

// TestCityStoreProbeForRigPoolWakesWarmGraphPool is the WARM-pool regression for
// the graph-resident rig-pool starve (maintainer-city gcg-42082 →
// gascity/gc.review-synthesizer): a rig-scoped pool's own Dolt rig store cannot
// surface graph-resident routed work (GraphOnlyReadyFor(rigStore)=false), so the
// graph demand only appears via the city Router store. The original gate probed
// the city store ONLY when the pool was cold (0 running sessions), so a
// warm-but-asleep/wedged rig pool never re-woke for ready graph steps. The probe
// must fire for a warm rig pool whenever the city store carries the ClassGraph
// capability, while staying byte-identical (cold-only) on default Dolt-only
// cities. Fails pre-fix (warm+graph returns false).
func TestCityStoreProbeForRigPoolWakesWarmGraphPool(t *testing.T) {
	graphCity := &graphDemandStore{hasGraph: true} // graph_store=sqlite city Router
	doltCity := &graphDemandStore{hasGraph: false} // default Dolt-only city
	rigTarget := defaultScaleCheckTarget{
		template: "gascity/gc.review-synthesizer",
		storeKey: "rig:gascity",
		store:    &graphDemandStore{hasGraph: false}, // rig Dolt store, distinct instance
	}

	// WARM + graph-capable city: must probe (the fix). Pre-fix returned false.
	if !cityStoreProbeForRigPool(false, graphCity, rigTarget) {
		t.Fatal("warm rig pool on a graph_store=sqlite city must probe the city store for graph-resident routed demand")
	}
	// WARM + Dolt-only city: must NOT probe (byte-identical default behavior).
	if cityStoreProbeForRigPool(false, doltCity, rigTarget) {
		t.Fatal("warm rig pool on a Dolt-only city must not probe the city store (preserve default behavior)")
	}
	// COLD: always probes (cross-store cold-wake), regardless of graph capability.
	if !cityStoreProbeForRigPool(true, doltCity, rigTarget) {
		t.Fatal("cold rig pool must probe the city store (cross-store cold-wake)")
	}
	// Guard: a city-scoped pool's own target is already the city store.
	cityScoped := defaultScaleCheckTarget{template: "x", storeKey: "city", store: graphCity}
	if cityStoreProbeForRigPool(false, graphCity, cityScoped) {
		t.Fatal("city-scoped pool must not add a redundant city-store probe")
	}
	// Guard: a rig store aliasing the city store must not double-count.
	aliased := defaultScaleCheckTarget{template: "x", storeKey: "rig:x", store: graphCity}
	if cityStoreProbeForRigPool(false, graphCity, aliased) {
		t.Fatal("rig store aliasing the city store must not add a duplicate probe")
	}
}
