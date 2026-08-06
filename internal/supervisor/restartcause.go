package supervisor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gastownhall/gascity/internal/fsys"
)

// Previous-exit classifications carried by the supervisor.started event.
// They describe how the previous supervisor instance on this machine
// exited, derived from the clean-shutdown handoff token and bounded
// shutdown-intent marker below.
//
// Attribution is best-effort across binary up/downgrades and
// misattributes at most one start per mixed-version window: a
// token-unaware binary neither writes nor consumes the token, so its
// crash can be masked by a stale token from an earlier token-aware
// clean stop, and its clean stop reads as a crash on the next
// token-aware start. Both directions self-correct after one full cycle.
const (
	// PreviousExitClean means the previous instance completed its
	// orderly STOPPING path and left the handoff token behind.
	PreviousExitClean = "clean"
	// PreviousExitCrash means a previous instance ran but exited
	// without completing its STOPPING path (no handoff token).
	PreviousExitCrash = "crash"
	// PreviousExitUnknown means there is no evidence either way —
	// typically the first start on this machine, or after a reboot
	// cleared the runtime dir holding the prior instance's lock file.
	PreviousExitUnknown = "unknown"
)

// shutdownMarkerName is the filename of the clean-shutdown handoff token.
// The token is written atomically as the final step of the supervisor's
// STOPPING path and consumed (removed) by the next instance at startup.
// It is a handoff token between consecutive instances, not a liveness or
// status file: it never describes a running process, so it cannot go
// stale — its presence means exactly "the previous shutdown completed",
// and consuming it on startup re-arms the signal for the next cycle.
const shutdownMarkerName = "supervisor.shutdown-complete"

// preserveShutdownRequestedMarkerName is a narrower marker written as soon
// as a preserve-sessions shutdown is requested. It does not prove the
// STOPPING path completed; it only prevents a supervisor killed mid-preserve
// from being mislabeled as a crash on the next start.
const preserveShutdownRequestedMarkerName = "supervisor.shutdown-requested-preserve"

// ShutdownMarkerPath returns the clean-shutdown handoff token path under
// the given GC home directory. The token lives in the home directory
// (not the runtime dir) so clean-shutdown attribution survives reboots
// that wipe a tmpfs-backed runtime dir.
func ShutdownMarkerPath(home string) string {
	return filepath.Join(home, shutdownMarkerName)
}

// PreserveShutdownRequestedMarkerPath returns the bounded shutdown-intent
// marker path under the given GC home directory. The marker is best-effort
// restart-cause evidence, not a liveness or completion token.
func PreserveShutdownRequestedMarkerPath(home string) string {
	return filepath.Join(home, preserveShutdownRequestedMarkerName)
}

// WriteShutdownMarker atomically writes the clean-shutdown handoff token
// under home. Called as the final step of the supervisor STOPPING path,
// after all managed cities have been stopped.
func WriteShutdownMarker(home string) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("creating home dir for shutdown handoff token: %w", err)
	}
	if err := fsys.WriteFileAtomic(fsys.OSFS{}, ShutdownMarkerPath(home), []byte("clean\n"), 0o600); err != nil {
		return fmt.Errorf("writing shutdown handoff token: %w", err)
	}
	return nil
}

// WritePreserveShutdownRequestedMarker atomically writes a marker that a
// preserve-sessions shutdown was requested. Unlike WriteShutdownMarker, this
// is written at request time, before city preservation can be interrupted by
// an external kill. It is therefore evidence for PreviousExitUnknown, not
// PreviousExitClean, when no completion token follows.
func WritePreserveShutdownRequestedMarker(home string) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("creating home dir for preserve shutdown request marker: %w", err)
	}
	if err := fsys.WriteFileAtomic(fsys.OSFS{}, PreserveShutdownRequestedMarkerPath(home), []byte("preserve_sessions\n"), 0o600); err != nil {
		return fmt.Errorf("writing preserve shutdown request marker: %w", err)
	}
	return nil
}

// ClearPreserveShutdownRequestedMarker disarms a preserve-shutdown request
// marker. Absence is success: callers use this when a shutdown is completed
// cleanly or superseded by a destructive request.
func ClearPreserveShutdownRequestedMarker(home string) error {
	if err := os.Remove(PreserveShutdownRequestedMarkerPath(home)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing preserve shutdown request marker: %w", err)
	}
	return nil
}

// ConsumePreviousExit removes the clean-shutdown handoff token under home
// and, when present without a clean token, the preserve-shutdown request
// marker under home, then classifies how the previous supervisor instance
// exited. Callers must already hold the supervisor instance lock so exactly
// one instance consumes these markers. priorInstanceRan reports whether any
// artifact of a previous instance exists (the supervisor lock file, observed
// before this instance recreated it); with all markers absent it distinguishes
// a crashed prior instance from a first start.
//
// detail is non-nil only when the token could not be removed for a
// reason other than absence (permissions, IO error), or when a preserve
// shutdown request was observed but the completion token was not. The class
// is then PreviousExitUnknown — the classification refuses to guess — and
// the detail distinguishes an unremovable stale token or interrupted
// preserve request from a true first start. An unremoved marker stays armed,
// so a later start that does remove it may report stale evidence; surfacing
// the detail keeps that window observable.
func ConsumePreviousExit(home string, priorInstanceRan bool) (class string, detail error) {
	err := os.Remove(ShutdownMarkerPath(home))
	switch {
	case err == nil:
		if preserveErr := ClearPreserveShutdownRequestedMarker(home); preserveErr != nil {
			return PreviousExitUnknown, fmt.Errorf("removing stale preserve shutdown request marker after clean shutdown: %w", preserveErr)
		}
		return PreviousExitClean, nil
	case !os.IsNotExist(err):
		// The token may exist but cannot be removed (permissions, IO
		// error). Refuse to guess a classification.
		return PreviousExitUnknown, fmt.Errorf("removing shutdown handoff token: %w", err)
	}

	preserveErr := os.Remove(PreserveShutdownRequestedMarkerPath(home))
	switch {
	case preserveErr == nil:
		return PreviousExitUnknown, fmt.Errorf("preserve shutdown was requested but completion marker was absent")
	case !os.IsNotExist(preserveErr):
		return PreviousExitUnknown, fmt.Errorf("removing preserve shutdown request marker: %w", preserveErr)
	case priorInstanceRan:
		return PreviousExitCrash, nil
	default:
		return PreviousExitUnknown, nil
	}
}
