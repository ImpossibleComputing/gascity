package main

import (
	"errors"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
)

func TestVerifyCanonicalBdScopeStoreReadyAcceptsPartial(t *testing.T) {
	// A partial result proves the store surface is up (one leg returned
	// rows); the readiness probe must treat it as ready instead of
	// wedging startup on a single-leg outage.
	store := &partialGuardListStore{
		MemStore: beads.NewMemStore(),
		rows:     []beads.Bead{{ID: "b-1", Type: "task"}},
		err:      &beads.PartialResultError{Op: "bd list", Err: errors.New("graph leg down")},
	}

	if err := verifyCanonicalBdScopeStoreReady(store); err != nil {
		t.Fatalf("verifyCanonicalBdScopeStoreReady = %v, want nil (partial proves liveness)", err)
	}
}
