# Release Gate: readyAssignedByBeadID Fail-Closed Index Mismatch

- Deploy bead: `ga-5xbdko`
- Review bead: `ga-3f12y2`
- Implementation bead: `ga-frpt4k`
- Reviewed commit: `a7f3d5fc42daf321ebf0e2f3502821d77974e02c`
- Evaluated branch: `deploy/ga-5xbdko-readyassigned-failclosed`
- Parent dependency: PR #3860 for `fda233f2d49d7c7e756a1f037038a087394386f4`
- PR: HOLD until PR #3860 merges
- Gate date: 2026-07-01

## Scope

This is a single follow-up fix on top of the already reviewed pool resume
readiness-gate work in PR #3860. The deployable delta for this bead is limited
to:

- `cmd/gc/assigned_work_scope.go`
- `cmd/gc/compute_awake_bridge_test.go`

Against `origin/main`, the branch also contains the parent PR #3860 commit.
Opening a PR before #3860 merges would duplicate the parent diff into a second
open PR, so the release action is held even though the gate criteria pass.

## Checklist

| # | Criterion | Verdict | Evidence |
|---|-----------|---------|----------|
| 1 | Review PASS present | PASS | `ga-3f12y2` records `REVIEW VERDICT (gascity/reviewer, 2026-07-01): PASS` for `a7f3d5fc42daf321ebf0e2f3502821d77974e02c`. |
| 2 | Acceptance criteria met | PASS | `readyAssignedByBeadID` now resolves an out-of-range `storeRefs` index directly to `false` and skips the empty-store-ref lookup, matching `readyAssignedFlagsForBeads`. `TestReadyAssignedByBeadIDFailsClosedOnIndexMismatch` covers the previous false-positive shape. |
| 3 | Tests pass | PASS | Focused `cmd/gc` regression suite passed, `go vet ./cmd/gc/...` passed, `go vet ./...` passed, and `make test-fast-parallel` passed all 8 fast jobs. |
| 4 | No high-severity review findings open | PASS | Review notes record no OWASP-relevant surface and no blocking findings. The only referenced finding was the non-blocking style hardening that this bead fixes. |
| 5 | Final branch is clean | PASS | `git status --short --branch` was clean on `deploy/ga-5xbdko-readyassigned-failclosed` before adding this gate file. |
| 6 | Branch diverges cleanly from main | PASS | `git merge-tree --write-tree origin/main HEAD` completed without conflicts against `origin/main` at `c02b3be84` and produced tree `ca1d39e6e25f55612cab5fd74483711c0e2f139d`. |
| 7 | Single feature theme | PASS | The bead delta touches one `cmd/gc` pool-readiness subsystem. The full branch contains the parent pool-readiness PR plus this defensive follow-up; there are no independent feature themes. |

## Local Test Details

Passed:

```text
go test ./cmd/gc/ -run 'TestReadyAssignedByBeadID|TestReadyAssignedFlagsForBeads|TestWorkBeadResumeReady|TestComputePoolDesiredStates' -count=1
ok  	github.com/gastownhall/gascity/cmd/gc	0.350s
```

Passed:

```text
go vet ./cmd/gc/...
go vet ./...
```

Passed:

```text
make test-fast-parallel
[fsys-darwin-compile] ok
[unit-cmd-gc-1-of-6] ok
[unit-cmd-gc-2-of-6] ok
[unit-cmd-gc-3-of-6] ok
[unit-cmd-gc-4-of-6] ok
[unit-cmd-gc-5-of-6] ok
[unit-cmd-gc-6-of-6] ok
[unit-core] ok
All fast jobs passed
```

## Release Action

Gate verdict: PASS.

Release action: HOLD. PR #3860 is still open and unmerged as of this gate. Once
PR #3860 lands on `main`, re-cut or rebase the follow-up branch so the PR shows
only this bead's delta plus this gate file, then open the follow-up PR and route
the merge request to mayor. Do not merge from the deployer seat.
