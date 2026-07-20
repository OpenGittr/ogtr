package auth

import (
	"encoding/json"
	"net/http"
	"strings"

	gofrHTTP "gofr.dev/pkg/gofr/http"
)

// APIKeyHeader carries a developer API key on the two key-auth routes
// (ARCHITECTURE.md §4): POST /api/v1/links and GET /api/v1/resolve.
const APIKeyHeader = "X-API-Key"

// Middleware returns the Gofr HTTP middleware that guards the management API.
//
// Guarded: every /api/* route. Exempt: POST /api/v1/auth/google,
// POST /api/v1/auth/microsoft, POST /api/v1/auth/dev and
// POST /api/v1/auth/refresh (they bootstrap a session),
// GET /api/v1/auth/providers (the SPA asks it before any session
// exists), GET /api/v1/resolve
// (resolution never requires login, FEATURES.md §2.1), plus everything
// outside /api/ — Gofr's well-known health/alive/openapi paths, favicon, and
// the public GET /{code} redirect. Metrics live on a separate port.
//
// extraExempt lists additional exact request paths (any method) a
// deployment exempts from auth — endpoints it registers itself that carry
// their own authentication (e.g. signature-verified callback receivers).
// Empty for the stock assembly.
//
// X-API-Key: on POST /api/v1/links and GET /api/v1/resolve ONLY, a present
// X-API-Key header replaces JWT auth — the raw key is forwarded on the
// context (APIKeyFromContext) and the handler authenticates it against the
// database (invalid/disabled key → 401 there; the org context then comes
// from the key, not a token). When both header and bearer token are sent on
// those routes, the key wins. On every other route the header is ignored and
// JWT is required as usual.
//
// On success the validated access-token claims are placed on the request
// context (ClaimsFromContext). Org-required endpoints additionally call
// RequireOrg — an org-less token authenticates but cannot touch org resources.
func Middleware(tokens *TokenIssuer, extraExempt ...string) gofrHTTP.Middleware {
	exempt := make(map[string]bool, len(extraExempt))
	for _, path := range extraExempt {
		exempt[path] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rawKey := r.Header.Get(APIKeyHeader); rawKey != "" && isKeyAuthRoute(r) {
				next.ServeHTTP(w, r.WithContext(ContextWithAPIKey(r.Context(), rawKey)))

				return
			}

			if isAuthExempt(r) || exempt[r.URL.Path] {
				next.ServeHTTP(w, r)

				return
			}

			raw, ok := bearerToken(r)
			if !ok {
				writeAuthError(w, "missing bearer token")

				return
			}

			claims, err := tokens.Parse(raw, TokenTypeAccess)
			if err != nil {
				writeAuthError(w, err.Error())

				return
			}

			next.ServeHTTP(w, r.WithContext(ContextWithClaims(r.Context(), claims)))
		})
	}
}

// isKeyAuthRoute lists the only routes where X-API-Key is honored
// (ARCHITECTURE.md §4): programmatic link creation and resolution.
func isKeyAuthRoute(r *http.Request) bool {
	return (r.Method == http.MethodPost && r.URL.Path == "/api/v1/links") ||
		(r.Method == http.MethodGet && r.URL.Path == "/api/v1/resolve")
}

func isAuthExempt(r *http.Request) bool {
	path := r.URL.Path

	if !strings.HasPrefix(path, "/api/") {
		return true
	}

	if r.Method == http.MethodPost &&
		(path == "/api/v1/auth/google" || path == "/api/v1/auth/microsoft" ||
			path == "/api/v1/auth/dev" || path == "/api/v1/auth/refresh") {
		return true
	}

	if r.Method == http.MethodGet && path == "/api/v1/auth/providers" {
		return true
	}

	// Public abuse reporting: recipients of a bad link have no account here
	// (rate-limited per IP in the service instead).
	if r.Method == http.MethodPost && path == "/api/v1/report" {
		return true
	}

	return r.Method == http.MethodGet && path == "/api/v1/resolve"
}

func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")

	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}

	return strings.TrimSpace(header[len(prefix):]), true
}

// writeAuthError emits a 401 in Gofr's error envelope; the middleware runs
// before Gofr's responder so it has to write the response itself.
func writeAuthError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)

	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"message": msg},
	})
}
