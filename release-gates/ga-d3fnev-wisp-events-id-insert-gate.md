# Release Gate: ga-d3fnev - wisp_events id insert fix

Date: 2026-06-14
PR: https://github.com/gastownhall/gascity/pull/3489
Deploy bead: ga-d3fnev
Source review bead: ga-e5vv1h
Branch: builder/ga-xd30uy
Gated head: 652a8facb23ff7b6f8af1aa096318c85ca579e71
Base checked: origin/main @ a2b890dd74cc5e30a6e357a75480514a11657bbf

## Summary

PASS. The branch replaces the problematic `github.com/steveyegge/beads`
v1.0.5 dependency with the pinned pseudo-version
`v1.0.5-0.20260611054652-dc0561af28e9`, whose `RecordEventInTable`
implementation inserts an explicit `id` into `wisp_events`. The branch also
adds an integration regression test for ephemeral native Dolt bead creation and
test-only HOME pinning needed to keep city discovery hermetic in CI.

`docs/PROJECT_MANIFEST.md` was not present in this checkout, so the deployer
gate used the release criteria in the active deployer prompt.

## Gate Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present | PASS | `bd show ga-e5vv1h` records `Reviewer verdict: PASS` with category `auto-merge`, high confidence. Deploy bead `ga-d3fnev` was routed from `gascity/reviewer` with reviewed PASS status. |
| 2 | Acceptance criteria met | PASS | `go.mod` replaces beads v1.0.5 with pseudo-version `dc0561af28e9`; module cache inspection confirmed the pseudo-version's `RecordEventInTable` calls `NewEventID()` and inserts the `id` column. `internal/beads/native_dolt_store_integration_test.go` adds `TestNativeDoltStoreEphemeralCreate`, which exercises `Create(Bead{Ephemeral: true})` through `wisp_events`. `scripts/check-native-dependency-surface.sh` raises the module threshold from 725 to 726, matching the replace-induced module count. |
| 3 | Tests pass | PASS | `go test -tags integration ./internal/beads -run TestNativeDoltStoreEphemeralCreate -count=1` passed. `bash scripts/check-native-dependency-surface.sh` passed with `modules=726 aws=25 azure=9 dolthub=14 googleapi=1 binary_bytes=246478008`. `make test-fast-parallel` passed all 8 fast jobs. `go build ./...` passed. `go vet ./...` passed. |
| 4 | No high-severity review findings open | PASS | Reviewer notes in `ga-e5vv1h` list no high-severity findings and mark the PR auto-merge/high confidence. The only hold note is an architectural-significance path-match false positive for a test-only `cmd/gc/cmd_supervisor_test.go` HOME pin; no production architectural contract changed. |
| 5 | Final branch is clean | PASS | Clean temporary worktree was created at `/tmp/gascity-deploy-ga-d3fnev.BnKsLy`; `git status --short --branch` showed `## builder/ga-xd30uy...origin/builder/ga-xd30uy` before adding this gate file. After this file is committed, the final branch should be clean before push. |
| 6 | Branch diverges cleanly from main | PASS | GitHub reports PR #3489 `mergeable=MERGEABLE` and `mergeStateStatus=CLEAN`. Local `git merge-tree --write-tree HEAD origin/main` exited successfully and produced tree `39f6580e4b25c9a48406ee3c46fbe0f5d0c40d35`. |
| 7 | Single feature theme | PASS | The production change is one beads dependency pin to restore `wisp_events` writes after the upstream schema/code mismatch. The integration regression test, dependency-surface threshold bump, and test-only HOME pinning are release support for that same dependency update, not independent user-facing features. |

## Diff Surface

- `go.mod`, `go.sum`
- `internal/beads/native_dolt_store_integration_test.go`
- `scripts/check-native-dependency-surface.sh`
- Test-only HOME isolation in `cmd/gc/*_test.go`

## Notes For Merge Authority

The deploy bead notes mention an architectural-significance hard-hold on PR
#3489. The current PR labels observed during this gate were `status/needs-triage`
and `status/reviewing`; no hold label was visible through `gh pr view`. The
deployer did not clear any hold and will not merge. Merge authority should clear
or confirm the hold state before merging if the maintainer-review system still
tracks it internally.
