# Session-class periphery — shape-pass handoff

**Branch** `upstream/object-front-doors-cleanup` (base `main`), **PR #3839 DRAFT**,
worktree `/data/projects/gascity/.claude/worktrees/object-front-doors`.
**HEAD `92f05221d`** (always `git rev-parse HEAD` — line numbers below drift; re-grep
before editing any file).

This continues the object-model front-door migration onto the SESSION-class
periphery. It is the successor to `RECONCILER-FRONT-DOOR-LOCKSTEP-DROP.md` (the
reconciler decision path, DONE) and is governed by
`SESSION-PERIPHERY-CLOSURE-PLAN.md` (read its **Progress log** first — it is the
live status; this doc is the narrative handoff).

---

## The governing scope decision (owner call — do not relitigate)

Seal each periphery file in **two separate passes**:

1. **Shape pass** (in progress) — route every raw session-bead *field read* through
   the `session.Info` codec (`session.InfoFromPersistedBead(bead).<Field>` or a typed
   Info-form helper), and add the file to `metadataInfoOnlyFiles` /
   `snapshotInfoOnlyFiles`. Backend-shape-invariant hygiene.
2. **Access pass** (separate, later) — route the bead *LOAD* through
   `sessionsBeadStore()` / `resolveSessionStore` so a `[beads.classes.sessions]`
   relocation actually captures it. That is the `frontDoorStoreFreeFiles` boundary.

**Membership in `metadataInfoOnlyFiles` is SHAPE-SEALED, NOT relocation-safe.** A file
is only captured by the swap once it is on `frontDoorStoreFreeFiles` **too**. The guard
doc comment in `frontdoor_di_guard_test.go` says this. Shape-first is forced because
`session.Store.Get` returns `Info` (not a raw bead), so a file must be shape-converted
before its load can route through the Info front door.

`soft_reload.go` (this session) is the **first fully-sealed** file — it was already
store-free, so shape-converting it put it on all three lists.

---

## What is DONE (this pass, commits `d3bc67ee3`..`92f05221d`)

`Info.ProviderKind` codec mirror added (`1e1a80138`, Phase A — real persisted
`provider_kind` family key; full struct + codec + `ApplyPatch` + oracle wiring).

**10 periphery files sealed** (each: build/vet/gofmt/golangci-lint 0 + guard +
revert-canary + targeted tests + a **fable adversarial byte-identity review, 0
findings**):

| file | lists | notes |
|---|---|---|
| session_template_start.go | metadata | session_name→SessionNameMetadata |
| adoption_barrier.go | metadata | session_name→SessionNameMetadata |
| cmd_prime.go | metadata | template/common_name/session_key |
| cmd_skill.go | metadata | agent_name |
| session_resolve.go | metadata | alias + isNamedSessionInfo/namedSessionIdentityInfo |
| cmd_session_logs.go | metadata | work_dir/provider_kind/provider/session_key/state; sessionLogFallbackCandidateLive sig→Info |
| mcp_integration.go | metadata | agent_name/work_dir/provider_kind/provider |
| session_index.go | metadata | +deleted dead pool_template field (no-ghosts) |
| cmd_session_wake.go | metadata | 2 local helpers→Info |
| **soft_reload.go** | **ALL 3** | **fully sealed**; Open()→OpenInfos(); + Phase B helpers |

**Phase B additive Info-form helper wrappers** (each bead form now DELEGATES to its
Info form — DRY, byte-identical, big-file callers untouched; these also **unblock the
Tier-1 giants**): `sessionCoreConfigForHashInfo` (session_hash.go),
`applyTemplateOverridesToConfigInfo` (session_reconciler.go),
`cancelSessionConfigDriftDrainInfo` (session_wake.go). Pre-existing siblings you'll
reuse: `isNamedSessionInfo`, `namedSessionIdentityInfo`, `normalizedSessionTemplateInfo`,
`sessionBeadAgentNameInfo`, `isManualSessionInfoForAgent`, `isEphemeralSessionInfoForAgent`,
`IsSessionBeadOrRepairableInfo`, `cancelSessionDrainIfInfo`, `ParseTemplateOverridesFromInfo`.

Current guard lists (`frontdoor_di_guard_test.go`):
- `frontDoorStoreFreeFiles`: session_circuit_breaker, soft_reload
- `snapshotInfoOnlyFiles`: template_resolve, session_name_lookup, cmd_citystatus,
  city_status_snapshot, session_reconciler_trace_cycle, providers, nudge_dispatcher,
  named_sessions, soft_reload
- `metadataInfoOnlyFiles`: the 10 rows above minus soft_reload's snapshot entry, plus
  the reconciler decision-path files (compute_awake_bridge, session_progress,
  session_circuit_breaker) and city_status_snapshot.

---

## The two load-bearing lessons

**1. Clean-Tier-4 criterion (sharper than the census).** A file is a clean this-pass
target only when its raw reads are on a bead **the function loaded itself** (via
`store.Get` / `ListAllSessionBeads` / a snapshot it holds) — so converting changes no
external signature. If the `.Metadata[` lives in a helper that takes a `beads.Bead`
**parameter**, its callers (usually the big decision files) pass raw beads, and
converting drags them in. Those are the **library trap** — convert them *with* their
callers, not standalone.

**2. Guard eligibility.** The `metadataInfoOnlyFiles` guard is a file-level substring
check for `.Metadata[`. A file qualifies only if converting clears **all** of them.
Files that also read **work/wait/opts** metadata (`wb.Metadata[...]`,
`opts.Metadata[...]`) can't join it — their session reads are still worth converting
(shape value) but the file stays OFF the substring guard (documented census). Beware
the `meta := bead.Metadata; meta["k"]` alias pattern — it is a raw read that the
`.Metadata[` needle does NOT catch (`usage_compute.go` does this).

**Census caveat:** `raw/session-tier4-census.json` (16 haiku agents) was WRONG on
several "MISSING" flags — `session_origin`/`pool_slot`/`pool_managed`/`generation`/
`instance_token`/`sleep_reason` are all already on `Info`. **Re-verify every field
against the `Info` struct (`internal/session/manager.go:74`) + codec
(`internal/session/info_store.go`) yourself before converting.**

---

## What is LEFT

### Remaining Tier-4 — all DEFER (verified by direct inspection, not clean wins)
- **session_origin.go** — bead-form helper library; its bead forms (`sessionOrigin`,
  `isEphemeralSessionBead`, `isManualSessionBeadForAgent`, …) are STILL called by
  `build_desired_state.go` (×5), `session_reconcile.go`, `session_beads.go`,
  `session_name_lookup.go`, **and the classifier-equivalence oracle test**. Every bead
  form already HAS an Info sibling here. Convert **with** the Tier-1 files.
- **pool_desired_state.go** — `poolSessionConsumesNewDemand` is NOT dead; it's the
  oracle-equivalence reference (census was wrong). Plus 6 work-bead reads → no guard.
- **usage_compute.go** — reads via `meta := bead.Metadata` (bypasses the guard) + needs
  Phase A for `awake_started_at`/`slept_at`/`usage_compute_emitted_at` + touches the
  `beadmeta.ResolveRunID` run-chain keys (workflow_id/molecule_id/gc.root_bead_id, a
  molecule concern). Its own focused effort.
- **pool_session_name.go**, **doctor_session_model.go** — mixed (work/wait/opts
  `.Metadata[`). Session reads convertible (shape value) but stay OFF the substring
  guard. Low priority (no guard payoff).

### The bigger tranches (plan phases C-tier1, D, E, F)
- **Tier-2**: `cmd_start.go` (~1529 ln), `cmd_session.go` (~2541 ln). Larger CLI; verify
  each `.Metadata[` is session vs work vs wait first.
- **Tier-1 giants** (each its OWN session, reconciler-grade care):
  `build_desired_state.go` (~4520 ln), `city_runtime.go` (~3477 ln). The Phase B
  helpers now exist to support them; `session_origin.go`'s bead forms convert here.
- **Phase D** `internal/api` session handlers (read `engdocs/architecture/api-control-plane.md`
  first). Different package — extend the guard's dir resolution or add sibling guards.
- **Phase E** `internal/worker` (`factory.go` real_world_app_session_kind/worker_profile
  — needs Phase A; `invocation_telemetry.go` provider_kind — Info.ProviderKind now
  exists; `handle_construct.go` is raw-by-design, leave).
- **Phase F** `internal/session` own runtime/lifecycle (the package doesn't dogfood its
  own Info; riskiest, hot paths).

### The access-layer pass (whole separate initiative)
Route the ad-hoc `openCityStoreAt`/`store.Get` loads in the shape-sealed CLI files
through `sessionsBeadStore()`/`resolveSessionStore` so relocation captures them, then add
each to `frontDoorStoreFreeFiles`. Only then are those files relocation-safe.

---

## The discipline (unchanged — this is the bar)

Per file: verified per-file census (re-grep) → build the Info-form sibling(s) if the
raw read flows into a bead-form helper (delegate the bead form to it, DRY) → flip the
reads to `InfoFromPersistedBead(bead).<VerbatimField>` → **byte-identity traps:
`MetadataState` not `State` (closed-blanked); `SessionNameMetadata` not `SessionName`
(s-<ID> fallback)** → add to the guard list(s) → `gofmt` / `go build ./cmd/gc/` /
`go vet` / `golangci-lint run ./cmd/gc/` (0) / targeted tests / the reconciler subset if
a hot-path helper changed → **revert-canary** (reintroduce a crack, guard must fail) →
**a fable adversarial byte-identity review BEFORE the commit** (owner prefers fable;
`model:'fable'`, high effort; ask it to REFUTE identity) → commit + push `--no-verify`
(trailer `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`).

Gotchas: `git push` runs a ~7min pre-push hook — always `--no-verify` (gates run
manually). Use an isolated `GOCACHE=$(mktemp -d)`; never `go clean -cache`. gascity Dolt
is LOCAL-ONLY — `git push` only, never `bd dolt push`. The `cmd/gc` test binary is huge:
scope `go test` with `-run` and allow a cold-compile timeout. `#3839` stays DRAFT.

Acceptance test for the whole class: relocate `[beads.classes.sessions]` and confirm all
session access follows — only true once BOTH passes close for every file.
