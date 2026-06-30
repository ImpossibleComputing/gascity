# P4 conversion contract — migrate snapshot consumers off raw session beads

You are converting ONE file's session-bead **field reads** to the typed
`session.Info` front door. The foundation (P1–P3) is done and equivalence-proven:
the `*Info` classifier siblings and the snapshot's typed accessors already agree,
byte-for-byte, with their raw-bead originals on every bead shape. So a faithful
swap CANNOT change behavior.

## Hard rules

1. **Byte-identical behavior.** Never change logic, ordering, or values. Only
   swap the *source* of a session attribute from a raw bead to the typed `Info`.
2. **Do NOT invent siblings.** If a site needs a classifier/helper that has no
   `Info` form yet, STOP and report it (file:line + the missing function). Do not
   create one — the orchestrator owns shared foundation edits.
3. **Work beads stay raw.** Only **session** beads convert. A `.Metadata[...]` read
   on a work/mail/order/nudge bead, or a bead passed to a store op
   (`store.Update/Get/Close`), is out of scope — leave it.
4. **The codec edge is exempt.** `session_bead_snapshot.go` (`newSessionBeadSnapshot`)
   legitimately decodes raw beads. Never touch it.

## Snapshot accessor swaps

| Raw (returns `beads.Bead`)                  | Typed (returns `session.Info`)        |
| ------------------------------------------- | ------------------------------------- |
| `snap.Open()` (loop var `beads.Bead`)       | `snap.OpenInfos()` (loop var `Info`)  |
| `snap.FindByID(id)`                         | `snap.FindInfoByID(id)`               |
| `snap.FindSessionBeadByTemplate(t)`         | `snap.FindInfoByTemplate(t)`          |
| `snap.FindSessionBeadByNamedIdentity(x)`    | `snap.FindInfoByNamedIdentity(x)`     |

`FindSessionNameByTemplate` / `FindSessionNameByNamedIdentity` already return
clean strings — leave them.

## Classifier sibling table (bead form → Info form)

`isPoolManagedSessionBead`→`isPoolManagedSessionInfo` ·
`isNamedSessionBead`→`isNamedSessionInfo` · `isManualSessionBead`→`isManualSessionInfo` ·
`isDrainedSessionBead`→`isDrainedSessionInfo` · `isFailedCreateSessionBead`→`isFailedCreateSessionInfo` ·
`isEphemeralSessionBead`→`isEphemeralSessionInfo` · `isStaleCreating`→`isStaleCreatingInfo` ·
`isPendingPoolCreate`→`isPendingPoolCreateInfo` · `isKnownState`→`isKnownStateInfo` ·
`isPoolSessionSlotFreeable`→`isPoolSessionSlotFreeableInfo` ·
`sessionOrigin`→`sessionOriginInfo` · `sessionBeadAgentName`→`sessionBeadAgentNameInfo` ·
`sessionBeadStoredTemplate`→`sessionBeadStoredTemplateInfo` ·
`resolvedSessionTemplate(b,cfg)`→`resolvedSessionTemplateInfo(i,cfg)` ·
`normalizedSessionTemplate(b,cfg)`→`normalizedSessionTemplateInfo(i,cfg)` ·
`isCanonicalPoolManagedSessionBeadForTemplate(b,t)`→`isCanonicalPoolManagedSessionInfoForTemplate(i,t)` ·
`stampedPoolQualifiedIdentity`→`stampedPoolQualifiedIdentityInfo` ·
`beadOwnsPoolSessionName`→`infoOwnsPoolSessionName` ·
`sessionBeadAssigneeIdentities`→`sessionBeadAssigneeIdentitiesInfo` ·
`sessionMetadataState`→`sessionMetadataStateInfo` ·
`sessionWakeAttempts`→`sessionWakeAttemptsInfo` ·
`sessionIsQuarantined(b,clk)`→`sessionIsQuarantinedInfo(i,clk)`.

**No Info form yet (report, do not convert the site if it's the only read):**
`namedSessionMode`, `namedSessionIdentity`, `namedSessionContinuityEligible`,
`isManualSessionBeadForAgent`, `isEphemeralSessionBeadForAgent`,
`isLegacyManualSessionBeadForAgent`, `sessionAgentMetricIdentity`, `existingPoolSlot`,
`sessionWakeRequestedCreate`, `sessionWakeHasRunnableTemplate`,
`isRetiredSessionModelOwner`, `beadUsesACPTransport`.

## Direct field reads (`b.Metadata[...]` / `b.X` → `info.Field`)

| Raw read                                  | `Info` field                                  |
| ----------------------------------------- | --------------------------------------------- |
| `b.ID`                                     | `i.ID`                                         |
| `b.Title`                                  | `i.Title`                                      |
| `b.Metadata["template"]`                   | `i.Template`                                   |
| `b.Metadata["alias"]`                      | `i.Alias`                                      |
| `b.Metadata["agent_name"]`                 | `i.AgentName`                                  |
| `b.Metadata["provider"]`                   | `i.Provider`                                   |
| `b.Metadata["transport"]`                  | `i.Transport`                                  |
| `b.Metadata["configured_named_identity"]`  | `i.ConfiguredNamedIdentity`                    |
| `b.Metadata["configured_named_mode"]`      | `i.ConfiguredNamedMode`                        |
| `b.Metadata["common_name"]`                | `i.CommonName`                                 |
| `b.Metadata["pool_slot"]`                  | `i.PoolSlot`                                   |
| `b.Metadata["session_origin"]`             | `i.SessionOrigin`                              |
| `b.Metadata["pending_create_started_at"]`  | `i.PendingCreateStartedAt`                     |
| `b.Metadata["quarantined_until"]`          | `i.QuarantinedUntil`                           |
| `b.Labels`                                 | `i.Labels`                                     |

### Fidelity-trap RAW fields — read these when the original read raw metadata

- `b.Metadata["session_name"]` (raw, NO `sessionNameFor(ID)` fallback) → **`i.SessionNameMetadata`**.
  (`i.SessionName` carries the fallback — only use it if the original called a path that applied the fallback.)
- `b.Metadata["state"]` (raw, untrimmed, NOT blanked on closed) → **`i.MetadataState`**.
  (`i.State` is the normalized liveness form — different value space; do not substitute.)
- `b.Metadata["manual_session"]` (raw, untrimmed) → **`i.ManualSessionMetadata`**.
  (`i.ManualSession` is the trimmed bool.)

Booleans like `pool_managed == "true"`, `dependency_only == "true"` already have
typed bools: `i.PoolManaged`, `i.DependencyOnly`. Use them.

## Per-site procedure

1. Replace the snapshot accessor; the loop/return var is now `session.Info`.
2. For each field read on that var, substitute per the tables (mind RAW fields).
3. For each classifier call on that var, substitute the `Info` sibling.
4. If a site ALSO needs the raw bead (e.g. it passes it to a store op or a
   `[]beads.Bead` helper that has no `Info` form), leave that path on the bead —
   only the *field reads* convert. Note it in your report.

## Verify (mandatory before reporting success)

```
go build ./cmd/gc/
go test ./cmd/gc/ -run 'TestSessionClassifierInfoEquivalence|TestSessionSnapshotInfoEquivalence' -count=1
# plus any *_test.go whose name matches your file's subject, e.g. -run TestSoftReload
go vet ./cmd/gc/ 2>&1 | head
```

`git checkout go.sum` if it changed. Report: the unified diff, the verify output,
any sites left on the bead (with why), and any missing-sibling gaps you hit.
