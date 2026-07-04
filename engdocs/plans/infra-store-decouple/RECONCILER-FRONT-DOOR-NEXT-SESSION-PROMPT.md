# Next-session prompt — reconciler front-door LOCKSTEP DROP: Step 3.5 (wakeTargets / sleep_intent)

Paste the block below into a fresh session.

---

Continue the **session reconciler front-door migration** on **PR #3839** (branch
`upstream/object-front-doors-cleanup`, base `main`, DRAFT, worktree
`/data/projects/gascity/.claude/worktrees/object-front-doors`; run `git rev-parse HEAD`,
expect `0d694acee` or later). We are in the **LOCKSTEP DROP** phase; Steps 1–3 are done and
pushed. **Do Step 3.5 next.**

**Read first, in order:**
1. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-LOCKSTEP-DROP.md` — the focused
   plan. START HERE. Progress checklist at top; remaining consumers #4–#6; the exposure set.
2. `engdocs/plans/infra-store-decouple/raw/lockstep-drop-step{1,2,3}-*.md` — the byte-identity
   analyses + fable-review outcomes for the shipped steps (the method to copy).
3. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-HANDOFF.md` — master backlog.

**Where things stand.** Steps 1 (`ec6127ead`), 2a (`4bcec563b`), 2b (`1d2ea2028`), and 3
(`0d694acee`: `buildAwakeInputFromReconciler` domain → order-preserving `[]session.Info`) are
shipped, fable-reviewed (0 findings), and gate-green. `circuitSessionByIdentity`, `beadByID`,
`sessionLookup` are GONE, and the **awake scan no longer touches any raw session bead** — it takes
`sessionInfos []session.Info`. Still raw: the `ordered []beads.Bead` working set, the forward-pass
`session.Metadata[k]=v` mirrors, and the `wakeTargets` `target.session` reads (Step 3.5).

**Confirm a green baseline (isolated GOCACHE; the FULL cmd/gc suite times out at 600s — use the
reconciler subset; a WARM shared GOCACHE is fine and much faster for re-runs):**
```
go build ./... && go vet ./cmd/gc/... ./internal/session/...
golangci-lint run ./cmd/gc/... ./internal/session/...   # expect 0
go test ./cmd/gc/ -timeout 25m -run 'Reconcile|Awake|Wake|Sleep|Pool|DrainAck|Recycle|Zombie|Heal|Drift|Churn|Stability|RateLimit|Named|Restart|Progress|Rollback|PendingCreate|MinFloor|Idle|MaxAge|Detach|Rebaseline|Relaunch|Quarantine|Circuit|Lifecycle|Session' -count=1   # ~210s, ok
git checkout go.sum
```

**DO — Step 3.5: the `wakeTargets` / `sleep_intent` raw reads + mirror (consumer #4).**
`target.session` is a raw `*beads.Bead` carried on `wakeTarget` (`session_reconciler.go:36`) — a
**DIFFERENT source than `ordered`** (a pointer into `ordered`, so `infoByID[target.session.ID]` IS the
coherent snapshot for it). Re-grep; current sites:
- `compute_awake_bridge.go:168` — the `wakeTargets` loop reads `target.session.Metadata["session_name"]`.
- `compute_awake_bridge.go:195/199/202/204` — `shouldProbeAttachmentForAwakeInput` reads `state`,
  `detached_at`, `template` raw (`normalizedSessionTemplate(*target.session, cfg)`).
- `session_reconciler.go:3195-3197` — the **mirror**: `sleep_intent == "idle-stop-pending"` →
  `SetMarker(id,"sleep_intent","")` (store) + `target.session.Metadata["sleep_intent"] = ""` (raw clear).
- `session_reconciler.go:3203` reads `intent := target.session.Metadata["sleep_intent"]` right after;
  `:3232` uses it; `:4372` reads `sleep_intent != ""` later.
- **Read path:** convert the reads to `Info` via `infoByID[target.session.ID]`
  (`.SessionNameMetadata`/`.SleepIntent`/`.State`/`.DetachedAt`/`.Template`) — `Info.SleepIntent` exists
  (`b.Metadata["sleep_intent"]`, raw). `shouldProbeAttachmentForAwakeInput`/`normalizedSessionTemplate`
  take `*beads.Bead` today; give them `Info` siblings (like `normalizedSessionTemplateInfo` already
  exists) OR pass the Info in.
- **Mirror path (the careful bit):** the `:3197` raw clear (`Metadata["sleep_intent"]=""`) is a
  lockstep write, and `:3203`/`:4372` read `sleep_intent` off the same session SAME-TICK. Fold the clear
  onto `infoByID` (write-returns-Info: `infoByID[id] = infoByID[id].ApplyPatch({"sleep_intent":""})`)
  and route the later reads onto `Info.SleepIntent`, so the store write + same-tick read convert as ONE
  unit (governing principle). The store `SetMarker` write is unchanged.
- **CAUTION:** `wakeTargets` may carry a session whose bead is NOT in `ordered`/`infoByID` (e.g. built
  after a mid-tick close/eviction). Re-verify every `target.session.ID` keys `infoByID` before dropping
  the raw read; if not, either add a fallback `InfoFromPersistedBead(*target.session)` for that path or
  keep it raw and scope it out (document why). Prove byte-identity per the analyses.

**Then Steps 4 (`newSessionBeadSnapshot` off a store source — hardest, consumer #5), 5 (drop the
remaining mirrors + `ordered` + the dead `sessionBeads` param + cut the tick-start build to the store),
6e (extend the `snapshotInfoOnlyFiles` guard).** LOCKSTEP-DROP.md has the detail.

**Discipline (unchanged):** convert each consumer + verify BEFORE deleting its raw source; the
comprehensive reconciler subset is the byte-identity gate (a wrong conversion flips an awake/drain
decision and fails). Run a **fable adversarial review per non-trivial step** (owner prefers fable, not
opus) — a 4-lens + verify shape (the Step-3 review script is at
`.claude/.../workflows/scripts/step3-awake-domain-review-*.js`; reuse via `{scriptPath}`). Subagents
are fine for mechanical test-site conversions. Gates per commit: build · vet · golangci-lint 0 · gofmt ·
the reconciler subset. `git checkout go.sum` after. Commit AND push `--no-verify`. Trailer:
`Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. Never `tmux kill-server` /
`go clean -cache` (`-testcache` ok). gascity Dolt LOCAL-ONLY (no `bd dolt push`). #3839 stays DRAFT.
Update LOCKSTEP-DROP.md + memory (`infra-beads-decoupling-plan.md`, add a CONT-27 note) as steps land.

---
