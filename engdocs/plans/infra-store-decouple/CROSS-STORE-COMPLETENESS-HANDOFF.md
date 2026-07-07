# Cross-store completeness — session handoff

**2026-07-07.** Where the domain/infra store split stands and what's next.
Companion: the landmine inventory + test plan of record at
`engdocs/contributors/cross-store-split-landmines.md`.

## TL;DR

The **interface (front-door) layer is merged to OSS main** (#4017). The
**store-split is rebased onto it and validated** (branch
`feat/domain-infra-store-split`), and two red-team fixes landed. An audit then
found the split is **not worker-complete**: 16 cross-store landmines, one root
pattern. The unifying design is settled (composite `claimableStore` +
composite-aware `gc ready`). Next up: **P0** — make a formula actually *run* on a
split city (dispatcher discovery + worker claim), then the DAG-complete parent
lifecycle. All TDD, tests fail-on-split-first.

## Branch state

- **Working branch: `feat/domain-infra-store-split` @ `fb4150654`** (18 commits
  ahead of `origin/main` @ `37b7af53f`). **LOCAL, not pushed.** Builds, `go vet`
  clean, full fast suite + real-Dolt integration (E2.5/E3/E4.4) all GREEN.
  - E2.1→flip (the rebased split, 15 commits) + this session's 3:
    - `cf8eff46f` fix issue-2 `scopedStoreLike` class-preserving (TDD +
      real-Dolt validated).
    - `881833b68` fix issue-4 `handler_status.go` API status → `SessionsBeadStore`.
    - `fb4150654` the landmine audit doc.
- **Original preserved:** `upstream/object-front-doors-cleanup` @ `ec7352b36`
  (pre-rebase; do not delete).
- **Upstream:** `origin/main` @ `37b7af53f` = the front-door interface layer
  (#4017, squash). **Do NOT re-do the interface layer — it's merged.**
- **Deployed reference:** worktree `/data/projects/gascity/.claude/worktrees/fix-main-ci`,
  branch `rebase/dispatch-control-ready-onto-main` @ `c2257d206` — a LIVE
  sqlite-split deployment. Its 4 commits (`95518bc3a..c2257d206`) are the
  **proven port** for landmine #1 (control-ready discovery). READ-ONLY — it's
  running (tmux window 3).

## Settled design (do not relitigate)

- **Keep `ClassGraph` in the infra store.** Formula step beads carry
  `gc.root_bead_id` → `ClassGraph` → infra. Rationale (owner): high-volume,
  ephemeral substeps of the human-readable `ClassWork` domain beads; the split's
  value is relocating that storage independently.
- **Unifying fix = a composite `claimableStore` (work ∪ graph)** + make
  **`gc ready` composite-aware**, and switch the split-city `work_query` from
  `bd ready` (single-store external CLI) → `gc ready` (in-process composite).
  `Ready()`/`List` fan out + merge; `Get`/claim route to the owning backing store
  by id-prefix/class (a bead lives in exactly one store → no double-claim). This
  subsumes per-caller federation (the hook's `appendGraphHookStore`, the pool
  scan's `coordClassStoreCandidates`).
- **Control dispatch stays `ClassGraph`-only** → point it at the graph store; do
  NOT federate. **Finalize parent-close** stays domain-via-`source_store_ref`.

## The common pattern (why 16 landmines)

The split routed **writes** through the class front doors but cross-store
**reads / links / discovery** were not — they fall through to the wrong store and
**FAIL OPEN** (empty result sets, traced no-ops, premature readiness) rather than
erroring. Fixing the split = routing each such read/link through the composite or
the correct store, with a fail-*loud* guard.

## Owner's headline worry — DAG-complete parent lifecycle (1-of-3)

- **Close** ✅ works: `processWorkflowFinalize` → `walkSourceBeadChain`
  (`internal/dispatch/runtime.go:711-898`) resolves `gc.source_store_ref` via
  `ProcessOptions.ResolveStoreRef` and closes the `ClassWork` parent in the
  domain store. Test-covered.
- **Progress** ❌ does not exist anywhere — must be **built** (deployed branch is
  close-only too; `copyNonGCMetadata` strips all `gc.*`).
- **Non-blocking ref** ❌ fail-open: `cook --attach`
  (`cmd/gc/cmd_formula.go:915`) writes a work-store `blocks` edge to the
  infra-resident root; dangling target = non-blocking → parent shows READY
  mid-DAG (double-execute) and permanently blocked on `OutcomeFail`.

## Next steps (sequencing — all TDD, integration gated by `GC_FAST_UNIT=0`)

1. **P0 (the gate — nothing else is testable until a formula runs on a split city):**
   - Port the deployed control-ready discovery pattern (landmine #1): supervisor-
     cached `ListReadyBeads` + `internal/beads/control_ready_filter.go` +
     `ErrControllerAPIUnavailable` transient mapping. Source: `fix-main-ci`
     commits `d9a23e6fb`/`3c2173765`/`c2257d206`.
   - Build the composite `claimableStore` + make `gc ready` read it; switch the
     split-city `work_query` to `gc ready` (landmine #2).
   - Tests first: `TestSplitCity_DispatcherDiscoversInfraControlBeads`,
     `TestSplitCity_HookClaimFindsInfraStepBead`.
2. **P1 (lifecycle):** `source_store_ref` no-op→error (#3), bidirectional
   `routes.jsonl` on E2-born cities (#6), then **build** parent progress + DAG-
   complete dep-unblocking (#4, #5), drain membership (#7).
3. **P2/P3:** the remaining reads/links (#8–#16), each "route through the
   composite / correct store" + a fast-unit guard.
4. Land `TestSplitCity_EndToEndFormulaLifecycle` as the standing regression.

See the plan of record for all 16 landmines + the 16-test suite with file:line.

## Gotchas / conventions

- Build cold with `GOCACHE=/tmp/gc-reloc-cache go build ./...`. NEVER
  `go clean -cache`. `go clean -testcache` is fine.
- Integration tests: `GC_FAST_UNIT=0 ... go test -tags integration ./cmd/gc/`;
  use the `setupManagedBdWaitTestCity` harness (real managed Dolt), NOT doltlite.
- Fast suite: `make test-fast-parallel`. Known unrelated flake:
  `internal/eventfeed` `TestMuxSource` (load flake; passes in isolation).
- Commits: `git commit --no-verify` (stale absolute `core.hooksPath` breaks the
  hook here). Trailer: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.
- `bd` is unusable in this repo (schema skew) — track in files/memory, not bd.
- Do NOT push `feat/domain-infra-store-split` yet (WIP). `git push` only (Dolt is
  local-only here).
