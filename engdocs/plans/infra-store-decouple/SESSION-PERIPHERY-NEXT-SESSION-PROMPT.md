# Next-session prompt — session-class periphery, shape pass (continued)

Paste the block below into a fresh session.

---

Continue the **object-model front-door migration**, session-class periphery **shape
pass**, on branch `upstream/object-front-doors-cleanup` (base `main`, DRAFT PR #3839,
worktree `/data/projects/gascity/.claude/worktrees/object-front-doors`; run
`git rev-parse HEAD` — should be at/after `92f05221d`).

The reconciler decision path, nudge, and mail classes are sealed. This pass drives
SESSION-bead *field reads* behind the typed `session.Info` codec and guards each clean
file. **10 periphery files + `Info.ProviderKind` are done; `soft_reload.go` is the first
FULLY-sealed file (all three guard lists).**

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

**Pick your target (recommended order):**
- The **Tier-1 giants** each warrant their OWN session (`build_desired_state.go` ~4520 ln,
  `city_runtime.go` ~3477 ln) — reconciler-grade care. The Phase B Info-form helpers
  (`sessionCoreConfigForHashInfo`, `applyTemplateOverridesToConfigInfo`,
  `cancelSessionConfigDriftDrainInfo`) plus the `session_origin.go` Info siblings already
  exist to support them; converting `session_origin.go`'s bead-form callers happens here.
- **Tier-2** `cmd_start.go` / `cmd_session.go` — medium CLI; classify each `.Metadata[`
  (session vs work vs wait) first.
- The **remaining Tier-4** are all DEFER/no-guard (session_origin = library trap,
  pool_desired_state oracle ref, usage_compute needs Phase A, pool_session_name /
  doctor_session_model = mixed). See the handoff for why; only convert their session
  reads if you want the shape value without a guard entry.
- The **access-layer pass** (route loads through `sessionsBeadStore()` and add the 9
  shape-sealed CLI files to `frontDoorStoreFreeFiles`) is a coherent separate slice that
  makes those files truly relocation-safe.

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
