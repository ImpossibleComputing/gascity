package dispatch

import (
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
)

// graphAwareDrainStore is a MemStore that advertises a graph-class id prefix, so
// drainGraphIDPrefix resolves it the way the live graph_store=sqlite Router does.
type graphAwareDrainStore struct {
	*beads.MemStore
	prefix string
}

func (g *graphAwareDrainStore) GraphIDPrefix() string { return g.prefix }

func (g *graphAwareDrainStore) ListGraphOnly(q beads.ListQuery) ([]beads.Bead, error) {
	return g.MemStore.List(q)
}

func TestDrainCrossLegBlocker(t *testing.T) {
	cases := []struct {
		name, prefix, member, blocker string
		want                          bool
	}{
		{"single-store is never cross-leg", "", "gcg-1", "ga-2", false},
		{"graph member, work blocker", "gcg-", "gcg-1", "ga-2", true},
		{"work member, graph blocker", "gcg-", "ga-1", "gcg-2", true},
		{"both graph", "gcg-", "gcg-1", "gcg-2", false},
		{"both work-leg", "gcg-", "ga-1", "mc-2", false},
	}
	for _, tc := range cases {
		if got := drainCrossLegBlocker(tc.prefix, tc.member, tc.blocker); got != tc.want {
			t.Errorf("%s: drainCrossLegBlocker(%q,%q,%q) = %v, want %v",
				tc.name, tc.prefix, tc.member, tc.blocker, got, tc.want)
		}
	}
}

func TestDrainGraphIDPrefix(t *testing.T) {
	if p := drainGraphIDPrefix(beads.NewMemStore()); p != "" {
		t.Errorf("plain MemStore prefix = %q, want empty (single-store fail-open)", p)
	}
	g := &graphAwareDrainStore{MemStore: beads.NewMemStore(), prefix: "gcg"}
	if p := drainGraphIDPrefix(g); p != "gcg-" {
		t.Errorf("graph-aware prefix = %q, want gcg-", p)
	}
}

func seedDrainStore(deps []beads.Dep) *graphAwareDrainStore {
	return &graphAwareDrainStore{MemStore: beads.NewMemStoreFrom(0, nil, deps), prefix: "gcg"}
}

func TestDrainProjectedBlockerIDs_FailsLoudOnRawCrossLegBlocker(t *testing.T) {
	// gcg- member depends on an out-of-manifest ga- work-leg bead: a raw
	// cross-leg block that can never release.
	st := seedDrainStore([]beads.Dep{{IssueID: "gcg-member", DependsOnID: "ga-external", Type: "blocks"}})
	manifest := drainManifest{Rows: []drainManifestRow{{Index: 0, MemberID: "gcg-member", Status: "wired"}}}

	_, err := drainProjectedBlockerIDs(st, "gcg-member", manifest)
	if err == nil {
		t.Fatal("want a loud error on a raw cross-leg blocker, got nil")
	}
	if !strings.Contains(err.Error(), "cross-leg") {
		t.Errorf("error = %v, want a cross-leg mention", err)
	}
}

func TestDrainProjectedBlockerIDs_AllowsSameLegRawBlocker(t *testing.T) {
	// gcg- member depends on an out-of-manifest gcg- bead: same-leg, releases
	// normally — no guard.
	st := seedDrainStore([]beads.Dep{{IssueID: "gcg-member", DependsOnID: "gcg-external", Type: "blocks"}})
	manifest := drainManifest{Rows: []drainManifestRow{{Index: 0, MemberID: "gcg-member", Status: "wired"}}}

	got, err := drainProjectedBlockerIDs(st, "gcg-member", manifest)
	if err != nil {
		t.Fatalf("same-leg blocker must not error: %v", err)
	}
	if len(got) != 1 || got[0] != "gcg-external" {
		t.Errorf("blockers = %v, want [gcg-external]", got)
	}
}

func TestDrainProjectedBlockerIDs_ExemptsProjectedInManifestBlocker(t *testing.T) {
	// member-a's raw dep is on ga-member-b (cross-leg), but member-b IS in the
	// manifest with an item root, so it projects to gcg-rootb (same-leg). The
	// projected edge is the intended drain mechanism and must NOT trip the guard.
	st := seedDrainStore([]beads.Dep{{IssueID: "gcg-member-a", DependsOnID: "ga-member-b", Type: "blocks"}})
	manifest := drainManifest{Rows: []drainManifestRow{
		{Index: 0, MemberID: "gcg-member-a", Status: "wired"},
		{Index: 1, MemberID: "ga-member-b", ItemRootID: "gcg-rootb", Status: "wired"},
	}}

	got, err := drainProjectedBlockerIDs(st, "gcg-member-a", manifest)
	if err != nil {
		t.Fatalf("projected in-manifest blocker must not error: %v", err)
	}
	if len(got) != 1 || got[0] != "gcg-rootb" {
		t.Errorf("blockers = %v, want [gcg-rootb] (projected to same-leg root)", got)
	}
}

func TestDrainProjectedBlockerIDs_InertOnSingleStore(t *testing.T) {
	// A plain MemStore (no graph prefix) never treats anything as cross-leg —
	// byte-identical to pre-guard behavior on a single-store city.
	st := beads.NewMemStoreFrom(0, nil, []beads.Dep{{IssueID: "a-1", DependsOnID: "b-2", Type: "blocks"}})
	manifest := drainManifest{Rows: []drainManifestRow{{Index: 0, MemberID: "a-1", Status: "wired"}}}

	got, err := drainProjectedBlockerIDs(st, "a-1", manifest)
	if err != nil {
		t.Fatalf("single-store must not error: %v", err)
	}
	if len(got) != 1 || got[0] != "b-2" {
		t.Errorf("blockers = %v, want [b-2]", got)
	}
}
