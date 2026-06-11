package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The CLI client reads GC_SUPERVISOR_API_TOKEN from its own environment and
// sends it as Authorization: Bearer on every request, so a token-gated
// supervisor (ga-7otii0) keeps accepting CLI-routed mutations.

func TestClientSendsBearerTokenFromEnv(t *testing.T) {
	t.Setenv(SupervisorAPITokenEnv, "tok-123\n")

	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`)) //nolint:errcheck
	}))
	defer ts.Close()

	c := NewCityScopedClient(ts.URL, "alpha")
	if err := c.SuspendCity(); err != nil {
		t.Fatalf("SuspendCity: %v", err)
	}
	if gotAuth != "Bearer tok-123" {
		t.Fatalf("Authorization = %q, want %q (token trimmed of whitespace)", gotAuth, "Bearer tok-123")
	}
}

func TestClientOmitsAuthorizationWhenTokenUnset(t *testing.T) {
	t.Setenv(SupervisorAPITokenEnv, "")

	var gotAuth string
	var sawAuth bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, sawAuth = r.Header["Authorization"]
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`)) //nolint:errcheck
	}))
	defer ts.Close()

	c := NewCityScopedClient(ts.URL, "alpha")
	if err := c.SuspendCity(); err != nil {
		t.Fatalf("SuspendCity: %v", err)
	}
	if sawAuth {
		t.Fatalf("Authorization = %q, want header absent when no token configured", gotAuth)
	}
}
