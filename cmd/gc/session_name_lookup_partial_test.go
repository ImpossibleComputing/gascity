package main

import (
	"errors"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
)

// partialGuardListStore embeds a real MemStore and forces List to return a
// caller-supplied (rows, err) pair, so tests can drive the
// PartialResultError degrade path deterministically.
type partialGuardListStore struct {
	*beads.MemStore
	rows []beads.Bead
	err  error
}

func (s *partialGuardListStore) List(beads.ListQuery) ([]beads.Bead, error) {
	return s.rows, s.err
}

func TestFindSessionNameByAgentLabelToleratesPartialResult(t *testing.T) {
	rows := []beads.Bead{{
		ID:     "s-1",
		Type:   "session",
		Status: "open",
		Metadata: map[string]string{
			"session_name": "mc-worker-1",
		},
	}}
	store := &partialGuardListStore{
		MemStore: beads.NewMemStore(),
		rows:     rows,
		err:      &beads.PartialResultError{Op: "bd list", Err: errors.New("graph leg down")},
	}

	got := findSessionNameByAgentLabel(store, "worker")
	if got != "mc-worker-1" {
		t.Fatalf("findSessionNameByAgentLabel = %q, want %q (survivor rows must be used on partial)", got, "mc-worker-1")
	}
}

func TestFindSessionNameByMetadataToleratesPartialResult(t *testing.T) {
	rows := []beads.Bead{{
		ID:     "s-2",
		Type:   "session",
		Status: "open",
		Metadata: map[string]string{
			"agent_name":   "worker",
			"session_name": "mc-worker-2",
		},
	}}
	store := &partialGuardListStore{
		MemStore: beads.NewMemStore(),
		rows:     rows,
		err:      &beads.PartialResultError{Op: "bd list", Err: errors.New("graph leg down")},
	}

	got := findSessionNameByMetadata(store, "agent_name", "worker", true)
	if got != "mc-worker-2" {
		t.Fatalf("findSessionNameByMetadata = %q, want %q (survivor rows must be used on partial)", got, "mc-worker-2")
	}
}
