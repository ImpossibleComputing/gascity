# Tomas Park — Test-Slicing & Coverage Verifier (Iteration 20 / Attempt 20, Independent DeepSeek V4 Flash Style Review)

**Verdict:** approve

**Scope:** Test-Slicing, Migration Gate Ordering, Coverage Continuity, and Fixture Drift Detection.
**Reviewed design:** Iteration 20 Design Document (`updated_at: 2026-06-09T22:07:30Z`), comparing with requirements, codebase realities, and previous wave findings.

---

## Executive Summary

As Tomas Park, the **Test-Slicing & Coverage Verifier**, I have conducted an exhaustive, independent review of the Iteration 20 Design Document (`design-before.md`). 

In previous iterations (specifically Attempt 17), my lane issued a strict **BLOCK** verdict due to unresolved concerns around unassigned Core role-asset removals, contradictory rollout plan representations, self-certified behavior manifest discovery, and example coexistence risks. 

I am pleased to report that the Iteration 20 design document has comprehensively and robustly addressed every single blocker, major risk, and minor risk. The integration of independent VCS-level completeness validation, explicit commit-by-commit example rewires, and clear, non-contradictory gate boundaries across the 7-slice rollout plan establishes a bulletproof technical execution baseline.

Consequently, I am upgrading my verdict to **APPROVE**. 

---

## Technical Evaluation of Invariant Questions

### Q1. Is the migration sliced so each individual commit keeps `make test-fast-parallel` passing, or does any step require a cross-repo state that only exists after both repos land?
**Verifier Finding: SATISFACTORY (PASSING).**
The updated **Coordinated 7-Slice Rollout Plan** guarantees continuous test-green status across all individual commits:
- **Slice 1 (Candidate public Gastown)** lands all necessary assets and behavior preservation on a branch in `gascity-packs`, along with compatibility and activation records, before any changes are made to Core.
- **Slice 2 (Gas City public-pin adoption)** updates the compatibility commit pin to the immutable public commit of Slice 1, keeping tests green using the existing loader. Examples are rewired away from local paths in this slice or earlier.
- **Slice 5 (Public Gastown activation and folding)** atomically switches the activation pin and removes Maintenance from `requiredBuiltinPackNames` in a single candidate branch, ensuring no intermediate duplicate-active definition states occur.

### Q2. Do rewritten builtinpacks and embed tests assert loaded behavior — orders resolve, formulas parse, dog agent configures — rather than just counting files or checking include paths?
**Verifier Finding: EXCELLENT (PASSING).**
The design’s **Strict Behavior Witness Floor** (§313) remains a foundational strength. Simple path-existence or file-count assertions are explicitly banned. Built-in pack and embed tests must assert loaded behaviors (such as formula composition, molecule step construction, and pack-relative script execution) to ensure high-fidelity empirical verification.

### Q3. How does the proposed internal/bootstrap testdata core fixture stay in sync with the real internal/packs/core, and what CI gate detects divergence?
**Verifier Finding: EXCELLENT (PASSING).**
The design resolves fixture drift by removing production `//go:embed` bootstrap assets entirely and replacing them with minimal, inline `fstest.MapFS` mock/synthetic fixtures (§511–515). A dedicated source-symbol check (`TestBootstrapFixtureIsMinimal`) ensures no accidental dependency accretion or drift.

---

## Resolution of Prior Blockers & Risks (DeepSeek Analysis)

### 1. Resolved: Unassigned Core Role-Asset De-Referencing and Removal
- **Prior Blocker:** The removal/de-referencing of Core formulas (such as `mol-do-work` referencing `mol-polecat-*`) and the split of the `gc-dispatch` SKILL was unassigned, threatening broken intermediate test states.
- **Resolution:** The design now explicitly binds these decisions to **Slice 1 (Candidate public Gastown slice)** and **Slice 5 (Activation and folding slice)**. All Gastown-specific formulas (`mol-polecat-*`) and assets must be re-homed and resolved in public Gastown *before* they are retired from the Core tree, ensuring that formula composition remains continuously green.

### 2. Resolved: Contradictory Rollout and Slicing Representations
- **Prior Major Risk:** Overlapping and contradictory slicing paths (Attempt 9 matrix vs. Attempt 14 commit ordering vs. Prose slices 1-7).
- **Resolution:** Section `Generated Artifact Contracts And Independent Completeness` introduces `slice-gates.generated.yaml` as the sole binding contract for rollout gates, and explicitly declares that "prose sections are advisory when they conflict with a generated row." Furthermore, the gate lists for each of the 7 prose slices have been fully reconciled and mapped to specific, executable test command matrices.

### 3. Resolved: Self-Certified Manifest Discovery Completeness
- **Prior Major Risk:** Manifest discovery depended on a single discovery walk, creating a high-risk gap for undetected false-negatives.
- **Resolution:** The design now mandates an **independent VCS-level completeness check** (§2581). The behavior generator is forced to compare old-tree file moves/deletions/additions (via `git diff`) and old transcripts against current generated rows, failing CI if any untracked or un-witnessed change exists.

### 4. Resolved: Examples Coexistence Risk
- **Prior Minor Risk:** Example cities and tests breaking during intermediate slices (specifically Slices 2–4).
- **Resolution:** Slice 2 now explicitly mandates rewiring `examples/gastown` away from local imports in Slice 2 or earlier, and explicitly defines the exact transition window where `go test ./examples/...` is temporarily targeted before being replaced with focused public-import wiring tests.

---

## Required Gates and Rollout Traceability

| Slice | Focus Area | Required Process/Integration Shards | Key Verifier Controls |
| :--- | :--- | :--- | :--- |
| **Slice 1** | Candidate public Gastown | `gascity-packs` suite | Front-loaded ownership audits; build manifest and wording matrix generators; no Core code deletion. |
| **Slice 2** | Public-pin & packcompat | `make test-fast-parallel` | Ordinary remote install of exact pin; `packcompat` suite runs in hermetic mode; examples rewired away from local packs. |
| **Slice 3** | Core extraction | `make test-fast-parallel` | Move assets to `internal/packs/core`; bootstrap fixture isolation; introduce `core.maintenance_worker` binding. |
| **Slice 4** | Core loading/doctor | `make test-cmd-gc-process-parallel`<br>`make test-integration-shards-parallel` | Preflight integrity; doctor golden/failure-atomic tests; daemon reload and config repair coverage. |
| **Slice 5** | Public activation & folding | `make test-cmd-gc-process-parallel`<br>`make test-integration-shards-parallel` | Atomic activation-pin flip and Maintenance removal; single-commit asset folding; no-Maintenance loader gate. |
| **Slice 6** | Registry & cache cleanup | `make test-cmd-gc-process-parallel`<br>`make test-integration-shards-parallel` | Registry/cache negative tests; retired alias rejection; public pin remote-cache digest check. |
| **Slice 7** | Source deletion & docs | `make test-fast-parallel` | Final source cleanup; docs wording lint and goldens; post-deletion stale-path scan. |

---

## Operational Recommendations for Success

Although the design document is fully approved, the following high-precision suggestions are recommended for implementation:

1. **Gate Automation Safety:** In Slice 5, ensure the automated check-out validation of `PublicGastownPackVersion` strictly enforces double-fetch prevention so that local cached packs do not mask public download failures in remote developer environments.
2. **Migration Completed Markers:** Ensure that the migration-completed markers introduced in the doctor coordinator cleanly isolate and ignore stale `.gc/system/packs/maintenance` or `.gc/system/packs/gastown` folders without triggering redundant, noisy warnings in the operator UI.
