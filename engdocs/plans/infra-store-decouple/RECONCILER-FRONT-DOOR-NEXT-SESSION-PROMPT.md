# Next-session prompt — reconciler front-door Step 4D (snapshot plumbing + 3 scans)

Paste the block below into a fresh session.

---

Continue the **session reconciler front-door migration** on **PR #3839** (branch
`upstream/object-front-doors-cleanup`, base `main`, DRAFT, worktree
`/data/projects/gascity/.claude/worktrees/object-front-doors`, HEAD `6843e8607`).

**Read first, in order:**
1. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-HANDOFF.md` — the
   authoritative handoff + ordered backlog. Steps 0–3 DONE; Step 4A + 4B + 4C DONE;
   you are starting **Step 4D**. (This SUPERSEDES the `SPINE-FLIP-*` docs.)
2. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-SPEC.md` — design v2.

**Where things stand.** 4B typed `LifecycleInput` + the FromMetadata/FromInfo
constructors + the byte-identical oracle. 4C (`6843e8607`) added `Info.RestartRequested`
and converted `buildAwakeInputFromReconciler`'s session-beads loop to read
`info := session.InfoFromPersistedBead(*b)` — a LOCAL re-derive — with every fact off
`Info` and the lifecycle view fed by `LifecycleInputFromInfo(info)`.

**Confirm a green baseline:**
```
git rev-parse HEAD            # expect 6843e8607 (or later)
go build ./... && go vet ./cmd/gc/... ./internal/session/...
golangci-lint run ./cmd/gc/... ./internal/session/...   # expect 0
go test ./cmd/gc/ -run 'TestSessionClassifierInfoEquivalence|TestFrontDoorStoreFreeFilesStayStoreFree|TestSnapshotInfoOnlyFilesStayOnInfoAccessors' -count=1
go test ./internal/session/ -run 'TestLifecycleInputConstructorsProjectIdentically|TestGetReflectsApplyPatch' -count=1
git checkout go.sum
```

**DO STEP 4D (this session):** stop the re-derive in `buildAwakeInputFromReconciler`,
then convert the 3 simpler scans. Each is one verified commit.

1. **Snapshot plumbing.** `buildAwakeInputFromReconciler` (`compute_awake_bridge.go`)
   takes `sessionBeads []beads.Bead` and re-derives `InfoFromPersistedBead(*b)` per
   iteration (the 4C local shape). Change it to consume the reconciler's coherent
   snapshot so the awake scan reads the SAME `Info` the tick already refreshed. The
   snapshot is `infoByID map[string]sessionpkg.Info` built in `session_reconciler.go`
   @~1329 (from `ordered`, kept fresh by `refreshSessionInfo`). Production caller is
   `session_reconciler.go`@~2721. Either change the param to an ordered
   `[]session.Info` (built from `ordered`/`infoByID` in tick order) or pass `infoByID`
   + keep iterating `sessionBeads` for order and look each `info` up by ID — pick the
   shape that keeps the loop order identical and the diff smallest. **~14 test callers**
   of `buildAwakeInputFromReconciler` (`compute_awake_bridge_test.go`,
   `compute_awake_set_min_active_test.go`) build `[]beads.Bead` — update them (project
   via `InfoFromPersistedBead` or build `Info` directly). Behavior byte-identical:
   `infoByID[id] == InfoFromPersistedBead(ordered-bead)` by construction (raw-refresh,
   Step 3 decision). Drop the 4C local `info := InfoFromPersistedBead(*b)`.
2. **Min-floor scan** (`build_desired_state.go` @~1031/1039, `b.Status != "closed"`):
   convert to `!Info.Closed`. **INVARIANT:** every mid-tick close site must set
   `Closed` on the refreshed snapshot entry (via `refreshSessionInfo`) so a bead closed
   earlier in the tick reads closed here — confirm each close path refreshes before this
   scan runs, and add a multi-session read-after-close same-tick test.
3. **`computeNamedSessionProgressSignatures`** (`session_circuit_breaker.go`@~855) and
   **`advanceSessionDrains`** (`session_wake.go`@~392): route their per-session raw
   reads through `Info` (use the `*Info` classifier siblings / `Info` fields; add
   mirrors only if a genuinely-unmirrored key surfaces — check the Step-1 inventory
   first). Keep each byte-identical; equivalence-case any new mirror.

Then **Step 5** (`session.Store.CircuitState` typed accessor over the 9-key
`session_circuit_*` cluster — a dedicated value, NOT `Info`) and **Step 6** (drop the
lockstep + remove the raw `ordered`/`beadByID` working set + cut `refreshSessionInfo`
over to `sessFront.Get` + add explicit intra-tick suppression of `reset_committed_at`
/ `started_live_hash` so the #2345 force-wake regression can't return).

**Gates per commit:** `go build ./...` · `go vet` · `golangci-lint ./cmd/gc/...
./internal/session/...`=0 · gofmt · `TestSessionClassifierInfoEquivalence` + front-door
guards + the `LifecycleInput` oracle · whole-tick `TestReconcileSessionBeads*` +
pool/named/chaos/wake/sleep/drain/trace/awake after any read/scan change (run heavy
suites in the background; the broad `Pool|Named|Wake|Sleep|Drain|Trace|Reconcil|Phase0|
Circuit|Awake` sweep is ~125s here). Per §2 governing principle, each lockstep-adjacent
close/refresh conversion needs a read-after-write same-tick test. `git checkout go.sum`
after. Commit AND push `--no-verify`. Trailer:
`Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. Never
`tmux kill-server` / `go clean -cache` (`-testcache` ok); gascity Dolt is LOCAL-ONLY
(no `bd dolt push`). #3839 stays DRAFT.

**Cautions:** quote grep globs (`--include='*.go'`) — an unquoted `--include=*.go`
errors under zsh and reads as a false "not found". Read-only mapping agents have
repeatedly read the WRONG worktree (`.worktrees/pack-crud`) — pin
`git rev-parse HEAD` and restrict them to this worktree; verify their line numbers.
Update the handoff (check boxes) + memory (`infra-beads-decoupling-plan.md`) as you
land each phase.

---
