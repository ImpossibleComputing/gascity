# Marcus Driscoll - DeepSeek V4 Flash

**Verdict:** block

**Top strengths:**
- **Robust review-gated migration invariants:** The explicit invariants (line 42) and slice-by-slice gates establish a highly disciplined rollout process. This prevents team members from skipping verification steps on complex multi-repository shifts.
- **Deterministic and clean registry isolation:** Retiring legacy `gastown` and `maintenance` aliases from `All()` and updating `syntheticPackLayouts` is precisely scoped. The negative tests on `IsSource`, `NameForSource`, and lock generation ensure legacy interface boundaries fail closed.
- **Fail-closed system pack materialization:** Shifting system pack loading to `internal/systempacks` with a mandatory `assertRequiredSystemPackProvenance` gate guarantees that the orchestrator never boots on a corrupt or partially materialized Core pack.

---

**Critical risks:**

- **[Blocker] Offline/air-gapped upgrade failure due to un-namespaced `RepoCacheKey` transition.**
  Once `IsSource(PublicGastownPackSource)` returns `false` (as public Gastown is no longer embedded), `RepoCacheKey` will compute an un-namespaced cache key instead of the synthetic namespaced key (`bundled-synthetic-v1\x00...`). In an offline/air-gapped environment, existing cities upgrading to the new binary will compute the new key, fail to find the cache, and fail to boot because the network is unreachable to fetch the pack. The correct Gastown files are already present on disk in the old namespaced synthetic cache directory, but the new binary will completely ignore them.
  *Evidence:* `RepoCacheKey` at `pack_include.go:302–309`, `EnsureRepoInCache` at `cache.go:63`.

- **[Blocker] Git-validation crash on stale synthetic caches at legacy `IsSource` branches.**
  `validateInstalledRemoteCache` (pack_include.go:213) and `ReadCachedPackImports` (install.go:49) branch on `IsSource(source)` to decide whether to run synthetic validation or git-checkout validation. After migration, public Gastown is no longer recognized as `IsSource`, but stale synthetic cache directories still exist on disk. When checking or installing, the new binary will attempt `git checkout` or `git rev-parse` validation on these synthetic directories, which lack a `.git` repository, crashing with raw Git errors rather than raising a clean migration diagnostic.
  *Evidence:* Five distinct `IsSource` branches mapped across `pack_include.go`, `cache.go`, and `install.go`.

- **[Major] Global `SyntheticContentHash` invalidation cascade on first startup.**
  Removing `maintenance` and `gastown` from `All()` alters the output of `SyntheticContentHash()`, which hashes all bundled packs. This change invalidates all existing synthetic caches (including `bd` and `dolt`). While `MaterializeSyntheticRepo` is safe-by-construction and will self-heal by re-materializing them from the new embeds, this cascade creates a one-time startup CPU and IO overhead that is undocumented and untested.
  *Evidence:* `SyntheticContentHash()` at `registry.go:252–274`, global cache-mismatch rejection at `ValidateSyntheticRepo:225`.

- **[Major] Ambiguity in `dog` pool-name contract vs. Core role-neutrality.**
  Core asserts that the `dog` maintenance agent is purely configurable, and renaming/omitting it must not break SDK behavior. However, `examples/dolt/orders/mol-dog-stale-db.toml` hardcodes `pool = "dog"`. If `dog` is freely renameable, Dolt's database maintenance breaks. If `dog` is a stable contract that provider packs must bind to, Core is not fully role-neutral. The design fails to specify how provider dependencies resolve this pool name without creating Go-level or pack-level special cases.
  *Evidence:* `examples/dolt/pack.toml:6` and dolt's database maintenance orders.

- **[Minor] Residual relative fallbacks in `dolt-target.sh`.**
  Moving `dolt-target.sh` to `internal/packs/core` alters its directory depth. Its relative fallback path (`$SCRIPT_DIR/../../../../../dolt/assets/scripts`) to locate `port_resolve.sh` will break unless updated. Additionally, `examples/dolt/port_resolve_test.go` still contains hardcoded paths to the legacy Maintenance script location.
  *Evidence:* `dolt-target.sh:153-157` and `port_resolve_test.go:148`.

---

**Missing evidence:**
- **Offline test-strategy resolution:** The design accepts that network-required public install is acceptable for Gastown, but does not specify how offline-only tests like `TestSyncLockUsesBundledFallbackForPublicGastownWhenRemoteUnavailable` will be re-written or replaced without failing on network-less CI paths.
- **Materialization pruning vs. quarantine details:** The design wants operator-edited files under required packs (`core`, `bd`, `dolt`) quarantined rather than deleted, but does not define the staging folder structure or how the content-integrity gate handles quarantined file-sets.

---

**Required changes:**
1. **Implement dual-read offline cache fallback:** Update the cache lookup in `internal/config/pack_include.go` so that if an un-namespaced public Gastown lookup fails offline, the cache manager checks for the presence of the old namespaced synthetic cache. If found, it should copy-promote those assets to the new un-namespaced cache path to prevent boot failure.
2. **Explicitly handle legacy synthetic directories at Git validation boundaries:** Ensure that `validateInstalledRemoteCache` verifies the presence of a `.git` directory before executing Git commands on the path, raising a clean migration advisory if it detects a legacy synthetic cache structure.
3. **Resolve the `dog` pool-name contract:** Declare `dog` as a stable pool-name contract for provider-bound maintenance, or update dolt's orders to resolve the pool dynamically from the Core pack configuration.
4. **Specify and test `SyntheticContentHash` self-healing:** Document the one-time re-materialization penalty on first boot and add a test in `registry_test.go` proving that `bd` and `dolt` re-materialize successfully offline when a global hash mismatch is encountered.
5. **Rewire `dolt-target.sh` and `port_resolve_test.go`:** Update the fallback depth inside `dolt-target.sh` to match its new path, and rewrite `port_resolve_test.go` to use `GC_SYSTEM_PACKS_DIR` instead of the hardcoded legacy path.

---

**Questions:**
- Can `normalizeRepository`'s special-case mapping of `gascity-packs` clone URLs to `PublicRepository` be safely removed, or is it still required for ordinary remote URL resolution?
- Should the doctor check provide a warning-level diagnostic with a manual cleanup hint for the stale `.gc/system/packs/maintenance` and `.gc/system/packs/gastown` directories left on disk?
