# C4b — async rig-create: locks, goroutine placement, and sync-path composition

**Status:** DESIGN (C4 sub-doc). Companion to `G13-request-id-state-machine.md` (the
admission/state-machine contract this doc wires into code seams).
**Gates:** G14 (async 202 + rollback), G16 (per-rig-name lock), G17 (per-city guard,
G13 §7.1). Grounded against HEAD (`worktree-gc-remote`); every anchor read, not assumed.

---

## 1. Where the async branch lives

`humaHandleRigCreate` (`internal/api/huma_handlers_rigs.go:67`) grows the branch.
`RigCreateInput.Body` (`internal/api/huma_types_rigs.go:24`) gains `GitURL string
json:"git_url,omitempty"` and `RequestID string json:"request_id,omitempty"` (G13 §1/§10).

```
humaHandleRigCreate:
  git_url == ""  ⇒ today's sync path, unchanged: sm.CreateRig(rig) (:80) → 201
  git_url != ""  ⇒ async path:
      1. validate request_id + rig name (G13 §2) — BEFORE any lock/store access
      2. WithRigNameLock(city, name) {            // G16, §3 below — admission only
           run the G13 §4 state machine:
             live-index hit  → 202 replay (stored cursor) or 409
             durable succeeded → 200 exists / rolled_back → re-clone reset / 409 mismatch
             name-collision axis (§4.4) → 409 rig_name_conflict
           reqID  := body id, or newRequestID() synthetic (request_id.go:14) when absent
           cursor := s.currentCityEventCursor()   // request_id.go:22 — BEFORE the goroutine
           create durable in_flight record + live inflight/byName entry (G13 §3.5)
         }
      3. go provisionRigAsync(...)                // §2 — lock already released
      4. return 202
```

**The 202 body.** Per G13 §10 the operation has ONE unified output struct
(`RigCreateOutput{ Status int json:"-"; Body RigCreateResponseBody }`, mirroring
`SessionCreateOutput` at `huma_types_sessions.go:85-88`). The accepted variant populates
exactly the `asyncAcceptedBody` field set (`huma_types_sessions.go:77-82`):
`{status:"accepted", request_id, event_cursor}` with `Status = 202`. `asyncAcceptedBody`
itself cannot be the output type (Huma binds one output per op; this op also returns
200/201), so it is mirrored field-for-field, doc strings included.

**Cursor ordering is load-bearing.** `currentCityEventCursor()` is captured in the
synchronous handler body, before `go func(){…}` — the exact session ordering at
`huma_handlers_sessions_command.go:315→319→323`. Captured inside the G16 critical section
so the in-flight-replay 202 returns the same cursor the first 202 returned (G13 §5).

## 2. The detached goroutine

Copy the session-create launch shape (`huma_handlers_sessions_command.go:323-367`):
detached from the request context, every ctx-taking call gets `context.Background()`
(cf. `handle.Create(context.Background(), …)` at `:341`).

```go
go func() {
    defer s.recoverAsRequestFailed(reqID, RequestOperationRigCreate) // request_id.go:77 — runs LAST
    defer func() {                                                    // runs FIRST (LIFO)
        if !terminalized { idx.drop(cityName, reqID, rigName) }       // panic backstop: never wedge the name
    }()

    // A. clone/prepare, OUTSIDE both locks (only the byName live entry guards the name)
    if err := rig.CloneForProvision(gitURL, rigPath /* hardened, Group C */); err != nil {
        rollbackStaging(); markRolledBack(); terminalize()
        s.emitRequestFailed(reqID, RequestOperationRigCreate, "clone_failed", err.Error())  // request_id.go:68
        return
    }
    // B. the city-scoped window, under the G17 per-city guard (§4)
    if err := sm.CreateRig(rig); err != nil {                          // api_state.go:1505 — reused as-is (§5)
        rollbackStaging(); markRolledBack(); terminalize()
        s.emitRequestFailed(reqID, RequestOperationRigCreate, mutationErrorCode(err), err.Error())
        return
    }
    // C. G17 visibility barrier BEFORE terminal success (G13 §3.5 terminal order)
    waitRigVisible(s.state, rigName)   // s.state.Config() shows the rig && BeadStore(rigName)!=nil
    markSucceeded(resultFields)        // durable succeeded via SetMetadataBatch
    terminalize()                      // drop inflight/byName, close done chan
    s.emitAsyncResult(events.RequestResultRigCreate, rigName, RigCreateSucceededPayload{…}) // request_id.go:59
}()
```

- `RequestOperationRigCreate = "rig.create"` joins the operation constants
  (`event_payloads.go:48-50`); `RigCreateSucceededPayload` is registered in `init()`
  (`:558-560` pattern) and added to `requestIDFromPayload` (`request_id.go:83`) — G13 §10.
- **Panic path:** `recoverAsRequestFailed` only emits `request.failed`; the extra defer
  drops the live entry so the rig name is not wedged until restart (G13 §3.5 liveness).
  The durable record stays `in_flight` → it is an orphan; the **re-clone admission runs
  the same staging cleanup as the boot sweep for that `rig_name` before resetting to
  `in_flight`** (keeps the G13 §6 "never re-clone over un-cleaned staging" invariant for
  in-process-panic orphans that no boot sweep saw).
- **Rollback order (G13 §6):** staging dir/DB/config removal → durable `rolled_back` →
  live-entry drop → `request.failed` emit. Never mark before cleanup completes.

## 3. G16 — the per-rig-name lock (exact shape)

A new in-process lock in `internal/api`, mirroring `sourceworkflow.WithLock`'s
**refcounted channel-token** tier (`internal/sourceworkflow/sourceworkflow.go:228`) —
NOT a `map[string]*sync.Mutex` (leaks forever) and NOT the flock tier (admission is
process-local by construction: the live index is process-local, single-replica accepted,
G13 §12; a concurrent CLI `gc rig add` in another process is out of this lock's scope and
is caught by `CreateRig`'s under-lock duplicate guard, `api_state.go:1543-1547`).

```go
// internal/api/rig_name_lock.go
var (
    rigNameLocksMu sync.Mutex
    rigNameLocks   = map[string]*rigNameLock{} // key: normCityPath + "\x00" + rigName
)
type rigNameLock struct { token chan struct{}; refs int } // cap-1 token, sourceworkflow.go:219-222

func withRigNameLock(ctx context.Context, cityPath, rigName string, fn func() error) error {
    if strings.TrimSpace(rigName) == "" {
        return errors.New("rig name lock: empty key") // NOT fn() — the WithLock :230 gotcha, inverted
    }
    key := normalizePathForCompare(cityPath) + "\x00" + rigName
    lk := acquireRef(key)          // mirrors inProcessMutex, sourceworkflow.go:264
    defer releaseRef(key, lk)      // mirrors releaseInProcessMutex, :276 — refs==0 deletes the entry
    select {
    case <-lk.token:
    case <-ctx.Done():
        return ctx.Err()
    }
    defer func() { lk.token <- struct{}{} }()
    return fn()
}
```

- **Held for admission only** (validate → index → durable fallback → collision → record +
  entry + cursor). The clone/provision runs outside it; the `byName` live entry — not a
  held lock — excludes same-name work for the goroutine's lifetime (G13 §7).
- **Empty-key is an error, not a bypass.** `sourceworkflow.WithLock` early-returns
  `fn()` unlocked on an empty id (`sourceworkflow.go:230`); this variant refuses instead
  (rig name is already `minLength:"1"` on the wire, `huma_types_rigs.go:27`, plus G13 §2
  validation — the refusal is the programming-error backstop).
- **Conflict → 409 shape:** the lock never conflicts (it blocks); the 409s come from the
  admission checks under it. Mirror `sourceworkflow.ConflictError` (`sourceworkflow.go:36`)
  with a typed `rigNameConflictError{ Rig, InFlightRequestID, EventCursor }` rendered as a
  Huma 409 with `ErrorDetail`-style extensions — the structured-409 precedent is
  `internal/api/huma_handlers_sling.go:102`. Distinct codes: `request_id_conflict`
  (digest mismatch) vs `rig_name_conflict` (G13 §4.2).

## 4. G17 — the per-city guard (exact shape)

**The distinct per-city lock already exists and is already correct: it is
`controllerState.SerializeConfigWrite` (`cmd/gc/api_state.go:1324`) = `configedit.Editor.Do`
(`internal/configedit/configedit.go:153`) — a plain non-reentrant `sync.Mutex`
(`configedit.go:74`), one `Editor` per `controllerState`, i.e. per city in-process.**
C4b introduces no new per-city lock; it reuses this one via the sync `CreateRig` (§5).

Why it satisfies G13 §7.1's requirements:

1. **It is NOT `acquireProviderSemaphore`** (`cmd/gc/beads_provider_lifecycle.go:2110`;
   G13 cites :2075 — HEAD has drifted). The provider semaphore is the self-deadlocking
   one: cap-1 channel (`:2112`), re-entered by the provisioning path itself via
   `ensureBeadsProvider` → `acquireProviderSemaphoreForOp(cityPath,"start")` (`:722→:731`). Under `Editor.Do` the
   lock order is strictly `Editor.Do → providerSemaphore` (the semaphore is acquired and
   released inside the held `Do` span); nothing acquires them in the reverse order, so no
   deadlock. **Rule: the async goroutine must never call `SerializeConfigWrite` from code
   already holding the provider semaphore.**
2. **It covers the whole §7.1 window.** `CreateRig` wraps everything in
   `cs.SerializeConfigWrite(func() { return cs.mutateAndPoke(…) })` (`api_state.go:1530`):
   the `city.toml` read-modify-append (via `rig.Provision`'s config write), the routes
   regeneration (`WriteRoutes` dep → `writeAllRigRoutes(collectRigRoutes(…))`,
   `api_state.go:1567-1569`), and the `cityDoltConfigs` register + its `defer clear`
   (`api_state.go:1555-1556`) — register and clear both sit **inside** the held lock span,
   and `registerCityDoltConfigIfAbsent` (`beads_provider_lifecycle.go:123`) never clobbers
   the controller's persistent boot registration. (The §7.1 caveat about an unlocked
   `defer clearCityDoltConfig` is the CLI path `cmd_rig.go:276`, not this one.)
3. **Keyed correctly.** `cityDoltConfigs` writes under `normalizePathForCompare(cityPath)`
   (`beads_provider_lifecycle.go:109-115`); `Editor.Do` needs no key (one Editor per city),
   and the G16 key reuses the same normalization for its city component.

**Placement decision: the guard wraps the city-scoped window ONLY, not the clone.** A
`git clone` of a large repo can take minutes; holding `Editor.Do` for it would freeze every
config mutation city-wide (agent suspend, order toggles, other rig ops — every
`mutateAndPoke` call site). So the goroutine clones into the rig path first (step A, §2 —
no city-scoped resource touched; the name is protected by the `byName` entry), then enters
`SerializeConfigWrite` for the provision/config window (step B). Two concurrent
different-name adds therefore clone in parallel and serialize only their config appends —
exactly the §7.1 lost-update fix with minimal hold time.

## 5. Composition with the C2.4 sync `CreateRig` — reuse, not a fork

The async goroutine calls **the existing `controllerState.CreateRig`
(`cmd/gc/api_state.go:1505`) unchanged** — no async variant of the mutateAndPoke path.

- The clone (new, Group C: `internal/rig` gains the hardened clone step) materializes the
  rig working tree at `rigPath` *before* `CreateRig` runs. `rig.Provision` already handles
  a pre-existing directory: `StatRigPath` (provision.go:63-70) and the `.git` probe /
  `ProbeBranch` default-branch resolution (provision.go:73-80) treat the fresh clone as an
  adoptable existing path. `git_url` never needs to enter `ProvisionRequest`'s topology
  window.
- Everything C2.4 already built is inherited for free: the two-layer rollback
  (Provision topology snapshot inside, `mutateAndPoke`'s config snapshot outside,
  `api_state.go:1846`), the under-lock duplicate-name guard (`:1543-1547`, defense-in-depth
  behind G16 admission), the dolt-config registration window (`:1553-1557`), and the
  PostProvision no-self-dial rule (`:1580-1584`).
- **Error mapping diverges by mode:** sync 201 path maps `CreateRig` errors through
  `mutationError` to HTTP (`huma_handlers_rigs.go:81`); the async path maps the same
  errors to `request.failed` event codes (`provision_failed`, `already_exists`, …) —
  there is no HTTP channel left after the 202.
- **Async-only additions live around, not inside, `CreateRig`:** clone (before), staging
  rollback + durable state transitions + live-index terminalization + visibility barrier +
  terminal events (after). `CreateRig` stays mode-blind, so the sync 201 path cannot
  regress.

## 6. Failure matrix (goroutine)

| Failure point | Cleanup | Durable state | Live entry | Event |
|---|---|---|---|---|
| clone fails | remove partial dir | `rolled_back` | dropped | `request.failed` (`clone_failed`) |
| `CreateRig` fails (Provision inner) | Provision topology snapshot restored; remove cloned dir | `rolled_back` | dropped | `request.failed` |
| `CreateRig` fails (refresh outer) | `mutateAndPoke` config snapshot restored; remove cloned dir | `rolled_back` | dropped | `request.failed` |
| panic anywhere | none guaranteed (dir may remain) | stays `in_flight` (orphan; re-clone admission or boot sweep cleans staging) | dropped by backstop defer | `request.failed` (`internal_error`) via `recoverAsRequestFailed` |
| success | — | `succeeded` (after visibility barrier) | dropped after barrier | `request.result.rig.create` |

## 7. Risks / open points

1. **`Editor.Do` contention from other writers.** Any long-running `mutateAndPoke`
   caller (e.g. a slow rig init inside a config reload) delays the async goroutine's
   config window; conversely the provision window (bead-store init can exceed 30s,
   `providerOpTimeout` comment `beads_provider_lifecycle.go:2136`) blocks all config
   edits while held. Accepted: same exposure the sync C2.4 path already has; the clone —
   the unbounded part — is outside the lock.
2. **Panic-orphan staging cleanup is a new admission responsibility** (re-clone path must
   run the sweep's per-rig cleanup, §2) — easy to miss in C4 implementation; pinned in the
   §6 matrix and needs a dedicated test (G13 §11 "orphan never replays" + staging assert).
3. **Cross-process writers** (local CLI `gc rig add` against the same city) are outside
   both locks; the under-lock duplicate guard in `CreateRig` catches name collisions, but
   a CLI-side city.toml write during the goroutine's window can still interleave at the
   file level (pre-existing exposure, unchanged by this design; flock unification is
   future work).
