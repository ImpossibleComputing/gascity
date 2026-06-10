# Sofia Khoury — DeepSeek V4 Flash Independent Review (Iteration 22 / Attempt 22)

**Verdict:** approve-with-risks

**Lane:** doctor fix idempotency, legacy import rewrites, custom data preservation, operator-safe diagnostics.

Reviewed strictly against the Iteration 22 / Attempt 22 draft of `design-before.md` and `requirements.md` in the active repository workspace.

---

## Executive Summary

The Iteration 22 draft of the Core/Gastown pack split migration design integrates significant, high-quality updates over previous iterations. The addition of the "Evidence-generation slice 0" to produce a generated crosswalk (`slice-gates.generated.yaml`) is a major improvement for tracking and verification. The release compatibility matrix has also been significantly hardened with explicit models for the "one-way boundary" of the activation pin and rollback recovery transcripts.

However, under close inspection in my lane, a few critical safety-critical assumptions and edge cases around older-binary `gc doctor --fix` inertness and rollback guidance remain unaddressed. While the design is structurally sound and extremely thorough, these residual gaps must be resolved or explicitly documented before final merge.

Consequently, my verdict is **approve-with-risks**, contingent on addressing or documenting the risks identified below.

---

## Top Strengths

1. **Rigorous Version-Skew and Rollback Modeling (§Release Compatibility Matrix):**
   The formalization of the "one-way boundary" for the activation pin is extremely robust. Using undecoded TOML keys (`target_binding`/`gc.run_target_binding`) to trigger `fatalUndecodedWarnings` in older binaries is an elegant, zero-overhead fail-fast guard that prevents older systems from loading incompatible configurations.
2. **Introduction of Evidence-Generation Slice 0 (§Rollout):**
   Adding Slice 0 to generate schemas, freshness tests, and the `slice-gates.generated.yaml` crosswalk is an exceptional software engineering practice. This prevents "document drift" and ensures that the implementation of later slices is grounded in verifiable, generated rules.
3. **Hardened Quality Gates with Sharded Test Sweeps (§Rollout Slices 4 & 6):**
   Mandating broad, process-level and integration-sharded tests (`make test-cmd-gc-process-parallel` and `make test-integration-shards-parallel`) alongside unit tests ensures that no subtle regressions leak into downstream implementation phases.
4. **CST-Preserving TOML Editor Integration (§3709–3714):**
   Localizing TOML edits to CST spans rather than whole-file serialization prevents the silent loss of custom operator comments, spacing, and array layouts, satisfying a core lane requirement for data preservation.

---

## Fix Validations

We validated the proposed `gc doctor --fix` and Core Presence Doctor against the active implementation design and found that the core safety and consistency gaps have been largely mitigated:
- Plain `gc doctor` is strictly report-only (`LoadRuntimeCityNoRefresh`) and zero-write, verified by named tests (`TestCorePackDoctorReportOnlyNoMutation` and `TestCorePackDoctorFixViaCoordinator`).
- Direct writes are forbidden; all edits are routed through `doctor.MutationCoordinator` under an OS directory advisory lock.

However, the newly added skew/rollback scenarios introduce new behavioral risks that require validation.

---

## Findings

### [Backward Compatibility / Risk] The "Old Doctor Mutation Inertness" Assumption
- **Severity:** major
- **Confidence:** high
- **Quality dimension:** correctness
- **Evidence:** `Release Compatibility Matrix` ("old binary `gc doctor --fix` | any migrated city")
- **Why it matters:** The matrix claims that older binaries running `gc doctor --fix` against a migrated city are "either proven inert ... or every legacy whole-file mutation it can perform is named with its recovery path in release notes." Relying on "release notes" for recovery from silent destructive mutations by old binaries is highly risky. If an operator runs `gc doctor --fix` from an old binary, the old binary has no knowledge of public pins or migration markers. It may blindly overwrite `city.toml` and strip out new configuration elements or revert import paths.
- **Suggested fix:** Ensure that the old binary's config-load failure on undecoded keys (triggered by the new keys in the activation pin) actively blocks its `doctor --fix` command from executing any write/rename syscalls. If the config loader fails, the doctor must exit immediately with an error before attempting to write any file.

### [User Experience / Consistency] Ambiguity of "Doctor Output Downgrade Guidance"
- **Severity:** minor
- **Confidence:** high
- **Quality dimension:** correctness
- **Evidence:** `Release Compatibility Matrix` ("rollback from new to old")
- **Why it matters:** The matrix states that rolled-back cities get "explicit downgrade guidance in doctor output." However, an older binary's doctor has no knowledge of the migration and cannot print downgrade guidance. Thus, this guidance must be written *on-disk* by the new binary during the migration itself (e.g., as comments in `city.toml` or in a `.gc/migration-rollback-instructions.txt` file), or the "doctor output" refers solely to the new binary printing instructions *before* the rollback is initiated.
- **Suggested fix:** Clarify the physical mechanism for displaying downgrade instructions. Ideally, the new binary should write a clear warning comment into `city.toml` (e.g., `# gc:migrated-v2 - older binaries require rollback instructions: ...`) during the `--fix` mutation, ensuring it remains visible to operators inspecting the file after rollback.

### [Reliability / Concurrency] TOCTOU in Preflight Reachability and Cache Lookup
- **Severity:** minor
- **Confidence:** high
- **Quality dimension:** reliability
- **Evidence:** `Doctor Fix Safety Contract` (§3692–3705)
- **Why it matters:** Preflight verifies that the public Gastown pin is reachable or present in a cached entry. However, if the network drops or cache lookup times out *during* the subsequent stage/publish phase, the coordinator might abort mid-flight. Since the advisory lock is already held, we must ensure that the preflight cache-lookup is entirely transactional and read-only, and that any staged writes are cleanly rolled back (leaving the city byte-identical) if the publish phase fails.
- **Suggested fix:** Add a strict test fixture verifying that the `MutationCoordinator` is entirely inert (performs zero writes/renames) and leaves no temp files behind when preflight fails due to transient network drops during a public-pin rewrite.

---

## Evaluation of Sofia's Critical Questions

### 1. Is the Core presence doctor fix a proven no-op on a healthy city, including repeated or concurrent runs with a controller active?
**Yes.** The design has been successfully hardened to ensure that plain `gc doctor` is strictly report-only, and `gc doctor --fix` uses `doctor.MutationCoordinator` to perform compare-before-rename checks, bypassing writes if files are byte-identical. Concurrent runs are serialized by the OS advisory directory lock, and the coordinator refuses to run if a live controller process is discovered from live runtime state.

### 2. When `gc doctor --fix` removes redundant Core or legacy Maintenance imports, what prevents it from deleting user-added imports or custom pack edits?
**The CST-preserving TOML editor and the Retired-Source Classifier.** The parser prevents whole-file re-serialization, keeping custom comments and array layouts. The classifier tags non-standard edits as "custom local forks" and blocks automated mutations, redirecting operators to manual guidance. However, we must ensure that manual import edges containing preservation comments (e.g. `# gc:preserve`) are skipped by automatic doctor removal and flagged for manual confirmation.

### 3. If a local Gastown import is rewritten to a public remote, does the fix verify reachability and immutable provenance or fail with explicit operator guidance?
**Yes.** Preflight checks verify that the immutable version is present in `public-gastown-pins.yaml` and reachable or cached. Air-gapped states produce explicit actionable failures with no embedded fallback, and operator transcripts for those states are required artifacts, satisfying safety requirements.

---

## Required Changes for Finalization

1. **Harden Old Binary Doctor Blocks:** Ensure the older binary's config load failure (triggered by undecoded keys) actively blocks the old `gc doctor --fix` run from performing any write/rename syscalls.
2. **Clarify Downgrade Guidance Location:** Update the description of "downgrade guidance in doctor output" to specify that the new binary writes a persistent warning comment into `city.toml` during the `--fix` mutation, or that instructions are placed in `.gc/`.
3. **Add Network/Cache Failure Rollback Test:** Add a test verifying that the `MutationCoordinator` leaves the city completely byte-identical with zero temp files if a network drop or cache timeout occurs halfway through a public-pin rewrite.

---

## Consistency Report
- **Patterns checked:**
  - Core Presence Doctor Check (§3605–3646)
  - Doctor Fix Safety Contract (§3683–3730)
  - Release Compatibility Matrix (§Rollout)
- **Sibling files checked:**
  - `requirements.md` (Design-review inputs)
- **Drift detected:**
  - None. All previous contradictions on Plain-Doctor read-onliness and dog prompt ownership have been resolved and aligned in place in Attempt 22.

---

**Sofia Khoury approves the design with risks documented above.**
