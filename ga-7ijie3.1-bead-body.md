# Design: ValidateSyntheticRepo Fast/Full Split

**Bead:** ga-7ijie3.1  
**Designer:** gascity/designer  
**Date:** 2026-06-14  

## Visual Diagrams

**Diagram 1 — Two-Tier Validation Architecture:**
https://excalidraw.com/#json=YmUWh-XxV4BSf2RmuKnBf,0aiQNlgA51FW2HxixgiPMg

**Diagram 2 — Performance Impact (Before vs After):**
https://excalidraw.com/#json=Udt3IHjjSSf1aEArFesIk,PeZMb01ivnhw4skFmIynWg

Local excalidraw files:
- `/home/jaword/projects/gc-management/.gc/worktrees/gascity/designer/ga-7ijie3.1-validation-tiers.excalidraw`
- `/home/jaword/projects/gc-management/.gc/worktrees/gascity/designer/ga-7ijie3.1-performance-impact.excalidraw`

---

## 1. Design Overview

The fix introduces `ValidateSyntheticRepoFast` — a lightweight validation function for the
resolution hot path. The existing `ValidateSyntheticRepo` remains unchanged for install/doctor
paths where full byte-for-byte integrity matters.

**Key insight:** the synthetic cache is content-hash-keyed and immutable after materialization.
Proving the cache was written by the current binary requires only the marker file — not a walk
of every file. `ValidateSyntheticRepoFast` exploits this trust model to do O(1) I/O instead
of O(files) I/O.

---

## 2. API Design

### Function signature

```go
func ValidateSyntheticRepoFast(dir, commit string) error
```

**Identical signature to `ValidateSyntheticRepo`.** Drop-in replacement at call sites — no
callers need new error-handling logic. Same parameter naming eliminates confusion about argument
ordering.

### Naming rationale

`ValidateSyntheticRepoFast` is preferred over alternatives:

| Candidate | Assessment |
|---|---|
| `ValidateSyntheticRepoFast` | **Chosen.** "Fast" communicates the trade-off; implies a slower variant exists |
| `ValidateSyntheticRepoLite` | "Lite" implies reduced capability, not performance |
| `ValidateSyntheticRepoQuick` | "Quick" is informal; "fast" is idiomatic in Go stdlib |
| `ValidateSyntheticRepoHot` | "Hot" is call-site context, not function identity |

### Placement

Both functions in `internal/builtinpacks/registry.go`, immediately adjacent.
`ValidateSyntheticRepoFast` appears AFTER `ValidateSyntheticRepo` so the full version is
encountered first when reading top-to-bottom.

---

## 3. Fast Path Logic

Steps executed by `ValidateSyntheticRepoFast`:

1. `os.Lstat(dir)` — verify dir exists, is a directory, is not a symlink
2. `os.ReadFile(syntheticMarkerFile)` — read the marker file written at materialization time
3. `toml.Decode(marker)` — parse and validate marker schema (schema == 1, repository matches)
4. `gitutil.SameCommit(marker.Commit, commit)` — confirm cache was materialized for this commit
5. `syntheticContentHashOnce()` — get the memoized hash of the current binary's embedded pack content
6. `marker.ContentHash == wantHash` — confirm marker was written by the same binary version

If all six checks pass: the cache is valid for the resolution path.

**What is deliberately NOT checked:**
- `validateSyntheticRepoFileSet(dir)` — WalkDir over all materialized files (O(files))
- `validatePackFiles(pack, dir)` — ReadFile + bytes.Equal for every file (O(file bytes))

These checks are skipped because:
- Cache directory is keyed to the binary content hash — a different binary produces a different key
- `MaterializeSyntheticRepo` is the only writer; called under a write lock
- Individual file corruption is detected at use time (template rendering errors)
- `gc doctor` provides an explicit health-check escape hatch

### Secondary bug to fix simultaneously

The existing `ValidateSyntheticRepo` at line 285 calls `SyntheticContentHash()` directly,
bypassing `syntheticContentHashOnce`. Also fix line 285:

```go
// Before (line 285 in registry.go):
wantHash, err := SyntheticContentHash()
// After:
wantHash, err := syntheticContentHashOnce()
```

This recovers a small amount of latency on full-validation paths too, and is consistent with
the design intent of `syntheticContentHashOnce`.

---

## 4. Call Site Map

| File | Line | Current | Change | Purpose |
|---|---|---|---|---|
| `pack_include.go` | 301 | `ValidateSyntheticRepo` | **→ Fast** | resolveBundledSourceWithoutLock (read-lock pre-check) |
| `pack_include.go` | 420 | `ValidateSyntheticRepo` | **→ Fast** | validateInstalledRemoteCache (canonical-pin check) |
| `pack_include.go` | 305 | `ValidateSyntheticRepo` | Keep full | post-materialization, inside write lock (rare) |
| `packman/cache.go` | 69 | `ValidateSyntheticRepo` | Keep full | gc import install cache hydration |
| `packman/check.go` | 232 | `ValidateSyntheticRepo` | Keep full | gc doctor health check |
| `packman/install.go` | 50 | `ValidateSyntheticRepo` | Keep full | install path |

---

## 5. Error Message Design

All error message strings are identical to the existing `ValidateSyntheticRepo` where checks
overlap. No new error message formats. Callers treat all `ValidateSyntheticRepo*` errors as
"cache invalid, re-materialize" — the specific error text matters only for diagnostics.

| Check | Error string |
|---|---|
| dir missing | `"missing bundled pack cache marker"` |
| stat error | `"checking bundled pack cache root: %w"` |
| symlink root | `"bundled pack cache root %q is a symlink"` |
| not a directory | `"bundled pack cache root %q is not a directory"` |
| marker missing | `"missing bundled pack cache marker"` |
| marker read error | `"reading bundled pack cache marker: %w"` |
| marker parse error | `"parsing bundled pack cache marker: %w"` |
| schema mismatch | `"unsupported bundled pack cache marker schema %d"` |
| repository mismatch | `"bundled pack cache repository %q does not match %q"` |
| commit mismatch | `"bundled pack cache commit %q does not match %q"` |
| content hash mismatch | `"bundled pack cache content hash %q does not match current binary %q"` |

---

## 6. Developer Experience (A11y Audit)

Applying usability principles to the developer-facing API:

**Perceivability — errors are clear and actionable:**
- All errors include the problematic value (`%q`) ✓
- Error messages explain what was wrong, not just that something was wrong ✓

**Operability — API is hard to misuse:**
- Identical signature: callers cannot accidentally swap arguments ✓
- Both functions adjacent in the same file: developer reads both godoc before choosing ✓
- `ValidateSyntheticRepoFast` is purely read-only: no side effects on incorrect use ✓
- If fast path fails, caller falls through to `MaterializeSyntheticRepo` — the safe path ✓

**Understandability — doc comment must convey the trade-off:**

Proposed godoc for `ValidateSyntheticRepoFast`:
```go
// ValidateSyntheticRepoFast is the lightweight resolution-path variant of
// ValidateSyntheticRepo. It verifies only the marker file commit and content
// hash — no file-by-file comparison. The marker hash was computed from the
// full embedded content at materialization (MaterializeSyntheticRepo) and is
// sufficient to prove the cache was written by the current binary.
//
// Use this on the resolution hot path where ValidateSyntheticRepo is called
// on every gc subprocess invocation. Use ValidateSyntheticRepo for
// gc import install and gc doctor where full file integrity matters.
func ValidateSyntheticRepoFast(dir, commit string) error {
```

**Robustness — consistent behaviour under error conditions:**
- Fast path errors cause the same fallback as full path errors ✓
- No new error types — callers see the same `error` interface ✓

---

## 7. Test Coverage Requirements

All `TestValidateSyntheticRepoDetects*` tests must be duplicated for the fast variant.
The existing `materializeTestRepo` helper is reusable (package-internal).

### Required new tests

| Test name | What it checks |
|---|---|
| `TestValidateSyntheticRepoFastAcceptsValidRepo` | Happy path: materialized repo passes |
| `TestValidateSyntheticRepoFastAcceptsEquivalentCommit` | SameCommit handles abbreviated/uppercase |
| `TestValidateSyntheticRepoFastRejectsSymlinkRoot` | Symlink at cache root |
| `TestValidateSyntheticRepoFastRejectsNonDirectoryRoot` | File (not dir) at cache root |
| `TestValidateSyntheticRepoFastRejectsMissingMarker` | No marker file |
| `TestValidateSyntheticRepoFastRejectsWrongCommit` | Commit mismatch |
| `TestValidateSyntheticRepoFastRejectsWrongContentHash` | Tampered marker ContentHash |

The last test is the most critical — it proves the fast path catches the "wrong binary version"
case. See full design doc for the test body.

### Tests NOT required (fast path intentionally skips these checks)

- `TestValidateSyntheticRepoFastRejectsTamperedContent` — fast path does not check file contents
- `TestValidateSyntheticRepoFastRejectsTamperedMode` — fast path does not check file modes
- `TestValidateSyntheticRepoFastRejectsUnexpectedFiles` — fast path does not walk file set
- `TestValidateSyntheticRepoFastRejectsUnexpectedRootSibling` — same
- `TestValidateSyntheticRepoFastRejectsSymlinkAncestor` — WalkDir check is full-path only

Add a comment in `registry_test.go` clarifying this intentional gap.

---

## 8. Performance Recovery

| | Pre-#3344 baseline | Post-#3344 (broken) | After fix |
|---|---|---|---|
| Per-invoke cost | ~0.15ms | ~47ms (bypasses Once) | ~0.5ms |
| Per-subprocess (3 packs) | ~0.45ms | ~141ms | ~1.5ms |
| 290 subprocesses (CI workflow) | ~0.13s | ~40.9s | ~0.44s |
| TestRetryManagedPooledWorkerRecovers... | ~86.7s | ~127-133s | ≤90s target |

---

## 9. Files Changed

```
internal/builtinpacks/registry.go
  + ValidateSyntheticRepoFast (new function, ~30 lines)
  ~ ValidateSyntheticRepo line 285: SyntheticContentHash() → syntheticContentHashOnce()

internal/builtinpacks/registry_test.go
  + 7 new TestValidateSyntheticRepoFast* tests

internal/config/pack_include.go
  ~ Line 301: ValidateSyntheticRepo → ValidateSyntheticRepoFast
  ~ Line 420: ValidateSyntheticRepo → ValidateSyntheticRepoFast
```

Total diff: ~80 lines (+60 new function, +7 tests, 2 call-site changes, 1 line-285 fix).
