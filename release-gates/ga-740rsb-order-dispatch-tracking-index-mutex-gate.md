# Release Gate: ga-740rsb-order-dispatch-tracking-index-mutex

Generated: 2026-06-09T04:36:14Z

## Scope

- Deploy bead: `ga-740rsb` - needs-deploy: fix(order_dispatch) concurrent map writes
- Source review bead: `ga-bwbkuy` - Review: fix(order_dispatch) concurrent map writes in orderDispatchTrackingIndex
- Branch: `builder/ga-n8wri3`
- Reviewed head: `e50515b7d575ae6881eb841809df23086fa8b000`
- Base checked: `origin/main` at `4e8a6a04cf06ad28c8f6cd2e0edad8917402718b`
- Pull request: https://github.com/gastownhall/gascity/pull/3264

`docs/PROJECT_MANIFEST.md` is not present in this repository at this branch. This gate applies the deployer release-gate criteria plus the acceptance evidence from `ga-bwbkuy`.

## Gate Criteria

| # | Criterion | Status | Evidence |
|---|---|---|---|
| 1 | Review PASS present | PASS | `ga-bwbkuy` is closed with `VERDICT: PASS` from `gascity/reviewer`; `ga-740rsb` was created from that review and routed to deployer. |
| 2 | Acceptance criteria met | PASS | `orderDispatchTrackingIndex` now has a `sync.Mutex`; both cache-population paths, `historyEntriesForStore` and `entriesForStore`, lock around map reads and writes. This directly addresses the reviewed `fatal error: concurrent map writes` failure mode. |
| 3 | Tests pass | PASS | `make test-fast-parallel` passed all fast jobs; `go vet ./...` passed; focused race smoke `go test -race -run "TestOrderDispatch|TestGateOpenWork|TestCityRuntimeTick|TestRunControlDispatcher" ./cmd/gc/ -count=5` passed. |
| 4 | No high-severity review findings open | PASS | Reviewer reported two LOW/NOTE items only: lock held during beads I/O and no dedicated concurrent test. No HIGH findings or request-changes items are open. |
| 5 | Final branch is clean | PASS | `git status --short --branch` returned only `## HEAD (no branch)` before writing this gate file. |
| 6 | Branch diverges cleanly from main | PASS | `git merge-tree --write-tree origin/main HEAD` exited 0 and produced tree `88ec282f5ef19dcf44d2b4397448055810dd7bd6`. GitHub reports PR #3264 merge state `CLEAN`. |
| 7 | Single feature theme | PASS | `git diff --name-only origin/main...HEAD` lists only `cmd/gc/order_dispatch.go`; the commit set is one order-dispatch concurrency fix. |

## Acceptance Trace

| Acceptance item | Status | Evidence |
|---|---|---|
| Guard shared tracking-index maps from concurrent writers. | PASS | `cmd/gc/order_dispatch.go` adds `mu sync.Mutex` to `orderDispatchTrackingIndex`. |
| Protect both lazy cache paths that write `entries` and `errs`. | PASS | `historyEntriesForStore` and `entriesForStore` both call `idx.mu.Lock()` with `defer idx.mu.Unlock()` before checking, populating, or recording errors in the maps. |
| Preserve existing lookup semantics. | PASS | The change serializes the existing cache-check and populate cycle only; no dispatch decisions, bead query filters, or returned summary shapes changed. |
| Avoid new role behavior. | PASS | The diff is confined to `cmd/gc/order_dispatch.go` synchronization; it adds no role names or user-supplied behavior assumptions. |

## Test Evidence

| Command | Result |
|---|---|
| `make test-fast-parallel` | PASS: all fast jobs passed. |
| `go vet ./...` | PASS. |
| `go test -race -run "TestOrderDispatch\|TestGateOpenWork\|TestCityRuntimeTick\|TestRunControlDispatcher" ./cmd/gc/ -count=5` | PASS: `ok github.com/gastownhall/gascity/cmd/gc 74.386s`. |
| `git merge-tree --write-tree origin/main HEAD` | PASS. |

## Notes

- PR #3264 already existed before deployer evaluation. This gate updates the existing PR branch rather than opening a duplicate PR.
- GitHub CI on PR #3264 was green at deployer evaluation time, including `cmd/gc process / shard 5 of 12`, the shard called out in the review notes as previously crashing.
