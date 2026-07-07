# C8 — G23 Capstone: the one-liner E2E + the operator runbook (design, no code)

**Scope.** The LAST unit of Group C: prove
`gc --context prod rig add --git-url <repo> && gc --context prod sling <target> <bead>`
works end to end against a DIRECT hardened self-hosted city, and ship the
operator runbook the brief pins in §11. Gate: **G23** (`DESIGN-BRIEF.md:71`,
runbook sketch `:158-159`, capstone lock `:5`). Everything C8 composes is
already built (C2–C7); C8 adds **two tiny production seams, one boot warning,
two test files, and one runbook doc** — nothing else.

**What G23 actually requires** (`DESIGN-BRIEF.md:71`):
1. The capstone runbook (SPA-401s-by-design; exactly one key source; supervisor
   `allowed_hosts`/421 variant; **a loud boot warning enumerating the
   unauthenticated read surface** — this warning does **not exist yet**:
   `cmd/gc/controller.go:1357-1359` prints only the read-only notice, and a
   hardened `allow_mutations` bind boots silent).
2. The mechanical proof that the one-liner works — the E2E.

---

## 0. Verified ground truth (what C8 composes)

- **Client chain (C7, done).** `cmd/gc/cmd_rig.go:114-130` branches to
  `cmdRigAddRemote` (`cmd/gc/rig_remote.go:24-140`) before any local work;
  `cmd/gc/cmd_sling.go:222` branches to `cmdSlingRemote`
  (`cmd/gc/sling_remote.go:19-92`). Both take a caller-supplied `*api.Client`,
  so a test can inject one directly. `Client.RigCreate`
  (`internal/api/rig_create_client.go:159-216`) drives POST → 202 →
  `awaitRigProvision` (`:226-285`) → `waitForEventReconnecting` (`:311-424`)
  with an injectable `rigWaitParams` (`:41-45`) so tests compress the 30-min
  watchdog (`:26`). The grant rides automatically: `buildRemoteWriteClient`
  (`cmd/gc/remote_client.go:65-89`) wires `ctx.GrantCommand` →
  `clientgrant.GrantSource` → `RemoteOptions.Grant`, and the G18 editor mints
  per mutating request.
- **Server chain (C4, done).** `humaHandleRigCreate`
  (`internal/api/huma_handlers_rigs.go:94-105`) branches on `git_url`;
  admission runs under the name+request_id locks (`:159-184`);
  `spawnRigProvision` (`:262-347`) owns re-clone pre-drop (`:313-321`),
  rollback (`rollbackFailedProvision:356-370`), the G17 visibility barrier
  (`waitRigVisible:376-388`), and the terminal events. Failure codes at
  `:431-444`. Durable idempotency record = a `task` bead with `gc.idem.*`
  metadata (`internal/api/rigidem.go:30-73`; states
  `in_flight/succeeded/rolled_back`).
- **The real provision.** `controllerState.ProvisionRigFromGit`
  (`cmd/gc/api_state.go:1608-1692`): SSRF fence (`ensurePublicGitHost:2032-2041`
  → `ssrf.EnsurePublicHostStrict`, fail-closed) → absent-path check
  (`:1651-1655`) → record-then-create manifest (`:1660-1662`) →
  `git.Clone(ctx, gitURL, r.Path, git.CloneOptions{})` (`:1667`) →
  `provisionRigLocked` → `internal/rig.Provision` (`:1994-1999`).
- **The clone hardening that makes the E2E hard.** `internal/git/clone.go`:
  `classifyCloneScheme` (`:112-164`) rejects `file:` any-slash-count
  (`:126-128`), bare/scheme-less local paths (`:158-160`), and **`http://` /
  `git://` (`:156`, `ErrSchemeInsecure`)** — https (or opted-in ssh) only.
  `rigCloneHardeningArgs` pins `http.followRedirects=false` (`:266`).
  `cloneEnv` (`:280-289`) builds on `HermeticEnv` (GIT_CONFIG_NOSYSTEM,
  GIT_CONFIG_GLOBAL=/dev/null) with **no CA-trust injection point** — the only
  env seam is `opts.Cred.Env`, and the production call passes the zero
  `CloneOptions{}` (`api_state.go:1667`). The fence blocks loopback/private
  literals and hostnames outright (`internal/ssrf/ssrf.go:69-127`,
  `IsInternalIP:166-183`), and the strict variant blocks on any resolver error
  (`:81-83`). `internal/git.cloneRunner` is an **unexported** package var
  (`clone.go:77`) — reachable from `internal/git` tests only
  (`clone_test.go:10-23`).
- **Write-auth (Phase 1, done).** `writeAuthMiddleware`
  (`internal/api/writeauth.go:139-230`); key resolution env-over-config
  (`:314-317`), `GC_CITY_WRITE_EPOCH_FLOOR` env-only (`:331`); G10 boot gate +
  ack knob (`:362-373`); `InstallWriteAuth` (`:381-393`) called at both serve
  seams (`cmd/gc/controller.go:1371`, `cmd/gc/cmd_supervisor.go:1276`). Config
  fields: `internal/config/config.go:2085-2116`.
- **Test precedents to mirror.**
  - Wire E2E through the real middleware + in-process ed25519 signer:
    `internal/api/grant_e2e_test.go:39-60` (`signingGrantSource`), `:65-108`.
  - Real-binary grant chain (`//go:build integration`, builds `gc-write-mint`
    with `go build`): `cmd/gc-write-mint/e2e_test.go:25-99`.
  - Real mux in tests: `internal/api/test_helpers_test.go:21-40`
    (`NewSupervisorMux` wrap); the controller's production wrap is
    `cmd/gc/controller.go:1366-1380` (`singleCityStateResolver`, defined
    `:1408-1413` — package main, so a `cmd/gc` test can reuse it).
  - Real `controllerState` in tests: `newControllerState(ctx, cfg,
    runtime.NewFake(), events.NewFake(), name, t.TempDir())`
    (`cmd/gc/api_state_test.go:200` et al.; signature `api_state.go:125-131`).
    `events.NewFake()` implements `LatestSeq`/`Watch`
    (`internal/events/fake.go:90-107`), which is all the real SSE handler
    needs (`internal/api/huma_handlers_events.go:256-320`) — so the **real
    event stream** serves from a fake provider.
  - TLS + CA trust on the client: `internal/api/client_remote_test.go:274-333`
    (`httptest.NewTLSServer` + `writeServerCA` + `RemoteOptions.CAFile`).
  - Package-var injection precedent in `cmd/gc`:
    `controllerDropManagedDoltDatabase` (`api_state.go:1753`), stubbed by
    `api_state_rig_rollback_test.go:39-45`. SSRF resolver seam:
    `ssrf.HostResolver` exported var, documented save-and-restore
    (`internal/ssrf/ssrf.go:38-41`).
  - Sling server-side is in-process for a plain agent target:
    `apiBeadRouter.Route` (`internal/api/handler_sling.go:437-470`) sets
    `gc.routed_to` bead metadata via the store; the shell runner is used only
    for a custom `sling_query` (`:443-455`) — the E2E avoids it by configuring
    an agent without one.
- **Backends.** `GC_BEADS=file` + `GC_DOLT=skip` is the established no-infra
  test mode (`TESTING.md` testscript defaults; `cmd/gc/management_json_test.go:437,502`).
  Under `GC_DOLT=skip` the manifest claims no Dolt DB
  (`provisionedManagedDoltDatabase`, `api_state.go:1699-1705`), so the rollback
  path in the E2E never needs a Dolt server.

---

## 1. The E2E harness — how an automated test provides a "cloneable" git_url

### The bind

The G15 hardening was *designed* to make the E2E's natural shortcuts
impossible, and it succeeded on every axis at once:

| Shortcut | Blocked by |
|---|---|
| `file:///tmp/fixture-repo` | `ErrSchemeFile` (`clone.go:126-128`) |
| bare `/tmp/fixture-repo` | `ErrBareLocalPath` (`:158-160`) |
| `http://127.0.0.1:<port>/repo.git` (git http-backend) | `ErrSchemeInsecure` (`:156`) **and** the SSRF fence (loopback literal, `ssrf.go:97-101` + `IsInternalIP`) |
| `https://127.0.0.1:<port>/repo.git` | SSRF fence (loopback literal) **and** git does not trust the test CA (`cloneEnv:280-289` has no CA seam on the zero `CloneOptions{}`) |
| `https://myhost.test:<port>/…` with a stubbed `ssrf.HostResolver` | fence passes, but **git resolves via getaddrinfo**, not `ssrf.HostResolver` — the hostname never reaches the loopback server without /etc/hosts control or git ≥2.44 `http.curloptResolve` plumbing |
| redirect from a public host to loopback | `http.followRedirects=false` (`:266`) |

### Option (a): a real loopback https git server

Requires **three** new production accommodations, each of which weakens the
gate it tests: (i) a CA-trust env seam into `cloneEnv` (or plumbing
`CloneOptions.Cred.Env` from a config knob) so git accepts the test TLS cert;
(ii) an SSRF fence bypass/allowlist knob (a literal
"disable-the-SSRF-check" flag shipping in the production binary); (iii) DNS
control so a fence-passing hostname resolves to loopback for git itself
(host-file games or a git-version floor for `http.curloptResolve`). It also
drags `git http-backend` + TLS termination into the test bed — the classic
CI-only flake generator. **Rejected as the automated path.** What a real clone
would additionally prove — that git's own subprocess honors the assembled
argv/env — is already pinned by `internal/git/clone_test.go` (captured argv/env
tables via the package-internal `cloneRunner` seam, `clone_test.go:10-23`) and
is a property of git, not of this codebase.

### Option (b): a test seam at the git-fetch boundary — RECOMMENDED core

Stub **exactly one call**: the `git.Clone(...)` invocation inside
`ProvisionRigFromGit` (`api_state.go:1667`). Everything else stays real:
admission locks, the request_id state machine, the durable record, the SSRF
fence (fed a fence-passing fake-public host via the *existing*
`ssrf.HostResolver` seam), the absent-path guard, record-then-create manifest,
`internal/rig.Provision` (real beads init + config append), `mutateAndPoke`
reload, the G17 visibility barrier, typed events, the real SSE stream, the G18
grant editor, the real `writeAuthMiddleware`, TLS, and the CLI rendering.

**New production seam #1 (2-line diff):** in `cmd/gc/api_state.go`, a package
var mirroring the existing `controllerDropManagedDoltDatabase` precedent
(`:1753`):

```
var rigCloneGit = git.Clone   // swapped by the capstone E2E only
```

and `ProvisionRigFromGit` calls `rigCloneGit(ctx, gitURL, r.Path, git.CloneOptions{})`
at `:1667`. Production behavior is byte-identical; the var is package-private
to `cmd/gc` (`package main` — unimportable, so the seam cannot leak into any
other consumer).

**The stub materializes a real working tree** at `dst` in pure Go
(`os.MkdirAll` + a README) — no `git init` subprocess. This is valid because
the local rig-add contract treats the git check as informational
(`cmd/gc/cmd_rig.go` help text: "The git repo check remains informational"),
so `rig.Provision` warn-and-continues on a non-repo dir; the warning even
exercises the `Warn:true` progress-frame path (`rig_remote.go:82-84`). The
E2E passes `--default-branch` explicitly so the terminal event's resolved
branch is deterministic rather than git-detected.

**Fence handling:** the E2E uses `https://capstone.example.test/repo.git` and
stubs `ssrf.HostResolver` (save/restore per `ssrf.go:38-41`) to resolve that
host to `198.51.100.7` — TEST-NET-2, which `IsInternalIP` classifies public
(`internalCIDRv4` at `ssrf.go:141-146` contains no 198.51.100.0/24), and which
is guaranteed-unrouted if a regression ever let a dial escape the stub. The
strict fence thus runs **for real** and passes; the negative case (blocked
host) is separately asserted by resolving a second hostname to `10.0.0.7`.

### Option (c): split — (b) + an integration-tagged REAL clone

The real-clone leg inherits every option-(a) blocker — the fence and CA seams
would have to ship in the production binary regardless of the build tag on the
test. A public-network clone (e.g. a real GitHub URL) under `//go:build
integration` is possible but is exactly the network-flake class TESTING.md's
scrubbed-env shards exist to avoid, and it cannot run in CI's egress posture.

**Decision: a reshaped (c).** Three legs, none of which needs a fence/CA knob:

1. **The fast wire E2E (required, the G23 mechanical proof)** — option (b):
   real controllerState + real SupervisorMux + real write-auth over
   `httptest.NewTLSServer`, in-process ed25519 grant signer
   (`grant_e2e_test.go:39-60` pattern), clone stubbed at `rigCloneGit`.
   Sub-second, deterministic, runs in the default suite.
2. **The full-chain integration test (`//go:build integration`)** — same
   harness, but the grant comes from the **real `gc-write-mint` binary**
   through the **real `contexts.toml` → `resolveWriteTarget` →
   `buildRemoteWriteClient` → `clientgrant` env-exec** chain
   (`cmd/gc-write-mint/e2e_test.go:34-40` build pattern; `GC_HOME` pointed at a
   temp dir per `remote_target.go:177` + `supervisor/config.go:209-212`'s
   explicit-GC_HOME-in-tests guard). This is the only automated leg that
   executes the operator-visible configuration path end to end.
3. **The runbook's verification transcript (manual)** — the ONLY place a real
   network clone happens: the operator runs the one-liner against a hardened
   city with a real repo URL. G23 makes the runbook a deliverable anyway; its
   §"validate" section doubles as the real-git/real-DNS/real-TLS proof, which
   no CI environment here can honestly provide.

This is the arrangement that "actually proves G23 without flakiness": the wire
E2E proves the *composition* (every G10–G22 artifact chained), the per-layer
tests already prove each hardening in isolation (`clone_test.go` argv/env
tables, `ssrf` tables, `api_state_rig_rollback_test.go:187-189` fail-closed
fence, `rigidem_*` state machine, `grant_e2e_test.go` digest binding), and the
runbook proves the real-world leg once per release rather than once per CI run.

### Harness assembly (the fast wire E2E, no code — shape only)

1. `t.Setenv("GC_BEADS","file")`, `t.Setenv("GC_DOLT","skip")`,
   `t.Setenv("GC_HOME", t.TempDir())`.
2. City dir via `t.TempDir()`: `city.toml` with `[workspace] name`, one
   `[[agent]]` (any name — config-supplied, ZERO hardcoded roles; **no**
   `sling_query` so routing stays in-process,
   `handler_sling.go:443-455`) — mirror the store bootstrap used by
   `cmd/gc/management_json_test.go:437-510`.
3. `cs := newControllerState(ctx, cfg, runtime.NewFake(), events.NewFake(), city, cityPath)`.
4. `apiMux := api.NewSupervisorMux(&singleCityStateResolver{state: cs}, nil,
   /*readOnly=*/false, "controller", commit, time.Now())`;
   `apiMux.WithAnyHostAllowed()` (the controller's own production wrap,
   `controller.go:1366-1367`).
5. `api.InstallWriteAuth(apiMux, "k1:"+base64(pub), false,
   api.WriteAuthBindContext{NonLocal:true, AllowMutations:true})` — G10 boots
   *because* the key is present; a sibling assertion boots it key-less +
   ack-less and requires the refusal (`writeauth.go:362-373`).
6. `srv := httptest.NewTLSServer(apiMux.Handler())`; write the server CA to
   disk (`client_remote_test.go` `writeServerCA` pattern).
7. Client: `api.NewRemoteCityScopedClient(srv.URL, city,
   RemoteOptions{CAFile: ca, Grant: <in-process signer bound to city>})` —
   handed to `cmdRigAddRemote` / `cmdSlingRemote`, whose signatures already
   accept an injected client + `*remoteTarget` (`rig_remote.go:24-28`,
   `sling_remote.go:19`).
8. Seams: swap `rigCloneGit` (counter + optional gate channel + fail-N,
   mirroring `fakeMutatorState`'s `provisionGate`/`provisionFailN`,
   `internal/api/fake_state_test.go:191-198`); swap `ssrf.HostResolver`.

One caveat to carry into implementation: `Client.RigCreate` passes
`defaultRigWaitParams()` (`rig_create_client.go:256`) — the 30-min watchdog is
never *hit* on the happy/replay/failed paths (the terminal event arrives), so
the CLI-level tests need no param injection; only a deliberately-lost-stream
scenario would, and that scenario is already covered at the client layer
(`rig_create_client_test.go`), not re-proven in C8.

---

## 2. What the E2E asserts

**Scenario A — the capstone one-liner (CLI level, the G23 proof).**
`cmdRigAddRemote(client, target, nil, gitURL, reqID, "web", "w", "main", nil,
false, false, false, stdout, stderr)` then
`cmdSlingRemote(client, []string{agent, beadID}, …)`:

1. rig add exit 0; stdout carries the progress line
   `"  Cloning rig working tree from git"` (`api_state.go:1665`) and the
   terminal `"provisioned → web (prefix w, branch main)"`
   (`rig_remote.go:126-137`); the target echo landed on stderr
   (`formatRemoteTarget`, `cmd_context.go:390-401`).
2. Server state (G17 barrier made observable): `cs.Config()` lists the rig;
   `cs.BeadStore("web") != nil`; `city.toml` on disk carries the appended
   `[[rig]]`; the durable record is `gc.idem.state=succeeded` with
   `gc.idem.result.*` populated (`rigidem.go:44-57`).
3. Event stream: at least one `rig.provision.progress` frame and the
   `request.result.rig.create` terminal, both carrying the client-minted
   `request_id` — implicitly proven by `RigCreate` returning
   `Status:"provisioned"` (the wait *is* the SSE consumption,
   `rig_create_client.go:226-285`), plus an explicit
   `events.NewFake().List` scan for the progress/terminal pair.
4. Sling the new rig: seed one task bead **in the new rig's store**
   (`cs.BeadStore("web").Create(...)` — this is the assertion that the
   provisioned store actually works), then `cmdSlingRemote` → exit 0, stdout
   `"routed → <agent>"`; the bead's `gc.routed_to` metadata is set
   (`handler_sling.go:464-467`). This is deliberately the 2-arg
   explicit-target existing-bead shape — the only remote sling contract
   (`sling_remote.go:57-59`).
5. **The grant rode both mutations**: instrument the signer with a counter —
   exactly 2 mints (one per mutation: the rig-add POST and the sling POST; the
   SSE wait mints none, G18); and a grant-less sibling client gets 401
   `missing X-GC-City-Write grant` (`writeauth.go:237-240`) on the same rig-add
   body, non-fallenback (`ShouldFallback(c, err) == false`, G1).

**Scenario B — idempotency capstone (CLI level).** Re-run `cmdRigAddRemote`
with the **same** `--request-id` and same digest-affecting flags: exit 0,
stdout `"exists → web (idempotent replay)"` (`rig_remote.go:123-124`), HTTP 200
path (`existingRigOutput`, `huma_handlers_rigs.go:208-218`), and the
`rigCloneGit` counter still reads **1** — the retried add did not double-clone
(G13's whole point, `G13-request-id-state-machine.md`).

**Scenario C — in-flight replay + name conflict (api.Client level —
concurrency reads better without CLI rendering).** Hold the clone stub on a
gate channel; while held: (i) a second POST with the *same* request_id + body
→ 202 with the **original** EventCursor and **no second clone**
(`rigAdmitInflightReplay`, `huma_handlers_rigs.go:174-176`); (ii) a POST with a
*different* request_id for the same rig name → structured 409
`rig_name_conflict` carrying `in_flight_request_id` + `event_cursor`
(`mapRigAdmitError:236-254`), which `rigConflictFromError` decodes and the CLI
renders as the watch recipe (`rig_remote.go:165-172`). Release the gate; the
held provision completes normally.

**Scenario D — rollback capstone (CLI level).** Configure the stub to fail
the first clone (after materializing the dir — the realistic mid-clone
failure): `cmdRigAddRemote` exits 1 with `rig_provision_failed`, printing the
request_id + the re-attach recipe (`renderRemoteRigAddError:190-195`); the
terminal event is `request.failed` with code `clone_failed`
(`rigProvisionFailureCode`, `huma_handlers_rigs.go:437`); the rig dir is
**gone** (`TeardownPartialRig` removed it, `api_state.go:1791-1806`); the rig
is NOT in `cs.Config()`; the durable record is `gc.idem.state=rolled_back`.
Then re-run with the **same request_id** (exactly the printed recipe,
`rigAddReplayRecipe:217-229`): the retry re-clones cleanly (stub counter = 2)
and lands `provisioned` — the Decision-6/Decision-9 reconciliation, live.

**Scenario E — fence negative (unit-adjacent, same file).** A git_url whose
host resolves (stubbed) to `10.0.0.7` → the terminal is `request.failed` with
code `blocked_host` (`huma_handlers_rigs.go:433-434`) and no dir was created —
the fence ran before the manifest (`api_state.go:1643` precedes `:1660`).

The **integration-tagged** test re-runs Scenario A only, with the grant minted
by the built `gc-write-mint` binary via a real `contexts.toml`
(`grant_command = "<tmp>/gc-write-mint --kid k1 --key <tmp>/city.ed25519
--city <city>"`), target resolved by `resolveWriteTarget()` with
`GC_CITY_CONTEXT=prod` — proving resolver → helper-exec → TLS → middleware as
the operator will actually run it.

---

## 3. Where the E2E lives

**`cmd/gc/capstone_e2e_test.go` (package main, no build tag)** for the fast
wire E2E, and **`cmd/gc/capstone_integration_test.go` (`//go:build
integration`)** for the real-minter leg. Rationale:

- It must construct `controllerState` and reuse `singleCityStateResolver` —
  both unexported in `package main` (`api_state.go:125`,
  `controller.go:1408-1413`). `internal/api` cannot host it (it would need the
  real provision, which lives behind `StateMutator` in `cmd/gc`), and
  `test/integration/` cannot see either type.
- TESTING.md tier fit: the fast leg spawns **no subprocess** (pure-Go repo
  materialization, in-process signer, in-process sling routing), touches no
  tmux/Dolt/network beyond a loopback `httptest` TLS listener (the same class
  as the existing `cmd/gc/remote_client_test.go:76`), and uses `t.TempDir()`
  throughout — it belongs in the default unit sweep, not behind
  `GC_FAST_UNIT`. The integration leg runs `go build` of `gc-write-mint`
  (the `cmd/gc-write-mint/e2e_test.go:34-36` precedent is already
  integration-tagged for exactly this reason) and `sh -c` helper execs.
- New-file hygiene: `cmd/gc` already has its `testenv_import_test.go`; no new
  directory is created, so the `TestRequiresDedicatedTestenvImportFile` guard
  is unaffected (PHASE-2-HANDOFF.md §6).

Avoided-infra checklist per TESTING.md: no real Dolt (`GC_DOLT=skip` ⇒ empty
manifest DoltDB, `api_state.go:1699-1705`; the Dolt-drop seam
`controllerDropManagedDoltDatabase:1753` is additionally stubbed defensively),
no tmux (`runtime.NewFake()`), no external network (stubbed resolver +
TEST-NET-2 + stubbed clone), no wall-clock waits (event-driven; the SSE wait
terminates on the terminal frame).

---

## 4. The runbook (formalizing DESIGN-BRIEF §11)

**New doc: `docs/runbooks/remote-hardened-city.md`** (the `docs/runbooks/`
directory exists — sibling `managed-city-endpoints.md`; add the nav entry in
`docs/docs.json`). Content contract below — the doc is written as numbered
operator steps with expected output blocks, in this order:

### 4.1 Prerequisites & threat posture (read first)
- One controller replica ONLY (see §5 — replay guard + request_id live index
  are process-local). No code refuses a second replica today; this is an
  operator rule.
- The read plane is FULLY UNAUTHENTICATED (beads, mail, session peek,
  transcripts, the events stream — including the 202 provisioning stream).
  Write-auth gates mutations only. A network/TLS front (reverse proxy,
  tailnet, netpol) is REQUIRED, not optional.
- TLS: gc refuses a plain-http non-loopback URL at context validation; put the
  city behind a TLS terminator and pass its CA via `gc context add … --ca-file`
  if private.

### 4.2 Mint the city keypair
`gc-write-mint` accepts a PEM PKCS#8 or a raw/hex/base64 32-byte ed25519 seed
(`cmd/gc-write-mint/main.go:224-245`). Runbook shows the openssl one-liner to
generate PKCS#8, extract the raw 32-byte public key, and base64 it. Server
gets ONLY the public key; the private key stays on the operator machine
(`0600`, e.g. `~/.gc/keys/maintainer.ed25519`).

### 4.3 Configure and boot the hardened city
`city.toml`:
```toml
[api]
port = 9443            # behind the TLS front
bind = "0.0.0.0"       # non-loopback ⇒ read-only unless allow_mutations
allow_mutations = true
write_auth_verify_key = "k1:<base64 ed25519 pub>"   # kid:key, comma-separable
```
**Exactly one key source** (G23): `GC_CITY_WRITE_PUBKEY` env *overrides* the
config key (`writeauth.go:314-317`) — set one, never both;
`GC_CITY_WRITE_EPOCH_FLOOR` is env-only (`:331`). Boot behavior matrix:
- key present → boots hardened; **C8 adds the loud warning** (below).
- no key, no ack → **refuses to boot** with the G10 message
  (`writeauth.go:367-371`).
- no key + `write_auth_allow_unverified = true` (or
  `GC_CITY_WRITE_ALLOW_UNVERIFIED=1`) → boots with an unauthenticated write
  plane — only behind a trusted network front.

**New production change #2 (the G23 boot warning):** at both serve seams
(`controller.go:~1372` after `InstallWriteAuth` succeeds;
`cmd_supervisor.go:~1280`), on any non-loopback bind, print a multi-line
stderr warning enumerating the unauthenticated read surface — beads / mail /
session peek + transcripts / events stream (incl. provisioning progress) — and
stating whether mutations are grant-gated (key present), disabled (read-only),
or **UNVERIFIED-BY-ACK**. This is a projection-layer print, not new domain
logic; test-pinned string at both seams.

Supervisor-managed variant: `[supervisor] allowed_hosts` must include the
public hostname or every request dies **421** (`cmd_supervisor.go:1269-1270`);
the standalone controller allows any host (`controller.go:1367`,
`WithAnyHostAllowed`) because the network front is the boundary.

### 4.4 Configure the client context
```
gc context add prod \
  --url  https://city.example.com:9443 \
  --city maintainer-city \
  --grant-command "gc-write-mint --kid k1 --key ~/.gc/keys/maintainer.ed25519 --city maintainer-city" \
  [--ca-file ~/.gc/keys/city-front-ca.pem]
gc context current    # dry-run: prints the winning tier + shadowing
```
(`cmd/gc/cmd_context.go:74`; stored 0600 in `$GC_HOME/contexts.toml`.) Pin
`--city` on the minter — it refuses to sign for any other city
(`main.go:139-141`).

### 4.5 The one-liner
```
gc --context prod rig add --git-url https://github.com/org/repo.git --name web \
  && gc --context prod sling <agent> <bead-id>
```
Expected transcript (documented verbatim in the runbook):
- stderr: `target: maintainer-city @ https://… (context: prod, cred: grant:gc-write-mint …, source: flag)`
- stdout: progress lines (`Cloning rig working tree from git`, beads-init,
  packs, config steps) → `provisioned → web (prefix w, branch main)`
- then `routed → <agent> (<bead-id>)`.
Observe live from a second terminal:
`gc --context prod events --follow --type rig.provision.progress
--payload-match request_id=<id>` (`cmd_events.go:209`).
Remote sling contract reminder: 2-arg explicit target + existing bead ID only;
inline text / --stdin / 1-arg inference are refused
(`sling_remote.go:28-67`).

### 4.6 Failure & resume recipes (paste from the CLI's own output)
The CLI prints these itself (`rig_remote.go:149-199`); the runbook documents
what each means:
- **Lost stream / deadline** (`rig_stream_lost` / `rig_stream_deadline`): the
  provision CONTINUES server-side. Resume idempotently by re-running the exact
  printed command — same `--request-id`, same digest-affecting flags
  (`--name/--prefix/--default-branch/--git-url`; an omitted flag must stay
  omitted or the digest mismatches and the server 409s,
  `rigAddReplayRecipe:210-229`). Or watch passively:
  `gc --context prod events --watch --type request.result.rig.create
  --payload-match request_id=<id>`.
- **`rig_provision_failed`** (e.g. `clone_failed`, `blocked_host`): the server
  rolled back to no-rig; the SAME `--request-id` retry re-clones cleanly.
- **`rig_name_conflict` with an in-flight id**: another request is
  provisioning that name — watch ITS stream (printed recipe); never re-POST
  your body under its id (409).
- **SPA note**: the dashboard loads fine but **401s on every mutation — by
  design** (`writeauth.go:28-31`); operate writes through `gc` with the grant.
- **401 on `gc` mutations**: the context lacks `grant_command`, the kid/key
  mismatch, or the epoch floor moved — check server audit log; the client 403
  body is deliberately generic (`writeauth.go:222-226`).

### 4.7 Validate (the manual real-clone leg of §1)
A 5-step verification transcript: boot-warning present → grant-less curl
mutation 401s → `/svc` POST 403s (`writeauth.go:152-158`) → the one-liner
succeeds against a real repo → replayed `--request-id` prints
`exists → … (idempotent replay)`.

---

## 5. Risks / residuals the runbook MUST state (brief §8, `DESIGN-BRIEF.md:140-146`)

1. **Single-replica only.** `MemoryReplayGuard` (grant jti) and the rig-create
   live index are process-local; a second controller against the same city
   reopens grant replay (≤2m TTL + 30s skew window) and double-clone races.
   Nothing in code detects the second replica — operator rule.
2. **Unauthenticated read plane.** Everything readable is readable by anyone
   who reaches the port, including bead payloads, session transcripts, and
   the provisioning event stream. The mitigation is the network/TLS front —
   in-band read auth is Slice-3 work.
3. **Same-user grant trust.** Anyone who can exec as the operator user can run
   `grant_command` and mint valid grants; `0600` on `contexts.toml` and the
   key file is the boundary. Treat helper-exec access as write access. (Also:
   a credential embedded in a `--git-url` is argv-visible to same-user `ps`
   during the server-side clone — `api_state.go:1639-1642` accepted residual.)
4. **Repo-content trust (single-tenant assumption).** G15 blocks transport
   abuse (ext::/file://SSRF/hooks/submodules), but the CLONED CONTENT then
   runs inside pipeline agents. Only add repos you would run locally.
5. **DNS-rebinding TOCTOU.** The strict fence resolves once, fail-closed
   (`ssrf.go:73-83`); git re-resolves at fetch. Redirects are refused
   (`clone.go:266`) and rebind-to-SERVFAIL is blocked, but a fast
   A-record flip between fence and fetch remains theoretically open —
   accepted; the egress netpol on the host is the backstop.
6. **Old-binary env fail-open** — `GC_CITY_URL`/`GC_CITY_CONTEXT` are ignored
   by a pre-Slice-0 `gc`, silently going local: automation must use explicit
   flags. Plus the 1 MiB write-auth body cap (`writeauth.go:39`) and the
   inert-by-default epoch floor.

---

## 6. Build order (C8, each phase ≤5 files, TDD)

1. **C8.1 — seams + boot warning.** `rigCloneGit` var + call-through
   (`cmd/gc/api_state.go`, 2 lines); the read-surface boot warning at both
   seams (`controller.go`, `cmd_supervisor.go`) with string-pinned tests.
2. **C8.2 — the fast wire E2E.** `cmd/gc/capstone_e2e_test.go`: scenarios
   A/B/D/E, then C (Client-level, gated stub).
3. **C8.3 — the integration leg.** `cmd/gc/capstone_integration_test.go`
   (`//go:build integration`): scenario A via built `gc-write-mint` +
   `contexts.toml` + `resolveWriteTarget`.
4. **C8.4 — the runbook.** `docs/runbooks/remote-hardened-city.md` + docs nav;
   cross-link from `DESIGN-BRIEF.md` §11 and close G23.

Quality gates: `make test` (fast suite incl. the new E2E) + `go vet ./...`;
no schema/spec changes expected (no wire-type edits ⇒ no genspec/genclient
regen; no config fields ⇒ no genschema) — if C8.1's warning grows a config
knob instead of being unconditional, that changes and `go run ./cmd/genschema`
joins the gate. Adversarial-review pass per PHASE-2-HANDOFF.md §6 (it caught
real defects in every prior phase); lenses: harness-fidelity (what did the
stub silently exempt?), runbook-accuracy (every command copy-paste-runs), and
residual-risk honesty.
