package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMiddleware(t *testing.T) {
	issuer := testIssuer()

	pair, err := issuer.IssuePair(11, 22, "OWNER")
	require.NoError(t, err)

	refreshOnly, err := issuer.IssuePair(11, 22, "OWNER")
	require.NoError(t, err)

	tests := []struct {
		desc        string
		method      string
		path        string
		authHeader  string
		apiKey      string
		wantStatus  int
		wantClaims  bool
		wantUserID  int64
		wantOrgID   int64
		wantReached bool
		wantAPIKey  string
	}{
		{
			desc: "well-known health is exempt", method: http.MethodGet, path: "/.well-known/alive",
			wantStatus: http.StatusOK, wantReached: true,
		},
		{
			desc: "future redirect path is exempt", method: http.MethodGet, path: "/somecode",
			wantStatus: http.StatusOK, wantReached: true,
		},
		{
			desc: "login is exempt", method: http.MethodPost, path: "/api/v1/auth/google",
			wantStatus: http.StatusOK, wantReached: true,
		},
		{
			desc: "dev login is exempt", method: http.MethodPost, path: "/api/v1/auth/dev",
			wantStatus: http.StatusOK, wantReached: true,
		},
		{
			desc: "providers listing is exempt", method: http.MethodGet, path: "/api/v1/auth/providers",
			wantStatus: http.StatusOK, wantReached: true,
		},
		{
			desc: "POST providers is not exempt", method: http.MethodPost, path: "/api/v1/auth/providers",
			wantStatus: http.StatusUnauthorized,
		},
		{
			desc: "refresh is exempt", method: http.MethodPost, path: "/api/v1/auth/refresh",
			wantStatus: http.StatusOK, wantReached: true,
		},
		{
			desc: "GET resolve is exempt", method: http.MethodGet, path: "/api/v1/resolve",
			wantStatus: http.StatusOK, wantReached: true,
		},
		{
			desc: "POST resolve is not exempt", method: http.MethodPost, path: "/api/v1/resolve",
			wantStatus: http.StatusUnauthorized,
		},
		{
			// FEATURES.md INV-3 regression: unauthenticated visitors must
			// never be able to write deep-link metadata. The owner-only
			// write endpoint must always demand a session token.
			desc: "unauthenticated deeplink write is rejected", method: http.MethodPut,
			path: "/api/v1/links/9/deeplink",
			wantStatus: http.StatusUnauthorized,
		},
		{
			desc: "api without token is rejected", method: http.MethodGet, path: "/api/v1/me",
			wantStatus: http.StatusUnauthorized,
		},
		{
			desc: "api with garbage token is rejected", method: http.MethodGet, path: "/api/v1/me",
			authHeader: "Bearer garbage", wantStatus: http.StatusUnauthorized,
		},
		{
			desc: "api with refresh token as access is rejected", method: http.MethodGet, path: "/api/v1/me",
			authHeader: "Bearer " + refreshOnly.RefreshToken, wantStatus: http.StatusUnauthorized,
		},
		{
			desc: "api with malformed authorization header", method: http.MethodGet, path: "/api/v1/me",
			authHeader: "Token abc", wantStatus: http.StatusUnauthorized,
		},
		{
			desc: "api with valid token passes and carries claims", method: http.MethodGet, path: "/api/v1/org",
			authHeader: "Bearer " + pair.AccessToken, wantStatus: http.StatusOK,
			wantReached: true, wantClaims: true, wantUserID: 11, wantOrgID: 22,
		},
		{
			desc: "X-API-Key on POST /links skips JWT and forwards the key", method: http.MethodPost,
			path: "/api/v1/links", apiKey: "slk_abc",
			wantStatus: http.StatusOK, wantReached: true, wantAPIKey: "slk_abc",
		},
		{
			desc: "X-API-Key wins over a bearer token on POST /links", method: http.MethodPost,
			path: "/api/v1/links", apiKey: "slk_abc", authHeader: "Bearer " + pair.AccessToken,
			wantStatus: http.StatusOK, wantReached: true, wantAPIKey: "slk_abc",
		},
		{
			desc: "X-API-Key on GET /resolve is forwarded", method: http.MethodGet,
			path: "/api/v1/resolve", apiKey: "slk_abc",
			wantStatus: http.StatusOK, wantReached: true, wantAPIKey: "slk_abc",
		},
		{
			desc: "X-API-Key on any other route is ignored: JWT still required", method: http.MethodGet,
			path: "/api/v1/links", apiKey: "slk_abc",
			wantStatus: http.StatusUnauthorized,
		},
		{
			desc: "X-API-Key on DELETE /api-keys is ignored: JWT still required", method: http.MethodDelete,
			path: "/api/v1/api-keys/11", apiKey: "slk_abc",
			wantStatus: http.StatusUnauthorized,
		},
		{
			desc: "POST /links without key still requires JWT", method: http.MethodPost,
			path: "/api/v1/links",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			var (
				reached bool
				claims  *SessionClaims
				gotKey  string
			)

			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reached = true
				claims, _ = ClaimsFromContext(r.Context())
				gotKey, _ = APIKeyFromContext(r.Context())
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(tc.method, tc.path, http.NoBody)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}

			if tc.apiKey != "" {
				req.Header.Set(APIKeyHeader, tc.apiKey)
			}

			rec := httptest.NewRecorder()

			Middleware(issuer)(next).ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Equal(t, tc.wantReached, reached)

			if tc.wantClaims {
				require.NotNil(t, claims)
				assert.Equal(t, tc.wantUserID, claims.UserID)
				assert.Equal(t, tc.wantOrgID, claims.OrgID)
			}

			assert.Equal(t, tc.wantAPIKey, gotKey)

			if tc.wantStatus == http.StatusUnauthorized {
				assert.Contains(t, rec.Body.String(), "error")
				assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			}
		})
	}
}

func TestMiddleware_ExtraExemptPaths(t *testing.T) {
	issuer := testIssuer()

	tests := []struct {
		desc       string
		method     string
		path       string
		exempt     []string
		wantStatus int
	}{
		{
			desc: "listed path passes without a token", method: http.MethodPost,
			path: "/api/v1/inbound/callback", exempt: []string{"/api/v1/inbound/callback"},
			wantStatus: http.StatusOK,
		},
		{
			desc: "listed path is method-agnostic", method: http.MethodGet,
			path: "/api/v1/inbound/callback", exempt: []string{"/api/v1/inbound/callback"},
			wantStatus: http.StatusOK,
		},
		{
			desc: "unlisted sibling path still requires auth", method: http.MethodPost,
			path: "/api/v1/inbound/other", exempt: []string{"/api/v1/inbound/callback"},
			wantStatus: http.StatusUnauthorized,
		},
		{
			desc: "no extra paths keeps the stock behavior", method: http.MethodPost,
			path: "/api/v1/inbound/callback",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(tc.method, tc.path, http.NoBody)
			rec := httptest.NewRecorder()

			Middleware(issuer, tc.exempt...)(next).ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
		})
	}
}
