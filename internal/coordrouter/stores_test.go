package coordrouter

import (
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
)

// TestNonGraphStoresAreSatisfiedByBeadsStore proves at runtime what the
// compile-time assertions in stores.go assert statically: a plain beads.Store is
// the bd-delegating first implementation of every non-graph class seam, and a
// create/read round-trip works through each narrow interface.
func TestNonGraphStoresAreSatisfiedByBeadsStore(t *testing.T) {
	roundTrip := func(t *testing.T, create func(beads.Bead) (beads.Bead, error), get func(string) (beads.Bead, error)) {
		t.Helper()
		created, err := create(beads.Bead{Title: "seam round-trip"})
		if err != nil {
			t.Fatalf("Create through seam: %v", err)
		}
		if created.ID == "" {
			t.Fatal("Create returned empty ID")
		}
		got, err := get(created.ID)
		if err != nil {
			t.Fatalf("Get through seam: %v", err)
		}
		if got.ID != created.ID {
			t.Fatalf("Get returned ID %q, want %q", got.ID, created.ID)
		}
	}

	t.Run("WorkStore", func(t *testing.T) {
		var s WorkStore = beads.NewMemStore()
		roundTrip(t, s.Create, s.Get)
	})
	t.Run("MessageStore", func(t *testing.T) {
		var s MessageStore = beads.NewMemStore()
		roundTrip(t, s.Create, s.Get)
	})
	t.Run("SessionsStore", func(t *testing.T) {
		var s SessionsStore = beads.NewMemStore()
		roundTrip(t, s.Create, s.Get)
	})
	t.Run("OrdersStore", func(t *testing.T) {
		var s OrdersStore = beads.NewMemStore()
		roundTrip(t, s.Create, s.Get)
	})
	t.Run("NudgesStore", func(t *testing.T) {
		var s NudgesStore = beads.NewMemStore()
		roundTrip(t, s.Create, s.Get)
	})
}

// GraphStore is satisfied by any beads.GraphApplyStore implementation (the
// capability it embeds), independent of the bd-delegating adapter in
// bdgraphstore.go. fakeGraphStore (router_test.go) is one such implementation.
var _ GraphStore = (*fakeGraphStore)(nil)
