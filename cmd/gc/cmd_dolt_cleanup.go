package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/fsys"
	"github.com/spf13/cobra"
)

// CleanupSchemaVersion is the stable schema identifier for the JSON output of
// `gc dolt-cleanup --json`. Documented in AD-04 designer Wireframe 6.
const CleanupSchemaVersion = "gc.dolt.cleanup.v1"

// CleanupReport is the typed JSON output of `gc dolt-cleanup`.
//
// Fields are populated incrementally: the port section is filled from the
// AD-04 §4.1 discovery chain; rigs_protected, dropped, purge, reaped are
// populated by their respective steps as they come online. The shape is
// stable from day one — empty arrays and zero structs render as `[]` /
// `{...}` so callers can rely on the schema across versions.
type CleanupReport struct {
	Schema        string                 `json:"schema"`
	Port          CleanupPortReport      `json:"port"`
	RigsProtected []CleanupRigProtection `json:"rigs_protected"`
	Dropped       CleanupDroppedReport   `json:"dropped"`
	Purge         CleanupPurgeReport     `json:"purge"`
	Reaped        CleanupReapedReport    `json:"reaped"`
	Summary       CleanupSummary         `json:"summary"`
	Errors        []CleanupError         `json:"errors"`
}

// CleanupPortReport is the resolved-port section of the JSON envelope.
type CleanupPortReport struct {
	Resolved int    `json:"resolved"`
	Source   string `json:"source"`
	Fallback bool   `json:"fallback"`
}

// CleanupRigProtection records a registered rig DB whose name will not be
// dropped even if it appears in the orphan scan.
type CleanupRigProtection struct {
	Rig string `json:"rig"`
	DB  string `json:"db"`
}

// CleanupDroppedReport summarises the drop step.
type CleanupDroppedReport struct {
	Count      int                  `json:"count"`
	BytesFreed int64                `json:"bytes_freed"`
	Failed     []CleanupDropFailure `json:"failed"`
}

// CleanupDropFailure records a single drop step that did not complete.
type CleanupDropFailure struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

// CleanupPurgeReport summarises the purge step.
type CleanupPurgeReport struct {
	OK             bool  `json:"ok"`
	BytesReclaimed int64 `json:"bytes_reclaimed"`
}

// CleanupReapedReport summarises the orphan-process reap step.
type CleanupReapedReport struct {
	Count         int      `json:"count"`
	ProtectedPIDs []int    `json:"protected_pids"`
	Errors        []string `json:"errors"`
}

// CleanupSummary aggregates totals across the three steps.
type CleanupSummary struct {
	BytesFreedDisk int64 `json:"bytes_freed_disk"`
	BytesFreedRSS  int64 `json:"bytes_freed_rss"`
	ErrorsTotal    int   `json:"errors_total"`
}

// CleanupError is a single error entry tagged with the stage that produced
// it. Stage values are e.g. "drop", "purge", "reap", "port".
type CleanupError struct {
	Stage string `json:"stage"`
	Name  string `json:"name,omitempty"`
	Error string `json:"error"`
}

// MarshalJSON ensures slices serialise as `[]` rather than `null` for empty
// values. The JSON contract documents these as always-present arrays.
func (r CleanupReport) MarshalJSON() ([]byte, error) {
	type alias CleanupReport
	if r.RigsProtected == nil {
		r.RigsProtected = []CleanupRigProtection{}
	}
	if r.Dropped.Failed == nil {
		r.Dropped.Failed = []CleanupDropFailure{}
	}
	if r.Reaped.ProtectedPIDs == nil {
		r.Reaped.ProtectedPIDs = []int{}
	}
	if r.Reaped.Errors == nil {
		r.Reaped.Errors = []string{}
	}
	if r.Errors == nil {
		r.Errors = []CleanupError{}
	}
	return json.Marshal(alias(r))
}

// cleanupOptions bundles the inputs to runDoltCleanup so the command body
// stays Cobra-free and testable. The Cobra command builds an options value
// from flags and city state and hands it off.
type cleanupOptions struct {
	Flag     string
	CityPort int
	Rigs     []resolverRig
	FS       fsys.FS
	JSON     bool
	Probe    bool
	Host     string
}

// runDoltCleanup is the testable core of the `gc dolt-cleanup` command. It
// applies the AD-04 §4.1 port-resolution chain, optionally probes the
// resolved port, and writes either a CleanupReport JSON envelope or a
// human-readable summary to stdout. Returns the exit code.
//
// Drop, purge, and reap stages are not yet implemented; their JSON sections
// render as zero-valued structs (count=0, ok=false, etc.) so the schema is
// stable from day one. Subsequent commits will populate them.
func runDoltCleanup(opts cleanupOptions, stdout, stderr io.Writer) int {
	in := PortResolverInput{
		Flag:     opts.Flag,
		CityPort: opts.CityPort,
		Rigs:     opts.Rigs,
		FS:       opts.FS,
	}
	resolution := ResolveDoltPort(in)

	report := CleanupReport{
		Schema: CleanupSchemaVersion,
		Port: CleanupPortReport{
			Resolved: resolution.Port,
			Source:   resolution.Source,
			Fallback: resolution.Fallback,
		},
	}

	if opts.Probe {
		host := opts.Host
		if host == "" {
			host = "127.0.0.1"
		}
		if err := probeDoltPort(host, resolution.Port); err != nil {
			report.Errors = append(report.Errors, CleanupError{
				Stage: "port",
				Error: err.Error(),
			})
			report.Summary.ErrorsTotal++
			emitReport(report, resolution, opts, stdout, stderr)
			return 1
		}
	}

	emitReport(report, resolution, opts, stdout, stderr)
	return 0
}

func emitReport(report CleanupReport, resolution PortResolution, opts cleanupOptions, stdout, stderr io.Writer) {
	if opts.JSON {
		data, err := json.Marshal(report)
		if err != nil {
			fmt.Fprintf(stderr, "gc dolt-cleanup: marshal report: %v\n", err) //nolint:errcheck
			return
		}
		fmt.Fprintln(stdout, string(data)) //nolint:errcheck
		return
	}

	host := opts.Host
	if host == "" {
		host = "127.0.0.1"
	}
	if resolution.Fallback {
		fmt.Fprintf(stdout, "⚠ Dolt server port: %d (legacy default — fallback)\n", resolution.Port)             //nolint:errcheck
		fmt.Fprintln(stdout, "  Tried sources, in order:")                                                        //nolint:errcheck
		for _, attempt := range resolution.Tried {
			fmt.Fprintf(stdout, "    %-46s  %s\n", attempt.Source, attemptStatusLabel(attempt))                   //nolint:errcheck
		}
	} else {
		fmt.Fprintf(stdout, "Dolt server: %s:%d (resolved from %s)\n", host, resolution.Port, resolution.Source) //nolint:errcheck
	}
}

func attemptStatusLabel(a PortResolutionAttempt) string {
	switch a.Status {
	case "found":
		return "← " + a.Detail
	case "error":
		if a.Detail != "" {
			return "error: " + a.Detail
		}
		return "error"
	case "not-provided":
		return "not provided"
	case "not-set":
		return "not set"
	case "not-found":
		return "not found"
	default:
		return a.Status
	}
}

func probeDoltPort(host string, port int) error {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
	if err != nil {
		return fmt.Errorf("dolt server at %s unreachable: %w", addr, err)
	}
	_ = conn.Close()
	return nil
}

// newDoltCleanupCmd builds the `gc dolt-cleanup` Cobra command.
//
// Top-level (not under a `dolt` parent) because the existing `dolt` pack
// binding owns that namespace. The pack's `gc dolt cleanup` script can
// delegate to this Go-side command once feature parity lands.
func newDoltCleanupCmd(stdout, stderr io.Writer) *cobra.Command {
	var (
		portFlag string
		jsonOut  bool
		probe    bool
	)

	cmd := &cobra.Command{
		Use:   "dolt-cleanup",
		Short: "Find and remove orphaned Dolt databases (Go-side core)",
		Long: `gc dolt-cleanup is the Go-side implementation of the operational Dolt
cleanup tool. It resolves the Dolt server port via the AD-04 chain
(--port > city dolt.port > <rigRoot>/.beads/dolt-server.port > 3307)
and prints a structured report.

Drop, purge, and orphan-reap steps are wired in subsequent commits;
the JSON schema (gc.dolt.cleanup.v1) is stable from day one.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cityPath, err := resolveCity()
			if err != nil {
				fmt.Fprintf(stderr, "gc dolt-cleanup: %v\n", err) //nolint:errcheck
				return errExit
			}
			cfg, err := loadCityConfig(cityPath, stderr)
			if err != nil {
				fmt.Fprintf(stderr, "gc dolt-cleanup: %v\n", err) //nolint:errcheck
				return errExit
			}
			rigs := loadResolverRigs(cityPath, cfg)
			opts := cleanupOptions{
				Flag:     portFlag,
				CityPort: cfg.Dolt.Port,
				Rigs:     rigs,
				FS:       fsys.OSFS{},
				JSON:     jsonOut,
				Probe:    probe,
				Host:     cfg.Dolt.Host,
			}
			if code := runDoltCleanup(opts, stdout, stderr); code != 0 {
				return errExit
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&portFlag, "port", "", "override the resolved Dolt port")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON envelope (gc.dolt.cleanup.v1)")
	cmd.Flags().BoolVar(&probe, "probe", false, "TCP-probe the resolved port; fail if unreachable")
	return cmd
}

// loadResolverRigs builds the resolver's rig list from a city config. The HQ
// rig (the city itself) is added first so it wins the AD-04 §4.1 tie when
// multiple <rigRoot>/.beads/dolt-server.port files exist; non-HQ rigs follow
// in city.toml order. Paths are resolved to absolute form via
// resolveRigPaths so the resolver's filesystem reads work regardless of how
// the rig was registered.
func loadResolverRigs(cityPath string, cfg *config.City) []resolverRig {
	rigs := make([]config.Rig, len(cfg.Rigs))
	copy(rigs, cfg.Rigs)
	resolveRigPaths(cityPath, rigs)

	out := make([]resolverRig, 0, len(rigs)+1)
	out = append(out, resolverRig{
		Name: cfg.EffectiveCityName(),
		Path: cityPath,
		HQ:   true,
	})
	for _, r := range rigs {
		out = append(out, resolverRig{
			Name: r.Name,
			Path: r.Path,
			HQ:   false,
		})
	}
	return out
}
