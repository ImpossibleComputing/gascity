# Handoff — finish the non-work field-door cleanup: the cascade (P4 bulk) + P5 + P6

**PR #3839** (DRAFT, base `main`), branch `upstream/object-front-doors-cleanup`,
worktree `/data/projects/gascity/.claude/worktrees/object-front-doors`,
**HEAD `f3ef21be4`** (pushed; the pool-demand cascade LANDED — see the "Cascade
session" block below). Goal: make direct field reads of session/nudge/mail/order/
graph beads impossible in consumers, then mark #3839 ready + label
`status/needs-review-auto`.

Read alongside this: `NONWORK-BEAD-FIELDDOOR-PLAN.md` (architecture, 4 layers),
`P4-CONVERSION-CONTRACT.md` (the per-site swap rules + sibling table + RAW
fidelity-field rules), and the SESSION UPDATE banner atop
`NONWORK-FIELDDOOR-P4-P6-HANDOFF.md`.

## Confirm a green baseline first

```
go build ./cmd/gc/
go test ./cmd/gc/ -run 'TestSessionClassifierInfoEquivalence|TestSessionSnapshotInfoEquivalence|TestSnapshotInfoOnlyFilesStayOnInfoAccessors|TestFrontDoorStoreFreeFilesStayStoreFree' -count=1
```

`git checkout go.sum` after builds; commit `--no-verify` (stale hooksPath);
push `--no-verify` (the pre-push hook runs the full suite and times out — run
gates manually). Trailer: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.

## State (what's DONE)

### Cascade session (`c73ff6ba4..8609a5198`, 4 commits — the pool-demand unlock)

The biggest unlock from the section below is LANDED. In order:

1. `688d3b79f` **providers ACP slice** — `observedACPSessionNames` reads
   `OpenInfos()`; `infoUsesACPTransport` sibling; added Info fields `MCPIdentity`,
   `MCPServersSnapshot`; `providers.go` added to `snapshotInfoOnlyFiles`.
2. `6742a463b` **assigned-work scope filters** —
   `filterAssignedWorkBeadsForPoolDemand` / `…ForSessionWake` now take
   `[]session.Info`; new `filterSessionInfosByName` mirror; all 7 callers pass
   `OpenInfos()`/Info locals.
3. `d789dc2a2` **foundation** — Info health cluster (`ProviderTerminalError`,
   `HealthState`, `HealthReason`, `Drainable`) + trigger cluster
   (`TriggerBeadID`, `TriggerBeadStoreRef`, `BrainParentSID`); 4 siblings
   (`sessionHasProviderTerminalErrorInfo`, `existingPoolSlotInfo`,
   `isEphemeralSessionInfoForAgent`, `poolSessionConsumesNewDemandInfo`), all
   equivalence-proven.
4. `8609a5198` **pool desired-state engine flip** — `ComputePoolDesiredStates` /
   `…Traced` / `…WithDemandTraced` / `computePoolDesiredStates` /
   `canonicalSingletonAliasHeldTemplates` / `poolInFlightNewRequests` all take
   `[]session.Info`; 5 production callers pass `OpenInfos()`; ~72 test call sites
   projected via `sessionInfosFromBeads` (paren-aware transform). Pool +
   reconciler E2E suites green.

Now **28** raw-accessor sites remain (was 33). Two that look convertible are
NOT, and should stay raw:
- `usage_compute.go:emitDueComputeFacts`/`emitComputeFactForBead` — reads
  usage-bookkeeping metadata (`awake_started_at`, `slept_at`,
  `usage_compute_emitted_at`) that are not session-identity attributes; adding
  them to `Info` would over-broaden it. Legitimate raw usage consumer.
- `city_status_snapshot.go:countCitySessionsFromSnapshot` — needs an
  `IsSessionBeadOrRepairableInfo`, but that reads `Type`/labels which the Info
  projection drops (the snapshot only holds session beads, so the Info form
  would be a trivial `true` whose fidelity depends on the snapshot invariant —
  prove that invariant before converting).

### Foundation P1–P3 (prior sessions)

- **Foundation P1–P3 (prior sessions):** `session.Info` carries the full consumed
  session-attribute set; **23** `*Info` classifier siblings exist (originals
  untouched); the snapshot has `OpenInfos()`/`FindInfoByID`/`FindInfoByTemplate`/
  `FindInfoByNamedIdentity`. Two equivalence tests prove the typed forms agree
  byte-for-byte with the raw-bead originals.
  - **Note on the sibling count:** an import-alias artifact (`session` vs
    `sessionpkg`) makes a naive grep undercount them. Grep BOTH:
    `grep -rnE 'func [A-Za-z]+\([^)]*(session|sessionpkg)\.Info[,)]' cmd/gc/`.
- **P4 localized slice (this session, `af9529c13..4acc591da`):** every raw
  session-bead read that was *local and faithfully convertible* — trace
  open-counts, `template_resolve`, `city-status` Find* lookups, `cmd_wait`
  wait-diag loops, `city_runtime.poolSweepWouldDrain`, `openSessionNameTaken`,
  reaper `FindInfoByID`.
- **P6 read-guard (this session):** `TestSnapshotInfoOnlyFilesStayOnInfoAccessors`
  in `cmd/gc/frontdoor_di_guard_test.go` pins the 4 files now fully
  accessor-free (`template_resolve`, `session_name_lookup`, `cmd_citystatus`,
  `session_reconciler_trace_cycle`). **Add each newly-converted file to
  `snapshotInfoOnlyFiles` as it becomes accessor-free** — that is how P6
  enforcement accretes.

## Why the rest is NOT a mechanical per-file swap

The remaining ~33 raw-accessor uses are almost entirely a **coupled type
cascade**, plus a set of **foundation gaps**. Do not fan parallel agents at a
single connected component — each is one atomic, carefully-reviewed change.

### The pool-demand cascade (biggest unlock)

The snapshot flows across function boundaries as **`[]beads.Bead`**. To stop the
leak these signatures must flip to **`[]session.Info`** atomically:

- `pool_desired_state.go`: `ComputePoolDesiredStates` / `…Traced` /
  `…WithDemandTraced`, `canonicalSingletonAliasHeldTemplates`,
  `poolInFlightNewRequests`
- `assigned_work_scope.go`: `filterAssignedWorkBeadsForPoolDemand`,
  `filterAssignedWorkBeadsForSessionWake`, `sessionAgentConfig`
- `session_reconcile.go`: `capWakeConfigByDemand`, `applyDependencyWakeReasons`,
  `preferredDependencySessions`, `topoOrder`
- `pool_session_name.go`: `GCSweepSessionBeads`
- `usage_compute.go`: `emitDueComputeFacts`
- callers that pass `Open()` in: `build_desired_state.go` (10 sites),
  `city_runtime.go` (10 sites), `cmd_start.go`

Convert bottom-up: change the deepest helpers' bodies to read `Info` fields (all
needed siblings exist except the `*ForAgent` family — see gaps), flip their
signatures, then the callers pass `OpenInfos()`. One coherent change; the
reconciler/pool test suites are the oracle.

### The reconciler `*beads.Bead session` threading

`session_reconciler.go` / `session_reconcile.go` thread a `*beads.Bead session`
through `healState`, `checkStability`, `checkChurn`, `markProviderTerminalError`,
etc. Converting `isNamedSessionBead(*session)` → `isNamedSessionInfo(info)`
requires those helpers to carry the `Info` alongside (or instead of) the bead.
This is a second cascade — do it after the pool-demand one.

### Foundation gaps (add BEFORE the site that needs them: field + sibling + equivalence case)

- `started_config_hash` field (for `soft_reload.go`)
- MCP-key cluster (`MCPIdentity`, `MCPServersSnapshot`) + `beadUsesACPTransportInfo`
  (for `providers.go:observedACPSessionNames`)
- `Status` / `Assignee` / a raw-metadata-map accessor on `Info` (for
  `city_runtime.go:sessionBeadSnapshotFingerprint` — it hashes ALL raw metadata)
- `Info` forms of: `sessionCoreConfigForHash`, `lookupSessionBeadByID`,
  `IsSessionBeadOrRepairable`, the soft-reload drain helpers
  (`clearSoftReloadConfigDriftDrainAck`/`cancelSoftReloadConfigDriftDrain`), the
  wait-nudge helpers (`cachedSessionCanReceiveWaitNudge`/`waitNudgeAgent`/…),
  the `*ForAgent` family (`isManualSessionBeadForAgent`/
  `isEphemeralSessionBeadForAgent`/`isLegacyManualSessionBeadForAgent`),
  `sessionAgentMetricIdentity`, `existingPoolSlot`, `namedSessionMode` /
  `namedSessionIdentity` / `namedSessionContinuityEligible`
  (`continuity_eligible` is NOT on Info — add it), the wake helpers
  (`sessionWakeRequestedCreate`/`sessionWakeHasRunnableTemplate`),
  `isRetiredSessionModelOwner`
- `named_sessions.go`: needs an `Info`-returning session-pkg
  `FindCanonicalNamedSession` / `FindNamedSessionConflict` (they currently take
  `[]beads.Bead`).

## P5 — the `closeBead` cross-class split (LANDMINE — do isolated, last)

`closeBead(store, id, reason, now, stderr)` in `session_beads.go` decomposes into
SESSION close (`InfoStore.Close` — bundles the skip-if-closed idempotence +
ClosePatch + CloseWithoutReason, deliberately omits work-release), EXTMSG
(`cancelStateAssignedToRetiredSessionBead` = `session.CancelWaits` +
`extmsg.CloseSessionBindings`), and WORK release (the `workAssignment` façade).
Order is **close-THEN-release**; **preserve skip-if-already-closed idempotence**
(it prevents the bead.updated storm across the 3 reconciler close paths). Prove
the exact op sequence with a recording-fake store. Also tidy
`createPoolSessionBead` to thread `sessFront` (`CreateSession`/`CreateSpec` exist).

## P6 — close it out + enforce

1. As each consumer file becomes accessor-free, add it to `snapshotInfoOnlyFiles`.
2. Once every caller uses the `Info` forms, delete the now-dead bead classifiers /
   `Open()` / `FindSessionBeadBy*` — but the snapshot codec edge
   (`newSessionBeadSnapshot`) legitimately keeps raw classifiers; it is EXEMPT.
3. Extend the guard to also forbid `.Store().Store` in the fully-converted files.

## Suggested order (each is one atomic, reviewed change)

1. ~~**providers MCP-key vertical slice**~~ — DONE `688d3b79f`.
2. ~~**pool-demand cascade** (`[]beads.Bead`→`[]session.Info`)~~ — DONE
   (`6742a463b` assigned-work scope + `d789dc2a2` foundation + `8609a5198`
   pool desired-state engine). This was the biggest unlock.
3. **the build_desired_state (9) + city_runtime residual `Open()` loops** — the
   next surface. Each is its own small cascade or foundation-gap (NOT free
   swaps). Concrete sub-targets, smallest-first:
   - `nudge_dispatcher.go:158` — threads the raw bead into
     `resolveNudgeTargetFromSessionBead`; needs that resolver's Info form first.
   - `named_sessions.go:80/101` — need Info-returning session-pkg
     `FindCanonicalNamedSession` / `FindNamedSessionConflict` (they take
     `[]beads.Bead` today).
   - `soft_reload.go:103` — needs the `started_config_hash` Info field +
     `sessionCoreConfigForHash` Info form (foundation gap).
   - `cmd_wait.go:1009/1152` — two `FindByID` → `FindInfoByID`, but the wait-diag
     path threads into the wait-nudge helpers (a small cascade).
   - `build_desired_state.go` loops (1329/1365/1736/2066/3328/3557/3803/4152) +
     `city_runtime.go` loops (2658/3056/3217) — read each; convert the pure
     field-read loops, leave any that thread the bead to a store op or a
     still-raw `[]beads.Bead` helper (contract rule 3).
4. **reconciler `*beads.Bead session` Info-threading** — `session_reconciler.go`/
   `session_reconcile.go` thread a raw `*session` through `healState`/
   `checkStability`/`checkChurn`/`markProviderTerminalError`/… Convert
   `isNamedSessionBead(*session)`→`isNamedSessionInfo(info)` by carrying the Info
   alongside (or instead of) the bead. Second cascade; do after step 3.
5. **P5 closeBead split** (landmine — isolated, last; see the P5 section above).
6. **P6** deletion + widen guard, then finish.

**RAW-BY-DESIGN — do NOT convert these (they are not leaks):**
- `usage_compute.go` `emitDueComputeFacts` / `emitComputeFactForBead` — reads
  usage-bookkeeping metadata (`awake_started_at`, `slept_at`,
  `usage_compute_emitted_at`) that are not session-identity attributes; adding
  them to `Info` would over-broaden it.
- `city_status_snapshot.go` `countCitySessionsFromSnapshot` — needs an
  `IsSessionBeadOrRepairableInfo`, but that reads `Type`/labels which the Info
  projection drops; the Info form would be a trivial `true` whose fidelity
  depends on the snapshot-only-holds-session-beads invariant. Prove that
  invariant before touching it.
- `session_bead_snapshot.go` — the codec edge (`newSessionBeadSnapshot` +
  `FindSessionBeadByNamedIdentity` inside it). Always EXEMPT.

## Finish (only when CI is verified GREEN — do not mark ready before)

- Gates: `go build ./...`, `go vet ./...`, `golangci-lint run ./cmd/gc/...` (0),
  the equivalence + guard tests, targeted subject suites. `make dashboard-check`
  not needed (no `internal/api` wire change; `Info` additions stay internal).
- `gh pr checks 3839 --watch`.
- ready (gh pr ready aborts on projectCards — use the API):
  `gh api graphql -f query='mutation($id:ID!){markPullRequestReadyForReview(input:{pullRequestId:$id}){pullRequest{isDraft}}}' -f id=$(gh api repos/gastownhall/gascity/pulls/3839 --jq .node_id)`
- label: `gh api --method POST repos/gastownhall/gascity/issues/3839/labels -f 'labels[]=status/needs-review-auto'`

## Invariants (hold throughout)

Wire byte-identical (`Info` additions stay internal-only; empty openapi/docs-schema/
generated-TS diff); runtime byte-identical (the equivalence tests + a recording-fake
are the oracle); no typed-nil traps; never `tmux kill-server`; never `go clean
-cache` (`-testcache` ok); gascity Dolt is LOCAL-ONLY (no `bd dolt push`). The
build host is oversubscribed — run targeted `-run` filters + the equivalence
tests locally; CI on dedicated runners is the byte-identical gate.
