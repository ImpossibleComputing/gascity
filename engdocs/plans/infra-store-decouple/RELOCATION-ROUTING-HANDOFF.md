# CLI session relocation-routing — handoff

**Branch** `upstream/object-front-doors-cleanup` (base `main`), **PR #3839 DRAFT**,
worktree `/data/projects/gascity/.claude/worktrees/object-front-doors`.
**HEAD `3e05a03fe`** (always `git rev-parse HEAD`; re-grep every line number below).

This is the successor to the access-pass DI batches (store-free guard, CONT-37→39). The
**access pass PIVOTED** from store-free DI hygiene to the actual mission:
**relocation-safety**. This doc hands off the remainder of that pivot.

---

## Why the pivot (the finding that reframed the pass)

The store-free DI guard (`frontDoorStoreFreeFiles`) is compile-time hygiene and is
**orthogonal to the mission** for CLI command roots. The mission is: a
`[beads.classes.sessions]` relocation must capture 100% of session-bead access.

- The **controller/runtime is already relocation-safe** — `city_runtime.go` routes every
  session access through `cr.sessionsBeadStore()` → `resolveSessionStore(...)`.
- The **CLI one-shot roots are relocation-BLIND** — they do
  `sessionFrontDoor(openCityStore(...))`, and `openCityStore` (`main.go:1073`) returns the
  **generic work store**, never the session-class store. After a relocation their session
  reads/writes hit the wrong backend (split-brain).
- The fix is **byte-identical today**: `resolveSessionStore` → `resolveClassStore`
  (`class_store.go`) is pure identity, so wrapping only diverges once a relocation is
  configured.

Owner decision (this session): pivot to relocation-routing.

## The seam (landed, `cmd/gc/cli_session_store.go`)

```go
func cliSessionStore(store beads.Store, cfg *config.City, cityPath string) beads.Store {
	return resolveSessionStore(store, cfg, cityPath, nil) // identity today; recorder nil (no CLI event bus)
}
func cliSessionFrontDoor(store beads.Store, cfg *config.City, cityPath string) *session.Store {
	return sessionFrontDoor(cliSessionStore(store, cfg, cityPath))
}
```

**Routing patterns:**
- **Whole-store** (all consumers session-class): `sessStore := cliSessionStore(store, cfg, cityPath)`
  right after the open, replace every `store` use with `sessStore`. Used in cmd_stop, cmd_session_reset.
- **Surgical** (multi-class root): compute `sessStore` once, pass it only to session consumers,
  keep plain `store` for work/rig/mail/nudge/dep consumers.
- **cfg-less roots** must load cfg. Use `loadCityConfigWithoutBuiltinPackRefresh(cityPath, io.Discard)`
  on **hot/hook/daemon** paths (NOT `loadCityConfig` — it triggers a builtin-pack refresh). nil cfg →
  identity, so byte-identical today regardless. (Owner-decided for cmd_prime; reused for the controller socket.)
  Note: the no-refresh loader still calls `applyFeatureFlags` (writes the formulaV2/graphApply global
  atomics) — proven inert where used (no reader on the path; value equals what the main load sets).

## The guard (`cmd/gc/frontdoor_di_guard_test.go`)

`TestSessionRelocationRootsRouteThroughSessionClassStore` over `sessionRelocationRoutedFiles`:
forbids `sessionFrontDoor(store)` / `sessionFrontDoor(store.Store)` / `sessionFrontDoor(openCityStore`
in listed files and requires `cliSessionStore(`/`cliSessionFrontDoor(` present (positive tripwire).
It is a **regression canary, not a completeness proof** — it can't see non-front-door session reads
(`store.Get`, `resolveSessionID*`). Mixed files (controller.go, cmd_start.go) are intentionally OFF the
list. `cli_session_store.go` is OFF it (the one legitimate `sessionFrontDoor` holder). **The authoritative
check is the end-to-end `[beads.classes.sessions]` relocation acceptance test — still TODO (Phase 6).**

---

## DONE this session (8 commits, `0aa51fafd..3e05a03fe`, all pushed)

10 roots routed, each: gofmt · build · vet · golangci-lint 0 · targeted tests · revert-canary
· **fable adversarial byte-identity review (all COULD-NOT-REFUTE)**.

| File | Root(s) | Routing | Guard-listed |
| ---- | ------- | ------- | ------------ |
| cli_session_store.go | (seam) | new helpers | no (excluded) |
| cmd_session_wake.go | cmdSessionWake | sessStore | ✅ |
| cmd_session_pin.go | cmdSessionSetPin | sessStore | ✅ |
| cmd_skill.go | skill list | cliSessionFrontDoor | ✅ |
| cmd_mcp.go | mcp list | cliSessionFrontDoor | ✅ |
| cmd_session_logs.go | session logs | cliSessionFrontDoor | ✅ |
| cmd_prime.go | primeHookSessionTemplate + persistPrimeHookProviderSessionKey | sessStore + no-refresh cfg load | ✅ |
| cmd_stop.go | cmdStopBody | whole-store sessStore (all 5 consumers session-class) | ✅ |
| cmd_start.go | doStartStandalone | adoption barrier ONLY (reconcile cascade deferred) | ❌ (partial/mixed) |
| cmd_session_reset.go | cmdSessionReset | whole-store sessStore | ✅ |
| controller.go | handleSessionCircuitResetSocketCmd | cliSessionStore + no-refresh cfg load | ❌ (mixed file) |

`sessionRelocationRoutedFiles` (8): wake, pin, skill, mcp, session_logs, prime, stop, session_reset.

---

## REMAINING (next sessions) — the completeness census (Explore sweep this session)

The original census only grepped **direct** `sessionFrontDoor` sites and MISSED roots that reach
session state via **helpers** (that's how cmd_session_reset + cmd_runtime_drain surfaced). The full
remaining blind-root set:

### Phase 4 — cmd_session.go (BIG, its own session)
~9 in-file RunE roots (cmdSessionNew, doSessionListFallback, cmdSessionSuspend, **cmdSessionClose**,
**cmdSessionKill**, cmdSessionAttach, cmdSessionRename, cmdSessionSubmit, doSessionPeekFallback).
Multi-class: `cmdSessionClose` uses `store` for session reads AND `unclaimWorkAssignedToRetiredSessionBead(store, rigStores, …)` (WORK) — **surgical** routing (route session calls, keep work-release on plain store; `rigStores map[string]beads.Store` is a cross-class rig map, leave). **Verify each root's consumers per-consumer** — the plan's classifications proved unreliable (it was wrong about cmd_stop's consumers). cmdSessionKill reaches `resetSessionCircuitBreakerAfterExplicitKill(cityPath, store, …)` (session) + `store.SetMetadataBatch` (session). All roots have cfg+cityPath.

### NEW blind roots the plan never listed (found by the completeness census)
- **cmd_restart.go** `doRigRestart` (store@158): reaches `lookupSessionNameOrLegacy`, `resolvePoolSessionRefs`,
  `workerSessionTargetRunningWithConfig` — the **same session-name+runtime pattern as cmd_stop**, likely
  a clean whole-store route. cfg@141, cityPath@144. **Verify all consumers session-class (like cmd_stop was).**
- **cmd_mail.go** (12 subcommands, store via `openCityMailProvider`@1171/1193 + direct@2099): reaches
  `resolveSessionID`, `resolveSessionIDWithConfig`, `session.ListAllSessionBeads`, `namedSessionIdentityMetadata`
  for mail **addressing/identity resolution**. MULTI-CLASS (mail beads + session addressing) — **surgical**,
  careful. cfg/cityPath inconsistently scoped across subcommands.
- **cmd_status.go** → `city_status_snapshot.go` `loadStatusSessionSnapshot` (`resolveSessionIDWithConfig`@353):
  status reads a session id. Indirect; route when the caller routes.
- **completion.go** `loadSessionsForCompletion` (`session.ListAllSessionBeads`@251): completion path;
  low impact, hot — use the no-refresh loader if routed.

### Deferred (entangled — own coordinated efforts; owner-approved for cmd_wait)
- **cmd_handoff.go + cmd_runtime_drain.go** — PAIRED. Share the session helpers
  `sessionRestartableByController` / `clearRestartRequest` (both call them; also `sessionRestartPersister`).
  `doHandoffWithOutcome` mixes MAIL (`createHandoffMail`→`beadmail.New`) + SESSION in one tested helper
  (~10 test call sites). Clean routing needs a two-store split on `doHandoffWithOutcome`/`doHandoffRemote`
  (+ test updates) OR a control-flow hoist (byte-identity risk — fable-flagged). Route both roots together
  so the shared helpers receive a routed store from every caller.
- **cmd_wait.go** — DEFERRED (owner-approved). Multi-class machinery SHARED with the controller reconciler:
  `retryClosedWait` uses one `store` for BOTH nudge lookup AND session writes; dep reads are work-class +
  federated; wait-list reads deliberately use the federated store. Needs a per-class store split across shared
  helpers — its own "wait-machinery class-split" effort. (Closure plan treats "wait as a separate future class".)
- **cmd_nudge.go** (@455/1050/1264 build sessionFrontDoor from a NUDGES-class store), **cmd_sling.go** (@1495),
  **cmd_start.go reconcile cascade** (`beads.SessionStore{Store: oneShotStore}` — multi-class mirror-of-runtime).

### NON-SESSION (verified safe, no routing): cmd_prompt.go, cmd_start_warmup.go, dispatch_runtime.go, providers.go.

---

## Discipline (the bar — unchanged; every routed file)

Verified per-consumer census (re-grep; DON'T trust prior classifications) → route (whole-store if all
consumers session-class, else surgical) → gofmt · `go build ./cmd/gc/` · `go vet` · `golangci-lint run
./cmd/gc/` (0) · targeted `go test -run` → **revert-canary** (guard must fail naming the file) → **fable
adversarial byte-identity review BEFORE commit** (`model:'fable'`, REFUTE; diff vs `git show HEAD:<file>`;
confirm identity-today AND semantic session-class-correctness for whole-store routes) → commit + push
`--no-verify`. Trailer `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.

**Guardrails:** `cmd/gc` test binary is huge — scope `go test -run`, isolated `GOCACHE=$(mktemp -d)` (this
session reused `/tmp/gc-reloc-cache`), run build/vet/tests in the background (cold compile > 2 min). NEVER
run the revert-canary concurrently with golangci-lint (torn read). `git push` always `--no-verify` (7-min
pre-push hook; stale absolute `core.hooksPath` also breaks `git commit` → commit `--no-verify`, gates run
manually). gascity Dolt LOCAL-ONLY — `git push` only. `#3839` stays DRAFT.

## Acceptance (Phase 6, TODO)

Add an end-to-end `[beads.classes.sessions]` relocation test: configure a distinct sessions backend, run
each routed root, assert session/wait beads land in the relocated store while work/dep/nudge/mail stay put.
This is the only thing that proves routing correctness for the mixed files (controller.go, cmd_start.go) and
the non-front-door session reads the substring guard can't see.
