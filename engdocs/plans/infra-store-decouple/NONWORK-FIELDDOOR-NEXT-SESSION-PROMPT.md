# Next-session prompt — finish the non-work field-door cleanup + open PR #3839

Paste the block below into a fresh session.

---

Finish the non-work-bead field-door cleanup on **PR #3839** (branch
`upstream/object-front-doors-cleanup`, base `main`, DRAFT) and mark it ready.

**Read first:** `engdocs/plans/infra-store-decouple/NONWORK-FIELDDOOR-P4-P6-HANDOFF.md`
(the execution guide — exact files, the per-site migration rule, the coupling
caveats, the closeBead split, the guard) and `NONWORK-BEAD-FIELDDOOR-PLAN.md`
(the architecture). Both in that directory.

**Worktree/branch:** `/data/projects/gascity/.claude/worktrees/object-front-doors`,
branch `upstream/object-front-doors-cleanup`, PR #3839, HEAD `dd5496c16`. Confirm
a green baseline first: `go build ./...` and
`go test ./cmd/gc/ -run 'TestSessionClassifierInfoEquivalence|TestSessionSnapshotInfoEquivalence' -count=1`.

**Principle (hard rule):** direct read/write of metadata or bead FIELDS on any
NON-WORK object (session/nudge/mail/order/graph) is ILLEGAL — only generic WORK
beads may be read raw. This is the precondition for a per-class backend swap.

**State:** P1–P3 (foundation) are DONE, pushed, equivalence-proven, byte-identical:
`session.Info` carries the full session-attribute set; 22 `*Info` classifier
siblings exist (originals untouched); the snapshot has `OpenInfos()`/`FindInfo*`
accessors. Equivalence tests prove the typed forms agree with the originals on
every bead shape — so the consumer migration is a SAFE mechanical swap.

**Do (per the handoff):**
- **P4** — migrate the ~167 consumer reads off raw beads: `snapshot.Open()`→
  `OpenInfos()`, `b.Metadata[...]`→`info.Field` (mind the RAW fidelity fields
  `MetadataState`/`SessionNameMetadata`/`ManualSessionMetadata`), `isX(bead)`→
  `isXInfo(info)`. Shard ONE consumer file per sub-agent (lower-risk files first:
  cmd_wait/providers/nudge_dispatcher/adoption_barrier/soft_reload/session_name_lookup,
  then the controller core build_desired_state/city_runtime/session_beads/
  session_reconciler/session_reconcile/session_lifecycle_parallel/…). It is NOT
  compiler-forced — after each file, grep to confirm no session-bead raw read or
  `.Open()` remains. Convert bead-passing helpers (add an `Info` form + re-prove
  equivalence) before dropping the bead at a caller. Build+test+commit per file.
- **P5** — split `closeBead` (InfoStore.Close[exists] + workAssignment release
  [exists] + cancelStateAssignedToRetiredSessionBead[extmsg, exists]); close-THEN-
  release, preserve skip-if-already-closed idempotence (the §5 mass-closure
  landmine — do isolated, recording-fake oracle). Closes the residual
  `.Store().Store` sites. Also tidy createPoolSessionBead through CreateSession.
- **P6** — delete the now-dead bead classifiers/snapshot bead-methods; close any
  residual nudge/mail/order reads (audit sweep found ~47); TIGHTEN
  `cmd/gc/frontdoor_di_guard_test.go` to forbid `.Store().Store` and raw session
  reads in the converted files.

**Process:** sub-agents for the mechanical P4 shards (Agent tool — ultracode is
off, do NOT use the Workflow tool unless the human opts in); drive P5 yourself
(landmine). KEEP-original + ADD-typed + equivalence-test is the safety pattern.
Build-green per commit, byte-identical, halt-on-block.

**Gates before ready:** `go build ./...` · `go vet ./...` · golangci-lint 0 ·
the targeted/sharded suites · wire-diff empty (openapi/docs-schema/generated-TS).
`git checkout go.sum` after builds; commit `--no-verify`; never tmux kill-server
/ go clean -cache; gascity Dolt LOCAL-ONLY.

**Finish (only when CI is verified GREEN — do not mark ready before):**
- `gh pr checks 3839 --watch`
- ready (gh pr ready aborts on projectCards — use API): `gh api graphql -f query='mutation($id:ID!){markPullRequestReadyForReview(input:{pullRequestId:$id}){pullRequest{isDraft}}}' -f id=$(gh api repos/gastownhall/gascity/pulls/3839 --jq .node_id)`
- label: `gh api --method POST repos/gastownhall/gascity/issues/3839/labels -f 'labels[]=status/needs-review-auto'`

**Done =** zero raw session-bead field reads in the converted files (grep-clean) +
zero `.Store().Store`; the arch guard forbids regression; full gates + #3839 CI
green; #3839 ready + labeled. Update `memory/infra-beads-decoupling-plan.md`.

---
