# Release Gate: partial-demand runaway fix

Bead: `ga-zu2rsu`  
Source fix: `ga-01yukx` Changes 1-3  
Branch: `builder/ga-01yukx-partial-demand-fix`  
Reviewed commit: `8f032557bb04cacf2b1d56033adb569107faa1c8`  
Base: `origin/main` at `32ca47acd639b80eee37f4623d0277018b674c06`

Note: `docs/PROJECT_MANIFEST.md` is not present in this checkout. This gate uses the deployer role's release criteria and the repo's `TESTING.md`/Makefile gates.

## Gate Result

PASS.

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present | PASS | Review bead `ga-5h3l4j` is closed with `Reviewer verdict: PASS` for commits `73f15a956` and `8f032557b`. Reviewer recorded no blockers and no security findings. |
| 2 | Acceptance criteria met | PASS | FR-01: `selectOrPlanPoolSessionBead` blocks fresh creates when `poolScaleCheckPartialTemplates[template]` is true, after reuse/resume paths. FR-02: active/awake pool sessions are retained. FR-03: creating/start-pending sessions are preserved by the desired overlay but no longer counted as wake demand. FR-04/Change 3: partial overlay uses `scaleCheckPartialSessionPreservable`, so draining/drained/archived sessions are not revived. NFR-01/NFR-03: behavior is centralized in desired-state build logic and applies to every pool template without config. NFR-02: resume requests with `SessionBeadID` follow the reuse path before the create guard. |
| 3 | Tests pass | PASS | Focused regression: `go test ./cmd/gc -run 'TestBuildDesiredState_ScaleCheckPartialPoolBlocksNewCreates|TestCityRuntimeBeadReconcileTick_ScaleCheckPartialKeepsOnlyAffectedPoolSession|TestCityRuntimeBeadReconcileTick_ScaleCheckPartialPreservesDormantAffectedPoolSessionWithoutDrain|TestBuildDesiredState_NamedBackedPoolPartialRetainsGenericPoolSession|TestRetainScaleCheckPartialPoolDesiredNormalizesLegacyBoundTemplate'` passed. First `make test-fast-parallel` run failed in unrelated tests (`examples/gastown`, `internal/beads`, `internal/eventfeed`); each exact failing test passed on isolated rerun. Second full `make test-fast-parallel` passed all shards. `go vet ./...` passed. `go build ./cmd/gc` passed. |
| 4 | No high-severity review findings open | PASS | Review notes for `ga-5h3l4j` report no blockers, no security findings, and only one non-blocking INFO comment wording observation. |
| 5 | Final branch is clean | PASS | Gate worktree was clean before this file was added; generated `gc` build artifact was removed. Deployer verifies `git status --short --branch` is clean after the gate commit and before push. |
| 6 | Branch diverges cleanly from main | PASS | `git merge-base origin/main fork/builder/ga-01yukx-partial-demand-fix` returned `32ca47acd639b80eee37f4623d0277018b674c06`; `git merge-tree $(git merge-base 8f032557b origin/main) 8f032557b origin/main` produced no conflicts. |
| 7 | Single feature theme | PASS | Commit set touches one subsystem and one behavior: `cmd/gc` supervisor desired-state handling for pool partial demand reads. Changed files are `cmd/gc/agent_build_params.go`, `cmd/gc/build_desired_state.go`, and `cmd/gc/build_desired_state_test.go`. |

## Test Log Notes

- Initial fast-suite log directory: `/tmp/gc-local-tests.ggMhCv`
- Initial transient failures:
  - `go test ./examples/gastown -run TestReaperClosesGraphWorkflowWispTrackedToClosedRoot`
  - `go test ./internal/beads -run TestExecCommandRunnerStopsBDSlowTimerForFastBDCommand`
  - `go test ./internal/eventfeed -run TestMuxSource_YieldsAndPicksUpNewCity`
- Immediate isolated reruns of all three tests passed.
- Full `make test-fast-parallel` rerun passed.
