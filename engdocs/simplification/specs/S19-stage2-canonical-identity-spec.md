# S19 Stage 2 — Durable Canonical-Identity Schema (Write-Only) — Implementation Spec

Status: READY FOR IMPLEMENTATION
Backlog item: S19s2
Parent spec: engdocs/simplification/specs/S19-level-triggered-convergence-spec.md (Stage 2)
Predecessor: Stage 1 MERGED (#4034, pure deriveConvergeActions cores)

## Target design

**Base ref:** `origin/main` AFTER #4034 (`c6b851ac1`). NOT this audit branch —
`simplify/audit-2026-07-07` predates the Stage-1 merge; the Stage-2 branch
must fork from post-Stage-1 main so `desiredSessionIdentity`
(`cmd/gc/session_identity.go`), `promptDelivery` (`cmd/gc/prompt_delivery.go`),
and `deriveConvergeActions` (`cmd/gc/session_level_converge.go`) already exist.

**What Stage 2 adds (four deliverables, all write-side, all dormant):**

1. **`internal/session/canonical_identity.go`** — cherry-adapt from
   `simplify/s19-c` (NOT a branch merge; the spike diff reverts newer main
   work — sleep-reason typed constants, `canonicalLifecycleState`,
   `sessionMatchesFilters` sharing — that must NOT be touched). Take only:
   - `CanonicalInstanceNameMetadata = "canonical_instance_name"` and
     `CanonicalPoolSlotMetadata = "canonical_pool_slot"` key constants.
   - `CanonicalIdentity{QualifiedInstanceName string; PoolSlot int; Present bool}`.
   - `CanonicalIdentityFromMetadata(meta map[string]string) CanonicalIdentity`:
     pure, reads exactly the two keys; record exists iff
     `TrimSpace(name) != ""`; slot parse via `parseCanonicalSlot` (missing/
     non-numeric/non-positive ⇒ 0 = unslotted/singleton).
   - `canonical_identity_test.go` from the spike, adapted.

2. **`Info.CanonicalIdentity` projection** — additive field on
   `session.Info` (`internal/session/manager.go` Info struct) plus one line in
   `InfoFromPersistedBead` (`internal/session/info_store.go`):
   `info.CanonicalIdentity = CanonicalIdentityFromMetadata(b.Metadata)`.
   Internal-only, absent from the HTTP wire (R2), and **deliberately NOT
   folded by `ApplyPatch`**: a canonical-identity heal is a rare joint
   two-key write refreshed by re-projecting the bead; a per-key fold of a
   jointly-derived record is order-dependent and cannot round-trip. The field
   doc comment must state this, and `TestInfoApplyPatchMatchesReprojection`
   must remain green (see Test plan for how).

3. **Stamp the canonical record at create + adoption** — extend
   `sessionIdentityInputs`/`desiredSessionIdentity` (Stage-1 pure core) to
   also emit `canonical_instance_name` (= the qualified instance name the
   path already computes) and `canonical_pool_slot` (= `PoolSlot` when > 0),
   with the same emit-only-when-meaningful rule the Stage-1 keys use, so the
   record lands wherever the derivation is already wired (the
   `syncSessionBeads` create block and the adoption barrier).

4. **Fold priming markers into `CommitStartedPatch`** — new optional fields
   on `session.CommitStartedPatchInput` (`internal/session/
   lifecycle_transition.go`): `PrimedAt time.Time` + `PromptHash string`
   (zero values ⇒ keys not emitted). The two cmd/gc call sites
   (`session_lifecycle_parallel.go` :1942/:2151 region) pass them when
   `promptDelivery(...).Delivered && start succeeded`. Clear the three
   priming keys (`primed_at`, `priming_attempted_at`, `prompt_hash`) at the
   existing `started_config_hash` clear sites ONLY (M3/§2.2 lifecycle of the
   parent spec): `clearStaleResumeKeyMetadata` and the pending-create reset.
   `priming_attempted_at` is defined in Stage 2 (constant + clear sites) but
   never written — the write-ahead attempt path is Stage 4's awake-scan work.

**Hard constraint (the stage's identity):** Stage 2 is WRITE-ONLY. No reader
— no ladder, no reconciler decision, no priming decision, no API projection
consumer — consults `CanonicalIdentity`, `primed_at`, `priming_attempted_at`,
or `prompt_hash`. All new metadata is additive and invisible to every decision
path; `Info.CanonicalIdentity` is computed but dormant (like Stage 1's
`deriveConvergeActions`, which only tests exercise). Therefore Stage 2 is
behavior-preserving by construction, and the proof obligations are (a) the
new writes land at exactly the specified sites, (b) nothing reads them,
(c) the full reconciler suite + Stage-1 parity pins stay green.

**Key-ownership table after Stage 2:**

| key | writer(s) | reader(s) |
|---|---|---|
| `canonical_instance_name` | create stamp, adoption stamp | none (Stage 5 cuts reads over) |
| `canonical_pool_slot` | create stamp, adoption stamp | none |
| `primed_at` | `CommitStartedPatch` (launch-confirmed only) | none (Stage 3 shadow, Stage 4 acts) |
| `prompt_hash` | `CommitStartedPatch` | none |
| `priming_attempted_at` | none in Stage 2 (cleared only) | none |
| `started_config_hash` | unchanged (`CommitStartedPatch` + existing clears) | unchanged (C3: identity/priming code never writes it) |

## Current behavior (site-by-site enumeration)

All references are `origin/main` post-#4034. Sites are grouped: (A) identity
stamp sites, (B) start-commit sites, (C) `started_config_hash` clear sites
(the priming-key lifetime rule binds to these), (D) projection surfaces.

### A. Identity stamp sites (where the canonical record is added)

**A1 — `cmd/gc/session_beads.go:1157` (syncSessionBeads create block).**
Today: `meta := desiredSessionIdentity(sessionIdentityInputs{AgentName:
agentName, State: createState, Generation, ContinuationEpoch, InstanceToken})`
where `agentName` is `tp.TemplateName`, overridden to `tp.InstanceName` for
pool/canonical-singleton instances (`:1073-1082`) — i.e. already the qualified
instance name. `PoolSlot` is NOT passed; the block hand-stamps
`meta["pool_slot"] = strconv.Itoa(poolSlot)` + pending pool `session_name`
under `if poolSlot > 0` (`:1207-1210`).
New form: pass `PoolSlot: poolSlot` into the derivation and delete the
now-duplicate manual `meta["pool_slot"]` line (the derivation emits it —
final map byte-identical); the derivation additionally emits
`canonical_instance_name = AgentName` and `canonical_pool_slot = PoolSlot`
per the emit rules below. The `session_name = pendingPoolSessionName(...)`
hand-stamp stays (it is not an identity-derivation output in Stage 2).

**A2 — `cmd/gc/adoption_barrier.go:174` (adoption barrier).**
Today: calls `desiredSessionIdentity` EARLY with `AgentName` empty and no
slot, then hand-stamps `meta["agent_name"]` in three arms (pool instance
`instanceName = QualifiedName()-slot`; singleton `QualifiedName()`; orphan
fallback `agent_name = sessionName`) and `meta["pool_slot"]` in the
pool-with-instance-expansion arm only (`:174-226`).
New form: move the `desiredSessionIdentity` call BELOW the existing
pool-base/singleton resolution `switch` and pass fully resolved inputs
(`AgentName` = the resolved instance/qualified name exactly as hand-stamped
today, `PoolSlot` = the slot exactly when today's code stamps `pool_slot`,
else 0). The hand-stamps of `agent_name`/`pool_slot` are deleted; the
derivation emits them with identical values (map assembly order is
irrelevant; stderr log ordering unchanged — logs stay where they are).
Canonical-record emission at adoption is gated on `isConfigAgent` (see emit
rules): the orphan arm (`agent_name = sessionName`, the ga-fiw phantom
shape) gets NO canonical record — a wrong authoritative record is worse
than an absent one; the Stage-3+ heal decides orphans with full config
context.

**A3 — `cmd/gc/session_name_lookup.go:246-277` (ephemeral pool-session
mint).** Today: hand-rolls its meta map (`agent_name`, `pool_slot` when
`identity.Slot > 0`, `session_name`, pool-managed marker) without
`desiredSessionIdentity`. New form: additive only — stamp
`canonical_instance_name = agentName` and, when `identity.Slot > 0`,
`canonical_pool_slot = identity.Slot` into the same map. Do NOT restructure
this site onto `desiredSessionIdentity` in Stage 2 (that consolidation is
Stage-6 material); the identity here is pool-resolved config identity, so
the record is safe to stamp.

**Emit rules for the canonical record (normative, all sites):**
- `canonical_instance_name` is stamped iff the site's resolved instance name
  is non-empty AND config-resolved (A1: always — syncSessionBeads iterates
  configured agents; A2: `isConfigAgent` only; A3: always — pool identity is
  config-resolved). Value = byte-identical to the `agent_name` the site
  stamps today.
- `canonical_pool_slot` is stamped iff `canonical_instance_name` is stamped
  AND slot > 0, with `strconv.Itoa(slot)`. Never stamped alone
  (`CanonicalIdentityFromMetadata` ignores a stray slot without a name, but
  we do not create that state).
- No site ever OVERWRITES an existing canonical record in Stage 2 (create
  and adoption only mint new beads / new meta maps; no patch path touches
  the two keys). Healing existing beads is Stage 3+ (`actionStampCanonicalIdentity`).

### B. Start-commit sites (where `primed_at` + `prompt_hash` fold in)

**B0 — the Delivered signal must be threaded, not inferred.**
`templateParamsToConfig` (`cmd/gc/template_resolve.go:780-795`) calls the
Stage-1 pure `promptDelivery(...)` and sets `env[GC_STARTUP_PROMPT_DELIVERED]
= "1"` when `Delivered`. CRITICAL: the env marker is NOT "delivered this
launch" — the resume path in `buildPreparedStartWithWorkDirResolver`
(`session_lifecycle_parallel.go:969-983`, `!firstStart && !forceFresh &&
hasResumeKey`) clears `PromptSuffix`/`PromptFlag`, swaps in
`restartPromptNudge`, and RE-SETS the env marker to "1" for hook consumption
even though nothing is delivered in that incarnation. Reading
`cfg.Env[startupPromptDeliveredEnv]` at commit time would therefore
mis-stamp `primed_at` on every resume. New form:
- `templateParamsToConfig` grows a sibling
  `templateParamsToConfigWithDelivery(tp) (runtime.Config, promptDeliveryResult)`;
  the existing function becomes a wrapper discarding the second value (all
  other call sites unchanged).
- `preparedStart` (`session_lifecycle_parallel.go:188`) gains two fields:
  `promptDelivered bool`, `promptHash string`. Inside
  `buildPreparedStartWithWorkDirResolver`, after `firstStart`/`forceFresh`/
  `hasResumeKey` are computed:
  `promptDelivered = delivery.Delivered && (firstStart || forceFresh || !hasResumeKey)`
  (exactly the complement of the resume-override condition), and
  `promptHash = session.PromptHash(tp.Prompt)`.
- The existing env choreography (`startupPromptDeliveredEnv` set/delete,
  `restartPromptNudge`) is NOT touched — M4 deletes it at Stage 6; hooks
  still read it. Stage 2 clears NO existing stamp site.

**B1 — `commitStartResultTraced` (`session_lifecycle_parallel.go:1942`).**
The launch-success commit. New form: add to the `CommitStartedPatchInput`
literal: `PrimedAt: clk.Now()` and `PromptHash:
result.prepared.promptHash` — but only via the input struct's own gate (see
B3); the call site passes them unconditionally guarded by
`result.prepared.promptDelivered` (zero `time.Time` / `""` otherwise). This
is on the `result.err == nil` path, so "start succeeded" is already
established — parent-spec confirmation signal 1
(`Delivered && start succeeded`) holds exactly.

**B2 — `recoverRunningPendingCreate` (`session_lifecycle_parallel.go:2151`).**
The crash-recovery re-confirmation of an already-running runtime. It rebuilds
`prepared` via `buildPreparedStart` from CURRENT durable state; because a
pre-commit crash left `started_config_hash == ""`, the rebuild classifies
`firstStart = true` and `prepared.promptDelivered` mirrors what the original
launch delivered (same template resolution — the same current-state
re-derivation this site already uses for the hashes it stamps). New form:
same two fields as B1, gated on `prepared.promptDelivered`, `Now` as the
site already computes. Documented caveat: if config changed between the
original launch and recovery, the stamped `prompt_hash` describes the
CURRENT rendered prompt, consistent with the site stamping current hashes;
Stage 3+ attempt-eligibility treats a hash mismatch as re-eligible, which is
the desired convergence semantics.

**B3 — `session.CommitStartedPatch` (`internal/session/lifecycle_transition.go:240,261`).**
New optional input fields and the single point of marker emission:

```go
// PrimedAt, when non-zero and PromptHash is non-empty, records that this
// start's launch path delivered the rendered startup prompt (S19 §2
// confirmation signal 1). Emitted atomically with started_config_hash so
// priming inherits the start path's crash semantics. Zero ⇒ no priming keys.
PrimedAt   time.Time
PromptHash string
```

Patch emission (inside `CommitStartedPatch`, after the existing keys):
`if !input.PrimedAt.IsZero() && input.PromptHash != "" {
patch[PrimedAtMetadataKey] = input.PrimedAt.UTC().Format(time.RFC3339);
patch[PromptHashMetadataKey] = input.PromptHash }`.
Both-or-neither by construction; P5 (never stamp for an empty prompt) holds
because `PromptHash("") == ""` and `promptDelivery("").Delivered == false`
(two independent gates). `priming_attempted_at` is NEVER emitted here — the
write-ahead attempt marker belongs to the Stage-4 awake-scan path (P4:
`primed_at` is confirmation-only; `CommitStartedPatch` and the future
post-Nudge stamp are its only writers).

### C. `started_config_hash` clear sites — the priming-key lifetime rule

**Rule (normative): `primed_at`, `priming_attempted_at`, and `prompt_hash`
have exactly the lifetime of `started_config_hash`. Every site that writes
`started_config_hash = ""` also clears all three priming keys; no other site
clears them.** This refines the parent spec's §2.2 site list (it named 2-3
sites; main has 6) into a rule that is greppable and testable. A fresh
incarnation re-primes; a resumed/churned incarnation (hash kept) keeps its
markers. Clearing keys that were never set is a no-op at the store layer
(`MetadataPatch` doc: empty values clear), so all six edits are
behavior-preserving in a write-only stage.

Inventory (every non-test `started_config_hash = ""` writer on origin/main):

| # | site | today | Stage-2 edit |
|---|---|---|---|
| C-1 | `internal/session/lifecycle_transition.go:15-21` `freshWakeConversationResetKeys` + `:55 applyFreshWakeConversationReset` (wake_mode=fresh / PreWakePatch / drain+drift resets) | clears session_key, started_config_hash, started_live_hash, live_hash, startup_dialog_verified | append the three priming keys to BOTH the list and the apply function (they must stay aligned; add an alignment assert to the existing tests if absent) |
| C-2 | `internal/session/lifecycle_exits.go:184-193` `ConversationResetPatch(clearStartedConfigHash bool)` (wake failures clear the hash; churn keeps it) | clears hash only inside `if clearStartedConfigHash` | add the three priming clears inside the SAME `if` — churn keeps markers (correct: conversation continuity) |
| C-3 | `internal/session/chat.go:145-155` `Manager.clearStaleResumeMetadata` | `SetMetadata(id, "started_config_hash", "")` + mirror | clear the three keys in the same operation (batch if available; mirror map updated identically) |
| C-4 | `cmd/gc/session_lifecycle_parallel.go:1862-1885` `clearStaleResumeKeyMetadata` (pre-flight stale-resume guard) | patch clears session_key + started_config_hash, sets continuation_reset_pending | add the three priming clears to the patch map |
| C-5 | `cmd/gc/session_beads.go:418-432` reopen-with-pending-create batch (`reopenClosedConfiguredNamedSessionBead` region) | clears started_config_hash, started_live_hash, live_hash, startup_dialog_verified when re-claiming | add the three priming clears to the same batch |
| C-6 | `cmd/gc/session_reconcile.go:1238-1246` asleep heal (`ResetContinuation || stalePendingCreateRollback`, non-always named sessions) | clears session_key + started_config_hash, sets continuation_reset_pending | add the three priming clears inside the same guard |

`internal/session/lifecycle_transition.go` is also where the key constants
land: `PrimedAtMetadataKey = "primed_at"`, `PrimingAttemptedAtMetadataKey =
"priming_attempted_at"`, `PromptHashMetadataKey = "prompt_hash"`, plus
`func PromptHash(prompt string) string` (sha256 hex of the exact rendered
prompt; `""` for the empty prompt). All six C-sites and B3 use the
constants — no raw key strings at any new site. (The Stage-1
`cmd/gc/session_level_converge.go` doc comments naming these keys stay
accurate; its `durableFacts` fields bind to the constants in Stage 3.)

### D. Projection surfaces

**D1 — `internal/session/info_store.go:145` `InfoFromPersistedBead`.** Gains
the canonical-identity projection (design below). **D2 —
`internal/session/info_apply_patch.go` `Info.ApplyPatch`** and its oracle
`TestInfoApplyPatchMatchesReprojection` (`info_apply_patch_test.go:170`),
whose patch corpus is generated from `allProjectedMetadataKeys` ("every
metadata key InfoFromPersistedBead reads"). This corpus makes the spike's
"stored struct, not folded by ApplyPatch" design UNSOUND here: once the
canonical keys join the corpus (which the corpus's own definition requires,
since the projection now reads them), fold-vs-reproject diverges — and the
spike's escape (excluding the keys) both weakens the oracle and leaves the
Step-6d write-returns-Info snapshot stale after a future heal writes the
record. Stage 2 therefore adapts the spike as follows (deliberate refinement,
same external contract):

- `Info` gains two VERBATIM raw mirrors (existing house pattern —
  `PendingCreateClaimMetadata`, `DependencyOnlyMetadata`):
  `CanonicalInstanceNameMetadata string` and `CanonicalPoolSlotMetadata
  string`, populated by `InfoFromPersistedBead` and folded per-key by
  `ApplyPatch` (verbatim copy — trivially fold-equal).
- `CanonicalIdentity` becomes a PURE ACCESSOR:
  `func (i Info) CanonicalIdentity() CanonicalIdentity` delegating to
  `CanonicalIdentityFromMetadata`-equivalent logic over the two mirrors
  (implemented as one shared helper so the two can never drift). No stored
  derived struct ⇒ nothing to go stale, nothing excluded from the oracle.
- The two keys are ADDED to `allProjectedMetadataKeys` so the oracle
  actively covers them, including the stray-slot-without-name edge.
- R2 holds: `Info` is internal; the raw mirrors and accessor never appear on
  the HTTP/SSE wire (no change to `internal/api` types or openapi.json —
  verified by `TestOpenAPISpecInSync` being untouched).

## Invariants — the correctness contract

**S2-1 — WRITE-ONLY (the stage's defining constraint).** No production code
path reads `canonical_instance_name`, `canonical_pool_slot`, `primed_at`,
`priming_attempted_at`, or `prompt_hash` to make ANY decision. The only
permitted reads are: the pure projection (`CanonicalIdentityFromMetadata`,
`InfoFromPersistedBead`, `ApplyPatch` fold) — computed, returned, and used
by NOTHING outside tests — and test assertions. Greppable acceptance check:
outside `internal/session/{canonical_identity,info_store,info_apply_patch}*.go`
and `_test.go` files, zero non-test references to the five keys/constants
appear on any read path (writes at the A/B/C sites only).

**S2-2 — behavior preservation.** Every existing decision (firstStart,
identity ladders, priming env choreography, pool-slot resolution, adoption
dedup, drift comparison) runs on exactly the legacy signals it read before.
Follows from S2-1 plus: (i) the A1/A2 restructuring produces byte-identical
final meta maps modulo the two ADDED keys; (ii) B additions only ADD keys to
the `CommitStartedPatch` batch; (iii) C additions only clear keys nothing
reads; (iv) D is compute-only. New metadata keys are invisible to hash
computations (`runtime.CoreFingerprint` et al. hash `runtime.Config`, not
bead metadata) and to `sessionMatchesFilters`/state classification.

**S2-3 — canonical-record honesty.** A canonical record is stamped only from
config-resolved identity (A emit rules); the orphan-adoption arm never mints
one. `canonical_pool_slot` never exists without `canonical_instance_name`.
No Stage-2 code path overwrites or deletes an existing canonical record
(the two keys appear in no clear list — identity survives incarnation
resets; C3 analog: the record is incarnation-independent).

**S2-4 — priming-key lifetime.** The three priming keys are written only by
`CommitStartedPatch` (confirmation pair, both-or-neither) and cleared only
at the six C-sites, i.e. exactly where `started_config_hash` clears. P4
(confirmation honesty) and P5 (no markers for empty prompts) hold at every
Stage-2 write site. `priming_attempted_at` has clear sites but no writer
until Stage 4 — asserted by grep-level test (no non-test writer exists).

**S2-5 — single owner for `started_config_hash` (C3 of parent spec).**
Stage 2 touches `CommitStartedPatchInput` but the identity stamps and
priming clears never write `started_config_hash` themselves; its writer set
is unchanged.

**S2-6 — projection coherence.** `Info.CanonicalIdentity()` computed from a
folded `ApplyPatch` snapshot equals the one computed from a full
re-projection for EVERY patch over the two keys (oracle-enforced, corpus
extended). The record-existence rule is exactly the spike's:
`Present ⇔ TrimSpace(canonical_instance_name) != ""`; slot parse: trimmed,
`Atoi`, `> 0` else 0.

**Repo invariants (inherited, all preserved):** R1 zero hardcoded roles (all
new code is key plumbing; no role names); R2 typed wire (no new wire types;
no `map[string]any` on HTTP/SSE — bead metadata maps are the existing
generic bead substrate, not new wire surface); R3 typed events (Stage 2 adds
NO event types, nothing to register); R4 worker boundary (no new session
create/lifecycle call sites; all bead writes ride existing `sessFront`
patches/creates); R5 no upward imports (`internal/session` additions import
only stdlib; `cmd/gc` imports `internal/session` — downward only); R6
cmd/gc stays a projection (the only cmd/gc logic added is input threading);
R7 no `config.Agent` field added (no genschema run, no AgentPatch sync).

## Behavior-preserving migration/staging

Land as ONE PR of four commits, each independently green (`make test`,
`go vet ./...`, `.githooks/pre-commit`), off post-#4034 `origin/main`.
Ordering puts the pure substrate first and the highest-blast-radius edit
(A2 adoption reorder) last so it can be dropped/reverted alone.

**Commit 1 — internal/session substrate (pure, zero call sites).**
`canonical_identity.go` (+ spike test, adapted): constants, record,
`CanonicalIdentityFromMetadata`, `parseCanonicalSlot`. Priming constants +
`PromptHash` in `lifecycle_transition.go`. `Info` raw mirrors +
`CanonicalIdentity()` accessor; `InfoFromPersistedBead` + `ApplyPatch` fold;
`allProjectedMetadataKeys` extended. `CommitStartedPatchInput.PrimedAt/
PromptHash` + emission. C-1/C-2/C-3 clears (the internal/session-owned clear
sites). Nothing in cmd/gc changes; the oracle and the full
`internal/session` suite prove the projection.

**Commit 2 — launch-path threading (cmd/gc, additive).**
`templateParamsToConfigWithDelivery` sibling + wrapper;
`preparedStart.promptDelivered/promptHash`; the
`delivery.Delivered && (firstStart || forceFresh || !hasResumeKey)`
computation in `buildPreparedStartWithWorkDirResolver`; B1 + B2 call-site
inputs. C-4/C-5/C-6 clears. No decision path altered; the resume-path env
choreography untouched.

**Commit 3 — create-site canonical stamp (A1 + A3).**
`desiredSessionIdentity` emits the canonical pair per the emit rules;
A1 passes `PoolSlot` and drops its duplicate manual `pool_slot` stamp;
A3 adds its two additive lines. Golden-map tests pin byte-identical
final maps modulo the two added keys.

**Commit 4 — adoption reorder (A2).**
Move the derivation call below pool-base resolution; delete the three
`agent_name` and one `pool_slot` hand-stamps; pass resolved inputs.
The existing adoption-barrier test suite (incl.
`TestAdoptionBarrier_StaleDashNSingleton*` and the orphan/ga-fiw cases)
plus new golden-map assertions prove identical adoption metadata.

**Rollback story:** each commit reverts cleanly in isolation (4 → 1). All
writes are additive metadata; a revert strands at most inert keys on beads
minted in the window, which Stage 3's heal treats identically to
never-stamped beads (absent/stale record ⇒ heal; unread priming markers ⇒
cleared on next incarnation reset). No migration, no backfill, no schema
gate: old binaries ignore the keys entirely (bd metadata is open-world).

**Explicit non-goals (deferred, do not do here):** no reader cutover (Stage
5); no heal action execution (Stage 3); no `priming_attempted_at` writer, no
Nudge re-delivery (Stage 4); no deletion of `restartPromptNudge`/env
choreography, no ladder or backfill-machine changes (Stage 6); no
`durableFacts` population from the new keys (Stage 3); A3 stays off
`desiredSessionIdentity` (Stage 6).

## Test plan (incl. -race/parity if applicable)

**New unit tests (TDD — write first, watch fail):**

1. `internal/session/canonical_identity_test.go` (adapt spike): table over
   {absent map, name only, name+slot, whitespace name/slot trimming, stray
   slot without name ⇒ zero record, non-numeric slot ⇒ 0, zero/negative
   slot ⇒ 0}.
2. `TestInfoCanonicalIdentityAccessor`: `InfoFromPersistedBead` mirrors
   verbatim; accessor equals `CanonicalIdentityFromMetadata` for every
   corpus bead (shared-helper drift guard).
3. Oracle extension: two canonical keys in `allProjectedMetadataKeys`; add
   edge patches {name set on stray-slot base, name cleared, slot cleared,
   slot garbage} — `TestInfoApplyPatchMatchesReprojection` must pass
   (this FAILS against the spike's stored-field design; it is the test that
   justifies the refinement).
4. `TestCommitStartedPatchPriming`: {zero PrimedAt ⇒ no keys; PrimedAt +
   empty hash ⇒ no keys (P5); both ⇒ both keys RFC3339/verbatim;
   `started_config_hash` writer set unchanged}.
5. `TestPromptHash`: empty ⇒ ""; deterministic; distinct prompts ⇒ distinct.
6. Clear-site rule test (the lifetime invariant, greppable form): for each of
   C-1..C-6, build the patch/batch and assert the three priming keys are
   cleared whenever `started_config_hash` is; plus
   `TestConversationResetPatch` churn arm (false ⇒ markers kept) and
   fresh-wake list/apply alignment.
7. `TestDesiredSessionIdentity` extension: canonical pair emitted per emit
   rules (with/without AgentName, with/without slot); Stage-1 rows unchanged.
8. Golden-map stamp tests: A1 create block (pool + singleton + pending-pool)
   and A2 adoption (pool instance, singleton, stale-dash-N singleton, orphan)
   produce EXACTLY the pre-Stage-2 map plus — where the emit rules say so —
   the two canonical keys; orphan arm: NO canonical keys.
9. `TestPreparedStartPromptDelivered`: truth table over {Delivered} ×
   {firstStart, forceFresh, hasResumeKey} — resume incarnation ⇒ false even
   though env marker says "1" (the B0 trap, pinned).
10. B2 recovery test: pending-create recovery with a prompt-bearing template
    stamps the pair; with empty prompt stamps nothing.

**No-behavior-change proof (the stage's core obligation):**
- Full reconciler + lifecycle suites green untouched: `make test` and
  `make test-cmd-gc-process-parallel` (TESTING.md shard); integration
  shards for tmux-touching paths per `make test-integration-shards-parallel`.
- Stage-1 parity pins stay green unmodified: `TestDeriveFirstStart*`,
  `TestDeriveFirstStartMatchesLegacyWhenUnprobed`, `TestDeriveConvergeActions*`,
  `TestDesiredSessionIdentity` (extended rows only), `TestPromptDelivery`.
- Guard tests untouched and green: `TestGCNonTestFilesStayOnWorkerBoundary`,
  `TestInfoApplyPatchMatchesReprojection`, `TestOpenAPISpecInSync`,
  `TestEveryKnownEventTypeHasRegisteredPayload`, `TestAgentFieldSync`.
- `-race`: `go test -race ./internal/session/ ./cmd/gc/` for the touched
  packages (the new code is pure or single-writer-batch; race exposure is
  the `preparedStart` threading, which is per-goroutine already).
- Write-only grep gate (S2-1), as a test or reviewed checklist: no non-test
  read site for the five keys outside the projection files.

## Top correctness risks

1. **Mis-stamping `primed_at` on resume (the B0 trap).** The tempting signal
   — `cfg.Env[GC_STARTUP_PROMPT_DELIVERED]` — is deliberately set to "1" on
   resume paths that deliver nothing. Stamping from it would durably record
   fresh confirmations for every wake, and when Stage 4 activates, the
   crash-window table's guarantees would be built on false confirmations
   (a re-primed-looking session that lost its prompt would never be healed).
   Mitigation: explicit `promptDelivered` threading + test 9 pinning the
   resume row false.

2. **Adoption reorder (A2) silently changing adoption metadata.** Moving the
   derivation below pool-base resolution touches the code path behind the
   #3872-1 family and the ga-fiw orphan shape; a slipped value (e.g. slot
   stamped in the stale-dash-N singleton arm, or `agent_name` drift in the
   orphan arm) changes dedup/identity behavior NOW, not in a later stage.
   Mitigation: golden-map tests per arm (test 8), commit 4 isolated and
   individually revertible.

3. **A wrong canonical record is a Stage-5 time bomb.** Nothing reads the
   record in Stage 2, so a bad stamp (orphan adoptions, hand-built A3 meta)
   is invisible until the read cutover makes it authoritative — the failure
   would surface months later as mis-identity with no nearby diff.
   Mitigation: config-resolved-only emit rule (S2-3), no-overwrite rule, and
   value = byte-identical to today's `agent_name` so audits are one grep.

4. **ApplyPatch/projection drift (the D2 refinement).** If the stored-struct
   spike design were landed as-is, the fold-vs-reproject oracle either
   weakens (keys excluded) or fails (keys included), and Stage-5 readers of
   folded snapshots would see stale `Present=false` after heals. Mitigation:
   verbatim mirrors + pure accessor + corpus extension (test 3).

5. **Priming-clear omissions.** Missing one of the six C-sites leaves a
   stale `primed_at` on a fresh incarnation; from Stage 4 on, that session's
   startup prompt is silently never delivered (#3872-3 recreated with
   durable camouflage). The parent spec's own site list undercounts (2-3 of
   6) — the lifetime RULE, not the list, is the contract. Mitigation: test 6
   over all six sites + the grep-derived inventory in section C.

6. **Scope creep breaking "write-only".** Any convenience read (e.g. "the
   record is right there, use it in the backfill diff") converts a
   behavior-preserving stage into an unreviewed Stage-5. Mitigation: S2-1
   grep gate in review; the non-goals list is normative.
