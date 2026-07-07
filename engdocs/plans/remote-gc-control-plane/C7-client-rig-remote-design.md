# C7 — Client half of Group C: `gc --context prod rig add --git-url <repo>` (design, no code)

**Scope.** The CLIENT side of server-side rig provisioning: `Client.RigCreate` in
`internal/api/client.go`, the G21 heartbeat-anchored reconnecting wait, the
`cmd/gc/rig_remote.go::cmdRigAddRemote` routing branch, and the `--git-url` /
`--request-id` flags on `gc rig add`. Consumes the C4 wire (202 + EventCursor +
typed events) that is already built and regenerated. Gates: **G18** (grant rides
automatically), **G21** (rig-create wait + minimal reconnect), **G22** (spec/regen
discipline, byte-identical local output). Brief: `DESIGN-BRIEF.md:69-70` (G21/G22),
`:20` (Decision 6, client-generated `request_id`), `:23` (Decision 9, async
202+EventCursor), `:118-131` (§7.3 state machine, pinned in
`G13-request-id-state-machine.md`).

**Verified ground truth this design builds on**

- Server wire (C4, DONE): `POST /v0/city/{n}/rigs` branches on `git_url`
  (`internal/api/huma_handlers_rigs.go:94-105`); async admission returns
  202 `{status:"accepted", request_id, event_cursor}` / 200 `{status:"exists", rig,
  prefix, default_branch}` / structured 409s (`:198-256`); the provisioning
  goroutine emits `rig.provision.progress` per step (`:271-278`), then
  `request.result.rig.create` (`:345`) or `request.failed` with a stable
  `error_code` (`:369`, codes at `:431-444`: `blocked_host`, `already_exists`,
  `clone_failed`, `invalid_request`, `provision_failed`). The 202 cursor is
  captured under the admission lock **before** the goroutine spawns
  (`internal/api/rigidem.go:694`), so `after_seq=<cursor>` (strictly-greater
  semantics, `internal/api/huma_types_events.go:85`) can never miss the terminal.
- Generated client (C4/G22, DONE): `genclient.CreateRigWithResponse`
  (`internal/api/genclient/client_gen.go:31414`), body alias
  `CreateRigJSONRequestBody = RigCreateBody` (`:7174`, struct `:2595`), params
  `CreateRigParams{XGCRequest}` (`:6847-6850`), response
  `CreateRigResponse{JSON200,JSON201,JSON202 *RigCreateResponseBody}` (`:29282-29287`).
- Wire envelope already carries seq: `eventStreamEnvelope.Seq uint64 json:"seq"`
  (`internal/api/convoy_event_stream.go:135`, populated at `:232`); the *client's*
  `sseEnvelope` just doesn't decode it (`internal/api/client.go:347-350`).
- The wait model to extend: `waitForEvent` (`client.go:356-479`) is single-shot —
  one connect, scan to match or die; per-(re)connect bearer re-mint already exists
  (`:393-397`); the 45s per-frame idle watchdog already exists for remote stream
  clients (`:374-383`, `remoteStreamIdleTimeout` at `client_remote.go:32`); server
  heartbeats every 15s (`internal/api/sse.go:17`,
  `huma_handlers_events.go:287,333-336`).
- The routing pattern to mirror: `cmd/gc/cmd_sling.go:222-226`
  (`resolveWriteTarget` → remote branch **before any local work**) →
  `cmd/gc/sling_remote.go:19-92` (refuse local-only modes, forward params, render).
  Grant wiring: `cmd/gc/remote_client.go:65-89` (`buildRemoteWriteClient` wires
  `ctx.GrantCommand` into `RemoteOptions.Grant`); the grant editor is attached LAST
  at client construction (`internal/api/client_remote.go:111,143-169`) and mints
  per mutating request over the final body — **G18 is automatic for
  `Client.RigCreate`; no per-method work.**
- CLI entry: `cmd/gc/cmd_rig.go:64-143` (`newRigAddCmd`) — flags today are
  `--include --name --prefix --default-branch --start-suspended --adopt --json`;
  **there is no `--git-url` flag yet; C7 adds it** (and `--request-id`).
  `cmdRigAdd` (`:168-186`) and the `--json` RunE path (`:107-127`) both call
  `resolveCity()` first — the remote branch must run before either.

---

## 0. Prerequisite fix (small, server-side, same commit): the missing default error response

The `create-rig` operation is registered with a **manual `Responses` map**
(`internal/api/supervisor_city_routes.go:128-145`) and the resulting spec declares
**only** `200/201/202` for `POST /v0/city/{cityName}/rigs` — no `default`
problem+json (verified against `internal/api/openapi.json`; contrast
`POST .../sling` = `['200','default']`). Consequently the generated
`CreateRigResponse` has **no `ApplicationproblemJSONDefault` field**
(`client_gen.go:29282-29287`), so the standard client error path
(`pdOf` → `apiErrorFromResponse`, `client.go:1413-1433,1440`) would degrade every
non-2xx — including the structured 409s that carry the re-attach cursor — to a
detail-less `"API returned NNN"`.

**Fix:** add the `default` `application/problem+json` response (schema-ref
`ErrorModel`, exactly the shape Huma auto-emits for `cityPost` ops) to the manual
`Responses` map at `supervisor_city_routes.go:135-144`, then regen
`openapi.json` + genclient + dashboard TS **in the same commit** (G22,
`DESIGN-BRIEF.md:70`; `TestOpenAPISpecInSync` + `make dashboard-check` pin it).
Hand-decoding the raw `Body` bytes client-side is rejected: it violates the
typed-wire invariant (AGENTS.md "Typed wire").

---

## 1. `Client.RigCreate` (internal/api/client.go)

### Signature

```go
// RigCreateRequest carries the parameters of a rig-create mutation. It mirrors
// RigCreateBody (rigidem.go:150-157). Path is deliberately absent: the remote
// path never forwards a client filesystem path; the server derives the clone
// destination (huma_types_rigs.go:26-28).
type RigCreateRequest struct {
	Name          string
	Prefix        string
	DefaultBranch string
	GitURL        string // required: triggers async 202 provisioning
	RequestID     string // client-minted idempotency id (Decision 6); required with GitURL
}

// RigCreateResult is the terminal outcome of a rig create.
type RigCreateResult struct {
	Status        string // "created" (201 sync) | "provisioned" (202→terminal success) | "exists" (200 replay)
	Rig           string
	Prefix        string
	DefaultBranch string
	RequestID     string
}

// RigCreate creates a rig over the control plane (POST /v0/city/{city}/rigs).
// With GitURL set it drives the async protocol: POST → 202 {request_id,
// event_cursor} → reconnecting SSE wait (G21) for request.result.rig.create /
// request.failed, invoking onProgress for each rig.provision.progress frame.
// A remote client attaches the X-GC-City-Write grant automatically (G18).
func (c *Client) RigCreate(req RigCreateRequest, onProgress func(RigProvisionProgressPayload)) (RigCreateResult, error)
```

Placement: next to `Sling` (`client.go:1316`), same adapter conventions
(`requireCityScope` `:542`, `connError` wraps, `setStrPtr` where applicable —
note `RigCreateBody` uses plain strings with `omitempty`, not pointers, so fields
are assigned directly).

### request_id minting (Decision 6, `DESIGN-BRIEF.md:20`)

The **client owns the id and reuses it on retry**. Split of responsibility:

- **`cmdRigAddRemote` mints it** (`uuid.NewString()`; `github.com/google/uuid`
  is already a direct dep, `go.mod:14`, already used in `cmd/gc`,
  `provider_health_gate.go:171`) — or takes it verbatim from the new
  `--request-id` flag (the resume path). The CLI must own minting because the
  failure recipe (§3) has to print the id even when `RigCreate` fails before or
  during the wait.
- **`Client.RigCreate` validates, never mints**: `GitURL != "" && RequestID == ""`
  is a hard client-side error. A UUIDv4 trivially satisfies the server's G13 §2
  charset (`rigidem.go:88,99-107`: 8-200 chars of `[A-Za-z0-9._~:-]`, not a bare
  JSON literal).

The id and `git_url` ride **in the body** (`RigCreateBody.RequestID/GitURL`,
`rigidem.go:150-157`) — never a header/query — so the G13 digest
(`rigCreateDigest`, `:171-186`, RequestID zeroed, provisioning fields trimmed)
and the G18 grant digest both bind them.

### Flow

1. `requireCityScope()`.
2. Build `genclient.CreateRigJSONRequestBody{Name, Prefix, DefaultBranch,
   GitURL, RequestID}`; **Path stays empty** on the git_url path.
   Body size is bounded (five short strings) — trivially under the 1 MiB
   write-auth cap; no explicit pre-check needed (G22 note).
3. `params := &genclient.CreateRigParams{XGCRequest: "true"}` (mirror
   `Sling`, `client.go:1336`).
4. `resp, err := c.cw.CreateRigWithResponse(context.Background(), c.cityName, params, body)`.
   The REST shape's 120s ceiling (`client_remote.go:28`) bounds the POST — the
   202 returns as soon as admission completes under the per-name lock; a wedged
   lock is the server's 408 (`huma_handlers_rigs.go:186-188`), not a client hang.
   **G18 is automatic:** `remoteGrantEditor` (`client_remote.go:143`) runs last,
   buffers the final body via `GetBody` (`:187-200`), computes
   `citywriteauth.ReqDigest` and mints via the configured `GrantSource` — the
   exact machinery `Sling` already rides.
5. Error mapping: `connError` on transport/nil; then
   `apiErrorFromResponse(resp.StatusCode(), resp.ApplicationproblemJSONDefault)`
   (available after §0), **preceded by** a rig-specific decode of the structured
   409 (§4's `RigCreateConflictError`) from `ErrorModel.Errors`
   (`client_gen.go:1057-1087`; server shape at `huma_handlers_rigs.go:223-256`).
6. Success shapes (`RigCreateResponseBody`, `huma_types_rigs.go:46-53`):
   - `JSON201` (`status:"created"`, sync config-append — only when GitURL was
     empty) → return `{Status:"created", Rig, RequestID}` — no wait.
   - `JSON200` (`status:"exists"`, idempotent replay of a succeeded create) →
     return `{Status:"exists", Rig, Prefix, DefaultBranch, RequestID}` — no wait.
   - `JSON202` (`status:"accepted"`) → the G21 wait (§2):

```go
ctx, cancel := context.WithTimeout(context.Background(), rigCreateWaitTimeout)
defer cancel()
env, err := c.waitForEventReconnecting(ctx, resp.JSON202.RequestID,
	events.RequestResultRigCreate, RequestOperationRigCreate,
	resp.JSON202.EventCursor, onEnvelope)
```

   where `onEnvelope` filters `env.Type == events.RigProvisionProgress`
   (`internal/events/events.go:123`), decodes `RigProvisionProgressPayload`
   (`event_payloads.go:127-133`), matches `payload.RequestID == requestID`
   (other concurrent provisions share the city stream), and forwards to
   `onProgress` when non-nil.

### Terminal decode (mirrors `SubmitSession`, `client.go:1259-1279`)

- `env.Type == events.RequestFailed` → decode `RequestFailedPayload`
  (`event_payloads.go:194-199`; `waitForEventReconnecting` has already matched
  `operation == "rig.create"` via `payloadMatchesRequest`, `client.go:493-502`)
  → return typed `*RigCreateFailedError{RequestID, Code, Message}` rendering as
  `"rig create failed: <code>: <message>"`. Codes are the async taxonomy of
  `rigProvisionFailureCode` (`huma_handlers_rigs.go:431-444`).
- else (`events.RequestResultRigCreate`, matched by `payloadContainsRequestID`,
  `client.go:481-491`) → decode `RigCreateSucceededPayload`
  (`event_payloads.go:113-118`) → `{Status:"provisioned", Rig: p.Rig,
  Prefix: p.Prefix, DefaultBranch: p.DefaultBranch, RequestID: p.RequestID}`.
- Any wait failure (deadline, reconnect budget, permanent stream status) wraps as
  `*RigCreateWaitError{RequestID string; Err error}` so the CLI can print the
  resume recipe without string-parsing. **The provision keeps running
  server-side** — this error means "lost the stream," not "provision failed."

---

## 2. G21 wait + minimal reconnect

### 2a. `Seq` on `sseEnvelope`

```go
// client.go:347 →
type sseEnvelope struct {
	Seq     uint64          `json:"seq"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}
```

Purely additive: the wire already emits `seq` on every typed envelope
(`convoy_event_stream.go:135,232`); session waits ignore it. Heartbeat frames
(`HeartbeatEvent{Timestamp}`, no `seq`/`type` keys) decode to `Seq:0, Type:""`
and are skipped by the type match exactly as today — and `lastSeq` only advances
on `env.Seq > lastSeq`, so heartbeats can never regress the cursor.

### 2b. Structure: a new reconnecting helper over an extracted single-shot core

**Decision: do NOT grow `waitForEvent` in place.** Sessions
(`SendSessionMessage` `client.go:1196-1228`, `SubmitSession` `:1233-1280`) depend
on its exact single-shot semantics and error strings; the brief scopes reconnect
to "the rig-add wait only" in Slice 1 and generalizes in Slice 2
(`DESIGN-BRIEF.md:69,152`). Refactor mechanically:

1. Extract the body of `waitForEvent` (`client.go:356-479`) into
   `waitForEventOnce(ctx, requestID, successType, failOp, afterSeq string,
   onEnvelope func(*sseEnvelope)) (env *sseEnvelope, lastSeq uint64, err error)`
   — identical logic plus (a) `lastSeq` tracking from `env.Seq`, (b) the optional
   `onEnvelope` tap invoked for every decoded typed envelope before matching,
   (c) non-2xx connects return a typed
   `*sseConnectError{Status int, RetryAfter, Detail string}` whose `Error()`
   renders the exact current string (`"SSE connect failed: %s: %s"`,
   `client.go:411-417`).
2. `waitForEvent` becomes a one-line delegate (`nil` tap, error surfaced as-is)
   — **byte-stable for sessions**; existing session tests pin it.
3. New `waitForEventReconnecting(ctx, requestID, successType, failOp,
   eventCursor string, onEnvelope func(*sseEnvelope)) (*sseEnvelope, error)`
   loops `waitForEventOnce`, resuming from `max(lastSeq, cursor)`.

### 2c. The reconnect loop

```go
const (
	// rigCreateWaitTimeout is the absolute watchdog on the whole rig-create
	// wait, including reconnects. A WAN clone of a large repo routinely exceeds
	// the 4-minute sessionMessageTimeout (client.go:321) — never reuse it here.
	rigCreateWaitTimeout = 30 * time.Minute
	// rigCreateReconnectMaxDelay caps the exponential reconnect backoff
	// (mirrors streamReconnectMax, cmd/gc/cmd_events.go:1226).
	rigCreateReconnectMaxDelay = 30 * time.Second
	// rigCreateMaxSilentAttempts bounds consecutive connects that deliver no
	// frame at all — the "server is gone" cutoff. Any delivered frame
	// (heartbeat or typed event) resets the counter.
	rigCreateMaxSilentAttempts = 8
)
```

Per attempt:

- **Resume cursor.** First attempt: `after_seq = <202 EventCursor>` (the pre-spawn
  capture, `rigidem.go:694`); subsequent: `after_seq = lastSeq` (max typed-envelope
  seq consumed). `after_seq` is strictly-greater (`huma_types_events.go:85`,
  `resolveAfterSeq :102-113`), and the server watcher replays from the log
  (`huma_handlers_events.go:279`), so the terminal can be neither missed nor
  double-processed. Query param, not `Last-Event-ID` — `waitForEvent` already
  builds the URL that way (`client.go:363`); no reason to switch transports.
- **Per-attempt 401 re-mint.** Already structural: `waitForEventOnce` calls
  `c.bearerToken()` on every connect (`client.go:393-397`), and the mutex-guarded
  `TokenSource` is invoked live (`client_remote.go:38-40`, `client.go:312-319`).
  A 401 connect (`sseConnectError.Status == 401`) is retried **once per fresh
  mint** with the anti-spin guard: two consecutive 401s ⇒ permanent (the
  credential is revoked, not stale) — same posture as G7/G8
  (`DESIGN-BRIEF.md:52-53`) and `classifyStreamStatus`
  (`cmd/gc/cmd_events.go:1257-1266`).
- **Status classification.** Mirror `classifyStreamStatus` semantics exactly:
  429/503 transient honoring `Retry-After` (bounded, `parseRetryAfter`
  `cmd_events.go:1268-1285`); 401 reauth-once; 403/404/421/anything-else
  permanent. That classifier lives in `cmd/gc` today; C7 adds a package-private
  twin in `internal/api` (≈12 lines) rather than exporting across the boundary —
  unifying the two is the Slice-2 "generalize waitForEvent reconnect" work
  (`DESIGN-BRIEF.md:152`). Transport-level failures — dial errors, mid-stream
  EOF (`"SSE stream closed before event"`, `client.go:478`), scan errors, and
  the 45s idle-watchdog cancel (readCtx canceled while the caller's ctx is
  live, `:374-383,469-477`) — are all transient.
- **Backoff & anchoring.** Exponential 1s→2s→…→`rigCreateReconnectMaxDelay`
  (mirror `streamReconnectBackoff`, `cmd_events.go:1231-1240`), with the attempt
  counter reset whenever an attempt delivered any frame — the same
  "delivered a frame? reset backoff" rule as `streamCityEvents`
  (`cmd_events.go:1336-1341`). **Heartbeat anchoring is two-layer:** within a
  connection, every frame (heartbeats included) resets the 45s idle watchdog
  (`client.go:424-426`); across connections, every frame resets the
  silent-attempt counter. So a live-but-slow provision (server heartbeating
  through a 20-minute clone step with no progress frames) waits up to the 30m
  watchdog; a dead peer dies in ≤45s per attempt and exhausts
  `rigCreateMaxSilentAttempts` in a few minutes.
- **Progress rendering.** The `onEnvelope` tap fires for `rig.provision.progress`
  envelopes as they arrive (including replays after reconnect — impossible by
  cursor math within one process; across a `--request-id` re-run, the in-flight
  replay 202 returns the **original** cursor (G13 §4.2), so progress re-renders
  from the start, which is correct resume UX).

### 2d. Unrecoverable failure → request_id + resume recipe

On watchdog expiry, silent-attempt exhaustion, or a permanent status, the helper
returns the classified error; `RigCreate` wraps it in `*RigCreateWaitError`;
`cmdRigAddRemote` renders (stderr):

```
gc rig add: lost the provisioning stream: <cause> (request_id=<id>)
the provision continues server-side. Resume the wait (idempotent):
  gc --context <name> rig add --git-url <url> --name <rig> --request-id <id>
or inspect the terminal event without streaming:
  gc --context <name> events --type request.result.rig.create request_id=<id>
  gc --context <name> events --type request.failed request_id=<id>
```

The re-run is the real resume: G13 in-flight ⇒ 202 replay + original cursor;
succeeded ⇒ 200 `exists` with the resolved fields; rolled_back ⇒ clean re-clone
(`DESIGN-BRIEF.md:119`). The `gc events` recipe uses the non-stream list
(`cmd_events.go:194,241` — `--type` + payload-match args already exist and are
remote-capable per Slice 0).

---

## 3. `cmd/gc/rig_remote.go::cmdRigAddRemote`

New file mirroring `sling_remote.go`. Signature:

```go
func cmdRigAddRemote(c *api.Client, target *remoteTarget, args []string,
	gitURL, requestID, nameFlag, prefixFlag, defaultBranchFlag string,
	includes []string, startSuspended, adopt, jsonOutput bool,
	stdout, stderr io.Writer) int
```

### Branch point

At the **top** of `newRigAddCmd`'s RunE (`cmd_rig.go:107`), before both the
`--json` path's `resolveCity()` (`:109`) and `cmdRigAdd`'s (`:174`) — the exact
pattern of `cmd_sling.go:218-226`:

```go
if remoteC, isRemote, _, rerr := resolveWriteTarget(); rerr != nil {
	→ fail "city_resolve_failed"
} else if isRemote {
	return cmdRigAddRemote(remoteC, <target>, args, gitURL, requestID, ...)
}
// local path: byte-identical to today (G22)
```

(`resolveWriteTarget`, `remote_client.go:122-135`, builds the client via
`buildRemoteWriteClient` `:65-89` — the grant rides automatically; a context
without `grant_command` gets a clean 401 from a hardened city.)

To hand the CLI the context name/URL for the echo + recipe, `resolveWriteTarget`
gains a variant (or the existing `resolvedContext.Remote *remoteTarget` is
returned alongside — smallest diff: return the `*remoteTarget` too, mirroring
what `resolveContextAllowRemote` already carries at `remote_client.go:127`).

### Refusals (mirror `sling_remote.go:28-67` — fail fast, clear message, before any wire call)

| Input | Verdict | Message shape (`fail(code, msg)` mirroring `sling_remote.go:20-26`) |
|---|---|---|
| positional path arg (`len(args) > 0`) | refuse | `unsupported_remote`: "a remote city cannot see a client filesystem path; use --git-url" |
| `--git-url` empty | refuse | `invalid_arguments`: "a remote rig add requires --git-url (server-side clone)" |
| `--adopt` | refuse | `unsupported_remote`: adopt reads the client's `.beads/` (`cmd_rig.go:93-97`) |
| `--include` | refuse | `unsupported_remote`: "not supported for a remote city yet" (RigCreateBody has no includes field, `rigidem.go:150-157`) |
| `--start-suspended` | refuse | `unsupported_remote` (no suspended field on the wire body) |

### Forwarding

- `name`: `--name`, else derived from the git URL path basename minus `.git`
  (client-side sugar mirroring the local basename default, `cmd_rig.go:237-240`);
  empty derivation is an `invalid_arguments` error. The server independently
  re-validates (`validateRigName`, `rigidem.go:118-137`).
- `requestID`: `--request-id`, else `uuid.NewString()` minted **here**.
- Echo `formatRemoteTarget(target)` (`cmd_context.go:390-401`) to stderr (human
  mode only) — the brief's per-invocation target echo (`DESIGN-BRIEF.md:105`);
  the failure path appends any captured `request_id=` from
  `api.RequestIDForError` (`client_remote.go:299-311`) already threaded into
  client error strings.
- Call `c.RigCreate(api.RigCreateRequest{...}, onProgress)`.

### Rendering

- **Progress** (human): mirror the local `OnStep` contract exactly
  (`cmd_rig.go:300-306`): `Warn` → stderr `"gc rig add: <detail>"`, else stdout
  `<detail>` (fall back to the step name when `Detail` is empty). `--json`:
  suppress progress on stdout (JSONL purity; progress goes to stderr or is
  dropped — match `doRigAddWithResult`'s `--json` behavior, which discards step
  stdout via `io.Discard`, `cmd_rig.go:123`).
- **Terminal** (human): one line, sling-style (`sling_remote.go:147-155`):
  `provisioned → <rig> (prefix <prefix>, branch <branch>)`; for `exists`:
  `exists → <rig> (idempotent replay)`.
- **`--json`**: `writeManagementActionJSON` with `rigAddJSONSummary` field parity
  (`cmd/gc/management_json.go:73-92`): `command:"rig add"`, `action:"add"`,
  `name`, `rig`, `prefix`, `default_branch`; `path` omitted (server-owned);
  additive `status` + `request_id`. Scripts repointed at a remote city keep the
  automation-critical keys — the same parity rule the remote sling `--json`
  followed (`sling_remote.go:117-146`).
- **Conflict 409** (§4 typed error): print the in-flight `request_id` +
  re-attach recipe (`--request-id <in_flight_id>`); `rig_name_conflict` without
  an in-flight id is a plain "name taken" error.
- **`*RigCreateWaitError`**: the §2d recipe.
- **`*RigCreateFailedError`**: `gc rig add: <code>: <message> (request_id=<id>)`
  — a rolled-back provision; the retry recipe with the SAME `--request-id`
  re-clones cleanly (G13 `rolled_back` purge).

---

## 4. Routing precedence, target echo, error taxonomy

**Precedence is already decided and built** — `resolveWriteTarget` →
`resolveContextAllowRemote` (`cmd/gc/main.go:494-511`, table in
`DESIGN-BRIEF.md:105`): flag (`--context`/`--city-url`) > env > local cwd
discovery > sticky default; remote+local and remote+remote conflicts are loud
errors in the resolver (`remote_target.go:44-56`). C7 adds **no new precedence**;
it only moves the `gc rig add` branch point ahead of `resolveCity()` so a
resolved remote target never touches local config/city state (the
remote-never-touches-disk test class, `DESIGN-BRIEF.md:156`).

Error taxonomy the CLI surfaces — **two planes** (a grounded correction to the
task brief: `invalid_git_url`/`blocked_host` are NOT sync 400s today; URL
hardening runs inside the async clone):

| Plane | Shape | Client handling |
|---|---|---|
| sync 400 | `invalid request_id` / `invalid rig name` (`huma_handlers_rigs.go:136-143`) | problem Detail via `apiErrorFromResponse` (post-§0) → `gc rig add: <detail>` |
| sync 422 | `path is required` (sync branch only, `:110-113`) | unreachable remotely (git_url always set) |
| sync 409 | `request_id_conflict` / `rig_name_conflict` w/ `body.code`, `body.in_flight_request_id`, `body.event_cursor` in `ErrorModel.Errors` (`:223-256`) | new typed `*RigCreateConflictError{Code, Rig, RequestID, InFlightRequestID, EventCursor}` decoded from `pd.Errors`; CLI prints the re-attach recipe |
| sync 401/403 | hardened city, missing/bad grant | error string + `request_id=` header capture (G9); non-fallbackable (G1, `client.go:208-242`) |
| 202 → async `request.failed` | `error_code` ∈ blocked_host, clone_failed, invalid_request, already_exists, provision_failed (`:426-444`) | `*RigCreateFailedError`; exit 1; same-id retry is safe (rollback purge) |
| 202 → stream lost | watchdog / reconnect budget / permanent status | `*RigCreateWaitError` → §2d recipe; exit 1 |

`200 exists` is **success** (exit 0) — idempotency working as designed.

---

## 5. Test plan (TESTING.md tiers — no live sockets; httptest only)

Precedents: `cmd/gc/sling_remote_test.go:46-123` (httptest servers driving the
remote branch), `internal/api/client_remote_test.go` (TLS/CA/redirect),
`cmd/gc/remote_client_test.go` (grant wiring).

**internal/api (unit, `client.go` + new helper):**

1. **Happy path**: httptest handler: POST `/v0/city/c/rigs` → 202
   `{status:"accepted", request_id, event_cursor:"7"}`; GET
   `/v0/city/c/events/stream` asserts `after_seq=7`, writes SSE frames —
   heartbeat, `rig.provision.progress` (seq 8, step clone), progress (seq 9,
   warn), `request.result.rig.create` (seq 10). Assert: result fields, progress
   callback order + warn flag, `X-GC-Request` on both, one POST only.
2. **Reconnect across mid-stream EOF (the G21 core)**: first stream connection
   sends progress seq=8 then closes the body; server asserts the second connect
   carries `after_seq=8`, then sends the terminal seq=9. Assert: exactly one
   terminal decode, no duplicated progress render, backoff attempt counter reset
   by the delivered frame.
3. **401 re-mint per attempt**: `TokenSource` returns `t1` then `t2`; stream
   connect #1 → 401; assert connect #2 carries `Bearer t2` and succeeds. Then
   the anti-spin twin: two consecutive 401s → permanent error (no third dial).
4. **429/503 + Retry-After honored; 404 permanent** (classifier twin parity
   with `cmd_events.go:1257-1266`).
5. **Watchdog + silent-attempt budget**: helper invoked directly with tiny
   deadline/attempt params (the reconnecting helper takes them as arguments from
   `RigCreate`'s consts, so tests never sleep 30m). Assert `*RigCreateWaitError`
   wraps and carries the request_id.
6. **Terminal request.failed** → `*RigCreateFailedError{Code:"blocked_host"}`;
   envelope for a DIFFERENT request_id (both success-type and failed-type) is
   skipped — concurrent-provision isolation (`payloadContainsRequestID`
   `client.go:481`, `payloadMatchesRequest` `:493`).
7. **200 exists / 201 created**: no SSE dial at all (assert zero stream hits).
8. **Grant discipline (G18)**: `GrantSource` recorded — minted for the POST with
   a digest over the exact body (git_url + request_id present); **never** minted
   for the SSE GET (reads carry no grant, `client_remote.go:145`); redirect on
   the grant-bearing POST refused (`client_remote.go:283-284`, existing test
   class).
9. **Session non-regression**: `waitForEvent` byte-stable — existing
   SendSessionMessage/SubmitSession tests unchanged; add one pin that
   `sseEnvelope` with a `seq` key still decodes for sessions.
10. **Seq hygiene**: heartbeat frames (Seq 0) never regress `lastSeq`.

**cmd/gc (unit, `rig_remote_test.go` mirroring `sling_remote_test.go`):**

11. **Refusal matrix**: path arg / missing `--git-url` / `--adopt` / `--include`
    / `--start-suspended` — each fails with its message, **zero HTTP requests**,
    and (in an empty `t.TempDir()`) zero disk reads — the
    remote-never-touches-disk invariant.
12. **request_id ownership**: no flag ⇒ a valid UUIDv4 in the POST body; with
    `--request-id r-123` ⇒ forwarded verbatim.
13. **Name derivation** from `--git-url https://h/o/repo.git` ⇒ `repo`;
    `--name` overrides.
14. **`--json` parity**: single JSONL object with `rigAddJSONSummary` keys +
    `status`/`request_id`; progress absent from stdout.
15. **Failure recipes**: wait-error output contains `request_id=` + the two
    `gc events --type` lines; 409 in-flight output contains the in-flight id.
16. **Local path untouched**: `gc rig add <path>` with no remote target resolves
    exactly as today (existing `cmd_rig.go` tests + tutorial goldens pin G22
    byte-identical output).

**Spec discipline**: `TestOpenAPISpecInSync`, `make dashboard-check`,
`TestEveryKnownEventTypeHasRegisteredPayload` (already green for the C4 events,
`event_payloads.go:591-593`) — all re-run because §0 regenerates the spec.

---

## 6. RISKS

1. **The `sessionMessageTimeout` trap (highest severity).** Every existing async
   wait hard-codes the 4-minute ceiling (`client.go:321,1200,1257`). Any code
   path that reuses `SendSessionMessage`'s scaffolding for rig-create — or a
   future refactor that "unifies" the timeouts — kills a routine WAN clone at
   240s and strands the user mid-provision (the server finishes; the client
   reports failure). Mitigation: `rigCreateWaitTimeout` is a **distinct const
   with a comment forbidding reuse**, and test #5 pins the wait outliving a
   >4-min scripted provision (compressed via injected params).
2. **Reconnect resume correctness.** Safe by construction *today*: cursor
   captured pre-spawn under the admission lock (`rigidem.go:694`) +
   strictly-greater `after_seq` + log-backed `Watch` replay
   (`huma_handlers_events.go:279`). Two residual edges: (a) **event-log
   rotation** during a 30-minute wait — if the segment holding `lastSeq+1..`
   is archived mid-wait, replay behavior needs verification against the
   rotation/anchor machinery (`huma_handlers_events.go:240-251`); test or
   document before ship. (b) `event_cursor == "0"` is overloaded ("no provider /
   empty log", `huma_types_sessions.go:81`) — after_seq=0 replays the whole log;
   harmless for correctness (request_id filter) but potentially slow on a huge
   log; acceptable, note in the helper comment.
3. **Sharing `waitForEvent` with sessions.** The extraction (§2b) touches the
   one function every existing async op transits. A behavior drift (error
   strings, idle-watchdog scoping, the 2xx check) regresses
   `SendSessionMessage`/`SubmitSession` invisibly. Mitigation: mechanical
   extraction, `waitForEvent` as a pure delegate, session tests as the pin;
   reconnect logic lives only in the new helper.
4. **`--git-url` on a local target.** This design makes it a **loud error**
   ("--git-url requires a remote city target; for a local city run
   `git clone` + `gc rig add <path>`") rather than teaching the local path to
   clone. Rationale: the local sync path must stay byte-identical (G22), local
   clone semantics (cwd-relative dest? `resolveRigAddPath` dot-branch,
   `cmd_rig.go:188-204`?) are un-specced, and the capstone only needs remote.
   Cost: a mild DX asymmetry (`gc rig add --git-url` works remotely but not
   locally); revisit as a fast-follow — the server's `internal/rig` +
   `internal/git` hardening is reusable locally when someone specs it.
5. **Missing `ApplicationproblemJSONDefault` on `CreateRigResponse`** (§0). If
   the spec fix slips, every 400/409 renders detail-less and the structured 409
   re-attach data is unreachable typed — silently degrading the whole error
   UX. It must land (with regen) in the same commit as `Client.RigCreate`.
6. **Duplicated stream-status classifier.** The `internal/api` twin of
   `classifyStreamStatus` (§2c) is a small DRY debt with drift risk; parity is
   pinned by test #4 and unification is explicitly Slice-2
   (`DESIGN-BRIEF.md:152`).
