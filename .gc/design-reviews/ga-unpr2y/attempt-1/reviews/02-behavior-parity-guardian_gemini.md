# Natasha Volkov — DeepSeek V4 Flash (Independent Review, Attempt 17)

**Verdict:** block

**Scope:** REQUIREMENTS scenario parity, regression prevention, characterization tests, proof freshness — with evidence drawn from verification of `internal/session/DESIGN.md`, `internal/session/REQUIREMENTS.md`, and the active workspace.

---

## Overview

Attempt 17 is a highly impressive and safety-critical "iterate" response that successfully closes several of the most difficult architectural gaps identified in prior reviews. By introducing a completely inventory-driven migration model (DESIGN.md:400+) utilizing `SESSION_BOUNDARY_SYMBOLS.yaml`, `API_CLI_ROUTE_INVENTORY.yaml`, and `WORKER_BOUNDARY_EXCEPTIONS.yaml`, the design establishes a concrete, audit-safe boundary for the refactor. Furthermore, the newly articulated pure-decider guardrails (DESIGN.md:588+), strict event payload requirements (DESIGN.md:620+), and structured query-side `repair-needed` wrapper constraints (DESIGN.md:289-296) are outstanding additions that significantly lower the risk of behavioral regressions.

However, from the perspective of the Behavior Parity Guardian, Attempt 17 still contains critical **circular design loops**, **traceability gaps**, and **verification blind spots** that will lead to severe downstream failures if approved. Under project schedules, forcing the non-mutating Slice 0 to mutatively repair complex reconciler integration tests is a fatal boundary violation that will permanently block CI progress. In addition, the persistent refusal to map requirements to backlog slices inside `DESIGN.md` leaves the completeness of the refactor unproven.

---

## Top Strengths

- **Inventory-Driven Migration Boundary (DESIGN.md:400+):** Moving away from broad file-name triggers to explicit row citations across three distinct ledgers (`SESSION_BOUNDARY_SYMBOLS.yaml`, `API_CLI_ROUTE_INVENTORY.yaml`, and `WORKER_BOUNDARY_EXCEPTIONS.yaml`) is a major victory. It guarantees that every single mutating API/CLI route is either safely routed through `worker.Handle` or has an expiring, owner-approved exception.
- **Strict Pure-Decider Boundary Rules (DESIGN.md:588+):** Forbidding mutation-feeding deciders from importing the event bus, store, runtime, or ambient clocks prevents non-deterministic, time-dependent behavior and guarantees total replayability.
- **Audited Query-Side `repair-needed` Precedence (DESIGN.md:289-296):** Specifying exact adapter behaviors for `repair-needed` (including audited repair owners, retry rules, and database-write fences) perfectly protects the read-only query paths from silent write-on-touch races.
- **Typed Event and Recovery Scans (DESIGN.md:620+):** Enforcing that `session.*` events must prove committed facts, canonical identifiers, public SSE/OpenAPI visibility, and durable scan recovery is an exceptional control that guarantees robust work release even under crash-fault scenarios.

---

## Blocking Findings

### 1. [Blocker] The Stale Evidence Paradox remains unresolved in Slice 0
The design continues to state that the non-mutating Slice 0 "must repair or owner-retire the evidence for `SESSION-RECON-002`, `SESSION-RECON-003`, `SESSION-RECON-006`, and `SESSION-RECON-007` before a later slice cites those rows." (`DESIGN.md:196-200`).

This requirement contains a fatal **architectural contradiction**:
1. Slice 0 is defined as strictly **non-mutating and session-only**; it does not touch reconciler policy or write to any non-session store.
2. The stale/missing evidence paths cited in `REQUIREMENTS.md` (e.g., `cmd/gc/scale_from_zero_test.go`, `cmd/gc/provider_health_gate_test.go`, and `cmd/gc/session_progress_test.go`) belong to the **reconciler and provider-health sub-systems** (Layer 2-4), which are outside `internal/session`.
3. Repairing these tests requires restoring or writing complex reconciler scaling, health, and progress integration tests that require a functional store and runtime provider.
4. "Owner-retiring" these rows is unacceptable because they represent safety-critical production behaviors (such as cold-start clamping and health alerts) that must not be deleted or hidden just because their test evidence is currently missing on this branch.

Because Slice 0 cannot mutatively restore Layer 2-4 reconciler integration tests without violating its own "session-only, non-mutating" boundary, and cannot retire them without deleting product requirements, the Slice 0 validator is **guaranteed to fail immediately and permanently**, blocking all progress.

**Required change:** 
- Explicitly allocate the repair/restoration of `scale_from_zero_test.go`, `provider_health_gate_test.go`, and `session_progress_test.go` to a preceding or parallel **Reconciler Test-Hardening Slice (Slice 6 Backlog)** rather than the non-mutating Slice 0.
- Provide a machine-readable **Transition Proof Allowlist** in Slice 0's validator allowing these specific reconciler rows to temporarily fall back to their historical commit citations (e.g., `commit a2b2da046`, `commit dbda1e380`) in `REQUIREMENTS.md` until the corresponding test-hardening slice restores the files.
- Annotate these four affected rows directly in `REQUIREMENTS.md` as `[STALE - REQUIRES SLICE 6 REPAIR]` so they are not mistaken for live executable evidence.

---

### 2. [Blocker] Deferred design-time traceability prevents backlog verification
The backlog slices (Slices 1 to 6) in `DESIGN.md` still lack scenario-row mapping. By deferring the row-to-slice allocation to a future `SCENARIO_PARITY.yaml` file to be created in Slice 0, we cannot verify at this design gate that the refactor backlog is comprehensive.

There is no structural proof in `DESIGN.md` that critical scenario rows (such as `SESSION-LIFE-001` legacy state projection, `SESSION-LIFE-002` pending-create claim, `SESSION-LIFE-008` user-facing projection guard, or `SESSION-RUNTIME-004` stop turn) are actually covered by any future slice.

**Required change:** Re-introduce a high-level **Scenario Allocation Matrix** directly in `DESIGN.md` that maps groups of `SESSION-*` requirements to their target backlog slices (e.g., Slices 1 & 2 own Target Identity and Surfaces; Slice 3 owns Wake; Slice 4 owns Close; Slice 5 owns Runtime Start; Slice 6 owns Reconciler Facts). This ensures completeness is proven at the design gate before implementation begins.

---

### 3. [Blocker] Lack of Black-Box Assertion Rules for Characterization Tests
The `Refactor Rules` (`DESIGN.md:770-785`) state that "The test should prove the behavior the user sees, not every internal branch." However, this is too weak to prevent regression. 

Without a strict prohibition against white-box mock assertions (such as mocking internal store interfaces or asserting internal function call chains), developers or agents will write brittle mocks that pass during the refactor even if the user-visible product behavior (exit codes, output payloads, or database commit states) is completely broken.

**Required change:** Add an explicit, non-negotiable rule to the `Refactor Rules`:
> "Characterization tests must be black-box, end-to-end, or integration-level tests asserting user-visible or system-level outputs (such as exit codes, stdout/stderr shape, API status codes, and database commit states) rather than white-box mocks of internal interfaces. The exact same characterization tests must run unchanged against both the legacy baseline and the refactored path to prove parity."

---

## Major Findings

### [Major] Lack of assertion-level verification in Proof-Freshness validation
The Slice 0 validator `TestScenarioParityFreshness` checks if cited file paths exist. However, a file-existence check cannot detect if the tests inside that file have been renamed, gutted, or bypassed using `t.Skip()`. The fact that reconciler requirements were allowed to go completely missing while their citations remained in `REQUIREMENTS.md` proves that path-level checks are insufficient to prevent proof rot.

**Required change:** Require that `SCENARIO_PARITY.yaml` specifies both the file path and the **exact test function symbol(s)** (e.g., `TestSessionLifecycle/Wake_Held_Until`). The Slice 0 freshness validator must dynamically parse or execute the tests to verify that the named test functions exist and do not contain hardcoded skips.

---

## Minor Findings & Questions

- **Dynamic Key Static Guard:** How will the static guard specified in Slice 0 handle dynamic metadata key writes (e.g., loops writing variable key patterns)? Will it enforce that any `SetMetadata` with a non-literal key is a violation? We recommend that dynamic-key patterns must be explicitly registered as exceptions in `SESSION_BOUNDARY_SYMBOLS.yaml`.

---

## Summary of Required Changes

1. **Resolve the Stale Evidence Paradox:** Move reconciler integration test restoration to Slice 6 Backlog, add a **Transition Proof Allowlist** to Slice 0's validator allowing missing paths to fall back to historical commit hashes, and annotate the affected rows in `REQUIREMENTS.md`.
2. **Re-introduce the Scenario Allocation Matrix:** Add a high-level table to `DESIGN.md` mapping all `SESSION-*` requirements to their target backlog slices to ensure design-time coverage verification.
3. **Mandate Black-Box Characterization Tests:** Add a strict rule to the Refactor Rules forbidding white-box mock assertions.
4. **Verify Assertion-Level Freshness:** Update the Slice 0 freshness validator to check exact test function symbols and skip-states, not just file existence.
