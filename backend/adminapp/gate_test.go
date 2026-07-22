package adminapp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAdminTokenGate(t *testing.T) {
	const token = "local-admin-token"

	tests := []struct {
		desc        string
		configured  string
		path        string
		header      string
		wantStatus  int
		wantReached bool
	}{
		{
			desc: "token unset: admin path is 404 even with a header", configured: "",
			path: "/api/internal/users", header: token,
			wantStatus: http.StatusNotFound,
		},
		{
			desc: "token unset: admin path without header is 404", configured: "",
			path: "/api/internal/users",
			wantStatus: http.StatusNotFound,
		},
		{
			desc: "missing header is 404, not 401", configured: token,
			path: "/api/internal/users",
			wantStatus: http.StatusNotFound,
		},
		{
			desc: "wrong token is 404, not 401", configured: token,
			path: "/api/internal/users", header: "wrong-token",
			wantStatus: http.StatusNotFound,
		},
		{
			desc: "prefix of the real token is 404", configured: token,
			path: "/api/internal/users", header: "local-admin",
			wantStatus: http.StatusNotFound,
		},
		{
			desc: "right token passes", configured: token,
			path: "/api/internal/users", header: token,
			wantStatus: http.StatusOK, wantReached: true,
		},
		{
			desc: "right token passes on nested admin paths", configured: token,
			path: "/api/internal/links/7/disable", header: token,
			wantStatus: http.StatusOK, wantReached: true,
		},
		{
			desc: "non-admin API path passes through untouched", configured: token,
			path: "/api/v1/me",
			wantStatus: http.StatusOK, wantReached: true,
		},
		{
			desc: "non-admin path passes through when token unset", configured: "",
			path: "/somecode",
			wantStatus: http.StatusOK, wantReached: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			reached := false

			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				reached = true
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, tc.path, http.NoBody)
			if tc.header != "" {
				req.Header.Set(AdminTokenHeader, tc.header)
			}

			rec := httptest.NewRecorder()

			AdminTokenGate(tc.configured)(next).ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Equal(t, tc.wantReached, reached)

			if tc.wantStatus == http.StatusNotFound {
				assert.Contains(t, rec.Body.String(), "error")
				assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			}
		})
	}
}

// TestAdminTokenMatches pins the comparison helper: it is the constant-time
// SHA-256 form (see adminTokenMatches) — these cases guard its correctness
// across lengths so a refactor cannot quietly break matching semantics.
func TestAdminTokenMatches(t *testing.T) {
	tests := []struct {
		desc                  string
		presented, configured string
		want                  bool
	}{
		{desc: "equal tokens match", presented: "abc123", configured: "abc123", want: true},
		{desc: "different tokens do not match", presented: "abc124", configured: "abc123"},
		{desc: "different lengths do not match", presented: "abc", configured: "abc123"},
		{desc: "empty presented does not match", presented: "", configured: "abc123"},
		{desc: "both empty match (gate rejects earlier on empty config)", presented: "", configured: "", want: true},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			assert.Equal(t, tc.want, adminTokenMatches(tc.presented, tc.configured))
		})
	}
}
