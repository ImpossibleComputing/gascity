package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/citylayout"
	"github.com/gastownhall/gascity/internal/forge"
)

// runOutcome is the terminal disposition of a one-shot workflow run.
type runOutcome struct {
	// Terminal is true when the workflow root closed; false when the watch gave
	// up on the deadline (a non-destructive timeout, not a completion).
	Terminal bool
	// Outcome is the gc.outcome stamped on the closed root ("pass" | "fail" | …).
	Outcome string
}

// Passed reports whether the run completed successfully. Only a terminal
// gc.outcome of "pass" counts; a non-terminal (deadline) result is never a pass.
func (o runOutcome) Passed() bool {
	return o.Terminal && o.Outcome == beadmeta.OutcomePass
}

// watchWorkflowRoot polls the workflow root bead until it CLOSES, returning its
// gc.outcome. The control-dispatcher closes the root inside
// processWorkflowFinalize — before it closes the finalize bead itself — so a
// closed root is the authoritative terminal signal (internal/dispatch/runtime.go).
// We watch the root's close rather than a ready-queue-empty heuristic, which the
// fork's wisp-flood history disproved.
//
// If deadline elapses before the root closes, it returns a NON-terminal result:
// the caller keeps the city directory and never destroys work on a timer. The
// deadline is a fail-safe against a wedged run hanging forever, not a reaper.
func watchWorkflowRoot(ctx context.Context, store beads.Store, rootID string, poll, deadline time.Duration) (runOutcome, error) {
	if poll <= 0 {
		poll = time.Second
	}
	var deadlineC <-chan time.Time
	if deadline > 0 {
		timer := time.NewTimer(deadline)
		defer timer.Stop()
		deadlineC = timer.C
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	for {
		root, err := store.Get(rootID)
		switch {
		case err == nil && root.Status == "closed":
			return runOutcome{Terminal: true, Outcome: root.Metadata[beadmeta.OutcomeMetadataKey]}, nil
		case err != nil && !errors.Is(err, beads.ErrNotFound):
			return runOutcome{}, fmt.Errorf("watching workflow root %s: %w", rootID, err)
		}
		select {
		case <-ctx.Done():
			return runOutcome{}, ctx.Err()
		case <-deadlineC:
			return runOutcome{Terminal: false}, nil
		case <-ticker.C:
		}
	}
}

// ---- one-shot execution --------------------------------------------------
//
// executeOneShot runs a formula to completion in a transient city with a real
// provider in the loop, then tears the city down. It orchestrates the verified
// recipe (see engdocs/plans/formula-as-unit/demo/): a Dolt-backed city minted
// via `gc init --no-start` — which NEVER registers with the shared machine
// supervisor — hosted by the STANDALONE controller (`gc start --controller`),
// with a real subprocess worker + the providerless control-dispatcher. The
// formula is slung, driven to its workflow-finalize close, and the city reaped.
//
// Lifecycle commands run as isolated `gc` child processes (proven, robust);
// completion is detected in-process via watchWorkflowRoot over the Dolt store.

// gcExecutable returns the path to the running gc binary so child gc processes
// (init/start/sling/stop) are the same build.
func gcExecutable() string {
	if exe, err := os.Executable(); err == nil && strings.TrimSpace(exe) != "" {
		return exe
	}
	return "gc"
}

// runGC runs a gc subcommand to completion and returns its combined output.
func runGC(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, gcExecutable(), args...).CombinedOutput()
	return string(out), err
}

// oneShotCityTOML is the city config for a one-shot run: a subprocess-provider
// city running formula-compiler v2, with a single self-closing worker agent
// (its start_command is workerStartCmd) kept always-on. The control-dispatcher
// comes from the auto-added core import (gc init), matching the verified demo.
func oneShotCityTOML(name, workerStartCmd string) string {
	return fmt.Sprintf(`[workspace]
name = %q

[session]
provider = "subprocess"

[daemon]
formula_v2 = true
patrol_interval = "100ms"

[[agent]]
name = "worker"
max_active_sessions = 1
start_command = %q

[[named_session]]
template = "worker"
mode = "always"
`, name, workerStartCmd)
}

// executeOneShot manufactures the transient city, runs formula to completion,
// and returns its outcome. folders become rigs; workerStartCmd is the agent
// process that does the work (an LLM wrapper in production, a deterministic
// script for tests). The city is reaped unless keep is set.
func executeOneShot(ctx context.Context, name, formulaFileName string, formulaBody []byte, folders []forge.Folder, workerStartCmd string, keep bool, stdout io.Writer) (runOutcome, error) {
	if strings.TrimSpace(workerStartCmd) == "" {
		return runOutcome{}, fmt.Errorf("gc run: execution requires --agent-cmd (the worker command that performs and closes the work)")
	}
	foundry, err := os.MkdirTemp("", "gc-run-"+name+"-*")
	if err != nil {
		return runOutcome{}, fmt.Errorf("gc run: foundry dir: %w", err)
	}
	cityRoot := filepath.Join(foundry, "city")

	configPath := filepath.Join(foundry, "city-config.toml")
	if err := os.WriteFile(configPath, []byte(oneShotCityTOML(name, workerStartCmd)), 0o644); err != nil {
		return runOutcome{}, fmt.Errorf("gc run: writing config: %w", err)
	}

	// 1. Manufacture a Dolt-backed city WITHOUT registering with the supervisor.
	if out, err := runGC(ctx, "init", "--no-start", "--skip-provider-readiness", "--file", configPath, cityRoot); err != nil {
		return runOutcome{}, fmt.Errorf("gc run: init transient city: %w\n%s", err, out)
	}

	var ctrl *exec.Cmd
	teardown := func() {
		_, _ = runGC(context.Background(), "stop", "--city", cityRoot)
		if ctrl != nil && ctrl.Process != nil {
			_ = ctrl.Process.Kill()
			_, _ = ctrl.Process.Wait()
		}
		if !keep {
			_ = os.RemoveAll(foundry)
		} else {
			fmt.Fprintf(stdout, "gc run: kept city at %s\n", cityRoot) //nolint:errcheck // best-effort stdout
		}
	}
	defer teardown()

	// 2. Bind folders as rigs (each --folder → a rig; workdir routing).
	for _, f := range folders {
		if out, err := runGC(ctx, "rig", "add", f.Path, "--name", f.Name, "--city", cityRoot); err != nil {
			return runOutcome{}, fmt.Errorf("gc run: rig add %q: %w\n%s", f.Name, err, out)
		}
	}

	// 3. Materialize the formula into the city's formula layer.
	if len(formulaBody) > 0 {
		fdir := filepath.Join(cityRoot, citylayout.FormulasRoot)
		if err := os.MkdirAll(fdir, 0o755); err != nil {
			return runOutcome{}, fmt.Errorf("gc run: formulas dir: %w", err)
		}
		if err := os.WriteFile(filepath.Join(fdir, formulaFileName), formulaBody, 0o644); err != nil {
			return runOutcome{}, fmt.Errorf("gc run: materialize formula: %w", err)
		}
	}

	// 4. Start the STANDALONE controller (own lock+socket, no supervisor).
	ctrl = exec.Command(gcExecutable(), "start", "--controller", "--city", cityRoot)
	if err := ctrl.Start(); err != nil {
		return runOutcome{}, fmt.Errorf("gc run: start controller: %w", err)
	}
	if err := waitControllerReady(ctx, cityRoot, 90*time.Second); err != nil {
		return runOutcome{}, err
	}

	// 5. Sling the formula on a one-member convoy and capture the workflow root.
	root, err := slingOneShot(ctx, cityRoot, formulaName(formulaFileName))
	if err != nil {
		return runOutcome{}, err
	}
	fmt.Fprintf(stdout, "gc run: workflow root %s\n", root)     //nolint:errcheck // best-effort stdout
	_, _ = runGC(ctx, "--city", cityRoot, "convoy", "dispatch") // nudge the control lane

	// 6. Drive to completion: watch the workflow root close (finalize), in-process.
	store, err := openCityStoreAt(cityRoot)
	if err != nil {
		return runOutcome{}, fmt.Errorf("gc run: open store: %w", err)
	}
	return watchWorkflowRoot(ctx, store, root, time.Second, 10*time.Minute)
}

// waitControllerReady blocks until the standalone controller's socket exists and
// the bead store answers, or the timeout elapses.
func waitControllerReady(ctx context.Context, cityRoot string, timeout time.Duration) error {
	sock := filepath.Join(cityRoot, citylayout.RuntimeRoot, "controller.sock")
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fi, err := os.Stat(sock); err == nil && fi.Mode()&os.ModeSocket != 0 {
			if _, err := runGC(ctx, "--city", cityRoot, "bd", "ready", "--json"); err == nil {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("gc run: controller not ready after %s", timeout)
}

// slingOneShot creates a one-member convoy and slings the formula onto it,
// returning the workflow root bead id parsed from the sling output.
func slingOneShot(ctx context.Context, cityRoot, formula string) (string, error) {
	beadOut, err := runGC(ctx, "--city", cityRoot, "bd", "create", "one-shot: "+formula, "--json")
	if err != nil {
		return "", fmt.Errorf("gc run: create work bead: %w\n%s", err, beadOut)
	}
	beadID := jsonField(beadOut, "id")
	if beadID == "" {
		return "", fmt.Errorf("gc run: could not parse work bead id from:\n%s", beadOut)
	}
	convoyOut, err := runGC(ctx, "--city", cityRoot, "convoy", "create", "one-shot "+formula, beadID, "--json")
	if err != nil {
		return "", fmt.Errorf("gc run: create convoy: %w\n%s", err, convoyOut)
	}
	convoyID := jsonField(convoyOut, "convoy_id") // convoy create --json emits convoy_id, not id
	if convoyID == "" {
		return "", fmt.Errorf("gc run: could not parse convoy id from:\n%s", convoyOut)
	}
	slingOut, err := runGC(ctx, "--city", cityRoot, "sling", "worker", convoyID, "--on="+formula)
	if err != nil {
		return "", fmt.Errorf("gc run: sling: %w\n%s", err, slingOut)
	}
	root := parseWorkflowRoot(slingOut)
	if root == "" {
		return "", fmt.Errorf("gc run: no workflow root in sling output:\n%s", slingOut)
	}
	return root, nil
}

// formulaName strips the extension from a formula filename.
func formulaName(fileName string) string {
	return strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
}

// parseWorkflowRoot extracts the root id from a sling line like
// `Attached workflow ci-xyz (formula "…")` or `Started workflow ci-xyz …`.
func parseWorkflowRoot(slingOut string) string {
	for _, line := range strings.Split(slingOut, "\n") {
		if i := strings.Index(line, "workflow "); i >= 0 {
			rest := strings.TrimSpace(line[i+len("workflow "):])
			return strings.FieldsFunc(rest, func(r rune) bool { return r == ' ' || r == '\t' })[0]
		}
	}
	return ""
}

// jsonField pulls a top-level string field out of a bd/gc --json object; bd
// emits either a single object or a one-element array.
func jsonField(jsonOut, field string) string {
	// Cheap, dependency-free extraction of "field":"value"; bd --json values
	// for id are simple strings without embedded quotes.
	needle := "\"" + field + "\":"
	i := strings.Index(jsonOut, needle)
	if i < 0 {
		return ""
	}
	rest := strings.TrimSpace(jsonOut[i+len(needle):])
	if !strings.HasPrefix(rest, "\"") {
		return ""
	}
	rest = rest[1:]
	if j := strings.IndexByte(rest, '"'); j >= 0 {
		return rest[:j]
	}
	return ""
}
