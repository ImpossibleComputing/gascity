# Release Gate: Postgres Auth Doctor Observability

- Deploy/review bead: `ga-yjcmd`
- Source bead: `ga-yih2`
- Rebase/deploy prerequisite bead: `ga-l5qqp`
- Branch: `builder/ga-yih2-2`
- Remote branch: `fork/builder/ga-yih2-2`
- Reviewed commit: `0998821b3 feat(doctor): add postgres auth observability`
- Note: `docs/PROJECT_MANIFEST.md` is not present in this worktree, so this gate uses the deployer role criteria and the source bead acceptance checklist.

## Gate Criteria

| # | Criterion | Result | Evidence |
|---|---|---|---|
| 1 | Review PASS present | PASS | `bd show ga-yjcmd` shows `Review verdict: PASS` for commit `0998821b300142a579cfd93292cb7102189043e0`. |
| 2 | Acceptance criteria met | PASS | Checked the source bead criteria against the code and tests; details below. |
| 3 | Tests pass | PASS | `make dashboard-check`, `make test-fast-parallel`, `go vet ./...`, and `git diff --check origin/main...HEAD` all exited 0. |
| 4 | No high-severity review findings open | PASS | Review notes list two LOW/INFO findings and explicitly state no blocking findings; unresolved HIGH findings count is 0. |
| 5 | Final branch is clean | PASS | Branch was clean before gate creation; deployer will recheck clean status after committing this gate before push. |
| 6 | Branch diverges cleanly from main | PASS | `git merge-base --is-ancestor origin/main HEAD` exited 0; `git merge-tree $(git merge-base HEAD origin/main) HEAD origin/main` reported no conflicts. |

## Acceptance Evidence

| Source criterion | Result | Evidence |
|---|---|---|
| Doctor check exists with the required status branches and no auto-fix. | PASS | `internal/doctor/postgres_auth.go` adds `PostgresAuthCheck`, five explicit credential-resolution branches, and `CanFix() == false`; `internal/doctor/postgres_auth_test.go` covers branch messages and hints. The source bead's older `internal/doctor/checks` package path was adapted to the current flat `internal/doctor` layout. |
| Check registration is gated on Postgres-backed scopes. | PASS | `cmd/gc/cmd_doctor.go` registers `doctor.NewPostgresAuthCheck` only when `--explain-postgres-auth` is requested or `doctor.HasPostgresBackedScope(cityPath, cfg)` is true; tests cover the gating helper and command flag. |
| `gc doctor --explain-postgres-auth` renders the tier diagnostics without secret values. | PASS | `cmd/gc/cmd_doctor.go` adds the flag and `internal/doctor/postgres_auth.go` implements `RenderExtras`; `TestPostgresAuthExplainTable` and `TestPostgresAuthExplainEmptyScopes` cover the table, footer, and empty case. |
| `[YES]`, `[no]`, `[skip]`, and `[ERR]` semantics are exercised. | PASS | `internal/doctor/postgres_auth_test.go` asserts winner and skip behavior in explain output plus permission/malformed-file handling in status branches. |
| Typed event payload is registered and omits the password. | PASS | `internal/events/events.go` adds `events.PostgresCredentialResolved`; `internal/pgauth/events.go` registers `PostgresCredentialResolvedPayload` with six string fields and no password field; `grep -R "\"password\"" -n internal/pgauth/events.go` returned no matches. |
| Event emission lives in the credential env helper and is best-effort. | PASS | `cmd/gc/bd_env.go` calls `emitPostgresCredentialResolved(...)` after successful resolution in `applyResolvedScopePostgresEnv`; tests cover the emitted event and environment projection. |
| Redaction regression test passes. | PASS | `internal/pgauth/events_redact_test.go` contains `TestPostgresEventOmitsPassword`; review and fast-suite runs passed. |
| Generated API and dashboard artifacts are synchronized. | PASS | `internal/api/openapi.json`, `docs/schema/openapi.*`, `internal/api/genclient/client_gen.go`, and dashboard generated types are updated; `make dashboard-check` passed. |
| Warm-up alert is not implemented in this slice. | PASS | The builder found no reusable startup alert path and did not add a one-off path; follow-up warm-up producer work remains tracked by `ga-uslskt`. `PostgresAuthCheck.WarmupEligible()` returns false in this branch. |
| Architecture guardrails hold. | PASS | No `map[string]any` or `json.RawMessage` introduced on wire payloads; `git diff origin/main...HEAD | rg -n "mayor|Mayor|deacon|Deacon|polecat|Polecat"` returned no matches. |

## Reviewer Notes

- New operator surface: `gc doctor --explain-postgres-auth`.
- New doctor check surface: `postgres-auth`, shown only for Postgres-backed scopes unless explain mode is requested.
- New typed event: `pg.credential_resolved`, with host, port, user, scope, and source metadata only.
- Security-sensitive review surface: no password value appears in doctor output, explain output, event payloads, or event JSON.
- This PR intentionally does not add the warm-up alert producer; that remains blocked behind the warm-up runner and tracked separately.
