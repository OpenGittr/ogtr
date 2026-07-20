package app

import (
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/migration"

	"github.com/opengittr/ogtr/backend/auth"
	"github.com/opengittr/ogtr/backend/limits"
)

// stubProvider is a no-op IdentityProvider for wiring tests.
type stubProvider struct{}

func (stubProvider) Verify(*gofr.Context, string) (auth.Identity, error) {
	return auth.Identity{}, nil
}

// allowAllPolicy is a distinct Policy implementation so plumb-through is
// observable by identity.
type allowAllPolicy struct{ limits.UnimplementedPolicy }

func TestNewOptions_Defaults(t *testing.T) {
	o := newOptions()

	assert.Equal(t, limits.Unlimited{}, o.policy, "default policy must be Unlimited (current behavior)")
	assert.Empty(t, o.migrations)
	assert.Nil(t, o.routes)
	assert.Empty(t, o.providers)
	assert.Empty(t, o.exemptPaths)
}

func TestNewOptions_PlumbThrough(t *testing.T) {
	policy := allowAllPolicy{}
	migs := map[int64]migration.Migrate{20990101000000: {}}
	providers := map[string]auth.IdentityProvider{"stub": stubProvider{}}
	routes := func(*gofr.App, *Services) {}

	o := newOptions(
		WithPolicy(policy),
		WithMigrations(migs),
		WithRoutes(routes),
		WithProviders(providers),
		WithAuthExemptPaths("/api/v1/callback", "/api/v1/other"),
	)

	assert.Equal(t, policy, o.policy)
	assert.Equal(t, migs, o.migrations)
	assert.NotNil(t, o.routes)
	assert.Equal(t, providers, o.providers)
	assert.Equal(t, []string{"/api/v1/callback", "/api/v1/other"}, o.exemptPaths)
}

func TestWithAuthExemptPaths_Accumulates(t *testing.T) {
	o := newOptions(WithAuthExemptPaths("/a"), WithAuthExemptPaths("/b"))

	assert.Equal(t, []string{"/a", "/b"}, o.exemptPaths)
}

func TestMergeMigrations(t *testing.T) {
	core := map[int64]migration.Migrate{1: {}, 2: {}}

	t.Run("no extras returns core unchanged", func(t *testing.T) {
		merged, err := mergeMigrations(core, nil)

		require.NoError(t, err)
		assert.Len(t, merged, 2)
	})

	t.Run("extras merge after core", func(t *testing.T) {
		merged, err := mergeMigrations(core, map[int64]migration.Migrate{3: {}})

		require.NoError(t, err)
		assert.Len(t, merged, 3)
		assert.Contains(t, merged, int64(3))
		// The input maps are not mutated.
		assert.Len(t, core, 2)
	})

	t.Run("timestamp collision refuses to start", func(t *testing.T) {
		_, err := mergeMigrations(core, map[int64]migration.Migrate{2: {}})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "migration 2")
	})
}

// testEnv sets the minimal config a successful setup needs.
func testEnv(t *testing.T) {
	t.Helper()
	t.Setenv("JWT_SIGNING_KEY", "test-signing-key")
	t.Setenv("SHORT_DOMAIN", "sho.rt")
	t.Setenv("AUTH_PROVIDERS", "dev")
}

// portBase hands every gofr.New() in this package a distinct HTTP + metrics
// port pair: gofr fatals when a port it was configured with is already bound,
// and each app keeps its listeners for the process lifetime, so ports cannot
// be reused within one test binary.
var portBase atomic.Int64

func init() { portBase.Store(56000) }

// newTestApp builds a gofr app on its own HTTP and metrics ports.
func newTestApp(t *testing.T) *gofr.App {
	t.Helper()

	base := portBase.Add(2)
	t.Setenv("HTTP_PORT", strconv.FormatInt(base, 10))
	t.Setenv("METRICS_PORT", strconv.FormatInt(base+1, 10))

	return gofr.New()
}

func TestSetup_DefaultsSucceed(t *testing.T) {
	testEnv(t)

	require.NoError(t, setup(newTestApp(t), newOptions()))
}

func TestSetup_OptionsPlumbThrough(t *testing.T) {
	testEnv(t)

	var got *Services

	err := setup(newTestApp(t), newOptions(
		WithPolicy(allowAllPolicy{}),
		WithMigrations(map[int64]migration.Migrate{20990101000000: {}}),
		WithProviders(map[string]auth.IdentityProvider{"stub": stubProvider{}}),
		WithAuthExemptPaths("/api/v1/callback"),
		WithRoutes(func(app *gofr.App, s *Services) {
			require.NotNil(t, app)
			got = s
		}),
	))

	require.NoError(t, err)
	require.NotNil(t, got, "WithRoutes callback must run")
	assert.NotNil(t, got.Usage, "Services.Usage must be wired")
	assert.NotNil(t, got.Members, "Services.Members must be wired")
}

func TestSetup_ConfigErrors(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{
			name:    "missing JWT_SIGNING_KEY",
			env:     map[string]string{"SHORT_DOMAIN": "sho.rt", "AUTH_PROVIDERS": "dev"},
			wantErr: "JWT_SIGNING_KEY",
		},
		{
			name:    "missing SHORT_DOMAIN",
			env:     map[string]string{"JWT_SIGNING_KEY": "k", "AUTH_PROVIDERS": "dev"},
			wantErr: "SHORT_DOMAIN",
		},
		{
			name:    "unknown auth provider",
			env:     map[string]string{"JWT_SIGNING_KEY": "k", "SHORT_DOMAIN": "sho.rt", "AUTH_PROVIDERS": "bogus"},
			wantErr: "bogus",
		},
		{
			name: "migration collision",
			env:  map[string]string{"JWT_SIGNING_KEY": "k", "SHORT_DOMAIN": "sho.rt", "AUTH_PROVIDERS": "dev"},
			// collision with the core initial schema timestamp
			wantErr: "20260713221852",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for key, val := range tc.env {
				t.Setenv(key, val)
			}

			opts := newOptions()
			if tc.name == "migration collision" {
				opts = newOptions(WithMigrations(map[int64]migration.Migrate{20260713221852: {}}))
			}

			err := setup(newTestApp(t), opts)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestBuildProviders(t *testing.T) {
	testEnv(t) // AUTH_PROVIDERS=dev

	t.Run("config providers only", func(t *testing.T) {
		providers, names, err := buildProviders(newTestApp(t), nil)

		require.NoError(t, err)
		assert.Equal(t, []string{"dev"}, names)
		assert.Contains(t, providers, "dev")
	})

	t.Run("extra providers are additive and login-enabled", func(t *testing.T) {
		extra := map[string]auth.IdentityProvider{"stub": stubProvider{}, "another": stubProvider{}}

		providers, names, err := buildProviders(newTestApp(t), extra)

		require.NoError(t, err)
		// Config names first, then extras sorted for determinism.
		assert.Equal(t, []string{"dev", "another", "stub"}, names)
		assert.Contains(t, providers, "stub")
		assert.Contains(t, providers, "another")
		assert.Contains(t, providers, "dev")
	})

	t.Run("extra with colliding name replaces built-in without duplicating", func(t *testing.T) {
		extra := map[string]auth.IdentityProvider{"dev": stubProvider{}}

		providers, names, err := buildProviders(newTestApp(t), extra)

		require.NoError(t, err)
		assert.Equal(t, []string{"dev"}, names)
		assert.IsType(t, stubProvider{}, providers["dev"])
	})
}
