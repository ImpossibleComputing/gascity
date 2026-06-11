package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Bearer-token auth on the supervisor mutation surface (ga-7otii0).
// When a token is configured, every mutating method (POST/PUT/PATCH/DELETE)
// requires Authorization: Bearer <token>; reads stay open so loopback
// observability (dashboard reads, event streams) keeps working.

func TestSupervisorAPITokenMutationWithoutTokenUnauthorized(t *testing.T) {
	sm := newTestSupervisorMux(t, map[string]*fakeState{})
	sm.WithAPIToken("seekrit")

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8372/v0/city/ghost/unregister", nil)
	req.Header.Set("X-GC-Request", "true")
	rec := httptest.NewRecorder()

	sm.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/problem+json") {
		t.Fatalf("Content-Type = %q, want application/problem+json", got)
	}
	if !strings.Contains(rec.Body.String(), "unauthorized") {
		t.Fatalf("body = %q, want unauthorized problem detail", rec.Body.String())
	}
	if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Bearer") {
		t.Fatalf("WWW-Authenticate = %q, want Bearer challenge", got)
	}
}

func TestSupervisorAPITokenMutationWithWrongTokenUnauthorized(t *testing.T) {
	sm := newTestSupervisorMux(t, map[string]*fakeState{})
	sm.WithAPIToken("seekrit")

	cases := []struct {
		name  string
		value string
	}{
		{"wrong token", "Bearer not-the-token"},
		{"empty bearer", "Bearer "},
		{"wrong scheme", "Basic c2Vla3JpdA=="},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8372/v0/city/ghost/unregister", nil)
			req.Header.Set("X-GC-Request", "true")
			req.Header.Set("Authorization", tc.value)
			rec := httptest.NewRecorder()

			sm.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
			}
		})
	}
}

func TestSupervisorAPITokenMutationWithTokenReachesHandler(t *testing.T) {
	sm := newTestSupervisorMux(t, map[string]*fakeState{})
	sm.WithAPIToken("seekrit")

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8372/v0/city/ghost/unregister", nil)
	req.Header.Set("X-GC-Request", "true")
	req.Header.Set("Authorization", "Bearer seekrit")
	rec := httptest.NewRecorder()

	sm.Handler().ServeHTTP(rec, req)

	// nil initializer → the handler itself answers 501. Anything but
	// 401/403 proves the request cleared token auth and CSRF.
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d (handler reached); body=%s", rec.Code, http.StatusNotImplemented, rec.Body.String())
	}
}

func TestSupervisorAPITokenReadsStayOpen(t *testing.T) {
	sm := newTestSupervisorMux(t, map[string]*fakeState{})
	sm.WithAPIToken("seekrit")

	for _, path := range []string{"/v0/cities", "/health"} {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8372"+path, nil)
		rec := httptest.NewRecorder()

		sm.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d; body=%s", path, rec.Code, http.StatusOK, rec.Body.String())
		}
	}
}

func TestSupervisorAPITokenCoversSvcProxyMutations(t *testing.T) {
	sm := newTestSupervisorMux(t, map[string]*fakeState{})
	sm.WithAPIToken("seekrit")

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8372/v0/city/ghost/svc/myservice/run", strings.NewReader("{}"))
	rec := httptest.NewRecorder()

	sm.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestSupervisorAPITokenUnsetLeavesMutationsOpen(t *testing.T) {
	sm := newTestSupervisorMux(t, map[string]*fakeState{})

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8372/v0/city/ghost/unregister", nil)
	req.Header.Set("X-GC-Request", "true")
	rec := httptest.NewRecorder()

	sm.Handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("status = %d; token auth must be disabled when no token is configured (body=%s)", rec.Code, rec.Body.String())
	}
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d (handler reached); body=%s", rec.Code, http.StatusNotImplemented, rec.Body.String())
	}
}

func TestSupervisorAPITokenHostRejectionPrecedesTokenAuth(t *testing.T) {
	sm := newTestSupervisorMux(t, map[string]*fakeState{})
	sm.WithAPIToken("seekrit")

	req := httptest.NewRequest(http.MethodPost, "http://evil.example:8372/v0/city/ghost/unregister", nil)
	rec := httptest.NewRecorder()

	sm.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMisdirectedRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusMisdirectedRequest, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "unauthorized") {
		t.Fatalf("body = %q, host rejection must happen before token auth", rec.Body.String())
	}
}
