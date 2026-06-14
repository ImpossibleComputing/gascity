---
title: "Graph Store Backend Selection"
---

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-06-14 |
| Author(s) | Claude Opus 4.8 |
| Relates to | `engdocs/design/beads-work-infra-split.md` (the seam), `engdocs/design/beads-dolt-contract-redesign.md` (governance) |
| Supersedes | the round-2 "build bespoke HQStore" conclusion in `engdocs/coordination-store/round2/R2.2-author-design.md` |

## Summary

Pick the backend for the `ClassGraph` store — the formula-v2 execution nodes
(the bead explosion) once they move behind the `GraphStore` seam. The decision:
**an embedded, in-process, pure-Go (`modernc`) SQLite store, recovered from the
ga-aec8q coordstore program**, exposed through the *narrow* `GraphStore` interface.
**`beads_rust` (`br`) is rejected for the hot path** (kept as a compat/exec
backend). This is not new work — ga-aec8q already built, benchmarked (against 8
backends), and *selected* this store; it was deleted only for lacking the seam,
conformance, and ownership that the work-vs-infrastructure split now provides.

## Context: the problem

Today every bead operation forks+execs the `bd` CLI (`BdStore`, 1,748 LOC) which
talks the MySQL wire to a Dolt server. The graph engine's per-node churn
(order-tracking ~3,500/day, field-change events 9–17k/day, mail, wisp labels
~23k) drives a read:write ratio of **≈265:1** and a per-write fork/commit cost
that wastes tens of engineering-hours per week. Dolt's differentiating features
(git versioning, branching) are **100% unused** by this workload
(`engdocs/coordination-store/discovery.md`). Two backend proposals were raised:
`beads_rust` and a custom high-performance store.

## Decision

Back `ClassGraph` with an **embedded in-process SQLite store** (pure-Go
`modernc.org/sqlite`, already a transitive dependency via doltlite), recovered
from ga-aec8q and exposed through `GraphStore`. Keep `br` as a portability/compat
`exec` backend only.

## Options considered

| Option | Verdict | Why |
|---|---|---|
| **beads_rust (`br`)** | ✗ hot path; ✓ compat only | SQLite+JSONL but **CLI-only (no Rust library API)** → integrable only via `exec` (subprocess + DB-open *per call*), single-writer WAL, and **missing every graph-critical primitive**: no atomic batch/graph-apply, no arbitrary metadata, no ephemeral/TTL, no CAS claim (`docs/reference/exec-beads-provider.md` gap analysis). Same engine as the chosen store, strictly worse access pattern. Value is bd-free portability + git-friendly JSONL. |
| **Embedded SQLite coordstore (recovered)** | ✓ **chosen** | Pure-Go, in-process, WAL + 8-conn read pool for the 265:1 read profile, two-tier (main/wisp) schema, indexed hot paths, atomic `Tx`, CAS (`ReleaseIfCurrent`), retention sweeper. Benchmarked + selected by ga-aec8q. Crash-recovery is free (vs bespoke). |
| **Bespoke HQStore (WAL + indexed mem-core)** | ✗ | R2.2 estimated ~9 eng-days / ~1,600 LOC and flagged "owning crash-recovery correctness" as the principal risk — which SQLite gets for free. ga-aec8q built it (#2590) then removed it. |
| **DoltLite-native / keep optimizing Dolt** | ✗ for graph | What "survived" the last round, but keeps the per-op fork/MySQL-wire architecture and the git/branch machinery the workload never uses. |

## Why the prior attempt was removed — and why it survives now

The ga-aec8q SQLite/coordstore work was **not removed for failing**. It hit the
targets (FilterScan p99 **1.48 ms**, Ready p99 **5.63 ms**, point-read p99
1.09 ms, 151,580 ops / **0 errors**, HeapInuse **15.9 MiB**) and SQLite-CGo was
explicitly "selected" (gate `ga-aec8q-19`, 2026-05-31). It was removed because it
was a **parallel backend with no seam**:

- PR **#2873** (HQStore): "*not wired into live provider dispatch*" — dormant code.
- PR **#3151** (sqlite_store): "*the experiment is over*" — mechanical deletion + hard-error switch (`cmd/gc/main.go:1182`).
- PR **#3155** (harness): retired the benchmark tree.
- The split design doc's "decisive lesson": *a parallel backend without an interface-first migration story, conformance tests, and an ownership story gets removed; what survived was optimizing the same store in-process.*
- Governance (`beads-dolt-contract-redesign.md`, Accepted 2026-04-11): one canonical store-target contract, **no new parallel control planes**.

The current initiative supplies exactly the three missing things, so the revival
survives:

| what killed it | what now exists |
|---|---|
| no interface-first migration story | the `GraphStore` seam + `RouterReady` + the phased P0–P5 plan |
| no conformance tests | `RunGraphStoreTests` / `RunClassedStoreTests` (`internal/coordrouter/coordtest`) |
| no ownership story | `coordclass` — `ClassGraph` owns it |
| "no parallel control planes" | it is a **routed class behind the one router/Store contract**, not a competing authority (the wide `InfraStore` facade is explicitly rejected) |

## Two non-negotiable rules

1. **Wrap, don't widen.** Recover the SQLite *implementation* (schema, `Tx`, CAS,
   sweeper) but expose it through the **narrow `GraphStore` seam**. Do **not**
   resurrect the monolithic 22-method adapter — "that wide shape is what got
   deleted." Use the recovered 21-method `StoreAdapter` SPI
   (`git show beeac65b7^:internal/benchmarks/coordstore/adapter.go`) as a
   *completeness checklist* only.
2. **Embed, don't daemonize.** The store lives **in-process in the controller**;
   `gc ready`/`gc hook` resolve through the controller's already-open store
   (local API), not a fresh DB-open per subprocess. A separate CLI/DB daemon would
   reintroduce the exact `exec`/IPC pathology (the bd→Dolt fork-per-op cost) we
   are removing.

## Implementation map: recovered SQLite store → `GraphStore` seam

Source: `git show ba607c16d^:internal/beads/sqlite_store.go` (1,018 LOC). The
recovered store is a complete `beads.Store`; mapping to the seam:

| `GraphStore` need | recovered store has | gap to close |
|---|---|---|
| `ApplyGraphPlan` (atomic pour) | `Tx()` (`runSequentialTx`) + `upsertBeadTx` + `depAddTx` | **compose**: an `ApplyGraphPlan` that wraps one `Tx` over the nodes+edges (small) |
| tier-aware pour (`ApplyGraphPlanWithStorage`) | `tier` column (`main`/`wisp`), `CHECK (tier IN ...)`, tier-routed create | wire plan storage-class → tier (small) |
| `ReadyCandidates` (routed, unblocked, defer) | `Ready()` already does `status='open'` + the identical `readyExcludeTypes` + a `NOT EXISTS` blocking-dep subquery (`blocks`/`waits-for`/`conditional-blocks`) + tier + `IsReadyCandidateForTier` (defer_until) + `ORDER BY created_at,id` | **add** a `gc.routed_to` metadata-JOIN filter (the `metadata(meta_key,meta_value)` index already exists) (small) |
| atomic CAS claim | `ReleaseIfCurrent` (CAS release) + `ConditionalAssignmentReleaser` | **add** the dual `Claim` = `UPDATE … SET assignee=? WHERE assignee=''` (mirror `ReleaseIfCurrent`, small) — closes the bd-only claim gap |
| idempotency (`FindOrCreateByKey`) | `metadata` PK `(bead_id, meta_key)`; `Tx` | **add** a unique index on the idempotency-key metadata + keyed upsert (medium) — closes the racy striped mutex |
| change feed (watch/notify) | — | reuse the bd-hook event stream initially, or add a SQLite-side notify (medium / deferrable) |
| both-tier reads | `tier` column + `TierMode` in every query | **done** |
| retention / TTL sweep | `startRetentionSweeper` + `purgeTerminal` (4h default, 30s cadence, FK-cascade) | **done** |
| within-graph deps / `is_blocked` | `deps` table + indexed + the Ready not-blocked subquery | **done** (cross-boundary blocks edges stay in the Work store per the split design) |

Concurrency: 1 write connection serializes mutations (the bead explosion now
arrives as one `ApplyGraphPlan` transaction, far fewer write txns than per-bead
Dolt forks) + an 8-connection read pool in WAL for the 265:1 read profile, with
`busy_timeout` + application-level `retryOnBusy`.

**Net:** ~90% of the graph backend is recoverable; the gaps are `ApplyGraphPlan`
(compose over `Tx`), the `ReadyCandidates` routed-metadata filter, and an atomic
`Claim` — all small — plus idempotency-unique-key and change-feed as medium/
deferrable follow-ups.

## Consequences

- **Must prove, not assume.** Land the recovered `internal/benchmarks/coordstore/`
  harness (incl. the **Dolt baseline** soak launcher) and re-run the soak/chaos
  vs Dolt on current hardware before flipping the `ClassGraph` config flag.
  Re-establish the recovered p99 targets as the ship gate.
- **modernc only.** No CGo (the program already migrated off `mattn` to
  `modernc`); `CGO_ENABLED=0` must stay green.
- **Risk: single-writer throughput.** One write connection is correct for a single
  controller; if multi-controller writes to the graph store ever materialize, the
  CAS-claim and idempotency-unique-index become the correctness guards (already in
  the gap list).
- **Reversible.** A one-line re-register points `ClassGraph` back at the bd store;
  Dolt stays a cold backup during cutover (R2.3: ≤60s provider swap, 48h rollback).

## References (recoverable artifacts)

- Requirements / census: `engdocs/coordination-store/discovery.md` + `findings/S1–S6` (recovered to the tree).
- Solution landscape / design / migration: `engdocs/coordination-store/round2/{R2.1b-adapter-sweep,R2.2-author-design,R2.3-migration-path}.md`.
- Deleted store: `git show ba607c16d^:internal/beads/sqlite_store.go`.
- Benchmark harness (8 adapters + Dolt baseline + soak/chaos/leak): `internal/benchmarks/coordstore/` on `quad341/builder/ga-aec8q-12` (removed from main by #3155).
- `StoreAdapter` SPI checklist: `git show beeac65b7^:internal/benchmarks/coordstore/adapter.go`.
- `br` gap analysis: `docs/reference/exec-beads-provider.md`.
