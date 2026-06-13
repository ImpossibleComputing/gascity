# Owen Gallagher — DeepSeek V4 Flash (Pack Boundary Containment Reviewer, Attempt 13, Independent Review)

**Verdict:** approve

> **Lane:** Core versus Gastown ownership split, retired Maintenance source containment, active discovery classifier, no duplicate active behavior.
>
> Reviewed against the Attempt 13 design document snapshot (`.gc/design-reviews/ga-1ekw9l/attempt-13/design-before.md`, 1894 lines, `updated_at: 2026-06-10T10:17:33Z`) — specifically §"Summary", §"Current System", §"Proposed Implementation", §"Required System Pack Loader", §"Pack Registry, Cache, And Retired Source Authority", §"Role Neutrality And Configurable Bindings", and §"Testing".
>
> This independent review is produced using the DeepSeek V4 Flash persona, focusing specifically on first-principles trust boundaries, cross-document state consistency, and unstated runtime assumptions.

---

## Schema Conformance

Conforms to `gc.mayor.implementation-plan.v1`. Front matter carries the required keys with `phase: implementation-plan` and no `design_file`; the eight required top-level sections appear once each in the required order, and `Open Questions` is `None`. No appended attempt/review prose in the artifact.

---

## Top Strengths of the Design

- **Unified Classifier Authority and Active Discovery Gating:** Consolidating retired Maintenance and Gastown classification under `internal/packsource` (lines 701–708) and forcing all active behavior discovery to route through `packsource.ActiveRootsFor(kind)` (lines 712–716) is structurally sound and avoids ad-hoc string checks.
- **AST-Aware Linter and Direct Walk Prevention:** Upgrading the linter to an AST-based type-aware validator (lines 647–674) that rejects raw filesystem reads like `filepath.Glob` or direct `os.ReadDir` on pack roots is an excellent defense against containment bypasses.
- **Immutable Pin-Before-Delete Sequence:** Correctly staging and locking down public `gascity-packs` prerequisites (Slices 1a/1b/1c) before permitting any local source deletions or Maintenance fold (Slices 5b/7) prevents unverified and destructive changes.

---

## Resolution of Previous Blocker Risks (DeepSeek V4 Flash Style)

The Attempt 13 design-before snapshot successfully addresses all six of the critical blockers identified in the Attempt 11/12 reviews with precise, robust architectural guarantees:

### 1. The Circular Preflight Deadlock — RESOLVED
- **The Risk:** In previous attempts, the `zero-duplicate-active` gate would fail-close during startup on duplicates, blocking the operator from running `gc doctor --fix` because the doctor itself would crash during config loading.
- **The Resolution:** The plan now specifies a non-executing bootstrap-diagnostic load mode for `LoadRuntimeCity` and `LoadRuntimeCityNoRefresh` (lines 716–719) and maps the `doctor` and mutation commands to run in this mode (lines 603–605). This breaks the circular preflight deadlock completely, allowing repair mutations even on blocked/duplicate-active cities.

### 2. Config-Preflight Classifier Cycle (Chicken-and-Egg Loader Lock) — RESOLVED
- **The Risk:** The classifier previously faced a circular dependency: it needed config resolution to identify active public vs. retired packs, but the loader needed the classifier to find the roots to load.
- **The Resolution:** The plan establishes an explicit, non-circular bootstrap-preflight classification path in `internal/packsource` (lines 712–716) that operates over raw root provenance (paths, lock entries, import edges) before config resolution, preventing any chicken-and-egg dependency cycle.

### 3. Trigger-Level ID Ambiguity on Split Assets — RESOLVED
- **The Risk:** Coarse-grained duplicate active gates could either false-alarm on trigger-split assets (like `mol-shutdown-dance`) or miss actual trigger collisions.
- **The Resolution:** The plan defines clear behavior-identity rules per kind (declared TOML name for formulas/orders, relative path for scripts, render-target key for prompts/overlays, registered name for commands/skills) at lines 721–725. This removes ambiguity and makes duplicate detection mechanical and precise.

### 4. Rollback Version-Skew and Stale Directory Re-Activation — RESOLVED
- **The Risk:** Rolling back to an older binary could cause it to eagerly scan and load stale, leftover Maintenance directories on disk, causing dual-fire/shadowing behavior.
- **The Resolution:** The plan declares adopting the activation pin a hard, manual-recovery-only one-way boundary for old binaries in the release matrix (lines 1878–1879) and details rollback diagnostics and recovery (lines 814–819), ensuring skew is detected and handled.

### 5. Independent Glob Scanner Loopholes (Glob Bypasses) — RESOLVED
- **The Risk:** Parts of the codebase using `path/filepath.Glob` or standard `os.ReadDir` would bypass the `packsource` active roots classification, leaking retired/stale pack behaviors into active runtime memory.
- **The Resolution:** The plan specifies a comprehensive, AST-aware, type-resolved loader-bypass scanner (lines 647–674) that denies raw `filepath.Glob`, `os.ReadDir`, and hand-rolled TOML decoding outside `internal/config`, completely sealing the glob containment bypass.

### 6. Shared Core Assets Retaining Gastown-Specific Branches — RESOLVED
- **The Risk:** Shared prompt templates or assets in Core containing inline conditionals checking for Gastown roles (e.g., `{{if eq .Role "mayor"}}`) would violate role neutrality.
- **The Resolution:** The plan explicitly bans Core-owned behavior from containing or branching on specific roles (lines 1070–1071), enforcing absolute template and role neutrality.

---

## Remaining Operational Risks & Recommendations (Non-Blocking)

- **AST Scanner Performance Overhead:** The type-resolved AST scanner run during CI must be optimized or cached to ensure it does not slow down local edit-test loops for developers.
- **Offline Cache Promotion Edge Case:** If the staging directory rename fails due to cross-device mount issues in specific container environments, a fallback copy-then-delete mechanism should be specified rather than a bare rename fail.

---

## Required Structural & Schema Changes

None. The structural changes in the plan are fully satisfactory and conformant.

---

## Questions

1. **AST Scanner Caching:** Does the `go/packages` scanner utilize build cache artifacts, or will it compile from scratch on every pre-commit/CI hook invocation?
2. **Mount Namespace Isolation:** In containerized environments, is the `.staging` folder guaranteed to reside on the same filesystem as the final cache path to ensure the `os.Rename` remains atomic?
