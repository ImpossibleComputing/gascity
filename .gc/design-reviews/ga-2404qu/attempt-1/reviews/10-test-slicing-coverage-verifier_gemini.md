# Tomas Park — Test-Slicing & Coverage Verifier (Iteration 19, Independent DeepSeek V4 Flash Style Review)

**Verdict:** BLOCK

**Scope:** Test-Slicing, Migration Gate Ordering, Coverage Continuity, and Fixture Drift Detection.
**Reviewed design:** Iteration 19 Design Document (`updated_at: 2026-06-09T08:40:42Z`, 3515 lines), comparing with requirements, codebase realities, and historical wave findings.

---

## Executive Summary

As Tomas Park, the **Test-Slicing & Coverage Verifier**, I have conducted an independent, technically rigorous review of the Iteration 19 Design Document (`design.md`). While some peer lanes are moving toward approval, my lane mandates a strict mathematical verification of the technical execution, commit-level dependency ordering, and continuous test passability across the entire migration boundary.

My independent audit, styled with DeepSeek V4 Flash precision, reveals that while the design makes exceptional architectural progress—especially with the introduction of the authoritative `slice-gates.generated.yaml` contract and the behavior-evidence witness floor—the actual rollout plan contains several critical blocker contradictions, major completeness gaps, and significant technical slicing risks that mathematically guarantee a broken intermediate or unverified state during execution.

The primary blockers are:
1. **Unassigned Core Role-Asset Deletion & De-referencing:** Core role-asset removal (moving `mol-polecat-*` to public Gastown) is unassigned to any named green slice, meaning formula composition (`mol-do-work` referencing `mol-polecat-*`) will break in intermediate commits.
2. **Prose Rollout vs. Slice Gate Matrix Gaps:** Prose Rollout Slice 4 completely omits running the massive process-level and integration-level sharded tests (`make test-cmd-gc-process-parallel` and `make test-integration-shards-parallel`), whereas the Slice Gate Matrix explicitly mandates them.
3. **Prose Rollout vs. Matrix Stale-Cache Contradiction:** Prose Slice 2 mandates *"stale synthetic-cache rejection for retired aliases"*, but the Slice Gate Matrix specifies *"stale synthetic cache ignored"*. Rejection (fatal error) and Ignoring (silent bypass) are mutually exclusive.
4. **First-Pass Evidence Generation is Floating/Unassigned:** First-pass evidence generation—which produces `behavior-manifest.generated.yaml` and other essential assets before any destructive move—is not assigned to any explicit prose rollout slice.

Accordingly, I must issue a **BLOCK** verdict. Resolving these concerns requires minor text and contract updates to the design document to ensure flawless execution.

---

## Technical Evaluation of Invariant Questions

### Q1. Is the migration sliced so each individual commit keeps `make test-fast-parallel` passing, or does any step require a cross-repo state that only exists after both repos land?
**Verifier Finding: AT RISK (BLOCKING GAPS).**
1. **Commit Steps A–D Duplication:** The transition from Step A to Step D creates an intermediate commit state where both Core-owned Maintenance and the legacy Maintenance pack are present, leading to duplicate active definitions which break config loading and `make test-fast-parallel`.
2. **Unassigned Core De-Referencing:** Core formulas and dispatch skills that reference Gastown-bound formulas/roles are not systematically assigned to a clean pre-activation or post-activation slice, creating broken intermediate states where reference checks fail.
3. **Prose/Matrix Shard Mismatches:** Prose Slice 4 omits the process and integration sharded tests, while the Matrix mandates them. If an implementer only runs `make test-fast-parallel` in Prose Slice 4, they will miss doctor-active and config-repair regressions that are only caught by the process/integration shards.

### Q2. Do rewritten builtinpacks and embed tests assert loaded behavior — orders resolve, formulas parse, dog agent configures — rather than just counting files or checking include paths?
**Verifier Finding: EXCELLENT (MITIGATED BY DESIGN).**
The design’s **Behavior-Oriented Witness Floor** and **Behavior Manifest** are excellent. They explicitly require executing actual witnesses, verifying formula composition, order resolution, and prompt rendering rather than just doing file counts or existence checks.

### Q3. How does the proposed internal/bootstrap testdata core fixture stay in sync with the real internal/packs/core, and what CI gate detects divergence?
**Verifier Finding: SATISFACTORY (MITIGATED BY DESIGN).**
The design decouples bootstrap from production Core entirely, using minimal, inline `fstest.MapFS` mock/synthetic fixtures instead of copying the whole core. This is guarded by `TestBootstrapFixtureIsMinimal` and a stale-path scan to detect any dependency drift.

---

## Critical Risks & Gaps

### 1. [Blocker] Unassigned Core Role-Asset Deletion & De-referencing
* **The Contradiction:** Core "contains Polecat, Refinery, Witness, Mayor, and Gastown references in formulas and skills," and the migration map sends `mol-polecat-base/commit/report` to Gastown while `mol-do-work.toml` is `core-renamed` to "remove references to Polecat, Refinery, and Gastown formulas."
* **The Risk:** No slice names the deletion of the Core `mol-polecat-*` formulas, the `gc-dispatch` SKILL split, or the `mol-do-work` de-referencing. The moment `mol-polecat-*` leaves Core while `mol-do-work` still references it, formula composition fails. The expiring role-token allowlist keeps the scanner green during the gap but cannot keep composition green, and the allowlist's expiry slice is itself undefined.
* **Required Recommendation:** Assign the in-tree Core role-asset removal to a named slice and sequence it so composition stays green: `mol-polecat-*` must be re-homed in public Gastown *before or in the same commit* that removes them from Core, and `mol-do-work` must be de-referenced in that same commit.

### 2. [Blocker] Prose Rollout vs. Slice Gate Matrix Gaps
* **The Contradiction:** Prose Rollout Slice 4 specifies running only `make test-fast-parallel` and `go vet ./...` plus specific package tests. However, the Slice Gate Matrix explicitly mandates running `make test-cmd-gc-process-parallel` and `make test-integration-shards-parallel` shards that cover controller reload and config repair.
* **The Risk:** An implementer following the step-by-step prose rollout of Slice 4 will skip running the process and integration sharded tests. Regressions in daemon reloading and configuration repair will be missed, allowing broken code to land before the full suite is executed in Slice 5.
* **Required Recommendation:** Align Prose Rollout Slice 4 with the Slice Gate Matrix by explicitly adding `make test-cmd-gc-process-parallel` and `make test-integration-shards-parallel` to its required gates list.

### 3. [Blocker] Prose Rollout vs. Matrix Stale-Cache Contradiction
* **The Contradiction:** Prose Slice 2 mandates *"stale synthetic-cache rejection for retired aliases"*, but the Slice Gate Matrix specifies *"stale synthetic cache ignored"*.
* **The Risk:** Rejection (fatal error) vs. Ignoring (silent bypass) are mutually exclusive. This contradiction will lead to divergent test implementations and inconsistent CI gates.
* **Required Recommendation:** Standardize on one behavior. Update both sections to consistently mandate either "rejection" or "ignored" for stale synthetic caches.

### 4. [Major] Floating / Unassigned First-Pass Evidence Generation
* **The Contradiction:** The "first implementation slice" produces evidence before any destructive source move (generating `behavior-manifest.generated.yaml`, role-surface, etc.) but is not assigned to any prose rollout slice.
* **The Risk:** Without a designated "Slice 0" or explicit pre-requisite slice in Gas City assigned to this phase, the generation of the safety evidence is floating.
* **Required Recommendation:** Formally define "Slice 0: First-Pass Evidence Generation" in the Prose Rollout section, assigning the generation and validation of `behavior-manifest.generated.yaml`, `role-surface.generated.yaml`, etc., to an explicit initial rollout gate.

### 5. [Major] Behavior-manifest Completeness is Self-Certified
* **The Contradiction:** Traceability rests on `TestBehaviorManifestFresh` comparing generated-vs-checked-in. But both the generator output and the freshness comparison derive from the *same* discovery walk.
* **The Risk:** A discovery false-negative — an un-walked helper reference or an asset the generator never visits — produces no row, is identical in generated and checked-in copies, and passes both checks silently. The central acceptance claim ("every moved asset has old+new witnesses") cannot be proven complete.
* **Required Recommendation:** Add an independent manifest-discovery completeness check that reconciles manifest rows against a VCS-level enumeration of moved/deleted/added pack assets (e.g. `git diff --name-status` over pack roots), so a discovery false-negative fails CI rather than passing silently.

---

## Required Gates and Rollout Traceability

| Slice | Focus Area | Required Process/Integration Shards | Key Verifier Controls |
| :--- | :--- | :--- | :--- |
| **Slice 0** | Evidence Generation | `make test-fast-parallel` | Generate and validate `behavior-manifest.generated.yaml`, `role-surface.generated.yaml`, and pilot rows. |
| **Slice 1** | Candidate public Gastown | `gascity-packs` suite | Front-loaded ownership audits; build manifest and wording matrix generators; no Core code deletion. |
| **Slice 2** | Public-pin & packcompat | `make test-fast-parallel` | Ordinary remote install of exact pin; `packcompat` suite runs in hermetic mode; examples rewired away from local packs. Consistently reject or ignore stale caches. |
| **Slice 3** | Core extraction | `make test-fast-parallel` | Move assets to `internal/packs/core`; bootstrap fixture isolation; introduce `core.maintenance_worker` binding. |
| **Slice 4** | Core loading/doctor | `make test-cmd-gc-process-parallel`<br>`make test-integration-shards-parallel` | Preflight integrity; doctor golden/failure-atomic tests; daemon reload and config repair coverage. |
| **Slice 5** | Public activation & folding | `make test-cmd-gc-process-parallel`<br>`make test-integration-shards-parallel` | Atomic activation-pin flip and Maintenance removal; single-commit asset folding; no-Maintenance loader gate. |
| **Slice 6** | Registry & cache cleanup | `make test-cmd-gc-process-parallel`<br>`make test-integration-shards-parallel` | Registry/cache negative tests; retired alias rejection; public pin remote-cache digest check. |
| **Slice 7** | Source deletion & docs | `make test-fast-parallel` | Final source cleanup; docs wording lint and goldens; post-deletion stale-path scan. |

---

## Required Changes for Finalization

1. **Assign Core Role-Asset Deletion to Named Slice:** Assign the in-tree Core role-asset removal to a named slice. Re-home referenced formulas in public Gastown before/in the same commit that removes them from Core, and state the expiry slice for each temporary role-token allowlist row.
2. **Align Slice 4 Gates (Blocker):** Update Prose Rollout Slice 4 to explicitly require `make test-cmd-gc-process-parallel` and `make test-integration-shards-parallel` in its list of required gates, resolving the contradiction with Matrix §1493.
3. **Reconcile Stale-Cache Behavior (Blocker):** Update Prose Slice 2 and the Slice Gate Matrix to use identical language ("ignored" or "rejected") for stale synthetic-cache behavior.
4. **Assign Slice 0 for Evidence Generation (Major):** Formally define "Slice 0: First-Pass Evidence Generation" in the Prose Rollout section, assigning the generation and validation of `behavior-manifest.generated.yaml`, `role-surface.generated.yaml`, etc., to an explicit initial rollout gate.
5. **Mitigate Examples Drift (Minor):** Detail the exact mechanism for keeping `go test ./examples/...` passing during the Slice 2-4 transition window when example cities are rewired but local sources are not yet deleted.
6. **Incorporate VCS-level Behavior Manifest Completeness check:** Specify a check that compares manifest rows against moved/deleted/added pack assets via git diff so discovery false-negatives cannot slip through.
