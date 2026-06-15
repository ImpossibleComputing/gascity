//go:build integration

package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestBdShimInstalledForFileOnlyCityUngated proves the C4 shim install is
// UNGATED: a city with NO graph_store=sqlite still gets the gc-as-bd shim
// installed at <city>/.gc/shimbin/{gc,bd} by the supervisor, so a managed
// worker's by-id bead ops route through the controller and keep its cache
// authoritative in Dolt/file-only cities too — not just graph_store=sqlite ones.
//
// The full pure-HTTP routing THROUGH this install is proven end-to-end by
// TestGraphStoreSQLiteDeployedCityConverges; here we prove the install itself
// runs for a non-sqlite city (the shim's per-verb behavior then adapts at
// runtime via classifyBdShimVerb, routing by-id verbs in every phase).
func TestBdShimInstalledForFileOnlyCityUngated(t *testing.T) {
	env := newGraphStoreSQLiteShimEnv(t) // generic isolated shim env; no sqlite-specific setup remains

	cityName := uniqueCityName()
	cityDir := filepath.Join(t.TempDir(), cityName)
	// A plain file-backed city: provider="file", NO graph_store, so the city
	// builds policy(caching(file-work)) with no Router. The shim must still be
	// installed.
	cityToml := fmt.Sprintf(`[workspace]
name = %q

[beads]
provider = "file"

[session]
provider = "subprocess"

[daemon]
patrol_interval = "100ms"

[[agent]]
name = "worker"
max_active_sessions = 1
start_command = "bash %s"
`, cityName, agentScript("graph-store-sqlite-worker.sh"))
	configPath := filepath.Join(t.TempDir(), "fileonly.toml")
	if err := os.WriteFile(configPath, []byte(cityToml), 0o644); err != nil {
		t.Fatalf("writing city config: %v", err)
	}

	out, err := runGCWithEnv(env, "", "init", "--skip-provider-readiness", "--file", configPath, cityDir)
	if err != nil {
		t.Fatalf("gc init failed: %v\noutput: %s", err, out)
	}
	registerCityCommandEnv(cityDir, env)
	t.Cleanup(func() {
		unregisterCityCommandEnv(cityDir)
		if out, err := runGCWithEnv(env, "", "supervisor", "stop", "--wait"); err != nil {
			t.Logf("cleanup: gc supervisor stop --wait: %v\n%s", err, out)
		}
		cleanupTestCityDir(cityDir)
	})
	waitForControllerReady(t, cityDir, 30*time.Second)

	// The ungated install ran for a non-sqlite city: both shim symlinks exist.
	shimbin := filepath.Join(cityDir, ".gc", "shimbin")
	gcLink := filepath.Join(shimbin, "gc")
	bdLink := filepath.Join(shimbin, "bd")
	for _, link := range []string{gcLink, bdLink} {
		info, err := os.Lstat(link)
		if err != nil {
			t.Fatalf("shim symlink %s not installed for file-only city: %v", link, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s is not a symlink (mode %v)", link, info.Mode())
		}
	}
	// bd -> the in-dir gc symlink, so a worker's `bd` execs gc-invoked-as-bd.
	if target, err := os.Readlink(bdLink); err != nil || target != gcLink {
		t.Fatalf("bd symlink -> %q (err %v), want gc symlink %q", target, err, gcLink)
	}
}
