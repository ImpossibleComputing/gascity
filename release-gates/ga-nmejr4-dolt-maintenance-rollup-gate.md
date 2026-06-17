# Release Gate: ga-nmejr4 Dolt maintenance rollup for PR #3579

Date: 2026-06-17
Result: **PASS**

## Candidate

- Deploy bead: `ga-nmejr4`
- PM sequencing bead: `ga-hxy2j3.2`
- Review bead: `ga-hxy2j3.1`
- Existing PR: `https://github.com/gastownhall/gascity/pull/3579`
- PR branch: `deploy/ga-wjms2g-retire-maintenance-dolt`
- Base checked: `origin/main` at `6da53889e5efafc76ad158582e1e9faf253fa43f`
- Reviewed source head: `builder/ga-84xwd5.1` at `7c3db2116252f91e0be4f61b9d332f5448da0f62`
- PR branch head before this gate artifact: `f0c8d09357efc5a1f6fb5eb00a8c01050c4d7038`

## Rollup Scope

This gate updates the already-open PR #3579 instead of opening a duplicate PR.
The PR branch preserves the prior release-gate commit and cherry-picks the
additional reviewer-approved rollup commits on top.

| Scope | Reviewed source SHA | PR branch SHA | Evidence |
|---|---:|---:|---|
| Add Dolt Storage Maintenance runbook | `159e75ca7` | `7a1daf82c` | `docs/runbooks/dolt-compact.md`, docs nav, and bloat-recovery cross-link added. |
| Fix runbook link/style findings | `016871fd6` | `71c6944ac` | Root-relative docs links and Observability subheadings. |
| Accept hook-claim polecat startup in Gastown conformance | `989b5de45` | `81a2137cb` | Test-only update for the Gastown 0.1.10 startup protocol. |
| Refresh bundled Gastown/Gas City pack pins | `7c3db2116` | `f0c8d0935` | Pack pins, lockfile, docs recipe, and `public_packs.go` updated. |

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present | PASS | `ga-hxy2j3.1` records `REVIEWER VERDICT: PASS` for `builder/ga-84xwd5.1` at `7c3db2116252f91e0be4f61b9d332f5448da0f62`, covering PR #3579 lineage plus the runbook, Gastown test, and pack-pin commits. |
| 2 | Acceptance criteria met | PASS | Reviewer accepted the combined scope as one release unit. The runbook docs are present, the Gastown conformance update is test-only, and `scripts/update-bundled-gastown-pack --check` passed with Gastown `0.1.10` at `33d3a430a67d`. |
| 3 | Tests pass | PASS | `make check-docs`, `make dashboard-check`, dashboard preview smoke on `127.0.0.1:4187`, `go build ./...`, `go vet ./...`, and `make test-fast-parallel` all passed on the final PR branch. |
| 4 | No high-severity review findings open | PASS | `ga-hxy2j3.1` records `High-severity findings: NONE`. |
| 5 | Final branch is clean | PASS | `git status --porcelain=v1` was empty before writing this gate artifact. |
| 6 | Branch diverges cleanly from main | PASS | `git merge-tree --write-tree origin/main HEAD` exited 0. |
| 7 | Single feature theme | PASS | Reviewer explicitly accepted this as one Dolt maintenance release unit: maintenance retirement plus compact-backed replacement behavior, operator runbook, and mechanical pack/test freshness needed for the branch gate. |

## Check Log

- `scripts/update-bundled-gastown-pack --check`:
  `gastown pins match registry release 0.1.10 (33d3a430a67d)`
- `make check-docs`: PASS
- `make dashboard-check`: PASS
- Dashboard preview smoke: PASS (`curl -fsS http://127.0.0.1:4187/`)
- `go build ./...`: PASS
- `go vet ./...`: PASS
- `make test-fast-parallel`: PASS (`All fast jobs passed`)

## Outcome

Gate PASS. Update PR #3579 in place; do not open a duplicate PR.
