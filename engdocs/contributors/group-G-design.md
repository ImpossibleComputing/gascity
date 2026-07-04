# Group G design — graph-aware molecule reads in `pr_merge.py`

Audit findings **#62, #63 (HIGH); #61, #64, #65 (MEDIUM)** from
`engdocs/contributors/graph-store-split-audit.md` (§ "Group G", lines 77–83).
Target repo: **`/data/projects/workflows`** (shared `gastownhall/workflows` —
do NOT push without owner sign-off; see §9).
Target file: `/data/projects/workflows/scripts/pr_merge.py` (2680 lines,
verified at branch `fix/adopt-pr-root-id-from-convoy` @ `922fb57f7`, clean tree).

Status: **design only — no production code written.** Line numbers below are
verified against the current working tree, not the audit's approximations.

---

## 1. Doctrine note (why Group G is doctrine-independent)

Under `graph_store=sqlite`, formula (ClassGraph) beads — molecule ROOT and
STEP beads for adopt-PR / pr-review workflows — live in the **city-scope
SQLite graph store** with ID prefix `gcg-`
(`cmd/gc/api_state.go:274 graphStoreIDPrefix = "gcg"`, one global `gcg-N`
sequence for all scopes), behind the controller's `coordrouter.Router`.

`gc --rig <rig> bd list/show` is a **pure passthrough** to the rig's Dolt
work store — verified: `cmd/gc/cmd_bd.go:189-354 doBd()` resolves a scope
directory and `exec.Command(bdPath, bdArgs...)` with zero routing. It can
never see a `gcg-` bead. That is the whole bug: four `pr_merge.py` readers
query molecule subtrees through this passthrough, get **empty forever**, and
the recovery gates degrade into permanent skip codes with a green patrol
summary (`review_not_done` / `approval_gates_not_done` are members of
`AUTO_APPROVED_RECOVERY_SKIP_CODES`, pr_merge.py:53-58). Live consequence:
auto-approved merge-ready PRs silently skipped every 5-minute
`pr-merge-ready-patrol` tick (`orders/scripts/pr-merge-ready-patrol.sh` runs
`pr_merge.py sweep-merge-ready --fail-on-error`).

This fix is **required by** the graph-store doctrine, not obviated by it:
even in a future where formula beads are *always* graph-resident, a reader
must reach whichever store holds the bead. The graph route
(`beadStoresForID`, `internal/api/handler_beads.go:159-180`) resolves the
store by ID prefix and serves **both** graph-resident (`gcg-`) and
rig-resident roots, so routing reads through it is correct in the identity
phase and the split phase alike. Conversely, the rig-bd fallback for
non-`gcg-` roots is kept byte-identical so nothing changes where the old
path was already correct.

## 2. Verified bug sites (current line numbers)

| # | Symbol | Lines | Blind read |
|---|--------|-------|-----------|
| #62 | `review_loop_done` | 726–744 | `bounded_rig_bd_list(city, rig, "--all", "--metadata-field", f"gc.root_bead_id={root_id}", "--limit", "0")` at :729–737 |
| #63 | `recovery_approval_gates_done` | 747–774 | same shape at :753–761 |
| #61 | `recover_source_from_finalizer` | 301–354 | `bounded_rig_bd_list(..., f"pr_review.final_pr_number={final_pr_number}", ...)` at :313–321 — finalize STEP beads are `gcg-` residents |
| #64 | `cleanup_superseded_review_workflow` | 1230–1271 | root show via `pr_review.rig_bd_show_optional` at :1252 (returns `None` for `gcg-` roots → root never stamped/closed) + `workflow_id={root_id}` list at :1254–1262 (only finds rig-resident rig-launch beads — molecule STEP beads are never closed → open ready-frontier steps inflate run-operator pool demand) |
| #65 | `MERGE_READY_HANDOFF_SKIP_CODES` | 64–67 | `source_bead_not_found` conflates true orphaned labels with graph-invisibility; classification at :1360 |

Sites verified **out of scope** (rig-resident data, reads are correct):
`open_merge_beads` :249 (pr_merge beads, created via `rig bd create` :534),
`recovery_comment_metadata` :788 (rig-launch bead hydration),
`merged_launch_cleanup_candidates` :1835, `_merge_beads_for_rig` :1930.

Note on the audit's finding-#65 name: **`source_confirmation_failed` does not
exist anywhere in the workflows repo** (grep-verified). The real code in the
skip set is `source_bead_not_found`, raised at
`find_merge_ready_source` :298. The split in §7 applies to that code.

## 3. Read mechanism — validated refinement of the audit prescription

The audit prescribes a direct HTTP `GET /v0/city/<city>/beads/graph/<rootID>`.
Investigation found a strictly better transport to the **same route**:

**`gc bd-shim mol current <root_id> --json`** (shell-out, `cwd=city`).

Evidence chain (all in `/data/projects/gascity/.claude/worktrees/beads`):

- `cmd/gc/cmd_bd_shim.go:1128-1138` — `gc bd-shim` is a registered (hidden)
  gc subcommand; invoking it by name does **not** require `GC_BD_REAL`
  (that check only guards the `argv[0]==bd` install path, :1112-1122).
- `cmd/gc/cmd_bd_shim.go:263-271 classifyBdShimVerb` — `mol current|progress
  <id> [--json]` classifies `bdRoute` **in both phases** (the `splitPhase`
  flag only affects refusal of unroutable shapes).
- `cmd/gc/cmd_bd_shim.go:469-480` — routed `mol` calls
  `client.GetBeadGraph(id)`; `internal/api/client.go:984-1005 GetBeadGraph`
  is the typed client for `GET /v0/city/{cityName}/beads/graph/{rootID}`
  (registered at `internal/api/supervisor_city_routes.go:150`).
- `cmd/gc/cmd_bd_shim.go:356-368 bdShimAPIClient` — the shim itself performs
  controller URL discovery (standalone `[api]` port, else the supervisor's
  per-city API) and city resolution from `--city`/cwd (`runBdShim` :990-1019
  → `resolveBdCity`). **Python needs no URL, port, or city-name discovery.**
- Deployed-build check: `git merge-base --is-ancestor` confirms the mol
  routing commit `305bed90d` is an ancestor of the installed build
  `2a83e20bd` (`deploy/sqlite-b36-probe-attribution`). The verb exists in
  production.
- Precedent: `scripts/adopt_pr_approval_summary.py:1037,1073` already reads
  graph-resident approval state via `bd mol current <root> --json` (operator
  sessions, where the shim is installed as `bd`). `pr_merge.py` runs in the
  order/patrol context where only `gc` is guaranteed on PATH, hence
  `gc bd-shim` rather than bare `bd`.

Raw-HTTP alternative (rejected): the controller URL comes from the
supervisor config (`cmd/gc/cmd_supervisor_city.go:698-711`) or the city's
standalone `[api]` port, and the URL path needs the city NAME while the
scripts only carry the city PATH (`route["city"]` is used as a subprocess
`cwd`, e.g. pr_merge.py:121,126). Re-implementing that discovery in Python
duplicates `bdShimAPIClient` and drifts. If the shim verb is ever
unavailable, the raw route is
`GET <base>/v0/city/<cityName>/beads/graph/<rootID>` with the §4 schema.

Output contract of `gc bd-shim mol current <root> --json`
(`cmd_bd_shim.go:673-711 renderBdMol/molSteps` + `writeReadyJSON`):
a JSON **array of the subtree's beads excluding the root**, each in bd wire
format (§4 `Bead`), including **closed** steps. This is exactly what
`pr_review.issue_list` (pr_review.py:552-563) + `pr_review.bead_metadata`
(:573) already parse — zero new parsing code.

> **Trap:** the root bead is NOT in `mol current --json` output. Never use
> this helper to read root metadata (`molecule_root_metadata` in
> adopt_pr_approval_summary.py:1065-1088 already tolerates root absence for
> this reason). None of the four Group-G readers needs the root.

## 4. Graph route response schema (for reference / raw-HTTP fallback)

`GET /v0/city/{cityName}/beads/graph/{rootID}` — handler
`internal/api/huma_handlers_beads.go:509-556 humaHandleBeadGraph`, response
type `internal/api/handler_beads.go:335-339`:

```json
{
  "root":  { ...Bead... },
  "beads": [ { ...Bead... }, ... ],
  "deps":  [ {"from": "<id>", "to": "<id>", "kind": "parent|..."} ]
}
```

- `beads` contains the root itself plus every subtree bead
  (`collectBeadGraph` :341-369 upserts the root first, then lists by
  `gc.root_bead_id` metadata with `IncludeClosed: true` — closed steps ARE
  returned).
- `deps` element type: `internal/api/huma_types_convoys.go:51-55`.
- Headers `X-GC-Index`, `X-GC-Cache-Age-S` (`huma_types.go:173-177`).
- 404 (root unknown in every store): problem+json, detail
  `"bead <id> not found"` (huma_handlers_beads.go:530).

`Bead` wire fields (`internal/beads/beads.go:50-87`, JSON tags):
`id`, `title`, `status` (`open|in_progress|closed`), `issue_type`,
`priority?`, `created_at`, `updated_at?`, `assignee?`, `from?`,
`parent?` (ParentID), `ref?`, `needs?`, `description?`, `labels?`,
`metadata?` (**map[string]string — all the `gc.*` / `pr_review.*` /
`review.*` / `ci.*` keys the readers filter on**), `dependencies?`,
`ephemeral?`, `no_history?`, `defer_until?`, `is_blocked?`.

## 5. The shared helper (copy-paste-ready)

Answer to investigation Q4: `bounded_rig_bd_list` **already exists** at
pr_merge.py:125-127; the helper's fallback reuses it verbatim.

Insert after `bounded_rig_bd_list` (i.e. after line 127). Add the prefix
constant next to the other module constants (e.g. after
`BD_LOOKUP_TIMEOUT_SECONDS` at :36):

```python
# Molecule roots/steps minted by the controller's formula engine live in the
# city-scope SQLite graph store behind the Router and carry this ID prefix
# (gascity cmd/gc/api_state.go graphStoreIDPrefix). `gc --rig <rig> bd` is a
# pure passthrough to the rig's Dolt work store and can never see them.
GRAPH_ROOT_PREFIX = "gcg-"
```

```python
def graph_root(root_id: str) -> bool:
    return root_id.startswith(GRAPH_ROOT_PREFIX)


def graph_children_for_root(city: str | None, rig: str, root_id: str) -> list[dict[str, Any]]:
    """Child (step) beads of a molecule root, read through a graph-aware path.

    gcg- roots: read the subtree via the controller's graph route
    (GET /v0/city/<city>/beads/graph/<rootID>) through
    `gc bd-shim mol current <root> --json`, which resolves the controller URL
    and city scope itself and returns the subtree's beads (root excluded,
    closed steps included) in bd wire format. A missing/purged molecule maps
    to [] (the pre-existing "not done yet" semantics); any other graph-read
    failure raises code=graph_read_failed so the patrol goes RED instead of
    silently skipping — falling back to the rig store for a gcg- root would
    just re-instate the silent-empty bug this helper exists to fix.

    Non-graph roots: the existing rig-bd read, byte-identical.
    """
    if not root_id:
        return []
    if not graph_root(root_id):
        return bounded_rig_bd_list(
            city,
            rig,
            "--all",
            "--metadata-field",
            f"gc.root_bead_id={root_id}",
            "--limit",
            "0",
        )
    try:
        data = load_json_command_bounded(
            ["gc", "bd-shim", "mol", "current", root_id, "--json"],
            cwd=city,
        )
    except pr_review.CommandError as exc:
        detail = ""
        if exc.result is not None:
            detail = (exc.result.stderr or exc.result.stdout or "").lower()
        if "not found" in detail or "no issue found" in detail:
            return []
        raise TransientMergeError(
            f"graph children read for {root_id} failed: {exc}",
            code="graph_read_failed",
        ) from exc
    return pr_review.issue_list(data)
```

Design notes:

- **Timeout/JSON/error plumbing** reuses `load_json_command_bounded`
  (pr_merge.py:96-117): 20s bound (`BD_LOOKUP_TIMEOUT_SECONDS`),
  `TransientMergeError(code="command_timeout")` on timeout (already a
  recovery-visible failure, not a skip), `invalid_command_json` on garbage.
- **Not-found sniffing** mirrors the repo's own precedent
  (`pr_review.rig_bd_show_optional`, pr_review.py:698-707) and the Go
  shim's own classifier (`isBdShimAPINotFound`,
  cmd_bd_shim.go:796-802 matches `"not found"`/`"not_found"`). The shim's
  `mol` error line is `gc bd-shim: mol current "<id>" via API: bead <id> not
  found` (stderr), sourced from the huma 404 detail.
- `graph_read_failed` is deliberately **not** added to either skip set —
  controller-down must surface red, not green.
- `pr_review.issue_list` accepts the raw JSON array the shim prints;
  `metadata` is already a dict on these beads so `bead_metadata` works
  unchanged.

## 6. Per-reader changes

### 6.1 `review_loop_done` (:726-744, finding #62)

Replace the `bounded_rig_bd_list(...)` call at :729-737 with:

```python
    beads = graph_children_for_root(city, rig, root_id)
```

Everything else (the `gc.step_id == "review-loop"`,
`gc.outcome == "pass"`, `review.verdict == "done"` filter loop at :738-744)
is unchanged — those are metadata-map keys present verbatim on graph nodes.

### 6.2 `recovery_approval_gates_done` (:747-774, finding #63)

Replace the `bounded_rig_bd_list(...)` call at :753-761 with:

```python
    beads = graph_children_for_root(city, rig, root_id)
```

Filter loop at :762-774 (`gc.outcome`, `gc.step_id` ∈
{`pre-approval-ci`, `human-approval`}, `ci.verdict` ∈
{done, repaired, repaired_local}) unchanged.

### 6.3 `recover_source_from_finalizer` (:301-354, finding #61)

This reader queries by `pr_review.final_pr_number`, not by root — the graph
route is rooted, so derive the root from the source bead the function
already resolved (:310), and only branch for graph roots so the non-graph
path stays byte-identical:

Replace :313-321 (`finalizers = bounded_rig_bd_list(...)`) with:

```python
    source_root_md = metadata(source)
    root_id = str(
        source_root_md.get("pr_review.workflow_root_id")
        or source_root_md.get("workflow_id")
        or ""
    ).strip()
    if graph_root(root_id):
        finalizers = [
            bead
            for bead in graph_children_for_root(city, rig, root_id)
            if metadata(bead).get("pr_review.final_pr_number") == str(final_pr_number)
        ]
    else:
        finalizers = bounded_rig_bd_list(
            city,
            rig,
            "--all",
            "--metadata-field",
            f"pr_review.final_pr_number={final_pr_number}",
            "--limit",
            "0",
        )
```

The per-bead filter loop at :322-353 is unchanged; every key it reads
(`gc.step_id=finalize`, `gc.outcome`, `pr_review.final_pr_url`,
`pr_review.finalizer_head_sha`, `pr_review.ci_status`,
`pr_review.merge_ready_label_applied`, `pr_review.review_comment_posted`,
`gc.root_bead_id` at :346, `pr_review.merge_ready_at`/`ci_run_url` at
:348-350, and `bead_id(bead)` at :351) is present on graph nodes.

Scope note: the old rig-wide `final_pr_number` query could in principle see
finalizers from *other* molecules of the same PR; the graph path reads only
the source's recorded (latest) root. That is the molecule where the
successful finalize ran — the recorded `workflow_root_id` is refreshed on
every (re)launch — so recovery semantics are preserved.

### 6.4 `cleanup_superseded_review_workflow` (:1230-1271, finding #64)

Two changes:

1. **Close the molecule subtree first** via the graph-aware convoy delete,
   reusing `pr_review.convoy_delete_workflow` (pr_review.py:979-1001 —
   `gc convoy delete <root_id> --force`, `check=False` best-effort, matches
   `gc.root_bead_id` through the controller's Router; already on
   `origin/main` since `9b8c12a48`, already live). Insert immediately after
   the `if not root_id: return` guard at :1242-1243:

```python
    # Close the superseded molecule's FULL subtree (root + step beads)
    # through the graph-aware convoy delete BEFORE the rig-store cleanup
    # below. The rig bd reads here are passthrough to the Dolt work store
    # and cannot see gcg- graph residents, so without this the superseded
    # molecule's ready-frontier steps stay open forever and inflate
    # run-operator pool demand. Best-effort, mirroring
    # pr_review.cleanup_previous_workflow.
    pr_review.convoy_delete_workflow(city, root_id)
```

   Unconditional (not gated on `graph_root(root_id)`) — for rig-resident
   molecules the current code also leaks open step children (it only closes
   the root and rig-launch beads), and `pr_review.cleanup_previous_workflow`
   :1069-1070 already calls it unconditionally for every root. Idempotent on
   already-closed subtrees.

2. **Keep the rest as is.** The `rig_bd_show_optional` root-stamp at
   :1252-1253 still stamps `cleanup_md` on rig-resident roots and is a
   silent no-op for `gcg-` roots (the convoy delete already closed them);
   the `workflow_id={root_id}` list at :1254-1262 finds the rig-launch beads,
   which are rig-store residents by construction (created by pr_review's
   launch path) — not graph-blind.

   *Optional follow-up (not required for Group G):* stamp `cleanup_md` on
   `gcg-` roots via the shim's routed update
   (`gc bd-shim update <root_id> --set-metadata ...`), so the
   `recovered_already_merged` tell survives on graph roots. Deliberately
   out of scope: the operational goal (no open ready-frontier steps) is met
   by the convoy delete, and the memory file
   `maintainer-city-rogue-out-of-band-merge` flags
   `recovered_already_merged` as a misleading tell anyway.

Both callers (`cleanup_merged_launch_bead` :1429-1440,
`cleanup_merged_source_bead` :1536-1547) need no change.

## 7. Finding #65 — split `source_bead_not_found` out of the skip set

Replace :60-67 with:

```python
# Finding #65 (graph-store-split audit) — OWNER DECISION, one-line toggle.
# While True, a merge-ready-labeled PR whose PR-review source bead cannot be
# found or reconstructed (source_bead_not_found, raised by
# find_merge_ready_source) is recorded as a patrol SKIP: the sweep stays
# green, matching the 922fb57f7 behavior that stopped one orphaned label
# (gastownhall/gastown#4371) from failing the patrol every 5 minutes.
# Historically this code conflated two populations: genuinely orphaned
# labels AND graph-invisible sources whose finalize steps live in the SQLite
# graph store (fixed by graph_children_for_root). With the graph fix landed,
# the remaining population should be true orphans plus real confirmation
# failures; flipping this to False makes those go RED (sweep status=failed
# under --fail-on-error) instead of silently green. Flip only after the
# pr-merge-ready-patrol's alert consumers are confirmed ready to absorb a
# red on the currently-latent class.
SOURCE_BEAD_NOT_FOUND_IS_SKIP = True

# Merge-ready-labeled PRs the sweep can't act on. pr_not_open (the PR closed
# since labeling) is always a skip; source_bead_not_found membership is the
# owner-gated toggle above.
MERGE_READY_HANDOFF_SKIP_CODES = {
    "pr_not_open",
    *(("source_bead_not_found",) if SOURCE_BEAD_NOT_FOUND_IS_SKIP else ()),
}
```

No change to the classification site (:1354-1363) — it already tests set
membership. Ship with the toggle **True** (behavior-preserving); flipping to
`False` is the literal one-line change the audit asks for, surfaced to the
owner. Note the audit's name `source_confirmation_failed` for this code does
not exist in the repo; `source_bead_not_found` is the real member (§2).

The recovery-path skips (`review_not_done`, `approval_gates_not_done`,
`ci_not_pass`, `pr_not_open` in `AUTO_APPROVED_RECOVERY_SKIP_CODES` :53-58)
stay skips: after §6 they are truthful "not done yet" states again, which is
exactly what a patrol should wait on. `graph_read_failed` joins neither set.

## 8. `922fb57f7` reconciliation

Facts (verified in `/data/projects/workflows`):

- `922fb57f7` = **HEAD of local branch `fix/adopt-pr-root-id-from-convoy`**,
  the current checkout, clean tree. `git branch -a --contains` shows only
  that local branch — **unpushed**, as the audit says.
- The branch carries **7 unpushed commits** over `origin/main`
  (`fc129de39 → 01d6f7129 → 62afeb456 → 40b89c195 → 49a19f491 → 36dbe7e4c →
  922fb57f7`).
- `922fb57f7` touched only `scripts/pr_merge.py` (+27/−8) and
  `scripts/pr_merge_test.py`: it **introduced `MERGE_READY_HANDOFF_SKIP_CODES`
  = {source_bead_not_found, pr_not_open}** and the handoff-path
  skip-vs-failure classification at :1354-1363. It did NOT touch
  `pr_review.py`.
- `convoy_delete_workflow` was introduced by `9b8c12a48` ("close prior
  molecule subtree via convoy delete on retry"), which **IS on
  `origin/main`** (merge-base verified) and on the current branch. The
  audit's phrase "922fb57f7-era" refers to the same remediation wave, not to
  that commit's diff.

Reconciliation verdict: **Group G stacks on top of `922fb57f7`; it neither
supersedes it nor is orthogonal to it.**

- §7 edits the exact constant `922fb57f7` introduced, and §6 makes the skip
  codes truthful again — the two changes are one causal chain
  (`922fb57f7` stopped the patrol from crying wolf on orphans; Group G stops
  it from staying silent on graph-invisible work).
- Do NOT rebase Group G under it, do NOT re-land `convoy_delete_workflow`
  (import it from `pr_review`, which the file already does at :24).

Plan (do not execute without the owner):

1. Branch `fix/graph-aware-pr-merge-readers` FROM
   `fix/adopt-pr-root-id-from-convoy@922fb57f7` (or continue on that branch).
2. Land Group G as two commits: (a) helper + four readers + tests,
   (b) the #65 toggle + its test (separable if the owner wants to hold #65).
3. Push the whole 7+2 stack to `origin` and PR it — owner-gated: the repo is
   shared, and `922fb57f7` is already live in the deployed pack but absent
   from `origin/main`, so until the stack is pushed every pack re-sync from
   origin would regress the patrol fix. Surface this drift to the owner in
   the PR description.

## 9. TDD plan

Harness (verified): plain `unittest`, run via
`python -m unittest discover -s scripts -p '*_test.py'` (README.md:71 + CI
`.github/workflows/ci.yml`). `scripts/pr_merge_test.py` loads the module
fresh per test through `load_module()` (importlib, :18-23) and monkeypatches
by **attribute assignment on the module instance** (e.g.
`pr_merge.bounded_rig_bd_list = lambda ...`, see :396-397). No pytest, no
`unittest.mock` needed. New tests go in `scripts/pr_merge_test.py`, new
class `GraphChildrenForRootTest(unittest.TestCase)` + additions to the
existing sweep tests. Write these first; all of them fail against
`922fb57f7`.

1. **`test_graph_children_for_root_routes_gcg_roots_through_graph`**
   — patch `pr_merge.load_json_command_bounded` to capture `(args, cwd)` and
   return `[{"id": "gcg-43", "status": "closed", "metadata": {"gc.root_bead_id": "gcg-42", "gc.step_id": "review-loop"}}]`;
   patch `pr_merge.bounded_rig_bd_list = lambda *a, **k: self.fail("rig bd is graph-blind and must not serve gcg- roots")`.
   Call `graph_children_for_root("/tmp/test-city", "gascity", "gcg-42")`;
   assert captured args `== ["gc", "bd-shim", "mol", "current", "gcg-42", "--json"]`,
   cwd `== "/tmp/test-city"`, and the bead list round-trips.

2. **`test_graph_children_for_root_keeps_rig_bd_for_work_store_roots`**
   — patch `load_json_command_bounded` to `self.fail(...)`; patch
   `bounded_rig_bd_list` to capture args and return `[]`. Call with
   `root_id="gascity-abc"`; assert the rig call got
   `("--all", "--metadata-field", "gc.root_bead_id=gascity-abc", "--limit", "0")`.

3. **`test_graph_children_for_root_missing_molecule_is_empty`**
   — patch `load_json_command_bounded` to raise
   `pr_merge.pr_review.CommandError(["gc"], subprocess.CompletedProcess(["gc"], 1, stdout="", stderr='gc bd-shim: mol current "gcg-9" via API: bead gcg-9 not found'), "boom")`;
   assert result `== []` (a purged/TTL'd molecule must degrade to "not done",
   not a red patrol — the 922fb57f7 orphan-label incident class).

4. **`test_graph_children_for_root_controller_down_raises_graph_read_failed`**
   — same, but stderr `"connection refused"`; `assertRaises` on
   `pr_merge.TransientMergeError`, assert `.code == "graph_read_failed"`,
   and assert `"graph_read_failed" not in pr_merge.MERGE_READY_HANDOFF_SKIP_CODES`
   and `not in pr_merge.AUTO_APPROVED_RECOVERY_SKIP_CODES`.

5. **`test_review_loop_done_sees_graph_resident_steps`** — THE regression
   test for the live incident. Patch `load_json_command_bounded` to return
   the review-loop step
   `{"id": "gcg-43", "metadata": {"gc.step_id": "review-loop", "gc.outcome": "pass", "review.verdict": "done", "gc.root_bead_id": "gcg-42"}}`;
   patch `bounded_rig_bd_list = lambda *a: []` (simulating the blind rig
   store). Assert
   `pr_merge.review_loop_done("/tmp/test-city", "gascity", "gcg-42") is True`.
   Against current HEAD this returns `False` — fails red first.

6. **`test_recovery_approval_gates_done_sees_graph_resident_steps`** — same
   shape with `pre-approval-ci` (`ci.verdict=done`) + `human-approval`
   steps; assert `True` for root `"gcg-42"`, rig list patched to `[]`.

7. **`test_recover_source_from_finalizer_reads_graph_resident_finalizer`**
   — build `city_candidates` containing a source bead whose metadata has
   `pr_review.version=1`, `pr_review.pr_url/pr_number` matching, and
   `pr_review.workflow_root_id="gcg-42"`; patch the graph read to return a
   finalize step carrying the full :322-337 pass conditions plus
   `pr_review.final_pr_number`; patch `bounded_rig_bd_list` to `self.fail`.
   Assert a recovered dict is returned and
   `metadata["pr_review.workflow_root_id"]` comes from the finalizer's
   `gc.root_bead_id`.

8. **`test_cleanup_superseded_review_workflow_convoy_deletes_graph_root`**
   — patch `pr_merge.pr_review.convoy_delete_workflow` to capture
   `(city, root_id)` calls, `pr_merge.pr_review.rig_bd_show_optional =
   lambda *a: None`, `pr_merge.bounded_rig_bd_list = lambda *a: []`,
   `pr_merge.update_rig_metadata = lambda *a, **k: self.fail("gcg- root is not rig-updatable")`.
   Call `cleanup_superseded_review_workflow(city=..., rig=..., repo=...,
   number=1, pr_url=..., root_id="gcg-42", head_sha=..., merged_at=...)`;
   assert exactly one convoy-delete call with `"gcg-42"`.

9. **`test_source_bead_not_found_skip_membership_follows_toggle`** — assert
   `"source_bead_not_found" in pr_merge.MERGE_READY_HANDOFF_SKIP_CODES` iff
   `pr_merge.SOURCE_BEAD_NOT_FOUND_IS_SKIP`, and `"pr_not_open"` always in.
   (Keeps the existing
   `test_sweep_merge_ready_skips_orphaned_merge_ready_label` :294 green
   while the toggle is True; when the owner flips it, that test is the one
   they consciously rewrite — the toggle test makes the flip a reviewed,
   single-line decision.)

Existing-suite compatibility: all current recovery tests use non-`gcg-` root
ids (`"workflow-root"`) and patch `bounded_rig_bd_list`, so they exercise
the fallback branch and must pass unchanged — that IS the byte-identical
regression guard for the non-graph path.

Quality gates before handing back: full
`python -m unittest discover -s scripts -p '*_test.py'` (repo has no
type-checker/linter config for scripts — state that in the PR rather than
claiming lint-clean), plus one live smoke on maintainer-city:
`gc bd-shim mol current <live gcg- root> --json | jq length` and a manual
`pr_merge.py sweep-merge-ready` (no `--fail-on-error`) reading the summary
counts.

## 10. MUST-FIX risks

1. **New red-failure mode (`graph_read_failed`).** Controller down/API
   unreachable now fails recovery items red instead of skipping. This is
   the intended anti-silent-skip behavior, but it is patrol-visible: the
   pr-merge-ready-patrol will emit `order.failed` while the controller is
   restarting. Call this out in the PR; do not add it to the skip sets.
2. **Not-found detection is string sniffing** (`"not found"` in stderr). It
   mirrors `pr_review.rig_bd_show_optional` and the Go shim's own
   `isBdShimAPINotFound`, but drifts if the API's 404 detail wording
   changes. Mitigation: test #3 pins the current message; if gascity ever
   types this, switch to exit-code or a `--json`-error contract.
3. **`mol current --json` excludes the root bead** (molSteps,
   cmd_bd_shim.go:702-711). All four readers only need steps; any future
   reader needing root metadata must use `gc bd-shim show <gcg-id>` (routed
   by id, returns `[bead]`) — not this helper.
4. **Deployed-gc version coupling.** The helper hard-requires
   `gc bd-shim mol` routing (gascity `305bed90d`), verified present in the
   installed `2a83e20bd`. If the workflows pack is ever pointed at an older
   gc, `gc bd-shim` errors → `graph_read_failed` → red patrol (loud, not
   silent — acceptable), but note it in the PR as a version floor.
5. **`cwd=city` is load-bearing** for the shim's city resolution
   (`resolveBdCity` → cwd discovery). Every caller already passes
   `route["city"]` (a path) as `cwd`; keep the helper's `cwd=city` and never
   call it with `city=None` from a non-city working directory.
6. **Sweep latency.** Each recovery candidate now performs up to two
   controller HTTP round-trips (review-loop + gates) instead of two rig bd
   execs — comparable cost (each old exec was itself a bd/Dolt round-trip)
   and bounded by the existing 20s `BD_LOOKUP_TIMEOUT_SECONDS`. If patrol
   budget becomes a problem, memoize `graph_children_for_root` per
   `(root_id)` within one `recover_auto_approved_source` call — do not
   pre-optimize now.
7. **Unpushed-stack drift (§8).** Group G is built on 7 unpushed commits of
   a shared repo. Until the stack is pushed, any pack re-sync from
   `origin/main` silently reverts BOTH `922fb57f7` and Group G. The push is
   part of the definition of done, but it is owner-gated — get the sign-off.
8. **Metadata-key drift between formula versions.** The readers key on
   `gc.step_id`; adopt_pr_approval_summary.py:1054-1056 also accepts
   `gc.step_ref` containing the step name. Current pr-review formulas stamp
   `gc.step_id` on the step beads the four readers filter (verified against
   the existing tests' fixtures and the live incident data), so Group G
   intentionally does not widen the filter — flag it only if a formula
   rename lands.
