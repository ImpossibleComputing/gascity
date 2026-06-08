# Release Gate: Tier C Dolt Prefix Isolation

Bead: `ga-8dw808`
Source review bead: `ga-2yrz75`
PR: https://github.com/gastownhall/gascity/pull/3249
Branch: `fix/tier-c-dolt-prefix-isolation`
Base: `origin/main` at `e2f58a06a81ac28bec5551563c595ba12a375426`
Head evaluated: `ce77502a8d2f16627b514af94848e5125dd8cb6c`

## Summary

PASS. The PR changes one Tier C acceptance-test helper so throwaway rig
directories use short, test-derived names instead of the hardcoded `repo`
directory. That gives the four affected acceptance scenarios distinct Dolt DB
prefixes (`swc`, `pir`, `pl`, `mdp`) and prevents one crashed supervisor from
leaving dirty tables under a prefix reused by a later test.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present | PASS | Source review bead `ga-2yrz75` is closed with `REVIEWER VERDICT: PASS` for implementation commit `e595c7fa9df1b8594a0e147cf87fa85e20da4921`. PR head also includes mayor-authored test commit `ce77502a8`, evaluated by this gate. |
| 2 | Acceptance criteria met | PASS | `setupThrowawayRepo` now uses `uniqueRigName(t.Name())`; `TestUniqueRigName` locks the four target mappings: `TestSwarm_SlingWorkCoderCommits -> swc`, `TestGastown_PolecatImplementsRefineryMerges -> pir`, `TestGastown_PolecatLifecycle -> pl`, `TestGastown_MayorDispatchPipeline -> mdp`. |
| 3 | Tests pass | PASS | See test evidence below. Initial `make test-fast-parallel` run hit a transient failure in unchanged package `internal/runtime/tmux`; the exact failing test passed 10/10, the full package passed, and a second full `make test-fast-parallel` run passed. |
| 4 | No high-severity review findings open | PASS | Review notes list no findings; GitHub PR #3249 has no review comments or review decision findings; unresolved HIGH count: 0. |
| 5 | Final branch is clean | PASS | Deploy worktree was clean before adding this gate file. Final cleanliness verified after committing the gate file before push. |
| 6 | Branch diverges cleanly from main | PASS | `git merge-base --is-ancestor origin/main HEAD` passed and `git merge-tree --write-tree origin/main HEAD` produced a tree without conflicts. |
| 7 | Single feature theme | PASS | Commit set touches only `test/acceptance/tier_c/tierc_test.go` and only the Dolt DB prefix isolation helper/tests for Tier C acceptance tests. |

## Test Evidence

Commands run on `fix/tier-c-dolt-prefix-isolation` at `ce77502a8d2f16627b514af94848e5125dd8cb6c`:

- PASS: `go test -tags acceptance_c ./test/acceptance/tier_c -run 'TestUniqueRigName|TestTierCEnvAuthDoesNotMirrorAuthTokenIntoAPIKey' -count=1`
- PASS: `go vet -tags acceptance_c ./test/acceptance/tier_c`
- PASS: `git diff --check origin/main...HEAD`
- PASS: `go vet ./...`
- PASS after transient recovery: `make test-fast-parallel`

Transient recovery detail:

- Initial `make test-fast-parallel` failed only in `internal/runtime/tmux` on `TestDoStartSession_TreatsDeadlineAfterReadyAsSuccessWhenSessionAlive`, with an extra `acceptStartupDialogs` call. This package is outside the PR diff.
- PASS: `go test ./internal/runtime/tmux -run TestDoStartSession_TreatsDeadlineAfterReadyAsSuccessWhenSessionAlive -count=10`
- PASS: `go test ./internal/runtime/tmux -count=1`
- PASS: second full `make test-fast-parallel`

## Scope

Changed files:

- `test/acceptance/tier_c/tierc_test.go`

No production code, API schema, dashboard, config, or generated artifacts are
changed.
