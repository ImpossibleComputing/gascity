# C4c — Clone integration + server-side atomic rollback (G14) design

**Status:** DESIGN (pre-code), grounded against `worktree-gc-remote` @ `7bc41a4a9`.
**Parents:** `C3-git-hardening-design.md` §1 (the `git.Clone` seam and staging-dir
contract), `G13-request-id-state-machine.md` §6 (drop-then-mark rollback + boot
sweep, the ordering contract this doc implements), `C2.4-api-repoint-design.md`
§1/§7 (the `mutateAndPoke` composition and the deferred R2/R6 findings this doc
closes). Provision core: `internal/rig/provision.go:25`; Deps:
`internal/rig/deps.go:25`.

**Scope.** Four things, one doc: (1) where the clone slots into
`internal/rig.Provision` and how the staging→rename shape works; (2) the
server-layer atomic-rollback wrapper (G14) around Provision — precisely what it
adds beyond Provision's own topology rollback; (3) the retry-poison fix C2.4 §7
R2 deferred to this machine; (4) the boot sweep that reconciles orphans before
serving. No code.

---

## 1. Clone integration in `internal/rig`

### 1.1 The Deps seam (refines C3 §1.3's sketch)

C3 sketched `CloneGitURL func(ctx, gitURL, stagingDst string, opts
git.CloneOptions) error` (`C3-git-hardening-design.md:116-121`). Two problems
with that literal shape: Provision has no `context.Context` to thread, and it
does not (and should not) know the staging path — the staging root is a
server-owned concern (`C3:91-98`). Pin the seam as:

```go
// CloneGitURL materializes req.GitURL at rigPath. nil = the caller does not
// support --git-url. The closure owns transport hardening (C3), staging, and
// the atomic rename; when it returns nil, rigPath exists and is a COMPLETE
// clone. Fatal on error (no partial rig — the caller's G14 wrapper cleans up).
CloneGitURL func(gitURL, rigPath string) error
```

added to `Deps` after `ProbeBranch` (`internal/rig/deps.go:53`), plus
`GitURL string` on `ProvisionRequest` (`deps.go:84-92`). `ctx`, `git.CloneOptions`
(AllowSSH/Depth/Branch/RecurseSubmodules), and the staging root are **captured in
the server's closure** — so `internal/rig` imports nothing new (not even
`internal/git`), strictly better than C3's "nothing beyond internal/git"
(`C3:125-127`). `validateDeps` (`deps.go:153-176`) does **not** require it —
nil-optional, like `ProbeBranch`.

`validateRequest` (`deps.go:135-149`) gains two rejections: `GitURL != "" &&
Adopt` (contradiction: `--adopt` requires an existing dir, `provision.go:461-463`;
a clone requires an absent one) and — checked at step 2.5, not here, because it
needs the stat — `GitURL != ""` with a nil `Deps.CloneGitURL` ⇒
`"rig: git_url is not supported by this caller"`.

### 1.2 Where the step slots: 2.5, between stat and everything else

Per C3 §1.3, the clone lands between C2.2 step 2 (`StatRigPath`,
`provision.go:66`) and the git-detect at step 3 (`provision.go:72`) — **not**
merely "before MkdirAll at step 10 (`provision.go:203`)", because step 3's
`.git` stat and step 7's branch probe must see the cloned content:

```
Step 2.5 (only when req.GitURL != ""):
  if deps.CloneGitURL == nil        → fatal "git_url is not supported by this caller"
  if rigPathExists                  → fatal "rig path %s already exists; --git-url requires a new path"
  err := deps.CloneGitURL(req.GitURL, rigPath)
  if err != nil                     → fatal (F, no partial rig; wrapper rolls back — §2)
  rigPathExists = true              // the rename materialized it
```

Everything downstream is untouched: step 3 sees the fresh `.git` so `hasGit`
is true and `ProbeBranch` resolves the default branch (`provision.go:71-78`);
step 10's `MkdirAll` is skipped (`rigPathExists` true, `provision.go:201-206`);
the fresh-add store guard (`provision.go:251-268`) sees whatever `.beads/` the
cloned repo carries — a repo that ships a beads store correctly demands
`--adopt`, same as a local add. The clone *materializes* the directory the rest
of Provision already knows how to consume (C3 §1.3's framing, kept).

### 1.3 Staging→rename: split across the lock boundary (closes C2.4 R6)

C3 §1.2 mandates "stage clones in a temp dir under a server-owned root, rename
on full success" (`C3:91-98`) but §1.3 needs the content visible at `rigPath`
*during* Provision — and C2.4 §7 R6 mandates the clone stage stay **off the
config-write lock** (`C2.4:433-442`; Provision runs inside
`SerializeConfigWrite` + `mutateAndPoke`, `C2.4:96-108`). All three reconcile by
splitting the closure's work at the lock boundary:

1. **Pre-lock, in the provisioning goroutine (network, slow):**
   `git.Clone(ctx, url, stagingDst, opts)` into
   `<cityPath>/.gc/provision/<request_id>/clone/` — the server-owned staging
   root, per-request_id (C3 §1.2 item 1-2). The SSRF fence already ran at
   admission, before the 202 (`C3:322-328`). A clone failure here never enters
   Provision at all: the wrapper goes straight to rollback (§2.4).
2. **Inside the lock, as Provision step 2.5 (local, fast):** the injected
   `CloneGitURL` closure performs only `os.Rename(stagingDst, rigPath)` —
   atomic same-filesystem move (the staging root lives under `cityPath`
   precisely so the rename cannot cross filesystems). "Rename on full success"
   becomes "rename on *complete clone*": `rigPath` is only ever populated with
   a finished clone, never a partial one — a killed clone strands debris in
   staging (reclaimed by the boot sweep, §4), never at the final path. Removal
   of a *renamed* clone on later provision failure is the wrapper's job (§2),
   which preserves the G14 atomicity guarantee end to end.

The seam contract from `internal/rig`'s view is unchanged either way:
"populate rigPath from GitURL, completely or not at all." A future CLI
`--git-url` could pass a closure that clones directly; the split is a server
implementation detail behind the same seam.

### 1.4 The CLI stays byte-identical

`doRigAddWithResult` (`cmd/gc/cmd_rig.go:215`) passes `CloneGitURL: nil` and
never sets `req.GitURL`, so step 2.5 is skipped entirely (both gates false).
Zero behavior change for all 62 `TestDoRigAdd*` funcs and the G12 parity test
(`C2.4:310-358`). The only diff visible to the CLI path is two new inert struct
fields.

---

## 2. The server-layer atomic rollback (G14) wrapping Provision

### 2.1 What Provision's OWN rollback covers — and what it cannot

Provision's rollback is a **topology-file snapshot** scoped to the guarded
write window: `SnapshotTopologyFiles` at step 14 (`provision.go:350-352`),
`rollbackError` restores it on config/packs.lock/routes failure
(`provision.go:376,389,400,406`; helper at `:478-483`), and the panic guard
covers the same window until `committed = true` (`provision.go:356-368,408`).
It restores `city.toml`, `site.toml`, `packs.lock`, routes files. It does
**not** — and by design cannot (`C3:126-128`: "the atomic-rollback wrapper
[lives] in the server orchestration layer, not inside internal/rig") — undo:

- the **renamed clone dir** at `rigPath` (created at step 2.5) or the
  `MkdirAll` dir (step 10);
- the **rig `.beads/` store files** written by `InitStore`/`InitAndHook` at
  step 13 (`provision.go:315-346`) — created *before* the snapshot exists;
- the **rig's Dolt database** minted by bd init under managed Dolt (server-side
  SQL state, not a file under the topology snapshot at all);
- any failure at steps 2.5-13, which return plain errors with **no** restore
  (nothing config-side to restore — but the dir/store/DB persist);
- the idempotency record transition and the terminal event (G13's domain).

### 2.2 What the wrapper adds: a created-vs-preexisting manifest

The provisioning goroutine builds a manifest **before any mutation**, and
extends it **immediately before** each resource is created (record-then-create,
so a crash can never leave an unmanifested resource):

| Manifest entry | Set when | Ground truth |
|---|---|---|
| `staging_dir` | always, at goroutine start | `<cityPath>/.gc/provision/<request_id>/` |
| `created_dir` | before step-2.5 rename (git_url ⇒ always, since `rigPathExists` must be false) or, non-git_url API adds, iff `StatRigPath` reported absent | `provision.go:66,458-473` |
| `created_beads` | before Provision runs, iff fresh add (`!reAdd && !Adopt`) and no store present — exactly the condition the fresh-add guard enforces (`provision.go:251-268`) | a preexisting/adopted store is NEVER ours to remove |
| `dolt_db` | after step 13, read from `<rigPath>/.beads/metadata.json` `dolt_database` (`cmd/gc/beads_provider_lifecycle.go:455,1425`), only when `created_beads` and the city runs the managed-Dolt lifecycle | never set on re-add/adopt |

**The manifest is persisted into the durable idempotency record** as metadata
keys (`gc.idem.created_dir`, `gc.idem.created_beads`, `gc.idem.dolt_db`) via
`SetMetadataBatch` (`internal/beads/beads.go:398`) at the moments above — this
is what lets the boot sweep (§4) and the retry-poison path (§3) reconstruct
what to drop with no in-memory state. Absent-request_id provisions keep the
manifest in-memory only (no durable record exists, G13 §1); their staging debris
is covered by the §4 wholesale staging-root sweep.

### 2.3 Rollback ordering (G13 §6 drop-then-mark, runtime path)

On any failure — clone failure pre-lock, Provision error, non-nil
`PostProvisionErr` (`deps.go:104-108`, per C2.4 R6), or `mutateAndPoke` refresh
failure — the wrapper runs **after** Provision's own topology rollback has
already executed inside Provision (composition per C2.4 §1: mutate-error ⇒ no
`mutateAndPoke` restore, `api_state.go:1762-1764`; refresh-error ⇒
`mutateAndPoke` restores `city.toml`/`site.toml`, `:1766-1773`):

1. **Drop the renamed clone dir**: if `created_dir`, `os.RemoveAll(rigPath)`
   (whole-dir removal subsumes `.beads`); else if only `created_beads`, remove
   just the store files under `<rigPath>/.beads/` (metadata.json, config.yaml,
   .env — the set `beadsDirContainsStore` recognizes, `provision.go:258`),
   never user content in a preexisting dir.
2. **Drop the rig Dolt DB**: iff `dolt_db` is set, via the existing
   identifier-escaped drop primitive (`sqlCleanupDoltClient.DropDatabase`,
   `cmd/gc/dolt_cleanup_drop.go:208-214`, behind the `doltCleanupClient`
   interface at `:20` with its per-drop timeout `:36`) — reuse, do not
   hand-roll SQL. Never dropped on re-add/adopt (the manifest can't say so).
3. **Repair routes for the refresh-failure case only** (closes C2.4 R2):
   `mutateAndPoke`'s snapshot excludes `routes.jsonl` (its capture set is
   `city.toml` + `site.toml` + agent scaffolds, `api_state.go:1642-1727`), and
   once Provision `committed` its routes write it won't restore either. So when
   the failure is refresh-after-Provision-success, regenerate routes from the
   now-restored on-disk config (`writeAllRigRoutes(collectRigRoutes(...))`,
   `cmd/gc/rig_beads.go:48,95` — the same funcs `Deps.WriteRoutes` wraps,
   `C2.4:205`). Mid-Provision failures need no repair (Provision's snapshot
   covered routes).
4. **Remove the staging dir** `<cityPath>/.gc/provision/<request_id>/`
   (empty post-rename; holds the partial clone when the clone itself failed).
5. **`SetMetadataBatch(state=rolled_back)`** — only now, with disk fully
   clean: the G13 §6 invariant ("a record never reaches rolled_back until the
   partial clone dir / rig DB / any config registration is fully removed",
   `G13-request-id-state-machine.md:322-324`), because a same-digest retry is
   admitted the instant this state lands (§4.2 re-clone) and must never race
   an unfinished teardown.
6. **Remove the live-index entries** (`inflight`/`byName`, G13 §3.5) and close
   `done`; **emit terminal `request.failed`** carrying `request_id` (G20).

Steps 1-4 are per-resource idempotent (`RemoveAll` on absent path,
`DROP DATABASE` guarded by existence / IF EXISTS) so a crash mid-rollback
re-runs cleanly under §4. If any drop step fails, the record **stays
`in_flight`** (never mark with debris on disk); the failure is logged + evented,
and the boot sweep or the §3 retry pre-drop completes the teardown.

### 2.4 Success terminal (for contrast)

Only after `mutateAndPoke` returns nil — which per C2.4 §1 is precisely the G17
barrier (`Config()` shows the rig and `BeadStore(rigName) != nil`,
`C2.4:127-143`) — the wrapper writes durable `succeeded` (+ result keys),
removes the now-empty staging dir, drops the live entries, and emits the
success event. Ordering pinned by G13 §3.5 ("terminal — success: ORDER IS
LOAD-BEARING", `G13:198-204`).

### 2.5 Answer to "what exactly does the server layer add?"

Provision rolls back the **topology files it wrote in its guarded window**.
The wrapper adds the other five axes: (a) the cloned/created **rig directory**,
(b) the **rig `.beads` store**, (c) the **managed-Dolt database**, (d) the
**routes repair** for the refresh-failure orphan (R2), and (e) the
**idempotency-record + live-index + event** state machine transitions — all
sequenced drop-then-mark so retries and crash recovery are safe. Neither layer
duplicates the other; the composition is the same "one restorer per failure
class" argument C2.4 §1 made for `mutateAndPoke`.

---

## 3. The retry-poison fix (C2.4 §7 R2, deferred to here)

**The wedge.** A prior attempt initialized `.beads/` at `rigPath` (step 13) and
then failed or crashed in a way that left the store behind — e.g. a crash
between store init and rollback, a partially-failed drop (§2.3 failure arm), or
the pre-C4 refresh-orphan R2 explicitly deferred as "a retry-poisoning store"
(`C2.4-api-repoint-design.md:405-413`). The record is (or gets swept to)
`rolled_back`; a same-digest retry is admitted as re-clone (G13 §4.2)… and then
step 2.5 fails (`rigPath` already exists) or, for non-git_url adds, the
fresh-add guard kills it: *"`.beads` already contains a beads store; use
--adopt … or remove"* (`provision.go:251-268`). Every retry fails identically.
Permanent wedge — Decision 9's "a re-run is a clean re-clone" broken.

**The fix — pre-admission drop on the re-clone path.** When admission resolves
to **re-clone** (durable `state=rolled_back`, digest match, no live entry —
G13 §4.2 row 4), the wrapper runs a **poison pre-drop** before resetting the
record to `in_flight` and spawning the goroutine:

1. Read the persisted manifest keys off the rolled_back record
   (`gc.idem.created_dir` / `created_beads` / `dolt_db`, §2.2).
2. Re-run §2.3 steps 1-2-4 idempotently: remove the created dir (or created
   store files), drop the manifested Dolt DB, clear the old staging dir. A
   fully-clean prior rollback makes this a no-op; only the poison case bites.
3. **Safety rail:** if debris exists at `rigPath` but the record carries **no**
   manifest keys claiming it (legacy record, or a preexisting store the attempt
   adopted), do **not** delete — fail the retry with a typed 409
   `rig_path_poisoned` telling the operator what to remove. Never delete user
   data the machine cannot prove it created.

Then proceed exactly as G13 §4.2: reset durable to `in_flight` with a fresh
`event_cursor`, clear the stale manifest keys (overwrite with `""` —
`SetMetadataBatch` merges, no delete primitive, `G13:159-163`), new live entry,
202. The pre-drop runs under the same per-rig-name admission lock (G13 §7) so
it cannot race a concurrent same-name admission.

---

## 4. The boot sweep — reconcile before serving

Runs at controller startup, **strictly before the rig-create/sling handlers are
admitted to serve** (normative per `G13:333-337`), anchored where
`controllerState` is constructed (`newControllerState`, `cmd/gc/api_state.go:119`)
ahead of API accept. The live index starts empty (G13 §3.5 boot rule), so every
durable `in_flight` record is an orphan.

1. **Find orphans:** `Store.List` by the coarse labels
   `gc-idem`/`gc-idem-rig-create` (their sole purpose, `G13:125-128`), filter
   `gc.idem.state == "in_flight"`, `IncludeClosed: true`.
2. **Per orphan — completeness probe first.** A crash *after* Provision
   committed + refresh succeeded but *before* the durable `succeeded` write
   (the §2.4 window) leaves a **fully-provisioned rig** under an `in_flight`
   record. Blindly applying G13 §6's "drop any partial dir/DB/config" would
   destroy a live rig that sessions may already be hooked to. So: if the
   orphan's `rig_name` is present in the loaded city config **and** its store
   is structurally valid (`.beads/metadata.json` + `config.yaml` present — the
   same completeness `beadsDirContainsStore`/`ReadBeadsPrefix` recognize,
   `provision.go:210-216,258`), reconcile **forward**:
   `SetMetadataBatch(state=succeeded)` + result keys from config. This is the
   one place C4c refines G13 §6's letter ("drop any **partial** dir" — a
   complete rig is not partial) and needs a one-line G13 amendment
   acknowledging the probe. Everything else is drop-then-mark:
3. **Drop-then-mark for genuinely partial orphans:** run §2.3 steps 1-4 from
   the record's persisted manifest keys (drop dir → drop DB → repair routes
   from the on-disk config → drop staging), **then** step 5's
   `SetMetadataBatch(state=rolled_back)`. Never mark first: a same-id retry
   arriving the moment serving opens must find clean ground (`G13:322-337`).
   If the record has a rig in `city.toml` but a broken/absent store (crash
   inside the committed window before refresh), remove the config entry via the
   config-write path + routes regen as part of "config registration … removed"
   (`G13:322`), then mark.
4. **Wholesale staging-root sweep:** after all records reconcile, remove every
   remaining `<cityPath>/.gc/provision/*/` dir. No provision is live at boot,
   so anything left is an orphan by definition — including staging from
   absent-request_id (synthetic-id) provisions that have no durable record.

The sweep is idempotent (re-crash mid-sweep re-runs it) and its per-record
failure mode is "leave `in_flight`, log, keep serving-gate closed for that
record only if teardown failed" — a record that cannot be cleaned stays
un-retryable (the §3 safety rail) rather than silently poisonous.

---

## 5. Test matrix (delta over C3 §6 / G13 §11)

- **Step 2.5 gating:** `GitURL` set + nil `CloneGitURL` ⇒ fatal, no steps run;
  `GitURL` + `Adopt` ⇒ `validateRequest` error; `GitURL` + existing `rigPath`
  ⇒ fatal before the closure is called (assert a counting fake).
- **CLI byte-parity:** existing `TestDoRigAdd*` + the G12 parity test untouched
  (nil seam, empty GitURL) — the tripwire that step 2.5 is truly inert.
- **Clone-fail rollback (pre-lock):** failing `git.Clone` ⇒ Provision never
  invoked, staging removed, record `rolled_back`, `request.failed` emitted
  (extends C3 §6.6).
- **Post-clone Provision-fail:** fake `InitStore` error after a successful
  rename ⇒ renamed dir removed, manifested DB dropped (fake cleanup client
  records the name), staging gone, **then** `rolled_back` (assert write order
  via an instrumented store — mirrors G13 §11 "runtime rollback order").
- **Refresh-fail (R2 closure):** Provision succeeds, refresh fails ⇒
  `city.toml` restored by `mutateAndPoke`, wrapper removes dir/store/DB,
  routes regenerated to pre-add content (the assertion
  `TestControllerStateMutationRollsBackWhenRefreshFails` never made,
  `C2.4:294`).
- **Retry-poison:** seed a `rolled_back` record with manifest keys + a
  leftover `.beads` store ⇒ same-id retry pre-drops and re-clones green;
  same store **without** manifest keys ⇒ 409 `rig_path_poisoned`, store intact.
- **Boot sweep:** (a) partial orphan ⇒ drop-then-mark order asserted;
  (b) complete-rig orphan ⇒ reconciled to `succeeded`, rig untouched;
  (c) staging-root cleared wholesale; (d) handlers refuse until sweep done.
- **Dolt-drop safety:** re-add/adopt failure paths ⇒ `DropDatabase` never
  called (fake client asserts zero calls).

---

## 6. Risks

- **R1 — the completeness probe (§4.2) is the load-bearing judgment.** It
  refines G13 §6's literal "drop any partial dir/DB/config for its rig_name":
  mis-classifying a half-provisioned rig as complete resurrects a broken rig;
  mis-classifying a complete one as partial destroys a live rig post-crash.
  Mitigation: the probe requires config presence AND store validity, and G13
  gets an explicit amendment; the sweep tests pin both directions.
- **R2 — first destructive `DROP DATABASE` on the provisioning path.** A
  manifest-attribution bug (recording `dolt_db` for a store the attempt did not
  create, e.g. a re-add against an existing `dolt_database` in metadata.json)
  deletes user data. Mitigation: `dolt_db` is only manifested when
  `created_beads` is (fresh add, no prior store — the `provision.go:251-268`
  guard's exact condition), record-then-create ordering, the §3 no-manifest
  safety rail, and the zero-calls test on re-add/adopt paths.
- **R3 — clone runs outside the config-write lock (§1.3),** so during a long
  WAN clone the rig name is protected only by the live `byName` entry (G13
  §3.5/§4.4), not a held lock. That is G13's designed shape, but the wrapper
  must not accidentally release the live entry before the terminal step.
- **R4 — partial-rollback wedges are surfaced, not hidden:** a failed drop
  leaves the record `in_flight` (never `rolled_back` with debris), making the
  request un-retryable until the sweep or an operator completes teardown —
  correct per the G13 §6 invariant, but a new operator-visible failure mode
  for the G23 runbook.
