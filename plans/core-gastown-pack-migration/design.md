---
plan_slug: core-gastown-pack-migration
phase: design
rig: gascity
rig_root: /data/projects/gascity-fresh-main-20260604-VLKm8c
artifact_root: /data/projects/gascity-fresh-main-20260604-VLKm8c/plans
requirements_file: /data/projects/gascity-fresh-main-20260604-VLKm8c/plans/core-gastown-pack-migration/requirements.md
status: draft
created_at: 2026-06-04T15:07:35Z
updated_at: 2026-06-04T15:07:35Z
---

# Design: Core and Gastown Pack Split

## Summary

Move Gas City's required Core pack to `internal/packs/core`, retire the
standalone Maintenance pack, and make `gascity-packs/gastown` the only source
of Gastown behavior. Core remains a required system pack that `gc` materializes
and injects into normal config loads. Gastown remains an explicit external pack
import.

The migration has two coordinated tracks:

- Gas City repo: own Core, pack loading, doctor/fix behavior, source-tree
  cleanup, and tests/docs that describe system packs.
- `gascity-packs` repo: own Gastown roles, formulas, orders, scripts, prompts,
  and any Gastown behavior stripped from Core-bound assets.

Core is mandatory for real cities. There is no production opt-out. Tests and
low-level config fixtures may bypass Core only by using lower-level loading
helpers that do not represent the normal `gc` runtime path.

Gastown behavior preservation is a release gate. If a current Core or
Maintenance asset is generalized for Core, the implementation must retain or add
the removed Gastown-specific behavior in `gascity-packs/gastown` and record it
in a before/after behavior inventory.

## Current System

`internal/builtinpacks/registry.go` defines the embedded pack set:

- `core`, sourced from `internal/bootstrap/packs/core`
- `bd`, sourced from `examples/bd`
- `dolt`, sourced from `examples/dolt`
- `maintenance`, sourced from `examples/gastown/packs/maintenance`
- `gastown`, sourced from `examples/gastown/packs/gastown`

`cmd/gc/embed_builtin_packs.go` materializes all embedded packs into
`.gc/system/packs/{name}`. `requiredBuiltinPackNames` currently always requires
`core` and `maintenance`, then adds `bd` or `dolt` depending on the beads
provider. `builtinPackIncludes` appends those materialized system pack
directories to `config.LoadWithIncludes`, so Core and Maintenance are implicit
normal config layers.

`internal/hooks/hooks.go` imports `internal/bootstrap/packs/core` directly and
reads provider overlays from `core.PackFS`.

`internal/bootstrap/bootstrap.go` still embeds `packs/**` even though
`BootstrapPacks` is empty in production. Several tests override
`BootstrapPacks` with `AssetDir: "packs/core"`, so removing
`internal/bootstrap/packs/core` also requires a replacement test fixture or an
injectable bootstrap test filesystem.

`cmd/gc/import_state_doctor_check.go` currently rewrites legacy local Gastown
imports to `config.PublicGastownPackSource` and removes Maintenance imports
with the message that Maintenance/Core are supplied implicitly. That wording and
fix behavior no longer match the target state.

Fresh Gastown init already writes explicit public `gascity-packs/gastown`
imports through `internal/config.GastownCityWithProviders`, using
`PublicGastownPackSource` and `PublicGastownPackVersion`. The public
`gascity-packs/gastown` pack no longer imports Maintenance, but its comments
and patches still assume the host runtime supplies an implicit maintenance/core
utility layer.

The in-repo `examples/gastown` tree still has local pack sources:

- `examples/gastown/pack.toml` imports `packs/gastown`.
- `examples/gastown/city.toml` default rig imports `packs/gastown`.
- `examples/gastown/packs/gastown/pack.toml` imports `../maintenance`.
- `examples/gastown/packs/maintenance` still owns Dog, cleanup orders, and
  maintenance scripts.

Tests and docs have many path and behavior assumptions:

- `internal/builtinpacks/registry_test.go` pins the embedded pack list and
  synthetic repo paths.
- `cmd/gc/embed_builtin_packs_test.go` asserts Maintenance order paths and
  builtin include counts.
- `cmd/gc/controller_test.go` expects orders from Core, Maintenance, `bd`, and
  `dolt`.
- `cmd/gc/import_state_doctor_check_test.go` expects Maintenance imports to be
  removed because Maintenance/Core are implicit.
- `examples/gastown/gastown_test.go` contains extensive Gastown pack behavior
  tests against local `examples/gastown/packs/gastown`.
- Docs such as `docs/guides/shareable-packs.md`,
  `docs/reference/system-packs.md`, and `docs/getting-started/troubleshooting.md`
  describe implicit Maintenance or `.gc/system/packs/maintenance`.

## Proposed Design

### Pack Ownership

Create `internal/packs/core` as the canonical Core package:

- Move Core pack assets from `internal/bootstrap/packs/core` to
  `internal/packs/core`.
- Add `internal/packs/core/embed.go` with `PackFS` embedding Core assets.
- Update `internal/hooks/hooks.go` and `internal/builtinpacks/registry.go` to
  import `github.com/gastownhall/gascity/internal/packs/core`.
- Remove `internal/bootstrap/packs/core` as an asset source.

Retire the Maintenance package:

- Remove `examples/gastown/packs/maintenance/embed.go`, `pack.toml`, and
  Maintenance-only tests after assets are moved.
- Do not create a replacement `maintenance` system pack.
- Keep "maintenance" Go/config names that refer to supervisor/store
  maintenance out of scope.

Move Gastown assets out of the Gas City source tree:

- Remove `examples/gastown/packs/gastown` as a maintained pack source.
- Keep `examples/gastown` only as an example city, if still useful, with public
  `gascity-packs/gastown` imports rather than local `packs/gastown` imports.
- Move pack-specific tests from `examples/gastown/gastown_test.go` to the
  `gascity-packs` repo, or replace them with Gas City tests that assert init and
  import wiring only.

### Core Asset Contents

Core owns:

- CLI usage skills under `skills/gc-*`, after removing Gastown-only examples.
- Provider hook overlays under `overlay/per-provider/*` when they are generic
  Gas City hook behavior.
- Generic formulas such as direct work, scoped work, prompt synthesis, and
  review quorum only when role-neutral.
- Generic infrastructure orders and scripts from Maintenance, including gate
  sweep, route nudge, blocker-close cascade, orphan sweep, order tracking,
  spawn storm detection, wisp compaction, JSONL export, stale cleanup/reaper,
  and binary doctor checks.
- A default configurable `dog` maintenance agent. `dog` is allowed as pack
  configuration, not as a Go special case.
- `dolt-target.sh` for now, because Dolt remains a Core requirement until bd
  provider support is restored.

Core must not own:

- Mayor, Deacon, Polecat, Refinery, Witness, Boot, Crew, or Gastown role
  behavior.
- Branch pruning. Move `prune-branches.sh` and `orders/prune-branches.toml` to
  `gascity-packs/gastown`.
- Polecat formulas. Move `mol-polecat-base`, `mol-polecat-commit`, and
  `mol-polecat-report` to `gascity-packs/gastown`.
- Gastown examples inside `gc-dispatch` or other Core skills.

Dog-specific Core formulas may keep names such as `mol-dog-jsonl` and
`mol-dog-reaper` for compatibility, because `dog` is now the Core maintenance
agent. The implementation should rename only Gastown role names, not `dog`
itself. If a formula is renamed, add compatibility aliases or migration tests
for existing order skips and references.

`mol-shutdown-dance` should move to Core as generic stuck-session due process
for the Core `dog` agent. Its current detector table and examples mention
Deacon, Witness, Polecat, and Mayor. Remove those from the Core copy, then add
or preserve equivalent Gastown detector/requester instructions in Gastown-owned
prompts or formulas.

`mol-review-quorum` remains a design-time audit item. If its content is
role-neutral, keep it in Core. If it references Polecat, Refinery, Mayor, or
Gastown review flows, move it to `gascity-packs/gastown`.

### Gastown Behavior Preservation

Add a behavior inventory in the `gascity-packs` repo, recommended path:
`gastown/docs/behavior-preservation.md`.

The inventory must list every moved or generalized asset with:

- old path
- new Core path, if any
- new Gastown path, if any
- preserved behavior
- behavior intentionally removed from Core
- replacement Gastown behavior
- test coverage

This inventory is required for these high-risk moves:

- Maintenance `dog` prompt to Core, with Gastown notification/requester behavior
  preserved in Gastown prompts or formulas.
- `mol-shutdown-dance` to Core, with Gastown detector examples preserved in
  Deacon/Witness/Boot/Gastown documentation.
- `mol-dog-jsonl` and `mol-dog-reaper` to Core, with Mayor/Deacon/Gastown
  notifications preserved or replaced.
- `prune-branches` to Gastown.
- Core `mol-polecat-*` formulas to Gastown.
- Core `gc-dispatch` skill split.

The public `gascity-packs/gastown/pack.toml` should be updated so comments no
longer describe an implicit Maintenance layer. Gastown should not import Core;
Core remains a required host system pack. Gastown may continue to patch the
Core `dog` agent for theming or work_dir behavior if that patch is the explicit
Gastown behavior, and the inventory should name that dependency.

After the `gascity-packs` change lands, update Gas City's
`internal/config/PublicGastownPackVersion` and registry expectations to the new
commit/hash.

### Builtin Registry And Synthetic Cache

Update `internal/builtinpacks/registry.go`:

- `All()` should return Core, `bd`, and `dolt`.
- Core subpath becomes `internal/packs/core`.
- Remove Maintenance and Gastown from the Gas City embedded set.
- Remove public synthetic aliases for `gastown` and `maintenance`, because
  Gastown is no longer embedded in the Gas City binary.
- Keep synthetic cache validation for the remaining bundled packs.

Update tests in `internal/builtinpacks/registry_test.go`:

- Expected identities become `core=internal/packs/core`, `bd=examples/bd`, and
  `dolt=examples/dolt`.
- Source-recognition variants use `internal/packs/core`.
- Tamper and unexpected-file tests write under `internal/packs/core`.
- Public Gastown synthetic cache tests move out of Gas City or change to assert
  no bundled synthetic cache is available for public Gastown sources.

Do not aggressively delete stale `.gc/system/packs/maintenance` or
`.gc/system/packs/gastown` directories on startup. They should simply stop
being generated and stop being included. A separate doctor advisory can report
unused legacy system pack directories if needed. This avoids deleting operator
edits in formerly non-required generated packs.

### System Pack Loading

Update `cmd/gc/embed_builtin_packs.go`:

- `requiredBuiltinPackNames` starts with `[]string{"core"}`.
- Keep provider-dependent `bd` and `dolt` inclusion as today.
- Remove Maintenance from required pack refresh and `builtinPackIncludes`.
- Update comments from "Core and maintenance" to "Core".
- Keep `MaterializeBuiltinPacks` as the single materialization entrypoint for
  required system packs.

Update config-loading call sites:

- Normal command paths must use `loadCityConfigWithBuiltinPacks`,
  `cityConfigIncludesWithBuiltinPacks`, or `builtinPackIncludesForConfigLoad`.
- Direct `config.LoadWithIncludes` calls in production `cmd/gc` files should be
  audited. If they represent runtime config resolution, route them through the
  system-pack wrapper. If they intentionally inspect partial/broken config, add
  a comment and a focused test.
- Low-level `internal/config` tests may continue to call `config.LoadWithIncludes`
  directly because those tests exercise the config package, not full `gc`
  runtime behavior.

The dev/test escape hatch is not a CLI flag or environment variable. Production
`gc` commands always include Core. Tests that need no-Core behavior should call
the lower-level config loader directly or use a `_test.go` helper that bypasses
`cityConfigIncludesWithBuiltinPacks`.

### Core Presence Doctor

Add a new doctor check, recommended file:
`cmd/gc/core_pack_doctor_check.go`.

The check should:

- Materialize intent: Core is required for real cities.
- Verify `.gc/system/packs/core/pack.toml` exists and matches the embedded Core
  manifest using the same manifest comparison behavior already used by
  `unusableRequiredBuiltinPackNames`.
- Load resolved config through the normal system-pack wrapper and verify
  provenance includes the Core system pack path.
- Return Error or Warning according to the existing doctor severity convention
  for required repairable checks.
- Include details naming the missing condition: missing materialized Core,
  invalid materialized Core, or Core absent from resolved config provenance.
- Set `FixHint` to run `gc doctor --fix`.

`Fix` should:

- Call `MaterializeBuiltinPacks(cityPath)`.
- Re-run the Core manifest/provenance checks.
- Return an error if Core is still absent, because production opt-out is not
  supported.

This fix "adds Core" by repairing the generated system pack and normal include
path. It should not write `[imports.core]` to `city.toml` or `pack.toml`.

Update existing doctor/import-state behavior:

- In `cmd/gc/import_state_doctor_check.go`, change Maintenance messaging from
  "maintenance/core is supplied implicitly" to "maintenance is retired; Core
  supplies generic maintenance and Gastown supplies Gastown-specific behavior".
- Continue rewriting legacy Gastown local imports to public
  `gascity-packs/gastown`.
- Continue removing legacy Maintenance imports when the source is
  `.gc/system/packs/maintenance` or `examples/gastown/packs/maintenance`.
- Add or adjust tests so doctor fix no longer implies a standalone Maintenance
  layer.
- Flag explicit durable Core imports pointing at generated or legacy sources,
  such as `.gc/system/packs/core` or old `internal/bootstrap/packs/core`, as
  redundant and fixable. The fix may remove those imports after confirming the
  required Core system pack is present.
- If a city imports a custom `core` source, report it as unsupported/manual.
  Core is not user-replaceable in production; users can override Core-provided
  pack configuration through normal config layering, but not replace the
  required Core system pack itself.

### Bootstrap Cleanup

`internal/bootstrap/bootstrap.go` should no longer depend on
`internal/bootstrap/packs/core`.

Options:

- Preferred: introduce a small `internal/bootstrap/testdata/packs/core` fixture
  used only by bootstrap tests, and make tests inject `bootstrapAssets` to that
  fixture filesystem.
- Alternative: keep a tiny non-production bootstrap fixture under
  `internal/bootstrap/packs/test-core` and update test `AssetDir` values.

The production `BootstrapPacks` default remains empty. If there are no
production bootstrap assets, remove the `//go:embed packs/**` dependency or
replace it with an embed path that is explicitly a test/compatibility fixture.

### Examples And Docs

Update `examples/gastown`:

- Keep it as an example city only if it can use public Gastown imports without
  vendoring `packs/gastown`.
- Remove `examples/gastown/packs/maintenance`.
- Remove `examples/gastown/packs/gastown`.
- Update comments to describe Core as the required system pack and Gastown as
  the explicit external pack.
- Update tests that currently read `examples/gastown/packs/gastown` to either
  move to `gascity-packs` or assert the public-import wiring.

Update docs:

- `docs/reference/system-packs.md`: Core is the required system pack; `bd` and
  `dolt` remain provider-dependent system packs; Maintenance and Gastown are no
  longer system packs.
- `docs/guides/shareable-packs.md`: remove "core and maintenance stay
  implicit" guidance.
- `docs/getting-started/troubleshooting.md`: replace
  `.gc/runtime/packs/maintenance` and `.gc/system/packs/maintenance` references
  with Core or Gastown paths as appropriate.
- `docs/tutorials/01-cities-and-rigs.md`: describe the `dog` pool as Core's
  configurable maintenance agent, not the Maintenance pack.
- Keep `[maintenance.dolt]` store-maintenance docs intact unless they refer to
  the retired Maintenance pack.

## Testing

### Gas City Unit Tests

Add or update tests for:

- `internal/builtinpacks.All`: only Core, `bd`, and `dolt`; Core subpath is
  `internal/packs/core`.
- Source recognition for `internal/packs/core`; old
  `internal/bootstrap/packs/core` sources should be rejected or covered by
  explicit migration diagnostics.
- Synthetic cache materialization and validation for the remaining bundled
  packs.
- `MaterializeBuiltinPacks`: no Maintenance or Gastown materialization expected;
  Core includes moved maintenance orders and scripts.
- `builtinPackIncludes`: default includes Core and `bd`; non-bd includes Core
  only; exec `gc-beads-bd` includes Core, `bd`, and `dolt`.
- Required pack refresh: Core is repaired when missing or corrupted.
- Core presence doctor: missing/corrupt Core is reported and fixed.
- Import-state doctor: legacy Gastown imports rewrite to public
  `gascity-packs/gastown`; legacy Maintenance imports are removed with the new
  retired-pack wording.
- Hook installation: `internal/hooks` reads overlays from `internal/packs/core`.
- Core role-name guard: Core assets must not contain Mayor, Deacon, Polecat,
  Refinery, Witness, Boot, Crew, or Gastown outside allowed docs/tests.
  `dog` is allowed only under Core maintenance-agent assets.

### Gastown Pack Tests

In `gascity-packs`:

- Move Gastown pack behavior tests from `examples/gastown/gastown_test.go` or
  recreate equivalent tests against `gastown/`.
- Add behavior-preservation tests for all inventory entries.
- Verify branch pruning order/script exists in Gastown.
- Verify Polecat formulas moved from Core and still compose with
  `mol-polecat-work`.
- Verify Gastown detector/requester behavior removed from Core still exists in
  Gastown prompts or formulas.
- Verify Gastown can run with host Core present and without a Maintenance pack.

### Integration And Regression Coverage

Run focused suites first:

- `go test ./internal/builtinpacks ./internal/hooks ./internal/bootstrap`
- `go test ./cmd/gc -run 'BuiltinPack|ImportStateDoctor|CorePack|TryReloadConfig|MaterializeBuiltinPacks'`
- `go test ./test/packlint`

Then run the repo gate documented in `TESTING.md`:

- `make test-fast-parallel`
- `go vet ./...`

Run `make dashboard-check` only if API, dashboard, or generated schema files
are touched. This migration should not require dashboard changes.

## Rollout

Use a staged rollout to keep Gas City and `gascity-packs` in sync.

1. Land `gascity-packs` Gastown behavior preservation first on a branch:
   move/add Gastown-owned assets, add the behavior inventory, update Gastown
   tests, and record the resulting commit/hash.
2. Update Gas City to point fresh Gastown init at the new
   `gascity-packs/gastown` release commit.
3. Move Core assets to `internal/packs/core` and fold Core-owned Maintenance
   assets into Core.
4. Remove Maintenance and Gastown from Gas City's embedded pack registry.
5. Update doctor checks and fixes.
6. Remove in-tree `examples/gastown/packs/*` sources and migrate docs/tests.
7. Run focused tests, then full local gates.

Backward compatibility:

- Existing cities with legacy local Gastown imports get rewritten to the public
  Gastown pack by `gc doctor --fix`.
- Existing cities with legacy Maintenance imports get those imports removed by
  `gc doctor --fix`; Core supplies generic maintenance and public Gastown
  supplies Gastown-specific behavior.
- Existing stale `.gc/system/packs/maintenance` or `.gc/system/packs/gastown`
  directories are ignored by config loading after the migration. Doctor may
  report them as legacy ignored directories, but `gc doctor --fix` must not
  delete them because they may contain operator edits.
- Existing order skip lists containing moved Core order names should continue
  to work when names are preserved. If any order is renamed, provide aliases or
  a migration test.

Operational risk:

- The highest risk is losing Gastown behavior while generalizing Core. The
  behavior inventory and Gastown pack tests are the mitigation.
- The second risk is stale assumptions in `cmd/gc` direct config loads. The
  production call-site audit and Core presence doctor are the mitigation.
- The third risk is public pack version skew. Land `gascity-packs` first and
  update `PublicGastownPackVersion` only after the public commit is available.

## Open Questions

No shared requirements or policy questions remain.

Implementation audits still required:

- Audit `mol-review-quorum` for Gastown role assumptions. Its final home is
  Core only if it is role-neutral.
- Audit the current Gastown Codex overlay to decide whether it is generic Core
  hook behavior or Gastown-specific provider configuration.
