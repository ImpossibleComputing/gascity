---
plan_slug: core-gastown-pack-migration
phase: requirements
rig: gascity
rig_root: /data/projects/gascity-fresh-main-20260604-VLKm8c
artifact_root: /data/projects/gascity-fresh-main-20260604-VLKm8c/plans
status: approved
created_at: 2026-06-04T00:00:00Z
updated_at: 2026-06-04T15:07:35Z
---

# Requirements: Core and Gastown Pack Split

## Problem Statement

Gas City still carries system pack assets across three legacy locations:

- `internal/bootstrap/packs/core`
- `examples/gastown/packs/maintenance`
- `examples/gastown/packs/gastown`

This keeps SDK-required behavior, generic operational maintenance, and Gas
Town-specific orchestration behavior mixed together. It also keeps Gastown and
Maintenance assets under `examples/`, even though they are not examples in
practice.

The target state is:

- Core is the only Gas City-owned pack required for `gc` to run.
- Core lives in the Gas City source tree, but not under `examples/`.
- Core contains only SDK-generic behavior: CLI usage skills, provider overlays,
  the default Core maintenance agent, and infrastructure orders or formulas
  needed by any Gas City deployment.
- Core may define a default configurable maintenance agent named `dog` because
  some Core maintenance operations need agent execution. This is pack
  configuration, not a Go special case, and users must be able to override it
  through explicit configuration.
- Core does not contain Gastown-specific roles, naming, prompts, workflows, or
  decision logic. Role names such as Mayor, Deacon, Polecat, Refinery, Witness,
  Boot, Crew, or Gastown must not appear in Core assets except in migration
  documentation or tests proving their absence.
- Maintenance is retired as a standalone pack.
- Maintenance assets are split between Core and `gascity-packs/gastown`.
- Gastown-specific assets live in the external `gascity-packs/gastown` pack.
- Gastown does not lose behavior. When a current Maintenance or Core asset is
  moved to Core and role-specific behavior is removed from that Core copy,
  equivalent Gastown behavior must be retained or added in
  `gascity-packs/gastown`.
- No pack under `examples/gastown/packs/` remains the source of truth.

The migration must preserve operator behavior for supported templates while
making pack ownership explicit and auditable.

## Solution

Create the canonical Core pack at `internal/packs/core`. It must replace
`internal/bootstrap/packs/core` as the maintained source location and must be
loaded as the required Gas City runtime pack.

Add doctor/import-state diagnostics for resolved configurations that are
missing Core. The diagnostic must explain that Core is required, identify the
resolved config source that omitted it, and offer a fix that adds the Core pack
entry. Explicit opt-out behavior remains an open design question.

Retire the Maintenance pack by splitting its assets:

- Move SDK-generic operational cleanup, health, routing, and CLI support into
  Core, including the default `dog` maintenance agent and any Core maintenance
  formulas it needs. Remove Gastown-specific names, requesters, notification
  paths, and examples from those assets.
- Move role-driven maintenance behavior, prompt fragments, and Gas Town
  orchestration behaviors into `gascity-packs/gastown`.
- For every order, formula, script, prompt, or template fragment whose Core copy
  is generalized, add or retain the stripped Gastown-specific behavior in
  `gascity-packs/gastown` so Gastown remains behaviorally equivalent.
- Delete or rewrite Maintenance-only embed files, tests, docs, and implicit
  import behavior.

Move the in-tree Gastown pack out of `examples/gastown/packs/gastown` and make
`gascity-packs/gastown` the source of truth for Gastown roles, formulas, orders,
scripts, prompts, overlays, doctor checks, and commands.

Update pack import resolution and diagnostics so new cities do not depend on
implicit Maintenance or in-tree Gastown assets. Existing cities that reference
legacy local paths must receive a clear migration path.

## User Stories

As a Gas City maintainer, I can inspect the source tree and see a single
canonical Core pack in Gas City, with no Core, Maintenance, or Gastown pack
source under `examples/gastown/packs/`.

Acceptance criteria:

- `examples/gastown/packs/core`, `examples/gastown/packs/maintenance`, and
  `examples/gastown/packs/gastown` do not exist as maintained pack sources.
- `internal/bootstrap/packs/core` is removed or reduced to a compatibility shim
  that does not own pack assets.
- Tests fail if Core reintroduces Gastown role names or Gastown-only workflows.
- Tests allow `dog` only as Core's configurable default maintenance agent, not
  as a hardcoded Go role.

As a Gas City operator, I can initialize and run a non-Gastown city with only
Core-provided SDK behavior.

Acceptance criteria:

- Core is available without importing `gascity-packs/gastown`.
- Doctor warns when a resolved config lacks Core and offers a fix that adds it.
- Core provides CLI operating guidance skills for the `gc` command surface.
- Core provides only infrastructure orders and formulas that are valid for any
  Gas City city, such as stale closed-order cleanup, generic orphan cleanup,
  generic health checks, routing/nudge maintenance, or wisp compaction.
- Core provides a default configurable `dog` maintenance agent for Core
  maintenance operations that need agent execution.
- Users can override the Core `dog` configuration explicitly without changing Go
  code.
- Removing Gastown roles does not break Core infrastructure behavior.

As a Gastown operator, I can initialize Gastown from the public pack registry
and receive the Gastown roles and workflows explicitly from
`gascity-packs/gastown`.

Acceptance criteria:

- `gc init --template gastown` imports `gascity-packs/gastown` explicitly.
- The Gastown template does not rely on in-tree `examples/gastown/packs/gastown`
  or implicit Maintenance imports.
- Gastown role prompts, agents, formulas, commands, checks, and scripts resolve
  from `gascity-packs/gastown`.
- Any behavior removed from Core-owned copies during generalization is present
  in `gascity-packs/gastown` under Gastown-owned assets.
- A before/after behavior inventory proves Gastown keeps its current orders,
  formulas, script effects, prompts, notification paths, and role-specific
  recovery flows.

As a pack author, I can tell whether an asset belongs in Core or Gastown by its
behavior rather than its current path.

Acceptance criteria:

- Assets that explain how to use `gc` belong in Core only when they are
  role-neutral.
- Assets that mention or depend on Gastown roles belong in Gastown, or must be
  renamed and rewritten before they can be Core.
- Assets that mention `dog` may belong in Core only when they are maintenance
  agent assets and remain user-overridable pack configuration.
- Pack ownership is documented in a file-by-file migration table.

As an upgrader of an existing city, I receive actionable diagnostics for legacy
pack imports.

Acceptance criteria:

- Doctor/import-state checks identify legacy local pack sources such as
  `packs/gastown` and `packs/maintenance`.
- Diagnostics distinguish required Core behavior from optional Gastown behavior.
- Migration messaging no longer says Maintenance is supplied implicitly.

As a Gas City developer, I can run tests and lint checks that cover the
migration.

Acceptance criteria:

- Pack loading tests cover Core as the required pack and Gastown as an explicit
  external import.
- Tests that previously asserted Maintenance auto-inclusion are rewritten or
  removed.
- Docs and examples no longer instruct users to import Maintenance.

## Out Of Scope

- Redesigning the MEOW primitives or adding new SDK primitives.
- Reworking the Gastown role model beyond moving it to the correct pack.
- Changing external third-party packs except where their import metadata or
  documentation points at retired Maintenance assets.
- Renaming unrelated Go packages or config fields that use "maintenance" for
  supervisor/store maintenance rather than the retired Maintenance pack.
- Implementing the migration in this requirements phase.
- Replacing the public pack registry mechanism.
- Keeping historical comments in source files for removed pack locations.

## Other Notes

Resolved decisions from requirements review:

- Core source path: `internal/packs/core`.
- Core loading: Core is required in the resolved config; doctor must warn and
  offer a fix when it is absent.
- Core maintenance agent: Core defines a default configurable `dog` maintenance
  agent for agent-executed Core maintenance operations.
- Polecat formulas: Polecat is a Gastown concept and moves to
  `gascity-packs/gastown`.
- Dolt helper: `dolt-target.sh` moves to Core for now because Dolt remains a
  Core requirement until provider support is restored.
- Branch pruning: branch pruning originated in Gastown and moves to
  `gascity-packs/gastown`.
- Core opt-out: Core is mandatory for real cities. Missing Core is
  doctor-diagnosable and fixable. Explicit opt-out is unsupported except for a
  narrowly scoped dev/test escape hatch.

Current `origin/main` already contains partial migration work:

- The default Gastown template imports the public `gascity-packs/gastown` pack.
- The public `gascity-packs/gastown` pack no longer imports Maintenance.
- The in-tree `examples/gastown/packs/gastown` pack still imports
  `../maintenance`.
- Builtin pack materialization still embeds and auto-includes Core,
  Maintenance, `bd`, and `dolt` in several paths.
- The current Core pack is not purely generic. It contains Polecat, Refinery,
  Witness, Mayor, and Gastown references in formulas and skills.
- The current Maintenance Dog assets are usable only after removing Gastown
  requesters, notifications, and role examples that do not belong in Core.

Recommended classification rules for design:

- `core`: move to the canonical Gas City Core pack, preserving or rewriting the
  behavior as SDK-generic.
- `core-renamed`: move behavior to Core only after role-specific names, prompt
  text, assignees, formulas, metadata, and examples are made role-neutral.
- `gastown`: move to or keep in `gascity-packs/gastown`.
- `split`: divide the file between Core-generic and Gastown-specific outputs.
- `retire`: delete, replace, or rewrite because the old pack/package/test/doc
  should not survive the migration.
- `review`: inspect during design because ownership depends on contents or on a
  related pack decision.

Behavior preservation rule:

- No Gastown behavior may disappear as a side effect of making a Core asset
  generic. If an implementation strips Gastown-specific text, variables,
  requesters, notification targets, formulas, order triggers, script branches,
  or prompt instructions from a Core-bound asset, the same implementation must
  add or preserve equivalent behavior in `gascity-packs/gastown` and include it
  in the before/after inventory.

### Existing Asset Migration Map

#### Current Core Assets

| Current path | Target | Requirement |
| --- | --- | --- |
| `internal/bootstrap/packs/core/assets/prompts/graph-worker.md` | `core` | Keep only if role-neutral and useful to any graph/task worker. |
| `internal/bootstrap/packs/core/assets/prompts/pool-worker.md` | `core` | Keep only if role-neutral and not tied to Gastown pools. |
| `internal/bootstrap/packs/core/embed.go` | `retire` | Replace with embed/bootstrap code for the new canonical Core path. |
| `internal/bootstrap/packs/core/formulas/mol-do-work.toml` | `core-renamed` | Keep generic direct-work behavior; remove references to Polecat, Refinery, and Gastown formulas. |
| `internal/bootstrap/packs/core/formulas/mol-polecat-base.toml` | `gastown` | Polecat-specific formula behavior belongs in Gastown. |
| `internal/bootstrap/packs/core/formulas/mol-polecat-commit.toml` | `gastown` | Polecat-specific direct-commit workflow belongs in Gastown unless rewritten as a neutral Core workflow. |
| `internal/bootstrap/packs/core/formulas/mol-polecat-report.toml` | `gastown` | Polecat-specific reporting workflow belongs in Gastown unless rewritten as a neutral Core workflow. |
| `internal/bootstrap/packs/core/formulas/mol-prompt-synth.toml` | `core` | Keep if it is generic prompt synthesis with no Gastown role assumptions. |
| `internal/bootstrap/packs/core/formulas/mol-review-quorum.toml` | `review` | Keep in Core only if the review quorum is role-neutral and reusable outside Gastown; otherwise move to Gastown. |
| `internal/bootstrap/packs/core/formulas/mol-scoped-work.toml` | `core` | Keep if it is generic scoped work dispatch with no Gastown role assumptions. |
| `internal/bootstrap/packs/core/orders/beads-health.toml` | `core` | Keep as generic task-store health/order infrastructure. |
| `internal/bootstrap/packs/core/overlay/per-provider/codex/.codex/hooks.json` | `core` | Keep if it only installs generic `gc` hook behavior. |
| `internal/bootstrap/packs/core/overlay/per-provider/copilot/.github/copilot-instructions.md` | `core` | Keep if instructions are Gas City CLI-generic. |
| `internal/bootstrap/packs/core/overlay/per-provider/copilot/.github/hooks/gascity.json` | `core` | Keep if hook behavior is Gas City CLI-generic. |
| `internal/bootstrap/packs/core/overlay/per-provider/cursor/.cursor/hooks.json` | `core` | Keep if hook behavior is Gas City CLI-generic. |
| `internal/bootstrap/packs/core/overlay/per-provider/gemini/.gemini/settings.json` | `core` | Keep if settings are provider integration only. |
| `internal/bootstrap/packs/core/overlay/per-provider/kiro/.kiro/agents/gascity.json` | `core` | Keep if the agent description is role-neutral. |
| `internal/bootstrap/packs/core/overlay/per-provider/kiro/AGENTS.md` | `core` | Keep if instructions are Gas City CLI-generic. |
| `internal/bootstrap/packs/core/overlay/per-provider/omp/.omp/hooks/gc-hook.ts` | `core` | Keep as provider hook if generic. |
| `internal/bootstrap/packs/core/overlay/per-provider/opencode/.opencode/plugins/gascity.js` | `core` | Keep as provider hook if generic. |
| `internal/bootstrap/packs/core/overlay/per-provider/pi/.pi/extensions/gc-hooks.js` | `core` | Keep as provider hook if generic. |
| `internal/bootstrap/packs/core/pack.toml` | `core` | Move to the canonical Core pack path and ensure no Gastown imports. |
| `internal/bootstrap/packs/core/skills/gc-agents/SKILL.md` | `core` | Keep and update for role-neutral agent/session terminology. |
| `internal/bootstrap/packs/core/skills/gc-city/SKILL.md` | `core` | Keep as Core CLI usage skill. |
| `internal/bootstrap/packs/core/skills/gc-dashboard/SKILL.md` | `core` | Keep as Core CLI usage skill. |
| `internal/bootstrap/packs/core/skills/gc-dispatch/SKILL.md` | `split` | Keep generic `gc sling` guidance in Core; move Polecat, Refinery, Witness, Mayor, Deacon, and Gastown formula examples to Gastown. |
| `internal/bootstrap/packs/core/skills/gc-mail/SKILL.md` | `core` | Keep as Core CLI usage skill if role-neutral. |
| `internal/bootstrap/packs/core/skills/gc-rigs/SKILL.md` | `core` | Keep as Core CLI usage skill if role-neutral. |
| `internal/bootstrap/packs/core/skills/gc-work/SKILL.md` | `core` | Keep as Core CLI usage skill if role-neutral. |

#### Current Maintenance Assets

| Current path | Target | Requirement |
| --- | --- | --- |
| `examples/gastown/packs/maintenance/README.md` | `split` | Replace with Core operational docs plus Gastown migration notes; no standalone Maintenance docs remain. |
| `examples/gastown/packs/maintenance/agents/dog/agent.toml` | `core` | Move as Core's default configurable maintenance agent. |
| `examples/gastown/packs/maintenance/agents/dog/overlay/.gitkeep` | `core` | Keep only if the Core `dog` agent still needs an overlay directory. |
| `examples/gastown/packs/maintenance/agents/dog/prompt.template.md` | `core-renamed` | Move to Core after removing Gastown requesters, notifications, and role examples. |
| `examples/gastown/packs/maintenance/assets/scripts/_bd_trace.sh` | `core` | Keep if it is generic task-store trace support. |
| `examples/gastown/packs/maintenance/assets/scripts/cascade-nudge-on-blocker-close.sh` | `core` | Keep as generic dependency/routing maintenance if role-neutral. |
| `examples/gastown/packs/maintenance/assets/scripts/cross-rig-deps.sh` | `core` | Keep as generic cross-rig dependency maintenance if role-neutral. |
| `examples/gastown/packs/maintenance/assets/scripts/dolt-target.sh` | `core` | Move to Core for the current Dolt-required architecture; revisit when bd providers are re-enabled. |
| `examples/gastown/packs/maintenance/assets/scripts/gate-sweep.sh` | `core` | Keep as generic gate/order maintenance if role-neutral. |
| `examples/gastown/packs/maintenance/assets/scripts/jsonl-export.sh` | `core-renamed` | Keep generic export behavior under Core `dog`; remove Mayor and Gastown daemon assumptions. |
| `examples/gastown/packs/maintenance/assets/scripts/nudge-on-route.sh` | `core` | Keep as generic route-to-session maintenance if role-neutral. |
| `examples/gastown/packs/maintenance/assets/scripts/orphan-sweep.sh` | `core` | Keep as generic orphan bead/work recovery if role-neutral. |
| `examples/gastown/packs/maintenance/assets/scripts/prune-branches.sh` | `gastown` | Branch pruning originated in Gastown and belongs in the Gastown pack. |
| `examples/gastown/packs/maintenance/assets/scripts/reaper.sh` | `core-renamed` | Keep generic stale wisp/order cleanup behavior under Core `dog`; remove Mayor and Gastown escalation assumptions. |
| `examples/gastown/packs/maintenance/assets/scripts/spawn-storm-detect.sh` | `core` | Keep as generic spawn-storm health detection if role-neutral. |
| `examples/gastown/packs/maintenance/assets/scripts/wisp-compact.sh` | `core` | Keep as generic molecule/wisp compaction if role-neutral. |
| `examples/gastown/packs/maintenance/doctor/check-binaries/doctor.toml` | `core` | Keep as generic required-binary doctor check if not Gastown-specific. |
| `examples/gastown/packs/maintenance/doctor/check-binaries/run.sh` | `core` | Keep as generic required-binary doctor check if not Gastown-specific. |
| `examples/gastown/packs/maintenance/embed.go` | `retire` | Remove with the standalone Maintenance pack. |
| `examples/gastown/packs/maintenance/formulas/mol-dog-jsonl.toml` | `core-renamed` | Preserve generic JSONL export under Core `dog`; remove Mayor and Gastown role behavior. |
| `examples/gastown/packs/maintenance/formulas/mol-dog-reaper.toml` | `core-renamed` | Preserve generic cleanup/reaper behavior under Core `dog`; remove Mayor and Gastown role behavior. |
| `examples/gastown/packs/maintenance/formulas/mol-shutdown-dance.toml` | `core-renamed` | Preserve generic stuck-session due process under Core `dog`; move Gastown detector examples to Gastown. |
| `examples/gastown/packs/maintenance/orders/cascade-nudge-on-blocker-close.toml` | `core` | Keep as generic dependency/routing order if role-neutral. |
| `examples/gastown/packs/maintenance/orders/cross-rig-deps.toml` | `core` | Keep as generic cross-rig dependency order if role-neutral. |
| `examples/gastown/packs/maintenance/orders/gate-sweep.toml` | `core` | Keep as generic gate maintenance order. |
| `examples/gastown/packs/maintenance/orders/mol-dog-jsonl.toml` | `core` | Preserve generic JSONL export scheduling for Core `dog`. |
| `examples/gastown/packs/maintenance/orders/mol-dog-reaper.toml` | `core` | Preserve generic stale cleanup scheduling for Core `dog`. |
| `examples/gastown/packs/maintenance/orders/nudge-on-route.toml` | `core` | Keep as generic routing/nudge order if role-neutral. |
| `examples/gastown/packs/maintenance/orders/order-tracking-sweep.toml` | `core` | Keep as generic order tracking/cleanup order. |
| `examples/gastown/packs/maintenance/orders/orphan-sweep.toml` | `core` | Keep as generic orphan cleanup order. |
| `examples/gastown/packs/maintenance/orders/prune-branches.toml` | `gastown` | Branch pruning originated in Gastown and belongs in the Gastown pack. |
| `examples/gastown/packs/maintenance/orders/spawn-storm-detect.toml` | `core` | Keep as generic spawn-storm detection order. |
| `examples/gastown/packs/maintenance/orders/wisp-compact.toml` | `core` | Keep as generic wisp compaction order. |
| `examples/gastown/packs/maintenance/pack.toml` | `retire` | Remove standalone Maintenance pack metadata. |
| `examples/gastown/packs/maintenance/pack_test.go` | `retire` | Replace with Core and Gastown pack tests at their new homes. |
| `examples/gastown/packs/maintenance/template-fragments/architecture.template.md` | `split` | Move generic Gas City architecture guidance to Core skills/docs; move role behavior to Gastown. |
| `examples/gastown/packs/maintenance/template-fragments/following-mol.template.md` | `split` | Move generic formula-following guidance to Core only if role-neutral; otherwise Gastown. |
| `examples/gastown/packs/maintenance/template-fragments/propulsion.template.md` | `gastown` | Prompt propulsion behavior belongs with configured Gastown agents unless rewritten as neutral Core CLI guidance. |
| `examples/gastown/packs/maintenance/testenv_import_test.go` | `retire` | Replace with tests for explicit Core/Gastown import behavior. |

#### Current Gastown Assets

| Current path | Target | Requirement |
| --- | --- | --- |
| `examples/gastown/packs/gastown/agents/boot/agent.toml` | `gastown` | Boot is Gastown role configuration. |
| `examples/gastown/packs/gastown/agents/boot/prompt.template.md` | `gastown` | Boot prompt belongs in Gastown. |
| `examples/gastown/packs/gastown/agents/deacon/agent.toml` | `gastown` | Deacon is Gastown role configuration. |
| `examples/gastown/packs/gastown/agents/deacon/prompt.template.md` | `gastown` | Deacon prompt belongs in Gastown. |
| `examples/gastown/packs/gastown/agents/mayor/agent.toml` | `gastown` | Mayor is Gastown role configuration. |
| `examples/gastown/packs/gastown/agents/mayor/prompt.template.md` | `gastown` | Mayor prompt belongs in Gastown. |
| `examples/gastown/packs/gastown/agents/polecat/agent.toml` | `gastown` | Polecat is Gastown role configuration. |
| `examples/gastown/packs/gastown/agents/polecat/namepool.txt` | `gastown` | Polecat name pool belongs in Gastown. |
| `examples/gastown/packs/gastown/agents/polecat/prompt.template.md` | `gastown` | Polecat prompt belongs in Gastown. |
| `examples/gastown/packs/gastown/agents/refinery/agent.toml` | `gastown` | Refinery is Gastown role configuration. |
| `examples/gastown/packs/gastown/agents/refinery/prompt.template.md` | `gastown` | Refinery prompt belongs in Gastown. |
| `examples/gastown/packs/gastown/agents/witness/agent.toml` | `gastown` | Witness is Gastown role configuration. |
| `examples/gastown/packs/gastown/agents/witness/prompt.template.md` | `gastown` | Witness prompt belongs in Gastown. |
| `examples/gastown/packs/gastown/assets/namepools/minerals.txt` | `gastown` | Gastown naming asset. |
| `examples/gastown/packs/gastown/assets/prompts/crew.template.md` | `gastown` | Crew prompt belongs in Gastown. |
| `examples/gastown/packs/gastown/assets/scripts/agent-menu.sh` | `gastown` | Gastown operator UI/helper script. |
| `examples/gastown/packs/gastown/assets/scripts/bind-key.sh` | `gastown` | Gastown operator UI/helper script. |
| `examples/gastown/packs/gastown/assets/scripts/checks/adopt-pr-review-approved.sh` | `gastown` | Gastown review workflow check. |
| `examples/gastown/packs/gastown/assets/scripts/checks/code-review-approved.sh` | `gastown` | Gastown review workflow check. |
| `examples/gastown/packs/gastown/assets/scripts/checks/design-review-approved.sh` | `gastown` | Gastown review workflow check. |
| `examples/gastown/packs/gastown/assets/scripts/cycle.sh` | `gastown` | Gastown workflow helper. |
| `examples/gastown/packs/gastown/assets/scripts/polecat-churn-watcher.sh` | `gastown` | Polecat-specific observability belongs in Gastown. |
| `examples/gastown/packs/gastown/assets/scripts/status-line.sh` | `gastown` | Gastown operator UI/helper script. |
| `examples/gastown/packs/gastown/assets/scripts/tmux-keybindings.sh` | `gastown` | Gastown operator UI/helper script. |
| `examples/gastown/packs/gastown/assets/scripts/tmux-theme.sh` | `gastown` | Gastown operator UI/helper script. |
| `examples/gastown/packs/gastown/assets/scripts/worktree-setup.sh` | `gastown` | Gastown worktree workflow helper. |
| `examples/gastown/packs/gastown/commands/status/help.md` | `gastown` | Gastown-specific command help. |
| `examples/gastown/packs/gastown/commands/status/run.sh` | `gastown` | Gastown-specific command implementation. |
| `examples/gastown/packs/gastown/doctor/check-scripts/run.sh` | `gastown` | Gastown pack self-check. |
| `examples/gastown/packs/gastown/embed.go` | `retire` | External pack should not need an in-tree Go embed package. |
| `examples/gastown/packs/gastown/formulas/mol-deacon-patrol.toml` | `gastown` | Deacon patrol is Gastown orchestration. |
| `examples/gastown/packs/gastown/formulas/mol-digest-generate.toml` | `gastown` | Digest generation to Mayor is Gastown orchestration. |
| `examples/gastown/packs/gastown/formulas/mol-idea-to-plan.toml` | `gastown` | Gastown planning/review flow. |
| `examples/gastown/packs/gastown/formulas/mol-polecat-work.toml` | `gastown` | Polecat/Refinery work flow. |
| `examples/gastown/packs/gastown/formulas/mol-refinery-patrol.toml` | `gastown` | Refinery patrol is Gastown orchestration. |
| `examples/gastown/packs/gastown/formulas/mol-review-leg.toml` | `gastown` | Polecat review leg is Gastown workflow unless rewritten as neutral Core review. |
| `examples/gastown/packs/gastown/formulas/mol-witness-patrol.toml` | `gastown` | Witness patrol is Gastown orchestration. |
| `examples/gastown/packs/gastown/orders/digest-generate.toml` | `gastown` | Mayor digest order is Gastown-specific. |
| `examples/gastown/packs/gastown/overlay/per-provider/codex/.codex/hooks.json` | `review` | Move to Core only if it is generic Gas City hook behavior; otherwise keep in Gastown. |
| `examples/gastown/packs/gastown/pack.toml` | `gastown` | Move authoritative metadata to `gascity-packs/gastown`; remove `../maintenance` import. |
| `examples/gastown/packs/gastown/template-fragments/approval-fallacy.template.md` | `gastown` | Gastown prompt behavior. |
| `examples/gastown/packs/gastown/template-fragments/architecture.template.md` | `gastown` | Gastown prompt fragment unless split into generic Core docs. |
| `examples/gastown/packs/gastown/template-fragments/capability-ledger.template.md` | `gastown` | Gastown prompt behavior. |
| `examples/gastown/packs/gastown/template-fragments/command-glossary.template.md` | `split` | Keep generic `gc` CLI glossary in Core skills; move role-specific examples to Gastown. |
| `examples/gastown/packs/gastown/template-fragments/following-mol.template.md` | `gastown` | Gastown prompt behavior unless rewritten as neutral Core guidance. |
| `examples/gastown/packs/gastown/template-fragments/operational-awareness.template.md` | `gastown` | Gastown prompt behavior. |
| `examples/gastown/packs/gastown/template-fragments/propulsion.template.md` | `gastown` | Gastown prompt behavior. |
| `examples/gastown/packs/gastown/template-fragments/tdd-discipline.template.md` | `split` | Generic TDD guidance may move to Core docs/skills; role-specific prompt behavior remains Gastown. |

### Downstream References To Update

The implementation plan must account for these reference classes:

- Builtin pack registry and materialization code, especially
  `internal/builtinpacks/registry.go`, `cmd/gc/embed_builtin_packs.go`, and
  related tests.
- Hook code that imports the current Core package from
  `internal/bootstrap/packs/core`.
- Doctor/import-state diagnostics that currently say Core and Maintenance are
  supplied implicitly.
- Template and example configuration under `examples/gastown`.
- Acceptance tests and docs that reference `.gc/system/packs/maintenance`,
  `packs/maintenance`, or `packs/gastown`.
- `examples/dolt` and other packs that currently depend on Maintenance scripts.
- Tests that assert Maintenance order names are auto-included.
- Documentation that describes the in-tree Gastown pack as the source of truth.

### Remaining Open Questions For Design

- None at requirements level. Design may still resolve implementation details
  such as exact fix-command wiring, test helper opt-out shape, and ownership of
  assets marked `review`.
