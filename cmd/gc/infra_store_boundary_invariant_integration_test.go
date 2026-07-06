//go:build integration

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/orders"
	"github.com/gastownhall/gascity/internal/session"
)

// This is the E2.5 integration-tier of the domain/infra store boundary invariant
// (design step 2b). Where the fast tier (infra_store_boundary_invariant_test.go)
// runs the production wrapper stack over MemStore, this tier runs it over the
// REAL two-store shape: a doltlite (embedded Dolt) city with a seeded .gc/infra
// scope, brought up through the true production lifecycle (doInit's infra seed +
// startBeadsLifecycle's infra bd-init), with every store opened through the true
// production open path (openStoreResultAtForCity / openCityInfraStoreResultAt,
// incl. wrapStoreWithBeadPolicies / wrapInfraStoreWithBeadPolicies + reserved-
// prefix minting). It proves the split holds end-to-end against real Dolt, and
// that an existing single-store city (no infra scope) is left untouched.
//
// It reuses the package-exported assertStoreClassBoundary helper verbatim — that
// is exactly why the fast tier exported it.
//
// REQUIRES a clean dolt-capable environment. `bd` must resolve to an isolated
// embedded-Dolt store (HOME + BEADS_DIR scrubbed); in an environment where bd is
// federated onto a shared live ledger the init aborts, so the test skips rather
// than write into the wrong store.

func TestInfraStoreBoundaryInvariantIntegration(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd CLI required for the real two-store infra boundary integration test")
	}
	clearInheritedBeadsEnv(t)
	configureTestDoltIdentityEnv(t)
	t.Setenv("GC_BEADS_BACKEND", "doltlite")
	t.Setenv("BEADS_BACKEND", "doltlite")
	t.Setenv("BD_NON_INTERACTIVE", "1")
	t.Setenv("GC_INFRA_STORE_SPLIT", "1")

	cityPath := shortSocketTempDir(t, "gc-infra-split-")
	disableManagedDoltRecoveryForTest(t)
	cleanupManagedDoltTestCity(t, cityPath)

	writeDoltliteCityTOMLForInfraSplit(t, cityPath)
	materializeBuiltinPacksForTest(t, cityPath)

	// gc init's infra-scope seed writes the .gc/infra canonical scope config.
	if err := seedInitInfraScope(cityPath); err != nil {
		t.Fatalf("seedInitInfraScope: %v", err)
	}
	if !cityHasInfraStore(cityPath) {
		t.Fatal("cityHasInfraStore is false after seeding the infra scope; the split did not activate")
	}

	cfg, _, err := loadCityConfigWithBuiltinPacks(cityPath)
	if err != nil {
		t.Fatalf("load city config: %v", err)
	}

	// gc start: bd-init both the city (hq) and the infra (gcg) Dolt databases.
	if err := startBeadsLifecycle(cityPath, "infra-split", cfg, os.Stderr); err != nil {
		t.Fatalf("startBeadsLifecycle: %v", err)
	}
	t.Cleanup(func() { _ = shutdownBeadsProvider(cityPath) })

	// Open both stores through the true production open path.
	workStore, err := openCityStoreAt(cityPath)
	if err != nil {
		t.Fatalf("open city work store: %v", err)
	}
	infraStore, present, err := openCityInfraStoreAt(cityPath)
	if err != nil {
		t.Fatalf("open city infra store: %v", err)
	}
	if !present || infraStore == nil {
		t.Fatal("infra store not present after startBeadsLifecycle on a split city")
	}

	// Preflight: a trivial create on the WORK store proves the local bd/dolt
	// toolchain is version-compatible with this gascity build. If it fails on a
	// bd-version skew (e.g. an unknown-flag / unsupported-backend mismatch) the
	// whole real-store lifecycle is inoperable here — for EVERY store, not just
	// the infra one — so skip rather than report a false split failure. This is
	// the "needs a dolt-capable, version-matched environment" guard.
	if _, err := workStore.Create(beads.Bead{Title: "bd toolchain preflight", Type: "task"}); err != nil {
		t.Skipf("bd/dolt toolchain is not version-compatible with this build (work-store create failed: %v); "+
			"the real two-store lifecycle needs a dolt-capable, bd-version-matched environment", err)
	}

	// Seed a representative infra bead mix through the production accessors, each
	// resolving its class store off the real infra store.
	seedRepresentativeInfraBeads(t, workStore, infraStore, cfg, cityPath)

	// The boundary invariant, end-to-end against real Dolt: the work store holds
	// no infra-class bead, the infra store holds no domain-class bead.
	assertStoreClassBoundary(t, "domain:hq", workStore, false)
	assertStoreClassBoundary(t, "infra", infraStore, true)

	// The ID-prefix half: every infra bead carries a reserved class prefix, no
	// work bead does.
	assertReservedPrefixBoundary(t, workStore, infraStore)

	// An EXISTING single-store city — one with no seeded infra scope — must stay
	// single-store: no infra scope, byte-identical lifecycle.
	assertSingleStoreCityUntouched(t)
}

func writeDoltliteCityTOMLForInfraSplit(t *testing.T, cityPath string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(cityPath, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "[workspace]\nname = \"infra-split\"\n\n" +
		"[beads]\nprovider = \"bd\"\nbackend = \"doltlite\"\n\n" +
		"[[agent]]\nname = \"mayor\"\nstart_command = \"echo hello\"\n"
	if err := os.WriteFile(filepath.Join(cityPath, "city.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// The city scope's canonical doltlite metadata (matches the bd-store bridge
	// tests' shape) so startBeadsLifecycle bd-inits an embedded Dolt DB.
	if err := os.MkdirAll(filepath.Join(cityPath, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `{"backend":"doltlite","database":"doltlite","dolt_database":"hq"}`
	if err := os.WriteFile(filepath.Join(cityPath, ".beads", "metadata.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
}

// seedRepresentativeInfraBeads creates one bead of each infra coordination class
// through the production class resolvers, plus a plain work bead, so the boundary
// assertion has a representative population on both sides.
func seedRepresentativeInfraBeads(t *testing.T, workStore, infraStore beads.Store, cfg *config.City, cityPath string) {
	t.Helper()

	// SESSION — resolveSessionStore → infra store.
	sessStore := resolveSessionStore(workStore, infraStore, cfg, cityPath, nil)
	if _, err := session.NewStore(beads.SessionStore{Store: sessStore}).CreateSession(session.CreateSpec{
		Title:     "worker-1",
		AgentName: "worker-1",
		Metadata:  map[string]string{"provider": "tmux", "template": "claude"},
	}); err != nil {
		t.Fatalf("session create: %v", err)
	}

	// MAIL — newCityMailProvider (messaging class) → infra store.
	mp := newCityMailProvider(workStore, infraStore, cfg, cityPath, nil)
	if _, err := mp.Send("human", "worker-1", "hello", "body text"); err != nil {
		t.Fatalf("mail send: %v", err)
	}

	// NUDGE — resolveNudgesStore → infra store.
	nudgeStore := resolveNudgesStore(workStore, infraStore, cfg, cityPath, nil)
	if _, _, err := ensureQueuedNudgeBead(beads.NudgesStore{Store: nudgeStore}, newQueuedNudge("worker-1", "please continue", time.Now().UTC())); err != nil {
		t.Fatalf("nudge enqueue: %v", err)
	}

	// ORDER-TRACKING — resolveOrderStore → infra store.
	orderStore := resolveOrderStore(workStore, infraStore, cfg, cityPath, nil)
	if _, err := orders.NewStore(beads.OrdersStore{Store: orderStore}).CreateRun("gate-alpha", orders.RunOpts{}); err != nil {
		t.Fatalf("order run create: %v", err)
	}

	// PLAIN WORK — stays in the work/domain store.
	if _, err := workStore.Create(beads.Bead{Title: "real backlog item", Type: "task"}); err != nil {
		t.Fatalf("plain task create: %v", err)
	}
}

// assertReservedPrefixBoundary asserts the ID-prefix half of the invariant over
// real stores: every infra bead carries a reserved class prefix, no work bead
// does.
func assertReservedPrefixBoundary(t *testing.T, workStore, infraStore beads.Store) {
	t.Helper()
	infra, err := infraStore.List(beads.ListQuery{IncludeClosed: true, TierMode: beads.TierBoth, AllowScan: true})
	if err != nil {
		t.Fatalf("infra List: %v", err)
	}
	if len(infra) == 0 {
		t.Fatal("infra store holds no beads; the reserved-prefix boundary is vacuous")
	}
	for _, b := range infra {
		if !config.IsReservedClassPrefix(idPrefixSegment(b.ID)) {
			t.Errorf("infra bead %q (type=%q) lacks a reserved class prefix", b.ID, b.Type)
		}
	}
	work, err := workStore.List(beads.ListQuery{IncludeClosed: true, TierMode: beads.TierBoth, AllowScan: true})
	if err != nil {
		t.Fatalf("work List: %v", err)
	}
	for _, b := range work {
		if config.IsReservedClassPrefix(idPrefixSegment(b.ID)) {
			t.Errorf("work bead %q (type=%q) carries a reserved class prefix", b.ID, b.Type)
		}
	}
}

// assertSingleStoreCityUntouched brings up a second doltlite city WITHOUT the
// infra-store split (GC_INFRA_STORE_SPLIT off for its seed) and confirms it stays
// single-store: no infra scope, and the infra store opener reports absence.
func assertSingleStoreCityUntouched(t *testing.T) {
	t.Helper()
	cityPath := shortSocketTempDir(t, "gc-single-store-")
	cleanupManagedDoltTestCity(t, cityPath)
	writeDoltliteCityTOMLForInfraSplit(t, cityPath)
	materializeBuiltinPacksForTest(t, cityPath)

	// Deliberately DO NOT seed the infra scope: this is an existing single-store
	// city. cityHasInfraStore must stay false and the infra opener must report
	// absence, so class routing is identity and the city is byte-identical.
	if cityHasInfraStore(cityPath) {
		t.Fatal("single-store city unexpectedly reports an infra scope")
	}
	if _, err := os.Stat(infraScopeRoot(cityPath)); !os.IsNotExist(err) {
		t.Fatalf("single-store city has an .gc/infra dir (err=%v); it must be untouched", err)
	}
	cfg, _, err := loadCityConfigWithBuiltinPacks(cityPath)
	if err != nil {
		t.Fatalf("load single-store city config: %v", err)
	}
	if err := startBeadsLifecycle(cityPath, "single-store", cfg, os.Stderr); err != nil {
		t.Fatalf("startBeadsLifecycle (single-store): %v", err)
	}
	t.Cleanup(func() { _ = shutdownBeadsProvider(cityPath) })

	if _, present, err := openCityInfraStoreAt(cityPath); err != nil {
		t.Fatalf("open infra store (single-store): %v", err)
	} else if present {
		t.Fatal("single-store city reports an infra store present; the split leaked into a non-seeded city")
	}
}
