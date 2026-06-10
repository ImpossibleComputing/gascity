package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/builtinpacks"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/fsys"
	"github.com/gastownhall/gascity/internal/packman"
)

// TestRigAddIncludeCanonicalizesBuiltinPackSource reproduces gascity#3137:
// `gc rig add <path> --include packs/gastown` writes the literal flag value
// (./packs/gastown) into city.toml instead of the canonical, resolvable builtin
// import source. Builtin packs are not registered in [packs]; the pack resolver
// joins local import sources to the city root, so ./packs/gastown resolves to
// <city>/packs/gastown, which does not exist — breaking pack expansion citywide.
//
// The --include flag's own --help promises it "writes canonical rig imports".
// This asserts that promise: a --include token naming a materialized builtin
// pack must be written as a durable source+sha import, not a repo-local system
// pack path or the literal token.
func TestRigAddIncludeCanonicalizesBuiltinPackSource(t *testing.T) {
	cityPath := t.TempDir()
	writeSchema2RigCity(t, cityPath, "test-city", "[workspace]\n", "")

	rigPath := filepath.Join(t.TempDir(), "myproj")
	if err := os.MkdirAll(rigPath, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GC_DOLT", "skip")
	t.Setenv("GC_BEADS", "bd")
	t.Setenv("GC_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	// Exactly the form documented in `gc rig add --help`.
	code := doRigAdd(fsys.OSFS{}, cityPath, rigPath, []string{"packs/gastown"}, "", "", "", false, false, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doRigAdd returned %d, stderr: %s", code, stderr.String())
	}

	data, err := os.ReadFile(filepath.Join(cityPath, "city.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cityToml := string(data)

	// The literal flag value must NOT be persisted verbatim — it does not
	// resolve (the pack lives under .gc/system/packs, not ./packs).
	if strings.Contains(cityToml, "./packs/gastown") {
		t.Errorf("city.toml persisted the literal --include value %q; pack expansion will fail citywide:\n%s",
			"./packs/gastown", cityToml)
	}
	if strings.Contains(cityToml, ".gc/system/packs") {
		t.Fatalf("city.toml persisted a repo-local builtin pack path:\n%s", cityToml)
	}
	wantSource := builtinpacks.MustSource("gastown")
	if !strings.Contains(cityToml, `source = "`+wantSource+`"`) {
		t.Fatalf("city.toml import source did not canonicalize to %q (gascity#3137):\n%s", wantSource, cityToml)
	}
	commit, err := builtinPackSyntheticCommit()
	if err != nil {
		t.Fatal(err)
	}
	wantVersion := "sha:" + commit
	if !strings.Contains(cityToml, `version = "`+wantVersion+`"`) {
		t.Fatalf("city.toml import version did not pin builtin source to %q:\n%s", wantVersion, cityToml)
	}
	lock, err := readImportLockfile(fsys.OSFS{}, cityPath)
	if err != nil {
		t.Fatalf("reading packs.lock after rig add: %v", err)
	}
	if got, ok := lock.Packs[wantSource]; !ok {
		t.Fatalf("packs.lock missing builtin rig import %q: %#v", wantSource, lock.Packs)
	} else if got.Commit != commit {
		t.Fatalf("packs.lock[%q].Commit = %q, want %q", wantSource, got.Commit, commit)
	}
	cacheDir, err := packman.RepoCachePath(wantSource, commit)
	if err != nil {
		t.Fatalf("RepoCachePath: %v", err)
	}
	if err := builtinpacks.ValidateSyntheticRepo(cacheDir, commit); err != nil {
		t.Fatalf("builtin rig import cache was not materialized at %s: %v", cacheDir, err)
	}
	if _, _, err := config.LoadWithIncludes(fsys.OSFS{}, filepath.Join(cityPath, "city.toml")); err != nil {
		t.Fatalf("LoadWithIncludes after rig add: %v", err)
	}
}

// TestRigAddIncludePrefersConfiguredPackOverBuiltin guards the collision case:
// a bare `--include gastown` where "gastown" is BOTH a registered [packs] key
// AND an embedded builtin. Builtin canonicalization must not shadow the explicit
// [packs] reference — the written import source must be the configured [packs]
// source, not the builtin source. This makes the flag's "preserves [packs]
// references" guarantee true in all cases (gascity#3137).
func TestRigAddIncludePrefersConfiguredPackOverBuiltin(t *testing.T) {
	cityPath := t.TempDir()
	const configuredSource = "https://github.com/example/gastown"
	cityToml := "[workspace]\n\n[packs.gastown]\nsource = \"" + configuredSource + "\"\n"
	writeSchema2RigCity(t, cityPath, "test-city", cityToml, "")

	rigPath := filepath.Join(t.TempDir(), "myproj")
	if err := os.MkdirAll(rigPath, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GC_DOLT", "skip")
	t.Setenv("GC_BEADS", "bd")
	t.Setenv("HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := doRigAdd(fsys.OSFS{}, cityPath, rigPath, []string{"gastown"}, "", "", "", false, false, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doRigAdd returned %d, stderr: %s", code, stderr.String())
	}

	data, err := os.ReadFile(filepath.Join(cityPath, "city.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cityToml = string(data)

	// The configured [packs] source must win — the import must reference it.
	if !strings.Contains(cityToml, configuredSource) {
		t.Errorf("city.toml dropped the configured [packs.gastown] source %q; builtin canonicalization shadowed the explicit reference:\n%s",
			configuredSource, cityToml)
	}
	if strings.Contains(cityToml, ".gc/system/packs") {
		t.Errorf("city.toml persisted a repo-local builtin pack path instead of honoring the configured [packs.gastown] reference:\n%s", cityToml)
	}
	builtinSource := builtinpacks.MustSource("gastown")
	if strings.Contains(cityToml, builtinSource) {
		t.Errorf("city.toml persisted the builtin source %q instead of honoring the configured [packs.gastown] reference:\n%s",
			builtinSource, cityToml)
	}
}
