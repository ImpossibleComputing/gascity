# Next-session prompt — reconciler front-door Step 6b (convert residual raw decision reads to Info)

Paste the block below into a fresh session.

---

Continue the **session reconciler front-door migration** on **PR #3839** (branch
`upstream/object-front-doors-cleanup`, base `main`, DRAFT, worktree
`/data/projects/gascity/.claude/worktrees/object-front-doors`; `git rev-parse HEAD`).

**Read first, in order:**
1. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-STEP6-DESIGN.md` — the
   Step-6 design + backlog. **Read §5 (fable red-team corrections)** — it constrains
   6b/6c/6d. READ THIS FIRST.
2. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-HANDOFF.md` — status.
   Steps 0–5 DONE, Step 6 designed, **6a DONE**; you are on **6b**.
3. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-SPEC.md` — §2 governing
   principle.

**Where things stand.** 6a landed the codec-gap `Info` mirrors
(`Info.SessionIDFlag`/`TemplateOverrides`/`WakeAttemptsMetadata`, commit `ea5103b96`).
The naive Get-cutover was reverted (STEP6-DESIGN §2); the cutover is 6d via
write-returns-`Info`. A live min-floor bug the fable review found was fixed (`212581818`).

**Confirm a green baseline:**
```
go build ./... && go vet ./cmd/gc/... ./internal/session/...
golangci-lint run ./cmd/gc/... ./internal/session/...   # expect 0
go test ./cmd/gc/ -run 'TestSessionClassifierInfoEquivalence|TestReconcileSessionBeads_ProgressStall' -count=1
git checkout go.sum
```

**DO STEP 6b (this session): convert the residual raw decision reads to `Info`.** Flip
reconciler-tick DECISION reads that still crack `session.Metadata[...]` to the coherent
`infoByID` snapshot / existing `*Info` siblings. Byte-identical during coexistence
(snapshot == raw bead via the lockstep), PROVIDED you verify the snapshot is fresh at
each read point (spec §2 — a converted read must sit after the refreshSessionInfo that
reflects all prior same-iteration mutations). Audit D `note` cluster (STEP6-DESIGN §3
6b): `lifecycleTimerBlocker`@2582/2654 (→ `Info.HeldUntil`/`QuarantinedUntil`),
`evaluateWakeReasons` (DELETE — deprecated dead code), `healExpiredTimers`,
`sessionExitFacts`, `recordWakeFailure` (wake_attempts dual-form → `Info.WakeAttemptsMetadata`
from 6a), `healStatePatchWithRollback` (→ `LifecycleInputFromInfo`, exists), the
`pendingCreate*`/config-drift/hash reads, the 6a-gap consumers (`freshRestartSessionKey`,
`applyTemplateOverridesToConfig`, `parseSessionTemplateOverridesForLaunch`), and an
`isDrainAckStopPendingInfo` sibling (`Info.MetadataState`/`StateReason` already exist).

**HARD CONSTRAINTS (fable §5):**
- **Read-side only.** Do NOT touch `for i := range ordered`, `&ordered[i]`, `beadByID`,
  refreshSessionInfo's source, or any lockstep mirror — those are 6c/6d. Converting a
  loop that CONTAINS a lockstep mirror (2151-2159, 2635-2636, 2709-2710, 2821) would
  drop the lockstep early and silently stale reads (invisible to the write oracle).
- Each conversion = a small commit with an **equivalence oracle** case
  (`isX(bead) == isXInfo(InfoFromPersistedBead(bead))`), then a **same-tick
  read-after-write** whole-tick test if the read sits after a mutation.
- Trace payloads that need the raw verbatim string (e.g. `pending_create_claim` bool
  gap) keep a named raw accessor (spec §4.1) — do not normalize the trace surface.

**Then 6c** (retire the raw working set — read-side aggregate consumers only) and **6d**
(the write-returns-`Info` cutover + lockstep drop; re-derive the complete 9-refresh-site
/ ~15-writer set at implementation time per §5). **Do NOT re-attempt the naive
`refreshSessionInfo → Get` flip.**

**Gates per commit:** `go build ./...` · `go vet` · `golangci-lint ./cmd/gc/...
./internal/session/...`=0 · gofmt · equivalence oracle + whole-tick
`TestReconcileSessionBeads*` + circuit/named/pool/wake/sleep/drain/trace (heavy suites in
the background, ~70–130s). `git checkout go.sum` after. Commit AND push `--no-verify`.
Trailer: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. Never
`tmux kill-server` / `go clean -cache` (`-testcache` ok); gascity Dolt LOCAL-ONLY (no
`bd dolt push`). #3839 stays DRAFT. Quote grep globs (`--include='*.go'`). Mapping agents
have read the WRONG worktree (`.worktrees/pack-crud`) — pin HEAD, verify line numbers.
Update the handoff + STEP6-DESIGN check boxes + memory (`infra-beads-decoupling-plan.md`).

---
