# Next-session prompt — reconciler front-door Step 4B (typed LifecycleInput)

Paste the block below into a fresh session.

---

Continue the **session reconciler front-door migration** on **PR #3839** (branch
`upstream/object-front-doors-cleanup`, base `main`, DRAFT, worktree
`/data/projects/gascity/.claude/worktrees/object-front-doors`, HEAD `d251a6b64`).

**Read first, in order:**
1. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-HANDOFF.md` — the
   authoritative handoff + ordered backlog. Steps 0–3 DONE; Step 4A DONE; you are
   starting **Step 4B**. (This SUPERSEDES the `SPINE-FLIP-*` docs.)
2. `engdocs/plans/infra-store-decouple/RECONCILER-FRONT-DOOR-SPEC.md` — design v2.
   Caveat: its §3 claim that `LifecycleInput` "is already typed with WaitHold/…
   fields" is INACCURATE — it still holds a raw `Metadata map[string]string`; that
   is exactly what 4B removes.

**Where things stand.** The reconciler tick loads a coherent typed snapshot
(`infoByID map[string]session.Info`, built once from `ordered`) and every
per-session decision read + all four post-mutation re-derives go through it,
refreshed by `refreshSessionInfo(id)`. **Governing decision (do not undo):**
`refreshSessionInfo` refreshes from the **raw working copy**
(`InfoFromPersistedBead(*beadByID[id])`), NOT `sessFront.Get`, during coexistence
— byte-identical by construction AND it preserves the reconciler's deliberate
intra-tick raw/store divergences (the restart handoff persists `reset_committed_at`
but the lockstep skips it, #2145/#2345; the RunLive re-apply persists
`started_live_hash` un-locksteped). Step 6 does the Get cutover + intra-tick
suppression of those keys.

**Confirm a green baseline:**
```
git rev-parse HEAD            # expect d251a6b64 (or later)
go build ./... && go vet ./cmd/gc/... ./internal/session/...
golangci-lint run ./cmd/gc/... ./internal/session/...   # expect 0
go test ./cmd/gc/ -run 'TestSessionClassifierInfoEquivalence|TestFrontDoorStoreFreeFilesStayStoreFree|TestSnapshotInfoOnlyFilesStayOnInfoAccessors' -count=1
go test ./internal/session/ -run 'TestGetReflectsApplyPatch' -count=1
git checkout go.sum
```

**Step 4 approach (owner-chosen): Full typed `LifecycleInput`.** Key scoping
already established: **`ProjectLifecycle` itself reads only 13 keys** — `state`,
`sleep_reason`, `continuity_eligible`, `configured_named_identity`, `held_until`,
`quarantined_until`, `pending_create_claim`, `last_woke_at`, `session_key`,
`started_config_hash`, `pending_create_started_at`, `pin_awake`, `wake_request`
(ALL mirrored in `session.Info`; `wake_request` added in 4A). The
`session_circuit_state` / `restart_requested` / `wait_hold` / `alias` /
`session_name` reads are in the POST-VIEW display helpers
(`lifecycleDisplayReasonFromView`, `lifecycleResetPendingReasonVisible`,
`LifecycleIdentifiersReleased`) — those are display/API paths, NOT the reconciler
scan, so they KEEP their `map` params and are OUT OF SCOPE for Step 4.

**DO STEP 4B (this session):** the typed-core rewrite, in `internal/session`.
1. `internal/session/lifecycle_projection.go` — drop `LifecycleInput.Metadata
   map[string]string`; add the 13 typed fields (12 raw-string:
   `StoredState`/`SleepReason`/`ContinuityEligible`/`ConfiguredNamedIdentity`/
   `HeldUntil`/`QuarantinedUntil`/`LastWokeAt`/`SessionKey`/`StartedConfigHash`/
   `PendingCreateStartedAt`/`PinAwake`/`WakeRequest`, plus `PendingCreateClaim bool`).
   Keep `Status`/`Runtime`/`NamedIdentity`/`WakeCauses`/`PreserveIdentity`/
   `ConfigMissing`/`CreatedAt`/`StaleCreatingAfter`/`Now` unchanged.
2. Convert `ProjectLifecycle` + helpers (`projectBaseState` already takes strings;
   `projectBlockers`, `projectWakeCauses`, `projectRuntimeProjection`,
   `creatingStateIsStale`, `shouldResetContinuation`, `projectContinuityEligibility`)
   to read the typed fields instead of `meta[k]`. Keep every `strings.TrimSpace(...)`
   / `== "true"` / `time.Parse(...)` exactly where it is (move the *source* from the
   map to the field, NOT the logic) so the projection is byte-identical.
3. Add two constructors, both in `internal/session` (so the metadata-key literals
   stay below the codec edge): `LifecycleInputFromMetadata(status string, meta
   map[string]string) LifecycleInput` (fills only the 13 metadata fields; caller
   sets `Status`/`Now`/`Runtime`/… after) and `LifecycleInputFromInfo(info Info)
   LifecycleInput` (same fields from `Info`). Guard with a **byte-identical oracle**:
   for every representative bead shape, `ProjectLifecycle(fromMeta) ==
   ProjectLifecycle(fromInfo(InfoFromPersistedBead(bead)))` across the 13-key set,
   incl. missing-key-vs-empty-string.
4. Route the internal wrappers (`LifecycleDisplayReason*`, `LifecycleWakeConflictState`,
   `LifecycleIdentityReleased`, and the internal `ProjectLifecycle(LifecycleInput{…})`
   sites in `lifecycle_projection.go`, `manager.go:~1279`, `waits.go:~207`) through
   `FromMetadata`. Their POST-view `map` reads
   (`lifecycleDisplayReasonFromView(view, metadata)`) stay as-is.
5. Update `internal/session/*_test.go` LifecycleInput constructions.

Keep 4B confined to `internal/session` + its tests (cmd/gc callers are 4C). Verify
`internal/session` builds+tests green and the byte-identical oracle passes BEFORE
touching cmd/gc.

**Then 4C, 4D** (see the handoff's Step-4 entry): 4C converts the cmd/gc callers —
`compute_awake_bridge.go` `buildAwakeInputFromReconciler` → `LifecycleInputFromInfo
(info)` off the snapshot + its DIRECT `b.Metadata[...]` reads → `Info` (add a raw
`Info.RestartRequested` mirror for the §5.2 intra-tick marker; it reflects the
in-memory value under raw-refresh, Step 6 handles the Get-cutover carrier); route
`session_reconcile.go`/`session_sleep.go`/`cmd_session.go` through `FromMetadata`.
4D converts the 3 simpler scans (min-floor `!Info.Closed`, progress-signatures,
`advanceSessionDrains`) and passes `infoByID` into `buildAwakeInputFromReconciler`.
Then Step 5 (`CircuitState` accessor) and Step 6 (drop lockstep + Get cutover +
hidden-key suppression → files raw-free).

**Gates per commit:** `go build ./...` · `go vet` · `golangci-lint ./cmd/gc/...
./internal/session/...`=0 · gofmt · the byte-identical `LifecycleView` oracle · the
`TestSessionClassifierInfoEquivalence` + front-door guard tests · whole-tick
`TestReconcileSessionBeads*` + pool/named/chaos/wake/sleep/drain/trace after any
cmd/gc read/scan change (≥420s worst case; run in the background, split if it
overloads). `git checkout go.sum` after. Commit AND push `--no-verify`. Trailer:
`Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. Never
`tmux kill-server` / `go clean -cache` (`-testcache` ok); gascity Dolt is
LOCAL-ONLY (no `bd dolt push`). #3839 stays DRAFT.

**Cautions:** quote grep globs (`--include='*.go'`) — an unquoted `--include=*.go`
errors under zsh and reads as a false "not found" (it caused the Step-1
`session_name_explicit` miss; corrected in `838502956`). Read-only mapping agents
have repeatedly read the WRONG worktree (`.worktrees/pack-crud`) — pin
`git rev-parse HEAD` and restrict them to this worktree; verify their line numbers.
Update the handoff (check boxes) + memory (`infra-beads-decoupling-plan.md`) as you
land each phase.

---
