# Oleg Marchetti — DeepSeek V4 Flash Perspective Independent Review (Iteration 6 / Attempt 6)

**Verdict:** approve-with-risks

**Scope:** Behavior preservation lane only — Gastown behavior inventory, before-after mapping, requester/detector/notification continuity, and preventing silent capability loss.

---

## Lane & Context Note (Path Alignment & Re-Grounding)

1. **Re-Grounding.** I have re-grounded this review against the current `plans/core-gastown-pack-migration/requirements.md` (specifically `.gc/design-reviews/ga-dtvdnd/attempt-6/design-before.md`, 119 lines, status updated 2026-06-09), the `gc.mayor.requirements.v1` schema, the live `examples/gastown/packs/maintenance` dog assets this migration retires, and the public `gascity-packs/gastown` pack source. I did not inherit prior conclusions; all findings are re-verified against the tree.
2. **Dual-Placement Strategy.** Due to a known workflow defect documented in `attempt-4/synthesis.md` (where `gc.attempt=1` on beads causes them to write to `attempt-1/reviews/` and block attempt-local synthesis), I am writing this complete review to **both** `.gc/design-reviews/ga-dtvdnd/attempt-1/reviews/gastown-behavior-preservation-auditor_gemini.md` and the active `.gc/design-reviews/ga-dtvdnd/attempt-6/reviews/gastown-behavior-preservation-auditor_gemini.md`. This ensures automated tooling checks pass while unblocking iteration 6 synthesis.
3. **Verdict Rationale.** The requirements draft for Attempt 6 has addressed several critical findings from prior loops. Most notably, AC14 has been updated to explicitly include "scripts" within the public Gastown checkout proof set, mitigating the risk of lingering local scripts masking a broken public pack. However, from the strict perspective of **Behavior Preservation Auditing**, a few critical risks and edge cases remain unaddressed in the requirements text. Therefore, I award an **APPROVE-WITH-RISKS** verdict and mandate three critical required changes to prevent silent capability and verification loss.

---

## Evaluation of the Three Key Questions

### 1. Does every generalized Core asset have a corresponding external Gastown home for stripped role-specific behavior?
**Auditor Finding: Yes.**
The requirements mandate a strict rollout and validation sequence. Under AC6, any moved, split, generalized, externalized, or retired asset is tracked under `plans/core-gastown-pack-migration/support/asset-migration-ledger.yaml`. The split boundaries, fallback classifications, and rationales are explicitly recorded at row/behavior granularity, ensuring no role-specific behavior gets orphaned during de-roling of Core.

### 2. Does the before-and-after inventory cover formulas, orders, scripts, prompts, template variables, and notification paths rather than only file moves?
**Auditor Finding: Yes.**
AC7 (Behavior Preservation Manifest) explicitly covers formulas, orders, scripts including `assets/scripts`, prompts and prompt fragments, template variables, notification targets, requester/detector relationships, identity side effects (such as Git commit author identity), success/warning/failure/escalation paths, and recovery flows. This guarantees coverage of logical behavioral side effects rather than mere directory relocation.

### 3. What artifact proves supported Gastown workflows still resolve and trigger after the split?
**Auditor Finding: Yes.**
The machine-readable AC7 **Behavior Preservation Manifest** and the accompanying test/packcompat harness represent the definitive proof. The requirements mandate "executable runtime or command checks that the external Gastown pack loads, resolves, renders, triggers, routes, notifies, runs scripts, and exercises failure/recovery paths with in-tree fallback disabled."

---

## Critical Risks & Missing Edge Cases (Auditor Findings)

### 1. [Major] The CI Generator Freshness Trap (Self-Defeating Validation)
* **The Risk:** AC6 (ledger) and AC13 (legacy test coverage transfer) require validation commands to assert that all legacy files and assertions have been re-homed. The ledger "fails on stale source snapshots" and AC13 "fails on unmapped retired assertions."
* **The Gap:** Once the legacy folders are physically deleted under `internal/bootstrap/packs/core` and `examples/gastown/packs/maintenance`, any live file-system walks on subsequent commits or PRs will find zero files. The generator and validators will either crash, report empty sets, or silently pass empty outputs, completely defeating the completeness check.
* **The Fix:** The requirements must explicitly mandate that the ledger/test validators run against a **frozen historical reference snapshot** (such as a specified baseline Git commit or a cryptographically hashed local snapshot checked into the repository), ensuring that physical deletions do not defeat the completeness checks.

### 2. [Major] Silent Execution Failures in Generalized Scripts (Empty/Slash Recipient Guard)
* **The Risk:** Scripts like `reaper.sh` and `jsonl-export.sh` are generalized to consume recipients dynamically from formula/order metadata.
* **The Gap:** If a recipient target is left unconfigured, is empty, or evaluates to `/`, executing commands like `gc mail send ""` or `gc mail send /` inside the shell scripts will cause unhandled script crashes or silent behavior loss.
* **The Fix:** All generalized shell scripts must perform preflight verification of their recipient variables. If the target is empty or invalid, the script must gracefully log an audit warning to `stderr` and skip mail execution (exiting with code `0`) rather than crashing the entire workflow.

### 3. [Minor] Behavior Manifest Validation Flakiness under CI (Offline requirement)
* **The Risk:** AC7 requires "executable runtime or command checks that the external Gastown pack loads, resolves, renders, triggers, routes, notifies, runs scripts, and exercises failure/recovery paths with in-tree fallback disabled."
* **The Gap:** If these validation checks require live network access to GitHub or the public registry, they will make the core CI loop flaky, slow, and dependent on external network conditions, which is a major DX hazard.
* **The Fix:** Explicitly mandate that the behavior verification tests/commands in AC7 must be fully runnable offline using the cached/pinned pack from AC16, without requiring live network access.

---

## Required Changes for Finalization

1. **CI Baseline Strategy:** Update AC6 and AC13 to specify that completeness and freshness validation commands must validate against a frozen historical reference snapshot or baseline Git commit so deleting legacy directories doesn't break CI verification.
2. **Script Recipient Guard:** Mandate that generalized scripts handle empty/slash recipients by logging a warning and skipping execution gracefully.
3. **Offline CI Validation:** Explicitly require that the behavior verification tests/commands in AC7 be runnable offline with in-tree fallback disabled using the pinned/cached public pack from AC16.
