package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/coordclass"
	"github.com/gastownhall/gascity/internal/coordrouter"
)

// TestResolveRealBdPathHonorsGCBDReal proves the shim resolves the real bd
// binary from GC_BD_REAL by absolute path and does NOT depend on a PATH lookup
// of "bd" — the recursion trap that would resolve back to the shim once it is
// installed as `bd` first on an agent's PATH (graph-store-rollout-plan.md §C2).
func TestResolveRealBdPathHonorsGCBDReal(t *testing.T) {
	dir := t.TempDir()
	realBd := filepath.Join(dir, "bd-real")
	if err := os.WriteFile(realBd, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GC_BD_REAL", realBd)
	// Empty PATH: any accidental exec.LookPath("bd") would fail, so a successful
	// resolve proves GC_BD_REAL is honored without consulting PATH.
	t.Setenv("PATH", "")

	got, err := resolveRealBdPath()
	if err != nil {
		t.Fatalf("resolveRealBdPath: %v", err)
	}
	if got != realBd {
		t.Fatalf("resolveRealBdPath = %q, want %q", got, realBd)
	}
}

// TestResolveRealBdPathRejectsRelativeGCBDReal guards the install-time contract:
// GC_BD_REAL must be an absolute path (a relative one would re-introduce PATH
// ambiguity and the recursion risk).
func TestResolveRealBdPathRejectsRelativeGCBDReal(t *testing.T) {
	t.Setenv("GC_BD_REAL", filepath.Join("relative", "bd"))
	if _, err := resolveRealBdPath(); err == nil {
		t.Fatal("expected an error for a relative GC_BD_REAL, got nil")
	}
}

// TestExecRealBdUsesGCBDRealAndPropagatesExit proves the passthrough path execs
// the GC_BD_REAL binary (no LookPath), streams its stdout, and propagates its
// exit code unchanged — the bd exit-code contract the shim must preserve.
func TestExecRealBdUsesGCBDRealAndPropagatesExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake bd is POSIX-only")
	}
	dir := t.TempDir()
	realBd := filepath.Join(dir, "bd-real")
	if err := os.WriteFile(realBd, []byte("#!/bin/sh\necho \"args:$*\"\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GC_BD_REAL", realBd)
	t.Setenv("PATH", "") // prove no dependence on a PATH-resolved bd

	var out, errb bytes.Buffer
	code := execRealBd([]string{"version"}, dir, nil, &out, &errb)
	if code != 7 {
		t.Fatalf("execRealBd exit = %d, want 7 (stderr=%q)", code, errb.String())
	}
	if !strings.Contains(out.String(), "args:version") {
		t.Fatalf("execRealBd stdout = %q, want it to contain %q", out.String(), "args:version")
	}
}

// TestClassifyBdShimVerb pins the three-way disposition policy: routed verbs
// always route; provably-graph-free verbs always passthrough; graph-touching
// unrouted verbs passthrough in the identity phase (byte-identical, safe) but
// are refused in the split phase rather than silently bypassing the graph store.
func TestClassifyBdShimVerb(t *testing.T) {
	cases := []struct {
		verb  string
		split bool
		want  bdShimDisposition
	}{
		{"close", false, bdRoute},
		{"close", true, bdRoute},
		{"version", false, bdPassthrough},
		{"version", true, bdPassthrough},
		{"mol", false, bdPassthrough}, // identity phase: one backend, byte-identical
		{"mol", true, bdRefuse},       // split phase: would silently miss graph beads
		{"gate", true, bdRefuse},
		{"query", true, bdRefuse},
	}
	for _, tc := range cases {
		if got := classifyBdShimVerb(tc.verb, tc.split); got != tc.want {
			t.Errorf("classifyBdShimVerb(%q, split=%v) = %v, want %v", tc.verb, tc.split, got, tc.want)
		}
	}
}

// TestDispatchBdShimCloseRoutesGraphBeadToSQLite proves a routed `bd close`
// lands in the owning backend: a graph bead closes in the embedded SQLite store
// and a work bead closes in the work backend — routed by id through the Router,
// exactly as a worker's `bd close` must behave under graph_store=sqlite.
func TestDispatchBdShimCloseRoutesGraphBeadToSQLite(t *testing.T) {
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

	gb, err := r.Create(beads.Bead{Title: "graph step", Type: "task", Labels: []string{"gc:wisp"}})
	if err != nil {
		t.Fatalf("create graph bead: %v", err)
	}
	var out, errb bytes.Buffer
	if code := dispatchBdShimVerb(r, "close", []string{gb.ID}, nil, &out, &errb); code != 0 {
		t.Fatalf("close exit = %d, stderr=%q", code, errb.String())
	}
	stored, err := graph.Get(gb.ID)
	if err != nil {
		t.Fatalf("re-get graph bead from SQLite: %v", err)
	}
	if stored.Status != "closed" {
		t.Fatalf("graph bead status = %q, want closed", stored.Status)
	}

	wb, err := r.Create(beads.Bead{Title: "work item", Type: "task"})
	if err != nil {
		t.Fatalf("create work bead: %v", err)
	}
	if code := dispatchBdShimVerb(r, "close", []string{wb.ID}, nil, &out, &errb); code != 0 {
		t.Fatalf("close work exit = %d, stderr=%q", code, errb.String())
	}
	wstored, err := work.Get(wb.ID)
	if err != nil {
		t.Fatalf("re-get work bead: %v", err)
	}
	if wstored.Status != "closed" {
		t.Fatalf("work bead status = %q, want closed", wstored.Status)
	}
}
