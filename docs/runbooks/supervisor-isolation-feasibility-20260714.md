# Supervisor isolation feasibility gate — 2026-07-14

Owner: heimdall
Request: paul / mayor gate-check for per-city supervisor isolation before a post-KIVI cutover window.

## Verdict

**GO, but not as-is.** Concurrent per-city supervisors are feasible, and the Dolt/beads store is **not** the hard blocker. The current supervisor is still a **GC_HOME-scoped machine-wide supervisor**, not a first-class `--city <dir>` supervisor service. A safe cutover needs either isolated `GC_HOME` wrappers per city or first-class gc lifecycle changes that generate those isolated homes, single-city registries, unique ports, and fleet aggregation config.

Do **not** describe the current product as "one launchd job per city, each `--city <dir>` works as-is": `gc supervisor run` has no `--city` flag and reads the city registry from `supervisor.RegistryPath()` under the active `GC_HOME`.

## Explicit checks

### 1. Store / beads coupling

**Result: feasible; not one-city-coupled, with scope caveats.**

The supervisor runtime opens each city's store by city path:

- `CityRuntime.run()` opens `openCityStoreAt(cityRoot)` for that city.
- `openCityStoreAt(cityPath)` delegates to `openStoreResultAtForCity(cityPath, cityPath)`.
- Managed Dolt runtime layout defaults under the city path:
  - state: `<city>/.gc/runtime/packs/dolt/dolt-provider-state.json`
  - data: `<city>/.beads/dolt`
  - lock/config/log/pid: `<city>/.gc/runtime/packs/dolt/...`
- Managed Dolt port selection prefers the city-specific state file, then `GC_DOLT_PORT` if set, then a deterministic seed from `cityPath`.

Live host evidence at the time of this gate also showed separate managed Dolt runtimes already in use:

- `gt`: pid `70222`, port `15251`, data dir `/Users/qeetbastudio/gt/.beads/dolt`
- `hobby`: pid `64075`, port `31657`, data dir `/Users/qeetbastudio/hobby/.beads/dolt`

Caveats:

- The per-city property depends on not setting shared process-global overrides such as `GC_PACK_STATE_DIR`, `GC_CITY_RUNTIME_DIR`, `GC_DOLT_DATA_DIR`, `GC_DOLT_STATE_FILE`, `GC_DOLT_LOCK_FILE`, or a shared `GC_DOLT_PORT` in the per-city supervisor service environment.
- Rig stores are scope stores. If two isolated supervisors intentionally point at the same rig path, they will both touch that rig's `.beads` scope. The cutover should assign each rig path to one city or make any shared rig ownership explicit.

### 2. Global lockfiles

**Result: global per `GC_HOME` / runtime dir, not per city.**

`gc supervisor run` uses:

- `supervisorLockPath()` = `supervisor.RuntimeDir()/supervisor.lock`
- `supervisorSocketPath()` = `supervisor.RuntimeDir()/supervisor.sock`
- `supervisor.RegistryPath()` = `supervisor.DefaultHome()/cities.toml`
- `supervisor.ConfigPath()` = `supervisor.DefaultHome()/supervisor.toml`

With the same `GC_HOME`, a second supervisor is blocked by the alive check and the supervisor lock. With an isolated `GC_HOME` per city, existing code already makes the lock, socket, config, registry, and service label distinct.

### 3. API port

**Result: configurable; default only is `8372`.**

`internal/supervisor.Section.PortOrDefault()` returns `8372` only when `[supervisor].port` is not set. `runSupervisor()` listens on `[supervisor].bind` + `[supervisor].port`.

Concurrent per-city supervisors therefore need deterministic unique ports in each isolated `supervisor.toml`. In test/isolated mode the config seeder can choose a random loopback port, but production should use explicit ports for stable dashboard/ops routing.

### 4. Controller socket

**Result: per city for the city controller; per `GC_HOME` for the supervisor control socket.**

- Legacy/standalone controller socket: `<city>/.gc/controller.sock` when short enough, else `/tmp/gascity-controller/<hash>.sock` based on canonical city path.
- Legacy/standalone controller lock: `<city>/.gc/controller.lock`.
- Supervisor control socket: `supervisor.RuntimeDir()/supervisor.sock`, therefore shared if the same `GC_HOME` is reused and isolated if each supervisor has its own `GC_HOME`.

There is also a legacy hidden `gc start --foreground` controller path that can serve one city with city `[api]` config. That is not the same as first-class per-city `gc supervisor run` lifecycle and should not be treated as the launchd supervisor cutover design.

### 5. Dashboard / API aggregation

**Result: own work item, not trivial/as-is.**

The existing supervisor API/dashboard aggregation is easy only because one supervisor owns one registry and one `api.NewSupervisorMux` over all registered cities in that `GC_HOME`. If the cutover runs N isolated supervisors, each supervisor sees only its own isolated registry. Fleet visibility (`gc status`, dashboard, mayor/operator view) needs a small aggregator layer or config that lists city -> endpoint/GC_HOME and fans out across supervisors.

Minimum acceptable aggregator slice:

1. Config file listing each city name, supervisor API URL, and operator metadata.
2. Read-only status/dashboard fanout across `/v0/cities` and city-scoped endpoints.
3. Mutating commands either route to the correct endpoint or fail closed with a clear "choose city/supervisor" error.

## Recommended implementation path

### Smallest safe cutover shape

1. Create one isolated `GC_HOME` per city, e.g. `~/.gc-supervisors/gt` and `~/.gc-supervisors/hobby`.
2. Each isolated home contains:
   - `cities.toml` with exactly one city entry.
   - `supervisor.toml` with a unique explicit loopback port.
3. Launch one service per city with that `GC_HOME` and without shared provider credential env in the service plist/unit.
4. Ensure no service-level shared `GC_DOLT_*` or `GC_CITY_RUNTIME_DIR` overrides collapse stores back together.
5. Keep current city roots and Dolt data roots per city.
6. Add a fleet-status/dashboard aggregator before mayor/operator workflows depend on the split.

### First-class gc changes worth doing

- `gc supervisor install --city <dir> --gc-home <dir> --port <port> --label-suffix <name>` or equivalent command that creates the isolated home, writes a single-city registry, writes the port config, and installs the service.
- Doctor checks for per-city supervisor split:
  - duplicate API ports
  - shared `GC_HOME`
  - shared `GC_DOLT_*` paths/ports
  - shared rig paths across different city supervisors unless explicitly allowed
  - provider credential env present in service plist/unit when the scoped credential cutover is intended
- Read-only fleet aggregator for status/dashboard, then mutating-route support if needed.

## Rough effort

- Existing isolated-`GC_HOME` deployment + deterministic ports + single-city registries + runbook: **0.5–1 day** code/docs/tests, excluding the live cutover rehearsal.
- First-class install/doctor command for per-city supervisors: **1–2 days**.
- Fleet aggregator/dashboard across N supervisors: **1–3 days**, depending on read-only status only vs full dashboard and mutating route support.

The hard part is the control-plane split and fleet visibility, not Dolt store isolation.

## Validation performed

- Source inspection of supervisor config, run, lifecycle, controller socket, city runtime, managed Dolt layout, managed Dolt port selection, and store-open paths at gascity commit `e2064514d55f27d354536d44fb5cbb5aeae357ab` before branching from current `origin/main` for this note.
- Live registry/state check for `gt` and `hobby` showed distinct city roots and distinct managed Dolt state/data directories.
- `CGO_ENABLED=0 go test ./internal/supervisor -count=1` passed.
- A targeted `cmd/gc` test regex run executed matching tests, but package exit was failed by the package-level Dolt leak guard because this host denied `/bin/ps` (`fork/exec /bin/ps: operation not permitted`). The test bodies printed `PASS` before the guard converted the package result to failure; this is an environment validation caveat, not a code evidence claim.
