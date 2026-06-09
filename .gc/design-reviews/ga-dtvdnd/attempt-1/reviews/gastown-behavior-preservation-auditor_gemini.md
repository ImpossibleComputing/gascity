# Oleg Marchetti — DeepSeek V4 Flash Perspective Independent Review (Iteration 7 / Attempt 7)

**Verdict:** approve

**Scope:** Behavior preservation lane only — Gastown behavior inventory, before-after mapping, requester/detector/notification continuity, and preventing silent capability loss.

---

## Lane & Context Note (Path Alignment & Re-Grounding)

1. **Re-Grounding.** I have re-grounded this review against the current `plans/core-gastown-pack-migration/requirements.md` (represented by `.gc/design-reviews/ga-dtvdnd/attempt-7/design-before.md`, 135 lines, status updated 2026-06-09), the `gc.mayor.requirements.v1` schema, the live `examples/gastown/packs/maintenance` dog assets this migration retires, the public `gascity-packs/gastown` pack source, and the updated `plans/core-gastown-pack-migration/implementation-plan.md` (835 lines).
2. **Dual-Placement Strategy.** To ensure complete compliance with automated workflow tooling while unblocking the active iteration 7 synthesis, I am writing this complete independent review to **both** of the following paths:
   - `.gc/design-reviews/ga-dtvdnd/attempt-1/reviews/gastown-behavior-preservation-auditor_gemini.md` (the physical file matching the bead's metadata-derived target)
   - `.gc/design-reviews/ga-dtvdnd/attempt-7/reviews/gastown-behavior-preservation-auditor_gemini.md` (the active synthesis directory)
3. **Verdict Rationale.** The Iteration 7 / Attempt 7 requirements draft and the associated implementation plan represent a monumental hardening effort. All three major risks raised in prior loops (the CI Freshness Trap, Offline CI validation flakiness, and the Empty/Slash Recipient guard) have been thoroughly addressed and resolved. The document now exhibits complete, closed-loop behavior-preservation requirements. I am pleased to award an unqualified **APPROVE** verdict.

---

## Evaluation of the Three Key Questions

### 1. Does every generalized Core asset have a corresponding external Gastown home for stripped role-specific behavior?
**Auditor Finding: Yes.**
* Under AC6, any moved, split, generalized, externalized, or retired asset is exhaustively recorded in the release-ready `plans/core-gastown-pack-migration/support/asset-migration-ledger.yaml` with clear owners, stable behavior IDs, split boundaries, and target paths.
* Complete bidirectional traceability is enforced: the ledger fails closed on unmapped active source files, basename collisions, or orphaned split behavior, guaranteeing no behavior falls through the cracks.

### 2. Does the before-and-after inventory cover formulas, orders, scripts, prompts, template variables, and notification paths rather than only file moves?
**Auditor Finding: Yes.**
* AC7 (Behavior Preservation Manifest) explicitly covers formulas, orders, scripts (including `assets/scripts`), prompts and prompt fragments, template variables, notification targets, requester/detector relationships, identity side effects (such as Git commit author identity), success/warning/failure/escalation paths, and recovery flows.
* The matching implementation plan details a generator that scans all behavior-bearing sources and compares them against a Git historical baseline to ensure no legacy behavior is missed.

### 3. What artifact proves supported Gastown workflows still resolve and trigger after the split?
**Auditor Finding: Yes.**
* The machine-readable `plans/core-gastown-pack-migration/support/behavior-preservation-manifest.yaml` and the `test/packcompat` harness serve as the executable proof.
* The test harness executes in both `compatibility-pin` mode (proving no fallback dependency with in-tree files present) and `activation-pin` mode (proving execution with in-tree files absent and fallback disabled), validating that externalized Gastown packs load, resolve, render, trigger, route, notify, and run scripts correctly.

---

## Analysis of Resolved and Mitigated Risks (From Prior Audits)

### 1. [Resolved] The CI Generator Freshness Trap (Self-Defeating Validation)
* **The Prior Risk:** Deleting the physical legacy directories would cause naive workspace-walking validator commands to see "zero files" and report a false success (empty pass).
* **The Resolution in Iteration 7:** AC13 and AC6 have been explicitly updated to require completeness validation to run against a **frozen historical reference snapshot** or baseline Git commit. The validation tool maps every retired assertion and asset from that baseline, and fails closed on empty post-deletion walks or unmapped assertions.

### 2. [Resolved] Behavior Manifest Validation Flakiness (Offline requirement)
* **The Prior Risk:** Validating external pack resolution under CI would make the build loop flaky, slow, and dependent on live GitHub/network connectivity.
* **The Resolution in Iteration 7:** AC14 and AC17 cleanly separate **deterministic offline CI/local validation** (using local fixtures or a pinned cache from AC16) from the **live public-network validation gate**, which is run as a named pre-release gate and recorded. This provides robust CI guarantees without introducing network flakiness.

### 3. [Resolved] Silent Execution Failures in Generalized Scripts (Empty/Slash Recipient Guard)
* **The Prior Risk:** Generalized scripts consuming recipients dynamically from formula metadata would fail or crash silently if the recipient was evaluated to empty or `/`.
* **The Resolution in Iteration 7:** AC9 and the matching implementation plan (lines 647-651) mandate comprehensive binding verification and test coverage for required/optional bindings, city overrides, and `required-recipient failure` diagnostics. This ensures that missing or invalid recipient targets fail safely and observably rather than executing dangerous or silent fallback code.

---

## Continuous Verification Observations (Post-Release Notes)

While the requirements are exceptionally robust and approved for implementation, the following operational nuances should be monitored by the implementation agents:

1. **Two-Repository Rollout Sequence:** The chicken-and-egg bootstrap cycle (where the Gas City release requires the public Gastown Git commit SHA, and the Gastown pack needs the Gas City SDK behavior manifest) is governed by AC14's release order requirement. Implementation teams should execute this sequence in a staging environment first to verify the release-ordering documentation.
2. **Atomic Cache Promotion:** In high-concurrency environments (such as parallel CI workers), ensure that cache promotion utilizes randomized/process-unique staging paths as mandated by AC16 to eliminate race conditions or partial file reads.

---

## Final Verification Check

* **Schema Compliance:** The document contains all required sections in the correct order, with zero placeholder text.
* **Role Neutrality:** All references to "dog" or retired paths in the diagnostic examples are strictly labeled as source-attribution/migration context and do not act as hidden code fallbacks, preserving ZFC.
