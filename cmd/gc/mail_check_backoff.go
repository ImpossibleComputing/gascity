package main

import (
	"encoding/json"
	"hash/fnv"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Mail-check poll backoff.
//
// The per-turn UserPromptSubmit `mail check --inject` hook runs across the whole
// fleet. When the store is degraded (store_slow), every agent re-issues the same
// expensive inbox read every turn, and the aggregate load sustains a self-DoS:
// the store can never complete a scan (and warm) while it is being hammered.
//
// This backoff makes each agent skip the store read during an exponentially
// growing window after a store_slow result — re-emitting the cached degraded
// notice so the agent still knows mail is degraded — and probe again only once
// the window elapses. On the first healthy read it resets to the normal
// per-turn cadence. Jitter staggers fleet probes so they do not resynchronize
// into a thundering herd on recovery.
//
// Scope: the notification read path (`mail check --inject`) ONLY. Mail send and
// `gc hook` work-routing are untouched. Fully reversible: set
// GC_MAIL_CHECK_BACKOFF_DISABLE=1 to bypass, or tune the consts below.
const (
	mailCheckBackoffBase     = 15 * time.Second
	mailCheckBackoffMax      = 3 * time.Minute
	mailCheckBackoffMaxLevel = 5    // base<<5 = 8m already exceeds the cap; avoids shift overflow
	mailCheckBackoffJitter   = 0.25 // up to +25% of the window
	mailCheckBackoffEnvOff   = "GC_MAIL_CHECK_BACKOFF_DISABLE"
)

// Indirected for tests (deterministic clock + isolated state dir).
var (
	mailCheckBackoffNow   = time.Now
	mailCheckBackoffDirFn = mailCheckBackoffDefaultDir
	mailCheckBackoffJitFn = rand.Int63n //nolint:gosec // jitter, not security
)

type mailCheckBackoffState struct {
	Level int   `json:"level"`
	Until int64 `json:"until_unix_nano"`
}

func mailCheckBackoffDisabled() bool {
	return os.Getenv(mailCheckBackoffEnvOff) != ""
}

func mailCheckBackoffDefaultDir() string {
	if d := os.Getenv("GC_CITY_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "mail-check-backoff")
	}
	// No stable per-city runtime dir (unit tests, ad-hoc CLI use): the backoff
	// has nowhere durable to persist, so it stays inert and every check probes
	// the store — identical to the pre-backoff behavior.
	return ""
}

// mailCheckBackoffInactive reports whether the backoff is turned off — either by
// the kill switch or because there is no stable per-city state location.
func mailCheckBackoffInactive() bool {
	return mailCheckBackoffDisabled() || mailCheckBackoffDirFn() == ""
}

func mailCheckBackoffPath(recipient string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(recipient))
	return filepath.Join(mailCheckBackoffDirFn(), strconv.FormatUint(uint64(h.Sum32()), 16)+".json")
}

func mailCheckBackoffLoad(recipient string) mailCheckBackoffState {
	var st mailCheckBackoffState
	b, err := os.ReadFile(mailCheckBackoffPath(recipient))
	if err != nil {
		return st
	}
	_ = json.Unmarshal(b, &st) //nolint:errcheck // corrupt state degrades to "no backoff"
	return st
}

func mailCheckBackoffSave(recipient string, st mailCheckBackoffState) {
	dir := mailCheckBackoffDirFn()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	b, err := json.Marshal(st)
	if err != nil {
		return
	}
	_ = os.WriteFile(mailCheckBackoffPath(recipient), b, 0o644) //nolint:errcheck,gosec // best-effort local state
}

// mailCheckBackoffShouldSkip reports whether the caller is inside an active
// backoff window and should skip the store read. It returns false once the
// window has elapsed, so the next check probes the store and can detect
// recovery (the "probe at window-end" guarantee).
func mailCheckBackoffShouldSkip(recipient string) bool {
	if mailCheckBackoffInactive() {
		return false
	}
	st := mailCheckBackoffLoad(recipient)
	if st.Until == 0 {
		return false
	}
	return mailCheckBackoffNow().UnixNano() < st.Until
}

// mailCheckBackoffRecordSlow extends the backoff window after a store_slow read:
// exponential growth capped at mailCheckBackoffMax, plus jitter.
func mailCheckBackoffRecordSlow(recipient string) {
	if mailCheckBackoffInactive() {
		return
	}
	st := mailCheckBackoffLoad(recipient)
	level := st.Level
	if level > mailCheckBackoffMaxLevel {
		level = mailCheckBackoffMaxLevel
	}
	window := mailCheckBackoffBase << uint(level)
	if window <= 0 || window > mailCheckBackoffMax {
		window = mailCheckBackoffMax
	}
	if st.Level < mailCheckBackoffMaxLevel {
		st.Level++
	}
	if jitMax := int64(float64(window) * mailCheckBackoffJitter); jitMax > 0 {
		window += time.Duration(mailCheckBackoffJitFn(jitMax))
	}
	st.Until = mailCheckBackoffNow().Add(window).UnixNano()
	mailCheckBackoffSave(recipient, st)
}

// mailCheckBackoffReset clears the backoff after a healthy read, returning the
// agent to the normal per-turn cadence.
func mailCheckBackoffReset(recipient string) {
	if mailCheckBackoffInactive() {
		return
	}
	_ = os.Remove(mailCheckBackoffPath(recipient)) //nolint:errcheck // absent state == reset
}
