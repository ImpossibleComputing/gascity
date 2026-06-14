package coordrouter

import (
	"errors"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/coordclass"
)

// envActorClaimSpy is a work backend that claims for the actor configured on the
// store (beads.EnvActorClaimer), the way a BdStore claims via BEADS_ACTOR. It
// records the id it was asked to claim so the test can assert routing.
type envActorClaimSpy struct {
	*beads.MemStore
	claimed string
}

func (s *envActorClaimSpy) Claim(id string) (beads.Bead, bool, error) {
	s.claimed = id
	b, err := s.Get(id)
	if err != nil {
		return beads.Bead{}, false, err
	}
	return b, true, nil
}

// TestRouterClaimRoutesByOwningBackend proves Router.Claim bridges the split's two
// claim shapes: a graph bead routes to the SQLite backend's explicit-assignee
// Claimer, a work bead routes to the work backend's EnvActorClaimer (single-arg).
func TestRouterClaimRoutesByOwningBackend(t *testing.T) {
	// Offset the work MemStore's id sequence so it occupies a distinct id namespace
	// from the SQLite graph store (both otherwise mint gc-N — see ga-y5pwx3).
	work := &envActorClaimSpy{MemStore: beads.NewMemStoreFrom(1000, nil, nil)}
	sqlite, err := beads.OpenSQLiteStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenSQLiteStore: %v", err)
	}
	graph := sqlite.(*beads.SQLiteStore) // ids: gc-* (distinct from MemStore)
	t.Cleanup(func() { _ = graph.CloseStore() })

	r := New(work)
	r.Register(coordclass.ClassGraph, graph)

	// Graph bead -> SQLite (Claimer): the explicit assignee is honored.
	gb, err := r.Create(beads.Bead{Title: "wisp", Type: "task", Labels: []string{"gc:wisp"}})
	if err != nil {
		t.Fatalf("create graph bead: %v", err)
	}
	claimed, ok, err := r.Claim(gb.ID, "worker-1")
	if err != nil || !ok {
		t.Fatalf("claim graph bead = (ok=%v, %v), want ok=true", ok, err)
	}
	if claimed.Assignee != "worker-1" {
		t.Fatalf("graph claim returned assignee %q, want worker-1", claimed.Assignee)
	}
	stored, err := graph.Get(gb.ID)
	if err != nil {
		t.Fatalf("re-get graph bead: %v", err)
	}
	if stored.Assignee != "worker-1" || stored.Status != "in_progress" {
		t.Fatalf("sqlite after claim: assignee=%q status=%q, want worker-1/in_progress", stored.Assignee, stored.Status)
	}

	// Work bead -> EnvActorClaimer backend: routed to its single-arg Claim.
	wb, err := r.Create(beads.Bead{Title: "backlog", Type: "task"})
	if err != nil {
		t.Fatalf("create work bead: %v", err)
	}
	if wb.ID == gb.ID {
		t.Fatalf("test setup: id namespaces collided (%s)", wb.ID)
	}
	if _, ok, err := r.Claim(wb.ID, "worker-1"); err != nil || !ok {
		t.Fatalf("claim work bead = (ok=%v, %v), want ok=true", ok, err)
	}
	if work.claimed != wb.ID {
		t.Fatalf("EnvActorClaimer.Claim called with %q, want %q", work.claimed, wb.ID)
	}
}

// TestRouterClaimUnsupportedBackend proves a backend with no claim capability
// surfaces ErrClaimUnsupported rather than silently losing the claim.
func TestRouterClaimUnsupportedBackend(t *testing.T) {
	r := New(beads.NewMemStore()) // MemStore implements neither Claimer nor EnvActorClaimer
	b, err := r.Create(beads.Bead{Title: "x", Type: "task"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := r.Claim(b.ID, "a"); !errors.Is(err, beads.ErrClaimUnsupported) {
		t.Fatalf("Claim on a non-claimer backend = %v, want ErrClaimUnsupported", err)
	}
}
