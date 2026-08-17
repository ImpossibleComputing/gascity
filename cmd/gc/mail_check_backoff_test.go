package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/api"
)

// installMailCheckBackoffHarness points the backoff at an isolated temp dir with
// a deterministic clock and zero jitter. It returns a pointer to the fake clock;
// advance it with *clk = clk.Add(d).
func installMailCheckBackoffHarness(t *testing.T) *time.Time {
	t.Helper()
	dir := t.TempDir()
	now := time.Unix(1_700_000_000, 0)
	origNow, origDir, origJit := mailCheckBackoffNow, mailCheckBackoffDirFn, mailCheckBackoffJitFn
	mailCheckBackoffNow = func() time.Time { return now }
	mailCheckBackoffDirFn = func() string { return dir }
	mailCheckBackoffJitFn = func(int64) int64 { return 0 }
	t.Cleanup(func() {
		mailCheckBackoffNow = origNow
		mailCheckBackoffDirFn = origDir
		mailCheckBackoffJitFn = origJit
	})
	// The closure above reads `now` by reference, so returning &now lets the
	// caller advance the same variable the clock observes.
	return &now
}

func TestMailCheckBackoffSkipProbeReset(t *testing.T) {
	clk := installMailCheckBackoffHarness(t)
	const rcpt = "paul"

	if mailCheckBackoffShouldSkip(rcpt) {
		t.Fatal("fresh state must not skip")
	}

	// A store_slow read opens a backoff window: skip while inside it.
	mailCheckBackoffRecordSlow(rcpt)
	if !mailCheckBackoffShouldSkip(rcpt) {
		t.Fatal("must skip inside the backoff window")
	}

	// Probe-at-window-end: once the base window elapses, do not skip — the next
	// check hits the store so recovery is detectable.
	*clk = clk.Add(mailCheckBackoffBase + time.Second)
	if mailCheckBackoffShouldSkip(rcpt) {
		t.Fatal("must probe once the window has elapsed")
	}

	// Exponential growth: a second consecutive slow read yields a longer window.
	mailCheckBackoffRecordSlow(rcpt)
	*clk = clk.Add(mailCheckBackoffBase + time.Second)
	if !mailCheckBackoffShouldSkip(rcpt) {
		t.Fatal("second-level window must exceed the base window")
	}

	// A healthy read resets to the normal per-turn cadence.
	mailCheckBackoffReset(rcpt)
	if mailCheckBackoffShouldSkip(rcpt) {
		t.Fatal("reset must clear the backoff")
	}
}

func TestMailCheckBackoffCap(t *testing.T) {
	clk := installMailCheckBackoffHarness(t)
	const rcpt = "capped"

	// Many consecutive slow reads must never push the window past the cap.
	for i := 0; i < 20; i++ {
		mailCheckBackoffRecordSlow(rcpt)
	}
	*clk = clk.Add(mailCheckBackoffMax + time.Second)
	if mailCheckBackoffShouldSkip(rcpt) {
		t.Fatalf("window must be capped at %s", mailCheckBackoffMax)
	}
}

func TestMailCheckBackoffDisabledKillSwitch(t *testing.T) {
	t.Setenv(mailCheckBackoffEnvOff, "1")
	installMailCheckBackoffHarness(t)

	mailCheckBackoffRecordSlow("paul")
	if mailCheckBackoffShouldSkip("paul") {
		t.Fatal("kill switch must disable skipping entirely")
	}
}

// TestRouteMailCheckBackoffRecoveryProbe drives the backoff through the real
// routeMailCheck --inject path and asserts the recovery-probe contract end to
// end: a store_slow read opens the window; an in-window check SKIPS the store
// (no new request); once the window elapses the check PROBES the store (a real
// request); and a healthy probe RESETS the backoff so the next check is normal.
func TestRouteMailCheckBackoffRecoveryProbe(t *testing.T) {
	clk := installMailCheckBackoffHarness(t)

	var (
		mu   sync.Mutex
		slow = true
		hits int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		degraded := slow
		mu.Unlock()
		if degraded {
			// The production self-DoS path: an HTTP 503 store_slow error (what
			// the live GET /mail returns), which routes through IsStoreSlowError.
			mailProblemHandler(http.StatusServiceUnavailable, "store_slow: mail read timed out after 8s")(t).ServeHTTP(w, r)
			return
		}
		okMailCheckHandler(t).ServeHTTP(w, r)
	}))
	defer srv.Close()
	c := api.NewCityScopedClient(srv.URL, "test-city")
	hitCount := func() int { mu.Lock(); defer mu.Unlock(); return hits }
	check := func() {
		var out, errBuf bytes.Buffer
		routeMailCheck("", nil, true, "", c, "", &out, &errBuf)
	}

	// The store hit-count is the behavioral signal: a skip performs no store
	// read (count unchanged); a probe performs one (count increments).

	// 1. A store_slow read opens the backoff window (one store hit).
	check()
	if hitCount() != 1 {
		t.Fatalf("first check must hit the store, got %d hits", hitCount())
	}

	// 2. In-window: the next check SKIPS the store read — no new hit.
	check()
	if hitCount() != 1 {
		t.Fatalf("in-window check must NOT hit the store (skip), got %d hits", hitCount())
	}

	// 3. Window elapses and the store recovers: the probe HITS the store.
	mu.Lock()
	slow = false
	mu.Unlock()
	*clk = clk.Add(mailCheckBackoffMax + time.Second)
	check()
	if hitCount() != 2 {
		t.Fatalf("window-end probe must hit the store, got %d hits", hitCount())
	}

	// 4. The healthy read reset the backoff: the next check probes again.
	check()
	if hitCount() != 3 {
		t.Fatalf("after healthy reset the next check must probe, got %d hits", hitCount())
	}
}
