# Access-pass DI — HARD-ripple handoff

**Branch** `upstream/object-front-doors-cleanup` (base `main`), **PR #3839 DRAFT**,
worktree `/data/projects/gascity/.claude/worktrees/object-front-doors`.
**HEAD `2fd4cbc5a`** (always `git rev-parse HEAD`; re-grep every line number below).

This is the successor to the SESSION-PERIPHERY shape pass (DONE — the guard-earning
shape targets are exhausted; see `SESSION-PERIPHERY-CLOSURE-PLAN.md` CONT-37) and the
**access-pass DI** batches 1–2 (CONT-38/39, same plan). It hands off the last, hardest
tranche of the access pass: `cmd_session_wake.go` and `cmd_session.go`.

Read `SESSION-PERIPHERY-CLOSURE-PLAN.md` Progress log (CONT-38 + CONT-39) FIRST — it is
the live status. This doc is the narrative handoff for the HARD files.

---

## What the access pass is (the goal)

Make each session-touching `cmd/gc` file **store-free** — it holds no `beads.Store` /
`beads.SessionStore` type and constructs no front door inline — so it receives a typed
`*session.Store` and a `[beads.classes.sessions]` relocation captures its session access.
Enforced by `TestFrontDoorStoreFreeFilesStayStoreFree` (`frontDoorStoreFreeFiles` in
`cmd/gc/frontdoor_di_guard_test.go`), a substring guard forbidding `beads.Store`,
`beads.SessionStore`, `sessionFrontDoor(`, `orders.NewStore(`, `nudgeFrontDoor(`,
`workAssignment{` in a listed file.

**`frontDoorStoreFreeFiles` today (7):** session_circuit_breaker, soft_reload,
adoption_barrier, session_index, mcp_integration, skill_visibility, session_logs_resolve.

## The two proven patterns (byte-identical — reach-through, NOT typed Get)

1. **Receiver file** (its session funcs take a store param; composition root is elsewhere):
   swap `store beads.Store` → `sessFront *session.Store`; reach the raw session-class store
   via `store := sessFront.Store().Store` (a method+field chain the `beads.Store` needle
   can't see); `if store == nil` → `if !sessFront.Backed()`. Callers pass
   `sessionFrontDoor(store)`. Examples: mcp_integration, adoption_barrier, session_index.
   > Use the reach-through, NOT the typed `sessFront.Get(id)` — `session.Store.Get`
   > (`internal/session/info_store.go:178`) adds an `IsSessionBeadOrRepairable` validation
   > and re-wraps the error, so it is NOT byte-identical to `store.Get` + `InfoFromPersistedBead`.

2. **Composition-root file** (opens the store in its own RunE — the guard doc says roots
   are "intentionally not listed"): **SRP split** the pure receiver LEAF helpers into a new
   companion file (store-free, receives `sessFront`); the root stays and constructs
   `sessionFrontDoor(store)` at the call site. Examples: cmd_skill → `skill_visibility.go`,
   cmd_session_logs → `session_logs_resolve.go`. Wrap the moved funcs' test call sites in
   `sessionFrontDoor(store)`; prune the root's now-unused imports; list the COMPANION.

**Owner call (CONT-39):** prefer the SRP split over a composition-factory helper (which
would game the "receive, don't construct inline" intent for root files).

## Already-decided EXCLUSIONS (do NOT try to list these)

- **cmd_prime.go** — a genuine root: its hook helpers (`primeHookSessionTemplate`,
  `persistPrimeHookProviderSessionKey`) open the store from `cityPath` internally and do a
  single `SetMarker` write. No receiver leaf to split. Leave it (it constructs the front
  door inline — sanctioned).
- **session_resolve.go** — the shared `resolveSessionID*` resolver spine (**20 non-test
  callers across 13 files**). Converting it ripples across the whole session-command surface
  for little value; dependents reach through it. Treat as raw-store infra.
- **session_template_start.go** — the shared session-creation spine `session_resolve`
  drives. Same rationale.

These three are relocation-safe **via their dependents' reach-through** and need no
store-free listing. If relocation-safety of the SPINE itself is ever required, that is a
separate, larger effort (convert the spine + its 20 call sites together), not this pass.

---

## The HARD files (this session's work)

Both are **command-root collections** — per the guard doc they are not themselves
store-free targets. The tractable work is the **SRP split of any pure receiver leaf
helpers** + handling their specific blockers. Re-census each (the numbers below are from
the CONT-38 survey `wf_5bac5e83-758` — VERIFY before editing).

### `cmd_session_wake.go` (smallest, but 3 raw-bead escapes)
- 0 store-param sigs; the store is a local in `cmdSessionWake` (a root). One forbidden
  needle: `sessionFrontDoor(store).ApplyPatch(...)` (~:82).
- **The blocker:** `store.Get(id)` returns a raw `beads.Bead b` that is fed to three
  `session`-package raw-bead functions the typed door can't satisfy:
  `session.IsSessionBeadOrRepairable(b)`, `session.RepairEmptyType(store, &b)` (MUTATES b),
  `session.WakeSession(store, b, now)`. `sessFront.Get` returns Info, not a bead.
- **Options (pick with the owner):** (a) it is a root — leave it unlisted (honest; the wake
  logic IS the command, no receiver leaf to split); (b) reach-through the whole flow
  (`store := sessFront.Store().Store` for Get/RepairEmptyType/WakeSession; replace the `:82`
  `sessionFrontDoor(store)` with a `sessFront` obtained from a root helper) — but cmdSessionWake
  opens the store itself, so this only removes the inline `sessionFrontDoor(`, and the file
  stays a root that opened a raw store (needle-clean but semantically a root); (c) add
  `session.Store` wake/repair methods (`WakeSession(id)` / `RepairAndWake`) so the raw bead
  never surfaces — the cleanest but the most work (new front-door surface + oracle).

### `cmd_session.go` (LARGEST — ~2530 LOC, ~9 roots)
- **~9 in-file RunE composition roots** each opening a store: cmdSessionNew,
  doSessionListFallback, cmdSessionAttach, cmdSessionSuspend, cmdSessionClose,
  cmdSessionRename, cmdSessionPrune, cmdSessionKill, doSessionPeekFallback, cmdSessionSubmit.
- **`var rigStores map[string]beads.Store`** (~:1742, in cmdSessionClose) — a CROSS-CLASS rig
  map the session reach-through does NOT cover (it's not session-class). Its own ownership
  boundary; likely stays raw / handled separately.
- **4× inline `sessionFrontDoor(`** (~:336/:370/:450/:1614); **3 raw-bead escapes**:
  `unclaimWorkAssignedToRetiredSessionBead(store, rigStores, closedSessionBead, …)` (raw bead
  into a work-release helper), `namedSessionIdentity(bead)`, and `sessionReason` reading
  `b.Status`/`b.Metadata` directly off a `beadIndex map[string]beads.Bead`.
- **Reality:** this file is a collection of command roots; wholesale store-freedom is not the
  right frame. The tractable increments: (1) split any pure receiver leaf helpers into a
  store-free companion (as with logs/skill) — audit which of the ~9 flows have a receiver leaf
  vs open-and-use inline; (2) leave the roots unlisted; (3) treat rigStores as a separate rig
  class. Recommend scoping this as its own multi-commit session and confirming with the owner
  whether the roots warrant listing at all vs. a relocation-routing pass (route each root's
  store through `resolveSessionStore` for session-class relocation-safety — a different, harder-
  to-verify axis than the store-free guard).

---

## Discipline (the bar — unchanged)

Per file/companion: verified census (re-grep) → move receiver leaves verbatim into a companion
+ convert to `sessFront` (reach-through) → root passes `sessionFrontDoor(store)` + prune its
unused imports → wrap moved funcs' test call sites in `sessionFrontDoor(store)` → add the
companion to `frontDoorStoreFreeFiles` → `gofmt` · `go build ./cmd/gc/` · `go vet` ·
`golangci-lint run ./cmd/gc/` (0) · targeted tests → **revert-canary** (inject a `beads.Store`
decl into the companion, the guard must fail naming it) → **a fable adversarial behavior-identity
review BEFORE the commit** (`model:'fable'`, high effort, ask it to REFUTE; compare each moved
func against `git show HEAD:<root>` to prove verbatim) → commit + push `--no-verify` (trailer
`Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`). Update the plan
Progress log + memory (`infra-beads-decoupling-plan.md`).

**Gotchas:** the `cmd/gc` test binary is huge — scope `go test -run`, use an isolated
`GOCACHE=$(mktemp -d)`, and expect a cold compile to exceed a 2-min shell window (run
build/vet/tests in the background). Never run the canary CONCURRENTLY with golangci-lint (a
torn read mis-attributes findings). `git push` always `--no-verify` (7-min pre-push hook; gates
run manually). gascity Dolt is LOCAL-ONLY — `git push` only.

**Future guard tightening (noted by the CONT-39 fable review):** the reach-through
`sessFront.Store().Store` re-introduces a `beads.Store`-typed local the substring needle can't
see (true of every reach-through file). A stricter guard could forbid `.Store().Store` in
listed files; today it is the sanctioned byte-identity escape hatch. If you harden the guard,
audit soft_reload/adoption_barrier/mcp_integration/skill_visibility/session_logs_resolve too.

## Acceptance

The whole access pass closes when relocating `[beads.classes.sessions]` and confirming all
non-root session access follows. The command ROOTS legitimately stay unlisted; their
relocation-safety is a separate `resolveSessionStore`-routing concern, not the store-free guard.
