package adminapp

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gofr.dev/pkg/gofr"
)

func TestNewOptions_Defaults(t *testing.T) {
	o := newOptions()

	assert.Nil(t, o.routes, "no deployment routes by default (stock behavior)")
}

func TestNewOptions_PlumbThrough(t *testing.T) {
	o := newOptions(WithRoutes(func(*gofr.App, *Services) {}))

	assert.NotNil(t, o.routes)
}

// portBase hands every gofr.New() in this package a distinct HTTP + metrics
// port pair (same rationale as backend/app's tests: gofr fatals when a
// configured port is already bound).
var portBase atomic.Int64

func init() { portBase.Store(57000) }

// newTestApp builds a gofr app on its own HTTP and metrics ports.
func newTestApp(t *testing.T) *gofr.App {
	t.Helper()

	base := portBase.Add(2)
	t.Setenv("HTTP_PORT", strconv.FormatInt(base, 10))
	t.Setenv("METRICS_PORT", strconv.FormatInt(base+1, 10))

	return gofr.New()
}

func TestSetup_DefaultsSucceed(t *testing.T) {
	setup(newTestApp(t), newOptions())
}

func TestSetup_RoutesPlumbThrough(t *testing.T) {
	var got *Services

	setup(newTestApp(t), newOptions(WithRoutes(func(app *gofr.App, s *Services) {
		require.NotNil(t, app)
		got = s
	})))

	require.NotNil(t, got, "WithRoutes callback must run with a non-nil Services")
}

// TestGate_CoversDeploymentRoutePaths pins that a deployment route registered
// under AdminPathPrefix (the WithRoutes contract) sits behind the exact same
// token gate as the core routes: unauthenticated is 404, the right token
// passes through.
func TestGate_CoversDeploymentRoutePaths(t *testing.T) {
	const token = "deploy-token"

	deploymentPath := AdminPathPrefix + "hosted/some-report"

	reached := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	t.Run("no token is 404", func(t *testing.T) {
		reached = false
		req := httptest.NewRequest(http.MethodGet, deploymentPath, http.NoBody)
		rec := httptest.NewRecorder()

		AdminTokenGate(token)(next).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.False(t, reached)
	})

	t.Run("right token passes", func(t *testing.T) {
		reached = false
		req := httptest.NewRequest(http.MethodGet, deploymentPath, http.NoBody)
		req.Header.Set(AdminTokenHeader, token)
		rec := httptest.NewRecorder()

		AdminTokenGate(token)(next).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.True(t, reached)
	})
}
