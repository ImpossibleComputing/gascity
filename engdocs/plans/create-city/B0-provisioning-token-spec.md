# B0 — Accounts `provisioningToken` (red-teamed spec)

**Verdict:** safe-with-listed-mitigations · **Repo:** `gasworks-platform/internal/accounts`

Carve a narrow credential out of the platform god-token (`adminToken`) so the cross-cluster
identity-v0 provisioner can mint per-product SPs/keys for an org **without** `adminToken`
(which corp-public can't reach). The token must be **strictly narrower than `adminToken` on
every axis**.

## Load-bearing security decisions (do not weaken)
1. **DB-backed, per-org token — NOT a static env bearer.** A static `ACCOUNTS_PROVISIONING_TOKEN`
   is all-orgs (org bound only by the caller-controlled URL path) → broader than required. Reject it.
2. **`created_by` = non-NULL system sentinel** `"system:provisioner:identity-v0"`. NULL would exempt
   the SP from `access_review` recert (`WHERE created_by IS NOT NULL`) = a permanent un-certifiable
   principal (authority *excess*). Sentinel keeps provisioner SPs in the recert surface.
3. **Org-forgery defense:** `tokenOrg == path org_id` equality check (403 on mismatch).
4. **Route confinement by mount, not prefix:** `gateProvision` is mounted by name on exactly the 3
   routes; a provisioning bearer at any other route hits a gate that never calls
   `ResolveProvisioningToken` → rejected. Fails closed by construction.

## Implementation
**Migrations** (next free: 061/062):
- `061_provisioning_token`: `provisioning_token(token_hash PK, org_id FK→organization ON DELETE CASCADE, label, status CHECK active|revoked DEFAULT active, created_at)` + index on org_id. Store only `HashToken(plaintext)`.
- `062_service_principal_org_name_uniq`: `CREATE UNIQUE INDEX … ON service_principal (org_id, name)`. **Precondition:** verify no existing `(org_id,name)` dupes before build. Map pg 23505 → 409 `ErrServicePrincipalNameTaken` (create-or-fetch).

**Store:** `ResolveProvisioningToken(ctx, tokenHash) (orgID, err)` — `SELECT org_id WHERE token_hash=$1 AND status='active'`; no-rows → `ErrProvisioningTokenNotResolvable` (revocation is immediate, TOCTOU closed in the query). `CreateServicePrincipalAs(ctx, orgID, name, createdBy, actorKind)` — sets row `created_by` and audit `ActorKind` **independently** (the old `CreateServicePrincipal` infers `ActorKind=user` from `createdBy!=""`, which would mis-stamp the sentinel as a user).

**Gate** (`gateProvision`): machine path first — `Bearer` present **and** `caller_user_id` query absent → `ResolveProvisioningToken` → assert `tokenOrg==path org_id` → stamp `AuditActor{Kind:System, ID:sentinel}` + `provisionOrgCtxKey`. Else human path → existing `gateOrgAdmin` (writes its own 4xx). (Path selection by `caller_user_id` presence avoids the suppressed-ResponseWriter hack.)

**Routes** (`server.go` 193/196/199) → `s.gateProvision(handler)`, **drop `adminToken`**. List/get/revoke/deprecate (194/195/197/198) and `/v0/admin/*` **keep** `s.gate(s.adminToken,…)` (break-glass intact).

**Handlers** read org from `provisionOrgFrom(ctx)` (never path/body); `createdByFor(actor)` = caller (human) / sentinel (machine). MintKey + SetStatus must NOT touch `created_by`; SetStatus passes `actorUserID=""` on machine path so the system actor stands.

## Test matrix (TDD, write first)
ResolveToBoundOrg · MachineCreate-createdBy-is-sentinel · HumanPathUnchanged · OrgForgeryRejected(403) · RejectedOnNonCarveoutRoutes(401) · SPEntersRecert · MintKey+SetStatus-systemActor · ConcurrentSameName-oneWinner(409) · OrgMustExist · TokenNeverInGateOrgAdmin · RevocationImmediate · MigrationsApplyReplaySafe.

## Open risks (carry forward)
Bootstrap: first token row minted via an `adminToken` route or OpenBao seed — **never** self-mintable by a provisioning token. 062 fails on pre-existing dupes — dedup first. Document that operator break-glass (`/v0/admin/*`) is unaffected.
