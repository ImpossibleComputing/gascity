# Ritu Raman — DeepSeek V4 Flash Style Independent Review (Iteration 23 / Attempt 23)

**Verdict:** approve

**Lane:** 07-bootstrap-fixture-isolation-reviewer
**Scope:** Bootstrap embed cleanup, deterministic test fixtures, test-only no-Core path containment, hidden dependency discovery.

This independent review evaluates the Iteration 23 draft of the Core and Gastown Pack Split design (`.gc/design-reviews/ga-2404qu/attempt-23-retry-20260610T085741Z/design-before.md`) against the `requirements.md` and the active codebase at the `rig_root` (`/data/projects/gascity`).

---

## Executive Summary

As Ritu Raman, the **Bootstrap Fixture Isolation Reviewer**, I am issuing a **Verdict: Approve** for the Iteration 23 / Attempt 23 design.

The design represents an exceptionally clean, robust, and highly disciplined blueprint for decoupling the production binary from legacy global implicit imports. Transitioning to a **source-symbol guarded scanner** and defining `bootstrapAssets` as a private, non-nil empty filesystem returning `fs.ErrNotExist` (§4005–4007) represents excellent structural hygiene.

By systematically addressing the technical feedback and edge cases raised in prior iterations, the Iteration 23 document successfully eliminates all critical blocking gaps:
1. **Unspecified `bootstrapAssets` Default (Resolved §4005–4007):** The design explicitly mandates that `bootstrapAssets` must default to a private, non-nil, empty filesystem that returns `fs.ErrNotExist`, avoiding any latent nil-FS panics or opaque errors on standard loader paths.
2. **`GC_BOOTSTRAP=skip` Production Bypass (Resolved §4035–4039):** The environment variable is fully retired as a production switch, ensuring it cannot be abused as an escape hatch, while narrowing its test-only scope strictly to legacy empty fixture materialization.
3. **Contradictory Fixture Embed Paths (Resolved §4008–4014):** The contradictory "tiny compatibility embed" clause has been cleanly replaced by a strict inline `fstest.MapFS` requirement, preventing mutable production folders from leaking into low-level tests.
4. **Complete Hidden-Dependency Inventory (Resolved §4026–4031):** Critical single-point-of-change dependencies—including `cmd/gc/prompt_test.go`, `internal/config/bundled_import_test.go`, `examples/gastown/precompact_hook_test.go`, and Hook overlays—have been formally added to the refactoring checklist for Slice 3.

With these rigorous technical guards in place, the bootstrap extraction and test-isolation model is fully mature.

---

## Top Strengths of Current Design

* **Absolute Production Binary Containment (§4005–4007):** Deleting the `//go:embed packs/**` directive and the `embeddedBootstrapPacks` variable ensures that no Core asset leak can occur in production binaries.
* **Hermetic, Non-Nil Private fs.FS (§4005–4007):** Forcing `bootstrapAssets` to default to a custom, non-nil filesystem returning `fs.ErrNotExist` prevents standard loader code from throwing nil-pointer panics while cleanly reporting absent fixtures.
* **Decoupled Verification via Inline `fstest.MapFS` (§4008–4014):** Requiring bootstrap tests to use inline synthetic fixtures ensures config and bootstrap parsing remains fully testable without copying mutable production directories, completely eliminating disk-drift.
* **Negative Asset-Presence Guard (§4032–4034):** Implementing `TestBootstrapFixtureIsMinimal` (failing if inline fixtures include production-only directories such as `formulas/`, `orders/`, `overlay/`, `skills/`, or `assets/prompts/`) guarantees that synthetic test fixtures do not secretly grow to include real production behaviors.
* **Explicit Downstream Migration Paths (§4026–4031):** Formally documenting that `cmd/gc/prompt_test.go` must be updated to read from `core.PackFS` or the new `internal/packs/core` path rather than the old bootstrap path prevents intermediate compile or runtime test failures.

---

## De-risking Hidden Dependencies & Cross-Document Consistency (Independent DeepSeek V4 Flash Perspective)

To ensure that the design does not leave hidden traps that other reviewers might accept too quickly, we have cross-referenced the active codebase with the design's proposed changes:

### 1. `GC_BOOTSTRAP` Env-Var Footprint in `internal/doctor/implicit_import_cache_check.go`
* **The Current State:** The doctor's implicit import cache check (`implicit_import_cache_check.go:236–245`) unsets `GC_BOOTSTRAP` during the check to force proper validation and then restores it.
* **The Risk:** Since `GC_BOOTSTRAP=skip` is completely retired as a production behavior switch, leaving this environment variable manipulation in place adds vestigial, dead-code complexity.
* **The Design's Alignment:** The design correctly addresses this by mandating the removal or narrowing of `GC_BOOTSTRAP=skip` branches, but we must explicitly trace this during implementation so that the doctor check is pruned of `GC_BOOTSTRAP` env manipulation alongside the retirement of the environment variable in the Core extraction slice.

### 2. Double-Keyed Discovery Surfaces for `bootstrap.PackNames()`
* **The Current State:** The codebase couples to `bootstrap.PackNames()` in two key production files:
  - `internal/materialize/skills.go` (via `bootstrapSkillDirs()`)
  - `cmd/gc/skill_catalog_cache.go` (via `currentBootstrapCatalogState()`)
* **The Risk:** Since `BootstrapPacks` is permanently empty in production, `PackNames()` will return an empty slice, causing these two files to silently behave as empty without direct compilation failures, leading to potential lost capability if the required system pack discovery has not yet been wired.
* **The Design's Alignment:** The design cleanly handles this at L3293 by explicitly identifying these two double-keyed discovery surfaces and mandating that they are migrated to the required-system-pack model in the same slice that empties `BootstrapPacks`.

### 3. Collision Metadata and Legacy Core Names
* **The Current State:** `internal/bootstrap/collision.go` enforces that user-defined `[imports]` do not collide with bootstrap pack names.
* **The Risk:** If required-`core` collision checks are moved entirely to `internal/systempacks`/`internal/builtinpacks`, leaving `collision.go` unmodified would mean `PackNames()` returns empty, rendering collision detection for `core` dead on the bootstrap side.
* **The Design's Alignment:** The design resolves this gracefully by noting that `internal/bootstrap` retains exactly one post-extraction role: legacy bootstrap-collision metadata compatibility for adopted cities, while active required-`core` collision detection is cleanly relocated.

---

## Technical Evaluation of Invariant Questions

### Q1. Does `internal/bootstrap` stop embedding production Core while keeping bootstrap tests deterministic through explicit isolated fixtures?
* **Finding: Yes.** Deleting the `//go:embed packs/**` directive and reassigning `bootstrapAssets` to a custom private filesystem returning `fs.ErrNotExist` successfully purges production Core from the bootstrap package. Tests achieve full determinism by using inline synthetic `fstest.MapFS` structures that mimic the parsing requirements without relying on real disk assets.

### Q2. How is fixture drift against the shipped Core pack detected without causing low-level config tests to exercise production assets accidentally?
* **Finding: Satisfactory.** Low-level tests are completely isolated using inline `fstest.MapFS` structures, preventing them from touching real Core. Fixture minimal-size verification is continuously enforced by `TestBootstrapFixtureIsMinimal`, which fails if any production-only assets leak into the test fixtures. Any broader schema or behavioral drift is validated upstream at the integration/compilation layer via the `test/packcompat` suite without compromising low-level unit test hermeticity.

### Q3. Are tests needing no-Core behavior using structurally test-only lower-level loaders rather than runtime flags or environment switches?
* **Finding: Yes.** The complete retirement of `GC_BOOTSTRAP=skip` as a production bypass ensures that the normal runtime loader pathways cannot be skipped via environment state. Any test requiring explicit "no-Core" behavior must load the configuration using low-level package-internal loaders directly, enforcing a strict structural separation.

---

## Operational Recommendations for Implementation Success

1. **Prune Doctor Env Manipulation in Slice 3:** During the Core extraction slice, completely remove the `os.Unsetenv("GC_BOOTSTRAP")` and `os.Setenv("GC_BOOTSTRAP", prev)` dance in `internal/doctor/implicit_import_cache_check.go`, as the variable will have no production effect.
2. **Centralize Test Fixture Maps to Avoid Inline Duplication:** While tests should use `fstest.MapFS` for deterministic test fixtures, defining duplicate map literals across `prompt_test.go`, `bundled_import_test.go`, and collision tests could introduce drift. Consider creating a centralized helper (e.g., `bootstrap.NewMinimalCoreTestFixture()`) inside `bootstrap_test.go` or a testing helper file to build the mock maps uniformly.
