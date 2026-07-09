package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/gastownhall/gascity/internal/citylayout"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/forge"
	"github.com/spf13/cobra"
)

// newRunCmd builds `gc run <path>` — the one-shot front door. It manufactures a
// throwaway city-as-directory for a single formula run, binds --folder repos to
// it as rigs (referenced, never copied), and (with --dry-run) prints the
// synthesized city and reaps it.
//
// Only .toml formulas are accepted in this build; the .lumen arm plugs in behind
// the same forge at branch convergence.
func newRunCmd(stdout, stderr io.Writer) *cobra.Command {
	var (
		folderFlags []string
		varFlags    []string
		agentCmd    string
		keep        bool
		dryRun      bool
	)
	cmd := &cobra.Command{
		Use:   "run <path>",
		Short: "Run a formula as a one-shot in a manufactured transient city",
		Long: strings.TrimSpace(`
Manufacture a throwaway city-as-directory for a single formula run, bind the
given folders to it as rigs (referenced, never copied), run the formula, and
tear the city down.

Only .toml formulas are supported in this build. Use --dry-run to print the
synthesized city (city.toml + .gc/site.toml + resolved rig bindings) without
running; otherwise --agent-cmd is required and the formula is run to completion
in an isolated Dolt-backed city (standalone controller, never the shared
supervisor), then the city is reaped and the exit code reflects gc.outcome.

Security: each --folder grants the run full read-write access to that path as
the invoking user. gc run is local single-user only; do not expose it to
untrusted callers without an authorization gate.`),
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := runOneShot(cmd.Context(), stdout, stderr, args[0], folderFlags, varFlags, agentCmd, keep, dryRun, ""); err != nil {
				fmt.Fprintf(stderr, "%v\n", err) //nolint:errcheck // best-effort stderr
				return errExit
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&folderFlags, "folder", nil, "bind a repo as name=/path (repeatable); each becomes a rig with read-write access to that path")
	cmd.Flags().StringArrayVar(&varFlags, "var", nil, "formula variable as key=value (repeatable; validated but not yet applied in this build)")
	cmd.Flags().StringVar(&agentCmd, "agent-cmd", "", "worker command that performs and closes the work (required to run; e.g. an LLM wrapper or a script)")
	cmd.Flags().BoolVar(&keep, "keep", false, "retain the manufactured city directory instead of removing it")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "manufacture and print the synthesized city, then reap it without running")
	return cmd
}

// runOneShot runs (or, with dryRun, previews) a formula as a one-shot in a
// transient city. --dry-run manufactures a lightweight file city and prints it;
// a real run manufactures an isolated Dolt city, drives the formula to its
// workflow-finalize close with a real (subprocess) provider, and reaps it.
// foundryRoot is "" outside tests (dry-run defaults to os.TempDir via the forge).
func runOneShot(ctx context.Context, stdout, stderr io.Writer, path string, folderFlags, varFlags []string, agentCmd string, keep, dryRun bool, foundryRoot string) error {
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".toml" {
		return fmt.Errorf("gc run: unsupported file type %q; only .toml formulas are supported in this build", ext)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("gc run: reading formula %q: %w", path, err)
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return fmt.Errorf("gc run: formula %q is empty", path)
	}
	var probe any
	if _, err := toml.Decode(string(body), &probe); err != nil {
		return fmt.Errorf("gc run: formula %q is not valid TOML: %w", path, err)
	}
	folders, err := parseFolderFlags(folderFlags)
	if err != nil {
		return err
	}
	// --var is validated for forward compatibility; the prototype does not yet
	// thread vars into the run (see the flag help).
	if _, err := parseKeyValueFlags(varFlags); err != nil {
		return err
	}

	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))

	if dryRun {
		city, err := forge.Manufacture(forge.Spec{
			Name:            name,
			FormulaFileName: base,
			FormulaBody:     body,
			Folders:         folders,
			FoundryRoot:     foundryRoot,
			Keep:            keep,
		})
		if err != nil {
			return fmt.Errorf("gc run: manufacturing city: %w", err)
		}
		defer func() {
			if err := city.Teardown(); err != nil {
				fmt.Fprintf(stderr, "gc run: teardown: %v\n", err) //nolint:errcheck // best-effort stderr
			}
		}()
		printManufacturedCity(stdout, city)
		return nil
	}

	// Real run: manufacture an isolated Dolt city, drive to completion, reap.
	outcome, err := executeOneShot(ctx, name, base, body, folders, agentCmd, keep, stdout)
	if err != nil {
		return err
	}
	if !outcome.Terminal {
		return fmt.Errorf("gc run: %s did not reach a terminal outcome within the deadline (kept for inspection)", name)
	}
	fmt.Fprintf(stdout, "gc run: %s completed with gc.outcome=%s\n", name, outcome.Outcome) //nolint:errcheck // best-effort stdout
	if !outcome.Passed() {
		return fmt.Errorf("gc run: %s finished with gc.outcome=%s", name, outcome.Outcome)
	}
	return nil
}

// parseFolderFlags parses repeatable name=/path bindings into forge.Folders,
// canonicalizing each path (resolving symlinks, which also verifies existence).
// The bead prefix is left to the forge (the single derivation owner).
func parseFolderFlags(flags []string) ([]forge.Folder, error) {
	var folders []forge.Folder
	seen := make(map[string]struct{}, len(flags))
	for _, raw := range flags {
		name, path, ok := strings.Cut(raw, "=")
		name = strings.TrimSpace(name)
		path = strings.TrimSpace(path)
		if !ok || name == "" || path == "" {
			return nil, fmt.Errorf("gc run: --folder %q must be name=/path", raw)
		}
		if _, dup := seen[name]; dup {
			return nil, fmt.Errorf("gc run: --folder name %q used more than once", name)
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("gc run: resolving --folder %q: %w", raw, err)
		}
		// Symlinked targets are intentionally allowed and resolved to their real
		// path here; EvalSymlinks also fails if the path does not exist.
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return nil, fmt.Errorf("gc run: --folder %q: %w", path, err)
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return nil, fmt.Errorf("gc run: stat --folder %q: %w", path, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("gc run: --folder %q is not a directory", path)
		}
		seen[name] = struct{}{}
		folders = append(folders, forge.Folder{Name: name, Path: resolved})
	}
	return folders, nil
}

// parseKeyValueFlags parses repeatable key=value flags into a map.
func parseKeyValueFlags(flags []string) (map[string]string, error) {
	out := make(map[string]string, len(flags))
	for _, raw := range flags {
		key, value, ok := strings.Cut(raw, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("gc run: --var %q must be key=value", raw)
		}
		out[key] = value
	}
	return out, nil
}

// printManufacturedCity prints the synthesized city.toml, .gc/site.toml, and the
// site-bound rig paths (from the config the forge already loaded back).
func printManufacturedCity(stdout io.Writer, city *forge.City) {
	fmt.Fprintf(stdout, "# manufactured transient city: %s\n\n", city.Root) //nolint:errcheck // best-effort stdout
	printFileSection(stdout, "city.toml", filepath.Join(city.Root, citylayout.CityConfigFile))
	printFileSection(stdout, ".gc/site.toml", config.SiteBindingPath(city.Root))
	fmt.Fprintln(stdout, "--- resolved rig bindings ---") //nolint:errcheck // best-effort stdout
	if len(city.Config.Rigs) == 0 {
		fmt.Fprintln(stdout, "(none)") //nolint:errcheck // best-effort stdout
	}
	for _, r := range city.Config.Rigs {
		fmt.Fprintf(stdout, "rig %q -> %s (prefix %q)\n", r.Name, r.Path, r.Prefix) //nolint:errcheck // best-effort stdout
	}
}

// printFileSection prints a labeled file, surfacing (not swallowing) a read
// failure — the file was written moments earlier, so unreadability is a signal.
func printFileSection(stdout io.Writer, label, path string) {
	fmt.Fprintf(stdout, "--- %s ---\n", label) //nolint:errcheck // best-effort stdout
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(stdout, "(unreadable: %v)\n\n", err) //nolint:errcheck // best-effort stdout
		return
	}
	fmt.Fprintf(stdout, "%s\n", string(data)) //nolint:errcheck // best-effort stdout
}
