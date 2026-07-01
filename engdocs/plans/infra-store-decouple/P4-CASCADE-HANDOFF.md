# Handoff — finish the non-work field-door cleanup: the cascade (P4 bulk) + P5 + P6

**PR #3839** (DRAFT, base `main`), branch `upstream/object-front-doors-cleanup`,
worktree `/data/projects/gascity/.claude/worktrees/object-front-doors`,
**HEAD `f38938195`** (pushed; the pool-demand cascade + four more small cascades
LANDED — see the "Cascade session" and "Post-cascade session" blocks below).
Goal: make direct field reads of session/nudge/mail/order/graph beads impossible
in consumers, then mark #3839 ready + label `status/needs-review-auto`.

## State (Post-cascade session, `29a152836..f38938195`, 4 commits)

Raw-accessor surface is down to **20** non-test sites (was 28). Landed, each an
atomic byte-identical change with an oracle:

1. `9a3380e0e` **nudge dispatcher** — `nudge_dispatcher.go` reads `OpenInfos()`
   + new `resolveNudgeTargetFromSessionInfo` (shared `buildNudgeTarget` tail).
   Foundation: `Info.TransportMetadata` (RAW transport — the resolver's
   found.Session fallback needs the raw, un-normalized value).
   `TestNudgeTargetInfoEquivalence` oracle. `nudge_dispatcher.go` guard-pinned.
2. `29a152836` **named-session snapshot lookups** — `findCanonicalNamedSessionInfo`
   / `findNamedSessionConflictInfo` read `OpenInfos()`; all 7 callers read
   `Info.SessionNameMetadata`. Foundation: `Info.Type` + `Info.ContinuityEligible`;
   `IsSessionBeadOrRepairableInfo` + the named-session Info classifier family +
   `FindCanonicalNamedSessionInfo`/`FindNamedSessionConflictInfo` (session pkg).
   `TestNamedSessionInfoEquivalence` oracle. `named_sessions.go` guard-pinned.
3. `2f61a7bf0` **build_desired_state pure loops** — the 3 pure-field-read loops
   (readyAssignedWorkAssignees skip-set, on_demand assignee collection,
   scale-check retained-count). Foundation: `scaleCheckPartialSessionRetainableInfo`
   (pinned in the classifier-equivalence test). File NOT guard-pinned (5 Open()
   loops remain — all thread the bead into the *ForAgent family / candidate slices).
4. `f38938195` **wait config-drift loop** — `cmd_wait.go` ~1009 reads
   `FindInfoByID` + new projecting `lookupSessionBeadByIDInfo`. File NOT
   guard-pinned (the ready-wait-nudge loop ~1164 threads the bead into the
   wait-nudge helper family — no Info form yet).

**Blocked-by-a-bigger-cascade (the 20 remaining sites):** almost all remaining
sites are blocked on the **reconciler `*beads.Bead session` threading** (step 4
below) or a store-op / raw-`[]beads.Bead` boundary (contract rule 3):
- `build_desired_state.go` 2079/3341/3570/3816/4165, `session_beads.go` 2033,
  `city_runtime.go` 2658 — thread the bead into the *ForAgent classifier family,
  `lookupSessionBeadByID`-style FindByID helpers, or candidate `[]beads.Bead`
  slices sorted/passed onward.
- `city_runtime.go` 3056 (`filterSessionBeadsByName`) — its remaining caller
  (2896) feeds `newSessionBeadSnapshot(open)` + the raw-bead reconciler; legit
  raw until the reconciler takes Info.
- `soft_reload.go` 103 — threads the bead into `sessionCoreConfigForHash`
  (→ `applyTemplateOverridesToConfig` reads RAW `template_overrides` via
  `ParseTemplateOverrides(session.Metadata)`) + the drain helpers
  (`clearSoftReloadConfigDriftDrainAck`/`cancelSoftReloadConfigDriftDrain` →
  `cancelSessionConfigDriftDrain`); entangled with the reconciler cascade.
- `cmd_start.go` 904/918, `city_runtime.go` 1159/2246/2158 — thread the raw
  `open []beads.Bead` into store/reconciler ops.
- `session_beads.go` 57 — returns `sessionBeads.Open()` as `[]beads.Bead` to
  raw-bead callers.
- `session_lifecycle_parallel.go` 809 — passes `snapshot.Open()` into
  `resolvePreservedConfiguredNamedSessionTemplate` (raw-bead helper).
- `cmd_wait.go` 1164 — the wait-nudge cascade (see commit 4).

**RAW-BY-DESIGN (confirmed, do NOT convert):** `city_status_snapshot.go` 411
(`countCitySessionsFromSnapshot`), `city_runtime.go` 2153 (`emitDueComputeFacts`,
usage bookkeeping), `city_runtime.go` 3217 (`sessionBeadSnapshotFingerprint` —
hashes ID/Status/Assignee/ALL raw metadata; a whole-bead change fingerprint, not
a session-attribute read).

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
3. ~~**the tractable build_desired_state + city_runtime + cmd_wait loops**~~ —
   the cleanly-convertible ones are DONE (post-cascade session commits 1–4:
   nudge dispatcher, named-session lookups, the 3 pure build_desired_state loops,
   the wait config-drift loop). Every REMAINING loop threads the bead into a
   store op / the *ForAgent family / a raw `[]beads.Bead` helper, so they are
   NOT free swaps — they unblock only after step 4. See the "State
   (Post-cascade session)" block above for the per-site blocking reason.
4. **reconciler `*beads.Bead session` Info-threading (NOW the primary unlock)** —
   `session_reconciler.go`/
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
