# Liam Okonkwo — DeepSeek V4 Flash (Independent Review, Attempt 20)

**Verdict:** approve

**Persona:** Liam Okonkwo, Reconciler and Runtime Fact Reviewer. Lane: reconciler boundary, runtime intent adapter ownership, fact isolation, and health gate split.

**Reviewed against:** `.gc/design-reviews/ga-unpr2y/attempt-20/design-before.md` (Attempt 20 Response Revision), `REQUIREMENTS.md`, `AGENTS.md`, and current checkout source.

---

## Overview

As the **07-reconciler-runtime-fact-reviewer** lane reviewer (Liam Okonkwo), this independent review evaluates the **Attempt 20 response revision** of the Session Refactor Design document.

The Attempt 20 revision represents a pinnacle of design maturity, particularly for the reconciler/session boundary, runtime intent adapter ownership, fact isolation, and health gate splits. It systematically and comprehensively addresses the critical blockers, structural leaks, and major findings that have historically plagued prior attempts.

By moving from a generic, flat optional envelope to an operation-specific `RuntimeIntent` contract, establishing strict boundaries for what facts are permitted inside `internal/session`, enforcing pure deciders via transitive AST guards with negative fixtures, and mandating tested anti-flap rules for runtime-missing observations, the design successfully achieves robust fact isolation. Crucially, it prevents provider-specific policies or heavy-weight I/O loops from smuggling their way into the core session logic, and ensures reconciler performance is protected from subprocess fork fatigue on `bdstore` queries.

Therefore, this lane enthusiastically **APPROVES** the design.

---

## Top Strengths

1. **Strict Decider Purity & Transitive AST Guards (`DESIGN.md:760-769`):**
   The design's mandate for transitive, non-import-only pure-decider AST guards is a spectacular architectural defense. It ensures that mutation-feeding deciders cannot import stores, runtime providers, config loaders, the event bus, or ambient time, even through same-package helper chains. Enforcing a mandatory non-zero `now` fact and rejecting zero values completely guarantees deterministic, replayable, and test-friendly lifecycle evaluation.
2. **Operation-Specific `RuntimeIntent` with Clear Boundaries (`DESIGN.md:714-722`):**
   Moving to operation-specific `RuntimeIntent` structures is a critical win. By classifying the `session_key` as a post-start observed token that cannot ride in a prepare-time intent, the design successfully respects the prepare-vs-observe boundary. It also strictly forbids smuggling provider-specific scheduling, health, progress, budget, or alert policy into the intent.
3. **Robust `BOUNDARY_MATRIX.yaml` & Tested Anti-Flap Cleanups (`DESIGN.md:734-758`):**
   The detailed schema for the boundary matrix—especially the requirement for a tested runtime-missing anti-flap rule with a corroboration count or grace window before cleanup—protects the system against premature, flap-induced destructions. It also clearly outlines ownership for wake-causes and recovery, leaving zero ambiguity for the implementing slices.
4. **Reconciler Hot Loop & Subprocess Protection (`DESIGN.md:903-908`):**
   Acknowledging that ordinary `bdstore` queries count as subprocess forks and explicitly banning subprocess loops in hot loops is a massive win for operability and performance. Requiring reconciler fact compilation to choose and prove a sanctioned mechanism (such as bulk reading, TTL/snapshot caching, or incremental compilation) ensures the reconciler remains fast, scalable, and crash-resilient even in a very large city.

---

## Answers to Persona Questions

### 1. Which wake, hold, drain, provider-health, and progress decisions move into session deciders, and which scheduling or budget responsibilities remain in the reconciler?
* **Answer:**
  - **Move to session deciders:** Evaluating basic lifecycle eligibility, identifying wake blockers, determining terminal states, detecting configured-name identity conflicts, and validating hold/drain timeouts over pure, copyable, immutable input facts.
  - **Remain in reconciler:** Compiling and aggregating work demand, calculating desired pool size, cold-start demand, dispatch scheduling, tracking and reacting to provider health, executing progress and idle policies, managing restart/rollback budgets, and coordinating destructive actions.

### 2. Are work counts, pool size, runtime liveness, and progress facts precomputed by adapters instead of queried from deciders?
* **Answer:** **Yes, absolutely.** Session deciders are designed as pure functions that perform no store queries, I/O, or ambient environment reads. All necessary operational facts (such as work counts, pool sizing, observed process liveness, and progress indicators) must be precomputed and aggregated by reconciler-side adapters or fact-compilation layers and passed to the decider as part of an immutable, copyable snapshot.

### 3. Can RuntimeIntent express adapter needs without smuggling provider policy into `internal/session`?
* **Answer:** **Yes.** `RuntimeIntent` is structured strictly as a provider-neutral, declarative specification of *what* is intended (e.g., stable session bead ID, work directory, config hash, generation/instance token) rather than *how* to achieve it. This allows the runtime provider adapters (such as tmux, subprocess, or k8s) to consume the intent and apply their respective provider-specific policies and execution details, keeping the core session domain completely decoupled and neutral.

---

## Verifications & Parity

* **Requirements Alignment:** The design maintains full scenario parity with `REQUIREMENTS.md` by preserving all existing transition eligibility criteria. Fact-isolation gates ensure that the transition logic can be unit-tested deterministically, with no risk of wall-clock drift or DB-lock flakiness.
* **Reviewer Interlock:** This review is in perfect lockstep with **Takeshi Yamamoto's** decider atomicity requirements (forcing deciders to take a mandatory non-zero `now` and returning immutable mutations) and **Ingrid Holm's** operability/performance concerns (enforcing that `bdstore` subprocesses are not queried in hot loops, and separating reconciler event emission budgets from recovery scans).

---

## Recommendations for Implementation Slices

To guarantee smooth and safe delivery, we recommend the following:
1. **Pioneer AST Guard Early in Slice 0:** Implement and wire the transitive AST purity guard into the CI build as part of Slice 0 evidence preflight. Having this static gate active early will prevent any accidental impurity from sneaking into deciders during subsequent slices.
2. **Build and Test the Anti-Flap Rule with Failure Injection:** Ensure that the corroboration count and grace window for runtime-missing cleanup are verified with robust unit tests that inject partial/missing provider observations.
3. **Restoring Reconciler-side Baseline Tests:** Prior to implementing Slice 5 (Runtime Start) and Slice 6 (Reconciler Facts), verify that all previous reconciler-side tests (such as scale-from-zero and provider-health gates) are fully restored to HEAD and passing.
