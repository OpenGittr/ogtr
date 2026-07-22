package adminapp

import "gofr.dev/pkg/gofr"

// Services exposes assembled dependencies to deployment-registered routes
// (WithRoutes). The surface is deliberately tight — currently empty, since
// composed operator routes work against the shared database via ctx.SQL —
// and grows additively when a real route needs more, mirroring
// backend/app.Services.
type Services struct{}

// options collects everything a deployment can customize; the zero value
// reproduces the stock single-binary behavior exactly.
type options struct {
	routes func(*gofr.App, *Services)
}

// Option customizes the service assembly (see Run).
type Option func(*options)

// newOptions applies opts over the defaults.
func newOptions(opts ...Option) *options {
	o := &options{}

	for _, opt := range opts {
		opt(o)
	}

	return o
}

// WithRoutes registers extra HTTP routes after all core routes; the callback
// receives the gofr app plus the assembled Services. Routes registered under
// AdminPathPrefix (/api/internal/) sit behind the same ADMIN_API_TOKEN gate
// as the core routes — deployment operator routes MUST use that prefix, or
// they are served ungated like the health endpoints.
func WithRoutes(fn func(*gofr.App, *Services)) Option {
	return func(o *options) { o.routes = fn }
}
