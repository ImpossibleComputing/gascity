# Next-session prompt — reconciler front-door Step 6 (drop the lockstep + remove the raw working set)

Paste the block below into a fresh session.

---

Continue the **session reconciler front-door migration** on **PR #3839** (branch
`upstream/object-front-doors-cleanup`, base `main`, DRAFT, worktree
`/data/projects/gascity/.claude/worktrees/object-front-doors`, HEAD `59af3b856`).

**Read first, in order:**
1. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-HANDOFF.md` — the
   authoritative handoff + ordered backlog. **Steps 0–5 DONE**; you are starting
   **Step 6, the finale**. (This SUPERSEDES the `SPINE-FLIP-*` docs.)
2. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-SPEC.md` — design v2
   (esp. §2 governing safety principle, §4.3 refresh-on-write, §5.2/§6 step 6).

**Where things stand.** Every reconciler decision-path read is now on typed
projections: the four cross-session scans read `Info` (Step 4), and the Phase-0.5
circuit-breaker restore reads `session.CircuitState` (Step 5). **No raw
`beads.Bead.Metadata` read remains on the reconciler decision path.** What still
remains is the raw *write* machinery that has coexisted with the typed snapshot all
along: the `session.Metadata[k]=v` **lockstep** mirror after each persisted write, the
raw `ordered []beads.Bead` + `beadByID` working set, and `refreshSessionInfo`'s
raw-bead projection source.

**Confirm a green baseline:**
```
git rev-parse HEAD            # expect 59af3b856 (or later)
go build ./... && go vet ./cmd/gc/... ./internal/session/...
golangci-lint run ./cmd/gc/... ./internal/session/...   # expect 0
go test ./internal/session/ -run 'TestCircuitStateFromMetadataProjectsVerbatim|TestStoreCircuitState|TestLifecycleInputConstructorsProjectIdentically' -count=1
go test ./cmd/gc/ -run 'TestSessionCircuitBreaker|TestSessionClassifierInfoEquivalence|TestBuildAwakeInputFromReconcilerReadsInfoSnapshot|TestOpenPoolSessionCountForTemplateExcludesClosed' -count=1
git checkout go.sum
```

**DO STEP 6 (this session): retire the raw lockstep + working set.** This is the
finale that makes the reconciler files raw-free and lets them join
`snapshotInfoOnlyFiles`. It is a LANDMINE — do it in small verified commits, each
guarded by a multi-session / read-after-write same-tick test (the byte-identical
**write** oracle is blind to same-tick stale reads — spec §2).

1. **Cut `refreshSessionInfo` over to the store, WITH intra-tick suppression.**
   Today `refreshSessionInfo(id)` re-projects from the raw working copy
   (`InfoFromPersistedBead(*beadByID[id])`). Switching it to `sessFront.Get(id)` is
   the goal, but a naive `Get` **exposes keys the reconciler deliberately keeps OFF
   the in-memory bead this tick** — `reset_committed_at` (persisted on the restart
   handoff but withheld from the lockstep, #2145/#2345 force-wake prevention) and the
   RunLive-reapplied `started_live_hash` (persisted without a lockstep). A `Get`
   refresh would pull those into the snapshot and **re-introduce the #2345 force-wake
   regression**. So Step 6 must add an **explicit intra-tick "hidden this tick" set**
   (analogous to the `restart_requested` intra-tick field, spec §5.2) that suppresses
   `reset_committed_at` / `started_live_hash` on the refreshed `Info` until the next
   tick. Land the suppression FIRST (with a regression test that force-wake stays
   suppressed), THEN flip the source.

2. **Drop the `session.Metadata[k]=v` lockstep** at every write site (heal, zombie,
   sleep, drain-finalize, CB persist, config-drift, restart handoff, …). Now safe:
   every dependent same-tick read is on the snapshot (Steps 3–5). Convert each
   `write + refresh + dependent-read` cluster as ONE commit; after each, run the
   whole-tick reconcile/pool/named/chaos/trace suite. Watch the §2 read-after-write
   sites: `infoPostHeal` (~1545), `infoPostZombie` (~1793), `infoAsleepDrift` (~2457),
   `restart_requested` (~2057, stays intra-tick), `churn_count` (~2133-2172).

3. **Remove the raw `ordered []beads.Bead` + `beadByID` / `circuitSessionByIdentity`
   aliasing** once nothing reads them. The tick's working set becomes the typed
   `infoByID` snapshot (+ the store as the write authority). `persistSessionCircuit
   BreakerMetadata`'s `sessionCircuitMetadataEqual(session.Metadata, …)` idempotence
   read + lockstep mirror is part of this — replace with a store-authoritative
   equality (e.g. read via `sessFront.CircuitState` / an Info field) or drop the
   in-memory mirror.

4. **Join `snapshotInfoOnlyFiles`.** Once `session_reconciler.go` /
   `compute_awake_bridge.go` / `session_circuit_breaker.go` no longer touch raw
   `beads.Bead.Metadata`, add them to the guard list so CI enforces they stay raw-free.

**Gates per commit:** `go build ./...` · `go vet` · `golangci-lint ./cmd/gc/...
./internal/session/...`=0 · gofmt · the byte-identical write oracle **+ a same-tick
read-after-write test per lockstep drop** · whole-tick `TestReconcileSessionBeads*` +
circuit/named/pool/wake/sleep/drain/trace (run heavy suites in the background; the
broad sweep is ~70–130s here). `git checkout go.sum` after. Commit AND push
`--no-verify`. Trailer:
`Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. Never
`tmux kill-server` / `go clean -cache` (`-testcache` ok); gascity Dolt is LOCAL-ONLY
(no `bd dolt push`). #3839 stays DRAFT.

**Cautions:** quote grep globs (`--include='*.go'`) — an unquoted `--include=*.go`
errors under zsh and reads as a false "not found". Read-only mapping agents have
repeatedly read the WRONG worktree (`.worktrees/pack-crud`) — pin
`git rev-parse HEAD` and restrict them to this worktree; verify their line numbers.
The `reset_committed_at` / `started_live_hash` intra-tick divergence is the #1
regression trap — do NOT flip `refreshSessionInfo` to `Get` before the suppression
set is in place and tested. Update the handoff (check boxes) + memory
(`infra-beads-decoupling-plan.md`) as you land each phase.

---
