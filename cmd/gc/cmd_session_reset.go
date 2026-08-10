package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/events"
	"github.com/spf13/cobra"
)

const (
	sessionResetLoopGuardWindow            = 2 * time.Minute
	sessionResetLoopGuardDistinctThreshold = 2
)

// newSessionResetCmd creates the "gc session reset <id-or-alias>" command.
func newSessionResetCmd(stdout, stderr io.Writer) *cobra.Command {
	var jsonOutput bool
	var force bool
	cmd := &cobra.Command{
		Use:   "reset <session-id-or-alias>",
		Short: "Restart a session fresh while preserving the bead",
		Long: `Request a fresh restart for an existing session without closing its bead.

The controller stops the current runtime and starts the same session again with
fresh provider conversation state. Session identity, alias, mail, and queued
work remain attached to the existing session bead. For named sessions, reset
also clears any tripped named-session respawn circuit breaker before requesting
the fresh restart.

Accepts a session ID (e.g., gc-42) or session alias (e.g., mayor).`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if cmdSessionResetWithForce(args, stdout, stderr, force, jsonOutput) != 0 {
				return errExit
			}
			return nil
		},
		ValidArgsFunction: completeSessionIDs,
	}
	cmd.Flags().BoolVar(&force, "force", false, "bypass the recent multi-session reset-loop guard")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSONL")
	return cmd
}

// cmdSessionReset is the CLI entry point for "gc session reset".
//
// This command intentionally requires a managed controller. The controller owns
// the fresh restart lifecycle, including key rotation and immediate restart of
// already-desired sessions.
func cmdSessionReset(args []string, stdout, stderr io.Writer) int {
	return cmdSessionResetWithForce(args, stdout, stderr, false)
}

func cmdSessionResetWithForce(args []string, stdout, stderr io.Writer, force bool, jsonOutput ...bool) int {
	asJSON := sessionJSONRequested(jsonOutput)
	store, code := openCityStore(stderr, "gc session reset")
	if store == nil {
		return code
	}

	cityPath, err := resolveCity()
	if err != nil {
		fmt.Fprintf(stderr, "gc session reset: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}
	if !cityUsesManagedReconciler(cityPath) {
		fmt.Fprintln(stderr, "gc session reset: a managed controller must be running") //nolint:errcheck // best-effort stderr
		return 1
	}
	if err := pokeController(cityPath); err != nil {
		fmt.Fprintf(stderr, "gc session reset: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}

	cfg, _ := loadCityConfig(cityPath, stderr)

	// Every store consumer here is session-class (ID resolution, worker handle,
	// session-bead load), so route the whole flow through the session
	// coordination-class store for relocation-safety.
	sessStore := cliSessionStore(store, cfg, cityPath)
	sessionID, err := resolveSessionIDWithConfig(cityPath, cfg, sessStore, args[0])
	if err != nil {
		fmt.Fprintf(stderr, "gc session reset: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}

	handle, err := workerHandleForSessionWithConfig(cityPath, sessStore, newSessionProvider(), cfg, sessionID)
	if err != nil {
		fmt.Fprintf(stderr, "gc session reset: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}

	bead, err := sessStore.Get(sessionID)
	if err != nil {
		fmt.Fprintf(stderr, "gc session reset: loading session %s: %v\n", sessionID, err) //nolint:errcheck // best-effort stderr
		return 1
	}
	actor := eventActor()
	if !force {
		if blocked, reason := sessionResetLoopGuardBlocks(cityPath, actor, sessionID, time.Now().UTC(), stderr); blocked {
			fmt.Fprintf(stderr, "gc session reset: refusing to queue another reset: %s; use --force to confirm an intentional multi-session reset\n", reason) //nolint:errcheck // best-effort stderr
			return 1
		}
	}
	identity := namedSessionIdentity(bead)
	if identity != "" {
		if err := resetSessionCircuitBreakerOnController(cityPath, sessionID, identity); err != nil {
			fmt.Fprintf(stderr, "gc session reset: clearing session circuit breaker for %q: %v\n", identity, err) //nolint:errcheck // best-effort stderr
			return 1
		}
	}

	if err := handle.Reset(context.Background()); err != nil {
		fmt.Fprintf(stderr, "gc session reset: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}

	_ = pokeController(cityPath)
	recordSessionResetRequested(cityPath, actor, sessionID, stderr)

	if asJSON {
		if err := writeSessionActionJSON(stdout, sessionActionResult{
			Action:    "reset",
			SessionID: sessionID,
			Identity:  identity,
		}); err != nil {
			fmt.Fprintf(stderr, "gc session reset: %v\n", err) //nolint:errcheck // best-effort stderr
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "Session %s reset requested. Controller will restart it fresh.\n", sessionID) //nolint:errcheck // best-effort stdout
	return 0
}

func sessionResetLoopGuardBlocks(cityPath, actor, sessionID string, now time.Time, stderr io.Writer) (bool, string) {
	if strings.TrimSpace(cityPath) == "" || strings.TrimSpace(sessionID) == "" {
		return false, ""
	}
	since := now.Add(-sessionResetLoopGuardWindow)
	recent, err := events.ReadFiltered(filepath.Join(cityPath, ".gc", "events.jsonl"), events.Filter{
		Type:  events.SessionResetRequested,
		Actor: strings.TrimSpace(actor),
		Since: since,
	})
	if err != nil {
		fmt.Fprintf(stderr, "warning: session reset-loop guard could not read recent reset events: %v\n", err) //nolint:errcheck // best-effort stderr
		return false, ""
	}
	distinctOther := map[string]bool{}
	for _, event := range recent {
		subject := strings.TrimSpace(event.Subject)
		if subject == "" || subject == sessionID {
			continue
		}
		distinctOther[subject] = true
	}
	if len(distinctOther) < sessionResetLoopGuardDistinctThreshold {
		return false, ""
	}
	return true, fmt.Sprintf("%d distinct other sessions were reset by %s in the last %s",
		len(distinctOther), displayResetActor(actor), sessionResetLoopGuardWindow)
}

func displayResetActor(actor string) string {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return "this actor"
	}
	return actor
}

func recordSessionResetRequested(cityPath, actor, sessionID string, stderr io.Writer) {
	rec := openCityRecorderAt(cityPath, stderr)
	rec.Record(events.Event{
		Type:      events.SessionResetRequested,
		Actor:     strings.TrimSpace(actor),
		Subject:   strings.TrimSpace(sessionID),
		Message:   "session reset requested",
		SessionID: strings.TrimSpace(sessionID),
	})
}

func resetSessionCircuitBreakerAfterExplicitKill(cityPath string, store beads.Store, sessionID, identity string) error {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return nil
	}
	if strings.TrimSpace(cityPath) != "" && cityUsesManagedReconciler(cityPath) {
		if err := resetSessionCircuitBreakerOnController(cityPath, sessionID, identity); err != nil {
			return err
		}
		_ = pokeController(cityPath)
		return nil
	}
	return resetSessionCircuitBreakerState(store, sessionID, identity, defaultSessionCircuitBreaker())
}
