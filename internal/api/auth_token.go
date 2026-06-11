package api

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
)

// SupervisorAPITokenEnv is the environment variable holding the shared
// bearer token that gates supervisor API mutations. When the supervisor
// process starts with this set, every mutating (POST/PUT/PATCH/DELETE)
// request must carry "Authorization: Bearer <token>"; reads stay open.
// The gc CLI client reads the same variable and attaches the header
// automatically, so CLI-routed mutations keep working unchanged.
const SupervisorAPITokenEnv = "GC_SUPERVISOR_API_TOKEN"

// SupervisorAPITokenFromEnv returns the configured supervisor API bearer
// token, trimmed of surrounding whitespace (so file-sourced exports with a
// trailing newline behave). Empty means token auth is disabled.
func SupervisorAPITokenFromEnv() string {
	return strings.TrimSpace(os.Getenv(SupervisorAPITokenEnv))
}

// withBearerTokenAuth enforces a shared bearer token on mutating requests
// when token is non-empty. Reads (GET/HEAD; OPTIONS is answered by the CORS
// layer) pass through so loopback observability — dashboard reads, event
// streams, status lines — keeps working without credential plumbing; the
// destructive surface is the defense-in-depth target. Runs at the mux level
// so it covers the /svc/* proxy as well as every Huma operation. Token
// comparison is constant-time.
func withBearerTokenAuth(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	want := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isMutationMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		got, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok || subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="gc-supervisor"`)
			problemAPITokenRequired.writeTo(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// setSupervisorAPITokenHeader attaches the GC_SUPERVISOR_API_TOKEN bearer
// token to req when one is configured in the client's environment. Sent on
// every request (reads included) — the server only requires it on mutations,
// and a stray Authorization header on open reads is harmless.
func setSupervisorAPITokenHeader(req *http.Request) {
	if token := SupervisorAPITokenFromEnv(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// bearerToken extracts the token from an Authorization header value.
// Returns false when the header is absent, uses a non-Bearer scheme, or
// carries an empty token.
func bearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	tok := strings.TrimSpace(header[len(prefix):])
	return tok, tok != ""
}
