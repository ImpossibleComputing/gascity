package cityinit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gastownhall/gascity/internal/fsys"
)

func TestBootstrapScopedFileProviderCityFS(t *testing.T) {
	root := t.TempDir()
	if err := BootstrapScopedFileProviderCityFS(fsys.OSFS{}, root); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	// Marker path and content must match the reader in cmd/gc/main.go.
	marker := filepath.Join(root, ".gc", "file-beads-layout")
	if got, err := os.ReadFile(marker); err != nil || string(got) != "scope-local-v1\n" {
		t.Errorf("marker: err=%v content=%q want %q", err, got, "scope-local-v1\n")
	}
	seed := filepath.Join(root, ".gc", "beads.json")
	if got, err := os.ReadFile(seed); err != nil || string(got) != "{\"seq\":0,\"beads\":[]}\n" {
		t.Errorf("seed: err=%v content=%q", err, got)
	}
}

func TestBootstrapScopedFileProviderCityFSPreservesExistingBeads(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(root, ".gc", "beads.json")
	if err := os.WriteFile(existing, []byte(`{"seq":7,"beads":["keep"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := BootstrapScopedFileProviderCityFS(fsys.OSFS{}, root); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(existing); string(got) != `{"seq":7,"beads":["keep"]}` {
		t.Errorf("existing beads.json was overwritten: %q", got)
	}
}
