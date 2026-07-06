# Next-session prompt — CLI session relocation-routing

Paste the block below into a fresh session.

---

Continue the **CLI session relocation-routing** pass on branch
`upstream/object-front-doors-cleanup` (base `main`, DRAFT PR #3839, worktree
`/data/projects/gascity/.claude/worktrees/object-front-doors`; run `git rev-parse HEAD` —
should be at/after `85c659be1`). **15 cmd/gc files routed + the beadmail two-store split**
(CONT-42 cmd_session.go 10 roots; CONT-43 the gc status trio; CONT-44 cmd_mail via the
beadmail messaging/session two-store split — the LAST non-deferred blind root).

**NO non-deferred blind roots remain.** The next work is a real decision (pick with the owner):
either **Phase 6** (write the SUBSTITUTE routing test — see below; the real E2E is blocked on
building the relocation mechanism) or start the **DEFERRED entangled set** (cmd_wait,
cmd_handoff+cmd_runtime_drain, cmd_nudge, cmd_sling, cmd_start cascade).

**Read first, in order:**
1. `engdocs/plans/infra-store-decouple/RELOCATION-ROUTING-HANDOFF.md` — the current-state
   handoff: why the access pass pivoted to relocation-routing, the seam
   (`cli_session_store.go` — `cliSessionStore`/`cliSessionFrontDoor`), the guard, the 10 roots
   DONE, and the REMAINING blind roots (with the completeness census). **START HERE.**
2. `engdocs/plans/infra-store-decouple/SESSION-PERIPHERY-CLOSURE-PLAN.md` Progress log
   (CONT-40) — the live status.
3. Memory `infra-beads-decoupling-plan.md` CONT-40.

**KEY DECISIONS (do not relitigate):**
- Route via `cliSessionStore`/`cliSessionFrontDoor` (= `resolveSessionStore`, identity today →
  byte-identical). Whole-store route only when EVERY consumer is session-class; else surgical
  (session calls → sessStore; work/rig/mail/nudge/dep → plain store).
- cfg-less / hot / hook / daemon paths load cfg via `loadCityConfigWithoutBuiltinPackRefresh(cityPath, io.Discard)`
  (NOT `loadCityConfig` — pack-refresh side effect).
- Mixed files (controller.go, cmd_start.go) route for correctness but stay OFF the guard list.
- DEFERRED (do not attempt piecemeal): cmd_wait.go (owner-approved), cmd_handoff.go+cmd_runtime_drain.go
  (paired shared-helper effort), cmd_nudge.go, cmd_sling.go, cmd_start.go reconcile cascade.

**IMMEDIATE WORK (pick with the owner):**
- **Phase 6 SUBSTITUTE routing test (recommended, writable today, ~hours, zero prod risk).** The LITERAL
  end-to-end `[beads.classes.sessions]` test is BLOCKED: the relocation mechanism does not exist yet —
  `resolveClassStore` (class_store.go:231) is a pure identity stub, and `config.BeadsConfig` has no
  `Classes` field so `[beads.classes.sessions]` decodes into nothing (see the HANDOFF "Acceptance"
  section). Instead upgrade the substring guard to a behavioral proof: (A) seam-parity — drive
  `cliSessionStore`/`cliSessionFrontDoor` with a recording fake `beads.Store`, assert every session op
  hits the injected store; (B) classifier-create — assert `createTarget(coordclass.Classify(b))` routes
  session+gc:wait→ClassSessions, work/mail/nudge→their classes. Reuse `countingStore`/`recordingStore`.
- **OR the DEFERRED entangled set** (each its own coordinated effort): cmd_handoff.go+cmd_runtime_drain.go
  (paired; also closes cmd_handoff.go:312 `beadmail.New` — the last single-store mail construction),
  cmd_wait.go (owner-approved), cmd_nudge.go, cmd_sling.go, cmd_start.go reconcile cascade.
- **OR build the relocation MECHANISM** (the multi-day feature that makes the whole pass pay off +
  unblocks the real E2E): a `BeadsConfig.Classes` config struct + a class-keyed store opener + a
  non-identity `resolveClassStore`. Deserves its own spec.

**DONE at CONT-44 (do not redo):** cmd_mail — the two-store beadmail split (commit `85c659be1`).
`beadmail.Provider` gained a `sessionStore` field; `New`/`NewCached` are identity shims over new
`NewWithStores`/`NewCachedWithStores` (0 caller edits, 77 callers byte-identical); the 11 session sites
route to `p.sessionStore` (Reply's original-MESSAGE Get correctly stayed on `store`); both prod paths
wired (`openCityMailProvider` CLI via `cliSessionStore`, `newCityMailProvider` controller via
`resolveSessionStore`). providers.go now FULLY routed (was PARTIAL). Fable review = GO (COULD-NOT-REFUTE).
The only residual single-store beadmail construction is cmd_handoff.go:312 (in the deferred paired set).

**DONE at CONT-43 (do not redo):** the gc status trio — cmd_status.go (`gc rig status`),
cmd_citystatus.go + city_status_snapshot.go (`gc status`). SURGICAL/multi-class: routed the session reads
(loadStatusSessionSnapshot, namedSessionStatusForCity's resolveSessionIDWithConfig + store.Get,
collectCitySessionCounts's workerSessionCatalogWithConfig) through cliSessionStore; kept
buildCityStoreHealth→collectStoreHealth→store.List (footprint of the OPENED store) on the plain work store;
observeSessionTargetWithWarning store param is DEAD. `collectCityStatusSnapshot` was a live TEST entry
(not dead), routed too. Commit `b7e359895`.

**DONE at CONT-41 (do not redo):** cmd_restart.go (whole-store at the cmdRigRestart caller),
completion.go (whole-store), providers.go (PARTIAL — loadProviderSessionSnapshot routed; the
openCityMailProvider/beadmail session read+write is the deferred two-store-mail gap, documented in-code).

**DONE at CONT-42 (do not redo):** cmd_session.go — all 10 gc session command roots (9 whole-store +
cmdSessionClose surgical). A 10-agent census workflow proved cmdSessionKill is whole-store (NOT
multi-class as the old plan guessed) and only cmdSessionClose has the WORK-class work-release. If you
tackle cmd_mail.go, remember the session reads live in the shared beadmail provider (openCityMailProvider),
not the subcommands — it is the two-store-mail follow-up, larger than a substring route.

**Discipline (byte-identity is the bar):** per root — verified per-consumer census (re-grep; DON'T trust
prior classifications) → route → gofmt·build·vet·`golangci-lint 0`·targeted tests → **revert-canary**
(guard fails naming the file) → **fable adversarial byte-identity review BEFORE commit** (`model:'fable'`,
REFUTE; diff vs `git show HEAD:<file>`; for whole-store routes also confirm semantic session-class-correctness)
→ commit + push `--no-verify`. Trailer `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
Add fully-routed single-class files to `sessionRelocationRoutedFiles`; grow the list at each phase end.

**Guardrails:** `cmd/gc` test binary is huge — scope `go test -run`, isolated `GOCACHE=$(mktemp -d)`, run
build/vet/tests in the background (cold compile > 2 min). NEVER run the canary concurrently with golangci-lint
(torn read). `git push` always `--no-verify`; commit `--no-verify` too (stale absolute core.hooksPath breaks
commit). gascity Dolt LOCAL-ONLY. `#3839` stays DRAFT.

**Phase 6 (still TODO):** the end-to-end `[beads.classes.sessions]` relocation acceptance test — the
authoritative check the substring guard can't provide.

---
