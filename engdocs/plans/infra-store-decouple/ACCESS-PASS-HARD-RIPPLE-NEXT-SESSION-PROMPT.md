# Next-session prompt — access-pass DI, HARD-ripple tranche

Paste the block below into a fresh session.

---

Continue the **object-model front-door access-pass DI** on branch
`upstream/object-front-doors-cleanup` (base `main`, DRAFT PR #3839, worktree
`/data/projects/gascity/.claude/worktrees/object-front-doors`; run `git rev-parse HEAD` —
should be at/after `2fd4cbc5a`). The shape pass is DONE; access-pass batches 1–2 sealed 7
files onto `frontDoorStoreFreeFiles`. This session is the LAST, HARDEST tranche:
`cmd_session_wake.go` and `cmd_session.go`.

**Read first, in order:**
1. `engdocs/plans/infra-store-decouple/ACCESS-PASS-HARD-RIPPLE-HANDOFF.md` — the current-state
   handoff: the goal, the two proven byte-identical patterns (reach-through for receivers;
   SRP split for roots), the already-decided EXCLUSIONS, the HARD files with their specific
   blockers + options, and the discipline. **START HERE.**
2. `engdocs/plans/infra-store-decouple/SESSION-PERIPHERY-CLOSURE-PLAN.md` Progress log
   (CONT-37 → CONT-39) — the live status.
3. Memory `infra-beads-decoupling-plan.md` CONT-38/39.

**KEY DECISIONS (do not relitigate):**
- Use the **reach-through** `store := sessFront.Store().Store` for byte-identity, NOT the
  typed `sessFront.Get` (it adds validation + re-wraps the error).
- Composition-**root** files (open the store in their own RunE) are "intentionally not listed"
  per the guard doc. Make them store-free by **SRP-splitting their receiver LEAF helpers** into
  a store-free companion file (the root stays and passes `sessionFrontDoor(store)`). Owner chose
  SRP split over a composition-factory (which would game the guard).
- **EXCLUDED (leave as raw-store infra):** cmd_prime.go (root), session_resolve.go (20-caller
  resolver spine), session_template_start.go (creation spine). Dependents reach through them.

**The HARD files (each its OWN careful pass — re-census first; numbers drift):**
- **cmd_session_wake.go** — a root with 3 raw-bead escapes on the WAKE bead
  (`session.WakeSession` / `RepairEmptyType` (mutates) / `IsSessionBeadOrRepairable`), which
  `sessFront.Get→Info` can't feed. Decide WITH THE OWNER: leave it unlisted (it's a root, no
  receiver leaf), reach-through the whole wake, or add `session.Store` wake/repair methods.
- **cmd_session.go** (~2530 LOC) — ~9 in-file RunE roots, a cross-class
  `rigStores map[string]beads.Store`, 4 inline `sessionFrontDoor(`, 3 raw-bead escapes. It is a
  collection of command roots; wholesale store-freedom is the wrong frame. Split any pure
  receiver leaf helpers into companions; leave the roots unlisted; treat rigStores as a separate
  rig class. Scope as its own multi-commit effort; confirm with the owner whether the roots
  warrant listing vs. a separate `resolveSessionStore`-routing (relocation-safety) pass.

**Discipline (byte-identity is the bar):** per companion — move receiver leaves verbatim,
convert `store beads.Store`→`sessFront *session.Store` (reach-through), root passes
`sessionFrontDoor(store)` + prune its unused imports, wrap the moved funcs' test call sites,
add the COMPANION to the guard list → build/vet/`golangci-lint 0`/gofmt/targeted tests →
**revert-canary** (inject a `beads.Store` decl, guard must fail) → **a fable adversarial
behavior-identity review BEFORE the commit** (`model:'fable'`, effort high, ask it to REFUTE;
diff each moved func against `git show HEAD:<root>` to prove verbatim) → commit + push
`--no-verify`. Trailer `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
Update the plan Progress log + memory as files close.

**Guardrails:** the `cmd/gc` test binary is huge — scope `go test -run`, isolated
`GOCACHE=$(mktemp -d)`, run build/vet/tests in the background (cold compile > 2 min). Never run
the canary concurrently with golangci-lint (torn read). `git push` always `--no-verify` (7-min
pre-push hook). gascity Dolt LOCAL-ONLY — `git push` only. `#3839` stays DRAFT. For any
`internal/api` work, read `engdocs/architecture/api-control-plane.md` first.

---
