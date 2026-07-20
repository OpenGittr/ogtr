package app

import (
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/migration"

	"github.com/opengittr/ogtr/backend/auth"
	"github.com/opengittr/ogtr/backend/limits"
	"github.com/opengittr/ogtr/backend/usage"
)

// Services exposes the assembled application dependencies that
// deployment-registered routes (WithRoutes) realistically need. The surface
// is deliberately tight — interfaces only, no concrete stores — and grows
// additively when a real route needs more.
type Services struct {
	// Usage exposes the per-org usage counters (backend/usage).
	Usage usage.Reader

	// Members reports a user's role in an org, for routes that restrict
	// actions to org owners (checked against the DB, like the core's own
	// OWNER-only endpoints).
	Members RoleReader
}

// RoleReader reports a user's role in an org; ("", nil) when the user is not
// a member. Satisfied by stores.MemberStore.
type RoleReader interface {
	GetRole(ctx *gofr.Context, orgID, userID int64) (string, error)
}

// options collects everything a deployment can customize; the zero value
// (plus defaults applied in setup) reproduces the stock single-binary
// behavior exactly.
type options struct {
	policy      limits.Policy
	migrations  map[int64]migration.Migrate
	routes      func(*gofr.App, *Services)
	providers   map[string]auth.IdentityProvider
	exemptPaths []string
}

// Option customizes the application assembly (see Run).
type Option func(*options)

// newOptions applies opts over the defaults.
func newOptions(opts ...Option) *options {
	o := &options{policy: limits.Unlimited{}}

	for _, opt := range opts {
		opt(o)
	}

	return o
}

// WithPolicy substitutes the deployment's limits.Policy (ARCHITECTURE.md §8
// "Extension seam: LimitsPolicy") for the default limits.Unlimited{}. Every
// service that consults the seam receives this instance.
func WithPolicy(p limits.Policy) Option {
	return func(o *options) { o.policy = p }
}

// WithMigrations registers additional database migrations, merged after the
// core's own (backend/migrations). Keys are the usual `date +%Y%m%d%H%M%S`
// timestamps; a key colliding with a core migration is a startup error.
func WithMigrations(m map[int64]migration.Migrate) Option {
	return func(o *options) { o.migrations = m }
}

// WithRoutes registers extra HTTP routes after all core routes; the callback
// receives the gofr app plus the assembled Services.
func WithRoutes(fn func(*gofr.App, *Services)) Option {
	return func(o *options) { o.routes = fn }
}

// WithProviders adds identity providers on top of those configured via
// AUTH_PROVIDERS (additive; a name colliding with a configured provider is
// replaced by the supplied one). Added providers are login-enabled exactly
// like configured ones.
func WithProviders(p map[string]auth.IdentityProvider) Option {
	return func(o *options) { o.providers = p }
}

// WithAuthExemptPaths exempts additional exact request paths from the auth
// middleware, for deployment-registered public endpoints (e.g. callback
// receivers authenticated by their own signature scheme).
func WithAuthExemptPaths(paths ...string) Option {
	return func(o *options) { o.exemptPaths = append(o.exemptPaths, paths...) }
}
