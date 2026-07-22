package app

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opengittr/ogtr/backend/auth"
)

// TestPublicServer_ServesNoAdminAPI is the regression test for the
// instance-admin split (ARCHITECTURE.md "Instance admin service"): the
// public server must not serve /api/internal/* under ANY circumstances —
// not even with ADMIN_API_TOKEN configured in its environment and the
// correct X-Admin-Token presented. The admin API lives exclusively in the
// separate backend/admin service.
func TestPublicServer_ServesNoAdminAPI(t *testing.T) {
	testEnv(t)
	// Even if an operator mistakenly configures the admin token on the
	// public server, no admin surface may appear.
	const adminToken = "should-be-ignored-by-the-public-server"

	t.Setenv("ADMIN_API_TOKEN", adminToken)

	app := newTestApp(t)
	httpPort := app.Config.Get("HTTP_PORT")

	require.NoError(t, setup(app, newOptions()))

	go app.Run()

	baseURL := "http://localhost:" + httpPort
	waitUntilUp(t, baseURL)

	client := &http.Client{Timeout: 2 * time.Second}

	// A valid ogtr session token: even an authenticated user must find no
	// admin route (gofr's router answers 404 route-not-found).
	pair, err := auth.NewTokenIssuer("test-signing-key", 0, 0).IssuePair(1, 1, "OWNER")
	require.NoError(t, err)

	adminPaths := []struct {
		method, path string
	}{
		{http.MethodGet, "/api/internal/users"},
		{http.MethodGet, "/api/internal/orgs"},
		{http.MethodGet, "/api/internal/orgs/1/users"},
		{http.MethodGet, "/api/internal/reports"},
		{http.MethodGet, "/api/internal/links/1"},
		{http.MethodPost, "/api/internal/links/1/disable"},
		{http.MethodPost, "/api/internal/links/1/enable"},
		{http.MethodGet, "/api/internal/stats/daily"},
	}

	for _, tc := range adminPaths {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			// With only the admin token: an ordinary guarded /api path —
			// 401, the admin token buys nothing.
			req, err := http.NewRequest(tc.method, baseURL+tc.path, http.NoBody)
			require.NoError(t, err)
			req.Header.Set("X-Admin-Token", adminToken)

			resp, err := client.Do(req)
			require.NoError(t, err)
			_ = resp.Body.Close()
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode,
				"admin token alone must not reach anything on the public server")

			// With a fully valid session AND the admin token: 404 — no
			// admin route is registered at all.
			req, err = http.NewRequest(tc.method, baseURL+tc.path, http.NoBody)
			require.NoError(t, err)
			req.Header.Set("X-Admin-Token", adminToken)
			req.Header.Set("Authorization", "Bearer "+pair.AccessToken)

			resp, err = client.Do(req)
			require.NoError(t, err)
			_ = resp.Body.Close()
			assert.Equal(t, http.StatusNotFound, resp.StatusCode,
				"no /api/internal route may exist on the public server")
		})
	}
}

// waitUntilUp polls the server's liveness endpoint until it answers.
func waitUntilUp(t *testing.T, baseURL string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/.well-known/alive") //nolint:noctx // test poll
		if err == nil {
			_ = resp.Body.Close()

			return
		}

		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("server at %s did not come up", baseURL)
}
