# Release Gate: hook-claim continuation nudge

Date: 2026-06-30
Deployer: gascity/deployer

## Scope

- Deploy bead: ga-g0kbyr
- Source bead: ga-zdunsh
- Review bead: ga-kxo9pk
- Reviewed branch: origin/work/ga-zdunsh-hook-claim-continuation-nudge
- Reviewed commit: 614468a1944cc342b80b49138a5b459efbf338f8
- Gate branch: release/ga-g0kbyr-hook-continuation-nudge
- Base checked: origin/main@4a07b03c256b837a9b409609a46cd7e016eb4d8c

The reviewed branch is stacked on the graph-only readiness / idle-respawn base
commits bfa6a0a1f and b3e98d2cb, already proposed separately as PR #3835.
Those base commits touch the same session/reconciler propagation path and are a
functional dependency for this hook-claim continuation nudge.

## Gate Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present | PASS | `bd show ga-kxo9pk` is closed with close reason `pass`; notes contain `VERDICT: PASS` for commit `614468a1944cc342b80b49138a5b459efbf338f8`. |
| 2 | Acceptance criteria met | PASS | `cmd/gc/cmd_hook_claim.go` enqueues `hook-claim-continuation` only when a freshly claimed bead has `gc.kind=workflow` and assigned continuation siblings; it targets the claiming session name. `cmd/gc/cmd_hook_claim_continuation_nudge_test.go` covers workflow root enqueues, step bead does not enqueue, and root with no siblings does not enqueue. ACP dispatcher dependency is documented at the enqueue site. In-flight ceiling sizing guidance is carried into the PR review notes so merge authority can publish it without deployer replying to an external issue thread. |
| 3 | Tests pass | PASS | `go test ./cmd/gc -run 'TestHookClaimContinuationNudge|TestSelectIdleProbeTargets|TestBeginIdleRespawnDrainIfIdle|TestDrainReasonCancelable'` -> `ok github.com/gastownhall/gascity/cmd/gc 0.588s`; `make test-fast-parallel` -> `All fast jobs passed`; `go vet ./...` -> clean; `GOCACHE=$(mktemp -d) go build ./cmd/gc` -> clean; `go run honnef.co/go/tools/cmd/staticcheck@latest -checks=QF1001 ./cmd/gc ./internal/beads ./test/docsync` -> clean. |
| 4 | No high-severity review findings open | PASS | Review notes for ga-kxo9pk report no blockers and only a non-code public-response note; no HIGH findings are present. |
| 5 | Final branch is clean | PASS | Before adding this gate file, `git status --short --branch` showed a clean branch tracking `origin/work/ga-zdunsh-hook-claim-continuation-nudge`. Deployer verifies clean status again after committing this gate file and before push. |
| 6 | Branch diverges cleanly from main | PASS | `git merge-tree --write-tree origin/main HEAD` exited 0 and produced tree `b849474dea300fc5604aa43b56ececca1941b53d`; `git diff --check origin/main...HEAD` passed. |
| 7 | Single feature theme | PASS | The commit set is one session/reconciler propulsion theme: graph-only readiness, idle-respawn safety net, and hook-claim continuation nudge for pool graph.v2 roots. Removing the base readiness commits would undermine the nudge feature, so the stack is not a bundle of independent features. |

## Test Log Summary

- PASS: focused `cmd/gc` acceptance tests for hook continuation nudge and idle-probe eligibility.
- PASS: fast unit baseline via `make test-fast-parallel`.
- PASS: `go vet ./...`.
- PASS: clean-cache build of `./cmd/gc`.
- PASS: QF1001-only staticcheck over touched packages using a Go 1.26-built analyzer.

Note: the installed `/home/jaword/go/bin/staticcheck` binary is built with Go
1.25.8 and cannot analyze this Go 1.26 module graph. A full local
`staticcheck ./...` invocation fails before branch diagnostics with Go-version
compile errors from dependencies. The targeted QF1001 check above was run via
`go run honnef.co/go/tools/cmd/staticcheck@latest` with the repo's Go 1.26
toolchain to cover the prior lint failure class.

## PR Notes To Carry Forward

- The user-visible behavior change is that a pool graph.v2 root claimed by a
  newly spawned pool session now queues a continuation nudge to the concrete
  session slot, so the first step can start without an operator-issued
  `gc session nudge`.
- No new config, schema, or bead metadata keys are introduced.
- ACP pool sessions still require `daemon.nudge_dispatcher = "supervisor"` for
  queued delivery; the legacy per-session poller cannot deliver to ACP sessions.
- Operators using in-flight ceilings should size for at least one workflow root
  plus the active step per concurrent formula. In shorthand:
  `max_concurrent_formulas * (1 + max_parallel_steps)`.
