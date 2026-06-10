# Takeshi Yamamoto — DeepSeek V4 Flash (Independent Review, Attempt 20)

**Verdict:** iterate

**Review scope:** Pure decider enforcement, optimistic concurrency, commit-event-intent ordering, stale-fact defense, and boundary-inventory-enforceability for the Decider Atomicity Enforcer mandate. This reviews the current Attempt 20 iteration of [design-before.md](file:///data/projects/gascity/.gc/design-reviews/ga-unpr2y/attempt-20/design-before.md) against `REQUIREMENTS.md`, `AGENTS.md`, and the active checkout source.

---

## Top Strengths

- **Complete Removal of Client-Side TOCTOU Loophole**: The design successfully responds to prior feedback by striking "durable precondition with immediate reread" (client-side reread followed by blind write) from the valid fence list (documented in [design-before.md:66](file:///data/projects/gascity/.gc/design-reviews/ga-unpr2y/attempt-20/design-before.md#L66), [design-before.md:510-513](file:///data/projects/gascity/.gc/design-reviews/ga-unpr2y/attempt-20/design-before.md#L510-L513), and [design-before.md:557-560](file:///data/projects/gascity/.gc/design-reviews/ga-unpr2y/attempt-20/design-before.md#L557-L560)). Defining exactly three valid write strategies (store-native conditional write, atomic transaction store fence, or repair-converged blind write with supersession keys, post-write verification, and a named recovery owner) is a massive victory for concurrency safety.
- **Absolute Store Capability Matrix Preflight Gate**: Requiring `STORE_CAPABILITY_MATRIX.yaml` to be fully materialized and validated before any mutating slices can land (documented in [design-before.md:211](file:///data/projects/gascity/.gc/design-reviews/ga-unpr2y/attempt-20/design-before.md#L211), [design-before.md:456](file:///data/projects/gascity/.gc/design-reviews/ga-unpr2y/attempt-20/design-before.md#L456), and [design-before.md:576-586](file:///data/projects/gascity/.gc/design-reviews/ga-unpr2y/attempt-20/design-before.md#L576-L586)) ensures that we do not write unbackable atomicity promises (like those in `SESSION-START-001`) onto backends that lack conditional write or transaction primitives.
- **Atomic Writer Ownership per Key Family**: The design mandates that a session-owned key family may never be concurrently written by a legacy blind writer and a new fenced command, transferring ownership atomically per family (documented in [design-before.md:514-516](file:///data/projects/gascity/.gc/design-reviews/ga-unpr2y/attempt-20/design-before.md#L514-L516)). This completely prevents split-brain writes during the transition phase.
- **Transitive Pure Decider Guard & Explicit Now Fact**: Enforcing call-level decider purity with a transitive pure-decider guard that follows call graphs, forbids ambient clock/store imports, and requires a mandatory non-zero `now` fact (documented in [design-before.md:759-768](file:///data/projects/gascity/.gc/design-reviews/ga-unpr2y/attempt-20/design-before.md#L759-L768)) provides excellent compiler-level and runtime guarantees of determinism.

---

## Critical Risks

### 1. [Major] Active System Clock Fallback Violates Pure Decider Mandate
- **Evidence:** [lifecycle_projection.go:379-382](file:///data/projects/gascity/internal/session/lifecycle_projection.go#L379-L382) and [lifecycle_projection.go:607-610](file:///data/projects/gascity/internal/session/lifecycle_projection.go#L607-L610).
- **Why it matters:** The design mandates that "Mutation-feeding deciders take a mandatory non-zero `now` fact and reject zero values" and must not import "ambient time" ([design-before.md:760-766](file:///data/projects/gascity/.gc/design-reviews/ga-unpr2y/attempt-20/design-before.md#L760-L766)). However, the active codebase in `lifecycle_projection.go` contains system wall-clock fallbacks:
  ```go
  now := input.Now
  if now.IsZero() {
  	now = time.Now().UTC()
  }
  ```
  If any caller or adapter fails to supply a non-zero timestamp, the decider yields non-deterministic results based on the local OS wall-clock. This breaks replayability of session traces and violates the pure decider rule. Purity must be call-level and absolute.
- **Required Resolution:** Completely remove the `time.Now().UTC()` fallbacks from `lifecycle_projection.go` and enforce that `input.Now` must be non-zero at the entry boundary (returning an error or panic on zero timestamps).

### 2. [Major] Lack of Stale-Fact/Concurrency-Lag Defense for Read-Only Classifier Paths
- **Evidence:** [design-before.md:321-358](file:///data/projects/gascity/.gc/design-reviews/ga-unpr2y/attempt-20/design-before.md#L321-L358) (Target Classification Contract) and [design-before.md:894-914](file:///data/projects/gascity/.gc/design-reviews/ga-unpr2y/attempt-20/design-before.md#L894-L914) (Cost rules / hot-path budget).
- **Why it matters:** During the migration cohabitation phase where read-only paths are migrated first, target classification runs as a side-effect-free query path. Under high concurrency or database projection delay (such as `BdStore` replica lag), the read-only classifier can query a stale snapshot of session bead metadata. The design does not specify a stale-fact detection or defense protocol for the read path. If a caller resolves a session name or alias based on a stale view while a concurrent process is performing an atomic write or repair, the caller will make decisions based on stale facts, leading to temporary routing anomalies (such as incorrect "not-found" or "rejected" classifications).
- **Required Resolution:** Add a specific stale-fact diagnostic marker (`stale_fact_detected`) to the `diagnostics` field of the classification results. Callers should be able to determine whether the snapshot they read is potentially stale and handle eventual consistency (e.g., retrying or degrading) instead of immediately returning a hard 404/409.

### 3. [Major] Unspecified Verification Logic for Vocabulary Exclusions
- **Evidence:** [design-before.md:404-407](file:///data/projects/gascity/.gc/design-reviews/ga-unpr2y/attempt-20/design-before.md#L404-L407) (`TestVocabularyCheckpoints` must fail when any `provisional` field appears in compiled Go structs, OpenAPI, or event payloads).
- **Why it matters:** The design relies heavily on `TestVocabularyCheckpoints` to prevent provisional fields from leaking into the compiled application. However, the exact mechanism of verification is left unspecified. Simple text-substring matching is highly brittle and prone to false negatives. If provisional vocabulary leaks into active structures, it violates the progressive capability model and compromises the pure separation of concerns.
- **Required Resolution:** Mandate that `TestVocabularyCheckpoints` uses robust Go AST parsing or type analysis (such as package `go/ast` or `go/types`) to programmatically verify structural exclusion of provisional fields, rather than relying on brittle text matching.

---

## Required Changes

1. **Enforce Absolute Decider Clock Purity**: Completely remove the `time.Now().UTC()` fallback from [lifecycle_projection.go](file:///data/projects/gascity/internal/session/lifecycle_projection.go) and ensure that `now` is a mandatory, non-zero field on `LifecycleInput`.
2. **Define Read-Path Stale-Fact Defense**: Introduce a structured diagnostic flag for stale/out-of-sync store views during target classification, specifying how callers should handle eventual consistency during migration cohabitation.
3. **Specify AST-Based Verification for Vocabulary Checkpoints**: Require that `TestVocabularyCheckpoints` uses robust Go AST or type-checking analysis to prevent provisional field leakage rather than relying on brittle text matching.

---

## Questions

1. When `ProjectLifecycle` is invoked on the query path, what mechanisms will be used to detect if the bead's metadata represents a stale or partially-propagated snapshot (especially considering `BdStore` projection lag)?
2. How does the implementation plan to statically enforce the pure-decider imports rule? Will there be a dedicated static analysis lint/test in `internal/session` to block imports of `time`, `os`, or `internal/beads` packages?
3. How will `TestVocabularyCheckpoints` robustly cross-reference compiled Go types with the provisional fields listed in `VOCABULARY_CHECKPOINTS.yaml` without incurring high test-maintenance overhead?
