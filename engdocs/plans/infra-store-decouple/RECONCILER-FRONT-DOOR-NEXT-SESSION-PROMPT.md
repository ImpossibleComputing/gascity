# Next-session prompt — reconciler front-door Step 6d (the cutover) / 6e (join the guard)

Paste the block below into a fresh session.

---

Continue the **session reconciler front-door migration** on **PR #3839** (branch
`upstream/object-front-doors-cleanup`, base `main`, DRAFT, worktree
`/data/projects/gascity/.claude/worktrees/object-front-doors`; `git rev-parse HEAD`).

**Read first, in order:**
1. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-STEP6-DESIGN.md` — the
   Step-6 design + backlog. **Read §2 (intra-tick model + why the Get-cutover is
   write-returns-`Info`, not a blanket `Get`), §5 (fable red-team constraints — the
   9-refresh-site set, ~15 nested-helper writers, the restart_requested overlay
   lifecycle, the store-only-close family), AND §7 (6c execution + the 6d
   carry-forward landmines).** READ THIS FIRST.
2. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-HANDOFF.md` — status.
   Steps 0–5 DONE, 6a/6b/6c DONE; you are on **6d** (then 6e).
3. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-SPEC.md` — §2 governing
   principle (never drop a lockstep before its same-tick reads are on the snapshot).

**Where things stand.** The reconciler decision path is fully on `Info`. 6b landed the
flippable decision-read conversions; **6c** (`3b7795598`) converted the sole remaining pure
read-side raw-working-set consumer (`clearMissingIdleProbes`→`infoByID` presence,
byte-identical), verified by an opus audit + a 4-lens fable adversarial panel (0 defects).
What remains raw is exactly the **write/lockstep machinery**: the forward-pass loop
`for i := range ordered`/`&ordered[i]`, the CB persist, the blanket refresh pre-pass @2774,
the wakeTargets loop, `sessionLookup`→drain mutations, and `refreshSessionInfo`'s raw
source. Removing all of it is **6d — the LANDMINE cutover.**

**6d foundation is LANDED (`b031a356d`) and the mechanism is OWNER-LOCKED = write-returns-`Info`.**
`Info.ApplyPatch(patch) Info` (internal/session/info_apply_patch.go) folds a metadata patch
onto a projected `Info` byte-identically to a full re-projection (oracle
`TestInfoApplyPatchMatchesReprojection`, normalizer-branch coverage mutation-verified; a
3-lens fable panel found 0 impl defects). It is UNWIRED. **The authoritative, verified
per-site wiring plan for what remains is STEP6-DESIGN §8 — read it first; it supersedes the
generic mechanism notes below.** Key §8 findings: (a) under write-returns-`Info` the snapshot
only ever receives MIRRORED batches, so the `reset_committed_at` freeze overlay is UNNEEDED
(only `restart_requested`, the in-memory-only write @~2130, still needs an ApplyPatch + a
clear-on-persisted); (b) every refresh site is a nested-helper-write (thread the batch out of
the helper) — there is NO by-construction status-close shortcut (the close helpers stamp a
ClosePatch too), and the store-only closes (`closeFailedCreateBead`/`rollbackPendingCreate`)
are markClosed-ONLY (they don't mirror the raw bead, so the current raw reproject doesn't see
their metadata either).

**Confirm a green baseline (use an ISOLATED GOCACHE — the shared cache on this host has a
documented stale-object hazard that flaked the oracle during 6b review):**
```
go build ./... && go vet ./cmd/gc/... ./internal/session/...
golangci-lint run ./cmd/gc/... ./internal/session/...   # expect 0
ISO=$(mktemp -d); GOCACHE=$ISO go test ./cmd/gc/ -run 'TestSessionClassifierInfoEquivalence' -count=3; rm -rf "$ISO"
git checkout go.sum
```

**DO STEP 6d (this session — the LANDMINE cutover).** Drop the raw lockstep, remove the raw
working set (`ordered []beads.Bead` / `beadByID` / `circuitSessionByIdentity` /
`sessionLookup`), and cut `refreshSessionInfo` off the raw bead — done as a sequence of small
per-lockstep commits, each with a **multi-session / read-after-write same-tick test** (the
byte-identical write oracle is BLIND to same-tick stale reads — SPEC §2). Do NOT do it as one
mass edit. Governing constraints, all specified in STEP6-DESIGN §2/§5/§7 — re-derive them
against live line numbers at implementation time:

- **Mechanism = write-returns-`Info` for adjacent-single-write refreshes + a TARGETED store
  re-read for the status-close and aggregating refreshes** (`Info.Closed` derives from
  `Status`, not a metadata key, so a returned patch cannot reconstruct it). **NO unconditional
  per-iteration `Get` on the forward pass** — the refresh @~1854 is unconditional and a `Get`
  there consumes the injected attachment-check errors (3 fail-safe tests: session_reconciler_test.go
  :7661,:7833; session_reconciler_progress_test.go:202) → the reverted-attempt failure returns.
- **Before deleting the blanket pre-pass @~2774, regenerate the COMPLETE forward-pass writer
  set** that writes an `Info`-read key without a per-site refresh (§5 lists ~15 writers, many
  2–3 helper layers deep → add a "nested-helper-write" bucket; `SleepPatch`@2631/@2705,
  `RestartRequestPatch`@2144, the 1424 drain-ack finalize, etc.).
- **Two intra-tick overlays:** `reset_committed_at` freeze-to-tick-start-value at snapshot
  build; `restart_requested` in-memory overlay set @~2098 that must CLEAR whenever a persisted
  batch carrying `restart_requested` lands (else #2574 phantom-restart) — needs a
  kill-success-then-refresh test asserting it reads empty.
- **Store-only-close family** (`closeFailedCreateBead`, `rollbackPendingCreate`): their close is
  masked by `Info.Closed` eviction from `AwakeInput.SessionBeads` — 6d must bless that eviction
  as its own tested commit.

**6d carry-forward from the 6c audit (STEP6-DESIGN §7):** the `ordered` domain params on
`openPoolSessionCountForTemplate` (safe domain-switch — unique IDs proven) and
`buildAwakeInputFromReconciler` (**NOT** safe — `input.SessionBeads` slice order is load-bearing
for `SessionName`-keyed last-write-wins in `ComputeAwakeSet`; keep the ordered domain) and
`advanceSessionDrains` (dead `ordered` param in the prod call) all retire WITH the working set,
not before. The derived `wakeTargets` aggregate keeps a raw `*beads.Bead` for the
`persistSleepPolicyMetadata` write @~2853.

**Then 6e** (extend `snapshotInfoOnlyFiles` in frontdoor_di_guard_test.go to forbid raw session
`.Metadata[` and add the reconciler files once raw-free).

**Optional 6d-prep siblings (additive, byte-identical; land only if useful):**
`freshRestartSessionKeyInfo` (reads `SessionIDFlag`/`ResumeFlag`/`ResumeStyle`/`ResumeCommand`
— all already on `Info`, NO codec gap), `recentlyDeferredSessionAttachedConfigDriftInfo`
(pure read), wire the existing `resetPendingCommittedAtInfo`. Their call sites are frozen
(forward pass / write-path) so the sibling lands in 6b-style but the flip is part of 6d.

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
