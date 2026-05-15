package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/beads/contract"
	"github.com/gastownhall/gascity/internal/events"
	"github.com/gastownhall/gascity/internal/fsys"
	"github.com/gastownhall/gascity/internal/pgauth"
)

func TestApplyResolvedScopePostgresEnvEmitsCredentialResolvedEvent(t *testing.T) {
	clearAmbientPostgresEnv(t)
	cityPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cityPath, ".gc"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cityPath, ".gc", "events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cityPath, "city.toml"), []byte(`[workspace]
name = "demo"

[[rigs]]
name = "frontend"
path = "rigs/frontend"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	scopeRoot := filepath.Join(cityPath, "rigs", "frontend")
	if err := os.MkdirAll(filepath.Join(scopeRoot, ".beads"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scopeRoot, ".beads", ".env"), []byte("BEADS_POSTGRES_PASSWORD=event-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	meta := contract.MetadataState{
		Database:         "postgres",
		Backend:          "postgres",
		PostgresHost:     "db.example.test",
		PostgresPort:     "5432",
		PostgresUser:     "bd",
		PostgresDatabase: "beads_pg",
	}
	if _, err := contract.EnsureCanonicalMetadata(fsys.OSFS{}, filepath.Join(scopeRoot, ".beads", "metadata.json"), meta); err != nil {
		t.Fatal(err)
	}

	env := map[string]string{"BEADS_ACTOR": "worker-123"}
	if err := applyResolvedScopePostgresEnv(env, cityPath, scopeRoot, meta); err != nil {
		t.Fatalf("applyResolvedScopePostgresEnv: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(cityPath, ".gc", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "event-secret") {
		t.Fatalf("event log leaked postgres password: %s", data)
	}
	var got events.Event
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("Unmarshal event: %v", err)
		}
		if got.Type == events.PostgresCredentialResolved {
			break
		}
	}
	if got.Type != events.PostgresCredentialResolved {
		t.Fatalf("did not find %q event in log: %s", events.PostgresCredentialResolved, data)
	}
	if got.Actor != "worker-123" {
		t.Fatalf("actor = %q, want worker-123", got.Actor)
	}
	if got.Subject != "rigs/frontend" {
		t.Fatalf("subject = %q, want rigs/frontend", got.Subject)
	}
	var payload pgauth.PostgresCredentialResolvedPayload
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatalf("Unmarshal payload: %v", err)
	}
	if payload.Source != pgauth.SourceScopeFile.String() {
		t.Fatalf("source = %q, want %q", payload.Source, pgauth.SourceScopeFile.String())
	}
	if payload.ScopeKind != "rig" || payload.ScopeName != "frontend" {
		t.Fatalf("scope = %s/%s, want rig/frontend", payload.ScopeKind, payload.ScopeName)
	}
}
