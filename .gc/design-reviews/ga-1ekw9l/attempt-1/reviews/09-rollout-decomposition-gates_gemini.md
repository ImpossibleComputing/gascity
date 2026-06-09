# Iris Kowalski — Gemini (Rollout & Decomposition Gates Reviewer, Attempt 4, Independent DeepSeek V4 Flash Style)

**Verdict:** block

> **Lane:** independently deployable slices, decomposition readiness, prerequisite honesty, exact gates, cross-repo sequencing, and test coverage.
>
> Reviewed against the Attempt 4 design document (`.gc/design-reviews/ga-1ekw9l/attempt-4/design-before.md`, 657 lines, `updated_at: 2026-06-09T07:28:00Z`) — §"Rollout And Recovery" (lines 561–647), §"Testing" (lines 487–560), §"Bootstrap Fixture Isolation" (lines 370–396), §"Doctor And Runtime-State Mutation Safety" (lines 286–327), and the parent `requirements.md` (`updated_at: 2026-06-09T08:58:00Z`).
>
> This independent review is produced using the DeepSeek V4 Flash persona. It focuses on cross-document consistency, latent edge cases, and assumptions that other reviewers may accept too quickly.

---

## 1. Executive Summary

As Iris Kowalski, the **Rollout & Decomposition Gates Reviewer**, I have performed a rigorous, first-principles independent review of the Attempt 4 implementation plan. My final determination is **Verdict: block**.

This iteration (Attempt 4) represents a massive leap forward in addressing previous rollout and decomposition hazards. Specifically, splitting the previously batched rollout into granular, distinct slices (Slices 1a–1c and Slices 4a–4c, 5a–5b) and placing the runtime-state migration explicitly into the timeline (Slice 4c) provides a highly structured and logical rollout framework.

However, several critical bootstrapping paradoxes, deployment deadlocks, and cross-document inconsistencies remain unaddressed:
1. **The `GC_BOOTSTRAP=skip` / Gate 1 Paradox:** Requiring Gate 1 fileset validation to run unconditionally under `GC_BOOTSTRAP=skip` breaks fast isolated unit tests, as no materialized Core pack exists in those isolated environments.
2. **Slice 5a Rollout Deadlock:** Loading the public activation pin while local legacy sources are still present on disk will trigger the strict `zero-duplicate-active` gate, crashing every city load during this transition step and halting the upgrade.
3. **Cross-Document Release Matrix Gap:** The Release Compatibility Matrix completely omits the "old binary + new activation pack" scenario explicitly mandated in AC15 of `requirements.md`.
4. **The "Manual Guidance Only" Upgrade Dead-End:** Over-relying on manual guidance for legacy pack imports will break non-interactive CI/CD pipelines, locking them in degraded states.

This block is constructive. Resolving these four critical issues will provide a flawless, production-ready rollout plan.

---

## 2. Detailed Responses to Lane-Specific Questions

### Q1: Can tasks be cut so each slice names concrete files, acceptance gates, cross-repo prerequisites, and a revert or one-way upgrade boundary before merge?

**Answer:** 
Yes, the proposed slicing framework (Slices 1a–7) successfully establishes clear, sequential boundaries. Revert boundaries are explicitly named (e.g., Slices 2, 3, 4a, and 6), and one-way upgrade boundaries are identified (such as Slice 5b removing Maintenance). 

However, the plan still lacks a **direct file-to-slice mapping** and does not explicitly bind each slice to its exact validation commands or required artifacts (see Critical Risks). Without this, a decomposer cannot safely generate independent, self-contained beads.

### Q2: Are open questions truly resolved, or are ownership audits, generated artifacts, and gascity-packs branch availability deferred as hidden blockers?

**Answer:** 
They are partially resolved in design, but remain **physically absent** from the repository, creating a major rollout risk. While the plan declares `Open Questions: None` (line 649), it relies on the physical creation of multiple complex artifacts (e.g., `test/packcompat`, the Behavior Evidence manifest, the public pin ledger) during "decomposition" or as "external prerequisites" (lines 652-656).

By deferring the creation of these generator scripts and schemas, the plan risks a scenario where the implementation proceeds without a validated, executable baseline. True "decomposition readiness" requires that the schema, generator, and test harness exist *before* the first implementation beads are executed.

### Q3: Does each intermediate commit pass the documented local gates and exercise production loaders rather than copied fixtures or direct config.Load shortcuts?

**Answer:** 
The design successfully mandates that production paths and intermediate slices route exclusively through `LoadRuntimeCity` or `LoadRuntimeCityNoRefresh` (lines 205-211) and provides AST-based scanners to reject direct `config.Load` bypasses (lines 235-242).

However, as highlighted in Critical Risk #1, this requirement creates an immediate deadlock for isolated unit tests running under `GC_BOOTSTRAP=skip`, as they will be unable to satisfy the mandatory pre-resolution fileset validation (Gate 1).

---

## 3. Critical Risks & Architectural Inconsistencies

### 3.1. [Blocker] The `GC_BOOTSTRAP=skip` / Gate 1 Bootstrapping Paradox
* **The Vulnerability:** Under §"Bootstrap Fixture Isolation" (lines 391–396), the plan specifies that `GC_BOOTSTRAP=skip` "must not skip `internal/systempacks` materialization, strict required Core file-set integrity...". However, the core purpose of `GC_BOOTSTRAP=skip` in unit and testscript tests is to execute in a lightweight, isolated environment *without* materializing or embedding a production Core fileset.
* **The Impact:** If Gate 1 (Pre-Resolution fileset validation) is strictly mandatory and cannot be bypassed or mocked under `GC_BOOTSTRAP=skip`, then any test executed with this environment variable will immediately fail Gate 1 because the required Core fileset is physically absent from disk. This introduces a bootstrapping deadlock: we cannot run fast unit/testscript tests without materializing the production Core, which completely defeats the purpose of test isolation and the `GC_BOOTSTRAP=skip` escape hatch, breaking CI.
* **Resolution:** Specify that in test mode under `GC_BOOTSTRAP=skip`, the file-set validator (Gate 1) must validate against the empty/minimal mock bootstrap fixture or accept a pre-computed test digest, rather than demanding a full production Core fileset.

### 3.2. [Blocker] Slice 5a / Compatibility Window Rollout Deadlock (Duplicate Active Behaviors)
* **The Vulnerability:** Slice 5a (lines 605–609) "consumes the public activation pin in a candidate branch and runs packcompat in no-Maintenance production-loader mode while local sources are still present."
* **The Impact:** If local sources (the legacy `examples/gastown/packs/maintenance` etc.) are still present on disk, and we load the public activation pin (which contains the newly migrated Maintenance and Gastown assets), the strict `zero-duplicate-active` gate (lines 277-279) will detect the same behavior IDs from both the local disk paths and the imported public pack. This will cause an immediate loader crash on every single city load during this transition step. Slices 2–5a will be completely unrunnable, deadlocking the entire rollout.
* **Resolution:** Specify that in Slice 5a, when running the `no-Maintenance` loader mode, the classifier in `internal/packsource` must actively ignore the local legacy Maintenance/Gastown directories even if they are present on disk, preventing them from being scanned and triggering duplicate behavior collisions.

### 3.3. [Major] Cross-Document Inconsistency: Release Compatibility Matrix Omission
* **The Vulnerability:** Under `requirements.md` (AC15, lines 89-91), it is explicitly stated: *"Old binary plus activation pin is unsupported and must fail closed with downgrade guidance."*
* **The Impact:** The Release compatibility matrix in `implementation-plan.md` (lines 632-641) lists "new binary + old locked pack", "new binary + new activation pack", and "rollback from new to old", but completely omits the **"old binary + new activation pack"** (old binary + activation pin) scenario. This is a direct cross-document inconsistency that risks a developer omitting the required fail-closed implementation on the older binary.
* **Resolution:** Add an explicit row to the Release compatibility matrix for `old binary + new activation pack` showing that it fails closed with explicit downgrade or re-upgrade guidance.

### 3.4. [Major] The "Manual Guidance Only" Upgrade Dead-End
* **The Vulnerability:** Under §"Doctor And Runtime-State Mutation Safety" (lines 292-294), the plan states: *"Existing doctor checks either return a structured intent through that API or are marked report-only with manual guidance."*
* **The Impact:** If critical legacy configurations (such as retired Maintenance pack imports or legacy Gastown local imports) are marked "report-only with manual guidance" rather than being automated through the mutation coordinator, then running `gc doctor --fix --non-interactive` in automated deployment pipelines will fail to resolve the issue. The loader will remain in `read_only_degraded` or `blocked` mode, but the automated pipeline has no way to apply "manual guidance," leading to permanent automated-deployment failures.
* **Resolution:** The plan must guarantee that all standard, non-custom legacy import shapes (including default Maintenance and Gastown paths) have fully automated, idempotent, and non-interactive `FixIntent` implementations, reserving "manual guidance" strictly for highly customized, unclassifiable user-fork configurations.

### 3.5. [Major] Lack of Formal Gate-to-Slice Binding Tables
* **The Vulnerability:** While §"Testing" lists various tests and §"Rollout and Recovery" lists slices, the plan fails to provide a structured table mapping each rollout slice to its exact merge-blocking proof commands, required artifacts, and validation tests.
* **The Impact:** Decomposers creating beads for Slices 1a through 7 have no formal contract stating what exact verification checks must pass before a slice can be merged. This creates a high risk that intermediate slices (such as Slice 3 or 4a) will be merged with under-tested loader or doctor changes, causing regressions.
* **Resolution:** Add a structured Rollout Gate Table under §"Rollout and Recovery" that binds every slice to its exact verification commands, required artifacts, and rollback scripts.

---

## 4. Evaluation against Lane Anti-patterns

| Anti-pattern / Risk | Mitigation in Attempt 4 Design | Status |
| :--- | :--- | :--- |
| **Tasks batch pin changes, source deletion, doctor mutation, docs, and activation into one fragile landing** | **Resolved.** Slices 2, 4a, 4b, 4c, 5a, 5b, 6, and 7 cleanly separate these phases. | **Pass** |
| **Status says decomposition-ready while required generators or public pack commits do not exist** | **Vulnerable.** The schemas, generators, and test harnesses do not physically exist in the workspace, yet the plan states "Open Questions: None". | **Fail-Closed Risk** |
| **Fast unit tests are used as the only proof for loader, doctor, runtime-state, and cross-repo behavior changes** | **Resolved.** §Testing (lines 513–530) explicitly incorporates `packcompat`, `packlint`, and sharded process/integration tests. | **Pass** |

---

## 5. Required Plan Updates

To achieve decomposition readiness, the implementation plan must be updated with the following:

1. **Test-Specific Gate 1 Exception:** Clarify that under `GC_BOOTSTRAP=skip` in test environments, the fileset validator (Gate 1) runs against the mock/empty bootstrap fixture rather than the production Core fileset.
2. **Slice 5a Local Source Ignorance:** Mandate that during Slice 5a's `no-Maintenance` loader verification, the classifier explicitly ignores local legacy directories to prevent duplicate-active behavior collisions.
3. **Explicit Release Matrix Entry:** Add the `old binary + new activation pack` row to the Release compatibility matrix, confirming it fails closed with downgrade/re-upgrade guidance.
4. **Automated Fix Guarantee:** Require that all standard legacy import and cache shapes must be automated via `FixIntent` rather than falling back to report-only manual guidance.
5. **Structured Rollout Gate Table:** Embed a table mapping each slice (1a to 7) to its exact gate verification commands, required artifacts, and rollback steps.

---

## 6. Questions

1. How can a test running with `GC_BOOTSTRAP=skip` pass Gate 1 fileset validation if the required Core fileset is not materialized or embedded?
2. How does Slice 5a avoid the `zero-duplicate-active` gate crash when both the public activation pin and local legacy directories contain the migrated Maintenance/Gastown assets?
3. What is the explicit fail-closed outcome and operator guidance when an older binary attempts to load a city pinned to the new activation-level public Gastown pack?
