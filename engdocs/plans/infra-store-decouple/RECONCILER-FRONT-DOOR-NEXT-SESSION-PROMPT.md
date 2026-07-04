# Next-session prompt — reconciler front-door LOCKSTEP DROP: Step 3 (awake-scan domain)

Paste the block below into a fresh session.

---

Continue the **session reconciler front-door migration** on **PR #3839** (branch
`upstream/object-front-doors-cleanup`, base `main`, DRAFT, worktree
`/data/projects/gascity/.claude/worktrees/object-front-doors`; run `git rev-parse HEAD`,
expect `1d2ea2028` or later). We are in the **LOCKSTEP DROP** phase; Steps 1–2 are done and
pushed. **Do Step 3 next.**

**Read first, in order:**
1. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-LOCKSTEP-DROP.md` — the focused
   plan. START HERE. Progress checklist at top; remaining consumers #3–#6; the awake-scan
   slice-order invariant; the exposure set (incl. the buildPreparedStart-residue CAUTION).
2. `engdocs/plans/infra-store-decouple/raw/lockstep-drop-step{1,2}-*.md` — the byte-identity
   analyses + fable-review outcomes for the two shipped steps (the method to copy).
3. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-HANDOFF.md` — master backlog.

**Where things stand.** Steps 1 (`ec6127ead`: circuit persist store-authoritative, drop
`circuitSessionByIdentity`), 2a (`4bcec563b`: `completeDrain` store-only), and 2b (`1d2ea2028`:
`advanceSessionDrains` off the raw bead → `beadByID`/`sessionLookup` retired) are shipped,
fable-reviewed, and gate-green. `circuitSessionByIdentity`, `beadByID`, `sessionLookup` are
GONE. Still present but read-dead for decisions: the raw `ordered []beads.Bead` and ~11
forward-pass `session.Metadata[k]=v` mirrors, plus the `wakeTargets` raw `target.session` beads.

**Confirm a green baseline (isolated GOCACHE; the FULL cmd/gc suite times out at 600s — use
the reconciler subset; a WARM shared GOCACHE is fine and much faster for re-runs):**
```
go build ./... && go vet ./cmd/gc/... ./internal/session/...
golangci-lint run ./cmd/gc/... ./internal/session/...   # expect 0
go test ./cmd/gc/ -timeout 25m -run 'Reconcile|Awake|Wake|Sleep|Pool|DrainAck|Recycle|Zombie|Heal|Drift|Churn|Stability|RateLimit|Named|Restart|Progress|Rollback|PendingCreate|MinFloor|Idle|MaxAge|Detach|Rebaseline|Relaunch|Quarantine|Circuit|Lifecycle|Session' -count=1   # ~210s, ok
git checkout go.sum
```

**DO — Step 3: `buildAwakeInputFromReconciler` domain → order-preserving `[]session.Info`.**
- The decision READS are ALREADY on Info (4C/4D). Only the iteration DOMAIN is raw: the loop
  does `for i := range sessionBeads { b := &sessionBeads[i]; info, ok := sessionInfoByID[b.ID];
  if !ok { info = InfoFromPersistedBead(*b) } ... }`.
- Replace the `sessionBeads []beads.Bead` + `sessionInfoByID map[string]session.Info` params
  with a single **order-preserving `sessionInfos []session.Info`**. Loop becomes
  `for i := range sessionInfos { info := sessionInfos[i]; ... }`. Drop the map + the fallback.
- In the reconciler (call ~3007), build it in `ordered` order:
  `sessionInfos := make([]session.Info, len(ordered)); for i := range ordered {
  sessionInfos[i] = infoByID[ordered[i].ID] }`. **NEVER `range infoByID`** — slice order is
  load-bearing (`ComputeAwakeSet` does `SessionName` last-write-wins + first-match
  `resolveNamedSessionBeadName`; `SessionName` is NON-unique). This is THE invariant to protect.
- 15 test call sites in `compute_awake_bridge_test.go` / `compute_awake_set_min_active_test.go`
  + 1 reconciler site: each passes its beads → build `[]Info` via
  `sessionpkg.InfoFromPersistedBead` per bead in the same order.
- **CAUTION (LOCKSTEP-DROP.md exposure set):** re-confirm via fable review that the
  un-threaded buildPreparedStart residue (`session_key`/`started_config_hash`/
  `continuation_reset_pending` stale-resume clears) stays inert for the awake scan's
  `info.ContinuationResetPending` read; thread it (like 2b did for `instance_token`) if a
  divergence surfaces.
- OPTIONAL Step 3.5 (can split or fold in): the `wakeTargets` / `sleep_intent` raw
  reads+mirror (`session_reconciler.go` ~3185-3222, ~4362; the wakeTargets loop's
  `target.session.Metadata["session_name"]`). `target.session` is a raw bead on `wakeTarget`
  (a DIFFERENT source than `ordered`). `Info.SleepIntent` exists.

**Then Steps 4 (`newSessionBeadSnapshot` off a store source — hardest), 5 (drop the remaining
mirrors + `ordered` + the dead `sessionBeads` param + cut the tick-start build to the store),
6e (extend the `snapshotInfoOnlyFiles` guard).** LOCKSTEP-DROP.md has the detail.

**Discipline (unchanged):** convert each consumer + verify BEFORE deleting its raw source; the
comprehensive reconciler subset is the byte-identity gate (a wrong conversion flips an
awake/drain decision and fails). Keep the slice-order invariant. Run a **fable adversarial
review per non-trivial step** (owner prefers fable, not opus) — the Step-1/2b reviews caught a
real byte-identity break; use the same 4-lens + verify shape (workflow scripts under
`.claude/.../workflows/scripts/step*-review-*.js` are reusable templates). Subagents are fine
for the mechanical test-site conversions. Gates per commit: build · vet · golangci-lint 0 ·
gofmt · the reconciler subset. `git checkout go.sum` after. Commit AND push `--no-verify`.
Trailer: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. Never
`tmux kill-server` / `go clean -cache` (`-testcache` ok). gascity Dolt LOCAL-ONLY (no
`bd dolt push`). #3839 stays DRAFT. Update LOCKSTEP-DROP.md + memory
(`infra-beads-decoupling-plan.md`, add a CONT-26 note) as steps land.

---
