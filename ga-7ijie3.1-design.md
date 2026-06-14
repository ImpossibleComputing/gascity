# Design: ValidateSyntheticRepo Fast/Full Split

**Bead:** ga-7ijie3.1  
**Designer:** gascity/designer  
**Date:** 2026-06-14  
**Diagrams:**
- Validation tier architecture: `ga-7ijie3.1-validation-tiers.excalidraw`
- Performance impact comparison: `ga-7ijie3.1-performance-impact.excalidraw`

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

### 2.1 Function signature

```go
func ValidateSyntheticRepoFast(dir, commit string) error
```

**Identical signature to `ValidateSyntheticRepo`.** This is the right choice:
- Drop-in replacement at call sites — no callers need new error-handling logic
- Same parameter naming eliminates confusion about argument ordering
- Callers switching from full→fast can do a direct text substitution

### 2.2 Naming

`ValidateSyntheticRepoFast` is preferred over alternatives:

| Candidate | Assessment |
|---|---|
| `ValidateSyntheticRepoFast` | **Chosen.** "Fast" communicates the trade-off; implies a slower variant exists |
| `ValidateSyntheticRepoLite` | "Lite" implies reduced capability, not performance |
| `ValidateSyntheticRepoQuick` | "Quick" is informal; "fast" is idiomatic in Go stdlib (e.g. `math/big.Int.SetBytes`) |
| `ValidateSyntheticRepoHot` | "Hot" is call-site context, not function identity |

### 2.3 Placement

Both functions in `internal/builtinpacks/registry.go`, immediately adjacent.
`ValidateSyntheticRepoFast` appears AFTER `ValidateSyntheticRepo` so the full
version is encountered first when reading top-to-bottom.

---

## 3. Fast Path Logic

Steps executed by `ValidateSyntheticRepoFast`:

1. `os.Lstat(dir)` — verify dir exists, is a directory, is not a symlink
2. `os.ReadFile(syntheticMarkerFile)` — read the marker file written at materialization time
3. `toml.Decode(marker)` — parse and validate marker schema (schema == 1, repository matches)
4. `gitutil.SameCommit(marker.Commit, commit)` — confirm cache was materialized for this commit
5. `syntheticContentHashOnce()` — get the memoized hash of the current binary's embedded pack content
6. `marker.ContentHash == wantHash` — confirm marker was written by the same binary version

If all six checks pass: the cache is valid for the resolution path. Return `nil`.

**What is deliberately NOT checked:**

- `validateSyntheticRepoFileSet(dir)` — WalkDir over all materialized files (O(files))
- `validatePackFiles(pack, dir)` — ReadFile + bytes.Equal for every file (O(file bytes))

These checks are skipped because:
- The cache directory is keyed to the binary content hash (`$GC_HOME/cache/repos/<sha>`)
- A different binary produces a different key, so old caches never serve new binaries
- `MaterializeSyntheticRepo` is the only writer; it's called under a write lock
- Individual file corruption is detected at use time (template rendering errors) — acceptable
  for a gc-managed, user-never-touches directory
- `gc doctor` (`packman/check.go`) provides an explicit health-check escape hatch

### 3.1 Critical implementation note: fix ValidateSyntheticRepo too

The existing `ValidateSyntheticRepo` at line 285 calls `SyntheticContentHash()` directly,
bypassing `syntheticContentHashOnce`. This is a secondary bug: even on full-validation paths,
the hash should be computed at most once per process.

**Also change line 285:**
```go
// Before (line 285 in registry.go):
wantHash, err := SyntheticContentHash()
// After:
wantHash, err := syntheticContentHashOnce()
```

This recovers a small amount of latency on install/doctor paths and is consistent with the
design intent of `syntheticContentHashOnce`.

---

## 4. Call Site Map

| File | Line | Function | Purpose | Change |
|---|---|---|---|---|
| `internal/config/pack_include.go` | 301 | `resolveBundledSourceWithoutLock` | Fast read-lock pre-check before acquiring write lock | **→ ValidateSyntheticRepoFast** |
| `internal/config/pack_include.go` | 420 | `validateInstalledRemoteCache` | Canonical-pin check on locked packs.lock hit | **→ ValidateSyntheticRepoFast** |
| `internal/config/pack_include.go` | 305 | `resolveBundledSourceWithoutLock` | Post-materialization check inside write lock (rare) | Keep full |
| `internal/packman/cache.go` | 69 | `cache hydration` | `gc import install` cache write | Keep full |
| `internal/packman/check.go` | 232 | `gc doctor` | Explicit health check | Keep full |
| `internal/packman/install.go` | 50 | `install path` | Install-path validation | Keep full |

The two changed call sites are on the resolution hot path — executed on every `gc` invocation
(~3x per subprocess, once per builtin pack: core, bd, dolt). The four unchanged call sites
are cold paths: install/doctor operations that are rare and intentionally pay the full cost.

---

## 5. Error Message Design

The fast path shares the same error message vocabulary as the full path. For overlapping
checks, the messages must be byte-identical to avoid confusing callers who pattern-match on
error strings:

| Check | Error string |
|---|---|
| dir missing | `"missing bundled pack cache marker"` (same as full) |
| stat error | `"checking bundled pack cache root: %w"` (same) |
| symlink root | `"bundled pack cache root %q is a symlink"` (same) |
| not a directory | `"bundled pack cache root %q is not a directory"` (same) |
| marker missing | `"missing bundled pack cache marker"` (same) |
| marker read error | `"reading bundled pack cache marker: %w"` (same) |
| marker parse error | `"parsing bundled pack cache marker: %w"` (same) |
| schema mismatch | `"unsupported bundled pack cache marker schema %d"` (same) |
| repository mismatch | `"bundled pack cache repository %q does not match %q"` (same) |
| commit mismatch | `"bundled pack cache commit %q does not match %q"` (same) |
| content hash mismatch | `"bundled pack cache content hash %q does not match current binary %q"` (same) |

**No new error message formats.** All errors from the fast path are already understood by
callers and error-handling logic. Callers treat all `ValidateSyntheticRepo*` errors as
"cache invalid, re-materialize", so the specific error text matters only for diagnostics.

---

## 6. Accessibility / Developer Experience Audit

Applying WCAG 2.1 AA principles to the developer API:

### 6.1 Perceivability — errors are clear and actionable
- All errors include the problematic value in the message (`%q`) ✓
- Error messages explain what was wrong, not just that something was wrong ✓
- `syntheticContentHashOnce()` failure surfaces as "cannot hash" with context, not a panic ✓

### 6.2 Operability — API is hard to misuse
- Identical signature: callers cannot accidentally swap arguments ✓
- Function names are adjacent in the same file: developer reads both godoc before choosing ✓
- `ValidateSyntheticRepoFast` is purely read-only: no side effects on incorrect use ✓
- If fast path fails, the caller falls through to `MaterializeSyntheticRepo` — the safe path ✓

### 6.3 Understandability — the doc comment must convey the trade-off
The godoc for `ValidateSyntheticRepoFast` must say explicitly:
- What it does NOT check (no file set walk, no per-file comparison)
- When it is safe (resolution hot path, cache is gc-managed and content-hash-keyed)
- When to use the full version instead (install, doctor, post-materialization)

Proposed godoc:
```
// ValidateSyntheticRepoFast is the lightweight resolution-path variant of
// ValidateSyntheticRepo. It verifies only the marker file commit and content
// hash — no file-by-file comparison. The marker hash was computed from the
// full embedded content at materialization (MaterializeSyntheticRepo) and is
// sufficient to prove the cache was written by the current binary.
//
// Use this on the resolution hot path where ValidateSyntheticRepo is called
// on every gc subprocess invocation. Use ValidateSyntheticRepo for
// gc import install and gc doctor where full file integrity matters.
```

### 6.4 Robustness — consistent behaviour under error conditions
- Fast path errors cause the same fallback as full path errors (re-materialization) ✓
- No new error codes or types — callers see the same `error` interface ✓
- `syntheticContentHashOnce` propagates hash errors identically to `SyntheticContentHash` ✓

---

## 7. Test Coverage Requirements

All `TestValidateSyntheticRepoDetects*` tests must be duplicated for the fast variant.
The test helper `materializeTestRepo` is reusable (package-internal).

### Required new tests:

| Test name | What it checks |
|---|---|
| `TestValidateSyntheticRepoFastAcceptsValidRepo` | Happy path: materialized repo passes |
| `TestValidateSyntheticRepoFastAcceptsEquivalentCommit` | `SameCommit` handles abbreviated/uppercase |
| `TestValidateSyntheticRepoFastRejectsSymlinkRoot` | Symlink at cache root → error |
| `TestValidateSyntheticRepoFastRejectsNonDirectoryRoot` | File, not dir, at cache root → error |
| `TestValidateSyntheticRepoFastRejectsMissingMarker` | No marker file → error |
| `TestValidateSyntheticRepoFastRejectsWrongCommit` | Commit mismatch → error |
| `TestValidateSyntheticRepoFastRejectsWrongContentHash` | Tampered marker ContentHash → error |

### Tests NOT required (fast path intentionally skips these):

| Test NOT needed | Reason |
|---|---|
| `TestValidateSyntheticRepoFastRejectsTamperedContent` | Fast path does not check file contents by design |
| `TestValidateSyntheticRepoFastRejectsTamperedMode` | Fast path does not check file modes |
| `TestValidateSyntheticRepoFastRejectsUnexpectedFiles` | Fast path does not walk the file set |
| `TestValidateSyntheticRepoFastRejectsUnexpectedRootSibling` | Same as above |
| `TestValidateSyntheticRepoFastRejectsSymlinkAncestor` | WalkDir check is full-path only |

The builder should add a comment in the test file:
```go
// Note: TestValidateSyntheticRepoFast does NOT test content/mode/file-set checks —
// those are intentionally skipped by the fast variant. See ValidateSyntheticRepo
// for the full integrity suite.
```

### For `TestValidateSyntheticRepoFastRejectsWrongContentHash`:

Construct a repo, then manually overwrite `syntheticMarkerFile` with a marker containing
a fake `ContentHash`. The fast variant should return an error because the tampered hash
doesn't match `syntheticContentHashOnce()`. This is the single most critical test to add:
it proves the fast path catches the "wrong binary version" case.

```go
func TestValidateSyntheticRepoFastRejectsWrongContentHash(t *testing.T) {
    dst := materializeTestRepo(t)
    // Overwrite marker with wrong content hash.
    markerPath := filepath.Join(dst, ".gc-bundled-pack-cache.toml")
    writeFile(t, markerPath, `schema = 1\nrepository = "https://github.com/gastownhall/gascity.git"\ncommit = "`+testCommit+`"\ncontent_hash = "sha256:000000000000000000000000000000000000000000000000000000000000000"\n`)

    err := ValidateSyntheticRepoFast(dst, testCommit)
    if err == nil {
        t.Fatal("ValidateSyntheticRepoFast accepted wrong content hash")
    }
    if !strings.Contains(err.Error(), "content hash") {
        t.Fatalf("error = %v, want content-hash detail", err)
    }
}
```

---

## 8. Performance Recovery Confirmation

The acceptance gate is:
```
TestRetryManagedPooledWorkerRecoversClaimed... <= 90s locally
```

Pre-#3344 baseline: 86.68s. The fix should restore close to this.

**Expected breakdown after fix:**
- Per subprocess: 3 × ~0.5ms (3 ReadFile(marker) + 1 hash computation memoized) = ~1.5ms
- 290 subprocesses: ~0.44s overhead (vs. ~40.6s before fix)
- Net recovery: ~40s removed from CI workflow

---

## 9. Files Changed

```
internal/builtinpacks/registry.go
  - Add: ValidateSyntheticRepoFast (new function)
  - Fix: ValidateSyntheticRepo line 285: SyntheticContentHash() → syntheticContentHashOnce()

internal/builtinpacks/registry_test.go
  - Add: 7 new TestValidateSyntheticRepoFast* tests

internal/config/pack_include.go
  - Line 301: ValidateSyntheticRepo → ValidateSyntheticRepoFast
  - Line 420: ValidateSyntheticRepo → ValidateSyntheticRepoFast
```

Total diff scope: ~80 lines (+60 new function, +7 tests, -2/+2 call sites, -1/+1 line 285).
