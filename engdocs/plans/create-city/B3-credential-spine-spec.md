# B3 — City-Provisioner Credential Spine (decision record)

**Status:** designed (judge-panel design workflow `design-b3-credential-spine`,
2026-06-27) · **Owner:** this session · **Blocks:** a *running* autonomous
city provisioner.

The B2 binary (`cmd/city-provisioner` + `internal/cityprovision`) is built and
red-teamed; its three adapters read short-lived EIAs from where `main` wires
them. **B3 is the deployment + the credential refresher that keeps those EIAs
current.** This is the single most security-sensitive piece — a cross-cluster
credential spine plus a cross-org read — so it was designed adversarially: three
independent architectures (security-min / operability-min / upstream-min), each
critiqued for cross-org credential leaks, then synthesized. The critiques killed
three real holes (recorded below) and forced the shape.

## Chosen shape

> **B3 = a per-pass per-org EIA minter, fed by an ID-ONLY cross-org discovery read.**

Nothing cross-org is ever minted-and-stored. Each pass:

1. Mint ONE **platform discovery EIA** (`aud=crucible`, carrying ONLY
   `crucible:city.provision`) and call the **ID-only** discovery endpoint to learn
   *which org_ids* have a pending city — no names, no detail.
2. For each such org, mint **per-org work EIAs** under *that org's* B0
   `provisioningToken` (Accounts org-bound machine path → STS): an `aud=crucible`
   work EIA (its tenant **is** the org) and an `aud=beads` (or interim `bts_`)
   credential. Run the existing B2 `RunOnce` for that org against its own
   org-scoped `/pending` + `/complete`.
3. Drop every minted EIA at pass end. Re-mint next pass (ticker faster than the
   ~90 s EIA TTL).

### Why the grounding forces it

`handleListPendingCities` and `handleCompleteCity` both do
`verifiedCaller` + `cities.List(p.Tenant)` — **hard org-scoped to the caller
EIA's tenant**, and `CityStore` has only `List(org)`. STS sets the EIA
`OrgID = the SP's session org`. So the *only* way to satisfy `/pending` and
`/complete` for org X is an EIA whose tenant is X — i.e. minted under X's
authority. There is no cross-tenant detail read, and **we must not add one.**

## The three-scope model (refinement of the B2 red-team fix)

The B2 red-team moved `/pending` + `/complete` off `sandbox.*` onto
`city.provision` (closing the confused deputy where any city's orchestrator SP
could poison a sibling). The B3 design sharpens this: if the per-org *work* EIA
carried `city.provision`, it would itself satisfy the cross-org discovery gate
(server `auth()` is a bare `HasScope`, blind to platform-vs-org subject) — making
every org's EIA a cross-org enumeration key. **Resolution (settles design
open-Q2 — the dedicated-scope option, NOT widening back to `sandbox.read`):**

| Scope | Held by | Gates |
|-------|---------|-------|
| `crucible:city.provision` | the **platform discovery** EIA only (machine-only; allow-listed subject) | `GET /v0/cities/pending/orgs` (ID-only) |
| `crucible:city.work` | the **per-org work** EIA only (machine-only) | `GET /v0/cities/pending` (own-tenant detail) · `POST /v0/cities/{id}/complete` |
| `crucible:sandbox.*` | city **orchestrator SPs** AND the per-org work EIA (for controller launch) | the sandbox CRUD surface |

A city's orchestrator SP holds `sandbox.*` only → cannot touch any `/v0/cities`
endpoint (confused deputy stays closed). The per-org work EIA holds
`sandbox.*` + `city.work` but **never** `city.provision` → cannot enumerate
cross-org. The platform discovery EIA holds `city.provision` only → can learn
org_ids but cannot read detail, complete, or launch.

## Discovery seam

- **Endpoint:** `GET /v0/cities/pending/orgs` (additive: one new file
  `internal/crucible/cities_discovery.go`, one route line, one `CityStore` method).
- **Auth:** `s.auth(scopeCityProvision, s.requireDiscoverySubject(handler))` —
  **double-gated**: (1) EIA carries `crucible:city.provision`, AND (2) a new
  `requireDiscoverySubject` checks `claims.Subject` against an explicit
  `Server.discoverySubjects` allow-list (wired via `UseCityProvisioning`). It does
  **not** call `verifiedCaller` (that would bind the read to the caller's own
  tenant; this route reads across orgs by design).
- **Returns:** `{"orgs":["org_..."]}` and nothing else — `SELECT DISTINCT org_id`
  for non-ready cities.
- **MUST NEVER return:** names, city_ids, **counts**, pack, workspace_id,
  beads_database, sp_id/key_id/prefix, secret_ref, status_detail, sandbox_id,
  timestamps. (Count is a cross-org business signal — explicitly dropped.)
- **Backing:** `CityStore.ListPendingOrgIDs(ctx) ([]string, error)` — additive,
  reuses the existing status column, no migration.
- The existing org-scoped `GET /v0/cities/pending` is unchanged (full detail, own
  tenant only) — its route gate moves from `city.provision` → `city.work`.

## Mint sequence (per org, per pass)

1. `TokenForOrg(orgID)` → that org's B0 `provisioningToken` (ESO file mount).
2. Caller-less Accounts client (**NOT** `cityidentity.HTTPAdmin`, which always
   sends `AdminToken`+`caller_user_id` and can never reach `gateProvision`'s
   machine path): `Authorization: Bearer <provToken>`, **no** `caller_user_id`.
   `EnsureProvisionerSP` (409→fetch) then `MintProductKey(product, scopes)` for
   `crucible` (`sandbox.*`+`city.work`) and for `beads` (`beads:*`).
3. STS machine-login (`POST /sts/v0/machine`, DPoP-bound) with each key →
   session pinned to that product's audience; `POST /sts/v0/token` → the
   `aud=crucible` work EIA and the `aud=beads` EIA. (Audience separation is
   cryptographic: STS 403s cross-audience exchange; a crucible key can never mint
   `aud=beads`. One process drives both legs via two keys.)
4. `CredentialHolder.Swap` (in-process `atomic.Pointer`) publishes the per-org set;
   the B2 adapters read via a new `TokenFunc` seam (no env mutation, no file race).

## Build list (code first — all unit-testable like B2's adapters; infra second)

- **CODE 1 (gasworks-platform / accounts):** `const ScopeCrucibleCityProvision`
  **and** `ScopeCrucibleCityWork` in `internal/accounts/scopes.go` as
  **machine-only** scopes (kept OUT of base/elevated/role-grantable sets, mirroring
  `recall:ingest`) so no human/role can carry them; both pass
  `ValidateScopesForProduct` on the `crucible:` prefix. *Load-bearing cross-team
  prereq — discovery is inert until it lands.*
- **CODE 2 (crucible):** `cities_discovery.go` — `handleListPendingOrgs` +
  `requireDiscoverySubject` + `discoverySubjects` allow-list; route
  `GET /v0/cities/pending/orgs`; `CityStore.ListPendingOrgIDs`. Tests: deduped
  org_ids; JSON has exactly key `orgs`; non-allow-listed `city.provision` subject
  → 403; missing scope → 403; org-scoped `/pending` unchanged.
- **CODE 3 (crucible):** move `GET /v0/cities/pending` + `POST .../complete` route
  gates from `scopeCityProvision` → `scopeCityWork`; keep `verifiedCaller`
  org-scoping. Update the B2 red-team tests' work-EIA scope to `city.work`. Keep
  `TestCityMachineEndpointsRejectSandboxScope` (sandbox-only still 403) green.
- **CODE 4 (`internal/cityprovision`):** `Discoverer` (HTTP for
  `/pending/orgs`), `Minter` (`PlatformDiscoveryEIA`/`OrgWorkEIA`/`OrgBeadsEIA`),
  `ProvisioningTokenStore.TokenForOrg`, `CredentialHolder` (atomic swap), and a
  `TokenFunc` seam on the three B2 adapters. httptest/fake unit tests.
- **CODE 5 (`sts_exchange.go`):** `MachineLogin` + `ExchangeEIA` + the DPoP-proof
  builder (ES256 over `htm|htu|iat|jti`) behind a clock/key interface. Unit-test
  the proof shape against the live `DPoPVerifier` htu/htm expectations (the only
  net-new crypto; a wrong htu 400s every login).
- **CODE 6 (`accounts_provision.go`):** the caller-less provisioning-token Accounts
  client (step 2 above). Test: request carries a `provToken` bearer and **no**
  `caller_user_id`; 409→fetch.
- **CODE 7 (`refresh.go` + `main.go`):** `RunPass` — discovery → per-org mint
  (bounded concurrency + `errors.Join`) → `holder.Swap` → per-org `RunOnce`.
  Fail-closed: discovery failure → no-op pass; one org's mint failure → others
  proceed. Replace the `os.Getenv("*_EIA")` reads with `TokenFunc`-backed adapters;
  ticker ~60 s.
- **INFRA 1–4:** Deployment in the **identity-v0 accounts namespace** (NEVER
  corp-public — that placement killed merged #25), dedicated SA, no admin token,
  non-root; Cilium egress allow-list (crucible.ops:443, accounts loopback, STS
  loopback, beads-web:443; default-deny, no ingress); ESO **file** mounts (platform
  key + DPoP key on a separate mount/role from the per-org tokens); one-time
  operator bootstrap (seed the platform discovery SP+key carrying `city.provision`
  via break-glass/OpenBao — it is platform-only and cannot be self-minted through
  the org plane; seed one provisioningToken per onboarded org; add the platform
  sp_id to `discoverySubjects`).

## Security properties

- **No standing cross-org EIA.** At rest the pod holds only (a) ONE platform
  discovery crucible key scoped to the single verb `city.provision`, and (b) per-org
  provisioningTokens that mint nothing without *also* passing the Accounts org-bound
  gate + `tokenOrg==path-org` re-gate. Every wire EIA is just-in-time, ~90 s,
  dropped at pass end.
- **Discovery is ID-only, double-gated, side-effect-free.** Leaking its result
  leaks only "these org_ids have a pending city." A leaked `city.provision` EIA
  cannot enumerate unless it is *also* the allow-listed platform subject.
- **`city.provision` rides ONLY the platform EIA**; per-org work EIAs carry
  `sandbox.*` + `city.work`, so a leaked org EIA cannot reach `/pending/orgs`.
- **Per-org work is org-bounded by construction** (`verifiedCaller` +
  `List(p.Tenant)`; the work EIA's tenant **is** the org; foreign city_id → 404).
- **Audience separation is cryptographic** (STS product-pinning + mono-product key
  scopes) — the crucible leg can never mint a beads EIA.
- **Fail-closed everywhere**; SP-disable + `provisioning_token.status=revoked` are
  instant kill-switches.
- **Named residual risk (not papered over):** the EIA itself has **no cnf/DPoP
  binding** — only the STS *session* is DPoP-bound (`eia.Verify` checks
  sig+iss+aud+exp). Any minted EIA is a replayable bearer for its ~90 s window.
  Mitigation is TTL + audit + per-org tenant scoping; we do not claim PoP.
- **Blast radius if the running provisioner is fully compromised, BOUNDED:**
  (1) enumerate which org_ids have a pending city (ID-only); (2) the platform EIA
  carries `city.provision` only → cannot read detail/complete/launch; (3) the
  currently-mounted per-org tokens → for *those opted-in orgs only*, mint
  `sandbox.*`+`beads:*` keys (sandbox launch/exec + beads:write to those ledgers) —
  fully audited (system sentinel), instantly revocable, conferring nothing on
  non-opted-in orgs, no adminToken, no platform-wide mint, no cross-product reach
  beyond crucible+beads, no persistence past revocation.

## Open questions for the founder

1. **Is `beads` in the STS signers map (a mintable audience) today?** The STS
   code is generic (signer-map keys *are* the audience allow-list — deploy config).
   If NOT, the `aud=beads` leg can't be minted and we fall back to the interim
   per-org `bts_` gateway token (same provisioningToken→Accounts mint, different
   wire credential). The `BeadsWebProvisioner` adapter is **agnostic** (its `Token`
   header works either way), so this changes only the `Minter.OrgBeadsEIA` body, not
   the build shape. Confirm before CODE 4/5 land.
2. **Per-pass token-mount narrowing (recommended for v1 if cheap on cherry):**
   mount only *this-pass discovered* orgs' provisioningTokens rather than all
   opted-in. This is the difference between "compromise exposes currently-active
   orgs" and "compromise exposes the whole enrolled fleet's ledgers." Needs an
   on-demand ESO/fetch path.

**Resolved here (no founder input needed):** the `?org_id=` cross-tenant detail
read is **rejected** (the central leak all three critiques converged on); the
per-org work scope is a **dedicated `crucible:city.work`** (open-Q2), not a widening
to `sandbox.read`; the `discoverySubjects` allow-list is **config-pinned** in
`UseCityProvisioning` for v1 (an Accounts platform-grant is the upstream-honest
follow-up, open-Q4).

## Critiques that forced the shape (evidence)

- **security-min** "add `?org_id=` to `/pending`" → would make `/pending` a
  cross-tenant read of full `cityPendingView` detail gated only by a scope. Rejected.
- **operability-min** "one platform key yields `aud=crucible` AND `aud=beads`" →
  structurally impossible: `ResolveKey` returns one `Product`, STS pins
  `Audience=keyRes.Product` and 403s cross-audience, `ValidateScopesForProduct`
  forbids cross-namespace key scopes. Rejected.
- **upstream-min** "mint `city.provision` onto the per-org EIAs" → server `auth()`
  is a bare `HasScope`, so every org EIA would satisfy cross-org discovery. Rejected
  → `city.provision` is platform-discovery-only; per-org work uses `city.work`.
