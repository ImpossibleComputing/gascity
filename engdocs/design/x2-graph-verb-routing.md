# X2 — route bd graph verbs through the controller (ga-2gap48.19)

Goal: under `graph_store=sqlite` the work-only `bd` cannot see graph/wisp beads
resident in the SQLite graph store, so the shim **refuses** `bd mol` / `bd gate
check` / `bd query` (`cmd/gc/cmd_bd_shim.go` `bdShimGraphTouchingUnroutedVerbs`)
rather than silently miss them. X2 routes the ones that matter through the
controller's Router instead.

Byte-identical *text* is the separate C2a corpus milestone, not X2. X2's bar is
routing with correct data/mutation + exit-code + JSON-shape fidelity.

## Status

- **query (ephemeral) — DONE** (`5ae1a0c8b`). New `GET
  /v0/city/{city}/beads/ephemeral` (`humaHandleBeadEphemeral`, TierWisps
  federation) + `api.Client.EphemeralBeads` + shim `case "query"`
  (`parseBdQueryEphemeral`, closed allowlist for `--json 'ephemeral=true AND
  <bare clauses>'`, maps both the `listEphemeral` argv and the `work_query`
  literal). `classifyBdShimVerb`: routes when mappable, refuses-under-split
  otherwise. Proven by `TestBeadEphemeralHandlerReachesSQLiteGraphBackend`.
- **mol (current/progress) — DONE** (`d5a2bb70f`). Reuses the existing `GET
  /beads/graph/{rootID}` (no new endpoint/regen): `api.Client.GetBeadGraph` +
  shim `case "mol"` rendering step indicators (done/current/pending) and
  progress (closed/total %). Open steps render `[pending]` not `[ready]`/
  `[blocked]` — the graph endpoint returns parent-child edges, not blocking deps;
  that precision is C2a. mol is LLM-facing (graph workers are forbidden from
  parsing it), so a faithful render suffices.
- **gate (`bd gate check`) — DEFERRED** (decision 2026-06-15). See below.

## Why gate is deferred (grounded — gate-model recon)

`bd gate check` (run by the `gate-sweep` exec order: `--type=timer --escalate`
and `--type=gh --escalate`) is **effectively a no-op in a gascity city**, so
routing it is X2 soundness/parity, **not** load-bearing:

- "Gate" is overloaded across ~5 unrelated bead populations. `bd gate check`
  targets exactly one: **bd-native await gates** — beads with first-class
  `await_type`/`await_id`/`timeout` columns (bd `internal/types/types.go:92-96`).
- **gc never creates those.** Only the external `bd` binary does (`bd cook` /
  `bd gate create`). gc's formula compiler emits `RecipeGate{Type,ID,Timeout}`
  (`internal/formula/compile.go:500-538`) but it is **dropped at instantiation**:
  `stepToBead` (`internal/molecule/molecule.go:996-1023`) never reads
  `step.Gate`. So a gc formula gate is a plain `type=gate` blocking bead with no
  `await_type`; `bd gate check` cannot see it. There is **no `bd cook` /
  `bd gate create` call site in the gascity fork** (grep; "Live rows: 0" in the
  fork's coordination-store audit).
- Formula gates are resolved by the **convergence handler** (a separate
  script-eval mechanism, `internal/convergence/gate.go`), invisible to
  `bd gate check` regardless of routing.

**Consequence for the demand-`Live`-read goal:** gate-sweep does **not** mutate
work-bead readiness in gascity, so the "X2/gate unblocks X1 → retires the demand
`Live` read" chain was overstated — gate was never a real out-of-band work
mutator. The path to retiring `build_desired_state.go`'s controller-demand `Live`
read is shorter than X1+X2; the open question is which exec orders (if any)
actually mutate work readiness out-of-band after C4 — gate is not one of them.

**When gate WOULD be needed:** a deployment that creates bd-native await gates
(e.g. a Gas Town-style city with `gh` CI gates via `bd gate create`). Then under
sqlite the shim must route `gate` to the SQLite store. The full controller-side
evaluator spec — timer/gh:run/gh:pr resolve+escalate predicates (grounded against
bd `cmd/bd/gate.go` v1.0.5), the close→dep-cascade unblock, the `--dry-run`/exit/
JSON contract, and the `gt escalate` gap (gascity has no `gt` → emit an event) —
is captured in the gate-model recon (session workflow `gate-model-recon`). The
smallest enablement is a single-site stamp of `await_type`/timeout into Metadata
at `compile.go:508-518` plus new `beadmeta` keys, then the evaluator + endpoint +
shim case + integration test.
