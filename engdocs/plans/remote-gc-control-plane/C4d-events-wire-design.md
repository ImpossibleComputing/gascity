# C4d — Typed events (G20) + wire shapes (G22) for server-side rig-add

Status: DESIGN (Fable pass). Companion to `G13-request-id-state-machine.md` §10 and
`DESIGN-BRIEF.md` §3 G20/G22. This pins the exact symbols, file anchors, and regen
mechanics so C5/C6 implementation is transcription, not invention.

Scope: events + HTTP wire only. The state machine (C4), provisioning core
(`internal/rig`, already landed: `internal/rig/provision.go:25` `Provision(deps, req)`),
and handler orchestration are specified elsewhere.

---

## 1. G20 — Typed events

### 1.1 New event-type constants (`internal/events/events.go`)

Add to the typed-async const block (`internal/events/events.go:112-117`):

```go
RequestResultRigCreate = "request.result.rig.create"
```

Add a new non-request progress event next to it (it is NOT a `request.result.*`
terminal — it is incremental telemetry):

```go
// RigProvisionProgress reports one provisioning step of a server-side
// rig add (clone, beads-init, packs, config, routes). Non-terminal;
// terminal outcome is RequestResultRigCreate or RequestFailed.
RigProvisionProgress = "rig.provision.progress"
```

Both MUST be appended to `KnownEventTypes` (`internal/events/events.go:212`;
the request-result group sits at `:229-231`). CI gate:
`TestEveryKnownEventTypeHasRegisteredPayload` fails the build for any
`KnownEventTypes` entry lacking a `RegisterPayload` registration — so §1.2/§1.4
land in the same commit as this block, or the build reds.

### 1.2 New operation const + payloads (`internal/api/event_payloads.go`)

**Operation const** — extend the block at `internal/api/event_payloads.go:44-51`:

```go
RequestOperationRigCreate = "rig.create"
```

**Success payload** — modeled on `CityCreateSucceededPayload`
(`event_payloads.go:60-67`); carries exactly what the brief's G20 line demands
(request_id + rig name + resolved prefix/branch, `DESIGN-BRIEF.md:68`):

```go
// RigCreateSucceededPayload is emitted on request.result.rig.create.
type RigCreateSucceededPayload struct {
    RequestID     string `json:"request_id" doc:"Correlation ID from the 202 response."`
    Rig           string `json:"rig" doc:"Rig name that was provisioned."`
    Prefix        string `json:"prefix" doc:"Resolved session-name prefix."`
    DefaultBranch string `json:"default_branch" doc:"Resolved mainline branch."`
}

// IsEventPayload marks RigCreateSucceededPayload as an events.Payload variant.
func (RigCreateSucceededPayload) IsEventPayload() {}
```

**Progress payload:**

```go
// RigProvisionProgressPayload is emitted on rig.provision.progress, one
// per provisioning step. RequestID lets watchers filter a single async
// rig-add on the shared city stream.
type RigProvisionProgressPayload struct {
    RequestID string `json:"request_id,omitempty" doc:"Correlation ID from the 202 response (empty on sync 201 provisions)."`
    Rig       string `json:"rig" doc:"Rig name being provisioned."`
    Step      string `json:"step" enum:"clone,beads-init,packs,config,routes" doc:"Provisioning step that completed."`
    Detail    string `json:"detail,omitempty" doc:"Human-readable step detail."`
    Warn      bool   `json:"warn,omitempty" doc:"True when the step reports a warn-and-continue condition."`
}

// IsEventPayload marks RigProvisionProgressPayload as an events.Payload variant.
func (RigProvisionProgressPayload) IsEventPayload() {}
```

The `Step`/`Detail`/`Warn` fields are a 1:1 projection of
`rig.ProvisionStep{Name, Detail, Warn}` (`internal/rig/deps.go:112-117`).
`clone` is the one step name `internal/rig` does not yet emit — the C-group git
work adds it; the enum here is authored forward to match `DESIGN-BRIEF.md:68`
(`{step: clone|beads-init|packs|config}` + the existing `routes` step).

**Failure enum extension** — `RequestFailedPayload.Operation`
(`event_payloads.go:164-169`) gets `rig.create` appended to its `enum` tag:

```go
Operation string `json:"operation" enum:"city.create,city.unregister,session.create,session.message,session.submit,rig.create" doc:"Which operation failed."`
```

Terminal failure MUST be `events.RequestFailed` carrying `request_id`
(`DESIGN-BRIEF.md:68`) — reuse `EmitRequestFailed` / `(*Server).emitRequestFailed`
(`internal/api/request_id.go:50,68`) and `recoverAsRequestFailed`
(`request_id.go:77`) with `RequestOperationRigCreate`. No new failure type.

### 1.3 `requestIDFromPayload` cases (`internal/api/request_id.go:83-100`)

Add two cases to the switch:

```go
case RigCreateSucceededPayload:
    return p.RequestID
case RigProvisionProgressPayload:
    return p.RequestID
```

The first is load-bearing (the `emitAsyncResult` nil-provider log path at
`request_id.go:62`, and the client `waitForEvent` correlates on it — brief
`DESIGN-BRIEF.md:68` warns the waiter blocks until SSE close if envelopes lack
`request_id`). The second is cheap symmetry so progress events log/filter the
same way.

Add the emit helper alongside its session siblings (`request_id.go:102-113`
pattern):

```go
// emitRigCreateSucceeded records a request.result.rig.create event.
func (s *Server) emitRigCreateSucceeded(requestID, rig, prefix, defaultBranch string) {
    s.emitAsyncResult(events.RequestResultRigCreate, rig, RigCreateSucceededPayload{
        RequestID: requestID, Rig: rig, Prefix: prefix, DefaultBranch: defaultBranch,
    })
}
```

### 1.4 `RegisterPayload` calls (`internal/api/event_payloads.go` init, ~`:556-561`)

Append to the "Typed async request result events" block:

```go
events.RegisterPayload(events.RequestResultRigCreate, RigCreateSucceededPayload{})
events.RegisterPayload(events.RigProvisionProgress, RigProvisionProgressPayload{})
```

(`events.RequestFailed` is already registered at `event_payloads.go:561`; the
enum-tag extension in §1.2 changes its schema, which is regen-visible — see §3.)

### 1.5 Wiring `Deps.OnStep` — non-blocking + panic-safe (MANDATORY)

`internal/rig/deps.go:76-79` declares
`OnStep func(step ProvisionStep)` with the contract "the API emits typed events
(G20)". `Provision` invokes it inline on the provisioning goroutine
(`internal/rig/provision.go:46-47`), and — critically — `provision.go:356`
documents that **a panic in an injected func (or OnStep) triggers filesystem
rollback**. So the API-side closure has two hard requirements:

```go
deps.OnStep = func(step rig.ProvisionStep) {
    defer func() {
        if r := recover(); r != nil {
            log.Printf("api: rig.provision.progress emit panic (rig %s, step %s): %v", name, step.Name, r)
        }
    }()
    s.emitAsyncResult(events.RigProvisionProgress, name, RigProvisionProgressPayload{
        RequestID: requestID, Rig: name, Step: step.Name, Detail: step.Detail, Warn: step.Warn,
    })
}
```

- **Panic-safe:** the recover lives INSIDE the closure. Do not rely on the
  goroutine's `defer s.recoverAsRequestFailed(...)` — that would both roll back
  a healthy provision AND mark the request failed because an *observability*
  emit hiccuped. An event-bus problem must never fail a rig add.
- **Non-blocking:** `emitAsyncResult` → `EmitTypedEvent` → `rec.Record`
  (`request_id.go:59-66,35-47`) is a marshal + bus enqueue; it must not grow
  synchronous I/O or waits. If a future recorder can block, wrap the emit in a
  fire-and-forget goroutine — the provision loop's latency budget is not the
  event bus's to spend. Warnings ride both the stream (`Warn: true`) and the
  final `ProvisionResult.Warnings` (`deps.go:97-102`), preserving the
  warn-and-continue contract of `DESIGN-BRIEF.md:70`.

### 1.6 CI gates this section trips

- `TestEveryKnownEventTypeHasRegisteredPayload` — both new constants in
  `KnownEventTypes` need `RegisterPayload` (§1.4) or build fails.
- `spec-ci` drift check (Makefile:747-757) — new event types + the Operation
  enum change regenerate `docs/reference/schema/events.json` / `events.txt`.
- `TestOpenAPISpecInSync` — payload doc-tag changes flow into the spec.

---

## 2. G22 — Wire shapes

### 2.1 Inputs: `request_id` on both bodies

`RigCreateInput.Body` (`internal/api/huma_types_rigs.go:24-32`) and
`SlingInput.Body` (`internal/api/huma_types_sling.go:14-29`) each gain:

```go
RequestID string `json:"request_id,omitempty" doc:"Client-minted idempotency key (see request-id state machine). Optional; absent preserves non-idempotent behavior." minLength:"8" maxLength:"128"`
```

Optional (`omitempty`, not `required`) keeps existing callers and
`TestOpenAPISpecInSync` happy (`G13-request-id-state-machine.md:440-441`).
Validation beyond length (charset, bd type-inference guard) stays in the G13
validator, not Huma tags. `RigCreateInput.Body` also gains the Group-C
`git_url` field — out of scope here, specified by C4/C6.

### 2.2 Output: ONE unified Go output type

Huma binds exactly one output type per operation, so the three success shapes
(201 created / 202 accepted / 200 exists) share one struct
(`G13-request-id-state-machine.md:443-458`), placed in `huma_types_rigs.go`:

```go
// RigCreateOutput is the unified Huma output for POST /v0/city/{cityName}/rigs.
type RigCreateOutput struct {
    Status int `json:"-"` // runtime code: 200 | 201 | 202 (huma reads this)
    Body   RigCreateResponseBody
}

// RigCreateResponseBody is the union success body for rig create.
type RigCreateResponseBody struct {
    Status        string `json:"status" enum:"created,accepted,exists" doc:"created (201 sync), accepted (202 async provisioning), exists (200 idempotent replay)."`
    Rig           string `json:"rig,omitempty" doc:"Rig name (created/exists)."`
    RequestID     string `json:"request_id,omitempty" doc:"Correlation ID; echo of the request's request_id."`
    EventCursor   string `json:"event_cursor,omitempty" doc:"City event-stream cursor captured before accept (202 only); pass as after_seq to the events stream."`
    Prefix        string `json:"prefix,omitempty" doc:"Resolved session-name prefix (created/exists)."`
    DefaultBranch string `json:"default_branch,omitempty" doc:"Resolved mainline branch (created/exists)."`
}
```

Precedents: `Status int json:"-"` + async body = `asyncAcceptedBody` /
`SessionCreateOutput` (`internal/api/huma_types_sessions.go:77-88`); the
`request_id`/`event_cursor` doc strings mirror `huma_types_sessions.go:79-81`.
`asyncAcceptedBody` itself is NOT reused as the output type — the union body is
the operation's single Go output; 400/409 use the Huma error return only
(`G13-request-id-state-machine.md:461-464`).

Handler population matrix:

| Case | `Status` (int) | Body fields set |
|---|---|---|
| sync config-append created | 201 | `status="created"`, `rig`, `prefix`, `default_branch`, `request_id` (echo if sent) |
| async provisioning accepted | 202 | `status="accepted"`, `request_id`, `event_cursor` |
| idempotent replay of succeeded | 200 | `status="exists"`, `rig`, `prefix`, `default_branch`, `request_id` |

### 2.3 Documenting 200/202 — manual `op.Responses` on the literal

A runtime `Status int` adds NOTHING to the OpenAPI document: Huma's generator
only schematizes `op.DefaultStatus` (`G13-request-id-state-machine.md:466-471`).
And create-rig is registered via `cityRegister`
(`internal/api/supervisor_city_routes.go:118-124`), whose signature
(`internal/api/city_scope.go:175-182`) takes **no** op-modifier closure and
passes `op` by value into `huma.Register` — so the extra responses must exist
on the `huma.Operation` literal *before* the call. Concretely, in
`supervisor_city_routes.go`:

```go
rigBodyRef := sm.humaAPI.OpenAPI().Components.Schemas.Schema(
    reflect.TypeOf(RigCreateResponseBody{}), true, "RigCreateResponseBody")
cityRegister(sm, huma.Operation{
    OperationID:   "create-rig",
    Method:        http.MethodPost,
    Path:          "/rigs",
    Summary:       "Create a rig",
    DefaultStatus: http.StatusCreated, // KEEP 201 — Huma auto-schematizes the union body for it
    Responses: map[string]*huma.Response{
        "200": {
            Description: "Rig already exists — idempotent request_id replay of a succeeded create.",
            Content: map[string]*huma.MediaType{"application/json": {Schema: rigBodyRef}},
        },
        "202": {
            Description: "Provisioning accepted; watch the city event stream from event_cursor for request.result.rig.create, rig.provision.progress, or request.failed with this request_id.",
            Content: map[string]*huma.MediaType{"application/json": {Schema: rigBodyRef}},
        },
    },
}, (*Server).humaHandleRigCreate)
```

Load-bearing details:

- **Both 200 AND 202 manually, with real Content schemas.** The nearest in-tree
  precedent, `normalizeSSEResponseHeaders` (`internal/api/sse.go:224-234`) and
  `sseResponseHeaders` (`sse.go:200-218`), mutates schema-**less** responses —
  it under-represents the work here, because create-rig has a real typed body
  and schema-less 200/202 entries would generate untyped clients
  (`G13-request-id-state-machine.md:480-482`).
- All three codes reference the SAME registry `$ref` (the union body), so
  genclient and the dashboard TS get one type, discriminated by `status`.
- `DefaultStatus` stays `http.StatusCreated` (201) — unchanged from today's
  `supervisor_city_routes.go:123` — and Huma fills `Responses["201"]` itself
  (Register only adds the DefaultStatus response when absent).
- Alternative (acceptable, larger diff): extend `cityRegister` to accept
  `opts ...func(*huma.Operation)` like Huma's own registration helpers. Not
  required for one operation; prefer the literal.
- 400 (invalid request_id) / 409 (body-mismatch, name-collision) ride the Huma
  **error** return — no `Responses` entries needed; they document via the
  shared error model.
- CSRF: `cityRegister` auto-applies `addMutationCSRFParam` for POST
  (`city_scope.go:179-181`); nothing to do.
- Sling's success body echo of `request_id` (G13 §11 wire row,
  `G13-request-id-state-machine.md:532-534`) is a body-field addition on the
  existing sling output — no status/Responses surgery there.

---

## 3. Regen sequence + which generated files change

Run, in order, in the SAME commit as the Go changes
(`G13-request-id-state-machine.md:490-492`; enforced by `spec-ci`,
Makefile:746-757, and `TestOpenAPISpecInSync`):

```bash
go run ./cmd/genspec                     # spec + schema mirrors
go generate ./internal/api/genclient     # scripts/gen-client.sh → cmd/gen-client (needs make install-oapi-codegen once)
make dashboard-check                     # dashboard-build + npm typecheck + dashboardspa/dashboardbff go tests (Makefile:716-719)
```

Generated artifacts that WILL diff:

| File | Why |
|---|---|
| `internal/api/openapi.json` | new input fields, union body schema, 200/202 responses |
| `docs/reference/schema/openapi.json` + `openapi.txt` | mirrors of the above (genspec) |
| `docs/reference/schema/events.json` + `events.txt` | two new event types + `RequestFailedPayload.Operation` enum growth |
| `internal/api/genclient/client_gen.go` | regenerated Go client (gen-client.sh writes via temp-file + mv, `scripts/gen-client.sh:15-17`) |
| `internal/api/dashboardspa/web/shared/src/generated/gc-supervisor-client/{types.gen.ts,sdk.gen.ts,zod.gen.ts}` | dashboard TS client (`@hey-api/openapi-ts ^0.97.3`, `web/package.json:31`); the `Operation` enum change lands in `zod.gen.ts` validators too |
| `internal/api/dashboardspa/dist/` | rebuilt SPA bundle (`make dashboard-ci` fails on stale dist, Makefile:737-744) |

NOTE: the `@hey-api/openapi-ts` regen has **no npm script or Makefile target**
in-tree (verified: `web/package.json:10-17` scripts block has only
build/typecheck/lint/format) — it is currently a manual `npx @hey-api/openapi-ts`
invocation. C6 must pin the exact command (and ideally add a
`gen:client` npm script) rather than assuming `make dashboard-check`
regenerates the TS; dashboard-check only *typechecks* what is committed.

---

## 4. Test hooks (what C5/C6 assert)

- `TestEveryKnownEventTypeHasRegisteredPayload` green with both new constants.
- `TestOpenAPISpecInSync`: create-rig documents **200, 201, 202** (+400/409 via
  error model); both `request_id` input fields present
  (`G13-request-id-state-machine.md:532-534`).
- Unit: `requestIDFromPayload(RigCreateSucceededPayload{RequestID: "x"}) == "x"`.
- Unit: an `OnStep` closure whose emit panics does NOT abort/roll back a
  provision and does NOT emit `request.failed` (guards §1.5 against
  `provision.go:356` rollback semantics).
- E2E (C7): 202 → progress events (`rig.provision.progress` step sequence) →
  `request.result.rig.create` with matching `request_id`, resumable from the
  202's `event_cursor`.
