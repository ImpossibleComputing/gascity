# Camille Sato - DeepSeek

**Verdict:** approve-with-risks

Lane: required Core and provider pack loading, typed participation provenance, deny-by-default loaders, bypass containment, fail-closed behavior. Reviewed plans/core-gastown-pack-migration/implementation-plan.md (updated_at 2026-06-10T10:17:33Z) against plans/core-gastown-pack-migration/requirements.md (updated_at 2026-06-10T03:46:02Z).

---

## Top Strengths

- **Elegantly Resolved Circular Dependencies:** Defining `config.RequiredPackDescriptor` and `config.RequiredSystemPackParticipation` entirely within `internal/config` (lines 543-560) is an exceptional architectural resolution. This permits the lower-level config package to remain a leaf with zero dependencies on `internal/systempacks`, while still allowing `internal/systempacks` to pass descriptors during include/import resolution.
- **Structural, Non-Bypassable Readiness Guards:** Transitioning away from a voluntary `RequireReady` check to a structural API design (lines 575-584) is magnificent. Having `RuntimeResult.BehaviorConfig(op)` error out on non-`ready` modes and restricting read-only configurations in `blocked` mode prevents developers from accidentally bypassing the fail-closed loader.
- **Unconditionally Fatal Gate 1 & 2 Execution:** Completely removing manual include assembly in `tryReloadConfig` and replacing it with `LoadRuntimeCityNoRefresh` (lines 614-619) closes the `--no-strict` and warning-downgrade escape hatches. This ensures required Core and provider pack integrity is non-negotiable and unconditionally fail-closed.
- **Rigorous, AST-Aware Bypass Scanner:** The scope is appropriately expanded to all non-test files across Go (except the config and loader packages themselves, lines 647-656). Utilizing `go/packages` and `go/types` for type-awareness ensures that aliased imports, wrapper functions, stored function values, and selector-reached loads are caught with mathematical precision.
- **Advisory Locked, Atomic Cache Promotion:** Forcing `EnsureRepoInCache` to clone into a process-unique staging directory and perform single-atomic `os.Rename` under a shared advisory lock (lines 765-774) ensures that concurrent commands never observe or promote a partially written pack.

---

## Critical Risks

### [Major] First-Run Materialization Crash Recovery (The Half-Materialized Bricking Hazard)
The plan specifies that `MaterializeRequiredPacks` may write `.gc/system/packs/<pack>` only when the target directory is absent or empty (lines 823-825).
If a first-run command or initialization crashes, is killed, or runs out of disk space during materialization, it will leave a half-written, corrupt directory at `.gc/system/packs/<pack>`.
Because the load path "may create, never repair" and any subsequent normal invocation (including status queries or subsequent runs of the same command) sees a non-empty but corrupted/divergent directory, Gate 1 will fail and block the city. The city is effectively bricked until the operator manually runs `gc doctor --fix --non-interactive` (lines 829-834).
*Recommendation:* Use the same staging-and-rename pattern for required pack materialization as used for cache promotion. Materialize the pack into a process-unique staging directory (e.g., `.gc/system/packs/.staging/<pack>.<pid>.<random>`) and atomic `os.Rename` to `.gc/system/packs/<pack>` only upon successful completion. This guarantees materialization is atomic and crash-resilient.

### [Major] Ephemeral CLI Cold-Start Reporting Discrepancy under Degradation
Under `read_only_degraded` mode, "the controller keeps the last-known-good runtime config only for read-only status/reporting, publishes an event and diagnostic, and pauses or refuses behavior-changing... operations" (lines 628-634).
While this works perfectly for the long-running controller, an ephemeral CLI command (such as `gc status`) is a cold start. It does not have an in-memory last-known-good configuration.
When Core or a provider pack on disk is missing or corrupted, a cold-start CLI status invocation will hit Gate 1 failure and fail closed (`blocked` mode), completely unable to perform the "read-only status/reporting" permitted under `read_only_degraded` mode.
*Recommendation:* Explicitly define that the CLI status/reporting commands must attempt to query the running controller's API when degraded to fetch the last-known-good status before falling back to local file checks, or document that a cold-start CLI status command will directly print the bootstrap-only doctor diagnostic when no controller is running.

### [Major] Preflight Bootstrapping Cycle on Provider-Conditioned Packs
To run Gate 1 (file-set validation) on the required provider pack (`bd` or `dolt`), `RequiredPackNames(cityPath)` must know which beads provider is selected (lines 606-608).
However, this provider selection is defined within `city.toml`.
This introduces a bootstrapping cycle: to resolve config, we must have validated the provider pack. To validate the provider pack, we must know which one is selected by parsing `city.toml`!
If the parser used to extract the provider field is full config resolution, we will hit a circular loading failure. If it is a partial TOML parser, we must ensure it is completely isolated from behavior-driving code.
*Recommendation:* Explicitly specify that `RequiredPackNames` uses a safe, isolated preflight TOML parser restricted strictly to extracting the `[beads] provider` field from `city.toml` before any other config resolution, Gates, or loaders are initialized.

### [Minor] AST Bypass Scanner Performance under Pre-Commit Hooks
Scoping the `go/packages` and `go/types` based AST scanner to all non-test files across `cmd/gc` and `internal/` (lines 647-654) is incredibly robust, but running full type-checking can take several seconds.
In a local developer workflow, slow pre-commit hooks lead to friction, and developers may disable hooks entirely, undermining the bypass containment objective.
*Recommendation:* Clarify that the local pre-commit scanner may be optimized (e.g., scoped to changed or staged Go files only) while the complete full-workspace sweep is run in CI as a blocking merge gate.

---

## Missing Evidence

- **Materialization Atomicity:** Detailed staging and renaming specification for `MaterializeRequiredPacks` to prevent the half-materialized bricking hazard on crash.
- **CLI-to-Controller Status Failover:** Explicit protocol describing how cold-start CLI status reporting interacts with the controller's in-memory last-known-good state during degradation.
- **Provider-Preflight Extraction Boundary:** The precise API and design for extracting the beads provider from `city.toml` before any Gates or loaders run.

---

## Questions

1. Does `MaterializeRequiredPacks` perform atomic directory renames from a process-unique staging path to prevent partial materialization?
2. If the disk Core is corrupt but the controller is running in `read_only_degraded` mode, how does a direct CLI `gc status` command retrieve the last-known-good status?
3. Does the AST bypass-scanner support a fast, file-scoped checking mode to keep developer pre-commit hooks responsive?
