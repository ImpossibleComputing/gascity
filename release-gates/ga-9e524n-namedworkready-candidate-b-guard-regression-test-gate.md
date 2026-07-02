# Release Gate: namedWorkReady Candidate-B guard regression test

Bead: `ga-9e524n`  
Source review bead: `ga-mojprz`  
Branch: `builder/ga-7x9khs-namedworkready-nocanonical-test`  
Reviewed commit: `5de45c92488a5ee40d847faaf5ce1398e3e0fd57`

## Summary

This deploy bead adds one regression test for the namedWorkReady Candidate-B
guard behavior on expanded-identity named sessions when the canonical session
bead is absent. The branch is stacked on `builder/ga-n2szjj`, which is open as
PR #3865 and must land before this test-only change can be reviewed as its own
clean PR against `main`.

## Gate Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present | PASS | `bd show ga-mojprz` contains `REVIEW: PASS (gascity/reviewer)` and closes with PASS. |
| 2 | Acceptance criteria met | PASS | The source review confirms the new test was added, is self-contained, matches sibling test style, passes, and its doc comment references `ga-tpe9od`. |
| 3 | Tests pass | PASS | `make test-fast-parallel` passed: all fast jobs passed. `go vet ./...` passed with no output. |
| 4 | No high-severity review findings open | PASS | Review notes on `ga-mojprz` contain no HIGH findings or blocking issues; reviewer called out only a non-blocking metadata hygiene note. |
| 5 | Final branch is clean | PASS | Branch was clean before this gate file was written; final clean state is expected after committing this file. |
| 6 | Branch diverges cleanly from main | PASS | `git merge-tree --write-tree origin/main origin/builder/ga-7x9khs-namedworkready-nocanonical-test` exited 0 and produced tree `b4f64e3e931f0168c5cbd4efa9e0e43f7a53f6ab`. |
| 7 | Single feature theme | PASS | PR-style diff from `origin/main...branch` touches `cmd/gc/build_desired_state.go`, `cmd/gc/build_desired_state_ga80pen8_test.go`, and the prerequisite gate file; the branch delta over `origin/builder/ga-n2szjj` is only the reviewed regression test. |

## Sequencing Hold

PR #3865 (`builder/ga-n2szjj` -> `main`) is still open. No PR should be opened
for this deploy bead until the merge authority lands or otherwise sequences
that prerequisite, because this branch currently includes the prerequisite's
implementation and gate commits in addition to the reviewed test commit.

Current prerequisite status checked during this gate:

- PR: <https://github.com/gastownhall/gascity/pull/3865>
- State: open
- Head: `builder/ga-n2szjj@b3c9a36c459af1d6563b6e250e1aff38f78ae0ba`
- Checks: GitHub status rollup reported SUCCESS/SKIPPED checks.

## Commands Run

```text
gc hook gascity/deployer
bd show ga-9e524n
bd show ga-mojprz
gh pr view 3865 --json number,state,mergedAt,mergeStateStatus,mergeable,headRefName,headRefOid,baseRefName,url,title,author,reviewDecision,statusCheckRollup
git fetch origin main builder/ga-7x9khs-namedworkready-nocanonical-test builder/ga-n2szjj
git diff --stat origin/main...origin/builder/ga-7x9khs-namedworkready-nocanonical-test
git diff --name-status origin/main...origin/builder/ga-7x9khs-namedworkready-nocanonical-test
git merge-tree --write-tree origin/main origin/builder/ga-7x9khs-namedworkready-nocanonical-test
make test-fast-parallel
go vet ./...
```
