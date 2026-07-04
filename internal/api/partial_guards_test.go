package api

import (
	"context"
	"errors"
	"testing"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
)

func newPartial(msg string) error {
	return &beads.PartialResultError{Op: "bd list", Err: errors.New(msg)}
}

// G4 — humaHandleBeadDeps must serve the reachable children when the child
// list read returns a PartialResultError alongside rows.
func TestHumaHandleBeadDepsServesPartial(t *testing.T) {
	fs := newFakeState(t)
	mem := beads.NewMemStore()
	parent, err := mem.Create(beads.Bead{Type: "task", Title: "parent"})
	if err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	child := beads.Bead{ID: "child-1", Type: "task", Title: "child", ParentID: parent.ID}
	fs.cityBeadStore = &failingBeadStore{
		Store:      mem,
		listResult: []beads.Bead{child},
		listErr:    newPartial("graph leg down"),
	}
	s := &Server{state: fs}

	out, err := s.humaHandleBeadDeps(context.Background(), &BeadDepsInput{ID: parent.ID})
	if err != nil {
		t.Fatalf("humaHandleBeadDeps = %v, want nil (partial must not 500 while holding children)", err)
	}
	found := false
	for _, b := range out.Body.Children {
		if b.ID == "child-1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("children = %+v, want the survivor child-1", out.Body.Children)
	}
}

// graphPartialStore distinguishes the two collectBeadGraph List queries so
// both guarded sites (metadata-children and BFS-children) can be exercised
// with survivor rows + a PartialResultError.
type graphPartialStore struct {
	*beads.MemStore
	metaRows []beads.Bead
	bfsRows  []beads.Bead
	err      error
}

func (s *graphPartialStore) List(q beads.ListQuery) ([]beads.Bead, error) {
	if len(q.ParentIDs) > 0 {
		return s.bfsRows, s.err
	}
	if q.Metadata[beadmeta.RootBeadIDMetadataKey] != "" {
		return s.metaRows, s.err
	}
	return nil, nil
}

// G5 — collectBeadGraph keeps partial rows at both List sites.
func TestCollectBeadGraphToleratesPartial(t *testing.T) {
	root := beads.Bead{ID: "root-1", Type: "task", Title: "root"}
	store := &graphPartialStore{
		MemStore: beads.NewMemStore(),
		metaRows: []beads.Bead{{ID: "meta-child", Type: "task", ParentID: "root-1"}},
		bfsRows:  []beads.Bead{{ID: "bfs-child", Type: "task", ParentID: "root-1"}},
		err:      newPartial("graph leg down"),
	}

	graphBeads, _, err := collectBeadGraph(store, root)
	if err != nil {
		t.Fatalf("collectBeadGraph = %v, want nil (partial rows must keep the walk going)", err)
	}
	ids := map[string]bool{}
	for _, b := range graphBeads {
		ids[b.ID] = true
	}
	if !ids["meta-child"] {
		t.Errorf("missing meta-child; graph = %v (metadata-children guard failed)", ids)
	}
	if !ids["bfs-child"] {
		t.Errorf("missing bfs-child; graph = %v (BFS-children guard failed)", ids)
	}
}

// G6 — snapshotFromStore returns the reachable workflow beads under a partial.
func TestSnapshotFromStoreToleratesPartial(t *testing.T) {
	fs := newFakeState(t)
	mem := beads.NewMemStore()
	root, err := mem.Create(beads.Bead{
		Type:  "task",
		Title: "wf-root",
		Metadata: map[string]string{
			beadmeta.KindMetadataKey:       "workflow",
			beadmeta.WorkflowIDMetadataKey: "wf-1",
		},
	})
	if err != nil {
		t.Fatalf("seed root: %v", err)
	}
	member := beads.Bead{ID: "wf-member", Type: "task", Title: "step", ParentID: root.ID}
	failing := &failingBeadStore{
		Store:      mem,
		listResult: []beads.Bead{member},
		listErr:    newPartial("graph leg down"),
	}
	info := workflowStoreInfo{ref: "rig:bad", store: failing}
	s := &Server{state: fs}

	snap, err := s.snapshotFromStore(info, root, "", "", "", nil, false, 0)
	if err != nil {
		t.Fatalf("snapshotFromStore = %v, want nil (partial must not drop the snapshot)", err)
	}
	found := false
	for _, b := range snap.Beads {
		if b.ID == "wf-member" {
			found = true
		}
	}
	if !found {
		t.Fatalf("snapshot beads = %+v, want the survivor wf-member", snap.Beads)
	}
}

// G7 — a partial-with-rows rig counts as a success, not a total outage.
func TestHumaHandleConvoyListPartialIsNotOutage(t *testing.T) {
	fs := newFakeState(t)
	fs.cityBeadStore = beads.NewMemStore()
	bad := beads.NewMemStore()
	convoy := beads.Bead{ID: "cv-1", Type: "convoy", Title: "the-convoy"}
	fs.stores = map[string]beads.Store{
		"bad": &failingBeadStore{
			Store:      bad,
			listResult: []beads.Bead{convoy},
			listErr:    newPartial("graph leg down"),
		},
	}
	s := &Server{state: fs}

	out, err := s.humaHandleConvoyList(context.Background(), &ConvoyListInput{})
	if err != nil {
		t.Fatalf("humaHandleConvoyList = %v, want nil (every-rig-partial-with-rows is not an outage)", err)
	}
	found := false
	for _, b := range out.Body.Items {
		if b.ID == "cv-1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("convoys = %+v, want the survivor cv-1", out.Body.Items)
	}
}
