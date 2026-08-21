package main

// Stale isolated supervisor-service sweep (gascity#3896).
//
// Every `gc supervisor install` under an isolated GC_HOME writes a
// per-home service file (com.gascity.supervisor.<suffix>.plist on
// launchd, gascity-supervisor-<suffix>.service on systemd) with
// RunAtLoad / Restart=always semantics. Test and e2e harnesses that
// crash or are interrupted before their `gc supervisor uninstall`
// teardown leak that service permanently: the service manager
// resurrects it on every login even after its temp GC_HOME is deleted,
// and nothing else ever removes it. The sweep below runs on the
// install and uninstall paths and boots out gc-owned *suffixed*
// service files whose GC_HOME no longer exists. It never touches the
// default (unsuffixed) service, the current process's own service
// file, or any file whose GC_HOME cannot be parsed or still exists.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// supervisorSystemdUnitPrefix is the shared prefix of suffixed
// (isolated-GC_HOME) supervisor units; keep in sync with
// supervisorSystemdServiceName.
const supervisorSystemdUnitPrefix = "gascity-supervisor-"

var supervisorBuildServiceDataForRepair = buildSupervisorServiceData

// sweepStaleIsolatedSupervisorServices removes leaked isolated-home
// supervisor services for the active platform. Failures are warnings
// on stderr; the sweep never blocks the caller.
func sweepStaleIsolatedSupervisorServices(stderr io.Writer) {
	switch supervisorRuntimeGOOS {
	case "darwin":
		sweepStaleIsolatedSupervisorLaunchd(stderr)
	case "linux":
		sweepStaleIsolatedSupervisorSystemd(stderr)
	}
}

// supervisorServiceGCHomeMissing reports whether a service file's
// GC_HOME is definitively gone. Only a clean not-exist counts:
// empty values, permission errors, and transient failures leave the
// service alone.
func supervisorServiceGCHomeMissing(gcHome string) bool {
	if strings.TrimSpace(gcHome) == "" {
		return false
	}
	_, err := os.Stat(gcHome)
	return errors.Is(err, os.ErrNotExist)
}

// sweepStaleIsolatedSupervisorLaunchd boots out and removes suffixed
// com.gascity.supervisor.* launch agents whose GC_HOME no longer
// exists. If it mutates any leaked launchd service, it finishes by
// re-writing and re-starting the canonical unsuffixed user LaunchAgent.
// That final repair is intentionally one-way: it never unloads or
// disables com.gascity.supervisor, because this sweep can run from the
// only live operator able to recover the machine supervisor.
func sweepStaleIsolatedSupervisorLaunchd(stderr io.Writer) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return
	}
	dir := filepath.Join(home, "Library", "LaunchAgents")
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Missing or unreadable LaunchAgents dir: nothing to sweep.
		return
	}
	ownPath := supervisorLaunchdPlistPath()
	mutated := false
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".plist") {
			continue
		}
		label := strings.TrimSuffix(name, ".plist")
		// Only suffixed labels: the default com.gascity.supervisor
		// agent is the user's real supervisor and is never swept.
		if !strings.HasPrefix(label, defaultSupervisorLaunchdLabel+".") {
			continue
		}
		path := filepath.Join(dir, name)
		if samePath(path, ownPath) {
			continue
		}
		// legacySupervisorHome parses GC_HOME out of any gc-rendered
		// service file, not only the legacy-labeled one.
		gcHome, ok := legacySupervisorHome(path)
		if !ok || !supervisorServiceGCHomeMissing(gcHome) {
			continue
		}
		mutated = true
		_ = supervisorLaunchctlRun("unload", path)
		_ = supervisorLaunchctlRun("disable", supervisorLaunchdServiceTarget(label))
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(stderr, "gc supervisor: removing stale isolated supervisor plist %s: %v\n", path, err) //nolint:errcheck // best-effort stderr
			continue
		}
		fmt.Fprintf(stderr, "gc supervisor: removed stale isolated supervisor service %s (GC_HOME %s no longer exists)\n", label, gcHome) //nolint:errcheck // best-effort stderr
	}
	if mutated {
		repairCanonicalSupervisorLaunchdAfterStaleSweep(home, stderr)
	}
}

func repairCanonicalSupervisorLaunchdAfterStaleSweep(home string, stderr io.Writer) {
	data, err := supervisorBuildServiceDataForRepair()
	if err != nil {
		fmt.Fprintf(stderr, "gc supervisor: warning: re-bootstrap canonical launchd supervisor after stale sweep: %v\n", err) //nolint:errcheck // best-effort stderr
		return
	}
	canonicalHome := filepath.Join(home, ".gc")
	data.GCHome = canonicalHome
	data.LogPath = filepath.Join(canonicalHome, "supervisor.log")
	data.LaunchdLabel = defaultSupervisorLaunchdLabel
	data.LaunchdSystem = false
	data.SafeName = sanitizeServiceName(filepath.Base(canonicalHome))

	content, err := renderSupervisorTemplate(supervisorLaunchdTemplate, data)
	if err != nil {
		fmt.Fprintf(stderr, "gc supervisor: warning: rendering canonical launchd supervisor plist after stale sweep: %v\n", err) //nolint:errcheck // best-effort stderr
		return
	}
	if !supervisorLaunchdPlistHasRunAtLoadAndKeepAlive(content) {
		fmt.Fprintf(stderr, "gc supervisor: warning: canonical launchd supervisor plist missing RunAtLoad/KeepAlive after render; not re-bootstrapping\n") //nolint:errcheck // best-effort stderr
		return
	}
	path := filepath.Join(home, "Library", "LaunchAgents", defaultSupervisorLaunchdLabel+".plist")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(stderr, "gc supervisor: warning: creating canonical launchd supervisor dir after stale sweep: %v\n", err) //nolint:errcheck // best-effort stderr
		return
	}
	if err := ensureSupervisorServiceLogDir(data.LogPath); err != nil {
		fmt.Fprintf(stderr, "gc supervisor: warning: creating canonical launchd supervisor log dir after stale sweep: %v\n", err) //nolint:errcheck // best-effort stderr
		return
	}
	if err := writeSupervisorServiceFile(path, []byte(content)); err != nil {
		fmt.Fprintf(stderr, "gc supervisor: warning: writing canonical launchd supervisor plist after stale sweep: %v\n", err) //nolint:errcheck // best-effort stderr
		return
	}
	if err := ensureSupervisorLaunchdLoadedAndStartedNoStop(path, defaultSupervisorLaunchdLabel, false); err != nil {
		fmt.Fprintf(stderr, "gc supervisor: warning: re-bootstrap canonical launchd supervisor after stale sweep: %v\n", err) //nolint:errcheck // best-effort stderr
		return
	}
	fmt.Fprintf(stderr, "gc supervisor: re-bootstrapped canonical launchd supervisor %s after stale isolated supervisor sweep\n", defaultSupervisorLaunchdLabel) //nolint:errcheck // best-effort stderr
}

func supervisorLaunchdPlistHasRunAtLoadAndKeepAlive(content string) bool {
	return strings.Contains(content, "<key>RunAtLoad</key>\n    <true/>") &&
		strings.Contains(content, "<key>KeepAlive</key>\n    <dict>")
}

func ensureSupervisorLaunchdLoadedAndStartedNoStop(path, label string, system bool) error {
	target := supervisorLaunchdServiceTargetForDomain(label, system)
	domain := supervisorLaunchdServiceDomainForDomain(system)
	loadErr := supervisorLaunchctlRun("load", path)
	enableErr := supervisorLaunchctlRun("enable", target)
	kickstartErr := supervisorLaunchctlRun("kickstart", "-p", target)
	if enableErr == nil && kickstartErr == nil {
		return nil
	}
	bootstrapErr := supervisorLaunchctlRun("bootstrap", domain, path)
	if bootstrapErr == nil {
		enableErr = supervisorLaunchctlRun("enable", target)
		kickstartErr = supervisorLaunchctlRun("kickstart", "-p", target)
		if enableErr == nil && kickstartErr == nil {
			return nil
		}
	}
	var errs []error
	if loadErr != nil {
		errs = append(errs, fmt.Errorf("load %s: %w", path, loadErr))
	}
	if enableErr != nil {
		errs = append(errs, fmt.Errorf("enable %s: %w", target, enableErr))
	}
	if kickstartErr != nil {
		errs = append(errs, fmt.Errorf("kickstart -p %s: %w", target, kickstartErr))
	}
	if bootstrapErr != nil {
		errs = append(errs, fmt.Errorf("bootstrap %s %s: %w", domain, path, bootstrapErr))
	}
	return fmt.Errorf("ensure launchd service %s loaded and started: %w", target, errors.Join(errs...))
}

// sweepStaleIsolatedSupervisorSystemd stops, disables, and removes
// suffixed gascity-supervisor-*.service user units whose GC_HOME no
// longer exists.
func sweepStaleIsolatedSupervisorSystemd(stderr io.Writer) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return
	}
	dir := filepath.Join(home, ".local", "share", "systemd", "user")
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Missing or unreadable unit dir: nothing to sweep.
		return
	}
	ownPath := supervisorSystemdServicePath()
	removed := false
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Only suffixed units: the default gascity-supervisor.service
		// (no trailing dash) is the user's real supervisor and is
		// never swept.
		if !strings.HasPrefix(name, supervisorSystemdUnitPrefix) || !strings.HasSuffix(name, ".service") {
			continue
		}
		path := filepath.Join(dir, name)
		if samePath(path, ownPath) {
			continue
		}
		gcHome, ok := legacySupervisorHome(path)
		if !ok || !supervisorServiceGCHomeMissing(gcHome) {
			continue
		}
		_ = supervisorSystemctlRun("--user", "stop", name)
		_ = supervisorSystemctlRun("--user", "disable", name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(stderr, "gc supervisor: removing stale isolated supervisor unit %s: %v\n", path, err) //nolint:errcheck // best-effort stderr
			continue
		}
		removed = true
		fmt.Fprintf(stderr, "gc supervisor: removed stale isolated supervisor service %s (GC_HOME %s no longer exists)\n", name, gcHome) //nolint:errcheck // best-effort stderr
	}
	if removed {
		_ = supervisorSystemctlRun("--user", "daemon-reload")
	}
}
