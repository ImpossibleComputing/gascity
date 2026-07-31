package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/packman"
)

// TestSupervisorHealthIncludesPacksLockSHA256 asserts /health surfaces
// the SHA-256 digest of the first managed city's packs.lock contents.
// Drift checkers compare this against the committed lockfile copy
// instead of shelling into the city directory (ga-qcnpu1).
func TestSupervisorHealthIncludesPacksLockSHA256(t *testing.T) {
	s := newFakeState(t)
	content := []byte("schema = 1\n\n[packs.\"https://example.com/tools.git\"]\nversion = \"1.4.2\"\ncommit = \"aaaa\"\nfetched = \"2026-01-02T03:04:05Z\"\n")
	if err := os.WriteFile(filepath.Join(s.CityPath(), packman.LockfileName), content, 0o644); err != nil {
		t.Fatalf("write packs.lock: %v", err)
	}
	sum := sha256.Sum256(content)
	want := hex.EncodeToString(sum[:])

	sm := newTestSupervisorMux(t, map[string]*fakeState{"test-city": s})

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	sm.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, _ := resp["packs_lock_sha256"].(string); got != want {
		t.Fatalf("packs_lock_sha256 = %q, want %q\nbody: %s", got, want, rec.Body.String())
	}
}

// TestSupervisorHealthOmitsPacksLockSHA256WhenMissing confirms the
// field is omitted (not emitted as an empty string) when the first
// managed city has no packs.lock on disk, matching the omitempty
// semantics used by build_id.
func TestSupervisorHealthOmitsPacksLockSHA256WhenMissing(t *testing.T) {
	s := newFakeState(t)
	sm := newTestSupervisorMux(t, map[string]*fakeState{"test-city": s})

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	sm.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, present := resp["packs_lock_sha256"]; present {
		t.Fatalf("packs_lock_sha256 present despite missing packs.lock; got: %v", resp["packs_lock_sha256"])
	}
}

// TestSupervisorHealthPacksLockSHA256SortsCities proves the legacy
// supervisor-scope /health packs_lock_sha256 projection is deterministic in
// multi-city supervisors. The real city registry snapshot is rebuilt from maps,
// so resolver order can change across samples; /health must not flip between
// different cities' lockfiles just because the snapshot was rebuilt.
func TestSupervisorHealthPacksLockSHA256SortsCities(t *testing.T) {
	alphaDir := t.TempDir()
	betaDir := t.TempDir()
	alphaLock := []byte("schema = 1\n\n[packs.\"https://example.com/alpha.git\"]\ncommit = \"alpha\"\n")
	betaLock := []byte("schema = 1\n\n[packs.\"https://example.com/beta.git\"]\ncommit = \"beta\"\n")
	if err := os.WriteFile(filepath.Join(alphaDir, packman.LockfileName), alphaLock, 0o644); err != nil {
		t.Fatalf("write alpha packs.lock: %v", err)
	}
	if err := os.WriteFile(filepath.Join(betaDir, packman.LockfileName), betaLock, 0o644); err != nil {
		t.Fatalf("write beta packs.lock: %v", err)
	}
	alphaSum := sha256.Sum256(alphaLock)
	want := hex.EncodeToString(alphaSum[:])

	resolver := &fakeCityResolver{listed: []CityInfo{
		{Name: "zeta", Path: betaDir, Running: true},
		{Name: "alpha", Path: alphaDir, Running: true},
	}}
	sm := NewSupervisorMux(resolver, nil, false, "test", "", time.Now())

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	sm.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, _ := resp["packs_lock_sha256"].(string); got != want {
		t.Fatalf("packs_lock_sha256 = %q, want alpha %q\nbody: %s", got, want, rec.Body.String())
	}
}
