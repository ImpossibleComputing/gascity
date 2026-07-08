// Package forge manufactures transient "one-shot" cities: it writes a
// city-as-directory whose rigs are bound to real repository folders via
// .gc/site.toml (referenced, never copied), so a formula can be run against it
// and the whole directory reaped afterward.
//
// It is the execution-model-agnostic host for `gc run <formula>` — the toml arm
// today; the lumen arm plugs in behind the same Manufacture/Teardown boundary at
// branch convergence. Manufacture writes only config + scaffold; it never spawns
// a process or opens a store, so the caller owns the run loop.
//
// Security: Folders bind arbitrary filesystem paths as rigs and grant the run
// read-write access to them. Callers crossing a trust boundary (shared or hosted
// manufacture) MUST gate Folders behind an authorization step before invoking
// Manufacture — see engdocs/plans/formula-as-unit/UNIFIED-GC-RUN.md (F3). This
// package assumes a local, single-user caller.
package forge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gastownhall/gascity/internal/cityinit"
	"github.com/gastownhall/gascity/internal/citylayout"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/fsys"
)

// maxCityNameLen caps the sanitized city name so the temp-dir path component
// always fits within NAME_MAX.
const maxCityNameLen = 64

// Folder binds a symbolic name to an on-disk repository path. Each folder
// becomes one synthesized rig in the manufactured city; the repository is
// referenced through .gc/site.toml, never copied.
type Folder struct {
	// Name is the rig name/alias a formula's steps reference. It must contain
	// only letters, digits, '-' or '_' (already canonical).
	Name string
	// Path is the path to the repository the rig binds to.
	Path string
	// Prefix overrides the bead-id prefix; defaults to a lowercased Name.
	Prefix string
}

// Spec describes a transient city to manufacture for a one-shot run.
type Spec struct {
	// Name is the workspace/city name; derived from FormulaFileName when empty.
	Name string
	// FormulaFileName is the filename (with extension) the formula body is
	// materialized under formulas/. Kept engine-neutral: the caller supplies the
	// full filename (e.g. "adopt-pr.toml") so the forge never assumes a suffix.
	FormulaFileName string
	// FormulaBody is written to formulas/<FormulaFileName>. Empty skips it.
	FormulaBody []byte
	// Folders each become a synthesized, path-bound rig.
	Folders []Folder
	// Agents are placed at city scope in the manufactured city.
	Agents []config.Agent
	// Keep retains the city directory when Teardown is called.
	Keep bool
	// FoundryRoot is the parent directory for the temp city; defaults to
	// os.TempDir().
	FoundryRoot string
}

// City is a manufactured transient city-as-directory.
type City struct {
	// Root is the absolute path to the manufactured directory.
	Root string
	// Name is the workspace/city name.
	Name string
	// Config is the composed config as loaded back from disk (rig paths bound),
	// so it is identical to what any later loader sees.
	Config *config.City
	// FormulaPath is the absolute path to the materialized formula, "" if none.
	FormulaPath string

	keep bool
}

// Manufacture writes a transient city-as-directory from spec and returns it. The
// repositories named by spec.Folders are referenced through .gc/site.toml, never
// copied, so Teardown can safely remove the whole directory. It validates the
// folders up front and re-loads the written city before returning, so a nil
// error guarantees the city loads with every rig bound. On any failure the
// partially written directory is removed.
func Manufacture(spec Spec) (*City, error) {
	rigs, err := resolveRigs(spec.Folders)
	if err != nil {
		return nil, err
	}
	name := deriveCityName(spec)

	foundry := spec.FoundryRoot
	if foundry == "" {
		foundry = os.TempDir()
	}
	if err := os.MkdirAll(foundry, 0o755); err != nil {
		return nil, fmt.Errorf("preparing foundry root %q: %w", foundry, err)
	}
	root, err := os.MkdirTemp(foundry, "gc-run-"+name+"-*")
	if err != nil {
		return nil, fmt.Errorf("creating transient city directory: %w", err)
	}

	fs := fsys.OSFS{}
	formulaPath, err := writeCity(fs, root, name, rigs, spec)
	if err != nil {
		_ = os.RemoveAll(root)
		return nil, err
	}

	// Re-load the written city so "Manufacture returned nil" means "the city
	// loads and every rig is bound" — the guarantee the lumen arm inherits.
	loaded, warnings, err := loadBound(fs, root)
	if err != nil {
		_ = os.RemoveAll(root)
		return nil, fmt.Errorf("validating manufactured city: %w", err)
	}
	if len(warnings) > 0 {
		_ = os.RemoveAll(root)
		return nil, fmt.Errorf("manufactured city has unresolved bindings: %s", strings.Join(warnings, "; "))
	}

	return &City{Root: root, Name: name, Config: loaded, FormulaPath: formulaPath, keep: spec.Keep}, nil
}

// resolveRigs validates and normalizes folders into rigs. Names must be
// canonical (already equal to sanitizeName), non-empty, unique, with a non-empty
// path; each yields a distinct rig with a derived, non-empty bead prefix.
func resolveRigs(folders []Folder) ([]config.Rig, error) {
	var rigs []config.Rig
	seen := make(map[string]struct{}, len(folders))
	for _, f := range folders {
		name := strings.TrimSpace(f.Name)
		path := strings.TrimSpace(f.Path)
		if name == "" {
			return nil, fmt.Errorf("folder name must not be empty")
		}
		if name != sanitizeName(name) {
			return nil, fmt.Errorf("folder name %q must contain only letters, digits, '-' or '_'", f.Name)
		}
		if path == "" {
			return nil, fmt.Errorf("folder %q must have a path", name)
		}
		if _, dup := seen[name]; dup {
			return nil, fmt.Errorf("folder name %q used more than once", name)
		}
		prefix := strings.ToLower(strings.TrimSpace(f.Prefix))
		if prefix == "" {
			prefix = strings.ToLower(name)
		}
		if prefix == "" {
			return nil, fmt.Errorf("folder %q has no derivable bead prefix", name)
		}
		seen[name] = struct{}{}
		rigs = append(rigs, config.Rig{Name: name, Path: path, Prefix: prefix})
	}
	return rigs, nil
}

func deriveCityName(spec Spec) string {
	name := sanitizeName(spec.Name)
	if name == "" {
		base := strings.TrimSuffix(spec.FormulaFileName, filepath.Ext(spec.FormulaFileName))
		name = sanitizeName(base)
	}
	if name == "" {
		name = "gc-run"
	}
	if len(name) > maxCityNameLen {
		name = name[:maxCityNameLen]
	}
	return name
}

// writeCity writes the scaffold, city config, and formula. Rig paths and the
// workspace identity live in .gc/site.toml (canonical v2); city.toml carries
// pathless rigs. It returns the materialized formula path ("" if none).
func writeCity(fs fsys.FS, root, name string, rigs []config.Rig, spec Spec) (string, error) {
	if err := cityinit.EnsureCityScaffoldFS(fs, root); err != nil {
		return "", err
	}
	if err := fs.MkdirAll(filepath.Join(root, citylayout.FormulasRoot), 0o755); err != nil {
		return "", fmt.Errorf("creating formulas directory: %w", err)
	}

	cfg := config.EmptyCity(name)
	// Workspace identity belongs in .gc/site.toml, not city.toml (matches gc
	// init; avoids the deprecated [workspace] name surface).
	cfg.Workspace.Name = ""
	cfg.Beads = config.BeadsConfig{Provider: "file"}
	// NOTE(phase2): provider "file" needs the scoped-layout bootstrap
	// (.gc/file-beads-layout marker + seeded .gc/beads.json, per gc init /
	// DESIGN.md Decision E) before any store is opened. Slice 1 only manufactures
	// and dry-runs, so the store is never opened here; the execution slice adds
	// the bootstrap where it is exercised and verifiable.
	cfg.Agents = spec.Agents
	cfg.Rigs = rigs

	cityTOML, err := cfg.MarshalForWrite()
	if err != nil {
		return "", err
	}
	if err := fs.WriteFile(filepath.Join(root, citylayout.CityConfigFile), cityTOML, 0o644); err != nil {
		return "", fmt.Errorf("writing city.toml: %w", err)
	}
	if err := config.PersistWorkspaceSiteBinding(fs, root, name, ""); err != nil {
		return "", err
	}
	if err := config.PersistRigSiteBindings(fs, root, rigs); err != nil {
		return "", err
	}

	formulaPath := ""
	if len(spec.FormulaBody) > 0 {
		fname := strings.TrimSpace(spec.FormulaFileName)
		if fname == "" {
			fname = "formula.toml"
		}
		formulaPath = filepath.Join(root, citylayout.FormulasRoot, filepath.Base(fname))
		if err := fs.WriteFile(formulaPath, spec.FormulaBody, 0o644); err != nil {
			return "", fmt.Errorf("materializing formula %q: %w", formulaPath, err)
		}
	}
	return formulaPath, nil
}

// loadBound loads the written city.toml and applies its site bindings, returning
// the composed config and any binding warnings. Phase 2 will use the pack-aware
// composed loader once agents/packs flow through; the packless one-shot output
// is fully validated by Load + ApplySiteBindings.
func loadBound(fs fsys.FS, root string) (*config.City, []string, error) {
	cfg, err := config.Load(fs, filepath.Join(root, citylayout.CityConfigFile))
	if err != nil {
		return nil, nil, err
	}
	warnings, err := config.ApplySiteBindings(fs, root, cfg)
	if err != nil {
		return nil, nil, err
	}
	return cfg, warnings, nil
}

// Teardown removes the manufactured city directory unless Keep was set. The
// bound repositories are untouched because they were referenced, not copied.
func (c *City) Teardown() error {
	if c == nil || c.keep || c.Root == "" {
		return nil
	}
	if err := os.RemoveAll(c.Root); err != nil {
		return fmt.Errorf("removing transient city %q: %w", c.Root, err)
	}
	return nil
}

// sanitizeName reduces s to a filesystem- and prefix-safe token.
func sanitizeName(s string) string {
	s = strings.TrimSpace(s)
	s = filepath.Base(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
