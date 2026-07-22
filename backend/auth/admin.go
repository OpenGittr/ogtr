package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	gofrHTTP "gofr.dev/pkg/gofr/http"
)

// AdminTokenHeader carries the instance-admin token on every
// /api/internal/* request (ARCHITECTURE.md "Instance admin API").
const AdminTokenHeader = "X-Admin-Token"

// AdminPathPrefix scopes the instance-admin API. The JWT middleware exempts
// this prefix (AdminTokenGate replaces it as the guard).
const AdminPathPrefix = "/api/internal/"

// AdminTokenGate returns the middleware guarding the instance-admin API
// (every route under /api/internal/). The gate is the config switch AND the
// authentication in one:
//
//   - configuredToken empty (ADMIN_API_TOKEN unset — the default): every
//     admin request answers 404, exactly like the dev-provider pattern —
//     the feature is dark and its existence is not advertised.
//   - configuredToken set: the request's X-Admin-Token header must equal it
//     (constant-time comparison over SHA-256 digests, so neither content nor
//     length leaks through timing). A missing or wrong token is also 404,
//     not 401 — a prober without the token cannot distinguish a deployment
//     that has the admin API enabled from one that does not.
//
// Non-admin paths pass through untouched.
func AdminTokenGate(configuredToken string) gofrHTTP.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, AdminPathPrefix) {
				next.ServeHTTP(w, r)

				return
			}

			if configuredToken == "" || !adminTokenMatches(r.Header.Get(AdminTokenHeader), configuredToken) {
				writeAdminNotFound(w)

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// adminTokenMatches compares the presented token against the configured one
// in constant time. Both sides are hashed first so the comparison length is
// fixed — subtle.ConstantTimeCompare alone short-circuits on unequal
// lengths, which would leak the configured token's length.
func adminTokenMatches(presented, configured string) bool {
	presentedSum := sha256.Sum256([]byte(presented))
	configuredSum := sha256.Sum256([]byte(configured))

	return subtle.ConstantTimeCompare(presentedSum[:], configuredSum[:]) == 1
}

// writeAdminNotFound emits a 404 in Gofr's error envelope; the middleware
// runs before Gofr's responder so it writes the response itself. The body is
// deliberately generic — the same for "feature disabled", "missing token"
// and "wrong token".
func writeAdminNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)

	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"message": "route not found"},
	})
}
