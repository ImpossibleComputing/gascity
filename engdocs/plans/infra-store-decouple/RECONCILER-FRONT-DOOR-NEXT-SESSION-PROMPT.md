# Next-session prompt — reconciler front-door Step 6c/6d (retire raw working set, then cutover)

Paste the block below into a fresh session.

---

Continue the **session reconciler front-door migration** on **PR #3839** (branch
`upstream/object-front-doors-cleanup`, base `main`, DRAFT, worktree
`/data/projects/gascity/.claude/worktrees/object-front-doors`; `git rev-parse HEAD`).

**Read first, in order:**
1. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-STEP6-DESIGN.md` — the
   Step-6 design + backlog. **Read §5 (fable red-team constraints) AND §6 (6b audit
   corrections + landed commits).** READ THIS FIRST.
2. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-HANDOFF.md` — status.
   Steps 0–5 DONE, 6a DONE, **6b substantively DONE**; you are on **6c** (then 6d/6e).
3. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-SPEC.md` — §2 governing
   principle.

**Where things stand.** 6b landed the flippable-in-6b decision-read conversions
(`lifecycleTimerBlocker` `7b5dbc64d`, `isDrainAckStopPending` `9a7bfe650`, template-override
consumers `bd9da510a`, oracle guard `5968a1a32`), all validated by a fable review/red-team
(0 confirmed defects). The reconciler decision path is now ~fully on `Info`; what remains
raw is the frozen forward-pass loop, the wakeTargets loops, and the write-path helpers —
i.e. **6c/6d territory.**

**Confirm a green baseline (use an ISOLATED GOCACHE — the shared cache on this host has a
documented stale-object hazard that flaked the oracle during 6b review):**
```
go build ./... && go vet ./cmd/gc/... ./internal/session/...
golangci-lint run ./cmd/gc/... ./internal/session/...   # expect 0
ISO=$(mktemp -d); GOCACHE=$ISO go test ./cmd/gc/ -run 'TestSessionClassifierInfoEquivalence' -count=3; rm -rf "$ISO"
git checkout go.sum
```

**DO STEP 6c (this session): retire the raw working set — READ-SIDE aggregate consumers
only.** Convert the three aliased consumers off `ordered []beads.Bead`/`beadByID` onto
`infoByID`/ID-lists WITHOUT touching any lockstep: `advanceSessionDrains`,
`clearMissingIdleProbes`, `computeNamedSessionProgressSignatures`,
`openPoolSessionCountForTemplate`, `circuitSessionByIdentity`. **HARD INVARIANT (fable §5):**
`for i := range ordered`, `&ordered[i]`, `beadByID`, the `refreshSessionInfo` raw source,
and EVERY lockstep mirror (`for k,v := range batch { session.Metadata[k]=v }` at ~2130/2191/
2668/2742 and the wakeTargets loop) stay UNTOUCHED until 6d. Keep
`computeNamedSessionProgressSignatures` on per-bead `InfoFromPersistedBead` (its scan @~1300
precedes the snapshot build @~1359 after the CB mutates `ordered[i]`) unless you hoist the
snapshot with a post-CB refresh + oracle.

**Then 6d** (the write-returns-`Info` cutover + lockstep drop — the LANDMINE; re-derive the
COMPLETE 9-refresh-site / ~15-writer set + the store-only-close family + the two intra-tick
overlays `reset_committed_at`-freeze / `restart_requested` per §2 and §5 at implementation
time; NO unconditional per-iteration `Get` on the forward pass) and **6e** (extend
`snapshotInfoOnlyFiles` to forbid raw session `.Metadata[` and add the reconciler files).

**Optional 6d-prep siblings (additive, byte-identical; land only if useful):**
`freshRestartSessionKeyInfo` (reads `SessionIDFlag`/`ResumeFlag`/`ResumeStyle`/`ResumeCommand`
— all already on `Info`, NO codec gap), `recentlyDeferredSessionAttachedConfigDriftInfo`
(pure read), wire the existing `resetPendingCommittedAtInfo`. Their call sites are frozen
(forward pass / write-path) so the sibling lands in 6b-style but the flip is 6d.

**DO NOT** delete the raw classifier siblings (`lifecycleTimerBlocker`,
`isDrainAckStopPending`, `ParseTemplateOverrides`) — they are the oracle's byte-identity
ground truth. **DO NOT** delete `evaluateWakeReasons`/`wakeReasons`/`computeWakeEvaluations`
— they are live (nil-guard fallback + `gc session list`).

**Gates per commit:** `go build ./...` · `go vet` · `golangci-lint ./cmd/gc/...
./internal/session/...`=0 · gofmt · equivalence oracle + whole-tick `TestReconcileSessionBeads*`
+ circuit/named/pool/wake/sleep/drain/trace (heavy suites in the background). **Run the
oracle under an isolated GOCACHE.** `git checkout go.sum` after. Commit AND push
`--no-verify`. Trailer: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
Never `tmux kill-server` / `go clean -cache` (`-testcache` ok); gascity Dolt LOCAL-ONLY (no
`bd dolt push`). #3839 stays DRAFT. Quote grep globs (`--include='*.go'`). Mapping agents
have read the WRONG worktree (`.worktrees/pack-crud`) — pin HEAD, verify line numbers.
Update the handoff + STEP6-DESIGN check boxes + memory (`infra-beads-decoupling-plan.md`).

---
