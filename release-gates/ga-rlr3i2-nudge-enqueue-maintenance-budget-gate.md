# Release gate: ga-rlr3i2 nudge enqueue maintenance budget

Overall result: FAIL

Bead: ga-rlr3i2
Source review bead: ga-nr4996
Reviewed commit: 93be1b652bc422fd6c921856ee11773973d5f170
Reviewed branch: builder/ga-1k4paf-nudge-enqueue-budget
Base checked: origin/main at b234f6497
Gate date: 2026-07-03

Note: `docs/PROJECT_MANIFEST.md` is not present in this checkout, and
`rg --files -g '*PROJECT_MANIFEST*' -g '*MANIFEST*'` found no manifest file.
This gate uses the deployer release criteria from the active rig prompt.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present | PASS | `bd show ga-nr4996` is closed with close reason `pass` and notes contain `REVIEW VERDICT: PASS` for commit `93be1b652`. `bd show ga-rlr3i2` also records reviewed + passed by reviewer. |
| 2 | Acceptance criteria met | PASS for reviewed commit | The reviewed commit touches only `cmd/gc/cmd_nudge.go`, `cmd/gc/cmd_nudge_test.go`, and `cmd/gc/sling_nudge_backlog_test.go`. Local symbol check confirms `nudgeEnqueueMaintenanceBudget`, `noMaintenanceDeadline`, deadline checks in the foreground enqueue maintenance path, and `TestSlingNudgeEnqueueBoundedByBacklog` are present. Reviewer evidence records `go test ./cmd/gc/ -run Nudge -v`, `go test ./internal/nudgequeue/...`, `go vet ./...`, and `gofmt -l` clean on the reviewed commit. |
| 3 | Tests pass | FAIL | Not run on a final branch because the release branch fails mergeability and theme checks below. Running tests on this stale/conflicting branch would not validate the PR candidate. |
| 4 | No high-severity review findings open | PASS | Review notes for ga-nr4996 state "No blocking findings" and identify only follow-up test coverage beads outside the deploy scope. No HIGH findings are recorded in the deploy bead. |
| 5 | Final branch is clean | FAIL | `git status --short --branch` in the checked feature worktree shows untracked files, including `.beads/`, `.claude/skills/...`, and `cmd/gc/sling_nudge_budget_test.go`. The branch is not in a clean release state. |
| 6 | Branch diverges cleanly from main | FAIL | After `git fetch origin main`, `git merge-tree --write-tree origin/main HEAD` exits nonzero with content conflicts in `cmd/gc/cmd_nudge.go` and `cmd/gc/cmd_nudge_test.go`. |
| 7 | Single feature theme | FAIL | `git log --oneline origin/main..HEAD` shows the reviewed nudge commit stacked on unrelated Dolt maintenance/runbook commits (`ga-3pg1y6.2`, `ga-qc5chv.2`, `ga-wfdunn.1`, `ga-hn88dw.1`, `ga-uz6mr1`, `ga-xxp9cd.2`, `ga-gx25ij.1`, and related formatting/revert commits). A single-bead deploy for nudge enqueue maintenance would bundle unrelated feature themes. |

## Decision

Do not push or open a PR.

Route ga-rlr3i2 back for branch isolation/rebase before deploy. The reviewed
nudge commit needs a clean branch off current `origin/main` with only the
nudge enqueue maintenance change and no merge conflicts.
