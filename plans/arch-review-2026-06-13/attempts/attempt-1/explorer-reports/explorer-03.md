# Architecture Explorer Report: explorer-03

**Status:** completed
**Primary scope:** .
**Focus:** Test-surface friction, locality gaps, and hidden behavior behind callers
**Evidence followed outside scope:** yes

## Context Read First
- `CONTEXT.md` (absent)
- `docs/adr` / `engdocs/design` (read [session-lifecycle-domain-cleanup-plan.md](file:///data/projects/gascity/engdocs/design/session-lifecycle-domain-cleanup-plan.md) first to understand the context of domain boundaries, hardening, and compatibility shims)
- `README.md`
- `AGENTS.md` (read key design principles like "ZERO hardcoded roles", "Bitter Lesson", and specifically the convention rule for "Adding agent config fields")

## Candidates

### CANDIDATE: Unified Agent Configuration Mutation and Copying
- Recommendation strength: Strong
- Dependency category: in-process
- Files: [internal/config/config.go](file:///data/projects/gascity/internal/config/config.go), [internal/config/patch.go](file:///data/projects/gascity/internal/config/patch.go), [internal/config/pack.go](file:///data/projects/gascity/internal/config/pack.go), [cmd/gc/pool.go](file:///data/projects/gascity/cmd/gc/pool.go), [internal/config/field_sync_test.go](file:///data/projects/gascity/internal/config/field_sync_test.go)
- Problem: The config module exhibits a shallow interface with high test-surface friction and a severe locality gap, forcing maintainers to manually synchronize any newly added Agent fields across three separate structs and four duplicate mapper/copier implementations.
- Solution: Transition from multiple hand-written mapping functions and synchronized structs to a unified, reflection-based or field-map-backed merge seam that can automatically patch, override, and deep-copy Agent configuration fields.
- Benefits:
  - locality: Maintainers only update the core Agent struct, with all other modifications and copies automatically inheriting the schema changes.
  - leverage: Callers gain deep, automated leverage from a single unified mutation and copying interface without needing to understand or coordinate multiple manual replication mechanics.
  - tests: The complex reflection-based field-sync tests can be deleted, drastically reducing test-surface friction since the unified seam guarantees consistency at compile-time or by design.
- Before diagram notes: Show the current system where Agent is the hub, but it is surrounded by three separate modification adapters (AgentPatch, AgentOverride, deepCopyAgent), each manually copying individual fields, and an outer test layer (TestAgentFieldSync) attempting to police this locality gap through reflection.
- After diagram notes: Show the Agent struct sitting behind a deep, unified Modifier seam. Patches, overrides, and deep-copies act as adapters or inputs to this unified modifier, with the compiler or generic mapping logic handling field synchronization automatically.
- ADR conflict: none
- Evidence:
  - config.Agent struct fields: [internal/config/config.go:L2200-2260](file:///data/projects/gascity/internal/config/config.go#L2200-L2260) (contains the primary schemas)
  - applyAgentPatchFields: [internal/config/patch.go:L307-450](file:///data/projects/gascity/internal/config/patch.go#L307-L450) (hand-written synchronization for patching)
  - applyAgentOverride: [internal/config/pack.go:L2581-2680](file:///data/projects/gascity/internal/config/pack.go#L2581-L2680) (hand-written synchronization for overriding)
  - deepCopyAgent: [cmd/gc/pool.go:L220-317](file:///data/projects/gascity/cmd/gc/pool.go#L220-L317) (hand-written synchronization for deep copying pool instances)
  - TestAgentFieldSync: [internal/config/field_sync_test.go:L17-130](file:///data/projects/gascity/internal/config/field_sync_test.go#L17-L130) (high-friction reflection-based validation code)
  - AGENTS.md guidelines: [AGENTS.md](file:///data/projects/gascity/AGENTS.md) (explicitly calling out this locality gap as a manual checklist task for developers under "Adding agent config fields")
- Grill prompts:
  - "If we replaced the explicit AgentPatch and AgentOverride structs with a generic, type-safe schema map (or generated the modifiers from city.toml schemas), what would be the trade-offs in startup performance and developer experience?"
  - "Why do we treat patching and overriding as distinct operations with completely separate structures and code flows if their target is the exact same set of Agent fields?"

## Non-Candidate Observations
- Reviewed [internal/worker/handle.go](file:///data/projects/gascity/internal/worker/handle.go) (the worker boundary seam). The interface is exceptionally deep, packaging multiple complex sub-capabilities (lifecycle, messaging, transcripts, interaction, live observation) behind a consolidated Handle interface. It provides massive leverage to callers (the API server and CLI) by hiding the complex session-management and tmux/exec provider details.
- Investigated [internal/session/lifecycle_projection.go](file:///data/projects/gascity/internal/session/lifecycle_projection.go). The projection and transition layers successfully centralized state transitions into pure, testable metadata patches, demonstrating great depth and high test-surface locality as outlined in the "Session Lifecycle Domain Cleanup Plan".
