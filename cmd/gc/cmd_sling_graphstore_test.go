package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/citylayout"
	"github.com/gastownhall/gascity/internal/coordclass"
	"github.com/gastownhall/gascity/internal/coordrouter"
)

// TestRegisterGraphStoreBackendFailsLoudNotDoltOnOpenError pins FIX L2: when the
// embedded SQLite graph store cannot be opened, registerGraphStoreBackend must
// register an erroring backend for the graph class rather than leaving it
// unregistered. Leaving it unregistered lets Router.Create fall through to the
// work (Dolt) backend, so a `gc sling` would silently write formula-v2 beads to
// Dolt and exit 0. The invariant is: graph-class writes FAIL LOUD, and the work
// backend receives nothing.
func TestRegisterGraphStoreBackendFailsLoudNotDoltOnOpenError(t *testing.T) {
	cfg := graphSQLiteCfg()
	cityPath := t.TempDir() // unique dir keeps graphStoreHandleCache clean

	// Make the SQLite graph path deterministically unopenable: pre-create
	// <city>/.gc/beads.sqlite as a DIRECTORY so OpenSQLiteStore's schema apply
	// fails (the DB file path is a directory).
	gcDir := filepath.Join(cityPath, citylayout.RuntimeRoot)
	if err := os.MkdirAll(filepath.Join(gcDir, "beads.sqlite"), 0o755); err != nil {
		t.Fatal(err)
	}

	work := beads.NewMemStore()
	router := coordrouter.New(work)
	registerGraphStoreBackend(router, cfg, cityPath)

	// A graph-classified bead (gc:wisp) must ERROR — not silently succeed into
	// the work backend.
	if _, err := router.Create(beads.Bead{Title: "wisp", Type: "task", Labels: []string{"gc:wisp"}}); err == nil {
		t.Fatal("graph create silently succeeded: formula beads misrouted to the work (Dolt) backend instead of failing loud")
	}

	// The work backend must have received nothing.
	workBeads, err := work.List(beads.ListQuery{AllowScan: true})
	if err != nil {
		t.Fatalf("work List: %v", err)
	}
	if len(workBeads) != 0 {
		t.Fatalf("work backend holds %d bead(s); a graph bead leaked to the work backend", len(workBeads))
	}
}

// TestOpenStoreResultAtForCityFailsWhenCityConfigUnloadable pins FIX L1/F2: when a
// city config exists and cannot be loaded (here: an unresolvable include) AND it
// opts into graph routing (graph_store="sqlite"), openStoreResultAtForCity must
// return the load error rather than swallow it. Swallowing yields a nil config,
// which disables graph routing (graphStoreSQLiteEnabled(nil)==false), so formula
// beads silently degrade to the work (Dolt) backend. The config is valid TOML —
// the failure is downstream of parse — so the narrowed guard reaches it via the
// minimal graph_store parse, not the "unparseable" fallback.
func TestOpenStoreResultAtForCityFailsWhenCityConfigUnloadable(t *testing.T) {
	cityDir := t.TempDir()
	// Valid TOML, graph_store set, but loadCityConfig fails on the missing include.
	cityTOML := "include = [\"does-not-exist.toml\"]\n\n[workspace]\nname = \"demo\"\n\n[beads]\nprovider = \"file\"\ngraph_store = \"sqlite\"\n"
	if err := os.WriteFile(filepath.Join(cityDir, citylayout.CityConfigFile), []byte(cityTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GC_BEADS", "file")
	if err := ensureScopedFileStoreLayout(cityDir); err != nil {
		t.Fatal(err)
	}
	if err := ensurePersistedScopeLocalFileStore(cityDir); err != nil {
		t.Fatal(err)
	}

	store, err := openStoreResultAtForCity(cityDir, cityDir)
	if err == nil {
		_ = closeBeadStoreHandle(store.Store)
		t.Fatal("openStoreResultAtForCity swallowed an unloadable graph_store city config: graph beads would silently degrade to the work backend")
	}
}

// TestOpenStoreResultAtForCityOpensWhenLoadFailsWithoutGraphStore pins FIX F2's
// narrowing: a loadCityConfig failure on a city that does NOT opt into graph
// routing (no [beads].graph_store) is routing-irrelevant, so the store open must
// still succeed rather than brick the whole bead plane (reads included) on a
// default-Dolt city. Same unresolvable-include failure as above, minus
// graph_store.
func TestOpenStoreResultAtForCityOpensWhenLoadFailsWithoutGraphStore(t *testing.T) {
	cityDir := t.TempDir()
	// Valid TOML, NO graph_store, but loadCityConfig fails on the missing include.
	cityTOML := "include = [\"does-not-exist.toml\"]\n\n[workspace]\nname = \"demo\"\n\n[beads]\nprovider = \"file\"\n"
	if err := os.WriteFile(filepath.Join(cityDir, citylayout.CityConfigFile), []byte(cityTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	// Sanity: the config really is unloadable (otherwise the test proves nothing).
	if _, err := loadCityConfig(cityDir, io.Discard); err == nil {
		t.Fatal("fixture precondition failed: expected loadCityConfig to error on the missing include")
	}
	t.Setenv("GC_BEADS", "file")
	if err := ensureScopedFileStoreLayout(cityDir); err != nil {
		t.Fatal(err)
	}
	if err := ensurePersistedScopeLocalFileStore(cityDir); err != nil {
		t.Fatal(err)
	}

	result, err := openStoreResultAtForCity(cityDir, cityDir)
	if err != nil {
		t.Fatalf("a routing-irrelevant load failure (no graph_store) must not brick the store open, got: %v", err)
	}
	t.Cleanup(func() { _ = closeBeadStoreHandle(result.Store) })
	if result.Store == nil {
		t.Fatal("expected a usable store despite the load failure")
	}
}

// TestOpenStoreResultAtForCityOpensWithoutCityConfig guards the out-of-city
// invariant that FIX L1 must NOT break: a store directory with NO city config is
// a legitimate out-of-city open and must still succeed. The L1 error only fires
// when a city config actually exists at the canonical path.
func TestOpenStoreResultAtForCityOpensWithoutCityConfig(t *testing.T) {
	dir := t.TempDir() // no city.toml
	t.Setenv("GC_BEADS", "file")
	if err := ensureScopedFileStoreLayout(dir); err != nil {
		t.Fatal(err)
	}
	if err := ensurePersistedScopeLocalFileStore(dir); err != nil {
		t.Fatal(err)
	}

	result, err := openStoreResultAtForCity(dir, dir)
	if err != nil {
		t.Fatalf("out-of-city open (no city.toml) must still succeed, got: %v", err)
	}
	t.Cleanup(func() { _ = closeBeadStoreHandle(result.Store) })
	if result.Store == nil {
		t.Fatal("expected a store for an out-of-city open")
	}
}

// TestSlingFormulaGraphBeadsLandInGraphLeg is the happy-path invariant: under
// graph_store=sqlite, a formula-v2 workflow bead routed through routedPolicyStore
// lands in the embedded SQLite graph leg, and the work (Dolt-analogue) leg holds
// nothing. This guards against a future regression where formula work is routed
// to the work backend.
func TestSlingFormulaGraphBeadsLandInGraphLeg(t *testing.T) {
	work := beads.NewMemStore() // stands in for the Dolt work backend
	store := routedPolicyStore(work, graphSQLiteCfg(), t.TempDir())
	t.Cleanup(func() { _ = closeBeadStoreHandle(store) })

	// A formula-v2 workflow root classifies ClassGraph via gc.kind=workflow.
	gb, err := store.Create(beads.Bead{
		Title:    "formula root",
		Type:     "task",
		Metadata: map[string]string{beadmeta.KindMetadataKey: beadmeta.KindWorkflow},
	})
	if err != nil {
		t.Fatalf("create formula graph bead: %v", err)
	}

	base, _, ok := unwrapBeadPolicyStore(store)
	if !ok {
		t.Fatal("expected the opted-in store to be policy-wrapped")
	}
	router, ok := base.(*coordrouter.Router)
	if !ok {
		t.Fatalf("graph_store=sqlite must insert a *coordrouter.Router, got %T", base)
	}
	sqliteBackend, ok := graphSQLiteBackend(router.Backend(coordclass.ClassGraph))
	if !ok {
		t.Fatalf("graph backend = %T, want an embedded SQLite store", router.Backend(coordclass.ClassGraph))
	}

	// The graph bead is in the SQLite leg...
	if _, err := sqliteBackend.Get(gb.ID); err != nil {
		t.Fatalf("formula bead %q not in the SQLite graph leg: %v", gb.ID, err)
	}
	// ...and the work leg holds nothing.
	workBeads, err := work.List(beads.ListQuery{AllowScan: true})
	if err != nil {
		t.Fatalf("work List: %v", err)
	}
	if len(workBeads) != 0 {
		t.Fatalf("work backend holds %d bead(s); a formula graph bead leaked to the work leg", len(workBeads))
	}
}

// TestLazyGraphStoreHealsAfterObstructionRemoved pins FIX F1: the self-healing
// backend errors while the SQLite graph store is unopenable, then succeeds once
// the obstruction clears and the backoff window passes — so a transient startup
// failure does not permanently poison the long-lived Router's graph leg.
func TestLazyGraphStoreHealsAfterObstructionRemoved(t *testing.T) {
	dir := filepath.Join(t.TempDir(), citylayout.RuntimeRoot)
	sqlitePath := filepath.Join(dir, "beads.sqlite")
	// Obstruct: beads.sqlite is a DIRECTORY, so OpenSQLiteStore fails.
	if err := os.MkdirAll(sqlitePath, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graphStoreHandleCache.Delete(dir) })

	oldBackoff := lazyGraphStoreRetryBackoff
	lazyGraphStoreRetryBackoff = 20 * time.Millisecond
	t.Cleanup(func() { lazyGraphStoreRetryBackoff = oldBackoff })

	s := newLazyGraphStore(dir, fmt.Errorf("initial open failed"))

	// Unhealed: a graph write must ERROR (never silently succeed onto Dolt).
	if _, err := s.Create(beads.Bead{Title: "x", Type: "task", Labels: []string{"gc:wisp"}}); err == nil {
		t.Fatal("expected an error while the graph store is unopenable")
	}

	// Clear the obstruction and let the backoff window pass.
	if err := os.RemoveAll(sqlitePath); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * lazyGraphStoreRetryBackoff)

	b, err := s.Create(beads.Bead{Title: "healed", Type: "task", Labels: []string{"gc:wisp"}})
	if err != nil {
		t.Fatalf("expected the backend to heal after the obstruction cleared and the backoff passed, got: %v", err)
	}
	if !strings.HasPrefix(b.ID, graphStoreIDPrefix+"-") {
		t.Fatalf("healed graph bead id %q lacks the graph prefix %q-", b.ID, graphStoreIDPrefix)
	}
	// The healed handle is cached for reuse by other openers.
	if _, ok := graphStoreHandleCache.Load(dir); !ok {
		t.Fatal("healed handle was not cached in graphStoreHandleCache")
	}
}

// TestLazyGraphStoreAdoptsCachedHandleWithoutOpening pins FIX F1's concurrent-open
// race handling: if another opener has cached a healthy handle for the same dir,
// the lazyGraphStore adopts it on its very first op WITHOUT attempting its own
// OpenSQLiteStore (proven by lastTry staying zero).
func TestLazyGraphStoreAdoptsCachedHandleWithoutOpening(t *testing.T) {
	dir := filepath.Join(t.TempDir(), citylayout.RuntimeRoot)
	t.Cleanup(func() { graphStoreHandleCache.Delete(dir) })

	// Pre-seed the cache with a healthy handle, as a racing opener would.
	healthy, err := beads.OpenSQLiteStore(dir, beads.WithSQLiteStoreRetention(0, 0), beads.WithSQLiteStoreIDPrefix(graphStoreIDPrefix))
	if err != nil {
		t.Fatal(err)
	}
	var seeded beads.Store = healthy
	if sq, ok := healthy.(*beads.SQLiteStore); ok {
		seeded = noCloseGraphStore{sq}
	}
	graphStoreHandleCache.Store(dir, seeded)

	s := newLazyGraphStore(dir, fmt.Errorf("seed error"))
	if _, err := s.Create(beads.Bead{Title: "adopted", Type: "task", Labels: []string{"gc:wisp"}}); err != nil {
		t.Fatalf("adopt cached handle: %v", err)
	}
	if !s.lastTry.IsZero() {
		t.Fatal("lazyGraphStore attempted its own OpenSQLiteStore instead of adopting the cached handle")
	}
	if s.delegate != seeded {
		t.Fatal("lazyGraphStore did not adopt the exact cached handle")
	}
}
