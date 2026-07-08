# Unified `gc run <path>` — lumen OR toml, one 1-city one-shot

**Status:** design synthesis — coordinated with window20 (lumen worktree). No code yet.
**Date:** 2026-07-08
**Method:** 9-agent workflow (3 Opus explore → 3 Fable design → 3 Fable red-team). Raw in `_raw/05-unified-gcrun/`.
**Companion:** the toml forge design is `DESIGN.md` (this fork's formula-as-unit). This doc adds the lumen convergence.

---

## 1. Verdict

**Qualified yes.** One `gc run <path>` that detects `.lumen` vs `.toml` and runs either as a manufactured transient 1-city one-shot is sound — because dispatch is **per-invocation**: one file = one engine = one substrate. The two execution engines (lumen journal-fold vs toml beads/molecule/dispatch) never run concurrently, so the scariest coexistence hazards (two persistence substrates in one `.gc` dir, two completion signals racing, dual run-loop ownership) **are dissolved, not deferred** — they never materialize at runtime. No `JournalStore` fold_owned facade migration is required for the one-shot; that stays out of scope until a single workflow mixes `.lumen` and `.toml` steps.

Two premises from the framing were corrected by the explore pass:
- **The lumen D6 agent bridge is BUILT**, not a gap: `enginehost.WorkerHost` already spawns agent `do`-steps through the canonical `worker.Factory`/`worker.Handle` boundary (`graph-substrate/internal/lumen/enginehost/worker_host.go:96,165`) — the *same* boundary toml dispatch uses. What's limited is honesty (phase-based: any process exit reads `pass`) and output capture, not existence.
- **Verify is not a completion signal** — it's a hash-chain integrity audit; lumen completion is the `run.closed` journal event / `RunResult.Outcome`. There is no Verify-vs-finalize race.

## 2. The split (locked — recorded in both trees)

Confirmed against window20's `PROGRESS.md:142` (its own "⇄ CONVERGENCE" entry) and this design:

- **frm owns** the `gc run` entry + file-type detection + **the forge / city lifecycle** (manufacture → run → teardown) + the **toml executor**.
- **window20 owns the lumen arm** — `engine.Advance` (L0, committed `84fa5dd56`) + L1 claim/close adapter + L2 `lumenRunsCh` controller loop + L3 do→pool via `worker.Handle` — consuming the forge as **blueprint-17's L3 city host**.
- **The forge = blueprint-17's L3 city host.** Blueprint 17 already assumes a city host and is standing-vs-transient agnostic, so the transient forge slots in with no lumen-side redesign.

**3 reconciliation points (from window20's record):**
1. Shared file `cmd/gc/cmd_run.go` — edited only through a named **lumen-arm seam**; diff the seam before either side touches it.
2. **Hosted `Advance`/controller path is primary** for lumen — NOT the standalone `RunWithOptions`+MemStore. (The Fable designers had proposed the sync path; this is the correction — honor the record.)
3. **Completion FORKS** — toml = workflow-finalize control-bead closes the root; lumen = `run.closed` journal event. **Not one finalize watcher for both.** The forge/Host is completion-blind: each executor blocks until its own terminal and returns a normalized `Outcome`.

## 3. The shared forge / Host contract (what the lumen arm consumes)

Engine-agnostic, ~30–100 lines, **new files only** (`internal/forge` + `cmd/gc/cmd_run.go`), no interface until branches merge:

1. **Manufacture** — temp `cityRoot` under a foundry root; `EnsureCityScaffoldFS` (`.gc/{cache,system,runtime}`+`events.jsonl`); `city.toml` with one pathless `[[rigs]]{name,prefix}` per `--folder`; `PersistRigSiteBindings` writes `{name,/repo}` into `.gc/site.toml` so `ApplySiteBindings` late-binds the path at load — **the repo is referenced, never copied**.
2. **Folder-bound execution context** — `City.Folder(name).Path` is the single cwd source. Distinct `rigName` per folder is the only disambiguator when two clones share a derived prefix.
3. **The agent-spawn seam as INPUTS, not an API** — one city-derived `runtime.Provider` (tmux `-L <cityName>` isolation, or subprocess), `CityPath`, `WorkDir`. Both executors already go through `worker.Handle`; the forge supplies the same inputs to both.
4. **Teardown** — `RemoveAll(cityRoot)` after the executor returns; `/repo` untouched by construction; `--keep` shared.

The lumen arm reads **nothing else** the forge writes (no `city.toml`/`ApplySiteBindings` on the lumen path) — `--folder` wires straight to `WorkerHostConfig.WorkDir` + the exec-cwd default. `site.toml`/`ApplySiteBindings` are **toml-executor-private**.

## 4. The four host-seam blockers (the real red-team value)

All at the host seam, all in frm/driver-owned code, none in the engines:

1. **`runController` returning is NOT terminal.** It returns `0` on *any* ctx-cancel — the watcher's socket `stop`, a user SIGINT (its own `signal.Notify` handler, `controller.go:1261`), or an unauthenticated `stop` line from any local process on `.gc/controller.sock`. → Classify by the **triple** (root-closed observed?, SIGINT observed?, return value), never by return alone. Register a host-side SIGINT observer *before* launching the controller goroutine. File the missing `signal.Stop` as a one-line upstream patch (rebases cleanly) rather than living with SIGINT-deafness during teardown.

2. **Watch the FINALIZE bead close, not the root.** The root closes when step beads close (`dispatch/runtime.go:745`) — but agents close their step bead *before* session wrap-up (this fork's protocol: `git push` *after* closing issues). Firing `stop` at root-close → `cr.shutdown` force-kills → an agent killed mid-push leaves `index.lock`/dirty state/**stranded unpushed commits in the user's real `/repo`.** The city dir is disposable; the bound repo is not. → Watch the **finalize bead** close (`runtime.go:762`, after the root) and insert a **drain gate** (watch the run's own sessions to a stopped phase, bounded); **never default to `stop-force`**. This also closes the `:745`→`:762` stop-races-the-dispatcher window.

3. **Hang-forever is the DEFAULT failure mode** — root-anchored predicate + no timer + live stall classes (control-ready finalize dispatch gaps are a documented incident on this fork; `safeTick` swallows reconciler panics by design). An unattended/scripted `gc run` inherits these hangs silently. → Move the **non-destructive max-lifetime deadline into B1** (stop sessions, **KEEP** dir, distinct exit code, emit `city.expired` — never RemoveAll on a timer), and surface *why* via an `onStatus`/stderr line (the `errFinalizePending` blocker id, repeated `safeTick` panic count).

4. **Lumen orphan sessions have no durable record** (shared hazard — affects window20's arm). Standalone `WorkerHost` uses `beads.NewMemStore()` (process-local); on the city's surviving tmux provider a driver crash leaves a live `claude` session cwd'd in the user's `/repo` with no ledger and no adoption path. → Default the one-shot lumen arm to **child-lifetime providers** (subprocess/exec) until the session ledger persists under the manufactured city; the drain-gate discipline from blocker 2 applies to lumen do→pool sessions too.

## 5. YAGNI cuts (all three designers over-built)

- **No `Executor` interface until the branches merge.** Ship a switch on extension calling two plain functions (`runFormulaOneShot`, `runLumenOneShot`). The correct interface falls out of the merge for free; interface drift between branches becomes structurally impossible. (Honors "no interfaces until two implementations exist"; one proposal was even uncompilable — `internal/runhost` cannot import package-main `runController`.)
- **Cut** the `.gc/run.json` Receipt, `--resume`, the `onStatus` callback *contract*, `--rm`, `--verify`, and the full keep/rm policy matrix from v1. Keep exactly: `--folder`, `--var`, `--keep`, exit `0/1/2` (`130` on SIGINT with dir kept), keep-dir-and-print-path on failure/interrupt. (Exception: the *deadline* from blocker 3 stays — it's hang-protection, not resume machinery.)
- Executors and any interface live in `cmd/gc` (where `runController` + the `cmd_start` closures live), **not** `internal/`.
- `.lumen` needs a pre-compiled sibling `.lumen.json` (`cmd_run.go:220`); `.toml` compiles in-process. Document this asymmetry in `gc run --help` — do **not** build a lumen compiler into `gc run`.

## 6. Sequencing — forge FIRST, CLI dispatch LAST

- **Slice 1 (frm, now):** formula-as-unit B0/B1 verbatim — `internal/forge` + toml-only `gc run` (new files + one `AddCommand` line), **no exported interface, no lumen awareness** (reject non-`.toml` with a plain "unsupported file type"). Fold blockers 1–3 into B1. Independently valuable, cherry-picks onto the sqlite deploy branch, and **actively unblocks window20** — the forge is the throwaway-city host its Phase-2 dogfood and L3 e2e need.
- **Slice 2 (window20, its own loop):** finish L1–L3 on `engine.Advance`; refactor the lumen entry behind the named `runLumenArm(...)` seam; adopt the forge as the L3 city host. **Nobody else commits into graph-substrate.**
- **Slice 3 (at branch convergence):** the ~30-line extension router in the one `cmd_run.go` fronting both functions, shaped against the *real* Advance-based lumen arm — plus the interface, if still wanted, extracted from two visible bodies.

The `gc run` name collision at merge is the **feature**, not a problem: both branches claiming top-level `gc run` forces the small extension-switch reconciliation that *is* the unification.

## 7. Open items for Julian

- **Compiled-IR asymmetry:** accept `gc run x.lumen` requiring a pre-built `.lumen.json` (documented contract), or ask the lumen track for an in-process compile step later? (Recommend: document now, no compiler in `gc run`.)
- **Security (F3):** `--folder name=/path` is an unauthenticated capability grant (path becomes a gate-script exec cwd). Fine for **local-single-user**; a manifest-authz gate is a hard precondition before any shared/hosted manufacture. Confirm local-only is the boundary for v1.
- Everything else is settled by the recorded split; no new decision needed to start Slice 1.
