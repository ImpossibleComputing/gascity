package agentutil

import (
	"errors"
	"testing"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
)

// partialGuardStore embeds a real MemStore and forces List to return a
// caller-supplied (rows, err) pair so tests can exercise the
// PartialResultError degrade path without a live multi-leg backend.
type partialGuardStore struct {
	*beads.MemStore
	rows []beads.Bead
	err  error
}

func (s *partialGuardStore) List(beads.ListQuery) ([]beads.Bead, error) {
	return s.rows, s.err
}

func TestFindSessionNameByTemplateToleratesPartialResult(t *testing.T) {
	rows := []beads.Bead{{
		ID:     "s-1",
		Type:   "session",
		Status: "open",
		Metadata: map[string]string{
			beadmeta.TemplateMetadataKey: "mc/worker",
			"session_name":               "mc-worker-1",
		},
	}}
	store := &partialGuardStore{
		MemStore: beads.NewMemStore(),
		rows:     rows,
		err:      &beads.PartialResultError{Op: "bd list", Err: errors.New("graph leg down")},
	}

	got := findSessionNameByTemplate(store, "mc/worker")
	if got != "mc-worker-1" {
		t.Fatalf("findSessionNameByTemplate = %q, want %q (survivor rows must be used on partial)", got, "mc-worker-1")
	}
}
