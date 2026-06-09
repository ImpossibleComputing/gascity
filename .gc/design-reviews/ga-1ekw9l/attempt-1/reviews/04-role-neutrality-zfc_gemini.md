# Anand Krishnaswamy — Gemini (Role Neutrality & ZFC Invariant Reviewer, Attempt 8, Independent DeepSeek V4 Flash Style)

**Verdict:** block

> **Lane:** Zero hardcoded roles in Go and assets, the symbolic maintenance-worker binding, SDK self-sufficiency, ZFC (Zero Framework Cognition) judgment containment.
>
> Reviewed against the Attempt 8 design document (`.gc/design-reviews/ga-1ekw9l/attempt-8/design-before.md`, 835 lines, `updated_at: 2026-06-09T13:20:59Z`) — specifically §"Role Neutrality And Configurable Bindings" (lines 409–458), §"Required System Pack Loader" (lines 221–312), and §"Data And State" (lines 532–603).
>
> This independent review is produced using the DeepSeek V4 Flash style, focusing specifically on cross-document consistency, missing edge cases, and assumptions other reviewers may accept too quickly.

---

## Executive Summary

As Anand Krishnaswamy, the **Role Neutrality & ZFC Invariant Reviewer**, I have conducted an independent, evidence-backed, and deeply analytical review of the Attempt 8 design for the Core and Gastown Pack Split. My verdict remains **Verdict: block**.

While Attempt 8 introduces essential, rigorous structures—such as the AST-aware static linter, config graph participation verifications, and a detailed rollout gate matrix—the core role neutrality design itself remains unchanged from previous iterations. It fails to address several **critical compile-bound role leakage vectors, out-of-the-box scaffolding violations, and unneutralized Go runtime components**. Other reviewers have approved these changes too quickly, focusing on the impressive rollout gates while ignoring active role leakage points still embedded in the core SDK Go code today.

To achieve true role neutrality, we must eliminate all compile-time role bindings and ensure the SDK remains a pure, de-roled transport.

---

## Detailed Responses to Lane-Specific Questions

### Q1: After binding indirection, does any Go, prompt asset, script, formula, order, generated help, or API route still branch on dog, Mayor, Maintenance, or another concrete role name?

**Answer: Yes.** While the Attempt 8 plan establishes symbolic mappings (`[gc.bindings.*]`, `[system_packs.*.bindings]`, and `target_binding`), several critical role-bias points remain unaddressed in the core SDK:

1. **Default Scaffolding Role Leakage**:
   In `internal/config/config.go`, the out-of-the-box non-Gastown SDK defaults `DefaultCity` (lines 3668–3674) and `WizardCity` (lines 3689–3704) explicitly hardcode:
   ```go
   Agents: []Agent{{Name: "mayor", PromptTemplate: "prompts/mayor.md"}},
   NamedSessions: []NamedSession{{Template: "mayor", Mode: "always"}},
   ```
   This is a compile-bound, literal reference to the `"mayor"` role and `"prompts/mayor.md"` template, directly violating the core directive that "the SDK has no built-in Mayor." Since this scaffolding runs by default during `gc init`, any fresh city will initialize with a hardcoded Gastown role. The design lists "default scaffolding" under manifest coverage (line 414) but fails to define a neutral end-state.
2. **Warmup Mail Fallback**:
   In `cmd/gc/cmd_start_warmup.go:33`, `defaultWarmupMailTo = "mayor"` is applied at line 195. Warmup runs under `gc start` (a core SDK/controller command). If a city has a renamed or omitted maintenance worker, running `gc start` will try to send warmup failure mail to a non-existent `"mayor"`. The design states that "No Go fallback may substitute `mayor`, `deacon`, `dog`, or another concrete role name" (lines 445-446) but fails to define a warmup-recipient binding, leaving this hardcoded fallback active.
3. **Compile-bound Tmux Heuristics**:
   `internal/runtime/tmux/theme.go` defines `MayorTheme()`, `DeaconTheme()`, and `DogTheme()`. Furthermore, `internal/runtime/tmux/tmux.go:80` defines `roleEmoji` map keyed on `"mayor"`, `"deacon"`, `"witness"`, `"refinery"`, `"crew"`, `"polecat"`, `"coordinator"`, `"health-check"`.
   These are Layer-0/1 Go code in the runtime layer that cannot move to a config pack. The design states tmux theme APIs are "assigned by manifest rows before source moves" (line 452), but Go functions cannot be relocated to config packs. The theme and emoji mapping must be fully de-roled.
4. **The Suffix Binding Gap**:
   Prompt templates and assets resolve prefixes using `binding_prefix` but leave role name suffixes (e.g., `dog`, `deacon`, `witness`) hardcoded in the templates. The proposed plan introduces symbolic bindings but does not reconcile them with `binding_prefix` or explain how template files (e.g., `prompts/mayor.md`) are resolved without hardcoded suffixes.
5. **Dolt and BD Pack Mail/Nudge Routes**:
   Required system-installed provider packs `dolt` and `bd` contain shell scripts/formulas that hardcode routes like `gc mail send mayor/` or `gc session nudge deacon/`. Under a non-Gastown city where these roles do not exist, they will fail.

---

### Q2: Can controller-owned SDK operations still run when the configured maintenance worker is renamed or omitted, with no dependency on a user agent entry?

**Answer: Yes, but with unresolved ZFC and declaration gaps.**

- If the `maintenance_worker` is renamed (e.g., from `dog` to `reconciler`), the framework resolves the target at runtime via `gc.run_target_binding` / `target_binding`, which is robust.
- However, if the `maintenance_worker` is omitted entirely from the config, the plan's behavior is unstated. Line 443 states: `"Missing optional bindings skip user-agent work with a typed diagnostic."` Under **ZFC**, the Go code must not make a judgment call about omitting required system-level transport workers; the config parser must fail-closed during pre-flight configuration validation or raise a descriptive pre-flight error rather than letting the dispatcher make an ad-hoc runtime judgment.
- Furthermore, the plan distinguishes between "missing optional bindings" and "missing required provider-pack escalation bindings" (lines 443-444), but never specifies where this optional-vs-required designation is declared. If Go classifies a binding as required or optional by its name or purpose, that is the judgment call ZFC forbids.

---

### Q3: Are role-name allowlists narrow, time-bounded, and failing when compatibility fixtures leak into live behavior?

**Answer: No.**

1. **The Scanner ignores `dog`**: The plan's proposed list of denied tokens (`mayor, deacon, witness, refinery, polecat, boot, crew, gastown`) completely omits `dog`. While `dog` is allowed in the Core default pack config, omitting it from the denied set means developers can silently hardcode `dog` in Go source code or script bodies without triggering a build failure.
2. **Missing Expiry Enforcement**: While the plan mentions that allowlist rows require an `expiry` date (line 594), it specifies no CI enforcement gate that fails the build when a row is past its expiry date. Without this, allowlists will grow indefinitely.

---

## Critical Risks & Architectural Inconsistencies (DeepSeek V4 Flash Style)

### 1. [Blocker] Scaffolding Role Leakage in `gc init` Defaults
- **The Risk:** `internal/config/config.go`'s `DefaultCity` and `WizardCity` functions hardcode the inline agent `"mayor"` and its template `"prompts/mayor.md"`. This is the out-of-the-box non-Gastown SDK default.
- **The Impact:** Running `gc init` creates a city that depends on a literal `"mayor"` role and template. This violates the zero-hardcoded-roles SDK guarantee. Since AC8 scopes role neutrality to "init/template resolution," the scanner will flag this unless it is allowlisted—and allowlisting a live SDK default defeats the entire migration.
- **Resolution:** Explicitly specify the role-neutral scaffolding for `gc init`. The default city should scaffold zero agents and zero named sessions, or alternatively use symbolic-binding equivalents. Add an AC8 check proving the non-Gastown default path contains no role literals.

### 2. [Blocker] Un-de-roled Warmup Mail Fallback
- **The Risk:** `cmd/gc/cmd_start_warmup.go:33` hardcodes `defaultWarmupMailTo = "mayor"`. This is core CLI command logic.
- **The Impact:** When running `gc start` on a city with a renamed or omitted maintenance worker, warmup failure mail will attempt to send to a non-existent `"mayor"`. This directly violates the rule "No Go fallback may substitute `mayor`" (line 445).
- **Resolution:** Define how the warmup mail recipient resolves with no Go fallback. It must resolve via a symbolic binding (e.g., `escalation_recipient`), or skip with a typed diagnostic when unbound. Name `cmd/gc/cmd_start_warmup.go` and its Core end-state in the design.

### 3. [Blocker] Compile-bound Tmux Theme and Emoji Maps
- **The Risk:** `internal/runtime/tmux/theme.go` defines `MayorTheme/DeaconTheme/DogTheme`, and `tmux.go` maps emoji icons to literal role keys like `"mayor"`, `"deacon"`, and `"witness"`.
- **The Impact:** Go functions and map keys are compile-bound in the runtime packages; they cannot be moved to configuration packs.
- **Resolution:** Fully de-role `internal/runtime/tmux`. Drive tmux status themes dynamically from config/pack bindings or a consistent hash fallback (`AssignTheme(agentName)`). Rename `ConfigureGasTownSession` to a neutral name.

### 4. [Major] Suffix Binding Ignored: The `BindingPrefix` Blocker
- **The Risk:** Prompt templates currently resolve prefixes using `binding_prefix` but leave role name suffixes (e.g., `dog`, `deacon`, `witness`) hardcoded in the templates (e.g., `{{ .BindingPrefix }}dog`).
- **The Impact:** The new symbolic binding system is not reconciled with how suffix-level prompt templates are resolved, which will result in code that still hardcodes literal suffixes.
- **Resolution:** Require that all role suffixes in prompt templates are replaced by symbolic bindings, deprecating prefix-only `binding_prefix` in favor of fully symbolic config-driven bindings.

### 5. [Major] Scanner Excludes `dog` and Lacks Expiry Failures
- **The Risk:** The scanner's denied token set (line 426-427) excludes `"dog"`.
- **The Impact:** Developers can silently hardcode `"dog"` in Go files or scripts without failing CI.
- **Resolution:** Add `"dog"`, `"coordinator"`, and `"health-check"` to the scanned denied token set, allowing `"dog"` *only* in the designated Core default pack configuration file and its associated tests. Enforce that any allowlist row with an expired `expiry` date fails the build in CI.

---

## Evaluation against Lane Anti-patterns

| Anti-pattern / Risk | Mitigation in Attempt 8 Design | Status |
| :--- | :--- | :--- |
| **`gc.routed_to`, mail, nudge, warmup, or theme logic still hardcodes `dog` or Gastown roles** | **Vulnerable.** Scaffolding, warmup mail, and tmux theme constants still hardcode roles in production Go code. Suffixes remain hardcoded in templates behind `binding_prefix`. | **Fail** |
| **Default binding behavior encodes a Go judgment call instead of pure transport** | **Excellent.** Core SDK operations are decoupled from agent configuration, but required-vs-optional binding metadata must be declared in TOML. | **Pass-with-Risks** |
| **Scanner coverage excludes scripts, overlays, docs, dashboard types, or generated fixtures** | **Good.** The proposed manifest covers Go, TOML, shell, markdown, templates, OpenAPI, and tmux helpers. But must explicitly enforce suffix de-roling. | **Pass-with-Risks** |

---

## Required Changes

Before the design can transition to implementation, the following changes must be incorporated into the proposed implementation plan:

1. **De-role Default Scaffolding (`gc init`):** Declare the role-neutral default for `DefaultCity` and `WizardCity`. They must emit a city with no agents/named sessions, or use symbolic-binding equivalents. Add an AC8 gate proving the non-Gastown default path contains no role literals.
2. **De-role Warmup Mail:** Define how the warmup mail recipient resolves with no Go role fallback. Name `cmd/gc/cmd_start_warmup.go` and its Core end-state.
3. **De-role Tmux Themes & Icons:** Deprecate `MayorTheme()`, `DeaconTheme()`, and `DogTheme()` in `internal/runtime/tmux/theme.go`. Drive status themes and emoji maps dynamically from config/pack bindings. Rename `ConfigureGasTownSession`.
4. **Suffix-Level Symbolic Bindings:** Require that all role suffixes in prompt templates are replaced by symbolic bindings, deprecating prefix-only `binding_prefix` in favor of config-driven bindings.
5. **De-role Required Provider Packs (`dolt`):** Map all hardcoded `mayor`/`deacon` mail/nudge escalation routes inside `examples/dolt` to configurable symbolic recipients, and fail CI on any hardcoded literal role route in a required provider pack.
6. **Specify Required-vs-Optional Semantics as Declarative Data:** Ensure that whether a binding is optional or required is declared as metadata within the formula/order/pack config itself rather than checking hardcoded names in Go.
7. **Add `dog` to Denied Set and Enforce Expiry:** Add `dog`, `coordinator`, and `health-check` to the active behavior denied token list (with narrow allowlists for Core defaults, Dolt/store-maintenance terms, and tests). Enforce that any allowlist row with an expired `expiry` date fails the build in CI.

---

## Questions

1. Is `gc start` warmup Core SDK infrastructure or Gastown-owned? If Core, the recipient must be neutral/bound; if Gastown, the warmup-mail default belongs in the public pack entirely.
2. For Core-retained surfaces the manifest assigns "to Core," is the only de-roling primitive the maintenance-worker binding, or will the plan provide a general symbolic binding table (default-city agents, warmup recipient, tmux theme/icon)?
3. Does the CI scanner enforce a strict build failure if any allowlist row has expired?
