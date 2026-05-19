# Active Workstream Coordination

Last updated: 2026-05-18 12:45 PT by Jasmine

This is a temporary cross-agent coordination channel, not product documentation.
Do not merge this file into public docs unless we explicitly promote it.

Use this file for concise handoffs between active agents. Prefer factual state,
links, branch names, and explicit interface constraints over narrative.

Severity labels:

- `red`: blocks another workstream.
- `yellow`: coordinate before touching the affected area.
- `green`: informational.

## Communication Mechanism

Chosen mechanism: repo-backed coordination branch.

- Repository: `gastownhall/gascity`
- Branch: `codex/workstream-coordination`
- File: `engdocs/coordination/active-workstreams.md`

Agents should fetch this branch when they need the latest shared coordination
state. Agents may propose updates on their own branches or directly update this
coordination branch when asked, but this branch is not a product PR.

## Workstream Handoff

### Workstream

JSON

### Current Branch / PR

Branch: `codex/json-rollup`

PR: not opened yet

Base: `origin/main`

Owner: Jasmine

Worktree: `/Users/dbox/repos/gc/gascity-json-rollup`

### Latest State

Jasmine owns the JSON rollout end to end. The previous many-small-PR strategy
is replaced by a single JSON rollup / review-train PR so Julian can review one
coherent `gc --json` / `--json-schema` surface instead of many small PRs.

`codex/json-rollup` now exists and is pushed. It currently points at latest
`origin/main`; train assembly has not started yet.

Current JSON source of truth is this workstream section plus
`codex/json-rollup`, not any individual JSON PR.

Included provenance PRs for the first train, if they remain clean:

- #2317: schema-platform plumbing plus native management action JSON.
- #2222: session detail JSON plus oddball/root command JSON.
- #2250: formula/order inspection JSON.
- #2257: convoy inspection JSON.
- #2258: agent/rig routing inspection JSON.
- #2259: mail/trace/events inspection JSON.
- #2265: miscellaneous inspection command JSON.
- #2266: runtime/nudge/drain inspection JSON.
- #2267: doctor diagnostics JSON.
- #2271: lifecycle action summary JSON.
- #2273: graph/converge/order/formula action summary JSON.
- #2274: convoy/mail action summary JSON.
- #2287: open passthrough/custom schema support.

Conditional include:

- #2256: service/skill inspection JSON. Earlier failed CI looked like
  cancellation/flaky infra after local tests passed, but it must be revalidated
  before inclusion.

Excluded from the first train unless repaired:

- #2288: superseded by #2317's adoption branch payload.
- #2270: local rebase branch had `TestAutoSuspendChatSessions` failure from
  deprecated `[[agent]]` warning leakage to stderr.
- #2291: same local `TestAutoSuspendChatSessions` failure family as #2270.

### Interface Contracts Other Agents Must Honor

- Human-readable output remains default.
- `--json` emits deterministic machine-readable output.
- stdout must be JSON-only when `--json` is used.
- Human diagnostics and warnings go to stderr unless intentionally represented
  in JSON.
- `--json-schema` exposes command schema metadata. The role-specific form
  `--json-schema=result` is accepted for result schemas.
- Result schemas live under `schemas/<command-path>/result.schema.json`.
- Shared failure schema lives at `schemas/failure.schema.json`.
- Do not introduce `--format json`.

Structured failure JSON policy:

- New JSON-enabled commands should use the shared failure schema where the
  platform path applies.
- Full structured failure JSON for every command is staged command-by-command,
  not a reason to block otherwise clean result-schema work.
- Commands with intentional command-authored nonzero JSON must preserve that
  behavior and declare compatible schemas/tests.

Schema extension conventions:

- JSON Schema remains the schema language.
- Gas City extensions use `x-gc-*`.
- `x-gc-jsonl` remains the convention for JSONL record-count metadata. Absence
  means a single JSON document unless command docs/schema say otherwise.
- Keep schemas open where the producer is a passthrough or custom command and
  Gas City does not own the payload shape.

Validation matrix for `codex/json-rollup`:

- `git diff --check`: pending for assembled train.
- `make fmt-check`: pending for assembled train.
- `make vet`: pending for assembled train.
- `make check-docs`: pending for assembled train.
- `GOOS=linux make lint`: pending for assembled train.
- `go test ./cmd/gc -run 'TestJSON|Test.*JSON|TestJSONSchema|TestJSONSchemaManifest|TestJSONCommandOutputMatchesDeclaredResultSchema' -count=1`: pending for assembled train.
- `go test ./cmd/gc -count=1`: pending for assembled train.
- `gc4gc` smoke tests: pending for assembled train.

Local-only JSON work state:

- The rollup branch is pushed at `origin/codex/json-rollup`.
- No meaningful rollup changes are local-only yet.
- Existing local worktrees for #2270 and #2291 contain unmerged/rebased state
  with known local test failures; they are excluded from the train until fixed
  or explicitly discarded.

### Blockers / Cross-Workstream Risks

- `yellow`: Registry/gc pack command schemas/tests should not freeze until
  Jasmine confirms the rollup branch has assembled schema-platform plumbing and
  the validation matrix is passing.
- `yellow`: Pack-defined commands may eventually need schema discovery rules;
  flag pack-facing schema changes to Jasmine rather than patching JSON rollout
  branches directly.
- `yellow`: Do not introduce `--format json` or command-specific schema
  discovery conventions in registry work.
- `yellow`: If registry commands need JSON schemas before the rollup lands, use
  `schemas/<command-path>/result.schema.json`, shared failure schema
  compatibility, and `x-gc-jsonl` for JSONL record-count metadata.

### Needed From Other Agents

- Jasmine: assemble and validate `codex/json-rollup`, then open the rollup PR.
- Cleo: flag any registry/gc pack command schema needs before freezing command
  output shapes.

### Last Updated

2026-05-18 12:45 PT by Jasmine

### New Machine Bootstrap

Repos to clone:

- `gastownhall/gascity`
- `gastownhall/gc4gc` or the available local equivalent for smoke testing, if
  needed.

Branches to fetch / checkout:

- `origin/main`
- `origin/codex/workstream-coordination`
- `origin/codex/json-rollup`
- Provenance branches for included PRs:
  - `origin/adopt/ga-nqfs0pd-pr2288`
  - `origin/codex/json-schema-platform`
  - `origin/codex/json-wave2-formula-order`
  - `origin/codex/json-convoy-workflow`
  - `origin/codex/json-rig-agent-routing`
  - `origin/codex/json-mail-events-trace`
  - `origin/codex/json-misc-inspection`
  - `origin/codex/json-runtime-nudge-drain`
  - `origin/codex/json-doctor-diagnostics`
  - `origin/codex/json-lifecycle-city-actions`
  - `origin/codex/graph-converge-order-actions`
  - `origin/codex/json-convoy-mail-actions`
  - `origin/codex/open-schema-passthrough-custom`
  - optional after revalidation: `origin/codex/json-pack-service-skill`

Worktrees to create:

- `/Users/dbox/repos/gc/gascity-workstream-coordination` on
  `codex/workstream-coordination`.
- `/Users/dbox/repos/gc/gascity-json-rollup` on `codex/json-rollup`.

Local-only state:

- None for the rollup branch.
- #2270 and #2291 old-machine worktrees have local/rebased state with known
  failing tests and are intentionally excluded from the first train.

Commands to validate setup:

```sh
git -C /Users/dbox/repos/gc/gascity-workstream-coordination status --short --branch
git -C /Users/dbox/repos/gc/gascity-json-rollup status --short --branch
git -C /Users/dbox/repos/gc/gascity-json-rollup fetch origin --prune
git -C /Users/dbox/repos/gc/gascity-json-rollup log --oneline -1
```

Old-machine worktrees safe to ignore:

- Individual clean JSON shard worktrees after their commits are represented in
  the rollup branch.
- Deleted/gone provenance branches for already-merged JSON PRs.

Old-machine worktrees that must not be deleted yet:

- `/Users/dbox/repos/gc/gascity-json-rollup`
- `/Users/dbox/repos/gc/gascity-workstream-coordination`
- `/Users/dbox/repos/gc/gascity-json-session-mutation-actions` until #2270 is
  fixed or explicitly discarded.
- `/Users/dbox/repos/gc/gascity-json-gnarly-session-order-actions` until #2291
  is fixed or explicitly discarded.
- Any Cleo/Mabel/Penelope pack worktrees they own.

Exact first prompt for Jasmine on a new machine:

> Jasmine, continue the JSON rollup from
> `engdocs/coordination/active-workstreams.md` on
> `origin/codex/workstream-coordination`. Clone/fetch `gastownhall/gascity`,
> create worktrees for `codex/workstream-coordination` and
> `codex/json-rollup`, then assemble the JSON train from the included
> provenance PR branches in the documented order. Do not include #2270 or
> #2291 unless their local failures are fixed. Preserve the accepted `--json`
> / `--json-schema` contract and run the documented validation matrix before
> opening or updating the rollup PR.

## Workstream Handoff

### Workstream

Pack Deprecation

### Current Branch / PR

Branch: `codex/packv2-wave2-goodbye-packv1`

PR: #2126, <https://github.com/gastownhall/gascity/pull/2126>

Base: `main`

Owner: Mabel / relevant implementation agents

### Latest State

#2126 is the source of truth for PackV1/PackV2 deprecation enforcement. It is
green and mergeable as of this update. It should remain conceptually separate
from registry/gc pack implementation.

Related docs/source reconciliation:

- #2318, <https://github.com/gastownhall/gascity/pull/2318>

### Interface Contracts Other Agents Must Honor

- Do not remove or change `gc import migrate` semantics until doctor /
  `doctor --fix` parity exists for the migrate corpus.
- No new `gc pack` replacement command for `gc import migrate`.
- Remediation messaging must remain actionable for hard-failed legacy
  constructs.
- Coordinate before changing legacy `gc pack fetch` or `gc pack list`
  compatibility.

### Blockers / Cross-Workstream Risks

- `red`: Removing `gc import migrate` before doctor parity would break the
  migration contract.
- `yellow`: Registry/gc pack work may touch compatibility messaging around
  `gc import` and legacy `gc pack` commands; coordinate before changing those
  behaviors.
- `green`: Pack deprecation can proceed independently from registry/gc pack as
  long as compatibility invariants are preserved.

### Needed From Other Agents

- Cleo: keep deprecation/remediation changes out of the registry workstream
  unless a compatibility invariant directly affects canonical `gc pack`
  behavior.
- Jasmine: flag if JSON diagnostics or stderr behavior affects deprecation
  warning/error tests.

### Last Updated

2026-05-18 12:10 PT by Mabel

## Workstream Handoff

### Workstream

Registry-gc-pack

### Current Branch / PR

Branch: `codex/pack-registry-workstream` planned by Cleo

PR: not opened yet

Base: latest `origin/main`

Owner: Cleo

Current local implementation worktree noted by Cleo:

- Worktree: `/Users/dbox/repos/gc-pr2119`
- Current branch: `codex/pack-registry-latest-main`
- State: dirty/unpushed; should be treated as Cleo-owned implementation state.

Older local branches are not current:

- `codex/pack-registry-1a-core`
- `codex/pack-registry-mainline`
- `codex/pack-registry-latest-main`

### Latest State

Cleo will maintain one long-lived registry/gc pack workstream branch for
several days rather than preparing small immediate review PRs. Registry
operations still come first inside that workstream.

The registry/gc pack source of truth is Cleo's planned
`codex/pack-registry-workstream` branch once created.

### Interface Contracts Other Agents Must Honor

- Registry operations land first.
- Dependency mutation must not race ahead of registry config/catalog
  correctness.
- Preserve current PackV2 import fields: `source`, `version`, `export`,
  `transitive`, `shadow`.
- Do not implement #2129 `[[exports]]` in this workstream; treat it as design
  input/future direction.
- Registry handles such as `main:lighthouse` are command-time selectors only.
- Durable `pack.toml` imports must store concrete `source` plus optional
  `version`, not `registry:<registry>:<pack>`.
- Lock/cache internals may preserve registry/ref/commit/hash metadata.
- Preserve `gc import` compatibility and legacy `gc pack fetch/list`
  compatibility.
- `gc import migrate` has no `gc pack` replacement; doctor / `doctor --fix`
  must reach parity before removal.
- Compose with Jasmine's JSON rollup conventions once stable.

### Blockers / Cross-Workstream Risks

- `red`: Do not base registry command JSON/schema tests on an unstable or
  superseded JSON branch without Jasmine confirmation.
- `red`: Do not change `gc import migrate` removal semantics in registry work.
- `yellow`: Coordinate with Pack Deprecation before changing legacy `gc pack`
  `fetch/list` behavior.
- `yellow`: Coordinate with Jasmine before freezing registry command JSON
  schemas or failure behavior.
- `green`: Registry/gc pack overlap with Pack Deprecation is small and should
  be managed through compatibility checkpoints, not branch merging.

### Needed From Other Agents

- Jasmine: confirm JSON rollup branch and schema/failure conventions.
- Mabel: keep Pack Deprecation source-of-truth visible and flag compatibility
  drift.
- Cleo: publish the long-lived branch name once created and summarize changed
  file ownership boundaries.

### Last Updated

2026-05-18 12:10 PT by Mabel

## Workstream Handoff

### Workstream

Pack Reuse / Customization Design

### Current Branch / PR

Branch: managed by Penelope on another machine

PR: feeds into #2119 / #2129 as appropriate

Base: not tracked in this coordination file

Owner: Penelope

### Latest State

Penelope is continuing the user-facing pack reuse/customization guide and
design exploration on a separate machine. Do not migrate or interrupt that
context from this coordination branch.

### Interface Contracts Other Agents Must Honor

- Treat #2129 `[[exports]]` as future design input, not as implemented registry
  behavior.
- Keep user-facing guide language aligned with actual implementation state.

### Blockers / Cross-Workstream Risks

- `yellow`: Reuse/customization guide may update terminology or examples used
  by #2119 and future registry docs.

### Needed From Other Agents

- Penelope: surface guide decisions that change registry/gc pack CLI wording or
  import/export semantics.

### Last Updated

2026-05-18 12:10 PT by Mabel
