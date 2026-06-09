# Owen Gallagher — DeepSeek V4 Flash (Pack Boundary Containment Reviewer, Attempt 7, Independent Review)

**Verdict:** block

> **Lane:** Core versus Gastown ownership split, retired Maintenance source containment, active discovery classifier, no duplicate active behavior.
>
> Reviewed against the Attempt 7 design document (`.gc/design-reviews/ga-1ekw9l/attempt-7/design-before.md`, 835 lines, `updated_at: 2026-06-09T13:20:59Z`) — specifically §"Pack Registry, Cache, And Retired Source Authority" (lines 313–357), §"Doctor And Runtime-State Mutation Safety" (lines 358–408), §"Role Neutrality And Configurable Bindings" (lines 409–458), and §"Rollout And Recovery" (lines 687–825).
>
> This independent review is produced using the DeepSeek V4 Flash persona, focusing specifically on first-principles trust boundaries, cross-document state consistency, and unstated runtime assumptions.

---

## Schema Conformance

Conforms to `gc.mayor.implementation-plan.v1`. Front matter carries the required keys with `phase: implementation-plan` and no `design_file`; the eight required top-level sections appear once each in the required order, and `Open Questions` is `None`. No appended attempt/review prose in the artifact.

---

## Top Strengths of the Design

- **Unified Classifier Authority with Active Roots Gating:** Consolidating retired Maintenance and Gastown classification under `internal/packsource` (lines 320–326) and forcing all active behavior discovery to route through `packsource.ActiveRootsFor(kind)` (lines 418–421) is structurally sound.
- **AST and Type-Aware Bypass Scanner:** Upgrading the linter to an AST-based type-aware validator (lines 421–424) that rejects raw filesystem reads like `os.ReadDir`, `fs.ReadDir`, or `filepath.Walk` on pack roots closes a major containment loophole.
- **Immutable Decomposition Readiness Gate:** Introducing the AC1-AC17 matrix under a strict "Decomposition Readiness Gate" (lines 689–723) prevents premature behavior-changing cuts, ensuring that all acceptance proofs exist before any Gas City source tree is deleted.

---

## Critical Risks & Consensus Blockers (DeepSeek V4 Flash Style)

### 1. [Blocker] The Circular Preflight Deadlock (Loader-Closed vs. Repair/Diagnostics)
- **The Risk:** The plan specifies that `LoadRuntimeCity` or `LoadRuntimeCityNoRefresh` fail-closes during startup if duplicate behaviors are detected (lines 347–351). It also states that `gc doctor --fix` is the sole automatic mutation path used to resolve legacy imports and remove duplicate-producing entries (lines 392–397).
- **The Reality:** Running `gc doctor --fix` requires loading the city's runtime configuration to analyze the import graph and cache state. If a duplicate active behavior exists (for example, after a copy or during rollout overlap), the global loader fail-closes immediately and terminates the process before the doctor can compile its repair list.
- **The Blocker:** This creates a permanent recovery deadlock: the operator cannot fix the duplicate behavior because the doctor cannot load, and the loader cannot load because of the duplicate behavior.
- **Required Change:** Specify an explicit bootstrap/preflight loading parameter for `LoadRuntimeCity` (such as `DiagnosticMode = true`), which permits config parsing and import resolution *without* triggering the `zero-duplicate-active` execution gate. Explicitly mandate that `gc doctor` and the mutation coordinator consume this bypass mode during diagnostics and repair staging.

### 2. [Blocker] The Compatibility-Window Rollout Deadlock (Slices 2–4 Overlap)
- **The Risk:** Slice 2 (compatibility-pin adoption, lines 739–745) adopts the public Gastown compatibility pin, while in-tree Maintenance remains a mandatory host system pack under `requiredBuiltinPackNames` until Slice 5b (Maintenance fold, lines 772–778).
- **The Reality:** The compatibility pin contains the migrated Maintenance and Gastown assets. If both the in-tree Maintenance pack and the imported public compatibility pack are present, the exact same behavior IDs (prompts, formulas, scripts) will be registered from *both* sources.
- **The Blocker:** Under this condition, the `zero-duplicate-active` gate (lines 347–351) will immediately trigger on every loader call, crashing the city and preventing any user from completing the compatibility window.
- **Required Change:** State explicitly that the compatibility-pin public pack *excludes* assets that duplicate still-required in-tree Maintenance (with only the activation pin absorbing them), or specify that the `zero-duplicate-active` gate runs in "report-only/warn-only" mode during the compatibility window (Slices 2 through 4c) and only hard-blocks execution starting with the activation candidate in Slice 5a.

### 3. [Blocker] Behavior-ID Schema Ambiguity
- **The Risk:** The zero-duplicate-active gate blocks loading if "the same behavior id is active from more than one source" (lines 349–350).
- **The Reality:** The plan refers to "behavior ids" as a concrete primitive but never defines how they are generated or structured for prompts, prompt fragments, scripts, hook overlays, formulas, or orders. Simple filename or path matching is insufficient due to moved formulas, split prompts, renamed orders, or subpath aliases.
- **The Blocker:** Without a concrete, stable identity scheme, the loader cannot programmatically detect duplicates or splits, leaving Slices 4a and 5a unimplementable.
- **Required Change:** Define the exact behavior-identity scheme in the plan: for each behavior type (e.g., formula, order, prompt, script, hook), specify the canonical identifier (such as the declared TOML name, trigger slug, or relative path relative to the pack root) used by the zero-duplicate-active gate to compare across bundled and public sources.

### 4. [Major] Rollback Version-Skew and Silent Shadowing (Old Binary + Activation Pin)
- **The Risk:** Once a city adopts the public activation pin, in-tree Maintenance is removed from `requiredBuiltinPackNames` (Slice 5b, lines 772–778). If an operator rolls back the city to an old binary, the old binary still force-includes Maintenance via its embedded `requiredBuiltinPackNames`.
- **The Reality:** Because the public activation pin already contains the migrated Maintenance assets, the old binary will load both the in-tree Maintenance assets and the public activation pin assets concurrently.
- **The Impact:** Since the old binary lacks the `zero-duplicate-active` gate and `packsource` classifier, it will silently load both, resulting in non-deterministic behavior shadowing, double execution of triggers, and corrupt map-merging.
- **Required Change:** Explicitly add the "old binary + activation pin" row to the rollout compatibility matrix (lines 810–819). Mandate that adopting the activation pin is a **hard one-way boundary** for rollback to old binaries, and make the downgrade-limit release notes mandatory with an explicit manual downgrade/clean recovery path.

### 5. [Major] Lack of State-by-Operation Matrix for Classifier States
- **The Risk:** The `packsource` classifier returns typed states such as active bundled, active public, retired generated, retired custom, stale cache, etc. (lines 324–326).
- **The Reality:** The plan fails to specify which of these states are permitted for each runtime operation. For example, is a `retired custom/fork` permitted to satisfy a config load? Is a `stale cache` entry allowed to satisfy a doctor preflight?
- **The Impact:** Without an explicit state-by-operation matrix, different callers (config load, install, lock refresh, cache read, doctor, rollback, etc.) will handle these states inconsistently, leading to containment escapes.
- **Required Change:** Provide an explicit **State-by-Operation Matrix** in the Proposed Implementation, detailing exactly which classifier states are permitted, ignored, or blocked for: (1) active behavior discovery, (2) doctor diagnostics, (3) doctor mutation, (4) cache promotion, and (5) rollback recovery.

### 6. [Major] In-Flight Session/Molecule Execution Survival
- **The Risk:** A multi-agent molecule begins execution under legacy in-tree Maintenance. Slices 5a and 5b are applied mid-execution, removing or ignoring those source paths.
- **The Reality:** The plan does not specify whether step definitions, prompt templates, or scripts are snapshotted into the task store (beads) at composition time, or read live from the pack directory during step execution.
- **The Impact:** If read live, mid-execution slices will cause immediate runtime crashes of running sessions.
- **Required Change:** Explicitly state the persistence policy for in-flight molecules: step definitions, prompt fragments, and script contents must be snapshotted in the task store (beads) at molecule composition/instantiation time, allowing active running sessions to finish executing against their snapshotted state while new dispatches resolve against the newly migrated paths.

### 7. [Major] Dirty Stale Directory "Custom Fork" Promotion False-Positives
- **The Risk:** Stale `.gc/system/packs/maintenance` directories are ignored unless classified as a `retired custom/fork` (lines 324–326, 353–356).
- **The Reality:** If the classifier's custom-fork detection is based on simple file-count checks or directory presence, non-behavioral temporary files or OS metadata (such as `.DS_Store`, `.tmp`, or editor swap files) will falsely promote a pristine stale directory to `retired custom/fork`.
- **The Impact:** The loader will re-activate the old, stale Maintenance formulas, causing instant duplicate active collisions and crashing the city.
- **Required Change:** Require that the `retired custom/fork` state is calculated solely by comparing cryptographic SHA-256 digests of *registered behavior-bearing files* (e.g., `.toml`, `.sh`, `.md` under active roots) against a pre-compiled pristine hash list, explicitly ignoring untracked, empty, or OS-specific paths.

---

## Missing Evidence

1. **Diagnostic Bypass Spec:** Lacks documentation of the `DiagnosticMode` or preflight bypass parameters in `systempacks.LoadRuntimeCity`.
2. **Behavior ID Generation Spec:** Lacks a defined schema for how "behavior ids" are programmatically computed from formulas, orders, prompts, and scripts.
3. **Pristine Hash Registry:** No pre-compiled registry of cryptographic digests for legacy Maintenance/Gastown files is cited.
4. **Wording Allowlist and Terminator Expiry rules:** No explicit expiration schedule for terminology exceptions.

---

## Required Structural & Schema Changes

To approve this plan, the `implementation-plan.md` must be updated to:

1. **Incorporate Doctor Bypass Mode:** Update `systempacks.LoadRuntimeCity` and `LoadRuntimeCityNoRefresh` to support a non-executing bootstrap/diagnostic load mode.
2. **Resolve Compatibility-Pin Deadlock:** Explicitly specify that the compatibility pin excludes assets that duplicate still-required in-tree Maintenance, or make the `zero-duplicate-active` gate warn-only during the compatibility window.
3. **Define Behavior ID Schema:** Define how behavior IDs are computed for each behavior kind to ensure reliable duplicate detection.
4. **Mandate Downgrade Limits:** Explicitly prohibit rolling back to old binaries once the activation pin is adopted, and detail the manual recovery path in the rollout matrix.
5. **Add State-by-Operation Matrix:** Define the allowed states for active behavior discovery, cache reads, and doctor fixes.
6. **Snapshot In-Flight Molecules:** Document that step definitions are snapshotted in beads at composition time to insulate running sessions from mid-migration cuts.
7. **Add Cryptographic Custom-Fork Filter:** Specify that custom/fork status is determined by behavior-bearing SHA-256 digests, ignoring OS-specific and non-behavioral metadata.

---

## Questions

1. **Circular Deadlock:** How does the doctor analyze and repair a city that cannot load because of a duplicate behavior collision?
2. **Behavior Identity:** What exact fields or properties constitute the "behavior id" of a prompt fragment, a script, or a hook overlay?
3. **Compatibility Window:** If an operator is running Slice 2, how does the system distinguish between the required in-tree Maintenance prompts and the imported compatibility-pin prompts without triggering the duplicate-active collision gate?
