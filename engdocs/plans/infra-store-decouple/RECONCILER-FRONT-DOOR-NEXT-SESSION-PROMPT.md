# Next-session prompt — reconciler front-door Step 6a (codec fidelity gaps)

Paste the block below into a fresh session.

---

Continue the **session reconciler front-door migration** on **PR #3839** (branch
`upstream/object-front-doors-cleanup`, base `main`, DRAFT, worktree
`/data/projects/gascity/.claude/worktrees/object-front-doors`, HEAD at the tip of
the branch — `git rev-parse HEAD`).

**Read first, in order:**
1. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-STEP6-DESIGN.md` — the
   Step-6 design (fable 4-lens audit + opus synthesis) with the ordered sub-phase
   backlog and the **evidence-based cutover correction** (§2, §3). READ THIS FIRST.
2. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-HANDOFF.md` — status +
   backlog. Steps 0–5 DONE; Step 6 designed; you are starting **6a**.
3. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-SPEC.md` — design v2.

**Where things stand.** Every reconciler decision-path *read* is already on typed
projections (Steps 1–5). Step 6 retires the raw lockstep + raw working set. The naive
keystone (flip `refreshSessionInfo` to `sessFront.Get`) was tried and **reverted** —
it is a per-tick Get storm + consumes test-injected Get-errors (STEP6-DESIGN §2). The
cutover is re-sequenced to LAST (6d) via **write-returns-`Info`**. Safe additive work
comes first.

**Confirm a green baseline:**
```
go build ./... && go vet ./cmd/gc/... ./internal/session/...
golangci-lint run ./cmd/gc/... ./internal/session/...   # expect 0
go test ./cmd/gc/ -run 'TestSessionClassifierInfoEquivalence|TestReconcileSessionBeads_ProgressStall' -count=1
git checkout go.sum
```

**DO STEP 6a (this session): the codec fidelity gaps.** Additive `Info` mirrors so the
residual raw decision reads (6b) can move onto `Info` without a fidelity loss. NO
behavior change, NO store-I/O change — the Step-1 pattern (add mirror + a
`TestSessionClassifierInfoEquivalence` case; consumer flips in 6b). Confirmed missing
from `Info` at HEAD (verified: `Info` already has `MetadataState`/`StateReason`/int
`WakeAttempts`):

1. **`session_id_flag`** — read by `freshRestartSessionKey(tp, session.Metadata)`
   (session_reconciler.go:~2139, restart-handoff path). Add `Info.SessionIDFlag =
   b.Metadata["session_id_flag"]`.
2. **`template_overrides`** — read by `ParseTemplateOverrides(session.Metadata)`
   (session_reconciler.go:~3918, via `sessionCoreConfigForHash`, config-drift hash
   path). Add `Info.TemplateOverrides = b.Metadata["template_overrides"]` (raw string;
   6b decides how `ParseTemplateOverrides` consumes it).
3. **raw `wake_attempts` fidelity** — `clearWakeFailures` (session_reconcile.go:~857)
   gates on the RAW string (`!="" && !="0"`), which the int-parsed `Info.WakeAttempts`
   (0-on-invalid) cannot reproduce (it collapses ""/"0"/"abc"). Add
   `Info.WakeAttemptsMetadata = b.Metadata["wake_attempts"]` (raw verbatim).

Each: add the field to the `Info` struct, populate it in `InfoFromPersistedBead`
(internal/session/info_store.go), and add a `stringChecks`/fixture case to
`TestSessionClassifierInfoEquivalence` (cmd/gc/session_classifier_info_equiv_test.go)
+ the internal projection test. `Info` gained a non-comparable field before, so keep
using `reflect.DeepEqual` where needed. No consumer flips this session (that is 6b).

**Then 6b** (next): flip the residual raw reads to the `*Info` siblings (Audit D `note`
cluster in STEP6-DESIGN §3 — `lifecycleTimerBlocker`, `evaluateWakeReasons`,
`healExpiredTimers`, `sessionExitFacts`, `recordWakeFailure`, pendingCreate*, the
config-drift/hash reads), each a small oracle-backed commit, verifying the snapshot is
fresh at each read point (spec §2 governing principle). 6c retires the raw working
set; 6d is the write-returns-`Info` cutover + lockstep drop; 6e the guard.

**Gates per commit:** `go build ./...` · `go vet` · `golangci-lint ./cmd/gc/...
./internal/session/...`=0 · gofmt · the new equivalence cases +
`TestSessionClassifierInfoEquivalence` + whole-tick `TestReconcileSessionBeads*` +
circuit/named/pool/wake/sleep/drain/trace (heavy suites in the background, ~70–130s).
`git checkout go.sum` after. Commit AND push `--no-verify`. Trailer:
`Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. Never
`tmux kill-server` / `go clean -cache` (`-testcache` ok); gascity Dolt is LOCAL-ONLY
(no `bd dolt push`). #3839 stays DRAFT.

**Cautions:** quote grep globs (`--include='*.go'`). Do NOT re-attempt the naive
`refreshSessionInfo → Get` flip (reverted; see STEP6-DESIGN §2) — the cutover is 6d
via write-returns-`Info`. Read-only mapping agents have repeatedly read the WRONG
worktree (`.worktrees/pack-crud`) — pin `git rev-parse HEAD`, restrict to this
worktree, verify line numbers. Update the handoff + STEP6-DESIGN check boxes + memory
(`infra-beads-decoupling-plan.md`) as you land each phase.

---
