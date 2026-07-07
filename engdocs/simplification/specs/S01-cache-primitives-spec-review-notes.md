# S01 review notes — branch simplify-land/s01-full vs origin/main

Reviewer: adversarial pass, 2026-07-07.

## Finding 0 (scope) — CORRECTED: branch scope is clean

The two-dot diff (`origin/main..branch`) showed 32 files, but that is
because origin/main advanced past the merge-base (b1436f0e5; S26 landed
on main as 65899d317). The true branch content
(`git diff origin/main...branch --stat`) is exactly 6 files, all
internal/beads (5 production + 1 new test file), +550/-184. The two-dot
and three-dot diffs for internal/beads/ are byte-identical (verified with
diff-of-diffs), so the site walk below is against the real change.
NOTE for landing: the branch must be rebased onto current main before
merge; no conflict analysis done here.

## Finding 1 (MAJOR, scope/completeness) — "one merge algorithm" NOT delivered

The item is "absorb/evict primitives + one merge algorithm". The diff in
`caching_store_reconcile.go` is 21 lines: only the Branch-A absorb (A4) and
Branch-A evict (E1) are rewritten onto the primitives. There is:
- NO deletion of Branch B (the rebuild path survives untouched),
- NO `reconcileMergeDecision` pure function,
- NO D1 deletedSeq GC pass, NO D3 dirty/beadSeq/localBeadAt GC pass,
- prime() fast path (A3) still whole-map rebuilds (allowed for Phase 1, but
  the spec's claim "zero non-primitive writers" is only true after Phase 2/3).

So the branch is spec Phase 1 ONLY despite the name `s01-full`. Phase 2
(the collapse, T3 differential gate, D2 Julian sign-off) and Phase 3 are
absent. The #1 review question "does the collapsed merge loop match the old
branches" is unanswerable — there is no collapsed loop.

## Finding 2 (test plan mostly unimplemented)

New test file `caching_store_primitives_test.go` (265 lines) ≈ T1 only
(primitive unit grid, partial: does not cover tombstoned/recent-local/
stale-local pre-states per-opts-combination as a full grid, but covers each
axis once). MISSING per spec: T2 (per-site conformance via public entry
points), T3 (differential corpus — moot without Phase 2 but spec Phase 0
required it BEFORE Phase 1 as the net), T4 (moot), T5 hammer -race test,
T6 (ApplyEvent OC-3 ordering regression), T7 (seqKeep divergence guard via
the real event sites). Spec Phase 0 ("pin current behavior FIRST") was
skipped — Phase 1 landed without the pinned-behavior net.

## Site-by-site equivalence walk (beads diff)

- A1 PrimeActive: equivalent (order of deletes shuffles under held lock;
  clearDirty:false preserved; depsExplicit/cloneDeps preserved). OK.
- A2 prime slow path: equivalent; adds one defensive clone (P1-allowed);
  seqClearBeadSeqOnly correct; clearDirty:false correct. OK.
- A4 reconcile absorb: byte-equivalent. Notification classification still
  pre-mutation (OC-4 held). OK.
- E1/E2/E3 evicts: byte-identical 6 deletes -> evictLocked. OK.
- R1/R2/R3: equivalent; R2/R3 gain a defensive clone (P1-allowed). OK.
- R4 Get dirty-refresh: depsFromFields unconditional + seqClearBeadSeqOnly
  + localBeadAt untouched — preserved. OK.
- W1/W2/W5/W13/W14/W15: seqKeep after noteLocalMutationLocked — correct
  per the #2210 warning; clearDirty matches old per site. OK modulo
  ORDER-SWAP question below.
- W3: absorb(clearDirty:false) + markDirtyLocked after — end-state equals
  old (dirty set, deletedSeq cleared). OK modulo order-swap.
- W6: absorb(depsKeepCached, clearDirty:false)+markDirty — matches old
  (old never wrote deps, set dirty, deleted deletedSeq). OK.
- W8/W9/W10: depsKeepCached preserved (Close keeps deps). OK.
- W11 CloseAll: depsDrop only when Status==closed — preserved; notification
  classification uses `previous` captured pre-absorb. OK.
- W16/W17 DepAdd/DepRemove refreshed: ORDER SWAP — old did
  clearReadyProjectionLocked BEFORE delete(dirty)/delete(deletedSeq); new
  absorbs (deletes both) BEFORE clearReadyProjectionLocked. Need to verify
  clearReadyProjectionLocked reads neither dirty nor deletedSeq.
- EV1/EV3: ORDER SWAP — old ran updateEventDepsLocked BEFORE the
  dirty/deletedSeq deletes; new absorbs (deletes both) then
  updateEventDepsLocked. Spec predicted this and demanded the reviewer
  "assert updateEventDeps reads neither dirty nor deletedSeq" (EV3 note).
  Verifying below.
- W2: same class — clearDependentReadyProjectionsLocked now runs AFTER
  dirty/deletedSeq deletes (old: before). Verify it reads neither.
- E4/E5/E6 tombstones: delete-then-set == old overwrite; seq sources
  identical (noteLocalMutation return / c.mutationSeq post-noteMutation). OK.
- EV5/EV6: byte-identical via markDirtyLocked/clearStalenessMarksLocked. OK.

## Order-swap verification (the equivalence linchpin)

Read the branch-tip bodies of the four helpers the swapped orderings
interact with:
- `clearReadyProjectionLocked` (events.go:363): reads/writes ONLY
  `c.beads[id].IsBlocked`.
- `clearDependentReadyProjectionsLocked` (:387): reads `deps`, `beads`,
  `depsComplete`; writes other rows' IsBlocked + `noteMutationLocked` on
  cleared rows. Never reads `dirty`/`deletedSeq`.
- `updateEventDepsLocked` / `setEventDepsLocked` (:285/:329): read/write
  `c.deps`, `depsComplete`, call clearReadyProjection. Never read
  `dirty`/`deletedSeq`.

Therefore the EV1/EV3 (deletes now before updateEventDepsLocked), W2/W16/
W17 (deletes now before projection clears) order swaps are equivalent
under the held lock. OC-3 (absorb installs the row before the overlay,
which must observe it) is preserved — beads write still precedes
updateEventDepsLocked at EV1/EV2/EV3.

## Branch-A tip re-read (reconcile)

Guards in OC-2 order (deletedSeq > startSeq → beadSeq > startSeq →
recency, with the depsComplete degradation on both skip arms), OC-4
classification (beadChanged/depsChanged against PRE-absorb state) before
`absorbFreshLocked`, missing-row loop guards + notification synthesis
before `evictLocked`. `nextDepsComplete` handling unchanged. Equivalent.

## Finding 3 (MAJOR) — missed site: refreshGraphAppliedBeads

`internal/beads/caching_store_graph_apply.go:89-102` (byte-identical on
main and branch) still hand-writes the maps:
`c.beads[item.id]=fresh; c.deps[item.id]=cloneDeps(...); delete(c.dirty);
delete(c.deletedSeq)` on the found arm and `c.dirty[item.id]=struct{}{}`
on the error arm. This is a full absorb-shaped site (write path, post-
noteLocalMutationLocked → should be absorb{depsExplicit, seqKeep,
clearDirty:true} + markDirtyLocked) that BOTH the spec's site enumeration
and the migration missed. Behavior is unchanged (zero-diff), but the
central S01(a) contract — "absorb/evict become the ONLY writers of
row-lifecycle state" — is not established. The doc comment on
absorbFreshLocked ("the only code that installs a cached row alongside
clearing tombstone/staleness state") is false as merged.

## Writers census at branch tip (non-test)

Remaining row installs: absorbFreshLocked itself (:423); prime() fast
path rebuild (:663/:672 — A3, allowed in Phase 1); clearReadyProjection
overlay (events.go:369 — enumerated non-goal); graph_apply (:92 — MISSED,
Finding 3); reconcile Branch B (:463/:492 + whole-map swap :515-521 —
Phase 2 never happened, Finding 1). Bare dirty/deletedSeq writers outside
primitives: only graph_apply. deps-overlay writers match the spec's
non-goal list.

## Verification actually run (reviewer)

- `go vet ./internal/beads/` on branch tip: clean.
- `go test -race -count=1 ./internal/beads/` on branch tip (detached
  worktree /tmp/s01-review): ok, 8.451s, no races. This is the whole
  existing ~6.8k-LOC white-box suite (UNMODIFIED by the branch) + the new
  T1 primitives tests. No evidence in the branch that the author ran
  -race; no T5 hammer test exists, so race coverage is only what the
  pre-existing concurrency tests exercise.
- Clone-direction audit: every site either kept its clone or GAINED a
  defensive clone via the primitive (A2, R2, R3, W6, W8-cached, W15,
  ReleaseIfCurrent fallback). No site lost a clone. P1 satisfied.
- seqMode audit per spec table: every write site (W1-W17) and event site
  (EV1-EV3) uses seqKeep after its note call — the #1-risk misassignment
  (seqClearGuarded at an event site) did NOT happen. A1/A4/R1-R3 use
  seqClearGuarded; A2/R4 use seqClearBeadSeqOnly. Exactly the spec table.
- clearDirty audit: false only at A1, A2, W3, W6 — matches old behavior
  (W13/W14 fallbacks correctly true).

## Finding 4 (minor) — commit message overclaims

Single squash commit 9522a7c3b is titled "beads cache absorb/evict
primitives + unified merge". There is no unified merge in the diff. If
this lands as-is the history will claim a collapse that never happened,
and a future reader will assume Branch B is gone.

## Finding 5 (minor) — per-absorb time.Now() calls added

Event/write sites now pass `time.Now()` per absorb where seqKeep ignores
it (EV1-EV3, W1, W2, ...). Pure cost, negligible; would vanish if the
sites passed the pass-level `now` they mostly already have.

## Verdict

needs-rework. What landed (Phase 1 substitution) is demonstrably
behavior-preserving — every substituted site verified equivalent, order
swaps proven safe by reading the helper bodies, -race clean under the
existing suite. But: (1) the item's second half ("one merge algorithm")
is entirely absent while the commit claims it; (2) the graph_apply site
was missed, so the primitives' exclusivity contract — the point of the
refactor — is not established and its doc comment is false; (3) the
spec's test plan is ~15% delivered (T1-lite only; no Phase 0 pinning
net, no T2 per-site conformance, no T5 hammer, no T6/T7 named guards for
the top-ranked risk). Rework: migrate graph_apply, add T7 (+T6), retitle
the commit to Phase-1 scope (or actually deliver Phase 2 behind the T3
differential gate), rebase onto current main.

