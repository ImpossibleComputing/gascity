# Release Gate: ga-hxy2j3 Dolt Storage Maintenance runbook re-gate

Date: 2026-06-17
Result: **FAIL**

## Candidate

- Bead: `ga-hxy2j3`
- Title: Deploy: add Dolt Storage Maintenance operator runbook (`ga-3m58sf` re-gate)
- Candidate branch: `builder/ga-84xwd5.1`
- Candidate head: `7c3db2116252f91e0be4f61b9d332f5448da0f62`
- Base checked: `origin/main` at `6da53889e5efafc76ad158582e1e9faf253fa43f`
- Existing overlapping PR: `https://github.com/gastownhall/gascity/pull/3579`

## Decision

No PR was opened.

The candidate head is not a clean reviewed deploy unit. It is the already-open
maintenance retirement PR lineage plus four newer commits:

- `159e75ca7` docs(runbooks): add Dolt Storage Maintenance operator runbook
- `016871fd6` docs(runbooks): fix link format and promote Observability subheadings
- `989b5de45` test(gastown): accept hook-claim polecat startup
- `7c3db2116` chore(packs): bump gastown to 0.1.10, verify gascity at 0.1.6

The docs commits were reviewed through `ga-wexskk` at `016871fd6`, but no
review PASS was found for the current deploy head `7c3db2116` or for the newer
Gastown conformance/pack-pin commits. PM needs to decide whether this should
update/supersede PR #3579, wait until PR #3579 lands and rebase as a clean
runbook deploy, or be routed through review/rollup as one combined release unit.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present | FAIL | `ga-wexskk` has final PASS for `016871fd6`; no review bead or notes were found for current head `7c3db2116`. `bd list --notes-contains 7c3db2116` found only builder bug `ga-5a8oha`, not a review PASS. |
| 2 | Acceptance criteria met | FAIL | Runbook AC were reviewed at `016871fd6`, but the submitted release unit also includes unreviewed Gastown test and pack-pin changes with no acceptance criteria on `ga-hxy2j3`. |
| 3 | Tests pass | FAIL | Not run after criteria 1 and 7 failed. Builder reported fast tests passing, but deployer did not verify because the release unit is not eligible for PR creation. |
| 4 | No high-severity review findings open | PASS | Existing review notes for `ga-wexskk` show only resolved low/style findings. No HIGH findings were found in the reviewed runbook notes. |
| 5 | Final branch is clean | PASS | Worktree was clean before writing this gate artifact. |
| 6 | Branch diverges cleanly from main | PASS | `git merge-tree --write-tree origin/main builder/ga-84xwd5.1` exited 0. |
| 7 | Single feature theme | FAIL | The candidate is not a clean single-bead runbook PR: it overlaps open PR #3579 and adds Gastown pack/test changes beyond the reviewed runbook head. This needs PM sequencing or a reviewed rollup/supersession plan. |

## Follow-up

Route `ga-hxy2j3` to PM with `needs-pm`. The deployer should not push or open a
PR for this head until PM supplies a reviewed, deployable unit.
