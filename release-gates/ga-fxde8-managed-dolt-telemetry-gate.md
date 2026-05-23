# Release Gate: Managed Dolt Telemetry Suppression

Date: 2026-05-23
Deployer: gascity/deployer
Deploy bead: ga-fxde8
Source bead: ga-um72h.3
Branch: builder/ga-um72h-3
Head: 125d1c2f7 (`fix(provider): persist managed dolt metrics opt-out`)

## Scope

This branch ships the managed Dolt telemetry suppression stack:

| Bead | Review bead | Commit | Status | Review evidence |
| --- | --- | --- | --- | --- |
| ga-um72h.2 | ga-n7qr3 | 493c3c233 | closed | `VERDICT: pass`; no blocking findings |
| ga-um72h.3 | ga-fxde8 | 125d1c2f7 | closed source, open deploy bead | `Review verdict: PASS`; no blockers |

`docs/PROJECT_MANIFEST.md` is not present in this worktree, so this gate applies the release criteria supplied by the deployer role prompt and the repository `AGENTS.md`.

## Gate Checklist

| # | Criterion | Result | Evidence |
| --- | --- | --- | --- |
| 1 | Review PASS present | PASS | Current deploy bead `ga-fxde8` notes include `Review verdict: PASS` from `gascity/reviewer`. Stack parent review bead `ga-n7qr3` is closed and notes `VERDICT: pass`. |
| 2 | Acceptance criteria met | PASS | `ensureBeadsProvider` calls `ensureManagedDoltMetricsDisabled()` only inside the managed bd provider block before provider startup. `ensureManagedDoltMetricsDisabled` runs `dolt config --global --add metrics.disabled true` with a lifecycle timeout and logs failures without returning an error. `providerLifecycleProcessEnvFromBase` strips inherited `DOLT_DISABLE_EVENT_FLUSH` and appends `DOLT_DISABLE_EVENT_FLUSH=1` inside the existing `providerUsesBdStoreContract` guard. File/external providers remain outside that guard. |
| 3 | Tests pass | PASS | `go test ./cmd/gc -run 'TestProviderLifecycleProcessEnv.*DoltDisableEventFlush\|TestEnsureBeadsProvider.*DoltConfig\|TestGcBeadsBdStartFallsBack' -count=1` passed. `make test-fast-parallel` passed all 8 fast jobs. `go vet ./...` completed cleanly. `git diff --check origin/main..HEAD` completed cleanly. |
| 4 | No high-severity review findings open | PASS | `ga-fxde8` review notes: "No blockers. Clean implementation." Parent review `ga-n7qr3`: "No blocking issues found." No unresolved HIGH findings are recorded in either review. |
| 5 | Final branch is clean | PASS | Before writing this gate, `git status --short --branch` was clean on `builder/ga-um72h-3` tracking `fork/builder/ga-um72h-3`. This checklist is the only deployer-added file and is committed as the release-gate commit. |
| 6 | Branch diverges cleanly from main | PASS | `git merge-tree $(git merge-base HEAD origin/main) HEAD origin/main` reported `no_conflict`. `origin/main..builder/ga-um72h-3` contains the two reviewed stack commits listed above. |

## Reviewer Notes

- Behavior is limited to managed bd provider processes; external/file providers are intentionally unchanged.
- The metrics opt-out is best-effort. Failure to run `dolt config` is logged with the existing `gc:` log prefix and does not block startup.
- The PR includes both stacked commits because the parent telemetry-flush suppression commit is not yet on `origin/main`.
