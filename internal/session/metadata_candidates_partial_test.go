package session

import (
	"errors"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
)

// partialGuardListStore embeds a real MemStore and forces List to return a
// caller-supplied (rows, err) pair to drive the PartialResultError path.
type partialGuardListStore struct {
	*beads.MemStore
	rows []beads.Bead
	err  error
}

func (s *partialGuardListStore) List(beads.ListQuery) ([]beads.Bead, error) {
	return s.rows, s.err
}

func TestExactMetadataSessionCandidatesKeepsPartialRows(t *testing.T) {
	rows := []beads.Bead{{
		ID:       "s-1",
		Type:     BeadType,
		Status:   "open",
		Metadata: map[string]string{"agent_name": "worker"},
	}}
	partial := &beads.PartialResultError{Op: "bd list", Err: errors.New("graph leg down")}
	store := &partialGuardListStore{
		MemStore: beads.NewMemStore(),
		rows:     rows,
		err:      partial,
	}

	got, err := ExactMetadataSessionCandidates(store, false, map[string]string{"agent_name": "worker"})
	if len(got) != 1 || got[0].ID != "s-1" {
		t.Fatalf("candidates = %+v, want the survivor row s-1", got)
	}
	if !beads.IsPartialResult(err) {
		t.Fatalf("err = %v, want a propagated PartialResultError", err)
	}
	if !errors.Is(err, partial) {
		t.Fatalf("err = %v, want it to wrap the injected partial error", err)
	}
}
