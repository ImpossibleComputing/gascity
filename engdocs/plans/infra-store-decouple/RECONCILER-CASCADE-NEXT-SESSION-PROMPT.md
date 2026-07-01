# Next-session prompt — finish the non-work field-door cleanup (recovery-loop slice + reconciler spine → ready)

Paste the block below into a fresh session.

---

Finish the non-work-bead field-door cleanup on **PR #3839** (branch
`upstream/object-front-doors-cleanup`, base `main`, DRAFT, worktree
`/data/projects/gascity/.claude/worktrees/object-front-doors`, HEAD `1dbf692e7`).

**Read first, in order:** `engdocs/plans/infra-store-decouple/RECONCILER-CASCADE-HANDOFF.md`
(the authoritative "finish it out" guide — the verified 21-site census, the
reconciler-spine flip cluster + Phase 2 lockstep requirement, the re-projection-
primitive blocker, and the foundation tiers), then `P4-CONVERSION-CONTRACT.md`
(per-site swap rules + sibling table + RAW fidelity-field rules) and
`NONWORK-BEAD-FIELDDOOR-PLAN.md` (architecture). `P4-CASCADE-HANDOFF.md` is the
completed-history record.

Confirm a green baseline:
```
go build ./cmd/gc/ ./internal/session/
go test ./cmd/gc/ -run 'TestSessionClassifierInfoEquivalence|TestSessionSnapshotInfoEquivalence|TestNudgeTargetInfoEquivalence|TestSnapshotInfoOnlyFilesStayOnInfoAccessors|TestFrontDoorStoreFreeFilesStayStoreFree|TestSweepUndesiredPoolSessionBeads' -count=1
go test ./internal/session/ -run 'TestNamedSessionInfoEquivalence' -count=1
git checkout go.sum
```

**Principle (hard rule):** direct read of metadata/bead FIELDS on any NON-WORK
object (session/nudge/mail/order/graph) is illegal — only generic WORK beads read
raw. This is the precondition for a per-class backend swap.

**Verified state (workflow `wf_7f806124-bcd`, adversarially cross-checked):** raw-
accessor surface is **20** non-test sites (was 21; the recovery loop was converted
— see below). The Info codec is RICH (already projects
state/sleep_reason/pool_slot/named/manual/health/lease clusters). ~30 `*Info`
siblings exist, incl. the whole pending-create lease family and the `*ForAgent`
family (both DONE this migration). The pool sweep loop AND the pool recovery loop
are DONE. Do NOT re-trust any agent that says "the `*Info` siblings don't exist" —
that was a stale out-of-tree checkout; pin `git rev-parse HEAD` if you re-run a
mapping agent.

**DONE (commit `1dbf692e7`): the recovery-loop slice.**
`discoverSessionBeadsWithRoots` (`build_desired_state.go:2079`) now iterates
`OpenInfos()` and recovers the raw bead via `FindByID(info.ID)` only for the
identity chain (`sessionBeadQualifiedName`, `canonicalSessionIdentityWithConfig`,
`resolveTemplateForSessionBead` — rule 3). Its two foundation siblings
(`scaleCheckPartialSessionPreservableInfo`, `staleNonExpandingPoolSessionBeadInfo`,
the latter equivalence-cased with a canonical-singleton agent fixture) landed and
were adversarially cross-verified byte-identical. This is the reference shape for
the remaining loops.

**What REMAINS (in recommended order — each is ONE atomic, carefully-reviewed
change; do NOT fan parallel agents at the reconciler connected component):**

1. **The reconciler spine flip — THE primary unlock, do this first.** The tick holds
   `session := &ordered[i]` (`session_reconciler.go:1227`) as a mutable working
   copy; `beadByID`/`circuitSessionByIdentity` alias the same array, and Phase 2
   `advanceSessionDrainsWithSessionsTraced` (`session_wake.go:428`) consumes it —
   so the flip MUST migrate Phase 2 + both maps in the same commit. The single
   hardest blocker: `session.Info` has NO `Metadata` map and no in-memory re-
   projection primitive, so the lockstep `session.Metadata[k]=v` has no Info
   analog yet. Land the foundation FIRST, each its own additive commit:
   - **Tier 0:** `Info.ResetCommittedAt` + `Info.ContinuationResetPending` (+
     `ChurnCount`/`SessionKey`/`StartedConfigHash`/`CoreHashBreakdown` if the
     flipped helpers read them) — struct + codec + equivalence.
   - **Tier 1:** the re-projection primitive (`Info.applyMetadataPatch` or re-
     project via `InfoFromPersistedBead`), proven by a recording test: apply
     batch to a bead + re-project == apply batch to Info directly, byte-equal for
     every spine-written key.
   - **Tier 2:** the missing pure-classifier Info siblings (`healStatePatchInfo`,
     `sessionExitFactsInfo`, `productiveLongEnoughInfo`, `stableLongEnoughInfo`,
     `sessionStartRequestedInfo`, `resetPendingCommittedAtInfo`,
     `pendingCreateLeaseExpiredForRollbackInfo`, `pendingCreateSessionStillLeasedInfo`,
     `shouldRollbackPendingCreateInfo`, `resolveSessionSleepPolicyInfo`,
     `isPoolExcessInfo`, `sessionWithinDesiredConfigInfo`) — each byte-identical +
     equivalence-cased.
   - **Then the flip:** the cluster (`healState`/`healStateWithRollback`/
     `checkStability`/`checkRateLimitStability`/`checkChurn`/
     `markProviderTerminalError`/`record*`/`clear*`/`healExpiredTimers`/
     `markDrainAckStopPending`/`recoverPendingIdleSleep`/`reconcileDetachedAt`/
     `persistSessionCircuitBreakerMetadata` + the inline writes at
     `session_reconciler.go:1350`/`:1574`/`:1858`/`:1908–1920`) + re-typing
     `beadByID`/`circuitSessionByIdentity` to `*session.Info` + migrating Phase 2
     — all in one reviewed commit. Byte-identity oracle = the reconciler/pool E2E
     suite + the recording fake.

2. **The remaining spine-blocked + other-blocked sites** as their ops take Info:
   `filterSessionBeadsByName` (`city_runtime.go:3085`), `cmd_wait.go:1164` (wait-
   nudge helper family), `soft_reload.go:103` (needs a `template_overrides`/raw-
   metadata accessor on `Info`), and the `open []beads.Bead` threads at
   `city_runtime.go:1159`/`:2158`/`:2246`, `cmd_start.go:904`/`:918`,
   `session_lifecycle_parallel.go:809`. Add each newly accessor-free file to
   `snapshotInfoOnlyFiles`.

3. **P5 `closeBead` cross-class split** (LANDMINE — isolated, last; recording-fake
   oracle; close-THEN-release; preserve skip-if-already-closed idempotence).

4. **P6** delete dead bead classifiers/`Open()`/`FindSessionBeadBy*` (codec edge
   `session_bead_snapshot.go:301` is EXEMPT) + widen the guard to forbid
   `.Store().Store` in converted files. When the pool sweep + recovery loops'
   raw siblings lose their last caller (`poolSessionBeadRuntimeRunning`,
   `scaleCheckPartialSessionPreservable`, `staleNonExpandingPoolSessionBead`),
   delete them here too.

**DO NOT convert (RAW-BY-DESIGN, not leaks):** `city_status_snapshot.go:411`
(`countCitySessionsFromSnapshot` — prove the snapshot invariant first),
`city_runtime.go:2153` (`emitDueComputeFacts` — usage bookkeeping),
`city_runtime.go:3246` (`sessionBeadSnapshotFingerprint` — hashes ALL raw
metadata), and the `session_bead_snapshot.go` codec edge. The 7 rule-3 store-op
sites (`build_desired_state.go:3341`/`:3570`/`:3816`/`:4165`,
`city_runtime.go:2752`, `session_beads.go:57`/`:2033`) stay raw.

**Method (proven):** keep each original classifier untouched + ADD the typed
sibling + ADD an equivalence case (byte-identical oracle), THEN flip the signature
with ALL callers in the SAME commit. `OpenInfos()[i]` is the precomputed
projection of `Open()[i]`, so raw + Info slices coexist during partial migration
(the spine mutation cluster is the one exception that must flip together). For
foundation gaps, add the Info field + codec population + equivalence case BEFORE
the site that needs it. Test call sites project fixtures via
`sessionInfosFromBeads([]beads.Bead) []session.Info`.

**Build/commit hygiene:** `git checkout go.sum` after builds; commit AND push with
`--no-verify` (stale hooksPath + the pre-push hook runs the full suite and times
out — run gates manually). Trailer:
`Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
Never `tmux kill-server` / `go clean -cache` (`-testcache` ok); gascity Dolt is
LOCAL-ONLY (no `bd dolt push`).

**Gates before ready:** `go build ./...` · `go vet ./...` ·
`golangci-lint run ./cmd/gc/... ./internal/session/...` (0) · the equivalence +
guard tests · targeted subject suites (reconcile/pool/wait). `make dashboard-check`
not needed (`Info` additions stay internal). The build host is oversubscribed —
targeted `-run` locally; CI on dedicated runners is the byte-identical gate.

**Finish (only when #3839 CI is verified GREEN — no premature ready):**
- `gh pr checks 3839 --watch`
- ready (gh pr ready aborts on projectCards — use the API): `gh api graphql -f query='mutation($id:ID!){markPullRequestReadyForReview(input:{pullRequestId:$id}){pullRequest{isDraft}}}' -f id=$(gh api repos/gastownhall/gascity/pulls/3839 --jq .node_id)`
- label: `gh api --method POST repos/gastownhall/gascity/issues/3839/labels -f 'labels[]=status/needs-review-auto'`

**Done =** every non-work consumer reads via `session.Info` (grep-clean of raw
snapshot accessors + `.Store().Store`), the guard forbids regression, full gates
+ #3839 CI green, #3839 ready + labeled. Update
`memory/infra-beads-decoupling-plan.md`.

---
