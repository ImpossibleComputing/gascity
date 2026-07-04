# Group E design: cross-leg dependency edges — written blind, never release, reclaimed as garbage

Findings #26, #27, #57, #58, #59, #60 of the graph-store-split audit. The largest,
cross-repo group.

Repos and branches:

- **gascity** `/data/projects/gascity/.claude/worktrees/beads`, branch
  `deploy/sqlite-b36-probe-attribution` (HEAD `6551b7006`; Groups A `0195f407e`,
  B `ec586c953`, C `adc789f9a` committed).
- **beads** `/data/projects/beads`, branch `local/deploy-current-integrated`
  (HEAD `dab71c5eb`, Group H already landed there). The working tree carries
  pre-existing uncommitted changes — investigated in §3; they do NOT implement
  any part of Group E.

All `file:line` references were re-located on these branches — do not trust the
audit's approximate line numbers. Paths without a repo prefix are gascity;
beads-repo paths are written `beads:…`.

Status: DESIGN — no production code in this document. Two owner decisions gate
most of the implementation; a decision-independent safe subset (§6) can land now.

---

## 1. OWNER DECISIONS REQUIRED

Both decisions are framed below with the code-level blast radius. **They are
coupled**: Decision 1 largely determines the coherent answer to Decision 2 (see
the coupling note after Decision 2). Nothing in §7 should be implemented until
both are made; everything in §6 is correct under every combination.

### Decision 1 — Representation of a cross-leg blocking dependency

**Question:** When a bead on one leg must wait for a bead on the other leg
(graph/sqlite `gcg-` ↔ work/Dolt `ga-`), what is the durable representation of
that gate?

Today the representation is a **raw dependency row pinned to the dependent's
leg** (`internal/coordrouter/router_mutation.go:72-74` routes `DepAdd` by
`backendForID(issueID)`), and each leg's readiness reader can only see its own
bead table — so the row either blocks forever (sqlite leg,
`internal/beads/sqlite_store.go:860-866`) or never blocks (Dolt leg via `bd`,
`beads:internal/storage/issueops/blocked_state.go:141-183`). Both are wrong.

**Option A — graph/leg-local PROXY/GATE bead released by the controller**
(audit-recommended, §5 of the audit).

At edge-write time, partition by `GraphIDPrefix()`
(`internal/coordrouter/router_federation.go:189-200`): same-leg blockers embed
as today; a cross-leg blocker becomes a **gate bead resident on the dependent's
own leg** (for a `gcg-` step waiting on `ga-X`: a `gcg-` bead of `issue_type:
gate` carrying `gc.gate_target_id=ga-X` + `gc.gate_target_store_ref`), and the
blocking edge becomes same-leg (`gcg-step → gcg-gate`). A controller sweep
lists open gate beads, `Get`s each target through the Router, and closes the
gate when the target closes. No raw cross-leg row is ever written again.

This generalizes a pattern the drain path already uses: `drainProjectedBlockerIDs`
(`internal/dispatch/drain.go:572-608`) already substitutes in-manifest source
members with their projected `gcg-` item roots precisely because "drains do not
close source members, so that edge would never release" (`drain.go:592-600`).
Option A extends the same projection to the blockers drain currently passes
through raw.

*Blast radius:*
- gascity only. New gate bead kind (note: `issue_type` `gate` is already
  excluded from ready candidacy on the sqlite leg, `sqlite_store.go:859`, so a
  gate bead can never itself dispatch) + one new idempotent controller sweep
  (status-copy only — no judgment in Go; converges like every other patrol).
- Write-site changes: `drainWorkflowExternalDeps`/`drainProjectedBlockerIDs`
  (`internal/dispatch/drain.go:957-986`, `:572-608`), the ExternalDeps embed
  and DepAdd paths in `internal/molecule/molecule.go` (`:550-559`, `:713`,
  `:831-840`, `:917`, `:934`), the graph-apply plan builder
  (`internal/molecule/graph_apply.go:223-235`, `:409-434`), and the two
  work→graph attach-gate writers (`internal/molecule/molecule.go:303`/`:359`,
  `cmd/gc/cmd_formula.go:689`/`:723` → `:892-908`).
- Requires a heal for already-written raw cross-leg rows (live maintainer-city
  attach gates and any stuck drain edges) — one-time sweep converting them to
  gates or clearing them on blocker closure.
- beads repo needs **only the doctor carve-out** (§6.1). `blocked_state.go` is
  untouched → the default Dolt product is byte-identical.
- Readiness latency for gate release = one controller sweep interval (an
  eventual-consistency window that did not exist when both beads shared a
  store).

**Option B — teach readiness to resolve absent dep targets THROUGH the Router.**

`SQLiteStore.Ready` (and the native `DoltliteReadStore.Ready`) would, on
encountering a dep target absent from the local bead table, resolve its status
via the federated Router (per-scan batch `Get` against the other leg, or a
controller-maintained shadow status table).

*Blast radius:*
- **Recouples the graph leg's readiness to Dolt availability** — the exact
  coupling the store split exists to remove. Every graph readiness scan (the
  controller's dispatch loop input) gains cross-store I/O; on the default
  build the Dolt leg is a `bd` CLI fork (`internal/beads/bdstore.go:2419`
  `Ready` → `bd ready --json`), i.e. ~1s subprocess per unresolved target
  batch, per tick. A flapping Dolt leg (the chronic bd/dolt EOF class that
  motivated Group F) stalls **all** graph dispatch city-wide, or dispatches
  wrongly if missing targets are then treated as non-blocking.
- Layering: `SQLiteStore` (Layer 1 substrate) would need a reference up into
  the federation (`coordrouter`) — an upward dependency the architecture
  forbids; realistically an injected resolver callback, which still smuggles
  federation into the store.
- **Structurally incomplete for the reverse direction:** the Dolt leg's
  blocked computation runs *inside the beads repo* (`bd`), which has no
  Router, no sqlite driver, and no access to the graph store at all
  (verified: no Router/GraphIDPrefix/sqlite backend anywhere in
  `/data/projects/beads` — §5.B). `bd` can never resolve a `gcg-` target, so
  the work→graph gate (#27) would still need a controller-driven release —
  i.e. half of Option A anyway. Option B degenerates into two asymmetric
  mechanisms.
- beads repo would additionally need the "unresolvable configured-foreign-prefix
  targets block" semantic (Decision 2 Option A) plus config plumbing for the
  foreign prefix set.

**Recommendation:** Option A, applied symmetrically (leg-local gate on
whichever leg the dependent lives). **Tradeoff being accepted:** gate release
is asynchronous (a sweep interval of added latency on cross-leg unblocking,
plus a new bead kind whose lifecycle must be healed like any other), in
exchange for keeping each leg's readiness computation local, synchronous, and
availability-isolated — and for confining the beads-repo change to the doctor
carve-out. Owner must sign off; do not implement either option before then.

### Decision 2 — #27: what does a work-leg dep row on an unresolvable foreign-prefix target MEAN?

**Question:** The attach gate (`ga-source → blocks → gcg-root`,
`internal/molecule/molecule.go:303`) is pinned to the Dolt leg
(`router_mutation.go:72-74`) and stored as `depends_on_external='gcg-…'` (raw,
no `external:` literal — `beads:internal/storage/dolt/dependencies.go:22-56`,
classification at `:32-40`). The two read paths **disagree about what that row
means**:

- **Default build** (`//go:build !gascity_native_beads`,
  `cmd/gc/doltlite_store_default.go:1-9`: `openOptimizedDoltliteStore` always
  returns `(nil,false)` → the store is a plain `BdStore`): readiness is `bd
  ready` (`internal/beads/bdstore.go:2419`, args built at `:2453`), computed by
  beads' `is_blocked` derivation, which **never references
  `depends_on_external`** — every blocking predicate is an inner JOIN on
  `depends_on_issue_id`/`depends_on_wisp_id`
  (`beads:internal/storage/issueops/blocked_state.go:141-183`; ready reads the
  flag via `beads:internal/storage/sqlbuild/ready.go:96` `is_blocked = 0`). A
  missing/foreign target therefore **never blocks** → the gate is **silently
  inert** → the gated source bead reports ready and can re-dispatch while its
  formula is still running, voiding attach/formula ordering (#27/#57/#59).
- **Native build** (`//go:build gascity_native_beads`,
  `cmd/gc/doltlite_store_native.go:15-24`, additionally gated on
  `GC_NATIVE_DOLTLITE_BEADS` truthy at `:16`, `:26-33`): readiness is
  `DoltliteReadStore.Ready` (`internal/beads/doltlite_read_store.go:291-344`),
  whose blocker predicate **includes `depends_on_external` in the target
  COALESCE** (`:89`) and LEFT-JOINs only the local issues table (`:92`); a
  missing blocker yields `COALESCE(blocker_issue.status,'') = ''` which `!=
  'closed'` (`:93`, `:104`) → the row is **permanently blocking** (no release
  is possible: nothing on the Dolt leg ever learns the `gcg-` root closed).

Both semantics are live in the codebase; the fix must bake in one. Note the
Router's own doc comment (`router_mutation.go:69-71`) calls the Work store "the
pinned home for **ready-blocking** deps" — i.e. the *writer's intent* was
blocking; the default build silently fails that intent.

**Option A — BLOCKING** (the audit's beads-half fix (b)): for blocking dep
types, treat an unresolvable target whose prefix is a configured same-city
foreign prefix as blocking (change
`beads:internal/storage/issueops/blocked_state.go` mark/unmark + the
`waits-for` gate at `:22-58`; add a config knob — the existing
`allowed_prefixes` machinery (`beads:internal/storage/domain/db/config.go:220-224`,
`beads:internal/utils/id_parser.go:69`) is create-time-only today and would
need a sibling like `foreign_blocking_prefixes`).

*Blast radius:* beads-repo semantic change + config plumbing (gascity must set
the knob per rig store); "blocking" alone is "blocked **forever**" unless
paired with a release mechanism (Decision 1A's controller gate-close, or
controller `DepRemove` of the raw row on root closure) — `bd` cannot observe
the graph leg (§5.B). Any stray foreign-prefix garbage row then wedges a work
bead until doctor surfaces it (doctor must report, never delete — §6.1). Also
changes behavior for every beads consumer that sets the knob; default
(knob-unset) stays byte-identical.

**Option B — NON-BLOCKING (today's default semantic, made consistent):**
declare the raw cross-leg row semantically inert; ordering is carried entirely
by the gascity-side representation (Decision 1A gates). Then: (i) align the
native reader — `doltliteReadyIssueWhere` must stop treating unresolvable
external targets as open blockers (drop `depends_on_external` from the blocking
COALESCE at `doltlite_read_store.go:89`, or exclude unresolvable ones) so both
builds agree; (ii) make the Router **fail loud** (or transparently mint the
gate) on any cross-leg blocking `DepAdd`, so no future writer silently gets a
no-op gate.

*Blast radius:* gascity-only (beads keeps only the doctor carve-out); but it
permanently defines "raw cross-leg rows are decorative", so the migration/heal
for existing attach-gate rows must install the replacement gates **before** any
consumer starts trusting the new semantic.

**Recommendation:** decide Decision 1 first. If Decision 1 = Option A (proxy
gates), Decision 2 Option B is the coherent complement (cross-leg raw rows
cease to be written; the residual question is only how strictly to reject
them). If Decision 1 = Option B (router resolution), Decision 2 **must** be
Option A, accepting the beads-side semantic change and the release-mechanism
dependency. **Tradeoff:** Option A preserves the writer's intended gate
semantics inside beads itself (defense in depth if a gascity release loop
stalls) at the cost of a cross-repo semantic + config surface; Option B keeps
beads pristine at the cost of concentrating all correctness in the gascity
controller.

---

## 2. Doctrine re-validation: which cross-leg edges actually survive

The Group C doctrine (`adc789f9a`) is a **write-path invariant**: formula
(ClassGraph, `gcg-`) work beads are always created in the sqlite graph store,
never Dolt. The audit's cross-leg analysis predates its enforcement and assumed
Dolt-resident formula work could exist. Re-derived against current code:

**Population (a) — graph → work (`gcg-` step blocks on `ga-` bead). REAL, narrow.**
The doctrine does NOT eliminate it, because the *blocker* is legitimately
Dolt-resident. Concrete producer — the drain path:

- Drained convoy members are work (`ga-`) beads. `drainProjectedBlockerIDs`
  (`internal/dispatch/drain.go:572-608`) reads each member's deps and projects
  **in-manifest** blockers to their `gcg-` item roots (`:590-591`) or skips
  them (`:592-600`), but an **out-of-manifest** blocker passes through **raw**
  (`:589` — `blockerID = dependsOnID`, e.g. an upstream `ga-` bead not part of
  this drain).
- Those raw IDs fan out onto **every step** of the item recipe as
  `ExternalDep{Type:"blocks"}` (`drainWorkflowExternalDeps`,
  `drain.go:957-986`), and again post-hoc via `ensureDrainWorkflowBlocksOn` →
  `ensureBlockingDependency` (`drain.go:610-629`,
  `internal/dispatch/control.go:284-295`).
- The steps are `gcg-` (doctrine), so the edges pin to the sqlite leg
  (`router_mutation.go:72-74` FROM-bead routing; or are written directly by
  `ApplyGraphPlan`, `internal/beads/sqlite_store_graph_apply.go:152-161`, which
  inserts edges with **zero target validation** via `depAddTx` — the literal-ID
  branch of `graphApplyResolveRef` at `:177-182`).
- sqlite `Ready` then blocks forever (#26): `internal/beads/sqlite_store.go:860-866`
  — `LEFT JOIN beads blocker ON blocker.id=d.depends_on_id … COALESCE(blocker.status,'') <> 'closed'`;
  a `ga-` target is absent → status `''` → treated as unresolved → the
  drain-item workflow never becomes ready, the drain tally stalls permanently.

Secondary producer: any caller-supplied `ExternalDeps` with a work-resident
target (`internal/molecule/molecule.go:106-111` — `DependsOnID` is "an
already-existing bead" with **no residency validation**; embed at `:550-559`
and `:831-840` into `b.Needs`, persisted by `SQLiteStore.Create` via
`depsFromBeadFields` (`internal/beads/caching_store.go:952-976`, applied in
`sqlite_store.go` `Create`'s dep loop); parent-child variants via explicit
`DepAdd` at `:713`, `:917`, `:934`).

**Population (b) — work → graph (`ga-` source blocks on `gcg-` root). REAL, and
the main live population.** This is the attach gate:

- `molecule.Attach` wires `store.DepAdd(attachBeadID, result.RootID, "blocks")`
  (`internal/molecule/molecule.go:303`; idempotent duplicate path `:340-359`,
  DepAdd at `:359`).
- Attach callers split by residency of `attachBeadID`:
  - `internal/dispatch/control.go:514` attaches onto **control beads**
    (`control.ID`) — control beads are workflow `gcg-` residents under the
    doctrine → **same-leg**, not cross-leg.
  - `cmd/gc/cmd_formula.go` `gc formula cook --attach <bead>`: the v1 path
    calls `molecule.Attach` at `:767`; the graph-v2 path instantiates then
    wires the gate via `ensureFormulaCookAttachDep` at `:689` (existing-root
    branch) and `:723` (fresh instantiate), helper at `:892-908`
    (`store.DepAdd(attachBeadID, rootID, "blocks")` at `:905`). The attach
    target is a caller-supplied **work source bead** (`ga-`) — every adopt-PR
    source in maintainer-city carries one of these gates.
- The edge pins to the **Dolt leg** (`router_mutation.go:72-74`), is stored as
  `depends_on_external='gcg-…'` **raw** (cross-prefix classification,
  `beads:internal/storage/dolt/dependencies.go:15-17`, `:32-40`; the schema
  *forces* this — `depends_on_issue_id` carries FK `fk_dep_issue_target` →
  `issues(id)`, `beads:internal/storage/schema/migrations/0041_split_dependencies_target.up.sql`,
  with `ck_dep_one_target` requiring exactly one target column).
- Read semantics then split by build (#27, Decision 2 above), and **`bd doctor
  --fix` deletes the row** (#58, §5.B4).

Note sling itself creates **no** dependency edges — source↔workflow linkage in
the sling path is metadata (`gc.source_bead_id`,
`internal/sling/sling_core.go:635`; `gc.root_bead_id`/`gc.root_store_ref` are
metadata stamps, e.g. `molecule.go:274-282`). Population (b) exists exactly
where `cook --attach` (or a future direct `molecule.Attach` on a work bead) is
used.

**Population (c) — graph → graph.** Same-leg (recipe deps `molecule.go:695`,
fanout `internal/dispatch/fanout.go:191`, retry/ralph wiring, drain projected
item-root edges). Excluded — not cross-leg.

**Population (d) — convoy `tracks` edges.** `internal/convoy/membership.go:28`
(`TrackItem`, type constant `:12`) may cross legs (a `gcg-` drain-unit convoy
tracks `ga-` members) but `tracks` is **not** a ready-blocking type
(`sqlite_store.go:864` blocking set = `blocks`,`waits-for`,`conditional-blocks`;
same on the beads side). Non-blocking → out of readiness scope. Doctor scope:
these rows pin to the convoy's leg (graph) which `bd doctor` never opens, so
they are not in #58's kill zone either.

**Verdict: the doctrine NARROWS Group E but does not empty it.** What it kills
is the audit's implicit "arbitrary formula work on Dolt" edge space; what
survives is exactly **two blocking populations, each with one canonical writer
choke point**: (a) drain out-of-manifest blockers → sqlite-leg `gcg-→ga-` rows
(#26), and (b) the cook-attach gate → Dolt-leg `ga-→gcg-` rows in
`depends_on_external` (#27/#57/#59, and the entirety of #58's deletion
exposure). This is loudly **not near-zero** — population (b) sits on the live
merge-pipeline path — but it converts the fix from "generic edge partition
everywhere" to "two insertion points plus a doctor carve-out," which
substantially shrinks §7.

Scope corollary for #58: `bd doctor` runs only against local Dolt stores
(`beads:cmd/bd/doctor/validation.go:21-41`, `fix/validation.go:31` →
`openDoltDB`), never the gascity sqlite graph store — so doctor threatens
**only population (b)** rows. Population (a) rows live in gascity's sqlite
`deps` table, beyond doctor's reach (their failure mode is #26's
blocked-forever, not deletion).

Correction to the audit's fix (c): "cross-store cycle check at the Router
boundary (federated DepList before DepAdd)" was filed as a *beads* landing, but
**the Router lives in gascity** (`internal/coordrouter/`); the beads repo has
no router, no `GraphIDPrefix`, no `gcg-` symbol (§5.B). Landing (c) is a
gascity change (§7.3).

---

## 3. Pre-existing beads dirty-tree investigation

`git status` in `/data/projects/beads` shows 8 modified tracked files plus
untracked local runtime debris (`.beads/formulas/*`, `.gc/`, `.claude/skills/`,
hook copies, `.local_version` — a gascity city was initialized inside the beads
repo checkout; not code). The tracked modifications, from `git diff`:

1. **`.githooks/pre-commit`** — golangci-lint bump v2.10.1→v2.12.2 + a
   `GOTOOLCHAIN` pin derived from `go.mod` (fixes the "linter built with go1.25
   refuses to lint" mismatch), plus an appended managed "BEADS INTEGRATION
   v1.1.0-rc.2" block (`bd hooks run pre-commit` with timeout fallbacks).
2. **`.githooks/pre-push`** — the same managed beads-integration block appended.
3. **`.pre-commit-config.yaml`** — the matching golangci-lint rev bump.
4. **`cmd/bd/ready_embedded_test.go`** — adds
   `TestEmbeddedReadyMetadataFiltersAcrossDurabilityModes`: embedded-Dolt
   coverage that `bd ready`/`bd list --ready` honor `--metadata-field
   gc.routed_to=…`, `--unassigned`, `--exclude-type epic`, `--include-ephemeral`
   across history/no-history/ephemeral durability modes. This is routed-demand
   (Group C-adjacent) *read-filter* coverage, *not* dependency-edge work.
5. **`internal/storage/issueops/dependencies.go`** — adds exported
   `DepTargetExprForAlias(alias)` (`:47-51`) and rewrites
   `cycleReachabilityQuery` (`:346-376`) so the multi-table recursive CTE emits
   one alias-qualified recursive term per dep table instead of joining a
   materialized `UNION` derived table (performance/correctness refactor of the
   recursive traversal; single-table branch also switches to the aliased expr).
6. **`internal/storage/issueops/dependencies_test.go`** — test updates asserting
   the new query shape (direct JOINs, no derived table).
7. **`internal/storage/dolt/wisps.go`** — the identical CTE refactor applied to
   `wispCycleReachabilityQuery` (`:596-617` on disk).
8. **`internal/storage/dolt/wisps_cycle_test.go`** — matching test updates.

**Overlap with Group E: NONE substantive.** Items 5-8 touch finding #60's file
(`issueops/dependencies.go` cycle check) but are a **same-store SQL refactor**
of the existing local cycle CTE — they do not add cross-store cycle detection,
foreign-prefix awareness, blocked-state changes, or doctor changes. Nobody has
started Group E's beads half. Items 1-4 are unrelated tooling/coverage work.

**Handling requirement:** Group E's beads changes must be developed *on top of*
this dirty state (or the owner commits it first). Do not revert it; the cycle
CTE refactor is the on-disk shape any §7.3 work composes with. Flag to owner:
these edits are uncommitted on a shared checkout and should be committed by
whoever owns them before Group E's beads landing to avoid attribution tangles.

---

## 4. Per-site current-code analysis

### 4.A gascity

**`internal/molecule/molecule.go`**
- `ExternalDep` type `:106-111` — `{StepID, DependsOnID, Type}`; `DependsOnID`
  is a caller-supplied *existing* bead ID; no residency/class validation
  anywhere. Options fields at `:36` (Instantiate) and `:99` (Fragment).
- Blocking/non-parent-child ExternalDeps are **embedded into `b.Needs` at
  create time**: `:550-559` (Instantiate), `:831-840` (InstantiateFragment) —
  so the edge is written by the *step bead's own Create* on its own leg.
- Parent-child ExternalDeps use explicit `store.DepAdd(fromID,
  dep.DependsOnID, dep.Type)`: `:713` (Instantiate), `:917`/`:934` (Fragment).
- `Attach` `:228-312`: loads the attach bead `:236`, stamps root metadata
  `:274-282`, instantiates `:292-300`, then **`store.DepAdd(attachBeadID,
  result.RootID, "blocks")` at `:303`** ("attach bead blocks on sub-DAG
  root"); duplicate-idempotency re-wire at `:340-359` (`DepAdd` `:359`).
- GraphApply variant: when `IsGraphApplyEnabled()`
  (`internal/molecule/graph_apply.go:36-39`, set by the daemon config loader),
  `Instantiate`/`InstantiateFragment` build a `GraphApplyPlan` instead
  (`molecule.go:475-499`, `:784-787`); ExternalDeps become plan edges with
  literal `ToID` (`graph_apply.go:223-235` and `:409-434`), applied by
  `SQLiteStore.ApplyGraphPlan` → `depAddTx` with **no target validation**
  (`internal/beads/sqlite_store_graph_apply.go:152-161`, ref resolution
  `:177-182`).

**`internal/coordrouter/`**
- `Router.DepAdd` `router_mutation.go:72-74`: routes to
  `backendForID(issueID)` — the FROM bead's leg. Doc comment `:69-71` declares
  the pin ("a cross-class blocks edge … is recorded in the Work store — the
  pinned home for ready-blocking deps"). `backendForID` `:21-43` = prefix
  routing (`prefixBackendForID`, `router_federation.go:41-53`) + Get-probe +
  primary fallback. **No cycle check, no cross-leg validation.**
- `Router.DepList` `router_federation.go:206-` federates both legs (partial
  results per Group D). `GraphIDPrefix()` `router_federation.go:189-200`
  returns the ClassGraph backend's prefix or `""` (single-store).

**`internal/beads/sqlite_store.go` (the graph leg)**
- `Ready` `:854-906`; the blocker predicate `:860-866`:
  `LEFT JOIN beads blocker ON blocker.id=d.depends_on_id … AND
  COALESCE(blocker.status,'') <> 'closed'` with blocking set
  `('blocks','waits-for','conditional-blocks')` `:864`. **Missing target ⇒
  status `''` ⇒ blocking forever** — finding #26's mechanism, verified.
  `issue_type` exclusion list `:859` already excludes `gate` (useful for
  Decision 1A).
- `DepAdd` `:993-1019` (`depAddTx` `:1007-1019`): plain
  `INSERT … ON CONFLICT DO NOTHING`. **No existence check, no cycle check**
  (#60's sqlite half, verified).

**`internal/beads/doltlite_read_store.go` + build-tag pair (finding #27's native leg)**
- Tag census: `cmd/gc/doltlite_store_native.go:1` (`gascity_native_beads`) vs
  `cmd/gc/doltlite_store_default.go:1` (`!gascity_native_beads`);
  `internal/beads/doltlite_read_store.go:1` and `doltlite_count.go:1` exist
  only under the tag. Swap point `cmd/gc/main.go:1332-1348` (`openBdStoreAt`
  wraps both city and rig stores). Native path additionally requires
  `GC_NATIVE_DOLTLITE_BEADS` truthy (`doltlite_store_native.go:16`, `:26-33`).
  **The tag does not exist in the beads repo at all** (grep-verified).
- `Ready` `:291-344` → `doltliteReadyIssueWhere` `:77-107`: target expr
  **includes `depends_on_external`** (`:89`), `LEFT JOIN` local issues only
  (`:92`), `COALESCE(blocker_issue.status,'')` (`:93`), predicate `!= 'closed'`
  (`:104`) ⇒ **missing/foreign target blocks permanently**. (Also true for
  `external:`-literal cross-rig rows — a native-build divergence even absent
  the graph split; see §6.3.)

**`internal/dispatch/drain.go`** — see §2 population (a):
`drainProjectedBlockerIDs` `:572-608` (projection `:590-591`, in-manifest skip
`:592-600`, **raw pass-through `:589`**); `drainWorkflowExternalDeps`
`:957-986`; `ensureDrainRowDependencyProjection` `:528-545`;
`ensureDrainWorkflowBlocksOn` `:610-629`; legacy-edge scrub
`repairDrainWorkflowSourceMemberDeps` `:636-663`.

**`cmd/gc/cmd_formula.go`** — v2 cook-attach gate `:650-724`
(`ensureFormulaCookAttachDep` calls `:689`/`:723`, helper `:892-908`, DepAdd
`:905`); v1 `molecule.Attach` call `:767`.

**`internal/sling/cycle.go`** — `DetectCycle` `:36-` : three-color DFS over
`DepList(id,"down")`, skipping non-scheduling types (`:81`). Run against the
Router this is **already cross-store-capable** (Router.DepList federates) — an
existing in-repo shape for §7.3, but it only guards the sling entry point, not
`DepAdd`/`ApplyGraphPlan`.

### 4.B beads

**B1. `internal/storage/issueops/blocked_state.go` — is_blocked derivation.**
`markBlockedTemplateForIssues()` `:141-183`: blocking requires
`JOIN issues t ON t.id = d.depends_on_issue_id` or
`JOIN wisps t ON t.id = d.depends_on_wisp_id` with
`t.status <> 'closed' AND t.status <> 'pinned'` (`:149-161`); parent-child
propagation `:162-175`; `waits-for` gate `waitsForGateBlockedSQL` `:22-58`
(also all inner JOINs). **`depends_on_external` never appears in the file.**
Unmark template `:185-229` is the symmetric `NOT EXISTS`. INNER JOIN semantics
⇒ absent target ⇒ never blocks (#57/#59 verified). Ready consumes the flag:
`beads:internal/storage/sqlbuild/ready.go:96` (`is_blocked = 0`). The
`blocked_issues` VIEW
(`beads:internal/storage/schema/migrations/0045_update_blocked_view_drop_depends_on_id.up.sql:1-35`)
matches.

**B2. `internal/storage/domain/db/dependency.go` — proxied-server DepAdd.**
`pickDepTargetColumn` `:41-57`: `external:`-literal → `depends_on_external`
(`:42-44`); else wisp-probe; else **`depends_on_issue_id`** (`:50-53`).
`Insert` `:59-159`: validates non-nil/non-empty/non-self (`:60-71`) and
duplicate (`:80-103`) — **no target-existence check, and no cross-prefix
concept at all**. A `gcg-` target would be classified `depends_on_issue_id` and
**fail the `fk_dep_issue_target` FK** (schema below) instead of landing in
`depends_on_external`. This write path *diverges* from the Dolt-store path (B3)
— finding #57's "proxied Insert, no classification/validation" site.
`markDirectBlockedSource` `:166-195` explicitly no-ops for external (`:178`).
`HasCycle` `:242-293` — local tables only.

**B3. `internal/storage/dolt/dependencies.go` — Dolt-store DepAdd.**
`isCrossPrefixDep` `:15-17` (`types.ExtractPrefix` inequality);
`AddDependency` `:22-56`: `isCrossPrefix || external:` ⇒
`issueops.DepTargetExternal` (`:32-40`) with target-existence validation
skipped (`issueops/dependencies.go:127-133`, `:155-165`); INSERT writes
`DependsOnID` **verbatim** into `depends_on_external`
(`issueops/dependencies.go:222-225`) — **no `external:` literal is added**.
Same in the tx path (`beads:internal/storage/dolt/transaction.go:667-688`) and
embedded path. Schema
(`0041_split_dependencies_target.up.sql`): `depends_on_issue_id` FK →
`issues(id)` CASCADE; `depends_on_wisp_id` and `depends_on_external` un-FK'd;
`ck_dep_one_target` = exactly one target column set.

**B4. `cmd/bd/doctor/validation.go` + `cmd/bd/doctor/fix/validation.go` —
the #58 deletion.** Check: `checkOrphanedDependenciesDB` `:55-114`, query
`:61-67`:
```sql
… WHERE d.depends_on_id NOT LIKE 'external:%'
    AND NOT EXISTS (SELECT 1 FROM issues i WHERE i.id = d.depends_on_id)
    AND NOT EXISTS (SELECT 1 FROM wisps  w WHERE w.id = d.depends_on_id)
```
over `doctorDependencyUnionSQL()` (`beads:cmd/bd/doctor/dependency_sql.go:6-13`)
which projects `depends_on_id = COALESCE(depends_on_issue_id,
depends_on_wisp_id, depends_on_external)` — **the COALESCE erases which column
matched**. Fix: `OrphanedDependencies`
(`beads:cmd/bd/doctor/fix/validation.go:25-113`): same selection `:40-46`
(union `fix/dependency_sql.go:4-13`), then `DELETE FROM
dependencies|wisp_dependencies WHERE issue_id=? AND
COALESCE(...)=?` (`:86-92`) + `CALL DOLT_COMMIT` (`:109`). **The only carve-out
is the `LIKE 'external:%'` value test** (comment cites #1593's synthetic
cross-rig refs). Since B3 stores cross-prefix targets **raw**, a Router-pinned
attach-gate row (`depends_on_external='gcg-…'`) fails the carve-out, is absent
from local `issues`/`wisps`, and **is deleted**. #58 verified end-to-end. Both
doctor halves open the local Dolt store directly (`validation.go:21-41`,
`fix/validation.go:31`, `:309-327`) — per-store, no router.

**B5. `internal/storage/issueops/dependencies.go` — cycle check (as on disk,
including the uncommitted refactor).** `CheckDependencyCycleInTx` `:323-342`
runs `cycleReachabilityQuery` `:346-376` over `cycleDetectionTables()` =
`{"dependencies","wisp_dependencies"}` (`:378-380`) — the recursive CTE follows
`COALESCE(depends_on_issue_id, depends_on_wisp_id, depends_on_external)`
targets but can only re-enter via local `issue_id` rows, so a cross-store hop
terminates the walk. Called from `AddDependencyInTx` `:180-184` (skippable via
`opts.SkipCycleCheck`). `ClassifyDepTarget` `:61-69`. **Local-per-leg only**
(#60's Dolt half). No federated pre-check anywhere in the repo.

**B6. Config.** `allowed_prefixes` exists
(`beads:internal/storage/domain/db/config.go:220-224`,
`beads:internal/utils/id_parser.go:69`, create-validation
`beads:internal/validation/bead.go:97-165`,
`beads:internal/storage/issueops/create.go:39-46`) but is consulted by **no**
blocked/ready/doctor path. There is no foreign-prefix/sibling-store concept in
beads today.

---

## 5. What each finding reduces to (validation of the audit's proposed fix)

| Finding | Audit claim | Verdict against current code |
|---|---|---|
| #26 | sqlite Ready COALESCEs missing blocker to open → gcg-→work edge blocks forever | **Confirmed** (`sqlite_store.go:860-866`); live producer = drain out-of-manifest blockers (§2a), plus any work-target ExternalDeps. The audit's cited site (":555/:836 ExternalDeps embed") is the write mechanism; the drain path is the production caller. |
| #27 | work→gcg- gate inert on default build, permanently blocking under the native tag | **Confirmed exactly**; both paths located and quoted (Decision 2). |
| #57 | proxied Insert no classification/validation | **Confirmed and sharpened**: `domain/db/dependency.go:41-57` doesn't just skip validation — it *misclassifies* cross-prefix targets into the FK'd column (write error), diverging from the Dolt-store path. |
| #58 | doctor --fix deletes Router-pinned cross-class edges | **Confirmed** (B4); scope = work-leg rows only (doctor never opens the sqlite graph store). |
| #59 | is_blocked ignores depends_on_external | **Confirmed** (B1). |
| #60 | split cycles undetectable; sqlite DepAdd has none | **Confirmed** (B5 + `sqlite_store.go:993-1019`); one correction: the Router boundary for fix (c) is **gascity**, not beads; and `ApplyGraphPlan` bypasses `Router.DepAdd`, so a Router-only check is incomplete (must also guard the plan-edge path). `sling.DetectCycle` (`internal/sling/cycle.go:36`) is an existing federated-capable DFS shape to reuse. |

Audit fix (a) (partition by `GraphIDPrefix()` + proxy gates) — **validated**,
with the doctrine-derived simplification that only the two §2 populations need
partitioning, and the note that the partition must ALSO be applied in the
GraphApply plan builder, not just the legacy embed/DepAdd paths. Fix (b)
(beads: foreign-prefix blocking + doctor carve-out) — the doctor carve-out is
validated and decision-independent (§6.1); the blocking semantic is exactly
Decision 2A and must not land before that decision. Fix (c) (cycle check) —
validated in intent, relocated to gascity, choke points enumerated (§7.3).

---

## 6. Decision-INDEPENDENT safe subset (implementable now)

These are correct under every combination of Decisions 1 and 2. Per the
handoff, **§6.1 must land no later than any change that writes more cross-leg
edges** — and since the live system is *already* writing attach-gate rows that
any `bd doctor --fix` run deletes today, §6.1 should land first, now.

### 6.1 The doctor carve-out (#58) — beads repo

**Fix:** make the orphan sweep **column-aware**: a row whose target lives in
`depends_on_external` is *never* an orphan candidate — by construction (B3 +
`ck_dep_one_target`) that column holds either an `external:` literal or a
cross-prefix (sibling-store) reference, and both are intentionally unresolvable
locally. This subsumes the existing `LIKE 'external:%'` value test and requires
no configuration.

Concretely, in both selections:
- `beads:cmd/bd/doctor/dependency_sql.go` and
  `beads:cmd/bd/doctor/fix/dependency_sql.go`: have the union project the
  target columns (or a `dep_target_kind` discriminator) instead of only the
  COALESCE, e.g. add `depends_on_external IS NOT NULL AS is_external`.
- `beads:cmd/bd/doctor/validation.go:61-67` and
  `beads:cmd/bd/doctor/fix/validation.go:40-46`: replace
  `d.depends_on_id NOT LIKE 'external:%'` with `NOT d.is_external` (keeping the
  issues/wisps NOT-EXISTS clauses for the two locally-resolvable columns).
  `depends_on_wisp_id` rows pointing at purged wisps and (theoretical)
  dangling `depends_on_issue_id` rows remain reclaimable exactly as today.
- Check side: report external-column rows under a separate informational
  count ("N cross-store/external references (not reclaimed)") so operators
  retain visibility without deletion. Keep `--fix` silent about them beyond
  that count.

**Behavior-change honesty:** this is *not* byte-identical for any beads user
who today has raw cross-prefix rows in `depends_on_external` — those rows stop
being deleted. That is the fix, and it is the conservative direction (never
delete); readiness is unaffected because `is_blocked` ignores the column (B1).
The `external:`-literal class was already protected (#1593); this extends the
same intent to the raw cross-prefix class the exporter/Dolt store also writes.

**TDD plan (red first):**
1. `beads:cmd/bd/doctor/fix/validation_test.go` (or the existing fix test
   home): seed a Dolt store with (i) a `dependencies` row
   `depends_on_external='gcg-xyz'` (raw), (ii) an `external:abc` row, (iii) a
   genuinely dangling `depends_on_wisp_id` row, (iv) a healthy local edge. Run
   `OrphanedDependencies`. Assert: (i) and (ii) survive; (iii) deleted; (iv)
   untouched. This test is RED today because (i) gets deleted.
2. Same matrix for `wisp_dependencies`.
3. Check-side test on `checkOrphanedDependenciesDB`: (i)/(ii) not in the
   orphan list (and surfaced in the informational count), (iii) flagged.
4. Regression guard: a test asserting the union SQL still projects the kind
   discriminator (mirroring the existing `dependency_sql_test.go` style).

Landing: beads repo commit → tag → gascity `go.mod` bump is NOT required for
doctor (doctor is `bd`-binary behavior; the fleet needs the new `bd`), but
coordinate the `bd` rollout with the owner since maintainer-city runs a pinned
`bd`.

### 6.2 Proxied-server DepAdd classification parity (#57's write half) — beads repo

**Fix:** port the Dolt store's cross-prefix classification (B3) into
`pickDepTargetColumn` (`beads:internal/storage/domain/db/dependency.go:41-57`):
if `types.ExtractPrefix(dep.IssueID) != types.ExtractPrefix(dep.DependsOnID)`,
classify as `depends_on_external` (before the wisp probe). Decision-independent
because both decision branches need the two write paths to agree, and today's
behavior on that path is an FK **error** (`fk_dep_issue_target`), not a
different semantic — converting a hard write failure into the same row the
Dolt-store path produces. Include `markDirectBlockedSource`'s existing
external no-op (`:166-195`) unchanged.

**TDD:** unit test on `pickDepTargetColumn`/`Insert` — cross-prefix target ⇒
row lands in `depends_on_external`, no FK error, `is_blocked` unaffected;
same-prefix missing target keeps today's behavior (FK error surfaces — that is
pre-existing and intentional for local refs).

**Honesty note (red-team):** `types.ExtractPrefix` returns `""` for a hyphenless
id, so a typo'd target (`"bd1"`) or a *locally resolvable* different-prefix
target (a mixed-prefix DB from `bd migrate-issues`) now silently lands as a
never-blocking, never-FK'd `depends_on_external` row (which §6.1 then protects
forever), where the proxied path previously failed loudly on
`fk_dep_issue_target`. This is exact parity with the committed Dolt store
(`internal/storage/dolt/dependencies.go` `isCrossPrefixDep`, cross-prefix beats
the wisp probe) — the two write paths disagreeing was the bug §6.2 fixes — so
it is accepted as by-design, but note cross-prefix ⇒ non-blocking now applies to
resolvable same-DB targets too, not only genuinely-foreign ones.

### 6.3 Native-reader parity for `external:`-literal rows — gascity (optional, small)

Under `gascity_native_beads`+`GC_NATIVE_DOLTLITE_BEADS`,
`doltliteReadyIssueWhere` treats **every** unresolvable `depends_on_external`
value as a permanent blocker (`doltlite_read_store.go:89-106`) — including the
`external:` cross-rig tracking refs that the default `bd` path has *always*
ignored (#1593). Whatever Decision 2 says about *foreign-prefix* targets, the
`external:`-literal class is uncontroversially non-blocking (both current
semantics agree the default build's treatment of them is the contract; the
native store's charter is read-parity with `bd`). Safe slice: exclude values
`LIKE 'external:%'` from the blocking predicate. **Do not** extend this to
foreign-prefix values — that is Decision 2.

**TDD:** native-store test (pattern of `doltlite_read_store_test.go`): a bead
with a dep whose `depends_on_external='external:foo'` is READY under the native
reader (RED today), while one blocked by an open local issue stays blocked.

### 6.4 NOT safe now (explicitly deferred to the decisions)

- Any change to `blocked_state.go` semantics (Decision 2).
- Any change to sqlite/native readiness for foreign-prefix targets
  (Decisions 1+2).
- Router fail-loud/reject on cross-leg blocking DepAdd (depends on whether
  cross-leg rows remain legal — Decision 1/2).
- The cross-store cycle check: correct under any decision, but its shape
  (which writes still cross legs, hence which choke points need guarding, and
  whether a federated reverse walk is affordable per-DepAdd) depends on
  Decision 1. Sketched in §7.3; recommend landing in the same arc as the
  representation change.

---

## 7. Decision-DEPENDENT design, sketched per branch

### 7.1 Branch: Decision 1 = A (proxy/gate beads) — the recommended shape

**New primitive-free mechanism** (passes the "no judgment in Go" test — the
sweep copies status, decides nothing):

1. **Gate minting helper** (`internal/molecule` or a small
   `internal/crossleg` package):
   `ensureCrossLegGate(store, dependentLegPrefix, targetID) (gateID, error)` —
   create-or-find (idempotency key = metadata pair) a bead on the *dependent's*
   leg: `issue_type: gate`, `status: open`, metadata
   `gc.gate_target_id=<targetID>`, `gc.gate_target_store_ref=<ref if known>`,
   `gc.gate_kind=cross-leg`. Graph-leg gates are ClassGraph (doctrine keeps
   them in sqlite); work-leg gates are plain work beads with `issue_type: gate`
   in the rig store. `gate` is already ready-excluded on the sqlite leg
   (`sqlite_store.go:859`); verify/extend the exclusion on the work leg
   (`readyExcludeTypes` used by `doltliteReadyIssueWhere`
   `doltlite_read_store.go:78`, and bd's ready-exclusion set) — if `gate` is
   not excluded by bd, use a dedicated custom type registered per Group H's
   custom-types sync, or `status: pinned`-style parking; resolve at
   implementation time with a conformance test.
2. **Partition at the writers** (the only two populations, §2):
   - *Graph→work (#26):* in `drainProjectedBlockerIDs`
     (`drain.go:572-608`), when a raw pass-through blocker (`:589`) does not
     carry the graph prefix (`GraphIDPrefix()` from the store; already
     forwarded through the policy wrapper per Group A), substitute
     `ensureCrossLegGate(...)`. Belt-and-braces: in the generic embed paths
     (`molecule.go:550-559`/`:831-840`/`:713`/`:917`/`:934`) and the plan
     builder (`graph_apply.go:223-235`/`:409-434`), partition ExternalDeps the
     same way so no caller can write a raw cross-leg blocking edge/plan edge.
   - *Work→graph (#27):* in `molecule.Attach` (`molecule.go:303`, `:359`) and
     `ensureFormulaCookAttachDep` (`cmd_formula.go:892-908`), when
     `attachBeadID`'s prefix ≠ root's prefix, wire
     `attachBead → blocks → <work-leg gate targeting the gcg- root>` instead of
     the raw row. Same-leg attaches (dispatch control beads,
     `control.go:514`) are untouched.
3. **Controller release sweep** (new control-plane patrol beside the existing
   reconciler ticks, or a control-bead type in `internal/dispatch`): list open
   `gc.gate_kind=cross-leg` beads on every leg (ListByMetadata; graph leg via
   `ListGraphOnly`), `Router.Get` each `gc.gate_target_id`; target closed ⇒
   `Close(gate)`; target **missing** ⇒ do NOT silently close — mark
   `gc.gate_error=target_missing` and surface via event (fail-loud; a deleted
   blocker must be a human/heal decision). Partial-result reads (Group D) must
   skip, not close.
4. **Router hardening:** `Router.DepAdd` rejects (or transparently converts —
   owner style call, propose reject-with-error first) blocking-type edges whose
   endpoint prefixes straddle legs; same guard in `ApplyGraphPlan`'s edge loop
   (`sqlite_store_graph_apply.go:152-161`) for literal `ToID`s carrying a
   non-graph prefix. Non-blocking types (`tracks`, `relates-to`) stay legal
   cross-leg.
5. **Heal:** one-time (or patrol-integrated) sweep for existing raw cross-leg
   rows: sqlite-leg `gcg-→ga-` rows → replace with gates; Dolt-leg
   `depends_on_external='gcg-…'` rows → replace with work-leg gates (or, if
   Decision 2=B, leave-and-ignore + rely on doctor's new informational count).
6. **Cycle check (§7.3)** in the same arc.

*TDD skeleton:* (i) unit — drain manifest with an out-of-manifest work blocker
produces a gate, not a raw edge (RED: today writes raw); (ii) unit — Attach on
a cross-prefix bead produces a work-leg gate; (iii) sweep unit — gate closes on
target closure, errors loud on target missing, no-ops on partial; (iv) the
Group-E slice of `TestGraphStoreSQLitePoolDemandOrphanClaimClose`-style
integration: a drain-item workflow over a routed city becomes ready after its
work-leg blocker closes (RED today: blocked forever, #26); a cook-attached
source stays un-ready until root closes and releases after (RED today on
default build: ready immediately, #27).

### 7.2 Branch: Decision 1 = B (readiness resolves through the Router)

- `SQLiteStore` gains an injected
  `ForeignStatusResolver func(ids []string) (map[string]string, error)`
  (constructor option; wired by `cmd/gc` where the Router is known — avoids
  the upward import). `Ready` collects unresolved `d.depends_on_id`s from the
  blocker scan and applies resolver results; resolver **error ⇒ treat as
  blocked** (fail-safe) and surface a partial signal.
- Native `DoltliteReadStore.Ready`: same resolver for foreign-prefix
  `depends_on_external` values.
- Dolt default leg (`bd ready`): **cannot** be resolver-fixed (§5.B) — requires
  Decision 2=A in beads (`blocked_state.go` mark/unmark templates gain an
  `EXISTS(dependencies WHERE depends_on_external <foreign-prefix predicate>)`
  disjunct + a `foreign_blocking_prefixes` config read; unmark releases only
  when the row is *removed*), **plus** a gascity controller loop that
  `DepRemove`s the attach-gate row when the `gcg-` root closes (the release
  mechanism `bd` cannot provide). Note this loop is ~the same code as 7.1's
  sweep — the reason §1 calls Option B degenerate.
- Cycle check unchanged (§7.3).
- *TDD:* resolver-injected Ready unit tests (resolved-closed ⇒ ready;
  resolver-error ⇒ blocked+partial); beads-side blocked-state tests keyed on
  the config knob (unset ⇒ byte-identical).

### 7.3 Cross-store cycle detection (#60) — shape under either branch

Cycles can only span legs through the cross-leg linkage; under 1A that linkage
is `gate → (controller watches target)`, which is *logical*, so a structural
per-leg walk cannot see it — the check must traverse `gc.gate_target_id` as an
edge. Design: a `crosslegDetectCycle(routerStore, fromID, toID)` reusing the
`sling.DetectCycle` DFS (`internal/sling/cycle.go:36-`) with two additions:
(1) treat an open gate bead as an edge to its `gc.gate_target_id`; (2) DepList
through the Router (already federated). Guard points: `Router.DepAdd`
(`router_mutation.go:72`) for blocking types whose walk could cross
(cheap pre-filter: only run the federated walk when either endpoint's leg
differs or a gate is encountered — same-leg adds keep today's per-leg
checks), `ensureCrossLegGate` minting, and `ApplyGraphPlan` literal-`ToID`
edges. Cost control: bound depth, and skip when `GraphIDPrefix()==""`
(single-store ⇒ per-leg checks already complete). Under 1B, the same walk runs
on raw rows instead of gates. *TDD:* construct `ga-A → gcg-B → … → gate(ga-A)`
and assert the add/mint is refused with a `CycleError`-style path.

---

## 8. Byte-identical guarantee for default (single-store Dolt) cities

- **gascity:** every §6/§7 change is gated on multi-leg reality:
  `Router.soleBackend()` short-circuits all routing (`router_mutation.go:22`),
  `GraphIDPrefix()` returns `""` single-store (`router_federation.go:189-200`),
  and both gate-minting and cycle-walk paths condition on a non-empty graph
  prefix / cross-prefix endpoints. `ExternalDep` partition: with one store, no
  target can be cross-prefix relative to the step ⇒ embed exactly as today.
  §6.3 is native-build-only (the default build compiles
  `doltlite_store_default.go` and never constructs the store).
- **beads:** §6.2 changes behavior only for cross-prefix targets (previously an
  FK error — no working deployment depends on that error). §6.1 is the one
  intentional behavior delta: doctor stops deleting external-column rows; for a
  default city that has never written a cross-prefix row, the selection result
  is identical (the `external:` literal class was already carved out).
  Decision 2A's blocked-state change (if chosen) must be config-keyed
  (`foreign_blocking_prefixes` unset ⇒ templates byte-identical).
- Verification: run the existing conformance suites
  (`internal/beads/sqlite_store_conformance_test.go`, doctor tests) plus a
  golden test asserting the doctor selection SQL for a store with no
  external-column rows returns the same set before/after.

---

## 9. MUST-FIX risks

1. **Doctor is deleting live gates today.** Every `bd doctor --fix` against a
   rig store deletes the maintainer-city attach-gate rows
   (`depends_on_external='gcg-…'`). §6.1 is not hygiene — it is stopping active
   data loss. Land first; the handoff's ordering constraint ("carve-out no
   later than the edge-writing change") is necessary but not sufficient — the
   carve-out is needed *before the next doctor run*, independent of Group E's
   write changes.
2. **The dirty beads tree** (§3): Group E's beads work must build on the
   uncommitted cycle-CTE refactor in `issueops/dependencies.go` /
   `dolt/wisps.go`, and those edits should be committed by their owner first.
3. **GraphApply bypasses `Router.DepAdd`.** Any Router-boundary guard (fail-
   loud, cycle check) that ignores `ApplyGraphPlan`'s edge loop
   (`sqlite_store_graph_apply.go:152-161`) is structurally incomplete — the
   drain ExternalDeps (population (a)) flow through the plan path whenever
   graph-apply is enabled.
4. **The two beads write paths disagree** (B2 vs B3): until §6.2 lands, any
   attach-gate write routed through the proxied server errors on the FK while
   the same write through the Dolt store succeeds — heals/retries that switch
   paths will behave inconsistently.
5. **Gate release must fail loud on missing targets** (7.1 step 3). Silently
   closing a gate whose target was deleted converts a data-loss event into an
   ordering violation; silently keeping it converts it into a permanent stall.
   Mark + event, let heal/doctor surface it.
6. **Existing wedged state needs a heal, not just a write-path fix.** #26
   rows already in live sqlite legs stay blocked forever under every
   write-path-only fix; the §7.1(5) sweep (or an explicit one-time migration)
   is part of done.
7. **Work-leg gate beads must be ready-excluded under BOTH readers** (bd's
   exclusion set and `doltliteReadyIssueWhere`) or gates themselves become
   dispatchable work — verify with a conformance test before shipping 7.1.
8. **Decision 2A without a release mechanism is a self-inflicted #26.**
   "Blocking" semantics for foreign-prefix targets must never land ahead of the
   controller release loop, or every attach gate becomes permanently wedged on
   the Dolt leg (strictly worse than today's inertness for the merge pipeline).
9. **Don't trust the audit's beads-landing for the cycle check** — the Router
   is gascity-resident (§2 correction); planning the beads tag/release around
   fix (c) would idle the beads release on code that belongs in gascity.

---

## 10. Landing order (one coordinated arc)

1. **Now (decision-independent):** §6.1 doctor carve-out (beads) → new `bd`
   rollout; §6.2 proxied classification parity (beads, same tag); §6.3 native
   `external:`-literal parity (gascity, tag-gated code only).
2. **Owner decides** Decisions 1 and 2 (§1).
3. **Then:** §7 branch per the decisions — gascity write-path partition + gate
   sweep (+ Router/plan guards + cycle check) and, only if Decision 2=A, the
   beads blocked-state change behind its config knob; heal sweep last.
4. Re-run the Group-level integration sentinel (drain-release + attach-gate
   release, §7.1 TDD iv) against a routed two-leg city before deploy.
