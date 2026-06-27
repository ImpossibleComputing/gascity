# B6 — Controller image for the Crucible-sandbox launch (spec + status)

**Status:** scaffold built + grounded; credential bootstrap (eia-helper) is the remaining
live-verified piece. The autonomous provisioner (B2/B3) launches this image as a Crucible gVisor
sandbox; it is the one artifact the provisioner *launches but does not build*.

## What the provisioner passes (B2 `controller_adapter.go`)

`POST /v0/sandboxes` with `image` + env: `GC_CITY_NAME`, `GC_PACK`, `GC_DOLT_HOST` (the hosted beads
dolt gateway host:port — added in `d0a7328`), `GC_DOLT_DATABASE` (the city's `bd_prj_<id>`),
`GC_WORKSPACE_ID`. The orchestrator credential is delivered to the sandbox **out of band**
(OpenBao/runtime), not on this call.

## Artifacts built here

- `contrib/k8s/gc-controller-crucible-entrypoint.sh` — env-driven entrypoint (shellcheck-clean):
  1. `gc init --template $GC_PACK --name $GC_CITY_NAME /city` (idempotent; skips if `.gc` exists).
  2. Wire bd to the HOSTED ledger (never embedded dolt): write `/city/.beads/metadata.json` with
     `{backend:"dolt", dolt_mode:"server", dolt_database:$GC_DOLT_DATABASE}` and project
     `GC_DOLT_HOST`/`GC_DOLT_PORT` + `GC_BEADS_BACKEND=dolt`. (Grounded in `cmd/gc/bd_env.go` +
     `internal/beads/contract` — gc resolves the connection from metadata + the `GC_DOLT_*` env;
     `dolt_host` in metadata is deprecated, so host/port ride env.)
  3. Set the beads credential (see below), fail-closed if absent.
  4. `exec gc start --foreground /city`.
- `contrib/k8s/Dockerfile.controller-crucible` — extends `gc-agent` (gc + bd + dolt baked), installs
  the entrypoint. `docker build --check` clean. No kubectl/copy-in: child agents spawn via the
  Crucible API (Model B), not K8s.

## The remaining piece — credential bootstrap (eia-helper)

bd authenticates to the hosted gateway with a short-lived **beads EIA as the dolt username**
(EIA-as-username — see the bd hosted-gateway write fix). The EIA is ~90s, so bd must **re-mint on
connect** via a dolt credential command. The controller mints it from its **orchestrator SP key**
(written to OpenBao by the create-city path, at the city's `SecretRef`) through STS machine-login →
`aud=beads` EIA — exactly the spine the provisioner already implements in `internal/cityprovision`
(`STSExchanger` + `AccountsProvisioner`), but run *inside the controller* against its own key.

To finish B6:
1. **Build the eia-helper** — a small binary/script the controller runs as `GC_DOLT_CRED_CMD`: read
   the orchestrator key (mounted from OpenBao), STS machine-login + token-exchange `aud=beads`, print
   the EIA bd uses as the dolt credential. (It can reuse `STSExchanger` from the provisioner package,
   or be a thin `gc` subcommand.) The same mechanism yields the `aud=crucible` EIA the controller
   needs to spawn child agents via the Crucible API.
2. **Deliver the orchestrator key to the sandbox** — the provisioner/crucible must mount the city's
   OpenBao `SecretRef` into the controller sandbox (out-of-band, per the adapter contract). Confirm
   the Crucible sandbox API supports a secret mount or an in-sandbox OpenBao fetch.
3. **Set `GC_DOLT_CRED_CMD=/usr/local/bin/eia-helper`** in the Dockerfile (the commented line) once
   the helper is installed.
4. **Verify live**: build the image (base→agent→controller), launch via the provisioner against a
   real hosted beads project + STS, and confirm the controller connects, `gc start` comes up, and a
   child agent + a slung bead work end-to-end (the spike proved this manually; B6 packages it).

## Why this is the honest stopping point for B6

Everything above the credential bootstrap is built and statically verified (shellcheck + docker
--check; the gc/bd binaries the image bakes are the shipping product). The bootstrap itself is a live
integration against OpenBao + STS + the hosted gateway that cannot be unit-verified from the dev
sandbox — it needs the cluster, so it is scoped here for the session that has that access.
