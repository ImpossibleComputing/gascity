# Next-session prompt — session-class periphery, shape pass (continued)

Paste the block below into a fresh session.

---

Continue the **object-model front-door migration**, session-class periphery **shape
pass**, on branch `upstream/object-front-doors-cleanup` (base `main`, DRAFT PR #3839,
worktree `/data/projects/gascity/.claude/worktrees/object-front-doors`; run
`git rev-parse HEAD` — should be at/after `92f05221d`).

The reconciler decision path, nudge, and mail classes are sealed. This pass drives
SESSION-bead *field reads* behind the typed `session.Info` codec and guards each clean
file. **11 periphery files + `Info.ProviderKind` + `Info.DependencyOnlyMetadata` are done;
`soft_reload.go` is the first FULLY-sealed file (all three guard lists);
`cmd_session.go` was sealed in CONT-37.**

> **READ THIS FIRST — the guard-earning shape pass in `cmd/gc` is EXHAUSTED (CONT-37).**
> Every remaining cmd/gc candidate is permanently guard-ineligible in a shape pass:
> the two Tier-1 giants (`build_desired_state.go` = session writes + work-bead reads;
> `city_runtime.go` = raw-by-design whole-map fingerprint + `.Open()` library-traps),
> `session_origin.go` (the classifier oracle's raw arm), `cmd_start.go` (its `.Open()`
> feeds the reconciler entry). Do NOT expect a quick guard win from more shape work.
> The next guard-earning + relocation-completing work is the **access-pass DI
> initiative** (below). Full rationale: the CONT-37 STRATEGIC FINDING in the plan
> Progress log.

**Read first, in order:**
1. `engdocs/plans/infra-store-decouple/SESSION-PERIPHERY-SHAPE-PASS-HANDOFF.md` — the
   current-state handoff: the shape-vs-access two-pass decision, what's done, the two
   load-bearing lessons (clean-Tier-4 criterion + guard eligibility), what's left, the
   discipline. **START HERE.**
2. `engdocs/plans/infra-store-decouple/SESSION-PERIPHERY-CLOSURE-PLAN.md` — the plan +
   the live **Progress log** at the bottom.
3. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-LOCKSTEP-DROP.md` — the
   proven pattern/discipline you are continuing.
4. Memory `infra-beads-decoupling-plan.md` CONT-34→36.

**KEY DECISION (do not relitigate):** seal in two passes — **shape** (reads → `Info`,
add to `metadataInfoOnlyFiles`/`snapshotInfoOnlyFiles`) then **access** (loads →
`sessionsBeadStore()`, add to `frontDoorStoreFreeFiles`). `metadataInfoOnlyFiles`
membership is SHAPE-sealed, NOT relocation-safe.

**Pick your target (recommended order, CONT-37):**
- **RECOMMENDED — the access-pass DI initiative.** The ONLY remaining guard-earning +
  relocation-completing work. Route the ~11 shape-sealed files' bead LOADS through the
  session-class front door and make each store-free so it joins `frontDoorStoreFreeFiles`
  (which forbids holding `beads.SessionStore` / calling `sessionFrontDoor(` — the
  composition root threads in `*session.Store`; `session.Store.Get` returns `Info`).
  This is a package-wide DI refactor (many cross-file call sites), so scope it: start
  with the files that already receive a store param on few call sites; `soft_reload.go`
  is the model (already on all three lists). Confirm with the owner that "separate,
  later" is now "now" before committing to the multi-session refactor.
- **Shape-value-only (no guard payoff) — the Tier-1 giants.** Each its OWN session,
  reconciler-grade care. Converting their session reads behind Info preps the eventual
  full seal but earns NO guard (session writes + work reads + raw-by-design residuals
  keep `.Metadata[`). Lower priority per the guard-eligibility lesson. The Phase B
  Info-form helpers + `session_origin.go` Info siblings exist to support them.
- **Different-package tranches (Phase D/E/F)** — `internal/api` (read
  `api-control-plane.md` first), `internal/worker`, `internal/session` own runtime. Each
  needs the guard's dir resolution extended or sibling guards; separate scoping.
- The **remaining Tier-4** are all DEFER/no-guard (see the handoff).

**Discipline (byte-identity is the bar):** re-grep each file's exact sites FIRST (census
line numbers drift and the haiku census was wrong on several fields — verify every field
against the `Info` struct at `internal/session/manager.go:74` + `info_store.go` codec).
Traps: `MetadataState` not `State`; `SessionNameMetadata` not `SessionName`. Per file:
build Info sibling if the read flows into a bead-form helper (delegate the bead form to
it) → flip reads → guard entry + **revert-canary** → build/vet/`golangci-lint 0`/gofmt/
targeted tests (+ reconciler subset if a hot helper changed) → **a fable adversarial
byte-identity review BEFORE the commit** (`model:'fable'`, effort high, ask it to REFUTE)
→ commit + push `--no-verify`. Trailer
`Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. Update the plan
Progress log + memory as files close.

**Guardrails:** `git push` always `--no-verify` (7-min pre-push hook; gates run
manually). Isolated `GOCACHE=$(mktemp -d)`; never `go clean -cache`. gascity Dolt
LOCAL-ONLY — `git push` only. `#3839` stays DRAFT. For `internal/api`, read
`engdocs/architecture/api-control-plane.md` first.

---
