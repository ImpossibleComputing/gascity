package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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
// processWorkflowFinalize — before the finalize bead itself — so a closed root
// is the authoritative terminal signal (internal/dispatch/runtime.go). We watch
// the close rather than a ready-queue-empty heuristic, which the fork's
// wisp-flood history disproved.
//
// A non-terminal (deadline) result is NON-destructive: the caller keeps the city
// so a slow-but-live run is never destroyed on a timer. The root was just slung,
// so if it stays ErrNotFound past a short grace the id is wrong (or the wrong
// store) — fail fast rather than burn the whole deadline.
func watchWorkflowRoot(ctx context.Context, store beads.Store, rootID string, poll, deadline time.Duration) (runOutcome, error) {
	if poll <= 0 {
		poll = time.Second
	}
	const notFoundGrace = 30 * time.Second
	start := time.Now()
	seen := false

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
		case err == nil:
			seen = true
		case errors.Is(err, beads.ErrNotFound):
			if !seen && time.Since(start) > notFoundGrace {
				return runOutcome{}, fmt.Errorf("workflow root %s never appeared (wrong id or store?)", rootID)
			}
		default:
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
// with a real subprocess worker + the providerless control-dispatcher. Lifecycle
// commands run as isolated `gc` child processes with a SCRUBBED environment (no
// ambient GC_*/BEADS_* leaks the transient city into a host city's Dolt); the
// controller runs in its own process group so teardown reaps its descendants.
//
// This choice — subprocess lifecycle over an in-process runController — is the
// settled architecture (see UNIFIED-GC-RUN.md): child processes give crash
// isolation and avoid the ambiguous runController-return classification.

// gcExecutable returns the path to the running gc binary so child gc processes
// (init/start/sling/stop) are the same build. It errors rather than falling back
// to a PATH-resolved "gc" that could be a different install.
func gcExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil || strings.TrimSpace(exe) == "" {
		return "", fmt.Errorf("gc run: cannot determine the running gc binary path: %w", err)
	}
	return exe, nil
}

// scrubbedEnv returns the environment for child gc processes with every
// GC_*/BEADS_* variable removed, so all city/store/Dolt layout resolves solely
// from --city. Without this, gc run launched inside a host city's session would
// resolve the transient city's managed-Dolt layout into the HOST city's runtime
// and teardown could kill the host's Dolt server.
func scrubbedEnv() []string {
	src := os.Environ()
	out := make([]string, 0, len(src))
	for _, kv := range src {
		key := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			key = kv[:i]
		}
		if strings.HasPrefix(key, "GC_") || strings.HasPrefix(key, "BEADS_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// runGC runs a gc subcommand to completion, capturing stdout separately from
// stderr (so JSON parsing never ingests warning lines), with a scrubbed env.
func runGC(ctx context.Context, args ...string) (string, error) {
	exe, err := gcExecutable()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Env = scrubbedEnv()
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// oneShotCityTOML is the city config for a one-shot run: a subprocess-provider
// city running formula-compiler v2, with a single self-closing worker agent
// (start_command workerStartCmd, bound to workerDir rig when set) kept always-on.
// The control-dispatcher comes from the auto-added core import (gc init).
func oneShotCityTOML(name, workerStartCmd, workerDir string) string {
	dirLine := ""
	if workerDir != "" {
		dirLine = fmt.Sprintf("dir = %q\n", workerDir)
	}
	return fmt.Sprintf(`[workspace]
name = %q

[session]
provider = "subprocess"

[daemon]
formula_v2 = true
patrol_interval = "100ms"

[[agent]]
name = "worker"
%smax_active_sessions = 1
start_command = %q

[[named_session]]
template = "worker"
mode = "always"
`, name, dirLine, workerStartCmd)
}

// executeOneShot manufactures the transient city, runs formula to completion,
// and returns its outcome. folders become rigs; workerStartCmd is the agent
// process that does the work. On a non-terminal (timed-out) or errored run the
// city is preserved for inspection; a passed run is reaped unless keep is set.
func executeOneShot(ctx context.Context, name, formulaFileName string, formulaBody []byte, folders []forge.Folder, workerStartCmd string, vars []string, keep bool, deadline time.Duration, stdout, stderr io.Writer) (runOutcome, error) {
	if strings.TrimSpace(workerStartCmd) == "" {
		return runOutcome{}, fmt.Errorf("gc run: execution requires --agent-cmd (the worker command that performs and closes the work)")
	}
	if len(folders) > 1 {
		return runOutcome{}, fmt.Errorf("gc run: multiple --folder is not yet supported for execution (folder→step routing is unimplemented); pass at most one --folder")
	}

	name = forge.SanitizeCityName(name)
	if name == "" {
		name = "gc-run"
	}
	foundry, err := os.MkdirTemp("", "gc-run-"+name+"-*")
	if err != nil {
		return runOutcome{}, fmt.Errorf("gc run: foundry dir: %w", err)
	}
	cityRoot := filepath.Join(foundry, "city")

	// Register teardown BEFORE any process is started, so an init/config failure
	// (which may have spawned a Dolt server) never leaks. preserve is flipped for
	// non-terminal/errored runs so evidence is kept.
	var ctrl *exec.Cmd
	ctrlDone := make(chan struct{})
	var ctrlErr error
	preserve := keep
	teardown := func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = runGC(stopCtx, "stop", "--city", cityRoot)
		if ctrl != nil && ctrl.Process != nil {
			_ = syscall.Kill(-ctrl.Process.Pid, syscall.SIGKILL) // whole controller process group
			select {
			case <-ctrlDone:
			case <-time.After(3 * time.Second):
			}
		}
		// Backstop: the managed Dolt sql-server leads its own process group and
		// survives the controller kill; stop it explicitly (the demo's pkill).
		if port := readDoltPort(cityRoot); port != "" {
			_, _ = stopManagedDoltProcess(cityRoot, port)
		}
		if preserve {
			fmt.Fprintf(stdout, "gc run: kept city at %s\n", cityRoot) //nolint:errcheck // best-effort stdout
			return
		}
		if err := removeAllRetry(foundry); err != nil {
			fmt.Fprintf(stderr, "gc run: could not fully remove %s: %v (remove it manually)\n", foundry, err) //nolint:errcheck // best-effort stderr
		}
	}
	defer teardown()

	configPath := filepath.Join(foundry, "city-config.toml")
	workerDir := ""
	if len(folders) == 1 {
		workerDir = folders[0].Name
	}
	if err := os.WriteFile(configPath, []byte(oneShotCityTOML(name, workerStartCmd, workerDir)), 0o644); err != nil {
		preserve = true
		return runOutcome{}, fmt.Errorf("gc run: writing config: %w", err)
	}

	// 1. Manufacture a Dolt-backed city WITHOUT registering with the supervisor.
	if out, err := runGC(ctx, "init", "--no-start", "--skip-provider-readiness", "--file", configPath, cityRoot); err != nil {
		preserve = true
		return runOutcome{}, fmt.Errorf("gc run: init transient city: %w\n%s", err, out)
	}
	// Enforce (not just assert in a comment) the no-supervisor invariant.
	if entry, found, err := registeredCityEntry(cityRoot); err == nil && found {
		_, _ = runGC(ctx, "unregister", cityRoot)
		return runOutcome{}, fmt.Errorf("gc run: transient city %q was registered with the shared supervisor; isolation broken (unregistered it)", entry.Name)
	}

	// 2. Bind the folder as a rig (single-folder; the worker runs in it).
	for _, f := range folders {
		if out, err := runGC(ctx, "rig", "add", f.Path, "--name", f.Name, "--city", cityRoot); err != nil {
			preserve = true
			return runOutcome{}, fmt.Errorf("gc run: rig add %q: %w\n%s", f.Name, err, out)
		}
	}

	// 3. Materialize the formula into the city's formula layer.
	if len(formulaBody) > 0 {
		fdir := filepath.Join(cityRoot, citylayout.FormulasRoot)
		if err := os.MkdirAll(fdir, 0o755); err != nil {
			preserve = true
			return runOutcome{}, fmt.Errorf("gc run: formulas dir: %w", err)
		}
		if err := os.WriteFile(filepath.Join(fdir, filepath.Base(formulaFileName)), formulaBody, 0o644); err != nil {
			preserve = true
			return runOutcome{}, fmt.Errorf("gc run: materialize formula: %w", err)
		}
	}

	// 4. Start the STANDALONE controller in its own process group, logging to
	//    the foundry so a startup failure leaves diagnostics.
	exe, err := gcExecutable()
	if err != nil {
		preserve = true
		return runOutcome{}, err
	}
	logPath := filepath.Join(foundry, "controller.log")
	logFile, _ := os.Create(logPath) //nolint:errcheck // diagnostics only
	ctrl = exec.Command(exe, "start", "--controller", "--city", cityRoot)
	ctrl.Env = scrubbedEnv()
	ctrl.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if logFile != nil {
		ctrl.Stdout, ctrl.Stderr = logFile, logFile
	}
	if err := ctrl.Start(); err != nil {
		preserve = true
		return runOutcome{}, fmt.Errorf("gc run: start controller: %w", err)
	}
	go func() { ctrlErr = ctrl.Wait(); close(ctrlDone) }()

	if err := waitControllerReady(ctx, cityRoot, ctrlDone, &ctrlErr, logPath, 90*time.Second); err != nil {
		preserve = true
		return runOutcome{}, err
	}

	// 5. Sling the formula on a one-member convoy; capture the workflow root.
	root, err := slingOneShot(ctx, cityRoot, formulaName(formulaFileName), vars)
	if err != nil {
		preserve = true
		return runOutcome{}, err
	}
	fmt.Fprintf(stdout, "gc run: workflow root %s\n", root) //nolint:errcheck // best-effort stdout
	_, _ = runGC(ctx, "--city", cityRoot, "convoy", "dispatch")

	// 6. Drive to completion: watch the workflow root close, in-process.
	store, err := openCityStoreAt(cityRoot)
	if err != nil {
		preserve = true
		return runOutcome{}, fmt.Errorf("gc run: open store: %w", err)
	}
	outcome, err := watchWorkflowRoot(ctx, store, root, time.Second, deadline)
	if err != nil || !outcome.Terminal {
		preserve = true // keep evidence on timeout/error
	}
	return outcome, err
}

// waitControllerReady blocks until the standalone controller's socket exists and
// the store answers, failing fast if the controller exits during startup or the
// timeout elapses (with a tail of the controller log for diagnosis).
func waitControllerReady(ctx context.Context, cityRoot string, ctrlDone <-chan struct{}, ctrlErr *error, logPath string, timeout time.Duration) error {
	sock := controllerSocketPath(cityRoot)
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
		case <-ctrlDone:
			exitErr := *ctrlErr
			if exitErr == nil {
				exitErr = errors.New("exited cleanly")
			}
			return fmt.Errorf("gc run: controller exited during startup: %w%s", exitErr, logTail(logPath))
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("gc run: controller not ready after %s%s", timeout, logTail(logPath))
}

// slingOneShot creates a one-member convoy and slings the formula onto it,
// returning the workflow root id from the typed --json output. vars are passed
// through as repeated --var flags.
func slingOneShot(ctx context.Context, cityRoot, formula string, vars []string) (string, error) {
	beadOut, err := runGC(ctx, "--city", cityRoot, "bd", "create", "one-shot: "+formula, "--json")
	if err != nil {
		return "", fmt.Errorf("gc run: create work bead: %w\n%s", err, beadOut)
	}
	var bead struct {
		ID string `json:"id"`
	}
	if err := decodeObjectOrFirst(beadOut, &bead); err != nil {
		return "", fmt.Errorf("gc run: parse work bead id: %w\n%s", err, beadOut)
	}
	if bead.ID == "" {
		return "", fmt.Errorf("gc run: no work bead id in:\n%s", beadOut)
	}
	convoyOut, err := runGC(ctx, "--city", cityRoot, "convoy", "create", "one-shot "+formula, bead.ID, "--json")
	if err != nil {
		return "", fmt.Errorf("gc run: create convoy: %w\n%s", err, convoyOut)
	}
	var convoy struct {
		ConvoyID string `json:"convoy_id"`
	}
	if err := decodeObjectOrFirst(convoyOut, &convoy); err != nil {
		return "", fmt.Errorf("gc run: parse convoy id: %w\n%s", err, convoyOut)
	}
	if convoy.ConvoyID == "" {
		return "", fmt.Errorf("gc run: no convoy id in:\n%s", convoyOut)
	}
	args := []string{"--city", cityRoot, "sling", "worker", convoy.ConvoyID, "--on=" + formula, "--json"}
	for _, v := range vars {
		args = append(args, "--var", v)
	}
	slingOut, err := runGC(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("gc run: sling: %w\n%s", err, slingOut)
	}
	var sl struct {
		WorkflowID string `json:"workflow_id"`
	}
	if err := decodeObjectOrFirst(slingOut, &sl); err != nil {
		return "", fmt.Errorf("gc run: parse workflow id: %w\n%s", err, slingOut)
	}
	if sl.WorkflowID == "" {
		return "", fmt.Errorf("gc run: no workflow id in:\n%s", slingOut)
	}
	return sl.WorkflowID, nil
}

// decodeObjectOrFirst unmarshals a bd/gc --json result into dst, handling both
// the object and single-element-array shapes bd emits.
func decodeObjectOrFirst(s string, dst any) error {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[") {
		var arr []json.RawMessage
		if err := json.Unmarshal([]byte(s), &arr); err != nil {
			return err
		}
		if len(arr) == 0 {
			return fmt.Errorf("empty json array")
		}
		return json.Unmarshal(arr[0], dst)
	}
	return json.Unmarshal([]byte(s), dst)
}

// formulaName strips the extension from a formula filename.
func formulaName(fileName string) string {
	return strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
}

// readDoltPort reads the managed Dolt sql-server port from the city's scope.
func readDoltPort(cityRoot string) string {
	b, err := os.ReadFile(filepath.Join(cityRoot, ".beads", "dolt-server.port"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// removeAllRetry removes dir, retrying briefly to survive a Dolt server still
// releasing files right after it is stopped.
func removeAllRetry(dir string) error {
	var err error
	for i := 0; i < 4; i++ {
		if err = os.RemoveAll(dir); err == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return err
}

// logTail returns a short trailing excerpt of a log file for error messages.
func logTail(path string) string {
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return ""
	}
	const maxTail = 800
	if len(b) > maxTail {
		b = b[len(b)-maxTail:]
	}
	return "\n--- controller.log (tail) ---\n" + string(b)
}
