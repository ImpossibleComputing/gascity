package pgauth

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/events"
	"github.com/gastownhall/gascity/internal/execenv"
)

func TestPostgresEventOmitsPassword(t *testing.T) {
	password := "redaction-canary-" + randomHexForPostgresEventTest(t)
	payload := PostgresCredentialResolvedPayload{
		ScopeKind: "rig",
		ScopeName: "frontend",
		Source:    SourceScopeFile.String(),
		Host:      "db.example.test",
		Port:      "5432",
		User:      "bd",
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal payload: %v", err)
	}
	event := events.Event{
		Type:    events.PostgresCredentialResolved,
		Ts:      time.Unix(1, 0).UTC(),
		Actor:   "controller",
		Subject: "rigs/frontend",
		Payload: payloadBytes,
	}

	t.Run("EventPayloadOmitsPassword", func(t *testing.T) {
		if bytes.Contains(payloadBytes, []byte(password)) {
			t.Fatalf("payload leaked password %q: %s", password, payloadBytes)
		}
		if bytes.Contains(payloadBytes, []byte(`"password"`)) {
			t.Fatalf("payload contains forbidden password field: %s", payloadBytes)
		}
	})

	t.Run("EventEnvelopeOmitsPassword", func(t *testing.T) {
		eventBytes, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("Marshal event: %v", err)
		}
		if bytes.Contains(eventBytes, []byte(password)) {
			t.Fatalf("event envelope leaked password %q: %s", password, eventBytes)
		}
	})

	t.Run("RedactTextScrubsPassword", func(t *testing.T) {
		text := "BEADS_POSTGRES_PASSWORD=" + password + " failed with " + password
		got := execenv.RedactText(text, []string{"BEADS_POSTGRES_PASSWORD=" + password})
		if strings.Contains(got, password) {
			t.Fatalf("RedactText leaked password %q in %q", password, got)
		}
	})

	t.Run("EventCarriesExpectedSource", func(t *testing.T) {
		var decoded PostgresCredentialResolvedPayload
		if err := json.Unmarshal(payloadBytes, &decoded); err != nil {
			t.Fatalf("Unmarshal payload: %v", err)
		}
		if decoded.Source != SourceScopeFile.String() {
			t.Fatalf("source = %q, want %q", decoded.Source, SourceScopeFile.String())
		}
	})

	t.Run("EventEmitsForResolvedSource", func(t *testing.T) {
		rec := &postgresEventCaptureRecorder{}
		rec.Record(event)
		if len(rec.events) != 1 {
			t.Fatalf("recorded %d events, want 1", len(rec.events))
		}
		if rec.events[0].Type != events.PostgresCredentialResolved {
			t.Fatalf("event type = %q, want %q", rec.events[0].Type, events.PostgresCredentialResolved)
		}
		var decoded PostgresCredentialResolvedPayload
		if err := json.Unmarshal(rec.events[0].Payload, &decoded); err != nil {
			t.Fatalf("Unmarshal recorded payload: %v", err)
		}
		if decoded.Source != SourceScopeFile.String() {
			t.Fatalf("recorded source = %q, want %q", decoded.Source, SourceScopeFile.String())
		}
	})
}

type postgresEventCaptureRecorder struct {
	events []events.Event
}

func (r *postgresEventCaptureRecorder) Record(event events.Event) {
	r.events = append(r.events, event)
}

func randomHexForPostgresEventTest(t *testing.T) string {
	t.Helper()
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return hex.EncodeToString(b[:])
}
