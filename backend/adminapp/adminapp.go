// Package adminapp assembles the ogtr instance-admin service — the operator
// surface of a deployment (ARCHITECTURE.md "Instance admin service"), split
// out of the public server so the cross-org admin API never shares a
// listener with internet-facing traffic. Deployed as `ogtr-internal`
// (k8s/internal-deployment.yaml): a cluster-internal ClusterIP service that
// must NEVER be exposed on any public ingress.
//
// The stock binary (backend/admin) calls adminapp.Run() with no options —
// behavior identical to the pre-package single-file main. A deployment may
// compose the same admin core with its own additions through the functional
// options (ARCHITECTURE.md §8 "Deployment composition"), mirroring
// backend/app for the public server: extra operator routes registered under
// the same /api/internal/* prefix and therefore behind the same token gate.
//
// One concern only: the /api/internal/* routes, guarded by the
// ADMIN_API_TOKEN gate (gate.go). No JWT middleware, no resolution, no
// migrations — schema is owned by the public server (backend/server), which
// migrates at boot; this service only reads/writes the same database
// through the shared stores/services packages.
package adminapp

import (
	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/handlers"
	"github.com/opengittr/ogtr/backend/services"
	"github.com/opengittr/ogtr/backend/stores"
)

// Run assembles the instance-admin service and starts serving. It never
// returns.
func Run(opts ...Option) {
	app := gofr.New()

	setup(app, newOptions(opts...))

	app.Run()
}

// setup wires the admin service onto app. Split from Run so tests can
// assemble without serving.
func setup(app *gofr.App, o *options) {
	// ADMIN_API_TOKEN unset (the default) keeps every /api/internal/* route
	// answering 404 — the service is dark, like the dev provider.
	adminToken := app.Config.Get("ADMIN_API_TOKEN")
	if adminToken == "" {
		app.Logger().Warnf("ADMIN_API_TOKEN is not set — every /api/internal/* endpoint answers 404")
	}

	// adminSvc reuses linkStore's SetStatusByID — operator disable/enable
	// shares the re-scan's abuse-status machinery exactly (public server).
	adminSvc := services.NewAdminService(stores.NewAdminStore(), stores.NewLinkStore())
	adminH := handlers.NewAdminHandler(adminSvc)

	// The gate covers everything under AdminPathPrefix — core routes below
	// AND deployment-registered ones (WithRoutes), which register under the
	// same prefix.
	app.UseMiddleware(AdminTokenGate(adminToken))

	// Instance admin API (deliberately cross-org — the sanctioned INV-6
	// exception; FEATURES.md §11). Same /api/internal/* paths as when these
	// routes lived on the public server: the console-gateway contract is
	// unchanged.
	app.GET("/api/internal/users", adminH.Users)
	app.GET("/api/internal/orgs", adminH.Orgs)
	app.GET("/api/internal/orgs/{id}/users", adminH.OrgUsers)
	app.GET("/api/internal/reports", adminH.Reports)
	app.GET("/api/internal/links/{id}", adminH.Link)
	app.POST("/api/internal/links/{id}/disable", adminH.DisableLink)
	app.POST("/api/internal/links/{id}/enable", adminH.EnableLink)
	app.GET("/api/internal/stats/daily", adminH.DailyStats)

	// Deployment-registered routes come last, after every core route.
	if o.routes != nil {
		o.routes(app, &Services{})
	}
}
