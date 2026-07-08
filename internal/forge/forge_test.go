package forge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/fsys"
)

// TestManufactureBindsFolderAndLoads is the load-bearing test: the manufactured
// city must (1) load through the real config pipeline, (2) carry pathless rigs
// in city.toml, (3) bind each rig to its real folder via .gc/site.toml, and
// (4) leave the bound repo untouched on teardown.
func TestManufactureBindsFolderAndLoads(t *testing.T) {
	foundry := t.TempDir()
	repo := t.TempDir()
	sentinel := filepath.Join(repo, "KEEP.txt")
	if err := os.WriteFile(sentinel, []byte("real repo file"), 0o644); err != nil {
		t.Fatal(err)
	}

	city, err := Manufacture(Spec{
		FormulaFileName: "hello.toml",
		FormulaBody:     []byte("# hello formula\n"),
		Folders:         []Folder{{Name: "w", Path: repo}},
		FoundryRoot:     foundry,
	})
	if err != nil {
		t.Fatalf("Manufacture: %v", err)
	}

	if _, err := os.Stat(filepath.Join(city.Root, ".gc", "events.jsonl")); err != nil {
		t.Errorf("missing scaffold events.jsonl: %v", err)
	}
	if city.FormulaPath == "" {
		t.Error("City.FormulaPath not set")
	} else if _, err := os.Stat(city.FormulaPath); err != nil {
		t.Errorf("formula not materialized at %s: %v", city.FormulaPath, err)
	}
	if filepath.Base(city.FormulaPath) != "hello.toml" {
		t.Errorf("formula filename: got %q want hello.toml", filepath.Base(city.FormulaPath))
	}

	// The returned Config is loaded back from disk with the rig bound.
	if len(city.Config.Rigs) != 1 || city.Config.Rigs[0].Path != repo {
		t.Errorf("City.Config rig not bound: %+v", city.Config.Rigs)
	}
	if city.Config.Rigs[0].Prefix != "w" {
		t.Errorf("derived prefix: got %q want w", city.Config.Rigs[0].Prefix)
	}

	// city.toml on disk is pathless; .gc/site.toml carries the bound path.
	fs := fsys.OSFS{}
	onDisk, err := config.Load(fs, filepath.Join(city.Root, "city.toml"))
	if err != nil {
		t.Fatalf("Load city.toml: %v", err)
	}
	if onDisk.Rigs[0].Path != "" {
		t.Errorf("city.toml rig should be pathless, got %q", onDisk.Rigs[0].Path)
	}

	if err := city.Teardown(); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if _, err := os.Stat(city.Root); !os.IsNotExist(err) {
		t.Errorf("city dir should be gone after teardown, stat err=%v", err)
	}
	got, err := os.ReadFile(sentinel)
	if err != nil || string(got) != "real repo file" {
		t.Errorf("bound repo modified/removed by teardown: err=%v content=%q", err, got)
	}
}

func TestManufactureKeepRetainsDir(t *testing.T) {
	city, err := Manufacture(Spec{FormulaFileName: "keepme.toml", FoundryRoot: t.TempDir(), Keep: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := city.Teardown(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(city.Root); err != nil {
		t.Errorf("keep=true should retain the city dir: %v", err)
	}
	_ = os.RemoveAll(city.Root)
}

func TestManufactureMultipleFoldersDistinctRigs(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	city, err := Manufacture(Spec{
		FormulaFileName: "multi.toml",
		Folders:         []Folder{{Name: "src", Path: a}, {Name: "dep", Path: b}},
		FoundryRoot:     t.TempDir(),
		Keep:            true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(city.Root) })

	bound := map[string]config.Rig{}
	for _, r := range city.Config.Rigs {
		bound[r.Name] = r
	}
	if bound["src"].Path != a || bound["dep"].Path != b {
		t.Errorf("folders not bound to distinct rigs: %+v (want src=%s dep=%s)", bound, a, b)
	}
	if bound["src"].Prefix != "src" || bound["dep"].Prefix != "dep" {
		t.Errorf("prefixes not derived from names: src=%q dep=%q", bound["src"].Prefix, bound["dep"].Prefix)
	}
}

func TestManufactureRejectsBadFolders(t *testing.T) {
	repo := t.TempDir()
	cases := map[string][]Folder{
		"empty name":     {{Name: "", Path: repo}},
		"empty path":     {{Name: "w", Path: ""}},
		"non-canonical":  {{Name: "My.Repo", Path: repo}},
		"duplicate name": {{Name: "w", Path: repo}, {Name: "w", Path: t.TempDir()}},
	}
	for label, folders := range cases {
		t.Run(label, func(t *testing.T) {
			city, err := Manufacture(Spec{FormulaFileName: "x.toml", Folders: folders, FoundryRoot: t.TempDir()})
			if err == nil {
				_ = city.Teardown()
				t.Fatalf("expected error for %s, got nil", label)
			}
		})
	}
}

func TestManufactureAgentsRoundTrip(t *testing.T) {
	city, err := Manufacture(Spec{
		FormulaFileName: "withagent.toml",
		Agents:          []config.Agent{{Name: "worker", PromptTemplate: "prompts/worker.md", Provider: "claude"}},
		FoundryRoot:     t.TempDir(),
		Keep:            true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(city.Root) })

	loaded, err := config.Load(fsys.OSFS{}, filepath.Join(city.Root, "city.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Agents) != 1 {
		t.Fatalf("want 1 agent, got %d", len(loaded.Agents))
	}
	a := loaded.Agents[0]
	if a.Name != "worker" || a.PromptTemplate != "prompts/worker.md" || a.Provider != "claude" {
		t.Errorf("agent did not round-trip: %+v", a)
	}
}
