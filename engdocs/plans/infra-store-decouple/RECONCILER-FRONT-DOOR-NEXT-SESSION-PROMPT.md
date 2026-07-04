# Next-session prompt — reconciler front-door LOCKSTEP DROP: finish line (Steps 5b–5e + 6e)

Paste the block below into a fresh session.

---

Continue and FINISH the **session reconciler front-door "lockstep drop"** on **PR #3839** (branch
`upstream/object-front-doors-cleanup`, base `main`, DRAFT, worktree
`/data/projects/gascity/.claude/worktrees/object-front-doors`; run `git rev-parse HEAD`, expect
`b98725a35` or later). The **entire decision-path READ conversion is DONE (Steps 1–5a)** and **Step 5b is
DONE** (`97fd6fbc6` drain-ack family). What remains: **Steps 5c → 5d → 5e → 6e.** Do them in order, one commit each.

**READ THIS FIRST — a systemic blocker was found & resolved in the 5b session (LOCKSTEP-DROP.md CONT / memory
CONT-31):** the raw metadata mirror WRITES are NOT read-dead — the controller's post-reconcile
`recordReconcileTraceResults` (city_runtime.go `beadReconcileTick`) reads `open[i].Metadata["state"]/["sleep_reason"]`
for the always-on gc-trace, and `open` shares Metadata maps with the reconciler's `ordered` set, so deleting a
mirror stales that trace. There is NO byte-identical replacement (an infoByID-return fix was fable-refuted:
`preWakeCommit` mirrors onto a discarded `store.Get` copy → stale for woken sessions). **Owner-approved fix
already landed (`245a86b4a`):** the gc-trace terminal read now sources from the authoritative post-reconcile
store snapshot (reusing the dispatch snapshot, no added query) — an intentional accuracy improvement, trace-only.
**This UNBLOCKS 5c:** deleting the remaining mirror WRITES no longer stales the trace for open sessions (only the
closed-this-tick fallback residual, LOW). Do NOT re-litigate this; build on it.

**Read first, in order:**
1. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-LOCKSTEP-DROP-FINISH.md` — START HERE. The
   finish-line handoff: current state, the per-step plan, the 5a residue lesson, the 5c start-execution
   census risk, the 6e scoping caveat, the definition of done, and the discipline.
2. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-REMAINING-PLAN.md` §Step 5 / §6e — the
   authoritative per-site design (fable audit+synthesis). Two settled boundary corrections: Step 5
   DEMOTES `ordered` (does not delete it — start execution is raw-by-design consumer #7, out of scope);
   Step 4 already fed the preserve path from live `infoByID`.
3. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-LOCKSTEP-DROP.md` — progress + commit hashes.
4. Memory `infra-beads-decoupling-plan.md` CONT-25→30 — session log; CONT-30 has the 5a residue lesson.

**Confirm a green baseline** (isolated GOCACHE; the FULL cmd/gc suite times out at 600s — use the
reconciler subset; a WARM shared GOCACHE is fine for re-runs):
```
go build ./... && go vet ./cmd/gc/... ./internal/session/...
golangci-lint run ./cmd/gc/... ./internal/session/...   # expect 0
go test ./cmd/gc/ -timeout 25m -run 'Reconcile|Awake|Wake|Sleep|Pool|DrainAck|Recycle|Zombie|Heal|Drift|Churn|Stability|RateLimit|Named|Restart|Progress|Rollback|PendingCreate|MinFloor|Idle|MaxAge|Detach|Rebaseline|Relaunch|Quarantine|Circuit|Lifecycle|Session' -count=1   # ~210s
git checkout go.sum
```

**DO, in order (re-grep every line anchor — they drift):**
- ~~**5b**~~ **DONE** (`97fd6fbc6`): drain-ack family off raw reads + its 3 mirror loops deleted.
  `markDrainAckStopPending` took `session.Info` (dropped the bead); `finalizeDrainAckStoppedSession` gained an
  `Info` param but **KEPT its `*beads.Bead`** (the plan's "drop the param" was infeasible — the whole-bead
  raw-by-design helpers `sessionHasOpenAssignedWorkForReachableStore`/`closeSessionBeadIfReachableStoreUnassigned`/
  `recordDrainAckAssignedWorkEvent`/`sessionAgentMetricIdentity` + the store.Get witness need the raw bead).
  Kept the `session.Status="closed"` struct write + witness swap (non-bracket, telemetry-test-asserted).
- **5c** — **THE riskiest. START HERE.** DELETE the raw lockstep mirror WRITE loops (re-grep
  `session\.Metadata\[.*\] *=[^=]`) — but ONLY after a **per-key census**: mirrors whose keys the
  START-EXECUTION path reads off the raw bead (`buildPreparedStart` reads session_key/instance_token/
  last_woke_at/currently_processing_bead_id via `startCandidate.session`) **SURVIVE** and must be
  documented. The reconciler suite may NOT catch a wrong deletion (start execution is downstream), so
  census by key, not by loop. Keep the `#2345` `ResetCommittedAtKey` exclusion in the surviving
  `restartFold` loop.
- **5d** — drop the dead `sessionBeads` param on `advanceSessionDrainsWithSessionsTraced`
  (`session_wake.go:484/489`); move the `computeWakeEvaluations` fallback into the test-only wrappers.
- **5e** — demote `ordered`: `orderedIDs` for the 2 order-sensitive rebuilds (never `range infoByID` for
  those — SessionName last-write-wins); `openPoolSessionCountForTemplate` MAY `range infoByID`; record the
  final raw census in LOCKSTEP-DROP.md.
- **6e** (LAST) — extend `frontdoor_di_guard_test.go` to forbid `session.Metadata[` on the reconciler
  decision path (needle `"session.Metadata["`; add session_reconciler.go/compute_awake_bridge.go/
  session_reconcile.go once raw-free; NOT session_sleep/session_wake/session_lifecycle_parallel/
  session_bead_snapshot). Handle any surviving start-coupled mirror per the FINISH-doc caveat. Revert-canary.

**Discipline (unchanged — byte-identity is the bar):** per commit — build · vet · golangci-lint 0 · gofmt ·
the reconciler subset (~210s) · `git checkout go.sum` · **a fable adversarial review workflow BEFORE the
commit** (owner prefers fable; reuse the find/verify shape at
`.claude/.../workflows/scripts/step5a-forward-reads-review-*.js`; `model:'fable'`, `effort:'high'`; NO
backticks inside template-literal prompts — build with array `.join('\n')`; the 5a review caught a real
MEDIUM, do NOT skip it, especially for 5c). Commit AND `git push --no-verify`. Trailer:
`Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. Never `tmux kill-server` /
`go clean -cache` (`-testcache` ok). gascity Dolt LOCAL-ONLY (no `bd dolt push`). #3839 stays DRAFT.
Update LOCKSTEP-DROP.md + memory (`infra-beads-decoupling-plan.md`, CONT-31…) as each lands. Definition of
done + the deferred/raw-by-design-forever list are in the FINISH doc.

---
