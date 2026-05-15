package doctor

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/beads/contract"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/fsys"
	"github.com/gastownhall/gascity/internal/pgauth"
)

func TestPostgresAuthCheckBranches(t *testing.T) {
	type setupFunc func(t *testing.T, cityPath, scopeRoot string) *PostgresAuthCheck

	tests := []struct {
		name        string
		setup       setupFunc
		wantStatus  CheckStatus
		wantMessage string
		wantFixHint string
	}{
		{
			name: "scope file success",
			setup: func(t *testing.T, cityPath, scopeRoot string) *PostgresAuthCheck {
				writePostgresAuthScopeEnv(t, scopeRoot, "steady-secret", 0o600)
				return newPostgresAuthTestCheck(cityPath, scopeRoot)
			},
			wantStatus:  StatusOK,
			wantMessage: "rigs/frontend (db.example.test:5432): password from scope file",
		},
		{
			name: "parent shell env warning",
			setup: func(t *testing.T, cityPath, scopeRoot string) *PostgresAuthCheck {
				t.Setenv("BEADS_POSTGRES_PASSWORD", "ambient-secret")
				return newPostgresAuthTestCheck(cityPath, scopeRoot)
			},
			wantStatus:  StatusWarning,
			wantMessage: "rigs/frontend (db.example.test:5432): password from parent shell env",
			wantFixHint: "parent-shell env works for the current shell only. Persist via rigs/frontend/.beads/.env (chmod 600) for non-interactive use.",
		},
		{
			name: "no credentials",
			setup: func(_ *testing.T, cityPath, scopeRoot string) *PostgresAuthCheck {
				return newPostgresAuthTestCheck(cityPath, scopeRoot)
			},
			wantStatus:  StatusError,
			wantMessage: "rigs/frontend (db.example.test:5432): no password resolvable",
			wantFixHint: "set BEADS_POSTGRES_PASSWORD in rigs/frontend/.beads/.env (chmod 600)\nor add a [db.example.test:5432] section to ~/.config/beads/credentials.",
		},
		{
			name: "permissive scope file",
			setup: func(t *testing.T, cityPath, scopeRoot string) *PostgresAuthCheck {
				writePostgresAuthScopeEnv(t, scopeRoot, "loose-secret", 0o644)
				return newPostgresAuthTestCheck(cityPath, scopeRoot)
			},
			wantStatus:  StatusError,
			wantMessage: "rigs/frontend (db.example.test:5432): credentials file mode 0644 (group/other readable)",
			wantFixHint: "chmod 600 " + filepath.Join("<scope>", ".beads", ".env"),
		},
		{
			name: "malformed credentials file",
			setup: func(t *testing.T, cityPath, scopeRoot string) *PostgresAuthCheck {
				credentialsPath := filepath.Join(t.TempDir(), "credentials")
				if err := os.WriteFile(credentialsPath, []byte("[db.example.test:5432\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("BEADS_CREDENTIALS_FILE", credentialsPath)
				return newPostgresAuthTestCheck(cityPath, scopeRoot)
			},
			wantStatus:  StatusError,
			wantMessage: "rigs/frontend (db.example.test:5432): parse <credentials> at line 1: unterminated section header (expected ']')",
			wantFixHint: "edit <credentials> line 1 — see slice-2 reason vocabulary",
		},
		{
			name: "unrecognized pgauth error",
			setup: func(_ *testing.T, cityPath, scopeRoot string) *PostgresAuthCheck {
				check := newPostgresAuthTestCheck(cityPath, scopeRoot)
				check.resolve = func(map[string]string, string, pgauth.Endpoint) (pgauth.Resolved, error) {
					return pgauth.Resolved{}, errors.New("unexpected resolver failure")
				}
				return check
			},
			wantStatus:  StatusError,
			wantMessage: "rigs/frontend (db.example.test:5432): pgauth returned unrecognized error: unexpected resolver failure",
			wantFixHint: "please file a bug — postgres-auth check did not recognize this error shape",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearPostgresAuthAmbient(t)
			cityPath, scopeRoot := newPostgresAuthCity(t)
			check := tc.setup(t, cityPath, scopeRoot)
			result := check.Run(&CheckContext{CityPath: cityPath})

			if result.Status != tc.wantStatus {
				t.Fatalf("status = %v, want %v; result = %+v", result.Status, tc.wantStatus, result)
			}
			wantMessage := tc.wantMessage
			if strings.Contains(wantMessage, "<credentials>") {
				wantMessage = strings.ReplaceAll(wantMessage, "<credentials>", os.Getenv("BEADS_CREDENTIALS_FILE"))
			}
			if result.Message != wantMessage {
				t.Fatalf("message = %q, want %q", result.Message, wantMessage)
			}
			wantFixHint := tc.wantFixHint
			if strings.Contains(wantFixHint, "<scope>") {
				wantFixHint = strings.ReplaceAll(wantFixHint, "<scope>", scopeRoot)
			}
			if strings.Contains(wantFixHint, "<credentials>") {
				wantFixHint = strings.ReplaceAll(wantFixHint, "<credentials>", os.Getenv("BEADS_CREDENTIALS_FILE"))
			}
			if result.FixHint != wantFixHint {
				t.Fatalf("fix hint = %q, want %q", result.FixHint, wantFixHint)
			}
		})
	}
}

func TestPostgresAuthCheckAggregatesMultipleScopes(t *testing.T) {
	clearPostgresAuthAmbient(t)
	cityPath, rigPath := newPostgresAuthCity(t)
	writePostgresAuthScopeEnv(t, cityPath, "city-secret", 0o600)
	cfg := &config.City{
		Workspace: config.Workspace{Name: "demo"},
		Rigs: []config.Rig{{
			Name: "frontend",
			Path: rigPath,
		}},
	}
	writePostgresAuthMetadata(t, cityPath)

	check := NewPostgresAuthCheck(cityPath, cfg)
	result := check.Run(&CheckContext{CityPath: cityPath})

	if result.Status != StatusError {
		t.Fatalf("status = %v, want error; result = %+v", result.Status, result)
	}
	wantMessage := "2 postgres-backed scope(s); first issue: rigs/frontend (db.example.test:5432): no password resolvable"
	if result.Message != wantMessage {
		t.Fatalf("message = %q, want %q", result.Message, wantMessage)
	}
	if len(result.Details) < 2 {
		t.Fatalf("details = %v, want at least two rows", result.Details)
	}
	if !strings.HasPrefix(result.Details[0], "✗ rigs/frontend (db.example.test:5432) — no password resolvable") {
		t.Fatalf("first detail = %q, want rig error first", result.Details[0])
	}
	if !strings.HasPrefix(result.Details[1], "✓ city (db.example.test:5432) — password from scope file") {
		t.Fatalf("second detail = %q, want city success second", result.Details[1])
	}
}

func TestPostgresAuthExplainTable(t *testing.T) {
	clearPostgresAuthAmbient(t)
	cityPath, scopeRoot := newPostgresAuthCity(t)
	writePostgresAuthScopeEnv(t, scopeRoot, "steady-secret", 0o600)
	check := newPostgresAuthTestCheck(cityPath, scopeRoot)
	result := check.Run(&CheckContext{CityPath: cityPath, ExplainPostgresAuth: true})
	if result.Status != StatusOK {
		t.Fatalf("status = %v, want ok; result = %+v", result.Status, result)
	}

	var out bytes.Buffer
	check.RenderExtras(&CheckContext{CityPath: cityPath, ExplainPostgresAuth: true}, &out)
	got := out.String()
	for _, want := range []string{
		"PG-backed scope: rigs/frontend (db.example.test:5432)  (host=db.example.test:5432 user=bd database=beads_pg)",
		"  Tier 1  projected env  GC_POSTGRES_PASSWORD",
		"  Tier 4  scope file     rigs/frontend/.beads/.env BEADS_POSTGRES_PASSWORD",
		"[YES]  ← winner",
		"  Tier 5  os.Getenv      BEADS_POSTGRES_PASSWORD",
		"[skip]",
		"  Source identifier: scope_file   Source position: tier 4 of 7",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("explain output missing %q:\n%s", want, got)
		}
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "scope file") {
			continue
		}
		if strings.Contains(line, "GC_POSTGRES_PASSWORD") || strings.Contains(line, "BEADS_POSTGRES_PASSWORD") {
			for _, token := range []string{"[no]", "[YES]", "[skip]"} {
				if idx := strings.Index(line, token); idx >= 0 && idx != 69 {
					t.Fatalf("status token %s in %q starts at byte %d, want 69", token, line, idx)
				}
			}
		}
	}
}

func TestPostgresAuthExplainEmptyScopes(t *testing.T) {
	clearPostgresAuthAmbient(t)
	cityPath := t.TempDir()
	check := NewPostgresAuthCheck(cityPath, &config.City{Workspace: config.Workspace{Name: "demo"}})

	var out bytes.Buffer
	check.RenderExtras(&CheckContext{CityPath: cityPath, ExplainPostgresAuth: true}, &out)
	if got, want := out.String(), "  no postgres-backed scopes (this flag has no effect)\n"; got != want {
		t.Fatalf("empty explain output = %q, want %q", got, want)
	}
}

func TestHasPostgresBackedScopeGatesOnMetadata(t *testing.T) {
	cityPath := t.TempDir()
	rigPath := filepath.Join(cityPath, "rigs", "frontend")
	cfg := &config.City{
		Workspace: config.Workspace{Name: "demo"},
		Rigs: []config.Rig{{
			Name: "frontend",
			Path: rigPath,
		}},
	}
	if HasPostgresBackedScope(cityPath, cfg) {
		t.Fatal("HasPostgresBackedScope() = true, want false without postgres metadata")
	}
	if err := os.MkdirAll(filepath.Join(cityPath, ".beads"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := contract.EnsureCanonicalMetadata(fsys.OSFS{}, filepath.Join(cityPath, ".beads", "metadata.json"), contract.MetadataState{
		Database:     "dolt",
		Backend:      "dolt",
		DoltMode:     "server",
		DoltDatabase: "demo",
	}); err != nil {
		t.Fatal(err)
	}
	if HasPostgresBackedScope(cityPath, cfg) {
		t.Fatal("HasPostgresBackedScope() = true, want false for dolt metadata")
	}
	writePostgresAuthMetadata(t, rigPath)
	if !HasPostgresBackedScope(cityPath, cfg) {
		t.Fatal("HasPostgresBackedScope() = false, want true for postgres rig metadata")
	}
}

func newPostgresAuthCity(t *testing.T) (string, string) {
	t.Helper()
	cityPath := t.TempDir()
	rigPath := filepath.Join(cityPath, "rigs", "frontend")
	writePostgresAuthMetadata(t, rigPath)
	return cityPath, rigPath
}

func newPostgresAuthTestCheck(cityPath, scopeRoot string) *PostgresAuthCheck {
	return NewPostgresAuthCheck(cityPath, &config.City{
		Workspace: config.Workspace{Name: "demo"},
		Rigs: []config.Rig{{
			Name: "frontend",
			Path: scopeRoot,
		}},
	})
}

func writePostgresAuthMetadata(t *testing.T, scopeRoot string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(scopeRoot, ".beads"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := contract.EnsureCanonicalMetadata(fsys.OSFS{}, filepath.Join(scopeRoot, ".beads", "metadata.json"), contract.MetadataState{
		Database:         "postgres",
		Backend:          "postgres",
		PostgresHost:     "db.example.test",
		PostgresPort:     "5432",
		PostgresUser:     "bd",
		PostgresDatabase: "beads_pg",
	}); err != nil {
		t.Fatal(err)
	}
}

func writePostgresAuthScopeEnv(t *testing.T, scopeRoot, password string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(scopeRoot, ".beads"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(scopeRoot, ".beads", ".env")
	if err := os.WriteFile(path, []byte(fmt.Sprintf("BEADS_POSTGRES_PASSWORD=%s\n", password)), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func clearPostgresAuthAmbient(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"GC_POSTGRES_PASSWORD",
		"BEADS_POSTGRES_PASSWORD",
		"BEADS_CREDENTIALS_FILE",
	} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())
}
