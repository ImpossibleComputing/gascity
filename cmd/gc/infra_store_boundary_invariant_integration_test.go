//go:build integration

package main

import "testing"

// This is the E2.5 integration-tier sketch of the boundary invariant (design
// step 2b). It is a documented TODO: the FAST tier
// (infra_store_boundary_invariant_test.go) is the E2.2 forcing function and is
// sufficient to drive E2.3. The integration tier is what ultimately proves the
// CLI one-shots (sling, nudge, mail) route correctly, because they run as real
// subprocess commands against the true production open path
// (openStoreResultAtForCity / openCityInfraStoreResultAt, incl.
// wrapStoreWithBeadPolicies + reserved-prefix resolution).
//
// It is intentionally left as a Skip until E2.4 lands (the infra scope is not
// created by `gc init` yet, and reserved-prefix minting does not exist), so
// there is nothing to open on a real city today.
//
// Intended shape (implement in E2.5, after E2.3 + E2.4):
//  1. `gc init` a temp city — which, post-E2.4, creates the `.gc/infra` scope.
//  2. `gc rig add <rig>`; `gc start` with the file or doltlite provider.
//  3. `gc sling <rig>/<agent> --formula <f>`; let one reconciler tick run;
//     `gc stop`.
//  4. Open EVERY store via the true production open path:
//     openStoreResultAtForCity(<rig root>, cityPath) for each domain store and
//     openCityInfraStoreResultAt(cityPath) for the infra store.
//  5. Run the SAME two assertions the fast tier uses (assertStoreClassBoundary,
//     wantInfra=false for each domain store, wantInfra=true for the infra
//     store) — the helper is package-exported precisely so this tier reuses it
//     verbatim against the real store shape.
//  6. Additionally assert (design e23SlingSplitBrain step 4): after the sling,
//     ZERO gc.root_bead_id-carrying beads exist in the rig domain store — the
//     end-to-end proof that E2.3 killed the split-brain through the real CLI.
func TestInfraStoreBoundaryInvariantIntegration(t *testing.T) {
	t.Skip("E2.5 worklist: requires the `gc init` infra-scope seed (E2.4) and the sling GraphStore wiring (E2.3); " +
		"neither has landed. See the file header for the intended real-city gc init/start/sling/stop + " +
		"assertStoreClassBoundary shape.")
}
