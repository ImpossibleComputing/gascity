package main

import (
	"fmt"
	"io"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
	"github.com/spf13/cobra"
)

// newCloseCmd builds `gc close <id>`: close a bead through the per-class Router so a
// graph-class bead (a molecule step, wisp, control bead) is closed in the embedded
// graph store while a work bead is closed in the work store — routed by id. It is
// the in-process, graph-store-aware close a worker uses to finish a step it found
// via `gc ready`, instead of a raw `bd close` that only reaches the Dolt work store.
// The controller's reconcile reads the close back from the shared graph store
// (graph reads bypass the work cache) and converges the molecule.
func newCloseCmd(stdout, stderr io.Writer) *cobra.Command {
	var outcome string
	cmd := &cobra.Command{
		Use:   "close <id>",
		Short: "Close a bead through the Router (graph beads close in the graph store)",
		Long: `Close a bead by id through the per-class Router.

When a city sets [beads] graph_store, a graph-class bead is closed in the embedded
graph store and a work bead in the work store — routed by id. Use --outcome to
stamp gc.outcome before closing (e.g. --outcome pass), as a worker does when
finishing a step so the molecule's evaluation can converge.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if doClose(args[0], outcome, stdout, stderr) != 0 {
				return errExit
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&outcome, "outcome", "", "stamp gc.outcome on the bead before closing (e.g. pass)")
	return cmd
}

// doClose opens the city store and closes the bead through the Router.
func doClose(id, outcome string, stdout, stderr io.Writer) int {
	store, code := openCityStore(stderr, "gc close")
	if store == nil {
		return code
	}
	defer closeBeadStoreHandle(store) //nolint:errcheck // best-effort close
	if err := closeBeadThroughStore(store, id, outcome); err != nil {
		fmt.Fprintf(stderr, "gc close: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}
	fmt.Fprintf(stdout, "closed %s\n", id) //nolint:errcheck // best-effort stdout
	return 0
}

// closeBeadThroughStore stamps gc.outcome (when given) and closes the bead. Both
// ops route by id through the Router to the bead's owning backend.
func closeBeadThroughStore(store beads.Store, id, outcome string) error {
	if outcome != "" {
		if err := store.SetMetadata(id, beadmeta.OutcomeMetadataKey, outcome); err != nil {
			return fmt.Errorf("stamping %s on %q: %w", beadmeta.OutcomeMetadataKey, id, err)
		}
	}
	if err := store.Close(id); err != nil {
		return fmt.Errorf("closing %q: %w", id, err)
	}
	return nil
}
