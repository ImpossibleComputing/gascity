# S19 Stage 2 review notes — simplify-land/s19-stage2 (adversarial)

Reviewer walk of `git diff origin/main..simplify-land/s19-stage2`.

## Immediate observations (first 1080 diff lines)

- FLAG: diff includes `.github/workflows/ci.yml` + `Makefile` changes REMOVING the
  dashboard `typecheck:test` step. Unrelated to S19 Stage 2 spec. Need to check
  whether this is merge-base skew (origin/main moved) or an actual out-of-scope
  change on the branch.
- A2 adoption reorder (adoption_barrier.go): derivation moved below resolution;
  hand-stamps replaced by resolvedAgentName/resolvedSlot. Need to verify:
  - orphan arm passes ConfigResolved=false → no canonical record. OK in code.
  - BUT: orphan arm passes AgentName=sessionName now via derivation instead of
    hand-stamp — same value, OK.
  - Slot switch: resolvedSlot set only in the `slot > 0 && isConfigAgent &&
    SupportsInstanceExpansion` arm — matches old `meta["pool_slot"]` stamp site.
  - Check: stale-dash-N singleton arm (first switch case) — old code? Need to
    diff against origin/main to confirm no arm behavior change (continue vs stamp).
- A1 syncSessionBeads: PoolSlot passed + ConfigResolved=true; manual
  `meta["pool_slot"]` stamp deleted; session_name pending-pool hand-stamp kept.
  Matches spec. Need to confirm `strconv` import still used in session_beads.go.
- desiredSessionIdentity: canonical pair gated on ConfigResolved && AgentName!="".
  Matches emit rules. Stage-1 rows unchanged (existing test rows kept).
- B0: templateParamsToConfigWithDelivery sibling + wrapper; promptDelivered =
  Delivered && (firstStart || forceFresh || !hasResumeKey). Matches spec.
  Trap test pins resume row false + env marker still "1".
- B1 commitStartResultTraced + B2 recoverRunningPendingCreate: gated zero-values,
  PrimedAt/PromptHash added to CommitStartedPatchInput. Matches.
- C-sites seen so far: C-3 chat.go (loop of SetMetadata per key — note: origin
  used individual SetMetadata calls already, so same idiom; mirror updated),
  C-4 clearStaleResumeKeyMetadata, C-5 session_beads reopen batch, C-6
  session_reconcile heal batch, C-2 ConversationResetPatch (inside if), C-1
  freshWakeConversationResetKeys list. Need to see applyFreshWakeConversationReset
  side + alignment test.
- D: Info gains two verbatim mirrors; accessor pure; oracle corpus extended with
  edge patches incl. stray slot. Matches D2 refinement.

## Resolved checks

- ci.yml/Makefile "removals" = MERGE-BASE SKEW, not branch content. Branch is one
  commit 8e435bed6 off 7efe9935f; origin/main tip 0ec2df88e added typecheck:test
  after the fork. True branch diff (7efe9935f..branch) = 23 files, all S19.
  NOT a finding; but the branch needs a rebase before merge (trivial, no overlap).
- S2-1 write-only gate PASSES: grep over branch for the 5 keys/constants — every
  non-test hit is a write (stamp or clear), the projection files, or doc comments
  (session_level_converge.go Stage-1 comments unchanged; deriveConvergeActions
  still has no production caller). internal/api untouched; no wire exposure.
- A2 reorder equivalence vs merge base: verified line-by-line. All three arms
  assign resolvedAgentName exactly the values previously hand-stamped; slot only
  in the expansion arm; stale-dash-N arm keeps slot 0 (canonical name w/o slot,
  intended); orphan arm ConfigResolved=false -> no canonical record but same
  agent_name=sessionName. detail/log/dryRun ordering unchanged. Meta built
  pre-dryRun in both. InstanceToken timing move is a no-op (random token).
- A1: PoolSlot passed + ConfigResolved=true; manual pool_slot stamp deleted;
  emission condition identical (PoolSlot>0). pending-pool session_name hand-stamp
  kept. Byte-identical modulo the two added canonical keys.
- B0/B1/B2: promptDelivered = Delivered && (firstStart||forceFresh||!hasResumeKey),
  exact complement of the resume override; env choreography untouched (test pins
  env marker still "1" on resume). CommitStartedPatch emits both-or-neither
  (!PrimedAt.IsZero() && PromptHash != ""). B2 caveat documented as spec'd.
- D2: verbatim mirrors + pure accessor + shared canonicalIdentityFrom helper;
  oracle corpus extended (keys + 5 edge patches incl. stray slot). Green.
- Tests run on branch worktree /tmp/s19s2-review:
  - go vet ./internal/session/ ./cmd/gc/ clean.
  - go test -race ./internal/session/ PASS (129s).
  - Targeted cmd/gc: TestDesiredSessionIdentity (8 rows incl. 4 new),
    TestAdoptionBarrier*, TestPreparedStartPromptDelivered (5 rows incl. trap),
    TestDeriveFirstStart*, TestDeriveConvergeActions*, TestPromptDelivery,
    TestSyncSessionBeads*, worker-boundary guard — all PASS.
  - Full ./cmd/gc/ suite running (bhn854kja).

## FINDINGS

### F1 (medium) — RestartRequestPatch is a 7th started_config_hash clear site; priming markers NOT cleared there
`internal/session/lifecycle_transition.go` `RestartRequestPatch` (line ~461 on
branch, 386 on merge base) writes `"started_config_hash": ""` but does not clear
primed_at/priming_attempted_at/prompt_hash. The spec's C-inventory claimed
"every non-test writer on origin/main" and listed 6 — it missed this one, and
the branch faithfully implements the incomplete list. This violates the stage's
own normative rule (S2-4: markers cleared exactly where the hash clears) and
plants exactly spec risk #5: a controller restart handoff forces the next wake
onto the first-start path while the PREVIOUS conversation's primed_at/prompt_hash
survive; if config is unchanged the stale prompt_hash still matches the current
rendered prompt, so a Stage-4 eligibility check (marker present + hash match)
would wrongly treat the fresh conversation as primed — #3872-3 with durable
camouflage. Zero behavior change in Stage 2 (nothing reads the keys), but the
whole point of the stage is leaving durable state that Stage 4 can trust.
FIX: one line (`clearPrimingMarkers(patch)` in RestartRequestPatch) + extend the
C-rule test; ideally add the repo-wide grep-form test the spec's test 6 promised
(the unit tests only cover C-1..C-3 mechanically, which is how this slipped).

### F2 (low) — A3 canonical stamp can be overwritten by identity.Metadata loop
`cmd/gc/session_name_lookup.go:268-271`: canonical keys are stamped BEFORE the
`for key, value := range identity.Metadata` copy loop. A caller-supplied
identity.Metadata entry named canonical_instance_name/canonical_pool_slot would
overwrite the config-resolved record (S2-3 honesty; Stage-5 time bomb class).
Pre-existing exposure for agent_name/pool_slot has the same shape, so this is
not new risk introduced, but the canonical record is supposed to be the ONE
trustworthy identity. Moving the two stamps below the loop (or skipping the two
keys in the loop) closes it. Nit-level today.

### F3 (info) — test-plan deltas vs spec
- Spec test 8 asked for golden FULL-map assertions per adoption arm (pool,
  singleton, stale-dash-N, orphan) and for the A1 create block. Branch adds
  canonical-key-only assertions for pool + orphan arms
  (TestAdoptionBarrier_StampsCanonicalIdentity); singleton and stale-dash-N arms
  rely on the pre-existing suites (TestAdoptionBarrier_StaleDashNSingleton*)
  which pin agent_name/pool_slot but not full maps. Equivalence was instead
  verified by review (see A2 notes) + existing suites green. Acceptable but
  weaker than spec'd.
- Spec test 10 (B2 recovery stamps pair / empty prompt stamps nothing): no
  direct test of recoverRunningPendingCreate stamping found; covered only
  indirectly via TestPreparedStartPromptDelivered + CommitStartedPatch units.
- C-4/C-5/C-6 (cmd/gc clear sites) have no dedicated new unit test rows; the
  clears ride existing suites (e.g. session_wake trace test extended).

### F5 (HIGH) — reconciler suite RED: TestHealStatePatchProjectsRuntimeLiveness not updated for the C-6 clear
`make test-cmd-gc-process-parallel` on the branch worktree: shard 2-of-6 FAILS.
`cmd/gc/session_reconcile_test.go:1990` `TestHealStatePatchProjectsRuntimeLiveness`
subtests "stale_creating_heals_to_asleep_and_resets_stale_resume_identity" and
"stale-creating_with_stale_pending_create_claim_heals_to_asleep_and_clears_claim"
pin the exact healStatePatch map; the C-6 edit (session_reconcile.go:1247-1249)
now adds primed_at/priming_attempted_at/prompt_hash="" to that batch and the
expected maps were never extended. got includes the 3 keys, want doesn't.
All other shards ok (1,3,4,5,6). The earlier single-process `go test ./cmd/gc/`
run died on go test's default 10m timeout (package needs the sharded target;
serial total >20min) — not a hang.
CONSEQUENCE: the stage's core proof obligation (c) "full reconciler suite green"
is NOT met as landed. Fix is mechanical (+3 rows in two want maps), but the
branch is red and the no-behavior-change proof is incomplete until it runs green.

### Verdict
needs-rework. Two required changes:
1. F5: update TestHealStatePatchProjectsRuntimeLiveness want maps; re-run the
   cmd/gc shard suite to green.
2. F1: add clearPrimingMarkers to RestartRequestPatch (7th clear site the spec
   inventory missed) + test row; otherwise S2-4 is violated and spec risk #5
   (stale primed_at camouflage at Stage 4) is landed.
Optional: F2 (A3 stamp ordering vs identity.Metadata loop), F3 test-plan gaps.
Everything else verified equivalent: write-only gate passes, A1/A2 maps
byte-identical modulo added keys, B0 trap correctly avoided, D2 oracle-sound,
internal/session -race green, targeted parity pins green, API/wire untouched.

### F4 (info) — spec base-ref note
Spec says fork from post-#4034 main; branch forks from 7efe9935f (post-#4038,
newer). Fine. Rebase onto 0ec2df88e needed at merge time (no file overlap).

