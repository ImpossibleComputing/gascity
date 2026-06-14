# bd shim → HTTP API redirect — design & status

**Goal (user-stated end state):** the bd shim (`cmd/gc/cmd_bd_shim.go`, gc-as-`bd`)
routes every bead op to the controller's **HTTP API** and **errors if no
controller is reachable** — no local in-process Router fallback. The controller
is the single store owner; every worker is a thin client. This reverses the
"no-socket worker mediation" decision (each gc process opening the Router
in-process) recorded as settled in `graph-store-session-handoff.md`. The user
accepts "reorganizing startup a bit so the beads subsystem is up immediately."

## Why it's viable (grounded)

- The controller's API already exposes the full bead surface
  (`internal/api/huma_handlers_beads.go`): GET `/v0/beads`, `/v0/beads/ready`,
  `/v0/beads/graph/{root}`, `/v0/bead/{id}`, `/v0/bead/{id}/deps`; POST
  `/v0/beads` (create), `/v0/bead/{id}/close|reopen|assign|update`; DELETE
  `/v0/bead/{id}`.
- The controller's **city store has the SQLite graph backend**:
  `newControllerStateOpenCityStore` → `openCityStoreResultAt` →
  `openStoreResultAtForCity` → `routedPolicyStore` (main.go:1234). The bead
  handlers mutate via `s.beadStoresForID(id)`, which includes the Router-wrapped
  city store, so an HTTP `bd close <gcg-id>` reaches SQLite.
  (An earlier recon agent claimed "not viable / feature not merged" — it read the
  wrong worktree (`/worktrees/beads`, not `ov3`). Refuted by
  `TestBeadCloseHandlerReachesSQLiteGraphBackend`.)

## Phases (status)

- **Phase 1 — DONE** (`33eba2008`). `api.Client` write methods: `CloseBead`,
  `ReopenBead`, `DeleteBead`, `UpdateBead` (maps `beads.UpdateOpts` → wire body),
  `ReadyBeads`. Viability test proves the HTTP close handler mutates a SQLite
  graph bead via the Router. `/v0/beads/ready` takes no predicate params, so
  callers post-filter client-side.
- **Phase 2a — DONE** (`313f69301`). `humaHandleBeadReady` now federates
  `CityBeadStore()` (it iterated only per-rig `BeadStores()`), so a single-HQ
  city's ready work is surfaced over HTTP. Guarded by
  `TestBeadReadyFederatesCityStore`.
- **Phase 2b — DONE** (`4206f70d1`). The shim's routed verbs
  (close/reopen/delete/update/show/ready) call the controller HTTP API when a
  controller is reachable. `bdShimAPIClient` prefers a standalone controller and
  otherwise reaches the **supervisor-served** per-city API — `apiClient` (read-path
  CLI, with a local fallback) deliberately does NOT route a supervisor-managed
  city to the supervisor client, so the shim needs its own getter.
  `GC_BD_SHIM_REQUIRE_API` makes the shim refuse the local fallback (the pure-HTTP
  behavior, gated). **The deployed convergence e2e sets it**, so it PROVES the
  pure-HTTP path: with NO local fallback, a real `graph_store=sqlite` city
  converges a graph.v2 molecule with the worker's complete AND the controller's
  discovery both going shim → HTTP → controller → Router → SQLite (non-flaky 3×).

## Phase 3 — remaining (startup reorg + flip the default)

The literal `#2`: make `GC_BD_SHIM_REQUIRE_API` the default (remove the local
fallback). The blocker is bootstrap: `gc init` and standalone `bd`/`gc bd` create
beads before a per-city controller exists. Notes:

- The gc-as-`bd` shim is only on **agent + controller** PATHs (the C4 install,
  not yet done). Agents/controllers run with a controller up, so for them
  pure-HTTP is already safe. init/standalone use the real `bd`/filebdshim, not the
  shim — so flipping the shim default is currently low-risk (only the convergence
  test installs the shim, and it already runs pure-HTTP).
- The remaining startup-window concern is the controller's OWN serve-loop
  discovery (`bd ready`) running before the controller's API is listening;
  today it would error-and-retry. The convergence test shows it heals in
  practice, but a clean fix brings the per-city beads API up early.
- **Smallest startup reorg** (recon-grounded): the supervisor already serves
  per-city bead routes via one Huma mux (`api.NewSupervisorMux` →
  `serveCityRequest` → per-city `State`). The per-city `State` (and its store)
  is built mid-reconcile by `newControllerState` (`cmd/gc/cmd_supervisor.go`
  ~1950). Bring that up **early in reconcile** (after config load) and register
  the State before lock/socket/runtime, so the beads API is reachable as soon as
  the city is known — independent of the full controller. Handle partial-startup
  cleanup. Then init/standalone can route through HTTP (or `gc init` ensures the
  supervisor + city registration first).

**Sequencing:** do the startup reorg first (so no-fallback is safe for the
controller startup window and future bootstrap), THEN remove the shim's local
fallback (make `GC_BD_SHIM_REQUIRE_API` the default / delete the branch).
`release-if-current` (handled before the verb switch, opens the local store) and
`create` (passthrough) also need an API path for a fully pure shim.

## Files

- `internal/api/client.go` — bead write-path client methods.
- `internal/api/huma_handlers_beads.go` — ready federates the city store.
- `internal/api/bead_http_graph_store_test.go` — viability + ready-federation +
  client-method tests.
- `cmd/gc/cmd_bd_shim.go` — `bdShimAPIClient`, `bdShimRequireAPI`,
  `dispatchBdShimVerbViaAPI`, the apiClient-first route in `runBdShim`.
- `cmd/gc/cmd_bd_shim_api_test.go` — verb→endpoint mapping.
- `test/integration/graph_store_sqlite_convergence_test.go` — sets
  `GC_BD_SHIM_REQUIRE_API`, so convergence is the pure-HTTP proof.
