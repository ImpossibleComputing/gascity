package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/coordrouter"
)

// TestWrapWithCachingStoreInsertsRouter proves B1b's wiring: the controller's
// store construction now layers policy(Router(caching(backend))) — the Router is
// present between the policy wrapper and the cache.
func TestWrapWithCachingStoreInsertsRouter(t *testing.T) {
	policy := wrapStoreWithBeadPolicies(beads.NewMemStore(), nil) // policy(mem)
	wrapped := wrapWithCachingStore(nil, policy, nil, false, "")  // policy(Router(caching(mem)))
	if wrapped == nil {
		t.Fatal("wrapWithCachingStore returned nil")
	}
	base, _, ok := unwrapBeadPolicyStore(wrapped)
	if !ok {
		t.Fatal("expected the result to be policy-wrapped")
	}
	if _, isRouter := base.(*coordrouter.Router); !isRouter {
		t.Fatalf("expected a *coordrouter.Router inside the policy wrapper, got %T", base)
	}
}

// TestCloseBeadStoreHandlePeelsRouter proves closeBeadStoreHandle reaches the
// underlying CachingStore through the Router (so StopReconciler/CloseStore fire
// and no reconciler goroutine leaks).
func TestCloseBeadStoreHandlePeelsRouter(t *testing.T) {
	cs := beads.NewCachingStore(beads.NewMemStore(), nil)
	wrapped := wrapStoreWithBeadPolicies(coordrouter.New(cs), nil) // policy(Router(caching(mem)))
	if err := closeBeadStoreHandle(wrapped); err != nil {
		t.Fatalf("closeBeadStoreHandle(policy(Router(caching))): %v", err)
	}
}

// TestWrapWithCachingStoreRegistersGraphSQLiteWhenOptedIn is E1: with
// [beads] graph_store = "sqlite" the controller's store construction registers an
// embedded SQLite backend for the graph class on the Router, and the store file
// is created under <scope>/.gc/. Work-class ops keep flowing to the cached work
// backend; only the graph class relocates.
func TestWrapWithCachingStoreRegistersGraphSQLiteWhenOptedIn(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.City{}
	cfg.Beads.GraphStore = "sqlite"

	policy := wrapStoreWithBeadPolicies(beads.NewMemStore(), cfg) // policy(mem)
	wrapped := wrapWithCachingStore(nil, policy, nil, false, dir) // policy(Router(caching(mem)) + sqlite graph)
	t.Cleanup(func() { _ = closeBeadStoreHandle(wrapped) })

	base, _, ok := unwrapBeadPolicyStore(wrapped)
	if !ok {
		t.Fatal("expected the result to be policy-wrapped")
	}
	router, ok := base.(*coordrouter.Router)
	if !ok {
		t.Fatalf("expected a *coordrouter.Router inside the policy wrapper, got %T", base)
	}

	var sqliteBackends int
	for _, b := range router.Backends() {
		if _, ok := b.(*beads.SQLiteStore); ok {
			sqliteBackends++
		}
	}
	if sqliteBackends != 1 {
		t.Fatalf("graph_store=sqlite registered %d *beads.SQLiteStore backends on the Router, want 1", sqliteBackends)
	}

	if _, err := os.Stat(filepath.Join(dir, ".gc", "beads.sqlite")); err != nil {
		t.Fatalf("expected the SQLite graph file at <scope>/.gc/beads.sqlite: %v", err)
	}
}

// TestWrapWithCachingStoreDefaultOffSkipsGraphStore proves the opt-in default:
// with no graph_store set the Router stays in its identity phase (one backend, no
// SQLite) and no store file is created — byte-identical to before E1.
func TestWrapWithCachingStoreDefaultOffSkipsGraphStore(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.City{} // GraphStore == ""

	policy := wrapStoreWithBeadPolicies(beads.NewMemStore(), cfg)
	wrapped := wrapWithCachingStore(nil, policy, nil, false, dir)
	t.Cleanup(func() { _ = closeBeadStoreHandle(wrapped) })

	base, _, _ := unwrapBeadPolicyStore(wrapped)
	router, ok := base.(*coordrouter.Router)
	if !ok {
		t.Fatalf("expected a *coordrouter.Router inside the policy wrapper, got %T", base)
	}
	for _, b := range router.Backends() {
		if _, ok := b.(*beads.SQLiteStore); ok {
			t.Fatal("default-off must not register a SQLite graph backend")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".gc", "beads.sqlite")); !os.IsNotExist(err) {
		t.Fatalf("default-off must not create a SQLite graph file; stat err = %v", err)
	}
}
