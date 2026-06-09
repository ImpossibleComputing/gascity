# Anand Krishnaswamy — Gemini (Role Neutrality & ZFC Invariant Reviewer, Attempt 4, Independent DeepSeek V4 Flash Style)

**Verdict:** block

> **Lane:** zero hardcoded roles in Go and assets, the symbolic maintenance-worker binding, SDK self-sufficiency, ZFC (Zero Framework Cognition) judgment containment.
>
> Reviewed against the Attempt 4 design document (`.gc/design-reviews/ga-1ekw9l/attempt-4/design-before.md`, 657 lines, `updated_at: 2026-06-09T07:28:00Z`) — §"Role Neutrality And Configurable Bindings" (328–369), §"Required System Pack Loader" (193-251), §"Bootstrap Fixture Isolation" (370-396), and §"Data And State" (426–486).
>
> This independent review is produced using the DeepSeek V4 Flash style, focusing specifically on cross-document consistency, missing edge cases, and assumptions other reviewers may accept too quickly.

---

## Executive Summary

As Anand Krishnaswamy, the **Role Neutrality & ZFC Invariant Reviewer**, I have conducted an independent, evidence-backed, and deeply analytical review of the Attempt 4 design for the Core and Gastown Pack Split. My verdict is **Verdict: block**.

This iteration (Attempt 4) introduces necessary clarifications, such as declarative formula-level branch metadata in TOML, explicit binding precedence rules, and a role-surface manifest. However, several **critical, compile-bound role leakage vectors, suffix-level binding holes, and unstated runtime assumptions** remain unresolved in the plan. Other reviewers have accepted the scope and completeness of this migration too quickly. 

Specifically, the plan invents a new symbolic binding system while ignoring the prefix-only `binding_prefix` mechanism currently in the code, which leaves all role suffixes hardcoded as literals in prompt templates. Furthermore, the plan fails to name key Go-side ownership boundaries (such as `internal/runtime/tmux/theme.go` and `internal/dispatch/fanout.go`), excludes `dog` from the scanned denied set, and lacks an enforcement gate for expired allowlist rows. We must address these vulnerabilities before approving the transition to implementation.

---

## Detailed Responses to Lane-Specific Questions

### Q1: After binding indirection, does any Go, prompt asset, script, formula, order, generated help, or API route still branch on dog, Mayor, Maintenance, or another concrete role name?

**Answer: Yes.** While the Attempt 4 plan introduces `[gc.bindings.*]` and `[system_packs.*.bindings]` to resolve symbolic targets, it leaves several critical role-bias points compile-bound or unneutralized in the asset space:
1. **The `BindingPrefix` / `binding_prefix` Paradox**: The live codebase uses `Agent.BindingPrefix()` (`internal/config/config.go:91-93`), `binding_prefix` (`internal/sling/sling.go:1003`), and template variables (e.g., `{{ .BindingPrefix }}dog` in witness/boot/deacon prompts and `{{binding_prefix}}dog` in formulas). Under the current plan, `binding_prefix` is completely ignored (mentioned zero times). If the plan is decomposed as-is, the role *suffix* (`dog`, `deacon`, etc.) remains a hardcoded literal in the prompt template or formula asset (e.g., `{{ .BindingPrefix }}dog`), meaning the assets still branch on concrete role names. Suffixes must also become fully symbolic bindings.
2. **Compile-bound Tmux Theme Heuristics**: `internal/runtime/tmux/theme.go:34-47` still returns hardcoded theme styles for `"mayor"`, `"deacon"`, and `"dog"`. This represents compilation-level role bias in shared SDK Go code.
3. **Dolt Pack Mail/Nudge Routes**: Required provider pack `dolt` (`examples/dolt`) still hardcodes nudge/mail targets like `gc mail send mayor/` and `gc session nudge deacon/` inside its shell scripts and formulas, which will fail at runtime in any non-Gastown city where these roles do not exist.
4. **API Types & Examples**: `internal/api` still contains OpenAPI types referencing hardcoded example role names like `"deacon-1"`, which leak into generated TS types and dashboard schemas.

### Q2: Can controller-owned SDK operations still run when the configured maintenance worker is renamed or omitted, with no dependency on a user agent entry?

**Answer: Yes, but with unmitigated edge-case risks.**
- If the `maintenance_worker` is renamed (e.g., from `dog` to `reconciler`), the framework resolves the target at runtime via `gc.run_target_binding` / `target_binding`, which works perfectly.
- However, if the `maintenance_worker` is omitted entirely from the config, the plan's behavior is unstated. Line 354 states: `"Missing optional bindings skip user-agent work with a typed diagnostic."` But under **ZFC**, the Go code must not make a judgment call about omitting required system-level transport workers; the config parser must fail-closed during pre-flight configuration validation or raise a descriptive pre-flight error rather than letting the dispatcher make an ad-hoc runtime judgment.

### Q3: Are role-name allowlists narrow, time-bounded, and failing when compatibility fixtures leak into live behavior?

**Answer: No.**
1. **The Scanner ignores `dog`**: The plan's proposed list of denied tokens (`mayor, deacon, witness, refinery, polecat, boot, crew, gastown`) completely omits `dog`. While `dog` is allowed in the Core default pack config, omitting it from the denied set means developers can silently hardcode `dog` in Go source code or script bodies without triggering a build failure.
2. **Missing Expiry Enforcement**: While the plan mentions that allowlist rows require an `expiry` date, it specifies no CI enforcement gate that fails the build when a row is past its expiry date. Without this, allowlists will grow indefinitely.

---

## Critical Risks & Architectural Inconsistencies (DeepSeek V4 Flash Style)

### 1. [Blocker] Suffix Binding Ignored: The `BindingPrefix` Blocker
- **The Risk:** The plan introduces a net-new binding system but completely ignores the existing `BindingPrefix` / `binding_prefix` routing variable. Today, assets are structured as `{{ .BindingPrefix }}dog` or `{{binding_prefix}}dog`.
- **The Impact:** Because `binding_prefix` only namespaces the *import prefix*, the role name *suffix* (e.g., `dog`, `deacon`, `witness`) remains a hardcoded literal in the prompt template or formula asset. The new `gc.run_target_binding` / symbolic bindings are never reconciled with how these template variables resolve. Without explicit design stating that (a) the role *suffix* also becomes symbolic, and (b) how the new bindings map to the existing `BindingPrefix` resolution, the decomposer will ship code that still hardcodes the literal suffixes, failing the core goal of the de-roling migration.
- **Resolution:** Explicitly specify that all role-name suffixes in prompt templates and assets are replaced by symbolic bindings (e.g., `{{ .Bindings.maintenance_worker }}` or a unified config-driven binding map). Clarify the resolution precedence and deprecate the prefix-only `binding_prefix` mechanism in favor of fully symbolic bindings.

### 2. [Blocker] Un-de-roled Go: Tmux Theme Constants
- **The Risk:** `internal/runtime/tmux/theme.go:34,40,46` contains hardcoded functions (`MayorTheme()`, `DeaconTheme()`, `DogTheme()`) returning styles for literal Gastown roles. This is core SDK Go code that cannot move to a pack.
- **The Impact:** If a city runs with a renamed or omitted maintenance worker, it loses warm tmux visual branding because the theme is hardcoded to the string `"dog"`. This is a compile-time role bias.
- **Resolution:** Explicitly de-role `theme.go`. Theme styles must become pack or config data keyed by the symbolic binding (or a generic aesthetic token like `"earthy"`, `"warm"`), with a consistent hash fallback (`AssignTheme(agentName)`) for any unbound agent. Name `internal/runtime/tmux/theme.go` and `theme_test.go` as forbidden-removal / de-roling sites.

### 3. [Blocker] Unnamed Go Ownership & Undefined Empty Binding Behavior (ZFC Violation)
- **The Risk:** The parser and resolver support for `[gc.bindings.*]` is described in prose, but no owning packages or files are named (e.g., `internal/config`, `internal/dispatch/fanout.go`). Furthermore, the behavior of an empty or unresolved optional binding is left to runtime judgment.
- **The Impact:** If the Go dispatcher makes a judgment call to skip or proceed under an empty binding, it violates the **Zero Framework Cognition (ZFC)** principle. The resolution must be structurally handled at the edges.
- **Resolution:** Specify the Go packages that own parsing (`internal/config`) and resolution (`internal/dispatch/fanout.go`). Define the empty-binding contract clearly: the bead remains in `open` or `unassigned` state in the task store, and a clear diagnostic event is appended to the event bus; no Go-side role substitution or ad-hoc skipping occurs.

### 4. [Major] Dolt Pack Escalation Targets Missing from Scope
- **The Risk:** The required provider pack `dolt` (`examples/dolt`) is registered in `builtinpacks/registry.go` and is essential for databases. However, its shell scripts and formulas hardcode nudge/mail targets like `gc mail send mayor/` and `gc session nudge deacon/`.
- **The Impact:** In a non-Gastown city running the dolt provider, these escalation targets will fail to resolve because the roles do not exist.
- **Resolution:** Bring all required provider packs (`dolt`, `bd`) into the strict de-roling scope. Require that `dolt`'s escalation targets are rebound using symbolic bindings (e.g., `escalation_recipient = "core.maintenance_worker"` or config-mapped keys), and add a CI positive control proving that any hardcoded literal role route (`mayor/` or `deacon/`) in a required provider pack fails the build.

### 5. [Major] Scanner Excludes `dog` and Lacks Expiry Failures
- **The Risk:** The scanner's denied token set (337-338) excludes `dog`.
- **The Impact:** Go source files, shell scripts, and templates can still hardcode the literal string `"dog"` for routing, prompting, or logic without failing CI.
- **Resolution:** Add `dog` to the scanned denied token set, allowing it *only* in the designated Core default pack configuration file and its associated tests. Additionally, add a CI enforcement check: any allowlist row whose `expiry` date is in the past must fail the build.

---

## Evaluation against Lane Anti-patterns

| Anti-pattern / Risk | Mitigation in Attempt 4 Design | Status |
| :--- | :--- | :--- |
| **`gc.routed_to`, mail, nudge, warmup, or theme logic still hardcodes `dog` or Gastown roles** | **Vulnerable.** `dog` is not in the denied tokens list. Tmux themes still hardcode role styles. Dolt scripts still hardcode `mayor`/`deacon` routes. Prompt templates still hardcode role suffixes behind `binding_prefix`. | **Fail** |
| **Default binding behavior encodes a Go judgment call instead of pure transport** | **Excellent.** Core SDK operations (health, dispatch) are decoupled from agent configuration. However, empty/unresolved binding behavior must be structurally fail-closed. | **Pass-with-Risks** |
| **Scanner coverage excludes scripts, overlays, docs, dashboard types, or generated fixtures** | **Good.** The proposed manifest covers Go, TOML, shell, markdown, templates, OpenAPI, and tmux helpers. But must explicitly enforce suffix de-roling. | **Pass-with-Risks** |

---

## Required Changes

Before the design can transition to implementation, the following changes must be incorporated into the proposed implementation plan:

1. **Suffix-Level Symbolic Bindings:** Explicitly require that all role-name suffixes in prompt templates and formula assets are replaced by symbolic bindings, deprecating the prefix-only `binding_prefix` mechanism in favor of fully symbolic config-driven bindings.
2. **De-role Tmux Themes:** Deprecate `MayorTheme()`, `DeaconTheme()`, and `DogTheme()` in `internal/runtime/tmux/theme.go`. Drive status themes dynamically from config/pack keys or a consistent hash fallback (`AssignTheme(agentName)`).
3. **Explicit Go Ownership & Empty Binding Contract:** Identify `internal/config` and `internal/dispatch/fanout.go` as the code boundaries for parsing and resolving bindings. Define the empty-binding contract to keep operations pure transport (the bead remains visible/diagnosable, and an event is emitted).
4. **De-role Required Provider Packs (`dolt`):** Map all hardcoded `mayor`/`deacon` mail/nudge escalation routes inside `examples/dolt` to configurable symbolic recipients, and fail CI on any hardcoded literal role route in a required provider pack.
5. **Add `dog` to Denied Set and Enforce Expiry:** Add `dog` to the denied token list (with narrow allowlists for Core defaults and tests). Enforce that any allowlist row with an expired `expiry` date fails the build in CI.
6. **Sling Heuristics Cleanup:** Specify the exact replacement for hardcoded Gastown prefix heuristics in `internal/sling/sling.go`: formulas declare branch-variable use in TOML (`uses_base_branch`/`uses_target_branch`), and Sling reads these fields from the parsed formula object, removing all compile-time Gastown string matching.

---

## Questions

1. How does the plan reconcile the existing `binding_prefix` prefix-only system with the new symbolic binding system to avoid hardcoded suffixes in prompt templates?
2. Are all required provider packs (`bd`, `dolt`) and shared Go utilities (like `internal/runtime/tmux/theme.go`) brought into the de-roling scope, and what is the exact configuration mechanism to map their escalation routes?
3. Does the CI scanner enforce a strict build failure if any allowlist row has expired?
