# V1 Formula Audit

Status: current as of 2026-06-02.

## Decision

Pool-targeted formula orders must create Ready-visible work. Do not reintroduce
an out-of-band metadata demand flag for pool scaling. If an order routes formula
work to a pool, the formula root must be actionable, typically by making the
formula `phase = "vapor"` so the root bead is a `task` with `gc.kind=wisp`.

Dog-owned formulas use the vapor-wisp model. Their formula descriptions tell
the dog to run `gc bd formula show <formula-name> --json`, follow the recipe
steps from the formula definition, close the bead, and run
`gc runtime drain-ack`. This is pack-authored behavior in the formulas, not a
compiler/runtime transformation.

## Dog Formula Status

These dog or dog-routed formulas are vapor/root-only:

| Formula | Path | Notes |
| --- | --- | --- |
| `mol-dog-stale-db` | `examples/dolt/formulas/mol-dog-stale-db.toml` | Cron formula order routes to dog. |
| `mol-digest-generate` | `examples/gastown/packs/gastown/formulas/mol-digest-generate.toml` | Cooldown formula order routes to dog. |
| `mol-dog-backup` | `examples/dolt/formulas/mol-dog-backup.toml` | Current shipped order is exec; recipe remains dog-callable. |
| `mol-dog-doctor` | `examples/dolt/formulas/mol-dog-doctor.toml` | Current shipped order is exec; recipe remains dog-callable. |
| `mol-dog-phantom-db` | `examples/dolt/formulas/mol-dog-phantom-db.toml` | Current shipped order is exec; recipe remains dog-callable. |
| `mol-dog-jsonl` | `examples/gastown/packs/maintenance/formulas/mol-dog-jsonl.toml` | Current shipped order is exec; recipe remains dog-callable. |
| `mol-dog-reaper` | `examples/gastown/packs/maintenance/formulas/mol-dog-reaper.toml` | Current shipped order is exec; recipe remains dog-callable. |
| `mol-shutdown-dance` | `examples/gastown/packs/maintenance/formulas/mol-shutdown-dance.toml` | Dog warrant recipe. |

## Remaining Materialized V1 Formulas

These formulas still materialize molecule/step beads by default. They must not
be used as pool-targeted formula orders unless they are converted to vapor or
to a graph workflow with a Ready-visible root.

| Formula | Path | Current use |
| --- | --- | --- |
| `mol-dolt-health` | `examples/dolt/formulas/mol-dolt-health.toml` | Legacy/manual formula; shipped health order is exec. |
| `mol-dolt-remotes-patrol` | `examples/dolt/formulas/mol-dolt-remotes-patrol.toml` | Legacy/manual formula; shipped remotes order is exec. |
| `mol-deacon-patrol` | `examples/gastown/packs/gastown/formulas/mol-deacon-patrol.toml` | Agent patrol loop, not a pool order. |
| `mol-polecat-work` | `examples/gastown/packs/gastown/formulas/mol-polecat-work.toml` | Agent work formula. |
| `mol-review-leg` | `examples/gastown/packs/gastown/formulas/mol-review-leg.toml` | Review workflow leg formula. |
| `mol-witness-patrol` | `examples/gastown/packs/gastown/formulas/mol-witness-patrol.toml` | Agent patrol loop, not a pool order. |
| `mol-do-work` | `internal/bootstrap/packs/core/formulas/mol-do-work.toml` | Core default work formula. |
| `mol-polecat-base` | `internal/bootstrap/packs/core/formulas/mol-polecat-base.toml` | Core composition base. |
| `mol-polecat-commit` | `internal/bootstrap/packs/core/formulas/mol-polecat-commit.toml` | Core composition fragment. |
| `mol-prompt-synth` | `internal/bootstrap/packs/core/formulas/mol-prompt-synth.toml` | Core prompt synthesis formula. |

## Graph V2 Formulas

These intentionally use the graph workflow runtime:

| Formula | Path |
| --- | --- |
| `mol-idea-to-plan` | `examples/gastown/packs/gastown/formulas/mol-idea-to-plan.toml` |
| `mol-refinery-patrol` | `examples/gastown/packs/gastown/formulas/mol-refinery-patrol.toml` |
| `mol-review-quorum` | `internal/bootstrap/packs/core/formulas/mol-review-quorum.toml` |
| `mol-scoped-work` | `internal/bootstrap/packs/core/formulas/mol-scoped-work.toml` |
