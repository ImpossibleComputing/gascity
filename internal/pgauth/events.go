package pgauth

import "github.com/gastownhall/gascity/internal/events"

// PostgresCredentialResolvedPayload describes a successful Postgres credential
// resolution without carrying the credential value.
type PostgresCredentialResolvedPayload struct {
	ScopeKind string `json:"scope_kind"`
	ScopeName string `json:"scope_name"`
	Source    string `json:"source"`
	Host      string `json:"host"`
	Port      string `json:"port"`
	User      string `json:"user"`
}

// IsEventPayload marks PostgresCredentialResolvedPayload as an events payload.
func (PostgresCredentialResolvedPayload) IsEventPayload() {}

func init() {
	events.RegisterPayload(events.PostgresCredentialResolved, PostgresCredentialResolvedPayload{})
}
