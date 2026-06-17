# Release Gate: ga-3m58sf Dolt Storage Maintenance Runbook

Result: FAIL
Date: 2026-06-17

## Candidate

- Deploy bead: ga-3m58sf
- Source review bead: ga-wexskk
- Source work bead: ga-gx25ij.1
- Reviewed head: 016871fd69f2c6b131154f0cdf51bf761aa0040a
- Candidate branch: deploy/ga-3m58sf-dolt-storage-runbook
- Builder branch: builder/ga-84xwd5.1
- Base checked: origin/main at 6da53889e5efafc76ad158582e1e9faf253fa43f
- Merge base: 70519347fde7944f1aafb0e6792b64a6b24d34a8

## Summary

The reviewed docs changes are correct, but the release gate fails on the
final candidate branch. The exact pack compatibility check used by CI fails
because bundled Gastown/Gas City pack pins are stale against the registry.
No PR was opened.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present | PASS | ga-wexskk notes contain `REVIEW VERDICT (FINAL): PASS` for 016871fd6. |
| 2 | Acceptance criteria met | PASS | Inspected `docs/runbooks/dolt-compact.md`, `docs/troubleshooting/dolt-bloat-recovery.md`, and `docs/docs.json` at 016871fd6. The runbook is present, nav is wired, links are root-relative, and the reviewer-requested fixes are included. |
| 3 | Tests pass | FAIL | `scripts/update-bundled-gastown-pack --check` failed. `make test-acceptance` also failed locally before completion on an ambient `/tmp/.beads/embeddeddolt/beads` initialization collision. |
| 4 | No high-severity review findings open | PASS | Review notes show both findings resolved and no high-severity findings. |
| 5 | Final branch is clean | PASS | Working tree was clean before writing this gate artifact. |
| 6 | Branch diverges cleanly from main | PASS | `git merge-tree $(git merge-base origin/main 016871fd6) origin/main 016871fd6` exited 0. |
| 7 | Single feature theme | PASS | The branch is a stacked Dolt storage-maintenance release theme; the runbook depends on the maintenance-retirement/compact stack and is not an unrelated feature. |

## Command Evidence

Passed:

```text
make check-docs
go build ./cmd/gc/
go vet ./...
make dashboard-check
```

Failed:

```text
scripts/update-bundled-gastown-pack --check
```

Key failure lines:

```text
PublicGastownPackVersion = 'sha:4212acb7046c11f6f633df73307006493185233a', want 'sha:33d3a430a67d1782ad364556cb566bdb01d0afe3'
pinned module gastown hash = sha256:ce2736669b4c737ff0f08264e097662e6a58291356a7014634e2097b41893142, want sha256:5a83996226e3154f29fc1bca13ff0e8b37a1a02cee8f71fb70bddcdcea2e6895
PublicGascityPackVersion = 'sha:99464ed9240b1f6e6b7ab1d351f67016e1a973ff', want 'sha:3b3b89f2011e06d84459aa7bea1552382f13930a'
pinned module gascity hash = sha256:12cd31ffae96b1dbef8e7e16a84903b47fc49098abb2fe8a1a7df62d6e042b6e, want registry sha256:149772065f9f2862965146e74d853d17e432628f57d25a4386bbef0fb6744e33
```

`make test-acceptance` local failure:

```text
TestBdBasicCRUD, TestBdDependencies, TestBdDestructive, and TestBdWorkflow failed:
Found existing Dolt database: /tmp/.beads/embeddeddolt/beads
```

This acceptance failure is likely host-state related, but the pack pin check is
deterministic and matches the open PR's failing CI surface.

## Disposition

Gate FAIL. Route ga-3m58sf back to builder to refresh the branch against the
current pack registry pins and re-run the release gate. No push and no PR.
