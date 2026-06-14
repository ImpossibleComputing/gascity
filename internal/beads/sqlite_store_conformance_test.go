package beads_test

import (
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/coordclass"
	"github.com/gastownhall/gascity/internal/coordrouter"
	"github.com/gastownhall/gascity/internal/coordrouter/coordtest"
)

// newSQLiteForConformance returns a fresh, empty SQLite store with cleanup
// registered. The factory closures below hand it to the shared conformance suites
// as both a coordrouter.GraphStore (it implements ApplyGraphPlan) and a
// beads.Store.
func newSQLiteForConformance(t *testing.T) *beads.SQLiteStore {
	t.Helper()
	s, err := beads.OpenSQLiteStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenSQLiteStore: %v", err)
	}
	store := s.(*beads.SQLiteStore)
	t.Cleanup(func() { _ = store.CloseStore() })
	return store
}

// TestSQLiteStoreSatisfiesGraphStoreConformance runs the SHARED GraphStore
// conformance suite (the same coordtest.RunGraphStoreTests every graph backend
// must pass) against the recovered SQLite store, un-skipped — proving its
// ApplyGraphPlan seam is conformant, not just its white-box behavior.
func TestSQLiteStoreSatisfiesGraphStoreConformance(t *testing.T) {
	coordtest.RunGraphStoreTestsWithOptions(t,
		func() coordrouter.GraphStore { return newSQLiteForConformance(t) },
		coordtest.Options{Skip: false})
}

// TestSQLiteStoreSatisfiesClassedStoreConformanceForGraph runs the shared
// classed-store conformance suite for ClassGraph against the SQLite store,
// un-skipped — proving it round-trips and correctly classifies graph-class beads.
func TestSQLiteStoreSatisfiesClassedStoreConformanceForGraph(t *testing.T) {
	coordtest.RunClassedStoreTestsWithOptions(t, coordclass.ClassGraph,
		func() beads.Store { return newSQLiteForConformance(t) },
		coordtest.Options{Skip: false})
}
