# Next-session prompt — session-class periphery closure

Paste the block below into a fresh session.

---

Continue the **object-model front-door migration** on branch
`upstream/object-front-doors-cleanup` (base `main`, DRAFT PR #3839, worktree
`/data/projects/gascity/.claude/worktrees/object-front-doors`; run `git rev-parse HEAD`).
The reconciler decision path is DONE + guarded (Steps 1–6e), and the nudge + mail classes
are sealed. Your job: **close the SESSION-class periphery** — drive every direct
session-bead reference behind `session.Info` / `session.Store` so a session backend swap
(`resolveSessionStore` + `[beads.classes.sessions]`, `cmd/gc/class_store.go`) captures 100%
of session access.

**Read first, in order:**
1. `engdocs/plans/infra-store-decouple/SESSION-PERIPHERY-CLOSURE-PLAN.md` — the execution
   plan: dependency ordering (Info fields → Info-sibling helpers → conversions), the
   per-file classified inventory (cmd/gc ~30 files, internal/api ~8, worker ~2,
   internal/session ~6), the raw-by-design exclusions, the WAIT-class caveat (cmd_wait), and
   the guarded-close discipline. **Line numbers/counts are a scout sweep — re-grep + verify
   before editing any file.**
2. `engdocs/plans/infra-store-decouple/CLASS-STORE-LEAK-CLOSURE-BACKLOG.md` — the whole-fleet
   backlog (why this = the per-class backend-swap prereq; the seam is `class_store.go`).
3. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-LOCKSTEP-DROP.md` — the proven
   pattern + discipline you are continuing (Info siblings, byte-identity, fable review).
4. Memory `infra-beads-decoupling-plan.md` CONT-32→34.

**KEY INSIGHT (why this isn't mechanical):** almost every periphery site reads a field that
ALREADY exists on `session.Info` but off the raw bead, AND its bead flows into a raw-bead
HELPER with no `Info`-form sibling (e.g. `sessionCoreConfigForHash`,
`releaseOrphanedPoolAssignmentsWhenSnapshotsComplete`, `canonicalSessionIdentity`,
`isStaleCreating`, `resolveNudgeTargetFromSessionBead`). So each conversion is: build the
`Info` sibling → flip the read → guard the file → fable-review for byte-identity. Same
discipline as the reconciler.

**DO, in order (small→big; verify each file's sites by grep first):**
- **Phase A:** add any missing `Info` fields (provider_kind, invocation_usage_cursor,
  active_work_bead, real_world_app_session_kind, worker_profile — grep the Info struct at
  `internal/session/manager.go` first; most needed fields already exist).
- **Phase C Tier-4** (small cmd/gc util/CLI files) FIRST — lowest risk, each ends with a
  guard entry + revert-canary. Then Tier-2 (soft_reload, cmd_start, cmd_session), then the
  Tier-1 giants (`build_desired_state.go`, `city_runtime.go`, `cmd_nudge.go`) LAST — each of
  the two big decision files warrants its OWN session (reconciler-grade care).
- **Phase D** internal/api (read `engdocs/architecture/api-control-plane.md` first).
- **Phase E** internal/worker (2 files; needs Phase A).
- **Phase F** internal/session's own runtime/lifecycle (`manager.go`/`chat.go`/`named_config.go`/
  `names.go`/`submit.go` — the package doesn't dogfood its own `Info`; riskiest, hot paths).

**Guard:** add each clean file to `snapshotInfoOnlyFiles` / `metadataInfoOnlyFiles` in
`cmd/gc/frontdoor_di_guard_test.go` (revert-canary). For internal/api|session|worker, extend
the guard's dir resolution or add sibling guards.

**Discipline (byte-identity is the bar):** per file — build · `go vet` · `golangci-lint 0` ·
`gofmt` · targeted tests (isolated GOCACHE; `git checkout go.sum` after) · **a fable
adversarial review BEFORE the commit** (owner prefers fable; reuse the find/verify workflow
shape under `.claude/wf-*.js`; `model:'fable'`, `effort:'high'`; NO backticks in template
literals). Commit + push `--no-verify`. Trailer:
`Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. #3839 stays DRAFT.
Never `tmux kill-server` / `go clean -cache` (`-testcache` ok). gascity Dolt LOCAL-ONLY
(no `bd dolt push`). Update the backlog + memory as files close.

**NOT session (do not convert as session):** cmd_wait.go WAIT-bead reads (wait class);
message/nudge/order/convoy beads (their own classes); the raw-by-design set —
`info_store.go` codec, `store.go` facades, `session_beads.go`/`session_bead_snapshot.go`/
`session_hash.go`/`session_sleep.go`/`session_wake.go`/`session_lifecycle_parallel.go`,
worker `handle_construct.go` construction, `manager.go` Create-path bead build.

---
