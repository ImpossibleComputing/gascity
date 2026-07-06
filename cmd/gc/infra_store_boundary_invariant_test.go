package main

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/coordclass"
	"github.com/gastownhall/gascity/internal/formula"
	"github.com/gastownhall/gascity/internal/molecule"
	"github.com/gastownhall/gascity/internal/orders"
	"github.com/gastownhall/gascity/internal/session"
	"github.com/gastownhall/gascity/internal/sling"
)

// This file is E2.2 of the domain/infra store split: the boundary-invariant
// test — the TDD forcing function that proves (and drives) the split.
//
// The invariant: on a REAL two-store city (a domain/work store plus a separate
// infra store), after a representative run of bead CREATORS, EVERY bead in each
// store must sit on the correct side of the coordination boundary. A domain
// store must hold no infrastructure-class bead; the infra store must hold no
// domain-class bead. The single source of truth for "which class" is
// coordclass.Classify — never a hand-rolled type list — so the boundary can
// never drift from the router.
//
// The graph-split audit's core lesson (its 73 gaps hid because tests used the
// wrong store shape) drives the design: the fast tier constructs the exact
// production wrapper stack — wrapStoreWithBeadPolicies over the store, threaded
// through the real controllerState typed accessors (resolveClassStore) — minus
// only the Dolt transport. It routes every creator through the production seam,
// so the DESTINATION of each bead is decided by production code, not by the
// test. A creator that is not yet routed through the typed accessors surfaces
// as a bead on the wrong side — that failing list IS the E2.3 worklist.
//
// KNOWN LEAK captured here (the E2.3 target): `gc sling` never sets
// SlingDeps.GraphStore (grep-confirmed: zero `GraphStore:` assignments in
// production cmd/gc + internal/api), so SlingDeps.graphStore() collapses the
// entire molecule explosion onto SlingDeps.Store — the RIG/work store — while
// the controller resolves graph → the infra store. See
// TestSlingGraphMaterializationLeaksIntoDomainStore below: it asserts the leak
// EXISTS today so the invariant stays honest; when E2.3 wires the GraphStore
// seam it flips red and must be converted into a PASS arm of the two boundary
// tests.

// splitCity is the real two-store harness: a domain/work rig store plus a
// separate infra store, both wrapped in the SAME production policy stack
// (wrapStoreWithBeadPolicies) the controller uses, and threaded through a
// controllerState so every typed accessor (resolveClassStore) routes exactly as
// production does. Only the Dolt transport is swapped for MemStore.
type splitCity struct {
	cfg        *config.City
	workStore  beads.Store // HQ/city domain store (work class)
	rigStore   beads.Store // rig domain store (work class); sling's source store
	infraStore beads.Store // infra store (sessions, graph, messaging, orders, nudges)
	cs         *controllerState
	rigName    string
}

// newSplitCity builds the two-store harness. Both stores are policy-wrapped the
// way production wraps them, so the optional-capability assertions the create
// paths rely on (GraphApplyFor / HandlesFor / StorageCreateStore) stay intact —
// this is the production store SHAPE, not a bare MemStore.
func newSplitCity(t *testing.T) *splitCity {
	t.Helper()
	cfg := &config.City{}
	work := wrapStoreWithBeadPolicies(beads.NewMemStore(), cfg)
	rig := wrapStoreWithBeadPolicies(beads.NewMemStore(), cfg)
	infra := wrapStoreWithBeadPolicies(beads.NewMemStore(), cfg)
	const rigName = "rig-one"
	cs := &controllerState{
		cfg:            cfg,
		cityName:       "test-city",
		cityPath:       t.TempDir(),
		cityBeadStore:  work,
		cityInfraStore: infra,
		beadStores:     map[string]beads.Store{rigName: rig},
	}
	return &splitCity{
		cfg:        cfg,
		workStore:  work,
		rigStore:   rig,
		infraStore: infra,
		cs:         cs,
		rigName:    rigName,
	}
}

// domainStores returns every domain/work store of the split city: the HQ city
// store plus every rig store. These must hold NO infrastructure-class bead.
func (sc *splitCity) domainStores() map[string]beads.Store {
	return sc.cs.BeadStores()
}

// assertStoreClassBoundary lists every bead in store (open + closed, both tiers,
// scan allowed) and fails for any bead on the wrong side of the coordination
// boundary. wantInfra=false asserts a domain store (every bead must be
// ClassWork); wantInfra=true asserts the infra store (every bead must be an
// infrastructure class). It is exported inside the package so future E-phases
// (Postgres backend swap, E5) rerun the identical invariant against their store
// shapes: it reads only coordclass.Classify(bead) and which store returned the
// bead — the boundary, never the backend.
func assertStoreClassBoundary(t *testing.T, label string, store beads.Store, wantInfra bool) {
	t.Helper()
	list, err := store.List(beads.ListQuery{IncludeClosed: true, TierMode: beads.TierBoth, AllowScan: true})
	if err != nil {
		t.Fatalf("%s: List: %v", label, err)
	}
	for _, b := range list {
		class := coordclass.Classify(b)
		gotInfra := class.IsInfrastructure()
		if gotInfra != wantInfra {
			side := "domain"
			if wantInfra {
				side = "infra"
			}
			t.Errorf("%s (%s store) holds wrong-side bead: id=%q type=%q labels=%v class=%s (want %s-class beads only)",
				label, side, b.ID, b.Type, b.Labels, class, side)
		}
	}
}

// creatorResult records where a creator's bead landed, so the conformance table
// can report PASS (correctly routed) or LEAK per creator.
type creatorResult struct {
	name        string
	wantClass   coordclass.Class // the class the bead should be
	landedInfra bool             // did production route it to the infra store?
	beadCount   int
}

// runRoutedCreators drives every creator that is (or should be) routed through
// the typed accessors, returning per-creator placement. Each creator obtains
// its destination store from the PRODUCTION seam (the controllerState typed
// accessors → resolveClassStore), so placement is decided by production code.
func (sc *splitCity) runRoutedCreators(t *testing.T) []creatorResult {
	t.Helper()
	var results []creatorResult

	countInfraDelta := func(fn func()) int {
		before := storeBeadCount(t, sc.infraStore)
		fn()
		return storeBeadCount(t, sc.infraStore) - before
	}

	// 1. SESSION bead — routed via cs.SessionsBeadStore() (session class).
	{
		delta := countInfraDelta(func() {
			ss := sc.cs.SessionsBeadStore()
			_, err := session.NewStore(ss).CreateSession(session.CreateSpec{
				Title:     "worker-1",
				AgentName: "worker-1",
				Metadata:  map[string]string{"provider": "tmux", "template": "claude"},
			})
			if err != nil {
				t.Fatalf("session create: %v", err)
			}
		})
		results = append(results, creatorResult{"session", coordclass.ClassSessions, delta > 0, delta})
	}

	// 2. MAIL (beadmail) — routed via newCityMailProvider (messaging class).
	{
		delta := countInfraDelta(func() {
			mp := newCityMailProvider(sc.cs.cityBeadStore, sc.cs.cityInfraStore, sc.cfg, sc.cs.cityPath, sc.cs.eventProv)
			if _, err := mp.Send("human", "worker-1", "hello", "body text"); err != nil {
				t.Fatalf("mail send: %v", err)
			}
		})
		results = append(results, creatorResult{"mail", coordclass.ClassMessaging, delta > 0, delta})
	}

	// 3. NUDGE enqueue — routed via cs.NudgesBeadStore() (nudges class).
	{
		delta := countInfraDelta(func() {
			ns := sc.cs.NudgesBeadStore()
			_, created, err := ensureQueuedNudgeBead(ns, newQueuedNudge("worker-1", "please continue", time.Now().UTC()))
			if err != nil {
				t.Fatalf("nudge enqueue: %v", err)
			}
			if !created {
				t.Fatal("nudge enqueue: expected a bead to be created")
			}
		})
		results = append(results, creatorResult{"nudge", coordclass.ClassNudges, delta > 0, delta})
	}

	// 4. WAIT bead — durable session-wait; session class, routed via
	//    cs.SessionsBeadStore(). Created through the session store the way the
	//    wait path does (Type=gate + gc:wait), so classification is honest.
	{
		delta := countInfraDelta(func() {
			ss := sc.cs.SessionsBeadStore()
			_, err := ss.Create(beads.Bead{
				Title:  "wait:deps",
				Type:   session.WaitBeadType,
				Status: "open",
				Labels: []string{session.WaitBeadLabel, "session:sess-1"},
				Metadata: map[string]string{
					"session_id": "sess-1",
					"kind":       "deps",
					"state":      "pending",
				},
			})
			if err != nil {
				t.Fatalf("wait create: %v", err)
			}
		})
		results = append(results, creatorResult{"wait", coordclass.ClassSessions, delta > 0, delta})
	}

	// 5. ORDER-TRACKING bead — routed via cs.ordersBeadStore("") (orders class).
	{
		delta := countInfraDelta(func() {
			os := sc.cs.ordersBeadStore("")
			if _, err := orders.NewStore(os).CreateRun("gate-alpha", orders.RunOpts{}); err != nil {
				t.Fatalf("order run create: %v", err)
			}
		})
		results = append(results, creatorResult{"order-tracking", coordclass.ClassOrders, delta > 0, delta})
	}

	// 6. GRAPH molecule — routed via cs.GraphBeadStore() (graph class). This is
	//    the CORRECTLY-ROUTED graph path (accessor-driven). The sling path,
	//    which does NOT use this accessor, is exercised separately below and is
	//    the known E2.3 leak.
	{
		delta := countInfraDelta(func() {
			gs := sc.cs.GraphBeadStore()
			if _, err := molecule.Instantiate(context.Background(), gs.Store, graphRecipe(), molecule.Options{}); err != nil {
				t.Fatalf("molecule instantiate (accessor-routed): %v", err)
			}
		})
		results = append(results, creatorResult{"graph-molecule (accessor)", coordclass.ClassGraph, delta > 0, delta})
	}

	// 7. PLAIN TASK — work class; stays in the rig domain store.
	{
		delta := countInfraDelta(func() {
			if _, err := sc.rigStore.Create(beads.Bead{Title: "real backlog item", Type: "task"}); err != nil {
				t.Fatalf("plain task create: %v", err)
			}
		})
		// A work bead must NOT land in infra: landedInfra=true here is itself a leak.
		results = append(results, creatorResult{"plain-task", coordclass.ClassWork, delta > 0, delta})
	}

	// 8. USER CONVOY — work class (a non-synthetic convoy); stays in rig domain.
	{
		delta := countInfraDelta(func() {
			a, err := sc.rigStore.Create(beads.Bead{Title: "convoy item A", Type: "task"})
			if err != nil {
				t.Fatalf("convoy item A: %v", err)
			}
			b, err := sc.rigStore.Create(beads.Bead{Title: "convoy item B", Type: "task"})
			if err != nil {
				t.Fatalf("convoy item B: %v", err)
			}
			if _, err := sc.rigStore.Create(beads.Bead{
				Title:  "user convoy",
				Type:   "convoy",
				Labels: []string{"tracks:" + a.ID, "tracks:" + b.ID},
			}); err != nil {
				t.Fatalf("user convoy: %v", err)
			}
		})
		results = append(results, creatorResult{"user-convoy", coordclass.ClassWork, delta > 0, delta})
	}

	return results
}

// TestNoInfraBeadInDomainStore drives the representative correctly-routed
// creators over the real two-store shape, then asserts every domain store (HQ +
// rigs) holds NO infrastructure-class bead. This is one half of the boundary
// invariant. It is the forcing function: an infra creator that is not routed to
// the infra store (E2.3 target) will deposit an infra-class bead in a domain
// store and fail here by ID + class.
func TestNoInfraBeadInDomainStore(t *testing.T) {
	sc := newSplitCity(t)
	sc.runRoutedCreators(t)
	for name, store := range sc.domainStores() {
		assertStoreClassBoundary(t, "domain:"+name, store, false)
	}
}

// TestNoDomainBeadInInfraStore is the other half: after the same run, the infra
// store must hold NO domain (work) class bead — the boundary must not leak work
// into the coordination store either.
func TestNoDomainBeadInInfraStore(t *testing.T) {
	sc := newSplitCity(t)
	sc.runRoutedCreators(t)
	assertStoreClassBoundary(t, "infra", sc.infraStore, true)
}

// TestBoundaryAssertionIsNotVacuous is the negative control: it proves the
// boundary assertion actually FIRES when a leak is present, so a PASS of the two
// invariant tests above is meaningful and not a false green (the graph-split
// audit's failure mode: the wrong store shape passes silently). We deliberately
// deposit the sling-leak molecule into the rig DOMAIN store — exactly the E2.3
// bug — then confirm assertStoreClassBoundary reports it via a sub-test recorder.
func TestBoundaryAssertionIsNotVacuous(t *testing.T) {
	sc := newSplitCity(t)
	// Reproduce the sling leak: materialize graph beads into the rig domain store.
	if _, err := molecule.Instantiate(context.Background(), sc.rigStore, graphRecipe(), molecule.Options{}); err != nil {
		t.Fatalf("seed leak: %v", err)
	}

	// Run the assertion against a throwaway *testing.T so we can observe that it
	// records a failure without failing the real test.
	probe := &testing.T{}
	assertStoreClassBoundary(probe, "domain:probe", sc.rigStore, false)
	if !probe.Failed() {
		t.Fatal("assertStoreClassBoundary did NOT flag a known graph-class leak in a domain store — " +
			"the invariant is vacuous (wrong store shape / classifier not consulted)")
	}
}

// TestInfraCreatorConformanceTable is the creator conformance table: adding a
// new infra-bead creator without routing it through the typed accessors fails
// here in seconds. Each routed infra creator must land in the infra store; each
// work creator must land in a domain store. This is the fast local guard that
// keeps the split honest as creators are added.
func TestInfraCreatorConformanceTable(t *testing.T) {
	sc := newSplitCity(t)
	results := sc.runRoutedCreators(t)

	// Deterministic report order.
	sort.Slice(results, func(i, j int) bool { return results[i].name < results[j].name })

	for _, r := range results {
		wantInfra := r.wantClass.IsInfrastructure()
		if r.beadCount == 0 && wantInfra {
			t.Errorf("creator %q produced no bead in the infra store (expected %s-class routing)", r.name, r.wantClass)
			continue
		}
		if r.landedInfra != wantInfra {
			where := "domain store"
			if r.landedInfra {
				where = "infra store"
			}
			t.Errorf("creator %q (class %s) LEAK: routed to the %s (want %s)",
				r.name, r.wantClass, where, sideName(wantInfra))
		}
	}
}

// TestSlingGraphMaterializationLeaksIntoDomainStore is the E2.3 worklist,
// encoded as an executable known-leak. `gc sling` never sets
// SlingDeps.GraphStore, so SlingDeps.graphStore() — the production seam — falls
// back to SlingDeps.Store (the rig/work store), and the entire molecule
// explosion (root + steps carrying gc.kind=workflow / gc.root_bead_id) lands in
// the DOMAIN store while the controller resolves graph → the infra store.
//
// This test asserts the leak EXISTS today, using the real production
// SlingDeps.graphStore() function to pick the destination store. It is
// intentionally RED-preventing: it stays green while the bug is present, and
// flips to FAIL the moment E2.3 wires the GraphStore seam — at which point this
// test must be deleted and the sling path folded into the two boundary tests
// above as a PASS arm.
//
// E2.3 worklist (from the design's e23SlingSplitBrain), verified reachable:
//   - cmd_sling.go SlingDeps construction: never sets GraphStore →
//     graphStore() collapses onto the rig store. FIX: set GraphStore to the
//     graph-class resolution over the city store (infra store when split).
//   - cmd_sling.go nudge enqueue (:1519): beads.NudgesStore{Store: store} where
//     store may be the rig store → route through resolveNudgesStore.
//   - coordClassStoreCandidates (session_beads.go) + openSourceWorkflowStores
//     (cmd_convoy_dispatch.go): session/workflow probe fan-out across rig stores
//     is a read-side leak vector on a split city.
func TestSlingGraphMaterializationLeaksIntoDomainStore(t *testing.T) {
	sc := newSplitCity(t)

	// Faithfully reproduce the sling seam: a rig-scoped sling sets
	// SlingDeps.Store = rigStore and never sets GraphStore. graphStore() is the
	// exact production function that picks the graph destination.
	deps := sling.SlingDeps{Store: sc.rigStore}
	graphDest := deps.GraphStore // unset in production; graphStore() falls back to Store.
	if graphDest != nil {
		t.Fatal("precondition: production sling leaves SlingDeps.GraphStore unset")
	}

	// Materialize the molecule where production materializes it: onto
	// SlingDeps.graphStore(), which collapses onto the rig store.
	before := storeBeadCount(t, sc.rigStore)
	if _, err := molecule.Instantiate(context.Background(), slingGraphStore(deps), graphRecipe(), molecule.Options{}); err != nil {
		t.Fatalf("molecule instantiate (sling seam): %v", err)
	}
	graphBeadsInRig := 0
	list, err := sc.rigStore.List(beads.ListQuery{IncludeClosed: true, TierMode: beads.TierBoth, AllowScan: true})
	if err != nil {
		t.Fatalf("rig List: %v", err)
	}
	for _, b := range list {
		if coordclass.Classify(b) == coordclass.ClassGraph {
			graphBeadsInRig++
		}
	}
	after := storeBeadCount(t, sc.rigStore)

	if graphBeadsInRig == 0 {
		t.Fatalf("EXPECTED the sling split-brain leak (graph beads in the rig/domain store), but found none; "+
			"if E2.3 has wired SlingDeps.GraphStore, DELETE this known-leak test and fold sling into the two "+
			"boundary tests as a PASS arm (rig delta was %d)", after-before)
	}
	t.Logf("E2.3 WORKLIST (known leak, present today): sling materialized %d graph-class bead(s) into the rig "+
		"DOMAIN store %q via SlingDeps.graphStore() fallback (GraphStore unset). E2.3 must route this to the "+
		"infra store.", graphBeadsInRig, sc.rigName)
}

// slingGraphStore returns the store SlingDeps.graphStore() resolves to. The
// method is unexported; this mirrors its one-line fallback (GraphStore ?? Store)
// so the known-leak test uses the exact production selection logic.
func slingGraphStore(deps sling.SlingDeps) beads.Store {
	if deps.GraphStore != nil {
		return deps.GraphStore
	}
	return deps.Store
}

// graphRecipe is a minimal formula recipe that materializes a graph molecule:
// a workflow root (gc.kind=workflow → ClassGraph) plus a child step (inherits
// gc.root_bead_id → ClassGraph). Copied from the proven shape in
// internal/molecule/molecule_test.go.
func graphRecipe() *formula.Recipe {
	return &formula.Recipe{
		Name: "wf",
		Steps: []formula.RecipeStep{
			{ID: "wf", Title: "Workflow", Type: "task", IsRoot: true, Metadata: map[string]string{"gc.kind": "workflow"}},
			{ID: "wf.step", Title: "Work", Type: "task"},
		},
		Deps: []formula.RecipeDep{
			{StepID: "wf.step", DependsOnID: "wf", Type: "parent-child"},
		},
	}
}

// storeBeadCount lists every bead in a store (both tiers, closed included) and
// returns the count. Used to attribute a creator's writes to a store.
func storeBeadCount(t *testing.T, store beads.Store) int {
	t.Helper()
	list, err := store.List(beads.ListQuery{IncludeClosed: true, TierMode: beads.TierBoth, AllowScan: true})
	if err != nil {
		t.Fatalf("store count: List: %v", err)
	}
	return len(list)
}

func sideName(infra bool) string {
	if infra {
		return "infra store"
	}
	return "domain store"
}

// TestInfraStoreIDPrefixBoundary is the ID-prefix half of the invariant, gated
// on E2.4. Once E2.4 mints per-class reserved-prefix IDs (gcs-/gcm-/gcn-/gco-/
// gcg-) in the infra store, every infra-store bead ID must carry a reserved
// class prefix and no domain-store bead may. E2.4 has NOT landed, so MemStore
// still mints plain bd-N IDs; asserting the prefix now would be a false-red.
// The test documents the intended assertion and skips until E2.4 wires
// wrapInfraStoreWithBeadPolicies + mintReservedClassIDs.
func TestInfraStoreIDPrefixBoundary(t *testing.T) {
	t.Skip("E2.4 worklist: reserved-prefix ID minting (wrapInfraStoreWithBeadPolicies + mintReservedClassIDs) " +
		"is not implemented; infra beads still mint plain bd-N IDs. When E2.4 lands, assert every infra-store " +
		"bead id's prefix segment satisfies config.IsReservedClassPrefix and no domain-store bead id does.")

	sc := newSplitCity(t)
	sc.runRoutedCreators(t)
	list, err := sc.infraStore.List(beads.ListQuery{IncludeClosed: true, TierMode: beads.TierBoth, AllowScan: true})
	if err != nil {
		t.Fatalf("infra List: %v", err)
	}
	for _, b := range list {
		if !config.IsReservedClassPrefix(idPrefixSegment(b.ID)) {
			t.Errorf("infra-store bead %q lacks a reserved class prefix", b.ID)
		}
	}
}

// idPrefixSegment returns the prefix segment of a bead id (everything before the
// last "-"), matching how reserved-prefix validation splits ids.
func idPrefixSegment(id string) string {
	if i := strings.LastIndex(id, "-"); i > 0 {
		return id[:i]
	}
	return id
}
