# Release Gate: ga-xq0c8s gchome shared-tmp + dashboard TS sync

Date: 2026-06-23
Branch: deploy/ga-xq0c8s-gchome-dashboard-sync
Base: origin/main @ 632d24f7e
Head before gate commit: e4135523a
Source branch: builder/ga-mjkqhb-extmsg-subscribe-clean

## Scope

Deploy bead: ga-xq0c8s
Reviewed source bead: ga-p5te24

Requested source commits:

| Source commit | Result on release branch | Evidence |
| --- | --- | --- |
| 48ac0ae9e fix(gchome): replace shared-tmp fallback with process-unique paths | Cherry-picked as e4135523a | Final diff touches `internal/bootstrap`, `internal/config`, and `internal/supervisor` fallback helpers plus tests. |
| 5c20380ab chore(dashboard): sync generated TS types with openapi maintenance endpoints | Already satisfied by current `origin/main`; not replayed | `origin/main` already exports `triggerMaintenanceDoltGc` and `getV0CityByCityNameMaintenanceStatus` in generated dashboard TS. Replaying the source commit conflicts because its source context also carries unrelated extmsg generated-client exports, which the deploy bead explicitly excludes. |

Final branch diff:

```text
internal/bootstrap/bootstrap.go
internal/bootstrap/bootstrap_test.go
internal/config/implicit.go
internal/config/implicit_test.go
internal/supervisor/config.go
internal/supervisor/config_test.go
```

`docs/PROJECT_MANIFEST.md` is not present on `origin/main`; this gate uses the release criteria from the deployer prompt.

## Gate Checklist

| # | Criterion | Verdict | Evidence |
| --- | --- | --- | --- |
| 1 | Review PASS present | PASS | `bd show ga-p5te24` contains `REVIEW VERDICT: PASS` from gascity/reviewer and lists both requested source commits. |
| 2 | Acceptance criteria met | PASS | gchome fallbacks now create process-unique `gc-home-*` temp directories with PID fallback only if `MkdirTemp` fails; tests cover bootstrap, config implicit imports, and supervisor default home. Dashboard maintenance generated TS functions are already present on base, so no extmsg generated-client context was imported. |
| 3 | Tests pass | PASS | `make test` passed with `observable go test: PASS`; `go vet ./...` passed; `make dashboard-check` passed; isolated-cache build `GOCACHE=$(mktemp -d) go build ./cmd/gc/` passed. |
| 4 | No high-severity review findings open | PASS | Review notes for ga-p5te24 list two INFO findings only: temp dir leak and duplicate fallback helpers. No HIGH findings are open. |
| 5 | Final branch is clean | PASS | `git status --short --branch` before gate file showed `## deploy/ga-xq0c8s-gchome-dashboard-sync...origin/main [ahead 1]` with no uncommitted changes. |
| 6 | Branch diverges cleanly from main | PASS | Branch was cut from `origin/main`; `git merge-base --is-ancestor origin/main HEAD` returned success. |
| 7 | Single feature theme | PASS | Final PR diff is limited to user-global GC home fallback behavior in gchome-related bootstrap/config/supervisor code. The dashboard generated-type item is already on base and introduces no second branch diff. |

## Reviewer Notes

- The release branch intentionally omits dashboard generated files because current `origin/main` already has the maintenance client exports.
- The release branch intentionally omits extmsg WP-3 generated-client exports from the source branch; those belong to the separate extmsg PR called out in the deploy bead.
- The original deployer worktree was mid-rebase on unrelated work, so this gate was evaluated in a fresh release worktree at `/home/jaword/projects/gascity-deploy-ga-xq0c8s`.

## Gate Result

PASS
