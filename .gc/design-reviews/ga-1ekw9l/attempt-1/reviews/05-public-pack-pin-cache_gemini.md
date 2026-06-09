# Lena Hoffmann — Public Pack Pin Cache Reviewer (Attempt 8, Independent DeepSeek V4 Flash Style)

**Verdict:** block

> **Lane:** Public Gastown pin integrity, immutable content hash, `RepoCacheKey` identity, synthetic-alias retirement, offline and rollback behavior.
>
> Reviewed against the Attempt 8 design document (`.gc/design-reviews/ga-1ekw9l/attempt-8/design-before.md`, 835 lines, `updated_at: 2026-06-09T13:20:59Z`) — specifically §"Pack Registry, Cache, And Retired Source Authority" (lines 313–357), §"Testing" (lines 603–686), and §"Rollout And Recovery" (lines 687–825).
>
> This independent review is produced using the DeepSeek V4 Flash persona, focusing specifically on first-principles trust boundaries, cross-document state consistency, and unstated runtime assumptions.

---

## Schema Conformance

Conforms to `gc.mayor.implementation-plan.v1`. Front matter carries the required keys with `phase: implementation-plan` and no `design_file`; the eight required top-level sections appear once each in the required order, and `Open Questions` is `None`. No appended attempt/review prose in the artifact.

---

## Top Strengths of the Design

- **Immutable Pin Security (Lines 329–331):** Anchoring `PublicGastownPackVersion` to an immutable `sha:` constant (line 331) and declaring branch/tag refs as non-authoritative fetchability metadata (line 331) successfully eliminates the risk of silent behavior drift on remote changes.
- **Fail-Closed Offline Assertions (Lines 668–670):** Specifying network-disabled tests for exact cache hits, digest mismatches, missing subpaths, and stale alias rejections guarantees that the cache boundaries remain secure in restricted or offline execution environments.
- **Zero-Duplicate-Active Loader Gate (Lines 347–351):** Comparing active bundled and public packs prior to execution and failing closed on behavior-row overlap ensures that retired or custom/forked source conflicts cannot execute.

---

## Critical Risks & Consensus Blockers (DeepSeek V4 Flash Style)

### 1. [Blocker] Timing Contradiction on Subpath Cache Key Enforcement (Slice 2 vs Slice 6)
- **The Risk:** Lines 336–337 state that the "subpath-aware lock/cache proof lands before the first Gas City slice that updates `internal/config/PublicGastownPackVersion`" (which is consumed at Slice 2, line 739). However, Slice 6 (lines 781–782) still reads "enforce subpath-aware ordinary cache keys", and the Slice 6 gate row (line 807) lists "subpath cache proof" as a must-pass gate.
- **The Impact:** If subpath enforcement is deferred to Slice 6, then during Slices 2-5, `gascity-packs` cache entries for the compatibility pin (which carries subpath `//gastown`, line 742) can conflate with other subpaths from the same repository and commit. Today, `RepoCacheKey(source, commit)` lacks subpath awareness, so they would resolve to the same physical cache directory—violating the logical identity this lane is designed to protect.
- **Required Resolution:** Resolve this contradiction by enforcing subpath-aware ordinary cache keys and the expanded `LockedPack` schema in Slice 2 (first pin consumption). Re-word Slice 6 (line 781) and its gate row (line 807) to "retire aliases and re-verify subpath keys" rather than "enforce."

### 2. [Blocker] Concurrent Cache-Write Corruption and Race Conditions (AC16 Gap)
- **The Risk:** Lines 330–331 specify that "Promotion and read hits verify source, commit, subpath, pack digest, and manifest digest" and that remote packs are written during checkout. However, there is no physical file-system write locking or atomic promotion mechanism specified in the design body.
- **The Impact:** In parallel testing (e.g., `make test-fast-parallel`) or concurrent CLI execution environments, multiple processes may simultaneously attempt to write to or promote cache bytes for the same `RepoCacheKey`. Without synchronization, this will result in write collisions, truncated directory checkouts, and corrupt cache states that fail read-time digest verification.
- **Required Resolution:** Explicitly specify the atomic concurrent promotion protocol in the Data And State section:
  - Cache checkouts and promotions must stage into process-unique or randomized temporary directories (e.g., `.gc/cache/tmp/promote-<uuid>`).
  - Upon successful checkout and digest verification, publish to the final `RepoCacheKey` path using a single, atomic POSIX rename (`os.Rename`).
  - If a concurrently running process has already published a valid directory at that target path, gracefully abort and treat it as a success.

### 3. [Blocker] Read-Hit Digest Verification Performance Bottleneck
- **The Risk:** Lines 330–331 state that "Promotion and read hits verify source, commit, subpath, pack digest, and manifest digest."
- **The Impact:** Performing a full recursive cryptographic hash of every file in the public pack cache on *every single read hit* (which occurs on every single `gc` CLI command execution, config load, and agent tick) is incredibly expensive and will introduce unacceptable performance degradation. This is an operational risk that other reviewers accepted without question.
- **Required Resolution:** Specify a two-tiered validation model:
  - **First-Write Validation:** On first cache-write, promotion, or install, calculate and verify the complete recursive content digest and write an immutable, tamper-evident marker file (e.g., `.gc_cache_validated`) containing the computed manifest digest.
  - **Read-Hit Validation:** Normal read hits check the existence of the validation marker and assert its stored digest matches `public-gastown-pins.yaml`. Optionally, perform a lightweight check (e.g., matching file counts, sizes, and mtimes) to detect drift, and reserve full recursive digest verification for explicit repair commands (`gc doctor --fix`) or cache-regeneration triggers.

### 4. [Blocker] Omission of Authoritative Gas City Pin Ledger in Coherence Gate (AC15 Gap)
- **The Risk:** The pin-coherence gate (lines 341–344) compares `PublicGastownPackVersion` with `public-gastown-pins.yaml` (external), fresh-init output, lockfile provenance, and cache proof.
- **The Impact:** It completely omits the authoritative internal ledger `support/public-gastown-pin-ledger.yaml` (which the plan lists as a support artifact on lines 561 and 720). If the external pins ledger drifts from Gas City's internal ledger, or if the `packcompat` witness uses an independent commit, the gate will fail to detect the divergence across the repo boundary.
- **Required Resolution:** Expand the pin-coherence gate definition (line 341) to include and compare `support/public-gastown-pin-ledger.yaml`, the external `public-gastown-pins.yaml`, the `packcompat` transcript's pin, and the version-skew matrix, and explicitly state that the internal ledger is the authoritative source for the Go constant.

### 5. [Major] Digest Determinism and Canonicalization Remain Undefined
- **The Risk:** The design references "canonical pack-tree `sha256`" and "behavior-manifest `sha256`" (line 338) but does not define the canonicalization rules (sorting order, permission treatment, excluded volatile fields).
- **The Impact:** `public-gastown-pins.yaml` contains a `generated-at` timestamp (line 132). If the behavior-preservation manifest or pack-tree also embeds a timestamp or volatile field, regeneration will produce a different hash, breaking offline comparison and triggering false positives.
- **Required Resolution:** Define the deterministic canonicalization rules (stable file list ordering, permission bit treatment, and exclusion of generated-at and other volatile fields) for both the pack-tree and behavior-manifest digests.

### 6. [Major] Slice 5b Rollback Duplicate-Definition Conflict (Un-folding Defect)
- **The Risk:** Slice 5b (lines 772–775) folds Maintenance assets into Core, and defines its rollback as "restore the compatibility pin and re-enable Maintenance" (lines 775–776).
- **The Impact:** Rolling back a city that has folded Maintenance assets into Core by re-enabling Maintenance will result in duplicate active definitions of behaviors and templates. This will trigger a fatal loader conflict under the zero-duplicate-active gate (lines 347–351), bricking the system.
- **Required Resolution:** Specify that Slice 5b rollback must either explicitly un-fold the moved Core assets back to Maintenance or declare the fold one-way with documented manual recovery instructions.

---

## Detailed Responses to Lane-Specific Questions

### Q1: Are PublicGastownPackVersion, pins.yaml, registry source, packcompat, and direct cache proof all bound to the same immutable commit and subpath?

**Answer:**
Yes. Under lines 329–331 and 336–344, they are logically bound to the single source of truth defined in the pins ledger and the `PublicGastownPackVersion` constant. However, to guarantee cross-repo consistency, the "durable public ref" (line 331) must be strictly non-authoritative and exist solely to prevent remote garbage collection. The pin-coherence gate (line 341) must cross-check all these surfaces in CI to prevent drift across the repo seam.

---

### Q2: Can any stale synthetic alias, embedded bytes, lock refresh, install path, or offline upgrade select retired Gastown or Maintenance content after the public pin lands?

**Answer:**
No. Under lines 328–334, synthetic Gastown/Maintenance cache entries are completely ignored, and the embedded set returned by `All()` is correctly pruned to Core, `bd`, and `dolt` (lines 316–318). New lock generation and runtime resolution paths will refuse to select retired or legacy embedded entries, successfully isolating them.

---

### Q3: What deployable state and rollback narrative exist across the window between gascity-packs landing and Gas City activation pin update?

**Answer:**
The transition relies on compatibility-pin adoption in Slices 2-4 (lines 739–755) and activation-pin adoption in Slice 5a (lines 766–771). Downgrading an activation-pinned city to an older binary requires rolling back the lockfile to the compatibility commit, which is handled in the release matrix (lines 810–818). However, rollback from Slice 5b (Maintenance fold) remains a critical gap due to the duplicate-definition risk, which requires either un-folding or a one-way rollback policy.

---

## Evaluation Against Lane Anti-patterns

| Anti-pattern / Red Flag | Mitigation in Current Design | Status |
| :--- | :--- | :--- |
| **Mutable branch/tag drift** | **Excellent.** Pinned to immutable commit SHAs in `PublicGastownPackVersion` and the ledger (lines 331, 337). | **Pass** |
| **Cache promotion laundering** | **Excellent.** Both promotion and read hits must verify source, commit, subpath, and digests (lines 330–331). | **Pass** |
| **Offline silent fallback** | **Excellent.** Handled by offline test coverage (lines 668–670) and zero-duplicate gates. | **Pass** |
| **Concurrent Cache Corruption** | **Missing.** No directory locking, staging paths, or atomic rename specified. | **Fail (Blocker)** |

---

## Final Verdict: Block

The Attempt 8 public pack pin cache design is extremely structured and shows great progress, particularly on immutable SHA pinning and offline test specifications. However, because it contains an **internal timing contradiction** regarding subpath enforcement, lacks **concurrent write locking and staging on promotions**, presents a **fatal read-hit verification performance bottleneck**, omits the **internal pin ledger** in the pin-coherence gate, and presents a **fatal duplicate-definition rollback conflict** in Slice 5b, I must **Block** the plan. Enforcing subpaths at Slice 2, specifying atomic concurrent promotion staging, introducing a two-tiered read-hit validation model, expanding the coherence gate inputs, and clarifying the fold-rollback path are necessary to make this caching architecture robust, performant, and secure.
