# C4a — Concrete Go design: `request_id` state machine + live index

**Status:** DESIGN (C4a). Implements G13 §2/§3/§3.5/§4 (`G13-request-id-state-machine.md`).
**Grounded against HEAD** (`worktree-gc-remote`); every anchor cited as `file:line` was read.

---

## 1. Component placement

A new file **`internal/api/rigidem.go`** holding a self-contained `rigIdemIndex`
component, mirroring the existing process-local dedup precedent
`idempotencyCache` (`internal/api/idempotency.go:30` — own file, own `sync.Mutex`,
no back-reference to `Server`). It hangs off `Server` exactly the way `idem` does:

- `Server` struct gains one field next to `idem *idempotencyCache`
  (`internal/api/server.go:61-62`):

  ```go
  // rigIdem is the in-process live index for rig-create request_id
  // idempotency (G13 §3.5). Authoritative for admission decisions;
  // the durable bead record (rigidem.go) backs crash recovery.
  rigIdem *rigIdemIndex
  ```

- Constructed in `NewServer` beside `idem: newIdempotencyCache(...)`
  (`internal/api/server.go:213`): `rigIdem: newRigIdemIndex(),`.

The durable-record CRUD helpers also live in `rigidem.go` as functions taking a
`beads.Store` (obtained per-request via `s.state.CityBeadStore()`,
`internal/api/state.go:107-109` — the city ledger, per G13 §3.1). Nothing in
`internal/beads` changes.

Admission is a `Server` method (it needs `s.state.Config()`,
`s.state.BeadStore(name)` (`state.go:66-68`), `s.currentCityEventCursor()`
(`internal/api/request_id.go:22`), and `s.rigIdem`), invoked from the rig-create
handler **inside the per-rig-name `sourceworkflow.WithLock`** (G13 §7;
`internal/sourceworkflow/sourceworkflow.go:228`, empty-key no-op gotcha at `:230`).

## 2. Live-index types (G13 §3.5)

```go
// internal/api/rigidem.go
package api

// idemKey identifies one logical request: (city, request_id). G13 §0.
type idemKey struct {
	city      string
	requestID string
}

// nameKey is the second dedupe axis: (city, rig name). G13 §4.4.
type nameKey struct {
	city string
	rig  string
}

// liveProvision is one currently-running async rig provision. A single
// value is shared (by pointer) between inflight and byName so terminal
// removal is atomic across both axes and both see the same done channel.
type liveProvision struct {
	requestID   string        // client-supplied, or synthetic newRequestID() (G13 §1)
	digest      string        // hex sha256 of the zeroed body, §3 below
	eventCursor string        // decimal seq captured BEFORE the goroutine (G13 §5)
	rigName     string
	beadID      string        // durable record ID; "" when synthetic (no dedup record)
	synthetic   bool          // true when the client sent no request_id
	done        chan struct{} // closed exactly once at terminal (success or rollback)
}

// rigIdemIndex is the in-process live index (G13 §3.5): authoritative for
// admission, holds ONLY currently-running provisions, starts empty at boot,
// never rebuilt from durable records. Single-replica by accepted constraint
// (G13 §12). Mirrors the idempotencyCache shape (idempotency.go:30) but is
// NOT TTL/cap-evicted: entries are removed only by their goroutine's
// terminal step — the same "pending entries are NEVER evicted" rule
// idempotency.go:80-84 pins, for the same double-execute reason.
type rigIdemIndex struct {
	mu       sync.Mutex
	inflight map[idemKey]*liveProvision
	byName   map[nameKey]*liveProvision
}

func newRigIdemIndex() *rigIdemIndex {
	return &rigIdemIndex{
		inflight: make(map[idemKey]*liveProvision),
		byName:   make(map[nameKey]*liveProvision),
	}
}
```

Registration and removal (removal implements the terminal ordering of G13
§3.5/§6; the pointer-identity guard prevents a stale terminal from evicting a
successor's `byName` entry after a re-clone reused the name):

```go
// register inserts entry under both keys. Caller holds the per-rig-name
// admission lock; register asserts neither key is occupied (programming
// error otherwise — admission already consulted the index under the
// same lock).
func (x *rigIdemIndex) register(city string, e *liveProvision) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.inflight[idemKey{city, e.requestID}] = e
	x.byName[nameKey{city, e.rigName}] = e
}

// remove drops entry from both maps and closes done. Called ONLY as the
// provision goroutine's terminal step — after the durable succeeded write
// + G17 visibility barrier on success, after drop-then-mark on rollback.
func (x *rigIdemIndex) remove(city string, e *liveProvision) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if cur := x.inflight[idemKey{city, e.requestID}]; cur == e {
		delete(x.inflight, idemKey{city, e.requestID})
	}
	if cur := x.byName[nameKey{city, e.rigName}]; cur == e {
		delete(x.byName, nameKey{city, e.rigName})
	}
	close(e.done)
}

// lookup / lookupByName are plain locked map reads returning (*liveProvision, bool).
```

`done` exists for the G13 §3.5 liveness watchdog (C4 SHOULD) and for tests
(`§11: assert no second goroutine`); nothing in the admission path blocks on it.

## 3. Digest computation (G13 §3.3) — exact

Prerequisite: C6 promotes the anonymous `RigCreateInput.Body` struct
(`internal/api/huma_types_rigs.go:24-32`, currently
`Name/Path/Prefix/DefaultBranch`) to a **named type** and adds the two new
fields, so the digest function can take it by value:

```go
// RigCreateBody is the POST /v0/city/{cityName}/rigs body.
// FIELD ORDER IS LOAD-BEARING: json.Marshal emits struct fields in
// declaration order and the §3 digest is computed over that encoding.
// Append new fields at the end; never reorder (golden test below).
type RigCreateBody struct {
	Name          string `json:"name" doc:"Rig name." minLength:"1"`
	Path          string `json:"path" doc:"Filesystem path." minLength:"1"`
	Prefix        string `json:"prefix,omitempty" doc:"Session name prefix."`
	DefaultBranch string `json:"default_branch,omitempty" ...`
	GitURL        string `json:"git_url,omitempty" ...`        // G14/Group C
	RequestID     string `json:"request_id,omitempty" ...`     // G13/Decision 6
}

type RigCreateInput struct {
	CityScope
	Body RigCreateBody
}
```

The digest — sha256 over the JSON encoding of the body **with `RequestID`
zeroed** (copy-by-value; the request is never mutated). Because `request_id`
carries `omitempty`, zeroing removes the key from the encoding entirely, so the
digest covers exactly the provisioning-relevant fields:

```go
// rigCreateDigest returns hex(sha256(json.Marshal(body with RequestID
// zeroed))) — G13 §3.3. Deterministic: encoding/json emits struct fields
// in declaration order (no maps in RigCreateBody). Distinct from
// citywriteauth.ReqDigest (citywriteauth.go:279), which digests
// method\npath[\nquery]\nhex(sha256(body)) to bind a write-auth GRANT to
// one HTTP request; this digest binds a request_id to one logical BODY.
// Do not conflate or reuse.
func rigCreateDigest(body RigCreateBody) (string, error) {
	body.RequestID = ""
	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("digesting rig-create body: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
```

Sling's variant (G13 §9) is the same function shape over `SlingInput.Body`;
its `vars` map is safe because `encoding/json` sorts map keys.

**Golden test (new, beyond G13 §11):** pin one literal body → one literal hex
digest. Any field reorder / tag change / omitempty flip in `RigCreateBody`
breaks the golden and is caught at build time — a silent digest change turns
every in-flight retry across a deploy into a spurious 409 body-mismatch.

## 4. `request_id` validation (G13 §2) — exact

Runs at the handler edge, before any lock, index, or store access:

```go
var (
	// G13 §2: opaque client id, safe for the digest preimage, the DoltLite
	// metadata JSON column, and the bd --metadata-field filter. The charset
	// excludes control chars, whitespace, and `"` by construction.
	requestIDCharset = regexp.MustCompile(`^[A-Za-z0-9._~:-]{8,200}$`)
)

// errInvalidRequestID renders as the 400 typed Huma error
// "invalid_request_id" (never 500, never a silently-minted substitute).
func validateRequestID(id string) error {
	if !requestIDCharset.MatchString(id) {
		return errInvalidRequestID
	}
	// bd's --metadata-field equality filter type-infers its value
	// (internal/beads/bdstore.go:2159): a value parseable as a JSON
	// number/bool/null is compared as that type and then never matches the
	// JSON-string-stored metadata — the (city,request_id) lookup MISSES and
	// the request re-clones (Decision 6 broken). json.Valid over the raw id
	// is the exact guard: it accepts precisely the literals a JSON parser
	// would infer (12345678, -42, 1.5, 1e5, true, false, null) and rejects
	// every id containing a letter run / dash-in-the-middle / uuid. This
	// SUBSUMES the G13 §2 regex pair (`at least one non-digit` +
	// `^(true|false|null|-?\d+(\.\d+)?)$`) and additionally covers the
	// exponent forms (1e5, 1E5) the spec regex misses.
	if json.Valid([]byte(id)) {
		return errInvalidRequestID
	}
	return nil
}
```

(`"..."` string literals cannot arise: `"` is outside the charset. A UUIDv4
passes trivially — the recommended client id.)

The **rig name** gets the same treatment before it is used as a metadata filter
value or lock key: Huma already enforces non-empty (`minLength:"1"`,
`huma_types_rigs.go:27` — load-bearing for the `WithLock` empty-key no-op,
`sourceworkflow.go:230`), and the handler additionally applies the same
`json.Valid` non-inferable guard (a purely-numeric rig name hits the identical
bd filter foot-gun on the §6-below `gc.idem.rig_name` scan).

## 5. Durable bead record CRUD (G13 §3.2/§3.4) — grounded

Store: `store := s.state.CityBeadStore()` (`internal/api/state.go:107-109`).
All metadata keys as constants (`gcIdemKind = "gc.idem.kind"`, etc.).

### 5.1 Create (reserve)

Signature: `Create(b Bead) (Bead, error)` (`internal/beads/beads.go:337-341`);
`Bead.Metadata` is `beads.StringMap` — underlying `map[string]string`
(`beads.go:74`). Durable `TierIssues` by default (`Ephemeral`/`NoHistory` left
false, `beads.go:80-83`) as G13 §3.1 requires.

```go
func createIdemRecord(store beads.Store, city, requestID, digest, cursor, rigName, state string) (string, error) {
	var id string
	// Tx (beads.go:421): atomic on NativeDoltStore (AtomicTx()==true,
	// native_dolt_store.go:978); sequential on BdStore/exec — both writes
	// below are individually idempotent so a partial apply is recoverable.
	err := store.Tx("gc: idem reserve rig-create "+requestID, func(tx beads.Tx) error {
		rec, err := tx.Create(beads.Bead{
			Type:   "task", // legal type, NEVER a new issue_type (G13 §3)
			Title:  "idem: rig-create " + requestID,
			Labels: []string{"gc-idem", "gc-idem-rig-create"}, // §6 boot-sweep markers ONLY
			Metadata: beads.StringMap{
				"gc.idem.kind":         "rig-create",
				"gc.idem.city":         city,
				"gc.idem.request_id":   requestID, // §4-validated, non-inferable
				"gc.idem.digest":       digest,
				"gc.idem.state":        state, // "in_flight" async; "succeeded" on the sync-201 path (G13 §8)
				"gc.idem.event_cursor": cursor,
				"gc.idem.rig_name":     rigName,
			},
		})
		if err != nil {
			return err
		}
		id = rec.ID
		// Close immediately: bead STATUS is not part of the machine
		// (gc.idem.state is). An OPEN "task" bead is Ready()-eligible
		// actionable work — "task" is absent from readyExcludeTypes
		// (beads.go:206-217) and "gc-idem" is not a ready-excluded label
		// (beads.go:277-285) — so an open record could be claimed/slung by
		// the dispatcher. Closed records stay out of every open/ready view;
		// all machine lookups use IncludeClosed:true anyway.
		return tx.Close(rec.ID) // Tx.Close: beads.go:143
	})
	return id, err
}
```

### 5.2 Lookup `(city, request_id)`

Uses `Store.List(ListQuery)` (`beads.go:364-366`) — the preferred surface
(`beads.go:368` marks `ListByMetadata` at `beads.go:399-402` legacy; that
helper with the `IncludeClosed` `QueryOpt` (`beads.go:311-313`) is the
behavioral equivalent, not used here). A metadata filter alone satisfies
`HasFilter` (`internal/beads/query.go:133-143`, `len(q.Metadata) > 0`), so no
`AllowScan`.

```go
func lookupIdemRecord(store beads.Store, city, requestID string) (*beads.Bead, error) {
	matches, err := store.List(beads.ListQuery{ // conjunctive AND equality (query.go:64-66)
		Metadata: map[string]string{ // ListQuery.Metadata: query.go:80
			"gc.idem.kind":       "rig-create",
			"gc.idem.city":       city,
			"gc.idem.request_id": requestID,
		},
		IncludeClosed: true, // MANDATORY (query.go:87): records are Closed at create (§5.1)
		Limit:         2,    // query.go:86; >1 result = invariant violation
	})
	if err != nil {
		return nil, fmt.Errorf("idem lookup %s/%s: %w", city, requestID, err)
	}
	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return &matches[0], nil
	default:
		return nil, fmt.Errorf("idem invariant: %d records for (%s,%s)", len(matches), city, requestID)
	}
}
```

The `gc.idem.rig_name` backstop scan (G13 §4.4, third bullet) is the same
`List` with `Metadata: {"gc.idem.kind": "rig-create", "gc.idem.city": city,
"gc.idem.rig_name": rigName}`, `IncludeClosed: true`, then a Go-side filter for
`gc.idem.state ∈ {in_flight, succeeded}`.

### 5.3 Transitions

`SetMetadataBatch(id string, kvs map[string]string) error` (`beads.go:408-414`)
— read-modify-**merge** on the single metadata column; no key delete exists, so
every transition is an overwrite of `gc.idem.state` plus additive keys. On
BdStore/exec the batch applies sequentially (`beads.go:410-412`) — each map
below is safe under partial application (state key last-writer-wins; result
keys additive and re-writable).

```go
// success — ONLY after the G17 visibility barrier: s.state.Config() shows
// the rig AND s.state.BeadStore(rigName) != nil (state.go:66-68). Then:
store.SetMetadataBatch(beadID, map[string]string{
	"gc.idem.state":         "succeeded",
	"gc.idem.result.rig":    rigName,
	"gc.idem.result.prefix": prefix,
	"gc.idem.result.branch": defaultBranch,
})
// ...then rigIdem.remove(city, entry), then the success event. Order per G13 §3.5.

// rollback — ONLY after dir/DB/config for gc.idem.rig_name are fully
// removed (drop-then-mark, G13 §6):
store.SetMetadataBatch(beadID, map[string]string{"gc.idem.state": "rolled_back"})
// ...then rigIdem.remove(city, entry), then the request.failed event.

// re-clone reset (admission path, §6 below) — fresh cursor per G13 §4.2/§5:
store.SetMetadataBatch(beadID, map[string]string{
	"gc.idem.state":        "in_flight",
	"gc.idem.event_cursor": freshCursor,
})
```

## 6. The admission function (G13 §4) — decision table + control flow

```go
type rigAdmitOutcome int

const (
	rigAdmitNew            rigAdmitOutcome = iota // 202: spawn goroutine
	rigAdmitInflightReplay                        // 202: live entry's cursor, NO spawn
	rigAdmitExisting                              // 200: served from durable record
	rigAdmitReclone                               // 202: durable reset, spawn
)

type rigAdmitResult struct {
	outcome     rigAdmitOutcome
	requestID   string // echoed verbatim (client's, or synthetic)
	eventCursor string
	entry       *liveProvision // non-nil for New/Reclone (the caller spawns with it)
	record      *beads.Bead    // non-nil for Existing (result fields from metadata)
}
```

Decision table (exact evaluation order; "id" = client `request_id` after §4
validation; "live" = `inflight[{city,id}]`; "rec" = durable lookup §5.2):

| # | id present | live entry | rec state | digest vs stored | Outcome | HTTP |
|---|---|---|---|---|---|---|
| 1 | yes | **hit** | — | equal | in-flight replay (live cursor) | **202** |
| 2 | yes | **hit** | — | differs | `request_id_conflict` | **409** |
| 3 | yes | miss | none | — | fall to name axis → new | **202** |
| 4 | yes | miss | any | differs | `request_id_conflict` (all states, incl. rolled_back — G13 §4.3) | **409** |
| 5 | yes | miss | `succeeded` | equal | existing (result from metadata) | **200** |
| 6 | yes | miss | `rolled_back` | equal | re-clone (reset + new live entry) | **202** |
| 7 | yes | miss | `in_flight` (orphan) | equal | inline drop-then-mark for its rig_name, then row 6 | **202** |
| 8 | any | name axis: `byName` hit under a DIFFERENT id, or rig in `s.state.Config()`, or §5.2 rig_name scan hits `in_flight`/`succeeded` | | | `rig_name_conflict` (ext: in-flight id + cursor when live) | **409** |
| 9 | no | (rows 1–7 skipped) | — | — | name axis → new, **synthetic id, no durable record** | **202** |

Control flow (the entire function runs inside `sourceworkflow.WithLock(rigName,
...)` — G13 §7; the index's own `mu` is nested within and never held across a
store call... except it is simpler and correct here to hold **only** the
per-rig-name lock for the whole admission and use the index mutex per-map-op,
because same-name contention is already serialized by `WithLock` and same-id
implies same-name):

```go
func (s *Server) admitRigCreate(city string, body RigCreateBody) (rigAdmitResult, error) {
	// (0) Validation already done at the handler edge: rig name non-empty +
	//     non-inferable; request_id absent or §4-valid. NEVER reached with
	//     an invalid supplied id.
	digest, err := rigCreateDigest(body) // §3
	if err != nil { return rigAdmitResult{}, err }
	store := s.state.CityBeadStore()

	// (1) request_id axis — LIVE INDEX FIRST (G13 §3.5: strong consistency;
	//     the durable store is consulted only for keys the index does not
	//     hold, where ledger read-after-write lag is irrelevant).
	if body.RequestID != "" {
		if live, ok := s.rigIdem.lookup(city, body.RequestID); ok {
			if live.digest != digest { return ..., errRequestIDConflict(...) }   // row 2
			return rigAdmitResult{rigAdmitInflightReplay, body.RequestID,
				live.eventCursor, nil, nil}, nil                                 // row 1
		}
		rec, err := lookupIdemRecord(store, city, body.RequestID) // §5.2
		if err != nil { return rigAdmitResult{}, err }
		if rec != nil {
			if rec.Metadata["gc.idem.digest"] != digest {
				return ..., errRequestIDConflict(...)                            // row 4
			}
			switch rec.Metadata["gc.idem.state"] {
			case "succeeded":
				return rigAdmitResult{rigAdmitExisting, body.RequestID,
					rec.Metadata["gc.idem.event_cursor"], nil, rec}, nil         // row 5
			case "in_flight": // orphan: live index missed (G13 §4.1 key rule).
				// Post-boot this is a goroutine-loss leak, not a crash
				// survivor (§6 sweep runs before serve). NEVER a passive
				// replay. Honor the §6 invariant before re-cloning:
				if err := rollbackRigArtifacts(city, rec.Metadata["gc.idem.rig_name"]); err != nil {
					return rigAdmitResult{}, err // refuse rather than clone over staging
				}
				fallthrough                                                      // row 7 → row 6
			case "rolled_back":                                                  // row 6
				return s.admitFreshLocked(store, city, body, digest, rec.ID)     // re-clone
			}
		}
	}

	// (2) Name-collision axis (G13 §4.4) — live byName FIRST, then config,
	//     then the durable rig_name backstop scan (§5.2 variant).
	if live, ok := s.rigIdem.lookupByName(city, body.Name); ok {
		return ..., errRigNameConflict(body.Name, live.requestID, live.eventCursor) // row 8
	}
	if s.state.Config() has rig body.Name {
		return ..., errRigNameConflict(body.Name, "", "")                        // row 8
	}
	if hit := durableRigNameScan(store, city, body.Name); hit {                  // in_flight|succeeded
		return ..., errRigNameConflict(...)                                      // row 8 backstop
	}

	// (3) Admit new (rows 3, 9).
	return s.admitFreshLocked(store, city, body, digest, "" /* no existing record */)
}

// admitFreshLocked: capture cursor → durable record (create or reset) →
// register live entry → return. Cursor BEFORE anything else and strictly
// before the goroutine (G13 §5; session precedent
// huma_handlers_sessions_command.go:315→323).
func (s *Server) admitFreshLocked(store beads.Store, city string,
	body RigCreateBody, digest, existingBeadID string) (rigAdmitResult, error) {

	cursor, err := s.currentCityEventCursor() // request_id.go:22 ("0" if no provider)
	if err != nil { return rigAdmitResult{}, err }

	requestID, synthetic := body.RequestID, false
	if requestID == "" { // G13 §1/§3.5: byName protection never depends on the opt-in
		requestID, err = newRequestID() // request_id.go:14, "req-"+hex(12) — §4-charset-safe
		if err != nil { return rigAdmitResult{}, err }
		synthetic = true
	}

	beadID := existingBeadID
	switch {
	case synthetic:
		// no durable record — correlation only (G13 §1)
	case existingBeadID != "": // re-clone: reset, fresh cursor (§5.3)
		if err := store.SetMetadataBatch(existingBeadID, map[string]string{
			"gc.idem.state": "in_flight", "gc.idem.event_cursor": cursor,
		}); err != nil { return rigAdmitResult{}, err }
	default: // brand new
		beadID, err = createIdemRecord(store, city, requestID, digest, cursor, body.Name, "in_flight")
		if err != nil { return rigAdmitResult{}, err }
	}

	entry := &liveProvision{requestID: requestID, digest: digest,
		eventCursor: cursor, rigName: body.Name, beadID: beadID,
		synthetic: synthetic, done: make(chan struct{})}
	s.rigIdem.register(city, entry)
	outcome := rigAdmitNew
	if existingBeadID != "" { outcome = rigAdmitReclone }
	return rigAdmitResult{outcome, requestID, cursor, entry, nil}, nil
}
```

The handler maps outcomes to the wire (G13 §4.2/§10 — one unified output
struct, runtime `Status int \`json:"-"\``): `New`/`InflightReplay`/`Reclone` →
`Status=202, body{status:"accepted", request_id, event_cursor}`; `Existing` →
`Status=200, body{status:"exists", request_id, rig, prefix, default_branch}`
(from `gc.idem.result.*`); the two 409s and the 400 ride Huma typed errors.
On `New`/`Reclone` only, the handler then spawns
`go s.provisionRig(context.Background(), city, body, entry)` with the
`defer recoverAsRequestFailed`-style backstop; that goroutine's terminal steps
follow §5.3's pinned orders and end in `s.rigIdem.remove(city, entry)`.

The sync no-`git_url` path (G13 §8) bypasses all of §6 except rows 4/5/8: after
a successful config append it calls
`createIdemRecord(..., state="succeeded")` + a follow-up `SetMetadataBatch` for
the `gc.idem.result.*` keys — no live entry, no in_flight window.

## 7. Why live-index-first is load-bearing (recap, for the reviewer)

`lookup-then-Create` against the hosted ledger is not mutual exclusion: bd/Dolt
gateway connections have documented cross-connection read-after-write lag
(`internal/sling/sling_core.go:24-30`; `waitForSourceWorkflowLaunchVisible`,
`sling_core.go:964`). Two same-id retries inside the lag window would both
List→miss and both Create → double-clone. Rows 1/2/8's map reads are the
authoritative decision; the durable store is only ever consulted for keys the
index does not hold — records committed strictly in the past (succeeded /
rolled_back / orphan), where lag cannot invert the answer. This is the exact
consistency split G13 §3.5 pins, realized as ~40 lines of locked map code.

## 8. Test hooks this design implies (delta over G13 §11)

- **Golden digest** (§3): literal body → literal hex; breaks on field reorder.
- **`json.Valid` guard** (§4): table incl. `1e5`, `1E5`, `-0`, `00000000`
  (all rejected) and `req-4fca...`, UUIDv4, `0a1b2c3d` (accepted).
- **Closed-at-create** (§5.1): a fresh in_flight record never appears in
  `Ready()` or open `List`s; lookup still finds it via `IncludeClosed`.
- **Pointer-identity remove** (§2): after re-clone under the same name, the
  FIRST attempt's late terminal `remove` does not evict the successor's
  `byName` entry.
- **Row 7 inline cleanup**: an orphan in_flight record's staging dir is gone
  before the re-clone's goroutine starts.

## 9. Anchor index (this doc's grounding)

| Claim | Anchor |
|---|---|
| `Create(b Bead) (Bead, error)` | `internal/beads/beads.go:337-341` |
| `List(query ListQuery) ([]Bead, error)` | `internal/beads/beads.go:364-366` |
| `ListByMetadata` legacy + `IncludeClosed` opt | `internal/beads/beads.go:399-402`, `:311-313` |
| `SetMetadataBatch` merge + non-atomic external note | `internal/beads/beads.go:408-414` |
| `Tx` + `Tx.Create`/`Tx.Close` | `internal/beads/beads.go:421`, `:139-144` |
| `ListQuery{Metadata,Limit,IncludeClosed}` / `HasFilter` | `internal/beads/query.go:66-101`, `:133-143` |
| `Bead.Type`/"task" default, `Metadata StringMap`, tier flags | `internal/beads/beads.go:54`, `:74`, `:80-83` |
| "task" is Ready-eligible (exclusion lists) | `internal/beads/beads.go:206-217`, `:277-285` |
| `idempotencyCache` precedent (mutex/map/pending-never-evicted) | `internal/api/idempotency.go:30-42`, `:80-84` |
| `Server.idem` placement + construction | `internal/api/server.go:52`, `:61-62`, `:213` |
| `CityBeadStore()` / `BeadStore(rig)` / `Config()` | `internal/api/state.go:107-109`, `:66-68` |
| `newRequestID` / `currentCityEventCursor` | `internal/api/request_id.go:14`, `:22-32` |
| Current `RigCreateInput` (anonymous Body to promote) | `internal/api/huma_types_rigs.go:24-32` |
| `citywriteauth.ReqDigest` (contrast, not reuse) | `internal/citywriteauth/citywriteauth.go:262-282` |
| Per-rig-name lock + empty-key no-op | `internal/sourceworkflow/sourceworkflow.go:228`, `:230` (per G13 §13) |
| Spec sections implemented | `G13-request-id-state-machine.md` §2 (:57-83), §3.2 (:104-123), §3.3 (:130-147), §3.4 (:149-168), §3.5 (:170-218), §4 (:222-298), §5 (:302-317), §6 (:320-342), §8 (:400-417) |
