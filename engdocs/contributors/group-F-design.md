# Group F design: per-store-scope gating of orphan-pool-assignment release

Finding #7 of the graph-store-split audit. Branch: `deploy/sqlite-b36-probe-attribution`
(HEAD `6551b7006`; Groups A `0195f407e`, B `ec586c953`, C `adc789f9a` already committed).
All file:line references below were re-located on this branch — do not trust the
audit's line numbers.

Status: DESIGN — no production code in this document. Implement phases in order,
TDD (red test first per phase).

---

## 1. Doctrine re-validation

The governing invariant (Group C, `adc789f9a`) is a **write-path** guarantee:
formula (ClassGraph, `gcg-`) work beads are always created in the configured
graph store (sqlite), never Dolt. Group F remains a real bug **even when that
invariant holds perfectly**, because the broken gate is not about where beads
live — it is about how *query-failure blame* is aggregated across stores:

- `collectAssignedWorkBeadsWithStores` fans one goroutine out per store — the
  city store (which federates the graph leg) at ref `""` plus every rig Dolt
  store (`cmd/gc/build_desired_state.go:1088-1096`) — and collapses every
  goroutine's errors into one boolean `partial`
  (`cmd/gc/build_desired_state.go:1159-1171` and `:1223-1234`).
- That boolean becomes the single global `DesiredStateResult.StoreQueryPartial`
  (`cmd/gc/build_desired_state.go:70`, set at `:912`).
- `releaseOrphanedPoolAssignmentsWhenSnapshotsComplete` bails out of ALL orphan
  release when `result.snapshotQueryPartial()` is true
  (`cmd/gc/pool_session_name.go:94-96`; `snapshotQueryPartial` =
  `StoreQueryPartial || SessionQueryPartial`, `cmd/gc/build_desired_state.go:79-81`).

So with 100% of formula beads graph-resident, one flaky **rig Dolt** leg (the
chronic bd/dolt EOF class) sets the global bool every tick and suppresses
release of `gcg-` orphans whose own store — the city graph/sqlite leg — returned
a complete, healthy snapshot. Molecules stall for as long as the Dolt leg flaps.
Group F is doctrine-independent: it does not weaken the Group C invariant, and
the Group C invariant does not fix it.

## 2. Current signature and every call site of `collectAssignedWorkBeadsWithStores`

Current signature (`cmd/gc/build_desired_state.go:1075-1081`):

```go
func collectAssignedWorkBeadsWithStores(
	cfg *config.City,
	cityStore beads.Store,
	rigStores map[string]beads.Store,
	suspendedRigPaths map[string]bool,
	sessionBeads *sessionBeadSnapshot,
) ([]beads.Bead, []beads.Store, []string, map[string]bool, bool)
```

Returns, in order: work beads, index-aligned stores, index-aligned storeRefs,
`readyAssignedIDs map[string]bool`, `partial bool`.

**New signature** — append the per-scope map as a SIXTH (last) return value, so
every existing destructuring updates by appending one identifier:

```go
) ([]beads.Bead, []beads.Store, []string, map[string]bool, bool, map[string]bool)
//  beads         stores         storeRefs  readyAssignedIDs partial  partialByStoreRef
```

Call sites that must be updated (complete list, verified by
`grep -rn "collectAssignedWorkBeadsWithStores(" cmd/gc/`):

Production (2):
1. `cmd/gc/build_desired_state.go:660` — inside `buildDesiredStateWithSessionBeads`
   (function starts at `:441`). Assigns 5 values today; add a 6th
   (`storePartialByRef`), declared next to `storePartial` at `:653`.
2. `cmd/gc/build_desired_state.go:1064` — the thin wrapper
   `collectAssignedWorkBeads` (`:1060-1066`). It keeps its 2-value signature
   `([]beads.Bead, bool)` — its ~15 test callers (build_desired_state_test.go
   266, 298, 330, 370, 397, 442, 469, 501, 537, 568, 603, 1777, …) are untouched.
   Update its internal destructuring to `result, _, _, _, partial, _ :=`.

Tests calling `collectAssignedWorkBeadsWithStores` directly (17) — each appends
one `_` (or a named var where a new assertion is added):
- `cmd/gc/build_desired_state_test.go`: 1809, 1840, 1887, 1932, 2004, 2068, 2134, 2194, 10426
- `cmd/gc/pool_session_name_test.go`: 1162, 1213, 1263, 1301
- `cmd/gc/assigned_work_scope_graph_storeref_test.go`: 81, 103, 127, 204

There are no other callers (lines 1183 and 3700 of build_desired_state.go /
its test file are comments only).

### Producing the map inside `collectAssignedWorkBeadsWithStores`

The per-goroutine errors are already scoped: `results[idx].errs` is filled by
the goroutine for `stores[idx]`, whose ref is `stores[idx].ref`
(`""` for the city store — first entry `{store: cityStore}` at
`build_desired_state.go:1088` — and `rig.Name` for each rig, `:1094`). The
second (Ready-probe) pass reuses the same `stores` slice indices
(`readyResults[idx]`, `:1190-1234`). Change the two aggregation loops to keyed
form:

- First pass (`:1160-1171`): `for idx, r := range results { … }`; inside the
  existing `for _, err := range r.errs` keep the log line and `partial = true`,
  and add `partialByStoreRef[stores[idx].ref] = true` (allocate the map lazily
  on first error, so a healthy tick returns a nil map).
- Early return (`:1186-1188`): return `partialByStoreRef` as the 6th value.
- Ready pass (`:1223-1234`): same keyed change over `readyResults`.
- Final return (`:1235`): add `partialByStoreRef`.

Invariant to keep: `partial == (len(partialByStoreRef) > 0)` — both derive from
the same `r.errs`. Note the partial-append salvage paths
(`:1126-1128`, `:1145-1148`) DO append beads from a leg that also records an
error; those beads' data is exactly what the per-bead gate must distrust, and
they gate correctly because their storeRef equals the failing leg's key.

## 3. Plumbing: collection → `DesiredStateResult` → release

### 3a. New `DesiredStateResult` field

Add to the struct (`cmd/gc/build_desired_state.go:33-77`), next to
`StoreQueryPartial` (`:66-70`):

```go
// AssignedWorkPartialStoreRefs records, per collection scope, which store's
// assigned-work queries failed this tick ("" = city store, otherwise the rig
// name — the same key space as AssignedWorkStoreRefs BEFORE the
// graph-resident remap). Orphan release gates each bead on its OWNING
// scope's health instead of the global StoreQueryPartial, so one flaky rig
// Dolt leg no longer suppresses release of graph-resident (gcg-) orphans
// whose own store snapshot was complete. nil/empty means no store failed;
// when StoreQueryPartial is set without this attribution (e.g. the squatter
// guard), release falls back to the historical global skip.
AssignedWorkPartialStoreRefs map[string]bool
```

Also extend the `StoreQueryPartial` doc comment (`:66-69`) with one sentence
pointing at the companion field.

Populate it in the constructor at `cmd/gc/build_desired_state.go:900-914`
(`AssignedWorkPartialStoreRefs: storePartialByRef,` next to
`StoreQueryPartial: storePartial,` at `:912`). The other constructor,
`DesiredStateResult{}` at `:454` (suspended city), correctly leaves it nil.
The refresh path (`refreshed := result` struct copy, `:1043`) carries the map
along; no change needed there.

**Ordering note:** the map is produced at `:660` and is NOT touched by
`remapGraphResidentAssignedWorkStoreRefs` at `:669` (Group B). The map's keys
are *collection-time* scopes; the remap rewrites only the index-aligned
`assignedWorkStoreRefs` slice. This asymmetry is intentional — see §4.

### 3b. Wrapper `releaseOrphanedPoolAssignmentsWhenSnapshotsComplete`

Current body (`cmd/gc/pool_session_name.go:83-98`) bails on
`result.snapshotQueryPartial()`. New body:

```go
// Missing session beads make ANY assigned work look orphaned, so the
// session snapshot stays a global gate (§5).
if result.SessionQueryPartial {
	return nil
}
// StoreQueryPartial without per-scope attribution (squatter guard
// city_runtime.go:2134-style setters, hand-built results): historical
// global skip — never release on unattributed partiality.
if result.StoreQueryPartial && len(result.AssignedWorkPartialStoreRefs) == 0 {
	return nil
}
return releaseOrphanedPoolAssignmentsWithPartialScopes(store, cfg, cityPath,
	openSessionBeads, result.AssignedWorkBeads, result.AssignedWorkStores,
	result.AssignedWorkStoreRefs, rigStores, result.AssignedWorkPartialStoreRefs)
```

(The squatter guard sets `result.StoreQueryPartial = true` AFTER the release
call on the daemon tick — `cmd/gc/city_runtime.go:2113` vs `:2134` — so it
never feeds this gate there; the fallback exists for the one-shot path and any
future/test constructor that sets the bool without the map. **Do not reorder
the release call below the squatter guard.**)

The wrapper's production callers are unchanged in shape:
`cmd/gc/city_runtime.go:2113` and `cmd/gc/cmd_start.go:900` (both already pass
`result`/`dsResult`, which now carries the map). Wrapper test callers
(`cmd/gc/cmd_start_test.go:627,650,673`,
`cmd/gc/assigned_work_scope_graph_storeref_test.go:189`) also compile
unchanged.

### 3c. `releaseOrphanedPoolAssignments` — delegate pattern, not signature churn

`releaseOrphanedPoolAssignments` (`cmd/gc/pool_session_name.go:104-217`) has
**34** direct test call sites in `cmd/gc/pool_session_name_test.go` alone.
Follow the codebase's existing wrapper convention
(`collectAssignedWorkBeads` → `collectAssignedWorkBeadsWithStores`):

- Rename the current implementation to
  `releaseOrphanedPoolAssignmentsWithPartialScopes`, adding one trailing
  parameter `partialStoreRefs map[string]bool`.
- Keep `releaseOrphanedPoolAssignments` with its EXACT current signature as a
  one-line delegate passing `nil` for the map. All 34 existing test call sites
  and their behavior are untouched (nil map ⇒ gate compiled out, see §4).

**Do NOT pre-filter the assigned-work slices in the wrapper instead.** The
returned `releasedPoolAssignment.Index` values are indices into the ORIGINAL
`result.AssignedWorkBeads` and are consumed index-aligned by
`filterReleasedAssignedWorkSnapshot` (`cmd/gc/city_runtime.go:2121`,
implementation `:2403-2440`, which cross-checks `assignedWorkBeads[r.Index].ID != r.ID`).
Pre-filtering would silently invalidate every released Index. Gating must
happen inside the loop, preserving `i`.

## 4. Per-bead gating logic (the fix itself)

Inside `releaseOrphanedPoolAssignmentsWithPartialScopes`:

**(i) Conservative bail when refs are unusable** — right after `storeRefAware`
is computed (`cmd/gc/pool_session_name.go:121-124`):

```go
// Per-scope partiality can only be attributed through index-aligned
// storeRefs; without them, fall back to the historical global skip.
if len(partialStoreRefs) > 0 && !storeRefAware {
	log.Printf("releaseOrphanedPoolAssignments: store snapshot partial (%d scope(s)) with no store-ref alignment; skipping release this tick", len(partialStoreRefs))
	return nil
}
```

**(ii) Hoist the graph-residency prefix once, before the loop** (this is the
exact residency predicate the loop already uses at `:198-202` for the
`ownerStore` remap, and that Group B's
`remapGraphResidentAssignedWorkStoreRefs` uses —
`cmd/gc/assigned_work_scope.go:72-80`):

```go
var graphPrefix string
if gol, ok := beads.GraphOnlyListFor(store); ok {
	if pfx := gol.GraphIDPrefix(); pfx != "" {
		graphPrefix = pfx + "-"
	}
}
```

**(iii) The per-bead gate** — first statement after the status filter at
`:139-141`, before any assignee/session logic (earliest point where `wb` and
`i` are in scope; everything upstream of the release write is read-only except
`probeDetachedWork` at `:206`, which the gate must precede):

```go
if len(partialStoreRefs) > 0 {
	// A bead is gated on the health of the scope it was COLLECTED from.
	// Graph-resident (gcg-) beads physically live in the city graph store
	// even when Group B retagged their logical storeRef to the routed rig
	// (remapGraphResidentAssignedWorkStoreRefs), so they gate on the city
	// leg's key "" — a flaky rig Dolt leg must not strand them. Everything
	// else was collected from the store its ref names.
	scope := assignedWorkStoreRefs[i]
	if graphPrefix != "" && strings.HasPrefix(wb.ID, graphPrefix) {
		scope = "" // city/graph leg
	}
	if partialStoreRefs[scope] {
		continue
	}
}
```

**(iv) Reuse the hoisted prefix at the existing ownerStore remap** — replace
`:198-202` with:

```go
if graphPrefix != "" && strings.HasPrefix(wb.ID, graphPrefix) {
	ownerStore = store
}
```

Byte-identical to the current per-iteration `GraphOnlyListFor` call and
guarantees the partiality-scope decision and the physical-owner decision use
the SAME predicate — if one classifies a bead as graph-resident, both do.

### Why the key is `""` for graph-resident beads (traced)

- `stores[0] = workStore{store: cityStore}` with zero-value `ref == ""`
  (`build_desired_state.go:1088`); rig legs get `ref: rig.Name` (`:1094`).
  So the city/graph scope's key in `partialByStoreRef` is exactly `""`.
- Rig Dolt stores never federate `gcg-` beads (the graph leg belongs to the
  city Router only — see `graphFederatingStore` doc,
  `cmd/gc/pool_session_name_test.go:2161-2165`), so every `gcg-` bead in the
  snapshot was collected by the city goroutine and tagged ref `""` at
  collection.
- Group B (`ec586c953`) then retags those refs in place to the routed RIG name
  (`build_desired_state.go:669`, `assigned_work_scope.go:68-89`) so rig-scoped
  reachability gates match. Post-remap, `assignedWorkStoreRefs[i]` for a `gcg-`
  bead may be `"gascity"` — gating on it would recreate the bug for exactly
  the beads Group F exists to protect. Hence the residency override back to
  `""`.
- Non-graph beads' refs are never touched by the remap (it only rewrites
  entries with `storeRefs[i] == ""` AND a graph-prefixed ID,
  `assigned_work_scope.go:81-88`), so for them
  `assignedWorkStoreRefs[i]` == collection scope. The mapping
  `scope(bead) = graphResident ? "" : storeRefs[i]` therefore equals the
  collection-time source ref for every bead.

Known theoretical edge: a graph-prefixed bead ID that somehow materialized in a
rig Dolt store would be mis-scoped to `""`. The existing ownerStore remap at
`:198-202` already makes the identical assumption (it forces such a bead's
writes to the city store), so this design adds no new exposure.

## 5. `SessionQueryPartial` stays a global gate (unchanged)

Missing session beads make ANY assigned work look orphaned — orphan detection
compares each bead's assignee against the open-session snapshot
(`openIdentifiers`/`legacyOpenIdentifiers`, `pool_session_name.go:126-135`),
and session beads live in the city store regardless of which store holds the
work. So the wrapper keeps `if result.SessionQueryPartial { return nil }` as an
unconditional global bail (§3b). No change to how `SessionQueryPartial` is set
(`build_desired_state.go:437`, `city_runtime.go:2104`, `cmd_start.go:892`).

Equally unchanged: every OTHER consumer of the global flags — the drain gates
at `city_runtime.go:2180` and `:2225`, `cmd_start.go:934`, and the trace fields
(`city_runtime.go:2334-2340`, `:2915-2916`) keep using
`snapshotQueryPartial()`/`StoreQueryPartial` globally. Group F narrows exactly
one consumer: `pool_session_name.go:94`. Keep the `snapshotQueryPartial()`
method; it still has three call sites.

## 6. TDD plan — failing tests first

Existing fakes to reuse (do not invent new ones):
- `graphFederatingStore` (`cmd/gc/pool_session_name_test.go:2166-2220`) — the
  graph_store=sqlite primary-store shape: MemStore + federated `gcg-` graph leg
  + `ListGraphOnlyHandle` returning prefix `"gcg"` (via `graphOnlyAssignedReader`,
  `cmd/gc/assigned_work_scope_test.go:645-658`).
- `graphOnlyAssignedStore` (`cmd/gc/assigned_work_scope_test.go:631-643`) —
  available, but `graphFederatingStore` is the right one here (release needs
  Get/Update federation).
- `partialAssignedWorkStore` (`cmd/gc/build_desired_state_test.go:196-245`) —
  MemStore whose List/Ready return `*beads.PartialResultError`, for the
  collection-attribution test.
- Model test to copy setup from:
  `TestReleaseOrphanedPoolAssignments_ReleasesGraphResidentBeadBoundToRigStore`
  (`cmd/gc/pool_session_name_test.go:2230-2268`) — gcg-9001, routed_to
  run-operator, storeRefs `["gascity"]`, rig MemStore that does NOT hold the
  bead.

### Phase 1 (red): collection attribution — `cmd/gc/build_desired_state_test.go`

`TestCollectAssignedWorkBeadsWithStores_AttributesPartialByStoreRef`
- cfg with `Rigs: []config.Rig{{Name: "repo", Path: t.TempDir()}}`; city =
  plain `beads.NewMemStore()` holding one healthy in-progress assigned bead;
  `rigStores = map[string]beads.Store{"repo": &partialAssignedWorkStore{MemStore: beads.NewMemStore(), partialInProgress: true}}`.
- Assert: `partial == true`; `partialByStoreRef == map[string]bool{"repo": true}`
  (no `""` key); healthy city bead still collected with ref `""`.
- Second sub-case (or sibling test
  `TestCollectAssignedWorkBeadsWithStores_AttributesCityPartialToEmptyRef`):
  city = `partialAssignedWorkStore{partialInProgress: true}`, no rigs →
  `partialByStoreRef == map[string]bool{"": true}`.
- Fails first as a COMPILE failure (6th return value) — that is the red state
  for this phase; make it green by implementing §2 only.

### Phase 2 (red): per-scope release gating — `cmd/gc/pool_session_name_test.go`

All call the new `releaseOrphanedPoolAssignmentsWithPartialScopes` (last arg =
the map). Red state: function does not exist / gate not implemented.

(a) `TestReleaseOrphanedPoolAssignments_GraphResidentReleasedWhenOnlyRigLegPartial`
   — clone the `:2230` setup verbatim (graphFederatingStore city store, rig
   MemStore, storeRefs `["gascity"]` — the post-Group-B remapped RIG ref), pass
   `map[string]bool{"gascity": true}`. Expect `released == [{gcg-9001, 0}]` and
   graph bead reopened (`Status "open"`, empty assignee). This test encodes the
   CRITICAL SUBTLETY: naive gating on `storeRefs[i]` would skip the bead and
   fail this test.

(b) `TestReleaseOrphanedPoolAssignments_HoldsRigBeadWhenOwnRigLegPartial`
   — non-graph orphan (`ga-…` in a rig MemStore; copy the setup of
   `TestReleaseOrphanedPoolAssignments_UpdatesRigStoreFallback`, `:1319-1338`),
   storeRefs `["gascity"]`, map
   `{"gascity": true}`. Expect `released` empty and the bead's status/assignee
   untouched (Get and assert, as `:2149-2158` does).

(c) `TestReleaseOrphanedPoolAssignments_ReleasesRigBeadWhenOtherLegPartial`
   — same rig bead, map `{"otherrig": true}`. Expect released. Proves the gate
   is per-leg, not any-leg.

(d) `TestReleaseOrphanedPoolAssignments_PartialScopesWithoutStoreRefsSkipsAll`
   — populated map, `assignedWorkStoreRefs` nil (storeRefAware false). Expect
   nil released (the §4(i) conservative bail).

### Phase 3 (red): wrapper wiring — `cmd/gc/pool_session_name_test.go` (or cmd_start_test.go)

(e) `TestReleaseOrphanedPoolAssignmentsWhenSnapshotsComplete_ReleasesHealthyLegWhenOtherLegPartial`
   — the headline end-to-end failing test for the bug: build
   `DesiredStateResult{AssignedWorkBeads: [gcg bead], AssignedWorkStores: [rigStore], AssignedWorkStoreRefs: ["gascity"], StoreQueryPartial: true, AssignedWorkPartialStoreRefs: map[string]bool{"gascity": true}}`
   with `store` = graphFederatingStore. Today's code returns nil at
   `pool_session_name.go:94`; expect 1 release.

(f) `TestReleaseOrphanedPoolAssignmentsWhenSnapshotsComplete_SessionPartialBlocksDespiteHealthyScopes`
   — same result but `SessionQueryPartial: true` and a map naming only a
   NON-owning leg. Expect nil released — proves §5's global gate survives the
   per-scope split.

(g) Existing `TestReleaseOrphanedPoolAssignmentsWhenSnapshotsComplete_PartialSkipsCompleteReleases`
   (`cmd/gc/cmd_start_test.go:607-694`) **must pass UNCHANGED**: its first case
   sets `StoreQueryPartial: true` with NO map — the §3b map-less fallback is
   what keeps it green. Treat any needed edit to this test as a design
   violation, not a test bug.

### Phase 4: mechanical call-site updates + full gates

Update the 17 test destructurings (§2) with a trailing `_`; run:

```bash
export GOCACHE=/data/tmp/tmp.XgAejbMpvc
go build ./cmd/gc/
go vet ./cmd/gc/
go test ./cmd/gc/ -run 'TestReleaseOrphanedPoolAssignments|TestCollectAssignedWorkBeads|TestBuildDesiredState' -count=1
make test   # fast unit baseline per repo gates
```

## 7. Byte-identical guarantee for default Dolt cities — precise statement

Three regimes, all traced through the code above:

1. **Healthy tick (any city shape).** No goroutine records an error ⇒
   `partialByStoreRef` is nil ⇒ `len(partialStoreRefs) > 0` is false at both
   the §4(i) bail and the §4(iii) gate ⇒ the loop body is byte-identical to
   today (the hoisted `graphPrefix` is the same computation `:198-202` already
   performs per iteration). `StoreQueryPartial` false ⇒ wrapper proceeds, as
   today.

2. **Single-store city (default Dolt, no rigs — or any city where only the one
   store exists).** A partial tick yields `partialByStoreRef == {"": true}` and
   every bead's scope is `""` (no rigs ⇒ every ref is `""`; `GraphOnlyListFor`
   absent on a default Dolt city ⇒ `graphPrefix == ""` ⇒ no residency override
   ever fires, `internal/beads/graph_only_list.go:8-10` mandates prefix `""`
   in the identity phase). Every bead is skipped ⇒ zero releases ⇒ identical to
   today's global bail. The degenerate map IS the old behavior.

3. **Attribution missing.** Any `DesiredStateResult` carrying
   `StoreQueryPartial=true` with an empty/nil map (squatter guard-style
   setters, hand-built values) hits the §3b fallback ⇒ global skip, identical
   to today.

**Deliberate, scoped behavior delta (state it honestly):** a MULTI-store Dolt
city (rigs, no graph backend) with exactly one flaky leg previously released
nothing; now it releases orphans on the healthy legs. This is the audit's
prescription generalized — the same wrong-blame bug, minus the graph twist —
and it is safe because every per-bead release decision consults only (a) the
session snapshot, still globally gated by `SessionQueryPartial` (§5), and
(b) live re-validation against the bead's own healthy owner store
(`liveWorkAssignmentStillReleasable`, `pool_session_name.go:463-485`, which
fails CLOSED on error at `:474-477`, and `liveOpenSessionAssignmentExists`,
`:383-423`, which fails SAFE — returns "owner exists" — on error at
`:408-411`). Nothing about a flaky sibling leg feeds those decisions. If the
owner later wants graph-only narrowing, tighten the map, not the mechanism.

## 8. MUST-FIX risks

1. **Do not pre-filter the work slices in the wrapper.** `released[].Index`
   must index the ORIGINAL `result.AssignedWorkBeads`
   (`filterReleasedAssignedWorkSnapshot`, `city_runtime.go:2403-2440` verifies
   `assignedWorkBeads[r.Index].ID == r.ID`). Gate with `continue` inside the
   loop only.
2. **Map-less `StoreQueryPartial` fallback is mandatory** (§3b). Without it,
   the existing `cmd_start_test.go:607` regression test breaks AND any caller
   that sets the bool without attribution silently loses its safety gate.
3. **`!storeRefAware` + populated map ⇒ global skip** (§4(i)). Never gate a
   bead whose scope you cannot attribute; `assignedWorkStoreRefs[i]` may not
   exist or align.
4. **One residency predicate.** The partiality-scope decision (§4(iii)) and the
   physical-owner remap (`:198-202`) must share the single hoisted
   `graphPrefix` derived from `beads.GraphOnlyListFor(store)`. Two independent
   computations can drift (Group A's whole finding was a wrapper dropping
   `ListGraphOnlyHandle` — `0195f407e`).
5. **Key space discipline.** `partialByStoreRef` is keyed by COLLECTION-time
   source ref (`""` city, `rig.Name` rigs, `build_desired_state.go:1088,1094`).
   Never key or look it up with post-remap refs for graph beads; never run
   `remapGraphResidentAssignedWorkStoreRefs` over the map.
6. **Ready-pass attribution.** Both error loops (`:1160-1171` AND `:1223-1234`)
   must feed the map — a leg that lists clean but fails its Ready probe is
   still an untrusted leg. Both index into the same `stores` slice.
7. **Ordering in `city_runtime.go`.** The squatter guard sets
   `result.StoreQueryPartial = true` at `:2134` AFTER the release call at
   `:2113`. Do not move the release call below it, and do not have the guard
   populate `AssignedWorkPartialStoreRefs` — its partiality is
   store-identity-wide by design.
8. **Nil-map safety.** Reads on a nil `map[string]bool` are safe in Go
   (`partialStoreRefs[scope]` returns false), but the design never reaches the
   gate with a nil map anyway (`len(...) > 0` guard); keep that guard so the
   nil path stays provably byte-identical.
9. **Two adjacent `map[string]bool` returns** (`readyAssignedIDs` at position
   4, `partialByStoreRef` at position 6, with `bool` between). The compiler
   will not catch a positional swap against another map. The Phase 1 test's
   exact-map assertion (`{"repo": true}` with rig-only failure) is the guard —
   do not weaken it to a `len()` check.

## Implementation order (recap)

1. Phase 1 red → §2 (signature + map production + 19 call-site updates).
2. §3a (`DesiredStateResult` field + constructor) — Phase 3 tests now compile.
3. Phase 2 red → §3c delegate split + §4 gating.
4. Phase 3 red → §3b wrapper rewrite.
5. Phase 4 gates (`go build`, `go vet`, targeted tests, `make test`);
   confirm `cmd_start_test.go:607` passes unmodified.
