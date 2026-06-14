package main

import (
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/coordclass"
	"github.com/gastownhall/gascity/internal/coordrouter"
)

// TestGcCloseRoutesGraphBeadToSQLite proves `gc close` routes a graph bead's
// close (and its gc.outcome stamp) to the embedded graph store, while a work bead
// closes in the work store — routed by id through the Router. This is the worker's
// graph-store-aware close that finishes a step found via `gc ready`.
func TestGcCloseRoutesGraphBeadToSQLite(t *testing.T) {
	// Offset the work MemStore so it occupies a distinct id namespace from the
	// SQLite graph store (both otherwise mint gc-N — see ga-y5pwx3).
	work := beads.NewMemStoreFrom(1000, nil, nil)
	sqlite, err := beads.OpenSQLiteStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenSQLiteStore: %v", err)
	}
	graph := sqlite.(*beads.SQLiteStore)
	t.Cleanup(func() { _ = graph.CloseStore() })

	r := coordrouter.New(work)
	r.Register(coordclass.ClassGraph, graph)

	// A graph bead closes (with outcome) in SQLite.
	gb, err := r.Create(beads.Bead{Title: "graph step", Type: "task", Labels: []string{"gc:wisp"}})
	if err != nil {
		t.Fatalf("create graph bead: %v", err)
	}
	if err := closeBeadThroughStore(r, gb.ID, "pass"); err != nil {
		t.Fatalf("closeBeadThroughStore(graph): %v", err)
	}
	stored, err := graph.Get(gb.ID)
	if err != nil {
		t.Fatalf("re-get graph bead from SQLite: %v", err)
	}
	if stored.Status != "closed" || stored.Metadata["gc.outcome"] != "pass" {
		t.Fatalf("graph bead in SQLite = status %q outcome %q, want closed/pass", stored.Status, stored.Metadata["gc.outcome"])
	}

	// A work bead closes in the work backend, not SQLite.
	wb, err := r.Create(beads.Bead{Title: "work item", Type: "task"})
	if err != nil {
		t.Fatalf("create work bead: %v", err)
	}
	if err := closeBeadThroughStore(r, wb.ID, ""); err != nil {
		t.Fatalf("closeBeadThroughStore(work): %v", err)
	}
	wstored, err := work.Get(wb.ID)
	if err != nil {
		t.Fatalf("re-get work bead: %v", err)
	}
	if wstored.Status != "closed" {
		t.Fatalf("work bead status = %q, want closed", wstored.Status)
	}
}
