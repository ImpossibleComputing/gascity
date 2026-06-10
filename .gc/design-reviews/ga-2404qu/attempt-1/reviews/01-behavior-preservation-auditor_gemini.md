# Nadia Volkov — DeepSeek V4 Flash Perspective Independent Review (Iteration 21 / Attempt 1)

**Verdict:** Major revision required (BLOCK)

**Scope:** Behavior preservation lane only — Gastown behavior inventory, cross-repo evidence chains, requester/detector/notification continuity, and preventing silent capability loss.

---

## Executive Summary

As Nadia Volkov, the Behavior Preservation Auditor, I am issuing a **Verdict of Major revision required (BLOCK)** for the Attempt 1 design draft. While the transition from in-tree dependencies to an external `gascity-packs/gastown` pack is directionally correct, the current design fails to protect against silent capability loss and lacks operationalized gates for critical execution-level behaviors. 

Specifically, this review highlights major gaps in cross-repo version skew proof, silent notification swallows, un-operationalized trigger changes on moved orders, and witness floor loopholes for rewritten assets.

---

## Critical Risks & Gaps (The Blockers)

### 1. Old-Binary / New-Pack Rollout Deadlock and Lack of Gated Compatibility
- **The Risk:** The release matrix promises compatibility when running an `old binary | new pack` configuration (the new public pack remains compatible with host Core and the absence of Maintenance). However, the design does not require any execution-level gate to prove this matrix cell.
- **The Gap:** The new public pack could easily depend on newly moved Core assets, new loader/provenance behavior, or lack old Maintenance semantics, which will pass the new-binary packcompat test but fail for active operators loading the new pack on older binaries.
- **Required Change:** Add an explicit compatibility gate to the candidate public Gastown slice forcing the new public `gascity-packs/gastown` commit to run and pass load, initialization, and basic script execution with the current pre-migration Gas City binary before the pin is treated as immutable.

### 2. Silent Notification Loss under the Optional-Recipient Model
- **The Risk:** Current Maintenance scripts (`jsonl-export.sh` and `reaper.sh`) explicitly mail the `mayor/` on escalation/anomalies. Transitioning these to config-lookup-based bindings under the attempt-9 role-surface decision converts these active alert paths into silent no-ops when unset.
- **The Gap:** The current witness floor only checks evidence *level* (e.g., verifying "script tolerates empty recipient" satisfies the floor), while the actual alert delivery silently vanishes.
- **Required Change:** Add a mandatory `recipient-binding` field to the behavior-manifest row schema for any row with hardcoded notifications/nudges/mail targets. Force the packcompat witness to assert that alerts actually fire to the configured equivalent recipient in a Gastown-city fixture.

### 3. Order Trigger Semantics are Not Operationalized
- **The Risk:** The design states that packcompat must "exercise the original trigger," but provides no schema field or validation ensuring old-vs-new equality for actual trigger fields (e.g., `trigger = "cooldown"` or `interval = "15m"` on `mol-dog-jsonl`).
- **The Gap:** An order can move to Core or public Gastown, pass composition/script-execution witnesses, yet fire on a weakened interval, a different trigger type, or an exec-vs-pool shape change.
- **Required Change:** Extend manifest rows for moved orders to include trigger-identity fields (trigger type, interval/schedule, gate conditions, exec-vs-pool shape, and enabled state) and require packcompat to assert old == new (or document an approved semantic delta).

### 4. Witness-Floor Loophole for Rewritten, Un-tested Assets
- **The Risk:** For script branches and prompt fragments whose only old witness is "explicit source assertion," the attempt-9 floor permits a moved and role-cleaned asset (digest changed) to land with source-existence witnesses on both ends plus an unverified human "semantic-equivalence assertion."
- **The Gap:** This allows modified assets to escape actual execution-level behavior tests, creating a critical risk of undetected regressions during role-cleaning rewrites.
- **Required Change:** Mandate that any row whose asset content changed during the move (normalized digest mismatch) requires an execution-level final witness regardless of its old evidence level.

---

## Evaluation of the Three Key Questions

1. **Does the behavior inventory enumerate every Gastown-specific requester, detector, notification path, formula, order, script branch, and prompt fragment removed from Core?**
   - **Auditor Finding: No.** The current design lacks a concrete generated manifest (first-slice deliverable), and does not specify a generated source-manifest starting point, allowing behaviors to be silently omitted from the inventory.
2. **Which concrete `gascity-packs/gastown` tests prove each restored behavior fires under the same trigger conditions rather than merely existing?**
   - **Auditor Finding: Unsatisfactory.** The design lacks trigger-field equality validations and permits empty-recipient tolerance to satisfy witness requirements, bypassing actual firing assertions.
3. **Can reviewers trace each high-risk Maintenance or Core move to old path, new path, landing commit, and observable test evidence?**
   - **Auditor Finding: No.** The tracing mechanisms are vague; there is no frozen old-tree baseline commit designated for the independent completeness check, and role-surface rows are not cross-linked with behavior-manifest rows.

---

## Questions & Clarifications Needed

1. **What is the designated baseline old-tree commit?** There must be a specific, immutable Gas City commit designated as the reference for old-tree file states and baseline transcripts to make independent auditing meaningful.
2. **Are there any other hardcoded notification paths in Maintenance or Core?** Beyond `reaper.sh` and `jsonl-export.sh`, we need a complete scan to ensure no other embedded alert pathways (like `DOG_DONE` nudges or DB compactors) are silently converted into optional config lookups.
3. **What blocks Slice 1 from declaring manifest completeness?** If all execution-level proof is deferred to Gas City's `test/packcompat`, does `gascity-packs` have any test suite capable of validating moved behavior on its own at Slice 1?
