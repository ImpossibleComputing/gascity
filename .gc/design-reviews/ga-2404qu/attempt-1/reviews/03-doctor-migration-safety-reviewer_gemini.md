# Sofia Khoury — DeepSeek V4 Flash (Independent)

**Verdict:** approve

**Lane:** doctor fix idempotency, legacy import rewrites, custom data preservation, operator-safe diagnostics.

Reviewed against the Iteration 23 / Attempt 23 design document `.gc/design-reviews/ga-2404qu/attempt-23/design-before.md` and the active repository workspace.

---

## Executive Summary

The Iteration 23 design represents a major breakthrough for the safety, predictability, and containment of the `gc doctor` and `--fix` mutation lifecycle. Most notably, the critical blocker regarding the plain (no-`--fix`) check has been resolved. By routing plain `gc doctor` strictly through `LoadRuntimeCityNoRefresh` and `ValidateRequiredFileSetsNoRefresh`, the design now guarantees an airtight read-only diagnostic boundary. 

Furthermore, the introduction of named unit and failure-injection tests (`TestCorePackDoctorReportOnlyNoMutation` and `TestCorePackDoctorFixViaCoordinator`) ensures that this read-only behavior is verified programmatically and cannot degrade in future iterations. 

All of my core lane concerns—specifically around custom data preservation, local development path containment, and preflight reachability checks—have been cleanly addressed with robust, first-principles specifications. The design is now fully mature and implementation-ready from the perspective of doctor and import-state safety. Consequently, my verdict is **approve**.

---

## Top Strengths

1. **Airtight Read-Only Diagnostic Boundary (§3876–3899):**
   The plain `gc doctor` run is now strictly read-only and prohibited from executing materialization, repairs, prunes, or renames. By returning `diagnostic_unavailable_without_fix` when participation cannot be computed without a materializing loader, the check remains highly informative without violating the zero-write diagnostic golden.
2. **Named failure-injection test coverage (§3906–3912):**
   The specification of `TestCorePackDoctorReportOnlyNoMutation` ensures that any filesystem mutation attempted by a plain `gc doctor` check will fail the CI gate immediately by asserting that all file hashes and mtimes under the target city remain byte-identical.
3. **CST-Preserving TOML Editor Integration (§3978–3980):**
   Requiring the use of a scoped, CST-preserving TOML editor instead of whole-file serialization prevents the silent loss of hand-authored comments, formatting, and custom tables. Forcing the doctor to refuse and output manual steps on unpreservable structures is a robust fail-safe.
4. **Preflight-Gated Immutable Provenance (§3958–3963, §3987–3989):**
   The requirement that the public Gastown source reachability, cache validity, and installability must be validated under an advisory lock before any write syscall occurs prevents half-migrated, broken states in air-gapped or transient-network environments.

---

## Findings & Residual Risks

### [Minor] Import Edge Comment Preservation during Redundant-Core Removal
- **Severity:** minor
- **Confidence:** high
- **Quality dimension:** correctness
- **Evidence:** `Doctor Fix Safety Contract` (§3940–3944, §3978–3980)
- **Why it matters:** The design instructs the doctor to remove redundant Core imports pointing at legacy sources. While the CST-preserving TOML editor blocks mutations when unrelated comments would be lost, we must ensure that any hand-authored comments on the redundant import *itself* (such as custom developer notes) are not silently dropped. 
- **Suggested fix:** If a redundant `[imports.core]` block contains non-standard, custom comments or local annotations, the doctor should treat it as a custom fork (manual) or preserve the comment block by re-attaching it to an appropriate location in `city.toml`.

### [Minor] Concurrency Window of Mid-Publish Network Drops (TOCTOU)
- **Severity:** minor
- **Confidence:** high
- **Quality dimension:** reliability
- **Evidence:** `Doctor Fix Safety Contract` (§3958–3967)
- **Why it matters:** Preflight reachability checks verify that the public Gastown commit is reachable or cached. However, if the network drops *during* the subsequent stage/publish phase of public Gastown import rewrite, the coordinator might abort mid-flight. Since the advisory lock is already held, we must guarantee that any partially-staged writes or temporary folders are cleanly rolled back, leaving the city byte-identical and eliminating phantom `.tmp` files.
- **Suggested fix:** Explicitly require that the `MutationCoordinator`'s rollback handler cleans up all staged temporary files and directory paths in the `.gc/` workspace on any subsequent publish-phase failure.

---

## Evaluation of Persona Questions

### 1. Is the Core presence doctor fix a proven no-op on a healthy city, including repeated or concurrent runs with a controller active?
**Yes.** Under Iteration 23, the plain doctor is proven inert by `TestCorePackDoctorReportOnlyNoMutation`. For `gc doctor --fix`, concurrent runs are fully serialized via the OS directory advisory flock (rather than brittle status/PID files). The coordinator actively discovers live controller processes from runtime facts and refuses to proceed with automatic fixes, preventing self-heal and doctor write-write or write-read conflicts.

### 2. When `gc doctor --fix` removes redundant Core or legacy Maintenance imports, what prevents it from deleting user-added imports or custom pack edits?
**The Generated-Source Provenance check and CST Editor.** Local legacy Gastown or Maintenance imports are only automatically rewritten/removed when their source paths match known generated system paths (`.gc/system/packs/*` or `examples/gastown/*`). Operator-owned, custom, or edited local paths are classified as manual diagnostics. Additionally, the CST-preserving parser prevents whole-file re-serialization, keeping custom formatting, spacing, and comments untouched.

### 3. If a local Gastown import is rewritten to a public remote, does the fix verify reachability and immutable provenance or fail with explicit operator guidance?
**Yes.** Preflight verification ensures that the public Gastown `sha:` pin is present in the `public-gastown-pins.yaml` ledger and reachable via network or present in a digest-validated local cache before any manifest write occurs. Air-gapped and offline failures are caught during preflight and reported as explicit manual-intervention instructions.

---

**Sofia Khoury approves the Iteration 23 design.**
