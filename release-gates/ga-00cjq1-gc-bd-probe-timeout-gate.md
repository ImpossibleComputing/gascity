# Release Gate: ga-00cjq1 GC_BD_PROBE_TIMEOUT

Date: 2026-06-17
Deployer branch: `release/ga-00cjq1-gc-bd-probe-timeout`
Base: `origin/main` at `70519347fde7944f1aafb0e6792b64a6b24d34a8`
Feature head before gate commit: `abbac2b912295f88695a4d1317a5c4f46a6c24ec`
Source branch: `origin/builder/ga-x5ocw1`
Deploy bead: `ga-00cjq1`
Source review bead: `ga-ciqexy`

## Scope

Single-bead deploy. The branch contains one feature commit:

- `abbac2b91 feat(pool): make bdProbeTimeout configurable via GC_BD_PROBE_TIMEOUT env var (ga-x5ocw1)`

Changed files:

- `cmd/gc/pool.go`
- `cmd/gc/pool_test.go`
- `test/integration/review_formula_test.go`

`docs/PROJECT_MANIFEST.md` is not present in this checkout, so no additional
project-manifest release criteria were available. This gate uses the deployer
release criteria from `gc prime` plus the bead acceptance/review evidence.

## Gate Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present | PASS | `bd show ga-ciqexy` contains `REVIEWER VERDICT: PASS`; reviewer mail `gm-wisp-5mb5ymf` says deploy bead `ga-00cjq1` is ready. |
| 2 | Acceptance criteria met | PASS | `cmd/gc/pool.go` reads `GC_BD_PROBE_TIMEOUT`, defaults to `180s`, parses Go durations, and enforces a `5s` floor. `cmd/gc/pool_test.go` covers default, override, floor, zero, and invalid values. `test/integration/review_formula_test.go` injects `GC_BD_PROBE_TIMEOUT=30s` before isolated supervisor env construction. |
| 3 | Tests pass | PASS | `go build ./cmd/gc`; `go test ./cmd/gc -run TestParseBdProbeTimeout -count=1`; `make test-fast-parallel`; `go vet ./...`; `./scripts/test-integration-shard review-formulas-basic-1-of-2`; `git diff --check origin/main...HEAD`. |
| 4 | No high-severity review findings open | PASS | Review notes report no findings to escalate and no security findings; no unresolved HIGH findings are present in the deploy/review bead notes. |
| 5 | Final branch is clean | PASS | `git status --short --branch` was clean before adding this gate file; deployer rechecks cleanliness after committing the gate before push. |
| 6 | Branch diverges cleanly from main | PASS | `origin/main` is the merge base for `origin/builder/ga-x5ocw1`; `git merge-tree $(git merge-base origin/main origin/builder/ga-x5ocw1) origin/main origin/builder/ga-x5ocw1` reported a clean synthetic merge with no conflict markers. |
| 7 | Single feature theme | PASS | Commit set touches only pool probe timeout configuration and the review-formula integration test that consumes the lower timeout. |

## Test Evidence

```text
go build ./cmd/gc
PASS

go test ./cmd/gc -run TestParseBdProbeTimeout -count=1
ok  	github.com/gastownhall/gascity/cmd/gc	0.453s

make test-fast-parallel
All fast jobs passed

go vet ./...
PASS

./scripts/test-integration-shard review-formulas-basic-1-of-2
ok  	github.com/gastownhall/gascity/test/integration	96.382s

git diff --check origin/main...HEAD
PASS
```

## Result

PASS. Open a PR from `release/ga-00cjq1-gc-bd-probe-timeout` to `main`, then
route merge authority to mayor/mpr. Deployer must not merge.
