# Next-session prompt — reconciler front-door Step 4C (cmd/gc callers → Info)

Paste the block below into a fresh session.

---

Continue the **session reconciler front-door migration** on **PR #3839** (branch
`upstream/object-front-doors-cleanup`, base `main`, DRAFT, worktree
`/data/projects/gascity/.claude/worktrees/object-front-doors`, HEAD `17f138775`).

**Read first, in order:**
1. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-HANDOFF.md` — the
   authoritative handoff + ordered backlog. Steps 0–3 DONE; Step 4A + 4B DONE; you
   are starting **Step 4C**. (This SUPERSEDES the `SPINE-FLIP-*` docs.)
2. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-SPEC.md` — design v2.

**Where things stand.** 4B landed the typed `LifecycleInput` (13 typed fields, no
`.Metadata` map), the `LifecycleInputFromMetadata` / `LifecycleInputFromInfo`
constructors (both in `internal/session`, key literals below the codec edge), and
the byte-identical oracle `TestLifecycleInputConstructorsProjectIdentically`
(`internal/session/lifecycle_input_test.go`) proving
`ProjectLifecycle(FromMetadata(b)) ≡ ProjectLifecycle(FromInfo(InfoFromPersistedBead(b)))`
across 15 shapes. **The cmd/gc construction sites were already routed through
`FromMetadata` in 4B** (the mechanical compile-fix that dropping the struct field
forces for `go build ./...`). So 4C is now purely the SEMANTIC conversion.

**Confirm a green baseline:**
```
git rev-parse HEAD            # expect 17f138775 (or later)
go build ./... && go vet ./cmd/gc/... ./internal/session/...
golangci-lint run ./cmd/gc/... ./internal/session/...   # expect 0
go test ./internal/session/ -run 'TestLifecycleInputConstructorsProjectIdentically|TestSessionClassifierInfoEquivalence|TestGetReflectsApplyPatch' -count=1
go test ./cmd/gc/ -run 'TestSessionClassifierInfoEquivalence|TestFrontDoorStoreFreeFilesStayStoreFree|TestSnapshotInfoOnlyFilesStayOnInfoAccessors' -count=1
git checkout go.sum
```

**DO STEP 4C (this session):** convert `cmd/gc/compute_awake_bridge.go`
`buildAwakeInputFromReconciler` to read `session.Info` instead of raw
`b.Metadata[...]`, so the awake scan runs off the reconciler's typed view.

1. **Add the `Info.RestartRequested` raw mirror** (`internal/session/info_store.go`
   codec + the `Info` struct + a `TestSessionClassifierInfoEquivalence`
   `stringChecks` case for `restart_requested`). It is the §5.2 intra-tick marker
   set in-memory @~2084; under raw-refresh coexistence the mirror reflects the
   in-memory value, and Step 6 handles the Get-cutover intra-tick carrier.
2. In `buildAwakeInputFromReconciler`, replace the lifecycle construction
   `LifecycleInputFromMetadata(b.Status, b.Metadata)` with
   `LifecycleInputFromInfo(info)` and convert every DIRECT `b.Metadata[...]` read in
   the loop to the `Info` field: `session_name`→`info.SessionNameMetadata` (the
   raw-name guard), `template`→`info.Template`, `sleep_reason`→`info.SleepReason`,
   `dependency_only`→`info.DependencyOnly`, `wait_hold`→`info.WaitHold` (`== "true"`),
   `restart_requested`→`info.RestartRequested`,
   `continuation_reset_pending`+`reset_committed_at`→`info.ContinuationResetPending`+
   `info.ResetCommittedAt`, `currently_processing_bead_id`→`info.CurrentlyProcessingBeadID`,
   `detached_at`→`info.DetachedAt`. (`isManualSessionBead`/`isNamedSessionBead` take a
   bead today — either give them `Info` peers or leave as-is if they still take `*b`;
   note which in the commit.)
3. **The `info` SOURCE**: 4C may re-derive `info := session.InfoFromPersistedBead(*b)`
   locally to keep the diff self-contained; **4D** then changes the caller to pass
   the reconciler's coherent `infoByID` snapshot in so the awake scan reads the SAME
   Info the tick already built (no re-derive). Do NOT invent a second snapshot.

**Then 4D** (see the handoff's Step-4 entry): pass `infoByID` into
`buildAwakeInputFromReconciler`; convert the 3 simpler scans — min-floor
(`ordered[j].Status != "closed"` → `!Info.Closed`; every close site must set Closed
on the refreshed snapshot), `computeNamedSessionProgressSignatures`,
`advanceSessionDrains`. Then Step 5 (`CircuitState` accessor) and Step 6 (drop
lockstep + Get cutover + hidden-key suppression → files raw-free).

**Gates per commit:** `go build ./...` · `go vet` · `golangci-lint ./cmd/gc/...
./internal/session/...`=0 · gofmt · the byte-identical oracle · the
`TestSessionClassifierInfoEquivalence` + front-door guard tests · whole-tick
`TestReconcileSessionBeads*` + pool/named/chaos/wake/sleep/drain/trace after any
cmd/gc read/scan change (run heavy suites in the background; the broad
`Pool|Named|Wake|Sleep|Drain|Trace|Reconcil|Phase0|Circuit` sweep is ~125s here).
`git checkout go.sum` after. Commit AND push `--no-verify`. Trailer:
`Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. Never
`tmux kill-server` / `go clean -cache` (`-testcache` ok); gascity Dolt is
LOCAL-ONLY (no `bd dolt push`). #3839 stays DRAFT.

**Cautions:** quote grep globs (`--include='*.go'`) — an unquoted `--include=*.go`
errors under zsh and reads as a false "not found". Read-only mapping agents have
repeatedly read the WRONG worktree (`.worktrees/pack-crud`) — pin
`git rev-parse HEAD` and restrict them to this worktree; verify their line numbers.
Update the handoff (check boxes) + memory (`infra-beads-decoupling-plan.md`) as you
land each phase.

---
