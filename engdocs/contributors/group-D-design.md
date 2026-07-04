# Group D design: federated reads must not launder a failed leg into a complete result

Graph-store-split remediation, finding #4. Branch investigated:
`deploy/sqlite-b36-probe-attribution`, worktree
`/data/projects/gascity/.claude/worktrees/beads`. Every file:line below was
re-located on this branch.

## 1. Doctrine independence

Group D is independent of the routing doctrine ("ClassGraph / `gcg-` work
beads always live in the sqlite graph store, never Dolt"). The doctrine
governs *where beads are written*; Group D governs *what a federated read
reports when one leg cannot answer*. Even in a city where the doctrine is
perfectly enforced, a locked or failing sqlite leg plus a healthy Dolt leg
makes `federateRead` return only the Dolt rows with a `nil` error — the
in-progress `gcg-` beads vanish from a read every caller treats as complete.
Symmetrically, if a doctrine violation ever leaked formula beads into Dolt, a
failing Dolt leg would hide them the same way. The bug and the fix apply to
any multi-backend Router leg combination (including the planned
graph-on-Postgres backend from the infra-decoupling work), for any class
(graph, messaging, sessions, orders, nudges): whenever `len(Backends()) > 1`,
a degraded leg must be *reported*, not silently subtracted from the union.

## 2. The bug, precisely

`internal/coordrouter/router_federation.go`:

- `federateRead` (lines 242–268): loops `r.Backends()`; a leg error sets
  `lastErr = err; continue` (248–251), dropping that leg's rows entirely.
  After the loop the only error return is `if merged == nil && lastErr != nil`
  (260–262) — i.e. it errors only when **every** leg failed **and** zero rows
  survived. One healthy leg → `(survivorRows, nil)`.
- `DepList` (lines 205–238): identical pattern at 221–226; hard-fail only at
  `if out == nil && lastErr != nil` (234–236).

Consequences (verified against current code):

- `cmd/gc/build_desired_state.go` — the assigned-work collector's
  drain-suppression fail-safe (`storePartial`, printed at line 671
  "drain decisions suppressed") is armed by
  `beads.IsPartialResult(err) && len(inProgress) > 0` at lines 1126 and 1145.
  A federated leg failure never produces that error today, so the fail-safe
  never engages, in-progress work "disappears", and sessions holding live
  graph work become drainable.
- Every orphan-release / sweep / tally input that reads through the Router
  gets a truncated row set labeled complete (full inventory in §6).

A second, same-class defect lives inside the same loop: a *leg itself* can
return `(rows, *beads.PartialResultError)` — the CachingStore does exactly
this (`internal/beads/caching_store_reads.go:76` and `:94`, forwarding
BdStore partials from `internal/beads/bdstore.go:2159/2252/2348/2447`).
`federateRead` currently treats that as a total leg failure and **discards
the usable rows** (a bd parse partial on the work leg deletes every work row
from the union while returning `nil`… or worse, only the graph rows with a
clean error). The fix below repairs both.

## 3. `PartialResultError` — shape and construction (design question 1)

`internal/beads/bdstore.go:616–651`:

```go
type PartialResultError struct {
	Op  string // e.g. "bd list", "bd ready"
	Err error  // wrapped cause(s)
}
func (e *PartialResultError) Error() string  // "%s: %v", e.Op, e.Err
func (e *PartialResultError) Unwrap() error  // e.Err
func IsPartialResult(err error) bool         // errors.As(&partial)
```

Existing constructions: `&PartialResultError{Op: "bd list", Err: parseErr}`
(bdstore.go:2159), `&PartialResultError{Op: op, Err: fmt.Errorf("bd list: %w",
primaryErr)}` (2348), `Op: "cache list include closed"`
(caching_store_reads.go:76), `Op: "controller ready demand"`
(cmd/gc/build_desired_state.go:1764). The doc comment (616–621) states the
contract we rely on: *"The successful entries are still returned alongside
this error; callers that can surface partial data may proceed with those
rows, while callers that require a complete picture should treat this as a
hard failure."*

**Chosen Op strings.** Thread the calling method's name through
`federateRead` (a new first parameter — the function is private and all seven
call sites are in the same file), producing `"coordrouter List"`,
`"coordrouter ListOpen"`, `"coordrouter Children"`, `"coordrouter
ListByLabel"`, `"coordrouter ListByAssignee"`, `"coordrouter ListByMetadata"`,
`"coordrouter Ready"`. `DepList` constructs its own with
`Op: "coordrouter DepList"`. Wrap `lastErr` in `Err`. Nesting is fine: when
`lastErr` is itself a `*PartialResultError` from a leg, `errors.As` traversal
via `Unwrap` still satisfies `IsPartialResult`, and the message composes
("coordrouter List: bd list: …").

Package direction is already correct: `coordrouter` imports `beads`
(router_federation.go:7); `beads` never imports `coordrouter`. No new imports
needed.

## 4. Exact new function bodies (design questions 2 & 3)

### 4.1 `federateRead` (replaces router_federation.go:240–268)

The partial error is returned on the success-with-partial path **after**
sorting and limiting; the all-legs-failed hard-fail path is preserved
byte-for-byte. Additionally, a leg that returned usable rows alongside a
`PartialResultError` now contributes its rows to the union instead of being
dropped.

```go
// federateRead runs read on every backend, unions the results, dedups by id,
// and re-sorts + re-limits over the combined set. A failed leg never silently
// truncates the union: when any leg errors but rows survive (from the other
// legs, or from the failing leg itself when it returned usable rows alongside
// a beads.PartialResultError), the merged rows are returned WITH a
// beads.PartialResultError so callers that need a complete picture — drain
// suppression, orphan release, sweeps — can tell a degraded union from a
// complete one. Only when every leg fails with no usable rows does the read
// hard-fail with the last leg error, unchanged.
func (r *Router) federateRead(op string, order beads.SortOrder, limit int, read func(beads.Store) ([]beads.Bead, error)) ([]beads.Bead, error) {
	seen := make(map[string]bool)
	var merged []beads.Bead
	var lastErr error
	for _, backend := range r.Backends() {
		got, err := read(backend)
		if err != nil {
			lastErr = err
			if !beads.IsPartialResult(err) {
				continue
			}
			// A partial leg returned usable rows alongside its error
			// (the PartialResultError contract): merge them below and
			// keep the error so the union is still reported partial.
		}
		for _, b := range got {
			if seen[b.ID] {
				continue
			}
			seen[b.ID] = true
			merged = append(merged, b)
		}
	}
	if merged == nil && lastErr != nil {
		return nil, lastErr
	}
	sortBeads(merged, order)
	if limit > 0 && len(merged) > limit {
		merged = merged[:limit]
	}
	if lastErr != nil {
		return merged, &beads.PartialResultError{Op: op, Err: lastErr}
	}
	return merged, nil
}
```

The seven wrappers change one line each, e.g. `List` (router_federation.go:89):

```go
	return r.federateRead("coordrouter List", query.Sort, query.Limit, func(s beads.Store) ([]beads.Bead, error) {
		return s.List(query)
	})
```

(`ListOpen`:99 → `"coordrouter ListOpen"`, `Children`:110 → `"coordrouter
Children"`, `ListByLabel`:120, `ListByAssignee`:130, `ListByMetadata`:140,
`Ready`:155 → likewise.)

Notes:

- **Ordering subtlety resolved**: the wrap happens after `sortBeads` and the
  limit cut, so the returned rows are identical to what a fully-successful
  union of the surviving legs would produce; only the error slot differs.
- `merged == nil && lastErr == nil` (all legs empty) still returns
  `(nil, nil)` — unchanged.
- Edge case intentionally preserved: one leg succeeds with **zero** rows and
  another hard-fails → `merged == nil`, `lastErr != nil` → hard-fail with the
  bare leg error, exactly as today. `PartialResultError`'s contract is
  "returned at least one usable entry" (bdstore.go:616), so an empty partial
  would be a contract violation; and the pre-change behavior for this shape
  is already the safe one.
- `lastErr` stays last-wins across multiple failing legs (deterministic:
  `Backends()` returns primary first then `coordclass.Classes()` order,
  router.go:95–109). Switching to `errors.Join` accumulation is compatible
  with every `IsPartialResult` check in-tree (`errors.As` traverses joins)
  but changes the all-legs-failed error text; it is explicitly out of scope.

### 4.2 `DepList` (replaces router_federation.go:205–238)

```go
// DepList federates dependency reads: an edge touching id may be recorded in
// any backend (a cross-class blocks edge lives in the Work store), so it
// unions and dedups across backends. A failed leg with surviving deps returns
// the union wrapped in beads.PartialResultError rather than posing as a
// complete edge set (a hidden cross-store edge would corrupt readiness,
// tally, and cycle checks).
func (r *Router) DepList(id, direction string) ([]beads.Dep, error) {
	if b, ok := r.soleBackend(); ok {
		return b.DepList(id, direction)
	}
	if owner := r.prefixBackendForID(id); owner != nil {
		if deps, err := owner.DepList(id, direction); err == nil && len(deps) > 0 {
			return deps, nil
		}
		// Empty or error from the owner: fall through to full federation so a
		// cross-store edge (a work bead blocked-by a graph root, recorded in the
		// Work store) is still found. Graph molecules are self-contained in
		// practice, so this fast-path returns on the first hit without the fork.
	}
	seen := make(map[beads.Dep]bool)
	var out []beads.Dep
	var lastErr error
	for _, backend := range r.Backends() {
		deps, err := backend.DepList(id, direction)
		if err != nil {
			lastErr = err
			if !beads.IsPartialResult(err) {
				continue
			}
		}
		for _, d := range deps {
			if !seen[d] {
				seen[d] = true
				out = append(out, d)
			}
		}
	}
	if out == nil && lastErr != nil {
		return nil, lastErr
	}
	if lastErr != nil {
		return out, &beads.PartialResultError{Op: "coordrouter DepList", Err: lastErr}
	}
	return out, nil
}
```

**Fast-path confirmation (design question 3).** The prefix-owner fast path at
router_federation.go:209–217 returns only on `err == nil && len(deps) > 0`;
an owner error or an owner-empty result falls through to full federation and
is re-queried there, so the fast path never emits (or masks) a partial. It
does keep its pre-existing, documented tradeoff — a first-hit owner read
skips cross-store edges by design — which is unrelated to Group D. (BdStore's
`DepList` never returns deps-with-error — bdstore.go:2496–2516 — so the
partial-merge branch in DepList is defensive symmetry; the merge is a no-op
for hard errors because `deps` is nil.)

## 5. Who is *structurally* exposed

The multi-backend Router exists only when `[beads] graph_store = "sqlite"`:
`routedPolicyStore` (cmd/gc/api_state.go:246–253) builds
`wrapStoreWithBeadPolicies(Router(work, graph))`; `wrapWithCachingStore`
(api_state.go:167–235) keeps the CachingStore **below** the Router as the
work leg and re-wraps the Router in the policy store. The policy store is a
pure error pass-through on every read (cmd/gc/bead_policy_store.go:75–225 —
`List`/`Ready` forward; `Children`/`ListByLabel`/`ListByAssignee`/
`ListByMetadata`/`ListOpen` are all self-delegations to `s.List`), so the new
`PartialResultError` reaches callers unmodified.

One amplifier: the Router has **no** native `Handles()` implementation
(verified: no `Handles` symbol in internal/coordrouter/ outside
`beads.HandlesFor(graph)` at router_federation.go:186), so
`beads.HandlesFor(policyStore)` → `beadPolicyStore.Handles()`
(bead_policy_store.go:96–102) → `beads.HandlesFor(Router)` → the *logical*
readers (internal/beads/caching_store_handles.go:64–68, 89–123), whose
`Live.List`/`Cached.List`/`Live.Ready`/`Live.DepList` all call the Router's
federated methods. Every `HandlesFor(store).Live.*` caller is therefore in
scope. `Count` is not: the Router *does* declare
`Count(ctx, query, excludeTypes...)` (router_mutation.go:119–126), but its
split-case body deliberately returns `beads.ErrCountUnsupported` (line 125),
so `beadPolicyStore.Count` (bead_policy_store.go:88–94) propagates it and
callers fall back to `List` — already counted. **Guard for the future:** the
`ErrCountUnsupported` return is the only thing keeping `Count` out of scope;
any future federated `Count` body MUST apply the `federateRead` partial-result
contract, or it will launder a partial exactly like `federateRead` did
pre-fix, with no §6.3 guard covering it.

**Not** exposed: `Get` (unchanged), `ReadyGraphOnly` / `ListGraphOnly`
(single-leg direct reads, router_federation.go:167–187), all mutations, and
anything reading a leg directly (e.g. t3bridge's `cache.Children` on the
CachingStore).

## 6. Caller blast radius (design question 4)

Method: every non-test call of `.List( .ListOpen( .Children( .ListByLabel(
.ListByAssignee( .ListByMetadata( .Ready( .DepList(` on a bead-store receiver
across `cmd/gc` and `internal/` was enumerated (four parallel sweeps;
non-bead receivers — supervisor registry, event providers, session-manager
catalogs, k8s clients, extmsg service registries — excluded). ~110 bead-store
sites classified.

### 6.1 The governing argument: hard-fail is the contract, not a regression

`PartialResultError` is **already** part of these methods' error surface for
every one of these callers: a single-store leg emits it today for bd parse
partials (bdstore.go:2159/2252/2348/2447) and cache-degradation
(caching_store_reads.go:76), and production has already been burned into
adding guards at the API and controller layers (gascity#3253;
build_desired_state.go:1126/1145/1479/1747/1760/3729;
internal/session/list_all.go:50/59). The federation layer is currently the
*one* layer that launders this error class away. A caller that does
`if err != nil { return err }` is exercising the documented contract choice —
"callers that require a complete picture should treat this as a hard failure"
— and for the destructive paths (sweeps, tallies, closes, drains, dedup
lookups) aborting the tick and retrying next cycle is precisely the fail-safe
the system's convergence model prescribes ("the system converges because work
persists"). **Default verdict is therefore SAFE — no caller edit.** NEEDS-GUARD
is reserved for three regression shapes:

- (a) callers that *were using* the survivor rows and will now **drop them
  for a benign zero value that changes a decision** (`if err != nil { return
  "" }` — the rows arrive, the error makes them invisible);
- (b) read-only user-facing endpoints that would newly 500 while holding
  perfectly renderable rows;
- (c) liveness parity: probes/loops where today *both* degraded states pass
  and the fix alone would newly wedge startup.

Known liveness cost accepted deliberately: during a Dolt-leg outage,
dispatcher/reconciler ticks that read through full federation will abort and
retry instead of proceeding on graph rows. That is the intended fail-safe;
the durable mitigation is the graph-only read surfaces (`ListGraphOnly` /
`ReadyGraphOnly`, Groups A/B territory), which bypass federation entirely.

### 6.2 GUARDED already (benefit immediately, zero change)

| Caller | Site | Guard |
|---|---|---|
| `readyForControllerDemandQuery` | cmd/gc/build_desired_state.go:1747, 1760, 1764 | `!beads.IsPartialResult(...)`; re-wraps as `Op:"controller ready demand"` |
| assigned/routed work collectors | cmd/gc/build_desired_state.go:981, 1126, 1145, 1479, 3729 | `IsPartialResult && len(rows) > 0` keeps rows + arms `storePartial` → **the drain-suppression fail-safe at :670–672 now actually engages** |
| `ListAllSessionBeads` | internal/session/list_all.go:50, 59 | `err != nil && !beads.IsPartialResult(err)` |
| `humaHandleBeadList` | internal/api/huma_handlers_beads.go:153–163 | `IsPartialResult(err) && len(list) > 0` → keep rows, `pa.success()` |
| `humaHandleBeadReady` | internal/api/huma_handlers_beads.go:352–360 | same pattern |
| `humaHandleBeadEphemeral` | internal/api/huma_handlers_beads.go:454, 473 | same pattern per goroutine result |
| `statusStoreWorkCounts` | internal/api/handler_status.go:566–572 | `!IsPartialResult(err) \|\| len(list) == 0` |
| session read-model | internal/api/cache_read_model.go:60–67 | rows + partial → degraded-note strings |
| order history CLI | cmd/gc/cmd_order.go:1374, 1666 | len-guarded partial tolerance |
| orders feed freshness | internal/api/orders_feed.go:385 | uses rows even when `err != nil` |
| orders runtime helpers | internal/orders/runtime_helpers.go:22, 70 | `len(results) == 0` gate, logs partial |

### 6.3 NEEDS-GUARD (exact edits; the full extent of multi-file fallout)

Eight fixes. G1–G3 are regression shape (a), G4–G7 shape (b), G8 shape (c).

**G1 — `internal/agentutil/pool.go:51` `findSessionNameByTemplate`.**
Currently `if err != nil { return "" }` after `store.List(...)`. A partial
now hides an *existing* pool session → callers treat the pool slot as
unnamed/absent (duplicate-session risk). Guard:

```go
	if err != nil && !(beads.IsPartialResult(err) && len(beadList) > 0) {
		return ""
	}
```

**G2 — session-name lookups, two coupled sites.**
`cmd/gc/session_name_lookup.go:500–503` `findSessionNameByAgentLabel`
(`if err != nil { return "" }`): guard exactly as G1
(`!(beads.IsPartialResult(err) && len(items) > 0)`).
`internal/session/metadata_candidates.go:54–57`
`exactMetadataSessionCandidates` (`items, err := store.List(query); if err !=
nil { return nil, err }` — discards the survivor rows). This sits inside the
per-filter loop, so mirror the accumulate-then-propagate shape of
list_all.go:108–113: keep filtering the partial `items`, remember the first
partial error, and return `(candidates, firstPartialErr)` at the end:

```go
		items, err := store.List(query)
		if err != nil {
			if !beads.IsPartialResult(err) || len(items) == 0 {
				return nil, err
			}
			if firstPartialErr == nil {
				firstPartialErr = err
			}
		}
		// existing filter/append loop unchanged
	}
	return candidates, firstPartialErr
```

…and its consumer `findSessionNameByMetadata`
(cmd/gc/session_name_lookup.go:507–510, `if err != nil { return "" }`) gets
the G1-style guard so the surfaced partial doesn't re-blank the name. The
remaining `ExactMetadataSessionCandidates` consumers are fine as-is:
internal/session/names.go:328/531/587 and
internal/api/session_resolution.go:150 return the error upward (fail-safe);
cmd/gc/template_resolve.go:250 is an `if err == nil` fallthrough (deferred
list, non-destructive).

**G3 — `cmd/gc/beads_provider_lifecycle.go:956` `verifyCanonicalBdScopeStoreReady`.**
Readiness probe: `_, err := store.List(...); if err == nil { return nil }` in
a 20×500ms retry loop. Today **both** degraded states pass this probe
(federation returns nil); without a guard the fix turns any single-leg outage
into a startup wedge. Liveness parity guard:

```go
		_, err := store.List(beads.ListQuery{AllowScan: true, Limit: 1})
		if err == nil || beads.IsPartialResult(err) {
			return nil
		}
```

(A partial proves the store surface is up; the failing leg surfaces through
its own ops and `lazyGraphStore` self-heal, api_state.go:282–296.)

**G4 — `internal/api/huma_handlers_beads.go:595–601` `humaHandleBeadDeps`.**
`GET /v0/bead/{id}/deps` would 500 while holding the reachable children.
Mirror the file's own :155 pattern:

```go
		children, err := store.List(beads.ListQuery{ParentID: id, Sort: beads.SortCreatedAsc})
		if err != nil && !(beads.IsPartialResult(err) && len(children) > 0) {
			return nil, huma.Error500InternalServerError(err.Error())
		}
```

**G5 — `internal/api/handler_beads.go:367–373 and :420–427` `collectBeadGraph`.**
The bead-graph dashboard endpoint (`humaHandleBeadGraph` maps any error to a
500). Two sites, same guard shape:

```go
	if err != nil && !(beads.IsPartialResult(err) && len(metadataChildren) > 0) {
		return nil, nil, fmt.Errorf("listing metadata children for bead %q: %w", root.ID, err)
	}
```

(and `len(children) > 0` for the BFS site at :420; partial rows keep the BFS
walking the reachable subtree).

**G6 — `internal/api/handler_convoy_dispatch.go:171–176` `snapshotFromStore`.**
The workflow-snapshot bd-fallback; its sibling root-scan at :127–135 already
degrades via `listPartial = true`. Guard:

```go
		if err != nil && !(beads.IsPartialResult(err) && len(all) > 0) {
			return nil, err
		}
```

(Optional polish, not required: also surface the partial in the snapshot's
existing partial marker.)

**G7 — `internal/api/huma_handlers_convoys.go:90–96` `humaHandleConvoyList`.**
A partial-with-rows rig currently counts as a failed attempt; if *every* rig
is partial, `pa.totalOutage()` (line 98) 500s despite rows. Mirror
huma_handlers_beads.go:155:

```go
		list, err := store.List(beads.ListQuery{Type: "convoy"})
		if err != nil {
			if beads.IsPartialResult(err) && len(list) > 0 {
				pa.record("rig "+rigName, err)
				pa.success()
			} else {
				pa.record("rig "+rigName, err)
				continue
			}
		} else {
			pa.success()
		}
		convoys = append(convoys, list...)
```

**G8 — `cmd/gc/session_reconciler.go:3960 and :4005` (`resolveTaskWorkDir`,
`resolveTaskOptionOverrides`).** Both do `if err != nil { continue }` per
assignee; a partial now silently downgrades a task's `work_dir` /
provider-option overrides to defaults (the run lands in the wrong worktree).
Guard in both:

```go
		if err != nil && !(beads.IsPartialResult(err) && len(assigned) > 0) {
			continue
		}
```

### 6.4 SAFE — hard-fail is the correct fail-safe (no edits; exhaustive)

Every site below checks `err` *before* consuming rows, so the new error can
only abort earlier — it can never itself drive a destructive decision. The
destructive risk was in the old proceed-on-truncated-rows behavior, which the
router fix eliminates; the caller failure mode becomes tick-abort + retry.

**internal/dispatch** (the control-dispatcher engine; all
`if err != nil { return … }` unless noted; the highest-value protections in
the whole group): runtime.go:446 (`liveListForRoot`), 989
(`sourceWorkflowChildSources`), 1197 (`resolveBlockingSubjectID`), 1319
(scope-skip dep load — gated a destructive member skip), 1500
(`resolveBlockedOutcome` — gated a premature `pass` finalize); retry.go:603;
ralph.go:691, 783, 1203, 1216, 1220 (retry-DAG edge copies + idempotency);
**tally.go:56** (voter deps — the mistally-and-close case this change exists
to prevent); drain.go:575, 646, 773, 834, 898, 1011 (drain projections +
drain-unit/item-root dedup — duplicate-creation risks removed); control.go:285,
565, 1359; fanout.go:226, 542, 612, 641, 732; fanout.go:707
(`canDiscardPartialFragmentBead` — returns `false` on error, conservative).

**internal/molecule**: molecule.go:326, 347, 414, 1360 (attach idempotency /
ID-reuse maps — duplicate-instantiation risks removed); cleanup.go:38, 58
(`ListSubtree` feeding `CloseSubtree` — aborts before any close).

**internal/sling**: sling.go:1364, 1416, 1469 (graph-v2 root uniqueness /
failed-root cleanup); sling_core.go:1179; cycle.go:49 (`DetectCycle` — a
truncated edge set previously meant false-acyclic); sling_attachment.go:63
(records `firstErr`, still returns collected attachments alongside it).

**internal/convoy**: membership.go:36, 69, 94, 134, 149 (`Members` feeds
completion tallies — premature convoy completion risk removed).

**internal/session**: named_config.go:316, 373, 440; metadata_candidates.go:54
(guarded via G2); resolve.go:131; manager.go:1354 (`PruneDetailed`), 1472
(`ListFull`); waits.go:97; chat.go:970.

**internal/mail/beadmail**: beadmail.go:566, 723, 868 (thread render /
recipient routing fail closed — no misdelivery on a half-view).

**internal/extmsg** (all `return fmt.Errorf(...)`): delivery_service.go:70,
130, 214; binding_service.go:255, 371, 478, 619, 739, 812, 835, 1000;
group_service.go:80, 163, 261, 486, 523, 547; transcript_service.go:451, 491,
747, 773, 799, 845; binding_reaper.go:59, 156.

**internal/nudgequeue**: waits.go:153.

**internal/sourceworkflow** (via `HandlesFor(store).Live.List`):
sourceworkflow.go:178, 373, 482, 577, 587.

**internal/convergence** + its cmd/gc adapter: convergence_store.go:41, 149,
241, 260, 278, 304; reconcile.go:329, 352, 477, 657; manual.go:448;
handler.go:813, 920; convergence_tick.go:571 (forces startup-reconcile retry
until legs heal — convergent, watch-item only). Metric-degrading tolerants
(reconcile.go:592, 667; handler.go:838) unchanged.

**cmd/gc destructive sweeps/GC** (abort-before-delete is the point):
order_dispatch.go:1885, 2063, 2182, 2249, 2423, 2557; wisp_gc.go:251, 302,
541, 617, 655, 665; nudge_mail_sweep.go:59, 106, 153, 184; cmd_formula.go:948.

**cmd/gc reconciler/dispatch**: session_reconciler.go:2870, 2987, 3154, 3300
(assigned-work predicates — err propagates to the tick);
session_beads.go:653, 716, 2361 (orphan release per-assignee skip → deferral,
converges next tick — this *is* the "orphan-release input truncates" repair:
the truncation becomes an explicit skip instead of a silent half-release);
pool_session_name.go:404 (returns `true` = blocks release, fail-safe), 469
(returns `false` = won't release, fail-safe);
session_lifecycle_parallel.go:636 (treats as in-flight, fail-safe);
order_dispatch.go:1497, 1580, 1675, 1719; cmd_convoy_dispatch.go:1426, 1430,
1571; cmd_formula.go:896, 916, 971; cmd_github.go:269; wisp_autoclose.go:150;
cmd_convoy.go:648.

**cmd/gc one-shot CLI / bridge / doctor** (error text shown to a human, rows
retriable): cmd_beads.go:284; cmd_bd_store_bridge.go:223, 229, 238, 255, 292;
cmd_graph.go:180; cmd_converge.go:294, 650; cmd_wait.go:769;
cmd_session_logs.go:273; nudge_beads.go:61; cmd_stop.go:351;
doctor_run_target_backfill.go:71; doctor_routed_to_checks.go:91;
doctor_work_option_metadata.go:92; doctor_order_tracking_retention.go:57;
doctor_backlog_depth.go:108, 114; doctor_session_model.go:164.

**internal/api tolerant/best-effort** (record a partial marker or skip a
scope; rows dropped, non-destructive; optional follow-up hardening only):
huma_handlers_convoys.go:545, 557, 686, 700, 730, 739;
handler_convoy_dispatch.go:127, 414; huma_handlers_orders.go:442, 445, 490;
orders_feed.go:79, 101, 135, 202, 224, 297, 317; handler_agents.go:332;
handler_mail.go:157 (caller at :133 swallows), 257 (mail resolution fails
closed — no misrouting); session_resolution.go:208 (reassignment aborts
whole, retried); huma_handlers_beads.go:595 → G4; handler_beads.go:367/420 →
G5; handler_convoy_dispatch.go:171 → G6; huma_handlers_convoys.go:90 → G7.

**Deferred (documented, not fixed here)**: internal/dispatch/ralph.go:815
(`resolveLogicalBeadID` `if err == nil` — falls to its metadata-ref fallback
on partial instead of consulting the survivor deps; non-destructive),
cmd/gc/providers.go:221, cmd/gc/city_runtime.go:1478,
cmd/gc/cmd_convoy_dispatch.go:1811/1822, cmd/gc/template_resolve.go:250,
internal/api IGNORED sites above.
Each degrades gracefully and converges; none makes a *worse* decision than
pre-fix.

**Bottom line: Group D is the 2-function router change plus exactly 8 caller
guards (10 lines-of-guard across 8 files); everything else is SAFE by the
contract argument in §6.1.**

## 7. TDD plan

Order of work (each step: write test, watch fail, implement, watch pass).
All coordrouter tests go in
`internal/coordrouter/router_federation_test.go`, reusing the existing
multi-backend harness from `TestRouterFederatesReadsAcrossBackends`
(router_federation_test.go:11–18): `work := beads.NewMemStore()`,
`graph := beads.NewMemStoreFrom(1000, nil, nil)`, `r := New(work)`,
`r.Register(coordclass.ClassGraph, graph)`. Add two file-local fakes in the
style of router_test.go:15–27 (`recordingStore` embeds `*beads.MemStore`):

```go
// failingReadStore embeds a MemStore and fails every federated read method.
type failingReadStore struct {
	*beads.MemStore
	err error
}
func (s *failingReadStore) List(beads.ListQuery) ([]beads.Bead, error)      { return nil, s.err }
func (s *failingReadStore) ListOpen(...string) ([]beads.Bead, error)        { return nil, s.err }
func (s *failingReadStore) Children(string, ...beads.QueryOpt) ([]beads.Bead, error) { return nil, s.err }
func (s *failingReadStore) ListByLabel(string, int, ...beads.QueryOpt) ([]beads.Bead, error) { return nil, s.err }
func (s *failingReadStore) ListByAssignee(string, string, int) ([]beads.Bead, error) { return nil, s.err }
func (s *failingReadStore) ListByMetadata(map[string]string, int, ...beads.QueryOpt) ([]beads.Bead, error) { return nil, s.err }
func (s *failingReadStore) Ready(...beads.ReadyQuery) ([]beads.Bead, error)  { return nil, s.err }
func (s *failingReadStore) DepList(string, string) ([]beads.Dep, error)      { return nil, s.err }
```

(MemStore has no `IDPrefix()` — verified — so `prefixBackendForID` returns
nil for it and the DepList federated loop is exercised, not the fast path.)

**Phase 1 — router (the fix proper), failing tests first:**

1. `TestRouterFederatedReadReturnsPartialWhenOneLegFails` — work MemStore
   with one created bead; graph = `&failingReadStore{MemStore:
   beads.NewMemStoreFrom(1000, nil, nil), err: errBoom}` registered as
   ClassGraph. Table-drive all seven surfaces (`List{AllowScan:true}`,
   `ListOpen()`, `Children(parent)`, `ListByLabel`, `ListByAssignee`,
   `ListByMetadata`, `Ready()`). Assert: returned rows contain the work bead;
   `err != nil`; `beads.IsPartialResult(err)`; `errors.Is(err, errBoom)`
   (Unwrap chain). **Fails today** (err is nil).
2. `TestRouterDepListReturnsPartialWhenOneLegFails` — DepAdd an edge in the
   work MemStore; graph leg failing. `r.DepList(id, "down")` → deps present +
   `IsPartialResult` + `errors.Is(err, errBoom)`. **Fails today.**
3. `TestRouterFederatedReadAllLegsFailedHardFails` — both legs
   `failingReadStore` → `(nil, err)`, `errors.Is(err, errBoom)`,
   `!beads.IsPartialResult(err)` (hard-fail path byte-preserved).
   **Passes today; pins the preserved path.**
4. `TestRouterFederatedReadMergesRowsFromPartialLeg` — a third fake
   (`partialReadStore` embedding MemStore) whose `List` returns
   `(rows, &beads.PartialResultError{Op: "fake", Err: errBoom})`; healthy
   other leg. Assert the union contains BOTH legs' rows and
   `IsPartialResult(err)`. **Fails today** (partial leg's rows dropped).
5. `TestRouterSingleBackendReadErrorPassesThroughUnwrapped` — identity phase:
   `New(&failingReadStore{...})` sole backend → `r.List` returns the bare
   `errBoom`, not a `PartialResultError` (byte-identical guarantee, §8).
   **Passes today; pins the fast path.**

Then replace the two bodies per §4; 1–5 green; existing
`TestRouterFederatesReadsAcrossBackends` / `TestRouterSingleBackendReadsDelegate`
/ `TestRouterListGraphOnly*` / conformance suites stay green (no
success-path change).

**Phase 2 — caller guards G1–G8, one failing test each (same PR, separate
commit; Phase 1 alone would regress these callers):**

- `TestFindSessionNameByTemplateToleratesPartialResult`
  (internal/agentutil/pool_test.go): fake store returning one matching
  session bead + `&beads.PartialResultError{}` → expect the session name, not
  `""`.
- `TestFindSessionNameByAgentLabelToleratesPartialResult` +
  `TestExactMetadataSessionCandidatesKeepsPartialRows`
  (cmd/gc/session_name_lookup_test.go, internal/session tests).
- `TestVerifyCanonicalBdScopeStoreReadyAcceptsPartial`
  (cmd/gc/beads_provider_lifecycle_test.go): store whose `List` returns
  `([]beads.Bead{…}, &beads.PartialResultError{…})` → nil (and: hard error
  still exhausts the retry loop — shrink the sleep via the existing test
  seam if one exists, else inject a 1-attempt variant).
- `TestHumaHandleBeadDepsServesPartial`, `TestCollectBeadGraphToleratesPartial`,
  `TestSnapshotFromStoreToleratesPartial`,
  `TestHumaHandleConvoyListPartialIsNotOutage` (internal/api tests — reuse
  the package's existing fake-store harness for the sibling guarded handlers,
  e.g. the fixtures around huma_handlers_beads partial tests).
- `TestResolveTaskWorkDirToleratesPartial` /
  `TestResolveTaskOptionOverridesToleratesPartial`
  (cmd/gc/session_reconciler tests): fake store returns the assigned bead
  with `work_dir`/`opt_*` metadata + partial error → resolution still lands.

**Phase 3 — end-to-end assertion of the finding itself (recommended):** a
cmd/gc test on `collectAssignedWorkBeadsWithStores` (or
`readyForControllerDemandQuery`) with a policy-wrapped two-leg Router whose
graph leg fails: assert `storePartial == true` — i.e. the drain-suppression
fail-safe engages through the real store stack. This is the test shape the
audit flagged as missing ("tests all pass because they use the wrong store
shape"); build it with `wrapStoreWithBeadPolicies(router, cfg)` around a
`coordrouter.New(work)` + failing ClassGraph leg.

Gates: `go build ./...`, `make test` (or `make test-fast-parallel`),
`go vet ./...`, with `export GOCACHE=/data/tmp/tmp.XgAejbMpvc` (never
`go clean -cache`).

## 8. Byte-identical guarantee for default cities (design question 5)

Two independent layers guarantee it:

1. **No Router at all**: a default (Dolt-only) city never constructs one —
   `routedPolicyStore` (cmd/gc/api_state.go:246–249) returns
   `wrapStoreWithBeadPolicies(workBackend, cfg)` directly unless
   `graphStoreSQLiteEnabled(cfg)`.
2. **Identity phase**: wherever a Router does exist with one distinct
   backend, every read method short-circuits through `soleBackend()`
   (router_federation.go:23–29) before `federateRead`/the DepList loop is
   reached — `List`:86, `ListOpen`:96, `Children`:107, `ListByLabel`:117,
   `ListByAssignee`:127, `ListByMetadata`:137, `Ready`:148, `DepList`:206.
   The fix touches only code *after* those returns.

So on the default path neither the rows nor the error can differ by a single
byte; pinned by test 5 and the existing
`TestRouterSingleBackendReadsDelegate`. The caller guards G1–G8 are likewise
inert on default cities: they only alter behavior when `IsPartialResult(err)`
is true, and (with the federation laundering removed) a single-store leg
already delivered that error class before this change.
