# Release Gate: ga-ab92m9 bd probe timeout

Feature branch: `builder/ga-e4oku0-bd-probe-timeout`
Reviewed head: `fe8c2b6dc104211f8e31673a58668d4a02c1c48f`
Base: `origin/main` at `ca35aa9fb75931cbf3cf4b571f39e3643c057ae3`
Deploy bead: `ga-ab92m9`
Source bead: `ga-e4oku0`
Review bead: `ga-7je8qa`

## Summary

This release makes the `cmd/gc` pool `bd` probe timeout configurable with
`GC_BD_PROBE_TIMEOUT`. Production keeps the 180s default, invalid values fall
back to 180s, and values below 5s are raised to a 5s floor. Review-formula
integration cities set `GC_BD_PROBE_TIMEOUT=30s` so test subprocesses are less
likely to stall for the full production timeout under CI load.

## Gate Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present | PASS | `ga-7je8qa` is closed with `REVIEW PASS (gascity/reviewer, 2026-06-17)` and verdict `PASS`. |
| 2 | Acceptance criteria met | PASS | `cmd/gc/pool.go` reads `GC_BD_PROBE_TIMEOUT`; empty/invalid input returns 180s; durations below 5s are floored to 5s; `test/integration/review_formula_test.go` injects `GC_BD_PROBE_TIMEOUT=30s`; `cmd/gc/pool_test.go` covers 8 parser cases. |
| 3 | Tests pass | PASS | `make test-fast-parallel` passed; `go vet ./...` passed; `go build ./cmd/gc/` passed; `go test ./cmd/gc -run TestParseBdProbeTimeout -count=1` passed; `make test-integration-review-formulas-basic` passed with `ok github.com/gastownhall/gascity/test/integration 286.255s`. |
| 4 | No high-severity review findings open | PASS | Reviewer notes report style clean, security no concerns, coverage complete, and no follow-up findings. No HIGH findings are listed in the deploy or review bead notes. |
| 5 | Final branch is clean | PASS | Candidate checkout was clean before writing this gate file (`git status --short --branch` showed only detached HEAD state). This gate file is the only deployer change and is committed as the branch tip before push. |
| 6 | Branch diverges cleanly from main | PASS | `git merge-tree --write-tree origin/main fe8c2b6dc104211f8e31673a58668d4a02c1c48f` exited 0 and produced tree `0f62c46ec8077b13d5f3650e21669069800e91bd`. |
| 7 | Single feature theme | PASS | Diff is limited to `cmd/gc/pool.go`, `cmd/gc/pool_test.go`, and review-formula test env injection for the same timeout knob. One commit: `fe8c2b6dc feat(pool): make bdProbeTimeout configurable via GC_BD_PROBE_TIMEOUT`. |

## Diff Scope

```text
cmd/gc/pool.go                          | 24 ++++++++++++++++++++++--
cmd/gc/pool_test.go                     | 23 +++++++++++++++++++++++
test/integration/review_formula_test.go |  1 +
3 files changed, 46 insertions(+), 2 deletions(-)
```

Gate verdict: PASS.
